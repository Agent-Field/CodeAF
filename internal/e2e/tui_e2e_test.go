//go:build e2e

package e2e

// TestTUIE2E is the ambient side of v3, driven end to end: the real binary, a
// real terminal, a real model, and the screen read back with capture-pane.
//
// Every subtest builds its own AFORGE_HOME and its own repository, and every
// assertion below is a string the product actually draws — quoted from the
// manual pages and from the band files that write them, never invented here.

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// modelPatience is how long any one real turn is given. deepseek-v4-flash
// answers a one-line question in seconds; a reminder that has to reach for the
// `stand` tool takes longer, and a machine under load takes longer again.
const modelPatience = 90 * time.Second

func TestTUIE2E(t *testing.T) {
	requireTmuxAndKey(t)

	t.Run("home_opens_on_launch_with_the_two_tier_shape", testHomeShape)
	t.Run("a_real_conversation_and_its_home_card", testRealConversation)
	t.Run("ask_here_end_to_end", testAskHere)
	t.Run("the_firing_reaches_the_person", testFiringReachesThePerson)
	t.Run("answer_from_home_across_two_windows", testAnswerFromHome)
	t.Run("hover_previews_the_row_under_the_pointer", testHover)
	t.Run("m_toggles_the_folds_on_a_card", testFolds)
	t.Run("narrow_window_ask_here", testNarrow)
	t.Run("the_project_card", testProjectCard)
}

// ── 1 ───────────────────────────────────────────────────────────────────────

// testHomeShape opens the product with five projects on the machine and reads
// the shape home.md describes: three drawn open, a dim `elsewhere` rule, the
// rest folded to one `▸ name` line each, and one blank padding row above the
// foot.
func testHomeShape(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "shapews", false)
	r := start(t, "afe2e_shape", home, ws, 120, 40)

	screen := r.waitFor(20*time.Second, "home", "esc close")
	t.Logf("home greeted on launch:\n%s", screen)

	for _, want := range []string{
		"─ elsewhere ",
		"▸ delta",
		"▸ epsilon",
		"Seed Alpha",
		"Seed Beta",
		"Seed Gamma",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("home is missing %q", want)
		}
	}
	// The two folded projects must sit UNDER the rule, which is what makes the
	// block a second tier rather than a list with a caption in it.
	if rule, delta := strings.Index(screen, "─ elsewhere "), strings.Index(screen, "▸ delta"); rule < 0 || delta < rule {
		t.Errorf("the folded projects are not under the elsewhere rule (rule at %d, ▸ delta at %d)", rule, delta)
	}

	// THE LIST NEVER TOUCHES THE RULE ABOVE THE BOX (home.go says so, and its
	// own unit test pins it at every height): the row above the foot's rule is
	// blank whatever the list did.
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
	t.Logf("padding row above the foot rule (row %d) is blank; foot rule row %d = %q",
		foot-1, foot, strings.TrimSpace(lines[foot]))

	// esc closes home into the conversation the launch loaded, and /home opens
	// it again — the two doors home.md names.
	r.keys("Escape")
	closed := r.waitFor(15*time.Second, "space space home · / commands")
	t.Logf("esc closed home into the conversation the launch loaded:\n%s", closed)
	if strings.Contains(closed, "─ elsewhere ") {
		t.Errorf("esc did not close home:\n%s", closed)
	}
	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	back := r.waitFor(15*time.Second, "home", "esc close", "─ elsewhere ")
	t.Logf("/home reopened it:\n%s", back)

	// And two spaces on an empty box is the other door.
	r.keys("Escape")
	time.Sleep(1200 * time.Millisecond)
	r.keys("Space")
	r.keys("Space")
	gesture := r.waitFor(15*time.Second, "home", "esc close")
	t.Logf("space space opened home:\n%s", gesture)
}

// ── 2 ───────────────────────────────────────────────────────────────────────

