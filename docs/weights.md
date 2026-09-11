# Tuned weights

The native engine is weights-agnostic: the base model ships baked into the
engine binary, and a **tuned archive** (a `.cact` file) can replace it at load
time without recompiling anything. This page covers running tuned archives and
the process architecture that makes them safe.

- [Running a tuned archive](#running-a-tuned-archive)
- [Generation tags](#generation-tags)
- [The worker process](#the-worker-process)
- [Confidence and tuned weights](#confidence-and-tuned-weights)
- [Lifecycle](#lifecycle)
- [Getting archives](#getting-archives)

## Running a tuned archive

One option:

```go
agent, err := needle.New(
    needle.WithWeights("my_needle.cact"),
    needle.WithTools(tools),
)
```

Everything else works identically — `Run`, `Complete`, extraction, retrieval,
`Reset` — with the tuned weights driving generation. The
[weights example](../examples/weights/main.go) is a complete program.

Multiple tuned agents with *different* archives coexist in one process
easily, because each one runs in its own worker process (below). A base-model
agent and a tuned agent also coexist.

## Generation tags

Every `.cact` archive carries a format tag in its header; the library reads it
and dispatches to the matching engine generation automatically:

| Tag | Generation |
| --- | --- |
| `0x05E12A83` | 2 — the 45M-parameter tool-calling model this package targets |
| `0x05E12A84` | 3 — reserved for the next generation |

A generation-3 archive therefore never routes through a generation-2 engine —
and vice versa. A mismatched or unreadable tag is reported by `New` (or
`ExtractWeights`) as an error naming the path.

`needle.Agent.Generation()` reports the generation actually in use, and
`Tuned()` reports whether tuned weights are active.

## The worker process

The native engine keeps global state and **cannot unload weights** once
loaded. Loading a tuned archive in your own process would therefore pin those
weights forever. The library solves this with process isolation:

1. `WithWeights` starts a **worker process** — a re-exec of *your own binary*.
2. The child is started with a private `--needle-worker <nonce>` argument plus
   a matching `NEEDLE_GO_WORKER` environment value.
3. A package-level `init()` in needle-go detects the handshake and takes the
   child over **before `main()` runs**, so applications never need to
   cooperate — importing the library is enough.
4. The child loads the engine, injects the archive with `needle_load`, owns an
   independent engine, KV cache and conversation, and serves requests over
   stdin/stdout with a length-prefixed JSON protocol.

```
your process                         worker child (same binary)
    │ complete(query)                    │
    ├───────── JSON request ────────────▶│ engine + tuned weights
    │◀──────── JSON response ────────────┤ (own KV cache, own conversation)
```

Consequences worth knowing:

- **True isolation.** Each tuned agent has its own engine state; no turn-taking
  constraints between tuned agents (contrast
  [Conversations: concurrency](conversations.md#concurrency-and-multiple-agents)).
- **The nonce makes accidental activation impossible.** A stray
  `NEEDLE_GO_WORKER` in the environment never hijacks a process: the argv
  value must match too.
- **Cancellation kills the child.** A cancelled `Run`/`Complete` propagates by
  terminating the worker — no zombie generation.
- **Re-exec means zero deployment friction.** No extra binary to ship; the
  worker is the application binary itself, launched with different arguments.
- `needle.IsWorker()` reports whether the current process was started as a
  worker child — exported for applications that want to assert they never run
  in worker mode accidentally.

## Confidence and tuned weights

Tuned agents report `Confidence` as **nil** on every response. The calibration
head is part of the base model's training; fine-tuning does not update it, so
scores would be meaningless. Gate tuned-agent calls on domain validation
instead (handler-side checks, schema bounds — both still fully enforced by the
grammar).

## Lifecycle

- `Close()` is **required** for tuned agents — it shuts the worker process
  down. Base-model agents tolerate a missing `Close` (the engine unloads with
  the process); tuned agents leak a child process without it. `defer agent.Close()`
  covers every case and is safe to call twice.
- The worker inherits stdout/stderr hygiene: the library redirects the child's
  file descriptor 1 so stray native prints land on stderr instead of
  corrupting the protocol stream.

## Getting archives

Tuned archives come from training runs on your own data. The CLI can also pull
published archives from any Hugging Face repository:

```sh
needle download <org>/<repo>/<file>.cact --out .
needle download <org>/<repo>            # when the repo holds exactly one archive
```

The base model's own archive (`needle2.cact`) is a useful smoke test for the
worker machinery — it exercises the same load path with the same weights the
engine bakes in:

```sh
needle download Cactus-Compute/needle2/needle2.cact --out .
needle run --weights needle2.cact --tools tools.json --prompt "..."
```

## Where to go next

- The tuning-free path: [Getting started](getting-started.md)
- Process and concurrency model: [Conversations](conversations.md#concurrency-and-multiple-agents)
- Download mechanics: [CLI reference](cli.md#download)
