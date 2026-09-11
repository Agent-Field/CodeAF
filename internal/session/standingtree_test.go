package session

// WHERE YOU STAND, YOU WRITE; WHERE YOU REFER, THE WORK IS KEPT AND LANDED.
//
// These are written from the person's side of the promise: their own folder
// does not move until they say so, and the model's work is not lost while it
// waits. So every assertion below is either "the real folder still says what it
// said" or "the work came home whole".

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// standingLab is a conversation standing in one directory and referring to
// another — the shape this whole file is about, built once.
func standingLab(t *testing.T, folder string) (*Agent, string, string) {
	t.Helper()
	place := t.TempDir()
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(place, "session.jsonl")
		config.Place = Place{Dir: place}
	})
	if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	return agent, workspace, place
}

// writeThrough is the model's own hand: the belt's write, called with the path
// the model would name — the real one, inside the folder it was told about.
func writeThrough(t *testing.T, agent *Agent, path, content string) string {
	t.Helper()
	arguments, err := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{path, content})
	if err != nil {
		t.Fatal(err)
	}
	text, isError := runTool(t, agent, "write", string(arguments))
	if isError {
		t.Fatalf("write %s: %s", path, text)
	}
	return text
}

// readThrough is the same for read.
func readThrough(t *testing.T, agent *Agent, path string) string {
	t.Helper()
	arguments, err := json.Marshal(struct {
		Path string `json:"path"`
	}{path})
	if err != nil {
		t.Fatal(err)
	}
	text, isError := runTool(t, agent, "read", string(arguments))
	if isError {
		t.Fatalf("read %s: %s", path, text)
	}
	return text
}

// THE FIRST WRITE CUTS ONE COPY AND EVERY LATER ONE REUSES IT, and the person's
// own checkout does not move while that happens. This is the whole standing
// default in one test.
func TestTheFirstWriteIntoAReferredFolderCutsOneCopyAndTheSecondReusesIt(t *testing.T) {
	repo := newTestRepo(t)
	agent, _, _ := standingLab(t, repo)

	said := writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the changed line\n")
	if !strings.Contains(said, "/land") {
		t.Fatalf("the first write says nothing about where it went:\n%s", said)
	}
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "the original line\n" {
		t.Fatalf("the person's own file says %q — nothing may reach it before the landing", got)
	}
	trees := agent.StandingTrees()
	if len(trees) != 1 {
		t.Fatalf("one write made %d working copies", len(trees))
	}
	if trees[0].Mode != TaskModeWorktree || trees[0].Branch == "" {
		t.Fatalf("a repository was not given a branch: %+v", trees[0])
	}
	if got := readFile(t, filepath.Join(trees[0].Dir, "shared.txt")); got != "the changed line\n" {
		t.Fatalf("the copy holds %q", got)
	}

	// The second write goes to the same copy, and says nothing again: the model
	// has been told, and telling it twice is the noise this file is careful of.
	again := writeThrough(t, agent, filepath.Join(repo, "notes.md"), "words\n")
	if strings.Contains(again, "/land") {
		t.Fatalf("the second write repeated itself:\n%s", again)
	}
	if now := agent.StandingTrees(); len(now) != 1 || now[0].Dir != trees[0].Dir {
		t.Fatalf("the second write cut another copy: %+v", now)
	}
	waiting := agent.UnlandedChanges()
	if len(waiting) != 1 || waiting[0].Files != 2 || waiting[0].Name != filepath.Base(repo) {
		t.Fatalf("what is waiting reads %+v", waiting)
	}
}

// AND THE MODEL SEES ITS OWN WORK. A write followed by a read of the same path
// must answer what was written — the copy is that folder's truth for this
// conversation — while a file the conversation never touched is still read
// where it actually lives.
func TestReadingBackAWriteIntoAReferredFolderSeesTheWrite(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "untouched.txt"), "as it was\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "second")
	agent, _, _ := standingLab(t, repo)

	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the changed line\n")
	if got := readThrough(t, agent, filepath.Join(repo, "shared.txt")); !strings.Contains(got, "the changed line") {
		t.Fatalf("reading back its own write answered:\n%s", got)
	}
	if got := readThrough(t, agent, filepath.Join(repo, "untouched.txt")); !strings.Contains(got, "as it was") {
		t.Fatalf("a file nobody changed answered:\n%s", got)
	}
}

