package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ON A TEAM, AN APPROVAL THAT NAMES SOMEONE IS THEIRS: the job's person is
// refused with who may, and can.approve and can.answer say so to each caller.
func TestOnlyTheNamedApproverMayApprove(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stages := []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true, Who: "@rel"}, {Name: "write", Kind: factory.StageChat, On: true}}
	it, err := st.Create(factory.Item{Title: "named", Repo: "owner/repo", State: factory.StateNeedsYou, Assignee: "sam", QKind: factory.QKindApprove, Question: "plan is ready · continue, or send it back?",
		Stages: stages, Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseWaiting}, {Name: "write"}}}})
	if err != nil {
		t.Fatal(err)
	}
	login := "sam"
	seam := factory.Seam{
		Load:   func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err },
		Launch: func(int) error { return nil },
		Answer: func(int, bool, string) error { return nil },
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: login}, nil },
	}
	h := serve.New(seam, config)
	t.Cleanup(h.Close)
	read := func() api.Job {
		var job api.Job
		w := verbRequest(h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
		if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || w.Code != 200 {
			t.Fatalf("%d %s %v", w.Code, w.Body, err)
		}
		return job
	}
	job := read()
	if job.NeedsYou == nil || job.NeedsYou.WhoCanAnswer != factory.WhoApprover || job.NeedsYou.Approver != "@rel" || job.Can.Answer {
		t.Fatalf("sam's view: %+v %+v", job.NeedsYou, job.Can)
	}
	w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/answer", it.ID), `{"yes":true}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"reason":"not_approver"`) || !strings.Contains(w.Body.String(), "@rel") {
		t.Fatalf("sam answered @rel's approval: %d %s", w.Code, w.Body)
	}
	login = "rel"
	if job := read(); !job.Can.Answer {
		t.Fatalf("rel's view: %+v", job.Can)
	}
	if w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/answer", it.ID), `{"yes":true}`); w.Code == http.StatusForbidden {
		t.Fatalf("rel was refused: %s", w.Body)
	}
}

// AN APPROVAL THAT WAITS ON THE OPERATORS shows Approve to an operator: can.*
// knows what only the serving factory knows, that the caller is one.
func TestAnOperatorSeesTheApprovalThatWaitsOnTheOperators(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stages := []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true, Who: factory.ApproverOperator}, {Name: "write", Kind: factory.StageChat, On: true}}
	it, err := st.Create(factory.Item{Title: "ops", Repo: "owner/repo", State: factory.StateNeedsYou, Assignee: "sam", QKind: factory.QKindApprove, Question: "plan is ready · continue, or send it back?",
		Stages: stages, Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseWaiting}, {Name: "write"}}}})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.Seam{
		Load:   func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err },
		Launch: func(int) error { return nil },
		Answer: func(int, bool, string) error { return nil },
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "ops"}, nil },
	}
	h := serve.New(seam, config)
	t.Cleanup(h.Close)
	var job api.Job
	w := verbRequest(h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || w.Code != 200 {
		t.Fatalf("%d %s %v", w.Code, w.Body, err)
	}
	if job.NeedsYou == nil || job.NeedsYou.Approver != factory.ApproverOperator || !job.Can.Answer {
		t.Fatalf("the operator's view: %+v %+v", job.NeedsYou, job.Can)
	}
	var q api.Queue
	w = verbRequest(h, "GET", "/v1/queue", "")
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil || len(q.Jobs) != 1 || !q.Jobs[0].Can.Answer {
		t.Fatalf("the queue: %v %+v", err, q.Jobs)
	}
	// Without the role, the same caller is offered nothing: can.* alone is not the factory's word.
	if c := api.CanFor(it, "ops"); c.Answer || c.Approve {
		t.Fatalf("%+v", c)
	}
}
