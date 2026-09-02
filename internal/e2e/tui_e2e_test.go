//go:build e2e

package e2e

// TestTUIE2E is the ambient side of v3, driven end to end: the real binary, a
// real terminal, a real model, and the screen read back with capture-pane.
//
// Every subtest builds its own AFORGE_HOME and its own repository, and NOT ONE
// STRING IS WRITTEN DOWN HERE. Every needle comes through [say] out of the table
// in tuiwords_test.go, which an ordinary untagged test reads back against
// internal/tui3's own sources — so a sentence the surface stops drawing turns a
// four-hundred-millisecond gate red on the pull request that removed it, rather
// than turning this seventeen-minute suite red in a wave nobody ran it in. That
// is issue #184's whole mechanism, and it exists because this file spent a week
// waiting for a home that had been redesigned out from under it.
//
// ── HOW TO RUN IT ───────────────────────────────────────────────────────────
//
//	go test -tags e2e -count=1 -timeout 40m -v ./internal/e2e/
//
// It needs OPENROUTER_API_KEY and tmux, costs a few cents, and takes about
// seventeen minutes. CLAUDE.md's Tests section says the same thing.
//
// ── TWO WIDTHS, AND THE REASON IS IN THE PRODUCT ────────────────────────────
//
// Home has two shapes and the width chooses between them (internal/tui3's
// homebridge.go): below a hundred and sixty cells the switcher takes the whole
// frame and THERE IS NO CARD AT ALL, and at or above it the card stands beside
// the list. So a subtest that reads a card runs at [tuiWide] and a subtest that
// is about the plain list runs at [tuiPlain] — and a card assertion at a hundred
// and twenty cells is not a failure, it is a test asking the product for
// something it correctly does not draw there.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// modelPatience is how long any one real turn is given. deepseek-v4-flash
// answers a one-line question in seconds; a reminder that has to reach for the
// `stand` tool takes longer, and a machine under load takes longer again.
const modelPatience = 90 * time.Second

// The two frames this suite drives, and why each is the width it is.
const (
	// tuiPlain is an ordinary terminal: home is one flat ranked list and the
	// right-hand note on each row carries the one fact the card was for.
	tuiPlain = 120
	// tuiWide is past internal/tui3's homeCardMin, where the width is genuinely
	// spare and the card stands beside the list.
	tuiWide = 180
	// tuiCardAt is the first screen column the card's own text stands in at
	// [tuiWide], and it is arithmetic rather than a guess: homeColumns gives the
	// card thirty-six cells plus half of everything past the tier's floor —
	// 36 + (180-160)/2 = 46 — with a four-cell gutter before it, so the list ends
	// at 130 and the card begins at 134. A subtest that reads the pane fails
	// loudly rather than quietly if that ever moves, because the pane comes back
	// empty.
	tuiCardAt = 134
)

func TestTUIE2E(t *testing.T) {
	requireTmuxAndKey(t)

	t.Run("home_opens_on_launch_as_the_ranked_switcher", testHomeShape)
	t.Run("a_real_conversation_and_its_home_card", testRealConversation)
	t.Run("ask_here_end_to_end", testAskHere)
	t.Run("the_firing_reaches_the_person", testFiringReachesThePerson)
	t.Run("answer_from_home_across_two_windows", testAnswerFromHome)
	t.Run("hover_previews_the_row_under_the_pointer", testHover)
	t.Run("the_fold_at_the_foot_opens_and_shuts", testFold)
	t.Run("narrow_window_ask_here", testNarrow)
	t.Run("alt_g_groups_the_list_by_project", testGrouped)
	t.Run("one_figure_on_every_spend_surface", testOneSpendFigure)
	t.Run("a_nested_landing_asks_and_a_key_answers_it", testNestedGate)
	t.Run("a_crew_older_than_the_work_seat_says_so_once", testInheritedWorkSeat)
}

// ── 11 ──────────────────────────────────────────────────────────────────────

// testNestedGate is issue #268's acceptance, on the real screen: a part one
// level down that nobody could check ASKS, in one frame, and a key answers it.
//
// THE MEASURED FAILURE IS WHY IT IS HERE. A nested part landed needing somebody
// to decide, and a fifteen-second capture of the whole run shows no answers row
// for it, ever — the card was written for ROOT nodes only, and the roster filed
// the node under `done` while its parent still ran. So the gate expired without
// a person ever being able to see it, let alone answer it.
//
// AND IT COSTS NOTHING. The family is seeded as the graph the process left
// behind ([seedDecidedFamily]); the surface replays it on attach. No model is
// asked anything, so what this subtest measures is the surface and the engine's
// settle door and nothing else — which is exactly what went wrong.
func testNestedGate(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "gatews", false)
	seedDecidedFamily(t, home, ws)
	r := start(t, "afe2e_gate", home, ws, tuiWide, 40)

	// WHICHEVER DOOR THE LAUNCH TOOK. A machine with no conversation for this
	// workspace opens home; one that has the seeded conversation opens straight
	// into it — both are the product behaving, and esc from the first is the
	// second.
	r.waitForAny(20*time.Second, say(t, "homeFootWord"), say(t, "settleAskWord"))
	r.keys("Escape")

	// ONE FRAME, BOTH HALVES. The roster's `?` and its words for a node waiting
	// on a person, and the answers row on the card — all on screen at once, which
	// is the whole of what "answerable" means here.
	screen := r.waitFor(20*time.Second,
		say(t, "settleAskWord"), say(t, "settleAccept"),
		say(t, "taskLookWord"), say(t, "unverifiedGlyph"))
	t.Logf("a nested landing asking on every surface:\n%s", screen)
	if !strings.Contains(screen, "Port the parser") {
		t.Fatalf("the nested part is not named on the screen:\n%s", screen)
	}

	// AND A KEY ANSWERS IT. The four letters work on the SELECTED card and only
	// over an empty message box, exactly as `x` does — so ↑ walks to the card the
	// landing just wrote, and `a` is the accept.
	r.keys("Up")
	r.lit("a")
	settled := r.waitFor(20*time.Second, say(t, "settleTookLine"))
	t.Logf("the accept was spent and the card wears the receipt:\n%s", settled)
	r.quit()
}

// ── 1 ───────────────────────────────────────────────────────────────────────