// A FOLDER SOMEBODY SAID "IN PLACE" ABOUT IS WRITTEN DIRECTLY, and no copy is
// made at all. A said mode is never overruled, which is the law places.go keeps
// about the rung it feeds.
func TestAFolderSaidToBeWorkedInDirectlyIsWrittenDirectly(t *testing.T) {
	repo := newTestRepo(t)
	agent, _, _ := standingLab(t, repo)
	if err := agent.SetPlaceMode(repo, "in place"); err != nil {
		t.Fatalf("SetPlaceMode: %v", err)
	}

	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "straight in\n")
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "straight in\n" {
		t.Fatalf("the folder says %q — a said mode was overruled", got)
	}
	// The head start is owed work, so the count below is asked once it has all
	// happened. Winning a race against `git worktree add` is not a proof.
	agent.SettleWrites()
	if trees := agent.StandingTrees(); len(trees) != 0 {
		t.Fatalf("a copy was made anyway: %+v", trees)
	}
	if waiting := agent.UnlandedChanges(); len(waiting) != 0 {
		t.Fatalf("nothing is waiting and the chip would draw %+v", waiting)
	}
}

// THE FOLDER THE CONVERSATION IS STANDING IN IS NEVER STAGED. A person who
// opened aforge inside their project loses nothing and notices nothing.
func TestTheFolderYouAreStandingInIsWrittenDirectly(t *testing.T) {
	repo := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		config.Place = Place{Dir: t.TempDir()}
	})
	// Referring to the folder it is standing in is the awkward case: the
	// workspace must still win.
	if _, err := agent.ReferPlace(repo, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}

	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "here, directly\n")
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "here, directly\n" {
		t.Fatalf("the workspace says %q", got)
	}
	agent.SettleWrites()
	if trees := agent.StandingTrees(); len(trees) != 0 {
		t.Fatalf("the standing workspace was staged: %+v", trees)
	}
	if branches := gitOut(t, repo, "branch", "--list", "chat/*"); strings.TrimSpace(branches) != "" {
		t.Fatalf("a branch was cut in the person's own repository: %q", branches)
	}
}

// SAYING "IN PLACE" AFTER THE COPY HAS ALREADY BEEN CUT TAKES IT BACK. The mode
// is a fact about a place, so it can only be said once the place is referred —
// by which time the head start has run — and a person's word about their own
// folder that arrives second still has to win completely.
func TestSayingInPlaceAfterTheCopyWasCutTakesItBack(t *testing.T) {
	repo := newTestRepo(t)
	agent, _, _ := standingLab(t, repo) // refers the folder, which starts the cut
	agent.SettleWrites()
	if trees := agent.StandingTrees(); len(trees) != 1 {
		t.Fatalf("the head start made no copy to take back: %+v", trees)
	}
	cut := agent.StandingTrees()[0]

	if err := agent.SetPlaceMode(repo, "in place"); err != nil {
		t.Fatalf("SetPlaceMode: %v", err)
	}
	if trees := agent.StandingTrees(); len(trees) != 0 {
		t.Fatalf("the copy outlived the word that retired it: %+v", trees)
	}
	if _, err := os.Stat(cut.Dir); !os.IsNotExist(err) {
		t.Fatalf("the working copy is still on disk at %s", cut.Dir)
	}
	if branches := gitOut(t, repo, "branch", "--list", cut.Branch); strings.TrimSpace(branches) != "" {
		t.Fatalf("the branch outlived the copy: %q", branches)
	}

	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "straight in\n")
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "straight in\n" {
		t.Fatalf("the folder says %q — the word did not take", got)
	}
	agent.SettleWrites()
	if trees := agent.StandingTrees(); len(trees) != 0 {
		t.Fatalf("a copy was made after the word: %+v", trees)
	}
}

// A COPY SOMETHING HAS BEEN WRITTEN INTO IS NEVER TAKEN BACK, even by the word
// that would have stopped it being made. The work is in that copy and nowhere
// else, and [Agent.Land] is the only door that moves it out.
func TestSayingInPlaceKeepsACopyThatAlreadyHoldsWork(t *testing.T) {
	repo := newTestRepo(t)
	agent, _, _ := standingLab(t, repo)
	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "written into the copy\n")
	agent.SettleWrites()

	if err := agent.SetPlaceMode(repo, "in place"); err != nil {
		t.Fatalf("SetPlaceMode: %v", err)
	}
	trees := agent.StandingTrees()
	if len(trees) != 1 {
		t.Fatalf("the work was thrown away with the copy: %+v", trees)
	}
	if got := readFile(t, filepath.Join(trees[0].Dir, "shared.txt")); got != "written into the copy\n" {
		t.Fatalf("the copy says %q", got)
	}
	if waiting := agent.UnlandedChanges(); len(waiting) != 1 {
		t.Fatalf("the chip lost the way back to the work: %+v", waiting)
	}
}

