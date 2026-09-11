package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Worker runs a tuned engine in a child process and speaks the
// length-prefixed JSON protocol over its stdin/stdout. The child is a
// re-exec of the current binary started with the "--needle-worker"
// argument and a matching NEEDLE_GO_WORKER nonce; the library's package
// init detects this and runs the child loop before main() gets a chance
// to execute.
type Worker struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	mu      sync.Mutex
	closed  bool
	prefix  int
	exited  chan struct{}
	waitErr error
	once    sync.Once
}

// WorkerArg is the argv marker that selects worker mode.
const WorkerArg = "--needle-worker"

// WorkerEnv is the environment variable carrying the worker nonce.
const WorkerEnv = "NEEDLE_GO_WORKER"

// IsWorker reports whether the current process was started as a worker
// child by StartWorker.
func IsWorker() bool {
	return len(os.Args) >= 3 && os.Args[1] == WorkerArg &&
		os.Getenv(WorkerEnv) == os.Args[2]
}

// WorkerNonce returns the nonce a parent passes when re-execing itself.
func WorkerNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("w%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// StartWorker launches the child process and initializes it with the
// given configuration.
func StartWorker(ctx context.Context, libPath, weightsPath, system, toolsJSON, toolIndex string, bufSize, generation int) (*Worker, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("needle: cannot re-exec worker (os.Executable: %w)", err)
	}
	nonce := WorkerNonce()
	cmd := exec.Command(exe, WorkerArg, nonce)
	cmd.Env = append(os.Environ(), WorkerEnv+"="+nonce)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("needle: cannot start worker process: %w", err)
	}
	w := &Worker{cmd: cmd, stdin: stdin, stdout: stdout, exited: make(chan struct{})}
	go func() { w.waitErr = cmd.Wait(); close(w.exited) }()

	cfg := workerConfig{
		Library:    libPath,
		Weights:    weightsPath,
		System:     system,
		Tools:      toolsJSON,
		ToolIndex:  toolIndex,
		BufferSize: bufSize,
		Generation: generation,
	}
	if err := writeMessage(stdin, cfg); err != nil {
		w.Close()
		return nil, fmt.Errorf("needle: worker failed to start: %w", err)
	}
	// Allow a generous startup budget: the child loads the .cact archive
	// and embeds the tool catalogue before reporting readiness.
	startup := 300 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < startup {
			startup = remaining
		}
	}
	var ready workerResponse
	if err := w.receive(ctx, &ready, startup); err != nil {
		w.Close()
		return nil, fmt.Errorf("needle: worker did not initialize: %w", err)
	}
	if ready.Status != "ready" {
		w.Close()
		return nil, fmt.Errorf("needle: worker failed to start: %s", ready.Message)
	}
	w.prefix = ready.PrefixTokens
	return w, nil
}

// PrefixTokens reports the prompt prefix token count the child observed
// after loading weights and initializing the engine.
func (w *Worker) PrefixTokens() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.prefix
}

// receive reads one response message, honoring ctx and timeout.
func (w *Worker) receive(ctx context.Context, out *workerResponse, timeout time.Duration) error {
	type result struct {
		msg workerResponse
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var msg workerResponse
		err := readMessage(w.stdout, &msg)
		ch <- result{msg, err}
	}()
	var timerC <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timerC = t.C
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timerC:
		return fmt.Errorf("worker did not respond in time")
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("worker exited unexpectedly: %w", r.err)
		}
		*out = r.msg
		return nil
	}
}

// request sends one operation and waits for its reply.
func (w *Worker) request(ctx context.Context, req workerRequest) (*workerResponse, error) {
	if err := writeMessage(w.stdin, req); err != nil {
		return nil, fmt.Errorf("needle: worker exited unexpectedly: %w", err)
	}
	var resp workerResponse
	if err := w.receive(ctx, &resp, 0); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Complete runs one inference turn in the child.
func (w *Worker) Complete(ctx context.Context, input string, maxNewTokens int) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return "", fmt.Errorf("needle: worker is closed")
	}
	resp, err := w.request(ctx, workerRequest{
		Operation:    "complete",
		Text:         input,
		MaxNewTokens: maxNewTokens,
	})
	if err != nil {
		return "", err
	}
	if resp.Status != "ok" {
		return "", fmt.Errorf("needle: %s", resp.Message)
	}
	return resp.Response, nil
}

