package main

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

type appAIIdentity struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Harness string `json:"harness"`
}

func appAIIdentityFor(r runs.Run) appAIIdentity {
	harness := r.Harness
	if harness == "" {
		harness = "codeaf"
	}
	names := map[string]string{"codeaf": "codeaf", "claude_code": "Claude Code", "codex": "Codex", "agentfield": "AgentField"}
	return appAIIdentity{ID: r.App, Name: names[harness], Harness: harness}
}

type appAIStep struct {
	ID      string    `json:"id"`
	Label   string    `json:"label"`
	Kind    string    `json:"kind"`
	State   string    `json:"state"`
	Started time.Time `json:"started_at"`
	Ended   time.Time `json:"ended_at,omitzero"`
}

type appAILog struct {
	At   time.Time `json:"at"`
	Step string    `json:"step"`
	Text string    `json:"text"`
}

type appAIArtifact struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Queue string `json:"queue"`
}

type appAICost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type appAIProjection struct {
	ID        string                           `json:"id"`
	Agent     appAIIdentity                    `json:"agent"`
	Record    *runs.Subject                    `json:"record,omitempty"`
	Screen    string                           `json:"screen,omitempty"`
	Title     string                           `json:"title"`
	State     string                           `json:"state"`
	Steps     []appAIStep                      `json:"steps"`
	Log       []appAILog                       `json:"log,omitempty"`
	Cost      *appAICost                       `json:"cost,omitempty"`
	Output    string                           `json:"output,omitempty"`
	Artifacts []appAIArtifact                  `json:"artifacts,omitempty"`
	Approvals []*appwire.AppAgentPendingEffect `json:"approvals,omitempty"`
	Allowed   map[string]bool                  `json:"allowed,omitempty"`
}

func appAIRunProjection(r runs.Run) appAIProjection {
	title := r.Title
	if title == "" {
		title = r.DefinitionID
	}
	step := r.Step
	if step == "" {
		step = r.FirstStep
		if step == "" {
			step = "result"
		}
	}
	row := appAIProjection{
		ID: r.ID, Agent: appAIIdentityFor(r), Record: r.Subject, Screen: r.Screen, Title: title, State: r.State,
		Steps: []appAIStep{{ID: step, Label: title, Kind: "agent", State: r.State, Started: r.StartedAt, Ended: r.EndedAt}},
		Log:   []appAILog{{At: r.StartedAt, Step: step, Text: "Run started"}},
	}
	if r.Error != "" {
		row.Log = append(row.Log, appAILog{At: r.UpdatedAt, Step: step, Text: r.Error})
	}
	if r.Cost > 0 {
		row.Cost = &appAICost{Amount: r.Cost, Currency: "USD"}
	}
	if r.Proposal != nil && r.State == runs.Done {
		row.Output = appAIValues(r.Proposal.Set)
		row.Artifacts = []appAIArtifact{{ID: r.ID, Kind: "draft", Label: "Review proposed change", Queue: r.Queue}}
	}
	if effect := appAIPendingEffect(r); effect != nil {
		row.Approvals = []*appwire.AppAgentPendingEffect{effect}
	}
	if r.State == runs.Running || r.State == runs.YourCall {
		row.Allowed = map[string]bool{"stop": true, "steer": r.State == runs.Running && r.CanSteer}
	}
	return row
}

type appAIReviewProposal struct {
	ID       string        `json:"id"`
	Record   runs.Subject  `json:"record"`
	Title    string        `json:"title"`
	Agent    appAIIdentity `json:"agent"`
	Field    string        `json:"field"`
	Before   string        `json:"before"`
	After    string        `json:"after"`
	Safe     bool          `json:"safe"`
	Revision string        `json:"expected_rev"`
}

func (b *appBackend) appAIReviewQueue(app, queue string) ([]appAIReviewProposal, error) {
	rows, err := b.ListRuns(app, "")
	if err != nil {
		return nil, err
	}
	db, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	decided, err := db.AIReviews(app, queue)
	if err != nil {
		return nil, err
	}
	out := []appAIReviewProposal{}
	for _, r := range rows {
		draft := r.Proposal
		if r.Queue != queue || draft == nil || r.State != runs.Done || decided[r.ID] != "" {
			continue
		}
		safe, err := db.AIReviewSafe(data.Scope{App: app}, draft.Type, data.AIEffect{Record: draft.Record, Revision: draft.Revision, Set: draft.Set})
		if err != nil {
			return nil, appDataError(err)
		}
		out = append(out, appAIReviewProposal{
			ID: r.ID, Record: runs.Subject{Type: draft.Type, ID: draft.Record}, Title: r.Title, Agent: appAIIdentityFor(r),
			Field: strings.Join(slices.Sorted(maps.Keys(draft.Set)), ", "), Before: appAIValues(draft.Before), After: appAIValues(draft.Set),
			Safe: safe, Revision: strconv.FormatInt(draft.Revision, 10),
		})
	}
	return out, nil
}

func appAIValues(values map[string]any) string {
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		for _, v := range values {
			if text, ok := v.(string); ok {
				return text
			}
		}
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}

type appAIPresence struct {
	Agent  appAIIdentity `json:"agent"`
	Record runs.Subject  `json:"record"`
	Task   string        `json:"task"`
}

func (b *appBackend) appAIPresence(app, typ string) ([]appAIPresence, error) {
	rows, err := b.ListRuns(app, "")
	out := []appAIPresence{}
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if (r.State == runs.Running || r.State == runs.YourCall) && r.Subject != nil && r.Subject.Type == typ && r.Harness != "" {
			out = append(out, appAIPresence{Agent: appAIIdentityFor(r), Record: *r.Subject, Task: r.Title})
		}
	}
	return out, nil
}
