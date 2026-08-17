package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE REWIND SURFACE: the double esc that opens it, the line it draws through
// the conversation, the keys and the pointer that move that line, and what a
// commit does to the blocks on screen.

// rewindFake is [fakeAgent] with the engine's rewind contract on it, and it is a
// type of its own for the reason the surface asserts rather than widens (see
// [rewindAgent]): every other test in this package drives an agent that has never
// heard of a rewind, and that has to keep working.
//
// The points are derived from the transcript the same way the engine's stub
// derives them — a user message is a turn, an assistant message is a step — so a
// test that walks them is walking a real list rather than a hand-written one.
type rewindFake struct {
	*fakeAgent
	// err is what the engine refuses with, or nil.
	err error
	// cuts is every index [rewindFake.RewindAt] was asked to cut at.
	cuts []int
}

func (r *rewindFake) RewindPoints() []session.RewindPoint {
	var out []session.RewindPoint
	for i, e := range r.past {
		switch e.Role {
		case "user":
			out = append(out, session.RewindPoint{Index: i, Turn: true, Said: e.Text, Entry: i})
		case "assistant":
			out = append(out, session.RewindPoint{Index: i, Entry: i})
		}
	}
	return out
}

func (r *rewindFake) RewindAt(index int) ([]session.DisplayEntry, error) {
	if r.err != nil {
		return nil, r.err
	}
	if index < 0 || index > len(r.past) {
		return nil, session.ErrNothingToRewind
	}
	r.cuts = append(r.cuts, index)
	dropped := append([]session.DisplayEntry(nil), r.past[index:]...)
	r.past = append([]session.DisplayEntry(nil), r.past[:index]...)
	return dropped, nil
}

// rewindPast is the conversation these tests rewind: three turns, each answered.
func rewindPast() []session.DisplayEntry {
	return []session.DisplayEntry{
		{Role: "user", Text: "one"},
		{Role: "assistant", Text: "first answer"},
		{Role: "user", Text: "two"},
		{Role: "assistant", Text: "second answer"},
		{Role: "user", Text: "three"},
		{Role: "assistant", Text: "third answer"},
	}
}

// newRewindApp opens the surface on a resumed conversation, at a hundred columns
// — the width the mode bar's two halves were laid out against.
func newRewindApp(t *testing.T, past []session.DisplayEntry) (*app, *rewindFake) {
	t.Helper()
	agent := &rewindFake{fakeAgent: &fakeAgent{model: "m", past: past}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab"})
	a.width, a.height = 100, 24
	a.pal = newPalette(tokens.ANSI256, false)
	a.welcome = welcome{spent: true}
	a.touch()
	return a, agent
}

// saidAt is the message a point names, for the assertions below.
func saidAt(a *app) string {
	if a.rew.at < 0 || a.rew.at >= len(a.rew.points) {
		return ""
	}
	return a.rew.points[a.rew.at].Said
}

// rewindRowY is the screen row a drawn line landed on, resolved exactly as
// [app.rowAt] resolves it, so a click built from it is the click a person makes.
func rewindRowY(a *app, want string) (int, bool) {
	body, pad := a.window(a.bodyWidth(), a.viewHeight())
	for i, r := range body {
		if strings.Contains(plain(r.text), want) {
			return a.bodyTop() + pad + i, true
		}
	}
	return 0, false
}

// ── 1. the door ─────────────────────────────────────────────────────────────

// The first esc arms and says so; the second one inside the window opens the
// mode over the conversation.
func TestEscTwiceInsideTheWindowEntersRewind(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())

	drive(t, a, key("esc"))
	if !a.rewindArmed() {
		t.Fatal("the first esc did not arm the rewind")
	}
	if a.rew.on {
		t.Fatal("one esc entered the mode")
	}
	if got := a.hintWord(); got != rewindArmWord {
		t.Fatalf("hint slot = %q, want %q", got, rewindArmWord)
	}
	if got := plain(frame(a)); !strings.Contains(got, rewindArmWord) {
		t.Fatalf("the armed frame does not say so:\n%s", got)
	}

	drive(t, a, key("esc"))
	if !a.rew.on {
		t.Fatal("the second esc did not enter the mode")
	}
}

