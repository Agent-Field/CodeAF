package session

import (
	"strings"
	"testing"
	"time"
)

// THE PERSON ADDS NOTHING, AND THIS IS THE PART A BUILD FAILS ON.
//
// [Person] exists so that an interactive session cannot tell that any of this
// arrived. That is a claim about behaviour, so it is written down as behaviour:
// a golden table of every end-of-turn a session can reach, and the answer the
// engine gave to each one BEFORE the interface existed, spelled out here rather
// than derived from the implementation under test.
//
// THE GOLDEN RULE IS [Agent.readRemains]'S OWN, verbatim: a reader with
// something to say re-opens the turn on exactly that line, and an empty reading
// ends the turn. Nothing else was ever consulted, so every row below carries
// landings, checks and an acceptance that would change a [Steward]'s mind and
// must change nothing here.
func TestThePersonDecidesExactlyWhatTheEngineDecidedBefore(t *testing.T) {
	// The evidence a Steward would act on, in the shapes that most want acting
	// on: work that did not finish, a tree that does not build, a session that
	// finished nothing at all.
	loaded := Remains{
		Acceptance: "every fixture parses and `go build ./...` passes",
		Landings: []Landing{
			{ID: 1, Title: "port the parser", State: TaskFailed, Report: "incomplete", Signature: "incomplete"},
		},
		Checks: []CheckRun{{Command: "go build ./...", Passed: false}},
	}

	for _, c := range []struct {
		what   string
		in     Remains
		verb   DecisionVerb
		brief  string
		reason string
	}{
		{what: "a reader with nothing to say ends the turn",
			in: Remains{}, verb: DecideDone},
		{what: "a reader with a line re-opens on exactly that line",
			in: Remains{Reader: "the handlers are still unwired"}, verb: DecideCarryOn, brief: "the handlers are still unwired"},
		{what: "whitespace is not a line",
			in: Remains{Reader: "   \n "}, verb: DecideDone},
		{what: "a line is trimmed, as the reader's own clip already trimmed it",
			in: Remains{Reader: "  finish the writer  "}, verb: DecideCarryOn, brief: "finish the writer"},
		{what: "a landing that did not finish changes nothing for a person",
			in: withReader(loaded, ""), verb: DecideDone},
		{what: "a tree that does not build changes nothing for a person",
			in: withReader(loaded, "the handlers are still unwired"), verb: DecideCarryOn, brief: "the handlers are still unwired"},
		{what: "a session that landed nothing changes nothing for a person",
			in: Remains{Said: "I think the plan is sound.", Acceptance: "every fixture parses"}, verb: DecideDone},
	} {
		got := NewPerson().Decide(c.in)
		if got.Verb != c.verb || got.Brief != c.brief || got.Reason != c.reason {
			t.Fatalf("%s:\n got %+v\nwant {Verb:%s Brief:%q Reason:%q}", c.what, got, c.verb, c.brief, c.reason)
		}
	}
}

func withReader(remains Remains, reader string) Remains {
	remains.Reader = reader
	return remains
}

// AND THE OTHER FOUR ANSWERS ARE EMPTY, WHICH IS THE CONTRACT AND NOT A STUB.
//
// A person holds their own acceptance, spends against their own judgement, and
// decides for themselves what to do about a landing. Each of these emptinesses
// is what makes an existing road behave exactly as it did: an empty Acceptance
// is what keeps the terminal audit off an attended session, an unset Budget is
// what keeps a person's session from ever being stopped by one, and an empty
// Report is what keeps the landing note addressed to them.
func TestThePersonHoldsNoAcceptanceNoBudgetAndStartsNothing(t *testing.T) {
	person := NewPerson()
	if person.Acceptance() != "" {
		t.Fatalf("a person was given an acceptance: %q", person.Acceptance())
	}
	if budget := person.Budget(); budget.Set() {
		t.Fatalf("a person was given a budget: %+v", budget)
	}
	for _, state := range []TaskState{TaskDone, TaskFailed, TaskUnverified} {
		if brief := person.Report(Landing{ID: 1, State: state, Report: "incomplete — the router has no PATCH", Signature: "x"}); brief != "" {
			t.Fatalf("a person's session started work on a %s landing: %q", state, brief)
		}
	}
}

