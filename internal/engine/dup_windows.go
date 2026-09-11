//go:build windows

package engine

import "os"

// redirectChildStdout protects the worker protocol stream as well as it
// can on Windows: the Go-level os.Stdout is captured as the protocol
// stream and the package variable is re-pointed at stderr so later Go
// writes do not interleave. C runtime writes that go straight to the
// process handle remain on the pipe; the engine only produces those under
// NEEDLE_DEBUG, which is documented as unsupported for tuned workers on
// Windows.
func redirectChildStdout() *os.File {
	proto := os.Stdout
	os.Stdout = os.Stderr
	return proto
}
