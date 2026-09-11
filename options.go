package needle

import (
	"log/slog"
)

// Option configures an Agent.
type Option func(*agentConfig)

type agentConfig struct {
	toolsSource   any
	system        string
	weightsPath   string
	toolIndexPath string
	bufSize       int
	enginePath    string
	logger        *slog.Logger
}

func newConfig(opts []Option) *agentConfig {
	cfg := &agentConfig{bufSize: 65536}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg
}

// WithTools declares the agent's toolset. Accepted forms:
//
//   - values built by ToolFunc or NewTool(...).Build() (*Tool)
//   - raw JSON-schema maps ({"name": ..., "description": ...,
//     "parameters": {...}}), or bare parameter objects
//   - a JSON string holding an array of schemas (exactly what the engine
//     consumes)
//   - any slice mixing the above
func WithTools(tools ...any) Option {
	return func(c *agentConfig) { c.toolsSource = tools }
}

// WithToolsJSON declares the toolset from a JSON string: an array of
// {"name", "description", "parameters"} schema objects.
func WithToolsJSON(toolsJSON string) Option {
	return func(c *agentConfig) { c.toolsSource = toolsJSON }
}

// WithSystem attaches environment facts to every conversation turn. Facts,
// never instructions: recognized keys are date, locale, device, battery,
// network, location, user and assistant. Relative language ("tomorrow at
// 7") resolves only when a date: fact licenses it.
func WithSystem(system string) Option {
	return func(c *agentConfig) { c.system = system }
}

// WithWeights runs the agent on a tuned .cact archive instead of the
// baked base model. The archive's format tag selects a compatible engine
// generation automatically. Each tuned agent runs in its own worker
// process, so several tuned agents with different weights can coexist,
// and confidence is reported as nil because fine-tuning does not update
// the calibration head.
func WithWeights(path string) Option {
	return func(c *agentConfig) { c.weightsPath = path }
}

// WithToolIndexPath persists the tool-retrieval embeddings on disk. With
// more than five declared tools the engine embeds each schema once,
// renders only the top five per turn, and constrains the grammar to that
// subset; the index makes restarts instant by fingerprinting the schemas.
func WithToolIndexPath(path string) Option {
	return func(c *agentConfig) { c.toolIndexPath = path }
}

// WithBufferSize sizes the response buffer, in bytes (default 65536). A
// response larger than the buffer fails with the engine's truncation
// error.
func WithBufferSize(size int) Option {
	return func(c *agentConfig) { c.bufSize = size }
}

// WithEnginePath pins the native engine library to an explicit path,
// overriding the environment variables and the cache lookup. Use it to
// ship an engine next to your binary.
func WithEnginePath(path string) Option {
	return func(c *agentConfig) { c.enginePath = path }
}

// WithLogger installs a logger for download progress and engine lifecycle
// events. The agent is silent by default.
func WithLogger(logger *slog.Logger) Option {
	return func(c *agentConfig) { c.logger = logger }
}

// CompleteOption tunes one completion call.
type CompleteOption func(*completeConfig)

type completeConfig struct {
	maxNewTokens int
}

// MaxTokens caps the response length in tokens (default 256).
func MaxTokens(n int) CompleteOption {
	return func(c *completeConfig) { c.maxNewTokens = n }
}

// RunOption tunes one agentic loop.
type RunOption func(*runConfig)

type runConfig struct {
	maxSteps     int
	maxNewTokens int
	strict       *bool
}

// MaxSteps caps the number of tool-call rounds in Agent.Run (default 8).
func MaxSteps(n int) RunOption {
	return func(c *runConfig) { c.maxSteps = n }
}

// RunMaxTokens caps each round's response length in tokens (default 256).
func RunMaxTokens(n int) RunOption {
	return func(c *runConfig) { c.maxNewTokens = n }
}

// RunStrict toggles strict grounding inside Agent.Run. Strict (the
// default) refuses to execute a call whose arguments the engine flagged
// ungrounded, feeding an error back to the model instead; loose executes
// regardless.
func RunStrict(strict bool) RunOption {
	return func(c *runConfig) { c.strict = &strict }
}

// ExtractOption tunes one extraction.
type ExtractOption func(*extractConfig)

type extractConfig struct {
	system       string
	maxNewTokens int
	strict       *bool
	weightsPath  string
	enginePath   string
	logger       *slog.Logger
}

// ExtractSystem attaches system facts to a one-shot extraction.
func ExtractSystem(system string) ExtractOption {
	return func(c *extractConfig) { c.system = system }
}

// ExtractMaxTokens caps the response length in tokens (default 256).
func ExtractMaxTokens(n int) ExtractOption {
	return func(c *extractConfig) { c.maxNewTokens = n }
}

// ExtractStrict toggles strict extraction (default true): temporal values
// that contradict a literal year in the input, engine-reported fabricated
// values, and negated requests raise ExtractionValidationError instead of
// being returned silently.
func ExtractStrict(strict bool) ExtractOption {
	return func(c *extractConfig) { c.strict = &strict }
}

// ExtractWeights extracts against a tuned .cact archive.
func ExtractWeights(path string) ExtractOption {
	return func(c *extractConfig) { c.weightsPath = path }
}

// ExtractEnginePath pins the engine library for a one-shot extraction.
func ExtractEnginePath(path string) ExtractOption {
	return func(c *extractConfig) { c.enginePath = path }
}
