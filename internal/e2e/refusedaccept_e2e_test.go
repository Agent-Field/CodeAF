//go:build e2e

package e2e

// refusedaccept_e2e_test.go IS THE ACCEPTANCE OF #767 ON A REAL SCREEN.
//
// 2026-09-09, on the owner's own machine: a divided task landed as their call,
// the model spent `accept` on it under `task.settle = auto`, and the merge was
// refused by the person's own uncommitted copies of the very files the task had
// written. The node re-settled asking a DIFFERENT question — now theirs — and
// nothing on any screen carried a single answer for it. The conversation's card
// was frozen in the shape of the first landing, the node's own room read `this
// task has finished — say it to main` over work nobody had decided about, and
// the column read `your call · conflicts with your branch:` with the file list
// shed and the colon left dangling. A bright terminal state with no handle.
//
// WHAT IS MEASURED HERE IS THE HANDLE, on the checkpoint that shape leaves
// behind. It pays for no model: every fact this reads is one the record carries
// and the surface draws.
//
//	go test -tags e2e -run TestRefusedAccept -count=1 -timeout 20m -v ./internal/e2e/

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The words this file waits for. They are the ENGINE's, spelled once in
// internal/session (task_status.go's ask table) and drawn by the surface, and
// they are written out here rather than taken from the shared needle table
// because they are this file's own subject: a change to either side has to be
// made deliberately in both.
const (
	refusedGroundReason = "your folder already has files the task wrote"
	refusedYesWord      = "resolve it"
	refusedNoWord       = "drop it"
	refusedTellWord     = "tell it"
	// refusedKeptBeside is the half of the yes's consequence a person has to
	// read BEFORE they press it, because pressing it moves files of theirs
	// (internal/session's taskAskGroundConsequence). It is the reason this one
	// landing asks on the card rather than on the task-states row: a consequence
	// is drawn beside its answer on a card and nowhere on a row.
	refusedKeptBeside = ".yours"
	// refusedFinishedFoot is the sentence the room must NOT be wearing over a
	// node that is still somebody's call (internal/tui3's roomrefusal.go).
	refusedFinishedFoot = "this task has finished"
)

func TestRefusedAcceptE2E(t *testing.T) {
	requireTmuxAndKey(t)

	t.Run("a_refused_landing_has_answers_in_the_conversation", testRefusedAcceptAsks)
	t.Run("the_same_answers_stand_inside_the_nodes_room", testRefusedAcceptInTheRoom)
}

// THE CONVERSATION ASKS, and the column says what for WITHOUT ENDING IN A COLON.
func testRefusedAcceptAsks(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "auto"})
	ws := newWorkspace(t, "refusedws", false)
	seedRefusedAccept(t, home, ws)
	r := start(t, "afe2e_refused_asks", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)

	screen := r.waitFor(45*time.Second, refusedGroundReason, refusedYesWord)
	t.Logf("a landing whose merge was refused by the person's own copies:\n%s", screen)

	// THE THREE COLUMNS, ALWAYS THE SAME THREE (docs/design/task-states/DESIGN.md).
	for _, want := range []string{refusedYesWord, refusedNoWord, refusedTellWord} {
		if !strings.Contains(screen, want) {
			t.Errorf("the screen offers no %q, so the person is told what is wrong and given "+
				"nothing to do about it:\n%s", want, screen)
		}
	}
	// AND THE ROW NEVER ENDS IN A BARE COLON. `conflicts with your branch:` is the
	// announcement of a list that is not there, which is what a person read while
	// their task sat waiting for them.
	for _, line := range strings.Split(screen, "\n") {
		if said := strings.TrimRight(line, " "); strings.HasSuffix(said, ":") &&
			strings.Contains(said, "your") {
			t.Errorf("a row ends in a bare colon and names nothing after it: %q", said)
		}
	}
	// AND THE YES SAYS WHAT IT WILL DO TO THEIR FILES. `resolve it` means one
	// more merge round everywhere else on this table; here it carries the
	// person's own copies aside and puts them back, and where both wrote the
	// same path theirs is kept beside the task's. A key that moves somebody's
	// unfinished work may not be pressed blind.
	if !strings.Contains(screen, refusedKeptBeside) {
		t.Errorf("the yes never says what it does to their own copies (%q):\n%s", refusedKeptBeside, screen)
	}
	statesNoDeletedWords(t, screen)
	r.quit()
}

