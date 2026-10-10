package desktopbridge

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestPlanCardEventHasNamedJSONPayload(t *testing.T) {
	plan := &session.Plan{ID: "plan-1", ReachesBeyond: true, Steps: []session.PlanCardStep{
		{Kind: session.PlanStepAskPlace, Target: session.PlanTarget{Place: "software"}, Text: "Run the v1 suite"},
	}}
	ev := session.Event{Kind: session.EventPlan, Plan: plan}
	// This is the pump's record shape, so both the named kind and the raw
	// payload are checked at the JSON boundary the renderer actually reads.
	record := Record{Type: "event", Event: &Event{Kind: eventKind(ev.Kind), Raw: remote.WireEvent(ev)}}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Type  string `json:"type"`
		Event struct {
			Kind string `json:"kind"`
			Raw  struct {
				Plan *session.Plan `json:"plan"`
			} `json:"raw"`
		} `json:"event"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "event" || decoded.Event.Kind != "plan" || !reflect.DeepEqual(decoded.Event.Raw.Plan, plan) {
		t.Fatalf("plan event lost its name or payload: %s", data)
	}
}
