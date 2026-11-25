package session

import (
	"path/filepath"
	"strings"
	"testing"
)

// A TASK THAT ONLY READS ITS GROUND SAYS SO AT ALL THREE ENDS: the proposer's
// receipt, the worker's own words, and the landing. The run this is from: a
// proposal gave a repository as its ground and named no file by path, the
// worker built the project in its own empty folder and reported it
// "integrated", and nothing told anybody the repository was untouched.
func TestAReferenceTaskSaysItsGroundDoesNotChange(t *testing.T) {
	root := t.TempDir()
	ground, own := filepath.Join(root, "MacStats"), filepath.Join(root, "trees", "1")

	said := taskStandSentence(taskStand{dir: ground, rung: taskGroundSaid, mode: TaskModeReference})
	for _, want := range []string{"It reads " + ground, "nothing in " + ground + " changes", "propose it again naming the files"} {
		if !strings.Contains(said, want) {
			t.Errorf("the receipt lacks %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "It works in") {
		t.Errorf("the receipt still says the task works in the ground it only reads:\n%s", said)
	}
	if got := taskStandSentence(taskStand{dir: ground, rung: taskGroundSaid, mode: TaskModeWorktree}); !strings.HasPrefix(got, "It works in "+ground) {
		t.Errorf("a task that works in its ground is said as %q", got)
	}

	note := copyOnto(ground, TaskModeReference, own, Place{}).ownFolderNote()
	for _, want := range []string{ground + " is yours to read, not to change", "Everything you make goes in " + own, "never say the work is in " + ground} {
		if !strings.Contains(note, want) {
			t.Errorf("the worker's note lacks %q:\n%s", want, note)
		}
	}
	if got := copyOnto(ground, TaskModeReference, ground, Place{}).ownFolderNote(); got != "" {
		t.Errorf("a reference standing in its own ground was given a note: %q", got)
	}
	if got := copyOnto(ground, TaskModeWorktree, own, Place{}).ownFolderNote(); got != "" {
		t.Errorf("a worktree was told its ground is read-only: %q", got)
	}

	line := referenceLandingLine(taskTree{dir: own, ground: ground, mode: TaskModeReference})
	if line != "it changed nothing in "+ground+": what it made is in "+own {
		t.Errorf("the landing line is %q", line)
	}
	if got := beltLandingLine(RunLanding{Line: line}); got != line {
		t.Errorf("the landing's one line is %q", got)
	}
	for _, tree := range []taskTree{
		{dir: own, ground: ground, mode: TaskModeWorktree},
		{dir: ground, ground: ground, mode: TaskModeReference},
		{dir: own, mode: TaskModeReference},
	} {
		if got := referenceLandingLine(tree); got != "" {
			t.Errorf("%+v was given a reference landing: %q", tree, got)
		}
	}

	brief := withHomeReadingNote(Place{}, "Build it.", taskStand{dir: ground, mode: TaskModeReference})
	if !strings.Contains(brief, ground+" is yours to read, not to change") || !strings.Contains(brief, "your assigned working directory") {
		t.Errorf("a run's workers are not told their ground is read-only:\n%s", brief)
	}
	if got := withHomeReadingNote(Place{}, "Build it.", taskStand{dir: ground, mode: TaskModeWorktree}); got != "Build it." {
		t.Errorf("a run that works in its ground was told otherwise:\n%s", got)
	}
}

// A MANAGER IS TOLD A TASK IS NOT A WAY ROUND A MEMBER THAT DID NOT START.
func TestTheManagersLawsSayWhenATaskIsUsed(t *testing.T) {
	for _, want := range []string{"A task (propose_task) is not a member", "never to get round a member that did not start"} {
		if !strings.Contains(teamManagerLaws, want) {
			t.Errorf("the manager's laws lack %q", want)
		}
	}
}
