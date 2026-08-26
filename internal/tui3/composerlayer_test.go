package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE COMPOSER LAYER (SCREEN 2e) ──────────────────────────────────────────
//
// The layer is a decision drawn over a page rather than a page of its own, and
// every test here is about one half of that sentence: what it says, and that the
// page under it is still there and still exactly where it was.

// layerApp is a place with something typed into its composer and the layer up,
// on whichever place the caller names. It fixes the git reading so the branch
// clause is a fact about this test rather than about the machine the suite runs
// on.
func layerApp(t *testing.T, at page, width int) *app {
	t.Helper()
	a := placeApp(t)
	a.width, a.height = width, 30
	restore := homeGitStatus
	homeGitStatus = func(context.Context, string) ([]byte, error) {
		return []byte("# branch.head master\n"), nil
	}
	t.Cleanup(func() { homeGitStatus = restore })
	a.showPage(at)
	box := a.placeBox()
	box.reset()
	for _, r := range "cut the opus spend in half" {
		box.insert(string(r))
	}
	a.placeKeyPress(key("alt+enter"))
	return a
}

// layerText is the whole frame with the paint stripped, which is how a person
// reads it.
func layerText(a *app) string {
	f, _, _ := a.frame()
	return plain(f)
}

// THE LAYER SAYS THE SAME FOUR THINGS AT EVERY WIDTH A PERSON READS AT, and the
// page behind it is still on the frame rather than covered. Both halves are the
// design's own claim about why this is a layer (SCREEN 2e).
func TestTheComposerLayerDrawsTheLeadTheThreeFactsAndTheFootAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		a := layerApp(t, pageSpend, width)
		if !a.composerShowing() {
			t.Fatalf("at %d alt+enter over a typed composer opened no layer", width)
		}
		text := layerText(a)
		for _, want := range []string{
			composerLeadWord,
			composerKindWord,
			tokens.GlyphProseBullet + " " + composerWhereInWord,
			composerCapSaysWord + dollars(composerCapDefault) + composerCapAsksWord,
			composerCapEditWord,
			composerFootSendWord + pageSpend.word(),
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("at %d the layer is missing %q:\n%s", width, want, text)
			}
		}
		// THE SENTENCE IS STILL IN THE BOX, unmoved, which is the whole reason
		// this is a layer: the composer does not go anywhere and neither does
		// what was typed into it.
		if !strings.Contains(text, "cut the opus spend in half") {
			t.Fatalf("at %d the layer lost the sentence it is about:\n%s", width, text)
		}
		// AND THE FRAME IS STILL EXACTLY THE TERMINAL. The layer's rows come out
		// of the body's room, never out of the frame.
		if got := len(strings.Split(strings.Join([]string{layerText(a)}, ""), "\n")); got != a.height {
			t.Fatalf("at %d the layer drew %d rows into %d", width, got, a.height)
		}
	}
}

// THE PAGE BEHIND DIMS RATHER THAN BEING COVERED. Its rows are still on the
// frame — you never lose your place — and every one of them is repainted, which
// is what says they are not the subject any more.
func TestTheComposerLayerDimsThePageBehindRatherThanCoveringIt(t *testing.T) {
	a := placeApp(t)
	a.width, a.height = 120, 30
	a.showPage(pageSpend)
	before, _, _ := a.frame()
	box := a.placeBox()
	for _, r := range "cut the opus spend in half" {
		box.insert(string(r))
	}
	a.placeKeyPress(key("alt+enter"))
	after, _, _ := a.frame()

	// The place's own body rows are on both frames, word for word.
	body := ""
	for _, line := range strings.Split(plain(before), "\n") {
		if trimmed := strings.TrimSpace(line); len(trimmed) > 12 && !strings.Contains(trimmed, "·") {
			body = trimmed
			break
		}
	}
	if body != "" && !strings.Contains(plain(after), body) {
		t.Fatalf("the layer covered the page instead of dimming it — %q is gone:\n%s", body, plain(after))
	}
	// And the fade is real paint: at the truecolor tier the two frames differ in
	// their escape sequences even where they agree word for word.
	if a.pal.fading() && before == after {
		t.Fatal("the layer changed no paint at all, so nothing receded")
	}
}

