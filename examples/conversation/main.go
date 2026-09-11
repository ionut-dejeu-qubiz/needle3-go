// Command conversation drives the agent by hand with Complete instead of
// Run: it shows the response contract, feeding results back, follow-up
// queries in the same conversation, confidence gating, and a topic switch
// mid-conversation.
//
//	go run ./examples/conversation
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/FlameInTheDark/needle-go"
)

type GetWeatherArgs struct {
	City string `needle:"city" desc:"city name" required:"true"`
}

type SetLightsArgs struct {
	Room       string `needle:"room" desc:"which room to control" required:"true"`
	On         bool   `needle:"on" desc:"whether the lights end up on" required:"true"`
	Brightness *int   `needle:"brightness" desc:"0 to 100" min:"0" max:"100"`
}

func main() {
	getWeather := needle.ToolFunc("get_weather",
		"Get the current weather for a city.",
		func(ctx context.Context, args GetWeatherArgs) (any, error) {
			// A real program calls a weather API here.
			conditions := map[string]any{"Paris": 21, "Lagos": 29}
			temp, ok := conditions[args.City].(int)
			if !ok {
				temp = 25
			}
			return map[string]any{"city": args.City, "temp_c": temp}, nil
		})
	setLights := needle.ToolFunc("set_lights",
		"Turn a room's lights on or off and set brightness.",
		func(ctx context.Context, args SetLightsArgs) (any, error) {
			return map[string]any{"ok": true, "room": args.Room, "on": args.On,
				"brightness": args.Brightness}, nil
		})
	// A dispatch table is all Run does for you; driving it by hand gives
	// you the raw response contract instead.
	tools := map[string]func(context.Context, map[string]any) (any, error){
		"get_weather": getWeather.Invoke,
		"set_lights":  setLights.Invoke,
	}

	// System facts carry environment state: relative language ("and what
	// about...") resolves against the declared date and locale.
	agent, err := needle.New(
		needle.WithTools(getWeather, setLights),
		needle.WithSystem("date: 2026-07-21 Tue 14:30; locale: en-US; device: phone"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// An agent owns one conversation: repeated Complete calls continue
	// it; later turns see the earlier ones.
	for _, turn := range []struct {
		text string
	}{
		{"what's the weather in Paris?"},
		{"and what about Lagos?"},         // follow-up, same conversation
		{"now dim the living room to 30"}, // topic switch
		{"write me a poem about the sea"}, // off-topic: refused, no free text
	} {
		resp, err := agent.Complete(ctx, turn.text)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("user       %s\n", turn.text)
		printTurn(resp)

		// Confidence gating: pick a threshold for your product, act at
		// or above it, re-ask or escalate below it.
		if resp.Confidence != nil && *resp.Confidence < 0.5 {
			fmt.Println("  note      confidence below 0.5 - a product would escalate here")
		}

		// When the model wants calls, execute them and feed the results
		// back as the next turn. A "respond" turn with no calls says the
		// loop is done for this query - the answer is the tool results.
		for resp.Type == "call" && resp.HasCalls() {
			var results []any
			for _, call := range resp.FunctionCalls {
				fmt.Printf("  call      %s %s\n", call.Name, mustJSON(call.Arguments))
				var result any = map[string]any{"error": "unknown tool: " + call.Name}
				if invoke := tools[call.Name]; invoke != nil {
					value, err := invoke(ctx, call.Arguments)
					if err != nil {
						result = map[string]any{"error": err.Error()}
					} else {
						result = value
					}
				}
				results = append(results, result)
				fmt.Printf("  result    %s\n", mustJSON(result))
			}
			feedback, _ := json.Marshal(results)
			resp, err = agent.Complete(ctx, string(feedback))
			if err != nil {
				log.Fatal(err)
			}
			printTurn(resp)
		}
		fmt.Println()
	}

	// Reset rewinds the conversation and keeps the tools loaded.
	if err := agent.Reset(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("conversation reset - the tools stay loaded")
}

func printTurn(resp *needle.Response) {
	fmt.Printf("  engine    type=%s calls=%d", resp.Type, len(resp.FunctionCalls))
	if resp.Confidence != nil {
		fmt.Printf(" confidence=%.4f", *resp.Confidence)
	}
	if resp.Validation != nil && resp.Validation.Negation {
		fmt.Print(" negation=true")
	}
	fmt.Println()
	if resp.Reasoning != nil && *resp.Reasoning != "" {
		fmt.Printf("  reasoning %s\n", *resp.Reasoning)
	}
}

func mustJSON(v any) string {
	blob, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(blob)
}
