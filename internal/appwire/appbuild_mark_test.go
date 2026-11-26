package appwire

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
)

type markWireEngine struct {
	fakeEngine
	jobs *builder.Jobs
}

func (e *markWireEngine) AppBuilds() (*builder.Jobs, error) {
	return e.jobs, nil
}

func TestAppMarksKeepTheDeskNodeID(t *testing.T) {
	jobs, err := builder.OpenJobs(builder.Options{
		Home: t.TempDir(),
		Model: func() (builder.Completer, error) {
			return nil, errors.New("No model needed to persist a mark.")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(jobs.Close)
	engine := &markWireEngine{jobs: jobs}

	for _, tc := range []struct {
		method, name, node string
		target             map[string]any
	}{
		{"apps.say", "desk", "deal-board", map[string]any{"node_id": "deal-board"}},
		{"apps.mark", "both spellings", "_deal-board", map[string]any{"node": "old-deal-board", "node_id": "_deal-board"}},
		{"apps.say", "legacy", "old-deal-board", map[string]any{"node": "old-deal-board"}},
	} {
		t.Run(tc.method+"/"+tc.name, func(t *testing.T) {
			opened, err := methods["apps.new"].call(engine, json.RawMessage(`{"name":"CRM"}`))
			if err != nil {
				t.Fatal(err)
			}
			draft := opened.(*draftParams).Draft
			tc.target["screen"] = "deals"
			tc.target["path"] = []string{"body"}
			request, err := json.Marshal(map[string]any{"draft": draft, "text": "Show the stage", "target": tc.target})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := methods[tc.method].call(engine, request); err != nil {
				t.Fatal(err)
			}
			reply, err := methods["apps.build_state"].call(engine, json.RawMessage(`{"draft":"`+draft+`"}`))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(reply)
			if err != nil {
				t.Fatal(err)
			}
			var state struct {
				Marks []struct {
					Target struct {
						Node string `json:"node"`
					} `json:"target"`
				} `json:"marks"`
			}
			if err := json.Unmarshal(raw, &state); err != nil {
				t.Fatal(err)
			}
			if len(state.Marks) != 1 || state.Marks[0].Target.Node != tc.node {
				t.Fatalf("Saved marks = %+v, want one mark on %q.", state.Marks, tc.node)
			}
		})
	}
}
