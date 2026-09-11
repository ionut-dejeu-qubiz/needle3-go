package needle

import (
	"os"

	"github.com/FlameInTheDark/needle-go/internal/engine"
)

// Worker-mode hook. Tuned agents re-exec the host binary as a worker
// child (argv: --needle-worker <nonce>; env: NEEDLE_GO_WORKER=<nonce>).
// The init function below detects that combination before main() runs and
// takes the process over for the worker loop, so applications never need
// to cooperate. The nonce makes accidental activation from a stray
// environment variable impossible: env value and argv value must match.
func init() {
	if engine.IsWorker() {
		os.Exit(engine.RunWorkerChild(os.Stdin))
	}
}

// IsWorker reports whether the current process was started as a worker
// child of a tuned agent. It is exported for applications that want to
// assert they are never accidentally running in worker mode.
func IsWorker() bool { return engine.IsWorker() }
