# Building tools

A tool is a function the model is allowed to call. Declaring one answers three
questions the engine asks:

1. **What does it do?** — the name and description; the model reads these to
   decide *whether* to call it.
2. **What arguments does it take?** — a JSON Schema; the engine compiles it
   into a byte-level grammar so the model can only emit *valid* arguments.
3. **What happens when it is called?** — a Go handler you supply; the engine
   never executes anything itself.

There are three equivalent ways to declare tools, from most to least
Go-idiomatic. They can be freely mixed in one toolset.

- [`ToolFunc` — typed tools with struct tags](#typed-tools-with-struct-tags)
- [`NewTool` — dynamic tools with builders](#dynamic-tools-with-builders)
- [Raw JSON schemas](#raw-json-schemas)
- [The tag reference](#the-tag-reference)
- [Go types to JSON types](#go-types-to-json-types)
- [Nested objects and arrays](#nested-objects-and-arrays)
- [The handler contract](#the-handler-contract)
- [Schema-only tools](#schema-only-tools)
- [What the grammar actually enforces](#what-the-grammar-actually-enforces)
- [Writing descriptions that work](#writing-descriptions-that-work)
- [Inspecting the generated schema](#inspecting-the-generated-schema)

## Typed tools with struct tags

`ToolFunc` is the workhorse: you define an argument struct, tag it, and the
handler receives calls already decoded into that struct. The tags compile into
the JSON Schema — you never write a schema by hand.

```go
type SetThermostatArgs struct {
    Temperature int    `needle:"temperature" desc:"target temperature in Celsius" min:"10" max:"30" required:"true"`
    Mode        string `needle:"mode" desc:"heating strategy to use" enum:"heat|cool|auto"`
}

setThermostat := needle.ToolFunc("set_thermostat",
    "Set the thermostat.",
    func(ctx context.Context, args SetThermostatArgs) (any, error) {
        return map[string]any{"temperature": args.Temperature, "mode": args.Mode}, nil
    })

agent, _ := needle.New(needle.WithTools(setThermostat))
resp, _ := agent.Run(ctx, "make it 21 and cool the room")
```

What the engine sees for the struct above:

```json
{
  "name": "set_thermostat",
  "description": "Set the thermostat.",
  "parameters": {
    "type": "object",
    "properties": {
      "temperature": {"type": "integer", "description": "target temperature in Celsius",
                       "minimum": 10, "maximum": 30},
      "mode":        {"type": "string", "description": "heating strategy to use",
                       "enum": ["heat", "cool", "auto"]}
    },
    "required": ["temperature"]
  }
}
```

Field-name resolution for the `needle` tag: the tag name wins, then the `json`
tag, then the snake_cased field name. `needle:"-"` skips the field entirely.
Because arguments re-key themselves onto Go field names when decoded, you never
need duplicate `json` tags — a field named `DueDate` with `needle:"due_date"`
receives the model's `due_date` value.

### Optional and required fields

- Plain fields are **required**: the grammar forces the model to emit them.
- `*T` pointer fields are **optional** and decode to nil when omitted.
- `required:"true"` forces a pointer field required; `optional:"true"` (or
  `omitempty`, or a `default`) forces a plain field optional.

The model omits optional fields it finds no evidence for — an argument without
a value in the input is *omitted, not guessed*. Always nil-check pointers
before use.

## Dynamic tools with builders

When the toolset is assembled at runtime — from configuration, plugins, or a
database — build schemas programmatically with `NewTool` and the `Property`
constructors:

```go
setLights := needle.NewTool("set_lights",
    "Turn a room's lights on or off and set brightness").
    Params(map[string]needle.Property{
        "room":       needle.Str(needle.Desc("which room to control"), needle.IsRequired),
        "on":         needle.Bool(needle.IsRequired),
        "brightness": needle.Int(needle.Desc("0 to 100"), needle.Min(0), needle.Max(100)),
    }).
    Handler(func(ctx context.Context, args map[string]any) (any, error) {
        return map[string]any{"ok": true, "room": args["room"]}, nil
    }).
    Build()
```

Every constructor takes constraint options that map onto JSON-Schema keys:

| Constructor | JSON type |
| --- | --- |
| `Str(opts...)` | string |
| `Int(opts...)` | integer |
| `Num(opts...)` | number |
| `Bool(opts...)` | boolean |
| `Any(opts...)` | unconstrained |
| `Enum(values ...any)` | type inferred from the first value |
| `ArrayOf(items Property, opts...)` | array with an item schema |
| `ObjectOf(props map[string]Property, opts...)` | nested object |

Available options: `Desc`/`Description`, `IsRequired`, `Min`, `Max`, `GT`,
`LT`, `Range`, `MultipleOf`, `MinLen`, `MaxLen`, `Pattern`, `Format`,
`MinItems`, `MaxItems`, `Unique`, `Const`, `Default`, `EnumValues`. Each
attaches the JSON-Schema key of the same meaning — the exact same vocabulary as
the struct tags in the [tag reference](#the-tag-reference) below.

`Params` collects every property marked `IsRequired` into the schema's
`required` array (deterministically sorted; struct-tag schemas keep declaration
order). `RawParams(schema map[string]any)` attaches a hand-written parameters
object instead, and `Build()` freezes the tool.

## Raw JSON schemas

The engine ultimately consumes a JSON array of
`{"name", "description", "parameters"}` objects. When your schemas already
exist in that shape — maintained as configuration, generated elsewhere, or
versioned as data — pass them straight through:

```go
toolsJSON := `[{
    "name": "set_lights",
    "description": "Turn a room's lights on or off and set brightness",
    "parameters": {
        "type": "object",
        "properties": {
            "room":       {"type": "string", "description": "which room to control"},
            "on":         {"type": "boolean"},
            "brightness": {"type": "integer", "minimum": 0, "maximum": 100}
        },
        "required": ["room", "on"]
    }
}]`

agent, _ := needle.New(needle.WithToolsJSON(toolsJSON))
```

`WithTools` accepts the same JSON string, raw `map[string]any` values, or a
slice mixing built `*Tool` values with raw schemas. Bare parameters objects
(wrapped with a generic name) are accepted too.

Because raw tools have no handler, calls to them are answered by `Run` with an
`unknown tool` error result — see [Schema-only tools](#schema-only-tools).

## The tag reference

| Tag | JSON-Schema key | Meaning |
| --- | --- | --- |
| `needle:"name"` | property name | The argument name; falls back to the `json` tag, then to the snake_case field name. `needle:"-"` skips the field. |
| `desc` / `description` | `description` | Human-readable description the model reads. |
| `required` | membership in `required` | `"true"` forces the field required. |
| `optional` | — | Forces the field optional (also implicit for pointers, `omitempty`, and `default`). |
| `enum` | `enum` | Fixed choice set, values separated by `\|` — e.g. `enum:"heat\|cool\|auto"`. Grammar-enforced: the model cannot emit anything else. |
| `const` | `const` | Single fixed value. |
| `min` / `minimum`, `max` / `maximum` | `minimum`, `maximum` | Inclusive numeric bounds. Grammar-enforced. |
| `gt` / `exclusive-min`, `lt` / `exclusive-max` | `exclusiveMinimum`, `exclusiveMaximum` | Exclusive numeric bounds. |
| `multiple-of` | `multipleOf` | Numeric multiple constraint. |
| `min-length`, `max-length` | `minLength`, `maxLength` | String length bounds. |
| `pattern` | `pattern` | Regular expression for strings. |
| `format` | `format` | `date`, `date-time`, and friends. |
| `min-items`, `max-items`, `unique` | `minItems`, `maxItems`, `uniqueItems` | Array constraints. |
| `default` | `default` | Default value; also makes the field optional. |

## Go types to JSON types

| Go type | JSON-Schema type |
| --- | --- |
| `string` | string |
| `bool` | boolean |
| all integer kinds (`int`, `int64`, `uint16`, …) | integer |
| `float32`, `float64` | number |
| slices and arrays | array (with the element type as `items`) |
| maps and structs | object (recursing into fields/entries) |
| pointers | unwrap (and make the field optional) |
| `any` / `interface{}` | unconstrained |
| `time.Time` | string with `format: date-time` |
| `time.Duration` | integer (nanoseconds) |

## Nested objects and arrays

Structs, slices of structs, embedded structs and nested maps all map onto
nested schemas automatically — the compiler recurses and applies tags at every
level:

```go
type CreatePlaylistArgs struct {
    Name  string `needle:"name" desc:"playlist name" required:"true"`
    Tracks []Track `needle:"tracks" desc:"tracks to add" min-items:"1"`
}

type Track struct {
    Title  string `needle:"title" desc:"track title" required:"true"`
    Artist string `needle:"artist" desc:"performing artist"`
}
```

produces:

```json
{
  "type": "object",
  "properties": {
    "name":   {"type": "string", "description": "playlist name"},
    "tracks": {"type": "array", "description": "tracks to add", "minItems": 1,
                "items": {"type": "object",
                          "properties": {
                            "title":  {"type": "string", "description": "track title"},
                            "artist": {"type": "string", "description": "performing artist"}
                          },
                          "required": ["title"]}}
  },
  "required": ["name"]
}
```

Typed handlers receive the whole decoded structure — `args.Tracks[0].Title`
works with no manual unmarshaling, and the same re-keying applies at every
nesting level.

## The handler contract

A handler is any function matching

```go
type Handler func(ctx context.Context, arguments map[string]any) (any, error)
```

For `ToolFunc`, the generic wrapper adapts your typed function onto this
shape. The rules:

- **The return value is fed back to the model** and attached to
  `Response.Results`. Any JSON-marshalable value works; values that fail to
  marshal fall back to their `fmt` representation.
- **Returning an error maps to a `{"error": "<message>"}` result** — the model
  sees it and can recover on the next round (retry with different arguments,
  or give up). Return errors for domain failures ("city not found"), not for
  panics.
- **Panics are recovered** and reported as errors, so one bad handler cannot
  take the process down.
- **The context is the call's context**: cancellation and deadlines propagate.

Handlers run only under `Agent.Run` (and `Tool.Invoke` when you drive the loop
yourself with `Complete` — see [Tool calling](tool-calling.md)).

## Schema-only tools

A declared schema without a handler still enters the grammar — the model *can*
call it. When `Run` executes such a call, the result is an
`unknown tool: <name>` error, which the model sees like any handler error.
This is useful for letting the engine surface what it *would* call before you
wire execution up, and it is exactly what happens with
[raw JSON schemas](#raw-json-schemas).

## What the grammar actually enforces

The engine compiles your schema into a byte-level grammar that constrains
*every decoded token*. On the current engine build (2.0.4):

- **Enums are hard constraints.** The model cannot emit a value outside the
  set — a typo is structurally impossible.
- **Numeric bounds are hard constraints.** An out-of-range value cannot be
  emitted; out-of-bounds *requests* are refused outright rather than clamped.
- **Descriptions, lengths and patterns guide but do not hard-enforce.** They
  shape what the model prefers to emit, and the engine's validation layer
  reports violations afterwards, but the grammar itself will not block, say, a
  41-character string under `max-length: 40`.

Treat enums and bounds as your correctness guarantees; treat descriptions and
string constraints as strong suggestions you verify in the handler.

## Writing descriptions that work

The model chooses tools and fills arguments using **only** the evidence in the
user text plus your names and descriptions. Refusals and hallucinated
arguments are almost always a description problem. Rules of thumb:

1. **Name the tool's purpose precisely.** "Get the current weather for a city"
   beats "weather" — the name narrows *when to call*, the description narrows
   *what it does*.
2. **Describe every argument in terms of the user's words.** "The room to
   control", "target temperature in Celsius" — the model maps input spans onto
   these phrases.
3. **Close the sets.** Anything with a fixed vocabulary (rooms, modes, colors,
   statuses) should be an `enum`, not a free string.
4. **Bound the numbers.** Give `min`/`max` for anything physical; the grammar
   then refuses out-of-range requests instead of emitting them.
5. **Say what to omit.** "include only when the user names one" on optional
   fields prevents the model from inventing values.

The [smart_home example](../examples/smart_home/main.go) is a curated reference
toolset built exactly on these rules.

## Inspecting the generated schema

To see what the engine will see — for debugging, diffing or feeding other
systems — compile a struct's schema directly:

```go
schema, err := needle.SchemaFromStruct[CreatePlaylistArgs]()
```

The CLI can also dump a complete toolset: the smart_home example has
`--schema` for exactly this.

## Where to go next

- Executing calls: [Tool calling](tool-calling.md)
- Large catalogues: [Tool indexes](tool-indexes.md)
- What each response carries: [Responses](responses.md)
