package factory

import "testing"

// P3, P5: decimal equality and whole micro-dollars govern both decisions.
func TestPolicyMoneyBoundaries(t *testing.T) {
	p, job, now := policyExample()
	p.Allowances[0].CapUSD, job.Cap = 0.30, 0.20
	spending := map[int]float64{2: 0.10}
	spentOf := func(id int) float64 { return spending[id] }
	if id, reason, ok := StartDecision(p, job, spentOf, now); !ok || id != "m2" || reason != "" {
		t.Fatalf("exact headroom = %q, %q, %t", id, reason, ok)
	}
	spending[2] += 0.000001
	if _, _, ok := StartDecision(p, job, spentOf, now); ok {
		t.Fatal("one micro-dollar beyond headroom was admitted")
	}
	p.Allowances[0].CapUSD = 0.80
	spending[2], spending[job.ID] = 0.10, 0.70
	if reason, ok := ContinueDecision(p, job, spentOf, now); ok || reason != "allowance m2 reached its $0.80 cap" {
		t.Fatalf("exact cap = %q, %t", reason, ok)
	}
	spending[job.ID] -= 0.000001
	if reason, ok := ContinueDecision(p, job, spentOf, now); !ok {
		t.Fatalf("one micro-dollar below cap = %q", reason)
	}
}
