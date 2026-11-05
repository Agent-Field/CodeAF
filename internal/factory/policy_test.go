package factory

import (
	"strings"
	"testing"
	"time"
)

func policyExample() (GroupPolicy, Item, time.Time) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	return GroupPolicy{Group: "g", Allowances: []Allowance{{ID: "m2", Jobs: []int{2, 3}, CapUSD: 15, EndsAt: now.AddDate(0, 0, 7).Format(time.RFC3339)}}}, Item{ID: 3, Group: "g", Cap: 5, PolicyRun: "m2"}, now
}

// P1, P2, P3, P4, P7: starts need full headroom and a future end.
func TestPolicyStartDecisions(t *testing.T) {
	p, job, now := policyExample()
	spent := func(id int) float64 {
		if id == 2 {
			return 12
		}
		return 0
	}
	if _, reason, ok := StartDecision(p, job, spent, now); ok || reason != "allowance m2 has $3.00 of $15.00 left; the job's budget is $5.00" {
		t.Fatalf("headroom = %q, %t", reason, ok)
	}
	p.Allowances[0].CapUSD = 20
	if id, reason, ok := StartDecision(p, job, spent, now); !ok || id != "m2" || reason != "" {
		t.Fatalf("start = %q, %q, %t", id, reason, ok)
	}
	p.Allowances[0].EndsAt = now.Format(time.RFC3339)
	if _, reason, ok := StartDecision(p, job, spent, now); ok || reason != "allowance m2 ended Nov 5" {
		t.Fatalf("end = %q, %t", reason, ok)
	}
	p.Stopped = true
	if _, reason, ok := StartDecision(p, job, spent, now); ok || reason != "its group is stopped" {
		t.Fatalf("stop = %q, %t", reason, ok)
	}
	p.Stopped, p.Allowances = false, nil
	if _, reason, ok := StartDecision(p, job, spent, now); ok || reason != "no allowance covers it" {
		t.Fatalf("scope = %q, %t", reason, ok)
	}
}

// P5, P6: a run stays tied to the allowance that began it.
func TestPolicyContinueDecisions(t *testing.T) {
	p, job, now := policyExample()
	if reason, ok := ContinueDecision(p, job, func(int) float64 { return 7.5 }, now); ok || reason != "allowance m2 reached its $15.00 cap" {
		t.Fatalf("cap = %q, %t", reason, ok)
	}
	if reason, ok := ContinueDecision(p, job, func(int) float64 { return 0 }, now); !ok {
		t.Fatal(reason)
	}
	p.Allowances[0].EndsAt = now.Format(time.RFC3339)
	if reason, ok := ContinueDecision(p, job, func(int) float64 { return 0 }, now); ok || reason != "allowance m2 ended Nov 5" {
		t.Fatalf("end = %q, %t", reason, ok)
	}
	p.Allowances[0].EndsAt = now.Add(time.Hour).Format(time.RFC3339)
	p.Allowances[0].Jobs = []int{2}
	if reason, ok := ContinueDecision(p, job, func(int) float64 { return 0 }, now); ok || reason != "no allowance covers it" {
		t.Fatalf("scope = %q, %t", reason, ok)
	}
	p.Allowances = append(p.Allowances, Allowance{ID: "other", Jobs: []int{3}, CapUSD: 20, EndsAt: now.Add(time.Hour).Format(time.RFC3339)})
	if _, ok := ContinueDecision(p, job, func(int) float64 { return 0 }, now); ok {
		t.Fatal("another allowance inherited the run")
	}
}

// P9: every run counts, including listed jobs outside the group.
func TestPolicySpend(t *testing.T) {
	p, _, _ := policyExample()
	group, allowances := PolicySpend(p, []Item{{ID: 2, Group: "g", SpentBefore: 3, ChatSpent: 2}, {ID: 3, Group: "other", Stream: &Stream{Spent: 4}}, {ID: 4, Group: "g", ChatSpent: 1}})
	if group != 6 || allowances["m2"] != 9 {
		t.Fatalf("spend = %g, %+v", group, allowances)
	}
}

// P11: policy bounds share one check at the engine and service.
func TestPolicyBounds(t *testing.T) {
	p, _, _ := policyExample()
	if err := p.Check(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*GroupPolicy){
		func(p *GroupPolicy) { p.Group = "" },
		func(p *GroupPolicy) { p.Group = strings.Repeat("x", 201) },
		func(p *GroupPolicy) { p.Group = "g\x00" },
		func(p *GroupPolicy) { p.Allowances[0].ID = "bad id" },
		func(p *GroupPolicy) { p.Allowances[0].Jobs = []int{1, 1} },
		func(p *GroupPolicy) { p.Allowances[0].CapUSD = 0 },
		func(p *GroupPolicy) { p.Allowances[0].CapUSD = 10001 },
		func(p *GroupPolicy) { p.Allowances[0].EndsAt = "tomorrow" },
	} {
		p, _, _ := policyExample()
		change(&p)
		if p.Check() == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}
