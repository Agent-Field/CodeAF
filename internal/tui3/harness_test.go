package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// The harness panel and the strip's chip.
//
// Everything here drives the real surface against a real registry in a
// temporary directory: the panel's whole job is to say what is on disk, and a
// test that stubbed the store would be a test of the stub.

// harnessApp is a surface with a registry behind /harness.
func harnessApp(t *testing.T, entries ...subharness.Harness) (*app, *subharness.Store) {
	t.Helper()
	store := subharness.New(t.TempDir())
	for _, entry := range entries {
		if _, err := store.Save(entry); err != nil {
			t.Fatalf("seed %s: %v", entry.Name, err)
		}
	}
	a := newTestApp(&fakeAgent{})
	a.harn = store
	a.width = 100
	return a, store
}

func demoHarness(name, description string) subharness.Harness {
	return subharness.Harness{
		Name: name, Description: description,
		Tools:  []string{"read"},
		Verify: subharness.RungLoop,
		Program: []subharness.Node{
			{ID: "look", Kind: subharness.KindAgentLoop, Prompt: "look at it", Tools: []string{"read"}},
			{ID: "land", Kind: subharness.KindHumanGate, Prompt: "land it?", Escalate: true},
		},
	}
}

// EACH ROW IS THE MARK, THE NAME AND THE VERSION, with what it is for and what
// it has done beside it.
func TestTheHarnessPanelDrawsWhatIsRegistered(t *testing.T) {
	a, _ := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeLine(t, a, "/harness")
	if !a.harnPanel.open {
		t.Fatal("/harness opened nothing")
	}
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, glyphHarness+" triage-flake v1") {
		t.Fatalf("a row is not a mark, a name and a version:\n%s", screen)
	}
	if !strings.Contains(screen, "chase a flaky test") {
		t.Fatalf("a row does not say what it is for:\n%s", screen)
	}
	// A HARNESS THAT HAS NEVER RUN SAYS SO rather than drawing an empty column
	// where a timing would go.
	if !strings.Contains(screen, "never run") {
		t.Fatalf("a row does not say it has never run:\n%s", screen)
	}
	drive(t, a, key("esc"))
	if a.harnPanel.open {
		t.Fatal("esc did not close the panel")
	}
}

