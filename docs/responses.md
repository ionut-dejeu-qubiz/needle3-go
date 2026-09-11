# Responses

Every turn — `Complete` or each round of `Run` — returns one `*needle.Response`.
This page documents every field, every helper, and the two judgment surfaces
you will use in production: confidence and validation.

- [The envelope](#the-envelope)
- [Field by field](#field-by-field)
- [Helpers](#helpers)
- [The refusal contract](#the-refusal-contract)
- [Confidence](#confidence)
- [Validation (grounding)](#validation-grounding)
- [Metrics](#metrics)

## The envelope

```json
{
  "type": "call",
  "success": true,
  "error": null,
  "error_code": null,
  "reason": null,
  "function_calls": [
    { "name": "set_lights", "arguments": { "room": "living room", "on": true, "brightness": 30 } }
  ],
  "reasoning": "'living room' -> room; 'dim' -> on true, brightness 30",
  "confidence": 0.94,
  "prefill_tps": 4300.0,
  "decode_tps": 850.0,
  "peak_ram_mb": 28.5,
  "validation": { "ungrounded": [], "negation": false },
  "results": [ ... ]
}
```

## Field by field

| Field | Meaning |
| --- | --- |
| `Type` | `"call"` while the model wants tool calls; `"respond"` when the loop is finished — the answer is the tool results, no free text is generated. Engine error paths may produce `"error"`, `"refuse"` or `"text"` envelopes. |
| `Success` | Whether the engine completed the turn. |
| `Error`, `ErrorCode` | Engine-level error message/code, or nil. |
| `Reason` | Short engine-level refusal reason, or nil. |
| `FunctionCalls` | The calls the model wants executed this turn. Grammar-constrained decoding guarantees the arguments match the declared schema. |
| `Reasoning` | The model's short unconstrained derivation of each argument from its source span — read it to understand *why* an argument got its value. |
| `Confidence` | Calibrated score in `[0,1]`, or nil when uncalibrated (tuned weights). |
| `PrefillTPS`, `DecodeTPS`, `PeakRAMMB` | Throughput and footprint metrics for the turn. |
| `Validation` | Grounding report: ungrounded argument paths and negation flag. |
| `Results` | Attached by `Run` only: the flattened list of values your handlers returned across the whole loop. The engine never populates it. |

### `FunctionCall`

```go
type FunctionCall struct {
    Name      string
    Arguments map[string]any
}
```

`Arguments` contains **only values evidenced by the input**. Optional fields
with no evidence are omitted, not guessed — reads of missing keys return zero
values, so treat presence as meaningful:

```go
call := resp.FirstCall()
if room, ok := call.Arguments["room"].(string); ok {
    // the input named a room
}
```

## Helpers

| Method | Description |
| --- | --- |
| `HasCalls() bool` | At least one requested call. |
| `FirstCall() *FunctionCall` | First call, or nil (the refusal case). |
| `Refused() bool` | No calls — off-topic, unsupported, ambiguous or negated input. |
| `UngroundedOf(tool string) []string` | Ungrounded argument paths reported for that tool's calls. |
| `String() string` | Compact JSON of the whole envelope. |

## The refusal contract

There is **no free-text fallback** anywhere in the model. When the input does
not map onto a declared tool, the engine returns a `"call"` turn with an empty
`FunctionCalls` list. Every caller must handle it:

```go
resp, err := agent.Complete(ctx, userText)
if err != nil { /* transport/execution error */ }

switch {
case resp.Refused():
    answerDifferently()          // product copy, clarification, fallback UI
case resp.HasCalls():
    execute(resp.FunctionCalls)  // or let Run have done it
}
```

The three refusal causes worth distinguishing in UX:

- **Off-topic** — nothing in the toolset matches ("write me a poem").
- **Unsupported** — the action is in-domain but not declared ("order me a
  taxi" against a smart-home toolset).
- **Negated** — the request is the *inverse* of a supported one; check
  `resp.Validation.Negation` ("don't turn on the lights").

A `"respond"` turn with no calls is **not** a refusal: the loop finished and
`Results` is the answer. Distinguish by `resp.Type`.

## Confidence

`Confidence` is the **minimum of a post-hoc calibration head and the decode
probability** of the call, so its failure mode is escalation, not silent wrong
execution. The production contract:

1. Pick a threshold for your product (0.8 is a reasonable start; the
   [smart_home](../examples/smart_home/main.go) suite demonstrates gating at
   `--min-confidence`).
2. **Act at or above it.**
3. **Re-ask or route to a bigger model below it.**

```go
if resp.HasCalls() && resp.Confidence != nil && *resp.Confidence >= threshold {
    execute(resp.FunctionCalls[0])
} else {
    escalateOrReask()
}
```

Facts worth knowing:

- Calibration holds for the **base model only**. Tuned weights report nil —
  fine-tuning does not update the calibration head
  ([Tuned weights](weights.md)).
- Confidence is per-turn; in a `Run` loop, the final response's score is the
  one that gates execution of the batch.
- A nil confidence is a *state*, not a zero — never treat it as "very
  unconfident".

## Validation (grounding)

`Validation` is the engine's evidence audit of the call it just emitted:

```go
type Validation struct {
    Ungrounded []string // "tool.field" paths
    Negation   bool
}
```

- **`Ungrounded`** lists `tool.field` paths whose values the engine does not
  consider evidenced in the input. This includes **temporal grounding**: a
  date argument whose year matches nothing in the conversation or system facts
  is ungrounded — "due 5 May 1999" against a conversation dated 2026 flags
  `invoice.due_date`.
- **`Negation`** reports a negated request ("don't turn on the lights").

Two enforcement layers use this report:

- **`Run` (strict by default)** refuses to *execute* a call whose arguments
  are flagged ungrounded — the model receives an error instead and can retry.
  `RunStrict(false)` executes regardless.
- **Strict extraction** raises `needle.ExtractionValidationError` on temporal
  contradictions and engine-reported ungrounded paths
  ([Extraction](extraction.md#strict-mode)).

To audit by hand:

```go
for _, path := range resp.Validation.Ungrounded {
    fmt.Println("not evidenced:", path)     // e.g. "set_lights.brightness"
}
if resp.Validation.Negation {
    fmt.Println("the request was negated")
}
```

`UngroundedOf("set_lights")` returns just the field names for one tool's
calls.

### What counts as grounded

The engine is strict about *evidence*, including formatting: it does not
consider `"$1,200.00"` evidenced when it emits `1200.0` — the reformatting
drops characters. Expect reformatted numbers, synthesized summaries and
year-mismatched dates to land in `Ungrounded`; that is the feature working.

## Metrics

Every turn reports its own cost:

- `PrefillTPS` — prompt processing throughput, typically in the thousands of
  tokens per second on desktop hardware.
- `DecodeTPS` — generation throughput, typically hundreds.
- `PeakRAMMB` — the turn's peak footprint; a full session runs in roughly
  28 MB thanks to the 256-token sliding window with tool schemas pinned as KV
  sinks.

`Run` reports the metrics of its final turn; accumulate per-turn metrics
yourself when driving with `Complete`.

## Where to go next

- Executing on these responses: [Tool calling](tool-calling.md)
- Multi-turn context: [Conversations](conversations.md)
- Grounding as a hard error: [Extraction](extraction.md#strict-mode)
