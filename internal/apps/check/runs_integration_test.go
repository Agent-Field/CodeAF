package check_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type runStore struct {
	svc.Store
	bundle *spec.Bundle
}

func (s runStore) Definition(context.Context, string) (*spec.Bundle, error) { return s.bundle, nil }
func (s runStore) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	return nil, nil
}

type triageModel struct{}

func (triageModel) Complete(context.Context, svc.ModelCall) (svc.ModelReply, error) {
	return svc.ModelReply{Text: `{"severity":"S1"}`}, nil
}

func TestCheckBugRunsAgainstRealSampleStore(t *testing.T) {
	files := spec.Files{
		"app.json": []byte(`{
"id":"bugs","name":"Bugs","agent":{"name":"Bug agent","file":"agent.md"},
"records":{"bug":{"title":"title","fields":{"title":{"type":"text","required":true},"severity":{"type":"enum","values":["S1","S2","S3"]},"status":{"type":"enum","default":"new","values":["new","triaged","in progress","fixed","won't fix"]}}}},
"capabilities":{"records":{"bug":"rw"},"ai":["sort"]},
"triggers":[{"id":"new-bug","on":"record.created","type":"bug","run":"triage","params":{"bug":"$record.id"}}],
"budget":{"month":20,"currency":"USD","per_run":1,"at_cap":"stop"}}`),
		"plan.json":         []byte(`{"ask":"Triage bugs; approve won't fix","name":"Bugs","data":[{"id":"bug","line":"Bugs"}],"automation":[{"id":"triage","line":"Triage new bugs"},{"id":"wont-fix","line":"Approve won't fix"}],"screens":[{"id":"bugs","line":"Bugs"}],"agent":{"line":"Bug agent"},"needs":"computed"}`),
		"agent.md":          []byte("---\nname: Bug agent\ntools: [records.query, records.get, records.create, records.update, records.delete, runs.start]\n---\nTriage bugs; ask approval before won't fix.\n"),
		"screens/bugs.json": []byte(`{"screen":"bugs","title":"Bugs","body":{"part":"RecordList","id":"bugs","label":"Bugs","bind":{"records":"bug","query":"status:\"in progress\""},"props":{"columns":["title","status"]}}}`),
		"runs/triage.yaml": []byte(`run: triage
budget: {per_run: 1}
params:
  bug: {type: text}
steps:
  - id: load
    records.query: {type: bug, query: 'id:{{params.bug}}', limit: 1}
  - id: suggest
    agent: Bug agent
    prompt: 'Suggest severity for {{steps.load.output.title}}'
    output: {type: object, required: [severity], properties: {severity: {type: string, enum: [S1, S2, S3]}}}
  - id: save
    records.update: {type: bug, id: $params.bug, set: {severity: $steps.suggest.output.severity}}
`),
		"runs/wont-fix.yaml": []byte(`run: wont-fix
budget: {per_run: 1}
params:
  bug: {type: ref, to: bug}
steps:
  - id: approve
    gate: {approval: start, who: $me, title: "Approval needed: won't fix"}
    records.update: {type: bug, id: $params.bug, set: {status: "won't fix"}}
`),
	}
	bundle, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := svc.Config{Root: t.TempDir(), Ctx: ctx}
	all := &svc.Services{Store: runStore{bundle: bundle}, Model: triageModel{}}
	all.Caps = caps.New(cfg, all)
	all.Sample = sample.New(cfg, all)
	all.Records = records.New(cfg, all)
	runner := runs.New(cfg, all)
	t.Cleanup(runner.Close)
	all.Runs = runner
	all.Approvals = runs.NewApprovals(cfg, all)
	all.Connectors = runs.NewConnectors(cfg, all)
	report, err := check.New(cfg, all).Run(ctx, "bugs", files, 1)
	if err != nil || !report.Pass || len(report.Runs) != 1 || report.Runs[0] != "triage" {
		t.Fatalf("Check report %+v, err %v", report, err)
	}
	scope := svc.Scope{App: "bugs", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataSample}
	states, err := runner.List(ctx, scope, bridge.ListRunsParams{})
	if err != nil || len(states.Runs) != 2 {
		t.Fatalf("runs %+v, err %v", states, err)
	}
	for _, state := range states.Runs {
		want := bridge.RunDone
		if state.Run == "wont-fix" {
			want = bridge.RunWaiting
		}
		if state.Status != want || state.Params["bug"] == nil || state.Params["bug"] == "sample" {
			t.Fatalf("run: %+v", state)
		}
	}
}
