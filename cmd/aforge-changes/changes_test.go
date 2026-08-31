package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const good = `---
kind: renamed
title: dev is the trunk and chat-v3-task is gone
pr: 82
surface: [build, docs]
invalidates:
  - "The trunk was ` + "`chat-v3-task`" + `; it no longer exists and work goes to dev."
---

Because a branch name in a standing order is the kind of thing nobody re-reads.
`

func TestAWellFormedEntryParses(t *testing.T) {
	dir := t.TempDir()
	e, errs := ParseEntry(write(t, dir, "82-the-trunk-moved.md", good))
	if len(errs) > 0 {
		t.Fatalf("expected no complaints, got %v", errs)
	}
	if e.Kind != "renamed" || e.PR != 82 {
		t.Fatalf("frontmatter did not land: %+v", e)
	}
	if len(e.Surface) != 2 || len(e.Invalidates) != 1 {
		t.Fatalf("lists did not land: %+v", e)
	}
	if !strings.HasPrefix(e.Body, "Because a branch name") {
		t.Fatalf("body did not land: %q", e.Body)
	}
}

// EVERY FAULT AT ONCE, NOT THE FIRST ONE. A gate that reveals one problem per
// run is a gate somebody bounces off four times and then works around, so this
// asserts the count as well as the content.
func TestABadEntryIsToldEverythingThatIsWrongWithIt(t *testing.T) {
	dir := t.TempDir()
	bad := `---
kind: tweaked
title: This one ends in a full stop.
pr: 7
surface: [tui3]
invalidates:
  - "branch names"
---
`
	_, errs := ParseEntry(write(t, dir, "9-wrong-number.md", bad))
	joined := strings.Join(errsToStrings(errs), "\n")

	for _, want := range []string{
		`kind is "tweaked"`,
		"full stop",
		"named for pull request 9 and the frontmatter says 7",
		`surface "tui3" is not one of`,
		"label rather than a claim",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("no complaint about %q in:\n%s", want, joined)
		}
	}
}

func TestAnInvalidationHasToBeAWholeClaim(t *testing.T) {
	dir := t.TempDir()
	// Long enough, but a fragment: no full stop, so it is not a claim anybody
	// can check their memory against.
	body := strings.Replace(good, `"The trunk was `+"`chat-v3-task`"+`; it no longer exists and work goes to dev."`,
		`"the trunk moved somewhere else entirely"`, 1)
	_, errs := ParseEntry(write(t, dir, "82-the-trunk-moved.md", body))
	if !strings.Contains(strings.Join(errsToStrings(errs), "\n"), "does not end in a full stop") {
		t.Fatalf("a fragment was accepted: %v", errs)
	}
}

func errsToStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

// THE TWO LAYERS ARE THE WHOLE FORMAT, so they are asserted rather than assumed:
// a headline that is always one visible line, and detail that is always exactly
// one fold away — and NO fold at all when there is nothing behind it.
func TestRenderKeepsTheHeadlineVisibleAndTheClaimsOneFoldAway(t *testing.T) {
	out := Render("v0.2.0", "2026-09-05", []Entry{
		{Kind: "changed", Title: "the loud one", PR: 82, Surface: []string{"build"},
			Invalidates: []string{"A branch push used to publish a release; only a tag does now."}},
		{Kind: "changed", Title: "the quiet one", PR: 83},
	})

	if !strings.Contains(out, "## v0.2.0 — 2026-09-05") || !strings.Contains(out, "### Changed") {
		t.Fatalf("headings missing:\n%s", out)
	}
	if !strings.Contains(out, "- **the loud one** — [#82](https://github.com/Agent-Field/aforge-v2/pull/82) · `build`") {
		t.Fatalf("headline is not one plain line:\n%s", out)
	}
	if !strings.Contains(out, "<details><summary>1 thing that is no longer true</summary>") {
		t.Fatalf("the fold is missing or misworded:\n%s", out)
	}
	// An entry with nothing behind it must not grow an empty triangle.
	quiet := out[strings.Index(out, "the quiet one"):]
	if strings.Contains(quiet, "<details>") {
		t.Fatalf("an entry with nothing to disclose grew a fold:\n%s", quiet)
	}
}

func TestRenderGroupsByKindInTheDeclaredOrder(t *testing.T) {
	out := Render("v9.9.9", "2026-01-01", []Entry{
		{Kind: "internal", Title: "last", PR: 3},
		{Kind: "added", Title: "first", PR: 1},
		{Kind: "fixed", Title: "middle", PR: 2},
	})
	added, fixed, internal := strings.Index(out, "### Added"), strings.Index(out, "### Fixed"), strings.Index(out, "### Internal")
	if !(added < fixed && fixed < internal) {
		t.Fatalf("kinds are out of order:\n%s", out)
	}
}

func TestInsertNeedsItsMarker(t *testing.T) {
	if _, err := Insert("# Changelog\n\n## v0.1.0\n", "## v0.2.0\n"); err == nil {
		t.Fatal("inserting into a changelog with no marker should fail rather than guess")
	}
	got, err := Insert("# Changelog\n\n"+insertMarker+"\n\n## v0.1.0 — old\n", "## v0.2.0 — new\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, "v0.2.0") > strings.Index(got, "v0.1.0") {
		t.Fatalf("the new version did not land above the old one:\n%s", got)
	}
}

// THE GATE POINTED AT THE REAL TREE. CI runs `aforge-changes check`, but a
// developer running `go test ./...` finds a malformed entry too — the same law
// enforced from both directions, which is how the manual corpus is guarded.
func TestTheEntriesInThisRepositoryAreWellFormed(t *testing.T) {
	dir := filepath.Join("..", "..", defaultDir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("%s does not exist", dir)
	}
	if _, errs := LoadDir(dir); len(errs) > 0 {
		t.Fatalf("docs/changes/unreleased is not clean:\n  %s", strings.Join(errsToStrings(errs), "\n  "))
	}
}

func TestTheChangelogStillCarriesItsInsertionMarker(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", changelogFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), insertMarker) {
		t.Fatalf("CHANGELOG.md lost the %q line, so `aforge-changes roll` has nowhere to insert", insertMarker)
	}
}
