package main

import (
	"strings"
	"testing"
)

const unnumberedBody = `---
kind: added
title: a change pushed without a pull request
surface: [engine]
invalidates: []
---
`

// A NEW ENTRY CARRIES ITS NUMBER. Only the names frozen in unnumbered.txt may
// go without one; any other entry without a pull request number is refused.
func TestANewEntryWithoutAPullRequestNumberIsRefused(t *testing.T) {
	_, errs := ParseEntry(write(t, t.TempDir(), "brand-new-change.md", unnumberedBody))
	joined := strings.Join(errsToStrings(errs), "\n")
	for _, want := range []string{"pr is missing", "has to be <pr>-<a-lowercase-slug>.md"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no complaint about %q in:\n%s", want, joined)
		}
	}
}

// A FROZEN NAME IS EXCUSED THE NUMBER AND NOTHING ELSE: the rest of the entry
// is still checked.
func TestAFrozenEntryIsExcusedOnlyItsNumber(t *testing.T) {
	name := frozenNames()[0]
	if _, errs := ParseEntry(write(t, t.TempDir(), name, unnumberedBody)); len(errs) != 0 {
		t.Fatalf("a frozen entry without a number was refused: %v", errs)
	}
	_, errs := ParseEntry(write(t, t.TempDir(), name, strings.Replace(unnumberedBody, "kind: added", "kind: tweaked", 1)))
	if joined := strings.Join(errsToStrings(errs), "\n"); !strings.Contains(joined, `kind is "tweaked"`) {
		t.Fatalf("a frozen entry escaped the other checks: %v", errs)
	}
}

// THE LIST ONLY SHRINKS. It was frozen at 128 names when the rule came in; a
// release roll-up consumes them, and nothing adds one.
func TestTheUnnumberedListOnlyShrinks(t *testing.T) {
	if n := len(frozenNames()); n == 0 || n > 128 {
		t.Fatalf("unnumbered.txt names %d entries; it was frozen at 128 and only shrinks", n)
	}
}

// A frozen entry rolls into the changelog without a link to a pull request
// that does not exist.
func TestAnUnnumberedEntryRendersWithoutALink(t *testing.T) {
	line := renderEntry(Entry{Kind: "added", Title: "pushed straight to the branch"})
	if strings.Contains(line, "/pull/") || strings.Contains(line, "#0") {
		t.Fatalf("an entry with no pull request links one: %q", line)
	}
}
