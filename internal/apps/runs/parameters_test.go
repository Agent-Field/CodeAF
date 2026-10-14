package runs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestRunParametersResolveWithoutLosingTypes(t *testing.T) {
	env := map[string]any{"params": map[string]any{"bug": "bug-1", "enabled": false, "count": float64(3)}, "steps": map[string]any{"triage": map[string]any{"output": map[string]any{"severity": "S1"}}}}
	for _, tc := range []struct {
		name string
		in   any
		want any
		err  bool
	}{
		{"query", "id:{{params.bug}}", "id:bug-1", false},
		{"typed id", "$params.bug", "bug-1", false},
		{"typed false", "$params.enabled", false, false},
		{"typed number", "$params.count", float64(3), false},
		{"step result", "$steps.triage.output.severity", "S1", false},
		{"nested", map[string]any{"id": "$params.bug"}, map[string]any{"id": "bug-1"}, false},
		{"missing template namespace", "id:{{bug}}", nil, true},
		{"missing id", "$params.missing", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRunValue(tc.in, env)
			if (err != nil) != tc.err || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("value %#v, err %v", got, err)
			}
			if tc.err && !strings.Contains(err.Error(), "{{params.<name>}}") {
				t.Fatalf("missing repair hint: %v", err)
			}
		})
	}
}

func TestQueryFilterPreservesQuotedValues(t *testing.T) {
	for _, tc := range []struct {
		value, query string
		want         bool
	}{
		{"in progress", `status:"in progress"`, true},
		{"won't fix", `status:"won't fix"`, true},
		{"in progress", `status:"in progress",fixed`, true},
		{"fixed", `status:-"in progress"`, true},
		{"in progress", `status:-"in progress"`, false},
		{"a,b", `status:"a,b"`, true},
		{"a", `status:"a,b"`, false},
	} {
		t.Run(tc.query+tc.value, func(t *testing.T) {
			row := bridge.Record{Fields: map[string]any{"status": tc.value}}
			if got := matchQuery(row, tc.query, time.Time{}); got != tc.want {
				t.Fatalf("match = %v", got)
			}
		})
	}
}

type failingQueryRecords struct{ svc.Records }

func (failingQueryRecords) Query(context.Context, svc.Scope, bridge.QueryParams) (bridge.QueryResult, error) {
	return bridge.QueryResult{}, errors.New(`expected field:value in "progress"`)
}

func TestRunQueryErrorShowsResolvedQueryAndFix(t *testing.T) {
	e := &engine{all: &svc.Services{Records: failingQueryRecords{}}}
	_, err := e.execQuery(context.Background(), &job{Env: map[string]any{}}, &recBody{Type: "bug", Query: "status:in progress"})
	if err == nil || !strings.Contains(err.Error(), `query "status:in progress"`) || !strings.Contains(err.Error(), `status:"in progress"`) {
		t.Fatalf("query error: %v", err)
	}
}

func TestRecordTriggerResolvesTheSameRecordFieldsAsCheck(t *testing.T) {
	for _, ref := range []string{"$record.id", "$record.name", "$record.fields.name"} {
		t.Run(ref, func(t *testing.T) {
			h := newHarness(t)
			h.bundle.Runs["context"] = &spec.Run{Run: "context", Params: map[string]*spec.RunParam{"value": {Type: "text"}}, Steps: []*spec.Step{{ID: "pause", Wait: &spec.Wait{For: "1h"}}}}
			row := &bridge.Record{ID: "dl_hooli", Type: "deal", Fields: map[string]any{"name": "Hooli"}}
			h.eng.fire(h.ctx, "crm", spec.Trigger{ID: "event", Run: "context", Params: map[string]any{"value": ref}}, bridge.RecordDiff{ID: row.ID, Type: row.Type, Record: row})
			list, err := h.runs.List(h.ctx, h.scope(), bridge.ListRunsParams{Run: "context"})
			want := "Hooli"
			if ref == "$record.id" {
				want = row.ID
			}
			if err != nil || len(list.Runs) != 1 || list.Runs[0].Params["value"] != want {
				t.Fatalf("runs %+v, err %v", list, err)
			}
		})
	}
}