// A window that lapses takes the meaning with it: the next esc is an ordinary
// esc, and the mode stays shut.
func TestTheArmLapsesAndAStrayEscChangesNothing(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	now := time.Now()
	a.clock = func() time.Time { return now }

	drive(t, a, key("esc"))
	if !a.rewindArmed() {
		t.Fatal("the first esc did not arm the rewind")
	}

	now = now.Add(rewindArmWindow + time.Millisecond)
	// The frame clock is what runs the window down — no goroutine of its own.
	drive(t, a, frameMsg{})
	if a.rewindArmed() || !a.escArm.IsZero() {
		t.Fatal("the arm survived its window")
	}
	if got := a.hintWord(); got == rewindArmWord {
		t.Fatal("the hint slot still offers a rewind after the window lapsed")
	}

	drive(t, a, key("esc"))
	if a.rew.on {
		t.Fatal("a stray esc after the window entered the mode")
	}
}

// Mid-turn the first esc keeps its own meaning — it stops the model — and the
// second one inside the window still opens the mode. Interrupt first, then
// rewind, which is the order the engine's refusal asks for.
func TestEscMidTurnInterruptsFirstAndThenRewinds(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	drive(t, a, submittedMsg{ch: make(chan session.Event)})
	a.state = stateWorking

	drive(t, a, key("esc"))
	if agent.stops != 1 {
		t.Fatalf("the first esc mid-turn did not interrupt (%d)", agent.stops)
	}
	if !a.rewindArmed() {
		t.Fatal("the first esc mid-turn did not arm the rewind")
	}
	drive(t, a, key("esc"))
	if !a.rew.on {
		t.Fatal("the second esc mid-turn did not enter the mode")
	}
}

// A backend that cannot rewind simply has no rewind: esc means what it always
// meant, twice.
func TestASurfaceWithoutARewinderNeverArms(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	drive(t, a, key("esc"), key("esc"))
	if a.rewindArmed() || a.rew.on {
		t.Fatal("a surface with no rewind door opened one")
	}
}

// ── 2. the mode ─────────────────────────────────────────────────────────────

// The door a /rewind command calls opens the same mode the keys do: a cut line
// through the transcript and a bar where the box was.
func TestEnterRewindDrawsTheCutLineAndTheModeBar(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	runCmd(a.enterRewind())
	if !a.rew.on {
		t.Fatal("enterRewind did not enter the mode")
	}
	// It opens on the LAST thing the person said.
	if got := saidAt(a); got != "three" {
		t.Fatalf("the cut opened at %q, want the last message", got)
	}

	got := plain(frame(a))
	for _, want := range []string{
		"⟲ rewind here",  // the cut line's label
		"⟲ drops 1 turn", // the bar's left half
		"↑↓ turns · ←→ steps · enter rewind · esc back", // and its right
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the rewind frame is missing %q:\n%s", want, got)
		}
	}
	// The draft box is not on the frame: the bar took its position, prompt and
	// all, so there is nothing under the rule that looks like a box to type in.
	block, _, _ := a.inputBlock(a.width - len(inputPad))
	if len(block) != 1 || strings.Contains(plain(block[0]), prompt) {
		t.Fatalf("the draft box is still drawn under the mode: %q", block)
	}
}