// `alt+w` CYCLES THE DESTINATION and comes back round to where it started. The
// list is this window's own project first, then every project home has read.
func TestAltWCyclesTheDestinationAndComesBackRound(t *testing.T) {
	a := layerApp(t, pageSpend, 120)
	places := a.composerPlaces()
	if len(places) < 2 {
		t.Fatalf("this lab has %d destinations and the cycle needs two", len(places))
	}
	first := a.composerWhere()
	if first != places[0] {
		t.Fatalf("the layer opened on %q rather than on the first destination %q", first, places[0])
	}
	for i := 1; i < len(places); i++ {
		a.placeKeyPress(key("alt+w"))
		if got := a.composerWhere(); got != places[i] {
			t.Fatalf("the %d%s alt+w reached %q, wanted %q", i, "th", got, places[i])
		}
	}
	a.placeKeyPress(key("alt+w"))
	if got := a.composerWhere(); got != first {
		t.Fatalf("the cycle did not come back round: %q, wanted %q", got, first)
	}
	// AND THE CLAUSE NAMING THE KEY IS ON THE LINE, because the key does
	// something here (SCREEN 3a's clause).
	if !strings.Contains(layerText(a), composerMoveWord) {
		t.Fatalf("the destination line does not name the key that moves it:\n%s", layerText(a))
	}
}

// TYPING DIGITS EDITS THE CAP, and what leaves with the task is the figure that
// was on the screen. The default is the tank the engine applies when nobody
// names one.
func TestTypingANumberEditsTheCapAndTheCapIsWhatLeaves(t *testing.T) {
	lab := newErrandLab(t)
	a := lab.app(lab.session("-tmp-alpha", "aaaa000000000001", "pricing research",
		lab.workspace("alpha"), time.Now()))
	a.width, a.height = 120, 30
	a.openHome()
	typeHome(a, "watch the relay")
	drive(t, a, key("alt+enter"))
	if !a.composerShowing() {
		t.Fatal("alt+enter opened no layer on home")
	}
	if got := a.composerCapUSD(); got != composerCapDefault {
		t.Fatalf("the layer opened on a cap of %v, wanted the engine's own %v", got, composerCapDefault)
	}
	for _, digit := range "2.50" {
		drive(t, a, key(string(digit)))
	}
	if got := a.composerCapWord(); got != "$2.50" {
		t.Fatalf("the third line reads %q after typing 2.50", got)
	}
	// A BACKSPACE TAKES ONE CHARACTER OFF, because a figure is typed and typed
	// figures are got wrong.
	drive(t, a, key("backspace"))
	if got := a.composerCapWord(); got != "$2.5" {
		t.Fatalf("backspace left %q", got)
	}
	drive(t, a, key("alt+enter"))
	if len(lab.orders) != 1 {
		t.Fatalf("the second alt+enter sent %d errands", len(lab.orders))
	}
	if got := lab.orders[0].CapUSD; got != 2.5 {
		t.Fatalf("the errand left with a cap of %v, wanted 2.5", got)
	}
	// AND THE SENTENCE WENT WITH IT, which is the whole point of the chord.
	if len(lab.agent.sent) != 1 || lab.agent.sent[0] != "watch the relay" {
		t.Fatalf("the errand carried %v", lab.agent.sent)
	}
}

// `esc` PUTS YOU BACK ON THE SAME PLACE, with the sentence still in the box.
// Nothing was applied on the way in, so nothing has to be undone.
func TestEscFromTheLayerReturnsToTheSamePlaceWithTheSentenceIntact(t *testing.T) {
	a := layerApp(t, pageSpend, 120)
	a.placeKeyPress(key("esc"))
	if a.composerShowing() {
		t.Fatal("esc left the layer up")
	}
	if !a.at(pageSpend) {
		t.Fatalf("esc walked off the spend place onto %q", a.page.word())
	}
	if got := a.placeBox().String(); got != "cut the opus spend in half" {
		t.Fatalf("esc lost the sentence: %q", got)
	}
	if !strings.Contains(layerText(a), placeHintTail) {
		t.Fatalf("the place did not get its own foot back:\n%s", layerText(a))
	}
}

