package session_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestPlanCardKindsAndTargetsSurviveJSON(t *testing.T) {
	plan := session.Plan{
		ID: "plan-1", ReachesBeyond: true,
		Steps: []session.PlanCardStep{
			{Kind: session.PlanStepHold, Target: session.PlanTarget{Task: "launch"}, Text: "Hold Launch post"},
			{Kind: session.PlanStepSteer, Target: session.PlanTarget{Chat: "draft"}, Text: "Use the new pricing"},
			{Kind: session.PlanStepStart, Target: session.PlanTarget{Task: "suite"}, Text: "Run the v1 suite"},
			{Kind: session.PlanStepStop, Target: session.PlanTarget{Task: "old-suite"}, Text: "Stop the old suite"},
			{Kind: session.PlanStepAskPlace, Target: session.PlanTarget{Place: "software"}, Text: "Ask Software"},
			{Kind: session.PlanStepRemember, Target: session.PlanTarget{Place: "marketing"}, Text: "Run the suite before launch"},
		},
	}
	want := `{"id":"plan-1","steps":[{"kind":"hold","target":{"task":"launch"},"text":"Hold Launch post"},{"kind":"steer","target":{"chat":"draft"},"text":"Use the new pricing"},{"kind":"start","target":{"task":"suite"},"text":"Run the v1 suite"},{"kind":"stop","target":{"task":"old-suite"},"text":"Stop the old suite"},{"kind":"ask-place","target":{"place":"software"},"text":"Ask Software"},{"kind":"remember","target":{"place":"marketing"},"text":"Run the suite before launch"}],"reachesBeyond":true}`
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("plan JSON = %s, want %s", data, want)
	}
	var decoded session.Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, plan) {
		t.Fatalf("decoded plan = %+v, want %+v", decoded, plan)
	}
}

func TestPlanCardLocalActionsKeepExplicitConfirmationBoundary(t *testing.T) {
	plan := session.Plan{ID: "local", Steps: []session.PlanCardStep{
		{Kind: session.PlanStepSteer, Target: session.PlanTarget{Chat: "current"}, Text: "Use a shorter title"},
	}}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["reachesBeyond"]) != "false" {
		t.Fatalf("local action boundary = %s, want explicit false for direct execution", fields["reachesBeyond"])
	}
}

func TestPlanCardEventSurvivesRemoteWire(t *testing.T) {
	// Existing peers read numeric event kinds, so the new kind must follow the
	// last shipped value without changing that value's meaning.
	if session.EventToolOutput != 55 || session.EventPlan != 56 {
		t.Fatalf("event wire numbers changed: output=%d, plan=%d", session.EventToolOutput, session.EventPlan)
	}
	for _, beyond := range []bool{false, true} {
		original := session.Event{Kind: session.EventPlan, Plan: &session.Plan{
			ID: "plan-remote", ReachesBeyond: beyond,
			Steps: []session.PlanCardStep{{Kind: session.PlanStepRemember, Target: session.PlanTarget{Place: "marketing"}, Text: "Remember the launch rule"}},
		}}
		data, err := json.Marshal(remote.WireEvent(original))
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields["plan"]) == 0 {
			t.Fatalf("plan payload missing from wire: %s", data)
		}
		var decoded remote.EventWire
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if got := decoded.Unwire(); !reflect.DeepEqual(got, original) {
			t.Fatalf("wire event = %+v, want %+v", got, original)
		}
	}
	data, err := json.Marshal(remote.WireEvent(session.Event{Kind: session.EventThinking}))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["plan"]; ok {
		t.Fatalf("unrelated event has a plan: %s", data)
	}
}
