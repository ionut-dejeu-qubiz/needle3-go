# CLI reference

The repository ships a companion CLI, built with
[urfave/cli v3](https://github.com/urfave/cli).

## Installing

**Prebuilt binary.** Every [GitHub
release](https://github.com/FlameInTheDark/needle-go/releases) publishes
ready-to-run archives for all supported platforms, plus a `checksums.txt`.
Download the one matching your machine, verify it, extract it and put the
binary on your `PATH`:

```sh
curl -fL -o needle.tar.gz \
  https://github.com/FlameInTheDark/needle-go/releases/latest/download/needle_<version>_linux_amd64.tar.gz
tar xzf needle.tar.gz needle_<version>_linux_amd64/needle
./needle_<version>_linux_amd64/needle version
```

On Windows, take `needle_<version>_windows_amd64.zip` instead. The archive
names and the full platform table are in
[Development & releases](development.md#cli-binaries-on-a-release).

**From source.**

```sh
go build -o needle ./cmd/needle
go install github.com/FlameInTheDark/needle-go/cmd/needle@latest
```

## Command overview

```
NAME:
   needle - on-device tool calling with the Needle 2 engine

USAGE:
   needle [global options] [command [command options]]

COMMANDS:
   fetch     fetch the inference engine for this machine into the cache
   download  download weights or a platform engine build
   run       answer one prompt through the library
   version   print the package and engine versions
   help      Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h     show help
   --version, -v  print the version
```

Conventions that apply to every command:

- Flags may appear before or after positional arguments
  (`needle download linux-x86_64 --out dir` and
  `needle download --out dir linux-x86_64` are equivalent).
- `--flag value` and `--flag=value` both parse.
- `needle help <command>` prints the full reference for one command.
- Errors exit with status 1; usage mistakes exit with status 2.

- [`needle fetch`](#fetch)
- [`needle download`](#download)
- [`needle run`](#run)
- [`needle version`](#version)

## `fetch`

Downloads the engine build matching this machine from the Hugging Face Hub,
extracts the shared library and caches it. Later agent runs find it there and
never touch the network.

```
needle fetch [command options]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--out DIR` | the cache | Directory to place the engine. |
| `--platform-tag TAG` | current machine | Fetch the build for another device, e.g. `manylinux2014_aarch64` or `musllinux_1_2_x86_64`. |
| `--generation N` | 2 | Needle engine generation to fetch. |

Examples:

```sh
needle fetch                                  # this machine, into the cache
needle fetch --out /mnt/usb/engine            # this machine, elsewhere
needle fetch --platform-tag win_amd64         # Windows build from a Linux host
```

Output includes the resolved path and a deployment hint for copying the file
to the same cache path on another device, or pointing `NEEDLE2_LIB_PATH` at
it. See [Engine management](engine.md#air-gapped--offline-devices) for the
full offline workflow.

## `download`

Pulls either a platform's standalone engine-runner files or a published
weights archive.

```
needle download [command options] <platform | org/repo[/file].cact>
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--out DIR` | `.` | Directory to place the files. |
| `--generation N` | 2 | Engine generation when downloading a platform build. |

A **platform name** copies that platform's standalone runner files into
`--out/<platform>/` and marks the native `needle` / `needle.exe` runner
executable. Platforms: `macos-arm64`, `linux-x86_64`, `linux-arm64`,
`linux-armv7`, `linux-riscv64`, `linux-mipsel`, `windows-x86_64`,
`windows-arm64`, `android-arm64`, `android-armv7`, `android-riscv64`,
`ios-arm64`, `ios-sim-arm64`, `tvos-arm64`, `watchos-arm64`, `wasm`,
`wasm-component`.

A **repository spec** pulls a published `.cact` weights archive:
`org/repo/file.cact` names the file; `org/repo` works when the repository
holds exactly one archive.

Examples:

```sh
needle download linux-x86_64                       # standalone runner
needle download Cactus-Compute/needle2/needle2.cact # the base weights archive
needle download myorg/my-tuned-tools               # the repo's single archive
```

The runner serves HTTP by itself: `./needle --tools tools.json --serve`
(see [Engine management: standalone runner](engine.md#the-standalone-runner)).
Downloaded weights feed `needle.New(needle.WithWeights(...))` — see
[Tuned weights](weights.md).

## `run`

Answers one prompt through the library itself, exercising the same engine
resolution, cache and download path your application will.

```
needle run [command options] [prompt ...]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--prompt TEXT` | — | Query text; positional words are joined by spaces when omitted. |
| `--tools FILE` | — | Tools JSON file (`[{"name", "description", "parameters"}, ...]`). |
| `--system TEXT` | — | System facts, e.g. `date: 2026-07-21 Tue 14:30`. |
| `--weights FILE` | base model | Tuned `.cact` to run (worker process). |
| `--max N` | 256 | Response token limit. |
| `--json` | off | Print the raw response envelope as JSON. |
| `--timeout DURATION` | 5m | Overall time limit. |

Examples:

```sh
needle run --tools tools.json --prompt "what's it like in Lagos right now?"
needle run --tools tools.json what's it like in Lagos right now?
needle run --tools tools.json --json --prompt "dim the study to 30"
needle run --tools tools.json --weights my.cact --prompt "..."
needle run --tools tools.json --system "date: 2026-09-11 Fri 10:30" --prompt "..."
```

Output, without `--json`:

```
call       get_weather
arguments  {
  "city": "Lagos"
}
confidence 0.9387
reasoning  'Lagos' -> city
```

A refusal prints `response   type=call (no call: off-topic, unsupported, or
negated)` — see [Responses: the refusal contract](responses.md#the-refusal-contract).

## `version`

```sh
needle version
# needle-go 0.2.0 (engine 2.0.4)
```

Reports the library version and the engine build version it targets.

## Where to go next

- What `fetch` puts where: [Engine management](engine.md)
- What to do with downloaded weights: [Tuned weights](weights.md)
- The response envelope `run --json` prints: [Responses](responses.md)
