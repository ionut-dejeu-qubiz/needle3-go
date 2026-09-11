package needle

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// This file implements the temporal grounding checks: values the model
// emits for date arguments are only licensed by years that literally
// appear in the conversation or the system facts, and the engine's own
// ungrounded/negation signals are surfaced on the response.

var monthAlternation = `(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`

// yearPatterns extract candidate year tokens. Some of them would need
// look-ahead/look-behind assertions in a general regex engine; Go's RE2
// does not support look-around, so the equivalent semantics are reproduced
// with post-match boundary checks in sourceYears (the assertions only test
// whether the characters around a match are digits or alphanumerics, and a
// shorter backtracked year is always followed by a digit of the longer
// run).
var yearPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{1,2}(?:st|nd|rd|th)?\s+` + monthAlternation + `[\s,]+(\d{1,4})`),
	regexp.MustCompile(`\b` + monthAlternation + `\s+\d{1,2}(?:st|nd|rd|th)?\s*,?\s+(\d{1,4})`),
	regexp.MustCompile(`\b` + monthAlternation + `[\s,]+(\d{3,4})`),
	regexp.MustCompile(`\byear\s+(\d{1,4})`),
	// (\d{1,4})[-/]mm[-/]dd with digit-boundary checks on both sides.
	regexp.MustCompile(`(\d{1,4})[-/]\d{1,2}[-/]\d{1,2}`),
}

var yearSuffix = regexp.MustCompile(`^(\d{4})-`)

func isASCIIAlnum(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// sourceYears extracts the literal years mentioned in a text: dates like
// "5 May 2026", "May 2026", "2026-07-21", explicit "year 1999" phrases,
// and the year part of date-shaped numbers.
func sourceYears(text string) map[int]bool {
	years := map[int]bool{}
	lowered := strings.ToLower(text)
	for i, re := range yearPatterns {
		for _, m := range re.FindAllStringSubmatchIndex(lowered, -1) {
			if len(m) < 4 {
				continue
			}
			start, end := m[0], m[1]
			switch i {
			case 0, 1, 2, 3:
				// Reproduce "(?![0-9A-Za-z])" after the year: the year
				// group ends at the match end for these patterns.
				if end < len(lowered) && isASCIIAlnum(lowered[end]) {
					continue
				}
			case 4:
				// Reproduce "(?<![0-9])" before the match and
				// "(?![0-9])" after the day component.
				if start > 0 && lowered[start-1] >= '0' && lowered[start-1] <= '9' {
					continue
				}
				if end < len(lowered) && lowered[end] >= '0' && lowered[end] <= '9' {
					continue
				}
			}
			var year int
			if _, err := fmt.Sscanf(lowered[m[2]:m[3]], "%d", &year); err == nil {
				years[year] = true
			}
		}
	}
	return years
}

// licensedYears merges the years seen so far in the conversation with the
// years mentioned in the system facts.
func licensedYears(seen map[int]bool, system string) map[int]bool {
	out := make(map[int]bool, len(seen))
	for y := range seen {
		out[y] = true
	}
	if len(out) > 0 && system != "" {
		for y := range sourceYears(system) {
			out[y] = true
		}
	}
	return out
}

// resolveRef follows a JSON-Schema "$ref" pointer inside root, with
// ~0/~1 unescaping and a cycle guard.
func resolveRef(node any, root map[string]any) any {
	seen := map[string]bool{}
	for {
		m, ok := node.(map[string]any)
		if !ok {
			return node
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return node
		}
		if seen[ref] {
			return node
		}
		seen[ref] = true
		target := any(root)
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
			if tm, ok := target.(map[string]any); ok {
				if v, exists := tm[part]; exists {
					target = v
				} else {
					target = map[string]any{}
				}
			} else {
				target = map[string]any{}
			}
		}
		node = target
	}
}

// schemaParametersOf extracts the parameters object of a tool schema,
// accepting a bare parameters object as-is.
func schemaParametersOf(schema any) map[string]any {
	raw, ok := schema.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	if params, ok := raw["parameters"].(map[string]any); ok {
		return params
	}
	return raw
}

// walkGrounding walks emitted arguments against their schema and checks
// every date-typed value against the licensed years. It returns the set
// of checked paths and the set of failing paths.
func walkGrounding(schema, arguments map[string]any, years map[int]bool) (checked, failures map[string]bool) {
	checked, failures = map[string]bool{}, map[string]bool{}
	root := schema
	var walk func(value any, node any, path string)
	walk = func(value any, node any, path string) {
		node = resolveRef(node, root)
		if m, ok := node.(map[string]any); ok {
			variants, _ := m["anyOf"].([]any)
			if variants == nil {
				variants, _ = m["oneOf"].([]any)
			}
			var concrete []any
			for _, v := range variants {
				vm, _ := resolveRef(v, root).(map[string]any)
				if vm == nil || vm["type"] != "null" {
					concrete = append(concrete, v)
				}
			}
			if len(concrete) == 1 {
				node = resolveRef(concrete[0], root)
			}
		}
		m, _ := node.(map[string]any)
		if m == nil {
			m = map[string]any{}
		}
		if format, _ := m["format"].(string); (format == "date" || format == "date-time") && value != nil {
			if s, ok := value.(string); ok {
				if match := yearSuffix.FindStringSubmatch(s); match != nil && len(years) > 0 {
					var year int
					if _, err := fmt.Sscanf(match[1], "%d", &year); err == nil {
						checked[path] = true
						if !years[year] {
							failures[path] = true
						}
					}
				}
			}
			return
		}
		switch typed := value.(type) {
		case map[string]any:
			properties, _ := m["properties"].(map[string]any)
			for key, item := range typed {
				if propSchema, ok := properties[key]; ok {
					child := key
					if path != "" {
						child = path + "." + key
					}
					walk(item, propSchema, child)
				}
			}
		case []any:
			if items, ok := m["items"]; ok {
				for index, item := range typed {
					walk(item, items, fmt.Sprintf("%s[%d]", path, index))
				}
			}
		}
	}
	walk(arguments, root, "")
	return checked, failures
}

// ungroundedPaths groups the engine-reported ungrounded entries by tool.
func ungroundedPaths(r *Response) map[string]map[string]bool {
	grouped := map[string]map[string]bool{}
	if r == nil || r.Validation == nil {
		return grouped
	}
	for _, name := range r.Validation.Ungrounded {
		tool, _, path := splitToolPath(name)
		if path == "" {
			path = tool
		}
		if grouped[tool] == nil {
			grouped[tool] = map[string]bool{}
		}
		grouped[tool][path] = true
	}
	return grouped
}

// annotateUngrounded folds temporal grounding failures into the response's
// validation report, so callers see one unified "ungrounded" list.
func annotateUngrounded(r *Response, toolSchemas []map[string]any, seenYears map[int]bool, system string) {
	if r == nil || len(r.FunctionCalls) == 0 {
		return
	}
	years := licensedYears(seenYears, system)
	if len(years) == 0 {
		return
	}
	schemas := map[string]map[string]any{}
	for _, entry := range toolSchemas {
		if name, ok := entry["name"].(string); ok {
			schemas[name] = entry
		}
	}
	var found []string
	for _, call := range r.FunctionCalls {
		schema := schemas[call.Name]
		if schema == nil {
			continue
		}
		_, failures := walkGrounding(schemaParametersOf(schema), call.Arguments, years)
		var paths []string
		for p := range failures {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			found = append(found, call.Name+"."+p)
		}
	}
	if len(found) == 0 {
		return
	}
	if r.Validation == nil {
		r.Validation = &Validation{}
	}
	existing := map[string]bool{}
	for _, name := range r.Validation.Ungrounded {
		existing[name] = true
	}
	for _, name := range found {
		if !existing[name] {
			r.Validation.Ungrounded = append(r.Validation.Ungrounded, name)
			existing[name] = true
		}
	}
}

// validateExtraction enforces strict extraction: temporal values whose
// year is not licensed, engine-reported ungrounded paths, and negated
// requests all become ExtractionValidationError.
func validateExtraction(text string, schema, arguments map[string]any, r *Response, system string) error {
	years := licensedYears(sourceYears(text), system)
	params := schemaParametersOf(schema)
	checked, failures := walkGrounding(params, arguments, years)
	if r != nil && r.Validation != nil {
		for _, name := range r.Validation.Ungrounded {
			path := name
			if idx := strings.Index(name, "."); idx >= 0 {
				path = name[idx+1:]
			}
			if !checked[path] || failures[path] {
				failures[path] = true
			}
		}
		if r.Validation.Negation {
			failures["negated request"] = true
		}
	}
	if len(failures) == 0 {
		return nil
	}
	var paths []string
	for p := range failures {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return fmt.Errorf("%w: %s", ExtractionValidationError, strings.Join(paths, ", "))
}
