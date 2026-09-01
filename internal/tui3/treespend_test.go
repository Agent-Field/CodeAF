package tui3

// THE MONEY SEGMENT SAYS WHAT THE WORK IS SPENDING, WHILE IT IS SPENDING IT.
//
// Issue #145: a conversation's ambient figure read $2.53 for two hours while the
// tasks under it burned $51.05, because a node's money only reaches the
// conversation's own books when the node closes. These tests pin the row, the
// note, and the two silences the emptiness law demands around them.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// treeConversation is the id the fixture's conversation is held under. It is
// sixteen hex characters because that is what a conversation id is
// (session.NewSessionID), and the surface reads it off the folder its journal
// sits in.
const treeConversation = "1111111111111111"

// spendTreeLab is an app standing in one conversation, over a ledger this test
// wrote, with a task on its roster.
func spendTreeLab(t *testing.T, own float64, lines []session.UsageLine) *app {
	t.Helper()
	root := t.TempDir()
	folder := filepath.Join(root, "projects", "repo", treeConversation)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(root, session.UsageLedgerName)
	var file strings.Builder
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		file.Write(raw)
		file.WriteByte('\n')
	}
	if err := os.WriteFile(ledger, []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(&fakeAgent{model: "m", usage: session.Usage{CostUSD: own}})
	a.usageLedger = ledger
	a.file = filepath.Join(folder, "transcript.jsonl")
	a.cost = own
	return a
}

// treeFixture is the run in the issue, in three rows: the conversation's own
// turn, and two nodes of a family that is still working.
func treeFixture() []session.UsageLine {
	now := time.Now()
	return []session.UsageLine{
		{At: now, Session: treeConversation, Model: "m", Calls: 1, Input: 100, Output: 20, USD: 2.53},
		{At: now, Session: "aaaa2222aaaa2222", Task: "1", Root: treeConversation, Calls: 4, USD: 40.00},
		{At: now, Session: "bbbb3333bbbb3333", Task: "2", Root: treeConversation, Calls: 3, USD: 11.05},
	}
}

// THE ACCEPTANCE ON THIS SURFACE: with a task tree spending, the ambient figure
// is the tree's, and the smaller number never stands alone.
func TestTheMoneySegmentCarriesWhatTheRunningWorkIsSpending(t *testing.T) {
	a := spendTreeLab(t, 2.53, treeFixture())
	a.width = 200
	a.readTreeSpend()

	line := plain(a.status(a.width))
	if !strings.Contains(line, dollars(53.58)) {
		t.Fatalf("the status line does not carry the tree's own total:\n%s", line)
	}
	if strings.Contains(line, dollars(2.53)) {
		t.Fatalf("the conversation's own half is standing alone on the row:\n%s", line)
	}
	// AND IT IS ONE SEGMENT AND NOT TWO. The split belongs to /cost; this row is
	// the most crowded thing on the screen and its segments must not grow.
	var money int
	for _, part := range a.telemetry(a.width) {
		if part.kind == segCost {
			money++
			if part.text != dollars(53.58) {
				t.Fatalf("the money segment reads %q, want the tree's total", part.text)
			}
		}
	}
	if money != 1 {
		t.Fatalf("the row carries %d money segments, want one", money)
	}
}

// AND THE FRAME CLOCK IS WHAT KEEPS IT TRUE — not the task closing. The ledger
// is re-read on the clock the rest of the telemetry rides, so a figure written a
// moment ago is on the row a moment later.
func TestTheFrameClockPicksUpWhatTheWorkHasJustSpent(t *testing.T) {
	a := spendTreeLab(t, 2.53, treeFixture()[:1])
	a.width = 200
	// A roster with work on it, which is what makes the reading worth taking at
	// all ([app.railAvail]).
	a.taskOrder = []uint64{1}
	a.tasks = map[uint64]*taskNode{1: {id: 1, title: "Fix the crash", state: session.TaskRunning}}

	for i := 0; i < 24; i++ {
		drive(t, a, frameMsg{})
	}
	if got := a.spendShown(); !near(got, 2.53) {
		t.Fatalf("the row reads %v before the work has spent anything, want 2.53", got)
	}

	// The family writes its lines, exactly as a running node does.
	appendLedger(t, a.usageLedger, treeFixture()[1:])
	for i := 0; i < 24; i++ {
		drive(t, a, frameMsg{})
	}
	if got := a.spendShown(); !near(got, 53.58) {
		t.Fatalf("the row reads %v after the work spent $51.05, want 53.58", got)
	}
}

