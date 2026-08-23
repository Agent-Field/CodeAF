package tui3

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// /subharness AND THE INTAKE CARD, AS A PERSON MEETS THEM.
//
// Every test below asserts the fact the surface exists for — what is on the
// screen, what it deliberately does NOT draw, and what reaches the engine when
// a key is pressed — rather than the shape of the code under it
// (subharness.go).

// subFake is a session that can be asked about subharnesses: the optional half
// of the seam ([subharnessAgent]), which the ordinary scripted agent has never
// heard of.
//
// IT ANSWERS THE WAY THE REAL DOORS DO. The intake it hands back is built out of
// the manifest's own schema exactly as internal/session's SubharnessIntake
// builds one — every field blank, every required one missing — so a card that
// only worked against a hand-written fixture could not pass here. What it adds
// is `filled`, which is the session lane's half arriving early: a card chat has
// already read the conversation into.
type subFake struct {
	*fakeAgent
	rows []session.SubharnessRow
	// filled is what one field is already answered with, by field name, which is
	// what an intake filled from the conversation looks like.
	filled map[string]json.RawMessage
	// why is chat's one line about why this one was raised, which the proposal
	// door will carry and the `/subharness` door never does.
	why string
	// what the launching door was handed, and how many times.
	ranName  string
	ranInput json.RawMessage
	runs     int
	// refuse is what the launching door answers with instead of a node.
	refuse error
	// answers is what the PROPOSAL door was told, in order — the third card's
	// half of this seam ([subharnessOfferAgent]).
	answers []subAnswer
}

// subAnswer is one answer to a card chat raised, as the engine receives it.
type subAnswer struct {
	id    uint64
	run   bool
	input json.RawMessage
}

func (f *subFake) ResolveSubharness(id uint64, run bool, input json.RawMessage) {
	f.answers = append(f.answers, subAnswer{id: id, run: run, input: input})
}

func (f *subFake) SubharnessList() []session.SubharnessRow { return f.rows }

func (f *subFake) SubharnessIntake(name string) (session.SubharnessCard, error) {
	for _, row := range f.rows {
		if row.Manifest.Name != strings.TrimSpace(name) {
			continue
		}
		card := session.SubharnessCard{Manifest: row.Manifest, Why: f.why}
		for _, field := range row.Manifest.Input.Fields() {
			one := session.SubharnessField{Field: field}
			if value, ok := f.filled[field.Name]; ok {
				one.Value, one.Filled = value, true
			}
			card.Fields = append(card.Fields, one)
			if field.Required && !one.Filled {
				card.Missing = append(card.Missing, field.Name)
			}
		}
		return card, nil
	}
	return session.SubharnessCard{}, errors.New("no such subharness: " + name)
}

func (f *subFake) SubharnessRun(_ context.Context, name string, input json.RawMessage) (uint64, string, error) {
	f.runs++
	f.ranName, f.ranInput = name, input
	if f.refuse != nil {
		return 0, "", f.refuse
	}
	return 41, "chasing the flake", nil
}

// flakeInput is a plausible input schema: one required field, one with a
// default, and an order the card is expected to keep.
const flakeInput = `{
  "type": "object",
  "required": ["test"],
  "x-order": ["test", "branch"],
  "properties": {
    "test":   {"type": "string", "description": "the failing test's name"},
    "branch": {"type": "string", "default": "main"}
  }
}`

// subRow is one plausible subharness as the registry hands it over.
func subRow(name, purpose string, from exec.Provenance, floor time.Duration, cues []string, input, lastRun string) session.SubharnessRow {
	return session.SubharnessRow{
		Manifest: exec.Manifest{
			SubharnessInfo: exec.SubharnessInfo{Name: name, Purpose: purpose, DeadlineFloor: floor},
			Cues:           cues,
			Input:          exec.Schema(input),
			Provenance:     from,
		},
		LastRun: lastRun,
	}
}

