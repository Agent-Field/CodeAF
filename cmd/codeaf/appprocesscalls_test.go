package main

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppProcessCallsReachTheWriterOverTheWire(t *testing.T) {
	backend, _ := dataBackend(t)
	s, err := backend.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterApp([]byte(`{
		"id":"ops","name":"Operations",
		"records":{"case":{"fields":{"name":{"type":"text"},"stage":{"type":"enum","values":["Open","Closed"]}},
		"lifecycle":{"field":"stage","transitions":[{"id":"close","from":["Open"],"to":"Closed","guard":"checklist_done('checks')"}]}}},
		"policies":[{"id":"checks","kind":"checklist","config":{"record":"case","steps":[{"id":"review","label":"Review","evidence":"note"}]}}],
		"capabilities":{"records":{"case":"rw"}}
	}`)); err != nil {
		t.Fatal(err)
	}
	w := newActionWire(t, backend)
	call := func(method string, params any, result any) {
		t.Helper()
		raw, wireErr := w.call(method, params)
		if wireErr != nil {
			t.Fatalf("%s: %v", method, wireErr)
		}
		if err := json.Unmarshal(raw, result); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	var created appwire.DataWriteResult
	call("data.create", appwire.DataCreateParams{Type: "case", Set: map[string]any{"name": "Review release"}}, &created)
	id := created.Rows[0]["id"].(string)
	params := appwire.ProcessParams{DataScope: appwire.DataScope{App: "ops"}, Record: appwire.DataRecordRef{Type: "case", ID: id}}

	var work appwire.DataItemResult
	call("data.work.create", appwire.DataWorkCreateParams{DataScope: params.DataScope, Type: "case", Record: id, Title: "Review"}, &work)
	workID := work.Item["id"].(string)
	var itemList appwire.DataItemsResult
	call("work_items.list", appwire.DataScope{}, &itemList)
	if len(itemList.Items) != 1 || itemList.Items[0]["id"] != workID {
		t.Fatalf("My work = %+v", itemList)
	}
	call("work_items.reassign", map[string]any{"id": workID, "assignee": ""}, &work)
	call("work_items.claim", appwire.DataIDParams{ID: workID}, &work)
	if work.Item["assignee"].(map[string]any)["name"] != "me" {
		t.Fatalf("claimed item = %+v", work)
	}
	var timer, signed appwire.DataItemResult
	call("data.timers.create", appwire.DataTimerCreateParams{Type: "case", Record: id, FireAt: "+3d", Kind: "follow_up"}, &timer)
	call("data.signoffs.create", appwire.DataSignParams{Type: "case", ID: id, Statement: "Ready for review"}, &signed)

	var snapshot appwire.DataProcessResult
	call("data.process", params, &snapshot)
	if snapshot.State != "Open" || snapshot.Rev != 1 || len(snapshot.Transitions) != 1 ||
		snapshot.Transitions[0].Ready || snapshot.Transitions[0].Blocked == "" || len(snapshot.WorkItems) != 1 ||
		len(snapshot.Timers) != 1 || len(snapshot.Signoffs) != 1 || snapshot.Signoffs[0]["current"] != true {
		t.Fatalf("before = %+v", snapshot)
	}
	move := appwire.TransitionBatchParams{DataScope: params.DataScope, Transition: "close",
		Records: []appwire.TransitionSelection{{Record: params.Record, ExpectedRev: 1}}}
	if _, wireErr := w.call("data.transition_batch", move); wireErr == nil || wireErr.Code != appwire.CodeLifecycle {
		t.Fatalf("guard refusal = %v", wireErr)
	}
	call("work_items.complete", map[string]any{"id": workID, "values": map[string]any{"outcome": "reviewed"}}, &work)
	if work.Item["status"] != "done" || work.Item["outcome"].(map[string]any)["outcome"] != "reviewed" {
		t.Fatalf("completed item = %+v", work)
	}
	check := map[string]any{"app": "ops", "record": params.Record, "checklist": "checks", "step": "review", "done": true,
		"expected_rev": "1", "evidence": map[string]any{"kind": "note", "text": "Checked the release"}}
	call("data.checklist_set", check, &snapshot)
	if snapshot.Rev != 2 || !snapshot.Checklist.Steps[0].Done ||
		!snapshot.Transitions[0].Ready || len(snapshot.WorkItems) != 0 || snapshot.Signoffs[0]["current"] != false {
		t.Fatalf("after checklist = %+v", snapshot)
	}
	move.Records[0].ExpectedRev = 2
	var receipt appwire.DataWriteResult
	call("data.transition_batch", move, &receipt)
	if receipt.Batch == "" || len(receipt.Rows) != 1 || receipt.Rows[0]["stage"] != "Closed" {
		t.Fatalf("move = %+v", receipt)
	}
}
