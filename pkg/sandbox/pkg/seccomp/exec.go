//go:build linux

package seccomp

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/unix"
)

// ScriptCmd prepares an *exec.Cmd that runs the given script with the seccomp
// filter applied. The caller should Start() the command and then, if the filter
// uses a notifier, read the notifier from the returned channel.
func (f *Filter) ScriptCmd(scriptPath, runtimeName, workspace string) (*exec.Cmd, <-chan *Notifier, error) {
	// If no filter needed, run directly.
	if f == nil || (f.mode == "none" && len(f.allowlist) == 0 && len(f.denylist) == 0) {
		return f.directCmd(scriptPath, runtimeName, workspace), nil, nil
	}

	needNotifier := f.NeedsNotifier()

	exe, err := os.Executable()
	if err != nil {
		exe = "/proc/self/exe"
	}

	args := []string{
		"sandbox-child",
		"--filter=" + f.MarshalFilter(),
		"--mode=" + f.mode,
		"--script=" + scriptPath,
		"--runtime=" + runtimeName,
	}
	if len(f.allowlist) > 0 {
		args = append(args, "--allowlist="+f.MarshalAllowlist())
	}
	if len(f.denylist) > 0 {
		args = append(args, "--denylist="+f.MarshalAllowlist())
	}

	cmd := exec.Command(exe, args...)
	cmd.Dir = workspace

	var notifCh chan *Notifier

	if needNotifier {
		// Create socketpair for fd passing from child to parent.
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		if err != nil {
			return nil, nil, fmt.Errorf("seccomp socketpair: %w", err)
		}
		parentFD := fds[0]
		childFD := fds[1]

		cmd.ExtraFiles = []*os.File{os.NewFile(uintptr(childFD), "seccomp-sock")}
		cmd.Env = append(os.Environ(),
			"SECCOMP_SOCK_FD=3",
		)

		notifCh = make(chan *Notifier, 1)
		go func() {
			defer unix.Close(parentFD)
			sock := os.NewFile(uintptr(parentFD), "seccomp-sock-parent")
			notif, err := recvNotifier(sock)
			if err != nil {
				notifCh <- nil
				return
			}
			notif.SetAllowlist(f.allowlist)
			notif.SetDenylist(f.denylist)
			notifCh <- notif
		}()
	}

	return cmd, notifCh, nil
}

// directCmd returns a plain command without seccomp filtering.
func (f *Filter) directCmd(scriptPath, runtimeName, workspace string) *exec.Cmd {
	scriptFile := filepath.Join(scriptPath, "script."+extForRuntime(runtimeName))
	var cmd *exec.Cmd
	switch runtimeName {
	case "python3":
		cmd = exec.Command("python3", scriptFile)
	case "node":
		cmd = exec.Command("node", scriptFile)
	default:
		cmd = exec.Command("bash", scriptFile)
	}
	cmd.Dir = workspace
	return cmd
}

// ── Child process (re-exec via /proc/self/exe sandbox-child) ──

// ChildMain is the entry point for the sandbox-child subcommand.
// It reads filter parameters from command-line flags, installs the seccomp
// filter, optionally passes the notifier fd back to the parent, and execs
// the target script.
func ChildMain() error {
	filterB64 := flagArg("filter")
	mode := flagArg("mode")
	scriptPath := flagArg("script")
	runtimeName := flagArg("runtime")

	if filterB64 == "" || scriptPath == "" {
		return fmt.Errorf("seccomp-child: --filter and --script are required")
	}

	// Decode filter program.
	flat, err := base64.StdEncoding.DecodeString(filterB64)
	if err != nil {
		return fmt.Errorf("seccomp-child: decode filter: %w", err)
	}
	n := len(flat) / 8
	if n == 0 {
		return fmt.Errorf("seccomp-child: empty filter")
	}
	insns := make([]unix.SockFilter, n)
	for i := 0; i < n; i++ {
		off := i * 8
		insns[i] = unix.SockFilter{
			Code: uint16(flat[off]) | uint16(flat[off+1])<<8,
			Jt:   flat[off+2],
			Jf:   flat[off+3],
			K:    uint32(flat[off+4]) | uint32(flat[off+5])<<8 | uint32(flat[off+6])<<16 | uint32(flat[off+7])<<24,
		}
	}

	// Install seccomp filter via syscall.
	flags := uintptr(seccompFilterTSync)
	notifierNeeded := mode == "allowlist" || mode == "denylist"
	if notifierNeeded {
		flags |= seccompFilterNotif
	}

	runtime.LockOSThread()
	r1, _, e1 := syscallSeccomp(seccompSetModeFilter, flags, uintptr(unsafeAddr(&insns[0])))
	if e1 != 0 {
		return fmt.Errorf("seccomp-child: install filter: %w", e1)
	}

	// If notifier needed, pass the notifier fd back to the parent via socket.
	if notifierNeeded && int(r1) >= 0 {
		notifFD := int(r1)
		sockFDStr := os.Getenv("SECCOMP_SOCK_FD")
		if sockFDStr != "" {
			// Send fd to parent via SCM_RIGHTS.
			var sockFD uintptr
			fmt.Sscanf(sockFDStr, "%d", &sockFD)
			sock := os.NewFile(sockFD, "seccomp-sock-child")
			if sock != nil {
				rights := unix.UnixRights(notifFD)
				unix.Sendmsg(int(sock.Fd()), []byte("ready"), rights, nil, 0)
				sock.Close()
			}
			unix.Close(notifFD)
		}
	}

	// Exec the actual script.
	scriptFile := filepath.Join(scriptPath, "script."+extForRuntime(runtimeName))
	realExe, _ := exec.LookPath(runtimeName)
	if realExe == "" {
		realExe = "/bin/bash"
	}
	return unix.Exec(realExe, []string{filepath.Base(realExe), scriptFile}, os.Environ())
}

// recvNotifier receives the notifier fd from the child via SCM_RIGHTS.
func recvNotifier(sock *os.File) (*Notifier, error) {
	defer sock.Close()

	buf := make([]byte, 64)
	oob := make([]byte, unix.CmsgSpace(4))

	n, oobn, _, _, err := unix.Recvmsg(int(sock.Fd()), buf, oob, 0)
	if err != nil {
		return nil, fmt.Errorf("recv notifier: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("recv notifier: empty message")
	}

	cmsgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, fmt.Errorf("parse cmsg: %w", err)
	}
	if len(cmsgs) == 0 {
		return nil, fmt.Errorf("no cmsg received")
	}

	fds, err := unix.ParseUnixRights(&cmsgs[0])
	if err != nil {
		return nil, fmt.Errorf("parse rights: %w", err)
	}
	if len(fds) == 0 {
		return nil, fmt.Errorf("no fd received")
	}

	return &Notifier{
		fd:   os.NewFile(uintptr(fds[0]), "seccomp-notify"),
		done: make(chan struct{}),
	}, nil
}

// ── Helpers ─────────────────────────────────────────────────────

func flagArg(name string) string {
	for _, a := range os.Args {
		prefix := "--" + name + "="
		if len(a) > len(prefix) && a[:len(prefix)] == prefix {
			return a[len(prefix):]
		}
	}
	return ""
}

func extForRuntime(r string) string {
	switch r {
	case "python3":
		return "py"
	case "node":
		return "js"
	default:
		return "sh"
	}
}
