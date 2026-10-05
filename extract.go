package needle

import (
	"context"
	"encoding/json"
	"fmt"
)

// Extract performs a one-shot structured extraction with the schema
// derived from the argument struct's tags. It returns nil (and no error)
// when nothing in the input matched the schema, the extracted fields
// decoded into A otherwise. Strict mode (the default) rejects temporal
// values that contradict a literal year in the input, engine-reported
// fabricated values, and negated requests with ExtractionValidationError.
//
//	type Invoice struct {
//	    Vendor  string  `needle:"vendor" required:"true"`
//	    Total   float64 `needle:"total" required:"true"`
//	    DueDate string  `needle:"due_date" format:"date"`
//	}
//
//	invoice, err := needle.Extract[Invoice](ctx,
//	    "Invoice from Acme Corp, $1,200.00, due 2026-09-01")
func Extract[A any](ctx context.Context, text string, opts ...ExtractOption) (*A, error) {
	parameters, err := SchemaFromStruct[A]()
	if err != nil {
		return nil, err
	}
	cfg := newExtractConfig(opts)
	return extractAs[A](ctx, text, parameters, cfg)
}

// ExtractAs performs a one-shot extraction against an explicit schema and
// decodes the result into A. The schema may be a *Tool, a raw schema map,
// or a JSON string.
func ExtractAs[A any](ctx context.Context, text string, schema any, opts ...ExtractOption) (*A, error) {
	params, err := extractionParameters(schema)
	if err != nil {
		return nil, err
	}
	cfg := newExtractConfig(opts)
	return extractAs[A](ctx, text, params, cfg)
}

// ExtractRaw performs a one-shot extraction against an explicit schema
// and returns the raw argument map.
func ExtractRaw(ctx context.Context, text string, schema any, opts ...ExtractOption) (map[string]any, error) {
	params, err := extractionParameters(schema)
	if err != nil {
		return nil, err
	}
	cfg := newExtractConfig(opts)
	response, arguments, err := runExtraction(ctx, text, params, cfg)
	if err != nil {
		return nil, err
	}
	if arguments == nil {
		return nil, nil
	}
	if *cfg.strict {
		if err := validateExtraction(text, params, arguments, response, cfg.system); err != nil {
			return nil, err
		}
	}
	return arguments, nil
}

// ExtractRaw on an agent reuses the agent's weights and engine settings
// for a one-shot extraction (a fresh engine conversation). Use the
// package-level Extract or ExtractAs for typed results, passing
// ExtractWeights to inherit a tuned archive.
func (a *Agent) ExtractRaw(ctx context.Context, text string, schema any, opts ...ExtractOption) (map[string]any, error) {
	cfg := newExtractConfig(opts)
	if cfg.generation == 0 {
		cfg.generation = a.generation
	}
	if cfg.weightsPath == "" && a.tuned {
		cfg.weightsPath = a.weightsPath
	}
	if cfg.enginePath == "" && a.enginePath != "" {
		cfg.enginePath = a.enginePath
	}
	parameters, err := extractionParameters(schema)
	if err != nil {
		return nil, err
	}
	response, arguments, err := runExtraction(ctx, text, parameters, cfg)
	if err != nil {
		return nil, err
	}
	if arguments == nil {
		return nil, nil
	}
	if *cfg.strict {
		if err := validateExtraction(text, parameters, arguments, response, cfg.system); err != nil {
			return nil, err
		}
	}
	return arguments, nil
}

func newExtractConfig(opts []ExtractOption) extractConfig {
	cfg := extractConfig{maxNewTokens: 256}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.strict == nil {
		strict := true
		cfg.strict = &strict
	}
	return cfg
}

// extractionParameters normalizes the accepted schema forms into a
// parameters object.
func extractionParameters(schema any) (map[string]any, error) {
	switch v := schema.(type) {
	case nil:
		return nil, fmt.Errorf("needle: extraction needs a schema")
	case *Tool:
		return v.Schema()["parameters"].(map[string]any), nil
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil, fmt.Errorf("needle: schema is not valid JSON: %w", err)
		}
		return extractionParameters(parsed)
	case map[string]any:
		return schemaParametersOf(v), nil
	}
	return nil, fmt.Errorf("needle: unsupported extraction schema of type %T", schema)
}

func extractAs[A any](ctx context.Context, text string, params map[string]any, cfg extractConfig) (*A, error) {
	response, arguments, err := runExtraction(ctx, text, params, cfg)
	if err != nil {
		return nil, err
	}
	if arguments == nil {
		return nil, nil
	}
	if *cfg.strict {
		if err := validateExtraction(text, params, arguments, response, cfg.system); err != nil {
			return nil, err
		}
	}
	out, err := decodeArguments[A](arguments)
	if err != nil {
		return nil, fmt.Errorf("needle: %w", err)
	}
	return &out, nil
}

// runExtraction builds a one-shot agent with a single tool and completes
// once. Extraction is not a separate mode: with one declared tool the
// grammar admits exactly one call of that name, so schema conformance is
// guaranteed rather than requested.
func runExtraction(ctx context.Context, text string, params map[string]any, cfg extractConfig) (*Response, map[string]any, error) {
	schemas := []map[string]any{{
		"name":        "extract",
		"description": "Extract structured data from the input.",
		"parameters":  params,
	}}
	toolsJSON, err := json.Marshal(schemas)
	if err != nil {
		return nil, nil, err
	}
	opts := []Option{
		WithToolsJSON(string(toolsJSON)),
		WithSystem(cfg.system),
	}
	if cfg.weightsPath != "" {
		opts = append(opts, WithWeights(cfg.weightsPath))
	} else if cfg.generation != 0 {
		opts = append(opts, WithGeneration(cfg.generation))
	}
	if cfg.enginePath != "" {
		opts = append(opts, WithEnginePath(cfg.enginePath))
	}
	agent, err := New(opts...)
	if err != nil {
		return nil, nil, err
	}
	defer agent.Close()
	a := agent
	response, err := a.Complete(ctx, text, MaxTokens(cfg.maxNewTokens))
	if err != nil {
		return nil, nil, err
	}
	if len(response.FunctionCalls) == 0 {
		return response, nil, nil
	}
	arguments := response.FunctionCalls[0].Arguments
	if arguments == nil {
		arguments = map[string]any{}
	}
	return response, arguments, nil
}
