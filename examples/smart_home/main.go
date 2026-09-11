// Command smart_home is a complete smart-home environment example: a
// hand-curated tool surface whose enums, bounds and descriptions map
// cleanly onto constrained decoding, plus a frozen acceptance suite.
// Swap the enum values for the rooms and devices your product exposes
// and keep the shapes: closed sets as enums, bounded numbers, verbatim
// copy for free text, five tools or fewer.
//
//	go run ./examples/smart_home            # run the acceptance suite
//	go run ./examples/smart_home --query "dim the study lights to 30 percent"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/FlameInTheDark/needle-go"
)

// System carries the environment's operating rules, as facts rather than
// instructions.
const system = "Map each explicit supported home action to exactly one declared call; " +
	"never duplicate an action. Do not guess missing targets or values. " +
	"Unsupported, invalid, ambiguous, and negated requests return no call."

// ---- Tool schemas (typed struct-tag declarations) ----

type ControlLightsArgs struct {
	Room       string  `needle:"room" desc:"The room to control." enum:"kitchen|living_room|bedroom|study" required:"true"`
	Action     string  `needle:"action" desc:"on, off, or dim." enum:"on|off|dim" required:"true"`
	Brightness *int    `needle:"brightness_percent" desc:"Brightness from 0 to 100. A dim request with a number must carry it in this same call." min:"0" max:"100"`
	Color      *string `needle:"color" desc:"The light color; include only when the user names one." enum:"warm white|cool white|red|green|blue"`
}

type SetThermostatArgs struct {
	Temperature int `needle:"temperature" desc:"Target temperature in degrees Celsius." min:"10" max:"30" required:"true"`
}

type ControlFanArgs struct {
	Room   string  `needle:"room" desc:"The room whose fan to control." enum:"living_room|bedroom|study" required:"true"`
	Action string  `needle:"action" desc:"on or off." enum:"on|off" required:"true"`
	Speed  *string `needle:"speed" desc:"Fan speed; include only when stated." enum:"low|medium|high"`
}

type ControlBlindsArgs struct {
	Room   string `needle:"room" desc:"The room whose blinds to move." enum:"kitchen|living_room|bedroom|study" required:"true"`
	Action string `needle:"action" desc:"open or close." enum:"open|close" required:"true"`
}

type StartVacuumArgs struct {
	Action string  `needle:"action" desc:"start, stop, or dock." enum:"start|stop|dock" required:"true"`
	Room   *string `needle:"room" desc:"The room to clean; include only when starting a run in a stated room." enum:"kitchen|living_room|bedroom"`
}

func toolsList() []map[string]any {
	var out []map[string]any
	for _, t := range tools() {
		out = append(out, t.Schema())
	}
	return out
}

func tools() []*needle.Tool {
	return []*needle.Tool{
		needle.ToolFunc("control_lights",
			"Turn lights on or off in a room, dim them to a brightness percentage, "+
				"or set their color. A color request means action on. Never use this "+
				"for blinds, fans, or any other device.",
			func(ctx context.Context, a ControlLightsArgs) (any, error) {
				return map[string]any{"ok": true, "room": a.Room, "action": a.Action,
					"brightness_percent": a.Brightness, "color": a.Color}, nil
			}),
		needle.ToolFunc("set_thermostat",
			"Set the home thermostat to a target temperature in degrees Celsius. "+
				"This never controls fans, lights, or any other device.",
			func(ctx context.Context, a SetThermostatArgs) (any, error) {
				return map[string]any{"ok": true, "temperature": a.Temperature}, nil
			}),
		needle.ToolFunc("control_fan",
			"Turn a room fan on or off, optionally at a stated speed. This never "+
				"changes the thermostat.",
			func(ctx context.Context, a ControlFanArgs) (any, error) {
				return map[string]any{"ok": true, "room": a.Room, "action": a.Action,
					"speed": a.Speed}, nil
			}),
		needle.ToolFunc("control_blinds",
			"Open or close the window blinds in one stated room. Never pick the room "+
				"yourself. Never use this for lights or the vacuum.",
			func(ctx context.Context, a ControlBlindsArgs) (any, error) {
				return map[string]any{"ok": true, "room": a.Room, "action": a.Action}, nil
			}),
		needle.ToolFunc("start_robot_vacuum",
			"Start or stop the robot vacuum, or send it back to its dock to charge.",
			func(ctx context.Context, a StartVacuumArgs) (any, error) {
				return map[string]any{"ok": true, "action": a.Action, "room": a.Room}, nil
			}),
	}
}

// ---- Frozen acceptance suite (ported verbatim) ----

type expectation struct {
	query    string
	expected map[string]any // name + arguments of the single expected call
}

