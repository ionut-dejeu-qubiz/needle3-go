# Extraction

Extraction turns unstructured text into structured data — invoices, event
mentions, contact blocks, config fragments — with the same grammar-constrained
guarantees tool calls get. It is not a separate mode: **extraction is tool
calling with exactly one schema**, where the "tool call's" arguments are the
extracted record and the input text is the query.

With one declared schema the grammar admits exactly one call of that shape, so
schema conformance is *guaranteed rather than requested*: the right keys, the
right types, always.

- [`Extract`: typed records from structs](#extract-typed-records-from-structs)
- [`ExtractAs`: explicit schemas](#extractas-explicit-schemas)
- [`ExtractRaw`: plain maps](#extractraw-plain-maps)
- [Options](#options)
- [Strict mode](#strict-mode)
- [Grounded vs ungrounded values](#grounded-vs-ungrounded-values)
- [Extraction on an existing agent](#extraction-on-an-existing-agent)

## `Extract`: typed records from structs

Declare the record as a struct — the same tags as `ToolFunc` — and call
`Extract` with the text:

```go
type Invoice struct {
    Vendor  string  `needle:"vendor" desc:"company that issued the invoice" required:"true"`
    Total   float64 `needle:"total" desc:"invoice total amount" required:"true"`
    DueDate string  `needle:"due_date" desc:"payment due date" format:"date"`
}

invoice, err := needle.Extract[Invoice](ctx,
    "Invoice from Acme Corp, total 1200.00, due 2026-09-01")

// invoice.Vendor  == "Acme Corp"
// invoice.Total   == 1200.0
// invoice.DueDate == "2026-09-01"
```

Behavior:

- Returns `*Invoice` — or **nil when nothing matched** (no error). A record
  must be *evidenced* in the text; absence is a valid outcome, not a failure.
- Fields follow the same optionality rules as tools: pointer fields are
  optional, plain fields are required, `Arguments`-style re-keying maps
  `due_date` onto `DueDate`.
- Nested structs and arrays work exactly as in
  [Building tools](tools.md#nested-objects-and-arrays) — line items, addresses
  and repeated entities map onto nested object/array schemas.

## `ExtractAs`: explicit schemas

When the schema already exists — from configuration, or built with the
[Property](tools.md#dynamic-tools-with-builders) constructors — pass it
directly and still decode into a typed struct:

```go
schema := map[string]any{ /* full tool schema, or bare parameters object */ }

event, err := needle.ExtractAs[CalendarEvent](ctx, text, schema)
```

`ExtractAs` accepts a `*needle.Tool` (from `ToolFunc` or `NewTool`), a schema
`map[string]any` (full `{"name","description","parameters"}` or a bare
parameters object), or a JSON string.

## `ExtractRaw`: plain maps

Skip decoding and take the raw argument map:

```go
fields, err := needle.ExtractRaw(ctx, text, schema)
if len(fields) > 0 {
    total, _ := fields["total"].(float64)
}
```

Useful for schema-of-unknown-shape pipelines (config ingestion, dynamic forms)
where the shape lives in data, not Go types.

## Options

| Option | Default | Effect |
| --- | --- | --- |
| `ExtractSystem(s)` | — | System facts for the one-shot agent; a `date:` fact licenses relative dates (see [Conversations](conversations.md#system-facts)). |
| `ExtractMaxTokens(n)` | 256 | Response token cap. |
| `ExtractStrict(false)` | strict | Return ungrounded values instead of raising (see below). |
| `ExtractWeights(path)` | — | Extract against a tuned `.cact` archive. |
| `ExtractEnginePath(path)` | — | Pin the engine library (see [Engine management](engine.md)). |

## Strict mode

Strict is the default, and it makes extraction *fail loudly* on values the
engine does not consider evidenced:

```go
_, err := needle.Extract[Invoice](ctx, "total $1,200.00 ...")
// err wraps needle.ExtractionValidationError: reformatted number
```

Strict mode raises `ExtractionValidationError` for:

1. **Temporal contradictions** — a date whose year matches nothing in the text
   or system facts (the `due 1999` case from
   [Responses: validation](responses.md#validation-grounding)).
2. **Engine-reported ungrounded paths** — values the engine's evidence audit
   flagged, including reformatted numbers.
3. **Negated requests** — "not an invoice for..." style input.

The alternative is explicit:

```go
invoice, err := needle.Extract[Invoice](ctx, text, needle.ExtractStrict(false))
// ungrounded values are returned instead of raising
```

Loose mode keeps whatever the model emitted; combine it with a manual audit of
`Validation.Ungrounded` when you want the values *and* the warnings.

## Grounded vs ungrounded values

The engine distinguishes *evidence* from *plausibility*, and extraction leans
on it hard:

| Input text | Emitted value | Grounded? |
| --- | --- | --- |
| `total 1200.00` | `1200.0` | yes — same characters |
| `total $1,200.00` | `1200.0` | **no** — `$` and `,` were dropped by reformatting |
| `due 5 May` (no year anywhere) | `2026-05-05` | **no** — year is invented |
| `due 5 May 2026` | `2026-05-05` | yes — year is licensed |
| `the Acme Corp invoice` | `Acme Corp` | yes — verbatim span |

Optional fields with no evidence are **omitted** (nil pointers / missing
keys), never guessed — check presence before use, same as
[tool arguments](tool-calling.md#typed-argument-decoding).

## Extraction on an existing agent

Agents can run one-shot extractions with their own weights and engine
settings, without disturbing their conversation:

```go
fields, err := agent.ExtractRaw(ctx, text, schema)
```

`agent.ExtractRaw` opens a fresh engine conversation for the extraction and
leaves the agent's main conversation untouched. To inherit the agent's tuned
archive into package-level extraction instead, pass `ExtractWeights`
explicitly.

## Where to go next

- The tag vocabulary for record structs: [Building tools](tools.md)
- The validation machinery behind strict mode: [Responses](responses.md#validation-grounding)
- Runnable code: [extraction example](../examples/extraction/main.go)