// testRealConversation asks the model one question, then reads the row that
// conversation grew on home: the left-off band, the repository band, the keys
// band, and the dim arithmetic under them.
func testRealConversation(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "repows", true)
	r := start(t, "afe2e_talk", home, ws, 120, 40)

	r.lit("what is 2+2, one word")
	r.keys("Enter")
	hit, screen := r.waitForAny(modelPatience, "\n4", " 4\n", "four", "Four")
	t.Logf("the model answered (%q):\n%s", hit, screen)

	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	card := r.waitFor(20*time.Second, "home", "esc close", "› what is 2+2")
	t.Logf("home, with the conversation's card up:\n%s", card)

	// The left-off band: the person's last message as one muted `› ` line.
	if !strings.Contains(card, "› what is 2+2") {
		t.Errorf("no left-off band on the card")
	}
	// The keys band, spelled as home.md quotes it.
	if !strings.Contains(card, "enter open · n new chat here") {
		t.Errorf("no keys band on the card")
	}
	// The repository band. It must agree with the repository, so the count is
	// taken from git at the moment of the assertion rather than assumed.
	dirty := dirtyFiles(t, ws)
	want := fmt.Sprintf("main · %d %s dirty", len(dirty), plural("file", len(dirty)))
	if !strings.Contains(card, want) {
		t.Errorf("the repo band does not read %q — it reads %q (git says %v)",
			want, firstMatch(card, "main"), dirty)
	} else {
		t.Logf("the repo band agrees with git: %q (%v)", want, dirty)
	}
	// AND WHAT MADE IT DIRTY. The test changed exactly one tracked file; a
	// second entry is something the product itself dropped in the person's
	// working directory, which is worth naming rather than absorbing.
	for _, name := range dirty {
		if !strings.HasSuffix(name, "README.md") {
			t.Logf("FINDING: the run left %q in the person's workspace and the repo band counts it", name)
		}
	}
	// The dim arithmetic. `last active` is always true of a conversation
	// somebody just spoke in; `spent` is drawn from the TASK rollup only
	// (home.go's homeFacts reads row.Tasks.Spend), so a conversation that has
	// run no task shows nothing for it. That is recorded, not asserted.
	if !strings.Contains(card, "last active") {
		t.Errorf("no `last active` clause under the card")
	}
	if strings.Contains(card, "spent $") {
		t.Logf("the spend band drew a `spent $…` clause")
	} else {
		t.Logf("FINDING: no `spent $…` clause on a conversation that really spent money — "+
			"homeFacts reads row.Tasks.Spend (task rollup) and this conversation ran no task. "+
			"The card reads: %s", firstMatch(card, "last active"))
	}
}

// ── 3 ───────────────────────────────────────────────────────────────────────

