package serve

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// P7: a real PUT's activity write interleaves a report after policy persistence
// and before its per-job pass. Worker clocks in either direction change nothing.
func TestPolicyStopReportDuringPUT(t *testing.T) {
	for _, skew := range []time.Duration{-30 * time.Second, 30 * time.Second} {
		for _, policyRun := range []string{"", "m2"} {
			t.Run(fmt.Sprintf("skew=%s/policy_run=%s", skew, policyRun), func(t *testing.T) {
				f := waitsSetup(t)
				job := f.addJob(t, 0)
				f.edit(t, job.ID, func(it *factory.Item) {
					it.Group, it.PolicyRun, it.State = "g", policyRun, factory.StateQueued
					it.Stream = &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "test", State: factory.PhaseRunning, Round: 1, Started: f.now.Add(skew)}}}
				})
				held, err := f.h.workers.desk.Pull(lease.Worker{ID: "w", Handle: "owner", Solo: true})
				if err != nil {
					t.Fatal(err)
				}
				interleaved := false
				history, err := record.Open(f.st, record.Options{Now: func() time.Time {
					if !interleaved {
						interleaved = true
						p, err := f.st.Policy("g")
						if err != nil || !p.Stopped || f.item(t, job.ID).StopAfterStep {
							t.Fatalf("report outside publication window: %+v, %v", p, err)
						}
						err = f.h.workers.desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error {
							return run.ApplyReport(it, factory.RunReport{Cost: 0.25}, f.now)
						})
						if err != nil {
							t.Fatalf("stop cut admitted step during PUT: %v", err)
						}
						it := f.item(t, job.ID)
						if !it.StopAfterStep || it.State != factory.StateRunning || factory.JobLifetimeSpent(it) != 0.25 {
							t.Fatalf("report did not arm step boundary: %+v", it)
						}
					}
					return f.now
				}})
				if err != nil {
					t.Fatal(err)
				}
				f.h.config.Record = history
				putPolicy(t, f, true, policyAllowance(f, job.ID))
				if !interleaved {
					t.Fatal("PUT did not reach activity write")
				}
				// Completion belongs to the admitted step and keeps its work.
				if err := f.h.workers.desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error {
					return run.ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{Name: "test", State: factory.PhaseDone, Round: 1}}}, f.now)
				}); err != nil {
					t.Fatalf("completion was withdrawn: %v", err)
				}
				// A delayed new round carries an old worker timestamp, including
				// after an identical stopped replacement advances UpdatedAt.
				f.now = f.now.Add(time.Second)
				putPolicy(t, f, true, policyAllowance(f, job.ID))
				err = f.h.workers.desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error {
					return run.ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{Name: "test", State: factory.PhaseRunning, Round: 2, Started: f.now.Add(-time.Hour)}}}, f.now)
				})
				if !errors.Is(err, lease.ErrWithdrawn) {
					t.Fatalf("delayed admission escaped stop: %v", err)
				}
				it := f.item(t, job.ID)
				if it.Blocked == nil || it.Blocked.Reason != "its group is stopped" || it.State != factory.StateNeedsYou || factory.JobLifetimeSpent(it) != 0.25 {
					t.Fatalf("new step hold = %+v", it)
				}
			})
		}
	}
}

// P7: stopped PUTs repair boundary flags even when already stopped.
func TestPolicyRepeatedStopRearmsJobs(t *testing.T) {
	f := waitsSetup(t)
	job := f.addJob(t, 0)
	f.edit(t, job.ID, func(it *factory.Item) { it.Group, it.State = "g", factory.StateRunning })
	putPolicy(t, f, true, policyAllowance(f, job.ID))
	if !f.item(t, job.ID).StopAfterStep {
		t.Fatal("initial stop did not arm boundary")
	}
	f.edit(t, job.ID, func(it *factory.Item) { it.StopAfterStep = false })
	putPolicy(t, f, true, policyAllowance(f, job.ID))
	if !f.item(t, job.ID).StopAfterStep {
		t.Fatal("repeated stopped PUT did not repair missing boundary")
	}
}

// P3, P5, P9: automatic start, lease admission and GET use decimal boundaries.
func TestPolicyHTTPDecimalMoney(t *testing.T) {
	f := waitsSetup(t)
	parent, child := policyPair(t, f)
	paid := f.addJob(t, 0)
	f.edit(t, paid.ID, func(it *factory.Item) { it.Group, it.ChatSpent = "g", 0.10 })
	f.edit(t, child.ID, func(it *factory.Item) { it.Cap = 0.20 })
	a := policyAllowance(f, child.ID, paid.ID)
	a.CapUSD = 0.30
	putPolicy(t, f, false, a)
	f.merge(t, parent.ID)
	f.scan(t)
	if it := f.item(t, child.ID); it.PolicyRun != "m2" || it.State != factory.StateQueued || f.launches != 1 {
		t.Fatalf("exact headroom refused: %+v; launches %d", it, f.launches)
	}
	f.edit(t, child.ID, func(it *factory.Item) { it.Cap, it.Stream.Spent = 2, 0.70 })
	a.CapUSD = 0.80
	putPolicy(t, f, false, a)
	var p api.Policy
	f.request(t, "GET", "/v1/groups/g/policy", "operator-secret", nil, 200, &p)
	if len(p.Allowances) != 1 || p.Allowances[0].State != "spent" {
		t.Fatalf("decimal read state = %+v", p)
	}
	if _, err := f.h.workers.desk.Pull(lease.Worker{ID: "w", Handle: "owner", Solo: true}); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("decimal cap lease = %v", err)
	}
	if it := f.item(t, child.ID); it.Blocked == nil || it.Question != "allowance m2 reached its $0.80 cap" {
		t.Fatalf("decimal cap hold = %+v", it)
	}
}