// subApp is a surface with a registry of subharnesses behind it.
func subApp(t *testing.T, rows ...session.SubharnessRow) (*app, *subFake) {
	t.Helper()
	agent := &subFake{fakeAgent: &fakeAgent{model: "m"}, rows: rows}
	a := newTestApp(agent)
	a.width, a.height = 100, 24
	return a, agent
}

// subScreen is the open overlay as a reader sees it, blank lines dropped — the
// block is padded to the height the frame reserved, and the padding is not
// something a person reads.
func subScreen(a *app) string {
	out := make([]string, 0, subRowsMax)
	for _, line := range plainOverlay(a) {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return strings.Join(out, "\n")
}

// twoSubharnesses is the fixture most of these tests open on.
func twoSubharnesses() []session.SubharnessRow {
	return []session.SubharnessRow{
		subRow("flake-triage", "chase a flaky test", exec.FromBinary, 15*time.Minute,
			[]string{"flaky", "intermittent"}, flakeInput, ""),
		// The last-run note arrives already worded, out of the run journal the
		// store keeps beside each program: when it last ran, whether it finished,
		// and what it cost. This surface prints the sentence and never rebuilds it
		// — one fact, one wording (internal/session's SubharnessRow.LastRun).
		subRow("weekly-update", "the Monday note for the team", exec.FromYou, 0,
			[]string{"marketing", "newsletter"}, "", "2d ago · done · $0.04"),
	}
}

// ── 1. the command ──────────────────────────────────────────────────────────

// /subharness IS ON THE LIST AND IN /help, which is one table read twice
// (commands.go), and the short word people type reaches the same row.
func TestSubharnessIsOnTheCommandListAndInHelp(t *testing.T) {
	var listed, argued bool
	for _, c := range commands {
		if c.name != "subharness" {
			continue
		}
		if c.args == "" {
			listed = true
		} else {
			argued = true
		}
	}
	if !listed || !argued {
		t.Fatal("the command table has no bare /subharness row, or no <name> row")
	}
	if canonicalCommand("sub") != "subharness" {
		t.Fatalf("/sub reaches %q rather than /subharness", canonicalCommand("sub"))
	}
	if help := helpText(""); !strings.Contains(help, "/subharness") {
		t.Fatal("/help never names /subharness")
	}
}

// AND IT IS NO LONGER A WORD FOR /harness. The two are different things now — a
// saved shape of work, and a typed program with a card — so one word reaching
// both would be a word nobody could rely on.
func TestTheSubharnessWordsNoLongerReachTheHarnessPanel(t *testing.T) {
	for _, word := range []string{"subharness", "sub"} {
		if got := canonicalCommand(word); got == "harness" {
			t.Fatalf("/%s still resolves to /harness", word)
		}
	}
	for _, word := range harnessPickWords() {
		if word == "subharness" || word == "sub" {
			t.Fatalf("the harness picker still opens on /%s", word)
		}
	}
}

// ── 2. the list ─────────────────────────────────────────────────────────────

// EVERY ROW SAYS WHAT IT IS, WHAT IT COSTS AND WHERE IT CAME FROM.
func TestTheListSaysWhatEachSubharnessIsForAndWhereItCameFrom(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness")
	if !a.subPage.open {
		t.Fatal("/subharness opened nothing")
	}
	screen := subScreen(a)
	for _, want := range []string{
		"flake-triage", "chase a flaky test", "up to 15m", "built-in",
		"weekly-update", "the Monday note for the team", "yours",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the list never says %q:\n%s", want, screen)
		}
	}
}

