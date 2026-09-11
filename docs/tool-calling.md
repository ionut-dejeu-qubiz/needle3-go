# Tool calling

Declaring tools (see [Building tools](tools.md)) is half the story; this page
is the other half — how calls actually execute, who drives the loop, and how to
read what comes back.

Two methods cover everything:

- **`Agent.Run`** — the agentic loop. The model emits calls, Needle executes
  your handlers, results are fed back, and the loop repeats until the model is
  done. One call, one final answer.
- **`Agent.Complete`** — one raw turn. You get the model's requested calls and
  execute them (or not) yourself, feeding results back with the next
  `Complete`. Maximum control, maximum visibility.

- [`Run`: the agentic loop](#run-the-agentic-loop)
- [`Complete`: driving the loop by hand](#complete-driving-the-loop-by-hand)
- [Typed argument decoding](#typed-argument-decoding)
- [Multiple tools in one toolset](#multiple-tools-in-one-toolset)
- [Parallel calls](#parallel-calls)
- [Refusals](#refusals)
- [Confidence gating](#confidence-gating)
- [Loop options](#loop-options)

## `Run`: the agentic loop

`Run` is the right default. One round of the loop looks like this:

```
user text ──▶ engine ──▶ function calls ──▶ your handlers ──▶ results
                         (grammar-constrained)        │
    ▲                                                   │
    └────────────── results fed back ◀──────────────────┘
```

The engine requests calls with `Type == "call"`, your handlers execute, each
round's results are marshaled and fed back as the next turn, and the loop stops
when the engine answers `Type == "respond"` — or after the round cap
(`MaxSteps`, default 8). The final response carries everything the loop
executed in `Results`:

```go
resp, err := agent.Run(ctx, "dim the living room to 30")
// resp.Type         == "respond"
// resp.Results      == [{"ok":true,"room":"living room","action":"dim","brightness":30}]
// resp.Confidence   != nil
```

Because results are fed back through the model, it can **recover from handler
errors**: if your handler returns an error, the model sees
`{"error": "<message>"}` and may retry with corrected arguments — for example
after an out-of-range or not-found error.

### Strict grounding inside `Run`

By default `Run` is strict: a call whose arguments the engine flagged as
ungrounded is *not* executed — an error is fed back instead. Use
`RunStrict(false)` to execute regardless (see
[Responses: validation](responses.md#validation-grounding)).

## `Complete`: driving the loop by hand

`Complete` returns the raw engine turn. When it carries calls, you execute
them and feed the results back as JSON:

```go
resp, _ := agent.Complete(ctx, "dim the living room to 30")

if resp.HasCalls() {
    call := resp.FirstCall()
    result := executeYourTool(call.Name, call.Arguments)   // your dispatch

    blob, _ := json.Marshal(result)
    resp, _ = agent.Complete(ctx, string(blob))            // feed the result back
}
```

The feedback turn is **the JSON encoding of the results** — for one call, the
result object; for several calls in one turn, the JSON array of results in the
same order. This is exactly the protocol `Run` implements on your behalf.

Hand-driving the loop buys you three things:

- **Arbitrary execution policy** — audit, rate-limit, require user
  confirmation, or forward calls to another machine before executing.
- **Custom result shaping** — filter or rewrite what the model sees.
- **Full protocol visibility** — every intermediate response is yours to
  inspect, log and metrics.

The [conversation example](../examples/conversation/main.go) drives a complete
session by hand, including a follow-up, a topic switch and an off-topic
refusal.

## Typed argument decoding

Driving by hand means working with `call.Arguments` (`map[string]any`).
`DecodeArguments` re-uses the same name mapping `ToolFunc` uses — snake_case
schema names land on Go field names — so one line gives you a typed value:

```go
call := resp.FirstCall()

var args SetLightsArgs
if err := needle.DecodeArguments[SetLightsArgs](call.Arguments); err != nil {
    // the call violated your struct; impossible when tags and schema match
}

if args.Brightness != nil {
    setBrightness(*args.Brightness)
}
```

It recurses through nested structs, pointers and slices, mirroring what typed
handlers receive automatically.

## Multiple tools in one toolset

Declare as many tools as you like (up to five rendered directly — beyond that,
retrieval engages, see [Tool indexes](tool-indexes.md)):

```go
getWeather := needle.ToolFunc("get_weather", /* ... */)
setLights  := needle.ToolFunc("set_lights",  /* ... */)

agent, _ := needle.New(needle.WithTools(getWeather, setLights))
```

The model picks the tool that matches the request:

```go
resp, _ := agent.Run(ctx, "what's the weather in Paris?")
// resp.FirstCall().Name == "get_weather"

resp, _ = agent.Run(ctx, "dim the living room to 30")   // same agent, same conversation
// resp.FirstCall().Name == "set_lights"
```

Two rules to internalize:

1. **One agent per toolset.** Tools are compiled into the engine session at
   `New`; changing tools means building a new agent. (An agent can answer
   un-related questions across topics — the toolset is fixed, the conversation
   is not.)
2. **An agent owns one conversation.** Repeated `Run`/`Complete` calls continue
   it; `Reset` rewinds it while keeping the tools loaded. For independent
   conversations in one process, see
   [Conversations: concurrency](conversations.md#concurrency-and-multiple-agents).

### Mixing declaration styles

`WithTools` accepts built `*Tool` values, raw schema maps, JSON strings and
slices of any of these, mixed:

```go
agent, _ := needle.New(needle.WithTools(
    getWeather,                       // *Tool from ToolFunc
    rawSchemaMap,                     // map[string]any
    `[{"name": "...", ...}]`,         // JSON string
))
```

`agent.Tools()` reports the declared names in declaration order — useful for
logging and for building your own dispatch tables under `Complete`.

## Parallel calls

One `"call"` turn may carry **several** `FunctionCalls` — the model batching
independent actions. For "turn on the kitchen lights and the bedroom lights"
over a single-tool schema, you may see two calls to the same tool with
different arguments.

`Run` executes every call of a turn in order and feeds all results back
together. Under `Complete`, iterate `resp.FunctionCalls` yourself and feed back
the JSON array of results **in the same order**:

```go
var results []any
for _, call := range resp.FunctionCalls {
    results = append(results, dispatch(call.Name, call.Arguments))
}
feedback, _ := json.Marshal(results)
resp, _ = agent.Complete(ctx, string(feedback))
```

Check `len(resp.FunctionCalls)` before assuming a single call, and prefer
handling "one logical action = one call" in descriptions if batching is
undesirable for your domain.

## Refusals

The engine has **no free-text fallback**. Off-topic input, unsupported
requests, ambiguity and negation all produce a `"call"` turn with an **empty**
`FunctionCalls` list:

```go
resp, _ := agent.Run(ctx, "write me a poem about the sea")

if resp.Refused() {
    // no call was made; a product answers, re-asks or routes elsewhere
}
```

`Refused()` is exactly `len(resp.FunctionCalls) == 0`; `FirstCall()` returns
nil in the same case. A `"respond"` turn with no calls means something
different: the loop *finished* — the answer is the accumulated `Results`.
Distinguish the two by `resp.Type`.

## Confidence gating

Every base-model response carries a calibrated `Confidence` in `[0,1]` — the
minimum of a post-hoc calibration head and the decode probability of the call.
The intended contract: pick a threshold for your product, **act at or above
it, re-ask or escalate below it**:

```go
resp, _ := agent.Complete(ctx, userText)

if resp.HasCalls() && resp.Confidence != nil && *resp.Confidence >= 0.8 {
    execute(resp.FunctionCalls[0])
} else {
    escalateOrReask()
}
```

`Confidence` is nil for tuned weights (fine-tuning does not update the
calibration head) — see [Tuned weights](weights.md). Full semantics:
[Responses: confidence](responses.md#confidence).

## Loop options

| Option | Applies to | Default | Effect |
| --- | --- | --- | --- |
| `MaxSteps(n)` | `Run` | 8 | Tool-call round cap for one `Run`. |
| `RunMaxTokens(n)` | `Run` | 256 | Per-round response token cap. |
| `RunStrict(false)` | `Run` | strict | Execute calls even when arguments are flagged ungrounded. |
| `MaxTokens(n)` | `Complete` | 256 | Response token cap for the turn. |

Both methods take a `context.Context`; in-process turns check it around the
native call, and worker turns propagate cancellation by killing the child
process (see [Tuned weights](weights.md)).

## Where to go next

- Reading every response field: [Responses](responses.md)
- Continuing conversations across turns: [Conversations](conversations.md)
- More than five tools: [Tool indexes](tool-indexes.md)
