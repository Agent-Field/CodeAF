package craft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
