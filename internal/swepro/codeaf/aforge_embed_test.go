// aforge-embed: added file. It tests the two embedding patches in main.go and
// nothing else; the vendored suite is untouched.
package codeaf

import (
	"fmt"
	"testing"
)

// The gate keeps its upstream shape for every value but the new one.
func TestAforgeEmbedControlPlaneGateKeepsUpstreamShapeExceptOff(t *testing.T) {
	real := backend(nil)
	fake := backend(&openRouterBackend{})
	for _, testCase := range []struct {
		name     string
		baseURL  string
		injected backend
		want     bool
	}{
		// Upstream rows: the shipped binary always gated; an injected
		// backend gated only when CODEAF_CP_URL named a plane.
		{"real backend, unset", "", real, true},
		{"real backend, set", "http://localhost:8080", real, true},
		{"injected backend, unset", "", fake, false},
		{"injected backend, set", "http://localhost:8080", fake, true},
		// The patch: "off" is the one value that refuses the gate, and it
		// refuses it for the real backend — the embedded case.
		{"real backend, off", "off", real, false},
		{"injected backend, off", "off", fake, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := controlPlaneEnabled(testCase.baseURL, testCase.injected); got != testCase.want {
				t.Fatalf("controlPlaneEnabled(%q, %v) = %v, want %v",
					testCase.baseURL, testCase.injected != nil, got, testCase.want)
			}
		})
	}
}

// The auto-resume supervisor re-execs os.Executable() with an argv it builds
// itself (runAutoResume, main.go). Embedded, os.Executable() is the aforge
// binary and the inherited AFORGE_SWEPRO=1 turns it back into codeaf — which
// is only correct if Main's own parser accepts that argv. This asserts it
// does, on the argv runAutoResume produces with every optional flag set.
func TestAforgeEmbedSupervisorResumeArgvIsAcceptedByMain(t *testing.T) {
	maxCost, maxHours := 12.5, 3.0
	args := cliArgs{
		High: "vendor/high", Low: "vendor/low", Frontier: "vendor/frontier",
		Variant: "high", Format: "json",
		PRReady: true, Hard: true, MaxCost: &maxCost, MaxHours: &maxHours,
	}
	const workspace = "/tmp/aforge-embed-workspace"

	// Kept in step with runAutoResume's ResumeOnce; if that argv changes and
	// this copy does not, one of the two assertions below fails.
	childArgs := []string{
		"resume", "--dir", workspace, "--high", args.High,
		"--low", args.Low, "--frontier", args.Frontier,
		"--variant", args.Variant, "--format", args.Format,
	}
	if args.PRReady {
		childArgs = append(childArgs, "--pr-ready")
	}
	if args.Hard {
		childArgs = append(childArgs, "--hard")
	}
	if args.MaxCost != nil {
		childArgs = append(childArgs, "--max-cost", fmt.Sprint(*args.MaxCost))
	}
	if args.MaxHours != nil {
		childArgs = append(childArgs, "--max-hours", fmt.Sprint(*args.MaxHours))
	}

	parsed, err := parseArgs(childArgs)
	if err != nil {
		t.Fatalf("supervisor argv rejected by parseArgs: %v", err)
	}
	if parsed.Command != "resume" || parsed.Directory != workspace ||
		parsed.High != args.High || parsed.Low != args.Low ||
		parsed.Frontier != args.Frontier || parsed.Variant != args.Variant ||
		parsed.Format != args.Format || !parsed.PRReady || !parsed.Hard {
		t.Fatalf("supervisor argv parsed to %#v", parsed)
	}
	if parsed.MaxCost == nil || *parsed.MaxCost != maxCost ||
		parsed.MaxHours == nil || *parsed.MaxHours != maxHours {
		t.Fatalf("budget ceilings lost: %#v", parsed)
	}
	// A resume argv carries no positional message; the goal is rehydrated
	// from the checkpoint. Main must not read that as a missing argument at
	// parse time.
	if parsed.Message != "" {
		t.Fatalf("resume argv carried a message: %q", parsed.Message)
	}
}
