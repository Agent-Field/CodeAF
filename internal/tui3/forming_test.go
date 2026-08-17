package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE FORMING PHASE, checked where it is visible.
//
// One claim, and every test here is a corner of it: a call that is ARRIVING is
// on screen while it arrives, in a row that becomes the announced row rather
// than being replaced by one — and a call that never finishes arriving resolves
// instead of pulsing forever.

// forming is one EventToolForming fragment, as session sends them: the id and
// the name as far as the wire has said them, the gloss built from the argument
// fields that have closed, and the raw partial text with its size.
func forming(id, tool, hint, args string) session.Event {
	return session.Event{
		Kind:     session.EventToolForming,
		CallID:   id,
		Tool:     tool,
		Hint:     hint,
		ArgsText: args,
		Bytes:    len(args),
	}
}

// formingTurn is a surface with a turn running and nothing scripted in it, so
// the test itself is the stream.
func formingTurn(t *testing.T) (*app, *fakeAgent) {
	t.Helper()
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.pal = newPalette(tokens.ANSI256, false)
	typeLine(t, a, "go on then")
	return a, agent
}

// formingRow is the row of the call that is arriving, plain.
//
// It is found by its RAIL rather than by its hit kind, because a forming row
// deliberately answers no pointer: there is nothing behind it to open until the
// announcement brings the payload (toolview.go's [app.toolRows]).
func formingRow(t *testing.T, a *app) string {
	t.Helper()
	return toolRowAt(t, a)
}

func toolEntries(a *app) int {
	n := 0
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			n++
		}
	}
	return n
}

// ── 1. the row, from the first fragment to the last ─────────────────────────

// A LONG CALL IS VISIBLE WHILE IT IS BEING WRITTEN, and it is the same row all
// the way through: bytes, then a name, then the announcement, then execution.
// A second row anywhere in that walk would be the surface saying the model
// called the tool twice.
func TestAFormingCallDrawsOneRowThatFillsIn(t *testing.T) {
	a, agent := formingTurn(t)

	// FRAGMENT ONE: the wire has an id and nothing else. The row exists anyway,
	// because "something is arriving" is the fact the old surface could not say.
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "", "", strings.Repeat("x", 40))})
	if toolEntries(a) != 1 {
		t.Fatalf("a forming call drew %d rows, want 1", toolEntries(a))
	}
	at := firstTool(t, a)
	if got := a.entries[at].status; got != toolForming {
		t.Fatalf("an arriving call is in state %v, want forming", got)
	}
	line := formingRow(t, a)
	if !strings.Contains(line, "receiving · 40 B") {
		t.Fatalf("a nameless forming row does not say what has arrived: %q", line)
	}
	if !strings.Contains(line, glyphQueued) {
		t.Fatalf("a forming row has no ◌: %q", line)
	}
	if strings.ContainsAny(line, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("A FORMING CALL IS SPINNING. Nothing has started: %q", line)
	}

	// THE COUNTER TICKS, and the name lands in front of it: the same call, more
	// of it, said in more words.
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "write", "", strings.Repeat("x", 1300))})
	if line := formingRow(t, a); !strings.Contains(line, "write · 1.3 KB") {
		t.Fatalf("the byte counter did not move: %q", line)
	}

	// THE FIELD CLOSES AND THE HINT TAKES OVER: what the call is about leads,
	// and the state trails it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "write", "write internal/tui3/app.go", strings.Repeat("x", 2600))})
	line = formingRow(t, a)
	if !strings.Contains(line, "write internal/tui3/app.go · receiving") {
		t.Fatalf("the gloss did not take the row: %q", line)
	}
	if toolEntries(a) != 1 {
		t.Fatalf("the hint landing drew %d rows, want 1", toolEntries(a))
	}

	// ANNOUNCED: the same row, in place, with the payload it was missing.
	args := `{"path":"internal/tui3/app.go","content":"package tui3\n"}`
	drive(t, a, streamEventMsg{gen: a.gen, ev: announced("write", "write internal/tui3/app.go", args)})
	if toolEntries(a) != 1 {
		t.Fatalf("THE ANNOUNCEMENT DREW A SECOND ROW: %d rows for one call", toolEntries(a))
	}
	if got := a.entries[at].status; got != toolQueued {
		t.Fatalf("the announced call is in state %v, want queued", got)
	}
	if a.entries[at].detail.Args != args {
		t.Fatalf("the announced row did not take the payload: %q", a.entries[at].detail.Args)
	}
	if line := formingRow(t, a); strings.Contains(line, receivingWord) {
		t.Fatalf("an announced row still says it is arriving: %q", line)
	}

	// And the rest of the walk is untouched: begin spins the row it already has.
	drive(t, a, streamEventMsg{gen: a.gen, ev: beginWith("write", "write internal/tui3/app.go", args)})
	if got := a.entries[at].status; got != toolRunning {
		t.Fatalf("the begun call is in state %v, want running", got)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: toolEnd("write", "wrote 1 file")})
	if got := a.entries[at].status; got != toolOK {
		t.Fatalf("the finished call is in state %v, want ok", got)
	}
	if toolEntries(a) != 1 {
		t.Fatalf("one call drew %d rows over its whole life", toolEntries(a))
	}
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// A PARALLEL BATCH KEEPS ITS ROWS APART, and the id is what keeps them apart:
// two writes arriving fragment by fragment are two calls, and a surface that
// matched them by name would fold both into whichever was drawn first.
func TestTwoFormingCallsKeepTheirOwnRows(t *testing.T) {
	a, agent := formingTurn(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: forming("c1", "write", "", strings.Repeat("x", 20))},
		streamEventMsg{gen: a.gen, ev: forming("c2", "write", "", strings.Repeat("x", 30))},
		streamEventMsg{gen: a.gen, ev: forming("c1", "write", "write one.go", strings.Repeat("x", 900))},
		streamEventMsg{gen: a.gen, ev: forming("c2", "write", "write two.go", strings.Repeat("x", 40))},
	)
	if toolEntries(a) != 2 {
		t.Fatalf("two parallel calls drew %d rows, want 2", toolEntries(a))
	}
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "write one.go · receiving") || !strings.Contains(body, "write two.go · receiving") {
		t.Fatalf("the two forming calls do not read as two calls:\n%s", body)
	}

	// Both announced, oldest first, and still two rows.
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: announced("write", "write one.go", `{"path":"one.go"}`)},
		streamEventMsg{gen: a.gen, ev: announced("write", "write two.go", `{"path":"two.go"}`)},
	)
	if toolEntries(a) != 2 {
		t.Fatalf("announcing two formed calls left %d rows, want 2", toolEntries(a))
	}
	for i := range a.entries {
		if e := &a.entries[i]; e.kind == entryTool && e.status != toolQueued {
			t.Fatalf("row %q is in state %v after its announcement, want queued", e.text, e.status)
		}
	}
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// ── 2. the stream that dies mid-call ────────────────────────────────────────

