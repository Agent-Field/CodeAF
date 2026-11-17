package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// ── A COPY KEPT FROM UNSAVED EDITS, REACHED FROM THE CHAT'S OWN ROW ─────────
//
// WHAT THIS PINS. A move that finds unsaved edits on the receiving computer
// keeps them as a branch whose parent is the chat that moved. Home folds such a
// copy onto the chat's single row as the dim `N copy` note, and the manual tells
// the person to open it or discard it from there. These tests drive the real
// Home with the shape every real kept copy has (a branch row whose Parent is a
// chat listed here), because the older branch tests used parentless rows, which
// never fold, and so stayed green while the real case had no door (#2780).

const keptMainID = "0000000000000001"

// keptCalls records what the branch callbacks were asked.
type keptCalls struct{ opened, discarded []string }

// keptCopyHome is Home with one local chat and the given branch copies of it in
// the directory listing, the chat held here (the state right after a move).
func keptCopyHome(t *testing.T, copies ...chatlist.Row) (*app, *keptCalls) {
	t.Helper()
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	mine := lab.session("project", keptMainID, "Main chat", workspace, time.Now())
	a := lab.app(mine)
	a.width, a.height = 200, 70
	drain(t, a, a.openHome())
	rows := chatlist.Static{{Cell: keptMainID, Title: "Main chat", Device: "desk", Status: chatlist.Here, Mine: true, Head: "turn-main", DurableAgo: time.Minute}}
	a.machines = append(rows, copies...)
	calls := &keptCalls{}
	a.branches = BranchActions{
		Open: func(_ context.Context, id string) (Taken, error) {
			calls.opened = append(calls.opened, id)
			// What the real door leaves behind: the copy, as a chat of its own.
			lab.session("project", id, "Kept copy", workspace, time.Now())
			return Taken{}, nil
		},
		Discard: func(_ context.Context, id string) error {
			calls.discarded = append(calls.discarded, id)
			return nil
		},
	}
	drain(t, a, a.askMachines())
	return a, calls
}

// keptCopy is a branch the move kept: its parent is the chat that moved.
func keptCopy(cell, device string, turns uint32) chatlist.Row {
	return chatlist.Row{Cell: cell, Title: "Main chat", Device: device, Parent: keptMainID, Status: chatlist.Branch,
		Head: "turn-" + cell, OrphanTurns: turns, DurableAgo: 3 * time.Minute}
}

// standOnMain puts the cursor on the chat's own row.
func standOnMain(t *testing.T, a *app) {
	t.Helper()
	for at, line := range a.home.lines {
		if line.kind == homeSession && homeSessionID(line.row) == keptMainID {
			a.dropHomeAsk()
			a.closeStrip()
			a.home.cursor = at
			return
		}
	}
	t.Fatalf("no row for the chat:\n%s", homeText(a))
}

// stripWords is the strip's verbs as `key word`, in the order they are drawn.
func stripWords(a *app) []string {
	var out []string
	for _, v := range a.strip.verbs {
		out = append(out, string(v.key)+" "+v.word)
	}
	return out
}

// pressKeptKey opens the row's strip and presses the kept-turns key.
func pressKeptKey(t *testing.T, a *app) {
	t.Helper()
	standOnMain(t, a)
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatalf("the chat's row offered no strip:\n%s", homeText(a))
	}
	drive(t, a, key("k"))
}

// The contract: a kept copy is folded onto the chat's one row AND reachable from
// that row. The first half is the older rule and must not move.
func TestKeptCopyStaysOnTheChatsRowAndTheRowNamesTheKey(t *testing.T) {
	a, _ := keptCopyHome(t, keptCopy("kept-1", "laptop", 1))
	if got := countHomeRowsForID(a, keptMainID); got != 1 {
		t.Fatalf("the chat shows %d rows, want 1:\n%s", got, homeText(a))
	}
	if said := homeRowWordsForID(a, keptMainID); !strings.Contains(said, "1 copy") {
		t.Fatalf("the row lost its copy note:\n%s", said)
	}
	standOnMain(t, a)
	drive(t, a, key("right"))
	words := strings.Join(stripWords(a), " | ")
	if !strings.Contains(words, "k kept turns") {
		t.Fatalf("the strip does not name the kept-turns key: %s\n%s", words, homeText(a))
	}
	if !strings.Contains(homeText(a), "k kept turns") {
		t.Fatalf("the screen does not draw the kept-turns key:\n%s", homeText(a))
	}
}

