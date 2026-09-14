//go:build e2e

// questions_e2e_test.go is THE QUESTIONS WAVE, DRIVEN THROUGH THE REAL BINARY
// IN A REAL TERMINAL AGAINST A REAL MODEL.
//
// docs/design/questions/DESIGN.md is a contract about what a person sees when
// this engine hands them a decision: a line, a card, a room, a sheet; a chip
// that counts what is waiting; `esc` that means later and never cancel; a
// receipt that stays where the question was. Six lanes built that in eleven
// packages, each green in its own unit tests. This file is the one place that
// asks whether it reaches a person — it starts tmux, sends the bytes a keyboard
// sends, steers deepseek/deepseek-v4-flash into calling `ask` with the shape
// each scenario is about, and reads the screen back with `capture-pane`.
//
// ── THE DOOR THIS SUITE OPENS, AND WHY IT IS NOT THE DEFAULT ONE ────────────
//
// Nearly every scenario below launches `codeaf chat --no-host`, which keeps the
// conversation IN THIS PROCESS. The ordinary road since #736 is a session host
// over a unix socket (cmd/codeaf's chatv3_local.go), and a scenario about a
// compare table or a dial has nothing to say about a wire: opening the host road
// for all of them would put a second process, a socket and a redial loop between
// a keystroke and the row it is about, and every flake in either would be
// reported here as a defect in the surface.
//
// SO THE WIRE GETS A SCENARIO OF ITS OWN INSTEAD.
// `TheOrdinaryRoadCarriesAQuestionAndItsAnswer` opens the door that ships and
// asks the whole question through it — raised, counted, answered with a key,
// recorded — so the choice above can never quietly become a suite that only
// tests a door nobody opens.
//
// ── THE NEEDLES ────────────────────────────────────────────────────────────
//
// Sentences THE PRODUCT spells come through [say] and are written down in
// tuiwords_test.go, whose untagged gate reads the surface's own sources back —
// that is the law this package has carried since #184. Sentences the MODEL
// spells are steered by the prompt in each scenario and are typed here as
// literals, exactly as the standing lane types the words it asked a model for:
// a table of product sentences is a source of truth, and a table of things a
// model happened to say is a table of coincidences.
//
// AND AN ANSWER IS BOTH, SO IT IS COMPOSED RATHER THAN PASTED. `1 delete it` is
// the scenario's own word with the PRODUCT's key grammar in front of it, so it
// goes through [keyedWord] and never into a literal here. Nine of them were
// pasted as `[1] delete it`, #933 took the brackets off every key, and this
// suite spent a fortnight waiting three minutes at a time for screens that were
// already right (issue #998). [TestNoNeedleSpellsAKeyTheSurfaceStoppedSpelling]
// is the untagged law that now refuses one.
//
// ── WHAT IT COSTS ──────────────────────────────────────────────────────────
//
// About a dollar for the whole file on deepseek/deepseek-v4-flash, and about
// forty minutes, most of it waiting for the model to load the `questions`
// capability group and then answer with one `ask` call. Iterate one at a time:
//
//	go test -tags e2e -run 'TestQuestionsE2E/ALine' -count=1 -timeout 15m -v ./internal/e2e/
//	go test -tags e2e -run TestQuestionsE2E -count=1 -timeout 90m -v ./internal/e2e/
//
// It SKIPS rather than fails with no provider key, no tmux or no bin/codeaf,
// exactly as the suite beside it does — and the key is resolved by the product's
// own three roads, through [liveKey].
package e2e

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/home"
)

// questionPatience is how long a scenario waits for a screen that needs a model
// round trip behind it. It is generous on purpose: a `deepseek-v4-flash` turn
// that has to fetch the `questions` capability group first has been measured at
// 45–70 seconds on this box, and a suite that failed at 60 would be a suite
// reporting the model's latency as the surface's defect.
const questionPatience = 3 * time.Minute

// questionSettleWait is longer than internal/tui3's own settle guard (250ms) and
// is what every scenario waits before pressing an answer key. The guard exists
// so a question landing under a moving hand takes nothing; a test that pressed
// inside it would be testing the guard rather than the answer.
const questionSettleWait = 700 * time.Millisecond

func TestQuestionsE2E(t *testing.T) {
	requireTmuxAndKey(t)

	t.Run("ALineAsksAndTheKeysAnswerIt", questionsLine)
	t.Run("ACardCountsDownToItsPickAndEnterTakesIt", questionsCard)
	t.Run("TheRoomComparesAnnotatesAsksBackAndSends", questionsRoom)
	t.Run("ACardWithABlockUnderEachAnswerArrivesWhole", questionsBlocksUnderAnswers)
	t.Run("ASentenceWithHolesIsFilledIn", questionsBlanks)
	t.Run("AChecklistTicksSeveralAnswers", questionsChecklist)
	t.Run("PairsAreAnsweredOneRowAtATime", questionsPairs)
	t.Run("ADialIsMovedWithTheArrows", questionsDial)
	t.Run("ARatifiedActSaysWhatItDidAndHowToUndoIt", questionsRatify)
	t.Run("AssumptionsStandUntilOneIsStruck", questionsAssumption)
	t.Run("TwoQuestionsInOneStepAreOnePanelWithTabs", questionsTabs)
	t.Run("FourReadsInOneBatchAreOnePermissionFrame", questionsGroup)
	t.Run("AnotherWindowAnswersAndTheFirstSaysWho", questionsReach)
	t.Run("AQuestionWhoseSubjectWentAwayIsWithdrawn", questionsWithdrawn)
	t.Run("AProjectRuleDecidesAChoiceAndTheRowWearsIt", questionsAutonomy)
	t.Run("HeadlessTakesTheDefaultAndSaysSo", questionsHeadless)
	t.Run("TheOrdinaryRoadCarriesAQuestionAndItsAnswer", questionsDefaultDoor)
	t.Run("TheOlderBlocksHaveNotMovedOntoTheObject", questionsOlderBlocks)
}

// ── the rig ─────────────────────────────────────────────────────────────────

// The terminal every scenario is drawn at. A hundred columns is the width the
// gallery is rendered at and the width the block's own give-up order was tuned
// against (internal/tui3's questionkeys.go), so a key missing from a row here is
// a key missing from the row a person reads.
const (
	questionCols = 100
	questionRows = 36
	// questionPageCols is the one scenario that is drawn wider: the page's two
	// panes stand where the BODY is a hundred cells or more, and the task
	// column — two cells even stowed — is charged against the body, so a
	// hundred-column terminal can only ever draw the page as one column.
	questionPageCols = 120
)

// questionRig is one codeaf on this machine's own keyboard, IN THIS PROCESS.
//
// `chat --no-host` is the whole of the difference from [start]'s ordinary use
// and the file header says why: the default road puts the session behind a unix
// socket that does not carry questions, so a suite that opened it would be
// measuring a wire rather than a surface.
func questionRig(t *testing.T, name string, overrides map[string]any) *rig {
	t.Helper()
	return questionRigAt(t, name, newHome(t, overrides), newWorkspace(t, name, false))
}

// questionRigAt is the same terminal on a state root and a project the caller
// already made — what the reach scenario needs, because two windows answering
// one question have to be two windows on ONE machine.
func questionRigAt(t *testing.T, name, home, ws string, cols ...int) *rig {
	t.Helper()
	wide := questionCols
	if len(cols) > 0 && cols[0] > 0 {
		wide = cols[0]
	}
	return start(t, name, home, ws, wide, questionRows, "chat", "--no-host")
}

// steer types a message and sends it.
//
// TELLING THE MODEL WHAT TO ASK IS THE POINT AND NOT A CHEAT. Every scenario
// here is about the SHAPE of a decision — a line, a dial, a checklist — and the
// shape is the asker's to choose. A test that hoped a model would happen to
// reach for a dial would be a test about a model's taste; naming the shape in
// the message is how a person steers one too, and what is under test is what the
// surface does with the object that comes back.
func steer(t *testing.T, r *rig, text string) {
	t.Helper()
	r.lit(text)
	time.Sleep(200 * time.Millisecond)
	r.keys("Enter")
}

