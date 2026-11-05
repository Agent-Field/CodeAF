package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// P2, P5: a policy block crosses even after waits retire; held stays a boolean.
func TestPolicyBlockRead(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 1, Kind: factory.KindIssue, State: factory.StateNew, Group: "g", WaitsOn: []int{2}, WaitsReleased: true, Blocked: &factory.JobBlock{By: factory.BlockPolicy, Reason: "no allowance covers it", Since: now}}
	j := FromItem(it)
	if j.Group != "g" || j.Blocked == nil || j.Blocked.Reason != "no allowance covers it" || j.State != "backlog" || j.NeedsYou != nil || j.Held {
		t.Fatalf("read = %+v", j)
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if raw, ok := fields["held"]; ok {
		var held bool
		if err := json.Unmarshal(raw, &held); err != nil || held {
			t.Fatalf("held = %s, %v", raw, err)
		}
	}
	if restored := ToItem(j); restored.Group != "g" || restored.PolicyRun != "" || restored.Blocked != nil {
		t.Fatalf("client authored policy state: %+v", restored)
	}
}

// P4, P9: the end instant wins over a reached cap and an unset read has [].
func TestPolicyReadSpendingAndState(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	p := factory.GroupPolicy{Group: "g", Allowances: []factory.Allowance{{ID: "m2", Jobs: []int{1, 99}, CapUSD: 5, EndsAt: now.Format(time.RFC3339)}}}
	read := PolicyFrom(p, []factory.Item{{ID: 1, Group: "g", SpentBefore: 2, Stream: &factory.Stream{Spent: 3}}}, now)
	if read.SpentUSD != 5 || read.Allowances[0].SpentUSD != 5 || read.Allowances[0].State != "ended" {
		t.Fatal(read)
	}
	read.Allowances[0].Jobs[0] = 100
	if p.Allowances[0].Jobs[0] != 1 {
		t.Fatal("read mutated saved scope")
	}
	if p := PolicyFrom(factory.GroupPolicy{Group: "empty"}, nil, now); p.Allowances == nil {
		t.Fatal("empty allowances are null")
	}
}
