package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestStepLabelTravelsWithoutChangingIdentity(t *testing.T) {
	for _, tc := range []struct{ name, field, want, wire string }{
		{"label", `,"Label":"Security review"`, "Security review", "Security review"},
		{"old job", "", "Securityrevi", ""},
		{"empty", `,"Label":""`, "Securityrevi", ""},
		{"whitespace", `,"Label":"   "`, "Securityrevi", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var st factory.Stage
			if err := json.Unmarshal([]byte(`{"Name":"securityrevi","Kind":"chat","Until":"clean","On":true`+tc.field+`}`), &st); err != nil {
				t.Fatal(err)
			}
			it := factory.Item{Stages: []factory.Stage{st}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "securityrevi", State: factory.PhaseDone, Result: &factory.StageResult{Done: true}}}}}
			facts := factory.BriefFacts(it)
			if len(facts) != 1 || facts[0].Label != tc.want {
				t.Errorf("shown label = %+v, want %q", facts, tc.want)
			}
			if len(facts) == 1 && (facts[0].Key != "securityrevi" || facts[0].Evidence.Step != "securityrevi") {
				t.Error("label changed fact identity")
			}
			job := api.FromItem(it)
			raw, err := json.Marshal(job)
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Steps []struct {
					Name  string
					Label string
				}
				Step struct {
					Name  string
					Label string
				}
			}
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Steps) != 1 || payload.Steps[0].Name != "securityrevi" || payload.Steps[0].Label != tc.wire || payload.Step.Name != "securityrevi" || payload.Step.Label != tc.wire {
				t.Errorf("step wire lost label or identity: %s", raw)
			}
			restored := api.ToItem(job)
			if restored.Stages[0].Name != "securityrevi" {
				t.Error("round trip changed id")
			}
			stageJSON, _ := json.Marshal(restored.Stages[0])
			var saved struct{ Label string }
			_ = json.Unmarshal(stageJSON, &saved)
			if saved.Label != tc.wire {
				t.Errorf("round trip lost label: %s", stageJSON)
			}
			if tc.field == "" {
				before, _ := json.Marshal(st)
				after, _ := json.Marshal(restored.Stages[0])
				if string(before) != string(after) || strings.Contains(string(after), `"Label"`) || strings.Contains(string(raw), `"label"`) {
					t.Errorf("old saved step changed: %s => %s", before, after)
				}
			}
		})
	}
}