// testHomeShape opens the product with five projects on the machine and reads
// the shape home has TODAY: one flat ranked list of conversations, the project
// demoted to a tag on the row, the foot's three verbs, and the two doors in and
// out of the screen.
//
// WHAT THIS SUBTEST USED TO ASSERT AND NO LONGER CAN. It waited for `aforge`
// and `esc close` in the head, a dim `─ elsewhere` rule, and two `▸ delta` /
// `▸ epsilon` folded project lines under it. All three went with the home
// rethink (internal/tui3/place_home.go, "HOME IS A SWITCHER, NOT A DIRECTORY"):
// the tree of projects became one flat ranked list, so there is no elsewhere
// rule and nothing on the desktop builds a folded project row at all. The two
// tiers the old name promised are gone; what is left is the list, and the fold
// at its foot, which subtest 7 is about.
func testHomeShape(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "shapews", false)
	r := start(t, "afe2e_shape", home, ws, tuiPlain, 40)

	screen := r.waitFor(20*time.Second, say(t, "homeFootWord"), say(t, "placeRestWord"))
	t.Logf("home greeted on launch:\n%s", screen)

	// EVERY SEEDED CONVERSATION IS ON THE LIST, whichever project it belongs to,
	// because the list is the switcher now and reaches all of them.
	for _, want := range []string{"Seed Alpha", "Seed Beta", "Seed Gamma", "Seed Delta", "Seed Epsilon"} {
		if !strings.Contains(screen, want) {
			t.Errorf("home is missing %q", want)
		}
	}
	// AND THE ROW SAYS THE PROJECT FOLDER IS NOT THERE, before anything is
	// pressed. The fixture's workspaces are paths under /tmp that were never
	// created, which is exactly the case the row's refusal exists for.
	if !strings.Contains(screen, say(t, "homeGoneShort")) {
		t.Errorf("no row says %q about a seeded project whose folder was never made:\n%s",
			say(t, "homeGoneShort"), screen)
	}
	// The whole foot, in one sentence: the three verbs and the key that leaves.
	if !strings.Contains(screen, say(t, "homeFootWord")+" · "+say(t, "placeHintTail")) {
		t.Errorf("home's foot is not the resting sentence:\n%s", firstMatch(screen, say(t, "homeFootWord")))
	}

	// THE LIST NEVER TOUCHES THE RULE ABOVE THE BOX (internal/tui3's home_test.go
	// pins it at every height): the row above the foot's rule is blank whatever
	// the list did.
	lines := r.lines()
	foot := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "────") {
			foot = i
			break
		}
	}
	if foot < 1 {
		t.Fatalf("no foot rule on the screen:\n%s", screen)
	}
	if got := strings.TrimSpace(lines[foot-1]); got != "" {
		t.Errorf("the list touches the foot: the row above the rule is %q", got)
	}
	t.Logf("padding row above the foot rule (row %d) is blank", foot-1)

	// esc closes home into the conversation the launch loaded, and the rule over
	// that conversation's box names both doors back.
	r.keys("Escape")
	closed := r.waitFor(15*time.Second, say(t, "homeDoorWord"), say(t, "microcopy"))
	t.Logf("esc closed home into the conversation the launch loaded:\n%s", closed)
	if strings.Contains(closed, say(t, "homeFootWord")) {
		t.Errorf("esc did not close home:\n%s", closed)
	}
	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	back := r.waitFor(15*time.Second, say(t, "homeFootWord"), "Seed Alpha")
	t.Logf("/home reopened it:\n%s", back)

	// And two spaces on an empty box is the other door.
	r.keys("Escape")
	time.Sleep(1200 * time.Millisecond)
	r.keys("Space")
	r.keys("Space")
	gesture := r.waitFor(15*time.Second, say(t, "homeFootWord"))
	t.Logf("space space opened home:\n%s", gesture)
}

// ── 2 ───────────────────────────────────────────────────────────────────────

// testRealConversation asks the model one question and then reads the CARD that
// conversation gets beside the list — which is why this one runs at [tuiWide].
//
// WHAT WENT, AND WHERE IT WENT. The card used to carry sixteen registered bands
// and now carries five (internal/tui3/place_home.go states the selection rule
// and lists every casualty). Two of them were this subtest's:
//
//   - the LEFT-OFF band, the person's last message as one muted `› ` line. The
//     row's own note carries that now, one column over, so a card drawing it
//     again would be the wide frame reading the same fact twice.
//   - the KEYS band, `enter open · n new chat here`. The legend became the
//     `→ verbs` line, because a letter is a verb only while the strip naming it
//     is on screen and a card printing `n new chat here` advertises a keystroke
//     the composer is about to eat.
//
// The repository band did not go — it folded into the card's place line, where
// the address it is about already stood, with its clauses joined by a comma. So
// that assertion is kept and respelled rather than dropped.
func testRealConversation(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "repows", true)
	r := start(t, "afe2e_talk", home, ws, tuiWide, 40)

	r.lit("what is 2+2, one word")
	r.keys("Enter")
	hit, screen := r.waitForAny(modelPatience, "\n4", " 4\n", "four", "Four")
	t.Logf("the model answered (%q):\n%s", hit, screen)

	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	// THE CURSOR OPENS ON THE CONVERSATION THIS WINDOW HOLDS, which is the only
	// one on this machine, so the card beside the list is this conversation's
	// without anything being walked to.
	card := r.waitFor(20*time.Second, say(t, "homeFootWord"), say(t, "homeVerbsWord"))
	t.Logf("home, with the conversation's card up:\n%s", card)
	pane := rightPane(card)

	// The repository, on the card's place line. It must agree with the
	// repository, so the count is taken from git at the moment of the assertion
	// rather than assumed — and the clauses inside it are joined with a comma,
	// because `main, 1 file dirty` is one clause about one repository.
	dirty := dirtyFiles(t, ws)
	want := fmt.Sprintf("main, %d %s dirty", len(dirty), plural("file", len(dirty)))
	if !strings.Contains(pane, want) {
		t.Errorf("the card's place line does not read %q — it reads %q (git says %v)",
			want, firstMatch(pane, "main"), dirty)
	} else {
		t.Logf("the place line agrees with git: %q (%v)", want, dirty)
	}
	// AND WHAT MADE IT DIRTY. The test changed exactly one tracked file; a
	// second entry is something the product itself dropped in the person's
	// working directory, which is worth naming rather than absorbing.
	for _, name := range dirty {
		if !strings.HasSuffix(name, "README.md") {
			t.Logf("FINDING: the run left %q in the person's workspace and the place line counts it", name)
		}
	}
	// The facts line. `last active` is always true of a conversation somebody
	// just spoke in; `spent` is drawn from the whole rollup (home.go's
	// homeFacts), so it is recorded rather than demanded.
	if !strings.Contains(pane, say(t, "homeFactsActive")) {
		t.Errorf("the card has no facts line:\n%s", pane)
	}
	if strings.Contains(pane, "spent $") {
		t.Logf("the facts line drew a `spent $…` clause: %s", firstMatch(pane, "spent $"))
	} else {
		t.Logf("FINDING: no `spent $…` clause on a conversation that really spent money. "+
			"The facts line reads: %s", firstMatch(pane, say(t, "homeFactsActive")))
	}
	// And the card's last line, which names what can be done and never which
	// letter does it.
	if !strings.Contains(pane, say(t, "homeVerbsWord")) {
		t.Errorf("the card has no verbs line:\n%s", pane)
	}
}

// ── 3 ───────────────────────────────────────────────────────────────────────

