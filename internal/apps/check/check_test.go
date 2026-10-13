package check

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type samples struct {
	rows  map[string][]bridge.Record
	err   error
	calls int
}

func (s *samples) Generate(context.Context, *spec.Bundle) (map[string][]bridge.Record, error) {
	s.calls++
	return s.rows, s.err
}

type records struct {
	svc.Records
	seeded map[string][]bridge.Record
	scope  string
}

func (r *records) Seed(_ context.Context, app string, rows map[string][]bridge.Record) error {
	r.scope = app
	r.seeded = rows
	return nil
}

type runs struct {
	svc.Runs
	state  bridge.RunState
	err    error
	calls  int
	scope  svc.Scope
	params bridge.StartRunParams
}

func (r *runs) Start(_ context.Context, scope svc.Scope, p bridge.StartRunParams) (bridge.RunState, error) {
	r.calls++
	r.scope = scope
	r.params = p
	return r.state, r.err
}

type hub struct {
	svc.Hub
	topics []string
	events []bridge.BuildEvent
}

func (h *hub) Publish(topic string, value any) {
	h.topics = append(h.topics, topic)
	h.events = append(h.events, value.(bridge.BuildEvent))
}

func fixture(t testing.TB) (spec.Files, *samples) {
	t.Helper()
	files, data := spec.GoldenCRM()
	s := &samples{}
	if err := json.Unmarshal(data, &s.rows); err != nil {
		t.Fatal(err)
	}
	for typ, rows := range s.rows {
		for i := range rows {
			rows[i].Type = typ
		}
		s.rows[typ] = rows
	}
	return files, s
}
func services(s *samples) *svc.Services {
	return &svc.Services{Sample: s, Records: &records{}, Runs: &runs{err: bridge.NotBuilt("runs")}}
}
func change(t *testing.T, files spec.Files, file, old, replacement string) {
	t.Helper()
	if !strings.Contains(string(files[file]), old) {
		t.Fatalf("fixture does not contain %q", old)
	}
	files[file] = []byte(strings.Replace(string(files[file]), old, replacement, 1))
}

func TestGoldenCRM(t *testing.T) {
	files, sample := fixture(t)
	all := services(sample)
	h := &hub{}
	all.Hub = h
	service := New(svc.Config{}, all)
	report, err := service.check(context.Background(), "crm", files, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Issues) > 0 {
		t.Fatalf("report: %+v", report)
	}
	if !reflect.DeepEqual(report.Rendered, []string{"contacts", "deal", "pipeline"}) {
		t.Fatalf("rendered: %v", report.Rendered)
	}
	statuses := []string{}
	for _, step := range report.Steps {
		statuses = append(statuses, step.Status)
	}
	if !reflect.DeepEqual(statuses, []string{"done", "done", "done", "skipped"}) {
		t.Fatalf("steps: %v", report.Steps)
	}
	if len(report.Runs) != 0 {
		t.Fatalf("stub runs counted as executed: %v", report.Runs)
	}
	logs := []string{}
	for i, event := range h.events {
		if h.topics[i] != bridge.TopicBuild("crm") || event.Step != bridge.StepCheck || event.Status != bridge.BuildRunning || event.Round != 2 {
			t.Fatalf("event: %+v", event)
		}
		logs = append(logs, event.Log)
	}
	for _, want := range []string{"schema · running", "schema · done", "kit · done", "render · done", "behaviour · skipped"} {
		if !contains(logs, want) {
			t.Fatalf("missing %q in %v", want, logs)
		}
	}
}

