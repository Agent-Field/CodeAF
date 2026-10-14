package check

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestEveryRunParamHasSampleValue(t *testing.T) {
	rows := map[string][]bridge.Record{"bug": {{ID: "bug-1"}, {ID: "bug-2"}}}
	for _, tc := range []struct {
		name  string
		param spec.RunParam
		want  any
	}{
		{"text", spec.RunParam{Type: "text"}, "sample"},
		{"number", spec.RunParam{Type: "number"}, float64(1)},
		{"bool", spec.RunParam{Type: "bool"}, true},
		{"date", spec.RunParam{Type: "date"}, "2026-10-13"},
		{"person", spec.RunParam{Type: "person"}, svc.Owner},
		{"enum", spec.RunParam{Type: "enum", Values: []string{"in progress", "fixed"}}, "in progress"},
		{"ref", spec.RunParam{Type: "ref", To: "bug"}, "bug-1"},
		{"refs", spec.RunParam{Type: "refs", To: "bug", Default: "status:new"}, []string{"bug-1", "bug-2"}},
		{"default", spec.RunParam{Type: "text", Default: "literal"}, "literal"},
		{"false default", spec.RunParam{Type: "bool", Default: false}, false},
		{"default reference", spec.RunParam{Type: "person", Default: "$me"}, svc.Owner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := &spec.Run{Params: map[string]*spec.RunParam{"value": &tc.param}}
			if got := runParams(run, rows)["value"]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sample param = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRecordTriggerSuppliesSampleContext(t *testing.T) {
	for _, on := range []spec.TriggerKind{spec.TriggerRecordCreated, spec.TriggerRecordChanged, spec.TriggerRecordDeleted} {
		t.Run(string(on), func(t *testing.T) {
			files, sample := fixture(t)
			b, err := spec.Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			b.Runs["follow-up"].Params["title"] = &spec.RunParam{Type: "text"}
			b.App.Triggers = []spec.Trigger{{ID: "event", On: on, Type: "deal", Run: "follow-up", Params: map[string]any{"title": "$record.name", "deals": []any{"$record.id"}}}}
			params, err := behaviourParams(b, "follow-up", sample.rows)
			if err != nil || params["title"] != sample.rows["deal"][0].Fields["name"] || !reflect.DeepEqual(params["deals"], []string{sample.rows["deal"][0].ID}) {
				t.Fatalf("params %v, err %v", params, err)
			}
		})
	}
}

func TestQueryRepairHintReachesBuilder(t *testing.T) {
	files, sample := fixture(t)
	change(t, files, "runs/follow-up.yaml", `query: "id:{{deal}}"`, `query: "stage:in progress"`)
	report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
	if err != nil || report.Pass {
		t.Fatalf("report %+v err %v", report, err)
	}
	for _, issue := range report.Issues {
		if issue.Rule == "schema/run-query" && strings.Contains(issue.Message, `expected field:value in "progress"`) && strings.Contains(issue.Message, `status:"in progress"`) && strings.Contains(issue.Message, "fix:") {
			return
		}
	}
	t.Fatalf("missing actionable query report: %+v", report)
}

type gatedRuns struct {
	svc.Runs
	started []bridge.StartRunParams
}

func (r *gatedRuns) Start(_ context.Context, _ svc.Scope, p bridge.StartRunParams) (bridge.RunState, error) {
	r.started = append(r.started, p)
	if p.Run == "approval" {
		return bridge.RunState{Status: bridge.RunWaiting, Fake: true}, nil
	}
	return bridge.RunState{Status: bridge.RunFailed, Error: "bad query", Fake: true}, nil
}

func TestPausedRunDoesNotHideAnotherRunFailure(t *testing.T) {
	files, sample := fixture(t)
	b, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	b.App.Triggers = nil
	b.Runs["approval"] = &spec.Run{Run: "approval", Params: map[string]*spec.RunParam{"bug": {Type: "text"}}}
	runner := &gatedRuns{}
	all := services(sample)
	all.Runs = runner
	r := Report{}
	status, err := New(svc.Config{}, all).behaviour(context.Background(), "crm", b, sample.rows, &r)
	if err != nil || status != "failed" || len(runner.started) != 2 || len(r.Issues) != 1 || runner.started[0].Params["bug"] != "sample" {
		t.Fatalf("status %s, starts %+v, report %+v, err %v", status, runner.started, r, err)
	}
}
