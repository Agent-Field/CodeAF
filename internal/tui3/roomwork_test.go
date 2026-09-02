package tui3

// ── THE ROOM FOLDS THE PAST AND KEEPS THE PRESENT WIDE ──────────────────────
//
// These tests used to hold the opposite law shut — A ROOM FOLDS NOTHING — and
// they were right about the defect they were written for and wrong about the
// page. The defect was that the CONVERSATION's chip, which is one chip per
// turn, ate a page that is one turn: a node's life ends in a report, so the
// instant it stopped running everything it had said and done went behind
// "▸ worked · 10 tool calls · ctrl+e" and the person who walked in to watch the
// work was handed the report and nothing else.
//
// The owner ruled the other way on 2026-09-01 (issue #252, ruling 1): the task
// page folds settled work into phase chips by default and the machinery stays
// one keypress away. The chip is spent per SETTLED PHASE now — the work that
// preceded each paragraph the node wrote — so the machinery collapses, every
// paragraph stays standing, and the live frontier does not fold at all. That
// answers the original complaint without building the page on the premise that
// every call has to be read.
//
// So these tests are rewritten WITH the design: what folds, what may never
// fold, and every door that opens what did.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// workedJournal is a node with a life behind it: an instruction, then two
// stretches of work each ending in a paragraph of the node's own prose, then the
// landing instruction the runner sends when a node is stopped at a checkpoint
// (internal/session's task_run.go) and the report it answers with.
//
// TWO PHASES AND A REPORT is the shape the page is designed against: the first
// two paragraphs are what the node said as it went, and the last is what it came
// home with.
func workedJournal(t *testing.T) string {
	t.Helper()
	return roomJournal(t,
		`{"type":"message","role":"user","content":"Draw two posters"}`,
		`{"type":"message","role":"assistant","content":"Reading the site first.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"index.html\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"98 lines"}`,
		`{"type":"message","role":"assistant","content":"I have the aesthetic. Generating both.","toolCalls":[{"id":"c2","function":{"name":"generate_image","arguments":"{\"prompt\":\"poster\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c2","content":"wrote poster.png"}`,
		`{"type":"message","role":"assistant","content":"Both rendered at the wrong size."}`,
		`{"type":"message","role":"user","content":"LAND NOW."}`,
		`{"type":"message","role":"assistant","content":"Here is the honest state of the deliverables."}`,
	)
}

// openWorked opens the room on [workedJournal] with its lane already over, which
// is the state every one of these reads the page in unless it says otherwise.
func openWorked(t *testing.T) *app {
	t.Helper()
	a, fake, _ := roomApp(t)
	fake.journal = workedJournal(t)
	a.workMode = config.WorkFold
	a.openRoom(7, "Draw two posters")
	a.touch()
	drive(t, a, roomClosedMsg{gen: a.room.gen})
	return a
}

// A FINISHED ROOM READS AS WHAT THE WORK CAME TO, WITH THE MACHINERY FILED. The
// paragraphs the node wrote stand, the report stands, and the calls between them
// are behind chips that say what they cost.
func TestAFinishedRoomFoldsSettledPhasesAndLeavesTheProseStanding(t *testing.T) {
	a := openWorked(t)
	page := roomText(a)

	for _, want := range []string{
		"Reading the site first.",
		"I have the aesthetic. Generating both.",
		"Both rendered at the wrong size.",
		"Here is the honest state of the deliverables.",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("a fold hid what the node SAID (%q):\n%s", want, page)
		}
	}
	if strings.Contains(page, "index.html") || strings.Contains(page, "generate_image") {
		t.Fatalf("the settled calls are still on the page:\n%s", page)
	}
	// THE CHIP'S GRAMMAR IS THE CONVERSATION'S, counted and never paraphrased.
	if n := strings.Count(page, "▸ worked"); n != 2 {
		t.Fatalf("want one chip per settled phase, got %d:\n%s", n, page)
	}
	if !strings.Contains(page, "1 tool call · ctrl+e") {
		t.Fatalf("the chip does not count its calls or name its door:\n%s", page)
	}
}

// AND THE REPORT IS NEVER INSIDE ONE. It is the last paragraph, so the last chip
// stops at it — the one line a person opens a landed task for.
func TestTheFinalReportNeverFolds(t *testing.T) {
	a := openWorked(t)
	page := roomText(a)
	report := strings.Index(page, "Here is the honest state of the deliverables.")
	if report < 0 {
		t.Fatalf("the report is gone:\n%s", page)
	}
	if last := strings.LastIndex(page, "▸ worked"); last > report {
		t.Fatalf("a chip was drawn after the report:\n%s", page)
	}
}

// A RUNNING ROOM FOLDS WHAT IS BEHIND THE FRONTIER AND LEAVES THE FRONTIER WIDE.
// The person watching NOW is the one reader for whom the machinery is the
// content, so everything after the last settled paragraph keeps every row.
func TestARunningRoomFoldsThePastAndKeepsTheFrontierWide(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = workedJournal(t)
	a.workMode = config.WorkFold

	a.openRoom(7, "Draw two posters")
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "bash", CallID: "live", Hint: "bash convert", Args: `{"cmd":"convert"}`,
	}})
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventTextDelta, Text: "Still working.",
	}})

	page := roomText(a)
	if !strings.Contains(page, "▸ worked") {
		t.Fatalf("a running node's settled phases did not fold:\n%s", page)
	}
	if !strings.Contains(page, "Reading the site first.") || !strings.Contains(page, "Still working.") {
		t.Fatalf("the room lost either its history or its live edge:\n%s", page)
	}
	// THE LIVE CALL IS ON THE PAGE. It arrived after the last settled paragraph,
	// so no chip may cover it.
	if !strings.Contains(page, "bash") {
		t.Fatalf("the frontier's own call was folded away:\n%s", page)
	}
}

