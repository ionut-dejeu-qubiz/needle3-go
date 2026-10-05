// Command needle3 runs a base Needle 3 agent.
//
//	go run ./examples/needle3
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/FlameInTheDark/needle-go"
)

type WeatherArgs struct {
	City string `needle:"city" desc:"city name" required:"true"`
}

func main() {
	getWeather := needle.ToolFunc("get_weather",
		"Get the current weather for a city.",
		func(ctx context.Context, args WeatherArgs) (any, error) {
			return map[string]any{
				"city":   args.City,
				"temp_c": 27,
				"sky":    "clear",
			}, nil
		})

	agent, err := needle.New(
		needle.WithGeneration(3),
		needle.WithTools(getWeather),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()

	resp, err := agent.Run(context.Background(), "what's the weather in Lagos?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Results)
}