func suite() []expectation {
	positives := []expectation{
		{"turn on the kitchen lights", call("control_lights", "room", "kitchen", "action", "on")},
		{"switch off the lights in the bedroom", call("control_lights", "room", "bedroom", "action", "off")},
		{"dim the living room lights to 35 percent", call("control_lights", "room", "living_room", "action", "dim", "brightness_percent", float64(35))},
		{"turn on the study lights in warm white", call("control_lights", "room", "study", "action", "on", "color", "warm white")},
		{"put the bedroom lights on in blue", call("control_lights", "room", "bedroom", "action", "on", "color", "blue")},
		{"set the thermostat to 22 degrees", call("set_thermostat", "temperature", float64(22))},
		{"warm the house to 24 degrees", call("set_thermostat", "temperature", float64(24))},
		{"cool the whole home down to 19 degrees", call("set_thermostat", "temperature", float64(19))},
		{"turn on the bedroom fan", call("control_fan", "room", "bedroom", "action", "on")},
		{"switch the study fan off", call("control_fan", "room", "study", "action", "off")},
		{"turn on the living room fan at high speed", call("control_fan", "room", "living_room", "action", "on", "speed", "high")},
		{"run the study fan on low", call("control_fan", "room", "study", "action", "on", "speed", "low")},
		{"open the kitchen blinds", call("control_blinds", "room", "kitchen", "action", "open")},
		{"close the blinds in the living room", call("control_blinds", "room", "living_room", "action", "close")},
		{"open up the bedroom blinds", call("control_blinds", "room", "bedroom", "action", "open")},
		{"start the robot vacuum", call("start_robot_vacuum", "action", "start")},
		{"start vacuuming the bedroom", call("start_robot_vacuum", "action", "start", "room", "bedroom")},
		{"stop the robot vacuum", call("start_robot_vacuum", "action", "stop")},
	}
	// Refusals: missing targets, unsupported devices, negations.
	refusals := []string{
		"turn on the lights",                          // missing room
		"set the thermostat to something comfortable", // missing value
		"open the blinds",                             // missing room
		"turn the fan on",                             // missing room
		"lock the back door",                          // unsupported
		"check whether the robot vacuum is charging",  // not an action
		"play some jazz in the living room",           // unsupported
		"don't turn on the study lights",              // negation
		"do not close the living room blinds",         // negation
		"never run the vacuum while I am on a call",   // negation
		"dim the bedroom lights to 150 percent",       // invalid: out of bounds
		"set the thermostat to 40 degrees",            // invalid: out of bounds
	}
	// Parallel: two independent actions in one query.
	parallel := []expectation{
		{query: "turn off the bedroom lights and set the thermostat to 18 degrees",
			expected: multi(
				call("control_lights", "room", "bedroom", "action", "off"),
				call("set_thermostat", "temperature", float64(18)),
			)},
		{query: "start the vacuum in the kitchen and open the living room blinds",
			expected: multi(
				call("start_robot_vacuum", "action", "start", "room", "kitchen"),
				call("control_blinds", "room", "living_room", "action", "open"),
			)},
	}
	var all []expectation
	for _, p := range positives {
		all = append(all, p)
	}
	for _, q := range refusals {
		all = append(all, expectation{query: q, expected: nil})
	}
	all = append(all, parallel...)
	return all
}

// multi builds the expected two-call list of a parallel case.
func multi(calls ...map[string]any) map[string]any {
	return map[string]any{"__multi__": calls}
}

func call(name string, kv ...any) map[string]any {
	out := map[string]any{"name": name}
	for i := 0; i+1 < len(kv); i += 2 {
		out[fmt.Sprint(kv[i])] = kv[i+1]
	}
	return out
}

func main() {
	query := flag.String("query", "", "answer one query instead of running the suite")
	minConfidence := flag.Float64("min-confidence", 0.0, "suite pass threshold")
	dumpSchema := flag.Bool("schema", false, "print the tool schemas as JSON and exit")
	flag.Parse()

	if *dumpSchema {
		blob, err := json.MarshalIndent(toolsList(), "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(blob))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// The engine's own strict-validation mode: it suppresses duplicate
	// and ungrounded calls.
	os.Setenv("NEEDLE_STRICT_VALIDATE", "1")

	agent, err := needle.New(needle.WithTools(tools()), needle.WithSystem(system))
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()

	if *query != "" {
		resp, err := agent.Complete(ctx, *query)
		if err != nil {
			log.Fatal(err)
		}
		if call := resp.FirstCall(); call != nil {
			fmt.Printf("%s %v\n", call.Name, call.Arguments)
		} else {
			fmt.Println("(no call: refused)")
		}
		return
	}

	pass, total := 0, 0
	for _, tc := range suite() {
		total++
		// Each case is an independent conversation: reset keeps the tools
		// loaded but rewinds the history.
		if err := agent.Reset(); err != nil {
			log.Fatal(err)
		}
		resp, err := agent.Complete(ctx, tc.query)
		if err != nil {
			log.Fatal(err)
		}
		if matches(resp, tc.expected, *minConfidence) {
			pass++
		} else {
			fmt.Printf("FAIL  %s\n  want %v\n  got  %s\n", tc.query, tc.expected, resp.String())
		}
	}
	fmt.Printf("\n%d/%d acceptance cases passed\n", pass, total)
	if pass < (total*9)/10 {
		os.Exit(1)
	}
}

func matches(resp *needle.Response, expected map[string]any, minConf float64) bool {
	if resp.Confidence != nil && *resp.Confidence < minConf {
		return false
	}
	if expected == nil {
		return len(resp.FunctionCalls) == 0
	}
	if multi, ok := expected["__multi__"].([]map[string]any); ok {
		if len(resp.FunctionCalls) != len(multi) {
			return false
		}
		for i, want := range multi {
			if !callMatches(&resp.FunctionCalls[i], want) {
				return false
			}
		}
		return true
	}
	if len(resp.FunctionCalls) != 1 {
		return false
	}
	return callMatches(&resp.FunctionCalls[0], expected)
}

func callMatches(got *needle.FunctionCall, expected map[string]any) bool {
	if got == nil || got.Name != expected["name"] {
		return false
	}
	for key, want := range expected {
		if key == "name" {
			continue
		}
		if got.Arguments[key] != want {
			return false
		}
	}
	return true
}
