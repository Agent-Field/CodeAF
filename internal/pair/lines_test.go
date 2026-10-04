package pair

import "testing"

// A pairing that reached no workspaces says only that it paired: a count of
// zero is never printed.
func TestJoinedFleetLineNeverPrintsZero(t *testing.T) {
	for workspaces, want := range map[int]string{
		0: "Paired.", 1: "Paired. 1 workspace available.", 3: "Paired. 3 workspaces available.",
	} {
		if got := JoinedFleetLine(workspaces); got != want {
			t.Errorf("JoinedFleetLine(%d) = %q, want %q", workspaces, got, want)
		}
	}
}

// The approving terminal names a system the way a person does, and leaves out
// one it cannot name rather than showing a raw word such as darwin.
func TestWantsToJoinLineNamesTheSystem(t *testing.T) {
	for platform, want := range map[string]string{
		"darwin": "laptop (Mac) wants to pair.",
		"linux":  "laptop (Linux) wants to pair.",
		"plan9":  "laptop wants to pair.",
		"":       "laptop wants to pair.",
	} {
		if got := WantsToJoinLine("laptop", platform); got != want {
			t.Errorf("WantsToJoinLine(%q) = %q, want %q", platform, got, want)
		}
	}
}