// The line sits ABOVE the block it cuts at, everything from there down is washed
// out, and the block itself is the one that is brightened.
func TestTheCutLineSitsAboveTheAnchorAndWashesWhatItDrops(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	runCmd(a.enterRewind())

	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	cut := -1
	for i, r := range body {
		if r.hit == hitRewind {
			cut = i
			break
		}
	}
	if cut < 0 {
		t.Fatalf("no cut line on the frame:\n%s", plain(frame(a)))
	}
	above := plain(strings.Join(textsOf(body[:cut]), "\n"))
	below := plain(strings.Join(textsOf(body[cut:]), "\n"))
	if !strings.Contains(above, "second answer") {
		t.Fatalf("the turn before the cut is not above the line:\n%s", above)
	}
	if !strings.Contains(below, "three") || !strings.Contains(below, "third answer") {
		t.Fatalf("the turn being dropped is not under the line:\n%s", below)
	}
	if strings.Contains(above, "third answer") {
		t.Fatalf("a dropped block was drawn above the line:\n%s", above)
	}

	// THE WASH IS A REPAINT: the person's own message is drawn in the accent
	// everywhere else on this surface, and under the line it is not.
	accent := paintPrefix(a.pal.accent("x"))
	// From under the LINE — the line itself wears the accent on its label, which
	// is the one painted thing this mode adds.
	for _, r := range body[cut+1:] {
		if strings.Contains(r.text, accent) {
			t.Fatalf("a row under the cut kept its paint: %q", r.text)
		}
	}
	kept := false
	for _, r := range body[:cut] {
		if strings.Contains(r.text, accent) {
			kept = true
		}
	}
	if !kept {
		t.Fatal("the wash reached above the cut line")
	}
}

// A frame with no room for both halves of the bar keeps the FACT and drops the
// legend: the keys are recoverable by pressing them, and the count is not
// recoverable by anything.
func TestTheModeBarDropsItsLegendBeforeItsCount(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	a.width, a.height = 44, 12
	a.touch()
	runCmd(a.enterRewind())

	got := plain(frame(a))
	if !strings.Contains(got, "⟲ drops") {
		t.Fatalf("the narrow bar lost the count:\n%s", got)
	}
	if strings.Contains(got, rewindKeysWord) {
		t.Fatalf("the narrow bar kept a legend it has no room for:\n%s", got)
	}
	if !strings.Contains(got, rewindCutWord) {
		t.Fatalf("the narrow frame lost the cut line:\n%s", got)
	}
}

// ── 3. the keys ─────────────────────────────────────────────────────────────

// ↑↓ walk the turns and stop at both ends; ←→ slide through the steps inside the
// turn the cut is in and no further.
func TestTheArrowsWalkTurnsAndSteps(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	runCmd(a.enterRewind())

	drive(t, a, key("up"))
	if got := saidAt(a); got != "two" {
		t.Fatalf("↑ landed on %q, want the turn before", got)
	}
	drive(t, a, key("up"))
	if got := saidAt(a); got != "one" {
		t.Fatalf("a second ↑ landed on %q, want the first turn", got)
	}
	drive(t, a, key("up"))
	if got := saidAt(a); got != "one" {
		t.Fatalf("↑ walked off the oldest turn onto %q", got)
	}
	drive(t, a, key("down"))
	if got := saidAt(a); got != "two" {
		t.Fatalf("↓ landed on %q, want the turn after", got)
	}

	// → steps INSIDE that turn: the reply is a point of its own, and it is not a
	// turn point, so ↑↓ never stop on it.
	at := a.rew.at
	drive(t, a, key("right"))
	if a.rew.at != at+1 || a.rew.points[a.rew.at].Turn {
		t.Fatalf("→ landed on point %d (turn=%v), want the step inside the turn",
			a.rew.at, a.rew.points[a.rew.at].Turn)
	}
	drive(t, a, key("right"))
	if a.rew.at != at+1 {
		t.Fatalf("→ walked past the turn's own steps onto point %d", a.rew.at)
	}
	drive(t, a, key("left"))
	if a.rew.at != at {
		t.Fatalf("← landed on point %d, want the turn point it started from", a.rew.at)
	}
	drive(t, a, key("left"))
	if a.rew.at != at {
		t.Fatalf("← walked out of the turn onto point %d", a.rew.at)
	}
}

