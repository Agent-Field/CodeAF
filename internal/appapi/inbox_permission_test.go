package appapi

import (
	"encoding/json"
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

// A look offers "for this task" first; a write and an irreversible ask never do.
func TestALookOffersForThisTaskFirst(t *testing.T) {
	q := permissionQuestion(3)
	q.PermissionCommand = "gh issue view 12"
	q.Card = &session.ConsentCard{Command: "gh issue view 12", Family: "gh", ReadOnly: true}
	p := inboxPermission("a1", q)
	if !p.ReadOnly || p.Scopes[0] != (PermissionScope{ID: "task", Command: "gh", Label: "for this task"}) || p.Scopes[1].ID != "exact" {
		t.Fatalf("look = %+v", p)
	}
	q.Stakes = session.StakesIrreversible
	if p := inboxPermission("a1", q); len(p.Scopes) != 0 {
		t.Fatalf("irreversible offered %+v", p.Scopes)
	}
	q.Stakes = session.StakesCostly
	q.Card = &session.ConsentCard{Command: "gh pr merge 7"}
	if p := inboxPermission("a1", q); p.ReadOnly || p.Scopes[0].ID != "exact" {
		t.Fatalf("a write offered %+v", p.Scopes)
	}
}

// "For this task" reaches the engine as a ScopeTask yes and writes no rule; a
// write refuses it.
func TestPermissionTaskScopeAnswersWithoutSavingARule(t *testing.T) {
	f := newAskFixture(t)
	q := permissionQuestion(6)
	q.PermissionCommand = "gh issue view 12"
	q.Card = &session.ConsentCard{Command: "gh issue view 12", Family: "gh", ReadOnly: true}
	id := f.ask(t, q)
	body := `{"verb":"always","rule":{"match":"task","who":"agent"}}`
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, body), 200)
	last := f.door.answers[len(f.door.answers)-1]
	if last.Key != "1" || last.Scope != session.ScopeTask {
		t.Fatalf("answer = %+v", last)
	}
	w := f.request(t, "GET", "/app/v1/rules", "")
	var rules RulesResponse
	json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules.Rules) != 0 {
		t.Fatalf("task scope saved a rule: %+v", rules)
	}
	q = permissionQuestion(7)
	q.PermissionCommand = "gh pr merge 7"
	q.Card = &session.ConsentCard{Command: "gh pr merge 7"}
	id = f.ask(t, q)
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, body), 422)
	if len(f.door.answers) != 1 {
		t.Fatal("a refused task scope reached the engine")
	}
}
