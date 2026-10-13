package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// scriptedModel replaces only the paid network in tests. Each script still
// goes through the real agent, capabilities, registry and SQLite records.
type scriptedModel struct {
	replies []string
	calls   []svc.ModelCall
}

func (m *scriptedModel) Complete(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	m.calls = append(m.calls, call)
	if len(m.replies) == 0 {
		return svc.ModelReply{}, fmt.Errorf("script exhausted")
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return svc.ModelReply{Text: reply, Cost: 0.001}, nil
}

func crmFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files, _ := spec.GoldenCRM()
	for name, raw := range files {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRealAgentUpdatesPrivateStoreAndOverridesBothModels(t *testing.T) {
	dir := crmFiles(t)
	before, err := spec.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedModel{replies: []string{
		`{"tool":"records.update","args":{"type":"deal","id":"sample_deal_0001","set":{"stage":"Won"}}}`,
		`{"say":"Marked the Acme deal Won."}`,
	}}
	j := job{"test/other-model", "crm", dir, presets["crm"][3]}
	r := runWithModel(context.Background(), j, time.Second, model)
	if !r.OK || !r.DataOK || !r.DataChanged || r.Tools != 1 || r.Cost != .002 || r.Final != "Marked the Acme deal Won." {
		t.Fatalf("result = %+v", r)
	}
	for _, call := range model.calls {
		if call.Model != j.model || !strings.Contains(call.System, "You are the CRM agent") || !strings.Contains(call.System, "records.update") {
			t.Fatalf("wrong agent path: %+v", call)
		}
	}
	files, bundle, err := modelFiles(dir, j.model)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Agent.Model != j.model || bundle.App.Agent.Model != j.model || len(files) != len(before) {
		t.Fatalf("model override: %+v", bundle)
	}
	after, err := spec.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range before {
		if string(after[name]) != string(raw) {
			t.Fatalf("source bundle changed: %s", name)
		}
	}
	// A second task starts with the original stage, rather than the first
	// task's updated data or conversation.
	next := &scriptedModel{replies: []string{`{"tool":"records.query","args":{"type":"deal"}}`, `{"say":"Three deals."}`}}
	r = runWithModel(context.Background(), job{j.model, "crm", dir, presets["crm"][2]}, time.Second, next)
	if !r.OK || !r.DataOK || r.DataChanged || len(next.calls[0].Messages) != 1 {
		t.Fatalf("isolation: %+v", r)
	}
}

func TestErrorAndDataOutcomes(t *testing.T) {
	dir := crmFiles(t)
	tests := []struct {
		name      string
		task      task
		replies   []string
		ok, data  bool
		errorText string
	}{
		{"create", presets["crm"][0], []string{`{"tool":"records.create","args":{"type":"contact","set":{"name":"Ana Lima","email":"ana@acme.test","company":"sample_company_0001"}}}`, `{"say":"Added Ana."}`}, true, true, ""},
		{"refusal", presets["crm"][4], []string{`{"say":"I cannot delete contacts."}`}, true, false, ""},
		{"tool error", presets["crm"][3], []string{`{"tool":"records.update","args":{"type":"deal","id":"missing","set":{"stage":"Won"}}}`, `{"say":"That record was missing."}`}, false, false, "no deal missing"},
		{"empty", presets["crm"][2], []string{`{}`}, false, false, "no final reply text"},
		{"intermediate speech", presets["crm"][2], []string{`{"tool":"records.query","args":{"type":"deal"},"say":"Checking."}`, `{}`}, false, true, "no final reply text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &scriptedModel{replies: tt.replies}
			r := runWithModel(context.Background(), job{"test/model", "crm", dir, tt.task}, time.Second, model)
			if r.OK != tt.ok || r.DataOK != tt.data || !strings.Contains(r.Error, tt.errorText) {
				t.Fatalf("result = %+v", r)
			}
		})
	}
}

func TestJSONLPreservesReply(t *testing.T) {
	dir := t.TempDir()
	reply := "Line one.\nLine two with \"quotes\"."
	want := result{Model: "test/model", App: "crm", Task: "pipeline", Final: reply, OK: true, DataOK: true}
	if err := saveResult(dir, 0, want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "001.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("not one JSONL record: %q", raw)
	}
	var got result
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Final != reply || got.Model != want.Model {
		t.Fatalf("result = %+v", got)
	}
}

func TestChecksRequireTheRequestedChange(t *testing.T) {
	before := snapshot{"contact": {{ID: "1", Fields: map[string]any{"name": "Ana Lima", "email": "ana@acme.test"}}}}
	expected := map[string]any{"name": "Ana Lima"}
	changed, ok := checkData(presets["crm"][4], "contact", expected, before, snapshot{"contact": {}}, nil)
	if !changed || !ok {
		t.Fatal("delete not detected")
	}
	_, ok = checkData(presets["crm"][4], "contact", expected, before, before, nil)
	if ok {
		t.Fatal("unchanged deletion passed")
	}
	_, ok = checkData(presets["crm"][2], "contact", nil, before, before, nil)
	if ok {
		t.Fatal("read without a records tool passed")
	}
}

func TestJobsSelectPresetsAndRejectInvalidBounds(t *testing.T) {
	o := options{models: "one/a, two/b", apps: "/builder/model", preset: "all", parallel: 8, timeout: time.Minute}
	jobs, err := o.jobs()
	if err != nil || len(jobs) != 40 {
		t.Fatalf("jobs=%d error=%v", len(jobs), err)
	}
	for _, j := range jobs {
		if j.dir != filepath.Join(o.apps, j.preset) {
			t.Fatalf("layout: %+v", j)
		}
	}
	for _, preset := range presetNames() {
		if len(presets[preset]) != 5 {
			t.Fatalf("%s has %d tasks", preset, len(presets[preset]))
		}
	}
	o.preset = "crm"
	jobs, err = o.jobs()
	if err != nil || len(jobs) != 10 {
		t.Fatalf("crm jobs=%d error=%v", len(jobs), err)
	}
	o.parallel = 0
	if _, err := o.jobs(); err == nil {
		t.Fatal("zero workers accepted")
	}
}

// blockingModel makes cancellation deterministic: the test waits for the
// completion to start, then cancels instead of sleeping out a deadline.
type blockingModel struct {
	started chan struct{}
	once    sync.Once
}

func (m *blockingModel) Complete(ctx context.Context, _ svc.ModelCall) (svc.ModelReply, error) {
	m.once.Do(func() { close(m.started) })
	<-ctx.Done()
	return svc.ModelReply{}, ctx.Err()
}

func TestCancellationReportsErrorAndKeepsDataMeasurement(t *testing.T) {
	dir := crmFiles(t)
	model := &blockingModel{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan result, 1)
	go func() {
		done <- runWithModel(ctx, job{"test/model", "crm", dir, presets["crm"][2]}, time.Minute, model)
	}()
	select {
	case <-model.started:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("model never started")
	}
	select {
	case r := <-done:
		if r.OK || !strings.Contains(r.Error, "context canceled") || r.After == nil {
			t.Fatalf("result = %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not stop")
	}
}

// presetBundle makes portable schemas for all four fixture families. It
// deliberately uses the second field spelling to exercise Builder variation.
func presetBundle(t *testing.T, preset string) *spec.Bundle {
	t.Helper()
	files, _ := spec.GoldenCRM()
	b, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	b.App.ID, b.App.Name = preset, preset
	b.App.Records = map[string]*spec.RecordType{}
	b.App.Capabilities.Records = map[string]spec.Access{}
	b.App.Triggers, b.App.Commands = nil, nil
	add := func(typ, aliases string, value any) {
		names := strings.Split(aliases, "|")
		name := names[min(1, len(names)-1)]
		rt := b.App.Records[typ]
		f := rt.Fields[name]
		if f == nil {
			f = &spec.Field{Type: spec.FieldText}
			rt.Fields[name] = f
		}
		switch value := value.(type) {
		case float64:
			f.Type = spec.FieldNumber
		case string:
			if strings.HasPrefix(value, "@") {
				f.Type, f.To = spec.FieldRef, strings.TrimPrefix(value, "@")
			}
			if name == "status" || name == "stage" || name == "priority" {
				f.Type = spec.FieldEnum
				if !slices.Contains(f.Values, value) {
					f.Values = append(f.Values, value)
				}
			}
		}
	}
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	hints := seedHints(preset, now)
	for _, hint := range hints {
		b.App.Records[hint.typ] = &spec.RecordType{Title: "name", Fields: map[string]*spec.Field{"name": {Type: spec.FieldText, Required: true}}}
		b.App.Capabilities.Records[hint.typ] = spec.AccessReadWrite
		for key, values := range hint.fields {
			for _, value := range values {
				add(hint.typ, key, value)
			}
		}
	}
	for _, task := range presets[preset] {
		for key, value := range task.Set {
			add(task.Type, key, value)
		}
	}
	raw, err := json.Marshal(b.App)
	if err != nil {
		t.Fatal(err)
	}
	files[spec.AppFile] = raw
	b, err = spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEveryPresetSeedsRealRecordsForEveryTask(t *testing.T) {
	for _, preset := range presetNames() {
		t.Run(preset, func(t *testing.T) {
			b := presetBundle(t, preset)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			all := wire(ctx, t.TempDir(), &scriptedModel{})
			defer all.Runs.(*runs.Service).Close()
			for _, task := range presets[preset] {
				j := job{"test/model", preset, "", task}
				seed, err := makeSeed(ctx, b, j, time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC))
				if err != nil {
					t.Fatalf("%s: %v", task.ID, err)
				}
				typ, expected, err := expectedFields(b, task, seed)
				if err != nil {
					t.Fatalf("%s: %v", task.ID, err)
				}
				scope, err := install(ctx, all, b, b.Files, seed)
				if err != nil {
					t.Fatalf("%s: %v", task.ID, err)
				}
				before, err := readSnapshot(ctx, all, scope, b.App)
				if err != nil {
					t.Fatal(err)
				}
				if len(before[typ]) != 3 {
					t.Fatalf("%s seeded %d rows", typ, len(before[typ]))
				}
				if task.Operation == "delete" && !matches(before[typ], expected) {
					t.Fatal("delete target missing from seed")
				}
				if task.Operation == "update" && matches(before[typ], expected) {
					t.Fatalf("%s already satisfied before the task", task.ID)
				}
			}
		})
	}
}