// AND WORK THE BELT DID NOT WRITE IS WORK ALL THE SAME. A shell command, a
// script the model ran, a task standing in that ground — none of them reach
// [noteStandingWrite], so [StandingTree.Wrote] is empty while the copy holds an
// hour of somebody's afternoon. Until 2026-09-11 the retire forced the removal
// and deleted it with no refusal and no sentence.
//
// The three cases are the three ways git itself refuses a removal, because git's
// refusal IS the guard now: a file it has never seen, a file staged for a commit
// that has not happened, and a tracked file edited in place.
func TestACopyIsKeptWhenItHoldsWorkTheBeltDidNotWrite(t *testing.T) {
	for _, aCase := range []struct {
		name  string
		leave func(t *testing.T, copyDir string) string
	}{
		{"a file git has never seen", func(t *testing.T, copyDir string) string {
			path := filepath.Join(copyDir, "made-by-bash.txt")
			writeFile(t, path, "an hour of work\n")
			return path
		}},
		{"a file staged but not committed", func(t *testing.T, copyDir string) string {
			path := filepath.Join(copyDir, "staged.txt")
			writeFile(t, path, "ready to commit\n")
			mustGit(t, copyDir, "add", "staged.txt")
			return path
		}},
		{"a tracked file edited in place", func(t *testing.T, copyDir string) string {
			path := filepath.Join(copyDir, "shared.txt")
			writeFile(t, path, "changed by a shell command\n")
			return path
		}},
	} {
		t.Run(aCase.name, func(t *testing.T) {
			repo := newTestRepo(t)
			agent, _, _ := standingLab(t, repo) // refers the folder, which starts the cut
			agent.SettleWrites()
			trees := agent.StandingTrees()
			if len(trees) != 1 {
				t.Fatalf("no copy was cut to hold anything: %+v", trees)
			}
			left := aCase.leave(t, trees[0].Dir)
			if wrote := agent.StandingTrees()[0].Wrote; len(wrote) != 0 {
				t.Fatalf("the belt recorded it after all, and this case proves nothing: %+v", wrote)
			}

			if err := agent.SetPlaceMode(repo, "in place"); err != nil {
				t.Fatalf("SetPlaceMode: %v", err)
			}

			if _, err := os.Stat(left); err != nil {
				t.Fatalf("%s was destroyed: %v", left, err)
			}
			if kept := agent.StandingTrees(); len(kept) != 1 {
				t.Fatalf("the record went and took the way back to the work with it: %+v", kept)
			}
			if branches := gitOut(t, repo, "branch", "--list", trees[0].Branch); strings.TrimSpace(branches) == "" {
				t.Fatalf("the branch of a copy that was kept was deleted")
			}
		})
	}
}

// THE SAME PROMISE ON A PLAIN FOLDER, which has no git to refuse for it and is
// compared with the folder it was copied from instead.
func TestACopyOfAPlainFolderIsKeptWhenItHoldsWork(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.txt"), "as it was\n")
	agent, _, _ := standingLab(t, folder)
	agent.SettleWrites()
	trees := agent.StandingTrees()
	if len(trees) != 1 || trees[0].Mode != TaskModeMirror {
		t.Fatalf("a plain folder was not mirrored: %+v", trees)
	}
	left := filepath.Join(trees[0].Dir, "made-by-bash.txt")
	writeFile(t, left, "an hour of work\n")

	if err := agent.SetPlaceMode(folder, "in place"); err != nil {
		t.Fatalf("SetPlaceMode: %v", err)
	}
	if _, err := os.Stat(left); err != nil {
		t.Fatalf("%s was destroyed: %v", left, err)
	}
	if kept := agent.StandingTrees(); len(kept) != 1 {
		t.Fatalf("the record went with it: %+v", kept)
	}
}

