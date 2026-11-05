package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func policyPair(t *testing.T, f *waitsFixture) (factory.Item, factory.Item) {
	t.Helper()
	parent, child := f.addJob(t, 1107), f.addJob(t, 0)
	f.edit(t, child.ID, func(it *factory.Item) { it.Group, it.Cap = "g", 5 })
	f.set(t, child.ID, []int{parent.ID}, 200)
	return parent, child
}

func policyAllowance(f *waitsFixture, jobs ...int) factory.Allowance {
	return factory.Allowance{ID: "m2", Jobs: jobs, CapUSD: 15, EndsAt: f.now.AddDate(0, 0, 7).Format(time.RFC3339)}
}

func putPolicy(t *testing.T, f *waitsFixture, stopped bool, allowances ...factory.Allowance) api.Policy {
	t.Helper()
	var out api.Policy
	f.request(t, "PUT", "/v1/groups/g/policy", "operator-secret", map[string]any{"stopped": stopped, "allowances": allowances}, 200, &out)
	return out
}

// P1, P10: merging releases once, records the allowance and survives a new handler.
func TestPolicyOrderingAndRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		f := waitsSetup(t)
		parent, child := policyPair(t, f)
		putPolicy(t, f, false, policyAllowance(f, child.ID))
		// The intake limit does not constrain a policy's explicit allowance.
		if _, err := f.st.ChangeSpending(func(s *factory.Spending) error { value := 1.0; s.AutoStartMaxUSD = &value; return nil }); err != nil {
			t.Fatal(err)
		}
		f.scan(t)
		if f.launches != 0 {
			t.Fatal("started before merge")
		}
		if restart {
			f.h.Close()
			f.h = New(f.seam, f.cfg)
			f.h.stopWaits()
			t.Cleanup(f.h.Close)
		}
		f.merge(t, parent.ID)
		f.scan(t)
		f.scan(t)
		it := f.item(t, child.ID)
		if f.launches != 1 || it.PolicyRun != "m2" || !it.WaitsReleased || it.Blocked != nil || len(it.WaitingOn) != 0 || it.State != factory.StateQueued {
			t.Fatalf("start = %+v; launches %d", it, f.launches)
		}
		events, err := f.cfg.Record.Since(context.Background(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, e := range events {
			if e.Kind == record.AutoStarted && e.Job == child.ID {
				count++
				if !strings.Contains(e.Detail, "m2") {
					t.Fatal(e)
				}
			}
		}
		if count != 1 {
			t.Fatalf("automatic receipts = %d", count)
		}
	}
}

// P2, P3, P4, P7: refusal leaves Backlog and later replacements are rechecked.
func TestPolicyRefusedReleaseAndLaterGrant(t *testing.T) {
	for _, kind := range []string{"missing", "headroom", "ended", "stopped"} {
		t.Run(kind, func(t *testing.T) {
			f := waitsSetup(t)
			parent, child := policyPair(t, f)
			a := policyAllowance(f, child.ID)
			want := "no allowance covers it"
			switch kind {
			case "headroom":
				paid := f.addJob(t, 0)
				f.edit(t, paid.ID, func(it *factory.Item) { it.ChatSpent = 12 })
				a.Jobs = append(a.Jobs, paid.ID)
				want = "allowance m2 has $3.00 of $15.00 left; the job's budget is $5.00"
				putPolicy(t, f, false, a)
			case "ended":
				a.EndsAt = f.now.Format(time.RFC3339)
				want = "allowance m2 ended Oct 11"
				p := putPolicy(t, f, false, a)
				if p.Allowances[0].State != "ended" {
					t.Fatal(p)
				}
			case "stopped":
				want = "its group is stopped"
				putPolicy(t, f, true, a)
			}
			f.merge(t, parent.ID)
			f.scan(t)
			j := f.readJob(t, child.ID)
			if j.State != "backlog" || j.Held || j.NeedsYou != nil || j.Blocked == nil || j.Blocked.By != "policy" || j.Blocked.Reason != want || !j.Can.Run || f.launches != 0 {
				t.Fatalf("refusal = %+v; launches %d", j, f.launches)
			}
			rev := waitsRevision(t, f, child.ID)
			f.scan(t)
			if waitsRevision(t, f, child.ID) != rev {
				t.Fatal("unchanged refusal rewrote job")
			}
			a.CapUSD, a.EndsAt = 20, f.now.Add(time.Hour).Format(time.RFC3339)
			putPolicy(t, f, false, a)
			f.scan(t)
			if f.launches != 1 || f.item(t, child.ID).PolicyRun != "m2" {
				t.Fatalf("grant did not start: %+v", f.item(t, child.ID))
			}
		})
	}
}

