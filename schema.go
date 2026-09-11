package needle

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// SchemaFromStruct derives a JSON-Schema "parameters" object from the
// fields of a Go struct, using struct tags:
//
//	type SetLightsArgs struct {
//	    Room   string `needle:"room" desc:"which room to control" required:"true"`
//	    Action string `needle:"action" desc:"on, off, or dim" enum:"on|off|dim"`
//	    Bright *int   `needle:"brightness" desc:"0 to 100" min:"0" max:"100"`
//	}
//
// The property name is the needle tag if present, else the json tag name,
// else the snake_case field name. A field is required unless it is a
// pointer, carries a default, is tagged optional, or its json tag has
// omitempty; tag required:"true" forces it back on. Constraint tags map
// one-to-one onto the JSON-Schema keys the engine compiles into its decode
// grammar: desc/description, enum (values separated by "|"), const, min
// (minimum), max (maximum), gt (exclusiveMinimum), lt (exclusiveMaximum),
// multiple-of, min-length, max-length, pattern, format, min-items,
// max-items, unique, default.
//
// Go types map to JSON types: string, bool, all integer kinds, both float
// kinds, slices and arrays (array), maps and structs (object, recursing
// into nested fields), pointers (unwrapped, making the field optional),
// and time.Time (string with format date-time). Interfaces map to an
// unconstrained property.
func SchemaFromStruct[A any]() (map[string]any, error) {
	var zero A
	t := reflect.TypeOf(zero)
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("needle: schema source must be a struct, got %v", t)
	}
	return schemaForStruct(t)
}

func schemaForStruct(t reflect.Type) (map[string]any, error) {
	params := map[string]any{"type": "object"}
	props := map[string]any{}
	var required []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("needle")
		if tag == "-" {
			continue
		}
		if field.Tag.Get("json") == "-" {
			continue
		}
		name := propertyName(field, tag)
		if name == "" {
			// Anonymous struct with no explicit name: flatten its fields
			// into the parent schema (Go embedding semantics).
			st := field.Type
			for st.Kind() == reflect.Pointer {
				st = st.Elem()
			}
			if st.Kind() != reflect.Struct {
				continue
			}
			inner, err := schemaForStruct(st)
			if err != nil {
				return nil, err
			}
			if innerProps, ok := inner["properties"].(map[string]any); ok {
				for k, v := range innerProps {
					props[k] = v
				}
			}
			if req, ok := inner["required"].([]string); ok {
				required = append(required, req...)
			}
			continue
		}
		schema, isOptional, err := schemaForField(field)
		if err != nil {
			return nil, err
		}
		props[name] = schema
		needRequired := !isOptional
		if v, ok := field.Tag.Lookup("required"); ok {
			needRequired = truthy(v)
		}
		if needRequired {
			required = append(required, name)
		}
	}
	params["properties"] = props
	if len(required) > 0 {
		params["required"] = required
	}
	return params, nil
}

func propertyName(field reflect.StructField, needleTag string) string {
	if needleTag != "" {
		name := strings.TrimSpace(strings.Split(needleTag, ",")[0])
		if name != "" {
			return name
		}
	}
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		name := strings.TrimSpace(strings.Split(jsonTag, ",")[0])
		if name != "" && name != "-" {
			return name
		}
	}
	if field.Anonymous {
		// Embedded structs contribute their fields directly, not a
		// property of their own; schemaForField handles the merge.
		return ""
	}
	return snakeCase(field.Name)
}

// schemaForField returns the property schema of one field and whether the
// field is optional by shape (pointer or default).
func schemaForField(field reflect.StructField) (schema map[string]any, optional bool, err error) {
	ft := field.Type
	optional = ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Interface
	schema, err = schemaForType(ft)
	if err != nil {
		return nil, false, err
	}
	schema, opt2, err := applyFieldTags(schema, field, ft)
	if err != nil {
		return nil, false, err
	}
	return schema, optional || opt2, nil
}

var timeType = reflect.TypeOf(time.Time{})

