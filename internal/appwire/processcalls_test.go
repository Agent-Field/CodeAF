package appwire

import (
	"fmt"
	"testing"
)

type processCallEngine struct {
	*fakeEngine
	ProcessCalls
}

func (e processCallEngine) DataTransitionBatch(p TransitionBatchParams) (*DataWriteResult, error) {
	return &DataWriteResult{Batch: "move", Rows: []DataRow{{"id": p.Records[0].Record.ID, "_rev": int64(p.Records[0].ExpectedRev) + 1}}}, nil
}

func (e processCallEngine) DataChecklistSet(p ChecklistSetParams) (*DataProcessResult, error) {
	return &DataProcessResult{Checklist: &ProcessChecklist{ID: p.Checklist, Steps: []ProcessChecklistStep{{ID: p.Step, Done: *p.Done}}}}, nil
}

func TestProcessCallsWireRevisionsAndRequiredFields(t *testing.T) {
	fake := &fakeEngine{}
	w := startWireOver(t, processCallEngine{fakeEngine: fake}, fake)
	w.next()
	for _, tc := range []struct {
		name, method, params string
		bad                  bool
	}{
		{"quoted revision", "data.transition_batch", `{"transition":"open","records":[{"record":{"type":"case","id":"one"},"expected_rev":"9"}]}`, false},
		{"numeric revision", "data.transition_batch", `{"transition":"open","records":[{"record":{"type":"case","id":"one"},"expected_rev":9}]}`, false},
		{"missing revision", "data.transition_batch", `{"transition":"open","records":[{"record":{"type":"case","id":"one"}}]}`, true},
		{"missing done", "data.checklist_set", `{"record":{"type":"case","id":"one"},"checklist":"checks","step":"review","expected_rev":9}`, true},
		{"explicit false", "data.checklist_set", `{"record":{"type":"case","id":"one"},"checklist":"checks","step":"review","expected_rev":9,"done":false}`, false},
		{"missing record", "data.process", `{}`, true},
		{"missing id", "work_items.claim", `{}`, true},
		{"missing assignee", "work_items.reassign", `{"id":"one"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.send(fmt.Sprintf(`{"id":1,"method":%q,"params":%s}`, tc.method, tc.params))
			reply := w.next()
			if tc.bad {
				if e, _ := reply["error"].(map[string]any); e["code"] != CodeBadParams {
					t.Fatalf("bad request reply = %v", reply)
				}
				return
			}
			got, ok := reply["result"].(map[string]any)
			if !ok {
				t.Fatalf("reply = %v", reply)
			}
			if tc.method == "data.transition_batch" {
				row := got["rows"].([]any)[0].(map[string]any)
				if row["_rev"] != float64(10) || row["id"] != "one" || got["batch"] != "move" {
					t.Fatalf("batch = %v", got)
				}
			} else if step := got["checklist"].(map[string]any)["steps"].([]any)[0].(map[string]any); step["done"] != false {
				t.Fatalf("checklist step = %v", step)
			}
		})
	}
}