// AND THE NODE'S OWN ROOM ASKS THE SAME THING. The room is where a person is
// invited to do the looking, so it is where the question has to be answerable —
// and what it must never say over a node that is still somebody's call is that
// the work has finished.
func testRefusedAcceptInTheRoom(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "auto"})
	ws := newWorkspace(t, "refusedroomws", false)
	seedRefusedAccept(t, home, ws)
	r := start(t, "afe2e_refused_room", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)
	r.waitFor(45*time.Second, refusedGroundReason)

	// The column's own row is the door into the room, and `alt+t` is how the
	// roster is handed the keyboard (internal/manual/chat/keys.md).
	r.keys("M-t")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")

	room := r.waitFor(45*time.Second, refusedGroundReason, refusedYesWord)
	t.Logf("the node's own room, over a landing that is still somebody's call:\n%s", room)
	for _, want := range []string{refusedYesWord, refusedNoWord, refusedTellWord} {
		if !strings.Contains(room, want) {
			t.Errorf("the room offers no %q:\n%s", want, room)
		}
	}
	if strings.Contains(room, refusedFinishedFoot) {
		t.Errorf("the room says %q over work nobody has decided about:\n%s", refusedFinishedFoot, room)
	}
	statesNoDeletedWords(t, room)
	r.quit()
}

// seedRefusedAccept writes the checkpoint #767's shape leaves behind: a root
// node the model accepted whose branch would not land, because the person's own
// uncommitted copies of the files it wrote are sitting in the folder.
//
// EVERY FIELD HERE IS ONE THE RECORD REALLY CARRIES. `merge: conflicted` and
// `groundHeld: true` are what say which of the three roads to the conflict's one
// question this landing took, `clashing` is the file list git's index held while
// the refused merge stood, and `decider` is EMPTY on purpose — the model's turn
// ended, and an unowned question belongs to whoever is looking at it
// (internal/session's task_store.go).
func seedRefusedAccept(t *testing.T, home, ws string) string {
	t.Helper()
	if canonical, err := filepath.EvalSymlinks(ws); err == nil {
		ws = canonical
	}
	bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
	sid := fmt.Sprintf("%016x", 0x3000000000000767)
	dir := filepath.Join(home, "v3", "projects", bucket, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the refused landing: %v", err)
	}
	statesSeedTranscript(t, dir, sid, ws, "find emails and socials for the fifteen leads")
	at := time.Now().Add(-25 * time.Minute)
	writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{
		"id": sid, "title": "The landing whose merge was refused", "workspace": ws,
		"created": at.Format(time.RFC3339Nano), "lastUserAt": at.Format(time.RFC3339Nano),
	})
	writeJSON(t, filepath.Join(dir, "tasks.json"), map[string]any{
		"type": "tasks", "version": 1, "seq": 1,
		"nodes": []map[string]any{{
			"id": 1, "title": "Emails and socials for the fifteen leads",
			"brief": "find them", "acceptance": "fifteen sourced rows",
			"state": "unverified", "merge": "conflicted",
			"branch":     "task/emails-and-socials",
			"groundHeld": true,
			"clashing":   []string{"leads-contact-sheet.md", "research/method.md"},
			"report": refusedGroundReason + ": leads-contact-sheet.md, research/method.md" +
				" — its branch task/emails-and-socials did not merge cleanly and was kept",
			"changed": []string{"leads-contact-sheet.md", "research/method.md"},
			"ground":  ws, "groundMode": "folder", "elapsed_ms": 1527000,
		}},
	})
	return dir
}
