// Command needle is the companion CLI of the needle-go library: it fetches
// and caches the native inference engine, downloads weights and platform
// builds from the Hugging Face Hub, and runs one-shot completions.
//
//	needle fetch                     engine for this machine into the cache
//	needle download linux-x86_64     a platform's standalone engine runner
//	needle download org/repo/x.cact  a published weights archive
//	needle run --prompt "..."        one completion through the library
//
// The CLI is built with urfave/cli v3; run "needle help <command>" for the
// full flag reference of any subcommand.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	needle "github.com/FlameInTheDark/needle-go"
	"github.com/FlameInTheDark/needle-go/fetch"
)

func main() {
	cmd := &cli.Command{
		Name:    "needle",
		Usage:   "on-device tool calling with the Needle engine",
		Version: needle.Version,
		Description: "Fetches and caches the native Needle inference engine, " +
			"downloads weights and standalone platform builds from the Hugging Face Hub, " +
			"and answers prompts through the library.",
		Commands: []*cli.Command{fetchCommand(), downloadCommand(), runCommand(), versionCommand()},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "needle: %v\n", err)
		os.Exit(1)
	}
}

// fetchCommand downloads the engine library for this machine (or an
// explicit --platform-tag) into the cache.
func fetchCommand() *cli.Command {
	return &cli.Command{
		Name:  "fetch",
		Usage: "fetch the inference engine for this machine into the cache",
		UsageText: "needle fetch [command options]\n\n" +
			"Downloads the engine build matching this machine from the Hugging Face Hub,\n" +
			"extracts the shared library and caches it. Later agent runs find it there\n" +
			"and never touch the network.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Usage: "directory to place the engine (default: the cache)",
			},
			&cli.StringFlag{
				Name:  "platform-tag",
				Usage: "fetch the build for another device, e.g. manylinux2014_aarch64",
			},
			&cli.IntFlag{
				Name:  "generation",
				Value: 2,
				Usage: "Needle engine generation to fetch",
			},
		},
		Action: fetchAction,
	}
}

func fetchAction(ctx context.Context, cmd *cli.Command) error {
	gen := cmd.Int("generation")
	dest := cmd.String("out")
	if dest == "" {
		cache, err := fetch.CacheDir(gen)
		if err != nil {
			return err
		}
		dest = cache
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	fetch.ProgressHook = newProgressPrinter()
	path, err := fetch.FetchLibrary(ctx, gen, "", cmd.String("platform-tag"), dest)
	if err != nil {
		return err
	}
	fmt.Printf("  %-9s %s\n", "engine", path)
	fmt.Printf("  %-9s copy to ~/.cache/needle-go/v%d/<version>/ on the device, or point NEEDLE%d_LIB_PATH at the file\n",
		"deploy", gen, gen)
	return nil
}

// downloadCommand pulls either a platform's standalone engine-runner files
// or a published .cact weights archive.
func downloadCommand() *cli.Command {
	return &cli.Command{
		Name:  "download",
		Usage: "download weights or a platform engine build",
		UsageText: "needle download [command options] <platform | org/repo[/file].cact>\n\n" +
			"A platform name (linux-x86_64, windows-x86_64, macos-arm64, wasm, ...) copies\n" +
			"that platform's standalone engine runner into --out/<platform>/. A repository\n" +
			"spec (org/repo or org/repo/file.cact) pulls a published weights archive.",
		ArgsUsage: "<platform | org/repo[/file].cact>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Value: ".",
				Usage: "directory to place the files",
			},
			&cli.IntFlag{
				Name:  "generation",
				Value: 2,
				Usage: "engine generation when downloading a platform build",
			},
		},
		Action: downloadAction,
	}
}

func downloadAction(ctx context.Context, cmd *cli.Command) error {
	if !cmd.Args().Present() {
		return cli.Exit("download needs one argument: a platform name or <org>/<repo>[/<file>.cact]", 2)
	}
	spec := cmd.Args().First()
	fetch.ProgressHook = newProgressPrinter()

	if !strings.Contains(spec, "/") {
		paths, err := fetch.DownloadPlatform(ctx, cmd.Int("generation"), spec, cmd.String("out"))
		if err != nil {
			return err
		}
		for _, p := range paths {
			fmt.Printf("  %-9s %s  %.2f MB\n", "file", p, fileSizeMB(p))
			if base := filepath.Base(p); base == "needle" || base == "needle.exe" {
				fmt.Printf("  %-9s %s --tools tools.json --serve\n", "next", p)
			}
		}
		return nil
	}

	path, err := fetch.DownloadWeights(ctx, spec, cmd.String("out"))
	if err != nil {
		return err
	}
	fmt.Printf("  %-9s %s  %.2f MB\n", "weights", path, fileSizeMB(path))
	fmt.Printf("  %-9s needle.New(needle.WithWeights(%q), needle.WithTools(...))\n", "next", path)
	return nil
}

