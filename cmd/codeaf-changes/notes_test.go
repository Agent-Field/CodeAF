package main

import (
	"fmt"
	"strings"
	"testing"
)

// A changelog with two versions, so a section's end is the next heading and
// not the end of the file.
func changelogWith(section string) string {
	return "# Changelog\n\n" + insertMarker + "\n\n## v0.2.0 — 2026-09-15\n\n" + section + "\n\n## v0.1.0 — 2026-08-17\n\nThe first one.\n"
}

func TestNotesAreTheWholeSectionWhenItFits(t *testing.T) {
	section := "### Fixed\n\n- **a small thing** — [#7](https://github.com/Agent-Field/codeaf/pull/7)\n"
	got, err := Notes(changelogWith(section), "v0.2.0", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.TrimSpace(section)+"\n" {
		t.Fatalf("a section that fits must be printed as written:\n%q", got)
	}
	if strings.Contains(got, "The first one") {
		t.Fatal("the notes ran past the next version's heading")
	}
	if strings.Contains(got, "CHANGELOG.md") {
		t.Fatal("a whole section must not say it was shortened")
	}
}

// v0.2.0 must not answer for v0.2.0-rc.1, and a missing section is a refusal
// the workflow reads — nothing on stdout, so it falls back to generated notes.
func TestNotesRefuseAMissingSectionAndAPrefixMatch(t *testing.T) {
	changelog := "# Changelog\n\n## v0.2.0-rc.1 — 2026-09-15\n\n- **rc only**\n"
	if _, err := Notes(changelog, "v0.2.0", 1000); err == nil {
		t.Fatal("v0.2.0 matched the v0.2.0-rc.1 section")
	}
	if _, err := Notes("# Changelog\n", "v0.2.0", 1000); err == nil {
		t.Fatal("a changelog with no section answered anyway")
	}
}

// THE HEADLINE LAYER SURVIVES WHOLE WHEN THE FOLDS DO NOT FIT. Every entry's
// one visible line and every kind heading is still on the release page; only
// the folds are gone, and the page says where they are.
func TestNotesDropTheFoldsBeforeTheyDropAnEntry(t *testing.T) {
	entries := make([]Entry, 0, 30)
	for i := 1; i <= 30; i++ {
		entries = append(entries, Entry{Kind: "fixed", Title: fmt.Sprintf("entry number %d", i), PR: i,
			Invalidates: []string{strings.Repeat("A claim that was true and is not any more. ", 20)}})
	}
	rendered := Render("v0.2.0", "2026-09-15", entries)
	body := strings.TrimPrefix(rendered, "## v0.2.0 — 2026-09-15\n")
	changelog := changelogWith(body)
	if len(body) < 20_000 {
		t.Fatalf("the fixture is too small to force the fold layer off: %d bytes", len(body))
	}

	got, err := Notes(changelog, "v0.2.0", 8_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 8_000 {
		t.Fatalf("notes are %d bytes, over the cap", len(got))
	}
	if strings.Contains(got, "<details>") || strings.Contains(got, "A claim that was true") {
		t.Fatalf("a fold survived:\n%s", got)
	}
	for i := 1; i <= 30; i++ {
		if !strings.Contains(got, fmt.Sprintf("- **entry number %d** — [#%d]", i, i)) {
			t.Fatalf("entry %d lost its headline:\n%s", i, got)
		}
	}
	if !strings.Contains(got, "### Fixed") {
		t.Fatalf("the kind heading is gone:\n%s", got)
	}
	if !strings.HasSuffix(got, notesTail("v0.2.0")+"\n") {
		t.Fatalf("a shortened body must end by saying where the rest is:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("removing the folds left holes in the list:\n%s", got)
	}
}

// WHEN EVEN THE HEADLINES DO NOT FIT, THE CUT IS AT A LINE AND THE TAIL IS
// STILL LAST: a headline is never split mid-word and the page still names
// the record.
func TestNotesCutAtALineBoundaryWhenTheHeadlinesDoNotFit(t *testing.T) {
	entries := make([]Entry, 0, 200)
	for i := 1; i <= 200; i++ {
		entries = append(entries, Entry{Kind: "fixed", Title: fmt.Sprintf("entry number %d with a long enough title to matter", i), PR: i})
	}
	rendered := Render("v0.2.0", "2026-09-15", entries)
	body := strings.TrimPrefix(rendered, "## v0.2.0 — 2026-09-15\n")

	const capBytes = 2_000
	got, err := Notes(changelogWith(body), "v0.2.0", capBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > capBytes {
		t.Fatalf("notes are %d bytes, over the cap of %d", len(got), capBytes)
	}
	if !strings.HasSuffix(got, notesTail("v0.2.0")+"\n") {
		t.Fatalf("the tail is not last:\n%s", got)
	}
	kept := strings.Split(strings.TrimSpace(strings.TrimSuffix(got, notesTail("v0.2.0")+"\n")), "\n")
	for _, line := range kept {
		if strings.HasPrefix(line, "- **") && !strings.Contains(line, "pull/") {
			t.Fatalf("a headline was cut mid-line: %q", line)
		}
	}
	if !strings.Contains(got, "- **entry number 1 with") {
		t.Fatalf("the first entry did not survive the cut:\n%s", got)
	}
}

// THE SHIPPED CAP LEAVES ROOM UNDER GITHUB'S CEILING. GitHub refuses bodies
// over 125,000 characters; the workflow appends an install paragraph after
// these notes; and bytes over-count characters, never under.
func TestTheShippedCapSitsUnderGitHubsCeiling(t *testing.T) {
	if notesCapBytes >= 125_000 || notesCapBytes < 100_000 {
		t.Fatalf("notesCapBytes is %d; it must leave room for the install paragraph under 125,000 and not be so small the page is useless", notesCapBytes)
	}
}
