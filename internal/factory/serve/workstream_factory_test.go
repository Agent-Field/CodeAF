package serve

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// A WORKSTREAM'S FACTORY SWITCH BRINGS A NEW ISSUE IN AND SPENDS WITHIN ITS
// RULE, on the organization's server, with no person in between. Pantry has
// the switch on and "start without asking" at $2; the repository's own file
// starts agent-ready issues at $5. A new agent-ready issue joins Pantry, is
// not started by its label, and the go-ahead pass starts it at $2. The issue
// opened before the switch went on stays out of Pantry.
//
// Before the fix nothing on the server linked a new issue to a workstream:
// the job ran by its label at the repository's $5 and Pantry's tasks never
// changed.
func TestAWorkstreamsFactorySwitchTakesNewIssuesWithinItsRule(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	path := "/v1/workstreams/" + ws.ID
	var cur api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &cur)
	rules := cur.Rules
	rules.StartWithoutAskingUSD = 2
	rules.KeepsRunning.Factory = true
	o.do(t, "santosh", "PATCH", path, cur.Rev, api.WorkstreamPatch{Rules: &rules}, 200, &cur)
	since := cur.Rules.KeepsRunning.FactorySince
	if since == nil {
		t.Fatal("turning the Factory switch on kept no time")
	}
	// A writer cannot move the time: the same rules sent again keep it.
	rules = cur.Rules
	forged := since.Add(-72 * time.Hour)
	rules.KeepsRunning.FactorySince = &forged
	o.do(t, "santosh", "PATCH", path, cur.Rev, api.WorkstreamPatch{Rules: &rules}, 200, &cur)
	if got := cur.Rules.KeepsRunning.FactorySince; got == nil || !got.Equal(*since) {
		t.Fatalf("a writer moved the switch's time: %v, want %v", got, since)
	}

	issue := func(num int, created time.Time) factory.Item {
		it, err := o.st.Create(factory.Item{Origin: factory.OriginForge, Repo: "acme/api", Num: num, Kind: factory.KindIssue, Tier: factory.TierOwner,
			Title: "Add Largest to Ledger", Body: "fix ledger.go", Labels: []string{"agent-ready"}, State: factory.StateNew, Created: created,
			Triage: factory.Triage{StartJudged: true, StartMatched: []string{}}})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	old := issue(1, since.Add(-time.Hour))
	fresh := issue(2, since.Add(time.Minute))

	// The intake the server's triage runs: the repository's file starts
	// agent-ready issues at $5. It must leave the fresh one to Pantry.
	f, err := pipeline.Parse("start:\n  labels: [agent-ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	f.DefaultBudget = 5
	history, err := record.Open(o.st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	started := map[int]bool{}
	in := intake.New(o.st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return f, nil }, Launch: func(id int) error {
		started[id] = true
		return o.st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
	}})
	ctx := context.Background()
	if err := in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if started[fresh.ID] {
		t.Fatal("the label started an issue Pantry's Factory switch takes, under the repository's budget")
	}

	if err := o.h.ClaimFactoryJobs(ctx); err != nil {
		t.Fatal(err)
	}
	o.do(t, "santosh", "GET", path, -1, nil, 200, &cur)
	if !slices.Equal(cur.Jobs, []int{fresh.ID}) {
		t.Fatalf("Pantry's jobs = %v, want only the issue opened after the switch went on (%d)", cur.Jobs, fresh.ID)
	}
	p, ok, err := o.st.Policy(ws.ID)
	if err != nil || !ok || len(p.Allowances) != 1 || p.Allowances[0].PerJobUSD != 2 || !slices.Equal(p.Allowances[0].Jobs, []int{fresh.ID}) {
		t.Fatalf("Pantry's start-without-asking rule does not cover it: %+v %v %v", p, ok, err)
	}

	if err := in.GoAheads(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := o.st.Get(fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !started[fresh.ID] || got.Cap != 2 {
		t.Fatalf("the go-ahead did not start it within Pantry's $2: started %v, cap %v", started[fresh.ID], got.Cap)
	}
	if !started[old.ID] {
		t.Fatal("an issue no workstream takes is still started by its labels, as before")
	}

	// A second pass claims nothing again.
	if err := o.h.ClaimFactoryJobs(ctx); err != nil {
		t.Fatal(err)
	}
	o.do(t, "santosh", "GET", path, -1, nil, 200, &cur)
	if len(cur.Jobs) != 1 {
		t.Fatalf("a second pass changed Pantry's jobs: %v", cur.Jobs)
	}
}