// While the mode is DOWN the draft keeps every key it has always had.
func TestTheDraftKeysAreUntouchedWhileTheModeIsDown(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	a.input.setText("a sentence")
	a.input.cursor = 4

	drive(t, a, key("left"))
	if a.input.cursor != 3 {
		t.Fatalf("← moved the caret to %d, want 3", a.input.cursor)
	}
	drive(t, a, key("right"), key("right"))
	if a.input.cursor != 5 {
		t.Fatalf("→ moved the caret to %d, want 5", a.input.cursor)
	}
	drive(t, a, key("x"))
	if got := a.input.String(); got != "a senxtence" {
		t.Fatalf("the draft is %q — a key did not reach the box", got)
	}
	if a.rew.on {
		t.Fatal("the mode opened with nobody asking for it")
	}
}

// ── 4. the pointer ──────────────────────────────────────────────────────────

// A click on a row moves the cut to the nearest point at or above it, hover
// brightens the block a click would choose, and the cut line itself commits.
func TestAClickChoosesTheCutAndTheCutLineCommits(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	runCmd(a.enterRewind())

	y, ok := rewindRowY(a, "first answer")
	if !ok {
		t.Fatalf("the first turn's reply is not on the frame:\n%s", plain(frame(a)))
	}
	a.setHover(0, y)
	if a.hot.kind != hoverRewind {
		t.Fatalf("hover over the transcript is %v, want a cut", a.hot.kind)
	}
	a.press(0, y)
	if got := a.rew.points[a.rew.at].Entry; got != 1 {
		t.Fatalf("the click chose the point at entry %d, want the reply it landed on", got)
	}

	cutY, ok := rewindRowY(a, rewindCutWord)
	if !ok {
		t.Fatalf("the cut line is not on the frame:\n%s", plain(frame(a)))
	}
	a.press(0, cutY)
	if a.rew.on {
		t.Fatal("a click on the cut line did not commit")
	}
	if len(agent.cuts) != 1 || agent.cuts[0] != 1 {
		t.Fatalf("the engine was cut at %v, want the chosen point", agent.cuts)
	}
}

// ── 5. the commit ───────────────────────────────────────────────────────────