// A LAST-RUN NOTE IS DRAWN WHEN THERE IS HISTORY AND NOT OTHERWISE. The
// emptiness law: no history renders as nothing — never "never run", never
// "0 runs".
func TestTheLastRunNoteIsAbsentUntilSomethingHasRun(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness")
	screen := subScreen(a)
	if !strings.Contains(screen, "2d ago · done · $0.04") {
		t.Fatalf("the row with history says nothing about it:\n%s", screen)
	}
	for _, line := range strings.Split(screen, "\n") {
		if !strings.Contains(line, "flake-triage") {
			continue
		}
		for _, banned := range []string{"never", "0 run", "no runs"} {
			if strings.Contains(strings.ToLower(line), banned) {
				t.Fatalf("a subharness nobody has run says %q:\n%s", banned, line)
			}
		}
	}
}

// AND A SUBHARNESS WHOSE MANIFEST DECLARES NO BUDGET SHAPE DRAWS NO FIGURE.
// exec.SubharnessInfo.Deadline would answer the generalist's fifteen minutes for
// it, and a row saying so would be the screen inventing a claim the program
// never made.
func TestASubharnessThatDeclaredNoBudgetShapeDrawsNoFigure(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness")
	for _, line := range strings.Split(subScreen(a), "\n") {
		if strings.Contains(line, "weekly-update") && strings.Contains(line, "up to") {
			t.Fatalf("a subharness with no budget shape drew one:\n%s", line)
		}
	}
}

// TYPING NARROWS IT, OVER THE NAME, THE ONE LINE AND THE CUES. The cues are the
// half a person is most likely to remember: nobody recalls that the program
// chasing flaky tests is called `flake-triage`.
func TestTypingNarrowsTheListOverNamePurposeAndCues(t *testing.T) {
	for _, probe := range []struct{ typed, want, gone string }{
		{"flake", "flake-triage", "weekly-update"},
		{"monday", "weekly-update", "flake-triage"},
		{"intermittent", "flake-triage", "weekly-update"},
		{"newsletter", "weekly-update", "flake-triage"},
	} {
		a, _ := subApp(t, twoSubharnesses()...)
		typeLine(t, a, "/subharness")
		for _, r := range probe.typed {
			drive(t, a, key(string(r)))
		}
		screen := subScreen(a)
		if !strings.Contains(screen, probe.want) {
			t.Fatalf("%q lost %s:\n%s", probe.typed, probe.want, screen)
		}
		if strings.Contains(screen, probe.gone) {
			t.Fatalf("%q kept %s:\n%s", probe.typed, probe.gone, screen)
		}
	}
}

// A FILTER THAT MATCHES NOTHING SAYS SO WHERE THE ROWS WERE.
func TestAFilterThatMatchesNothingSaysSo(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness")
	for _, r := range "zzzz" {
		drive(t, a, key(string(r)))
	}
	if screen := subScreen(a); !strings.Contains(screen, strings.TrimSpace(subNoMatchWord)) {
		t.Fatalf("a filter that matched nothing drew no answer:\n%s", screen)
	}
}

// ── 3. the empty build ──────────────────────────────────────────────────────

// A BUILD WITH NO SUBHARNESSES ANSWERS THE COMMAND AND OPENS NOTHING. The
// command exists, it says what a subharness is, and it draws no dead furniture —
// a capability that cannot work is absent rather than broken.
func TestSubharnessOnABuildWithNoneSaysSoAndOpensNothing(t *testing.T) {
	for _, probe := range []struct {
		name  string
		agent Agent
	}{
		// A session that has never heard of the doors at all — which is what a
		// conversation held over a connection looks like from here.
		{"no seam", &fakeAgent{model: "m"}},
		// A session with the doors and an empty registry behind them, which is
		// what the frozen stubs answer today.
		{"empty registry", &subFake{fakeAgent: &fakeAgent{model: "m"}}},
	} {
		a := newTestApp(probe.agent)
		a.width, a.height = 100, 24
		for _, line := range []string{"/subharness", "/subharness flake-triage"} {
			typeLine(t, a, line)
			if a.subPage.open {
				t.Fatalf("%s: %q opened an overlay with nothing on it", probe.name, line)
			}
			if screen := strings.Join(plainRows(a), "\n"); !strings.Contains(screen, subNothingWord) {
				t.Fatalf("%s: %q said nothing:\n%s", probe.name, line, screen)
			}
		}
	}
}

