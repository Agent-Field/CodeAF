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
		Steer: &session.SteerNote{ID: id, Words: words, At: time.Now()},
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
func TestASteeredTurnDrawsTheTrunkAndThenItsElbowsInOrder(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket", "and skip the cache")

	lines := plainRows(a)
	trunk, first, second := -1, -1, -1
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "› port the parser"):
			trunk = i
		case strings.HasPrefix(line, glyphSteer+"use the staging bucket"):
			first = i
		case strings.HasPrefix(line, glyphSteer+"and skip the cache"):
			second = i
		}
	}
	if trunk < 0 || first != trunk+1 || second != trunk+2 {
		t.Fatalf("the family is not trunk-then-elbows (trunk %d, elbows %d and %d):\n%s",
			trunk, first, second, strings.Join(lines, "\n"))
	}
	// AND THERE IS NO SECOND `›`. A correction drawn as a question of its own is
	// the whole defect this file exists to end.
	if got := strings.Count(strings.Join(lines, "\n"), "› "); got != 1 {
		t.Fatalf("a correction was drawn as a question of its own (%d `›` rows):\n%s",
			got, strings.Join(lines, "\n"))
	}
}

// THE GLYPH IS FURNITURE AND THE WORDS ARE THE PERSON'S, ONE READING STEP UNDER
// THE QUESTION'S OWN. This is the whole of the ink decision and it is asserted
// against the ramp constants rather than against a hex.
func TestASettledElbowWearsDimFurnitureAndNarrWords(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})
	// Past the whole ramp, so what is left is the tier the row RESTS at.
	a.clock = func() time.Time { return time.Now().Add(hudWarm + time.Second) }
	a.entries[a.trunkOf(a.turn)].stale = true
	a.touch()

	var row string
	for _, line := range rows(a) {
		if strings.HasPrefix(plain(line.text), glyphSteer+"use the staging") {
			row = line.text
		}
	}
	if row == "" {
		t.Fatalf("no elbow row:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if !strings.HasPrefix(row, sgrOf(a.pal.dim)) {
		t.Fatalf("the elbow glyph is not dim furniture: %q", row)
	}
	if !strings.Contains(row, sgrOf(a.pal.narr)+"use the staging bucket") {
		t.Fatalf("the correction's words are not one step under the question's: %q", row)
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

// A WIDE CORRECTION WRAPS ONTO A HANGING INDENT UNDER ITS OWN FIRST CHARACTER,
// which is the person's own block's rule and [bandClauses]'.
func TestAWideElbowWrapsUnderItsOwnFirstCharacter(t *testing.T) {
	_, a := steered(t, "port it",
		"use the staging bucket and not production, and leave the cache alone while you are in there")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})

	lines, at := plainRows(a), -1
	for i, line := range lines {
		if strings.HasPrefix(line, glyphSteer+"use the staging") {
			at = i
		}
	}
	if at < 0 || at+1 >= len(lines) {
		t.Fatalf("the wide correction did not wrap:\n%s", strings.Join(lines, "\n"))
	}
	cont := lines[at+1]
	lead := strings.Repeat(" ", len(glyphSteer)-len("└")+1)
	if !strings.HasPrefix(cont, lead) || strings.HasPrefix(strings.TrimSpace(cont), "└") {
		t.Fatalf("the continuation is not hung under the first character: %q", cont)
	}
	if strings.TrimSpace(cont) == "" {
		t.Fatalf("the wrap produced an empty row:\n%s", strings.Join(lines, "\n"))
	}
}

// ── 2. the landing moment ───────────────────────────────────────────────────