// AND THE CONTROL, on the road that has no git: a copy of a plain folder that
// holds nothing is still given back whole.
func TestACopyOfAPlainFolderHoldingNothingIsStillGivenBack(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.txt"), "as it was\n")
	agent, _, _ := standingLab(t, folder)
	agent.SettleWrites()
	cut := agent.StandingTrees()[0]

	if err := agent.SetPlaceMode(folder, "in place"); err != nil {
		t.Fatalf("SetPlaceMode: %v", err)
	}
	if kept := agent.StandingTrees(); len(kept) != 0 {
		t.Fatalf("a copy holding nothing was kept: %+v", kept)
	}
	if _, err := os.Stat(cut.Dir); !os.IsNotExist(err) {
		t.Fatalf("the copy is still on disk at %s", cut.Dir)
	}
}

// THE LANDING IS THE TASKS' OWN LANDING. A repository's work is committed on
// its branch and merged home; the copy and the branch go with it.
func TestLandingAReferredRepositoryMergesTheWorkHome(t *testing.T) {
	repo := newTestRepo(t)
	agent, _, _ := standingLab(t, repo)
	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the changed line\n")

	landing, err := agent.Land(repo)
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	if landing.Merged != mergeMerged {
		t.Fatalf("the landing came back %q with %q", landing.Merged, landing.Note)
	}
	if len(landing.Files) != 1 || landing.Files[0] != "shared.txt" {
		t.Fatalf("the landing names %v", landing.Files)
	}
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "the changed line\n" {
		t.Fatalf("after the landing the folder says %q", got)
	}
	if waiting := agent.UnlandedChanges(); len(waiting) != 0 {
		t.Fatalf("the chip would still draw %+v after a landing", waiting)
	}
	meta, err := LoadMeta(agent.config.Place.Dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if len(meta.Trees) != 0 {
		t.Fatalf("the landed copy is still written down: %+v", meta.Trees)
	}
}

// C10: a referred repository follows the same protected-branch law as task
// work: main keeps the chat branch, while an ordinary work branch still merges.
func TestC10LandingAReferredRepositoryProtectsMainAndMergesWork(t *testing.T) {
	t.Run("main is protected", func(t *testing.T) {
		repo := newTestRepo(t)
		mustGit(t, repo, "checkout", "-b", "main")
		beforeHead := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main"))
		beforeStatus := gitOut(t, repo, "status", "--porcelain")
		agent, _, _ := standingLab(t, repo)
		writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the protected change\n")
		trees := agent.StandingTrees()
		if len(trees) != 1 || trees[0].Home != "main" {
			t.Fatalf("standing tree = %+v, want its home branch recorded", trees)
		}
		branch, dir := trees[0].Branch, trees[0].Dir

		landing, err := agent.Land(repo)
		if err != nil {
			t.Fatal(err)
		}
		want := "its branch " + branch + " was kept: your checkout is on main, which tasks do not merge into automatically"
		if landing.Merged != mergeKept || !landing.Kept() || !strings.Contains(landing.Note, want) {
			t.Fatalf("landing = %+v, want the protected chat branch kept", landing)
		}
		if got := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main")); got != beforeHead {
			t.Fatalf("main moved from %s to %s", beforeHead, got)
		}
		if got := gitOut(t, repo, "status", "--porcelain"); got != beforeStatus {
			t.Fatalf("checkout status changed from %q to %q", beforeStatus, got)
		}
		if got := gitOut(t, repo, "show", branch+":shared.txt"); got != "the protected change\n" {
			t.Fatalf("kept chat branch holds %q", got)
		}
		if list := gitOut(t, repo, "worktree", "list"); strings.Contains(list, dir) {
			t.Fatalf("the standing copy stayed registered:\n%s", list)
		}
	})

	t.Run("work still merges", func(t *testing.T) {
		repo := newTestRepo(t)
		agent, _, _ := standingLab(t, repo)
		writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the ordinary change\n")
		landing, err := agent.Land(repo)
		if err != nil {
			t.Fatal(err)
		}
		if landing.Merged != mergeMerged || landing.Kept() {
			t.Fatalf("landing = %+v, want work to merge", landing)
		}
	})
}

