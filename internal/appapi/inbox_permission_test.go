package appapi

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The card names the exact command, never the tool; an irreversible ask
// offers no scope at all.
func TestInboxPermissionCarriesTheCommandAndItsScopes(t *testing.T) {
	q := session.Question{
		ID: 3, Kind: session.QuestionConsent, Head: "needs your ok to run bash", Reason: "gh acts in your name outside this machine",
		Subject:           session.SubjectRef{Kind: session.SubjectCall, CallID: "c1", Name: "bash"},
		PermissionCommand: "gh issue view 12",
		Card:              &session.ConsentCard{Command: "gh issue view 12", Touches: []string{"github.com · as you"}},
	}
	p := inboxPermission("a1", q)
	if p == nil || len(p.Commands) != 1 || p.Commands[0] != "gh issue view 12" || p.ReadOnly || p.Touches[0] != "github.com · as you" {
		t.Fatalf("permission = %+v", p)
	}
	if len(p.Scopes) == 0 || p.Scopes[0] != (PermissionScope{ID: "exact", Command: "gh issue view 12", Label: "exactly this command"}) {
		t.Fatalf("scopes = %+v", p.Scopes)
	}
	for _, scope := range p.Scopes {
		if scope.ID != "exact" && scope.ID != "class" {
			t.Fatalf("unknown scope %+v", scope)
		}
	}
	if len(p.CommandIDs) != 1 || p.CommandIDs[0] != askID("a1", q) {
		t.Fatalf("command ids = %v", p.CommandIDs)
	}
	q.Stakes = session.StakesIrreversible
	if p := inboxPermission("a1", q); len(p.Scopes) != 0 || !p.Irreversible {
		t.Fatalf("irreversible = %+v", p)
	}
	q.Card, q.PermissionCommand = nil, ""
	if p := inboxPermission("a1", q); p.Commands[0] != q.Head {
		t.Fatalf("fallback = %v", p.Commands)
	}
	q.Kind = session.QuestionTask
	if inboxPermission("a1", q) != nil {
		t.Fatal("a non-permission question got a card")
	}
}

// A call waiting on a person's ok counts as a step underway.
func TestAWaitingPermissionCountsAsAStep(t *testing.T) {
	f := newAskFixture(t)
	if waitingSteps(f.door) != 0 {
		t.Fatal("steps with nothing waiting")
	}
	f.ask(t, permissionQuestion(21))
	if waitingSteps(f.door) != 1 {
		t.Fatal("the waiting call is not a step")
	}
}