// awaitQuestion waits for a screen with every needle on it, giving the model the
// time one round trip actually takes.
func awaitQuestion(t *testing.T, r *rig, want ...string) string {
	t.Helper()
	return r.waitFor(questionPatience, want...)
}

// press answers, AFTER the settle guard has had its window. Every scenario goes
// through this rather than [rig.keys] so no scenario can accidentally pass by
// pressing inside the guard it is meant to respect.
func press(t *testing.T, r *rig, names ...string) {
	t.Helper()
	time.Sleep(questionSettleWait)
	r.keys(names...)
	time.Sleep(600 * time.Millisecond)
}

// aim is the gesture that makes the block's LETTER verbs live, and every
// scenario that presses one does it first.
//
// A LETTER IS THE QUESTION'S ONLY ONCE YOU HAVE AIMED AT IT. `o`, `c`, `x`, `?`
// and the rest are each the first letter of a word people type into the box —
// the `d` of "do the schema first" once handed the call back to the asker and
// left "o the schema first" behind — so internal/tui3 does not take one from
// somebody who has not yet looked at the question (questionkeys.go's
// [questionAimKey], and the manual's own questions.md says it in a person's
// words). Aiming is any key a sentence could not carry: the arrows, `tab`,
// `enter`, `esc`, or a click on an answer. The answers' own digits are exempt
// and answer the moment the row is on screen, which is why the scenarios that
// only press a number never needed this.
//
// IT IS `↓` AND THEN `↑` BECAUSE AN AIM MAY NOT BE AN ANSWER. `enter` would take
// the pick and `esc` would put the question away, which leaves the walk — and
// the walk moves the pointer, so it is walked straight back. The pointer ends
// where it started and the block has the hand, which is all a scenario about a
// page needs before it can open one.
//
// WITHOUT IT THE `o` GOES IN THE BOX, and the scenario waits out its patience
// for a page nobody opened while the screen shows `› o`. That is what six of
// these scenarios were doing.
func aim(t *testing.T, r *rig) {
	t.Helper()
	press(t, r, "Down")
	press(t, r, "Up")
}

// type_ is the same for literal bytes.
func type_(t *testing.T, r *rig, text string) {
	t.Helper()
	time.Sleep(questionSettleWait)
	r.lit(text)
	time.Sleep(600 * time.Millisecond)
}

// ── the pictures ────────────────────────────────────────────────────────────

// shot saves the screen as it stands and renders it.
//
// THE .ansi IS THE EVIDENCE AND THE .png IS THE GALLERY. capture-pane -e keeps
// every colour byte, so the file is what the terminal held; the png is that file
// through freeze, with the runs of blank rows collapsed so a mostly-empty screen
// does not cost half a megabyte. Both land under docs/design/questions/screens/
// because a picture nobody committed is a picture nobody looked at.
func shot(t *testing.T, r *rig, moment string) {
	t.Helper()
	raw, err := exec.Command("tmux", "capture-pane", "-e", "-p", "-t", r.name).Output()
	if err != nil {
		t.Logf("capture-pane: %v", err)
		return
	}
	dir := filepath.Join(repoRoot(t), "docs", "design", "questions", "screens")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("screens directory: %v", err)
		return
	}
	base := filepath.Join(dir, shotName(t)+"-"+moment)
	if err := os.WriteFile(base+".ansi", raw, 0o644); err != nil {
		t.Logf("write %s.ansi: %v", base, err)
		return
	}
	freeze, err := exec.LookPath(filepath.Join(os.Getenv("HOME"), "go", "bin", "freeze"))
	if err != nil {
		t.Logf("no freeze on this machine: %s.ansi is saved and unrendered", base)
		return
	}
	trimmed := base + ".trim.ansi"
	if err := os.WriteFile(trimmed, []byte(collapseBlankRows(string(raw))), 0o644); err != nil {
		t.Logf("write %s: %v", trimmed, err)
		return
	}
	defer os.Remove(trimmed)
	out, err := exec.Command(freeze, "--language", "ansi",
		"--font.size", "11", "--padding", "6", "--margin", "0", "--shadow.blur", "0",
		trimmed, "-o", base+".png").CombinedOutput()
	if err != nil {
		t.Logf("freeze %s: %v\n%s", base, err, out)
		return
	}
	if err := shrinkPNG(base + ".png"); err != nil {
		t.Logf("shrink %s.png: %v", base, err)
	}
}

// shotName is this subtest's own name as a file name: `TestQuestionsE2E/ALine…`
// with the parent and the camel humps taken out, so the gallery reads as a list
// of moments rather than of Go identifiers.
func shotName(t *testing.T) string {
	name := t.Name()
	if at := strings.LastIndex(name, "/"); at >= 0 {
		name = name[at+1:]
	}
	name = shotRunOn.ReplaceAllString(name, "$1-$2")
	name = shotHumps.ReplaceAllString(name, "$1-$2")
	return strings.ToLower(name)
}

// shotHumps splits `ALineAsks` at every hump and shotRunOn splits the run of
// capitals that opens one, so the file is `a-line-asks` rather than `aline-asks`.
var (
	shotHumps = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	shotRunOn = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
)

// collapseBlankRows turns every run of empty rows into one. A terminal screen is
// mostly air and freeze charges by the pixel; the rows that carry ink are the
// ones a caption is about.
func collapseBlankRows(screen string) string {
	lines := strings.Split(strings.TrimRight(screen, "\n"), "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		if strings.TrimSpace(shotInk.ReplaceAllString(line, "")) == "" {
			blanks++
			if blanks > 1 {
				continue
			}
		} else {
			blanks = 0
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n") + "\n"
}

// shotInk is an SGR escape, so a row of pure colour changes counts as blank.
var shotInk = regexp.MustCompile("\x1b\\[[0-9;]*m")

// screenSays is a substring assertion that names the moment rather than dumping
// the frame twice: [rig.waitFor] has already logged the screen when it gave up.
func screenSays(t *testing.T, screen, want, moment string) {
	t.Helper()
	if !strings.Contains(screen, want) {
		t.Errorf("%s: the screen never said %q.\nIt said:\n%s", moment, want, screen)
	}
}

// screenEchoes is the same assertion for a word the MODEL wrote rather than the
// product: matched without regard to case, because a label this suite asked for
// as `sqlite` comes back as `SQLite` about half the time and a scenario that
// failed on the shift key would be reporting a model's typography as a defect.
// Product sentences never go through here — those are exact, and [say] is where
// they come from.
func screenEchoes(t *testing.T, screen, want, moment string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(screen), strings.ToLower(want)) {
		t.Errorf("%s: the screen never said %q.\nIt said:\n%s", moment, want, screen)
	}
}

// screenSilent is the emptiness law asserted: a thing that must NOT be on screen.
func screenSilent(t *testing.T, screen, unwanted, moment string) {
	t.Helper()
	if strings.Contains(screen, unwanted) {
		t.Errorf("%s: the screen said %q and must not have.\nIt said:\n%s", moment, unwanted, screen)
	}
}

// ── the line ────────────────────────────────────────────────────────────────

