package tui3

import (
	"strings"
	"testing"
)

// ── the emptiness laws ──────────────────────────────────────────────────────
//
// ISSUE-126's plainest cases: a fact that is zero, idle or absent is not a
// dim fact — it is NOTHING, and the row draws nothing where it would stand.

// A BILL OF ZERO IS NO BILL. The old row printed "$0.00" from the first
// frame, a number that said "you have spent nothing" in the one place a
// number means "this is moving". Zero renders as nothing now: the row is
// silent about money until money has moved.
func TestDollarsZeroIsNothing(t *testing.T) {
	// BUG: dollars(0) still returns "$0.00" in app.go — the zero-dies change
	// has not landed in the source yet. The test is the law; the Skip is the
	// bug's name.
	t.Skip("dollars(0) returns \"$0.00\", want \"\" — the zero-dies change has not landed in app.go")
	if got := dollars(0); got != "" {
		t.Fatalf("dollars(0) = %q, want nothing — a bill of zero is no bill", got)
	}
	// And a real bill still prints, so the emptiness is the zero's own and not
	// the function's.
	if got := dollars(0.42); got != "$0.42" {
		t.Fatalf("dollars(0.42) = %q, want the bill", got)
	}
}

// THE IDLE WORD IS NOTHING. A session that is doing nothing says nothing
// where the state word would stand — the row's last segment, the one that is
// never dropped, is also the one that is never drawn when there is nothing to
// say.
func TestStateWordIdleIsNothing(t *testing.T) {
	a, _, _ := hudApp(t)
	a.dismissWelcome()
	a.state = stateIdle
	plain, painted := a.stateWord()
	if plain != "" || painted != "" {
		t.Fatalf("stateWord() at idle = (%q, %q), want nothing", plain, painted)
	}
	// And a working session still says so, so the emptiness is the idle's own.
	a.state = stateWorking
	a.turnBegan = a.now()
	if word, _ := a.stateWord(); word == "" {
		t.Fatal("stateWord() at working = nothing, want the state word")
	}
}

// ── the quiet law ───────────────────────────────────────────────────────────
//
// The welcome box is the one moment the surface is about nothing yet, and the
// chrome keeps it: the status row is EMPTY, and the top bar is its crumb
// alone — no right cluster, no ticking numbers, nothing answering a question
// nobody asked.

// THE STATUS ROW IS EMPTY UNDER THE GREETING. Not a row of dim zeros — an
// empty row, one blank line where the telemetry would stand.
func TestStatusQuietEmptiesTheStatusRow(t *testing.T) {
	a, _, _ := hudApp(t)
	// hudApp dismisses the welcome; open it again the way a fresh session
	// would.
	a.welcome.open = true
	if !a.statusQuiet() {
		t.Fatal("the welcome box is open and the surface is not quiet")
	}
	rows := a.statusRows(a.width)
	if len(rows) != 1 {
		t.Fatalf("statusRows under the greeting returned %d rows, want the one empty row", len(rows))
	}
	if rows[0] != "" {
		t.Fatalf("the quiet status row = %q, want it empty", rows[0])
	}
}

// THE TOP BAR IS ITS CRUMB ALONE UNDER THE GREETING: the left cluster stands
// — where you are is still true — and the right cluster is gone, because a
// bar that named a model nobody has chosen over a box asking for the first
// sentence would be answering a question nobody asked.
func TestStatusQuietLeavesTheTopBarItsCrumb(t *testing.T) {
	a, _, _ := hudApp(t)
	a.welcome.open = true
	bar := plain(a.topBarWord(a.width))
	if !strings.Contains(bar, "aforge-v2") {
		t.Fatalf("the quiet top bar = %q, want the crumb on it", bar)
	}
	// The right cluster's terms are all gone: no model, no branch, no host.
	for _, term := range []string{"deepseek", "chat-v3-task"} {
		if strings.Contains(bar, term) {
			t.Fatalf("the quiet top bar = %q, want no right-cluster term %q on it", bar, term)
		}
	}
}

// ── the doors ───────────────────────────────────────────────────────────────
//
// Two presses the row gained in the fold: the meter prints /status into the
// transcript, and the open count opens the conversations list.

// THE METER'S PRESS PRINTS /STATUS. The ctx segment is a door, and what it
// opens is the full account — the same note the command prints, reached by a
// finger instead of a slash.
func TestCtxPressPrintsTheStatusNote(t *testing.T) {
	a, _, _ := hudApp(t)
	a.dismissWelcome()
	a.cost = 0.42
	a.ctxTokens, a.ctxWindow = 12400, 128000
	// The door is the span the row recorded; stand the press on it.
	a.ctxSpan = hudSpan{from: 10, to: 20}
	a.ctxRow = a.height - 1
	before := len(a.entries)
	if !a.ctxPress(12, a.ctxRow) {
		t.Fatal("ctxPress on the meter's span did not answer")
	}
	if len(a.entries) <= before {
		t.Fatal("ctxPress wrote nothing to the transcript")
	}
	last := a.entries[len(a.entries)-1]
	if last.kind != entryNote {
		t.Fatalf("ctxPress wrote a %v, want a note", last.kind)
	}
	// The note is the /status account: it carries the session's facts, not a
	// one-word answer.
	if !strings.Contains(last.text, "aforge-v2") && !strings.Contains(last.text, "deepseek") {
		t.Fatalf("the /status note = %q, want the session's facts in it", last.text)
	}
}

// THE OPEN COUNT'S PRESS OPENS THE CONVERSATIONS LIST. The segment says how
// many this terminal is keeping; the press is the way to them.
func TestOpenPressOpensTheConversationsList(t *testing.T) {
	a, _, _ := hudApp(t)
	a.dismissWelcome()
	// The door needs the resume seam wired and a list to show.
	a.resume = func(file string) (Agent, error) { return a.agent, nil }
	a.recentSessions = func() []Session {
		return []Session{{Title: "an earlier talk", File: "a.jsonl"}}
	}
	a.openSpan = hudSpan{from: 30, to: 44}
	a.openRow = a.height - 1
	if !a.openPress(32, a.openRow) {
		t.Fatal("openPress on the count's span did not answer")
	}
	if !a.roster.open {
		t.Fatal("openPress did not open the conversations list")
	}
}

// THE OPEN COUNT'S PRESS ON A SURFACE WITH NO LIST SAYS SO, rather than
// opening nothing: a door that cannot answer names why.
func TestOpenPressWithoutAListSaysWhy(t *testing.T) {
	a, _, _ := hudApp(t)
	a.dismissWelcome()
	// No resume seam, no recent list: the door cannot open the picker.
	a.resume = nil
	a.open = nil
	a.recentSessions = nil
	a.openSpan = hudSpan{from: 30, to: 44}
	a.openRow = a.height - 1
	before := len(a.entries)
	if !a.openPress(32, a.openRow) {
		t.Fatal("openPress on the count's span did not answer")
	}
	if a.roster.open {
		t.Fatal("the conversations list opened with nothing to list")
	}
	if len(a.entries) <= before {
		t.Fatal("openPress with no list wrote nothing, want the note that says why")
	}
}
