# needle-go

**A pure-Go library for the Needle 2 on-device model — tool calling, structured
extraction and conversational agents in about 28 MB of RAM.**

Needle 2 is an open 45M-parameter model for tool calling, device use and structured
extraction. The whole model is a single ~14 MB native engine: text goes in, a
grammar-constrained JSON tool call comes back.

This library is a rewrite of the [original Python needle
library](https://github.com/cactus-compute/needle) in Go.

```go
agent, err := needle.New(needle.WithTools(getWeather))
resp, err  := agent.Run(ctx, "what's it like in Lagos right now?")
fmt.Println(resp.Results)
// [{city:Lagos temp_c:27 sky:clear}]
```

- **Self-contained** — the native engine is downloaded once from the Hugging Face
  Hub into `~/.cache/needle-go`; inference never touches the network afterwards.
- **Simple contract** — tool calls come back as structured data. A byte-level
  grammar compiled from your schemas constrains every token, so the call is
  always well-formed.
- **Confidence-gated** — every response carries a calibrated confidence score;
  set a threshold, act above it, escalate below it.
- **Tool retrieval** — declare a large catalogue and a built-in retrieval head
  renders only the top five tools per turn.
- **Bounded memory** — a 256-token sliding window with tools pinned as KV sinks
  keeps a session near 28 MB no matter how long the conversation runs.
- **Pure Go toolchain** — `go build` is all you need. `CGO_ENABLED=0` everywhere,
  cross-compilation from any machine to Windows and Linux (and macOS).

Runs natively on **Windows** (amd64, arm64) and **Linux** (amd64, arm64 — glibc or
musl), plus macOS. No interpreter, no scripting runtime: the engine shared
library is loaded with `dlopen`/`LoadLibrary` through
[purego](https://github.com/ebitengine/purego), the only third-party dependency.

## Installation

```sh
go get github.com/FlameInTheDark/needle-go
```

```go
import "github.com/FlameInTheDark/needle-go"
```

The package name is `needle`, so the calls read `needle.New`, `needle.WithTools`,
`needle.Extract`, and so on.

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/FlameInTheDark/needle-go"
)

type GetWeatherArgs struct {
    City string `needle:"city" desc:"city name" required:"true"`
}

func main() {
    getWeather := needle.ToolFunc("get_weather",
        "Get the current weather for a city.",
        func(ctx context.Context, args GetWeatherArgs) (any, error) {
            return map[string]any{"city": args.City, "temp_c": 27, "sky": "clear"}, nil
        })

    agent, err := needle.New(needle.WithTools(getWeather))
    if err != nil {
        log.Fatal(err)
    }
    defer agent.Close()

    resp, err := agent.Run(context.Background(), "what's it like in Lagos right now?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Results)
}
```

The first `Run` (or `Complete`) call locates the engine, downloading the ~14 MB
build from the Hugging Face Hub if this is the machine's first use. Every later
call is fully local. See [Getting started](docs/getting-started.md) for the
line-by-line walkthrough.

## Documentation

The full documentation lives in [`docs/`](docs/) and is organized by task — open
the page that matches what you are trying to do:

| I want to… | Read |
| --- | --- |
| Install and run my first agent, understand the first-run engine download | [Getting started](docs/getting-started.md) |
| Declare tools — struct tags, builders, raw JSON schemas, the full tag reference | [Building tools](docs/tools.md) |
| Execute tool calls — `Run` vs `Complete`, feeding results back, multiple tools | [Tool calling](docs/tool-calling.md) |
| Use a large catalogue — retrieval, the top-five window, persistent indexes | [Tool indexes](docs/tool-indexes.md) |
| Read everything a response carries — calls, confidence, validation, metrics | [Responses](docs/responses.md) |
| Hold multi-turn conversations, supply system facts, manage several agents | [Conversations](docs/conversations.md) |
| Turn unstructured text into typed structs | [Extraction](docs/extraction.md) |
| Run tuned weights (`.cact` archives) | [Tuned weights](docs/weights.md) |
| Manage the engine — cache layout, offline devices, musl, environment variables | [Engine management](docs/engine.md) |
| Use the `needle` command-line tool | [CLI reference](docs/cli.md) |
| Look up an exact signature | [API reference](docs/api.md) |
| Build, test and release the library itself — CI, conventional commits, release binaries | [Development & releases](docs/development.md) |

## Examples

Runnable programs for every feature live in [`examples/`](examples/):

| Example | Shows |
| --- | --- |
| `weather` | the smallest complete agent |
| `conversation` | manual loop with `Complete`, follow-ups, confidence gating, reset |
| `extraction` | typed, raw and strict extraction |
| `catalog` | more than five tools, retrieval, persistent tool index |
| `smart_home` | a complete smart-home environment with a 32-case acceptance suite |
| `weights` | an agent running a tuned `.cact` archive |

```sh
go run ./examples/weather
```

## Project layout

```
needle.go         Agent: New, Complete, Run, Reset, Close
options.go        With* / *Tokens / *Strict option functions
response.go       Response, FunctionCall, Validation
tool.go           Tool, ToolFunc, NewTool builder, Handler
schema.go         struct-tag → JSON-Schema compiler
property.go       Property builders and constraint options
rekey.go          engine-argument → Go-struct decoding
grounding.go      temporal grounding + strict validation
extract.go        Extract / ExtractAs / ExtractRaw
worker.go         worker-mode process hook (init)
fetch/            engine discovery, download + extraction, platform/weights downloads
internal/engine/  native bindings (purego/LoadLibrary), in-process backend,
                  worker protocol and child loop
cmd/needle/       the CLI (urfave/cli v3)
examples/         weather, extraction, conversation, catalog, smart_home, weights
docs/             the documentation set
```

## Testing

```sh
go test ./...              # unit tests always; engine tests skip when no engine
NEEDLE_TEST_WEIGHTS=/path/to/needle2.cact go test -race ./...   # also run the worker tests
go run ./examples/smart_home   # 32-case acceptance suite against the real engine
```

CI runs this on every push to `main` on Linux and Windows, and every release
is cut from the commit history automatically — see
[Development & releases](docs/development.md).

## License

Apache License 2.0 — see [LICENSE](LICENSE).
