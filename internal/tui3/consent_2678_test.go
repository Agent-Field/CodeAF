package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// Contract C5: The terminal keeps the refusal on the tool result without
// opening an approval card.
func TestProtectedDeletionIsShownAsARefusalWithoutAnApprovalCard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const command = "rm -rf /"
	decision := (approval.Policy{Default: approval.ActionAllow}).CheckBash(command)
	if !decision.Never {
		t.Fatalf("the protected deletion did not produce a hard refusal: %s", decision)
	}
	a, agent := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "bash", CallID: "c1", Hint: "bash " + command,
	}})
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolFailed, Tool: "bash", CallID: "c1", Output: decision.Rule, HarnessMade: true, Refused: true,
	}})
	if _, ok := a.questionHead(); ok {
		t.Fatal("the refusal opened an approval card")
	}
	found := false
	for _, e := range a.entries {
		if e.kind == entryTool && e.callID == "c1" {
			found = true
			if e.detail.Output != decision.Rule {
				t.Errorf("tool detail = %q, want %q", e.detail.Output, decision.Rule)
			}
		}
	}
	if !found {
		t.Error("the refusal left no tool row")
	}
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// #2678: A question graded as a critical command resolves its default pointer
// index to the deny option for both 2-row and 3-row cards, so an unaimed enter
// never allows or banks a critical command.
func TestCriticalCommandQuestionDefaultPointerIsDenyForTwoAndThreeRows_2678(t *testing.T) {
	twoRows := []session.AnswerOption{
		{Key: "1", Label: "allow once"},
		{Key: "3", Label: "deny", Safe: true},
	}
	threeRows := []session.AnswerOption{
		{Key: "1", Label: "allow once"},
		{Key: "2", Label: "always, this command", Widening: true},
		{Key: "3", Label: "deny", Safe: true},
	}
	for _, tc := range []struct {
		name     string
		options  []session.AnswerOption
		wantPick int
	}{
		{name: "two-row", options: twoRows, wantPick: 1},
		{name: "three-row", options: threeRows, wantPick: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := session.Question{
				ID:       7,
				Kind:     session.QuestionConsent,
				Ask:      session.AskPermission,
				Form:     session.FormCard,
				Asker:    session.Asker{Kind: session.AskerEngine},
				Head:     "needs your ok to run bash",
				Reason:   `critical command "rm -rf /*"`,
				Subject:  session.SubjectRef{Kind: session.SubjectCall, CallID: "c1", Name: "bash"},
				Options:  tc.options,
				Stakes:   session.StakesIrreversible,
				Blocking: session.Blocking{Turn: true},
			}
			got := questionPointerStart(q)
			if got != tc.wantPick || q.Options[got].Label != "deny" {
				t.Fatalf("questionPointerStart = %d (%+v), want %d (deny)", got, q.Options, tc.wantPick)
			}

			lab := newQuestionLab(t)
			lab.raise(q)
			lab.tick(questionSettle)
			lab.rows()

			head, ok := lab.a.questionHead()
			if !ok {
				t.Fatal("critical-command question is not on the block")
			}
			if head.pick != tc.wantPick || head.question.Options[head.pick].Label != "deny" {
				t.Fatalf("head.pick = %d (%q), want %d (deny)", head.pick, head.question.Options[head.pick].Label, tc.wantPick)
			}
			pointer := lab.a.icon(tokens.GPointer)
			if screen := lab.plain(); !strings.Contains(screen, pointer+" 3  deny") {
				t.Fatalf("rendered card does not place pointer on deny:\n%s", screen)
			}
			if !lab.press("enter") {
				t.Fatal("enter was not taken by the critical-command card")
			}
			if len(lab.answer) != 1 || lab.answer[0].FirstKey() != "3" {
				t.Fatalf("enter answered %+v, want key \"3\" (deny)", lab.answer)
			}
		})
	}
}

