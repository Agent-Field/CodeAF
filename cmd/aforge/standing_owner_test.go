package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// ONE LIVE OWNER PER REPORT PATH, AT THE TERMINAL TOO (the review of 417fa43a3,
// B1). The chat refused a second order on a report file when it drew the card;
// `aforge standing add --report` asked nobody, and two live orders then took
// turns replacing each other's report on every run. The store refuses it now,
// at the write, whichever door asks — and a successor after a stop is not a
// second owner.
func TestTheTerminalCannotAddASecondOwnerOfAReport(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	workspace := t.TempDir()
	add := func(words, watch string) (string, error) {
		var out bytes.Buffer
		err := runStandingTo([]string{"add", "--words", words, "--instructions", "Summarise what changed.",
			"--watch", watch, "--report", "reports/digest.md", "--workspace", workspace}, &out)
		return out.String(), err
	}
	first, err := add("keep reports/digest.md current from inbox", "inbox/*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := add("keep reports/digest.md current from notes", "notes/*"); err == nil || !strings.Contains(err.Error(), "is already the report of") {
		t.Fatalf("a second live order on one report was set up (err %v)", err)
	}
	found := regexp.MustCompile(`set up ([0-9a-f]+):`).FindStringSubmatch(first)
	if found == nil {
		t.Fatalf("add printed no id: %q", first)
	}
	id := found[1]
	var out bytes.Buffer
	if err := runStandingTo([]string{"stop", id}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := add("keep reports/digest.md current from notes", "notes/*"); err != nil {
		t.Fatalf("a successor after a stop was refused: %v", err)
	}
}
