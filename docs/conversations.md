# Conversations

An agent owns one conversation. This page covers keeping that conversation
coherent across turns, supplying environment facts, rewinding, and running
several agents side by side.

- [One agent, one conversation](#one-agent-one-conversation)
- [Multi-turn conversations](#multi-turn-conversations)
- [System facts](#system-facts)
- [Reset](#reset)
- [Concurrency and multiple agents](#concurrency-and-multiple-agents)
- [Context cancellation](#context-cancellation)

## One agent, one conversation

The contract in one breath: **one agent per toolset, one conversation per
agent.** `New` compiles the toolset into the engine session; from then on:

- Repeated `Run`/`Complete` calls **continue the same conversation** — later
  turns see earlier ones.
- `Reset` rewinds the conversation, keeping the tools loaded.
- Changing tools requires a **new agent** — the grammar is baked at `New`.

```go
agent, _ := needle.New(needle.WithTools(getWeather, setLights))
defer agent.Close()

agent.Run(ctx, "what's the weather in Paris?")       // turn 1
agent.Run(ctx, "and what about Lagos?")              // turn 2: sees turn 1
agent.Run(ctx, "now dim the living room to 30")      // topic switch, same conversation
```

## Multi-turn conversations

Later turns resolve against earlier ones. "And what about Lagos?" has no city
of its own — it inherits the intent of the weather question and substitutes
the new entity. The [conversation example](../examples/conversation/main.go)
shows the full pattern, including how a topic switch mid-conversation works
and how off-topic turns refuse cleanly:

```go
for _, turn := range []string{
    "what's the weather in Paris?",
    "and what about Lagos?",          // follow-up: inherits intent
    "now dim the living room to 30",  // switch: new tool, same conversation
    "write me a poem about the sea",  // off-topic: refused, no free text
} {
    resp, err := agent.Complete(ctx, turn)
    // handle calls, feed results back (see Tool calling), print, repeat
}
```

The conversation window is bounded — a 256-token sliding window with the tool
schemas pinned as KV sinks — so long conversations cost the same memory as
short ones, and nothing needs pruning on your side.

## System facts

An optional system turn carries **environment state as facts, never
instructions**:

```go
agent, _ := needle.New(
    needle.WithTools(tools),
    needle.WithSystem("date: 2026-07-21 Tue 14:30; locale: en-US; device: phone; battery: 62%"),
)
```

Recognized keys:

| Key | Example | Effect |
| --- | --- | --- |
| `date` | `date: 2026-07-21 Tue 14:30` | Licenses relative time: "tomorrow at 7" resolves to an absolute date *only* when a `date:` fact exists. |
| `locale` | `locale: en-US` | Formats dates, numbers and names. |
| `device` | `device: phone` | Surfaces device shape for device-aware tools. |
| `battery`, `network` | `battery: 62%` | Environment state your handlers can also read. |
| `location` | `location: Paris` | Grounds location-relative language. |
| `user`, `assistant` | `user: Alex` | Names the parties. |

Two design points:

1. **Facts, not prompts.** The model trains with and without this turn;
   instructions placed there do not steer it. Put steering in tool
   descriptions, where the grammar lives.
2. **Temporal grounding reads it.** A date argument is grounded when its year
   matches the conversation *or the `date:` fact* — supply the fact and
   relative dates stop being ungrounded (see
   [Responses: validation](responses.md#validation-grounding)).

## Reset

`Reset` rewinds the conversation while keeping the tools loaded — the engine
is re-initialized to the post-tools state, not to a blank slate:

```go
agent.Reset()
resp, _ := agent.Run(ctx, "totally unrelated new task")  // fresh conversation
```

Use it when one agent serves many independent requests in a row (a request
handler processing queued jobs, or the acceptance-suite loop in
[smart_home](../examples/smart_home/main.go), which resets between test
cases).

## Concurrency and multiple agents

The native engine keeps global state, so **at most one in-process session is
bound per engine generation**; calls are serialized by a package mutex.
Multiple agents in one process take turns — and switching agents reconfigures
the engine, which **rewinds the conversation being switched away from**.

What this means in practice:

- **Turn-taking is fine.** Agent A and agent B alternating calls works; each
  switch rewinds *the other agent's* conversation, so long-running
  conversations should not be interleaved with other agents.
- **Independent conversations want isolation.** Two options:
  - Tuned agents isolate themselves — each runs in **its own worker process**
    with its own engine state ([Tuned weights](weights.md)).
  - Base-model agents that need independent conversations run in **separate
    processes** (or accept the rewinding semantics above).
- **`Run` is not reentrant on one agent** — calls serialize, so concurrent
  `Run`s on the same agent queue behind each other rather than fail.

A typical single-purpose service needs none of this: one agent, one
conversation, `Reset` between jobs.

## Context cancellation

`Complete` and `Run` accept a `context.Context`:

- **In-process turns** check the context around the native call — deadlines
  and cancellation surface as errors from the pending call.
- **Worker turns** (tuned weights) propagate cancellation by terminating the
  child process, so a cancelled request cannot leave a zombie generation
  behind.

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

resp, err := agent.Run(ctx, query)
if errors.Is(err, context.DeadlineExceeded) {
    // the turn was cut off; the conversation state is whatever completed
}
```

## Where to go next

- The turns themselves: [Tool calling](tool-calling.md)
- What each turn returns: [Responses](responses.md)
- Process isolation: [Tuned weights](weights.md)