// schemaForType maps a Go type to a JSON-Schema fragment.
func schemaForType(t reflect.Type) (map[string]any, error) {
	if t == timeType {
		return map[string]any{"type": "string", "format": "date-time"}, nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return schemaForType(t.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.Slice, reflect.Array:
		items, err := schemaForType(t.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	case reflect.Map:
		return map[string]any{"type": "object"}, nil
	case reflect.Struct:
		inner, err := schemaForStruct(t)
		if err != nil {
			return nil, err
		}
		return inner, nil
	case reflect.Interface:
		return map[string]any{}, nil
	}
	return map[string]any{"type": "string"}, nil
}

// applyFieldTags folds the constraint tags of a field into its schema.
func applyFieldTags(schema map[string]any, field reflect.StructField, ft reflect.Type) (out map[string]any, optional bool, err error) {
	out = schema
	get := func(key string) (string, bool) {
		v, ok := field.Tag.Lookup(key)
		return strings.TrimSpace(v), ok
	}
	if v, ok := get("desc"); ok && v != "" {
		out["description"] = v
	}
	if v, ok := get("description"); ok && v != "" {
		out["description"] = v
	}
	if v, ok := get("optional"); ok && truthy(v) {
		optional = true
	}
	if jsonTag := field.Tag.Get("json"); strings.Contains(jsonTag, ",omitempty") {
		optional = true
	}
	if v, ok := get("enum"); ok && v != "" {
		values, err := parseEnumValues(v, ft)
		if err != nil {
			return nil, false, err
		}
		out["enum"] = values
	}
	if v, ok := get("const"); ok && v != "" {
		val, err := parseScalarValue(v, ft)
		if err != nil {
			return nil, false, err
		}
		out["const"] = val
	}
	if v, ok := get("default"); ok && v != "" {
		val, err := parseScalarValue(v, ft)
		if err != nil {
			return nil, false, err
		}
		out["default"] = val
		optional = true
	}
	numeric := func(key string, schemaKey string) error {
		if v, ok := get(key); ok && v != "" {
			f, err := parseFloatTag(v)
			if err != nil {
				return fmt.Errorf("needle: field %s has invalid %s value %q: %w", field.Name, key, v, err)
			}
			out[schemaKey] = f
		}
		return nil
	}
	for key, schemaKey := range map[string]string{
		"min":           "minimum",
		"minimum":       "minimum",
		"max":           "maximum",
		"maximum":       "maximum",
		"gt":            "exclusiveMinimum",
		"exclusive-min": "exclusiveMinimum",
		"lt":            "exclusiveMaximum",
		"exclusive-max": "exclusiveMaximum",
		"multiple-of":   "multipleOf",
	} {
		if err := numeric(key, schemaKey); err != nil {
			return nil, false, err
		}
	}
	intTags := map[string]string{
		"min-length": "minLength",
		"max-length": "maxLength",
		"min-items":  "minItems",
		"max-items":  "maxItems",
	}
	for key, schemaKey := range intTags {
		if v, ok := get(key); ok && v != "" {
			n, err := parseFloatTag(v)
			if err != nil {
				return nil, false, fmt.Errorf("needle: field %s has invalid %s value %q: %w", field.Name, key, v, err)
			}
			out[schemaKey] = int(n)
		}
	}
	if v, ok := get("pattern"); ok && v != "" {
		out["pattern"] = v
	}
	if v, ok := get("format"); ok && v != "" {
		out["format"] = v
	}
	if v, ok := get("unique"); ok && truthy(v) {
		out["uniqueItems"] = true
	}
	return out, optional, nil
}

// parseEnumValues splits a "|"-separated enum tag and types the values
// according to the field's Go type.
func parseEnumValues(v string, ft reflect.Type) ([]any, error) {
	base := ft
	for base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	parts := strings.Split(v, "|")
	values := make([]any, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		val, err := parseScalarValue(part, base)
		if err != nil {
			return nil, err
		}
		values = append(values, val)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("needle: enum tag %q holds no values", v)
	}
	return values, nil
}

// parseScalarValue parses one tag literal into a JSON value typed like
// the given Go type.
func parseScalarValue(v string, ft reflect.Type) (any, error) {
	base := ft
	for base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	switch base.Kind() {
	case reflect.String:
		return v, nil
	case reflect.Bool:
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no":
			return false, nil
		}
		return nil, fmt.Errorf("cannot parse %q as bool", v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as integer", v)
		}
		return n, nil
	case reflect.Float32, reflect.Float64:
		f, err := parseFloatTag(v)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as number", v)
		}
		return f, nil
	}
	// Fall back to JSON literals (objects, arrays, null).
	var out any
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return v, nil
	}
	return out, nil
}

func parseFloatTag(v string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(v), 64)
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "":
		return true
	}
	return false
}

// snakeCase converts a Go field name into its conventional JSON name:
// BrightnessPercent -> brightness_percent, URL -> url, URLPath -> url_path.
func snakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			prevUpperNextLower := i > 0 && i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && unicode.IsLower(runes[i+1])
			if prevLower || prevUpperNextLower {
				b.WriteRune('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
