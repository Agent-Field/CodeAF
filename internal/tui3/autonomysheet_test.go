package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// autonomyTestAgent keeps this project's rules in memory and REFUSES THE TWO
// ROWS THE ENGINE'S OWN DOOR REFUSES, because that is what the surface is being
// tested against: it holds no copy of those laws any more and prints whatever
// the door says (internal/session's autonomy.go).
type autonomyTestAgent struct {
	*fakeAgent
	rules map[session.AskKind]session.Policy
}

func (a *autonomyTestAgent) Autonomy() map[session.AskKind]session.Policy { return a.rules }
func (a *autonomyTestAgent) SetAutonomy(kind session.AskKind, policy session.Policy) error {
	if kind == session.AskConfirmation && policy.Kind != session.PolicyAsk {
		return errors.New("confirmation is asked before something destructive · it always asks")
	}
	if kind == session.AskClarification && policy.Kind != session.PolicyAsk {
		return errors.New("clarification never runs on a clock · only you have that answer")
	}
	a.rules[kind] = policy
	return nil
}

func newAutonomyApp() *app {
	return newTestApp(&autonomyTestAgent{fakeAgent: &fakeAgent{}, rules: map[session.AskKind]session.Policy{}})
}

func TestAutonomyShowsAndChangesThisProjectsRules(t *testing.T) {
	a := newAutonomyApp()
	a.slash("/autonomy")
	text := plain(lastNote(t, a))
	for _, want := range []string{
		autonomyHeadWord, "permission", autonomyAskWord,
		autonomyAlwaysWord, autonomyNoClockWord, autonomyUsageWord,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("/autonomy lost %q:\n%s", want, text)
		}
	}
	a.slash("/autonomy choice recommend 28s")
	if got := plain(lastNote(t, a)); got != "choice · recommend, auto in 28s · for this project" {
		t.Fatalf("change = %q", got)
	}
	a.slash("/autonomy")
	if got := plain(lastNote(t, a)); !strings.Contains(got, "choice           recommend, auto in 28s") {
		t.Fatalf("the sheet did not read back what was stored:\n%s", got)
	}
}

func TestAutonomyPrintsTheEnginesRefusalForTheTwoRowsNobodyMayChange(t *testing.T) {
	a := newAutonomyApp()
	a.slash("/autonomy clarification recommend 5s")
	if got := plain(lastNote(t, a)); !strings.Contains(got, "never runs on a clock") {
		t.Fatalf("clarification = %q", got)
	}
	a.slash("/autonomy confirmation decide")
	if got := plain(lastNote(t, a)); !strings.Contains(got, "it always asks") {
		t.Fatalf("confirmation = %q", got)
	}
}

func TestAQuestionUnderAProjectRuleWearsItOnTheRow(t *testing.T) {
	a := newAutonomyApp()
	a.slash("/autonomy choice recommend 30s")
	q := deliveryQuestion(1, session.AskChoice, true)
	q.Policy = session.Policy{Kind: session.PolicyRecommendThenAuto, After: 30 * time.Second}
	q.Deadline = a.now().Add(28 * time.Second)
	a.raiseQuestion(questionShown{question: q})
	head, ok := a.questionHead()
	if !ok {
		t.Fatal("the question never reached the block")
	}
	word := a.questionClockWord(head)
	if !strings.Contains(word, "sqlite in 28s") || !strings.Contains(word, questionOwnRuleWord) {
		t.Fatalf("clock = %q, want the answer, the time left and %q", word, questionOwnRuleWord)
	}
}

func TestAQuestionWithNoProjectRuleSaysNothingAboutOne(t *testing.T) {
	a := newAutonomyApp()
	q := deliveryQuestion(1, session.AskChoice, true)
	q.Policy = session.Policy{Kind: session.PolicyRecommendThenAuto, After: 30 * time.Second}
	q.Deadline = a.now().Add(28 * time.Second)
	a.raiseQuestion(questionShown{question: q})
	head, _ := a.questionHead()
	if strings.Contains(a.questionClockWord(head), questionOwnRuleWord) {
		t.Fatal("a clock the question came with claimed to be the project's rule")
	}
}

func TestDWritesTheProjectRuleAndSaysSo(t *testing.T) {
	agent := &autonomyTestAgent{fakeAgent: &fakeAgent{}, rules: map[session.AskKind]session.Policy{}}
	a := newTestApp(agent)
	q := deliveryQuestion(1, session.AskChoice, true)
	a.raiseQuestion(questionShown{question: q})
	head, _ := a.questionHead()
	a.questionDial(head)
	if rule := agent.rules[session.AskChoice]; rule.Kind != session.PolicyDecide {
		t.Fatalf("D left this project's rule at %#v", rule)
	}
	if got := plain(lastNote(t, a)); !strings.Contains(got, autonomyDecideWord) ||
		!strings.Contains(got, "/autonomy") {
		t.Fatalf("D said %q, want the rule it wrote and how to change it", got)
	}
}
