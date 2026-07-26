//go:build linux

package seccomp

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// seccompNotifSize is sizeof(struct seccomp_notif) on x86_64:
//
//	id(8) + pid(4) + flags(4) + seccomp_data(64) = 80 bytes
const seccompNotifSize = 80

// seccompNotifRespSize is sizeof(struct seccomp_notif_resp) on x86_64:
//
//	id(8) + val(8) + error(4) + flags(4) = 24 bytes
const seccompRespSize = 24

// Notifier handles SECCOMP_RET_USER_NOTIF events from a seccomp filter.
// Reads connection requests from the kernel, inspects the target sockaddr
// via /proc/<pid>/mem, and allows or denies each request against the
// pre-resolved allowlist/denylist.
//
// DNS (port 53) is auto-allowed regardless of the allowlist.
type Notifier struct {
	fd        *os.File
	allowlist []ipPort
	denylist  []ipPort
	done      chan struct{}
}

func newNotifier(fd *os.File, allowlist, denylist []ipPort) *Notifier {
	return &Notifier{
		fd:        fd,
		allowlist: allowlist,
		denylist:  denylist,
		done:      make(chan struct{}),
	}
}

// Start begins the notifier loop. Call this in a goroutine.
// Returns when Close() is called or the fd is closed.
func (n *Notifier) Start() {
	defer n.fd.Close()
	buf := make([]byte, seccompNotifSize)
	resp := make([]byte, seccompRespSize)

	for {
		select {
		case <-n.done:
			return
		default:
		}

		// Read a seccomp_notif from the kernel.
		_, err := io.ReadFull(n.fd, buf)
		if err != nil {
			return // fd closed or kernel stopped sending
		}

		id := binary.LittleEndian.Uint64(buf[0:8])
		pid := binary.LittleEndian.Uint32(buf[8:12])
		_ = buf[12:16] // flags (unused)

		// seccomp_data starts at offset 16 in the notification.
		data := buf[16:seccompNotifSize]

		// Syscall number is first 4 bytes of seccomp_data.
		// Arguments are at offset 16 within seccomp_data (data[16:64]).
		args := data[16:64]

		// args[1] is the sockaddr pointer for connect/sendto/sendmsg.
		sockaddrPtr := binary.LittleEndian.Uint64(args[8:16])

		allow, errno := n.checkTarget(pid, sockaddrPtr)

		// Build response.
		binary.LittleEndian.PutUint64(resp[0:8], id)
		if allow {
			binary.LittleEndian.PutUint64(resp[8:16], 0)
			binary.LittleEndian.PutUint32(resp[16:20], 0)
			binary.LittleEndian.PutUint32(resp[20:24], unix.SECCOMP_USER_NOTIF_FLAG_CONTINUE)
		} else {
			binary.LittleEndian.PutUint64(resp[8:16], 0)
			binary.LittleEndian.PutUint32(resp[16:20], uint32(errno))
			binary.LittleEndian.PutUint32(resp[20:24], 0)
		}

		n.fd.Write(resp)
	}
}

// checkTarget reads the sockaddr from the child's memory and checks
// whether the target (ip, port) is allowed.
func (n *Notifier) checkTarget(pid uint32, sockaddrPtr uint64) (allow bool, errno int32) {
	if sockaddrPtr == 0 {
		return false, int32(syscall.EPERM)
	}

	ip, port, err := readSockaddr(pid, sockaddrPtr)
	if err != nil {
		return false, int32(syscall.EPERM)
	}

	// DNS (port 53) is always allowed so hostnames can be resolved.
	if port == 53 {
		return true, 0
	}

	switch {
	case len(n.allowlist) > 0:
		for _, e := range n.allowlist {
			if e.ip == ip && e.port == port {
				return true, 0
			}
		}
		return false, int32(syscall.EPERM)

	case len(n.denylist) > 0:
		for _, e := range n.denylist {
			if e.ip == ip && e.port == port {
				return false, int32(syscall.EPERM)
			}
		}
		return true, 0

	default:
		return false, int32(syscall.EPERM)
	}
}

// readSockaddr reads sizeof(sockaddr_in) = 16 bytes from /proc/<pid>/mem
// at the given address and extracts the sin_addr and sin_port.
//
// struct sockaddr_in (x86_64, little-endian):
//
//	sin_family: u16 at offset 0
//	sin_port:   u16 at offset 2 (network byte order)
//	sin_addr:   u32 at offset 4 (network byte order)
func readSockaddr(pid uint32, addr uint64) (ip [4]byte, port uint16, err error) {
	memPath := fmt.Sprintf("/proc/%d/mem", pid)
	f, err := os.Open(memPath)
	if err != nil {
		return [4]byte{}, 0, err
	}
	defer f.Close()

	var raw [16]byte
	n, err := f.ReadAt(raw[:], int64(addr))
	if err != nil || n < 16 {
		return [4]byte{}, 0, fmt.Errorf("read mem at %#x: %w", addr, err)
	}

	family := binary.LittleEndian.Uint16(raw[0:2])
	if family != unix.AF_INET {
		// Not IPv4 — deny by default.
		return [4]byte{}, 0, fmt.Errorf("non-IPv4 family: %d", family)
	}

	port = binary.BigEndian.Uint16(raw[2:4])
	copy(ip[:], raw[4:8])
	return ip, port, nil
}

// SetAllowlist sets the allowlist after the notifier is constructed.
func (n *Notifier) SetAllowlist(list []ipPort) { n.allowlist = list }

// SetDenylist sets the denylist after the notifier is constructed.
func (n *Notifier) SetDenylist(list []ipPort) { n.denylist = list }

// Close stops the notifier loop.
func (n *Notifier) Close() {
	select {
	case <-n.done:
	default:
		close(n.done)
	}
}
