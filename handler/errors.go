//go:build !windows

package handler

import "syscall"

// errEPIPE is the OS broken-pipe error. Matches via errors.Is when the
// underlying write/read syscall returned EPIPE (wrapped in *os.SyscallError /
// *net.OpError chains by the stdlib).
var errEPIPE = syscall.EPIPE
