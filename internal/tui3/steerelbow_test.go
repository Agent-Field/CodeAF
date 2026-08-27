package tui3

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The elbows' acceptance tests: what a corrected question LOOKS like, and the
// three ways a correction can end.
//
// Each asserts the fact the design exists for. A steer is part of the question,
// so the trunk keeps it through every fold a past turn goes through and through
// a reload. A steer that has not reached the model says so, and one that never
// reached it at all is not drawn hanging off a question it was never part of.
// And a conversation nobody steered draws exactly as it always did.

// ── the scripted events ─────────────────────────────────────────────────────

func steerAcceptedEvent(id uint64, words string) session.Event {
	return session.Event{
		Kind:  session.EventSteerAccepted,
		Steer: &session.SteerNote{ID: id, Words: words, Landing: "stopped the reply here", At: time.Now()},
	}
}

func steerConsumedEvent(id uint64) session.Event {
	return session.Event{Kind: session.EventSteerConsumed, Steer: &session.SteerNote{ID: id}}
}

func steerFellEvent(id uint64, words string) session.Event {
	return session.Event{
		Kind:  session.EventSteerFellThrough,
		Steer: &session.SteerNote{ID: id, Words: words, At: time.Now()},
	}
}

// steered starts a turn and pushes the given corrections into it, accepted and
// not yet consumed — the state a person is looking at while the work runs.
func steered(t *testing.T, question string, words ...string) (*fakeAgent, *app) {
	t.Helper()
	agent := &fakeAgent{}
	a := newTestApp(agent)
	typeLine(t, a, question)
	for i, line := range words {
		drive(t, a, streamEventMsg{gen: a.gen, ev: steerAcceptedEvent(uint64(i+1), line)})
	}
	return agent, a
}

// elbowRowsOf is every row of the transcript that is an elbow — the fold line
// included, because it is one of them.
func elbowRowsOf(a *app) []string {
	var out []string
	for _, line := range plainRows(a) {
		if strings.HasPrefix(line, glyphSteer) {
			out = append(out, line)
		}
	}
	return out
}

// unwrapped is a block of drawn rows read back as one sentence: this surface
// wraps its own notes onto a continuation lead rather than cutting them, so a
// test about WHAT WAS SAID must not be a test about where the frame broke it.
func unwrapped(lines []string) string {
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

// clipboardOf is what an OSC 52 write actually put on the clipboard.
func clipboardOf(t *testing.T, raw string) string {
	t.Helper()
	body := strings.TrimSuffix(strings.TrimPrefix(raw, "\x1b]52;c;"), "\a")
	out, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("the clipboard write is not an OSC 52 payload: %q", raw)
	}
	return string(out)
}

// ── 1. the family ───────────────────────────────────────────────────────────

// THE QUESTION IS THE TRUNK AND THE CORRECTIONS HANG OFF IT, in the order they
// were sent, directly under it and above everything the turn then did.
func TestASteeredTurnDrawsEachCorrectionAsThePersonsOwnLine(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket", "and skip the cache")

	lines := plainRows(a)
	trunk, first, second := -1, -1, -1
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "› port the parser"):
			trunk = i
		case strings.HasPrefix(line, "› use the staging bucket"):
			first = i
		case strings.HasPrefix(line, "› and skip the cache"):
			second = i
		}
	}
	if trunk < 0 || first <= trunk || second <= first {
		t.Fatalf("the person's lines are not in order (question %d, steers %d and %d):\n%s",
			trunk, first, second, strings.Join(lines, "\n"))
	}
	if got := strings.Count(strings.Join(lines, "\n"), "› "); got != 3 {
		t.Fatalf("a correction was not drawn in the person's register (%d `›` rows):\n%s",
			got, strings.Join(lines, "\n"))
	}
}

