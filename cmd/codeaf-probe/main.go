// Command codeaf-probe is the coding-agent companion binary for testing
// CodeAF: one compact JSON object on stdout per invocation, human logs on
// stderr, non-zero exit on error.
package main

import (
	"os"

	"github.com/Agent-Field/codeaf/internal/probe"
)

func main() {
	if b := os.Getenv("CODEAF_PROBE_BASE"); b != "" { // test hook: pin the probe base for subprocess tests
		probe.ProbeBase = b
	}
	os.Exit(run(os.Args[1:]))
}
