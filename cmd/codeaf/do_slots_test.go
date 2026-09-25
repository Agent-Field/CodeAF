package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ONE ROW ANSWERS HOW MANY WORKERS RUN AT ONCE, and `codeaf do` reads it the
// way the chat door does. The door used to carry a constant of its own, four,
// so a person who had set `task.parallel` found this command ignoring it and a
// person who had set nothing got a bound the setting said did not exist.
func TestCodeafDoRunsAsManyWorkersAsTaskParallelSays(t *testing.T) {
	dir := t.TempDir()
	if got := (doRequest{}).slotsFor(dir); got != config.DefaultTaskParallel {
		t.Fatalf("an unset row resolves %d slots, want the setting's own default %d (no limit)", got, config.DefaultTaskParallel)
	}
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir: dir,
		ModelValue: func(slot string) string { return slot + "/model" },
		SetModel:   func(string, string) error { return nil },
		SplitPct:   func() int { return 0 },
	})
	row, found := registry.Row(config.KeyTaskParallel)
	if !found {
		t.Fatalf("no settings row %q", config.KeyTaskParallel)
	}
	if err := row.Apply("3"); err != nil {
		t.Fatal(err)
	}
	if got := (doRequest{}).slotsFor(dir); got != 3 {
		t.Fatalf("a row of 3 resolves %d slots for codeaf do, want 3", got)
	}
	// A figure named on the command outranks the row, and a named 0 is no
	// bound rather than "unset": the two are different requests.
	if got := (doRequest{slots: bound(2)}).slotsFor(dir); got != 2 {
		t.Fatalf("--slots 2 resolves %d, want 2 over a row of 3", got)
	}
	if got := (doRequest{slots: bound(0)}).slotsFor(dir); got != 0 {
		t.Fatalf("--slots 0 resolves %d, want 0 (no bound) over a row of 3", got)
	}
}

// The flag is read as text so that a blank and a zero stay two different
// things; everything that is not a whole number of workers is refused with the
// value it was handed.
func TestTheSlotsFlagTellsBlankFromZeroAndRefusesTheRest(t *testing.T) {
	if got, err := parseSlots(""); err != nil || got != nil {
		t.Fatalf("blank parses (%v, %v), want (nil, nil): the flag was not given", got, err)
	}
	if got, err := parseSlots(" 0 "); err != nil || got == nil || *got != 0 {
		t.Fatalf("0 parses (%v, %v), want a named zero", got, err)
	}
	if got, err := parseSlots("12"); err != nil || got == nil || *got != 12 {
		t.Fatalf("12 parses (%v, %v), want 12", got, err)
	}
	for _, bad := range []string{"-1", "four", "1.5"} {
		if _, err := parseSlots(bad); err == nil {
			t.Fatalf("--slots %q was accepted, want a refusal", bad)
		}
	}
}
