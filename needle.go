// Package needle is a pure-Go client for the Needle 2 on-device model: a
// 45M-parameter tool-calling engine that ships as a single ~14MB native
// library and runs a full session in about 28MB of RAM - pure Go toolchain,
// no cgo, one dependency (purego) for dlopen/LoadLibrary.
//
// Text goes in, a grammar-constrained JSON tool call comes out:
//
//	agent, err := needle.New(needle.WithTools(sendEmail))
//	resp, err := agent.Run(ctx, "email finance@example.com about expenses")
//	fmt.Println(resp.Results)
//
// The native engine is fetched once from the Hugging Face Hub on first
// use and cached under ~/.cache/needle-go. Inference never touches the
// network.
package needle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/FlameInTheDark/needle-go/fetch"
	"github.com/FlameInTheDark/needle-go/internal/engine"
)

// Agent binds one toolset, one optional system turn and one optional
// tuned weights archive to a native engine session. An agent owns a
// conversation: repeated Complete calls continue it, Reset rewinds it
// while keeping the tools loaded, and Close releases the session. One
// agent per toolset; to change tools, build a new agent.
type Agent struct {
	mu            sync.Mutex
	system        string
	toolsJSON     string
	toolSchemas   []map[string]any
	handlers      map[string]Handler
	nTools        int
	weightsPath   string
	generation    int
	tuned         bool
	toolIndexPath string
	bufSize       int
	enginePath    string
	logger        *slog.Logger

	backend   engine.Backend
	prefix    int
	seenYears map[int]bool
	closed    bool
}

// New creates an agent. The engine is located (or downloaded) lazily on
// first use, so New itself only validates and normalizes its inputs.
func New(opts ...Option) (*Agent, error) {
	cfg := newConfig(opts)
	a := &Agent{
		system:        cfg.system,
		weightsPath:   cfg.weightsPath,
		generation:    EngineGeneration,
		toolIndexPath: cfg.toolIndexPath,
		bufSize:       cfg.bufSize,
		enginePath:    cfg.enginePath,
		logger:        cfg.logger,
		seenYears:     map[int]bool{},
	}

	schemas, handlers, err := normalizeTools(cfg.toolsSource)
	if err != nil {
		return nil, err
	}
	a.toolSchemas = schemas
	a.handlers = handlers
	a.nTools = len(schemas)
	toolsJSON, err := json.Marshal(schemas)
	if err != nil {
		return nil, fmt.Errorf("needle: cannot serialize tool schemas: %w", err)
	}
	a.toolsJSON = string(toolsJSON)

	if cfg.weightsPath != "" {
		gen, err := engine.GenerationOfCactFile(cfg.weightsPath)
		if err != nil {
			return nil, err
		}
		a.generation = gen
		a.tuned = true
	}
	return a, nil
}