// /cost STATES THE SPLIT, so the total is auditable from one place: the two
// halves are printed under it and they add up to it.
func TestCostStatesTheSplitBetweenTheConversationAndItsWork(t *testing.T) {
	a := spendTreeLab(t, 2.53, treeFixture())
	text := a.costText()
	for _, want := range []string{
		"spend", dollars(53.58),
		"conversation", dollars(2.53),
		"tasks", dollars(51.05),
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("/cost does not say %q:\n%s", want, text)
		}
	}
	// The total leads and the halves follow it, because the figure a person is
	// deciding on is the whole bill.
	total := strings.Index(text, dollars(53.58))
	own := strings.Index(text, dollars(2.53))
	work := strings.Index(text, dollars(51.05))
	if total < 0 || own < total || work < own {
		t.Fatalf("/cost does not lead with the total:\n%s", text)
	}
}

// A CONVERSATION THAT HAS STARTED NO WORK HAS NO SPLIT TO STATE. The total IS
// the conversation, and two rows saying so twice — one of them `$0.00` — is the
// emptiness law broken in a note.
func TestCostSaysNothingAboutASplitThatIsNotOne(t *testing.T) {
	a := spendTreeLab(t, 2.53, treeFixture()[:1])
	a.readTreeSpend()
	text := a.costText()
	if !strings.Contains(text, dollars(2.53)) {
		t.Fatalf("/cost lost the conversation's own bill:\n%s", text)
	}
	for _, banned := range []string{"conversation", "tasks", "$0.00"} {
		if strings.Contains(text, banned) {
			t.Fatalf("/cost drew a split with nothing in it — it says %q:\n%s", banned, text)
		}
	}
}

// AND THE SILENCE AROUND THE FIGURE IS NOW TOTAL. The live row used to keep
// `$0.00` so its segments would not jump sideways — the one sanctioned exception
// to the emptiness law, and the exception is gone (ISSUE-126): the row's facts
// are the ticking ones now, they arrive and leave as the work does, and a zero
// bill is a fact nobody has yet. So all three spellings draw nothing.
func TestTheEmptinessLawSurvivesTheTreeReading(t *testing.T) {
	a := spendTreeLab(t, 0, nil)
	a.width = 200
	a.readTreeSpend()

	if line := plain(a.status(a.width)); strings.Contains(line, "$0.00") {
		t.Fatalf("the live row printed a zero bill:\n%s", line)
	}
	if text := a.statusText(); strings.Contains(text, "$0.00") {
		t.Fatalf("/status printed a zero bill:\n%s", text)
	}
	if text := a.costText(); strings.Contains(text, "$0.00") {
		t.Fatalf("/cost printed a zero bill:\n%s", text)
	}
}

// THE FIGURE NEVER GOES BACKWARDS. A conversation whose own books run ahead of
// the ledger — resumed from a journal older than the file, or on a machine whose
// ledger moved — keeps the larger figure rather than dropping to what the file
// can account for.
func TestAConversationWhoseBooksRunAheadKeepsTheLargerFigure(t *testing.T) {
	a := spendTreeLab(t, 12.00, treeFixture()[:1])
	a.readTreeSpend()
	if got := a.spendShown(); !near(got, 12.00) {
		t.Fatalf("the figure dropped to %v when the ledger could only account for 2.53", got)
	}
	// And no split is claimed, because the two halves would not add up to it.
	if _, _, ok := a.spendSplit(); ok {
		t.Fatalf("a split was claimed for a total the ledger does not hold")
	}
}

// appendLedger adds rows to a ledger already on disk, the way a running node
// does — an append, never a rewrite, so the tail read is the thing being tested.
func appendLedger(t *testing.T, path string, lines []session.UsageLine) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}
