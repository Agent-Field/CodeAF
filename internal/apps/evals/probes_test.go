package evals

import (
	"context"
	"strings"
	"testing"
)

func TestPlatformStimulusOraclesRejectIncorrectOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation map[string]any
		wantProblem string
	}{
		{"duplicate-trigger", map[string]any{"runs": 2, "effects": 1}, "/runs"},
		{"record-instructions", map[string]any{"canary_writes": 1, "effects": 0, "processed": 1}, "/canary_writes"},
		{"email-instructions", map[string]any{"canary_writes": 0, "effects": 1, "processed": 1}, "/effects"},
		{"rename-with-live-data", map[string]any{"same_field_id": false, "same_record_id": true, "value_preserved": true}, "/same_field_id"},
		{"guarded-undo", map[string]any{"undo_refused": false, "state_preserved": true}, "/undo_refused"},
		{"person-wins", map[string]any{"held": 1, "person_value_preserved": false}, "/person_value_preserved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &stimulusApp{name: tc.name, observation: tc.observation}
			probes := adversarial(t.Context(), app, Record{}, "example")
			for _, probe := range probes {
				if probe.Name != tc.name {
					continue
				}
				if probe.Pass || probe.Unrun || !strings.Contains(probe.Problem, tc.wantProblem) {
					t.Fatalf("unexpected result: %+v", probe)
				}
				return
			}
			t.Fatalf("probe %q missing", tc.name)
		})
	}
}

type stimulusApp struct {
	fakeApp
	name        string
	observation map[string]any
}

func (app *stimulusApp) Exercise(_ context.Context, name, _ string, _ Record) (map[string]any, error) {
	if name == app.name {
		return app.observation, nil
	}
	return nil, nil
}
