# Engine management

The inference engine is a native shared library (`libneedle.so` /
`libneedle.dll` / `libneedle.dylib`, ~14 MB) that embeds the whole model. This
page documents how the library finds it, downloads it, caches it, and what
you can configure at the process and machine level.

- [What the engine is](#what-the-engine-is)
- [Resolution order](#resolution-order)
- [The cache layout](#the-cache-layout)
- [Fetching explicitly](#fetching-explicitly)
- [Air-gapped / offline devices](#air-gapped--offline-devices)
- [Alpine (musl) and other libc flavours](#alpine-musl-and-other-libc-flavours)
- [Platform support](#platform-support)
- [Pinning the engine in code](#pinning-the-engine-in-code)
- [Environment variables](#environment-variables)
- [The standalone runner](#the-standalone-runner)

## What the engine is

One file per platform, published on the Hugging Face Hub. The library never
links it at build time — it calls `dlopen`/`LoadLibrary` at runtime through
[purego](https://github.com/ebitengine/purego), which is why `CGO_ENABLED=0`
builds work and cross-compiling from any host is a plain
`GOOS=windows go build`. Four C functions (`needle_init`, `needle_complete`,
`needle_reset`, `needle_load`) are the entire surface.

After the engine is in place, **inference never touches the network**.

## Resolution order

Every agent resolves its engine library on first use, in this order:

1. **`NEEDLE<GEN>_LIB_PATH`** — `NEEDLE2_LIB_PATH` for generation 2
   (`NEEDLE_LIB_PATH` is a legacy alias for generation 2 only, and is
   deliberately never used for generation 3 archives);
2. **a library placed next to your executable** — `libneedle2.so` /
   `libneedle.so` (or the `.dll` / `.dylib` equivalents) in the executable's
   directory;
3. **the cache** — `~/.cache/needle-go/v2/<engine-version>/libneedle.so` on
   Linux/macOS, `%USERPROFILE%\.cache\needle-go\v2\<engine-version>\libneedle.dll`
   on Windows;
4. **a download** of the engine package for the current platform from the
   Hugging Face Hub — a zip archive from which only `needle/libneedle.*` is
   extracted, atomically, into the cache.

`WithEnginePath(path)` in code pins step 1 outright (see
[below](#pinning-the-engine-in-code)).

## The cache layout

```
~/.cache/needle-go/
└── v2/                     engine generation
    └── 2.0.4/              engine build version
        └── libneedle.so    the extracted shared library
```

- One directory per generation × version — upgrading the engine build never
  clobbers the previous one, and generation 3 archives will sit beside
  generation 2 untouched.
- Downloads land as `libneedle.so.part` and are renamed into place, so a
  killed download can never leave a half-written library that loads by
  accident.
- Deleting the cache is always safe: the next agent re-downloads.

## Fetching explicitly

The `fetch` package and the CLI both expose explicit fetching (no agent
required):

```sh
needle fetch                          # engine for this machine, into the cache
needle fetch --out /tmp/engine        # ... into a specific directory
needle fetch --platform-tag manylinux2014_aarch64   # a build for another device
```

In code:

```go
path, err := fetch.FetchLibrary(ctx, 2, "", "", destDir)
```

Progress reporting hooks into both: assign `fetch.ProgressHook` (a
`func(label string, total, done int64)`) before the first fetch, or watch the
CLI's live progress lines.

## Air-gapped / offline devices

For a device that must never attempt the network:

1. On a connected machine, fetch the engine for the *device's* platform:

   ```sh
   needle fetch --platform-tag manylinux2014_aarch64
   ```

2. Copy the library to the same cache path on the device —
   `~/.cache/needle-go/v2/2.0.4/` — or drop it **next to the deployed
   executable**, or point `NEEDLE2_LIB_PATH` at it.
3. On the device, export `HF_HUB_OFFLINE=1`. A missing engine then fails fast
   with a clear error instead of attempting a download.

The same flag works for applications that want fail-fast behavior even on
networked machines: with the engine cached, `HF_HUB_OFFLINE=1` changes
nothing; without it, the first agent call errors immediately.

## Alpine (musl) and other libc flavours

On Linux the library detects musl userlands (via `/proc/self/maps` and the
`ld-musl` loader glob) and fetches the `musllinux_1_2_*` engine build instead
of `manylinux2014_*` — Alpine works out of the box. `cannot open engine
library` on Alpine almost always means a glibc build was placed manually;
delete the cached library and let the package re-fetch the matching one.

Architectures without published engine packages (linux/armv7, riscv64, …)
fail with a message pointing at the platform's `libneedle.a` in the Hub
repository for self-built engines.

## Platform support

| Platform | Engine package | Status |
| --- | --- | --- |
| linux/amd64 (glibc) | `manylinux2014_x86_64` | supported, auto-detected |
| linux/arm64 (glibc) | `manylinux2014_aarch64` | supported, auto-detected |
| linux/amd64, arm64 (musl) | `musllinux_1_2_*` | supported, auto-detected |
| windows/amd64 | `win_amd64` | supported, auto-detected |
| windows/arm64 | `win_arm64` | supported, auto-detected |
| macOS amd64 / arm64 | `macosx_11_0_*` | supported, auto-detected |
| other linux arches | — | manual: build from the platform's `libneedle.a` and set `NEEDLE_LIB_PATH` |

The **library itself** (not the engine) cross-compiles to a wider matrix —
windows/amd64, windows/arm64, windows/386, linux/amd64, linux/arm64,
linux/386, linux/riscv64, darwin/amd64, darwin/arm64 — all with
`CGO_ENABLED=0`; the narrower engine table above is what auto-downloads.

## Pinning the engine in code

```go
agent, err := needle.New(
    needle.WithTools(tools),
    needle.WithEnginePath("/opt/vendor/libneedle2.so"),
)
```

`WithEnginePath` overrides environment variables and the cache lookup —
useful for shipping an engine beside your binary under a known path, or for
test harnesses pinning a specific engine build.

## Environment variables

| Variable | Effect |
| --- | --- |
| `NEEDLE2_LIB_PATH` / `NEEDLE3_LIB_PATH` | Pin the engine library for a generation. |
| `NEEDLE_LIB_PATH` | Legacy alias for generation 2 only. |
| `HF_HUB_OFFLINE=1` | Never download; fail fast when the engine is missing. |
| `HF_ENDPOINT` | Use a different Hub endpoint (e.g. `https://hf-mirror.com`). |
| `HTTP(S)_PROXY` | Honored by the Go HTTP stack for engine downloads. |
| `NEEDLE_STRICT_VALIDATE=1` | The engine's own strict validation mode: suppresses duplicate and ungrounded calls at the engine level. Set before the first agent call. |
| `NEEDLE_THREADS`, `NEEDLE_DEBUG`, `NEEDLE_KV_BITS`, `NEEDLE_KV_WINDOW`, `NEEDLE_CONFIDENCE`, `NEEDLE_CONF_RESCORE` | Native engine tuning knobs, read by the engine itself. Set before the first agent call. |

## The standalone runner

Besides the shared library, every platform folder in the Hub repository
carries a **standalone engine runner** — a single `needle` / `needle.exe`
executable that serves HTTP by itself:

```sh
needle download linux-x86_64 --out .
./linux-x86_64/needle --tools tools.json --serve
```

- `POST /complete` with `{"input": "..."}` — one completion;
- `POST /reset` — rewind the conversation.

Useful for non-Go services that want the engine over a local socket, and as
a deployment smoke test. Downloads via the
[CLI](cli.md#download) mark the runner executable automatically.

## Where to go next

- The first-run flow: [Getting started](getting-started.md)
- The CLI that drives all of this: [CLI reference](cli.md)
- Download functions in code: [API reference](api.md)
