//go:build windows

package handler

import "syscall"

// errEPIPE is the Windows analogue of EPIPE — a connection aborted by the
// software in the host computer (data-transmission timeout or protocol error).
var errEPIPE = syscall.WSAECONNABORTED