// ── 4. the card ─────────────────────────────────────────────────────────────

// ENTER ON A ROW OPENS THAT SUBHARNESS'S CARD.
func TestEnterOnTheListOpensThatSubharnessCard(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness")
	drive(t, a, key("enter"))
	card := a.subPage.card
	if card == nil {
		t.Fatal("enter on the list opened no card")
	}
	if card.name != "flake-triage" {
		t.Fatalf("enter opened %q rather than the row under the cursor", card.name)
	}
	screen := subScreen(a)
	for _, want := range []string{"flake-triage", "test", "branch", subRunWord} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the card never says %q:\n%s", want, screen)
		}
	}
}

// AND ESC WALKS BACK OUT BY ONE: to the list it came from, and then away.
func TestEscOnTheCardGoesBackToTheList(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness")
	drive(t, a, key("enter"))
	drive(t, a, key("esc"))
	if a.subPage.card != nil || !a.subPage.open {
		t.Fatal("esc on a card opened off the list did not go back to the list")
	}
	drive(t, a, key("esc"))
	if a.subPage.open {
		t.Fatal("esc on the list did not close it")
	}
}

// A REQUIRED FIELD NOBODY HAS ANSWERED IS MARKED, AND ONE THAT IS ANSWERED IS
// NOT. The mark is `▲` — a person being waited on — and it is deliberately not
// the accent, which is the one lit element on a screen.
func TestTheCardMarksTheRequiredFieldsNobodyHasAnswered(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness flake-triage")
	blank := subScreen(a)
	var marked string
	for _, line := range strings.Split(blank, "\n") {
		if strings.Contains(line, subMissingGlyph) {
			marked += line
		}
	}
	if !strings.Contains(marked, "test") {
		t.Fatalf("the required blank is not marked:\n%s", blank)
	}
	if strings.Contains(marked, "branch") {
		t.Fatalf("a field nobody has to answer is marked:\n%s", blank)
	}

	// And with it filled from the conversation, the mark is gone and the answer
	// is stated in its place.
	a2, second := subApp(t, twoSubharnesses()...)
	second.filled = map[string]json.RawMessage{"test": json.RawMessage(`"TestReconcilerRace"`)}
	typeLine(t, a2, "/subharness flake-triage")
	filled := subScreen(a2)
	if strings.Contains(filled, subMissingGlyph) {
		t.Fatalf("a card with nothing missing still marks something:\n%s", filled)
	}
	if !strings.Contains(filled, "TestReconcilerRace") {
		t.Fatalf("the filled field is not stated:\n%s", filled)
	}
}

// A FIELD'S SCHEMA ACCOUNT IS DRAWN WHERE THERE IS NO ANSWER, AND ITS DEFAULT
// BEFORE ITS DESCRIPTION. Three readings, one tail, in the order that answers
// the question the one under it was going to.
func TestAFieldSaysItsAnswerThenItsDefaultThenWhatItIs(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness flake-triage")
	screen := subScreen(a)
	if !strings.Contains(screen, "the failing test's name") {
		t.Fatalf("a blank field says nothing about itself:\n%s", screen)
	}
	if !strings.Contains(screen, "main") {
		t.Fatalf("a field with a default does not draw it:\n%s", screen)
	}
}

