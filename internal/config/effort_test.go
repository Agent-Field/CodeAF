package config

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// Missing settings use the provider default; explicit choices survive a write.
func TestTheInstallRungDefaultsToTheShippedOneAndSurvivesAWrite(t *testing.T) {
	dir := t.TempDir()
	if got := DefaultEffortAt(dir); got != effort.Ship {
		t.Fatalf("an unconfigured profile reads %q, want the shipped %q", got, effort.Ship)
	}

	for _, rung := range append(append([]effort.Rung{}, effort.Rungs...), effort.None) {
		if err := WriteDefaultEffort(dir, rung); err != nil {
			t.Fatalf("WriteDefaultEffort(%q): %v", rung, err)
		}
		if got := DefaultEffortAt(dir); got != rung {
			t.Fatalf("wrote %q and read back %q", rung, got)
		}
	}

	// A word that is not a rung is refused at the writer rather than landing on
	// disk and reading back as the shipped default forever — which is
	// indistinguishable from the write having been ignored.
	if err := WriteDefaultEffort(dir, effort.Rung("deepest")); err == nil {
		t.Fatal("a word that is not a rung was written")
	}
	if got := DefaultEffortAt(dir); got != effort.None {
		t.Fatalf("the refused write moved the row to %q, want the off it was left at", got)
	}
}

// The settings row and the ladder are one list: a rung added or dropped moves
// the sheet with it rather than leaving a choice nothing can parse.
func TestTheSettingsRowOffersExactlyTheLadderAndAuto(t *testing.T) {
	if len(EffortChoices) != len(effort.Rungs)+1 {
		t.Fatalf("the row offers %v; the ladder is %v plus auto", EffortChoices, effort.Rungs)
	}
	if EffortChoices[0] != "auto" {
		t.Fatalf("the row's first choice is %q, want auto — an empty first option reads as broken",
			EffortChoices[0])
	}
	for _, choice := range EffortChoices {
		if _, ok := effort.Parse(choice); !ok {
			t.Fatalf("the row offers %q, which the ladder cannot parse", choice)
		}
	}
	if got := EffortWord(effort.None); got != "auto" {
		t.Fatalf("EffortWord(absence) = %q, want auto", got)
	}
}

// Old profiles must keep their meaning without requiring a migration.
func TestLegacyOffReadsAsAutoAndExplicitHighSurvives(t *testing.T) {
	dir := t.TempDir()
	if err := writeChoice(dir, KeyEffort, "off", []string{"off"}); err != nil {
		t.Fatal(err)
	}
	if got := EffortWord(DefaultEffortAt(dir)); got != "auto" {
		t.Fatalf("legacy off reads %q, want auto", got)
	}
	if err := WriteDefaultEffort(dir, effort.High); err != nil {
		t.Fatal(err)
	}
	if got := DefaultEffortAt(dir); got != effort.High {
		t.Fatalf("explicit high was replaced with %q", got)
	}
}