// #2678: Opening an approval card for a critical command reason puts the
// pointer on deny on both the model tool-call road and the ! composer door,
// while 1 or up+enter still allows once and 3 still denies.
func TestCriticalCommandApprovalCardOpensOnDenyForModelAndComposerDoors_2678(t *testing.T) {
	const (
		cmdText = "rm -rf /tmp/ws/tmp2"
		cmdArgs = `{"command":"rm -rf /tmp/ws/tmp2"}`
		rule    = `critical command "rm -rf /*"`
	)
	for _, road := range []struct {
		name   string
		line   string
		callID string
	}{
		{name: "model", line: "remove the scratch folder", callID: "call_1"},
		{name: "composer-bash", line: "!" + cmdText, callID: "user_bash_1"},
	} {
		t.Run(road.name, func(t *testing.T) {
			events := func() []session.Event {
				return []session.Event{
					{
						Kind: session.EventToolBegin, Tool: "bash", CallID: road.callID,
						Hint: "bash " + cmdText, Args: cmdArgs,
					},
					{
						Kind: session.EventConsentRequest, ID: 7, Tool: "bash", CallID: road.callID,
						Hint: "bash " + cmdText, Args: cmdArgs, Rule: rule, Memo: true,
					},
				}
			}

			// Bare enter denies.
			agent, a, saved := rememberingApp(t, events())
			a.width = 100
			typeLine(t, a, road.line)
			settleAsk(a)

			head := askHead(t, a)
			if head.question.Stakes != session.StakesIrreversible {
				t.Fatalf("critical command stakes = %q, want %q", head.question.Stakes, session.StakesIrreversible)
			}
			if head.pick < 0 || head.pick >= len(head.question.Options) || head.question.Options[head.pick].Label != "deny" {
				t.Fatalf("pointer opened on %d (%+v), want deny", head.pick, head.question.Options)
			}
			pointer := a.icon(tokens.GPointer)
			got := plain(frame(a))
			if !strings.Contains(got, pointer+" 3  deny") {
				t.Fatalf("card does not render pointer on 3 deny:\n%s", got)
			}
			if strings.Contains(got, "always") {
				t.Fatalf("critical command card still offers always:\n%s", got)
			}
			drive(t, a, key("enter"))
			if len(agent.answers) != 1 || agent.answers[0] != (answered{id: 7, allow: false, scope: session.ConsentOnce}) {
				t.Fatalf("enter resolved %+v, want deny once", agent.answers)
			}
			if len(saved.tools)+len(saved.commands) != 0 {
				t.Fatalf("enter banked a rule on a critical command: %+v", saved)
			}

			// Deliberate up + enter allows once.
			agent, a, _ = rememberingApp(t, events())
			a.width = 100
			typeLine(t, a, road.line)
			settleAsk(a)
			drive(t, a, key("up"), key("enter"))
			if len(agent.answers) != 1 || agent.answers[0] != (answered{id: 7, allow: true, scope: session.ConsentOnce}) {
				t.Fatalf("up then enter resolved %+v, want allow once", agent.answers)
			}

			// Deliberate 1 allows once; deliberate 3 denies.
			agent, a, _ = rememberingApp(t, events())
			a.width = 100
			typeLine(t, a, road.line)
			settleAsk(a)
			drive(t, a, key("1"))
			if len(agent.answers) != 1 || agent.answers[0] != (answered{id: 7, allow: true, scope: session.ConsentOnce}) {
				t.Fatalf("[1] resolved %+v, want allow once", agent.answers)
			}

			agent, a, _ = rememberingApp(t, events())
			a.width = 100
			typeLine(t, a, road.line)
			settleAsk(a)
			drive(t, a, key("3"))
			if len(agent.answers) != 1 || agent.answers[0] != (answered{id: 7, allow: false, scope: session.ConsentOnce}) {
				t.Fatalf("[3] resolved %+v, want deny", agent.answers)
			}
		})
	}
}

// #2678: Control test — a non-critical approval card still opens with the
// pointer on the first answer (1 allow once).
func TestNonCriticalApprovalCardDefaultsToFirstAnswer_2678(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(7, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	a.width = 100
	typeLine(t, a, "clean the build dir")
	settleAsk(a)

	head := askHead(t, a)
	if head.pick != 0 || head.question.Options[head.pick].Label != "allow once" {
		t.Fatalf("non-critical pointer opened on %d (%+v), want 0 (allow once)", head.pick, head.question.Options)
	}
	pointer := a.icon(tokens.GPointer)
	if got := plain(frame(a)); !strings.Contains(got, pointer+" 1  allow once") {
		t.Fatalf("non-critical card does not render pointer on 1 allow once:\n%s", got)
	}
	drive(t, a, key("enter"))
	if len(agent.answers) != 1 || agent.answers[0] != (answered{id: 7, allow: true, scope: session.ConsentOnce}) {
		t.Fatalf("enter on non-critical card resolved %+v, want allow once", agent.answers)
	}
}