// questionsLine is the smallest form and the whole key grammar on one row:
// pending, `esc` is later, the chip counts it, the chord brings it back, a digit
// answers it, and the receipt stays where it was.
func questionsLine(t *testing.T) {
	r := questionRig(t, "q-line", nil)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "delete the build directory?", kind permission, form line, `+
		`reason "the build directory is stale", stakes reversible, `+
		`options [{"key":"1","label":"delete it"},{"key":"2","label":"leave it"}].`)

	screen := awaitQuestion(t, r, "delete the build directory?", keyedWord("1", "delete it"))
	screenSays(t, screen, keyedWord("2", "leave it"), "the line's second answer")
	screenSays(t, screen, say(t, "questionLaterKeyWord"), "the line's way out")
	screenSays(t, screen, "the build directory is stale", "the line's reason")
	screenSays(t, screen, say(t, "questionChipTail"), "the chip counting one question")
	shot(t, r, "pending")

	// `esc` IS LATER AND CANCELS NOTHING: the rows fold, the chip keeps counting.
	press(t, r, "Escape")
	folded := r.capture()
	screenSilent(t, folded, keyedWord("1", "delete it"), "esc folded the block away")
	screenSays(t, folded, say(t, "questionChipTail"), "esc kept the question counted")
	shot(t, r, "folded")

	// AND A KEY AIMED AT THE BOX IS NOT AN ANSWER. This is the settle guard's own
	// sentence at the only granularity a terminal can honestly test it: a
	// question that is not on screen has never been shown, is therefore never
	// settled, and takes no key. The 250ms window itself is pinned by
	// internal/tui3's TestAKeyPressedBeforeTheQuestionSettledIsDroppedAndNeverApplied.
	type_(t, r, "1")
	typed := r.capture()
	screenSays(t, typed, say(t, "questionChipTail"), "the digit did not answer the folded question")
	r.keys("C-u")
	time.Sleep(400 * time.Millisecond)

	// The chord brings it back from wherever a person is standing.
	press(t, r, "M-a")
	back := r.waitFor(20*time.Second, keyedWord("1", "delete it"))
	screenSays(t, back, "delete the build directory?", "the chord raised the question again")

	type_(t, r, "1")
	receipt := r.waitFor(30*time.Second, say(t, "questionReceiptWord"), "delete the build directory?")
	screenSays(t, receipt, "delete it", "the receipt names what was picked")
	screenSays(t, receipt, say(t, "questionReceiptYouWord"), "the receipt says who decided")
	screenSilent(t, receipt, say(t, "questionChipTail"), "the answered question stopped being counted")
	shot(t, r, "receipt")
}

// ── the card ────────────────────────────────────────────────────────────────

// questionsCard is the form with a row per answer, and the one clock on this
// surface that acts without a key: `recommend-then-auto`, whose tail says which
// answer it is about to take and when.
func questionsCard(t *testing.T) {
	r := questionRig(t, "q-card", nil)

	// The clock is a PROJECT RULE and not a property of the question, so it is
	// set the way a person sets one.
	//
	// THIRTY SECONDS AND NOT THE DESIGN'S NINE, and the difference is the test
	// rather than the drawing. The row says the same sentence at either number;
	// nine seconds is measured from the moment the tool call is made, the quiet
	// batch holds a question for up to three of them, and a scenario that had to
	// read a card, photograph it and press a key inside the remaining six would
	// be reporting its own timing as the surface's defect. The rule ACTUALLY
	// RUNNING OUT is what `AProjectRuleDecidesAChoiceAndTheRowWearsIt` is for.
	steer(t, r, "/autonomy choice recommend 30s")
	rule := r.waitFor(20*time.Second, "recommend")
	screenSays(t, rule, "choice", "the rule was written for the choice kind")

	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "rewrite the packer?", kind choice, form card, `+
		`reason "it will run on its own branch and open a pull request", stakes reversible, `+
		`options [{"key":"1","label":"start it","consequence":"on a branch of its own"},`+
		`{"key":"2","label":"not now","consequence":"nothing runs"},`+
		`{"key":"3","label":"change it first"}], `+
		`pick {"key":"1","reason":"the packer is the only thing left on the list"}.`)

	// The needle is the CONSEQUENCE and not the head, because the head is also on
	// the receipt: a scenario that waited for the head would go green on the
	// record of a decision it never saw drawn.
	screen := awaitQuestion(t, r, "rewrite the packer?", "on a branch of its own")
	screenSays(t, screen, "nothing runs", "and what the second answer costs")
	screenSays(t, screen, "it will run on its own branch", "the card's attribution row")
	screenSays(t, screen, say(t, "questionTakeThePickWord"), "the card offers the pick")
	screenSays(t, screen, "start it in ", "the clock says which answer it is about to take")
	if !questionCountdown.MatchString(screen) {
		t.Errorf("the clock: the card said %q with no countdown after it.\nIt said:\n%s", "start it in ", screen)
	}
	screenSays(t, screen, say(t, "questionOwnRuleWord"), "the row wears the rule that is running its clock")
	shot(t, r, "clock")

	press(t, r, "Enter")
	receipt := r.waitFor(30*time.Second, say(t, "questionReceiptWord"), "rewrite the packer?")
	screenSays(t, receipt, "start it", "the receipt names the pick enter took")
	screenSays(t, receipt, say(t, "questionReceiptYouWord"), "enter is a person's key and the record says so")
	shot(t, r, "taken")
}

// questionsBlocksUnderAnswers is the call a model could not make. On 2026-09-10
// deepseek-v4-flash was given the message below — a person's own words, on the
// Spark, in a real terminal — and three calls running it sent the answers list
// as a JSON STRING holding the list; every one was refused with `options takes
// a list`, the turn ended on the loop guard and the person never saw a
// question. Two things changed (session/toolargs.go, session/tools_ask.go), and
// this is the scenario that says whether they were enough. THE MESSAGE IS THE
// PERSON'S, VERBATIM, and not an argument object spelled out: the shape the
// model reaches for on its own is the thing under test, and a cleaner steer
// ("call ask with options [...]") went green on the very build that failed the
// person. The law is read off the journal, where the tool calls actually are —
// one `ask`, carrying blocks, no refusal — and off the screen, where the
// question is drawn with a room behind it.
func questionsBlocksUnderAnswers(t *testing.T) {
	r := questionRig(t, "q-blocks", nil)
	steer(t, r, `Ask me with your question tool, as a card with a diagram block under each option, `+
		`how the breath should be paced in my meditation app: fixed 4/2/6, adaptive to HRV, free timer. `+
		`Give each option a body, a consequence, and recommend one with a reason and what would change your mind.`)

	// The needles are the person's own words for the answers, because the head
	// and the labels are the model's to write; `open it` says the card has a
	// room behind it, which is what bodies and blocks under the answers mean.
	card := awaitQuestion(t, r, "HRV", say(t, "questionOpenKeyWord"))
	screenSays(t, card, say(t, "questionTakeThePickWord"), "the asker recommended one, and the card offers it")
	shot(t, r, "card")

	// THE JOURNAL IS WHERE THE LAW IS READ. The screen can only say a question
	// arrived; it cannot say how many calls it took, and a question that arrives
	// on the third try after two argument refusals is the defect wearing a
	// green screen. One `ask` that the decoder read, carrying blocks, and no
	// argument refusal at all is the whole of what the fix promised.
	//
	// A call the QUESTION GATE turned away is not counted against it: the gate
	// refuses in its own words (`nothing was asked and the person saw no
	// question: the pick names "adaptive-hrv", which is not one of the answers`)
	// a question whose bytes were read perfectly well, and the model writes it
	// again. That is a different law with its own tests (session/question_test.go)
	// and a model's own mistake, and this scenario is about the bytes.
	asks, blocks, refusals, turnedAway := 0, 0, 0, 0
	var refused []string
	for _, journal := range sessionTranscripts(t, r.home) {
		asks += strings.Count(journal, `"function":{"name":"ask"`)
		blocks += strings.Count(journal, `blocks`)
		refusals += strings.Count(journal, "Invalid arguments: ")
		turnedAway += strings.Count(journal, "nothing was asked and the person saw no question: ")
		// The refusal's own words are the evidence: a shape this decoder does
		// not yet read is named there, and the journal is gone with the rig.
		for _, line := range strings.Split(journal, "\n") {
			if at := strings.Index(line, "Invalid arguments: "); at >= 0 {
				refused = append(refused, clip(line[at:], 240))
			}
		}
	}
	if asks-turnedAway != 1 || refusals != 0 || blocks == 0 {
		t.Errorf("the first ask did not arrive whole: %d ask calls (%d turned away by the question gate), "+
			"%d argument refusals, blocks mentioned %d times in the journal; want one call the decoder read, "+
			"carrying blocks, and none refused.\nRefused with:\n  %s\nThe journal is kept at %s",
			asks, turnedAway, refusals, blocks, strings.Join(refused, "\n  "), keepJournals(t, r))
	}

	aim(t, r)
	press(t, r, "o")
	room := r.waitFor(20*time.Second, say(t, "questionRoomWaitsWord"))
	screenSays(t, room, "HRV", "the page lists the answers")
	shot(t, r, "open")

	press(t, r, "Enter")
	receipt := r.waitFor(30*time.Second, say(t, "questionReceiptWord"))
	screenSays(t, receipt, say(t, "questionReceiptYouWord"), "enter is a person's key and the record says so")
	shot(t, r, "taken")
}