// Answer 1 opens the copy under its OWN id, and nothing else runs.
func TestKeptCopyOpensSeparatelyFromTheChatsRow(t *testing.T) {
	a, calls := keptCopyHome(t, keptCopy("kept-1", "laptop", 1))
	pressKeptKey(t, a)
	q, ok := a.homeAsking()
	if !ok || q.question.Head != branchAsk {
		t.Fatalf("the key raised no branch card: %+v\n%s", q, homeText(a))
	}
	labels := make([]string, 0, len(q.question.Options))
	for _, o := range q.question.Options {
		labels = append(labels, o.Key+" "+o.Label)
	}
	if got := strings.Join(labels, ", "); got != "1 open separately, 2 discard, 3 leave them" {
		t.Fatalf("the card offers %q", got)
	}
	if text := homeText(a); !strings.Contains(text, "1 turn from laptop") || strings.Contains(text, "merge") {
		t.Fatalf("the card does not name the copy, or offers a merge that is not wired:\n%s", text)
	}
	confirm(t, a)
	if len(calls.opened) != 1 || calls.opened[0] != "kept-1" || len(calls.discarded) != 0 {
		t.Fatalf("opened %v, discarded %v; want the copy opened under its own id", calls.opened, calls.discarded)
	}
	if a.at(pageHome) {
		t.Fatalf("the copy was opened but Home is still up:\n%s", homeText(a))
	}
}

// Answer 2 archives the copy, and the main chat is never named to a callback.
func TestKeptCopyDiscardsFromTheChatsRow(t *testing.T) {
	a, calls := keptCopyHome(t, keptCopy("kept-1", "laptop", 1))
	pressKeptKey(t, a)
	drive(t, a, key("2"), key("enter"))
	if len(calls.discarded) != 1 || calls.discarded[0] != "kept-1" || len(calls.opened) != 0 {
		t.Fatalf("opened %v, discarded %v; want only kept-1 discarded", calls.opened, calls.discarded)
	}
}

// `leave them` is the answer the cursor starts on, so a stray enter keeps the turns.
func TestKeptCopyLeaveThemRunsNothing(t *testing.T) {
	a, calls := keptCopyHome(t, keptCopy("kept-1", "laptop", 1))
	pressKeptKey(t, a)
	if _, asking := a.homeAsking(); !asking {
		t.Fatalf("the key raised no card:\n%s", homeText(a))
	}
	drive(t, a, key("enter"))
	if len(calls.opened)+len(calls.discarded) != 0 {
		t.Fatalf("leave them ran something: opened %v, discarded %v", calls.opened, calls.discarded)
	}
}

