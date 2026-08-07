package craft

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The craft repository is a real git repository, so the tests use a real git.
// A machine without one is a machine the resident cannot keep craft on, and
// that is worth skipping rather than faking.
func openRepo(t *testing.T) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return repo
}

func TestOpenLaysOutTheRepositoryAndSaysNothingTheSecondTime(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	repo, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, name := range []string{WorkflowDir, VerifierDir, SkillDir, ExemplarDir} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || !info.IsDir() {
			t.Fatalf("%s is not a directory: %v", name, err)
		}
	}
	first, err := repo.git("rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	if strings.TrimSpace(first) != "1" {
		t.Fatalf("a fresh repository has %s commits", strings.TrimSpace(first))
	}
	// The identity is the resident's, never the host's.
	who, err := repo.git("log", "-1", "--format=%an <%ae>")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if strings.TrimSpace(who) != commitAuthorName+" <"+commitAuthorEmail+">" {
		t.Fatalf("the initial commit was authored by %q", strings.TrimSpace(who))
	}
	if _, err := Open(dir); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	again, err := repo.git("rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	if strings.TrimSpace(again) != "1" {
		t.Fatalf("reopening wrote %s commits", strings.TrimSpace(again))
	}
}

func TestSaveLoadHistoryRevertKeepVersionIdentity(t *testing.T) {
	repo := openRepo(t)
	w := parseValid(t, presentationYAML)

	first, err := repo.Save(w, "first draft\n\ndistilled from job 41, job 44")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(first) != 40 {
		t.Fatalf("save returned %q", first)
	}

	loaded, err := repo.Load("presentation")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Commit != first {
		t.Fatalf("loaded at %s, saved at %s", loaded.Commit, first)
	}
	if loaded.Description != w.Description {
		t.Fatalf("loaded a different description: %q", loaded.Description)
	}
	// The load path clamps, so a reader can trust every number without
	// re-deriving the rails.
	if loaded.Steps[1].ForEach.Fan != 6 || loaded.Limits.WallClock == 0 {
		t.Fatalf("loaded unclamped: %+v", loaded.Limits)
	}

	// Saving what is already saved is not a new version.
	same, err := repo.Save(loaded, "no change at all")
	if err != nil {
		t.Fatalf("resave: %v", err)
	}
	if same != first {
		t.Fatalf("an unchanged save minted %s over %s", same, first)
	}

	loaded.Description = "Turn a topic into a presentation, now with a source check."
	second, err := repo.Save(loaded, "check sources before the draft\n\nafter job 52 shipped a deck with three wrong numbers")
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if second == first {
		t.Fatalf("a changed save reused %s", first)
	}

	history, err := repo.History("presentation", 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history has %d versions", len(history))
	}
	if history[0].Commit != second || history[1].Commit != first {
		t.Fatalf("history is not newest first: %+v", history)
	}
	if history[0].Subject != "presentation: check sources before the draft" {
		t.Fatalf("subject was %q", history[0].Subject)
	}
	if !strings.Contains(history[0].Body, "job 52") {
		t.Fatalf("the evidence did not survive into the body: %q", history[0].Body)
	}
	if history[0].When.IsZero() {
		t.Fatal("a version with no time")
	}

	old, err := repo.LoadAt("presentation", first)
	if err != nil {
		t.Fatalf("load at: %v", err)
	}
	if old.Commit != first || old.Description != w.Description {
		t.Fatalf("load at %s returned %s / %q", first, old.Commit, old.Description)
	}

	third, err := repo.Revert("presentation", first, "the source check doubled the cost and caught nothing")
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	back, err := repo.Load("presentation")
	if err != nil {
		t.Fatalf("load after revert: %v", err)
	}
	if back.Description != w.Description || back.Commit != third {
		t.Fatalf("revert landed %q at %s", back.Description, back.Commit)
	}
	history, err = repo.History("presentation", 0)
	if err != nil {
		t.Fatalf("history after revert: %v", err)
	}
	// A revert is a new version, never an erasure.
	if len(history) != 3 || history[1].Commit != second {
		t.Fatalf("revert rewrote history: %+v", history)
	}
	if !strings.Contains(history[0].Body, "restores "+first[:8]) {
		t.Fatalf("the revert did not record what it restored: %q", history[0].Body)
	}
}