// A REASON, WHEN THE CARD WAS RAISED FOR ONE, IS SAID ONCE ABOVE THE FIELDS —
// and the block the frame reserved still holds every row, because the height
// that counts the head and the draw that fills it are one answer.
//
// The `/subharness` door never carries a reason: the person chose it themselves
// and needs none given back to them. The proposal door will, and this is the
// card it will draw.
func TestAReasonForRaisingTheCardIsSaidAboveTheFields(t *testing.T) {
	a, agent := subApp(t, twoSubharnesses()...)
	agent.why = "the brief and a failing test name are both here"
	typeLine(t, a, "/subharness flake-triage")
	screen := subScreen(a)
	if !strings.Contains(screen, agent.why) {
		t.Fatalf("the reason is not on the card:\n%s", screen)
	}
	for _, want := range []string{"test", "branch", subRunWord} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the reason pushed %q off the card:\n%s", want, screen)
		}
	}
	if got, want := len(plainOverlay(a)), a.overlayHeight(); got != want {
		t.Fatalf("the card drew %d lines into a block of %d", got, want)
	}
}

// `/subharness <name>` JUMPS STRAIGHT TO THE CARD, and it is the SAME CARD: one
// shape for both interactive doors.
func TestSubharnessWithANameJumpsStraightToItsCard(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness weekly-update")
	card := a.subPage.card
	if card == nil || card.name != "weekly-update" {
		t.Fatalf("a named subharness did not open its own card: %+v", card)
	}
	if card.fromList {
		t.Fatal("a card opened by name thinks it has a list behind it")
	}
	// A subharness that takes no input has nothing to ask and the run row is the
	// whole card.
	if card.count() != 1 {
		t.Fatalf("a subharness with no input schema drew %d rows", card.count())
	}
}

// AND A NAME NOTHING ANSWERS TO IS A SEARCH RATHER THAN AN ERROR. A near miss on
// a surface holding the whole list is a filter.
func TestANameNothingAnswersToNarrowsTheListInstead(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness flake")
	if a.subPage.card != nil {
		t.Fatal("a name nothing answers to opened a card")
	}
	if !a.subPage.open {
		t.Fatal("a name nothing answers to closed the list")
	}
	if screen := subScreen(a); !strings.Contains(screen, "flake-triage") || strings.Contains(screen, "weekly-update") {
		t.Fatalf("the typed word did not become the filter:\n%s", screen)
	}
}

// ── 5. filling it in and running it ─────────────────────────────────────────

// ENTER ON A FIELD OPENS THE BOX, AND ENTER IN THE BOX KEEPS WHAT WAS TYPED.
func TestFillingAFieldInKeepsItOnTheCard(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness flake-triage")
	drive(t, a, key("enter"))
	if a.subPage.card.edit == nil {
		t.Fatal("enter on a field opened no box")
	}
	for _, r := range "TestFlaky" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
	if a.subPage.card.edit != nil {
		t.Fatal("enter in the box left it open")
	}
	if screen := subScreen(a); !strings.Contains(screen, "TestFlaky") {
		t.Fatalf("what was typed is not on the card:\n%s", screen)
	}
	if _, _, blank := a.subPage.card.blank(); blank {
		t.Fatal("the field is still counted as blank")
	}
}

// AND ESC IN THE BOX LEAVES THE FIELD EXACTLY AS IT WAS.
func TestEscInTheFieldBoxKeepsNothing(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness flake-triage")
	drive(t, a, key("enter"))
	for _, r := range "oops" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("esc"))
	if a.subPage.card == nil {
		t.Fatal("esc in the box closed the card as well")
	}
	if screen := subScreen(a); strings.Contains(screen, "oops") {
		t.Fatalf("a cancelled box wrote its text anyway:\n%s", screen)
	}
}

// CONFIRMING HANDS THE DOOR THE NAME AND THE INPUT THE CARD SETTLED — the
// answered fields, in the card's own order, and nothing else. A field carrying
// only its schema's default is not sent: the default is stated once, in the
// schema, and the runner reads it there.
func TestConfirmingHandsTheDoorTheAssembledInput(t *testing.T) {
	a, agent := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness flake-triage")
	drive(t, a, key("enter"))
	for _, r := range "TestReconcilerRace" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
	// The cursor lands on the run row once nothing is missing, so this enter is
	// the launch.
	if !a.subPage.card.running() {
		t.Fatal("the cursor did not settle on the run row")
	}
	drive(t, a, key("enter"))
	if agent.runs != 1 {
		t.Fatalf("the launching door was called %d times", agent.runs)
	}
	if agent.ranName != "flake-triage" {
		t.Fatalf("the door was handed %q", agent.ranName)
	}
	if got := string(agent.ranInput); got != `{"test":"TestReconcilerRace"}` {
		t.Fatalf("the door was handed %s", got)
	}
	if a.subPage.open {
		t.Fatal("the overlay stayed up over a run that started")
	}
}

