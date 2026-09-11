//go:build linux && (arm64 || riscv64 || loong64 || ppc64le || s390x || mips || mipsle || mips64 || mips64le)

package engine

import (
	"os"
	"syscall"
)

// redirectChildStdout is the arm64/riscv64 flavour of the fd dance: those
// kernels expose dup3 instead of dup2. Flags 0 makes them equivalent.
func redirectChildStdout() *os.File {
	dupFD, err := syscall.Dup(1)
	if err != nil {
		return os.Stdout
	}
	if err := syscall.Dup3(2, 1, 0); err != nil {
		return os.NewFile(uintptr(dupFD), "needle-worker-proto")
	}
	return os.NewFile(uintptr(dupFD), "needle-worker-proto")
}
