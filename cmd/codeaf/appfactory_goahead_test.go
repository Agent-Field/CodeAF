package main

import (
	"context"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// goAheadJob is the part of factory.job this test reads.
type goAheadJob struct {
	State     string              `json:"state"`
	WaitsOn   []int               `json:"waits_on"`
	WaitingOn []int               `json:"waiting_on"`
	PolicyRun string              `json:"policy_run"`
	Blocked   *factory.JobBlocked `json:"blocked"`
}

// A DESK-SHAPED GO-AHEAD IS FOLLOWED BY THE FACTORY ITSELF, END TO END: the
// app reads that this engine and this factory keep group policies, sets what
// a job waits on and the workstream's whole policy exactly as the desk sends
// them, and reads the policy back equal to what it sent. The factory's own
// after-poll pass then leaves the covered job waiting while its parent is
// open, and starts it, attributed to the allowance, once the parent merges.
// No app call is made between the merge and the start.
func TestADeskGoAheadStartsTheCoveredJobAfterItsParentMerges(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	history, err := record.Open(st, record.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, now)
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "alice"}, nil }
	h := serve.New(seam, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "alice", Store: st, Record: history})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); h.Close() })
	w := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	id := conn.ID

	for _, word := range []string{appwire.FeatureFactoryGroupPolicy, appwire.FeatureFactoryWaitsSet} {
		if !strings.Contains(w.hello, `"`+word+`"`) {
			t.Fatalf("hello does not announce %s", word)
		}
	}
	var caps appwire.FactoryCapabilities
	w.must("factory.capabilities", nil, &caps)
	var features appwire.FactoryFeatures
	w.must("factory.features", map[string]any{"factory": id}, &features)
	if !caps.GroupPolicy || !caps.WaitsOn || !slices.Contains(features.Features, "group_policy") || !slices.Contains(features.Features, "waits_on") {
		t.Fatalf("capabilities %+v, features %v", caps, features.Features)
	}

	add := func() int {
		it, err := st.Create(factory.Item{Repo: "owner/repo", Kind: factory.KindIssue, Origin: factory.OriginChat, State: factory.StateNew, Title: "work"})
		if err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	parent, child := add(), add()

	var waits appwire.FactoryWaitsSetResult
	w.must("factory.waits_set", map[string]any{"factory": id, "job": child, "waits_on": []int{parent}}, &waits)
	if waits.Factory != id || waits.Job != child || !slices.Equal(waits.WaitsOn, []int{parent}) || waits.Current == nil || !slices.Equal(waits.Current.WaitingOn, []int{parent}) {
		t.Fatalf("waits_set answered %+v", waits)
	}
	w.mustFail("factory.waits_set", map[string]any{"factory": id, "job": parent, "waits_on": []int{child}}, appwire.CodeBadParams)

	// What crates/core/src/engine/factory_policy.rs policy_set_params sends.
	ask := map[string]any{"factory": id, "group": "ws_8f2c1a", "stopped": false, "allowances": []map[string]any{
		{"id": "ga_0c44", "jobs": []int{child}, "cap_usd": 15.0, "ends_at": "2026-11-20T00:00:00Z"},
	}}
	var set api.Policy
	w.must("factory.policy_set", ask, &set)
	var got api.Policy
	w.must("factory.policy_get", map[string]any{"factory": id, "group": "ws_8f2c1a"}, &got)
	for _, p := range []api.Policy{set, got} {
		if p.Group != "ws_8f2c1a" || p.Stopped || p.Version != 1 || p.Owner != "alice" || p.SpentUSD != 0 || len(p.Allowances) != 1 {
			t.Fatalf("policy reads %+v", p)
		}
		a := p.Allowances[0]
		if a.ID != "ga_0c44" || !slices.Equal(a.Jobs, []int{child}) || a.CapUSD != 15 || a.EndsAt != "2026-11-20T00:00:00Z" || a.State != factory.AllowanceActive {
			t.Fatalf("allowance reads %+v", a)
		}
	}

	var launched []int
	in := intake.New(st, intake.Options{Record: history, Now: clock, Launch: func(id int) error {
		launched = append(launched, id)
		return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
	}})
	afterPoll := factoryAfterPoll(in)
	if err := afterPoll(context.Background()); err != nil {
		t.Fatal(err)
	}
	var job goAheadJob
	w.must("factory.job", map[string]any{"factory": id, "job": child}, &job)
	if len(launched) != 0 || job.State != "backlog" || job.Blocked == nil || job.Blocked.By != "waits_on" || job.Blocked.Job != parent {
		t.Fatalf("before the merge: launched %v, job %+v", launched, job)
	}
	// THE QUEUE'S ROW SAYS WHAT IT WAITS ON as the single-job read does: the
	// desk reads waits off the queue, and an absent waiting_on there reads as
	// a job already released.
	queueRow := func() api.Job {
		var queue api.Queue
		w.must("factory.queue", map[string]any{"factory": id}, &queue)
		for _, row := range queue.Jobs {
			if row.ID == child {
				return row
			}
		}
		t.Fatalf("the queue has no job %d", child)
		return api.Job{}
	}
	if row := queueRow(); !slices.Equal(row.WaitsOn, []int{parent}) || !slices.Equal(row.WaitingOn, []int{parent}) {
		t.Fatalf("before the merge the queue row reads waits_on %v, waiting_on %v", row.WaitsOn, row.WaitingOn)
	}

	if err := st.Update(parent, func(it *factory.Item) error { it.DoneHow = "merged"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := afterPoll(context.Background()); err != nil {
		t.Fatal(err)
	}
	job = goAheadJob{}
	w.must("factory.job", map[string]any{"factory": id, "job": child}, &job)
	if !slices.Equal(launched, []int{child}) || job.State != "queued" || job.PolicyRun != "ga_0c44" || job.Blocked != nil || len(job.WaitingOn) != 0 {
		t.Fatalf("after the merge: launched %v, job %+v", launched, job)
	}
	if row := queueRow(); len(row.WaitingOn) != 0 {
		t.Fatalf("after the merge the queue row still reads waiting_on %v", row.WaitingOn)
	}

	// Mark done on the desk stops the group: the policy reads stopped and empty.
	w.must("factory.policy_set", map[string]any{"factory": id, "group": "ws_8f2c1a", "stopped": true, "allowances": []any{}}, &set)
	if !set.Stopped || len(set.Allowances) != 0 || set.Version != 2 {
		t.Fatalf("the stop reads %+v", set)
	}
}
