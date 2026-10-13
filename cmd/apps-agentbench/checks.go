package main

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func readSnapshot(ctx context.Context, all *svc.Services, scope svc.Scope, app *spec.App) (snapshot, error) {
	out := snapshot{}
	for typ := range app.Records {
		p := bridge.QueryParams{Type: typ, Limit: 100}
		for {
			page, err := all.Records.Query(ctx, scope, p)
			if err != nil {
				return nil, err
			}
			out[typ] = append(out[typ], page.Rows...)
			if page.Next == "" {
				break
			}
			p.After = page.Next
		}
		slices.SortFunc(out[typ], func(a, b bridge.Record) int {
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			return 0
		})
	}
	return out, nil
}

func checkData(t task, typ string, expected map[string]any, before, after snapshot, events []bridge.AgentEvent) (changed, ok bool) {
	changed = !reflect.DeepEqual(before, after)
	prior, next := before[typ], after[typ]
	switch t.Operation {
	case "read":
		return changed, !changed && readUsed(events)
	case "create":
		return changed, len(next) == len(prior)+1 && matches(next, expected) && !matches(prior, expected)
	case "update":
		return changed, len(next) == len(prior) && matches(next, expected) && !matches(prior, expected)
	case "delete":
		return changed, len(next) == len(prior)-1 && matches(prior, expected) && !matches(next, expected)
	}
	return changed, false
}

func readUsed(events []bridge.AgentEvent) bool {
	for _, e := range events {
		if e.Kind == bridge.AgentTool && (e.Tool == bridge.MethodRecordsQuery || e.Tool == bridge.MethodRecordsGet) {
			return true
		}
	}
	return false
}

func matches(rows []bridge.Record, expected map[string]any) bool {
	for _, row := range rows {
		if containsFields(row.Fields, expected) {
			return true
		}
	}
	return false
}

func containsFields(fields, expected map[string]any) bool {
	for name, value := range expected {
		actual, _ := json.Marshal(fields[name])
		wanted, _ := json.Marshal(value)
		if string(actual) != string(wanted) {
			return false
		}
	}
	return true
}