// AND RUNNING IT WITH A REQUIRED FIELD STILL BLANK SAYS WHICH ONE, and does not
// call the door at all. A key that silently did nothing is a key a person
// presses twice and then goes looking for what is broken.
func TestRunningItWithARequiredFieldBlankSaysWhichOne(t *testing.T) {
	a, agent := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness flake-triage")
	// Walk to the run row past the two fields.
	drive(t, a, key("down"))
	drive(t, a, key("down"))
	if !a.subPage.card.running() {
		t.Fatal("the cursor never reached the run row")
	}
	drive(t, a, key("enter"))
	if agent.runs != 0 {
		t.Fatal("a card with a required blank started a run anyway")
	}
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, subBlankWord+"test") {
		t.Fatalf("nothing said which field was blank:\n%s", screen)
	}
	if a.subPage.card.cursor != 0 {
		t.Fatalf("the cursor did not land on the blank field: %d", a.subPage.card.cursor)
	}
}

// A REFUSAL FROM THE LAUNCHING DOOR IS SAID IN ITS OWN WORDS. Today's frozen
// stub answers "there is nothing here to run" on a build whose launching half
// nobody has wired, and that sentence is the honest one to print.
func TestARefusalFromTheLaunchingDoorIsSaidAsItStands(t *testing.T) {
	a, agent := subApp(t, twoSubharnesses()...)
	agent.refuse = errors.New("there is nothing here to run")
	typeLine(t, a, "/subharness weekly-update")
	drive(t, a, key("enter"))
	if screen := strings.Join(plainRows(a), "\n"); !strings.Contains(screen, "there is nothing here to run") {
		t.Fatalf("the door's own refusal never reached the person:\n%s", screen)
	}
}

// AND A RUN THAT STARTED IS HANDED TO THE TASK ROAD: the receipt names it, and
// this surface draws nothing else about it.
func TestARunThatStartedIsHandedToTheTaskRoad(t *testing.T) {
	a, _ := subApp(t, twoSubharnesses()...)
	typeLine(t, a, "/subharness weekly-update")
	drive(t, a, key("enter"))
	screen := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"weekly-update", "chasing the flake"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the receipt never says %q:\n%s", want, screen)
		}
	}
}

// ── the third door: the card chat itself raised ─────────────────────────────
//
// The proposal arrives on the harness lane while the turn that asked is parked
// inside its own batch (internal/session's tools_subharness.go). What these
// assert is what a person meets: the same card, with a reason on it and two
// answers under it, and a turn that is told what they said whichever key they
// press.

// flakeProposal is one raised card as the engine sends it, built through the
// same door the surface would have used — so a test cannot pass against a
// hand-written card the real intake would never produce.
func flakeProposal(t *testing.T, agent *subFake, id uint64, why string) session.Event {
	t.Helper()
	agent.why = why
	card, err := agent.SubharnessIntake("flake-triage")
	if err != nil {
		t.Fatalf("the fixture has no flake-triage: %v", err)
	}
	return session.Event{
		Kind:       session.EventSubharnessProposal,
		ID:         id,
		Text:       card.Manifest.Name,
		Hint:       card.Manifest.Purpose,
		Subharness: &card,
	}
}

