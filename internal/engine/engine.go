// Package engine contains the low-level bindings to the native Needle
// inference engine (libneedle.so / libneedle.dll / libneedle.dylib).
//
// Two backends implement the same contract:
//
//   - Inproc: the shared library is loaded into the current process with
//     purego (dlopen on Unix, LoadLibrary on Windows; no cgo anywhere) and
//     the four exported C functions are called directly. This is the path
//     used for the base model baked into the engine binary.
//   - Worker: a child process - a re-exec of the host binary - loads the
//     library, injects a tuned .cact weights archive with needle_load, and
//     serves length-prefixed JSON requests over stdin/stdout. The native
//     engine keeps global state and cannot unload weights, so each tuned
//     agent gets its own process.
package engine

import "context"

// Backend is a running engine session bound to one toolset.
type Backend interface {
	// Complete feeds one turn to the engine and returns the raw JSON
	// response envelope written by the engine.
	Complete(ctx context.Context, input string, maxNewTokens int) (string, error)
	// Reset rewinds the conversation, keeping the tools loaded.
	Reset() error
	// Close releases resources held by the backend. It is safe to call
	// more than once.
	Close() error
}

// CactGenerations maps the little-endian tag in the first four bytes of a
// .cact archive to the engine generation that understands it.
var CactGenerations = map[uint32]int{
	0x05E12A83: 2,
	0x05E12A84: 3,
}

// WeightGeneration reads the format tag of a .cact archive and reports the
// engine generation it belongs to.
func WeightGeneration(tag uint32) (int, bool) {
	gen, ok := CactGenerations[tag]
	return gen, ok
}
