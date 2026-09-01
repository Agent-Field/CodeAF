package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

// plainStringRowsText keeps the forming-card string rows distinct from the
// typed forming-block rows used by taskcommand_test.go.
func plainStringRowsText(rows []string) string {
	var out []string
	for _, line := range rows {
		out = append(out, plain(line))
	}
	return strings.Join(out, "\n")
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

// C1, C2, C3, C4 AND C9: the forming card's row moves, its clock floors cleanly, and its head stays still.
func TestTheFormingCardMovesBetweenPaints(t *testing.T) {
	base := time.Now()
	a, agent := formingTurn(t)
	a.clock = func() time.Time { return base }
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" Fix the nil-map crash", "{\"title\":\"Fix\"")})

	card := a.formingCard()
	if card == nil {
		t.Fatal("the forming fragment raised no card")
	}
	a.paints = 0
	a.clock = func() time.Time { return base.Add(500 * time.Millisecond) }
	firstRows := a.taskFormingRows(card, 60, false)
	first := plainStringRowsText(firstRows)
	if row := plain(firstRows[1]); strings.Contains(row, "0s") || strings.Contains(row, taskFormingWord+" ·") {
		t.Fatalf("a forming card under one second is timing itself: %q", row)
	}
	if !strings.ContainsAny(first, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the sub-second forming card has no moving braille mark: %q", first)
	}

	a.paints = spinnerStep
	a.clock = func() time.Time { return base.Add(2 * time.Second) }
	second := plainStringRowsText(a.taskFormingRows(card, 60, false))
	if first == second {
		t.Fatalf("the forming card is the same picture two paints apart: %q", first)
	}
	if !strings.Contains(second, taskFormingWord+" · 2s") {
		t.Fatalf("the forming card did not count from its first fragment: %q", second)
	}
	// Hold elapsed time still so only the spinner can make these pictures differ.
	a.paints = 0
	sameTime := plainStringRowsText(a.taskFormingRows(card, 60, false))
	if sameTime == second {
		t.Fatalf("the forming card's mark stayed still while its clock was fixed: %q", second)
	}
	if !strings.ContainsAny(second, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the moving forming row has no braille spinner: %q", second)
	}

	// The header's empty ident slot is a fact, not a second liveness signal.
	a.paints = 0
	head := a.taskFormingHead(card, 60, false)
	a.paints = pulseStep
	if got := a.taskFormingHead(card, 60, false); got != head {
		t.Fatalf("the forming card's head moved with the old pulse: %q then %q", plain(head), plain(got))
	}

	// The linear tier keeps one still mark while the number continues to climb.
	a.linear = true
	a.paints = 0
	a.clock = func() time.Time { return base.Add(2 * time.Second) }
	still := plainStringRowsText(a.taskFormingRows(card, 60, false))
	if !strings.Contains(still, glyphRunASCII+" "+taskFormingWord+" · 2s") {
		t.Fatalf("the linear forming card has no still running mark: %q", still)
	}
	a.paints = spinnerStep
	if got := plainStringRowsText(a.taskFormingRows(card, 60, false)); got != still {
		t.Fatalf("the linear tier animated the forming card's mark: %q then %q", still, got)
	}
	a.clock = func() time.Time { return base.Add(4 * time.Second) }
	if got := plainStringRowsText(a.taskFormingRows(card, 60, false)); !strings.Contains(got, glyphRunASCII+" "+taskFormingWord+" · 4s") {
		t.Fatalf("the linear forming card's clock did not climb: %q", got)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// C5 AND C6: a forming card starts and keeps the frame clock alive independently, then releases it when settled.
func TestTheFormingCardKeepsTheFrameClockTurning(t *testing.T) {
	a, agent := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" Fix the nil-map crash", "{\"title\":\"Fix\"")})
	if a.formingCard() == nil {
		t.Fatal("the forming fragment raised no card")
	}

	a.painting = false
	model, cmd := a.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a = model.(*app)
	framed := false
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(frameMsg); ok {
			framed = true
		}
	}
	if !framed {
		t.Fatal("the forming card is up with no frame on the way")
	}
	if !a.painting {
		t.Fatal("the forming card never claimed the frame clock")
	}

	a.state = stateIdle
	if cmd := a.paint(); cmd == nil {
		t.Fatal("an idle surface stopped painting while its forming card was still moving")
	}
	a.dropFormingCard()
	if cmd := a.paint(); cmd != nil {
		t.Fatal("a settled forming card kept the frame clock turning on its account")
	}

	a.state = stateWorking
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// REVIEW 4: the derived clock question stops at the live turn instead of searching old transcript entries.
func TestTheFormingCardLookupStopsAtTheLiveTurn(t *testing.T) {
	a, agent := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" Fix the nil-map crash", "{\"title\":\"Fix\"")})
	if a.formingCard() == nil {
		t.Fatal("the forming fragment raised no card")
	}

	oldTurn := a.turn
	a.turn++
	a.state = stateIdle
	if a.formingCard() != nil || a.formingCardLive() {
		t.Fatal("an older turn's forming card was mistaken for the live turn's card")
	}
	if cmd := a.paint(); cmd != nil {
		t.Fatal("an older turn's forming card kept the frame clock turning")
	}

	a.turn = oldTurn
	a.state = stateWorking
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// C7: an open forming card bypasses the row cache and a settled one rejoins it.
func TestTheFormingCardIsNeverServedFromTheRowCache(t *testing.T) {
	a, agent := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" Fix the nil-map crash", "{\"title\":\"Fix\"")})

	at := -1
	for i := range a.entries {
		if a.entries[i].kind == entryTask {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("the forming fragment raised no task entry")
	}
	d := a.conversation()
	before := a.renders
	_ = a.entryRows(d, at, 60)
	_ = a.entryRows(d, at, 60)
	if got := a.renders - before; got != 2 {
		t.Fatalf("the open forming card rendered %d times for two reads, want 2", got)
	}
	if a.entries[at].built {
		t.Fatal("the open forming card built a row-cache entry")
	}

	a.dropFormingCard()
	_ = a.entryRows(d, at, 60)
	if !a.entries[at].built {
		t.Fatal("the settled forming card did not rejoin the row cache")
	}
	before = a.renders
	_ = a.entryRows(d, at, 60)
	if got := a.renders - before; got != 0 {
		t.Fatalf("the cache re-rendered a settled forming card %d times", got)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// C8: retrying discards the half-arrived call, so its forming card disappears
// and the replacement attempt starts a separate card and clock.
func TestARetryDiscardsTheFormingCard(t *testing.T) {
	base := time.Now()
	a, agent := formingTurn(t)
	a.clock = func() time.Time { return base }
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" Fix the nil-map crash", "{\"title\":\"Fix\"")})

	card := a.formingCard()
	if card == nil {
		t.Fatal("the forming fragment raised no card")
	}
	a.clock = func() time.Time { return base.Add(8 * time.Second) }
	if got := plainStringRowsText(a.taskFormingRows(card, 60, false)); !strings.Contains(got, taskFormingWord+" · 8s") {
		t.Fatalf("the first attempt's clock never advanced: %q", got)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventRetrying, Text: "asking again"}})

	if card = a.formingCard(); card != nil {
		t.Fatalf("retrying kept the dead attempt's forming card: %+v", card)
	}
	if got := plainStringRowsText(plainRows(a)); strings.Contains(got, taskFormingWord) || strings.Contains(got, "Fix the nil-map") {
		t.Fatalf("the discarded proposal is still on screen: %q", got)
	}
	if a.formingCardLive() {
		t.Fatal("the discarded card still claimed the frame clock")
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c2", taskTool, taskTool+" Retry cleanly", "{\"title\":\"Retry\"")})
	card = a.formingCard()
	if card == nil {
		t.Fatal("the replacement attempt raised no fresh forming card")
	}
	immediate := plainStringRowsText(a.taskFormingRows(card, 60, false))
	if strings.Contains(immediate, "8s") || strings.Contains(immediate, "0s") {
		t.Fatalf("the replacement card kept the dead attempt's clock: %q", immediate)
	}
	if !strings.Contains(immediate, "Retry cleanly") {
		t.Fatalf("the replacement card has the wrong title: %q", immediate)
	}
	a.clock = func() time.Time { return base.Add(10 * time.Second) }
	if got := plainStringRowsText(a.taskFormingRows(card, 60, false)); !strings.Contains(got, taskFormingWord+" · 2s") {
		t.Fatalf("the replacement card did not count from its first fragment: %q", got)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// REVIEW 1: a refused propose_task result settles its card and releases the frame clock.
func TestARefusedTaskCallSettlesItsFormingCard(t *testing.T) {
	base := time.Now()
	a, agent := formingTurn(t)
	a.clock = func() time.Time { return base }
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" Fix the nil-map crash", "{\"title\":\"Fix\"")})

	card := a.formingCard()
	if card == nil {
		t.Fatal("the forming fragment raised no card")
	}
	a.clock = func() time.Time { return base.Add(8 * time.Second) }
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolFailed, Tool: taskTool,
		Hint: "Invalid arguments: brief is required", Err: errors.New("invalid arguments"),
	}})

	if a.formingCard() != nil {
		t.Fatal("a refused propose_task left its card forming")
	}
	settled := plainStringRowsText(a.taskCardRows(card, 60, false))
	if !strings.Contains(settled, taskFormingRefused) {
		t.Fatalf("the refused proposal does not say what happened: %q", settled)
	}
	if strings.Contains(settled, taskFormingWord) || strings.ContainsAny(settled, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the refused proposal still looks alive: %q", settled)
	}
	a.paints += spinnerStep
	a.clock = func() time.Time { return base.Add(10 * time.Second) }
	if got := plainStringRowsText(a.taskCardRows(card, 60, false)); got != settled {
		t.Fatalf("the refused proposal kept moving: %q then %q", settled, got)
	}
	a.state = stateIdle
	if cmd := a.paint(); cmd != nil {
		t.Fatal("the refused proposal kept the frame clock turning on its account")
	}

	a.state = stateWorking
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// REVIEW 2: a different propose_task call in the same turn starts its own elapsed clock.
func TestANewTaskCallRestartsTheFormingCardsClock(t *testing.T) {
	base := time.Now()
	a, agent := formingTurn(t)
	a.clock = func() time.Time { return base }
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool, taskTool+" First proposal", "{\"title\":\"First\"")})

	card := a.formingCard()
	if card == nil {
		t.Fatal("the first forming fragment raised no card")
	}
	a.clock = func() time.Time { return base.Add(8 * time.Second) }
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c2", taskTool, taskTool+" Corrected proposal", "{\"title\":\"Corrected\"")})

	if got := a.formingCard(); got != card {
		t.Fatal("the corrected call did not reuse the forming block")
	}
	if card.callID != "c2" {
		t.Fatalf("the forming card kept call id %q, want c2", card.callID)
	}
	immediate := plainStringRowsText(a.taskFormingRows(card, 60, false))
	if strings.Contains(immediate, "8s") || strings.Contains(immediate, "0s") {
		t.Fatalf("the corrected call inherited the first call's clock: %q", immediate)
	}
	a.clock = func() time.Time { return base.Add(10 * time.Second) }
	if got := plainStringRowsText(a.taskFormingRows(card, 60, false)); !strings.Contains(got, taskFormingWord+" · 2s") {
		t.Fatalf("the corrected call did not count from its own first fragment: %q", got)
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

// ── 6. the room ─────────────────────────────────────────────────────────────
//
// The gap this closes: a person opens a node's room BECAUSE they want to watch
// it work, and the room's own lane ignored the forming kind — so a node
// streaming a file out said nothing at all for the seconds that took, in the one
// place on the surface a person went to look. Everything below is the same law
// as the four sections above, asserted through the room's list (room.go).

// roomToolEntries counts the tool blocks on the open room's page.
func roomToolEntries(a *app) int {
	n := 0
	for i := range a.room.entries {
		if a.room.entries[i].kind == entryTool {
			n++
		}
	}
	return n
}

// A NODE'S CALL IS VISIBLE WHILE IT ARRIVES, in one row that fills in — the
// conversation's walk, run in the room.
func TestARoomDrawsTheNodesFormingCall(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	drive(t, a, roomEventMsg{gen: a.room.gen, ev: forming("c1", "", "", strings.Repeat("x", 40))})
	if roomToolEntries(a) != 1 {
		t.Fatalf("a node's forming call drew %d rows in its room, want 1", roomToolEntries(a))
	}
	if page := roomText(a); !strings.Contains(page, "receiving · 40 B") {
		t.Fatalf("the room says nothing about the call arriving:\n%s", page)
	}

	// The name, then the gloss: the same row, more of it.
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: forming("c1", "write", "", strings.Repeat("x", 1300))})
	if page := roomText(a); !strings.Contains(page, "write · 1.3 KB") {
		t.Fatalf("the room's byte counter did not move:\n%s", page)
	}
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: forming("c1", "write", "write internal/config/load.go", strings.Repeat("x", 2600))})
	if page := roomText(a); !strings.Contains(page, "write internal/config/load.go · receiving") {
		t.Fatalf("the gloss did not take the room's row:\n%s", page)
	}
	if roomToolEntries(a) != 1 {
		t.Fatalf("three fragments of one call drew %d rows", roomToolEntries(a))
	}
}