// testAskHere is the whole `ask here` flow against the real model: the row, the
// spinner, the card, the answer, the item it leaves standing, and the row still
// being there after the screen it was asked on has been closed and reopened.
func testAskHere(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "askws", false)
	r := start(t, "afe2e_ask", home, ws, 120, 40)

	// One ordinary conversation first, so this project has a block on home for
	// the exchange row to sit above.
	r.lit("say ok and nothing else")
	r.keys("Enter")
	r.waitForAny(modelPatience, "ok", "OK", "Ok")

	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	r.waitFor(20*time.Second, "home", "esc close")

	r.lit("remind me in 1 minute to drink water")
	time.Sleep(700 * time.Millisecond)
	typed := r.capture()
	if !strings.Contains(typed, "? ask here:") || !strings.Contains(typed, "+ start a new conversation:") {
		t.Errorf("the two action rows are not both drawn while something is typed:\n%s", typed)
	}
	t.Logf("the action rows while typing:\n%s", typed)

	// ctrl+enter, sent as the CSI 13;5u a kitty-protocol terminal sends.
	r.ctrlEnter()

	working := r.waitFor(20*time.Second, "? remind me in 1 minute", "working")
	t.Logf("the exchange row is working:\n%s", working)

	// The pane's own clock, caught in flight. It lives for seconds, so this is
	// a fast poll and it is a finding rather than a failure when it is missed.
	if caught, ok := r.glimpse(25*time.Second, "thinking ·", "writing ·", "running ·"); ok {
		t.Logf("the live strip was caught mid-turn:\n%s", caught)
	} else {
		t.Logf("FINDING: never caught `thinking ·`/`writing ·`/`running ·` in the pane")
	}

	// Everything the pane draws while the errand runs, kept so the tool rows
	// can be read afterwards.
	seen := []string{}
	deadline := time.Now().Add(modelPatience)
	waiting := ""
	for time.Now().Before(deadline) {
		screen := r.capture()
		seen = append(seen, screen)
		if strings.Contains(screen, "waiting on you") {
			waiting = screen
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if waiting == "" {
		t.Fatalf("the card never arrived. last screen:\n%s", r.capture())
	}
	// The card is drawn over several frames; give it one before reading the
	// chips off it, or this reads a half-painted row.
	time.Sleep(2 * time.Second)
	waiting = r.capture()
	seen = append(seen, waiting)
	t.Logf("the card arrived and the row tail reads `▲ waiting on you`:\n%s", waiting)
	for _, chip := range []string{"1 yes, set it up", "2 change when"} {
		if !strings.Contains(waiting, chip) {
			t.Errorf("the card is missing the chip %q", chip)
		}
	}
	// THE CARD DOES NOT ALWAYS HAVE THREE. A one-off reminder offers no
	// `once, not standing` — "do it once, now" says the wrong thing at the
	// wrong moment — so the third chip is recorded, never demanded
	// (standing.go's standAnswerWords holds the rule).
	if strings.Contains(waiting, "3 once, not standing") {
		t.Logf("the card offered all three answers")
	} else {
		t.Logf("the card offered two answers — the engine named no `once` option for a one-off reminder: %s",
			firstMatch(waiting, "1 yes, set it up"))
	}

	// WHAT THE MODEL ACTUALLY DID. No tool row may say `unknown`
	// (asking-from-home.md states it), and the instructions now forbid running
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
	again := r.waitFor(20*time.Second, "home", "esc close")
	if !strings.Contains(again, "? remind me in 1 minute") || !strings.Contains(again, "waiting on you") {
		t.Errorf("the exchange did not outlive the screen it was asked on:\n%s", again)
	}
	t.Logf("home reopened and the exchange is still waiting on somebody:\n%s", again)

	// Walk onto the row and hand the keyboard to the pane, which is what the
	// hint line under the box offers.
	if !walkTo(r, "? remind me in 1 minute") {
		t.Fatalf("could not put the cursor back on the exchange row:\n%s", r.capture())
	}
	r.keys("Enter")
	time.Sleep(1200 * time.Millisecond)

	r.lit("1")
	stood := r.waitFor(30*time.Second, "∙ stood")
	time.Sleep(1500 * time.Millisecond)
	stood = r.capture()
	t.Logf("answered `1` — the row reads `∙ stood`:\n%s", stood)
	if !strings.Contains(stood, "◦ remind me in 1 minute") {
		t.Errorf("no `◦ …` item row appeared under the project:\n%s", stood)
	}
	if !strings.Contains(stood, "yes, set it up · set up") {
		t.Errorf("the settled card does not carry `yes, set it up · set up`:\n%s", stood)
	}
	if !strings.Contains(stood, "kept · this exchange is filed under it") {
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

// walkTo steps the cursor up the list until it is standing on a row holding
// the given words, and answers whether it got there.
func walkTo(r *rig, want string) bool {
	for i := 0; i < 8; i++ {
		if cursorRow(r.capture()) != "" && strings.Contains(cursorRow(r.capture()), want) {
			return true
		}
		r.keys("Up")
		time.Sleep(400 * time.Millisecond)
	}
	return strings.Contains(cursorRow(r.capture()), want)
}

// cursorRow is the line the cursor is standing on — home marks it with `›` in
// the first column.
func cursorRow(screen string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.HasPrefix(line, "›") {
			return line
		}
	}
	return ""
}

// ── 4 ───────────────────────────────────────────────────────────────────────

// testFiringReachesThePerson stands a one-minute reminder, sits in an ordinary
// conversation of the same project, and waits for the window's own pass to
// fire it. Then it quits, fires a second one from outside every window with
// `aforge tick`, and reopens to read what was left waiting.
func testFiringReachesThePerson(t *testing.T) {
	if testing.Short() {
		t.Skip("this one waits for the five-minute standing pass")
	}
	home := newHome(t, nil)
	ws := newWorkspace(t, "firews", false)
	r := start(t, "afe2e_fire", home, ws, 120, 40)
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
	if !strings.Contains(r.capture(), "keeping an eye on") {
		t.Errorf("the status line has no `◦ keeping an eye on N` segment while an item stands:\n%s", r.capture())
	}
	r.lit("/status")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	status := r.waitFor(20*time.Second, "keeping watch")
	t.Logf("/status while something stands:\n%s", status)
	if !strings.Contains(status, "keeping an eye on") && !strings.Contains(status, "keeping watch") {
		t.Errorf("/status says nothing about what is being kept an eye on")
	}

	// Now wait for the pass. It is one interval from launch plus the minute the
	// reminder asked for, with room for a slow machine.
	wait := 6*time.Minute + 30*time.Second - time.Since(started)
	if wait < time.Minute {
		wait = time.Minute
	}
	t.Logf("waiting %s for the window's own standing pass", wait.Round(time.Second))
	// TWO NEEDLES, BECAUSE THE JOURNAL AND THE SCREEN SAY IT DIFFERENTLY. The
	// steering line the engine injects carries the person's whole sentence; the
	// ROW the surface draws wears the head's six-word cut of it and then what the
	// firing said (internal/tui3's standName, pinned by
	// TestAStandingUpdateIsExactlyOneLine). So the transcript is searched for the
	// sentence and the screen for the row.
	line := "◦ remind me in 1 minute to drink water"
	drawnRow, said := "◦ remind me in 1 minute to", "said:"
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
		if strings.Contains(raw, `\u25e6 remind me in 1 minute to drink water`) ||
			strings.Contains(raw, line) {
			delivered = true
			t.Logf("the firing reached the conversation's journal: %s", path)
		}
	}
	if !delivered {
		t.Fatalf("the firing never reached the person at all — nothing in any transcript. screen:\n%s", screen)
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

	inbox := projectInbox(t, home, ws)
	if !strings.Contains(inbox, "stretch") {
		t.Errorf("nothing was left waiting for the person after the tick. project inbox:\n%s", inbox)
	} else {
		t.Logf("the firing was filed under the project for the next window:\n%s", inbox)
	}

	// Reopen from ANOTHER project, with three newer projects seeded, so the one
	// that has news is a folded `▸ name` line under the elsewhere rule — the
	// only cursor stop that draws a PROJECT card, which is where the news band
	// for a project is drawn.
	for i, name := range []string{"one", "two", "three"} {
		seedProject(t, home, name, 40+i, time.Duration(i+1)*time.Minute)
	}
	other := newWorkspace(t, "otherws", false)
	r2 := start(t, "afe2e_fire_back", home, other, 120, 40)
	r2.waitFor(25*time.Second, "home", "esc close", "─ elsewhere ")
	folded := r2.capture()
	t.Logf("home after the firing, from another project:\n%s", folded)
	row := foldedRow(folded, "firews")
	if row < 0 {
		t.Fatalf("the project that has news is not a folded row under elsewhere:\n%s", folded)
	}
	r2.mouseTo(10, row)
	news := r2.waitFor(15*time.Second, "since you left")
	t.Logf("the project card with news on it:\n%s", news)
	if !strings.Contains(news, "◆ 1 thing since you left") {
		t.Errorf("the news band does not read `◆ 1 thing since you left`:\n%s", firstMatch(news, "since you left"))
	}
	if !strings.Contains(news, "stretch") {
		t.Errorf("the news band does not say what the firing said:\n%s", news)
	}
}

// openHome opens the screen with the slash command, which is the door that
// works whatever the machine holds.
func openHome(t *testing.T, r *rig) {
	t.Helper()
	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	r.waitFor(20*time.Second, "home", "esc close")
}

// standReminder types one sentence into home's box, asks it there, waits for
// the card and says yes. It is scenario 3's flow reduced to what the scenarios
// after it need from it.
func standReminder(t *testing.T, r *rig, words string) {
	t.Helper()
	r.lit(words)
	time.Sleep(600 * time.Millisecond)
	r.ctrlEnter()
	r.waitFor(modelPatience, "waiting on you")
	r.lit("1")
	r.waitFor(30*time.Second, "∙ stood")
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
	a := start(t, "afe2e_a", home, ws, 120, 40)

	a.lit("run `ls -la` with bash, nothing else")
	a.keys("Enter")
	asked := a.waitFor(modelPatience, "allow?")
	t.Logf("window A stopped on a consent card:\n%s", asked)

	b := start(t, "afe2e_b", home, ws, 120, 40)
	row := b.waitFor(40*time.Second, "waiting on you")
	t.Logf("window B's home says A is waiting on somebody:\n%s", row)
	for _, chip := range []string{"1 ", "allow once"} {
		if !strings.Contains(row, chip) {
			t.Errorf("home's answer band is missing %q:\n%s", chip, row)
		}
	}
	t.Logf("the chips home offered: %s", firstMatch(row, "allow once"))

	b.lit("1")
	time.Sleep(1500 * time.Millisecond)
	t.Logf("after pressing 1 in window B:\n%s", b.capture())

	// Window A picks the answer up on its own presence heartbeat, a second or
	// two later, and runs the call. The tool row collapses into the turn's
	// `▸ worked … 1 tool call` fold, so that is what the screen shows; the
	// output itself is read back out of the journal.
	done := a.waitFor(20*time.Second, "1 tool call")
	t.Logf("window A ran the command:\n%s", done)
	if strings.Contains(done, "allow?") {
		t.Errorf("window A is still asking after being answered from home:\n%s", done)
	}
	ran := false
	for path, raw := range sessionTranscripts(t, home) {
		if strings.Contains(raw, "README.md") && strings.Contains(raw, "ls -la") {
			ran = true
			t.Logf("the command really ran, and its output is in %s", path)
		}
	}
	if !ran {
		t.Errorf("no transcript holds the output of the command home allowed")
	}
}

// ── 6 ───────────────────────────────────────────────────────────────────────

// testHover drives the pointer over home's left column with the SGR motion
// reports the all-motion mode asks for, and reads the right pane.
func testHover(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "hoverws", false)
	r := start(t, "afe2e_hover", home, ws, 120, 40)
	screen := r.waitFor(25*time.Second, "home", "esc close", "Seed Beta")

	rows := r.lines()
	target := -1
	for i, line := range rows {
		if strings.Contains(line, "Seed Beta") && strings.Contains(line, "○") {
			target = i + 1 // capture-pane is 0-based here, the mouse report is 1-based
			break
		}
	}
	if target < 0 {
		t.Fatalf("no `Seed Beta` row to hover:\n%s", screen)
	}
	before := rightPane(screen)
	t.Logf("before hovering, the pane is about the cursor's row:\n%s", before)

	r.mouseTo(10, target)
	hovered := r.waitFor(15*time.Second, "Seed Beta")
	pane := rightPane(hovered)
	if !strings.Contains(pane, "Seed Beta") {
		t.Errorf("hovering `Seed Beta` did not move the pane onto it:\n%s", hovered)
	} else {
		t.Logf("the pointer previews the row under it:\n%s", hovered)
	}

	// Off the column, into the pane's own half: the card goes back to the
	// cursor's row.
	r.mouseTo(100, target)
	time.Sleep(1500 * time.Millisecond)
	off := rightPane(r.capture())
	if strings.Contains(off, "Seed Beta") && !strings.Contains(before, "Seed Beta") {
		t.Errorf("moving the pointer off the column left the pane on the hovered row:\n%s", r.capture())
	}
	t.Logf("pointer off the column, the pane is the cursor's again:\n%s", r.capture())
}

// ── 7 ───────────────────────────────────────────────────────────────────────

// testFolds seeds four things waiting on one conversation — more than the three
// a card draws — and toggles the fold with `m`.
func testFolds(t *testing.T) {
	home := newHome(t, nil)
	dir := seedProject(t, home, "foldy", 7, 5*time.Minute)
	seedNews(t, dir,
		"the tests passed",
		"the docs site answered 200",
		"the certificate has 40 days",
		"the nightly build landed",
	)
	ws := newWorkspace(t, "foldws", false)
	r := start(t, "afe2e_fold", home, ws, 120, 40)

	screen := r.waitFor(25*time.Second, "home", "esc close", "since you left")
	t.Logf("the card with four things waiting:\n%s", screen)
	if !strings.Contains(screen, "◆ 4 things since you left") {
		t.Errorf("the news heading does not read `◆ 4 things since you left`:\n%s", firstMatch(screen, "since you left"))
	}
	if !strings.Contains(screen, "▸ …1 more") {
		t.Errorf("no `▸ …N more` fold door on a card with four things:\n%s", screen)
	}
	if strings.Contains(screen, "the nightly build landed") {
		t.Errorf("the fourth thing is drawn before the fold was opened:\n%s", screen)
	}

	r.lit("m")
	opened := r.waitFor(15*time.Second, "the nightly build landed")
	t.Logf("`m` opened the fold:\n%s", opened)

	r.lit("m")
	time.Sleep(1500 * time.Millisecond)
	closed := r.capture()
	if strings.Contains(closed, "the nightly build landed") {
		t.Errorf("`m` did not close the fold again:\n%s", closed)
	}
	t.Logf("`m` closed it again:\n%s", closed)
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
	if caught, ok := r.glimpse(20*time.Second, "thinking ·", "writing ·", "running ·"); ok {
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

// testProjectCard puts the cursor on a folded project line and reads the card a
// whole project gets.
func testProjectCard(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "projws", false)
	r := start(t, "afe2e_proj", home, ws, 120, 40)
	screen := r.waitFor(25*time.Second, "home", "─ elsewhere ", "▸ delta")

	row := foldedRow(screen, "delta")
	if row < 0 {
		t.Fatalf("no folded `delta` row:\n%s", screen)
	}
	r.mouseTo(10, row)
	card := r.waitFor(15*time.Second, "/tmp/aforge-e2e-seed/delta")
	pane := rightPane(card)
	t.Logf("the project card:\n%s", card)

	for _, want := range []string{"delta", "/tmp/aforge-e2e-seed/delta", "1 conversation", "last active"} {
		if !strings.Contains(pane, want) {
			t.Errorf("the project card is missing %q:\n%s", want, pane)
		}
	}
	t.Logf("the facts line: %s", firstMatch(pane, "conversation"))
}

// dirtyFiles is what `git status --porcelain` says about the workspace, which
// is the reading the repo band is derived from (homeband_repo.go runs the same
// command in porcelain v2).
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

// rightPane is everything from the gutter rightwards, which is where home draws
// the card. The list column is 46 cells (home.go's homeColumns), so anything
// past column fifty belongs to the pane.
func rightPane(screen string) string {
	var b strings.Builder
	for _, line := range strings.Split(screen, "\n") {
		runes := []rune(line)
		if len(runes) <= 50 {
			b.WriteString("\n")
			continue
		}
		b.WriteString(strings.TrimRight(string(runes[50:]), " "))
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

// foldedRow is the 1-based terminal row of a `▸ name` line under the elsewhere
// rule, or -1.
func foldedRow(screen, name string) int {
	lines := strings.Split(screen, "\n")
	seenRule := false
	for i, line := range lines {
		if strings.Contains(line, "─ elsewhere ") {
			seenRule = true
			continue
		}
		if seenRule && strings.Contains(line, "▸ ") && strings.Contains(line, name) {
			return i + 1
		}
	}
	return -1
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
			trimmed := strings.TrimLeft(strings.TrimSpace(line), "⠁⠂⠄⠈⠐⠠⠆⠇⠋⠙⠸⠼⠴⠦⠧⠹⢀⡀ ")
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
