package seccomp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/flowgent-labs/flowgent/model/src"
	"golang.org/x/sys/unix"
)

const (
	seccompSetModeFilter = 1
	seccompFilterTSync   = 1 << 0
	seccompFilterNotif   = 1 << 2
)

type Filter struct {
	fprog     unix.SockFprog
	allowlist []ipPort
	denylist  []ipPort
	mode      string
}

type ipPort struct {
	ip   [4]byte
	port uint16
}

func BuildFilter(policy *model.NetworkPolicy) (*Filter, error) {
	if policy == nil {
		return &Filter{mode: "none"}, nil
	}
	mode := policy.Mode
	if mode == "" {
		mode = "none"
	}
	f := &Filter{mode: mode}
	switch mode {
	case "none":
	case "allowlist":
		list, err := resolveEntries(policy.Allowed)
		if err != nil {
			return nil, err
		}
		f.allowlist = list
	case "denylist":
		list, err := resolveEntries(policy.Denied)
		if err != nil {
			return nil, err
		}
		f.denylist = list
	default:
		return nil, fmt.Errorf("seccomp: unknown mode %q", mode)
	}
	f.fprog = buildSockFprog(mode)
	return f, nil
}

func (f *Filter) Mode() string        { return f.mode }
func (f *Filter) NeedsNotifier() bool { return f.mode == "allowlist" || f.mode == "denylist" }

func (f *Filter) Install() (notifyFD int, err error) {
	if f == nil || f.fprog.Filter == nil {
		return -1, nil
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return -1, fmt.Errorf("seccomp no_new_privs: %w", err)
	}
	flags := uintptr(seccompFilterTSync)
	if f.NeedsNotifier() {
		flags |= seccompFilterNotif
	}
	r1, _, e1 := syscall.Syscall(
		unix.SYS_SECCOMP, uintptr(seccompSetModeFilter), flags,
		uintptr(unsafe.Pointer(&f.fprog)),
	)
	if e1 != 0 {
		return -1, fmt.Errorf("seccomp: %w", e1)
	}
	if f.NeedsNotifier() {
		return int(r1), nil
	}
	return -1, nil
}

func (f *Filter) MarshalFilter() string {
	if f == nil || f.fprog.Filter == nil {
		return ""
	}
	n := int(f.fprog.Len)
	flat := make([]byte, n*8)
	for i := 0; i < n; i++ {
		inst := (*[1 << 16]unix.SockFilter)(unsafe.Pointer(f.fprog.Filter))[i]
		off := i * 8
		flat[off] = byte(inst.Code)
		flat[off+1] = byte(inst.Code >> 8)
		flat[off+2] = inst.Jt
		flat[off+3] = inst.Jf
		flat[off+4] = byte(inst.K)
		flat[off+5] = byte(inst.K >> 8)
		flat[off+6] = byte(inst.K >> 16)
		flat[off+7] = byte(inst.K >> 24)
	}
	return base64.StdEncoding.EncodeToString(flat)
}

func (f *Filter) MarshalAllowlist() string {
	if f == nil || len(f.allowlist) == 0 {
		return ""
	}
	type e struct {
		IP   string `json:"ip"`
		Port uint16 `json:"port"`
	}
	list := make([]e, len(f.allowlist))
	for i, a := range f.allowlist {
		list[i] = e{IP: net.IP(a.ip[:]).String(), Port: a.port}
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func buildSockFprog(mode string) unix.SockFprog {
	prog := buildBPF(mode)
	if len(prog) == 0 {
		return unix.SockFprog{}
	}
	return unix.SockFprog{Len: uint16(len(prog)), Filter: &prog[0]}
}

func resolveEntries(entries []string) ([]ipPort, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	var result []ipPort
	for _, entry := range entries {
		host, ps, err := net.SplitHostPort(entry)
		if err != nil {
			return nil, fmt.Errorf("seccomp: invalid %q: %w", entry, err)
		}
		port, err := strconv.Atoi(ps)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("seccomp: invalid port in %q", entry)
		}
		ips, err := net.LookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("seccomp: resolve %q: %w", host, err)
		}
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil {
				result = append(result, ipPort{ip: [4]byte(ip4), port: uint16(port)})
			}
		}
	}
	return result, nil
}

func syscallSeccomp(op, flags, args uintptr) (uintptr, uintptr, syscall.Errno) {
	return syscall.Syscall(unix.SYS_SECCOMP, op, flags, args)
}

func unsafeAddr(p *unix.SockFilter) uintptr {
	return uintptr(unsafe.Pointer(p))
}