// P1: two handlers sharing a folder admit the same released job only once.
func TestPolicyStartAcrossHandlers(t *testing.T) {
	f := waitsSetup(t)
	parent, child := policyPair(t, f)
	putPolicy(t, f, false, policyAllowance(f, child.ID))
	f.merge(t, parent.ID)
	other := New(f.seam, f.cfg)
	other.stopWaits()
	t.Cleanup(other.Close)
	// New scanned synchronously; subsequent concurrent scans cannot start again.
	var wg sync.WaitGroup
	for _, h := range []*Handler{f.h, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.scanWaits(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.launches != 1 {
		t.Fatalf("launches = %d", f.launches)
	}
}

// P5, P8: real Continue and Start resume held work as a person's run.
func TestPolicyHoldPersonDoors(t *testing.T) {
	for _, door := range []string{"answer", "run", "start_now"} {
		t.Run(door, func(t *testing.T) {
			f := waitsSetup(t)
			f.h.seam.Answer = func(int, bool, string) error { t.Fatal("policy hold used a withdrawn worker's answer"); return nil }
			parent, child := policyPair(t, f)
			a := policyAllowance(f, child.ID)
			a.CapUSD = 5
			putPolicy(t, f, false, a)
			f.merge(t, parent.ID)
			f.scan(t)
			f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/raise_budget", child.ID), "operator-secret", map[string]any{"budget": 10}, 200, nil)
			desk := f.h.workers.desk
			worker := lease.Worker{ID: "w", Handle: "owner", Solo: true}
			held, err := desk.Pull(worker)
			if err != nil {
				t.Fatal(err)
			}
			err = desk.Apply(held.ID, worker.ID, false, false, func(it *factory.Item) error { it.Stream.Spent = 5; return nil })
			if !errors.Is(err, lease.ErrWithdrawn) {
				t.Fatal(err)
			}
			var body any
			if door == "answer" {
				body = map[string]any{"choice": "continue"}
			}
			f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/%s", child.ID, door), "operator-secret", body, 200, nil)
			it := f.item(t, child.ID)
			if it.PolicyRun != "" || it.Blocked != nil || it.State != factory.StateQueued || factory.JobLifetimeSpent(it) != 5 {
				t.Fatalf("person resumed = %+v", it)
			}
			held, err = desk.Pull(worker)
			if err != nil {
				t.Fatal(err)
			}
			if err := desk.Apply(held.ID, worker.ID, false, false, func(it *factory.Item) error { it.Stream.Spent++; return nil }); err != nil {
				t.Fatal(err)
			}
			events, err := f.cfg.Record.Since(context.Background(), time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			holds := 0
			for _, e := range events {
				if e.Job == child.ID && e.Kind == record.Held && e.Rule == factory.BlockPolicy {
					holds++
				}
			}
			if holds != 1 {
				t.Fatalf("policy holds recorded = %d", holds)
			}
		})
	}
}

// P7: stopping requests the existing step boundary and gates queued person runs.
func TestPolicyStop(t *testing.T) {
	f := waitsSetup(t)
	parent, child := policyPair(t, f)
	putPolicy(t, f, false, policyAllowance(f, child.ID))
	f.merge(t, parent.ID)
	f.scan(t)
	worker := lease.Worker{ID: "w", Handle: "owner", Solo: true}
	held, err := f.h.workers.desk.Pull(worker)
	if err != nil {
		t.Fatal(err)
	}
	putPolicy(t, f, true, policyAllowance(f, child.ID))
	if !f.item(t, child.ID).StopAfterStep {
		t.Fatal("running job has no step stop")
	}
	if err := f.h.workers.desk.Apply(held.ID, worker.ID, false, false, func(it *factory.Item) error {
		it.Stream.Phases[0].State = factory.PhaseRunning
		return nil
	}); !errors.Is(err, lease.ErrWithdrawn) {
		t.Fatal(err)
	}
	putPolicy(t, f, false, policyAllowance(f, child.ID))
	f.scan(t)
	if it := f.item(t, child.ID); it.State != factory.StateNeedsYou {
		t.Fatalf("unstop resumed hold = %+v", it)
	}
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/run", child.ID), "operator-secret", nil, 200, nil)
	putPolicy(t, f, true, policyAllowance(f, child.ID))
	if _, err := f.h.workers.desk.Pull(worker); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("person's start escaped stop: %v", err)
	}
	if j := f.readJob(t, child.ID); j.State != "needs_you" || j.Blocked == nil || j.Blocked.Reason != "its group is stopped" {
		t.Fatal(j)
	}
}

// P7: a stopped group's paid step finishes before the next admission is held.
func TestPolicyStopFinishesTheStep(t *testing.T) {
	f := waitsSetup(t)
	parent, child := policyPair(t, f)
	putPolicy(t, f, false, policyAllowance(f, child.ID))
	f.merge(t, parent.ID)
	f.scan(t)
	w := lease.Worker{ID: "w", Handle: "owner", Solo: true}
	held, err := f.h.workers.desk.Pull(w)
	if err != nil {
		t.Fatal(err)
	}
	f.edit(t, child.ID, func(it *factory.Item) {
		it.Stream.Phases[0].State, it.Stream.Phases[0].Started = factory.PhaseRunning, f.now
	})
	f.now = f.now.Add(time.Second)
	putPolicy(t, f, true, policyAllowance(f, child.ID))
	if err := f.h.workers.desk.Apply(held.ID, w.ID, false, false, func(it *factory.Item) error { it.Stream.Spent = 1; return nil }); err != nil {
		t.Fatalf("paid step was cut: %v", err)
	}
	if it := f.item(t, child.ID); it.State != factory.StateRunning || !it.StopAfterStep || factory.JobLifetimeSpent(it) != 1 {
		t.Fatal(it)
	}
	if err := f.h.workers.desk.Apply(held.ID, w.ID, false, false, func(it *factory.Item) error {
		it.Stream.Spent = 2
		it.Stream.Phases[0].State = factory.PhaseDone
		return nil
	}); err != nil {
		t.Fatalf("completion = %v", err)
	}
	if err := f.h.workers.desk.Apply(held.ID, w.ID, false, false, func(it *factory.Item) error {
		it.Stream.Phases[0].State = factory.PhaseRunning
		it.Stream.Phases[0].Round++
		return nil
	}); !errors.Is(err, lease.ErrWithdrawn) {
		t.Fatalf("next admission = %v", err)
	}
	if it := f.item(t, child.ID); it.State != factory.StateNeedsYou || it.Blocked == nil || it.Blocked.Reason != "its group is stopped" || factory.JobLifetimeSpent(it) != 2 {
		t.Fatal(it)
	}
}

// P1, P8: a queued Start now makes the run the person's without a second launch.
func TestPolicyQueuedStartNow(t *testing.T) {
	f := waitsSetup(t)
	parent, child := policyPair(t, f)
	putPolicy(t, f, false, policyAllowance(f, child.ID))
	f.merge(t, parent.ID)
	f.scan(t)
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", child.ID), "operator-secret", nil, 200, nil)
	if it := f.item(t, child.ID); it.PolicyRun != "" || f.launches != 1 || !it.OverLimit {
		t.Fatalf("person's queued start = %+v, launches %d", it, f.launches)
	}
}

// P1: the automatic start ensures the same default job budget as create-and-start.
func TestPolicyStartEnsuresBudget(t *testing.T) {
	f := waitsSetup(t)
	parent, child := policyPair(t, f)
	f.edit(t, child.ID, func(it *factory.Item) { it.Cap = 0 })
	putPolicy(t, f, false, policyAllowance(f, child.ID))
	f.merge(t, parent.ID)
	f.scan(t)
	if it := f.item(t, child.ID); it.Cap != factory.DefaultJobBudget || it.PolicyRun != "m2" || f.launches != 1 {
		t.Fatalf("automatic budget = %+v", it)
	}
}

// P2, P4, P8: a person can start a refused release under an ended allowance.
func TestPolicyBacklogPersonStart(t *testing.T) {
	f := waitsSetup(t)
	parent, child := policyPair(t, f)
	a := policyAllowance(f, child.ID)
	a.EndsAt = f.now.Format(time.RFC3339)
	putPolicy(t, f, false, a)
	f.merge(t, parent.ID)
	f.scan(t)
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/run", child.ID), "operator-secret", nil, 200, nil)
	if it := f.item(t, child.ID); it.PolicyRun != "" || it.Blocked != nil || f.launches != 1 {
		t.Fatalf("person start = %+v", it)
	}
	if _, err := f.h.workers.desk.Pull(lease.Worker{ID: "w", Handle: "owner", Solo: true}); err != nil {
		t.Fatal(err)
	}
}

// P9, P11, P12: HTTP reads lifetime totals, state, versions and capabilities.
func TestPolicyHTTPReadsAndStale(t *testing.T) {
	f := waitsSetup(t)
	_, child := policyPair(t, f)
	f.edit(t, child.ID, func(it *factory.Item) { it.ChatSpent, it.SpentBefore = 2, 3 })
	other := f.addJob(t, 0)
	f.edit(t, other.ID, func(it *factory.Item) { it.Group, it.ChatSpent = "g", 4 })
	var empty api.Policy
	f.request(t, "GET", "/v1/groups/g/policy", "operator-secret", nil, 200, &empty)
	if empty.SpentUSD != 9 || empty.Stopped || len(empty.Allowances) != 0 || empty.Allowances == nil {
		t.Fatal(empty)
	}
	a := policyAllowance(f, child.ID)
	a.CapUSD = 5
	p := putPolicy(t, f, false, a)
	if p.SpentUSD != 9 || p.Allowances[0].SpentUSD != 5 || p.Allowances[0].State != "spent" || p.Version != 1 || p.UpdatedAt == nil {
		t.Fatal(p)
	}
	f.request(t, "PUT", "/v1/groups/g/policy", "operator-secret", map[string]any{"stopped": true, "allowances": []factory.Allowance{}, "version": 0}, 409, nil)
	var after api.Policy
	f.request(t, "GET", "/v1/groups/g/policy", "operator-secret", nil, 200, &after)
	if !reflect.DeepEqual(p, after) {
		t.Fatalf("stale wrote: %+v", after)
	}
	var about struct {
		Features []string `json:"features"`
	}
	f.request(t, "GET", "/v1/about", "", nil, 200, &about)
	if !strings.Contains(strings.Join(about.Features, ","), "group_policy") {
		t.Fatal(about)
	}
}

// P11: invalid replacements leave the policy and every job unchanged.
func TestPolicyHTTPBounds(t *testing.T) {
	f := waitsSetup(t)
	_, child := policyPair(t, f)
	putPolicy(t, f, false, policyAllowance(f, child.ID))
	before, err := os.ReadFile(filepath.Join(f.st.Root(), "factory-policies.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*factory.Allowance){
		func(a *factory.Allowance) { a.ID = "" }, func(a *factory.Allowance) { a.ID = strings.Repeat("x", 65) }, func(a *factory.Allowance) { a.ID = "bad id" },
		func(a *factory.Allowance) { a.Jobs = nil }, func(a *factory.Allowance) { a.Jobs = make([]int, 201) }, func(a *factory.Allowance) { a.Jobs = []int{0} }, func(a *factory.Allowance) { a.Jobs = []int{-1} }, func(a *factory.Allowance) { a.Jobs = []int{1, 1} },
		func(a *factory.Allowance) { a.CapUSD = 0 }, func(a *factory.Allowance) { a.CapUSD = 10001 }, func(a *factory.Allowance) { a.EndsAt = "tomorrow" },
	} {
		a := policyAllowance(f, child.ID)
		change(&a)
		f.request(t, "PUT", "/v1/groups/g/policy", "operator-secret", map[string]any{"stopped": true, "allowances": []factory.Allowance{a}}, 422, nil)
	}
	a := policyAllowance(f, child.ID)
	for _, allowances := range [][]factory.Allowance{{a, a}, make([]factory.Allowance, 51)} {
		f.request(t, "PUT", "/v1/groups/g/policy", "operator-secret", map[string]any{"allowances": allowances}, 422, nil)
	}
	for _, group := range []string{strings.Repeat("x", 201), "%00"} {
		f.request(t, "PUT", "/v1/groups/"+group+"/policy", "operator-secret", map[string]any{}, 422, nil)
	}
	after, err := os.ReadFile(filepath.Join(f.st.Root(), "factory-policies.json"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("invalid wrote policy: %s, %v", after, err)
	}
}

// P11: the first setter owns a team policy and only that person's jobs start.
func TestPolicyTeamOwnership(t *testing.T) {
	f, writer, reader := waitsTeamSetup(t)
	f.h.waits.launch = func(id int) error { f.launches++; return lease.NewDoors(f.h.workers.desk).Launch(id) }
	var p api.Policy
	f.request(t, "PUT", "/v1/groups/g/policy", writer, map[string]any{"allowances": []factory.Allowance{}}, 200, &p)
	if p.Owner != "writer" {
		t.Fatal(p)
	}
	f.request(t, "PUT", "/v1/groups/g/policy", reader, map[string]any{"stopped": true}, 403, nil)
	f.request(t, "PUT", "/v1/groups/g/policy", "floor-secret", map[string]any{"stopped": false}, 200, &p)
	if p.Owner != "writer" {
		t.Fatal("operator changed owner")
	}
	parent := waitsTeamJob(t, f, "acme/api", "writer", 1107)
	var children []factory.Item
	for _, person := range []string{"writer", "reader", ""} {
		child := waitsTeamJob(t, f, "acme/api", person, 0)
		f.edit(t, child.ID, func(it *factory.Item) {
			it.Group, it.Cap = "g", 5
			it.WaitsOn, it.WaitingOn = []int{parent.ID}, []int{parent.ID}
		})
		children = append(children, child)
	}
	a := policyAllowance(f, children[0].ID, children[1].ID, children[2].ID)
	f.request(t, "PUT", "/v1/groups/g/policy", writer, map[string]any{"allowances": []factory.Allowance{a}}, 200, nil)
	f.merge(t, parent.ID)
	f.scan(t)
	if f.launches != 2 {
		t.Fatalf("owner starts = %d", f.launches)
	}
	if it := f.item(t, children[1].ID); it.State != factory.StateNew || it.Blocked == nil || it.Blocked.By != "policy" {
		t.Fatal(it)
	}
	if it := f.item(t, children[2].ID); it.Person() != "writer" {
		t.Fatalf("unassigned start = %+v", it)
	}
}

// P13: a damaged policy file never admits a release.
func TestPolicyUnreadableRelease(t *testing.T) {
	f := waitsSetup(t)
	parent, child := policyPair(t, f)
	putPolicy(t, f, false, policyAllowance(f, child.ID))
	f.merge(t, parent.ID)
	if err := os.WriteFile(filepath.Join(f.st.Root(), "factory-policies.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	f.scan(t)
	if it := f.item(t, child.ID); f.launches != 0 || it.State != factory.StateNew || it.Blocked == nil || it.Blocked.Reason != "its group's policy cannot be read" {
		t.Fatalf("unreadable started: %+v", it)
	}
}

// P11, P12: create_group carries a trimmed opaque group through HTTP and reads.
func TestPolicyCreateGroup(t *testing.T) {
	f := waitsSetup(t)
	f.h.seam.NewJob = func(repo, title, text string) (int, error) {
		it, err := f.st.Create(factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Repo: repo, Title: title, Body: text})
		return it.ID, err
	}
	var created struct {
		ID int `json:"id"`
	}
	f.request(t, "POST", "/v1/jobs", "operator-secret", map[string]any{"repo": "owner/api", "title": "job", "text": "work", "group": " g "}, 201, &created)
	if j := f.readJob(t, created.ID); j.Group != "g" {
		t.Fatal(j)
	}
	for _, group := range []string{strings.Repeat("x", 201), "g\x00"} {
		f.request(t, "POST", "/v1/jobs", "operator-secret", map[string]any{"repo": "owner/api", "title": "job", "text": "work", "group": group}, 422, nil)
	}
	jobs, err := f.st.List()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("invalid created: %d, %v", len(jobs), err)
	}
	raw, err := json.Marshal(api.FromItem(f.item(t, created.ID)))
	if err != nil || !strings.Contains(string(raw), `"group":"g"`) {
		t.Fatalf("group read = %s, %v", raw, err)
	}
}