// `enter` TALKS ABOUT IT INSTEAD, which is the existing door: the layer goes and
// a conversation opens carrying the sentence.
func TestEnterFromTheLayerTalksAboutItInstead(t *testing.T) {
	a := layerApp(t, pageSpend, 120)
	a.placeKeyPress(key("enter"))
	if a.composerShowing() {
		t.Fatal("enter left the layer up")
	}
	if a.pageShowing() {
		t.Fatalf("enter left a place standing: %q", a.page.word())
	}
}

// THE ERRAND LANDS ON HOME, from whichever place it was sent. Home's column is
// the only surface that draws an errand's answer, and this is the behaviour the
// manual states out loud (places.md) until a place-independent errands band
// lands.
func TestSendingFromAnotherPlaceCarriesYouToHomeWhereTheAnswerIsDrawn(t *testing.T) {
	lab := newErrandLab(t)
	a := lab.app(lab.session("-tmp-alpha", "aaaa000000000001", "pricing research",
		lab.workspace("alpha"), time.Now()))
	a.width, a.height = 120, 30
	a.openHome()
	typeHome(a, "watch the relay")
	a.showPage(pageSpend)
	// The sentence stays in home's own box, which is the composer home types
	// into — so it is typed again into the box the spend place has.
	box := a.placeBox()
	box.reset()
	for _, r := range "watch the relay" {
		box.insert(string(r))
	}
	drive(t, a, key("alt+enter"))
	if !a.composerShowing() {
		t.Fatal("alt+enter opened no layer on the spend place")
	}
	drive(t, a, key("alt+enter"))
	if !a.at(pageHome) {
		t.Fatalf("the errand was minted on %q rather than carrying to home", a.page.word())
	}
	if theExchange(a) == nil {
		t.Fatal("the send left no errand on home")
	}
}

// `alt+o` OPENS THE MODEL LIST AND IT IS THE ONE MODEL LIST. What the layer
// holds is a [picker] — the same type, the same rows and the same walk /model
// and the settings panel use — and enter on a row binds the EXECUTION slot for
// this task only.
func TestAltOOpensTheOneModelListScopedToTheExecutionSlot(t *testing.T) {
	a := layerApp(t, pageSpend, 120)
	a.placeKeyPress(key("alt+o"))
	if !a.composer.pick.open {
		t.Fatal("alt+o opened no model list")
	}
	if !strings.Contains(layerText(a), composerPickWord) {
		t.Fatalf("the list drew no foot of its own:\n%s", layerText(a))
	}
	chosen, ok := a.composer.pick.choice()
	if !ok {
		t.Skip("this lab's catalog offers no model to pick")
	}
	a.placeKeyPress(key("enter"))
	if a.composer.pick.open {
		t.Fatal("enter left the list open")
	}
	if a.composer.model != chosen.ID {
		t.Fatalf("the layer bound %q rather than %q", a.composer.model, chosen.ID)
	}
	if !strings.Contains(layerText(a), chosen.ID) {
		t.Fatalf("the model line does not name what was chosen:\n%s", layerText(a))
	}
	// AND THE CONVERSATION'S OWN MODEL IS UNTOUCHED: the execution slot is what
	// the WORK runs on, and the errand still talks on the launch's model.
	if a.model == chosen.ID && chosen.ID != "m" {
		t.Fatal("alt+o moved the conversation's model, which is a different slot")
	}
}

// A KEY IS DRAWN ONLY WHERE IT DOES SOMETHING. With one destination there is
// nowhere to move a task to, so the clause naming `alt+w` is absent — and the
// key does nothing rather than something undrawn.
func TestTheMoveClauseIsAbsentWhereThereIsNowhereToMoveTo(t *testing.T) {
	a := layerApp(t, pageSpend, 120)
	a.composer.places = a.composer.places[:1]
	if got := len(a.composerPlaces()); got != 1 {
		t.Fatalf("this window has %d destinations, wanted one", got)
	}
	if strings.Contains(layerText(a), composerMoveWord) {
		t.Fatalf("the layer named a key with nothing to do:\n%s", layerText(a))
	}
	if a.composerMove() {
		t.Fatal("alt+w moved a task with nowhere to move it to")
	}
}