// Tools returns the declared tool names, in declaration order.
func (a *Agent) Tools() []string {
	names := make([]string, 0, len(a.toolSchemas))
	for _, schema := range a.toolSchemas {
		if name, ok := schema["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// Generation reports the engine generation the agent runs on: 2 for the
// base model, or the generation of its tuned .cact archive.
func (a *Agent) Generation() int { return a.generation }

// Tuned reports whether the agent runs tuned weights.
func (a *Agent) Tuned() bool { return a.tuned }

// PrefixTokens reports the tool-prompt prefix token count observed when
// the engine session was last initialized.
func (a *Agent) PrefixTokens() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prefix
}

// bind lazily locates the engine and starts the backend.
func (a *Agent) bind(ctx context.Context) error {
	if a.closed {
		return ErrClosed
	}
	if a.backend != nil {
		return nil
	}
	libPath := a.enginePath
	if libPath == "" {
		var err error
		libPath, err = fetch.LibraryPath(ctx, a.generation)
		if err != nil {
			return err
		}
	}
	if a.logger != nil {
		a.logger.Debug("needle: engine library", "path", libPath, "generation", a.generation)
	}
	if a.tuned {
		worker, err := engine.StartWorker(ctx, libPath, a.weightsPath,
			a.system, a.toolsJSON, a.toolIndexPath, a.bufSize, a.generation)
		if err != nil {
			return err
		}
		a.backend = worker
		a.prefix = worker.PrefixTokens()
		return nil
	}
	session, err := engine.NewInproc(libPath, a.generation, a.system,
		a.toolsJSON, a.toolIndexPath, a.bufSize)
	if err != nil {
		return err
	}
	a.backend = session
	return nil
}

// completeLocked performs one turn; a.mu must be held.
func (a *Agent) completeLocked(ctx context.Context, text string, maxNewTokens int, ground bool) (*Response, error) {
	if err := a.bind(ctx); err != nil {
		return nil, err
	}
	for y := range sourceYears(text) {
		a.seenYears[y] = true
	}
	raw, err := a.backend.Complete(ctx, text, maxNewTokens)
	if err != nil {
		return nil, err
	}
	var response Response
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, fmt.Errorf("needle: engine returned an unparseable envelope (%v); "+
			"this is an engine bug - please report it with the prompt and schema", err)
	}
	if a.tuned {
		// Fine-tuning does not update the confidence head; scores would
		// be uncalibrated, so they are reported as absent.
		response.Confidence = nil
	}
	if ground {
		annotateUngrounded(&response, a.toolSchemas, a.seenYears, a.system)
	}
	return &response, nil
}

// Complete performs one conversation turn and returns the engine's
// response envelope. When the model wants tool calls the response carries
// them (execute and feed results back with the next Complete); when it
// does not, an empty FunctionCalls list is the refusal contract - there
// is no free-text fallback.
func (a *Agent) Complete(ctx context.Context, text string, opts ...CompleteOption) (*Response, error) {
	cfg := completeConfig{maxNewTokens: 256}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.completeLocked(ctx, text, cfg.maxNewTokens, true)
}

// Run drives the full agentic loop: the model picks calls, Run executes
// the declared handlers, feeds each round's results back as the next
// turn, and stops when the model responds without calls or when maxSteps
// rounds have elapsed. The final response carries every executed result,
// flattened, in Results. Strict mode (the default) refuses to execute
// calls whose arguments were flagged ungrounded, feeding an error result
// back instead.
func (a *Agent) Run(ctx context.Context, query string, opts ...RunOption) (*Response, error) {
	cfg := runConfig{maxSteps: 8, maxNewTokens: 256}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	strict := true
	if cfg.strict != nil {
		strict = *cfg.strict
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	response, err := a.completeLocked(ctx, query, cfg.maxNewTokens, true)
	if err != nil {
		return nil, err
	}
	var executed []any
	for step := 0; step < cfg.maxSteps; step++ {
		if response.Type != "call" || len(response.FunctionCalls) == 0 {
			break
		}
		ungrounded := ungroundedPaths(response)
		results := make([]any, 0, len(response.FunctionCalls))
		for _, call := range response.FunctionCalls {
			var fabricated []string
			if paths := ungrounded[call.Name]; paths != nil {
				for p := range paths {
					fabricated = append(fabricated, p)
				}
			}
			sortStrings(fabricated)
			if strict && len(fabricated) > 0 {
				results = append(results, map[string]any{
					"error": "ungrounded " + joinStrings(fabricated, ", "),
				})
				continue
			}
			handler := a.handlers[call.Name]
			if handler == nil {
				results = append(results, map[string]any{
					"error": "unknown tool: " + call.Name,
				})
				continue
			}
			value, err := safeInvoke(ctx, handler, call.Name, call.Arguments)
			if err != nil {
				results = append(results, map[string]any{"error": err.Error()})
				continue
			}
			results = append(results, jsonable(value))
		}
		executed = append(executed, results...)
		feedback, err := json.Marshal(results)
		if err != nil {
			return nil, fmt.Errorf("needle: cannot serialize tool results: %w", err)
		}
		response, err = a.completeLocked(ctx, string(feedback), cfg.maxNewTokens, false)
		if err != nil {
			return nil, err
		}
	}
	response.Results = executed
	return response, nil
}

// safeInvoke runs a handler with panic recovery.
func safeInvoke(ctx context.Context, handler Handler, name string, arguments map[string]any) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("tool %s panicked: %v", name, r)
		}
	}()
	return handler(ctx, arguments)
}

// jsonable makes a handler result feedable: values that do not marshal
// fall back to their fmt representation.
func jsonable(value any) any {
	if value == nil {
		return nil
	}
	if _, err := json.Marshal(value); err != nil {
		return fmt.Sprintf("%v", value)
	}
	return value
}

func joinStrings(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// Reset rewinds the conversation, keeping the tools loaded.
func (a *Agent) Reset() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrClosed
	}
	if err := a.bind(context.Background()); err != nil {
		return err
	}
	if err := a.backend.Reset(); err != nil {
		return err
	}
	a.seenYears = map[int]bool{}
	return nil
}

// Close releases the agent's engine session. It is safe to call more than
// once and required for tuned agents (their worker process is killed).
func (a *Agent) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	if a.backend != nil {
		return a.backend.Close()
	}
	return nil
}