// C5-C6: /land reads the commit carried by the standing-tree record after the
// conversation that cut it has closed, and keeps the chat branch when the
// person moved that same branch in the meantime.
func TestAStandingTreeLandingKeepsItsBranchWhenTheRecordedTipMoved(t *testing.T) {
	repo := newTestRepo(t)
	cutTip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work"))
	agent, _, place := standingLab(t, repo)
	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the conversation's change\n")
	trees := agent.StandingTrees()
	if len(trees) != 1 || trees[0].Home != "work" || trees[0].HomeSha != cutTip {
		t.Fatalf("standing tree recorded %+v, want work at %s", trees, cutTip)
	}
	branch := trees[0].Branch
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	writeFile(t, filepath.Join(repo, "person.txt"), "the person's later work\n")
	mustGit(t, repo, "add", "person.txt")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "move work after the cut")
	personTip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work"))

	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(place, "session.jsonl")
		config.Place = Place{Dir: place}
	})
	if trees := second.StandingTrees(); len(trees) != 1 || trees[0].HomeSha != cutTip {
		t.Fatalf("reopened standing tree forgot the cut commit: %+v", trees)
	}
	landing, err := second.Land(repo)
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	want := "its branch " + branch + " was kept: work has moved on since the work was cut — inspect the retained task branch before choosing a destination"
	if landing.Merged != mergeKept || !landing.Kept() || landing.Note != want {
		t.Fatalf("landing = %+v, want the moved-tip chat branch kept with %q", landing, want)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "rev-parse", "work")); got != personTip {
		t.Fatalf("work moved from %s to %s", personTip, got)
	}
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "the original line\n" {
		t.Fatalf("the kept standing work reached the checkout as %q", got)
	}
	if got := gitOut(t, repo, "show", branch+":shared.txt"); got != "the conversation's change\n" {
		t.Fatalf("kept chat branch holds %q", got)
	}
}

// AND A PLAIN FOLDER IS LAID BACK BY NAME. No history to branch from is not a
// reason to leave somebody without the isolation a repository gets for free.
func TestLandingAPlainFolderLaysTheWorkBackByName(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.md"), "the original words\n")
	agent, _, _ := standingLab(t, folder)

	writeThrough(t, agent, filepath.Join(folder, "notes.md"), "the new words\n")
	if got := readFile(t, filepath.Join(folder, "notes.md")); got != "the original words\n" {
		t.Fatalf("the folder moved before the landing: %q", got)
	}
	trees := agent.StandingTrees()
	if len(trees) != 1 || trees[0].Mode != TaskModeMirror {
		t.Fatalf("a plain folder was given %+v", trees)
	}
	// The copy carries the folder as it was, which is what makes it usable —
	// reading anything in it is reading the folder.
	if got := readFile(t, filepath.Join(trees[0].Dir, "notes.md")); got != "the new words\n" {
		t.Fatalf("the copy holds %q", got)
	}

	if _, err := agent.Land(folder); err != nil {
		t.Fatalf("Land: %v", err)
	}
	if got := readFile(t, filepath.Join(folder, "notes.md")); got != "the new words\n" {
		t.Fatalf("after the landing the folder says %q", got)
	}
}

// UNLANDED WORK SURVIVES THE TERMINAL CLOSING. It is the one thing on the
// session's meta.json that is not recoverable by looking again, so a second
// process opens still holding it — and can still land it.
func TestUnlandedWorkSurvivesTheConversationClosing(t *testing.T) {
	repo := newTestRepo(t)
	agent, _, place := standingLab(t, repo)
	writeThrough(t, agent, filepath.Join(repo, "shared.txt"), "the changed line\n")
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(place, "session.jsonl")
		config.Place = Place{Dir: place}
	})
	waiting := second.UnlandedChanges()
	if len(waiting) != 1 || waiting[0].Files != 1 || waiting[0].Folder != canonicalPath(repo) {
		t.Fatalf("the reopened conversation is holding %+v", waiting)
	}
	if trees := second.StandingTrees(); len(trees) != 1 || trees[0].Home != "work" {
		t.Fatalf("the reopened standing tree forgot its home branch: %+v", trees)
	}
	landing, err := second.Land("")
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	if landing.Merged != mergeMerged {
		t.Fatalf("the reopened landing came back %q with %q", landing.Merged, landing.Note)
	}
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "the changed line\n" {
		t.Fatalf("after the landing the folder says %q", got)
	}
}

// THE REFUSALS, in the person's own words. Landing what nothing changed is not
// an error to swallow silently — somebody typed /land expecting something.
func TestLandingWithNothingWaitingSaysSoPlainly(t *testing.T) {
	repo := newTestRepo(t)
	agent, _, _ := standingLab(t, repo)

	if _, err := agent.Land(""); err == nil || !strings.Contains(err.Error(), "nothing is waiting") {
		t.Fatalf("landing nothing answered %v", err)
	}
	if _, err := agent.Land(t.TempDir()); err == nil || !strings.Contains(err.Error(), "nothing is waiting for") {
		t.Fatalf("landing a folder nobody wrote in answered %v", err)
	}
}