// THE BRIEF, A FAILED CALL AND A CORRECTION ARE NEVER FOLDED. Each is one of the
// five acts and each has its own reason, so each is asked separately.
func TestTheBriefAFailureAndAnElbowNeverFold(t *testing.T) {
	base := time.Unix(100, 0)
	work := func(extra ...entry) []entry {
		out := []entry{
			{kind: entryUser, text: "Draw two posters", turn: 1, brief: true},
			{kind: entryTool, tool: "read", turn: 1, status: toolOK, began: base, ended: base.Add(time.Second)},
		}
		out = append(out, extra...)
		return append(out, entry{kind: entryAssistant, text: "Both rendered.", turn: 1, settled: true})
	}
	cases := []struct {
		name    string
		entries []entry
		folds   int
	}{
		{"plain work folds", work(), 1},
		{"a failed call does not", work(entry{kind: entryTool, tool: "bash", turn: 1, status: toolFailed}), 0},
		{"an ask does not", work(entry{kind: entryTask, text: "may I?", turn: 1}), 0},
		{"a call still in flight does not", work(entry{kind: entryTool, tool: "bash", turn: 1, status: toolRunning}), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			folds := derivePhaseFolds(c.entries)
			if len(folds) != c.folds {
				t.Fatalf("want %d chips, got %d", c.folds, len(folds))
			}
			for _, f := range folds {
				if f.start == 0 {
					t.Fatalf("a chip started on the brief — the person's own words")
				}
			}
		})
	}
	// AND A CORRECTION BREAKS THE RUN IT LANDED IN. The elbow is the overseer's
	// primary act; a chip that covered it would be hiding the person's own words.
	steered := []entry{
		{kind: entryUser, text: "Draw two posters", turn: 1, brief: true},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK, began: base, ended: base.Add(time.Second)},
		{kind: entrySteer, text: "portrait, not landscape", turn: 1, steer: &steerElbow{}},
		{kind: entryAssistant, text: "Both rendered.", turn: 1, settled: true},
	}
	if folds := derivePhaseFolds(steered); len(folds) != 0 {
		t.Fatalf("a chip covered a correction: %v", folds)
	}
}

// EVERY DOOR OPENS A CHIP, because the disclosure ladder may never dead-end:
// ctrl+e opens the newest, and a scroll up at the top of the page opens the one
// nearest the top.
func TestCtrlEAndScrollUpBothOpenAPhaseChip(t *testing.T) {
	a := openWorked(t)
	if strings.Contains(roomText(a), "generate_image") {
		t.Fatalf("the page did not start folded")
	}
	if !a.toggleLatestWorkfold() {
		t.Fatal("ctrl+e found no chip to open")
	}
	if page := roomText(a); !strings.Contains(page, "generate_image") {
		t.Fatalf("ctrl+e did not open the newest chip:\n%s", page)
	}

	// The scroll gesture, from the top, opens the chip nearest the top — the one
	// ctrl+e did not take.
	b := openWorked(t)
	b.room.offset, b.room.stick = 0, false
	b.roomScroll(-1)
	if page := roomText(b); !strings.Contains(page, "index.html") {
		t.Fatalf("scrolling up at the top opened no chip:\n%s", page)
	}
}

// ui.work = open BEHAVES IN A ROOM EXACTLY AS IT DOES IN THE CONVERSATION, which
// is the answer this change owes: the setting says "I never want work folded",
// and a page that ignored it would be the surface keeping a second opinion about
// a preference the person already stated.
func TestTheWorkSettingOpensARoomsChipsToo(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = workedJournal(t)
	for _, mode := range []string{config.WorkFold, config.WorkOpen} {
		a.workMode = mode
		a.openRoom(7, "Draw two posters")
		a.touch()
		drive(t, a, roomClosedMsg{gen: a.room.gen})
		page := roomText(a)
		if hidden := !strings.Contains(page, "generate_image"); hidden != (mode == config.WorkFold) {
			t.Fatalf("ui.work=%s drew the wrong page:\n%s", mode, page)
		}
		// THE CHIP STAYS EITHER WAY. It is the door and the receipt, and a page
		// that removed it when the work was open would leave no way back.
		if !strings.Contains(page, "▸ worked") {
			t.Fatalf("ui.work=%s lost the chip:\n%s", mode, page)
		}
		a.closeRoom()
	}
}

// THE CONVERSATION STILL FOLDS BY TURN. The room's phases are the room's; a
// change that quietly re-cut the thread's chips would be trading one complaint
// for its opposite.
func TestTheConversationStillFoldsItsFinishedWork(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.stamps = map[int]turnStamp{1: {took: 47 * time.Second}}
	a.touch()
	if got := strings.Join(plainRows(a), "\n"); !strings.Contains(got, "▸ worked 47s") {
		t.Fatalf("the conversation lost its work chip:\n%s", got)
	}
}
