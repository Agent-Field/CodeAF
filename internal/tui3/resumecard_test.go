package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/inventory"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

func webResume() machine.Resume {
	return machine.Resume{
		From:    "blackmac",
		Missing: []inventory.Withheld{{Path: "web/node_modules", Lock: "web/package-lock.json"}, {Path: ".venv", Lock: "uv.lock"}},
		Stopped: []inventory.Running{{Command: "npm run dev", Cwd: "web", Ports: []int{3000}}},
		Lacks:   []machine.Item{{Bucket: machine.Installable, Name: "node", Want: "v22.4.0"}},
	}
}

func movedApp(t *testing.T) (*app, *setupAgent) {
	t.Helper()
	a, agent := machineApp(t, machine.Item{Bucket: machine.Installable, Name: "node"})
	agent.report.Resume = webResume()
	return a, agent
}

func askedCard(t *testing.T, a *app) questionShown {
	t.Helper()
	head, ok := a.questionHead()
	if !ok || head.question.Kind != resumeKind {
		t.Fatalf("no setup card is up: %+v (%v)", head.question.Kind, ok)
	}
	return head
}

func TestTheSetupCardAsksWhatTheAgentWillBeTold(t *testing.T) {
	a, _ := movedApp(t)
	a.offerSetup(webResume())
	card := askedCard(t, a)
	q := card.question
	if q.Head != "Set this machine up like blackmac had it?" || q.Ask != session.AskConfirmation || q.Stakes != session.StakesReversible {
		t.Fatalf("card %+v", q)
	}
	for _, want := range []string{"not brought along: web/node_modules (npm ci), .venv (uv sync)", "was running there: npm run dev in web/ (port 3000)", "also needed: needs node 22.4 — agent can set it up"} {
		if !strings.Contains(q.Reason, want) {
			t.Errorf("the card does not say %q:\n%s", want, q.Reason)
		}
	}
	if len(q.Options) != 2 || q.Options[0].Label != "set up" || q.Options[1].Label != "not now" || !q.Options[1].Safe || card.pick != 1 {
		t.Fatalf("options %+v, cursor %d: the cursor starts on the answer that runs nothing", q.Options, card.pick)
	}
}

func TestNoOfferWhenNothingToRestore(t *testing.T) {
	a, _ := movedApp(t)
	a.offerSetup(machine.Resume{})
	if _, ok := a.questionHead(); ok {
		t.Fatal("a takeover with nothing to say raised a card")
	}
	if len(a.entries) != 0 {
		t.Fatalf("and wrote %d lines to the transcript", len(a.entries))
	}
}

func TestACapabilityThatCannotWorkIsAbsent(t *testing.T) {
	a, _ := sheetApp(t) // an agent with no machine of its own
	a.offerSetup(webResume())
	if _, ok := a.questionHead(); ok {
		t.Fatal("a conversation with no setup door was offered one")
	}
}

func TestNoRunsWithoutYes(t *testing.T) {
	a, agent := movedApp(t)
	a.offerSetup(webResume())
	card := askedCard(t, a)
	if cmd := card.local(session.Answer{Key: resumeNotNowKey}); cmd != nil {
		t.Fatal("`not now` started something")
	}
	if agent.runs != 0 {
		t.Fatalf("`not now` ran %d setup turns", agent.runs)
	}
	if got := lastNote(t, a); got != chatlist.SetupLater {
		t.Fatalf("said %q, want %q", got, chatlist.SetupLater)
	}
	// Escape is the safe answer, which is the same answer.
	a.offerSetup(webResume())
	if cmd := askedCard(t, a).local(session.Answer{}); cmd != nil || agent.runs != 0 {
		t.Fatalf("an answer that is not `set up` ran something: %d turns", agent.runs)
	}
}

func TestSetUpStartsTheSetupTurn(t *testing.T) {
	a, agent := movedApp(t)
	a.offerSetup(webResume())
	cmd := askedCard(t, a).local(session.Answer{Key: resumeSetUpKey})
	runFirst(settledCmd(t, a, cmd))
	if agent.runs != 1 {
		t.Fatalf("`set up` started %d setup turns, want 1", agent.runs)
	}
}

func TestSlashSetupShowsWhatWasNotBroughtAlong(t *testing.T) {
	a, _ := movedApp(t)
	settleDoor(t, a, a.slash("/setup"))
	var all []string
	for _, e := range a.entries {
		all = append(all, e.text)
	}
	text := strings.Join(all, "\n")
	for _, want := range []string{"not brought along: web/node_modules (npm ci), .venv (uv sync)", "was running there: npm run dev in web/ (port 3000)"} {
		if !strings.Contains(text, want) {
			t.Errorf("/setup does not say %q:\n%s", want, text)
		}
	}
}

// Nothing a person reads on the card names the machinery behind it.
func TestResumeWordsHaveNoMachineryVocabulary(t *testing.T) {
	r := webResume()
	person := []string{chatlist.SetupHead(r.From), chatlist.SetupHead(""), chatlist.OfferSetUp, chatlist.OfferNotNow, chatlist.SetupLater}
	person = append(person, chatlist.SetupReasons(setupFacts(r))...)
	for _, line := range person {
		for _, banned := range []string{"record", "inventory", "seal", "lease", "brief", "cell", "verdict", "auditor"} {
			if strings.Contains(strings.ToLower(line), banned) {
				t.Errorf("%q says %q", line, banned)
			}
		}
	}
	if got := chatlist.SetupHead(""); got != "Set this machine up like it was?" {
		t.Fatalf("an unknown device reads %q", got)
	}
}

func TestALongListEndsWithHowManyMore(t *testing.T) {
	got := chatlist.SetupReasons(chatlist.SetupFacts{Missing: []string{"a", "b", "c", "d", "e"}})
	if len(got) != 1 || got[0] != "not brought along: a, b, c and 2 more" {
		t.Fatalf("reasons %v", got)
	}
	if chatlist.SetupReasons(chatlist.SetupFacts{}) != nil {
		t.Fatal("a line was said about nothing")
	}
}

func TestTheLastTestRunIsSaidOnlyWhenOneWasRecorded(t *testing.T) {
	if got := testsLine(machine.Resume{}); got != "" {
		t.Fatalf("a chat with no run said %q", got)
	}
	cases := map[string]inventory.TestRun{
		"last tests: passed":   {Passed: true},
		"last tests: failed 3": {Failed: 3},
		"last tests: failed":   {},
	}
	for want, run := range cases {
		run := run
		reasons := chatlist.SetupReasons(setupFacts(machine.Resume{Tests: &run}))
		if len(reasons) != 1 || reasons[0] != want {
			t.Errorf("run %+v said %v, want %q", run, reasons, want)
		}
	}
}
