package needle

import "encoding/json"

// Response is the envelope the engine returns for every turn. Needle
// solves every problem as a function call: "type" is "call" when the model
// wants tool calls (an empty FunctionCalls list is the refusal for
// off-topic input) and "respond" when the agentic loop is finished - the
// answer is the tool results, no free text is generated.
type Response struct {
	// Type is "call" while the model still wants tool calls, "respond"
	// when the loop is done, or (in engine error paths) "error" /
	// "refuse" / "text" for a handful of legacy envelopes.
	Type string `json:"type"`
	// Success reports whether the engine completed the turn.
	Success bool `json:"success"`
	// Error carries an engine-level error message, or nil.
	Error *string `json:"error"`
	// ErrorCode carries an engine-level error code, or nil.
	ErrorCode *string `json:"error_code"`
	// Reason carries a short engine-level refusal reason, or nil.
	Reason *string `json:"reason"`
	// FunctionCalls is the list of calls the model wants executed this
	// turn. Grammar-constrained decoding guarantees the arguments match
	// the declared schema. An empty list is the refusal contract for
	// off-topic input: there is no free-text fallback.
	FunctionCalls []FunctionCall `json:"function_calls"`
	// Reasoning is the model's short unconstrained derivation of each
	// argument from its source span, when present.
	Reasoning *string `json:"reasoning"`
	// Confidence is the calibrated score in [0,1] - the minimum of a
	// post-hoc calibration head and the decode probability of the call.
	// nil means uncalibrated (tuned weights, or an engine that did not
	// score the turn).
	Confidence *float64 `json:"confidence"`
	// PrefillTPS is the prompt prefill throughput in tokens per second.
	PrefillTPS *float64 `json:"prefill_tps"`
	// DecodeTPS is the decode throughput in tokens per second.
	DecodeTPS *float64 `json:"decode_tps"`
	// PeakRAMMB is the peak memory footprint of the turn, in megabytes.
	PeakRAMMB *float64 `json:"peak_ram_mb"`
	// Validation reports grounding findings: argument paths whose values
	// the engine does not consider evidenced in the input, and whether
	// the request was negated.
	Validation *Validation `json:"validation"`
	// Results is attached by Agent.Run: the flattened list of values
	// returned by the executed tool handlers across the whole loop. The
	// engine itself never populates it.
	Results []any `json:"results,omitempty"`
}

// FunctionCall is one call the model wants executed.
type FunctionCall struct {
	// Name of the declared tool to call.
	Name string `json:"name"`
	// Arguments holds only values evidenced by the input; optional fields
	// with no evidence are omitted, not guessed. Reads of missing keys
	// therefore return zero values.
	Arguments map[string]any `json:"arguments"`
}

// Validation carries the engine's grounding report for a turn.
type Validation struct {
	// Ungrounded lists "tool.field" names whose value is not grounded in
	// the input, including date arguments whose year matches nothing in
	// the conversation or the system facts.
	Ungrounded []string `json:"ungrounded"`
	// Negation reports a negated request ("don't turn on the lights").
	Negation bool `json:"negation"`
}

// HasCalls reports whether the model wants at least one tool call.
func (r *Response) HasCalls() bool { return r != nil && len(r.FunctionCalls) > 0 }

// FirstCall returns the first requested call, or nil when the model made
// no call (the off-topic refusal contract).
func (r *Response) FirstCall() *FunctionCall {
	if !r.HasCalls() {
		return nil
	}
	return &r.FunctionCalls[0]
}

// Refused reports whether this turn is a refusal: no tool call was made.
// Off-topic, unsupported, ambiguous and negated requests all refuse with
// an empty call list; there is no free-text fallback.
func (r *Response) Refused() bool { return r == nil || len(r.FunctionCalls) == 0 }

// UngroundedOf returns the ungrounded argument paths reported for calls of
// the named tool.
func (r *Response) UngroundedOf(tool string) []string {
	if r == nil || r.Validation == nil {
		return nil
	}
	var paths []string
	for _, name := range r.Validation.Ungrounded {
		owner, _, path := splitToolPath(name)
		if owner == tool && path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// String returns the compact JSON encoding of the response.
func (r *Response) String() string {
	if r == nil {
		return "<nil>"
	}
	b, err := json.Marshal(r)
	if err != nil {
		return "<unrepresentable response>"
	}
	return string(b)
}

func splitToolPath(name string) (tool, _, path string) {
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			return name[:i], ".", name[i+1:]
		}
	}
	return name, "", name
}