// testAskHere is the whole `ask here` flow against the real model: the two
// action rows, the errand's own row and its tails, the card, the answer, and the
// row still being there after the screen it was asked on has been closed and
// reopened.
//
// IT RUNS AT [tuiWide] SO THAT THE LIST AND THE EXCHANGE ARE BOTH ON SCREEN. At
// an ordinary width the exchange takes the whole frame and the row it belongs to
// — with the `working` / `waiting on you` / `stood` tails this subtest is really
// about — is not drawn at all.
//
// WHAT WENT. The old subtest ended by looking for a `◦ remind me …` row under
// the project on home, drawn by the standing band a project used to carry. There
// is no such band any more: the resting list is what wants you now, and what
// stands lives on the standing place (internal/manual/chat/home.md says so in as
// many words). What replaced the assertion is the settled card's own words in
// the pane, which say the same thing about the same act.
func testAskHere(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "askws", false)
	r := start(t, "afe2e_ask", home, ws, tuiWide, 45)

	// One ordinary conversation first, so this project has something on home for
	// the exchange row to sit above.
	r.lit("say ok and nothing else")
	r.keys("Enter")
	r.waitForAny(modelPatience, "ok", "OK", "Ok")

	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	r.waitFor(20*time.Second, say(t, "homeFootWord"))

	r.lit("remind me in 1 minute to drink water")
	time.Sleep(700 * time.Millisecond)
	typed := r.capture()
	if !strings.Contains(typed, say(t, "homeAskHereWord")+": ") ||
		!strings.Contains(typed, say(t, "homeStartWord")+": ") {
		t.Errorf("the two action rows are not both drawn while something is typed:\n%s", typed)
	}
	t.Logf("the action rows while typing:\n%s", typed)

	// ctrl+enter, sent as the CSI 13;5u a kitty-protocol terminal sends.
	r.ctrlEnter()

	working := r.waitFor(25*time.Second, "? remind me in 1 minute", say(t, "homeAskWorkingWord"))
	t.Logf("the exchange row is working:\n%s", working)

	// AND WITH SOMETHING MOVING, THE LIST WEARS ITS SECTION LINE — the one
	// heading over the flat ranked list, with the two keys that change its shape
	// out at the right margin. It is drawn only while something is asking or
	// moving, which is exactly now.
	for _, want := range []string{
		say(t, "switcherSectionWord"), say(t, "switcherGroupWord"), say(t, "switcherQuietWord"),
	} {
		if !strings.Contains(working, want) {
			t.Errorf("the section line is missing %q while an errand is running:\n%s", want, working)
		}
	}

	// The pane's own clock, caught in flight. It lives for seconds, so this is
	// a fast poll and it is a finding rather than a failure when it is missed.
	if caught, ok := r.glimpse(25*time.Second,
		say(t, "homeAskThinkWord"), say(t, "homeAskWriteWord"), say(t, "homeAskRunWord")); ok {
		t.Logf("the live strip was caught mid-turn:\n%s", caught)
	} else {
		t.Logf("FINDING: never caught the live strip in the pane")
	}

	// Everything the pane draws while the errand runs, kept so the tool rows
	// can be read afterwards.
	seen := []string{}
	deadline := time.Now().Add(modelPatience)
	waiting := ""
	for time.Now().Before(deadline) {
		screen := r.capture()
		seen = append(seen, screen)
		if strings.Contains(screen, say(t, "notifyAskWord")) {
			waiting = screen
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if waiting == "" {
		t.Fatalf("the card never arrived. last screen:\n%s", r.capture())
	}
	// The card is drawn over several frames; give it one before reading the
	// answers off it, or this reads a half-painted row.
	time.Sleep(2 * time.Second)
	waiting = r.capture()
	seen = append(seen, waiting)
	t.Logf("the card arrived and the row tail says it is waiting on somebody:\n%s", waiting)

	// THE ANSWERS ARE READ OFF THE FOOT AND NOT OFF THE CHIPS. The card lives in
	// a forty-six-cell pane and the chips give their words up to fit it — `[ 1
	// yes ]  [ 2 change ]  [ 0 no ]` — while the hint under the box spells every
	// answer in full at every width (internal/tui3's exchangeHint). So the foot
	// is where this suite reads what a person is being offered.
	//
	// AND THE FOOT NAMES WHAT THE CARD DREW AND NOTHING MORE (#189, fixed). It
	// used to say `3 just once` over a one-off reminder whose card offers no
	// such chip, because the line was a third hardcoded copy of a sentence
	// internal/tui3 already kept two correct spellings of. It is built from the
	// chips now, so the reminder this subtest asks for is offered three answers
	// and the needle spells three.
	if !strings.Contains(waiting, say(t, "exchangeAnswerHint")) {
		t.Errorf("the foot does not offer the card's answers:\n%s", waiting)
	}
	if !strings.Contains(waiting, say(t, "exchangeFollowUp")) {
		t.Errorf("the foot does not say what enter does in the pane:\n%s", waiting)
	}
	if !strings.Contains(waiting, say(t, "exchangeBack")) {
		t.Errorf("the foot does not name the way back to the list:\n%s", waiting)
	}

	// WHAT THE MODEL ACTUALLY DID. No tool row may say `unknown`
	// (asking-from-home.md states it), and the instructions forbid running
	// `date` to learn the time (keeping-an-eye.md).
	all := strings.Join(seen, "\n")
	if strings.Contains(all, "unknown") {
		t.Errorf("a tool row said `unknown`:\n%s", firstMatch(all, "unknown"))
	}
	if strings.Contains(all, "bash · date") || strings.Contains(all, "bash date") {
		t.Logf("FINDING: the model still ran `date` before setting the reminder: %s",
			firstMatch(all, "date"))
	} else {
		t.Logf("the model did not run `date` — it used the Now line in its instructions")
	}
	t.Logf("tool rows drawn in the pane: %s", strings.Join(toolRows(seen), " | "))

	// AN EXCHANGE OUTLIVES THE SCREEN IT WAS ASKED ON. This one is holding a
	// card, which is the case asking-from-home.md states outright: closing home
	// does not touch it, and neither does opening another conversation.
	r.keys("Escape") // keyboard back on the list
	time.Sleep(1200 * time.Millisecond)
	r.keys("Escape") // home closes into the conversation underneath
	time.Sleep(2500 * time.Millisecond)
	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	again := r.waitFor(20*time.Second, say(t, "homeFootWord"))
	if !strings.Contains(again, "? remind me in 1 minute") || !strings.Contains(again, say(t, "notifyAskWord")) {
		t.Errorf("the exchange did not outlive the screen it was asked on:\n%s", again)
	}
	t.Logf("home reopened and the exchange is still waiting on somebody:\n%s", again)

	// Walk onto the row and hand the keyboard to the pane. THE HINT UNDER THE BOX
	// IS THE ORACLE for where the cursor is standing: the switcher's rows carry
	// no `›` lead of their own, and the one line that changes with the cursor is
	// the hint (internal/tui3's homeHint).
	if !walkTo(r, say(t, "homeAnswerHint"), "Up") {
		t.Fatalf("could not put the cursor back on the exchange row:\n%s", r.capture())
	}
	r.keys("Enter")
	time.Sleep(1200 * time.Millisecond)

	r.lit("1")
	stood := r.waitFor(30*time.Second, say(t, "homeAskStoodTail"))
	time.Sleep(1500 * time.Millisecond)
	stood = r.capture()
	t.Logf("answered `1` — the row says something stands:\n%s", stood)
	if !strings.Contains(stood, say(t, "standYesWord")+" · "+say(t, "standSetWord")) {
		t.Errorf("the settled card does not carry the answer and its verdict:\n%s", stood)
	}
	if !strings.Contains(stood, say(t, "homeAskStoodWord")) {
		t.Errorf("the pane does not say the exchange is filed under what it made:\n%s", stood)
	}

	// Walk off the exchange onto another row: the pane must draw THAT row. The
	// keyboard is already back on the list — answering a card hands it back —
	// so an `esc` here would close home instead of moving anything.
	before := rightPane(r.capture())
	for i := 0; i < 3; i++ {
		r.keys("Down")
		time.Sleep(400 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)
	walked := r.capture()
	t.Logf("after walking down off the exchange row:\n%s", walked)
	if pane := rightPane(walked); pane == before {
		t.Errorf("the pane did not change when the cursor walked off the exchange row:\n%s", walked)
	}
	if strings.Contains(rightPane(walked), "› remind me in 1 minute to drink water") {
		t.Errorf("the pane still draws the exchange after the cursor walked off it:\n%s", walked)
	}
}

// walkTo steps the cursor along the list, one press of `key` at a time, until
// the hint under the box says it is standing on the row this test wants, and
// answers whether it got there.
//
// THE HINT IS THE ORACLE AND NOT THE ROW. The switcher's rows carry no `›` lead
// of their own — the band under the cursor is the whole of the selection — so
// the one line on the frame that changes with the cursor is the hint
// (internal/tui3's homeHint), and reading it is how this suite knows where the
// keyboard is standing without asking the product to draw a mark for the test's
// benefit.
func walkTo(r *rig, hint, key string) bool {
	for i := 0; i < 14; i++ {
		if strings.Contains(r.capture(), hint) {
			return true
		}
		r.keys(key)
		time.Sleep(400 * time.Millisecond)
	}
	return strings.Contains(r.capture(), hint)
}

// ── 4 ───────────────────────────────────────────────────────────────────────

// testFiringReachesThePerson stands a one-minute reminder, sits in an ordinary
// conversation of the same project, and waits for the window's own pass to
// fire it. Then it quits, fires a second one from outside every window with
// `aforge tick`, and reopens to read what was left waiting.
//
// WHAT WENT. The second half used to reopen from ANOTHER project, put the
// pointer on a folded `▸ firews` line and read the news band off the project
// card that only such a line drew. Folded project lines went with the home
// rethink and nothing on the desktop builds one now, so what a person actually
// meets when they come back is read instead: the project's own inbox, which is
// the road a firing takes with no window open, and home's `since you left`
// block, which is where that firing surfaces.
func testFiringReachesThePerson(t *testing.T) {
	if testing.Short() {
		t.Skip("this one waits for the five-minute standing pass")
	}
	home := newHome(t, nil)
	ws := newWorkspace(t, "firews", false)
	r := start(t, "afe2e_fire", home, ws, tuiWide, 45)
	started := time.Now()

	// An ordinary conversation to sit in. A firing whose origin is an `ask
	// here` exchange has no room of its own, so road 2 of the delivery — any
	// other open conversation of the same project — is the one under test.
	r.lit("say ok and nothing else")
	r.keys("Enter")
	r.waitForAny(modelPatience, "ok", "OK", "Ok")

	openHome(t, r)
	standReminder(t, r, "remind me in 1 minute to drink water")

	// Back into the conversation and wait. The window runs the same pass the
	// timer runs, every standing.Interval (five minutes), the first one an
	// interval after launch. Answering the card already handed the keyboard
	// back to the list, so ONE esc closes home.
	r.keys("Escape")
	time.Sleep(2500 * time.Millisecond)

	// /status, while something stands: the derived `keeping watch` line and the
	// status line's own segment.
	if !strings.Contains(r.capture(), say(t, "homeKeepingWord")) {
		t.Errorf("the status line has no `keeping an eye on N` segment while an item stands:\n%s", r.capture())
	}
	r.lit("/status")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	status := r.waitFor(20*time.Second, say(t, "homeWatchLabel"))
	t.Logf("/status while something stands:\n%s", status)
	if !strings.Contains(status, say(t, "homeKeepingWord")) {
		t.Errorf("/status says nothing about what is being kept an eye on:\n%s", status)
	}

	// WHAT THE ITEM ITSELF SAYS IT WILL SAY. The model names the standing order
	// and writes the sentence it fires, and neither is anything this test may
	// assume: one run called it `drink water reminder` and fired
	// `💧 Time to drink water!`. So the record on disk is read and the needles
	// are taken off it — which is the same law the rest of this suite follows
	// for the surface's own words, applied to the one vocabulary the MODEL owns.
	item, ok := standingRecordAbout(t, home, "water")
	if !ok {
		t.Fatalf("nothing stood for the water reminder at all. records:\n%s", standingRecordsDump(t, home))
	}
	t.Logf("the item that stood: %q, due %s, saying %q",
		item.Brief.Title, item.When.At.Format(time.RFC3339), item.Does.Say)

	// Now wait for the pass. It is one interval from launch plus the minute the
	// reminder asked for, with room for a slow machine.
	wait := 6*time.Minute + 30*time.Second - time.Since(started)
	if wait < time.Minute {
		wait = time.Minute
	}
	t.Logf("waiting %s for the window's own standing pass", wait.Round(time.Second))
	// TWO NEEDLES, BECAUSE THE JOURNAL AND THE SCREEN SAY IT DIFFERENTLY. The
	// steering line the engine injects carries what the item fires; the ROW the
	// surface draws wears the item's own short name and then what the firing said
	// (internal/tui3's standName, pinned by TestAStandingUpdateIsExactlyOneLine).
	// So the transcript is searched for the sentence and the screen for the row.
	said := say(t, "standSaidTag")
	// THE ROW WEARS THE PERSON'S OWN WORDS, CUT SHORT — internal/tui3's standName
	// takes the head of what was asked for, not the name the model gave the item
	// (which is often empty), so the needle is the head of the item's `words`.
	drawnRow := firstWords(item.Words, 5)
	if drawnRow == "" {
		t.Fatalf("the item that stood has no words to look for:\n%s", standingRecordsDump(t, home))
	}
	deadline := time.Now().Add(wait)
	drawn := false
	for time.Now().Before(deadline) {
		if screen := r.capture(); strings.Contains(screen, drawnRow) && strings.Contains(screen, said) {
			drawn = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	screen := r.capture()
	delivered := false
	for path, raw := range sessionTranscripts(t, home) {
		if strings.Contains(raw, firstWords(item.Does.Say, 4)) {
			delivered = true
			t.Logf("the firing reached the conversation's journal: %s", path)
		}
	}
	if !delivered {
		// AND THE SUITE SAYS WHY, RATHER THAN JUST THAT. An item whose expiry is
		// not after its own moment is retired by rail one of the pass before
		// anything is ever due (internal/standing/tick.go), so it can never fire —
		// and the `stand` tool accepts such a proposal today (issue #188). That is
		// a product defect and not this test's, and a failure that did not name it
		// costs somebody the hour it cost to find.
		later := standingRecordByID(t, home, item.ID)
		if !later.Rails.Expires.IsZero() && !later.Rails.Expires.After(later.When.At) {
			t.Fatalf("DEFECT (issue #188): the item stood with an expiry at or before its own moment — "+
				"expires %s, due %s — so the pass retired it (%q) instead of ever firing it.\nscreen:\n%s",
				later.Rails.Expires.Format(time.RFC3339), later.When.At.Format(time.RFC3339),
				later.LastCheckLine, screen)
		}
		t.Fatalf("the firing never reached the person at all — nothing in any transcript.\nrecord:\n%s\nscreen:\n%s",
			standingRecordsDump(t, home), screen)
	}
	if !drawn {
		t.Errorf("DEFECT: the firing reached the conversation but was never DRAWN in it.\n"+
			"The journal holds the line as a session-authored note, and the model answered it, "+
			"but no `%s … %s` row appears on the screen the person is looking at:\n%s", drawnRow, said, screen)
	} else {
		t.Logf("the firing is drawn in the conversation:\n%s", screen)
	}

	// ── the second half: nobody is here when it fires ──
	//
	// A firing wakes the conversation it lands in, so the model may still be
	// answering it. esc ends whatever is in flight before the next errand.
	r.keys("Escape")
	time.Sleep(2 * time.Second)
	openHome(t, r)
	standReminder(t, r, "remind me in 1 minute to stretch")
	r.keys("Escape")
	time.Sleep(2 * time.Second)
	r.quit()

	time.Sleep(75 * time.Second)
	out := tick(t, home)
	t.Logf("`aforge tick` said %q", strings.TrimSpace(out))

	second, ok := standingRecordAbout(t, home, "stretch")
	if !ok {
		t.Fatalf("nothing stood for the stretch reminder. records:\n%s", standingRecordsDump(t, home))
	}
	inbox := projectInbox(t, home, ws)
	if !strings.Contains(inbox, firstWords(second.Does.Say, 4)) {
		t.Errorf("nothing was left waiting for the person after the tick — the item fires %q. project inbox:\n%s",
			second.Does.Say, inbox)
	} else {
		t.Logf("the firing was filed under the project for the next window:\n%s", inbox)
	}

	// AND THE NEXT WINDOW IS TOLD, TWICE OVER. A launch in a project that already
	// holds a conversation resumes it rather than opening home, so the first thing
	// the person meets is the firing itself, drained out of the inbox into the
	// conversation it belongs to and drawn as its own row.
	said2 := firstWords(second.Does.Say, 3)
	if said2 == "" {
		t.Fatalf("the second item says nothing when it fires:\n%s", standingRecordsDump(t, home))
	}
	r2 := start(t, "afe2e_fire_back", home, ws, tuiWide, 45)
	back := r2.waitFor(40*time.Second, said2, say(t, "standSaidTag"))
	t.Logf("the window that came back was told what happened while it was shut:\n%s", back)

	// AND HOME'S OWN ACCOUNT OF IT IS RECORDED RATHER THAN DEMANDED. What happened
	// while nobody was looking is a block at the top of the list, built from the
	// standing items whose last firing is later than the look stamp
	// (internal/tui3/switcher.go's addLedger) — and a ONE-OFF reminder retires the
	// moment it fires, so by the time this window is up there is no live item left
	// for the walk to find. That is a real hole in the block's arithmetic rather
	// than a fact about this test, and it is written down here as a finding
	// because the delivery it is about has already been proved above, twice.
	openHome(t, r2)
	time.Sleep(3 * time.Second)
	news := r2.capture()
	switch {
	case strings.Contains(news, say(t, "switcherSinceLeft")) && strings.Contains(news, say(t, "standingFiredWord")):
		t.Logf("home's `since you left` block names the firing:\n%s", firstMatch(news, say(t, "standingFiredWord")))
	case strings.Contains(news, say(t, "switcherSinceLeft")):
		t.Logf("FINDING: home drew a `since you left` block that does not say the watch fired:\n%s", news)
	default:
		t.Logf("FINDING: a watch fired while no window was open and home drew no `since you left` block at all — "+
			"a one-off item retires as it fires and the ledger walks only what still stands:\n%s", news)
	}
}

// openHome opens the screen with the slash command, which is the door that
// works whatever the machine holds.
func openHome(t *testing.T, r *rig) {
	t.Helper()
	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	r.waitFor(20*time.Second, say(t, "homeFootWord"))
}

// standReminder types one sentence into home's box, asks it there, waits for
// the card and says yes. It is scenario 3's flow reduced to what the scenarios
// after it need from it.
func standReminder(t *testing.T, r *rig, words string) {
	t.Helper()
	r.lit(words)
	time.Sleep(600 * time.Millisecond)
	r.ctrlEnter()
	r.waitFor(modelPatience, say(t, "notifyAskWord"))
	// ctrl+enter HANDS THE KEYBOARD STRAIGHT TO THE PANE, so the digit reaches
	// the card with nothing walked onto — and the foot naming the card's own
	// answers is how this helper knows the pane has it. A reminder's card draws
	// three of them and the foot names three (#189). (Subtest 3 takes the other
	// road on purpose: it closes home first, which puts the keyboard back on the
	// list, and walks onto the row.)
	r.waitFor(20*time.Second, say(t, "exchangeAnswerHint"))
	r.lit("1")
	r.waitFor(30*time.Second, say(t, "homeAskStoodTail"))
	t.Logf("stood: %q\n%s", words, r.capture())
}

// ── 5 ───────────────────────────────────────────────────────────────────────

// testAnswerFromHome stops one window on a consent card and answers it from
// another window's home screen.
func testAnswerFromHome(t *testing.T) {
	home := newHome(t, map[string]any{
		"tools.approvalMode": "prompt",
		// The countdown that denies on silence is off, or the card is gone
		// before the second window has drawn it. Zero is the setting's own
		// "waits forever" (config's settings.go).
		"approval.timeout_seconds": 0,
	})
	ws := newWorkspace(t, "consentws", false)
	a := start(t, "afe2e_a", home, ws, tuiPlain, 40)

	a.lit("run `ls -la` with bash, nothing else")
	a.keys("Enter")
	asked := a.waitFor(modelPatience, say(t, "consentAskWord"))
	t.Logf("window A stopped on a consent card:\n%s", asked)

	b := start(t, "afe2e_b", home, ws, tuiPlain, 40)
	row := b.waitFor(40*time.Second, say(t, "notifyAskWord"))
	t.Logf("window B's home says A is waiting on somebody:\n%s", row)
	for _, chip := range []string{"1 ", say(t, "answersAllowOnce")} {
		if !strings.Contains(row, chip) {
			t.Errorf("home's answer band is missing %q:\n%s", chip, row)
		}
	}
	t.Logf("the chips home offered: %s", firstMatch(row, say(t, "answersAllowOnce")))

	b.lit("1")
	time.Sleep(1500 * time.Millisecond)
	t.Logf("after pressing 1 in window B:\n%s", b.capture())

	// Window A picks the answer up on its own presence heartbeat, a second or two
	// later, and runs the call.
	//
	// THE PROOF IS THE JOURNAL AND NOT THE SCREEN. A gate that was never answered
	// blocks the turn forever ([approval.timeout_seconds] is zero here), so the
	// only thing that can put the command's output in the transcript is the
	// answer this test gave from another window. The screen cannot carry the
	// claim on its own: the model is free to reach for bash a second time, and a
	// window drawing a SECOND question says nothing about the first — which is
	// exactly what one run did, with the first call's output already on the page.
	ran := ""
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) && ran == "" {
		for path, raw := range sessionTranscripts(t, home) {
			if strings.Contains(raw, "README.md") && strings.Contains(raw, "ls -la") {
				ran = path
				break
			}
		}
		if ran == "" {
			time.Sleep(time.Second)
		}
	}
	t.Logf("window A, after the answer came in from home:\n%s", a.capture())
	if ran == "" {
		t.Errorf("no transcript holds the output of the command home allowed:\n%s", a.capture())
	} else {
		t.Logf("the command really ran, and its output is in %s", ran)
	}
}

// ── 6 ───────────────────────────────────────────────────────────────────────

// testHover drives the pointer over home's left column with the SGR motion
// reports the all-motion mode asks for, and reads the card beside it. It runs at
// [tuiWide] because below that width there is no card for a hover to move.
func testHover(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "hoverws", false)
	r := start(t, "afe2e_hover", home, ws, tuiWide, 40)
	screen := r.waitFor(25*time.Second, say(t, "homeFootWord"), "Seed Beta")

	rows := r.lines()
	target := -1
	for i, line := range rows {
		if strings.Contains(line, "Seed Beta") {
			target = i + 1 // capture-pane is 0-based here, the mouse report is 1-based
			break
		}
	}
	if target < 0 {
		t.Fatalf("no `Seed Beta` row to hover:\n%s", screen)
	}
	before := rightPane(screen)
	t.Logf("before hovering, the card is about the cursor's row:\n%s", before)

	r.mouseTo(10, target)
	hovered := r.waitFor(15*time.Second, "Seed Beta")
	pane := rightPane(hovered)
	if !strings.Contains(pane, "Seed Beta") {
		t.Errorf("hovering `Seed Beta` did not move the card onto it:\n%s", hovered)
	} else {
		t.Logf("the pointer previews the row under it:\n%s", hovered)
	}

	// Off the column, into the card's own half: the card goes back to the
	// cursor's row.
	r.mouseTo(tuiWide-4, target)
	time.Sleep(1500 * time.Millisecond)
	off := rightPane(r.capture())
	if strings.Contains(off, "Seed Beta") && !strings.Contains(before, "Seed Beta") {
		t.Errorf("moving the pointer off the column left the card on the hovered row:\n%s", r.capture())
	}
	t.Logf("pointer off the column, the card is the cursor's again:\n%s", r.capture())
}

// ── 7 ───────────────────────────────────────────────────────────────────────

// testFold seeds more conversations than the list draws and works the one fold
// at its foot with the two arrows.
//
// THIS SUBTEST REPLACES `m_toggles_the_folds_on_a_card`, which drove a card band
// and a key that both went with the home rethink. The `news` band — `◆ 4 things
// since you left` with its own `▸ …N more` door — is not on the card any more
// (internal/tui3/place_home.go lists it among the eleven bands that became
// ledger lines), and `m` belongs to the box now: typing it at home filters the
// list. What survived, and what a person actually meets, is ONE fold at the foot
// of the list, opened with `→` and shut with `←` (home.go binds both, and the
// hint under the box says which way it goes).
func testFold(t *testing.T) {
	home := newHome(t, nil)
	// More than the eight rows the list draws, so there is something to fold.
	for i, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "foldws", false)
	r := start(t, "afe2e_fold", home, ws, tuiWide, 45)

	screen := r.waitFor(25*time.Second, say(t, "homeFootWord"), say(t, "foldMoreWord"))
	t.Logf("the list with a fold at its foot:\n%s", screen)
	if !strings.Contains(screen, say(t, "switcherQuietSince")) {
		t.Errorf("the fold does not say when the rows under it went quiet:\n%s",
			firstMatch(screen, say(t, "foldMoreWord")))
	}
	// THE OLDEST SEED IS BEHIND THE FOLD. The list is ranked and the twelfth
	// project is the quietest, so it is the one the cap drops.
	if strings.Contains(screen, "Seed L") {
		t.Errorf("a row the fold stands over is drawn before the fold was opened:\n%s", screen)
	}

	if !walkTo(r, say(t, "homeFoldOpenHint"), "Down") {
		t.Fatalf("could not put the cursor on the fold:\n%s", r.capture())
	}
	r.keys("Right")
	opened := r.waitFor(15*time.Second, "Seed L")
	t.Logf("`→` opened the fold:\n%s", opened)

	// AND THE FOLD IS STILL THERE, because it is the way back: an opened fold
	// still says how many rows it is the door over.
	if !strings.Contains(opened, say(t, "foldMoreWord")) {
		t.Errorf("the fold vanished when it was opened, so there is no way back:\n%s", opened)
	}
	if !walkTo(r, say(t, "homeFoldShutHint"), "Down") {
		t.Fatalf("could not put the cursor back on the opened fold:\n%s", r.capture())
	}
	r.keys("Left")
	time.Sleep(1500 * time.Millisecond)
	closed := r.capture()
	if strings.Contains(closed, "Seed L") {
		t.Errorf("`←` did not shut the fold again:\n%s", closed)
	}
	t.Logf("`←` shut it again:\n%s", closed)
}

// ── 8 ───────────────────────────────────────────────────────────────────────

// testNarrow is the phone-shaped window: sixty cells, where home has no room
// for two columns and the exchange takes the whole screen.
func testNarrow(t *testing.T) {
	home := newHome(t, nil)
	seedProject(t, home, "narrowseed", 9, 20*time.Minute)
	ws := newWorkspace(t, "narrowws", false)
	r := start(t, "afe2e_narrow", home, ws, 60, 30)

	screen := r.waitFor(25*time.Second, "home")
	t.Logf("home at 60x30:\n%s", screen)

	r.lit("what day is it")
	time.Sleep(600 * time.Millisecond)
	typed := r.capture()
	t.Logf("typing at 60 cells:\n%s", typed)
	r.ctrlEnter()

	stacked := r.waitFor(30*time.Second, "what day is it")
	t.Logf("the exchange at 60 cells:\n%s", stacked)
	if strings.Contains(stacked, "narrowseed") || strings.Contains(stacked, "Seed Narrowseed") {
		t.Errorf("the narrow exchange did not take the whole screen — the list is still drawn:\n%s", stacked)
	}
	if caught, ok := r.glimpse(20*time.Second,
		say(t, "homeAskThinkWord"), say(t, "homeAskWriteWord"), say(t, "homeAskRunWord")); ok {
		t.Logf("the live strip on a narrow window:\n%s", caught)
	} else {
		t.Logf("FINDING: no live strip caught on the narrow window")
	}

	hit, replied := r.waitForAny(modelPatience, "2026", "today", "Today")
	t.Logf("the reply arrived on the narrow window (%q):\n%s", hit, replied)

	r.keys("Escape")
	back := r.waitFor(20*time.Second, "? what day is it")
	t.Logf("esc brought the list back with the exchange row on it:\n%s", back)

	r.keys("Enter")
	reopened := r.waitFor(20*time.Second, "› what day is it")
	t.Logf("enter reopened the exchange:\n%s", reopened)
}

// ── 9 ───────────────────────────────────────────────────────────────────────

// testGrouped turns the flat list into one block per project and reads the
// blocks back.
//
// THIS SUBTEST REPLACES `the_project_card`. That one put the pointer on a folded
// `▸ delta` line and read the card a whole project got. Both went with the home
// rethink: the project is a TAG ON THE ROW now, nothing on the desktop builds a
// project line, and no cursor stop carries a project's card. What the surface
// kept is the other half of the same idea — `alt+g` groups the list by project,
// and each block wears its project's name as its heading (internal/tui3's
// switcher.go, addGrouped). So the test asks the surviving question: can a person
// still see this machine's work arranged by project.
func testGrouped(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "groupws", false)
	r := start(t, "afe2e_group", home, ws, tuiWide, 40)
	flat := r.waitFor(25*time.Second, say(t, "homeFootWord"), "Seed Beta")
	t.Logf("the flat list:\n%s", flat)

	// The key is named on the list's own section line while anything is asking
	// or moving; with nothing to triage the line is not drawn, so the key is
	// pressed rather than read. alt+g arrives as ESC g, which is what every
	// terminal sends for it.
	r.lit("\x1bg")
	grouped := r.waitFor(15*time.Second, "alpha")
	t.Logf("after alt+g, the list is grouped by project:\n%s", grouped)

	for _, name := range []string{"alpha", "beta", "gamma", "groupws"} {
		if !headingRow(grouped, name) {
			t.Errorf("no heading of its own for the project %q:\n%s", name, grouped)
		}
	}
	// AND THE ROWS ARE STILL THE CONVERSATIONS, under the heading that owns
	// them: the grouping is a view of one list and never a second reading.
	for _, want := range []string{"Seed Alpha", "Seed Beta", "Seed Gamma"} {
		if !strings.Contains(grouped, want) {
			t.Errorf("grouping lost the conversation %q:\n%s", want, grouped)
		}
	}

	// And the same key puts it back.
	r.lit("\x1bg")
	time.Sleep(1500 * time.Millisecond)
	back := r.capture()
	if headingRow(back, "alpha") {
		t.Errorf("alt+g a second time did not put the flat list back:\n%s", back)
	}
	t.Logf("alt+g again, back to the flat list:\n%s", back)
}

