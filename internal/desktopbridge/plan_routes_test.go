package desktopbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// planEffects records what a plan did, so a route test can say a cancel did nothing.
type planEffects struct{ calls []string }

func (p *planEffects) rec(s string) error          { p.calls = append(p.calls, s); return nil }
func (p *planEffects) NoteTask(id, _ string) error { return p.rec("note " + id) }
func (p *planEffects) PauseTask(id string) error   { return p.rec("pause " + id) }
func (p *planEffects) ResumeTask(id string) error  { return p.rec("resume " + id) }
func (p *planEffects) CancelTask(id string) error  { return p.rec("cancel " + id) }
func (p *planEffects) NoteChat(id, _ string) error { return p.rec("chat " + id) }
func (p *planEffects) AskPlace(_ context.Context, id, _ string) (string, error) {
	return "", p.rec("ask " + id)
}
func (p *planEffects) Remember(t string) (string, error) { return "", p.rec("remember " + t) }
func (p *planEffects) Forget(string) error               { return nil }

type planAgent struct {
	*fakeAgent
	book *session.PlanBook
}

func (a planAgent) PlanCards() *session.PlanBook { return a.book }

func planFixture(t *testing.T) (*Bridge, string, *planEffects, *session.PlanBook) {
	t.Helper()
	fx := &planEffects{}
	book := session.NewPlanBook(fx, nil)
	agent := planAgent{&fakeAgent{events: make(chan session.Event, 8), model: Model}, book}
	b := New(testToken, func(string) (Connection, error) {
		return Connection{Agent: agent, Welcome: remote.Welcome{SessionFile: "session.jsonl", Workspace: "/project", Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}, Close: func() {}}, nil
	})
	t.Cleanup(b.Close)
	w := request(b, "POST", "/api/engine/sessions", "{}")
	var snapshot Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	return b, snapshot.ID, fx, book
}

func TestPlanRoutesGoEditCancel(t *testing.T) {
	b, id, fx, book := planFixture(t)
	n := 0
	propose := func() string {
		n++
		plan := "plan-" + string(rune('a'+n))
		step := session.PlanCardStep{Kind: session.PlanStepHold, Target: session.PlanTarget{Task: "t1"}}
		if _, err := book.Propose(context.Background(), session.Plan{ID: plan, ReachesBeyond: true, Steps: []session.PlanCardStep{step}}); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	url := func(plan, verb string) string { return "/api/engine/sessions/" + id + "/plan/" + plan + "/" + verb }

	cancelled := propose()
	if w := request(b, http.MethodPost, url(cancelled, "cancel"), "{}"); w.Code != 200 || len(fx.calls) != 0 {
		t.Fatalf("cancel: %d %s calls=%v", w.Code, w.Body.String(), fx.calls)
	}
	if w := request(b, http.MethodPost, url(cancelled, "go"), "{}"); w.Code != 409 {
		t.Fatalf("go after cancel: %d %s", w.Code, w.Body.String())
	}

	if w := request(b, http.MethodPost, url("missing", "go"), "{}"); w.Code != 404 {
		t.Fatalf("unknown plan: %d", w.Code)
	}
	if w := request(b, http.MethodPost, "/api/engine/sessions/nope/plan/x/go", "{}"); w.Code != 404 || !strings.Contains(w.Body.String(), "reattach") {
		t.Fatalf("unknown session: %d %s", w.Code, w.Body.String())
	}

	waiting := propose()
	edit := `{"steps":[{"kind":"stop","target":{"task":"t9"}}]}`
	if w := request(b, http.MethodPost, url(waiting, "edit"), edit); w.Code != 200 {
		t.Fatalf("edit: %d %s", w.Code, w.Body.String())
	}
	if w := request(b, http.MethodPost, url(waiting, "edit"), `{"steps":[]}`); w.Code != 400 {
		t.Fatalf("empty edit: %d", w.Code)
	}
	w := request(b, http.MethodPost, url(waiting, "go"), "{}")
	var receipt session.PlanReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || w.Code != 200 || len(receipt.Results) != 1 || receipt.Results[0].Status != session.PlanStepDone {
		t.Fatalf("go: %d %s", w.Code, w.Body.String())
	}
	if len(fx.calls) != 1 || fx.calls[0] != "cancel t9" {
		t.Fatalf("the edited step must be the one that ran: %v", fx.calls)
	}
	request(b, http.MethodPost, url(waiting, "go"), "{}")
	if len(fx.calls) != 1 {
		t.Fatalf("a repeated Go ran twice: %v", fx.calls)
	}
}

func TestPlanRoutesRefuseAnEngineWithoutPlans(t *testing.T) {
	b, _, id := fixture(t)
	w := request(b, http.MethodPost, "/api/engine/sessions/"+id+"/plan/p/go", "{}")
	if w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
