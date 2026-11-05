package lease_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func leasePolicy(t *testing.T, f *fixture, job int, cap float64, end time.Time, stopped bool) {
	t.Helper()
	_, err := f.store.ChangePolicy("g", func(p *factory.GroupPolicy) error {
		p.Stopped = stopped
		p.Allowances = []factory.Allowance{{ID: "m2", Jobs: []int{job}, CapUSD: cap, EndsAt: end.Format(time.RFC3339)}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// P5, P6: reports and step admission withdraw execution without losing work.
func TestPolicyGateWithdraws(t *testing.T) {
	for _, ended := range []bool{false, true} {
		t.Run(map[bool]string{false: "cap", true: "end"}[ended], func(t *testing.T) {
			f := setup(t)
			job := f.add(t, factory.Item{Group: "g", PolicyRun: "m2", Assignee: "owner", Cap: 20, Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "test", State: factory.PhaseRunning}}}})
			leasePolicy(t, f, job.ID, 5, f.now.Add(10*time.Second), false)
			held, err := f.desk.Pull(worker("w", "owner"))
			if err != nil {
				t.Fatal(err)
			}
			if ended {
				f.now = f.now.Add(10 * time.Second)
			}
			err = f.desk.Apply(held.ID, "w", false, false, func(it *factory.Item) error { it.Stream.Spent = 5; return nil })
			if !errors.Is(err, lease.ErrWithdrawn) {
				t.Fatalf("report = %v", err)
			}
			it, err := f.store.Get(job.ID)
			if err != nil || it.State != factory.StateNeedsYou || it.QKind != "cap" || it.Asking == nil || it.Blocked == nil || it.Blocked.By != "policy" || it.PolicyRun != "" || factory.JobLifetimeSpent(it) != 5 {
				t.Fatalf("hold = %+v, %v", it, err)
			}
			if ended && it.Question != "allowance m2 ended Oct 11" || !ended && it.Question != "allowance m2 reached its $5.00 cap" {
				t.Fatalf("question = %q", it.Question)
			}
			j := api.FromItem(it)
			if j.State != "needs_you" || j.NeedsYou == nil || j.Blocked == nil || j.Held {
				t.Fatalf("read = %+v", j)
			}
			before := len(it.Activity)
			if err := f.desk.Apply(held.ID, "w", false, false, func(*factory.Item) error { t.Fatal("withdrawn report applied"); return nil }); !errors.Is(err, lease.ErrWithdrawn) {
				t.Fatal(err)
			}
			it, _ = f.store.Get(job.ID)
			if len(it.Activity) != before {
				t.Fatal("hold recorded twice")
			}
			if live, err := f.desk.Live(); err != nil || len(live) != 0 {
				t.Fatalf("leases = %+v, %v", live, err)
			}
		})
	}
}

// P7, P8, P13, P14: policy and lifetime budget gates compose at lease admission.
func TestPolicyGateLease(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		policyRun                                    string
		stopped, unreadable, ownCap, noPolicy, ended bool
		hold                                         bool
	}{
		{name: "person spent allowance"},
		{name: "person ended allowance", ended: true},
		{name: "policy spent allowance", policyRun: "m2", hold: true},
		{name: "person stopped group", stopped: true, hold: true},
		{name: "policy unreadable", policyRun: "m2", unreadable: true, hold: true},
		{name: "person unreadable", unreadable: true, hold: true},
		{name: "own budget", policyRun: "m2", ownCap: true, hold: true},
		{name: "no policy", policyRun: "m2", noPolicy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			job := f.add(t, factory.Item{Group: "g", PolicyRun: tc.policyRun, Assignee: "owner", Cap: 20, ChatSpent: 5})
			if !tc.noPolicy {
				cap := 5.0
				if tc.ownCap {
					cap = 100
				}
				end := f.now.Add(time.Hour)
				if tc.ended {
					end = f.now
				}
				leasePolicy(t, f, job.ID, cap, end, tc.stopped)
			}
			if tc.ownCap {
				if err := f.store.Update(job.ID, func(it *factory.Item) error { it.Cap = 5; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if tc.unreadable {
				if err := os.WriteFile(filepath.Join(f.store.Root(), "factory-policies.json"), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := f.desk.Pull(worker("w", "owner"))
			it, readErr := f.store.Get(job.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !tc.hold {
				if err != nil || it.State != factory.StateRunning {
					t.Fatalf("person run = %+v, %v", it, err)
				}
				return
			}
			if !errors.Is(err, lease.ErrNoJob) || it.State != factory.StateNeedsYou || it.QKind != "cap" {
				t.Fatalf("hold = %+v, %v", it, err)
			}
			if tc.ownCap {
				if it.Blocked != nil || !strings.Contains(it.Question, "job total") {
					t.Fatalf("budget changed = %+v", it)
				}
				return
			}
			if it.Blocked == nil || it.Blocked.By != "policy" {
				t.Fatalf("missing policy block = %+v", it)
			}
			if tc.stopped && it.Question != "its group is stopped" || tc.unreadable && it.Question != "its group's policy cannot be read" {
				t.Fatal(it.Question)
			}
		})
	}
}
