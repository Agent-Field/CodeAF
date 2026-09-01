package config

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
)

// THE PROVENANCE HAS TO SURVIVE THE HANDOVER. [contextLaw] is the one seam
// between the settings registry, which knows whether a person set a row, and
// ctxbudget, which spends the number — and it used to hand over the fully
// resolved figure, so a fill nobody had touched arrived as a plain 60 and could
// never again be told from a 60 somebody typed. The conversation's compaction
// trigger needs exactly that distinction (internal/session's
// compactThresholdOf), so it is pinned here at the seam rather than only where
// it is read.
func TestAFillNobodyWroteDownIsNotAPin(t *testing.T) {
	dir := t.TempDir()

	ctxbudget.Configure(contextLaw(dir))
	t.Cleanup(func() { ctxbudget.Configure(ctxbudget.Limits{}) })

	percent, pinned := ctxbudget.PinnedFillPercent()
	if pinned {
		t.Fatal("a profile with nothing written down reports a pinned fill")
	}
	if percent != ctxbudget.DefaultFillPercent {
		t.Fatalf("an unpinned fill reads as %d, want the shipped %d", percent, ctxbudget.DefaultFillPercent)
	}
	// And the sheet still SHOWS the shipped figure, which is what a person needs
	// to see on the row; showing it is not the same as somebody having chosen it.
	if got := ContextFillAt(dir); got != ctxbudget.DefaultFillPercent {
		t.Fatalf("the row reads %d, want the shipped %d", got, ctxbudget.DefaultFillPercent)
	}
}

// The registry's own two ways of saying a person set the row — writing it down
// in the profile, and the environment variable the row names — both have to
// reach ctxbudget as a pin, because both are somebody's instruction.
func TestAFillAPersonSetIsAPinByEitherRoad(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { ctxbudget.Configure(ctxbudget.Limits{}) })

	rows := registry(t, dir)
	row, ok := rows.Row(KeyContextFill)
	if !ok {
		t.Fatalf("registry lost %s", KeyContextFill)
	}
	// The written road. Apply persists it AND hands the whole law down, so no
	// second call is needed here — that is the seam being pinned.
	if err := row.Apply("42"); err != nil {
		t.Fatalf("apply context fill: %v", err)
	}
	percent, pinned := ctxbudget.PinnedFillPercent()
	if !pinned || percent != 42 {
		t.Fatalf("a written-down fill reads as %d/%v, want 42/true", percent, pinned)
	}

	// The environment road, on a profile with nothing written down at all.
	ctxbudget.Configure(contextLaw(t.TempDir()))
	if _, pinned := ctxbudget.PinnedFillPercent(); pinned {
		t.Fatal("a second, untouched profile inherited the first one's pin")
	}
	t.Setenv("AFORGE_CONTEXT_FILL_PCT", "35")
	percent, pinned = ctxbudget.PinnedFillPercent()
	if !pinned || percent != 35 {
		t.Fatalf("an environment pin reads as %d/%v, want 35/true", percent, pinned)
	}
}

// The other three knobs travel the same seam and must keep answering what they
// always answered: a profile with nothing written down spends the shipped
// defaults, whatever the provenance says about them.
func TestAnUntouchedProfileStillSpendsTheShippedContextLaw(t *testing.T) {
	dir := t.TempDir()
	ctxbudget.Configure(contextLaw(dir))
	t.Cleanup(func() { ctxbudget.Configure(ctxbudget.Limits{}) })

	for _, one := range []struct {
		name string
		got  int
		want int
	}{
		{"fill", ctxbudget.FillPercent(), ctxbudget.DefaultFillPercent},
		{"completion reserve", ctxbudget.CompletionReserve(), ctxbudget.DefaultCompletionReserveTokens},
		{"working set", ctxbudget.WorkingSetCeiling(), ctxbudget.DefaultWorkingSetTokens},
		{"reuse", ctxbudget.ReusePercent(), ctxbudget.DefaultReusePercent},
	} {
		if one.got != one.want {
			t.Fatalf("%s reads %d on an untouched profile, want the shipped %d", one.name, one.got, one.want)
		}
	}
}