// A commit cuts the session, rebuilds the blocks from what it now holds, says so
// once in the transcript, and puts the message back in the box.
func TestCommitCutsRebuildsAndPutsTheMessageBack(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	runCmd(a.enterRewind())
	drive(t, a, key("up")) // the cut moves to "two"

	if got := plain(frame(a)); !strings.Contains(got, "⟲ drops 2 turns") {
		t.Fatalf("the bar does not count what the cut takes:\n%s", got)
	}

	drive(t, a, key("enter"))
	if a.rew.on {
		t.Fatal("the commit left the mode up")
	}
	if len(agent.cuts) != 1 || agent.cuts[0] != 2 {
		t.Fatalf("the engine was cut at %v, want the chosen point", agent.cuts)
	}
	if len(agent.past) != 2 {
		t.Fatalf("the transcript is %d entries after the cut, want the first turn alone", len(agent.past))
	}

	// The CONVERSATION is what the cut edited — the box below it is holding the
	// message the cut took back, which is why the two are read apart here.
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	got := plain(strings.Join(textsOf(body), "\n"))
	for _, gone := range []string{"two", "second answer", "three", "third answer"} {
		if strings.Contains(got, gone) {
			t.Fatalf("the rewound turn is still drawn (%q):\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "first answer") {
		t.Fatalf("the rebuild dropped the turn that was kept:\n%s", got)
	}
	if !strings.Contains(got, "⟲ rewound · 2 turns") {
		t.Fatalf("the transcript does not say what happened:\n%s", got)
	}
	if a.input.String() != "two" {
		t.Fatalf("the draft is %q, want the message the cut took back", a.input.String())
	}
	if !a.stick {
		t.Fatal("the commit left the reader off the live edge")
	}
}

// esc leaves with nothing changed, and the sentence that was in the box comes
// back exactly as it was — a step cut, which takes no message back, restores it
// too.
func TestTheStashComesBackOnCancelAndOnAStepCut(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	a.input.setText("half a sentence")

	runCmd(a.enterRewind())
	drive(t, a, key("esc"))
	if a.rew.on {
		t.Fatal("esc did not leave the mode")
	}
	if got := a.input.String(); got != "half a sentence" {
		t.Fatalf("the draft came back as %q", got)
	}
	if len(agent.cuts) != 0 {
		t.Fatalf("a cancelled mode cut the session at %v", agent.cuts)
	}

	runCmd(a.enterRewind())
	drive(t, a, key("right")) // a STEP cut: nothing was said at it
	drive(t, a, key("enter"))
	if a.rew.on {
		t.Fatal("the step commit left the mode up")
	}
	if got := a.input.String(); got != "half a sentence" {
		t.Fatalf("a step cut put %q in the box, want the stash back", got)
	}
}

// The engine refuses while a turn is winding down. The sentence is the engine's
// own, it is drawn where the mode's feedback belongs, and the mode stays up so
// the same key can be pressed again a moment later.
func TestARefusalKeepsTheModeAndSaysWhy(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	agent.err = session.ErrTurnInFlight
	runCmd(a.enterRewind())

	drive(t, a, key("enter"))
	if !a.rew.on {
		t.Fatal("a refused commit left the mode")
	}
	if a.rew.said == "" {
		t.Fatal("the refusal was swallowed")
	}
	if got := plain(frame(a)); !strings.Contains(got, session.ErrTurnInFlight.Error()) {
		t.Fatalf("the mode bar does not carry the engine's sentence:\n%s", got)
	}
	// A key that moves the cut has answered it: the sentence was about a cut
	// that is no longer the one on offer.
	drive(t, a, key("up"))
	if a.rew.said != "" {
		t.Fatalf("the refusal outlived the cut it was about: %q", a.rew.said)
	}
	// And the offer is still there once the engine relents.
	agent.err = nil
	drive(t, a, key("enter"))
	if a.rew.on || len(agent.cuts) != 1 {
		t.Fatalf("the retried commit did not land (mode=%v, cuts=%v)", a.rew.on, agent.cuts)
	}
}

// ── 6. nothing to rewind ────────────────────────────────────────────────────

// A session with no points does not open a picker over an empty list: it says
// so, in the hint slot, and stays where it was.
func TestNothingToRewindSaysSoAndDoesNotEnter(t *testing.T) {
	a, _ := newRewindApp(t, nil)
	runCmd(a.enterRewind())
	if a.rew.on {
		t.Fatal("the mode opened over a conversation with nothing in it")
	}
	if got := a.hintWord(); got != rewindEmptyWord {
		t.Fatalf("hint slot = %q, want %q", got, rewindEmptyWord)
	}
	if got := plain(frame(a)); !strings.Contains(got, rewindEmptyWord) {
		t.Fatalf("the frame does not say there is nothing to rewind:\n%s", got)
	}

	// The double esc lands in the same place, and the sentence runs down on the
	// frame clock like every other window on this surface.
	now := time.Now()
	a.clock = func() time.Time { return now }
	drive(t, a, key("esc"), key("esc"))
	if a.rew.on {
		t.Fatal("esc esc opened a mode with no points")
	}
	if !a.rewindSaying() {
		t.Fatal("esc esc said nothing about an empty rewind")
	}
	now = now.Add(rewindSayWindow + time.Millisecond)
	drive(t, a, frameMsg{})
	if a.rewindSaying() || a.rewSay != "" {
		t.Fatal("the sentence outlived its window")
	}
}

// textsOf is the rows' text, for the assertions above.
func textsOf(rows []row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.text)
	}
	return out
}

// paintPrefix is the escape sequence one paint opens with, so a test can ask
// whether a row is still wearing it.
func paintPrefix(painted string) string {
	if at := strings.Index(painted, "m"); at > 0 {
		return painted[:at+1]
	}
	return painted
}
