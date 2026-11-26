package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/outputs"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

// CheckDraftRun executes a run on the draft's own sample store with a
// separate journal, fake agent and fake connectors.
func (b *appBackend) CheckDraftRun(ctx context.Context, draft, run string, def schema.Definition) error {
	root, err := os.MkdirTemp(filepath.Join(home.Dir(), "apps", "_drafts", draft), ".run-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	app, err := schema.ParseApp(def.Files[schema.AppFile])
	if err != nil {
		return err
	}
	sample, err := b.dataStore(draft)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	triggers := []runs.Trigger{}
	for _, trigger := range app.Triggers {
		if trigger.Run == run && trigger.On == "record.changed" {
			triggers = append(triggers, runs.Trigger{
				App: app.ID, ID: trigger.ID, On: trigger.On, Run: run, Type: trigger.Type, When: trigger.When,
			})
		}
	}
	engine, err := b.openScratchRuns(root, draft, app, def, triggers, func() time.Time { return now })
	if err != nil {
		return err
	}
	defer engine.Close()

	seenTypes := map[string]bool{}
	for _, trigger := range triggers {
		if seenTypes[trigger.Type] {
			continue
		}
		seenTypes[trigger.Type] = true
		page, err := sample.Query(data.Scope{App: app.ID, Agent: true}, trigger.Type, "", "", 50)
		if err != nil {
			return err
		}
		for _, row := range page.Rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			id, _ := row["id"].(string)
			if err := engine.RecordChanged(runs.Change{Type: trigger.Type, ID: id, Before: map[string]any{}, After: row}); err != nil {
				return err
			}
		}
	}
	started, err := engine.List(app.ID, run)
	if err != nil {
		return err
	}
	if len(triggers) > 0 && len(started) == 0 {
		return &check.RunProblem{Detail: "Include an example record that matches the automation's record change trigger."}
	}
	if len(started) == 0 {
		definition, err := scratchRun(def, run)
		if err != nil {
			return err
		}
		params, err := sampleRunParams(sample, app.ID, definition)
		if err != nil {
			return err
		}
		r, err := engine.Start(app.ID, run, params)
		if err != nil {
			return &check.RunProblem{Detail: fmt.Sprintf("The run %s could not start on the example records: %v", run, err)}
		}
		started = append(started, r)
	}
	for _, entry := range started {
		if err := tryScratchRun(ctx, engine, entry.ID, &now); err != nil {
			return err
		}
	}
	return nil
}

func scratchRun(def schema.Definition, id string) (runs.Definition, error) {
	for _, file := range def.RunFiles() {
		if schema.FileID(file) != id {
			continue
		}
		run, err := runs.Parse(def.Files[file])
		if err != nil {
			return runs.Definition{}, &check.RunProblem{Detail: fmt.Sprintf("The run %s could not start: %v", id, err)}
		}
		return run, nil
	}
	return runs.Definition{}, fs.ErrNotExist
}

func (b *appBackend) openScratchRuns(root, draft string, app schema.App, def schema.Definition, triggers []runs.Trigger, now func() time.Time) (*runs.Engine, error) {
	records := runs.Data{Store: func() (*data.Store, error) { return b.dataStore(draft) }}
	replies := map[string]runs.Reply{}
	for connector, cap := range app.Capabilities.Connectors {
		for _, verb := range cap.Can {
			replies[connector+"."+verb] = runs.Reply{Output: map[string]any{
				"id": "sample-item", "thread": "sample-thread", "text": "Example connector response.",
			}}
		}
	}
	return runs.Open(runs.Config{
		Root: root, Now: now,
		Load: func(id, run string) (runs.Definition, error) {
			if id != app.ID {
				return runs.Definition{}, store.ErrNotFound
			}
			return scratchRun(def, run)
		},
		Triggers:  func() ([]runs.Trigger, error) { return triggers, nil },
		Connector: runs.NewFakeConnector(replies),
		Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
			if call.AI == nil {
				return runs.Reply{Output: "Example draft from this app."}, nil
			}
			if call.AI.Example == nil {
				return runs.Reply{}, &check.RunProblem{Detail: "Add an example JSON response to the AI step so Check can exercise it without calling a model."}
			}
			output, err := json.Marshal(call.AI.Example)
			return runs.Reply{Output: string(output)}, err
		},
		Render: func(ctx context.Context, call runs.Call, action schema.RenderAction) (runs.Reply, error) {
			sample, err := records.Store()
			if err != nil {
				return runs.Reply{}, err
			}
			return renderRunFile(ctx, call, action, func(ctx context.Context, invocation outputs.Invocation) (data.Output, error) {
				return outputs.Invoke(ctx, sample, app, invocation)
			})
		},
		Records: records.Write, ReadRecords: records.ReadRecords,
		ResolveParams: records.ResolveParams, RecordInput: records.RecordInput,
	})
}

func sampleRunParams(sample *data.Store, app string, def runs.Definition) (map[string]any, error) {
	params := map[string]any{}
	for name, param := range def.Params {
		if param.Default != nil && param.Type != "refs" {
			continue
		}
		switch param.Type {
		case "refs":
			query, queryDefault := param.Default.(string)
			if queryDefault || param.Default == nil {
				page, err := sample.Query(data.Scope{App: app, Agent: true}, param.To, query, "", 1)
				if err != nil {
					return nil, &check.RunProblem{Detail: fmt.Sprintf("The record query for %s failed on example data: %v", name, err)}
				}
				if len(page.Rows) == 0 {
					return nil, &check.RunProblem{Detail: fmt.Sprintf("Include a sample %s that matches the query for %s.", param.To, name)}
				}
				params[name] = query
			}
		case "ref":
			page, err := sample.Query(data.Scope{App: app, Agent: true}, param.To, "", "", 1)
			if err != nil || len(page.Rows) == 0 {
				return nil, &check.RunProblem{Detail: fmt.Sprintf("Include a sample %s for %s.", param.To, name)}
			}
			params[name] = page.Rows[0]["id"]
		case "text", "longtext":
			params[name] = "Example"
		case "number", "money":
			params[name] = float64(1)
		case "bool":
			params[name] = true
		case "date":
			params[name] = time.Now().UTC().Format(time.DateOnly)
		case "datetime":
			params[name] = time.Now().UTC().Format(time.RFC3339)
		}
	}
	return params, nil
}

func tryScratchRun(ctx context.Context, engine *runs.Engine, id string, now *time.Time) error {
	const maxTransitions = 128
	for range maxTransitions {
		if err := engine.Tick(ctx); err != nil {
			return err
		}
		state, err := engine.Get(id)
		if err != nil {
			return err
		}
		switch {
		case state.State == runs.Done:
			return nil
		case state.Approval != nil && state.Approval.Kind != "budget":
			// An approval only continues a fake call in this engine.
			if err := engine.Answer(id, state.Approval.ID, true); err != nil {
				return err
			}
		case state.WakeAt.After(*now):
			*now = state.WakeAt
		case slices.Contains([]string{runs.Incomplete, runs.YourCall}, state.State):
			return &check.RunProblem{Detail: fmt.Sprintf("Step %s stopped on example data: %s", state.Step, state.Error)}
		}
	}
	return &check.RunProblem{Detail: "The example automation ran out of steps. Check its loop and wait conditions."}
}