// AND THE ANNOUNCEMENT ADOPTS IT, by the id session now stamps on it (its
// loop.go). One call, one line, from the first fragment to the last.
func TestARoomsAnnouncementAdoptsTheFormingRow(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	args := `{"path":"internal/config/load.go","content":"package config\n"}`
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: forming("c1", "write", "write internal/config/load.go", `{"path":"internal/config/load.go",`)})
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolAnnounced, CallID: "c1", Tool: "write",
		Hint: "write internal/config/load.go", Args: args,
	}})

	if roomToolEntries(a) != 1 {
		t.Fatalf("THE ANNOUNCEMENT DREW A SECOND ROW: %d rows for one call", roomToolEntries(a))
	}
	e := &a.room.entries[len(a.room.entries)-1]
	if e.status != toolQueued {
		t.Fatalf("the announced call is in state %v, want queued", e.status)
	}
	if e.detail.Args != args {
		t.Fatalf("the announced row did not take the payload: %q", e.detail.Args)
	}
	if page := roomText(a); strings.Contains(page, receivingWord) {
		t.Fatalf("an announced row still says it is arriving:\n%s", page)
	}

	// And the rest of the walk is what it was: begin spins the row it has.
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "write", Hint: "write internal/config/load.go", Args: args,
	}})
	if roomToolEntries(a) != 1 || a.room.entries[len(a.room.entries)-1].status != toolRunning {
		t.Fatalf("the begin did not land on the announced row: %d rows, state %v",
			roomToolEntries(a), a.room.entries[len(a.room.entries)-1].status)
	}
}