func TestBrokenDefinitions(t *testing.T) {
	cases := []struct{ name, file, old, replacement, rule, path string }{
		{"part", "screens/deal.json", `"part": "RecordPage"`, `"part": "DataGrid"`, "kit/parts", "/body/children/0/part"},
		{"schema", "screens/deal.json", `"fields": [`, `"fields": false, "bad": [`, "kit/parts", "/body/children/0/props/fields"},
		{"schema type", "screens/deal.json", `"part": "Stack"`, `"part": 1`, "schema", "/body/part"},
		{"record type", "screens/deal.json", `"type": "deal"`, `"type": "missing"`, "kit/bind", "/body/children/0/bind/type"},
		{"field", "screens/deal.json", `"weighted"`, `"missing"`, "kit/bind", "/body/children/0/props/fields/8"},
		{"run target", "screens/deal.json", `"run": "follow-up"`, `"run": "missing"`, "kit/bind", "/body/children/5/bind/run"},
		{"binding ref", "screens/deal.json", `"record": "$params.id"`, `"record": "$params.missing"`, "render/bind", "/body/children/0/bind/record"},
		{"binding wrong type", "screens/deal.json", `"record": "$params.id"`, `"record": "$selection.deals.count"`, "render/bind", "/body/children/0/bind/record"},
		{"binding literal id", "screens/deal.json", `"record": "$params.id"`, `"record": "absent"`, "render/bind", "/body/children/0/bind/record"},
		{"bad CEL", "screens/contacts.json", `$selection.contacts.count == 1`, `$selection.contacts.count ==`, "render/when", "/body/children/1/when"},
		{"CEL wrong type", "screens/contacts.json", `$selection.contacts.count == 1`, `$selection.contacts.count`, "render/when", "/body/children/1/when"},
		{"CEL unknown list", "screens/contacts.json", `$selection.contacts.count == 1`, `$selection.missing.count == 1`, "render/when", "/body/children/1/when"},
		{"CEL unknown field", "screens/contacts.json", `$selection.contacts.count == 1`, `$record.missing == 1`, "render/when", "/body/children/1/when"},
		{"CEL runtime error", "screens/contacts.json", `$selection.contacts.count == 1`, `1 / 0 > 1`, "render/when", "/body/children/1/when"},
		{"template", "screens/deal.json", `"text": "Approvals"`, `"text": "{{record.missing}}"`, "render/prop", "/body/children/2/props/text"},
		{"action target", "screens/deal.json", `"runs.start": "follow-up"`, `"runs.start": "missing"`, "kit/bind", "/body/children/1/children/0/on/click/0/runs.start"},
		{"action field", "screens/contacts.json", `"phone": "$input.phone"`, `"missing": "$input.phone"`, "kit/bind", "/body/children/1/children/0/on/submit/0/records.update/set/missing"},
		{"action ref", "screens/deal.json", `"deals": ["$params.id"]`, `"deals": ["$params.missing"]`, "render/action", "/body/children/1/children/0/on/click/0/params"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, sample := fixture(t)
			change(t, files, tc.file, tc.old, tc.replacement)
			report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if err != nil {
				t.Fatal(err)
			}
			if report.Pass {
				t.Fatal("broken app passed")
			}
			found := false
			for _, issue := range report.Issues {
				if issue.File == tc.file && issue.Rule == tc.rule && issue.Path == tc.path {
					found = true
					if !strings.Contains(issue.Message, "fix:") {
						t.Fatalf("missing fix hint: %+v", issue)
					}
				}
			}
			if !found {
				t.Fatalf("want %s %s %s; got %+v", tc.file, tc.path, tc.rule, report.Issues)
			}
		})
	}
}

func TestSchemaStopsBeforeSample(t *testing.T) {
	files, sample := fixture(t)
	delete(files, "app.json")
	r, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
	if err != nil || r.Pass || sample.calls != 0 {
		t.Fatalf("report %+v err %v sample calls %d", r, err, sample.calls)
	}
}

func TestSampleRequired(t *testing.T) {
	for _, err := range []error{bridge.NotBuilt("sample"), errors.New("sample failed")} {
		t.Run(err.Error(), func(t *testing.T) {
			files, sample := fixture(t)
			sample.err = err
			r, got := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if !errors.Is(got, err) || r.Pass {
				t.Fatalf("report %+v err %v", r, got)
			}
		})
	}
}

func TestBehaviour(t *testing.T) {
	cases := []struct {
		name   string
		state  bridge.RunState
		err    error
		status string
		pass   bool
		count  int
	}{
		{"stub", bridge.RunState{}, bridge.NotBuilt("runs"), "skipped", true, 0},
		{"fakes", bridge.RunState{Status: bridge.RunDone, Fake: true}, nil, "done", true, 1},
		{"real connectors", bridge.RunState{Status: bridge.RunDone}, nil, "failed", false, 0},
		{"run error", bridge.RunState{}, errors.New("bad step"), "failed", false, 0},
		{"failed state", bridge.RunState{Status: bridge.RunFailed, Error: "bad step"}, nil, "failed", false, 0},
		{"asynchronous", bridge.RunState{Status: bridge.RunRunning, Fake: true}, nil, "skipped", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, sample := fixture(t)
			all := services(sample)
			runner := &runs{state: tc.state, err: tc.err}
			all.Runs = runner
			report, err := New(svc.Config{}, all).check(context.Background(), "crm", files, 1)
			if err != nil {
				t.Fatal(err)
			}
			if report.Passed != tc.pass || report.Steps[3].Status != tc.status || len(report.Runs) != tc.count {
				t.Fatalf("report: %+v", report)
			}
			if runner.calls != 1 || runner.scope.Data != svc.DataSample || runner.scope.Caller.Member != svc.Owner {
				t.Fatalf("runner: %+v", runner)
			}
			if _, ok := runner.params.Params["deals"].([]string); !ok {
				t.Fatalf("sample params: %+v", runner.params)
			}
		})
	}
}

