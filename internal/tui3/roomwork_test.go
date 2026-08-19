package tui3

// ── THE ROOM SHOWS THE WORK ─────────────────────────────────────────────────
//
// The report these tests hold shut: "when a subharness is built and I click and
// go there I see no discussion of the chat at all, I wait with scrolling text in
// single line and at the end get the result … same for inside a task recursive …
// we can't even see chat in UI".
//
// The room was reading the whole journal and drawing it through the
// conversation's renderers, exactly as room.go promises. What nobody had noticed
// was that the conversation's renderer FOLDS: a completed turn with work and a
// trailing answer collapses to "▸ worked · N tool calls · ctrl+e"
// (workfold.go). A node's life is one long turn ending in a report, so the whole
// page went behind that chip the instant the node stopped running — and a person
// who walked in to watch the work was handed the report and nothing else, which
// is the complaint word for word.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// workedJournal is a node with a life behind it: an instruction, prose, calls,
// an answer, and then a second turn — the landing instruction the runner sends
// when a node is stopped at a checkpoint (internal/session's task_run.go), which
// is what makes the FIRST turn a completed one with an answer at the end of it,
// and therefore foldable.
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

// A FINISHED NODE'S ROOM IS THE WHOLE STORY, NOT THE LAST PARAGRAPH OF IT. This
// is the reproduction: the room is opened on a node whose lane is already over,
// and every earlier turn is a completed one.
func TestAFinishedRoomShowsTheWorkAndNotAWorkedChip(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = workedJournal(t)
	a.workMode = config.WorkFold

	a.openRoom(7, "Draw two posters")
	a.touch()
	drive(t, a, roomClosedMsg{gen: a.room.gen})

	page := roomText(a)
	if strings.Contains(page, "▸ worked") {
		t.Fatalf("the node's work collapsed into a chip:\n%s", page)
	}
	for _, want := range []string{
		"Reading the site first.",
		"read index.html",
		"I have the aesthetic. Generating both.",
		"generate_image",
		"Both rendered at the wrong size.",
		"Here is the honest state of the deliverables.",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the room is missing %q — a person who walked in to read the work:\n%s",
				want, page)
		}
	}
}

// AND IT IS THE WHOLE STORY WHILE THE NODE IS STILL RUNNING TOO. A node between
// steps has completed turns behind it, so the fold bit here as well — the page
// filled in live and then swallowed itself one turn at a time.
func TestARunningRoomShowsTheTurnsBehindTheOneInFlight(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = workedJournal(t)
	a.workMode = config.WorkFold

	a.openRoom(7, "Draw two posters")
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventTextDelta, Text: "Still working.",
	}})

	page := roomText(a)
	if strings.Contains(page, "▸ worked") {
		t.Fatalf("a running node's earlier turns collapsed into a chip:\n%s", page)
	}
	if !strings.Contains(page, "Reading the site first.") || !strings.Contains(page, "Still working.") {
		t.Fatalf("the room lost either its history or its live edge:\n%s", page)
	}
}

// AND ui.work = open CHANGES NOTHING IN HERE, because there was never anything
// to open: the setting is about the conversation's chips, and a room has none.
func TestTheWorkSettingDoesNotReachARoom(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = workedJournal(t)

	for _, mode := range []string{config.WorkFold, config.WorkOpen} {
		a.workMode = mode
		a.openRoom(7, "Draw two posters")
		a.room.dirty = true
		if page := roomText(a); strings.Contains(page, "▸ worked") {
			t.Fatalf("ui.work=%s drew a chip in a room:\n%s", mode, page)
		}
		a.closeRoom()
	}
}

// THE CONVERSATION STILL FOLDS. The exemption is the room's alone, and a change
// that quietly took the chip away from the thread would be trading one
// complaint for its opposite.
func TestTheConversationStillFoldsItsFinishedWork(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.stamps = map[int]turnStamp{1: {took: 47 * time.Second}}
	a.touch()
	if got := strings.Join(plainRows(a), "\n"); !strings.Contains(got, "▸ worked") {
		t.Fatalf("the conversation lost its work chip:\n%s", got)
	}
}
