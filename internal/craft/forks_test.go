package craft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// git is a process fork of about six milliseconds, and the two surfaces that
// read the catalogue read it on a timer: retrieval runs on every user message,
// and the Self pane lists on every poll for as long as it is open. Both used to
// fork once per workflow file. These are the tests that they no longer do.

// Retrieval ranks on name, description and briefs. It does not need a version
// and must not pay a fork per file to fetch one.
func TestMatchNeverForksGit(t *testing.T) {
	repo := matchRepo(t)
	before := repo.forks.Load()

	for range 5 {
		found := repo.Match("make me a presentation about the Q3 numbers", DefaultMatches)
		if len(found) == 0 || found[0].Name != "presentation" {
			t.Fatalf("retrieval stopped working: %+v", found)
		}
	}
	if forks := repo.forks.Load() - before; forks != 0 {
		t.Fatalf("five user messages cost %d git forks", forks)
	}
}

// The catalogue is remembered against the state of the directory: unchanged
// files, same summaries, no git.
func TestListIsFreeWhileNothingChanges(t *testing.T) {
	repo := matchRepo(t)

	first, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("listed %d workflows, want 3", len(first))
	}
	before := repo.forks.Load()

	for range 20 { // twenty polls of an open Self pane
		again, err := repo.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != len(first) {
			t.Fatalf("the remembered listing has %d entries, want %d", len(again), len(first))
		}
		for index := range first {
			if again[index] != first[index] {
				t.Fatalf("entry %d came back as %+v, want %+v", index, again[index], first[index])
			}
		}
	}
	if forks := repo.forks.Load() - before; forks != 0 {
		t.Fatalf("twenty polls of an unchanged catalogue cost %d git forks", forks)
	}
}

// Remembering is only allowed if it cannot answer with a catalogue that has
// moved on. Every way the directory changes has to be seen.
func TestListSeesEveryChangeToTheDirectory(t *testing.T) {
	repo := matchRepo(t)
	if _, err := repo.List(); err != nil {
		t.Fatal(err)
	}

	// A saved revision: same file, new description.
	revised := parseValid(t, strings.Replace(presentationYAML,
		"description: Turn a topic into a presentation",
		"description: Turn a topic into a presentation, with a summary slide", 1))
	if _, err := repo.Save(revised, "add the summary slide"); err != nil {
		t.Fatalf("save: %v", err)
	}
	summaries, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 3 || summaries[2].Name != "presentation" {
		t.Fatalf("the catalogue is not what the test assumes: %+v", summaries)
	}
	if !strings.Contains(summaries[2].Description, "summary slide") {
		t.Fatalf("the listing did not see the revision: %+v", summaries[2])
	}

	// A file that was not there before.
	fresh := parseValid(t, strings.Replace(invoiceYAML, "name: invoice", "name: dispatch", 1))
	if _, err := repo.Save(fresh, "first draft"); err != nil {
		t.Fatalf("save: %v", err)
	}
	grown, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(grown) != 4 {
		t.Fatalf("the listing did not see the new workflow: %+v", grown)
	}

	// A file removed behind the repository's back, which is what a user
	// tidying the directory by hand looks like.
	if err := os.Remove(filepath.Join(repo.Dir(), WorkflowDir, "changelog.yaml")); err != nil {
		t.Fatal(err)
	}
	after, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range after {
		if summary.Name == "changelog" {
			t.Fatal("the listing still holds a workflow that is not there")
		}
	}
	if len(after) != 3 {
		t.Fatalf("listed %d workflows after the removal, want 3", len(after))
	}
}

// The listing used to be a fresh slice every time, so a caller is entitled to
// keep what it is given.
func TestListHandsBackACallersOwnCopy(t *testing.T) {
	repo := matchRepo(t)
	first, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	first[0] = Summary{Name: "scribbled on"}

	second, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Name == "scribbled on" {
		t.Fatal("a caller writing to its listing changed the remembered one")
	}
}

// The Self pane does not stop at the catalogue: it loads every workflow the
// catalogue names, for the step count, which is the one number a summary does
// not carry. Each load resolves a version — a `git log -1` and a
// `git status --porcelain` — so the pane used to cost two forks per workflow
// on every poll, on top of the listing. What follows is the shape of that
// pane, built the way cmd/aforge builds it.

type craftsEntry struct {
	Name        string
	Description string
	Commit      string
	When        time.Time
	Steps       int
}

func craftsPane(t *testing.T, repo *Repo) []craftsEntry {
	t.Helper()
	summaries, _ := repo.List()
	entries := make([]craftsEntry, 0, len(summaries))
	for _, summary := range summaries {
		entry := craftsEntry{
			Name:        summary.Name,
			Description: summary.Description,
			Commit:      summary.Commit,
			When:        summary.When,
		}
		if workflow, err := repo.Load(summary.Name); err == nil {
			entry.Steps = len(workflow.Steps)
		}
		entries = append(entries, entry)
	}
	return entries
}

func samePane(t *testing.T, got, want []craftsEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the pane has %d rows, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("row %d came back as %+v, want %+v", index, got[index], want[index])
		}
	}
}