// TWO PARALLEL WRITES ARE TWO ROWS in a room as well: the ids are what tells
// them apart, and an announcement out of drawing order lands on its own call.
func TestARoomPairsParallelCallsByTheirIDs(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	drive(t, a, roomEventMsg{gen: a.room.gen, ev: forming("c1", "write", "write a.go", `{"path":"a.go",`)})
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: forming("c2", "write", "write b.go", `{"path":"b.go",`)})
	if roomToolEntries(a) != 2 {
		t.Fatalf("two parallel calls drew %d rows, want 2", roomToolEntries(a))
	}
	// The SECOND one is announced first, which is the case the id exists for.
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolAnnounced, CallID: "c2", Tool: "write",
		Hint: "write b.go", Args: `{"path":"b.go","content":"b"}`,
	}})
	if roomToolEntries(a) != 2 {
		t.Fatalf("the announcement drew a third row: %d", roomToolEntries(a))
	}
	var announced, still *entry
	for i := range a.room.entries {
		switch a.room.entries[i].callID {
		case "c1":
			still = &a.room.entries[i]
		case "c2":
			announced = &a.room.entries[i]
		}
	}
	if announced == nil || announced.status != toolQueued {
		t.Fatalf("c2's row did not take its own announcement: %+v", announced)
	}
	if still == nil || still.status != toolForming {
		t.Fatalf("c1's row was taken by c2's announcement: %+v", still)
	}
}

// A CALL THE NODE NEVER FINISHED ASKING FOR RESOLVES when its lane ends. It is
// the one row with no event coming for it, and a room left pulsing at a node
// that has landed is the defect [app.dropForming] closed out in the conversation.
func TestARoomResolvesAFormingCallWhenTheLaneEnds(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	drive(t, a, roomEventMsg{gen: a.room.gen, ev: forming("c1", "write", "write internal/config/load.go", `{"path":"internal/config/load.go",`)})
	drive(t, a, roomClosedMsg{gen: a.room.gen})

	page := roomText(a)
	if !strings.Contains(page, cancelledWord) {
		t.Fatalf("the abandoned call does not say what happened:\n%s", page)
	}
	if strings.Contains(page, receivingWord) {
		t.Fatalf("the room is still claiming a call is arriving:\n%s", page)
	}
}
