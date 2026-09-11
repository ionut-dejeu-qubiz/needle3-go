# Getting started

This page takes you from an empty project to a running agent, and explains what
happens on the way — especially the first-run engine download, which is the one
piece of magic worth understanding early.

## Requirements

- **Go 1.25 or newer** (`go version` to check).
- **A network connection the first time an agent runs** — the ~14 MB native
  engine is downloaded once from the Hugging Face Hub and cached. Offline
  machines can be pre-seeded instead (see
  [Engine management](engine.md#air-gapped--offline-devices)).
- **A supported machine**: Windows (amd64, arm64), Linux (amd64, arm64 — glibc
  or musl), or macOS (amd64, arm64). Other platforms can run with an engine
  provided manually.

There is no cgo, no interpreter and no scripting runtime anywhere in the
dependency chain: the engine shared library is loaded at runtime with
`dlopen`/`LoadLibrary` via [purego](https://github.com/ebitengine/purego), so
`CGO_ENABLED=0` builds work and cross-compiling from any host is a plain
`GOOS=windows go build`.

## Installation

From your module root:

```sh
go get github.com/FlameInTheDark/needle-go
```

The package name is `needle` (the import path ends in `needle-go`, the package
clause is `package needle`), so code reads:

```go
import "github.com/FlameInTheDark/needle-go"

agent, err := needle.New(needle.WithTools(getWeather))
```

## Your first agent

Here is the complete program from the README, annotated:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/FlameInTheDark/needle-go"
)

// 1. The argument struct: its tags define the JSON schema the engine's
//    decode grammar is compiled from.
type GetWeatherArgs struct {
    City string `needle:"city" desc:"city name" required:"true"`
}

func main() {
    // 2. The tool: a name, a description the model reads, and a typed
    //    handler that receives the arguments already decoded.
    getWeather := needle.ToolFunc("get_weather",
        "Get the current weather for a city.",
        func(ctx context.Context, args GetWeatherArgs) (any, error) {
            // A real program calls a weather API here.
            return map[string]any{"city": args.City, "temp_c": 27, "sky": "clear"}, nil
        })

    // 3. The agent binds one toolset (and optionally a system turn and
    //    tuned weights). New only validates; the engine is resolved lazily.
    agent, err := needle.New(needle.WithTools(getWeather))
    if err != nil {
        log.Fatal(err)
    }
    defer agent.Close()

    // 4. Run drives the whole loop: the model emits a call, the handler
    //    executes, the result is fed back, and the final response carries
    //    everything in Results.
    resp, err := agent.Run(context.Background(), "what's it like in Lagos right now?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Results)
    // [{city:Lagos temp_c:27 sky:clear}]
}
```

Running it:

```sh
go run .
```

The model emits a grammar-constrained call such as
`get_weather {"city": "Lagos"}`, your handler runs, and the result flows back
into the conversation. Because decoding is constrained by the schema, the call
is guaranteed well-formed — `city` is always present, and no invented keys can
appear.

A prompt that does not match any declared tool ("write me a poem") produces an
empty call list instead of free text — that is the refusal contract, covered in
[Responses](responses.md#the-refusal-contract).

## What happens on the first run

The first `Run`/`Complete` call resolves the native engine in this order:

1. `NEEDLE2_LIB_PATH` (or `NEEDLE_LIB_PATH`) if set;
2. a `libneedle.so`/`.dll` placed next to your executable;
3. the cache at `~/.cache/needle-go/v2/<engine-version>/`;
4. a download of the engine package for your platform from the Hugging Face
   Hub — from which only the shared library is extracted into the cache.

After step 4 succeeds once, every later run resolves at step 3 and never
touches the network. The download respects `HF_ENDPOINT` and the standard
proxy variables, and you can watch it happen with the CLI:

```sh
go run ./cmd/needle fetch        # explicit fetch, prints progress and the path
```

Full details, including the exact cache layout and offline pre-seeding:
[Engine management](engine.md).

## Verifying the setup

Build and run the CLI that ships with the repository:

```sh
go run ./cmd/needle version
# needle-go 0.2.0 (engine 2.0.4)

go run ./cmd/needle run --prompt "what's it like in Lagos right now?"
```

`run` without `--tools` refuses every prompt (nothing is declared), so to see a
real call, point it at a tools file:

```sh
go run ./cmd/needle run --tools mytools.json --prompt "what's it like in Lagos right now?"
```

Or run an example end to end:

```sh
go run ./examples/weather        # the quickstart agent, for real
```

## Where to go next

- Declare real tools: [Building tools](tools.md)
- Understand the calling loop: [Tool calling](tool-calling.md)
- More than five tools: [Tool indexes](tool-indexes.md)
- Text → structs: [Extraction](extraction.md)
- Every field of the response: [Responses](responses.md)