// THE GLYPH AND LANDING CLAUSE ARE FURNITURE. The correction itself has already
// been drawn in the person's ordinary register above it.
func TestASettledLandingClauseIsDimFurniture(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})
	// Past the whole ramp, so what is left is the tier the row RESTS at.
	a.clock = func() time.Time { return time.Now().Add(hudWarm + time.Second) }
	a.entries[a.trunkOf(a.turn)].stale = true
	a.touch()

	var row string
	for _, line := range rows(a) {
		if strings.HasPrefix(plain(line.text), glyphSteer+"stopped the reply") {
			row = line.text
		}
	}
	if row == "" {
		t.Fatalf("no elbow row:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if !strings.HasPrefix(row, sgrOf(a.pal.dim)) {
		t.Fatalf("the elbow glyph is not dim furniture: %q", row)
	}
	if !strings.Contains(row, sgrOf(a.pal.dim)+"stopped the reply here") {
		t.Fatalf("the landing clause is not in the surface's dim register: %q", row)
	}
	// AND THE QUESTION ITSELF IS UNMOVED. The elbow is one step BELOW it, so the
	// step only means anything while the trunk stays where it was.
	for _, line := range rows(a) {
		if strings.HasPrefix(plain(line.text), "› port the parser") &&
			!strings.Contains(line.text, sgrOf(a.pal.muted)) {
			t.Fatalf("the question left the muted tier: %q", line.text)
		}
	}
}

// A WIDE CORRECTION WRAPS AS THE PERSON'S OWN MESSAGE, while the short landing
// clause remains below it.
func TestAWideSteerWrapsAsThePersonsOwnMessage(t *testing.T) {
	_, a := steered(t, "port it",
		"use the staging bucket and not production, and leave the cache alone while you are in there")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})

	lines, at := plainRows(a), -1
	for i, line := range lines {
		if strings.HasPrefix(line, "› use the staging") {
			at = i
		}
	}
	if at < 0 || at+1 >= len(lines) {
		t.Fatalf("the wide correction did not wrap:\n%s", strings.Join(lines, "\n"))
	}
	cont := lines[at+1]
	if strings.HasPrefix(strings.TrimSpace(cont), "└") {
		t.Fatalf("the correction jumped straight to its landing clause instead of wrapping: %q", cont)
	}
	if strings.TrimSpace(cont) == "" {
		t.Fatalf("the wrap produced an empty row:\n%s", strings.Join(lines, "\n"))
	}
}

// ── 2. the landing moment ───────────────────────────────────────────────────

