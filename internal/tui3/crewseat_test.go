package tui3

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// A CREW OLDER THAN THE WORK SEAT, MET IN THE CONVERSATION (#312).
//
// #311 gave the headless doors the rung and the line. This surface had neither:
// a profile written before the worker row existed (#278) handed every task
// started from the thread to the build's own worker model, and the thread, the
// rail and the header all said nothing — on the surface where most tasks are
// started and where there is no `models:` line to carry the word.
//
// The line is the SAME line the run prints, said ONCE, at the moment work
// actually starts on the seat, and the /crew sheet says which of its rows was
// never written. Both halves are asserted here, and so is the silence on every
// profile the line is not about.

// crewSeatLab is a surface with a profile of a stated vintage behind it.
func crewSeatLab(t *testing.T, rows map[string]string) *app {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(dir), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(&fakeAgent{model: "openai/gpt-4.1-mini"})
	a.profileDir = dir
	// The app this suite builds has the question pinned as already asked
	// (tui3_test.go says why); this one has a profile of a stated vintage and is
	// asking it on purpose.
	a.workSeatSaid = false
	return a
}

// preSeatCrew is the shape a crew applied before the worker row existed has on
// disk: the four rows there were, and no fifth key.
var preSeatCrew = map[string]string{
	config.KeyTierReflexModel:     "vendor/pinned-reflex",
	config.KeyTierLowModel:        "vendor/pinned-small-work",
	config.KeyTierHighModel:       "vendor/pinned-careful",
	config.KeyTierMastermindModel: "vendor/pinned-thinking",
}

// THE LINE IS SAID ONCE, WHEN THE FIRST NODE STARTS WORKING.
//
// Not at the launch — a person who hands nothing off never meets this seat — and
// not per task, which is the noise the headless half refused for the same
// reason: a warning that arrives with all of a run's work is one somebody learns
// to read past, which leaves them exactly where the silence did.
func TestTheInheritedWorkSeatIsSaidOnceInTheThread(t *testing.T) {
	a := crewSeatLab(t, preSeatCrew)
	notice := config.TierSeatAt(a.profileDir, config.ModelTierWorker).Notice()
	if notice == "" {
		t.Fatal("the fixture's work seat is not inherited, so this test is about nothing")
	}

	if got := notesSaying(a, notice); got != 0 {
		t.Fatalf("the line was said %d times before any work started", got)
	}
	a.taskUpdate(oneRunningNode(41, "widening the sluice"))
	if got := notesSaying(a, notice); got != 1 {
		t.Fatalf("the first running node said the line %d times, want once", got)
	}
	// A second task, a second node of the same task, and the same node updating
	// again are all the same seat spending the same money.
	a.taskUpdate(oneRunningNode(42, "porting the parser"))
	a.taskUpdate(oneRunningNode(41, "widening the sluice"))
	if got := notesSaying(a, notice); got != 1 {
		t.Fatalf("two task starts said the line %d times, want once for the session", got)
	}

	// AND IT IS THE RUN'S OWN SENTENCE, so a person meets one voice on either
	// surface (internal/config's seats.go).
	if !strings.Contains(notice, "your crew was set before the work seat existed") {
		t.Fatalf("the line has been respelled: %q", notice)
	}
}

// AND EVERY OTHER SHAPE HEARS NOTHING. The line is a fact about a profile older
// than a seat, not decoration on a task starting: a row somebody pinned, a row
// they cleared on purpose and a profile that has said nothing are all answers,
// and none of them is this one.
func TestNoOtherProfileIsToldItsWorkSeatWasInherited(t *testing.T) {
	for name, rows := range map[string]map[string]string{
		"a pinned worker row": {
			config.KeyTierLowModel:    "vendor/pinned-small-work",
			config.KeyTierWorkerModel: "vendor/pinned-worker",
		},
		"a worker row cleared on purpose": {
			config.KeyTierLowModel:    "vendor/pinned-small-work",
			config.KeyTierWorkerModel: "",
		},
		"a profile that has said nothing": {},
	} {
		t.Run(name, func(t *testing.T) {
			a := crewSeatLab(t, rows)
			a.taskUpdate(oneRunningNode(41, "widening the sluice"))
			for _, entry := range a.entries {
				if entry.kind == entryNote && strings.Contains(entry.text, "your crew was set before") {
					t.Fatalf("%s was told about an inheritance that did not happen: %q", name, entry.text)
				}
			}
			if line := a.crewInheritedLine(); line != "" {
				t.Fatalf("%s: the crew sheet claims a row it did not inherit: %q", name, line)
			}
		})
	}
}

