//go:build windows

package engine

import (
	"fmt"
	"syscall"
	"unsafe"
)

// openLibrary loads a DLL through the standard library's LoadLibrary, the
// same approach purego's own examples use on Windows. Combined with
// purego.RegisterLibFunc this keeps the build cgo-free and
// cross-compilable from any platform.
func openLibrary(path string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(path)
	if err != nil {
		return 0, err
	}
	if handle == 0 {
		return 0, fmt.Errorf("LoadLibrary returned a null handle for %s", path)
	}
	return uintptr(handle), nil
}

// lookupSymbol resolves an exported symbol in an open DLL.
func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	addr, err := syscall.GetProcAddress(syscall.Handle(handle), name)
	if err != nil {
		return 0, err
	}
	if addr == 0 {
		return 0, fmt.Errorf("symbol %s resolved to a null address", name)
	}
	return addr, nil
}

// closeLibrary drops a library reference opened by openLibrary.
func closeLibrary(handle uintptr) {
	if handle != 0 {
		_ = syscall.FreeLibrary(syscall.Handle(handle))
	}
}

var _ = unsafe.Pointer(nil)
