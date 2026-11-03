package api_test

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestStepLabelReachesQueueStrip(t *testing.T) {
	for _, label := range []string{"Security review", ""} {
		it := factory.Item{Kind: factory.KindIssue, State: factory.StateRunning,
			Stages: []factory.Stage{{Name: "securityrevi", Label: label, On: true}},
			Stream: &factory.Stream{Phases: []factory.Phase{{Name: "securityrevi", State: factory.PhaseRunning}}}}
		raw, err := json.Marshal(api.FromSnapshot(factory.Snapshot{Items: []factory.Item{it}}, ""))
		if err != nil { t.Fatal(err) }
		var payload struct { Strip []struct { Step, Label string } }
		if err := json.Unmarshal(raw, &payload); err != nil { t.Fatal(err) }
		if len(payload.Strip) != 1 || payload.Strip[0].Step != "securityrevi" || payload.Strip[0].Label != label { t.Fatalf("queue lost step label or id: %s", raw) }
	}
}