// A CONNECTION READS NOBODY'S SEATS. Over --host the crew lives on the far
// machine and this window's profile is the laptop's, so a line built from it
// would be a fact about the wrong computer — which is the refusal /crew itself
// opens with ([app.runCrew]).
func TestAConnectedSurfaceSaysNothingAboutTheFarMachinesSeats(t *testing.T) {
	a := crewSeatLab(t, preSeatCrew)
	a.host = "elsewhere"
	a.taskUpdate(oneRunningNode(41, "widening the sluice"))
	if seat := a.workSeat(); seat.Model != "" || seat.Source != "" {
		t.Fatalf("a connected window seated %q (%s) off the laptop's profile", seat.Model, seat.Source)
	}
	if line := a.crewInheritedLine(); line != "" {
		t.Fatalf("a connected window drew %q on the crew sheet", line)
	}
	for _, entry := range a.entries {
		if entry.kind == entryNote && strings.Contains(entry.text, "your crew was set before") {
			t.Fatalf("a connected window said %q about the far machine's work", entry.text)
		}
	}
}

// THE /crew SHEET SAYS WHICH ROW IS NOT YOURS, at every width the sheet is
// drawn at. A receipt in the thread is the moment; the sheet is where somebody
// goes to check afterwards, and a sheet that showed the inherited row as though
// the person had pinned it would be the crew word disagreeing with the work.
func TestTheCrewSheetShowsAnInheritedWorkSeatAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120} {
		a := crewSeatLab(t, preSeatCrew)
		a.width, a.height = width, 30
		a.pal = newPalette(tokens.ANSI256, false)
		a.runCrew("")
		if !a.crewPick.open {
			t.Fatalf("%d columns: /crew opened no chooser", width)
		}
		screen := plain(frame(a))
		if !strings.Contains(screen, "inherited") {
			t.Fatalf("%d columns: the crew sheet does not say the work row was inherited:\n%s", width, screen)
		}
		if !strings.Contains(screen, "small work") {
			t.Fatalf("%d columns: the sheet does not name the row a person would go and find:\n%s", width, screen)
		}
		// The chooser's height and its rows are one arithmetic: a line the rows
		// draw and the height does not count is a line that eats the row above it.
		if got, want := len(a.crewPick.rows(width, a.crewPick.height(), a.pal, -1, a)), a.crewPick.height(); got != want {
			t.Fatalf("%d columns: the chooser drew %d rows and promised %d", width, got, want)
		}
	}
}

// AND PICKING A CREW ENDS IT, which is the promise the line makes. Every preset
// writes all five rows, so the row that was inherited is a row the person wrote
// from that moment on, and neither surface says anything more about it.
func TestPickingACrewWritesTheRowThatWasInherited(t *testing.T) {
	a := crewSeatLab(t, preSeatCrew)
	if a.crewInheritedLine() == "" {
		t.Fatal("the fixture's work seat is not inherited, so this test is about nothing")
	}
	a.applyCrew(config.CrewFrugal)
	if seat := a.workSeat(); seat.Source != config.SeatCrew {
		t.Fatalf("after /crew frugal the work seat reads %q (%s)", seat.Model, seat.Source)
	}
	if line := a.crewInheritedLine(); line != "" {
		t.Fatalf("the crew sheet still claims an inherited row: %q", line)
	}
	if notice := a.workSeat().Notice(); notice != "" {
		t.Fatalf("the thread would still say %q", notice)
	}
}

// THE SEAT THE CONVERSATION READS IS THE SEAT ITS TASKS RUN ON. The role map the
// door builds resolves the worker row through the same call this surface asks
// for its line, so the sentence and the work cannot be about two models
// (cmd/aforge's v3Crew, and internal/config's TestEveryReadOfATierRowGoesThroughTheLadder).
func TestTheWorkSeatTheSheetNamesIsTheModelTheWorkerRowResolvesTo(t *testing.T) {
	a := crewSeatLab(t, preSeatCrew)
	seat := a.workSeat()
	if got := config.TierModelAt(a.profileDir, config.ModelTierWorker); got != seat.Model {
		t.Fatalf("the work seat says %q and the worker row resolves to %q", seat.Model, got)
	}
	if seat.Model != preSeatCrew[config.KeyTierLowModel] {
		t.Fatalf("the work seat runs on %q, want the small-work model the person pinned", seat.Model)
	}
	if seat.Model == config.DefaultWorkerModel {
		t.Fatalf("the work seat fell to the build's default with a whole crew written above it")
	}
}

// ── the flag that overrides the crew ────────────────────────────────────────
//
// THE STATUS LINE NAMES WHAT SEATS THE CALL (#444).
//
// A canary run under `--one-model` on exactly the profile above read `crew
// custom` for its whole life and, when the first task started, was told its crew
// was set before the work seat existed and was running on its small work model
// "until you pick a crew again". Both sentences are true about the file on disk
// and false about the run: the flag empties the roles source and the task model
// at the door, so the four rows seat nothing and picking a crew would have
// changed nothing either.

