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

// Models the person asked for are the working pool in place of the crew's
// working seat, kept as asked (`--asked`), and the light seat still summarises.
func TestTheModelsAPersonAskedForAreSeniorDevsWorkingPool(t *testing.T) {
	got := strings.Join(crewFlags(delegate.Crew{Hands: "vendor/hands", Light: "vendor/light", Asked: []string{"vendor/one", "vendor/two"}}), " ")
	if want := "--crew --asked --high openrouter/vendor/one,openrouter/vendor/two --low openrouter/vendor/light"; got != want {
		t.Fatalf("flags = %q, want %q", got, want)
	}
}

// The crew's effort rides with its working seat as senior-dev's own
// `--variant`, and a crew that names none leaves senior-dev on its default.
func TestTheCrewsEffortBecomesSeniorDevsVariant(t *testing.T) {
	got := strings.Join(crewFlags(delegate.Crew{Hands: "vendor/hands", Effort: "xhigh"}), " ")
	if want := "--crew --high openrouter/vendor/hands --variant xhigh"; got != want {
		t.Fatalf("flags = %q, want %q", got, want)
	}
	if got := strings.Join(crewFlags(delegate.Crew{Hands: "vendor/hands"}), " "); strings.Contains(got, "--variant") {
		t.Fatalf("a crew with no effort named one: %q", got)
	}
	if (delegate.Crew{Effort: "low"}).IsZero() {
		t.Fatal("a crew naming only an effort reads as no crew, so its flag is never passed")
	}
}
