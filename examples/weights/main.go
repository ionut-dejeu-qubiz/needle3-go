// Command weights runs an agent on a tuned .cact archive. The engine is
// weights-agnostic, so a tuned archive runs on the same native engine
// without recompilation.
//
// Each tuned agent runs in its own worker process: the library re-exec
// the host binary with a private argv/env handshake, the child loads the
// .cact via needle_load and owns an independent engine and conversation.
// Applications do not need to cooperate - importing this package is
// enough. Confidence is reported as nil, because fine-tuning does not
// update the calibration head.
//
//	go run ./examples/weights my_needle.cact
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/FlameInTheDark/needle-go"
)

type SetLightsArgs struct {
	Room       string `needle:"room" desc:"which room to control" required:"true"`
	Action     string `needle:"action" desc:"on, off, or dim" enum:"on|off|dim" required:"true"`
	Brightness *int   `needle:"brightness" desc:"0 to 100" min:"0" max:"100"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("usage: weights <path-to-.cact>")
		fmt.Println("  bring your own tuned archive, or")
		fmt.Println("  download the base archive: needle download Cactus-Compute/needle2/needle2.cact")
		os.Exit(2)
	}
	weights := os.Args[1]

	agent, err := needle.New(
		needle.WithWeights(weights),
		needle.WithTools(needle.ToolFunc("set_lights",
			"Turn a room's lights on or off and set brightness.",
			func(ctx context.Context, a SetLightsArgs) (any, error) {
				return map[string]any{"ok": true, "room": a.Room, "action": a.Action,
					"brightness": a.Brightness}, nil
			})),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()

	fmt.Printf("engine generation %d, tuned weights %s\n", agent.Generation(), weights)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resp, err := agent.Run(ctx, "dim the living room to 30")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("type       %s\n", resp.Type)
	if resp.Confidence == nil {
		fmt.Println("confidence nil (tuned weights are uncalibrated)")
	}
	if blob, err := json.MarshalIndent(resp.Results, "", "  "); err == nil {
		fmt.Printf("results    %s\n", blob)
	}
}