// oneModelSeatLab is [crewSeatLab] with the launch's flag on, which is the one
// difference between the two halves below.
func oneModelSeatLab(t *testing.T, rows map[string]string) *app {
	t.Helper()
	a := crewSeatLab(t, rows)
	a.oneModel = true
	return a
}

// UNDER THE FLAG EVERY CREW SURFACE NAMES THE FLAG, AND NO SEAT IS PROMISED.
//
// The five readings come through one function on purpose (crew.go's
// [app.crewReading]), so this asserts all five: if one of them ever grows a
// second source, this is where it says so.
func TestUnderOneModelTheCrewSurfacesNameTheFlagAndNoSeatReceiptIsPosted(t *testing.T) {
	a := oneModelSeatLab(t, preSeatCrew)
	a.model = "deepseek/deepseek-v4-flash"

	if got := a.crewSegment(); got != crewOneModelSegment {
		t.Errorf("the status line's crew segment reads %q, want %q", got, crewOneModelSegment)
	}
	if got := a.crewWord(); got != crewOneModelWord {
		t.Errorf("the page's crew word reads %q, want %q", got, crewOneModelWord)
	}
	if a.crewHint() != a.crewSegment() {
		t.Errorf("the picker's hint says %q and the segment says %q", a.crewHint(), a.crewSegment())
	}
	if got := a.welcomeModelLine(); !strings.Contains(got, crewOneModelSegment) || strings.Contains(got, "crew") {
		t.Errorf("the welcome line reads %q, want the flag beside the model and no preset", got)
	}
	// AND NOT ONE OF THEM READS THE PRESET the four rows still derive to. The
	// rows are untouched on disk — that is the flag's own promise — so the word
	// they make is the one thing this run must not print.
	a.slash("/status")
	page := lastNote(t, a)
	if !strings.Contains(page, "\ncrew     "+crewOneModelWord) {
		t.Errorf("/status does not read the same word:\n%s", page)
	}
	if strings.Contains(page, config.CrewCustom) {
		t.Errorf("/status names the crew the flag overrode:\n%s", page)
	}
	if line := plain(a.status(200)); strings.Contains(line, "crew "+config.CrewCustom) {
		t.Errorf("the status line still draws the overridden crew:\n%q", line)
	}

	// AND THE RECEIPT IS NOT OWED. It reports a SUBSTITUTION, and the flag is
	// the person's own answer to the question it asks.
	a.taskUpdate(oneRunningNode(41, "widening the sluice"))
	for _, entry := range a.entries {
		if entry.kind == entryNote && strings.Contains(entry.text, "your crew was set before") {
			t.Fatalf("a run under --one-model was promised a crew that seats nothing: %q", entry.text)
		}
	}
	if seat := a.workSeat(); seat.Model != "" || seat.Source != "" {
		t.Errorf("the flag left a work seat of %q (%s)", seat.Model, seat.Source)
	}
	if line := a.crewInheritedLine(); line != "" {
		t.Errorf("the /crew sheet claims an inherited row under the flag: %q", line)
	}
}

// AND WITHOUT THE FLAG THE SAME PROFILE IS TOLD EXACTLY WHAT IT WAS BEFORE.
// The #311/#314 behaviour is not what was wrong here, and this is the half that
// says so: one profile, one difference, two readings.
func TestWithoutOneModelTheSameProfileStillDrawsItsCrewAndSaysTheSeatOnce(t *testing.T) {
	a := crewSeatLab(t, preSeatCrew)
	a.model = "deepseek/deepseek-v4-flash"

	if got, want := a.crewSegment(), "crew "+config.CrewCustom; got != want {
		t.Errorf("the status line's crew segment reads %q, want %q", got, want)
	}
	if got := a.crewWord(); !strings.HasPrefix(got, config.CrewCustom+" ·") {
		t.Errorf("the page's crew word reads %q, want the preset the four rows derive to", got)
	}
	if strings.Contains(a.crewWord(), crewOneModelSegment) {
		t.Errorf("a launch that never typed the flag reads %q", a.crewWord())
	}

	a.taskUpdate(oneRunningNode(41, "widening the sluice"))
	notice := config.TierSeatAt(a.profileDir, config.ModelTierWorker).Notice()
	if notice == "" {
		t.Fatal("the fixture's work seat is not inherited, so this test is about nothing")
	}
	if got := notesSaying(a, notice); got != 1 {
		t.Fatalf("the receipt was said %d times, want once for the session", got)
	}
}
