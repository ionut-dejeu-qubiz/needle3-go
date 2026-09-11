package needle

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// The schema names the engine emits come from the needle tag (or its
// snake_case fallback), but encoding/json matches either the json tag or
// the Go field name. A schema name like "due_date" would therefore never
// reach a field named DueDate without an explicit json tag. The helpers
// below re-key engine arguments onto the names encoding/json actually
// matches, recursing through nested structs, pointers and slices, so a
// plain needle-tagged struct decodes completely without json tags.

// DecodeArguments converts engine arguments into a typed struct, using
// the same name mapping as ToolFunc handlers:
//
//	var args LightsArgs
//	err := needle.DecodeArguments(r.FunctionCalls[0].Arguments, &args)
func DecodeArguments[A any](arguments map[string]any) (A, error) {
	return decodeArguments[A](arguments)
}

func decodeArguments[A any](arguments map[string]any) (A, error) {
	var out A
	if arguments == nil {
		return out, nil
	}
	t := reflect.TypeOf(out)
	if t != nil && t.Kind() == reflect.Struct {
		arguments = rekeyForStruct(arguments, t)
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		return out, fmt.Errorf("cannot re-encode tool arguments: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("tool arguments do not match the declared schema: %w", err)
	}
	return out, nil
}

// jsonNameOf returns the name encoding/json matches for a field: the json
// tag name when present, else the field name itself.
func jsonNameOf(field reflect.StructField) string {
	if tag := field.Tag.Get("json"); tag != "" {
		name := strings.TrimSpace(strings.Split(tag, ",")[0])
		if name != "" && name != "-" {
			return name
		}
	}
	return field.Name
}

// rekeyForStruct rewrites argument keys onto the json-visible names of t.
// Keys that match no field pass through unchanged.
func rekeyForStruct(args map[string]any, t reflect.Type) map[string]any {
	out := make(map[string]any, len(args))
	renamed := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		needleTag := field.Tag.Get("needle")
		if needleTag == "-" {
			continue
		}
		schemaName := propertyName(field, needleTag)
		jsonName := jsonNameOf(field)
		if schemaName == "" {
			// Embedded struct: flatten its fields into the same level.
			st := field.Type
			for st.Kind() == reflect.Pointer {
				st = st.Elem()
			}
			if st.Kind() == reflect.Struct && st != timeType {
				for k, v := range rekeyForStruct(args, st) {
					out[k] = v
				}
				for k := range fieldsOf(st) {
					renamed[k] = true
				}
			}
			continue
		}
		if value, ok := args[schemaName]; ok {
			out[jsonName] = rekeyValue(value, field.Type)
			renamed[schemaName] = true
		}
	}
	for key, value := range args {
		if !renamed[key] {
			out[key] = value
		}
	}
	return out
}

// fieldsOf returns the schema-level names of a struct's fields.
func fieldsOf(t reflect.Type) map[string]bool {
	names := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		if name := propertyName(field, field.Tag.Get("needle")); name != "" {
			names[name] = true
		}
	}
	return names
}

// rekeyValue recurses into nested values that decode into structs.
func rekeyValue(value any, t reflect.Type) any {
	if t == timeType {
		return value
	}
	switch t.Kind() {
	case reflect.Pointer:
		if t.Elem() == timeType {
			return value
		}
		return rekeyValue(value, t.Elem())
	case reflect.Slice, reflect.Array:
		if t.Elem() == timeType {
			return value
		}
		items, ok := value.([]any)
		if !ok {
			return value
		}
		elem := t.Elem()
		rewritten := make([]any, len(items))
		for i, item := range items {
			rewritten[i] = rekeyValue(item, elem)
		}
		return rewritten
	case reflect.Struct:
		if m, ok := value.(map[string]any); ok {
			return rekeyForStruct(m, t)
		}
	}
	return value
}
