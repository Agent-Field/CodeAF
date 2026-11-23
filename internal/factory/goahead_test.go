package factory

import (
	"strings"
	"testing"
	"time"
)

// AN ALLOWANCE IS KEPT ONLY WITHIN THE DESK'S OWN BOUNDS, so a policy one side
// keeps is one the other sends: a distinct plain id, 1–200 distinct jobs from
// 1, a cap above $0 and at most $10,000, and an RFC 3339 end.
func TestAllowancesAreKeptOnlyWithinTheirBounds(t *testing.T) {
	good := Allowance{ID: "ga_0c44", Jobs: []int{1107}, CapUSD: 15, EndsAt: "2026-11-20T00:00:00Z"}
	if err := CheckAllowances([]Allowance{good}); err != nil {
		t.Fatalf("a desk-shaped allowance was refused: %v", err)
	}
	if err := CheckAllowances(nil); err != nil {
		t.Fatalf("no allowances is a policy too: %v", err)
	}
	bad := map[string]func(*Allowance){
		"no id":        func(a *Allowance) { a.ID = "" },
		"id with a /":  func(a *Allowance) { a.ID = "ga/1" },
		"long id":      func(a *Allowance) { a.ID = strings.Repeat("a", PolicyAllowanceIDMost+1) },
		"no jobs":      func(a *Allowance) { a.Jobs = nil },
		"job zero":     func(a *Allowance) { a.Jobs = []int{0} },
		"repeated job": func(a *Allowance) { a.Jobs = []int{3, 3} },
		"zero cap":     func(a *Allowance) { a.CapUSD = 0 },
		"huge cap":     func(a *Allowance) { a.CapUSD = PolicyCapMostUSD + 1 },
		"bad end":      func(a *Allowance) { a.EndsAt = "next week" },
	}
	for name, spoil := range bad {
		a := good
		a.Jobs = append([]int(nil), good.Jobs...)
		spoil(&a)
		if CheckAllowances([]Allowance{a}) == nil {
			t.Errorf("%s was kept", name)
		}
	}
	if CheckAllowances([]Allowance{good, good}) == nil {
		t.Error("two allowances with one id were kept")
	}
	if _, err := CleanPolicyGroup(" ws_8f2c1a "); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"", "  ", "a\nb", strings.Repeat("g", PolicyGroupMost+1)} {
		if _, err := CleanPolicyGroup(group); err == nil {
			t.Errorf("group %q was kept", group)
		}
	}
}

// A JOB WAITS ON AT MOST TWENTY DISTINCT OTHER JOBS, never on itself.
func TestWaitsOnIsDistinctOthers(t *testing.T) {
	if err := CheckWaitsOn(5, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	for name, waits := range map[string][]int{"itself": {5}, "repeat": {1, 1}, "zero": {0}} {
		if CheckWaitsOn(5, waits) == nil {
			t.Errorf("%s was kept", name)
		}
	}
	many := make([]int, WaitsOnMost+1)
	for i := range many {
		many[i] = i + 10
	}
	if CheckWaitsOn(5, many) == nil {
		t.Error("too many waits were kept")
	}
}

// AN ALLOWANCE IS SPENT BY THE JOBS IT STARTED, and only those: a running job
// holds its whole budget, a finished one only what it spent, and a job another
// group's allowance of the same id started is not counted.
func TestAnAllowanceCountsOnlyTheJobsItStarted(t *testing.T) {
	items := []Item{
		{ID: 1, PolicyRun: "a", PolicyGroup: "g", Cap: 4, Stream: &Stream{Spent: 1}, State: StateRunning},
		{ID: 2, PolicyRun: "a", PolicyGroup: "g", Cap: 4, Stream: &Stream{Spent: 2.5}, DoneHow: "merged"},
		{ID: 3, PolicyRun: "a", PolicyGroup: "other", Cap: 9, Stream: &Stream{Spent: 9}},
		{ID: 4, Cap: 9, Stream: &Stream{Spent: 9}},
	}
	s := PolicySpend(items, "g")["a"]
	if s.Spent != 3.5 || s.Committed != 6.5 {
		t.Fatalf("spend = %+v, want spent 3.5 and committed 4 + 2.5", s)
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	a := Allowance{ID: "a", Jobs: []int{1}, CapUSD: 5, EndsAt: "2026-11-10T00:00:00Z"}
	p := GroupPolicy{Group: "g"}
	if got := AllowanceState(p, a, 3.5, now); got != AllowanceActive {
		t.Fatalf("state %s", got)
	}
	if got := AllowanceState(p, a, 5, now); got != AllowanceSpent {
		t.Fatalf("state %s", got)
	}
	if got := AllowanceState(p, a, 0, now.Add(12*time.Hour)); got != AllowanceEnded {
		t.Fatalf("an allowance at its end reads %s", got)
	}
	p.Stopped = true
	if got := AllowanceState(p, a, 0, now); got != AllowanceEnded {
		t.Fatalf("a stopped policy's allowance reads %s", got)
	}
}
