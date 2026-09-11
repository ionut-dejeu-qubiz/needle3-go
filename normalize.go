package needle

import (
	"encoding/json"
	"fmt"
	"strings"
)

// normalizeTools converts the accepted tool declarations into engine
// schemas. Accepted forms:
//
//   - *Tool (from ToolFunc, NewTool(...).Build(), ...)
//   - map[string]any: a full schema {"name": ..., "parameters": {...}} or
//     a bare parameters object (wrapped with a generic name)
//   - string or json.RawMessage: a JSON array of schemas, a single schema
//     object, or a bare parameters object
//   - a Go slice of any of the above
//
// Tools declared without an executable handler still enter the grammar;
// Agent.Run answers their calls with an "unknown tool" error result.
func normalizeTools(src any) ([]map[string]any, map[string]Handler, error) {
	switch v := src.(type) {
	case nil:
		return nil, nil, nil
	case *Tool:
		return []map[string]any{v.Schema()}, map[string]Handler{v.name: v.handler}, nil
	case string:
		return normalizeTools(json.RawMessage(v))
	case json.RawMessage:
		trimmed := strings.TrimSpace(string(v))
		if trimmed == "" {
			return nil, nil, nil
		}
		if trimmed[0] == '[' {
			var raws []json.RawMessage
			if err := json.Unmarshal(v, &raws); err != nil {
				return nil, nil, fmt.Errorf("needle: tools JSON is not a valid array: %w", err)
			}
			return normalizeTools(raws)
		}
		var obj map[string]any
		if err := json.Unmarshal(v, &obj); err != nil {
			return nil, nil, fmt.Errorf("needle: tools JSON is not a valid object: %w", err)
		}
		return normalizeTools(obj)
	case []json.RawMessage:
		var schemas []map[string]any
		for _, raw := range v {
			parsed, _, err := normalizeTools(raw)
			if err != nil {
				return nil, nil, err
			}
			schemas = append(schemas, parsed...)
		}
		return schemas, nil, nil
	case map[string]any:
		if _, ok := v["name"]; ok {
			return []map[string]any{ensureParameters(v)}, nil, nil
		}
		// A bare parameters object: give it a generic extraction name.
		wrapped := map[string]any{
			"name":        "extract",
			"description": "Extract structured data from the input.",
			"parameters":  v,
		}
		return []map[string]any{wrapped}, nil, nil
	case []any:
		var schemas []map[string]any
		handlers := map[string]Handler{}
		for _, item := range v {
			parsed, hs, err := normalizeTools(item)
			if err != nil {
				return nil, nil, err
			}
			schemas = append(schemas, parsed...)
			for k, h := range hs {
				handlers[k] = h
			}
		}
		return schemas, handlers, nil
	case []*Tool:
		var schemas []map[string]any
		handlers := map[string]Handler{}
		for _, tool := range v {
			if tool == nil {
				continue
			}
			schemas = append(schemas, tool.Schema())
			handlers[tool.name] = tool.handler
		}
		return schemas, handlers, nil
	}
	return nil, nil, fmt.Errorf("needle: unsupported tool declaration of type %T; "+
		"pass *needle.Tool, a JSON string, or a schema map", src)
}

// ensureParameters makes sure a schema map carries a parameters object.
func ensureParameters(schema map[string]any) map[string]any {
	if _, ok := schema["parameters"]; !ok {
		if props, ok := schema["properties"].(map[string]any); ok {
			params := map[string]any{"type": "object", "properties": props}
			if req, ok := schema["required"]; ok {
				params["required"] = req
			}
			schema["parameters"] = params
			delete(schema, "properties")
			delete(schema, "required")
			return schema
		}
		schema["parameters"] = map[string]any{"type": "object"}
	}
	return schema
}
