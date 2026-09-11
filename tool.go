package needle

import (
	"context"
	"fmt"
)

// Handler executes a tool call and returns the value that is fed back to
// the model and attached to Response.Results. Returning an error maps to
// a {"error": "<message>"} result, which the model sees and can recover
// from. Panics are recovered and reported the same way.
type Handler func(ctx context.Context, arguments map[string]any) (any, error)

// Tool is a declared function the model may call. Build one with ToolFunc
// (typed, struct-tag schema plus a typed handler), NewTool (dynamic
// builder over Property values), or pass raw JSON schemas to the agent
// directly.
type Tool struct {
	name        string
	description string
	parameters  map[string]any
	handler     Handler
}

// Name returns the tool's call name.
func (t *Tool) Name() string { return t.name }

// Description returns the tool's description.
func (t *Tool) Description() string { return t.description }

// HasHandler reports whether the tool carries an executable handler, i.e.
// whether Agent.Run can execute its calls.
func (t *Tool) HasHandler() bool { return t != nil && t.handler != nil }

// Schema returns the tool as the JSON-schema object the engine consumes:
// {"name": ..., "description": ..., "parameters": {...}}.
func (t *Tool) Schema() map[string]any {
	out := map[string]any{"name": t.name}
	if t.description != "" {
		out["description"] = t.description
	}
	if t.parameters != nil {
		out["parameters"] = t.parameters
	} else {
		out["parameters"] = map[string]any{"type": "object"}
	}
	return out
}

// Invoke executes the tool's handler. A tool without a handler returns
// an "unknown tool" error.
func (t *Tool) Invoke(ctx context.Context, arguments map[string]any) (result any, err error) {
	if t == nil || t.handler == nil {
		return nil, fmt.Errorf("unknown tool: %s", t.name)
	}
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("tool %s panicked: %v", t.name, r)
		}
	}()
	return t.handler(ctx, arguments)
}

// ToolFunc declares a tool from a typed handler. The argument struct's
// tags define the JSON schema the engine's decode grammar is compiled
// from, and the handler receives the call arguments decoded into that
// struct:
//
//	type LightsArgs struct {
//	    Room   string `needle:"room" desc:"which room to control" required:"true"`
//	    Action string `needle:"action" desc:"on, off, or dim" enum:"on|off|dim" required:"true"`
//	    Bright *int   `needle:"brightness" desc:"0 to 100" min:"0" max:"100"`
//	}
//
//	lights := needle.ToolFunc("control_lights",
//	    "Turn lights on or off in a room, dim them, or set color.",
//	    func(ctx context.Context, a LightsArgs) (any, error) {
//	        return map[string]any{"ok": true, "room": a.Room}, nil
//	    })
func ToolFunc[A any](name, description string, fn func(ctx context.Context, args A) (any, error)) *Tool {
	parameters, err := SchemaFromStruct[A]()
	if err != nil {
		return &Tool{name: name, description: description,
			parameters: map[string]any{"type": "object"},
			handler: func(ctx context.Context, arguments map[string]any) (any, error) {
				return nil, err
			}}
	}
	return &Tool{
		name:        name,
		description: description,
		parameters:  parameters,
		handler: func(ctx context.Context, arguments map[string]any) (any, error) {
			decoded, err := decodeArguments[A](arguments)
			if err != nil {
				return nil, err
			}
			return fn(ctx, decoded)
		},
	}
}

// ToolBuilder assembles a Tool dynamically with Property values.
type ToolBuilder struct {
	name        string
	description string
	parameters  map[string]any
	handler     Handler
	err         error
}

// NewTool starts building a tool with the given call name and
// description.
func NewTool(name, description string) *ToolBuilder {
	return &ToolBuilder{name: name, description: description}
}

// Params sets the parameters schema from named properties. Properties
// carrying the IsRequired option are collected into the schema's
// "required" array.
func (b *ToolBuilder) Params(props map[string]Property) *ToolBuilder {
	b.parameters = Params(props)
	return b
}

// RawParams sets the parameters schema from a hand-written JSON-Schema
// object ({"type": "object", "properties": ..., "required": ...}).
func (b *ToolBuilder) RawParams(schema map[string]any) *ToolBuilder {
	b.parameters = schema
	return b
}

// Handler attaches a dynamic handler receiving the raw arguments map.
func (b *ToolBuilder) Handler(fn Handler) *ToolBuilder {
	b.handler = fn
	return b
}

// Build freezes the builder into a Tool, surfacing any construction error
// at the point of use.
func (b *ToolBuilder) Build() *Tool {
	return &Tool{
		name:        b.name,
		description: b.description,
		parameters:  b.parameters,
		handler:     b.handler,
	}
}
