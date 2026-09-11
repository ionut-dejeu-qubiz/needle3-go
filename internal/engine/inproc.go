package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ebitengine/purego"
)

// Native symbols of the Needle engine, as declared in needle.h:
//
//	int  needle_init(const char* system_prompt,
//	                 const char* tools_json,
//	                 const char* tool_index_path);
//	int  needle_complete(const char* input,
//	                     int max_new_tokens,
//	                     char* out,
//	                     int out_capacity);
//	void needle_reset(void);
//	int  needle_load(const unsigned char* cact, unsigned long long n);
//
// All functions return a negative code on failure. needle_init returns the
// prompt prefix token count on success; needle_complete returns the number
// of generated tokens on success.
type native struct {
	init     func(system, tools, index *byte) int32
	complete func(input *byte, maxTokens int32, out *byte, outCap int32) int32
	reset    func()
	load     func(data *byte, size uint64) int32
}

var (
	libsMu   sync.Mutex
	libs     = map[string]*native{} // keyed by resolved library path
	loadErrs = map[string]error{}
)

// LoadNative opens the shared library at path and binds its symbols. The
// library is opened once per path and reused for every later call.
// Loading goes through purego's dlopen on Unix and the stdlib
// syscall.LoadLibrary on Windows - no cgo anywhere.
func LoadNative(path string) (*native, error) {
	libsMu.Lock()
	defer libsMu.Unlock()
	if lib, ok := libs[path]; ok {
		return lib, nil
	}
	if err, ok := loadErrs[path]; ok {
		return nil, err
	}
	handle, err := openLibrary(path)
	if err != nil {
		err = fmt.Errorf("needle: cannot open engine library %s: %w", path, err)
		loadErrs[path] = err
		return nil, err
	}
	lib := &native{}
	if err := bindNative(lib, handle); err != nil {
		closeLibrary(handle)
		loadErrs[path] = err
		return nil, err
	}
	libs[path] = lib
	return lib, nil
}

func bindNative(lib *native, handle uintptr) error {
	bind := func(name string, assign func()) error {
		if _, err := lookupSymbol(handle, name); err != nil {
			return fmt.Errorf("needle: engine library is missing symbol %s: %w", name, err)
		}
		assign()
		return nil
	}
	if err := bind("needle_init", func() {
		purego.RegisterLibFunc(&lib.init, handle, "needle_init")
	}); err != nil {
		return err
	}
	if err := bind("needle_complete", func() {
		purego.RegisterLibFunc(&lib.complete, handle, "needle_complete")
	}); err != nil {
		return err
	}
	if err := bind("needle_reset", func() {
		purego.RegisterLibFunc(&lib.reset, handle, "needle_reset")
	}); err != nil {
		return err
	}
	if err := bind("needle_load", func() {
		purego.RegisterLibFunc(&lib.load, handle, "needle_load")
	}); err != nil {
		return err
	}
	return nil
}

// cstring converts a Go string to a NUL-terminated C string, returning nil
// for the empty string so the engine sees a NULL pointer.
func cstring(s string) *byte {
	if s == "" {
		return nil
	}
	b := append(make([]byte, 0, len(s)+1), s...)
	b = append(b, 0)
	return &b[0]
}

// The engine keeps global state, so at most one session may be bound per
// generation within a process. Sessions take turns; switching sessions
// reconfigures the engine (which also rewinds the conversation).
var (
	stateMu      sync.Mutex
	activeInproc = map[int]*Inproc{}
	weightsBound = map[int]bool{}
)

// Inproc is a Backend that calls the native engine in the current process.
// It is the backend used for the base model whose weights are baked into
// the engine binary.
type Inproc struct {
	lib       *native
	gen       int
	system    string
	toolsJSON string
	toolIndex string
	buf       []byte
	prefixTok int
	closed    bool
}

// NewInproc creates an in-process backend for the library at libPath.
func NewInproc(libPath string, gen int, system, toolsJSON, toolIndex string, bufSize int) (*Inproc, error) {
	lib, err := LoadNative(libPath)
	if err != nil {
		return nil, err
	}
	if bufSize <= 0 {
		bufSize = 65536
	}
	return &Inproc{
		lib:       lib,
		gen:       gen,
		system:    system,
		toolsJSON: toolsJSON,
		toolIndex: toolIndex,
		buf:       make([]byte, bufSize),
	}, nil
}

