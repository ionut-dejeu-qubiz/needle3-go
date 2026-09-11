package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/FlameInTheDark/needle-go"
)

func main() {
	home, _ := os.UserHomeDir()
	fmt.Println("HOME:", home)
	agent, err := needle.New(needle.WithToolsJSON(`[{"name":"get_weather","description":"Get the current weather for a city.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]`))
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	resp, err := agent.Complete(ctx, "what's the weather in Paris?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("call:", resp.FirstCall().Name, resp.FirstCall().Arguments)
}