// keepJournals copies every transcript the rig wrote to a place the rig's
// teardown does not sweep, and names it. A refusal names the SHAPE the decoder
// would not read only in its first twenty-four runes; the bytes themselves are
// what the next fix is written against, and a journal under t.TempDir is gone
// the moment the scenario ends.
func keepJournals(t *testing.T, r *rig) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "codeaf-e2e-journals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "nowhere: " + err.Error()
	}
	kept := []string{}
	for path, journal := range sessionTranscripts(t, r.home) {
		name := filepath.Join(dir, shotName(t)+"-"+filepath.Base(filepath.Dir(path))+".jsonl")
		if err := os.WriteFile(name, []byte(journal), 0o644); err == nil {
			kept = append(kept, name)
		}
	}
	return strings.Join(kept, ", ")
}

// questionCountdown is the clock's tail: a whole number of seconds and the unit,
// which is countdownWord's own shape for anything under a minute.
var questionCountdown = regexp.MustCompile(`start it in \d+s`)

// ── the room ────────────────────────────────────────────────────────────────

// questionsRoom is the page: bodies, an attached block, the compare table the
// asker's own dimensions build, a comment on one answer, one exchange back, and
// an answer composed of a pick and words — which the model then names in its
// reply, because an answer that does not reach the asker is a page with no door
// out of it.
func questionsRoom(t *testing.T) {
	// THIS ONE SCENARIO GETS A WIDER TERMINAL AND A STOWED TASK COLUMN, because
	// it is the one that drives the page's two panes. The page is drawn in the
	// BODY and the column is charged against the body, so the split needs
	// `questionPageCols` less the stowed column's two cells to clear the
	// two-column floor. Both are pinned here rather than left to the machine's
	// own profile: a scenario whose layout depends on a setting somebody's
	// laptop happens to carry is a scenario that passes in one place and hangs
	// in another.
	r := questionRigAt(t, "q-room",
		newHome(t, map[string]any{config.KeyTaskColumn: false}),
		newWorkspace(t, "q-room", false), questionPageCols)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "which store should the ledger sit on?", kind choice, form room, `+
		`reason "a schema change is next and it is cheaper before rows exist", `+
		`stakes reversible, blocking {"turn":true}, `+
		`three options, each with key, label, body, consequence and a dimensions object `+
		`holding "ops cost" and "speed": 1 postgres, 2 sqlite, 3 a file per day. `+
		`pick {"key":"1","reason":"the reporting job already reads it","confidence":"medium",`+
		`"wouldChange":"the ledger ever has to run without a server"}. `+
		`attach one text block titled "what it would look like". `+
		`When the answer comes back, say in one sentence which option was picked and what was typed beside it.`)

	card := awaitQuestion(t, r, "which store should the ledger sit on?", say(t, "questionOpenKeyWord"))
	screenEchoes(t, card, "postgres", "the card lists the answers before the page is opened")
	shot(t, r, "card")

	aim(t, r)
	press(t, r, "o")
	room := r.waitFor(20*time.Second, say(t, "questionRoomWaitsWord"))
	screenSays(t, room, "which store should the ledger sit on?", "the page's head")
	screenSays(t, room, say(t, "questionRoomWaitsWord"), "the room says what is stopped on it")
	// THE PAGE OPENS WHERE THE BLOCK'S POINTER STOOD, so the foot already says
	// what `enter` would send — `answering 1 postgres` — rather than nothing.
	// That is #789's law arriving on the page ("every answer has a pointer the
	// arrows walk and enter takes"), and questionroom.go's own enter path states
	// it: "It costs nothing on a page nobody has walked, because the page opens
	// where the block's pointer stood."
	screenSays(t, room, say(t, "questionAnsweringWord"),
		"the foot says what enter would send from the row the page opened on")
	// AND IT MAY NOT CLAIM NOTHING IS CHOSEN WHILE THE POINTER IS ON AN ANSWER.
	// `nothing chosen yet` is the foot of a page standing on the one row that
	// names no answer — `something else…`, which comes after the last option and
	// carries no key ([questionRoomSends] finds none there). A page that opened
	// on an answer and said it anyway would be the emptiness law read backwards.
	screenSilent(t, room, say(t, "questionRoomNoPickWord"),
		"the page opened on an answer, so the foot may not say nothing is chosen")
	screenSays(t, room, "what it would look like", "the attached block")
	screenSays(t, room, say(t, "questionWouldSwitchWord"), "what would change the asker's mind")
	shot(t, r, "open")

	// TWO PANES, AND THE ARROWS MOVING BETWEEN THEM (owner ruling 2026-09-11,
	// page pick A): the answers on the left, the evidence of the one the pointer
	// is on beside them, and `→ detail` on the foot because there IS a pane to
	// move into. A page of one column unfolds that evidence under the pointer
	// and does not offer the key.
	screenSays(t, room, say(t, "questionPageDetailWord"), "the page is two panes at this width")
	shot(t, r, "split")

	press(t, r, "Right")
	reading := r.waitFor(15*time.Second, say(t, "questionPageScrollWord"))
	screenSays(t, reading, say(t, "questionWouldSwitchWord"),
		"the arrows are the evidence pane's, and the evidence is still what they are on")
	press(t, r, "Left")
	back := r.waitFor(15*time.Second, say(t, "questionPageDetailWord"))
	screenSilent(t, back, say(t, "questionPageScrollWord"),
		"the arrows are the list's again, so the key that gives them back is not offered")

	press(t, r, "x")
	compare := r.waitFor(15*time.Second, say(t, "questionCompareOnlyWord"))
	screenSays(t, compare, "ops cost", "the compare table is built on the asker's own dimensions")
	screenSays(t, compare, "speed", "the second dimension")
	shot(t, r, "compare")
	press(t, r, "x")

	press(t, r, "c")
	prompt := r.waitFor(15*time.Second, say(t, "questionCommentPromptWord"))
	screenSays(t, prompt, say(t, "questionCommentPromptWord"), "the comment prompt")
	type_(t, r, "the backup story matters more than speed")
	press(t, r, "Enter")
	noted := r.waitFor(15*time.Second, say(t, "questionNotedWord"))
	screenSays(t, noted, "the backup story matters more than speed", "the comment sits under the answer it is about")
	shot(t, r, "comment")

	press(t, r, "?")
	asking := r.waitFor(15*time.Second, say(t, "questionAskBackPromptWord"))
	screenSays(t, asking, say(t, "questionAskBackPromptWord"), "one exchange per answer, and the row says so")
	type_(t, r, "does the reporting job read it directly")
	press(t, r, "Enter")
	asked := r.waitFor(15*time.Second, "does the reporting job read it directly")
	screenSays(t, asked, "does the reporting job read it directly", "the question back is kept with the answer")
	shot(t, r, "askback")

	press(t, r, "2")
	picked := r.waitFor(15*time.Second, say(t, "questionAnsweringWord"))
	screenEchoes(t, picked, "sqlite", "the foot says what enter would send")
	type_(t, r, "but keep the sqlite file as the source of truth")
	shot(t, r, "composed")
	press(t, r, "Enter")

	receipt := r.waitFor(30*time.Second, say(t, "questionReceiptWord"))
	screenEchoes(t, receipt, "sqlite", "the receipt names the pick")
	shot(t, r, "answered")

	// AND THE ANSWER REACHES THE ASKER, WHOLE.
	//
	// It is asserted against the journal rather than against the screen, and the
	// reason is the law being checked: what has to arrive is the ANSWER OBJECT —
	// the pick, the words typed beside it and the note left on an option — and
	// only the tool result the model was handed says whether it did. A reply on
	// screen is the model's paraphrase of that, and a scenario waiting for a
	// sentence nothing promised would be waiting for a model's taste.
	answered := false
	deadline := time.Now().Add(questionPatience)
	for time.Now().Before(deadline) && !answered {
		for _, journal := range sessionTranscripts(t, r.home) {
			// The tool result is a JSON document inside a JSON string, so the
			// needle carries the escaping the journal actually holds.
			if strings.Contains(journal, "but keep the sqlite file as the source of truth") &&
				strings.Contains(journal, `\"picked\":[\"2\"]`) {
				answered = true
			}
		}
		if !answered {
			time.Sleep(pollEvery)
		}
	}
	if !answered {
		t.Errorf("the answer never reached the asker: no tool result in this conversation's journal "+
			"carries the pick and the words typed beside it.\nThe screen was:\n%s", r.capture())
	}
	shot(t, r, "reply")
}