// THE LANDING NOTE A PERSON READS IS THE NOTE THEY ALWAYS READ, word for word.
// It is the one sentence in this package that was measured being a dead end,
// so it is pinned in both directions: present for a person, absent for anybody
// else.
func TestThePersonsLandingNoteIsUnchanged(t *testing.T) {
	notice := TaskNotice{
		ID: 4, Title: "wire the handlers", State: TaskFailed,
		Report: incompleteLead + "the request router still has no route for PATCH",
	}
	note := taskNote(notice, "", TaskSettleAsk, landingAddress{person: true})
	const offered = "what is missing is above and the branch is kept: " +
		"offer them a follow-up in their own words before anything else is spent on it"
	if !strings.Contains(note, offered) {
		t.Fatalf("the person's own follow-up line has changed:\n%s", note)
	}
}

// AND AN UNATTENDED SESSION WITH NOTHING LEFT TO SAY SAYS SO, rather than
// borrowing that sentence for a room with nobody in it.
func TestAGoalOwnerThatHasStoppedOffersNobodyAFollowUp(t *testing.T) {
	notice := TaskNotice{
		ID: 4, Title: "wire the handlers", State: TaskFailed,
		Report: incompleteLead + "the request router still has no route for PATCH",
	}
	note := taskNote(notice, "", TaskSettleAuto, landingAddress{})
	if strings.Contains(note, "offer them a follow-up") {
		t.Fatalf("a room with nobody in it was offered a follow-up:\n%s", note)
	}
	if !strings.Contains(note, "nothing more is being started on it") {
		t.Fatalf("the note does not say the run is finished with it:\n%s", note)
	}
}

// A SESSION NOBODY CONFIGURED IS A PERSON'S SESSION. Every test in this package
// that builds an agent by hand, every headless door, every node's own worker:
// none of them says anything about a principal, and all of them must get the
// posture they had before this field existed.
func TestASessionThatSaysNothingWorksForAPerson(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, person := agent.who().(*Person); !person {
		t.Fatalf("a plain session was given something other than a person: %T", agent.who())
	}
	if agent.steward() != nil {
		t.Fatal("a plain session was given a goal owner that spends")
	}
	if agent.who().Budget().Set() {
		t.Fatal("a plain session was given a budget")
	}
}

// AND AN AGENT WITH NO PRINCIPAL AT ALL — a struct a test assembled field by
// field, which this package does in several places — still answers a person
// rather than panicking on a nil interface.
func TestAnAgentWithNoPrincipalStillAnswersAPerson(t *testing.T) {
	var agent Agent
	if _, person := agent.who().(*Person); !person {
		t.Fatalf("an agent with no principal answered %T", agent.who())
	}
	if agent.steward() != nil {
		t.Fatal("an agent with no principal answered a goal owner")
	}
}

// THE ONE BUDGET WORD A PERSON EVER SEES IS THE ONE THEY TYPED.
func TestABudgetIsSpelledTheWayItWasTyped(t *testing.T) {
	for _, c := range []struct {
		budget Budget
		want   string
	}{
		{Budget{Wall: time.Hour}, "hour"},
		{Budget{Wall: 6 * time.Hour}, "6 hours"},
		{Budget{Wall: 30 * time.Minute}, "30 minutes"},
		{Budget{USD: 20}, "$20"},
		{Budget{USD: 12.5}, "$12.50"},
		{Budget{Wall: 6 * time.Hour, USD: 20}, "6 hours · $20"},
	} {
		if got := budgetWords(c.budget); got != c.want {
			t.Fatalf("budgetWords(%+v) = %q, want %q", c.budget, got, c.want)
		}
	}
}

// ONLY A CONVERSATION HAS A GOAL OWNER.
//
// Two roads copy the conversation's whole config to build a worker
// (standing_run.go), so an unattended session's rows travel to agents that must
// never have them: a node has a brief and an auditor, a fork's hand has a
// scope, an errand is a pane that closes with home. Each of them getting a
// Steward would mean a worker spending the session's budget, sweeping the
// session's files, and deciding for itself that the whole ask was met.
func TestOnlyAConversationIsGivenAGoalOwner(t *testing.T) {
	unattended := func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: 6 * time.Hour}
	}
	for _, c := range []struct {
		what   string
		mutate func(*Config)
	}{
		{"a task node", func(c *Config) { unattended(c); c.InTask = true }},
		{"an errand", func(c *Config) { unattended(c); c.Errand = true }},
		{"a fork's hand", func(c *Config) { unattended(c); c.inHand = true }},
	} {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, c.mutate)
		if agent.steward() != nil {
			t.Fatalf("%s was given a goal owner of its own", c.what)
		}
	}
	// And the conversation itself still is, which is the other half of the same
	// assertion: a guard that answered no to everything would be this feature
	// switched off.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, unattended)
	if agent.steward() == nil {
		t.Fatal("the conversation was refused a goal owner too")
	}
}