// answeredApp is a surface holding a card chat raised, with every required field
// already answered — which is the ordinary proposal, since the engine fills the
// form from the conversation before it raises anything.
func answeredApp(t *testing.T) (*app, *subFake) {
	t.Helper()
	a, agent := subApp(t, twoSubharnesses()...)
	agent.filled = map[string]json.RawMessage{"test": json.RawMessage(`"TestReconcilerRace"`)}
	a.designEvent(flakeProposal(t, agent, 7, "the brief and a failing test name are both here"))
	return a, agent
}

// CHAT'S OWN OFFER IS THE SAME CARD, with its reason above the fields and the
// answers under them. A second card shape would be two things to learn and two
// places for the required fields to be marked differently (subharness.go).
func TestChatsOwnOfferOpensTheIntakeCardWithItsReason(t *testing.T) {
	a, _ := answeredApp(t)
	card := a.subPage.card
	if card == nil || !card.asked() || card.offer != 7 {
		t.Fatalf("the proposal did not open a card that can be answered: %+v", card)
	}
	screen := subScreen(a)
	for _, want := range []string{
		"flake-triage",
		"the brief and a failing test name are both here",
		"TestReconcilerRace",
		subRunWord,
		subNoChipWord,
		subSaysRun,
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the card never says %q:\n%s", want, screen)
		}
	}
	if got, want := len(plainOverlay(a)), a.overlayHeight(); got != want {
		t.Fatalf("the card drew %d lines into a block of %d", got, want)
	}
}

// AND THE PERSON CAN SEE THEY ARE BEING WAITED ON. The turn is technically
// working — the call that raised this is parked inside its batch — and what is
// true about it that a person can act on is that it is waiting for them.
func TestTheStatusLineSaysYourCallWhileTheOfferStands(t *testing.T) {
	a, _ := answeredApp(t)
	if !a.awaitingSubharness() {
		t.Fatal("a standing offer is not being waited on")
	}
	if word, _ := a.stateWord(); word != waitingWord {
		t.Fatalf("the status line read %q while a card was up", word)
	}
	drive(t, a, key("esc"))
	if a.awaitingSubharness() {
		t.Fatal("an answered offer is still being waited on")
	}
}

// ENTER ON THE ANSWERS RUNS IT, and the untouched card answers with NOTHING —
// nil is "as it was raised", and the engine builds the input from the card it
// sent rather than from this surface's second reading of it.
func TestEnterOnTheAnswersRunsWhatChatOffered(t *testing.T) {
	a, agent := answeredApp(t)
	if !a.subPage.card.running() {
		t.Fatal("a card with nothing left to fill in did not open on its answers")
	}
	drive(t, a, key("enter"))
	if len(agent.answers) != 1 {
		t.Fatalf("the offer was answered %d times", len(agent.answers))
	}
	answer := agent.answers[0]
	if answer.id != 7 || !answer.run {
		t.Fatalf("enter on `run it` answered %+v", answer)
	}
	if answer.input != nil {
		t.Fatalf("an untouched card sent its own reading of itself: %s", answer.input)
	}
	if agent.runs != 0 {
		t.Fatal("the surface launched the program itself instead of answering the card")
	}
	if a.subPage.open {
		t.Fatal("the card stayed up after it was answered")
	}
}

// ESC IS A NO AND NOT A WAY OUT. There is a turn waiting on this question, so
// the key that dismisses every other overlay answers this one.
func TestEscOnAnOfferAnswersNoRatherThanWalkingAway(t *testing.T) {
	a, agent := answeredApp(t)
	drive(t, a, key("esc"))
	if len(agent.answers) != 1 || agent.answers[0].run {
		t.Fatalf("esc on the card answered %+v", agent.answers)
	}
	if a.subPage.open {
		t.Fatal("the card stayed up after it was declined")
	}
}