// runCommand answers one prompt through the library, exercising the same
// engine resolution, cache and download path an application would.
func runCommand() *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "answer one prompt through the library",
		UsageText: "needle run [command options] [prompt ...]\n\n" +
			"The prompt comes from --prompt, or from the positional arguments joined by\n" +
			"spaces. With --tools the model may produce tool calls; without tools every\n" +
			"prompt is off-topic and the engine refuses. Prints the requested call, its\n" +
			"arguments and the confidence, or the raw response envelope with --json.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "prompt",
				Usage: "query text for tool-call generation",
			},
			&cli.StringFlag{
				Name:  "tools",
				Usage: "tools JSON file for tool-call generation",
			},
			&cli.StringFlag{
				Name:  "system",
				Usage: "system facts, e.g. 'date: 2026-07-21 Tue 14:30'",
			},
			&cli.StringFlag{
				Name:  "weights",
				Usage: "tuned .cact to run (default: the base model)",
			},
			&cli.IntFlag{
				Name:  "generation",
				Value: needle.EngineGeneration,
				Usage: "base Needle engine generation",
			},
			&cli.IntFlag{
				Name:  "max",
				Value: 256,
				Usage: "response token limit",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "print the raw response envelope as JSON",
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Value: 5 * time.Minute,
				Usage: "overall time limit",
			},
		},
		Action: runAction,
	}
}

func runAction(ctx context.Context, cmd *cli.Command) error {
	query := cmd.String("prompt")
	if query == "" && cmd.Args().Present() {
		query = strings.Join(cmd.Args().Slice(), " ")
	}

	var toolsOpt needle.Option = needle.WithTools()
	if toolsPath := cmd.String("tools"); toolsPath != "" {
		data, err := os.ReadFile(toolsPath)
		if err != nil {
			return err
		}
		toolsOpt = needle.WithToolsJSON(string(data))
	}
	opts := []needle.Option{toolsOpt}
	if system := cmd.String("system"); system != "" {
		opts = append(opts, needle.WithSystem(system))
	}
	if weights := cmd.String("weights"); weights != "" {
		opts = append(opts, needle.WithWeights(weights))
	} else {
		opts = append(opts, needle.WithGeneration(cmd.Int("generation")))
	}

	runCtx, cancel := context.WithTimeout(ctx, cmd.Duration("timeout"))
	defer cancel()

	agent, err := needle.New(opts...)
	if err != nil {
		return err
	}
	defer agent.Close()
	fetch.ProgressHook = newProgressPrinter()

	resp, err := agent.Complete(runCtx, query, needle.MaxTokens(cmd.Int("max")))
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	if call := resp.FirstCall(); call != nil {
		pretty, _ := json.MarshalIndent(call.Arguments, "", "  ")
		fmt.Printf("call       %s\narguments  %s\n", call.Name, string(pretty))
	} else {
		fmt.Printf("response   type=%s (no call: off-topic, unsupported, or negated)\n", resp.Type)
	}
	if resp.Confidence != nil {
		fmt.Printf("confidence %.4f\n", *resp.Confidence)
	}
	if resp.Reasoning != nil && *resp.Reasoning != "" {
		fmt.Printf("reasoning  %s\n", *resp.Reasoning)
	}
	return nil
}

// versionCommand prints the library version and the engine build version.
func versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "print the package and engine versions",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			engine, err := fetch.EngineVersion(needle.EngineGeneration)
			if err != nil {
				engine = "unknown"
			}
			fmt.Printf("needle-go %s (engine %s)\n", needle.Version, engine)
			return nil
		},
	}
}

// progressPrinter throttles download progress to one line per percent so a
// 14 MB engine fetch does not flood the terminal.
type progressPrinter struct{ lastPct int64 }

func newProgressPrinter() func(label string, total, done int64) {
	p := &progressPrinter{}
	return p.print
}

func (p *progressPrinter) print(label string, total, done int64) {
	if total <= 0 {
		fmt.Fprintf(os.Stderr, "\r  %-9s %s  %6.1f MB", "fetch", label, float64(done)/1e6)
		return
	}
	pct := 100 * done / total
	if pct < p.lastPct {
		p.lastPct = -1 // a new file started
	}
	if pct == p.lastPct && done < total {
		return
	}
	p.lastPct = pct
	fmt.Fprintf(os.Stderr, "\r  %-9s %s  %6.1f / %.1f MB (%3d%%)", "fetch", label,
		float64(done)/1e6, float64(total)/1e6, int(pct))
	if done >= total {
		fmt.Fprintln(os.Stderr)
		p.lastPct = -1
	}
}

func fileSizeMB(path string) float64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return float64(st.Size()) / 1e6
}