// ── the four structured shapes ──────────────────────────────────────────────

// questionsBlanks is a sentence with holes in it: the sixth rung of the ladder,
// where a key would not do and free text would take more than it needs.
func questionsBlanks(t *testing.T) {
	r := questionRig(t, "q-blanks", nil)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "name the three columns", kind clarification, form room, `+
		`reason "the schema needs names before I can write it", stakes reversible, `+
		`input {"kind":"blanks","prompt":"the ledger table",`+
		`"blanks":[{"label":"table"},{"label":"key column"},{"label":"timestamp column"}]}, `+
		`options [{"key":"1","label":"use these"},{"key":"2","label":"let me think"}].`)

	awaitQuestion(t, r, "name the three columns", say(t, "questionOpenKeyWord"))
	shot(t, r, "card")

	aim(t, r)
	press(t, r, "o")
	room := r.waitFor(20*time.Second, say(t, "questionRoomWaitsWord"))
	// THE HOLES WEAR THEIR OWN NAMES. A form of unlabelled boxes is a form
	// nobody can fill: the asker named each field and the page owes those names.
	screenSays(t, room, "table", "the first hole's name")
	screenSays(t, room, "key column", "the second hole's name")
	screenSays(t, room, "timestamp column", "the third hole's name")
	screenSays(t, room, say(t, "questionFilledWord"),
		"a shape has nothing to choose, so the foot says when to press enter instead")
	screenSays(t, room, say(t, "questionBlankKeyWord"), "the key that walks between the holes")
	screenSilent(t, room, say(t, "questionTickKeyWord"),
		"a sentence with holes has nothing to tick, so it may not offer the key that ticks")
	shot(t, r, "open")

	type_(t, r, "ledger")
	press(t, r, "Tab")
	type_(t, r, "run_id")
	filled := r.capture()
	screenSays(t, filled, "ledger", "what was typed into the first hole")
	screenSays(t, filled, "run_id", "what was typed into the second")
	shot(t, r, "filled")
}

// questionsChecklist is several answers at once, which is what `space` is for
// and the one shape whose keys have to survive the row's give-up order — a
// checklist a person cannot discover is tickable is an ordinary list.
func questionsChecklist(t *testing.T) {
	r := questionRig(t, "q-checklist", nil)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "which checks should run before merge?", kind choice, form room, `+
		`reason "the pipeline is being rewritten", stakes reversible, `+
		`input {"kind":"checklist"}, `+
		`options [{"key":"1","label":"unit tests"},{"key":"2","label":"vet"},`+
		`{"key":"3","label":"the packed manual"},{"key":"4","label":"the size budget"}], `+
		`pick {"key":"1"}.`)

	awaitQuestion(t, r, "which checks should run before merge?", say(t, "questionOpenKeyWord"))
	aim(t, r)
	press(t, r, "o")
	room := r.waitFor(20*time.Second, say(t, "questionRoomWaitsWord"), "unit tests")
	screenSays(t, room, say(t, "questionTickKeyWord"),
		"the checklist's own key is on the row: a shape whose verb is dropped for width is a shape nobody can work")
	shot(t, r, "open")

	press(t, r, "Space")
	press(t, r, "Down")
	press(t, r, "Space")
	ticked := r.capture()
	screenSays(t, ticked, say(t, "questionAnsweringWord"), "the foot says what enter would send")
	screenEchoes(t, ticked, keyedWord("1", "unit tests")+", "+keyedWord("2", "vet"),
		"both ticks are in the answer, in their own keys and in the order they were given")
	shot(t, r, "ticked")

	press(t, r, "Enter")
	receipt := r.waitFor(30*time.Second, say(t, "questionReceiptWord"))
	screenEchoes(t, receipt, "unit tests, vet", "the receipt carries every answer that was ticked")
	screenSays(t, receipt, say(t, "questionReceiptYouWord"),
		"and says the person gave them — an answer taken on this machine's own keyboard may never "+
			"be recorded as another window's")
	shot(t, r, "receipt")
}

// questionsPairs is this-or-this, one row at a time.
func questionsPairs(t *testing.T) {
	r := questionRig(t, "q-pairs", nil)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "settle the two naming calls", kind choice, form room, `+
		`reason "both spellings are in the diff and one has to go", stakes reversible, `+
		`input {"kind":"pairs","blanks":[`+
		`{"label":"the table","choices":["ledger","entries"]},`+
		`{"label":"the id column","choices":["run_id","id"]}]}, `+
		`options [{"key":"1","label":"take these"},{"key":"2","label":"leave both"}].`)

	awaitQuestion(t, r, "settle the two naming calls", say(t, "questionOpenKeyWord"))
	aim(t, r)
	press(t, r, "o")
	room := r.waitFor(20*time.Second, say(t, "questionRoomWaitsWord"))
	screenSays(t, room, "the table", "the first pair's own name")
	screenSays(t, room, "ledger", "the first side")
	screenSays(t, room, "entries", "the second side")
	screenSays(t, room, say(t, "questionPairAWord"), "the key that takes the first")
	screenSays(t, room, say(t, "questionPairBWord"), "the key that takes the second")
	shot(t, r, "open")

	press(t, r, "a")
	after := r.capture()
	screenSays(t, after, "the id column", "answering one pair walks on to the next")
	shot(t, r, "answered")
}

// questionsDial is a number on a range, moved with the arrows and read as words.
func questionsDial(t *testing.T) {
	r := questionRig(t, "q-dial", nil)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "how hard should the retry loop try?", kind judgement, form room, `+
		`reason "the endpoint flaps and the ceiling is a taste call", stakes reversible, `+
		`input {"kind":"dial","dial":{"min":0,"max":10,"default":3,`+
		`"labels":["give up at once","twice","three times","five times","ten times"]}}, `+
		`options [{"key":"1","label":"use it"},{"key":"2","label":"leave it alone"}].`)

	awaitQuestion(t, r, "how hard should the retry loop try?", say(t, "questionOpenKeyWord"))
	aim(t, r)
	press(t, r, "o")
	room := r.waitFor(20*time.Second, say(t, "questionRoomWaitsWord"))
	screenSays(t, room, say(t, "questionMoveItWord"), "the arrows move the dial rather than a cursor")
	shot(t, r, "open")

	press(t, r, "Right")
	press(t, r, "Right")
	moved := r.capture()
	screenSays(t, moved, say(t, "questionFilledWord"), "a dial has nothing to choose and the foot says so")
	shot(t, r, "moved")
}

