# API reference

Complete reference for the `needle` package, the `needle/fetch` package and
the CLI. The [topic guides](index.md) cover concepts and examples; this page
documents every exported symbol.

Import paths:

```go
import (
    "github.com/FlameInTheDark/needle-go"       // package needle
    "github.com/FlameInTheDark/needle-go/fetch" // package fetch
)
```

- [package needle](#package-needle)
  - [Agent](#agent)
  - [Construction options](#construction-options)
  - [Completion, run and extraction options](#completion-run-and-extraction-options)
  - [Response types](#response-types)
  - [Tool declaration](#tool-declaration)
  - [Property builders](#property-builders)
  - [Property constraint options](#property-constraint-options)
  - [Extraction](#extraction)
  - [Argument decoding](#argument-decoding)
  - [Schema compilation](#schema-compilation)
  - [Errors and constants](#errors-and-constants)
- [package needle/fetch](#package-needlefetchnet)
- [cmd/needle](#cmd-needle)

## package needle

### Agent

```go
func New(opts ...Option) (*Agent, error)
```

Creates an agent bound to one toolset, one optional system turn and one
optional tuned weights archive. `New` only validates and normalizes its
inputs; the engine is located (or downloaded) lazily on first use. Errors from
`New`: malformed tool declarations, unserializable schemas, or an
unreadable/unknown `.cact` tag when `WithWeights` is used.

Agent methods:

| Method | Description |
| --- | --- |
| `Complete(ctx, text string, opts ...CompleteOption) (*Response, error)` | One conversation turn. Returns the engine's envelope; a pending call must be answered by feeding its result back with the next `Complete`. |
| `Run(ctx, query string, opts ...RunOption) (*Response, error)` | The full agentic loop: executes the declared handlers, feeds each round's results back, stops at `"respond"` or after max steps. The final response carries every executed result in `Results`. |
| `Reset() error` | Rewinds the conversation, keeps the tools loaded. |
| `Close() error` | Releases the session; kills the worker process for tuned agents. Safe to call repeatedly. Required for tuned agents. |
| `ExtractRaw(ctx, text string, schema any, opts ...ExtractOption) (map[string]any, error)` | One-shot extraction reusing the agent's weights/engine settings (fresh engine conversation). |
| `Tools() []string` | Declared tool names, in declaration order. |
| `Generation() int` | Engine generation in use (2 for the base model, or the tuned archive's). |
| `Tuned() bool` | Whether tuned weights are active. |
| `PrefixTokens() int` | Tool-prompt prefix token count of the last engine initialization. |

Behavioural notes:

- One agent per toolset; new tools means a new agent. An agent owns one
  conversation.
- The empty `FunctionCalls` list is the refusal contract for off-topic input —
  there is no free-text fallback.
- Calls are serialized; the engine's global state means at most one
  in-process session is bound per generation, and switching agents
  reconfigures the engine (rewinding the switched-from conversation).
- Once a tuned `.cact` is loaded into a process, the engine cannot unload it.
  The worker design isolates every tuned agent in its own child process, so
  this only surfaces if you load weights in-process yourself.

### Construction options

| Option | Effect |
| --- | --- |
| `WithTools(tools ...any)` | Declares the toolset: `*Tool` values, raw schema maps, a JSON string, or a slice of any of these, mixed. |
| `WithToolsJSON(toolsJSON string)` | Declares the toolset from the engine's native JSON array of schemas. |
| `WithSystem(system string)` | Attaches environment facts (`date:`, `locale:`, `device:`, ...). |
| `WithWeights(path string)` | Runs a tuned `.cact` in its own worker process; the archive tag selects the engine generation. |
| `WithToolIndexPath(path string)` | Persists tool-retrieval embeddings for catalogues larger than five tools. |
| `WithBufferSize(size int)` | Response buffer size in bytes (default 65536). |
| `WithEnginePath(path string)` | Pins the engine library, overriding env vars and the cache. |
| `WithLogger(logger *slog.Logger)` | Debug logging for engine lifecycle and downloads. |

### Completion, run and extraction options

| Option | Applies to | Effect |
| --- | --- | --- |
| `MaxTokens(n)` | `Complete` | Response token cap (default 256). |
| `MaxSteps(n)` | `Run` | Tool-call round cap (default 8). |
| `RunMaxTokens(n)` | `Run` | Per-round token cap (default 256). |
| `RunStrict(false)` | `Run` | Execute calls even when arguments are flagged ungrounded (default strict). |
| `ExtractSystem(s)` | `Extract*` | System facts for the one-shot agent. |
| `ExtractMaxTokens(n)` | `Extract*` | Token cap (default 256). |
| `ExtractStrict(false)` | `Extract*` | Return ungrounded values instead of raising (default strict). |
| `ExtractWeights(path)` | `Extract*` | Extract against a tuned `.cact`. |
| `ExtractEnginePath(path)` | `Extract*` | Pin the engine library. |

### Response types

```go
type Response struct {
    Type          string         // "call" | "respond" | engine error kinds
    Success       bool
    Error         *string
    ErrorCode     *string
    Reason        *string
    FunctionCalls []FunctionCall
    Reasoning     *string
    Confidence    *float64       // nil = uncalibrated
    PrefillTPS    *float64
    DecodeTPS     *float64
    PeakRAMMB     *float64
    Validation    *Validation
    Results       []any          // attached by Run
}

type FunctionCall struct {
    Name      string
    Arguments map[string]any
}

type Validation struct {
    Ungrounded []string // "tool.field" paths
    Negation   bool
}
```

Response helpers:

| Method | Description |
| --- | --- |
| `HasCalls() bool` | At least one requested call. |
| `FirstCall() *FunctionCall` | First call, or nil (the refusal case). |
| `Refused() bool` | No calls — off-topic, unsupported, ambiguous or negated input. |
| `UngroundedOf(tool string) []string` | Ungrounded argument paths reported for that tool's calls. |
| `String() string` | Compact JSON of the whole envelope. |

### Tool declaration

```go
type Handler func(ctx context.Context, arguments map[string]any) (any, error)
```

Handlers return the value fed back to the model and attached to `Results`. An
error maps to a `{"error": "<message>"}` result the model sees and can recover
from; panics are recovered and reported the same way. Values that fail to
marshal fall back to their `fmt` representation.

```go
func ToolFunc[A any](name, description string, fn func(ctx context.Context, args A) (any, error)) *Tool
```

Typed tool: `A`'s struct tags define the schema; arguments arrive decoded into
`A`. Nested structs, slices, embedded structs, pointers (optional fields) and
`time.Time` are all supported. If `A` is not a struct the handler receives an
error at invoke time.

```go
func NewTool(name, description string) *ToolBuilder
```

Dynamic builder:

| Method | Description |
| --- | --- |
| `Params(props map[string]Property)` | Parameters from named properties (required collected from `IsRequired`). |
| `RawParams(schema map[string]any)` | Parameters from a hand-written JSON-Schema object. |
| `Handler(fn Handler)` | Attaches a dynamic handler over the raw argument map. |
| `Build() *Tool` | Freezes the tool. |

`*Tool` methods:

| Method | Description |
| --- | --- |
| `Name() string` | Call name. |
| `Description() string` | Description. |
| `HasHandler() bool` | Whether `Run` can execute this tool. |
| `Schema() map[string]any` | The engine-facing `{"name","description","parameters"}` object. |
| `Invoke(ctx, arguments) (any, error)` | Executes the handler with panic recovery; handlerless tools return an "unknown tool" error. |

### Property builders

```go
type Property map[string]any
type PropOption func(Property)
```

| Constructor | JSON type |
| --- | --- |
| `Str(opts...)` | string |
| `Int(opts...)` | integer |
| `Num(opts...)` | number |
| `Bool(opts...)` | boolean |
| `Any(opts...)` | unconstrained |
| `Enum(values ...any)` | type inferred from the first value |
| `ArrayOf(items Property, opts...)` | array |
| `ObjectOf(props map[string]Property, opts...)` | nested object |

```go
func Params(props map[string]Property) map[string]any
```

Assembles the parameters object; properties marked `IsRequired` are collected
into `required` (deterministically sorted for builders — struct-tag schemas
keep declaration order).

### Property constraint options

`Desc`/`Description`, `IsRequired`, `Min`, `Max`, `GT`, `LT`, `Range`,
`MultipleOf`, `MinLen`, `MaxLen`, `Pattern`, `Format`, `MinItems`,
`MaxItems`, `Unique`, `Const`, `Default`, `EnumValues` — each maps onto the
JSON-Schema key of the same meaning; see the
[tag reference](tools.md#the-tag-reference) for the struct-tag spellings.

### Extraction

```go
func Extract[A any](ctx context.Context, text string, opts ...ExtractOption) (*A, error)
func ExtractAs[A any](ctx context.Context, text string, schema any, opts ...ExtractOption) (*A, error)
func ExtractRaw(ctx context.Context, text string, schema any, opts ...ExtractOption) (map[string]any, error)
```

`Extract` derives the schema from `A`'s tags. `ExtractAs`/`ExtractRaw` accept
a `*Tool`, a schema map (full tool schema or bare parameters object), or a
JSON string. All return `(nil, nil)` when nothing matched. Strict mode raises
`ExtractionValidationError` for temporal contradictions, engine-reported
ungrounded paths, and negated requests.

### Argument decoding

```go
func DecodeArguments[A any](arguments map[string]any) (A, error)
```

Decodes engine arguments into a typed struct using the same needle-tag →
field-name mapping as `ToolFunc`, recursing through nested structs, pointers
and slices. Use it when driving the loop by hand over `Complete`.

### Schema compilation

```go
func SchemaFromStruct[A any]() (map[string]any, error)
```

Compiles a struct type into a JSON-Schema parameters object without creating
a tool. Handy for inspecting what the engine will see, or for feeding schemas
into other systems.

### Errors and constants

| Symbol | Meaning |
| --- | --- |
| `ExtractionValidationError` | Sentinel wrapped by strict-mode extraction failures. |
| `ErrClosed` | Operation on a closed agent. |
| `Version` | The library version (`"0.2.0"`). |
| `EngineGeneration` | Default engine generation (2). |
| `IsWorker() bool` | Whether this process was started as a worker child; exported so applications can assert they never run in worker mode accidentally. |

## package needle/fetch

Engine discovery and downloads. All functions take a `context.Context` and
honor `HF_ENDPOINT`, `HF_HUB_OFFLINE` and the standard proxy variables.

| Symbol | Description |
| --- | --- |
| `LibraryPath(ctx, generation) (string, error)` | Full resolution order: env override → executable directory → cache → download. The entry point the agent uses. |
| `FetchLibrary(ctx, generation, version, tag, destDir) (string, error)` | Downloads the engine package for a platform tag (current machine when empty), extracts the shared library into destDir, returns its path. |
| `DownloadPlatform(ctx, generation, platform, outDir) ([]string, error)` | Copies a platform folder's standalone runner files (`linux-x86_64`, ..., `wasm`, `wasm-component`) into `outDir/<platform>/`, marking native runners executable. |
| `DownloadWeights(ctx, spec, outDir) (string, error)` | Pulls `<org>/<repo>/<file>.cact` (or the single archive of `<org>/<repo>`) into outDir. |
| `CacheDir(generation) (string, error)` | The per-generation cache directory. |
| `CacheDirFor(generation, version) (string, error)` | CacheDir with an explicit engine version. |
| `PlatformTag() (string, error)` | Platform tag for the current machine. |
| `PlatformTagFor(goos, goarch) (string, error)` | Platform tag for an explicit pair. |
| `LibName() string` | `libneedle.so` / `.dll` / `.dylib` for the current GOOS. |
| `LibNameForTag(tag string) string` | Library file name inside a package built for the given platform tag. |
| `EngineRepo(generation)` / `EngineVersion(generation)` | Hub repository and build version per generation. |
| `EngineRepos` / `EngineVersions` | The underlying maps. |
| `Platforms` | The list of standalone-runner platform folders. |
| `Endpoint() string` | Active Hub endpoint (honors `HF_ENDPOINT`). |
| `Offline() bool` | Whether `HF_HUB_OFFLINE` is set. |
| `ProgressHook ProgressFunc` | Optional `func(label string, total, done int64)` progress callback, invoked during downloads. |
| `UserAgent` | The HTTP user agent (override before first use if you like). |

## cmd/needle

```
needle fetch    [--generation 2|3] [--out DIR] [--platform-tag TAG]
needle download <platform | org/repo[/file].cact> [--out DIR] [--generation N]
needle run      [--tools FILE] [--system TEXT] [--weights FILE] [--max N] [--json] [--timeout D] [prompt]
needle version
```

Positional arguments may appear before or after flags. `run` answers one
prompt through the library itself, so it exercises the same cache and download
path your application will. See the [CLI reference](cli.md) for the full flag
documentation.
