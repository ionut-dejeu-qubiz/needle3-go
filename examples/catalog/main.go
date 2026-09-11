// Command catalog declares more than five tools, which engages the
// engine's built-in retrieval head: every tool schema is embedded once at
// init, each turn embeds the query, and only the five highest-scoring
// tools enter the context with the grammar constrained to that subset.
// An unselected tool is unreachable, not merely unlikely.
//
//	go run ./examples/catalog
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/FlameInTheDark/needle-go"
)

// mediaControls is one enum shared by several tools - the closed sets,
// bounds and descriptions map cleanly onto the engine's constrained
// decoding.
type mediaControls struct{}

func main() {
	// A catalogue larger than five tools, built dynamically with the
	// property builders instead of struct tags.
	tools := []*needle.Tool{
		mediaTool("play_track", "Play a track by name.",
			map[string]needle.Property{
				"track":  needle.Str(needle.Desc("track title"), needle.IsRequired),
				"artist": needle.Str(needle.Desc("artist name")),
			}),
		mediaTool("pause_playback", "Pause the current playback.", nil),
		mediaTool("resume_playback", "Resume the current playback.", nil),
		mediaTool("set_volume", "Set the playback volume.",
			map[string]needle.Property{
				"percent": needle.Int(needle.Desc("volume 0-100"),
					needle.Min(0), needle.Max(100), needle.IsRequired),
			}),
		mediaTool("play_playlist", "Play a saved playlist.",
			map[string]needle.Property{
				"name":    needle.Str(needle.Desc("playlist name"), needle.IsRequired),
				"shuffle": needle.Bool(needle.Desc("shuffle the playlist")),
			}),
		mediaTool("skip_track", "Skip to the next track.", nil),
		mediaTool("queue_track", "Add a track to the play queue.",
			map[string]needle.Property{
				"track":  needle.Str(needle.Desc("track title"), needle.IsRequired),
				"artist": needle.Str(needle.Desc("artist name")),
			}),
		mediaTool("set_repeat_mode", "Set the repeat mode.",
			map[string]needle.Property{
				"mode": needle.Enum("off", "all", "one"),
			}),
	}

	agent, err := needle.New(
		needle.WithTools(tools),
		// Persist the tool embeddings on disk: keyed by a fingerprint
		// over the schemas and the model, a matching index loads
		// instantly and a changed schema re-embeds only what changed.
		needle.WithToolIndexPath("media-tools.idx"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()
	fmt.Printf("declared %d tools (retrieval selects five per turn)\n\n", len(agent.Tools()))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	for _, query := range []string{
		"play humble bundle by molly ten",
		"set the volume to 40",
		"play my late night playlist on shuffle",
	} {
		resp, err := agent.Complete(ctx, query)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("query      %s\n", query)
		if call := resp.FirstCall(); call != nil {
			if blob, err := json.Marshal(call.Arguments); err == nil {
				fmt.Printf("call       %s %s\n", call.Name, blob)
			}
		} else {
			fmt.Printf("call       (refused)\n")
		}
		if resp.Confidence != nil {
			fmt.Printf("confidence %.4f\n", *resp.Confidence)
		}
		fmt.Println()
	}
}

// mediaTool assembles one tool over the shared description pattern.
func mediaTool(name, description string, props map[string]needle.Property) *needle.Tool {
	builder := needle.NewTool(name, description)
	if props != nil {
		builder = builder.Params(props)
	}
	return builder.
		Handler(func(ctx context.Context, arguments map[string]any) (any, error) {
			return map[string]any{"ok": true, "executed": name}, nil
		}).
		Build()
}
