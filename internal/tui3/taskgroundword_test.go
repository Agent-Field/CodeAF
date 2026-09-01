package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── WHAT THIS SURFACE CALLS THE PLACE A TASK WORKED ─────────────────────────
//
// THE DEFECT: the settled card labelled every task's directory `worktree`. It
// was the machinery's own vocabulary, which this house bans in anything a person
// reads, and it was a claim about a mechanism the card had never checked — a
// repository task is grounded in a fork whenever furrow can make one
// (internal/session's groundladder.go), and the card was telling somebody their
// work had been in a git worktree it never went near.
//
// THE FIX IS ONE TABLE IN THE ENGINE and no words of this package's own
// ([session.GroundWord], keyed by the rung that made the node's world). These
// pin it from the two surfaces that read it — the settled card in the project's
// record, and the landing note a task writes when it comes home — because the
// point of one table is that those two can never say different things about one
// directory.

// groundWordCase is one rung, what was promised about it, and the words the two
// surfaces owe a person for it.
type groundWordCase struct {
	name  string
	rung  session.GroundRung
	mode  session.TaskMode
	label string
}

// groundWordCases is every rung, plus the two ways a surface can be handed no
// rung at all: a record row written before aforge wrote the rung down, and a
// node given an empty folder of its own on the reference promise.
var groundWordCases = []groundWordCase{
	{"a fork of the whole folder", session.GroundRungUniverse, session.TaskModeWorktree, "its own copy of the folder"},
	{"a worktree cut in the person's own repository", session.GroundRungSnapshot, session.TaskModeWorktree, "a branch of your repository"},
	{"a folder copied file by file", session.GroundRungCopy, session.TaskModeMirror, "its own copy of the folder"},
	{"work standing in the ground itself", session.GroundRungHere, session.TaskModeInPlace, "your own folder"},
	{"a row from before the rung was recorded", "", session.TaskModeWorktree, "a branch of your repository"},
}

// THE SETTLED CARD NAMES THE COPY THE WORK HAPPENED IN, ONE WORDING PER RUNG,
// and never the mechanism. The words are pinned as literals here on purpose: a
// test that asked [session.GroundWord] for them would pass on the day somebody
// quietly changed the table, and these strings are quoted in the manual.
func TestTheSettledCardNamesTheCopyTheWorkHappenedInForEveryRung(t *testing.T) {
	for _, tc := range groundWordCases {
		t.Run(tc.name, func(t *testing.T) {
			card := settledCardFor(t, tc.rung, tc.mode)
			if !strings.Contains(card, tc.label+railSep) {
				t.Fatalf("the card does not label the working copy %q:\n%s", tc.label, card)
			}
			if strings.Contains(card, "worktree") {
				t.Fatalf("the card said the machinery's own word:\n%s", card)
			}
		})
	}
}

// A DIRECTORY NOBODY CAN HONESTLY DESCRIBE IS NAMED AND NOT EXPLAINED. A node
// handed an empty folder of its own on the reference promise is nobody's copy
// and is not the person's folder either, so the card falls back to the word that
// claims nothing — the emptiness law, said about a label instead of a figure.
func TestASettledCardWithNoRungToNameSaysOnlyWhere(t *testing.T) {
	card := settledCardFor(t, "", session.TaskModeReference)
	if !strings.Contains(card, taskCardPlaceWord+railSep) {
		t.Fatalf("the card did not fall back to %q:\n%s", taskCardPlaceWord, card)
	}
	for _, never := range []string{"worktree", "its own copy of the folder", "a branch of your repository"} {
		if strings.Contains(card, never) {
			t.Fatalf("the card claimed %q about a directory nothing knows about:\n%s", never, card)
		}
	}
}

// settledCardFor opens the record card for one landed row grounded on this rung.
func settledCardFor(t *testing.T, rung session.GroundRung, mode session.TaskMode) string {
	t.Helper()
	a, _, _ := taskApp(t)
	entry := pastTask("9", "fix-the-nil-map-crash", "Fix the nil-map crash", time.Hour)
	dir := t.TempDir()
	entry.ArtifactURI = "file://" + dir
	entry.Where = dir
	entry.Rung, entry.Mode = rung, mode
	a.comp.tasks = []session.TaskIndexEntry{entry}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the page refused to open")
	}
	drive(t, a, key("enter"))
	return taskSheetText(a)
}

// THE LANDING NOTE SAYS IT IN THE SAME WORDS, on the row that names the branch.
// The branch is only half the answer to "where was this work left" — the same
// branch name means a checkout in the person's own repository on one rung and a
// branch fetched home out of a fork on another — and the label is the half that
// says which.
func TestTheLandingNoteNamesTheCopyTheWorkCameHomeFrom(t *testing.T) {
	for _, tc := range groundWordCases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, advance := taskApp(t)
			drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
				session.TaskRunning, session.TaskNotice{Rung: tc.rung, Mode: tc.mode})})
			advance(2 * time.Minute)
			drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
				session.TaskDone, session.TaskNotice{
					Rung: tc.rung, Mode: tc.mode, Report: "the guard is in",
					Branch: "task/fix-the-nil-map-crash-9c1a2f", Merge: "merged",
				})})
			clickHit(t, a, hitDone)
			text := taskText(a)
			if !strings.Contains(text, tc.label+" · task/fix-the-nil-map-crash-9c1a2f") {
				t.Fatalf("the landing note does not label the branch %q:\n%s", tc.label, text)
			}
			if strings.Contains(text, "worktree") {
				t.Fatalf("the landing note said the machinery's own word:\n%s", text)
			}
		})
	}
}

// A LANDING WHOSE COPY NOBODY RECORDED STILL NAMES ITS BRANCH. `branch · ` is
// true of every node that has one, whatever made its world, so it is what the
// row falls back to rather than a guess at a rung.
func TestALandingNoteWithNoRungToNameSaysOnlyBranch(t *testing.T) {
	a, _, advance := taskApp(t)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{})})
	advance(time.Minute)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{
			Report: "the guard is in", Branch: "task/fix-the-nil-map-crash-9c1a2f", Merge: "merged",
		})})
	clickHit(t, a, hitDone)
	text := taskText(a)
	if !strings.Contains(text, doneBranchLabel+"task/fix-the-nil-map-crash-9c1a2f") {
		t.Fatalf("the landing note did not fall back to %q:\n%s", doneBranchLabel, text)
	}
	if strings.Contains(text, "worktree") {
		t.Fatalf("the landing note said the machinery's own word:\n%s", text)
	}
}

// AND THE ROOM OF A NODE WORKING IN A FORK NEVER SAYS IT EITHER. The page a
// person stands in while the work runs is the third place they could be told
// where it is, and the pinned header above it is drawn by the frame rather than
// by the page — so both are read here.
func TestTheRoomOfAUniverseGroundedTaskNeverSaysWorktree(t *testing.T) {
	a, _, _ := roomApp(t)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{
			Rung: session.GroundRungUniverse, Mode: session.TaskModeWorktree,
			Where: "/tmp/lab/trees/7", Branch: "task/fix-the-nil-map-crash-9c1a2f",
		})})
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("the rail did not open the node's room")
	}
	page := roomText(a) + "\n" + plain(a.roomHead(a.width))
	if strings.Contains(page, "worktree") {
		t.Fatalf("the room said the machinery's own word:\n%s", page)
	}
}