// A CALL THAT NEVER ARRIVED IS RESOLVED, NOT LEFT PULSING. Nothing is coming
// for it — no announcement, no begin, no end — so the turn ending is the only
// event it will ever get, and the row says what happened instead of animating
// at a model that has stopped speaking.
func TestAFormingCallResolvesWhenTheStreamDies(t *testing.T) {
	a, agent := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "write", "write app.go", strings.Repeat("x", 700))})
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	at := firstTool(t, a)
	if e := &a.entries[at]; e.forming() {
		t.Fatal("a forming row outlived its stream")
	}
	line := formingRow(t, a)
	if !strings.Contains(line, cancelledWord) {
		t.Fatalf("a call that never arrived does not say so: %q", line)
	}
	if strings.Contains(line, glyphQueued) {
		t.Fatalf("a dead forming row is still drawing the arriving mark: %q", line)
	}
	if !strings.Contains(line, glyphIdle) {
		t.Fatalf("a dead forming row has no resolved mark: %q", line)
	}
	if a.running() {
		t.Fatal("the surface still counts a dead forming row as something running")
	}
}

// ── 3. the phone ────────────────────────────────────────────────────────────

// FORTY-FOUR COLUMNS, ONE LINE, and the target elided the way every other tool
// row elides one at this tier (port/p2's law).
func TestAFormingRowIsCompactOnAPhone(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.width = phoneWidth
	a.pal = newPalette(tokens.ANSI256, false)
	typeLine(t, a, "go on then")
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "write", "write internal/session/loop.go", strings.Repeat("x", 4096))})

	line := formingRow(t, a)
	if width := len([]rune(line)); width > phoneWidth {
		t.Fatalf("a forming row is %d cells wide at a phone's %d: %q", width, phoneWidth, line)
	}
	if !strings.Contains(line, "loop.go · "+receivingWord) {
		t.Fatalf("the phone's forming row does not read as one sentence: %q", line)
	}
	if strings.Contains(line, "internal/session/loop.go") {
		t.Fatalf("the phone kept the whole path: %q", line)
	}
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// ── 4. the spawn card ───────────────────────────────────────────────────────