// NOTHING CLAIMS CONSUMED BEFORE THE ENGINE SAYS SO. Until then the row wears
// the working idiom every live row on this surface wears.
func TestAnElbowSaysItIsSteeringUntilTheModelIsGivenIt(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")

	if !strings.Contains(strings.Join(plainRows(a), "\n"), steerPendingWord) {
		t.Fatalf("a correction the model has not been given says nothing about it:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})
	if strings.Contains(strings.Join(plainRows(a), "\n"), steerPendingWord) {
		t.Fatalf("a landed correction still says it is on its way:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
}

// AND THE LANDING IS NEWS AND THEN IT IS NOT: the status line's own ramp, ink
// while it is fresh, muted while it is recent, and the settled tier after.
func TestALandedElbowLightsAndComesBackDownTheRamp(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	landed := time.Now()
	a.clock = func() time.Time { return landed }
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})

	for _, step := range []struct {
		age  time.Duration
		want func(string) string
		word string
	}{
		{time.Second, a.pal.ink, "fresh"},
		{hudFresh + time.Second, a.pal.muted, "recent"},
		{hudWarm + time.Second, a.pal.narr, "settled"},
	} {
		a.clock = func() time.Time { return landed.Add(step.age) }
		a.touch()
		var row string
		for _, line := range rows(a) {
			if strings.HasPrefix(plain(line.text), glyphSteer+"use the staging") {
				row = line.text
			}
		}
		if !strings.Contains(row, sgrOf(step.want)+"use the staging bucket") {
			t.Fatalf("a %s landing is not on its own rung of the ramp: %q", step.word, row)
		}
	}
}

// THE FADE IS SCHEDULED AND NOT TICKED. A landing asks for the two one-shot
// wakeups the status line's own numbers ask for, which is this surface's whole
// idle budget.
func TestALandingAsksForTheFadesTwoWakeupsAndNoTicker(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	cmd := a.steerConsumed(&session.SteerNote{ID: 1})
	if cmd == nil {
		t.Fatal("a landing scheduled no wakeup, so the fresh tier would sit on an idle frame")
	}
	// The batch is read rather than RUN: the two wakeups are four and ten seconds
	// out, and a harness that waited for them would be a suite that waited for
	// them.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("a landing scheduled %v, want the fade's two wakeups", batch)
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
	if !strings.Contains(lines, glyphSteer+"use the staging bucket") {
		t.Fatalf("the collapse ate the question's corrections:\n%s", lines)
	}
}

// PAST THREE THEY FOLD, in the repo's one fold grammar — and the three that
// stay are the NEWEST three, exactly as a tool cluster keeps the call that is
// running and the two it followed.
func TestTheElbowsFoldPastThreeAndKeepTheNewest(t *testing.T) {
	_, a := steered(t, "port the parser", "one", "two", "three", "four", "five")

	elbows := elbowRowsOf(a)
	if len(elbows) != 1+steerWindow {
		t.Fatalf("the elbows did not fold to %d rows and a fold line:\n%s",
			steerWindow, strings.Join(elbows, "\n"))
	}
	if want := glyphSteer + bandFoldWord(2, steerFoldWhat, true); elbows[0] != strings.TrimRight(fit(want, a.width), " ") {
		t.Fatalf("the fold line is %q, want %q", elbows[0], want)
	}
	for i, word := range []string{"three", "four", "five"} {
		if !strings.HasPrefix(elbows[i+1], glyphSteer+word) {
			t.Fatalf("the fold kept the wrong three:\n%s", strings.Join(elbows, "\n"))
		}
	}
	if strings.Contains(strings.Join(elbows, "\n"), glyphSteer+"one") {
		t.Fatalf("a folded correction is still drawn:\n%s", strings.Join(elbows, "\n"))
	}
}

// AND THE FOLD LINE IS A DOOR, which opens and shuts on the same press.
func TestTheElbowFoldLineOpensAndShutsOnAClick(t *testing.T) {
	_, a := steered(t, "port the parser", "one", "two", "three", "four", "five")

	clickHit(t, a, hitSteerFold)
	if !strings.Contains(strings.Join(elbowRowsOf(a), "\n"), glyphSteer+"one") {
		t.Fatalf("the fold did not open:\n%s", strings.Join(elbowRowsOf(a), "\n"))
	}
	clickHit(t, a, hitSteerFold)
	if strings.Contains(strings.Join(elbowRowsOf(a), "\n"), glyphSteer+"one") {
		t.Fatalf("the fold did not shut again:\n%s", strings.Join(elbowRowsOf(a), "\n"))
	}
}

// ── 4. the fall-through ─────────────────────────────────────────────────────

// WORDS THAT NEVER REACHED THE MODEL ARE NOT DRAWN HANGING OFF THE QUESTION.
// They become the next question, which is what the engine already made them.
func TestAFellThroughSteerLeavesTheQuestionAndBecomesTheNextOne(t *testing.T) {
	agent, a := steered(t, "port the parser", "use the staging bucket")
	if !strings.Contains(strings.Join(elbowRowsOf(a), "\n"), "use the staging bucket") {
		t.Fatal("the correction never became an elbow, so this test proves nothing")
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: steerFellEvent(1, "use the staging bucket")})
	if got := elbowRowsOf(a); len(got) != 0 {
		t.Fatalf("a correction the model never read is still an elbow:\n%s", strings.Join(got, "\n"))
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

	elbows := strings.Join(elbowRowsOf(a), "\n")
	if !strings.Contains(elbows, glyphSteer+"landed") {
		t.Fatalf("the landed correction was swept with the other one:\n%s", elbows)
	}
	if strings.Contains(elbows, "never landed") {
		t.Fatalf("a correction the turn never carried is still hanging off it:\n%s", elbows)
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
		{Role: "user", Text: "use the staging bucket", Steer: &session.SteerMark{At: sent, Consumed: true}},
		{Role: "assistant", Text: "done"},
		{Role: "user", Text: "and now write the tests"},
	}
	a := newTestApp(&fakeAgent{past: past})
	a.replay()

	if got := len(a.entries); got != 3 {
		t.Fatalf("the reload built %d blocks, want the two questions and the answer:\n%#v",
			got, a.entries)
	}
	trunk := a.entries[0]
	if len(trunk.steers) != 1 || trunk.steers[0].words != "use the staging bucket" {
		t.Fatalf("the correction did not come back onto its trunk: %#v", trunk.steers)
	}
	if !trunk.steers[0].consumed {
		t.Fatal("a replayed correction that landed came back as though it had not")
	}
	// THE TURN COUNT IS THE QUESTIONS', not the messages'. A steer that bumped it
	// would split one turn's work across two and fold the wrong rows.
	if a.entries[2].turn != a.entries[0].turn+1 {
		t.Fatalf("the second question is %d turns after the first, want one",
			a.entries[2].turn-a.entries[0].turn)
	}
	lines := strings.Join(plainRows(a), "\n")
	if !strings.Contains(lines, "› port the parser\n"+glyphSteer+"use the staging bucket") {
		t.Fatalf("the reloaded family is not trunk-then-elbow:\n%s", lines)
	}
	// AND A REPLAYED LANDING IS A FACT AND NOT NEWS: it comes back settled, never
	// wearing the ramp's fresh tier for a correction made an hour ago.
	for _, r := range rows(a) {
		if strings.HasPrefix(plain(r.text), glyphSteer+"use the staging") &&
			!strings.Contains(r.text, sgrOf(a.pal.narr)) {
			t.Fatalf("a replayed correction came back lit: %q", r.text)
		}
	}
}

// A WINDOW THAT OPENS PART-WAY THROUGH A STEERED TURN still draws the person's
// corrections — hanging from a trunk above the top of the screen, which is what
// happened, rather than promoted into questions of their own.
func TestElbowsWhoseTrunkIsAboveTheWindowAreStillDrawnAsElbows(t *testing.T) {
	past := []session.DisplayEntry{
		{Role: "user", Text: "use the staging bucket", Steer: &session.SteerMark{At: time.Now(), Consumed: true}},
		{Role: "assistant", Text: "done"},
	}
	a := newTestApp(&fakeAgent{past: past})
	a.replay()

	lines := strings.Join(plainRows(a), "\n")
	if strings.Contains(lines, "› use the staging bucket") {
		t.Fatalf("a correction was promoted into a question:\n%s", lines)
	}
	if !strings.Contains(lines, glyphSteer+"use the staging bucket") {
		t.Fatalf("the correction was lost with its trunk:\n%s", lines)
	}
}

// ── 6. copy, and doors ──────────────────────────────────────────────────────

// DRAGGING OVER AN ELBOW COPIES THE CORRECTION. The sweep rides the drawn rows,
// so this is a test that the elbow IS one — text on the transcript and not a
// decoration painted beside it.
func TestDraggingOverAnElbowCopiesTheCorrection(t *testing.T) {
	_, a := steered(t, "port the parser", "use the staging bucket")
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerConsumedEvent(1)})

	at := -1
	for i, r := range a.allBodyRows() {
		if strings.HasPrefix(plain(r.text), glyphSteer+"use the staging") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no elbow row to sweep:\n%s", strings.Join(plainRows(a), "\n"))
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
		t.Fatalf("the sweep copied %d lines, want the elbow's one", a.dragCopied)
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
		if strings.HasPrefix(plain(r.text), glyphSteer+"the file is") {
			if !strings.Contains(r.text, "\x1b]8;;") {
				t.Fatalf("the path in a correction is not a door: %q", r.text)
			}
			return
		}
	}
	t.Fatalf("no elbow row:\n%s", strings.Join(plainRows(a), "\n"))
}
