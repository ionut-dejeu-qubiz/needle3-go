# Tool indexes

Small toolsets render directly in the engine's context window. Large ones
cannot — and do not need to. This page covers what changes when you declare
more than five tools, and how to persist the retrieval machinery so restarts
are instant.

- [The five-tool threshold](#the-five-tool-threshold)
- [How retrieval works](#how-retrieval-works)
- [Why the grammar follows the window](#why-the-grammar-follows-the-window)
- [Persisting indexes with `WithToolIndexPath`](#persisting-indexes-with-withtoolindexpath)
- [Index fingerprints and re-embedding](#index-fingerprints-and-re-embedding)
- [Managing index files](#managing-index-files)
- [Designing a catalogue that retrieves well](#designing-a-catalogue-that-retrieves-well)
- [A complete catalogue example](#a-complete-catalogue-example)

## The five-tool threshold

- **Five or fewer declared tools** — every schema is rendered into the context
  in full, and the decode grammar covers the entire toolset. Nothing to
  configure; this is the [smart_home](../examples/smart_home/main.go) shape.
- **More than five** — the engine switches to **retrieval**: only the five
  highest-scoring tools for the current query are rendered, and the grammar is
  rebuilt over exactly that subset.

The switch is automatic and per-turn. You declare the whole catalogue once at
`New`; the engine decides which five each turn needs.

## How retrieval works

At initialization, the engine embeds every tool schema once with a built-in
contrastive head. Then, for every turn:

1. The query text is embedded with the same head.
2. Every tool's schema embedding is scored against the query embedding.
3. The **top five** tools enter the context for that turn.

This is schema retrieval, not string matching — "quiet evening music" can
select `set_lights` (warm, dim) and `play_playlist` without sharing a single
keyword, because the embeddings compare *meanings*.

## Why the grammar follows the window

The constrained-decoding grammar is compiled over the tools actually present
in the context, so an unselected tool is **unreachable, not merely unlikely**:
the model physically cannot emit a call to it that turn. If the user's next
turn needs a different tool, that turn's window simply contains it instead.

Practical consequences:

- **No cross-tool argument bleed.** A tool that is not in the window cannot
  contribute its parameter names to a call.
- **No "when in doubt, call something".** Refusals stay clean: if nothing in
  the window matches, the model refuses rather than reaching outside it.
- **Catalogue size is cheap.** Doubling from 20 to 40 tools costs one extra
  embedding at init and per-turn scoring — not context window.

## Persisting indexes with `WithToolIndexPath`

Embedding a large catalogue at every process start takes time proportional to
the catalogue size. Persist the embeddings to skip it:

```go
agent, err := needle.New(
    needle.WithTools(bigCatalogue...),
    needle.WithToolIndexPath("tools.idx"),
)
```

- If `tools.idx` exists and matches the current catalogue, the index loads and
  the agent is ready immediately.
- If it does not exist, the catalogue is embedded now and the index is written
  out for next time.
- The path is yours to choose; a common pattern is a per-application file
  under the user's cache or config directory.

Without `WithToolIndexPath`, everything still works — you just re-embed the
catalogue in every process start.

## Index fingerprints and re-embedding

The index file is keyed by a **fingerprint over the tool schemas and the
model**. On load:

- **Matching fingerprint** — the whole index is reused; startup is instant.
- **Changed fingerprint** — the index is rebuilt. Only the *changed* schemas
  are re-embedded, so editing one tool's description in a 50-tool catalogue
  re-embeds one tool, not fifty.

You never manage fingerprints yourself; the guarantees are:

- A stale index is never silently used for a changed schema.
- Renaming, reordering, re-describing or re-constraining a tool invalidates
  exactly that tool's entry.

## Managing index files

- **One index per toolset.** The fingerprint covers the whole declared set;
  two agents with different catalogues need two files.
- **Index files are disposable.** Deleting one is always safe — the worst case
  is a one-time re-embed. Ship products that can regenerate them.
- **Committing indexes is usually wrong.** They are derived artifacts; keep
  them in the runtime cache directory (`.gitignore`'d — see
  [`*.idx` in the repo's `.gitignore`](../.gitignore)) rather than in source
  control.
- **Permissions** — the file is written next to where you point it; ensure the
  directory is writable at first launch.

## Designing a catalogue that retrieves well

Retrieval selects the *candidates*; the model still reads the same names and
descriptions to pick and fill the call. Everything from
[Writing descriptions that work](tools.md#writing-descriptions-that-work)
applies, plus catalogue-scale rules:

1. **Make names self-similar and distinctive.** `play_track`,
   `pause_playback`, `set_volume` retrieve better than generic
   `play`, `pause`, `volume` mixed into a big pool.
2. **Descriptions should name the use cases.** The embedding of "Skip to the
   next track" places the tool next to queries about skipping — even when the
   word "skip" never appears in the query.
3. **Split overloaded tools.** One tool with a `mode` enum of twelve values
   retrieves worse than three tools with clear purposes.
4. **Keep argument descriptions concrete too.** The whole schema is embedded,
   so argument wording influences which tools surface.
5. **Five tools is not a target.** Use exactly as many as the domain needs;
   crossing the threshold is free, and staying under it is not a virtue.

## A complete catalogue example

The [catalog example](../examples/catalog/main.go) declares eight media tools
with the builder API, persists an index, and runs three queries:

```go
agent, err := needle.New(
    needle.WithTools(tools),                // 8 tools - retrieval engages
    needle.WithToolIndexPath("media-tools.idx"),
)
fmt.Printf("declared %d tools (retrieval selects five per turn)\n", len(agent.Tools()))

for _, query := range []string{
    "play humble bundle by molly ten",
    "set the volume to 40",
    "play my late night playlist on shuffle",
} {
    resp, _ := agent.Complete(ctx, query)
    // resp.FirstCall().Name: play_track, set_volume, play_playlist
}
```

Run it with:

```sh
go run ./examples/catalog
```

## Where to go next

- Declaring the tools themselves: [Building tools](tools.md)
- Executing whatever got selected: [Tool calling](tool-calling.md)
- The context window and memory model: [Responses](responses.md#metrics)
