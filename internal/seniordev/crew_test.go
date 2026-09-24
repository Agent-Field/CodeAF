//go:build !windows

package seniordev

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// The crew reaches senior-dev as its own flags: the working seat is the pool
// it routes on, the light seat its summaries, and a seat left unset keeps
// senior-dev's own default. The planning seat is not passed: no call senior-dev
// makes rides the tier it would set.
func TestTheCrewBecomesSeniorDevsOwnPools(t *testing.T) {
	got := strings.Join(crewFlags(delegate.Crew{Brain: "vendor/brain", Hands: "vendor/hands", Light: "vendor/light"}), " ")
	if want := "--crew --high openrouter/vendor/hands --low openrouter/vendor/light"; got != want {
		t.Fatalf("flags = %q, want %q", got, want)
	}
	if got := strings.Join(crewFlags(delegate.Crew{Hands: "vendor/hands"}), " "); got != "--crew --high openrouter/vendor/hands" {
		t.Fatalf("flags for a crew with one seat = %q", got)
	}
}