func TestSaveRefusesWhatItShould(t *testing.T) {
	repo := openRepo(t)
	good := parseValid(t, presentationYAML)

	if _, err := repo.Save(good, "  "); err == nil || !strings.Contains(err.Error(), "empty message") {
		t.Fatalf("an empty message was accepted: %v", err)
	}

	broken := parseValid(t, presentationYAML)
	broken.Steps[2].Needs = []string{"outine"}
	_, err := repo.Save(broken, "break it")
	if err == nil || !strings.Contains(err.Error(), "refusing an invalid workflow") ||
		!strings.Contains(err.Error(), "did you mean outline?") {
		t.Fatalf("an invalid workflow was saved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Dir(), WorkflowDir, "presentation.yaml")); err == nil {
		t.Fatal("a refused save still wrote the file")
	}

	if _, err := repo.Save(good, "first"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := repo.Revert("presentation", "HEAD", "revert"); err == nil ||
		!strings.Contains(err.Error(), "has to say why") {
		t.Fatalf("a revert with no reason was accepted: %v", err)
	}
	if _, err := repo.Load("no-such-craft"); err == nil || !strings.Contains(err.Error(), "no workflow named no-such-craft") {
		t.Fatalf("loading a missing craft said %v", err)
	}
	if _, err := repo.Load("../../etc/passwd"); err == nil || !strings.Contains(err.Error(), "is not a workflow name") {
		t.Fatalf("a traversal name was accepted: %v", err)
	}
	if _, err := repo.LoadAt("presentation", "deadbeef"); err == nil ||
		!strings.Contains(err.Error(), "no such version of this workflow") {
		t.Fatalf("loading an unknown version said %v", err)
	}
}

func TestListSkipsTheCorruptFileAndKeepsTheRest(t *testing.T) {
	repo := openRepo(t)
	if _, err := repo.Save(parseValid(t, presentationYAML), "first draft"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := repo.Save(parseValid(t, changelogYAML), "first draft"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := writeCorrupt(repo); err != nil {
		t.Fatalf("write: %v", err)
	}

	summaries, err := repo.List()
	if err == nil || !strings.Contains(err.Error(), "skipped broken.yaml") {
		t.Fatalf("the corrupt file was not reported: %v", err)
	}
	// The listing is the surface the resident chooses from, so it stays whole.
	if len(summaries) != 2 || summaries[0].Name != "changelog" || summaries[1].Name != "presentation" {
		t.Fatalf("one bad file hid the rest: %+v", summaries)
	}
	for _, summary := range summaries {
		if summary.Commit == "" || summary.When.IsZero() || summary.Description == "" {
			t.Fatalf("summary is missing its version: %+v", summary)
		}
	}
}

func TestWriteVerifierAndSkillHoldThePathLaw(t *testing.T) {
	repo := openRepo(t)

	commit, err := repo.WriteVerifier("verifiers/deck_shape.sh", []byte("#!/bin/sh\nexit 0\n"), ScriptMode, "check every slide has notes")
	if err != nil {
		t.Fatalf("write verifier: %v", err)
	}
	info, err := os.Stat(filepath.Join(repo.Dir(), "verifiers", "deck_shape.sh"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("a verifier landed without its executable bit: %v", info.Mode())
	}
	subject, err := repo.git("log", "-1", "--format=%s", commit)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if strings.TrimSpace(subject) != "verifiers/deck_shape.sh: check every slide has notes" {
		t.Fatalf("subject was %q", strings.TrimSpace(subject))
	}

	// A bare name lands in the directory it belongs to.
	if _, err := repo.WriteSkill("deckbuild", []byte("#!/bin/sh\n"), 0, "build a deck from markdown"); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Dir(), "skills", "deckbuild")); err != nil {
		t.Fatalf("skill did not land: %v", err)
	}

	for _, probe := range []struct {
		path string
		want string
	}{
		{"../escape.sh", "must not climb out of verifiers/ with .."},
		{"verifiers/../../escape.sh", "must not climb out of verifiers/ with .."},
		{"/etc/cron.d/x", "must be repo-relative, not absolute"},
		{"skills/x.sh", `"skills/x.sh" must live under verifiers/`},
		{"", "no path given"},
	} {
		if _, err := repo.WriteVerifier(probe.path, []byte("x"), ScriptMode, "why"); err == nil ||
			!strings.Contains(err.Error(), probe.want) {
			t.Fatalf("path %q was answered with %v, wanted %q", probe.path, err, probe.want)
		}
	}
	if _, err := repo.WriteVerifier("x.sh", []byte("x"), ScriptMode, " "); err == nil ||
		!strings.Contains(err.Error(), "empty message") {
		t.Fatalf("an unexplained verifier was accepted: %v", err)
	}
}

// The git index is one file with no concurrency story, and the resident is one
// process with many arms. This is the test that fails loudly if the lock ever
// leaves.
func TestConcurrentSavesAreSerialized(t *testing.T) {
	repo := openRepo(t)
	const writers = 8

	// Parsed up front: each writer owns its own workflow, so the only thing
	// the goroutines share is the repository itself.
	drafts := make([]*Workflow, writers)
	for i := range drafts {
		drafts[i] = parseValid(t, presentationYAML)
		drafts[i].Name = fmt.Sprintf("deck-%d", i)
		drafts[i].Description = fmt.Sprintf("Deck number %d.", i)
	}

	var wait sync.WaitGroup
	commits := make([]string, writers)
	failures := make([]error, writers)
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			commits[i], failures[i] = repo.Save(drafts[i], fmt.Sprintf("first draft of deck %d", i))
		}(i)
	}
	wait.Wait()

	seen := map[string]bool{}
	for i, err := range failures {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
		if commits[i] == "" || seen[commits[i]] {
			t.Fatalf("writer %d landed on %q, already seen", i, commits[i])
		}
		seen[commits[i]] = true
	}
	status, err := repo.git("status", "--porcelain")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if strings.TrimSpace(status) != "" {
		t.Fatalf("the working tree is dirty after concurrent saves:\n%s", status)
	}
	if _, err := repo.git("fsck", "--no-progress"); err != nil {
		t.Fatalf("fsck: %v", err)
	}
	summaries, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != writers {
		t.Fatalf("%d of %d decks survived", len(summaries), writers)
	}
	for i := 0; i < writers; i++ {
		name := fmt.Sprintf("deck-%d", i)
		w, err := repo.Load(name)
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		if w.Description != fmt.Sprintf("Deck number %d.", i) {
			t.Fatalf("%s holds %q", name, w.Description)
		}
	}
}