// PrefixTokens reports the prompt prefix token count reported by the last
// successful needle_init call for this session.
func (s *Inproc) PrefixTokens() int {
	stateMu.Lock()
	defer stateMu.Unlock()
	return s.prefixTok
}

// bind reconfigures the engine for this session if it is not the active one.
func (s *Inproc) bind() error {
	if activeInproc[s.gen] == s {
		return nil
	}
	if weightsBound[s.gen] {
		// The engine cannot unload weights: a tuned archive was loaded into
		// this process, so a base-model session can no longer run here.
		return fmt.Errorf("needle: this process already holds tuned weights for engine generation %d; "+
			"the engine cannot unload them - run base-model agents in a separate process", s.gen)
	}
	rc := s.lib.init(cstring(s.system), cstring(s.toolsJSON), cstring(s.toolIndex))
	if rc < 0 {
		delete(activeInproc, s.gen)
		return fmt.Errorf("needle: needle_init failed (code %d)", rc)
	}
	activeInproc[s.gen] = s
	s.prefixTok = int(rc)
	return nil
}

// LoadWeights injects a .cact weights archive into the engine of the
// current process. Once loaded the weights cannot be unloaded; every later
// session in this process runs on them. Tuned agents normally isolate
// themselves in a worker child process instead of calling this directly.
func LoadWeights(libPath string, gen int, cact []byte) error {
	lib, err := LoadNative(libPath)
	if err != nil {
		return err
	}
	if len(cact) == 0 {
		return fmt.Errorf("needle: weights archive is empty")
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	rc := lib.load(&cact[0], uint64(len(cact)))
	if rc < 0 {
		return fmt.Errorf("needle: needle_load failed (code %d)", rc)
	}
	weightsBound[gen] = true
	return nil
}

// WeightsBound reports whether a .cact archive has been loaded into this
// process for the given engine generation.
func WeightsBound(gen int) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return weightsBound[gen]
}

// Complete runs one inference turn in-process.
func (s *Inproc) Complete(_ context.Context, input string, maxNewTokens int) (string, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if s.closed {
		return "", fmt.Errorf("needle: session is closed")
	}
	if err := s.bind(); err != nil {
		return "", err
	}
	if maxNewTokens <= 0 {
		maxNewTokens = 256
	}
	rc := s.lib.complete(cstring(input), int32(maxNewTokens), &s.buf[0], int32(len(s.buf)))
	if rc < 0 {
		detail := cbufferString(s.buf)
		if detail == "" {
			return "", fmt.Errorf("needle: needle_complete failed (code %d)", rc)
		}
		return "", fmt.Errorf("%s", detail)
	}
	return cbufferString(s.buf), nil
}

// Reset rewinds the conversation of the active session.
func (s *Inproc) Reset() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	if s.closed {
		return fmt.Errorf("needle: session is closed")
	}
	if err := s.bind(); err != nil {
		return err
	}
	s.lib.reset()
	return nil
}

// Close marks the session closed. The shared library itself stays loaded
// for the lifetime of the process.
func (s *Inproc) Close() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if activeInproc[s.gen] == s {
		delete(activeInproc, s.gen)
	}
	return nil
}

// cbufferString reads the NUL-terminated message the engine wrote into the
// response buffer, stopping at the first NUL and sanitizing invalid UTF-8.
func cbufferString(buf []byte) string {
	n := bytes.IndexByte(buf, 0)
	if n < 0 {
		n = len(buf)
	}
	s := string(buf[:n])
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	return s
}

// GenerationOfCact reports the engine generation of a .cact archive held
// in memory.
func GenerationOfCact(data []byte) (int, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("not a complete .cact archive")
	}
	tag := binary.LittleEndian.Uint32(data[:4])
	gen, ok := WeightGeneration(tag)
	if !ok {
		return 0, fmt.Errorf("unknown .cact format tag 0x%08x; cannot choose a compatible engine", tag)
	}
	return gen, nil
}

// GenerationOfCactFile reports the engine generation of a .cact file.
func GenerationOfCactFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := f.Read(head); err != nil {
		return 0, fmt.Errorf("%s is not a complete .cact archive: %w", path, err)
	}
	tag := binary.LittleEndian.Uint32(head)
	gen, ok := WeightGeneration(tag)
	if !ok {
		return 0, fmt.Errorf("%s has unknown .cact format tag 0x%08x; cannot choose a compatible Needle engine", path, tag)
	}
	return gen, nil
}
