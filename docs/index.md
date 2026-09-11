# needle-go documentation

This is the complete documentation for
[`github.com/FlameInTheDark/needle-go`](https://github.com/FlameInTheDark/needle-go),
organized so you can jump straight to the page that answers your question.

## Start here

| Page | What it covers |
| --- | --- |
| [Getting started](getting-started.md) | Requirements, installation, the first agent line by line, what happens on the first run, where the engine lives, verifying the setup. |

## By topic

### Tools

| Page | What it covers |
| --- | --- |
| [Building tools](tools.md) | The three declaration styles (typed struct tags, dynamic builders, raw JSON schemas), the complete tag reference, Go type mapping, nested objects and arrays, handler contract, what the grammar enforces, writing descriptions that work. |
| [Tool calling](tool-calling.md) | `Run` vs `Complete`, the agentic loop, feeding results back, typed argument decoding, declaring and dispatching multiple tools, parallel calls, refusals, confidence gating. |
| [Tool indexes](tool-indexes.md) | Catalogues larger than five tools: how retrieval works, the top-five window, persistent indexes with `WithToolIndexPath`, fingerprinting, index file management, catalogue design guidance. |

### Agents and conversation

| Page | What it covers |
| --- | --- |
| [Responses](responses.md) | Every field of the response envelope, the refusal contract, confidence semantics and gating, validation (ungrounded arguments, negation, temporal grounding), performance metrics. |
| [Conversations](conversations.md) | Multi-turn conversations, system facts, `Reset`, multiple agents and concurrency rules, context cancellation. |
| [Extraction](extraction.md) | Turning unstructured text into typed structs: `Extract`, `ExtractAs`, `ExtractRaw`, strict mode and its error contract. |
| [Tuned weights](weights.md) | Running `.cact` archives, the worker process architecture, generation tags, downloading published archives. |

### Runtime and operations

| Page | What it covers |
| --- | --- |
| [Engine management](engine.md) | Where the engine lives and how it is found, the cache layout, pre-seeding offline devices, musl/Alpine, platform support matrix, every environment variable. |
| [CLI reference](cli.md) | The `needle` command-line tool: installing a prebuilt binary or building it, `fetch`, `download`, `run`, `version`, all flags with examples. |
| [API reference](api.md) | Every exported symbol of the `needle` and `needle/fetch` packages. |
| [Development & releases](development.md) | Building from source, the three test layers, the CI workflows, conventional commits, how releases and CLI binaries are produced. |

## Common questions

**Where do I start?** [Getting started](getting-started.md), then copy the
quickstart agent from the [README](../README.md#quickstart).

**The model refuses everything.** Description quality decides most of this —
see [Writing descriptions that work](tools.md#writing-descriptions-that-work).

**I have more than five tools.** Read [Tool indexes](tool-indexes.md); the
engine switches to retrieval automatically.

**I need typed structs out of raw text, not tool calls.** That is
[Extraction](extraction.md) — it is tool calling with exactly one schema.

**I want to ship an offline device.** [Engine management](engine.md#air-gapped--offline-devices)
covers pre-seeding the cache and `HF_HUB_OFFLINE`.

**How do I know a call is safe to execute?** Confidence gating and validation:
[Responses](responses.md#confidence) and
[Responses](responses.md#validation-grounding).

**A symbol name escapes me.** The [API reference](api.md) lists them all.

**How do releases and the prebuilt CLI binaries get made?** Pushes to `main`
run the tests and then semantic-release; every release is followed by
platform binaries — [Development & releases](development.md).