// headingRow answers whether a project's name stands ALONE on a line of the
// LIST, which is what a grouping heading is — the conversations under it carry
// their own state mark and their own tail, so a name with anything beside it is
// a row and not a heading.
//
// ONLY THE LIST HALF IS READ, because the card shares these screen rows and a
// heading with the card's title beside it is still a heading.
func headingRow(screen, name string) bool {
	for _, line := range strings.Split(screen, "\n") {
		runes := []rune(line)
		if len(runes) > tuiCardAt {
			runes = runes[:tuiCardAt]
		}
		if strings.TrimSpace(string(runes)) == name {
			return true
		}
	}
	return false
}

// dirtyFiles is what `git status --porcelain` says about the workspace, which
// is the reading the card's place line is derived from (homeband_repo.go runs
// the same command in porcelain v2).
func dirtyFiles(t *testing.T, ws string) []string {
	t.Helper()
	command := exec.Command("git", "-C", ws, "status", "--porcelain")
	out, err := command.Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		// The porcelain line is two status letters, a space, and the path. The
		// leading letter is often a space itself, so the split is on fields
		// from the right rather than on a fixed offset.
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[len(fields)-1])
		}
	}
	return names
}

// plural is the spelling rule this suite quotes back at the product.
func plural(unit string, n int) string {
	if n == 1 {
		return unit
	}
	return unit + "s"
}