// THE TWO DIGITS THE ROW DRAWS ANSWER FROM ANYWHERE ON THE CARD: a question
// somebody has read is one they may answer without first walking to the row.
func TestTheAnswerRowsDigitsAnswerFromAnywhereOnTheCard(t *testing.T) {
	for _, one := range []struct {
		key string
		run bool
	}{{"1", true}, {"0", false}} {
		a, agent := subApp(t, twoSubharnesses()...)
		a.designEvent(flakeProposal(t, agent, 3, "the test name is here"))
		if a.subPage.card.running() {
			t.Fatal("a card with a required blank opened on its answers")
		}
		drive(t, a, key(one.key))
		if len(agent.answers) != 1 || agent.answers[0].run != one.run {
			t.Fatalf("%q answered %+v", one.key, agent.answers)
		}
	}
}

// THE ANSWERS ARE WALKED SIDEWAYS, and the line under them says what the one
// under the cursor will do — which is how walking the row is a way of READING
// the question rather than guessing at it (pickrow.go).
func TestWalkingTheAnswersSaysWhatEachOneWillDo(t *testing.T) {
	a, agent := answeredApp(t)
	if got := subScreen(a); !strings.Contains(got, subSaysRun) {
		t.Fatalf("the card did not open on what running it does:\n%s", got)
	}
	drive(t, a, key("right"))
	screen := subScreen(a)
	if !strings.Contains(screen, subSaysNo) {
		t.Fatalf("walking to the no did not say what it does:\n%s", screen)
	}
	if strings.Contains(screen, subSaysRun) {
		t.Fatalf("both consequence lines were on screen at once:\n%s", screen)
	}
	drive(t, a, key("enter"))
	if len(agent.answers) != 1 || agent.answers[0].run {
		t.Fatalf("enter on the walked-to `no` answered %+v", agent.answers)
	}
}

// A FIELD SOMEBODY CHANGED TRAVELS WITH THE YES. What launches has to be what
// they confirmed rather than what was inferred for them.
func TestAFieldChangedOnTheOfferTravelsWithTheAnswer(t *testing.T) {
	a, agent := answeredApp(t)
	// Up one row from the answers is `branch`, which carries only its schema's
	// default — so what is typed here is the first thing on this card anybody
	// said, and the answer has to carry it.
	drive(t, a, key("up"))
	drive(t, a, key("enter"))
	for _, r := range "topic" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
	if !a.subPage.card.running() {
		t.Fatal("keeping the last blank did not move the cursor to the answers")
	}
	drive(t, a, key("enter"))
	if len(agent.answers) != 1 || !agent.answers[0].run {
		t.Fatalf("the edited card answered %+v", agent.answers)
	}
	if got := string(agent.answers[0].input); got != `{"test":"TestReconcilerRace","branch":"topic"}` {
		t.Fatalf("the answer carried %s", got)
	}
}

// A CARD NOBODY IS LISTENING TO COMES DOWN AND SAYS SO. The window bounds the
// tool call rather than the person, so it can fire while the card is still on
// screen — and a card left standing after it would be a `run it` that resolves
// nothing in silence.
func TestAWithdrawnOfferComesDownAndSaysNothingRan(t *testing.T) {
	a, agent := answeredApp(t)
	a.designEvent(session.Event{Kind: session.EventSubharnessProposalOff, ID: 7, Text: "flake-triage"})
	if a.subPage.open {
		t.Fatal("a withdrawn card stayed up")
	}
	if len(agent.answers) != 0 {
		t.Fatalf("a withdrawn card was answered on the person's behalf: %+v", agent.answers)
	}
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "flake-triage"+subOfferEndedWord) {
		t.Fatalf("nothing said the offer ended:\n%s", screen)
	}
}

// AND A WITHDRAWAL FOR SOMETHING ELSE LEAVES THE CARD ALONE.
func TestAWithdrawalForAnotherOfferLeavesTheCardUp(t *testing.T) {
	a, _ := answeredApp(t)
	a.designEvent(session.Event{Kind: session.EventSubharnessProposalOff, ID: 8, Text: "weekly-update"})
	if !a.subPage.open || a.subPage.card == nil {
		t.Fatal("a withdrawal about another card took this one down")
	}
}
