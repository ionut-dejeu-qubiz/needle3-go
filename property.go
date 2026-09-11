package needle

// Property is one JSON-Schema property descriptor. The constructors below
// (Str, Int, Num, Bool, Enum, ArrayOf, ObjectOf) build properties, and the
// option functions (Desc, Required, Min, ...) attach the constraints that
// are compiled into the engine's decode grammar, so the model can only
// emit values that satisfy them.
type Property map[string]any

// PropOption mutates a property while it is being built.
type PropOption func(Property)

// Str builds a string property.
func Str(opts ...PropOption) Property { return build("string", opts) }

// Int builds an integer property.
func Int(opts ...PropOption) Property { return build("integer", opts) }

// Num builds a floating-point (JSON number) property.
func Num(opts ...PropOption) Property { return build("number", opts) }

// Bool builds a boolean property.
func Bool(opts ...PropOption) Property { return build("boolean", opts) }

// Any builds an unconstrained property.
func Any(opts ...PropOption) Property { return build("", opts) }

// Enum builds a property restricted to a fixed set of values. The JSON
// type is inferred from the first value: the model cannot emit anything
// outside the set.
func Enum(values ...any) Property {
	p := Property{}
	if len(values) > 0 {
		p["type"] = jsonTypeName(values[0])
	}
	list := make([]any, len(values))
	copy(list, values)
	p["enum"] = list
	return p
}

// ArrayOf builds an array property with the given item schema.
func ArrayOf(items Property, opts ...PropOption) Property {
	p := build("array", opts)
	p["items"] = propToMap(items)
	return p
}

// ObjectOf builds an object property with the given nested properties.
func ObjectOf(props map[string]Property, opts ...PropOption) Property {
	p := build("object", opts)
	out := map[string]any{}
	for k, v := range props {
		out[k] = propToMap(v)
	}
	p["properties"] = out
	return p
}

func build(jsonType string, opts []PropOption) Property {
	p := Property{}
	if jsonType != "" {
		p["type"] = jsonType
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// propToMap converts a Property (and every nested Property value) into
// plain map[string]any values, so downstream type assertions and JSON
// marshaling behave like hand-written schema maps.
func propToMap(p Property) map[string]any {
	out := make(map[string]any, len(p))
	for k, v := range p {
		if sub, ok := v.(Property); ok {
			out[k] = propToMap(sub)
			continue
		}
		out[k] = v
	}
	return out
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "number"
	}
	return "string"
}

// Desc attaches a human-readable description the model reads when filling
// the argument.
func Desc(text string) PropOption {
	return func(p Property) { p["description"] = text }
}

// Description is an alias of Desc matching the JSON-Schema key name.
func Description(text string) PropOption { return Desc(text) }

// IsRequired marks a property as required; Params collects required
// property names automatically.
var IsRequired PropOption = func(p Property) { p["__required"] = true }

// Min sets the inclusive minimum (JSON-Schema "minimum").
func Min(v float64) PropOption { return func(p Property) { p["minimum"] = v } }

// Max sets the inclusive maximum (JSON-Schema "maximum").
func Max(v float64) PropOption { return func(p Property) { p["maximum"] = v } }

// GT sets the exclusive minimum (JSON-Schema "exclusiveMinimum").
func GT(v float64) PropOption { return func(p Property) { p["exclusiveMinimum"] = v } }

// LT sets the exclusive maximum (JSON-Schema "exclusiveMaximum").
func LT(v float64) PropOption { return func(p Property) { p["exclusiveMaximum"] = v } }

// Range sets an inclusive minimum and maximum in one call.
func Range(lo, hi float64) PropOption {
	return func(p Property) {
		p["minimum"] = lo
		p["maximum"] = hi
	}
}

// MultipleOf restricts numeric values to multiples of v.
func MultipleOf(v float64) PropOption {
	return func(p Property) { p["multipleOf"] = v }
}

// MinLen sets the minimum string length.
func MinLen(n int) PropOption { return func(p Property) { p["minLength"] = n } }

// MaxLen sets the maximum string length.
func MaxLen(n int) PropOption { return func(p Property) { p["maxLength"] = n } }

// Pattern constrains strings to a regular expression.
func Pattern(re string) PropOption { return func(p Property) { p["pattern"] = re } }

// Format attaches a JSON-Schema format such as "date" or "date-time".
func Format(f string) PropOption { return func(p Property) { p["format"] = f } }

// MinItems sets the minimum array length.
func MinItems(n int) PropOption { return func(p Property) { p["minItems"] = n } }

// MaxItems sets the maximum array length.
func MaxItems(n int) PropOption { return func(p Property) { p["maxItems"] = n } }

// Unique requires array items to be unique.
func Unique() PropOption { return func(p Property) { p["uniqueItems"] = true } }

// Const restricts the property to a single fixed value.
func Const(v any) PropOption { return func(p Property) { p["const"] = v } }

// Default records a default value and makes the property optional.
func Default(v any) PropOption {
	return func(p Property) {
		p["default"] = v
		delete(p, "__required")
	}
}

// EnumValues restricts the property to a fixed set of values.
func EnumValues(values ...any) PropOption {
	return func(p Property) {
		list := make([]any, len(values))
		copy(list, values)
		p["enum"] = list
	}
}

// Params assembles an object "parameters" schema from named properties.
// Property names carrying the IsRequired option are collected into the
// schema's "required" array, so the single source of truth stays with the
// property.
func Params(props map[string]Property) map[string]any {
	out := map[string]any{"type": "object"}
	properties := map[string]any{}
	var requiredList []string
	for name, prop := range props {
		clean := propToMap(prop)
		if flag, ok := clean["__required"].(bool); ok && flag {
			requiredList = append(requiredList, name)
		}
		delete(clean, "__required")
		properties[name] = clean
	}
	out["properties"] = properties
	if len(requiredList) > 0 {
		sortStrings(requiredList)
		out["required"] = requiredList
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