// ── reading the screen ──────────────────────────────────────────────────────

// rightPane is everything from the gutter rightwards at [tuiWide], which is
// where home draws the card. See [tuiCardAt] for where that column comes from.
func rightPane(screen string) string {
	var b strings.Builder
	for _, line := range strings.Split(screen, "\n") {
		runes := []rune(line)
		if len(runes) <= tuiCardAt {
			b.WriteString("\n")
			continue
		}
		b.WriteString(strings.TrimRight(string(runes[tuiCardAt:]), " "))
		b.WriteString("\n")
	}
	return b.String()
}

// firstMatch is the first line holding a substring, for a log line that quotes
// the row rather than the whole screen.
func firstMatch(screen, sub string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, sub) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// toolRows is every `tool · something` row the pane drew, deduplicated, so a
// report can say what the model actually reached for.
func toolRows(screens []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, screen := range screens {
		for _, line := range strings.Split(screen, "\n") {
			// A running row leads with one of the braille spinner's eight
			// frames, so the leading glyph is stripped before the tool name is
			// read rather than every frame being spelled out here.
			trimmed := strings.TrimLeft(strings.TrimSpace(line), "⠁⠂⠄⠈⠐⠠⠆⠇⠋⠙⠸⠼⠴⠦⠧⠹⢀⡀✗ ")
			for _, tool := range []string{"bash ", "read ", "stand ", "ls ", "write ", "manual ", "watch "} {
				if strings.HasPrefix(trimmed, tool) && !seen[trimmed] {
					seen[trimmed] = true
					out = append(out, trimmed)
				}
			}
		}
	}
	if len(out) == 0 {
		return []string{"(none seen)"}
	}
	if len(out) > 12 {
		out = append(out[:12], fmt.Sprintf("… and %d more", len(out)-12))
	}
	return out
}