// ACCEPTANCE ALREADY KNOWS THE USEFUL LANDING FACT. The row says that fact at
// once instead of putting a generic steering spinner beside it.
func TestALandingClauseAppearsAtOnceAndDoesNotSpin(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")

	before := strings.Join(plainRows(a), "\n")
	if !strings.Contains(before, glyphSteer+"stopped the reply here") {
		t.Fatalf("the accepted steer has no landing clause:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
	if strings.Contains(before, steerPendingWord) {
		t.Fatalf("the accepted steer still wears a generic spinner:\n%s", before)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})
	if strings.Contains(strings.Join(plainRows(a), "\n"), steerPendingWord) {
		t.Fatalf("a consumed correction grew a spinner:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
}

// THE LANDING CLAUSE STAYS DIM. It is a durable surface fact rather than a
// transient piece of the person's prose.
func TestALandingClauseStaysDimAfterConsumption(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	landed := time.Now()
	a.clock = func() time.Time { return landed }
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})

	for _, age := range []time.Duration{time.Second, hudFresh + time.Second, hudWarm + time.Second} {
		a.clock = func() time.Time { return landed.Add(age) }
		a.touch()
		var row string
		for _, line := range rows(a) {
			if strings.HasPrefix(plain(line.text), glyphSteer+"stopped the reply") {
				row = line.text
			}
		}
		if !strings.Contains(row, sgrOf(a.pal.dim)+"stopped the reply here") {
			t.Fatalf("a landing at age %s left the dim register: %q", age, row)
		}
	}
}

// A STILL LANDING CLAUSE OWES NO WAKEUPS after the model consumes it.
func TestALandingClauseSchedulesNoFadeWakeups(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	cmd := a.steerConsumed(&session.SteerNote{ID: 1})
	if cmd != nil {
		t.Fatal("a still landing clause scheduled a redraw")
	}
}

// ── 3. collapse keeps the elbows ────────────────────────────────────────────

// THE POINT OF THE DESIGN. A finished turn collapses its machinery into one
// chip, and everything the person ASKED is still on the screen.
func TestACollapsedTurnKeepsTheQuestionsElbows(t *testing.T) {
	agent := &fakeAgent{turns: [][]session.Event{{
		toolBegin("read", "internal/parse/lex.go"),
		toolEnd("read", "package parse"),
		text(session.EventTextDelta, "done — the lexer is swapped"),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "port the parser")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerAcceptedEvent(1, "use the staging bucket")})
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	lines := strings.Join(plainRows(a), "\n")
	if !strings.Contains(lines, "worked") {
		t.Fatalf("the turn did not collapse, so this test proves nothing:\n%s", lines)
	}
	if !strings.Contains(lines, "› use the staging bucket") ||
		!strings.Contains(lines, glyphSteer+"stopped the reply here") {
		t.Fatalf("the collapse ate the question's corrections:\n%s", lines)
	}
}

// SEVERAL STEERS REMAIN SEVERAL USER LINES. They are model-visible messages,
// not annotations grouped beneath the opening question.
func TestSeveralSteersRemainSeveralUserLines(t *testing.T) {
	_, a := steered(t, "port the parser", "one", "two", "three", "four", "five")

	lines := strings.Join(plainRows(a), "\n")
	for _, word := range []string{"one", "two", "three", "four", "five"} {
		if !strings.Contains(lines, "› "+word) {
			t.Fatalf("the steer %q is not its own user line:\n%s", word, lines)
		}
	}
}

// THERE IS NO STEER FOLD DOOR ON THE NEW SHAPE. Each user line remains part of
// the readable transcript.
func TestSeveralSteersCreateNoElbowFoldDoor(t *testing.T) {
	_, a := steered(t, "port the parser", "one", "two", "three", "four", "five")

	for _, row := range a.visible(a.width) {
		if row.hit == hitSteerFold {
			t.Fatalf("a steer user line became a fold door: %q", plain(row.text))
		}
	}
}

// ── 4. the fall-through ─────────────────────────────────────────────────────

// WORDS THAT NEVER REACHED THE MODEL ARE NOT DRAWN HANGING OFF THE QUESTION.
// They become the next question, which is what the engine already made them.
func TestAFellThroughSteerLeavesTheQuestionAndBecomesTheNextOne(t *testing.T) {
	agent, a := steered(t, "port the parser", "use the staging bucket")
	if !strings.Contains(strings.Join(plainRows(a), "\n"), "› use the staging bucket") {
		t.Fatal("the correction never became the person's line, so this test proves nothing")
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: steerFellEvent(1, "use the staging bucket")})
	if got := strings.Join(plainRows(a), "\n"); strings.Contains(got, "› use the staging bucket") {
		t.Fatalf("a correction the model never read is still on the old turn:\n%s", got)
	}
	// The note WRAPS onto the dim lane's continuation lead rather than being cut
	// (render.go's entryNote), so the sentence is read back as one run.
	if !strings.Contains(unwrapped(plainRows(a)), steerFellWord) {
		t.Fatalf("the surface said nothing about words that missed their turn:\n%s",
			strings.Join(plainRows(a), "\n"))
	}

	// AND THE WORDS COME BACK AS THE NEXT QUESTION, drawn by the drain that draws
	// every other waiting message when its turn starts. The lane hands the stream
	// over; the surface never invents one.
	lane := make(chan session.Event)
	a.follows = append(a.follows, queued{text: "use the staging bucket", ch: lane})
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if !strings.Contains(strings.Join(plainRows(a), "\n"), "› use the staging bucket") {
		t.Fatalf("the fallen-through words never became a question:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
	close(lane)
}

// THE LANE IS WHAT CARRIES THE SEAM. A steer's own channel is an ordinary
// subscriber of the turn it was typed into, and the lane reads past all of that
// to answer one question: did these words land, or do they start a turn of
// their own?
func TestTheSteerLaneHandsOverTheStreamOnlyWhenTheWordsFellThrough(t *testing.T) {
	fell := make(chan session.Event, 4)
	fell <- steerAcceptedEvent(7, "use the staging bucket")
	fell <- text(session.EventTextDelta, "reading the lexer")
	fell <- steerFellEvent(7, "use the staging bucket")
	msgs := runCmd(waitSteerLane(fell, 3))
	if len(msgs) != 1 {
		t.Fatalf("the lane answered %d messages, want the seam", len(msgs))
	}
	seam, ok := msgs[0].(steerFellMsg)
	if !ok || seam.words != "use the staging bucket" || seam.gen != 3 {
		t.Fatalf("the lane answered %#v", msgs[0])
	}

	landed := make(chan session.Event, 4)
	landed <- steerAcceptedEvent(7, "use the staging bucket")
	landed <- steerConsumedEvent(7)
	close(landed)
	if got := runCmd(waitSteerLane(landed, 3)); len(got) != 0 {
		t.Fatalf("a landed steer's lane spoke: %#v", got)
	}
}

// A TURN SOMEBODY STOPPED DRAINS NOTHING. The engine drops a follow-up queued
// behind an interrupted turn, so a surface that queued one would draw a
// question with no answer coming.
func TestAFellThroughSteerIsNotQueuedOntoATurnSomebodyStopped(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	a.state = stateInterrupted
	lane := make(chan session.Event)
	close(lane)
	a.steerFell(steerFellMsg{gen: a.gen, words: "use the staging bucket", ch: lane})
	if len(a.follows) != 0 {
		t.Fatal("a stopped turn drained a correction into a turn of its own")
	}
}

// AND A CORRECTION THAT NEVER LANDED LEAVES THE QUESTION WHEN THE TURN ENDS.
// After a stop nothing new is drawn, so the fall-through's own event never
// reaches the screen — and an elbow left spinning would claim forever that the
// model had been given words it never saw.
func TestAnElbowThatNeverLandedLeavesTheQuestionAtTheTurnsEnd(t *testing.T) {
	agent, a := steered(t, "port the parser", "landed", "never landed")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	lines := strings.Join(plainRows(a), "\n")
	if !strings.Contains(lines, "› landed") {
		t.Fatalf("the landed correction was swept with the other one:\n%s", lines)
	}
	if strings.Contains(lines, "› never landed") {
		t.Fatalf("a correction the turn never carried is still hanging off it:\n%s", lines)
	}
}

// ── 5. history ──────────────────────────────────────────────────────────────

// A TRANSCRIPT WITH NO STEERS IN IT RENDERS BYTE FOR BYTE AS IT ALWAYS DID.
// This is the pin: every conversation anybody has ever had is one of these.
func TestASteerlessTranscriptIsUnchanged(t *testing.T) {
	past := []session.DisplayEntry{
		{Role: "user", Text: "port the parser"},
		{Role: "assistant", Text: "done — here is what changed"},
		{Role: "user", Text: "and now write the tests"},
		{Role: "assistant", Text: "written"},
	}
	a := newTestApp(&fakeAgent{past: past})
	a.replay()
	drawn := a.visible(a.width)

	// The same conversation through the same renderer with the elbows' whole
	// machinery inert: no block carries one, so no row may differ.
	for i := range a.entries {
		if len(a.entries[i].steers) != 0 || a.entries[i].steerFoldRow != 0 {
			t.Fatalf("a steerless block grew steer state: %#v", a.entries[i].steers)
		}
	}
	for _, r := range drawn {
		if strings.Contains(plain(r.text), glyphSteer) {
			t.Fatalf("an elbow appeared in a conversation nobody steered: %q", r.text)
		}
		if r.hit == hitSteerFold {
			t.Fatalf("a steerless row answers to the elbows' fold: %q", r.text)
		}
	}
}

// AND THE TRUNK AND ITS ELBOWS SURVIVE A RELOAD, rebuilt from the journal's own
// marks. A steer replays as an ordinary user message with a mark on it, and a
// surface that drew the mark-less shape would put questions in the transcript
// nobody asked.
func TestAReloadRebuildsTheTrunkAndItsElbowsFromTheMarks(t *testing.T) {
	sent := time.Now().Add(-time.Hour)
	past := []session.DisplayEntry{
		{Role: "user", Text: "port the parser"},
		{Role: "user", Text: "use the staging bucket", Steer: &session.SteerMark{At: sent, Consumed: true, Landing: "stopped the reply here"}},
		{Role: "assistant", Text: "done"},
		{Role: "user", Text: "and now write the tests"},
	}
	a := newTestApp(&fakeAgent{past: past})
	a.replay()

	if got := len(a.entries); got != 4 {
		t.Fatalf("the reload built %d blocks, want the question, steer, answer and next question:\n%#v",
			got, a.entries)
	}
	steer := a.entries[1]
	if !steer.steerLine || steer.text != "use the staging bucket" ||
		len(steer.steers) != 1 || steer.steers[0].words != "stopped the reply here" {
		t.Fatalf("the correction did not come back as its own marked user line: %#v", steer)
	}
	if !steer.steers[0].consumed {
		t.Fatal("a replayed correction that landed came back as though it had not")
	}
	// THE TURN COUNT IS THE QUESTIONS', not the messages'. A steer that bumped it
	// would split one turn's work across two and fold the wrong rows.
	if a.entries[3].turn != a.entries[0].turn+1 {
		t.Fatalf("the second question is %d turns after the first, want one",
			a.entries[3].turn-a.entries[0].turn)
	}
	lines := strings.Join(plainRows(a), "\n")
	if !strings.Contains(lines, "› port the parser\n\n› use the staging bucket") ||
		!strings.Contains(lines, glyphSteer+"stopped the reply here") {
		t.Fatalf("the reloaded steer is not a user line with its landing clause:\n%s", lines)
	}
	// AND A REPLAYED LANDING IS A FACT AND NOT NEWS: it comes back settled, never
	// wearing the ramp's fresh tier for a correction made an hour ago.
	for _, r := range rows(a) {
		if strings.HasPrefix(plain(r.text), glyphSteer+"stopped the reply") &&
			!strings.Contains(r.text, sgrOf(a.pal.dim)) {
			t.Fatalf("a replayed correction came back lit: %q", r.text)
		}
	}
}

// A WINDOW THAT OPENS PART-WAY THROUGH A STEERED TURN still draws the person's
// correction as the user message it was, even when the opening question is
// above the retained history window.
func TestASteerWhoseOpeningQuestionIsAboveTheWindowStillDraws(t *testing.T) {
	past := []session.DisplayEntry{
		{Role: "user", Text: "use the staging bucket", Steer: &session.SteerMark{At: time.Now(), Consumed: true}},
		{Role: "assistant", Text: "done"},
	}
	a := newTestApp(&fakeAgent{past: past})
	a.replay()

	lines := strings.Join(plainRows(a), "\n")
	if !strings.Contains(lines, "› use the staging bucket") {
		t.Fatalf("the correction was lost with its opening question:\n%s", lines)
	}
}

// ── 6. copy, and doors ──────────────────────────────────────────────────────

// DRAGGING OVER THE USER LINE COPIES THE CORRECTION. A steer has the same copy
// behavior as every other message from the person.
func TestDraggingOverASteerCopiesTheCorrection(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})

	at := -1
	for i, r := range a.allBodyRows() {
		if strings.HasPrefix(plain(r.text), "› use the staging") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no steer user row to sweep:\n%s", strings.Join(plainRows(a), "\n"))
	}
	var yanked string
	for _, msg := range runCmd(a.dragYank(dragSelect{on: true, anchorRow: at, row: at})) {
		if raw, ok := msg.(tea.RawMsg); ok {
			yanked = clipboardOf(t, raw.Msg.(string))
		}
	}
	if !strings.Contains(yanked, "use the staging bucket") {
		t.Fatalf("the sweep did not copy the correction: %q", yanked)
	}
	if a.dragCopied != 1 {
		t.Fatalf("the sweep copied %d lines, want the steer message's one", a.dragCopied)
	}
}

// A PATH INSIDE A CORRECTION IS A DOOR, on the person's own message's terms.
// The commonest steer there is names a file, and pathlink.go's law is that
// every path a person reads on this surface opens.
func TestAPathInsideACorrectionIsADoor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lex.go"), []byte("package parse\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{}
	a := newTestApp(agent)
	a.workspace = dir
	typeLine(t, a, "port the parser")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerAcceptedEvent(1, "the file is lex.go")})
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})

	for _, r := range rows(a) {
		if strings.HasPrefix(plain(r.text), "› the file is") {
			if !strings.Contains(r.text, "\x1b]8;;") {
				t.Fatalf("the path in a correction is not a door: %q", r.text)
			}
			return
		}
	}
	t.Fatalf("no steer user row:\n%s", strings.Join(plainRows(a), "\n"))
}
