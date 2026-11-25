package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
)

type ProbeResult struct {
	Name    string `json:"name"`
	Pass    bool   `json:"pass"`
	Problem string `json:"problem,omitempty"`
}

// Stimulus sends platform-controlled events and writes under the appropriate
// principal. The adapter must return observed engine state after the stimulus.
type Stimulus interface {
	Exercise(context.Context, string, string, Record) (map[string]any, error)
}

func equal(path string, value any) Check { return Check{Path: path, Equals: value} }

var stimulusChecks = map[string][]Check{
	"duplicate-trigger":     {equal("/runs", 1), equal("/effects", 1)},
	"record-instructions":   {equal("/canary_writes", 0), equal("/effects", 0), equal("/processed", 1)},
	"email-instructions":    {equal("/canary_writes", 0), equal("/effects", 0), equal("/processed", 1)},
	"rename-with-live-data": {equal("/same_field_id", true), equal("/same_record_id", true), equal("/value_preserved", true)},
	"guarded-undo":          {equal("/undo_refused", true), equal("/state_preserved", true)},
	"person-wins":           {equal("/held", 1), equal("/person_value_preserved", true)},
}

var dataProbes = []string{"empty-results", "large-results", "stale-edit"}

func skippedProbes(reason string) []ProbeResult {
	var result []ProbeResult
	for _, name := range append(slices.Clone(dataProbes), slices.Sorted(maps.Keys(stimulusChecks))...) {
		result = append(result, ProbeResult{Name: name, Problem: reason})
	}
	return result
}

func adversarial(ctx context.Context, app Adapter, record Record, id string) []ProbeResult {
	probes := skippedProbes("this engine has no platform stimulus adapter")
	for i := range probes {
		probe := &probes[i]
		var err error
		switch probe.Name {
		case "stale-edit":
			err = staleEdit(ctx, app, id, record)
		case "empty-results":
			err = runSteps(ctx, app, []Step{{
				Method: "data.query",
				Params: map[string]any{"app": id, "type": record.Type, "query": "fluid-eval-missing-98177"},
				Expect: []Check{equal("/total", 0)},
			}}, nil)
		case "large-results":
			err = largeResults(ctx, app, id, record)
		default:
			stimulus, ok := app.(Stimulus)
			if !ok {
				continue
			}
			var observed map[string]any
			observed, err = stimulus.Exercise(ctx, probe.Name, id, record)
			if err == nil {
				for _, check := range stimulusChecks[probe.Name] {
					if _, ok := observed[check.Path[1:]]; !ok || !reflect.DeepEqual(normalize(observed[check.Path[1:]]), normalize(check.Equals)) {
						err = errors.Join(err, fmt.Errorf("%s should equal %v", check.Path, check.Equals))
					}
				}
			}
		}
		probe.Problem = ""
		if err != nil {
			probe.Problem = err.Error()
		}
		probe.Pass = err == nil
	}
	return probes
}

func staleEdit(ctx context.Context, app Adapter, id string, record Record) error {
	steps := []Step{
		{Method: "data.create", Params: map[string]any{"app": id, "type": record.Type, "set": record.Value},
			Capture: map[string]string{"id": "/rows/0/id", "rev": "/rows/0/_rev"}},
		{Method: "data.update", Params: map[string]any{"app": id, "type": record.Type, "ids": []any{"$id"}, "rev": "$rev", "set": record.Edit}},
		{Method: "data.update", Params: map[string]any{"app": id, "type": record.Type, "ids": []any{"$id"}, "rev": "$rev", "set": record.Value},
			Error: "stale"},
		{Method: "data.get", Params: map[string]any{"app": id, "type": record.Type, "id": "$id"}},
	}
	for _, field := range slices.Sorted(maps.Keys(record.Edit)) {
		steps[3].Expect = append(steps[3].Expect, equal("/row/"+field, record.Edit[field]))
	}
	return runSteps(ctx, app, steps, map[string]any{})
}

func largeResults(ctx context.Context, app Adapter, id string, record Record) error {
	const batchSize, batches, pageSize = 100, 20, 100
	rows := make([]any, batchSize)
	for i := range rows {
		rows[i] = record.Value
	}
	for range batches {
		if _, err := app.Call(ctx, "data.create", map[string]any{"app": id, "type": record.Type, "rows": rows}); err != nil {
			return fmt.Errorf("seed rows: %w", err)
		}
	}
	type page struct {
		Rows  []struct{ ID string } `json:"rows"`
		Next  string                `json:"next"`
		Total int                   `json:"total"`
	}
	query := map[string]any{"app": id, "type": record.Type, "limit": pageSize}
	seen := map[string]bool{}
	cursors := map[string]bool{}
	for {
		body, err := app.Call(ctx, "data.query", query)
		if err != nil {
			return err
		}
		var result page
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		if result.Total < batchSize*batches || len(result.Rows) == 0 || len(result.Rows) > pageSize {
			return fmt.Errorf("unexpected result size: %d records, %d in page", result.Total, len(result.Rows))
		}
		for _, row := range result.Rows {
			if row.ID == "" || seen[row.ID] {
				return errors.New("page returned a missing or repeated row ID")
			}
			seen[row.ID] = true
		}
		if result.Next == "" {
			if len(seen) != result.Total {
				return fmt.Errorf("pages returned %d of %d rows", len(seen), result.Total)
			}
			return nil
		}
		if cursors[result.Next] {
			return errors.New("query returned a repeated cursor")
		}
		cursors[result.Next] = true
		query["cursor"] = result.Next
	}
}
