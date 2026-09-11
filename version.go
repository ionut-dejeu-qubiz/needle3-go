package needle

// Version is the version of the needle-go library.
//
// Release builds override it at link time with the release tag, so a shipped
// binary reports the version it was released under:
//
//	go build -ldflags "-X github.com/FlameInTheDark/needle-go.Version=v1.2.3" ./cmd/needle
//
// Builds made straight from source report the fallback value below.
var Version = "0.3.0"

// EngineGeneration is the default engine generation this package targets.
// The base model shipped with generation 2 weights is the 45M-parameter
// Needle 2 tool-calling model.
const EngineGeneration = 2
