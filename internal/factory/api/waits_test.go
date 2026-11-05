package api

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// W11: the new fields preserve held, Backlog, Start and the needs-you contract.
func TestWaitsReadShape(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 3, Kind: factory.KindIssue, State: factory.StateNew, WaitsOn: []int{1, 2}, WaitingOn: []int{1, 2}}
	before := CanFor(it, "", at)
	for _, blocked := range []bool{false, true} {
		if blocked {
			it.Blocked = &factory.JobBlock{By: factory.BlockWaitsOn, Reason: "waits on job 1, which failed at Test", Since: at, Job: 1}
		}
		j := FromItem(it)
		if j.State != StateBacklog || j.NeedsYou != nil || j.Held || !reflect.DeepEqual(CanFor(it, "", at), before) {
			t.Fatalf("job doors or state changed: %+v", j)
		}
		raw, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["waits_on"]) != "[1,2]" || string(fields["waiting_on"]) != "[1,2]" {
			t.Fatalf("wait fields: %s", raw)
		}
		if _, ok := fields["blocked"]; ok != blocked {
			t.Fatalf("blocked present = %v", ok)
		}
		if blocked {
			var block map[string]json.RawMessage
			if err := json.Unmarshal(fields["blocked"], &block); err != nil {
				t.Fatal(err)
			}
			if string(block["by"]) != `"waits_on"` || string(block["job"]) != "1" || string(block["since"]) != `"2026-11-05T12:00:00Z"` || len(block["reason"]) == 0 {
				t.Fatalf("block = %s", fields["blocked"])
			}
		}
	}
	it.State, it.Stream = factory.StateQueued, &factory.Stream{Paused: true}
	j := FromItem(it)
	if !j.Held || len(j.WaitingOn) != 0 || j.Blocked != nil {
		t.Fatalf("person's Hold = %+v", j)
	}
}

// W1, W11: reads copy the persisted waiting list and hide released or retired waits.
func TestWaitsReadPersistedFields(t *testing.T) {
	it := factory.Item{ID: 4, Kind: factory.KindIssue, State: factory.StateNew, WaitsOn: []int{3, 2, 1}, WaitingOn: []int{2, 1}}
	j := FromItem(it)
	if !reflect.DeepEqual(j.WaitsOn, it.WaitsOn) || !reflect.DeepEqual(j.WaitingOn, it.WaitingOn) {
		t.Fatalf("job = %+v", j)
	}
	j.WaitsOn[0], j.WaitingOn[0] = 99, 99
	if !reflect.DeepEqual(it.WaitsOn, []int{3, 2, 1}) || !reflect.DeepEqual(it.WaitingOn, []int{2, 1}) {
		t.Fatal("read changed the item's waits")
	}
	q := FromSnapshot(factory.Snapshot{Items: []factory.Item{it}}, "")
	if len(q.Jobs) != 1 || !reflect.DeepEqual(q.Jobs[0].WaitingOn, it.WaitingOn) {
		t.Fatalf("snapshot = %+v", q.Jobs)
	}
	it.Blocked = &factory.JobBlock{By: factory.BlockWaitsOn, Reason: "waits on job 2, which failed", Job: 2}
	for _, change := range []func(*factory.Item){
		func(cur *factory.Item) { cur.WaitsReleased = true },
		func(cur *factory.Item) { cur.State = factory.StateQueued },
		func(cur *factory.Item) { cur.Stream = &factory.Stream{} },
		func(cur *factory.Item) { cur.Failure = &factory.Failure{Code: factory.CodeStopped} },
	} {
		cur := it
		change(&cur)
		j = FromItem(cur)
		raw, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if j.WaitingOn != nil || j.Blocked != nil || !reflect.DeepEqual(j.WaitsOn, it.WaitsOn) || fields["waiting_on"] != nil || fields["blocked"] != nil {
			t.Fatalf("inert waits = %s", raw)
		}
	}
}
