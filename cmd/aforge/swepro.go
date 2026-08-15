package main

import "github.com/Agent-Field/aforge-v2/internal/swepro/codeaf"

// The vendored swe-pro engine keeps process-global state — env knobs it sets
// on itself, a plandb singleton, a working directory it owns — so a run of it
// is a process, not a goroutine. Rather than ship a second binary, the aforge
// binary agrees to *be* that process when an env sentinel says so: a parent
// that wants the engine execs its own executable with AFORGE_SWEPRO=1 and
// codeaf's argv, and gets codeaf.
//
// The sentinel earns its keep twice. The engine's auto-resume supervisor
// re-execs os.Executable() with codeaf argv of its own
// (internal/swepro/codeaf/main.go, runAutoResume) and passes os.Environ()
// through; because the sentinel is already in that environment, the
// grandchild is codeaf too, and the supervisor needed no change at all.
const sweproSentinelEnv = "AFORGE_SWEPRO"

// sweproSentinel reports whether this process was started to be the engine.
// Only the exact value "1" counts: an operator who exports the variable to
// something else — "0", "", "true" — gets aforge, because a sentinel that
// answers to near-misses would swallow a normal invocation.
func sweproSentinel(environ func(string) string) bool {
	return environ(sweproSentinelEnv) == "1"
}

// dispatchSwepro runs the vendored engine and returns its exit code. It is
// the whole of the seam: everything downstream of it is swe-pro-go's, and
// nothing of aforge's — no config, no TUI, no store — has been touched yet.
func dispatchSwepro(argv []string) int { return codeaf.Main(argv) }