// ── 10 ──────────────────────────────────────────────────────────────────────

// testOneSpendFigure is issue #269 end to end: run a turn, press escape in the
// middle of it, and read the money back on both spend surfaces. They must say
// the same thing.
//
// WHY AN INTERRUPTED TURN AND NOT AN ORDINARY ONE. Money is banked from the
// provider's own usage block as each call is decoded, and the machine's ledger
// used to be written only when a turn SEALED — so a turn nobody let finish was
// money the status line had and the file never got. On the measured chat that
// was $0.087 missing from `/spend` and from Settings→Spending, with the status
// line reading the truth beside them. Escape is the cheapest way to make a real
// binary produce exactly that state.
//
// WHAT IS ASSERTED IS A STRING AND NOT A NUMBER. Two surfaces agreeing to
// within a cent is two surfaces disagreeing; the point of the fix is that they
// are one reading of one total, so they are compared as the characters a person
// reads off the screen.
func testOneSpendFigure(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "spendws", false)
	r := start(t, "afe2e_spend", home, ws, tuiPlain, 40)

	// A machine with no conversations on it opens straight into one, which is
	// the state this scenario wants: one conversation, so the machine's day and
	// this conversation's total are the same money read two ways. The wait is
	// on the WORKSPACE's own name in the status line rather than on a product
	// sentence, because the welcome screen a fresh machine opens on draws none
	// of the rules the other subtests wait for.
	r.waitFor(20*time.Second, "spendws")

	// A question with a long answer, so there is a middle to interrupt.
	r.lit("count slowly from one to two hundred, one number per line")
	time.Sleep(600 * time.Millisecond)
	r.keys("Enter")
	if caught, ok := r.glimpse(modelPatience,
		say(t, "homeAskThinkWord"), say(t, "homeAskWriteWord")); ok {
		t.Logf("the turn was in flight when escape was pressed:\n%s", caught)
	} else {
		t.Logf("FINDING: the live strip was never caught — the turn may have finished first")
	}
	r.keys("Escape")
	// The ledger's writer is a background goroutine and both places read the
	// file on a three-second beat, so the reading is taken after one beat has
	// certainly turned rather than in the same instant as the keystroke.
	time.Sleep(5 * time.Second)

	// ── the /spend place ──────────────────────────────────────────────────
	//
	// `alt+5` and not `/spend`: the place's doors are the chord, `tab`, and the
	// word typed at home — `/spend` is an alias of `/cost`, which is this
	// conversation's own note rather than the machine's page. The chord arrives
	// as esc-then-5, which is what internal/tui3's placeDigit reads.
	r.lit("\x1b5")
	place := r.waitFor(25*time.Second, say(t, "spendRailsHint"))
	t.Logf("the spend place after the interrupted turn:\n%s", place)
	fromPlace := moneyOn(t, place, say(t, "spendRailsHint"))
	if fromPlace == "" {
		t.Fatalf("the spend place's pointer line carries no figure:\n%s", place)
	}
	t.Logf("the /spend place says %s", fromPlace)

	// ── Settings → Spending ───────────────────────────────────────────────
	r.keys("Escape")
	time.Sleep(600 * time.Millisecond)
	r.lit("/budget")
	time.Sleep(600 * time.Millisecond)
	r.keys("Enter")
	tab := r.waitFor(25*time.Second, say(t, "spendConversationRow"))
	t.Logf("the Spending tab after the interrupted turn:\n%s", tab)

	fromToday := moneyOn(t, tab, say(t, "spendTodayResets"))
	if fromToday == "" {
		t.Fatalf("the Spending tab's `today` row carries no figure:\n%s", tab)
	}
	fromThisOne := moneyAfter(t, tab, say(t, "spendThisOneWord"))
	if fromThisOne == "" {
		t.Fatalf("the Spending tab's `this one` receipt carries no figure:\n%s", tab)
	}
	t.Logf("Settings→Spending says today %s and this one %s", fromToday, fromThisOne)

	if fromPlace != fromToday {
		t.Errorf("the /spend place says %s and Settings→Spending's `today` says %s — "+
			"one machine, one day, two numbers (issue #269)", fromPlace, fromToday)
	}
	if fromThisOne != fromToday {
		t.Errorf("Settings→Spending says today %s and this one %s on a machine holding "+
			"exactly one conversation", fromToday, fromThisOne)
	}
}