// ── the rungs below the question ────────────────────────────────────────────

// questionsRatify is the ladder's third rung — act, then ratify. Nothing waits
// on it: the work is done, the row says what was done, and the way back is on it.
func questionsRatify(t *testing.T) {
	r := questionRig(t, "q-ratify", nil)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "renamed 12 files under src/", kind ratify, form line, `+
		`reason "the rename was reversible so I did it and am telling you", stakes reversible, `+
		`options [{"key":"1","label":"fine"},{"key":"2","label":"put them back"}].`)

	screen := awaitQuestion(t, r, "renamed 12 files under src/")
	// THE SETTLED MARK AND NOT THE ATTENTION ONE. A ratify row is a statement,
	// so it wears tokens.GSettled; a `?` here would be the surface asking for
	// something it has already had.
	screenSays(t, screen, say(t, "questionSettledMark")+" renamed 12 files",
		"the ratify line wears the settled mark")
	screenSays(t, screen, say(t, "questionChangeKeyWord"), "the way to change what was done")
	// AND THE WAY BACK. DESIGN.md's kind table gives ratify `u undo while real`;
	// a rung that acts first owes a person the undo, and a row without it is the
	// rung making a decision on their behalf with no way out.
	screenSays(t, screen, say(t, "questionUndoKeyWord"), "the ratify row's undo")
	shot(t, r, "line")
}

// questionsAssumption is the ladder's second rung: the asker says what it is
// assuming, every assumption stands, and a person strikes the wrong ones.
func questionsAssumption(t *testing.T) {
	r := questionRig(t, "q-assumption", nil)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "I am assuming these before I write the migration", kind assumption, form card, `+
		`reason "none of them is written down anywhere I can read", stakes reversible, `+
		`options [{"key":"1","label":"the ledger is append only"},`+
		`{"key":"2","label":"nobody reads it from a second process"},`+
		`{"key":"3","label":"the timestamps are UTC"}].`)

	screen := awaitQuestion(t, r, "I am assuming these before I write the migration")
	screenSays(t, screen, "the ledger is append only", "the first assumption")
	screenSays(t, screen, "the timestamps are UTC", "the third assumption")
	// EVERY GLYPH COMES FROM THE VOCABULARY, and DESIGN.md gives this kind its
	// own: "assumptions ≈". A card of assumptions drawn with the question mark
	// is a card asking for an answer none of these rows needs.
	//
	// THIS NEEDLE IS NOT IN tuiwords_test.go AND CANNOT BE. That table's gate
	// asserts a word still stands in the surface's own sources, and internal/tui2/
	// tokens has no slot for this mark at all — so a row for it would turn the
	// untagged gate red on a pull request for a sentence the product has never
	// spelled. It is typed here, with the design line it comes from, and it is
	// expected to fail until the vocabulary gains the meaning.
	screenSays(t, screen, questionAssumptionMark, "the assumption mark")
	shot(t, r, "card")

	press(t, r, "2")
	struck := r.waitFor(30*time.Second, say(t, "questionReceiptWord"))
	screenSays(t, struck, "nobody reads it from a second process", "the struck assumption is named in the record")
	shot(t, r, "struck")
}

// ── several at once ─────────────────────────────────────────────────────────

// questionsTabs is SEVERAL QUESTIONS FROM ONE STEP ARE ONE PANEL: two questions
// the model asked in one message arrive as one panel with a tab each, `←→` move
// between them, `enter` holds an answer rather than sending it, and the review
// sends every held answer through the one door in one command.
func questionsTabs(t *testing.T) {
	r := questionRig(t, "q-tabs", nil)
	steer(t, r, `Call the ask tool TWICE IN THE SAME MESSAGE — two tool calls at once, `+
		`not one after the other — and run nothing else. `+
		`The first: head "read package.json?", kind permission, form line, `+
		`reason "the dependency list decides the rest", stakes reversible, `+
		`options [{"key":"1","label":"allow once"},{"key":"2","label":"skip it"}]. `+
		`The second: head "read go.mod?", kind permission, form line, `+
		`reason "the module path decides the import", stakes reversible, `+
		`options [{"key":"1","label":"allow once"},{"key":"2","label":"skip it"}].`)

	// ONE PANEL, A TAB EACH: the first question in the body, the second one's
	// words in the top edge, and the key between them on the bottom edge.
	screen := awaitQuestion(t, r, say(t, "questionTabKeyWord"))
	screenSays(t, screen, "read package.json?", "the tab on screen")
	screenSays(t, screen, "go.mod", "the second question's tab")
	shot(t, r, "raised")

	press(t, r, "Right")
	press(t, r, "Right")
	review := r.capture()
	screenSays(t, review, say(t, "questionNotAnsweredWord"),
		"the review names a question with nothing held for it")
	shot(t, r, "review-empty")

	press(t, r, "Left")
	press(t, r, "Left")
	press(t, r, "1")
	press(t, r, "1")
	held := r.capture()
	screenSays(t, held, say(t, "questionSetSendWord"), "both answers held, and the review offers to send them")
	shot(t, r, "review-held")

	press(t, r, "Enter")
	sent := r.waitFor(30*time.Second, say(t, "questionReceiptWord"))
	screenSays(t, sent, "allow once", "the receipt of what was sent")
	shot(t, r, "sent")
}

