//go:build darwin || freebsd || linux || netbsd

package engine

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// openLibrary loads a shared object through purego's dlopen binding, which
// performs the dynamic loading with raw syscalls instead of cgo.
func openLibrary(path string) (uintptr, error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return 0, err
	}
	if handle == 0 {
		return 0, fmt.Errorf("dlopen returned a null handle for %s", path)
	}
	return handle, nil
}

// lookupSymbol resolves a symbol in an open library.
func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	addr, err := purego.Dlsym(handle, name)
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
		_ = purego.Dlclose(handle)
	}
}

// ensure unsafe stays referenced for the pointer conversions used by the
// generated trampolines (keeps goimports from trimming it in edits).
var _ = unsafe.Pointer(nil)
