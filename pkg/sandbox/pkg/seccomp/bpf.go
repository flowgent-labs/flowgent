//go:build linux

package seccomp

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// x86_64 syscall numbers targeted by the filter.
const (
	sysSocket        = 41  // __NR_socket
	sysConnect       = 42  // __NR_connect
	sysSendto        = 44  // __NR_sendto
	sysSendmsg       = 46  // __NR_sendmsg
	sysSendmmsg      = 307 // __NR_sendmmsg
	sysBpf           = 321 // __NR_bpf
	sysInitModule    = 175 // __NR_init_module
	sysKexecLoad     = 246 // __NR_kexec_load
	sysPerfEventOpen = 298 // __NR_perf_event_open
)

// Build a seccomp-bpf filter program as a []unix.SockFilter.
//
// x86_64 syscall numbers filtered:
//
//	__NR_socket(41)   → block SOCK_RAW, allow others
//	__NR_connect(42)  → mode "none": block; allowlist/denylist: USER_NOTIF
//	__NR_sendto(44)   → mode "none": block; allowlist/denylist: allow
//	__NR_sendmsg(46)  → mode "none": block; allowlist/denylist: allow
//	__NR_sendmmsg(307)→ mode "none": block; allowlist/denylist: allow
//	__NR_bpf(321)     → always block (privilege escalation)
//	__NR_init_module(175)   → always block
//	__NR_kexec_load(246)    → always block
//	__NR_perf_event_open(298) → always block
//
// Returns SECCOMP_RET_ALLOW for everything else.
func buildBPF(mode string) []unix.SockFilter {
	useNotif := mode == "allowlist" || mode == "denylist"
	b := newBuilder()

	// Block privilege-escalation syscalls unconditionally.
	for _, nr := range []uint32{sysBpf, sysInitModule, sysKexecLoad, sysPerfEventOpen} {
		b.loadNr()
		b.jeqKill(nr)
	}

	// socket(): block SOCK_RAW, allow everything else.
	b.loadNr()
	b.jeq2(nrSock) // if socket, jump to next insn (handler)
	b.ja(5)        // else skip handler (+5 → next block)
	// handler:
	b.ldArg(sockTypeArg)  // load socket type (arg[1] low 32 bits)
	b.jeqKill(sysSockRaw) // SOCK_RAW → kill
	b.retAllow()          // all other socket types allowed

	if useNotif {
		b.loadNr()
		b.jeqNotif(sysConnect)
	} else {
		for _, nr := range []uint32{sysConnect, sysSendto, sysSendmsg, sysSendmmsg} {
			b.loadNr()
			b.jeqKill(nr)
		}
	}

	b.retAllow()
	return b.insns
}

// ── BPF builder with label-based jump resolution ──────────────

type builder struct {
	insns []unix.SockFilter
}

func newBuilder() *builder { return &builder{insns: make([]unix.SockFilter, 0, 64)} }

func (b *builder) loadNr() {
	b.insns = append(b.insns, ld(offNr))
}

func (b *builder) ldArg(offset uint32) {
	b.insns = append(b.insns, ld(offset))
}

// jeqKill emits: JEQ nr, jt=1, jf=0 ; JA 1 ; RET_ERRNO(EPERM)
// If nr matches → skip JA → kill. Otherwise → JA 1 → skip kill → continue.
func (b *builder) jeqKill(nr uint32) {
	b.insns = append(b.insns, jeq(nr, 1, 0))
	b.insns = append(b.insns, ja(1))
	b.insns = append(b.insns, retErrno(uint16(syscall.EPERM)))
}

// jeqNotif emits: JEQ nr, jt=1, jf=0 ; JA 1 ; RET_USER_NOTIF
func (b *builder) jeqNotif(nr uint32) {
	b.insns = append(b.insns, jeq(nr, 1, 0))
	b.insns = append(b.insns, ja(1))
	b.insns = append(b.insns, retUserNotif())
}

// jeq emits: JEQ val, jt=X, jf=Y (jump forward X instructions if equal)
func (b *builder) jeq(val uint32, jt, jf uint8) {
	b.insns = append(b.insns, jeq(val, jt, jf))
}

// jeq2 emits: JEQ val, jt=1, jf=0 → if equal, skip the JA guard and enter
// the handler; else take the JA guard which jumps over the handler.
func (b *builder) jeq2(val uint32) {
	b.insns = append(b.insns, jeq(val, 1, 0))
}

// ja emits unconditional jump forward N instructions.
func (b *builder) ja(n uint8) {
	b.insns = append(b.insns, ja(n))
}

func (b *builder) retAllow() { b.insns = append(b.insns, retAllow()) }
func (b *builder) retKill()  { b.insns = append(b.insns, retErrno(uint16(syscall.EPERM))) }

// ── seccomp_data offsets (x86_64) ─────────────────────────────

const (
	offNr       = 0       // u32: syscall number
	offArg0     = 16      // u64: first argument
	offArg1     = 24      // u64: second argument
	sockTypeArg = offArg1 // syscall socket's 2nd arg = socket type
	nrSock      = 41      // __NR_socket
	sysSockRaw  = 3       // SOCK_RAW
)

// ── individual BPF instruction constructors ───────────────────

func ld(offset uint32) unix.SockFilter {
	return unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: offset}
}

func jeq(value uint32, jt, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: jt, Jf: jf, K: value}
}

func ja(jumps uint8) unix.SockFilter {
	return unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JA, K: uint32(jumps)}
}

func retErrno(errno uint16) unix.SockFilter {
	return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(errno)}
}

func retUserNotif() unix.SockFilter {
	return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_USER_NOTIF}
}

func retAllow() unix.SockFilter {
	return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW}
}
