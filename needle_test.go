package needle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlameInTheDark/needle-go/fetch"
)

// Integration tests run against the real native engine. They resolve the
// engine through the normal cache/env lookup and download it on first use
// when the network allows; otherwise they skip.

type weatherArgs struct {
	City string `needle:"city" desc:"city name" required:"true"`
}

func TestGenerationSelection(t *testing.T) {
	agent, err := New(WithGeneration(3))
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if agent.Generation() != 3 {
		t.Fatalf("generation = %d, want 3", agent.Generation())
	}
}

func TestGenerationSelectionRejectsUnsupported(t *testing.T) {
	if _, err := New(WithGeneration(4)); err == nil {
		t.Fatal("expected unsupported generation error")
	}
}

func engineAvailable(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := fetch.LibraryPath(ctx, 2); err != nil {
		t.Skipf("engine not available: %v", err)
	}
}

func TestIntegrationComplete(t *testing.T) {
	engineAvailable(t)
	agent, err := New(
		WithTools(ToolFunc("get_weather", "Get the current weather for a city.",
			func(ctx context.Context, a weatherArgs) (any, error) {
				return map[string]any{"city": a.City, "temp_c": 27}, nil
			})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := agent.Complete(ctx, "what's the weather in Paris?")
	if err != nil {
		t.Fatal(err)
	}
	call := resp.FirstCall()
	if call == nil {
		t.Fatalf("expected a get_weather call, got %s", resp.String())
	}
	if call.Name != "get_weather" {
		t.Fatalf("call name = %q", call.Name)
	}
	if call.Arguments["city"] != "Paris" {
		t.Fatalf("city = %v", call.Arguments["city"])
	}
	if resp.Confidence == nil || *resp.Confidence < 0.5 {
		t.Fatalf("confidence = %v", resp.Confidence)
	}
}

func TestIntegrationRunLoop(t *testing.T) {
	engineAvailable(t)
	agent, err := New(
		WithTools(ToolFunc("get_weather", "Get the current weather for a city.",
			func(ctx context.Context, a weatherArgs) (any, error) {
				return map[string]any{"city": a.City, "temp_c": 27, "sky": "clear"}, nil
			})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resp, err := agent.Run(ctx, "what's the weather in Lagos?")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) == 0 {
		t.Fatalf("run should execute the tool, got %s", resp.String())
	}
	first, ok := resp.Results[0].(map[string]any)
	if !ok {
		t.Fatalf("result[0] = %#v", resp.Results[0])
	}
	if first["city"] != "Lagos" {
		t.Fatalf("city = %v", first["city"])
	}
}

func TestIntegrationOffTopicRefusal(t *testing.T) {
	engineAvailable(t)
	agent, err := New(
		WithTools(ToolFunc("get_weather", "Get the current weather for a city.",
			func(ctx context.Context, a weatherArgs) (any, error) {
				return map[string]any{"temp_c": 20}, nil
			})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := agent.Complete(ctx, "write me a poem about the sea")
	if err != nil {
		t.Fatal(err)
	}
	if resp.HasCalls() {
		t.Fatalf("off-topic input must refuse with an empty call list, got %s", resp.String())
	}
}

func TestIntegrationReset(t *testing.T) {
	engineAvailable(t)
	agent, err := New(
		WithTools(ToolFunc("set_thermostat", "Set the thermostat temperature.",
			func(ctx context.Context, a struct {
				Temperature int `needle:"temperature" required:"true"`
			}) (any, error) {
				return map[string]any{"temperature": a.Temperature}, nil
			})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := agent.Complete(ctx, "set it to 21 degrees"); err != nil {
		t.Fatal(err)
	}
	if err := agent.Reset(); err != nil {
		t.Fatal(err)
	}
	resp, err := agent.Complete(ctx, "set the temperature to 19")
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstCall() == nil {
		t.Fatalf("expected a fresh call after reset, got %s", resp.String())
	}
}

type invoiceArgs struct {
	Vendor  string  `needle:"vendor" required:"true"`
	Total   float64 `needle:"total" required:"true"`
	DueDate string  `needle:"due_date" format:"date"`
}

func TestIntegrationExtractTyped(t *testing.T) {
	engineAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Note: "$1,200.00" is deliberately not used - the engine flags a
	// reformatted number ($ and commas dropped) as ungrounded, and strict
	// extraction rejects it.
	invoice, err := Extract[invoiceArgs](ctx,
		"Invoice from Acme Corp, total 1200.00, due 2026-09-01")
	if err != nil {
		t.Fatal(err)
	}
	if invoice == nil {
		t.Fatal("expected extraction to match")
	}
	if invoice.Vendor != "Acme Corp" {
		t.Errorf("vendor = %q", invoice.Vendor)
	}
	if invoice.Total != 1200 {
		t.Errorf("total = %v", invoice.Total)
	}
	if invoice.DueDate != "2026-09-01" {
		t.Errorf("due_date = %q", invoice.DueDate)
	}
}

func TestIntegrationExtractRawSchema(t *testing.T) {
	engineAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	schema := map[string]any{
		"name":        "receipt",
		"description": "A purchase receipt shared as text",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"merchant": map[string]any{"type": "string"},
				"total":    map[string]any{"type": "number"},
			},
			"required": []any{"merchant", "total"},
		},
	}
	args, err := ExtractRaw(ctx, "GreenMart receipt: oat milk 3.50, total 7.75 paid by visa", schema)
	if err != nil {
		t.Fatal(err)
	}
	if args == nil {
		t.Fatal("expected extraction to match")
	}
	if args["merchant"] != "GreenMart" {
		t.Errorf("merchant = %v", args["merchant"])
	}
	if args["total"] != 7.75 {
		t.Errorf("total = %v (%T)", args["total"], args["total"])
	}
}

func TestIntegrationExtractStrictTemporal(t *testing.T) {
	engineAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Strict mode surfaces engine-reported ungrounded values: a
	// reformatted dollar amount ($ and commas dropped) is not considered
	// evidenced, and ExtractionValidationError names the offending fields.
	_, err := Extract[invoiceArgs](ctx,
		"Invoice from Acme Corp, $1,200.00 total, due 1999-09-01, issued 5 May 2026")
	if err == nil {
		t.Fatal("expected ExtractionValidationError for ungrounded values")
	}
	if !strings.Contains(err.Error(), "total") {
		t.Errorf("error should name the offending field: %v", err)
	}
	// Loose mode returns the values without raising.
	invoice, err := Extract[invoiceArgs](ctx,
		"Invoice from Acme Corp, $1,200.00 total, due 1999-09-01, issued 5 May 2026",
		ExtractStrict(false))
	if err != nil || invoice == nil {
		t.Fatalf("loose extraction should return values, got %v, %v", invoice, err)
	}
}

func TestIntegrationTwoAgents(t *testing.T) {
	engineAvailable(t)
	mk := func(name, desc string) *Tool {
		return ToolFunc(name, desc, func(ctx context.Context, a weatherArgs) (any, error) {
			return map[string]any{"temp_c": 20}, nil
		})
	}
	thermostat, err := New(WithTools(ToolFunc("set_thermostat",
		"Set the thermostat temperature.",
		func(ctx context.Context, a struct {
			Temperature int `needle:"temperature" required:"true"`
		}) (any, error) {
			return map[string]any{"temperature": a.Temperature}, nil
		})))
	if err != nil {
		t.Fatal(err)
	}
	defer thermostat.Close()
	weather, err := New(WithTools(mk("get_weather", "Get the weather for a city.")))
	if err != nil {
		t.Fatal(err)
	}
	defer weather.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	resp, err := thermostat.Complete(ctx, "set it to 21 degrees")
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstCall() == nil || resp.FirstCall().Name != "set_thermostat" {
		t.Fatalf("thermostat agent: %s", resp.String())
	}
	resp, err = weather.Complete(ctx, "what's the weather in Paris?")
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstCall() == nil || resp.FirstCall().Name != "get_weather" {
		t.Fatalf("weather agent: %s", resp.String())
	}
}

// weightsPathForTests locates a .cact archive for the worker tests:
// $NEEDLE_TEST_WEIGHTS, testdata/needle2.cact, ../needle2.cact in order.
func weightsPathForTests(t *testing.T) string {
	t.Helper()
	candidates := []string{
		os.Getenv("NEEDLE_TEST_WEIGHTS"),
		filepath.Join("testdata", "needle2.cact"),
		filepath.Join("..", "needle2.cact"),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Size() > 1<<20 {
			return p
		}
	}
	t.Skip("no .cact weights available for the worker test; set NEEDLE_TEST_WEIGHTS " +
		"or drop testdata/needle2.cact (download: needle download Cactus-Compute/needle2/needle2.cact)")
	return ""
}

func TestIntegrationTunedWorker(t *testing.T) {
	engineAvailable(t)
	weights := weightsPathForTests(t)
	agent, err := New(
		WithWeights(weights),
		WithTools(ToolFunc("get_weather", "Get the current weather for a city.",
			func(ctx context.Context, a weatherArgs) (any, error) {
				return map[string]any{"city": a.City, "temp_c": 22}, nil
			})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if !agent.Tuned() {
		t.Fatal("agent should report tuned weights")
	}
	if agent.Generation() != 2 {
		t.Fatalf("generation = %d", agent.Generation())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resp, err := agent.Run(ctx, "what's the weather in Berlin?")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Confidence != nil {
		t.Errorf("tuned agents report confidence as nil, got %v", *resp.Confidence)
	}
	if len(resp.Results) == 0 {
		t.Fatalf("worker run should execute the tool, got %s", resp.String())
	}
	// The worker process must be gone after Close.
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationSystemFacts(t *testing.T) {
	engineAvailable(t)
	agent, err := New(
		WithSystem("date: 2026-07-21 Tue 14:30; locale: en-US; device: phone"),
		WithTools(ToolFunc("get_weather", "Get the current weather for a city.",
			func(ctx context.Context, a weatherArgs) (any, error) {
				return map[string]any{"temp_c": 20}, nil
			})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := agent.Complete(ctx, "what's the weather in Paris?")
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstCall() == nil {
		t.Fatalf("expected a call, got %s", resp.String())
	}
}

func TestToolsJSONString(t *testing.T) {
	engineAvailable(t)
	toolsJSON := `[{"name":"get_weather","description":"Get the current weather for a city.","parameters":{"type":"object","properties":{"city":{"type":"string","description":"city name"}},"required":["city"]}}]`
	agent, err := New(WithToolsJSON(toolsJSON))
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := agent.Complete(ctx, "what's the weather in Paris?")
	if err != nil {
		t.Fatal(err)
	}
	if resp.FirstCall() == nil {
		t.Fatalf("expected a call, got %s", resp.String())
	}
	// A pending call must be answered before the conversation continues:
	// reset, then Run the same query fresh.
	if err := agent.Reset(); err != nil {
		t.Fatal(err)
	}
	// Raw-schema agents have no handler: Run answers unknown tool.
	runResp, err := agent.Run(ctx, "what's the weather in Paris?")
	if err != nil {
		t.Fatal(err)
	}
	if len(runResp.Results) == 0 {
		t.Fatal("run should feed the unknown-tool error back")
	}
	blob, _ := json.Marshal(runResp.Results[0])
	if !strings.Contains(string(blob), "unknown tool") {
		t.Errorf("expected unknown-tool error result, got %s", blob)
	}
}
