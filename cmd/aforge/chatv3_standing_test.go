package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// THE STORE IS UNDER THE STATE ROOT AND NOWHERE ELSE, so AFORGE_HOME moves the
// ambient side with everything else it moves. A second spelling of this path
// would be two stores with half a person's reminders in each.
func TestStandingLivesUnderTheStateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("AFORGE_HOME", root)
	if got, want := v3StandingRoot(), home.Join("v3", "standing"); got != want {
		t.Fatalf("standing root = %q, want %q", got, want)
	}
	if !strings.HasPrefix(v3StandingRoot(), root) {
		t.Fatalf("standing root %q is outside the state root %q", v3StandingRoot(), root)
	}
}

// THE SEAM IS BUILT FOR A CONVERSATION AND THE STORE IS THE ONE AT THAT PATH.
// A door that could not open it hands over nil, which every caller reads as the
// ambient side being off — the absence law, not a broken tool.
func TestStandingSeamOpensTheStoreAtThatPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("AFORGE_HOME", root)
	seam := v3Standing(t.TempDir())
	if seam == nil || seam.Store == nil {
		t.Fatal("the door built no standing seam")
	}
	if seam.Store.Root() != v3StandingRoot() {
		t.Fatalf("the seam's store is at %q, want %q", seam.Store.Root(), v3StandingRoot())
	}
	// The daily rail is the person's own daily budget row and never a second
	// number invented for this.
	if seam.DailyRailUSD != v3StandingDailyRail(t.TempDir()) {
		t.Fatalf("the seam quotes %v as the daily rail", seam.DailyRailUSD)
	}
}

// THE OFFER IS NEVER MADE WHILE THERE IS NO TIMER TO INSTALL. standingWatch is
// the core lane's seam and answers nil until it lands; a card that offered to
// keep checking with nothing behind the yes would be a promise this build
// cannot keep.
