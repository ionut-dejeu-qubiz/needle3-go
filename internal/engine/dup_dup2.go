//go:build darwin || freebsd || netbsd || (linux && (amd64 || 386 || arm))

package engine

import (
	"os"
	"syscall"
)

// redirectChildStdout protects the worker protocol stream from C-level
// writes to stdout: the inherited stdout (the protocol pipe to the parent)
// is duplicated, then file descriptor 1 is re-pointed at stderr so any
// printf-style output the native engine might emit lands on the inherited
// stderr instead of corrupting the protocol.
func redirectChildStdout() *os.File {
	dupFD, err := syscall.Dup(1)
	if err != nil {
		// Without the duplication we can still run; the worker never
		// writes to os.Stdout directly after this point.
		return os.Stdout
	}
	if err := syscall.Dup2(2, 1); err != nil {
		// Keep the duplicate as the protocol stream anyway; fd 1 stays
		// pointed at the pipe, which is still correct for the protocol.
		return os.NewFile(uintptr(dupFD), "needle-worker-proto")
	}
	return os.NewFile(uintptr(dupFD), "needle-worker-proto")
}