// moneyOn is the FIRST dollar figure on the screen line carrying `needle`, and
// the empty string when there is no such line or no figure on it. Both rows it
// is used on lead with what was spent and follow it with the rail — `today
// $0.0003 of $500` — so the first figure is the spend on each.
//
// IT READS THE LINE THE PERSON READS. The whole claim under test is that these
// places render one STRING, so the figure is lifted out of the drawn row rather
// than recomputed from anything.
func moneyOn(t *testing.T, screen, needle string) string {
	t.Helper()
	return moneyIn(t, screen, needle, false)
}

// moneyAfter is the figure that FOLLOWS `needle` on its line, for the receipt
// whose row may carry a limit ahead of it — `per conversation  $20 · this one
// $0.0003`, where the first figure on the line is the rail and not the spend.
func moneyAfter(t *testing.T, screen, needle string) string {
	t.Helper()
	return moneyIn(t, screen, needle, true)
}

func moneyIn(t *testing.T, screen, needle string, after bool) string {
	t.Helper()
	for _, line := range strings.Split(screen, "\n") {
		found := strings.Index(line, needle)
		if found < 0 {
			continue
		}
		rest := line
		if after {
			rest = line[found+len(needle):]
		}
		at := strings.Index(rest, "$")
		if at < 0 {
			continue
		}
		figure := "$"
		for _, r := range rest[at+1:] {
			if (r < '0' || r > '9') && r != '.' {
				break
			}
			figure += string(r)
		}
		if len(figure) > 1 {
			return figure
		}
	}
	return ""
}