// A ROW THAT HAS RUN SAYS HOW OFTEN AND HOW IT WENT.
func TestTheHarnessPanelSaysWhatTheHistorySays(t *testing.T) {
	a, store := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	for _, status := range []subharness.Status{subharness.StatusOK, subharness.StatusDeclined} {
		if _, err := store.SaveRun(subharness.Run{
			Harness: "triage-flake", Version: 1, Status: status,
			Started: time.Now().Add(-time.Hour), Finished: time.Now().Add(-time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	typeLine(t, a, "/harness")
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, "2 runs") || !strings.Contains(screen, "last declined") {
		t.Fatalf("the row does not carry the history:\n%s", screen)
	}
}

// ENTER PUTS THE CARD IN THE CONVERSATION, which is where prose lives on this
// surface, and gets the list out of the way.
func TestEnterOnAHarnessPrintsItsCard(t *testing.T) {
	a, _ := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeLine(t, a, "/harness")
	drive(t, a, key("enter"))

	if a.harnPanel.open {
		t.Fatal("the panel stayed up under the card it printed")
	}
	if len(a.entries) == 0 {
		t.Fatal("enter printed nothing")
	}
	card := a.entries[len(a.entries)-1].text
	for _, want := range []string{
		"triage-flake · v1", "1   agent.loop", "2   human.gate", "tools  read", "verify  loop",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card is missing %q:\n%s", want, card)
		}
	}
}

// The last run rides under the card, because "what is this" and "what did it do
// last time" are one question asked twice.
func TestTheCardCarriesTheLastRun(t *testing.T) {
	a, store := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	if _, err := store.SaveRun(subharness.Run{
		Harness: "triage-flake", Version: 1, Status: subharness.StatusIntervened,
		Started: time.Now(), Finished: time.Now(),
		Nodes: []subharness.Trace{
			{ID: "land", Node: "land", Kind: subharness.KindHumanGate, OK: true, Answer: "intervened"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	typeLine(t, a, "/harness")
	drive(t, a, key("enter"))
	card := a.entries[len(a.entries)-1].text
	if !strings.Contains(card, "last run") || !strings.Contains(card, "intervened") {
		t.Fatalf("the card does not carry what happened last time:\n%s", card)
	}
}

// AN EMPTY REGISTRY SAYS SO. It is a list with nothing in it, not an error and
// not a missing command.
func TestAnEmptyRegistrySaysSo(t *testing.T) {
	a, _ := harnessApp(t)
	typeLine(t, a, "/harness")
	if !a.harnPanel.open {
		t.Fatal("/harness opened nothing on an empty registry")
	}
	if screen := strings.Join(plainOverlay(a), "\n"); !strings.Contains(screen, noHarnessWord) {
		t.Fatalf("the empty list does not say it is empty:\n%s", screen)
	}
}

// A SURFACE WITH NO REGISTRY SAYS SO rather than opening a list it cannot fill.
func TestNoRegistryIsSaidOutLoud(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	typeLine(t, a, "/harness")
	if a.harnPanel.open {
		t.Fatal("a surface with no registry opened a panel anyway")
	}
	if got := a.entries[len(a.entries)-1].text; got != harnessUnavailableWord {
		t.Fatalf("the surface said %q", got)
	}
}

// /harness IS ON THE COMMAND LIST AND IN /help, which is one table read twice.
func TestHarnessIsOnTheCommandList(t *testing.T) {
	found := false
	for _, one := range commands {
		if one.name == "harness" {
			found = true
		}
	}
	if !found {
		t.Fatal("/harness is not on the command list")
	}
	if !strings.Contains(helpText(""), "/harness") {
		t.Fatal("/harness is not in /help")
	}
	if canonicalCommand("harnesses") != "harness" {
		t.Fatalf("/harnesses does not reach /harness")
	}
}

// ── the strip ───────────────────────────────────────────────────────────────

// runningAgent is a surface's agent that is executing a harness.
type runningAgent struct {
	*fakeAgent
	name string
}

func (r *runningAgent) RunningHarness() (string, bool) { return r.name, r.name != "" }

// A RUNNING HARNESS RAISES THE STRIP AND LEADS IT. Nothing else on the surface
// would say so: the run happens inside one tool call, so the transcript shows a
// row that has not come back yet and nothing more.
func TestTheStripCarriesTheRunningHarness(t *testing.T) {
	a := newTestApp(&runningAgent{fakeAgent: &fakeAgent{}, name: "triage-flake"})
	a.width, a.height = 100, 30
	a.harn = subharness.New(t.TempDir())

	if !a.stripShowing() {
		t.Fatal("a running harness did not raise the strip")
	}
	row := plain(a.stripRow(a.width))
	if !strings.Contains(row, "triage-flake") {
		t.Fatalf("the strip does not name the harness: %q", row)
	}
	if a.stripHarn.to <= a.stripHarn.from {
		t.Fatalf("the chip recorded no columns to be pressed: %+v", a.stripHarn)
	}
	// AND THE CHIP IS A DOOR: pressing it opens the registry, which is the only
	// place a run can be looked at.
	if _, took := a.stripPress(a.stripHarn.from+1, a.headHeight()); !took {
		t.Fatal("the strip did not take the press")
	}
	if !a.harnPanel.open {
		t.Fatal("pressing the harness chip did not open the panel")
	}
}

// AND IT GOES AWAY when the run does — the strip is the live set, never a
// permanent bar.
func TestTheStripDropsTheHarnessWhenItEnds(t *testing.T) {
	agent := &runningAgent{fakeAgent: &fakeAgent{}, name: "triage-flake"}
	a := newTestApp(agent)
	a.width, a.height = 100, 30
	if !a.stripShowing() {
		t.Fatal("a running harness did not raise the strip")
	}
	agent.name = ""
	if a.stripShowing() {
		t.Fatal("the strip stayed up after the run ended")
	}
	if row := a.stripRow(a.width); row != "" {
		t.Fatalf("the strip drew a row for nothing: %q", row)
	}
}

// ── the third answer ────────────────────────────────────────────────────────

// gateAgent is a surface's agent that records how a harness gate was answered.
type gateAgent struct {
	*fakeAgent
	answers []session.GateChoice
}

func (g *gateAgent) ResolveHarnessGate(_ uint64, choice session.GateChoice, _ string) {
	g.answers = append(g.answers, choice)
}

// A GATE THAT OFFERS TO BE TAKEN OVER DRAWS THE THIRD KEY, and pressing it
// answers with the third answer rather than with a yes or a no.
func TestAGateOffersTheThirdAnswer(t *testing.T) {
	agent := &gateAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("harness", "harness run triage-flake"),
		{
			Kind: session.EventConsentRequest, ID: 7, Tool: "harness",
			Hint: "triage-flake · land the fix?", Rule: "harness gate — triage-flake",
			Escalate: true,
		},
	}}}}
	a := newTestApp(agent)
	a.width = 100
	typeLine(t, a, "run triage-flake")
	if !a.asking() {
		t.Fatal("the gate raised no question")
	}
	if offer := plain(a.consentOffer(a.width)); !strings.Contains(offer, "["+consentTake+"] I'll take it") {
		t.Fatalf("the offer does not carry the third answer: %q", offer)
	}
	drive(t, a, key(consentTake))
	if len(agent.answers) != 1 || agent.answers[0] != session.GateIntervene {
		t.Fatalf("the third key answered %v", agent.answers)
	}
	if a.asking() {
		t.Fatal("the question stayed up after it was answered")
	}
}

// AN ORDINARY TOOL QUESTION DOES NOT, because there is no run to take over —
// and an offer that is inert is worse than a missing key.
func TestAnOrdinaryQuestionHasNoThirdAnswer(t *testing.T) {
	agent := &gateAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(8, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	}}}}
	a := newTestApp(agent)
	a.width = 100
	typeLine(t, a, "clean the tree")
	if offer := plain(a.consentOffer(a.width)); strings.Contains(offer, "["+consentTake+"]") {
		t.Fatalf("an ordinary question offered to be taken over: %q", offer)
	}
	drive(t, a, key(consentTake))
	if len(agent.answers) != 0 {
		t.Fatalf("the third key answered a question that never offered it: %v", agent.answers)
	}
	if !a.asking() {
		t.Fatal("a key that means nothing here answered the question anyway")
	}
}