// An open Self pane polls four hundred milliseconds apart for as long as the
// user leaves it open. Nothing about an untouched catalogue changes between two
// of those, so nothing about it should cost a process.
func TestSelfPaneIsFreeWhileNothingChanges(t *testing.T) {
	repo := matchRepo(t)

	first := craftsPane(t, repo)
	if len(first) != 3 {
		t.Fatalf("the pane has %d rows, want 3", len(first))
	}
	for _, entry := range first {
		if entry.Steps == 0 {
			t.Fatalf("%s came back with no steps: %+v", entry.Name, entry)
		}
		if entry.Commit == "" {
			t.Fatalf("%s came back with no version: %+v", entry.Name, entry)
		}
	}
	before := repo.forks.Load()

	for range 20 { // twenty polls of an open Self pane
		samePane(t, craftsPane(t, repo), first)
	}
	if forks := repo.forks.Load() - before; forks != 0 {
		t.Fatalf("twenty polls of an unchanged pane cost %d git forks", forks)
	}
}

// Remembering a version is only allowed if it cannot outlive the file it
// describes — and it has to be remembered per workflow, or one edit would make
// the whole catalogue pay again.
func TestLoadRefetchesOnlyTheWorkflowThatChanged(t *testing.T) {
	repo := matchRepo(t)
	names := []string{"changelog", "invoice", "presentation"}
	poll := func() {
		t.Helper()
		for _, name := range names {
			if _, err := repo.Load(name); err != nil {
				t.Fatalf("load %s: %v", name, err)
			}
		}
	}

	poll()
	before := repo.forks.Load()
	poll()
	if forks := repo.forks.Load() - before; forks != 0 {
		t.Fatalf("reloading three unchanged workflows cost %d git forks", forks)
	}

	// One workflow edited under the repository's back, which is what a user
	// with the file open in an editor looks like.
	edited := strings.Replace(invoiceYAML,
		"brief: Apply the agreed rate to each line and total it.",
		"brief: Apply the agreed rate to each line and total it, rounded to the cent.", 1)
	if err := os.WriteFile(filepath.Join(repo.Dir(), WorkflowDir, "invoice.yaml"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	before = repo.forks.Load()
	poll()
	if forks := repo.forks.Load() - before; forks != 2 {
		t.Fatalf("one edited workflow cost %d git forks, want 2 — the log and the status for that file alone", forks)
	}

	// And what comes back is the edit, at a version that says it is one.
	workflow, err := repo.Load("invoice")
	if err != nil {
		t.Fatalf("load invoice: %v", err)
	}
	if !strings.Contains(workflow.Steps[1].Brief, "rounded to the cent") {
		t.Fatalf("the remembered version outlived the edit: %+v", workflow.Steps[1])
	}
	if !strings.Contains(workflow.Commit, "+dirty-") {
		t.Fatalf("an uncommitted edit was named %s, which reads as a saved version", workflow.Commit)
	}
}

// A workflow file dropped into the directory by hand and never saved has no
// version, so Load refuses it and the pane shows the row without a step count.
// That is what it did before it was remembered, and remembering a refusal must
// not change a character of it.
func TestSelfPaneShowsANeverCommittedWorkflowTheSameWayTwice(t *testing.T) {
	repo := matchRepo(t)
	dropped := strings.Replace(invoiceYAML, "name: invoice", "name: dropped", 1)
	if err := os.WriteFile(filepath.Join(repo.Dir(), WorkflowDir, "dropped.yaml"), []byte(dropped), 0o644); err != nil {
		t.Fatal(err)
	}

	// A second repository over the same directory remembers nothing, so this is
	// the pane as it reads with every answer resolved from scratch.
	cold, err := Open(repo.Dir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	want := craftsPane(t, cold)
	if len(want) != 4 {
		t.Fatalf("the pane has %d rows, want 4", len(want))
	}
	var found bool
	for _, entry := range want {
		if entry.Name != "dropped" {
			continue
		}
		found = true
		if entry.Steps != 0 || entry.Commit != "" {
			t.Fatalf("a workflow that was never committed read as %+v", entry)
		}
	}
	if !found {
		t.Fatalf("the hand-dropped workflow is not in the pane: %+v", want)
	}

	samePane(t, craftsPane(t, repo), want)

	before := repo.forks.Load()
	for range 20 {
		samePane(t, craftsPane(t, repo), want)
	}
	if forks := repo.forks.Load() - before; forks != 0 {
		t.Fatalf("twenty polls over a never-committed workflow cost %d git forks", forks)
	}
}

// What Load hands back used to be built fresh every call, and its callers fill
// parameters into it, compile it into a subtree, and hand it on to Save.
func TestLoadHandsBackACallersOwnWorkflow(t *testing.T) {
	repo := matchRepo(t)
	first, err := repo.Load("invoice")
	if err != nil {
		t.Fatal(err)
	}
	first.Description = "scribbled on"
	first.Steps[0].Brief = "scribbled on"
	first.Steps[1].Needs[0] = "scribbled on"

	second, err := repo.Load("invoice")
	if err != nil {
		t.Fatal(err)
	}
	if second.Description == "scribbled on" || second.Steps[0].Brief == "scribbled on" {
		t.Fatal("a caller writing to its workflow changed the remembered one")
	}
	if second.Steps[1].Needs[0] == "scribbled on" {
		t.Fatal("a caller writing to a step's needs changed the remembered one")
	}
}