// Reset rewinds the conversation in the child.
func (w *Worker) Reset() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("needle: worker is closed")
	}
	resp, err := w.request(context.Background(), workerRequest{Operation: "reset"})
	if err != nil {
		return err
	}
	if resp.Status != "ok" {
		return fmt.Errorf("needle: %s", resp.Message)
	}
	return nil
}

// Close shuts the worker down: best-effort handshake, then a graceful
// timeout, then escalation to kill.
func (w *Worker) Close() error {
	w.once.Do(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.closed {
			return
		}
		w.closed = true
		if w.cmd.Process != nil {
			_ = writeMessage(w.stdin, workerRequest{Operation: "close"})
			_ = w.stdin.Close()
			select {
			case <-w.exited:
			case <-time.After(2 * time.Second):
				_ = w.cmd.Process.Kill()
				<-w.exited
			}
		}
		_ = w.stdout.Close()
	})
	return nil
}

// RunWorkerChild implements the child side of the protocol on the given
// streams. It returns the process exit code. The function never returns
// normally in interactive use; callers should os.Exit with the result.
func RunWorkerChild(proto io.Reader) int {
	// Route C-level stdout writes of the engine away from the protocol
	// stream: on Unix fd 1 is re-pointed at stderr while the protocol
	// keeps a duplicate of the parent pipe.
	stdout := redirectChildStdout()

	var cfg workerConfig
	if err := readMessage(proto, &cfg); err != nil {
		return 1
	}
	gen := cfg.Generation
	if gen < 2 {
		gen = 2
	}
	lib, err := LoadNative(cfg.Library)
	if err != nil {
		_ = writeMessage(stdout, workerResponse{Status: "fatal", Message: err.Error()})
		return 1
	}
	weights, err := os.ReadFile(cfg.Weights)
	if err != nil {
		_ = writeMessage(stdout, workerResponse{Status: "fatal", Message: err.Error()})
		return 1
	}
	if len(weights) < 4 {
		_ = writeMessage(stdout, workerResponse{Status: "fatal",
			Message: fmt.Sprintf("%s is not a complete .cact archive", cfg.Weights)})
		return 1
	}
	if rc := lib.load(&weights[0], uint64(len(weights))); rc < 0 {
		_ = writeMessage(stdout, workerResponse{Status: "fatal",
			Message: fmt.Sprintf("needle_load failed for %s (code %d)", cfg.Weights, rc)})
		return 1
	}
	if err := markWeightsBound(gen); err != nil {
		_ = writeMessage(stdout, workerResponse{Status: "fatal", Message: err.Error()})
		return 1
	}
	prefix := lib.init(cstring(cfg.System), cstring(cfg.Tools), cstring(cfg.ToolIndex))
	if prefix < 0 {
		_ = writeMessage(stdout, workerResponse{Status: "fatal",
			Message: fmt.Sprintf("needle_init failed (code %d)", prefix)})
		return 1
	}
	bufSize := cfg.BufferSize
	if bufSize <= 0 {
		bufSize = 65536
	}
	buf := make([]byte, bufSize)
	if err := writeMessage(stdout, workerResponse{Status: "ready", PrefixTokens: int(prefix)}); err != nil {
		return 1
	}
	for {
		var req workerRequest
		if err := readMessage(proto, &req); err != nil {
			if err == io.EOF {
				return 0
			}
			_ = writeMessage(stdout, workerResponse{Status: "fatal", Message: err.Error()})
			return 1
		}
		switch req.Operation {
		case "complete":
			maxTok := req.MaxNewTokens
			if maxTok <= 0 {
				maxTok = 256
			}
			rc := lib.complete(cstring(req.Text), int32(maxTok), &buf[0], int32(len(buf)))
			if rc < 0 {
				_ = writeMessage(stdout, workerResponse{Status: "error",
					Message: cbufferString(buf)})
			} else {
				_ = writeMessage(stdout, workerResponse{Status: "ok",
					Response: cbufferString(buf)})
			}
		case "reset":
			lib.reset()
			_ = writeMessage(stdout, workerResponse{Status: "ok"})
		case "close":
			_ = writeMessage(stdout, workerResponse{Status: "ok"})
			return 0
		default:
			_ = writeMessage(stdout, workerResponse{Status: "error",
				Message: fmt.Sprintf("unknown worker operation: %s", req.Operation)})
		}
	}
}