// ── 12 ──────────────────────────────────────────────────────────────────────

// testInheritedWorkSeat is #312's acceptance on the real screen: a crew older
// than the work seat, met where a person actually meets it.
//
// THE PROFILE IS THE DEFECT. A crew applied before the worker class existed
// (#278) holds four `models.tiers.*` rows and no `worker` among them. Headless
// doors learned to read that shape in #311; the conversation did not, so every
// task started from a thread ran on the build's own worker model and nothing
// anywhere said which model that was or why. This subtest builds exactly that
// profile — four real rows, the fifth key deleted — starts one small task, and
// reads back two things a unit test cannot: that the LINE is on the screen once,
// and that the model the node actually called is the row the person pinned.
//
// THE MODEL IS READ OUT OF THE CALL LOG, which is always on and writes one line
// per model call with the tag the caller set (internal/calllog, and session's
// loop.go tags a node's calls `task`). That is the node's own journal, and it is
// the only evidence in this suite that comes off the wire rather than off the
// screen.
//
// IT IS DELIBERATELY THE CHEAPEST SHAPE THERE IS: `/task solo`, which runs one
// worker and makes no sizing call before it, on a brief that is one file.
func testInheritedWorkSeat(t *testing.T) {
	// The row the work must land on. It is a DIFFERENT id from the model the
	// conversation talks on (newHome pins that) and from this build's own worker
	// default, because the whole question is which of the three answered.
	const smallWork = "deepseek/deepseek-v4-flash-0731"
	home := newHome(t, map[string]any{
		"models.tiers.reflex":     "mistralai/mistral-nemo",
		"models.tiers.low":        smallWork,
		"models.tiers.high":       smallWork,
		"models.tiers.mastermind": smallWork,
	})
	dropWorkerRow(t, home)
	ws := newWorkspace(t, "seatws", false)
	r := start(t, "afe2e_seat", home, ws, tuiPlain, 40)

	// Whichever door the launch took — home on a machine with several
	// conversations, and straight into a greeted conversation on a fresh one,
	// which is what a state root built one minute ago always is.
	r.waitForAny(20*time.Second, say(t, "homeFootWord"), say(t, "starterTaskWord"))
	r.keys("Escape")
	r.lit("/task solo write a file called hello.txt containing the word hello")
	r.keys("Enter")

	// THE LINE, WHEN THE WORK STARTS. Both halves of it: the observation about
	// the profile and the promise about what ends it.
	screen := r.waitFor(4*time.Minute, say(t, "inheritedSeatObservation"), say(t, "inheritedSeatPromise"))
	t.Logf("the conversation says which row filled its work seat:\n%s", screen)

	// AND ONCE. A node divides into parts and each part starts; a line that
	// arrived with each of them is the noise this mechanism refused headless.
	if got := strings.Count(screen, say(t, "inheritedSeatObservation")); got != 1 {
		t.Errorf("the line is on the screen %d times, want once:\n%s", got, screen)
	}

	// AND THE WORK IS ON THE ROW THE PERSON PINNED. The node's calls carry the
	// `task` tag, and no call anywhere may have gone to the build's own worker.
	// The log is POLLED rather than read once: the receipt is written when the
	// node starts and the node's first call goes out a moment later, and this
	// subtest deliberately stops as soon as there is something to read rather
	// than paying for the whole piece of work.
	models := waitForTaskCalls(t, home, 3*time.Minute)
	if len(models) == 0 {
		t.Fatalf("no call in the log was tagged as a task's; the log held %v", callModels(t, home))
	}
	t.Logf("the node called: %v", models)
	for _, model := range models {
		if model != smallWork {
			t.Errorf("a task call went to %q, want the small-work row %q the crew pinned", model, smallWork)
		}
	}
	for _, model := range callModels(t, home) {
		if model == "z-ai/glm-5.3-flash" {
			t.Errorf("a call went to this build's own worker model, which is the substitution the issue is about")
		}
	}
	r.quit()
}

// dropWorkerRow deletes `models.tiers.worker` from a rig's config, so the
// profile is the shape a crew set before that class existed actually has.
//
// It is a DELETE and not an empty string: the two are different answers
// everywhere in this build — a row emptied on purpose means "follow the
// conversation" — and it is the one this suite must write, because [newHome]
// copies the person's own config and theirs may hold the key.
func dropWorkerRow(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	rows := map[string]any{}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("config: %v", err)
	}
	delete(rows, "models.tiers.worker")
	out, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
}

// waitForTaskCalls polls the call log until a node's own call is in it, and
// answers every model those calls asked for.
func waitForTaskCalls(t *testing.T, home string, within time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if models := taskCallModels(t, home); len(models) > 0 {
			return models
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(pollEvery)
	}
}

// callModels is every model this run asked for, and taskCallModels is the
// subset a task node asked for — read out of the always-on call log
// (internal/calllog), which is one JSON object per line under the state root.
func callModels(t *testing.T, home string) []string     { return callLogModels(t, home, "") }
func taskCallModels(t *testing.T, home string) []string { return callLogModels(t, home, "task") }

func callLogModels(t *testing.T, home, tag string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
	if err != nil {
		// A log that is not there yet is a run that has made no call yet, which
		// is a state the poll above is entitled to see once.
		return nil
	}
	var models []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record struct {
			Tag   string `json:"tag"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record.Model == "" || (tag != "" && record.Tag != tag) {
			continue
		}
		models = append(models, record.Model)
	}
	return models
}