// questionsGroup is PERMISSIONS FROM ONE STEP ARE ONE FRAME: four reads the
// model asked for in one message are one frame listing what each wants, with
// `allow all 4 · one by one · deny all`; `one by one` opens the same four as
// tabs, and the review sends what was held — an allow and a deny mixed — in one
// command.
//
// ONLY `read` ASKS, so nothing else in the turn puts a question of its own in
// front of the four. The files are named by their whole path because the
// model's working folder is not always the project it was opened on, and a read
// that failed for want of the file would still have asked.
func questionsGroup(t *testing.T) {
	r := questionRig(t, "q-group", map[string]any{"tools.approvalMode": "allow", "tools.approval": "read:prompt"})
	paths := make([]string, 0, 4)
	for _, name := range []string{"e", "f", "g", "h"} {
		path := filepath.Join(r.ws, name+".txt")
		if err := os.WriteFile(path, []byte("the "+name+" line\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		paths = append(paths, path)
	}
	steer(t, r, `Read `+strings.Join(paths, ", ")+` with the read tool — all four read calls `+
		`in the same message, at once, not one after another — and run nothing else. `+
		`Then tell me each file's first line.`)

	screen := awaitQuestion(t, r, say(t, "questionGroupAllowWord"))
	for _, name := range []string{"e.txt", "f.txt", "g.txt", "h.txt"} {
		screenSays(t, screen, name, "the frame lists what each call wants")
	}
	screenSays(t, screen, say(t, "questionGroupApartWord"), "the way to answer them one at a time")
	// AND THE FRAME NAMES THE ANSWER THAT LOSES NOTHING. Four reads are ordinary
	// calls, so the pointer opens on `allow all 4` now that the gate grades them
	// (#953) — which is precisely when the refusal must be marked, the pointer
	// being on the act. The frame said it only under its own pointer for a
	// while, and this line is what goes red the day the mark goes missing again.
	screenSays(t, screen, say(t, "questionGroupSafeWord"), "the frame names its way out, wherever the pointer is")
	shot(t, r, "raised")

	press(t, r, "2")
	tabs := r.capture()
	screenSays(t, tabs, say(t, "questionTabKeyWord"), "one by one opens the same four as tabs")
	shot(t, r, "one-by-one")

	// Allow the first three and deny the last: the review holds a mix, and the
	// deny goes through the same door as the allows.
	press(t, r, "1")
	press(t, r, "1")
	press(t, r, "1")
	press(t, r, "3")
	review := r.capture()
	screenSays(t, review, "send all 4", "all four held, and the review offers to send them")
	shot(t, r, "review")

	press(t, r, "Enter")
	sent := r.waitFor(60*time.Second, say(t, "questionReceiptWord"))
	screenSays(t, sent, "deny", "the receipt of the one that was denied")
	shot(t, r, "sent")
}

// ── two windows ─────────────────────────────────────────────────────────────

// questionsReach is FIRST ANSWER WINS across two terminals on one machine: the
// question is raised in one window, answered from home in the other, and the
// window that raised it is told who decided rather than being left saying `you`
// about a key nobody pressed there.
func questionsReach(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "q-reach", false)
	first := questionRigAt(t, "q-reach-a", home, ws)
	steer(t, first, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "publish the draft?", kind permission, form line, `+
		`reason "the draft has not been read by anybody else", stakes reversible, `+
		`options [{"key":"1","label":"publish it"},{"key":"2","label":"hold it"}].`)
	raised := awaitQuestion(t, first, "publish the draft?", keyedWord("1", "publish it"))
	screenSays(t, raised, say(t, "questionChipTail"), "the first window is counting it")
	shot(t, first, "raised")

	// The second window opens on home of its own accord: a launch into a project
	// that already holds a conversation lands on the ranked list rather than in
	// one, which is exactly where somebody who wandered off would be.
	second := questionRigAt(t, "q-reach-b", home, ws)
	// ON ANOTHER PAGE THE QUESTION IS A ROW WITH ITS OWN ANSWERS. Home lists
	// every open question under the conversation that raised it, with the same
	// keys, because a person standing on home is still the person being asked.
	elsewhere := second.waitFor(60*time.Second, "publish the draft?")
	screenSays(t, elsewhere, keyedWord("1", "publish it"),
		"home offers the question's OWN answers, on its own keys — the whole of what makes a row "+
			"answerable rather than a notice")
	shot(t, second, "home")

	press(t, second, "1")
	time.Sleep(3 * time.Second)
	shot(t, second, "answered")

	told := first.waitFor(90*time.Second, say(t, "questionReceiptWord"), "publish the draft?")
	screenSays(t, told, say(t, "questionOtherWindowWord"),
		"the window that raised it says who decided, and does not claim the key was pressed here")
	shot(t, first, "told")
}

// ── withdrawal ──────────────────────────────────────────────────────────────

// questionsWithdrawn is WITHDRAWN, WITH A REASON: a question whose subject went
// away — here, the turn it was holding open was let go of — leaves one dim line
// and stops being counted.
func questionsWithdrawn(t *testing.T) {
	r := questionRig(t, "q-withdrawn", nil)
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "overwrite the checkpoint?", kind permission, form line, `+
		`reason "the checkpoint is from the run before this one", stakes reversible, `+
		`options [{"key":"1","label":"overwrite it"},{"key":"2","label":"keep it"}].`)
	awaitQuestion(t, r, "overwrite the checkpoint?", keyedWord("1", "overwrite it"))
	shot(t, r, "raised")

	// `esc` twice: the first folds the question to the chip (it is LATER, not
	// cancel), the second is the surface's own interrupt, which takes the turn
	// the question was holding open — and with the turn gone the question has
	// stopped needing an answer.
	press(t, r, "Escape")
	press(t, r, "Escape")
	gone := r.waitFor(90*time.Second, say(t, "questionWithdrawnWord"))
	screenSays(t, gone, say(t, "questionWithdrawnMark"), "the withdrawn mark")
	screenSays(t, gone, "overwrite the checkpoint?", "the withdrawn line names the question that went away")
	screenSilent(t, gone, say(t, "questionChipTail"), "a withdrawn question stops being counted")
	shot(t, r, "withdrawn")
}

// ── the dial that answers for you ───────────────────────────────────────────

// questionsAutonomy is a project rule taking a decision while nobody presses
// anything: `/autonomy choice recommend 5s`, a choice raised under it, the clock
// wearing `your rule`, and the record saying it was the settings and not a person.
func questionsAutonomy(t *testing.T) {
	r := questionRig(t, "q-autonomy", nil)

	steer(t, r, "/autonomy")
	sheet := r.waitFor(20*time.Second, say(t, "autonomyHeadWord"))
	screenSays(t, sheet, say(t, "autonomyUsageWord"), "the sheet names the door that changes it")
	screenSays(t, sheet, say(t, "autonomyAlwaysWord"),
		"the one row no rule may cover, said on the sheet rather than discovered by being refused")
	shot(t, r, "sheet")

	steer(t, r, "/autonomy choice recommend 5s")
	r.waitFor(20*time.Second, "choice")
	shot(t, r, "rule")

	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "which formatter should the hook run?", kind choice, form card, `+
		`reason "the hook is being written now and either is fine", stakes reversible, `+
		`options [{"key":"1","label":"gofmt"},{"key":"2","label":"gofumpt"}], `+
		`pick {"key":"1","reason":"it is already in the toolchain"}.`)

	screen := awaitQuestion(t, r, "which formatter should the hook run?")
	screenSays(t, screen, say(t, "questionOwnRuleWord"),
		"a clock that is running because of something this project was told to do says so")
	shot(t, r, "counting")

	// NOBODY PRESSES ANYTHING. The clock runs out and the rule takes the pick.
	decided := r.waitFor(60*time.Second, say(t, "questionReceiptWord"), "which formatter should the hook run?")
	screenSays(t, decided, "gofmt", "the rule took the asker's own pick")
	screenSays(t, decided, say(t, "questionDecidedByRuleWord"),
		"and the record says the settings decided it, never that a person did")
	shot(t, r, "decided")
}

// ── nobody at the keyboard ──────────────────────────────────────────────────

// questionsHeadless is the HEADLESS law: `--once` has nobody to ask, so the
// policy applies and IS PRINTED. A run that took a default silently is a run
// whose decision nobody can find afterwards.
func questionsHeadless(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "q-headless", false)
	command := exec.Command(binary(t), "chat", "--once",
		`Call the ask tool now, exactly once, and run nothing else. `+
			`head "which store should the ledger sit on?", kind choice, `+
			`reason "the schema change is next", stakes reversible, `+
			`options [{"key":"1","label":"postgres"},{"key":"2","label":"sqlite"}], `+
			`pick {"key":"1"}.`)
	command.Dir = ws
	command.Env = append(os.Environ(), "CODEAF_HOME="+home,
		config.APIKeyEnv+"="+liveKey(t))
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("codeaf chat --once: %v\n%s", err, out)
	}
	printed := string(out)
	t.Logf("── codeaf chat --once ──\n%s", printed)
	if !strings.Contains(printed, say(t, "headlessAskedWord")) {
		t.Errorf("headless: the run never printed %q, so a decision it took on nobody's behalf "+
			"is a decision nothing on the outer stream records.\nIt printed:\n%s",
			say(t, "headlessAskedWord"), printed)
	}
	if !strings.Contains(printed, "which store should the ledger sit on?") {
		t.Errorf("headless: the printed line never named the question.\nIt printed:\n%s", printed)
	}
}

// ── the two findings this file exists to pin ────────────────────────────────

// questionsDefaultDoor is the road a person actually takes, and the one thing in
// this file that is not about a form.
//
// Bare `codeaf` has not run the conversation in this process since #736: it
// attaches this workspace's session host over a unix socket (cmd/codeaf's
// chatv3_local.go), so the question object has to cross internal/remote's wire
// in both directions — the raise on the way out and the answer on the way back —
// and the block deliberately draws NOTHING where it cannot resolve what it
// draws (A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN). Every other
// scenario here opens `--no-host` to keep what it is measuring to the surface,
// which would hide a wire that had stopped carrying either half.
//
// So this one asks the whole question through the door that ships: raised,
// counted, answered with a key, and recorded.
func questionsDefaultDoor(t *testing.T) {
	root := shortQuestionHome(t)
	ws := newWorkspace(t, "q-door", false)
	r := start(t, "q-door", root, ws, questionCols, questionRows)
	t.Cleanup(func() {
		stop := exec.Command(binary(t), "engine", "--stop", "--workspace", ws)
		stop.Env = append(os.Environ(), "CODEAF_HOME="+root)
		_ = stop.Run()
	})
	// AND IT PROVES WHICH ROAD IT IS ON BEFORE IT MEASURES ANYTHING. The rule
	// falls back to the in-process door whenever it cannot reach a host
	// (cmd/codeaf's v3TakeHostRoad), which is right for the product and fatal
	// for a scenario about the wire: it would pass by testing the door every
	// other scenario here already tests. A socket answering for this workspace
	// is the whole of the evidence.
	socket, err := enginehost.SocketPath(ws)
	if err != nil {
		t.Fatalf("name this workspace's socket: %v", err)
	}
	holding := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(holding) {
			t.Fatalf("no session host is holding %s after 30s (%s is not there), so this window "+
				"is on the in-process door and would prove nothing about the road that ships", ws, socket)
		}
		time.Sleep(pollEvery)
	}
	steer(t, r, `Call the ask tool now, exactly once, and run nothing else. `+
		`head "delete the build directory?", kind permission, form line, `+
		`reason "the build directory is stale", stakes reversible, `+
		`options [{"key":"1","label":"delete it"},{"key":"2","label":"leave it"}].`)
	screen := awaitQuestion(t, r, "delete the build directory?", keyedWord("1", "delete it"))
	screenSays(t, screen, say(t, "questionChipTail"),
		"the ordinary road has to be able to put a question in front of somebody")
	shot(t, r, "asked")

	// AND TAKE THE ANSWER BACK. A wire that carried the raise and not the resolve
	// would draw a question nobody in that window could answer, which is worse
	// than drawing none.
	type_(t, r, "1")
	receipt := r.waitFor(30*time.Second, say(t, "questionReceiptWord"), "delete the build directory?")
	screenSays(t, receipt, say(t, "questionReceiptYouWord"), "the answer crossed back and was recorded")
	screenSilent(t, receipt, say(t, "questionChipTail"), "and the question stopped being counted")
	shot(t, r, "answered")
}

// questionsOlderBlocks is the migration's own ledger, read off a real screen.
//
// internal/tui3's [app.questionDrawnHere] names the four lanes this block has
// taken over — the model's `ask`, the sub-harness question, the fuel gate and
// the conflict. Everything else in the audit's twenty-one mechanisms is still
// drawn by the block it always had: the approval gate here, and beside it the
// task proposal's own card and countdown in task.go. So a permission asked by
// the OLDEST and most common asker in the product — a shell command under an
// approval rule — reaches none of the wave's grammar: no chip, no `esc` that
// means later, no receipt.
//
// THIS SUBTEST IS EXPECTED TO FAIL until those lanes move onto the object.
func questionsOlderBlocks(t *testing.T) {
	r := questionRig(t, "q-older", map[string]any{"tools.approvalMode": "ask"})
	steer(t, r, `Run the shell command "ls -la" with the bash tool, once, and nothing else.`)
	// The wait is on the OLDER block's own offer row, so the assertions below are
	// made at the moment a permission is genuinely on screen rather than while
	// the turn is still starting.
	screen := awaitQuestion(t, r, say(t, "consentOldOfferWord"))
	shot(t, r, "consent")
	screenSays(t, screen, say(t, "questionChipTail"),
		"a consent is a permission question and the chip is what counts every one of them")
	screenSays(t, screen, say(t, "questionLaterKeyWord"),
		"and `esc` is later on every question, which is the law that retired the consent block's own")
	screenSilent(t, screen, say(t, "consentOldCancelWord"),
		"`cancel` is the word this wave retired: nothing is cancelled by putting a question off")
}

// questionAssumptionMark is docs/design/questions/DESIGN.md's own spelling for
// the assumption kind: "assumptions ≈". Nothing in internal/tui2/tokens carries
// it, which is why it is a literal here — see the comment at its one use.
const questionAssumptionMark = "≈"

// ── keeping the gallery small enough to commit ──────────────────────────────

// shrinkPNG rewrites one render with a palette of its own colours.
//
// A TERMINAL SCREEN IS A FEW DOZEN COLOURS AND A LOT OF ANTI-ALIASING. freeze
// writes truecolour, which costs half a megabyte for a picture of forty lines of
// text, and a gallery of twenty of those is ten megabytes in a repository that
// ratchets its own binary size. Counting the colours actually used and mapping
// every pixel to the nearest of the commonest 256 costs nothing visible on text
// and about two thirds of the bytes. It is deliberately NOT dithered: dithering
// anti-aliased glyphs is how a screenshot of a terminal comes out looking like a
// fax of one.
func shrinkPNG(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	src, err := png.Decode(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	counts := map[color.RGBA]int{}
	bounds := src.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			counts[color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}]++
		}
	}
	used := make([]color.RGBA, 0, len(counts))
	for hue := range counts {
		used = append(used, hue)
	}
	sort.Slice(used, func(i, j int) bool { return counts[used[i]] > counts[used[j]] })
	if len(used) > 256 {
		used = used[:256]
	}
	shades := make(color.Palette, 0, len(used))
	for _, hue := range used {
		shades = append(shades, hue)
	}
	out := image.NewPaletted(bounds, shades)
	draw.Draw(out, bounds, src, bounds.Min, draw.Src)
	target, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(target, out); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}

// shortQuestionHome is a state root A UNIX SOCKET CAN BE NAMED IN, with the same
// rows [newHome] writes.
//
// IT IS NOT t.TempDir, AND THAT IS THE WHOLE REASON IT EXISTS. Go names a temp
// directory after the test, this package's test names are sentences, and a unix
// socket path has about a hundred bytes to spend (internal/enginehost's
// socketLimit). Under `/tmp/TestQuestionsE2ETheOrdinaryRoadCarries…/001/home` the
// socket does not fit, [enginehost.Attach] cannot reach a host, and cmd/codeaf's
// rule falls back to the in-process door — silently, and correctly, because a
// road it cannot take is no road. The effect is that EVERY rig in this package
// has been running the in-process door while looking exactly like a launch of
// the product; nothing was wrong with those scenarios, which are about the
// surface, but a scenario about the wire has to be somewhere shorter.
func shortQuestionHome(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "afq")
	if err != nil {
		t.Fatalf("make a state root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	// The test process asks internal/enginehost where the socket would be, and
	// that answer moves with CODEAF_HOME exactly as the binary's does.
	t.Setenv(home.EnvVar, root)
	rows := map[string]any{}
	if there, err := os.UserHomeDir(); err == nil {
		if raw, err := os.ReadFile(filepath.Join(there, ".codeaf", "config.json")); err == nil {
			if err := json.Unmarshal(raw, &rows); err != nil {
				t.Fatalf("the profile config would not parse: %v", err)
			}
		}
	}
	rows["model.talk"] = e2eTmuxModel
	rows[config.KeyIcons] = config.IconsPlain
	rows["tools.approvalMode"] = "allow"
	raw, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), raw, 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	return root
}

// e2eTmuxModel is the model this suite drives, spelled where [newHome] spells it.
const e2eTmuxModel = "deepseek/deepseek-v4-flash"