// With several copies the note counts them and the key walks one card per copy:
// the same number of cards as the note's count, each naming its own copy, and a
// fresh round after the last.
func TestKeptCopyKeyWalksOneCardPerCopy(t *testing.T) {
	a, calls := keptCopyHome(t,
		keptCopy("kept-1", "laptop", 1),
		keptCopy("kept-2", "mac", 3),
	)
	if said := homeRowWordsForID(a, keptMainID); !strings.Contains(said, "2 copies") {
		t.Fatalf("the note does not count both copies:\n%s", said)
	}
	var seen []string
	for range 3 {
		pressKeptKey(t, a)
		q, ok := a.homeAsking()
		if !ok {
			t.Fatalf("the key raised no card:\n%s", homeText(a))
		}
		seen = append(seen, q.question.Reason)
	}
	if !strings.Contains(seen[0], "1 turn from laptop") || !strings.Contains(seen[1], "3 turns from mac") {
		t.Fatalf("the cards did not walk the copies in order: %q", seen)
	}
	if seen[0] == seen[1] {
		t.Fatalf("two copies drew the same card: %q", seen)
	}
	if seen[2] != seen[0] {
		t.Fatalf("the third press did not begin a fresh round: %q", seen)
	}
	// The fourth press is the second copy's card again, and answering it acts on
	// that copy.
	pressKeptKey(t, a)
	if q, _ := a.homeAsking(); !strings.Contains(q.question.Reason, "3 turns from mac") {
		t.Fatalf("the fourth press is not the second copy's card: %q", q.question.Reason)
	}
	drive(t, a, key("2"), key("enter"))
	if len(calls.discarded) != 1 || calls.discarded[0] != "kept-2" {
		t.Fatalf("the second card did not discard kept-2: %v", calls.discarded)
	}
}

// A copy whose every turn is already in the chat still offers discard, and the
// card says discarding loses nothing, so the note's sentence has something to
// point at.
func TestRedundantKeptCopyStillOffersDiscard(t *testing.T) {
	same := keptCopy("kept-1", "laptop", 2)
	same.BranchRedundant = true
	a, calls := keptCopyHome(t, same)
	pressKeptKey(t, a)
	q, ok := a.homeAsking()
	if !ok || !strings.Contains(q.question.Reason, "discarding loses nothing") {
		t.Fatalf("the card does not say discarding loses nothing: %+v", q.question)
	}
	drive(t, a, key("2"), key("enter"))
	if len(calls.discarded) != 1 || calls.discarded[0] != "kept-1" {
		t.Fatalf("discard did not run on the redundant copy: %v", calls.discarded)
	}
}

// The control: a chat with no copy has no such key, and the letter does nothing.
func TestAChatWithNoCopyHasNoKeptTurnsKey(t *testing.T) {
	a, calls := keptCopyHome(t)
	standOnMain(t, a)
	drive(t, a, key("right"))
	if words := strings.Join(stripWords(a), " | "); strings.Contains(words, "kept turns") {
		t.Fatalf("a chat with no copy names the key: %s", words)
	}
	drive(t, a, key("k"))
	if _, asking := a.homeAsking(); asking || len(calls.opened)+len(calls.discarded) != 0 {
		t.Fatalf("the letter did something on a chat with no copy:\n%s", homeText(a))
	}
}

// A VERB THAT CANNOT WORK IS ABSENT: a surface with neither open nor discard
// behind it does not name the key.
func TestKeptTurnsKeyIsAbsentWhereNoVerbIsBehindIt(t *testing.T) {
	a, _ := keptCopyHome(t, keptCopy("kept-1", "laptop", 1))
	a.branches = BranchActions{}
	standOnMain(t, a)
	drive(t, a, key("right"))
	if words := strings.Join(stripWords(a), " | "); strings.Contains(words, "kept turns") {
		t.Fatalf("a surface with no branch verbs names the key: %s", words)
	}
}

// With the directory out of reach the key says why on the foot, as enter does on
// any branch row, and runs nothing: only the directory can archive a copy.
func TestKeptTurnsKeySaysWhenTheDirectoryCannotBeReached(t *testing.T) {
	a, calls := keptCopyHome(t, keptCopy("kept-1", "laptop", 1))
	standOnMain(t, a)
	drive(t, a, key("right"))
	a.machineRead.down = true
	a.machineRead.err = errors.New("unreachable")
	drive(t, a, key("k"))
	if _, asking := a.homeAsking(); asking {
		t.Fatalf("a card was raised with the directory unreachable:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), chatlist.Unreachable) {
		t.Fatalf("the foot does not say the directory is unreachable:\n%s", homeText(a))
	}
	if len(calls.opened)+len(calls.discarded) != 0 {
		t.Fatalf("a verb ran with the directory unreachable: %v %v", calls.opened, calls.discarded)
	}
}