// THE CARD GROWS WHERE IT WILL STAND. A groomed proposal is the longest call
// the model makes, and the block used to appear whole, question-hued, under
// whatever was being read. It now opens dim the moment the call starts
// arriving, takes its title as the title field closes, and BECOMES the question
// when the engine sends one.
func TestTheSpawnCardFormsBeforeItAsks(t *testing.T) {
	a, agent := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, "", strings.Repeat("x", 60))})

	card := a.formingCard()
	if card == nil {
		t.Fatalf("no card was drawn while propose_task arrived:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if a.task != nil {
		t.Fatal("A FORMING CARD TOOK THE ANSWER LANE. There is nothing to answer yet.")
	}
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, taskFormingWord) {
		t.Fatalf("the forming card does not say what it is doing:\n%s", body)
	}
	if !strings.Contains(body, taskFormingName) {
		t.Fatalf("the forming card has no word in its head:\n%s", body)
	}
	if strings.Contains(body, "[ yes ]") {
		t.Fatalf("a forming card is offering answers:\n%s", body)
	}

	// The title field closes: the head takes the name, in place.
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" Fix the nil-map crash", strings.Repeat("x", 900))})
	if cards := taskEntries(a); cards != 1 {
		t.Fatalf("the title landing drew %d cards, want 1", cards)
	}
	if body := strings.Join(plainRows(a), "\n"); !strings.Contains(body, "Fix the nil-map") {
		t.Fatalf("the card did not take the title:\n%s", body)
	}

	// AND THE PROPOSAL TAKES OVER THE SAME BLOCK: one card, now a question.
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 30*time.Second)})
	if cards := taskEntries(a); cards != 1 {
		t.Fatalf("THE PROPOSAL DREW A SECOND CARD: %d cards for one proposal", cards)
	}
	if a.task == nil || a.task.forming {
		t.Fatalf("the landed proposal did not take the answer lane: %+v", a.task)
	}
	body = strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "[ yes ]") {
		t.Fatalf("the proposal is not asking:\n%s", body)
	}
	if strings.Contains(body, taskFormingWord) {
		t.Fatalf("the landed proposal is still forming:\n%s", body)
	}
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// A PROPOSAL THAT NEVER ARRIVED SETTLES, for the reason a forming row does: the
// model began asking for work and the turn ended before it finished, which is a
// fact — and a block left pulsing over a dead turn is not one.
func TestAFormingCardResolvesWhenTheStreamDies(t *testing.T) {
	a, agent := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" Fix the nil-map crash", "{\"title\":\"Fix\"")})
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	if a.formingCard() != nil {
		t.Fatal("a forming card outlived its stream")
	}
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, taskFormingLost) {
		t.Fatalf("the abandoned proposal does not say what happened:\n%s", body)
	}
	if strings.Contains(body, taskFormingWord) {
		t.Fatalf("the abandoned proposal is still forming:\n%s", body)
	}
}

// taskEntries counts the proposal blocks on the transcript.
func taskEntries(a *app) int {
	n := 0
	for i := range a.entries {
		if a.entries[i].kind == entryTask {
			n++
		}
	}
	return n
}

// ── 5. the ink ──────────────────────────────────────────────────────────────

// THE PULSE IS A PULSE, and it is still in the linear tier: a surface being
// read aloud hears an animation as the same claim repeated forever.
func TestTheFormingMarkPulsesAndTheLinearTierIsStill(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.ANSI256, false)

	a.paints = 0
	quiet := a.formingInk(glyphQueued)
	a.paints = pulseStep
	lit := a.formingInk(glyphQueued)
	if quiet == lit {
		t.Fatalf("the forming mark does not pulse: %q at both ends of the tick", plain(quiet))
	}

	a.linear = true
	a.paints = 0
	first := a.formingInk(glyphQueued)
	a.paints = pulseStep
	if second := a.formingInk(glyphQueued); first != second {
		t.Fatalf("THE LINEAR TIER IS ANIMATING: %q then %q", plain(first), plain(second))
	}
}