func TestCheckDirectory(t *testing.T) {
	files, sample := fixture(t)
	dir := t.TempDir()
	for name, data := range files {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := New(svc.Config{}, services(sample)).Check(context.Background(), dir)
	if err != nil || !report.Passed {
		t.Fatalf("report %+v err %v", report, err)
	}
}

func TestCanceledCheck(t *testing.T) {
	files, sample := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := New(svc.Config{}, services(sample)).Run(ctx, "crm", files, 1)
	if !errors.Is(err, context.Canceled) || report.Pass || sample.calls != 0 {
		t.Fatalf("report %+v err %v", report, err)
	}
}

func BenchmarkGoldenCRM(b *testing.B) {
	files, sample := fixture(b)
	service := New(svc.Config{}, services(sample))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := service.Run(context.Background(), "crm", files, 1)
		if err != nil || !r.Pass {
			b.Fatalf("report %+v err %v", r, err)
		}
	}
}

func TestEverySampleRowIsChecked(t *testing.T) {
	files, sample := fixture(t)
	change(t, files, "screens/deal.json", `"part": "RecordPage"`, `"part": "RecordPage", "when": "$record.amount > 0"`)
	sample.rows["deal"][len(sample.rows["deal"])-1].Fields["amount"] = "bad"
	report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.Pass {
		t.Fatal("last row CEL error passed")
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Path == "/body/children/0/when" && issue.Rule == "render/when" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues: %+v", report.Issues)
	}
}

func TestHiddenCELStillChecked(t *testing.T) {
	files, sample := fixture(t)
	files["screens/contacts.json"] = []byte(`{"screen":"contacts","title":"Contacts","body":{"part":"Stack","when":"false","children":[{"part":"Text","when":"1 / 0 > 0","props":{"text":"Hidden"}}]}}`)
	report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
	if err != nil || report.Pass {
		t.Fatalf("report %+v err %v", report, err)
	}
}

func TestSemanticSchema(t *testing.T) {
	cases := []struct {
		name             string
		edit             func(*testing.T, spec.Files)
		file, path, rule string
	}{
		{"missing planned screen", func(_ *testing.T, f spec.Files) { delete(f, "screens/contacts.json") }, "plan.json", "/screens/2/id", "schema/plan"},
		{"unsupported file", func(_ *testing.T, f spec.Files) { f["custom/editor.tsx"] = []byte("anything") }, "custom/editor.tsx", "", "schema/file"},
		{"multiple run actions", func(t *testing.T, f spec.Files) {
			change(t, f, "runs/follow-up.yaml", "records.query: { type: deal", "wait: { timeout: 1s }\n        records.query: { type: deal")
		}, "runs/follow-up.yaml", "/steps/0/steps/0", "schema/run-action"},
		{"bad title field", func(t *testing.T, f spec.Files) { change(t, f, "app.json", `"title": "name"`, `"title": "missing"`) }, "app.json", "/records/company/title", "schema/record"},
		{"bad relation", func(t *testing.T, f spec.Files) { change(t, f, "app.json", `"to": "company"`, `"to": "missing"`) }, "app.json", "/records/contact/fields/company/to", "schema/record"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, sample := fixture(t)
			tc.edit(t, files)
			report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if err != nil {
				t.Fatal(err)
			}
			if report.Pass || sample.calls != 0 {
				t.Fatalf("report %+v calls %d", report, sample.calls)
			}
			found := false
			for _, issue := range report.Issues {
				if issue.File == tc.file && issue.Path == tc.path && issue.Rule == tc.rule {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s %s %s; got %+v", tc.file, tc.path, tc.rule, report.Issues)
			}
		})
	}
}

func TestBoundFieldTypes(t *testing.T) {
	files, sample := fixture(t)
	sample.rows["deal"][0].Fields["amount"] = "not money"
	report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.Pass {
		t.Fatal("wrong sample field type passed")
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Rule == "render/prop" && strings.Contains(issue.Message, "deal.amount") {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues: %+v", report.Issues)
	}
}

func TestRecordFieldsDoNotReplaceScopeRoots(t *testing.T) {
	files, sample := fixture(t)
	change(t, files, "app.json", `"weighted": {`, `"selection": {"type":"text"}, "params": {"type":"text"}, "weighted": {`)
	for _, row := range sample.rows["deal"] {
		row.Fields["selection"] = "field value"
		row.Fields["params"] = "field value"
	}
	report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
	if err != nil || !report.Pass {
		t.Fatalf("report %+v err %v", report, err)
	}
}
