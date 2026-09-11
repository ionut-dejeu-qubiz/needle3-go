# Development, CI and releases

Everything about working on this repository itself: building from source,
running the test suite, the continuous-integration workflows, how commits
turn into releases, and how CLI binaries end up attached to them. If you
only want to *use* the library, start with [Getting started](getting-started.md)
instead.

- [Prerequisites](#prerequisites)
- [Building from source](#building-from-source)
- [Version stamping](#version-stamping)
- [Running the test suite](#running-the-test-suite)
- [Continuous integration](#continuous-integration)
- [Writing commits that release](#writing-commits-that-release)
- [How a release happens](#how-a-release-happens)
- [CLI binaries on a release](#cli-binaries-on-a-release)
- [Recovering a release](#recovering-a-release)

## Prerequisites

- **Go 1.25 or newer** — the exact minimum is pinned in `go.mod`; any newer
  toolchain works. `actions/setup-go` reads it automatically in CI.
- **git** — releases are computed from the commit history.
- Nothing else. There is no C toolchain requirement anywhere: the native
  engine bindings load a shared library at runtime (see
  [Engine management](engine.md)), so `CGO_ENABLED=0` is the default for
  every build, test and cross-compile in this repository.

## Building from source

```sh
go build ./...          # library, CLI and examples
go build -o needle ./cmd/needle
```

Cross-compiling is a plain environment-variable switch, because nothing in
the repository uses cgo:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o needle.exe ./cmd/needle
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o needle     ./cmd/needle
```

## Version stamping

`needle.Version` is a variable, not a constant, so release builds can
override it at link time without touching the source tree:

```sh
go build -trimpath \
  -ldflags "-s -w -X github.com/FlameInTheDark/needle-go.Version=v1.2.3" \
  -o needle ./cmd/needle
```

A binary built this way reports the stamped version through `needle
--version` and `needle version`. The value committed in `version.go` is the
fallback that plain `go build` / `go install` builds report — it only moves
when the source tree itself needs a new fallback, not per release.

## Running the test suite

The suite has three layers, and each decides for itself whether it can run:

| Layer | Runs when | What it needs |
| --- | --- | --- |
| Unit tests (`schema_test.go`, `grounding_test.go`, `rekey_test.go`, parts of `fetch_test.go`) | always | nothing |
| Engine integration tests (`needle_test.go`) | the native engine can be resolved | the engine shared library, auto-downloaded on first use (~14 MB) or restored from the cache |
| Worker tests | `NEEDLE_TEST_WEIGHTS` points at a `.cact` archive | the base weights (~14 MB) |

```sh
go test ./...                                        # unit + engine tests
curl -fL -o needle2.cact \
  https://huggingface.co/Cactus-Compute/needle2/resolve/main/needle2.cact
NEEDLE_TEST_WEIGHTS=$PWD/needle2.cact go test -race ./...   # everything, race-enabled
go run ./examples/smart_home                         # 32-case acceptance suite
```

Engine tests that cannot resolve an engine **skip** with a message instead
of failing, so a network-less environment still gets the unit layer. CI
always provides the network, the engine and the weights, so every layer runs
there.

Before pushing, keep the two cheap gates green as well:

```sh
gofmt -l .      # must print nothing
go vet ./...
```

## Continuous integration

The CI workflow lives in
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) and runs on every
push to `main` and every pull request targeting `main`:

1. `gofmt -l .` must come back empty — unformatted files fail the build.
2. `go vet ./...` must pass.
3. The engine cache (`~/.cache/needle-go`) is restored from the Actions
   cache when possible; the cache key hashes `fetch/platform.go`, which
   holds the pinned engine version, so the key rotates exactly when the
   engine pin does.
4. The base weights are downloaded from the Hugging Face Hub.
5. `go test -race ./...` runs with `NEEDLE_TEST_WEIGHTS` set — the complete
   suite, race detector on, against the real engine.

Both jobs run on **ubuntu-latest and windows-latest**, so a change that
breaks one platform never lands silently. The total download for a cold
runner is roughly 28 MB (engine wheel + weights); the engine half is cached
after the first run.

## Writing commits that release

Releases are cut by [semantic-release](https://github.com/semantic-release/semantic-release)
from the commit history, so commit messages *are* the release interface.
They must follow the conventional-commits format:

```
<type>(<optional scope>): <imperative summary>

<optional body>

BREAKING CHANGE: <why consumers must change>
```

| Commit type | Releases | Bumps |
| --- | --- | --- |
| `feat: …` | yes | minor (`0.4.0` → `0.5.0`) |
| `fix: …` | yes | patch (`0.4.0` → `0.4.1`) |
| `perf: …`, `refactor: …` with `BREAKING CHANGE` footer (or `feat!:` / `fix!:`) | yes | major (`0.4.0` → `1.0.0`) |
| `docs: …`, `chore: …`, `test: …`, `refactor: …`, `ci: …` | no | — |

A push to `main` containing **only** non-releasing types produces no release
at all — that is normal and expected. The release notes are generated from
the same messages, so the `feat:`/`fix:` summary line is what users will
read in the changelog; make it specific ("add WithToolIndexMinScore option",
not "update code").

## How a release happens

The release pipeline lives in
[`.github/workflows/release.yml`](../.github/workflows/release.yml) and is
configured by [`.releaserc.json`](../.releaserc.json). On every push to
`main`:

1. The full test matrix (Linux + Windows) must pass — releases never happen
   behind red tests.
2. semantic-release walks the commits since the last release tag. If there
   is nothing to release, the pipeline ends here with a green run.
3. Otherwise it creates the tag (`v1.2.3` by default), the GitHub release
   and the generated notes.
4. The CLI binary build is dispatched for the new tag (details below).

Two things are worth knowing:

- **First-release bootstrap.** With no tags in the repository,
  semantic-release defaults the very first release to `v1.0.0`. To keep
  releasing from the `0.x` line instead, seed a tag before enabling Actions:
  `git tag v0.3.0 && git push origin v0.3.0` — the next `feat` then releases
  `v0.4.0`.
- **Token choice.** With the built-in `GITHUB_TOKEN` everything works
  out of the box, but releases created with it do not emit `release` events
  (GitHub's recursion guard), which is why the release pipeline dispatches
  the binary build explicitly. If you switch semantic-release to a PAT, the
  release event fires the binary build directly instead; the pipeline
  detects that via the release creator and avoids double builds.

The release version never lives in the source tree — it is derived from
commits and stamped into the binaries at build time (see
[Version stamping](#version-stamping)). There is no `CHANGELOG.md` to
maintain; the GitHub release *is* the changelog.

## CLI binaries on a release

The build workflow lives in
[`.github/workflows/build-binaries.yml`](../.github/workflows/build-binaries.yml).
It triggers on every published release and can also be dispatched manually
(see [Recovering a release](#recovering-a-release)). It checks out the
release tag, cross-compiles the CLI with `CGO_ENABLED=0` for every supported
platform, and uploads the archives plus a `checksums.txt` to the release.

| Archive | Runs on |
| --- | --- |
| `needle_<v>_linux_amd64.tar.gz` | 64-bit Linux (glibc or musl) |
| `needle_<v>_linux_arm64.tar.gz` | 64-bit ARM Linux |
| `needle_<v>_linux_386.tar.gz` | 32-bit x86 Linux |
| `needle_<v>_linux_riscv64.tar.gz` | 64-bit RISC-V Linux |
| `needle_<v>_windows_amd64.zip` | 64-bit Windows |
| `needle_<v>_windows_arm64.zip` | ARM64 Windows |
| `needle_<v>_windows_386.zip` | 32-bit Windows |
| `needle_<v>_darwin_amd64.tar.gz` | Intel macOS |
| `needle_<v>_darwin_arm64.tar.gz` | Apple Silicon macOS |

Every archive contains the binary (`needle` / `needle.exe`), `README.md` and
`LICENSE`. Verify a download against `checksums.txt`:

```sh
sha256sum --check checksums.txt --ignore-missing
```

The amd64 and arm64 Windows and Linux builds resolve their engine
automatically on first use. The `386` and `riscv64` builds compile and run
the CLI, but no engine wheel is published for those platforms — place a
compatible library manually and point `NEEDLE_LIB_PATH` at it (see
[Engine management](engine.md)).

## Recovering a release

If the binary build failed after a release was published (a flaky network,
a runner hiccup), nothing needs to be re-released — the artifacts can
simply be rebuilt and re-uploaded for the existing tag:

```sh
gh workflow run build-binaries.yml --ref main -f tag=v1.2.3
```

or from the Actions tab: **Build CLI binaries → Run workflow → tag =
`v1.2.3`**. Re-uploads overwrite existing assets (`--clobber`), so partial
uploads heal themselves on the next run.
