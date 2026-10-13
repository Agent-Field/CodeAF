package prebuilt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestListReturnsFactory(t *testing.T) {
	want := bridge.PrebuiltApp{ID: ID, Name: "Factory", Kind: Kind, Version: 3, Space: "acme"}
	service := New(svc.Config{}, nil)
	for _, caller := range []svc.Caller{
		{},
		{Member: svc.Owner, Role: svc.RoleEditor},
		{Member: "mona", Role: svc.RoleViewer},
		{Member: "raj", Role: svc.RoleUser, Agent: true},
	} {
		t.Run(caller.Member+"/"+string(caller.Role), func(t *testing.T) {
			got, err := service.List(context.Background(), caller)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Apps) != 1 || got.Apps[0] != want {
				t.Fatalf("apps=%+v", got.Apps)
			}
		})
	}
}

func TestIsPrebuilt(t *testing.T) {
	for _, tt := range []struct {
		id   string
		want bool
	}{
		{ID, true},
		{"factory", true},
		{"Factory", false},
		{"crm", false},
		{"", false},
	} {
		t.Run(tt.id, func(t *testing.T) {
			if got := IsPrebuilt(tt.id); got != tt.want {
				t.Fatalf("IsPrebuilt(%q)=%v", tt.id, got)
			}
		})
	}
}

func TestLifecycleDeniesFactory(t *testing.T) {
	for _, tt := range []struct {
		verb string
		id   string
		deny bool
	}{
		{"archive", ID, true},
		{"delete", ID, true},
		{"copy", ID, true},
		{"shape", ID, true},
		{"archive", "crm", false},
		{"delete", "crm", false},
		{"copy", "crm", false},
		{"shape", "crm", false},
		{"archive", "", false},
	} {
		t.Run(tt.verb+" "+tt.id, func(t *testing.T) {
			if IsPrebuilt(tt.id) != tt.deny {
				t.Fatalf("IsPrebuilt(%q)=%v want %v", tt.id, IsPrebuilt(tt.id), tt.deny)
			}
			if !tt.deny {
				return
			}
			err := Denied()
			if err == nil || err.Code != bridge.CodeDenied || err.Message != "prebuilt" {
				t.Fatalf("denial=%v", err)
			}
		})
	}
}

func TestOpenFilesAreTheDefinition(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"app.json",
		"plan.json",
		"agent.md",
		"screens/jobs.json",
		"screens/job.json",
		"screens/repos.json",
		"screens/pipelines.json",
		"screens/runners.json",
		"screens/people.json",
		"screens/activity.json",
	} {
		if len(files[name]) == 0 {
			t.Errorf("missing %s", name)
		}
	}
	files["app.json"][0] = 'X'
	again, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	if again["app.json"][0] == 'X' {
		t.Fatal("Files returned the embedded bytes")
	}
}

func TestDefinitionPassesSpecAndKitLaw(t *testing.T) {
	dir := writeDefinition(t)
	bundle, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.App.ID != ID || bundle.App.Name != "Factory" || bundle.App.Version != 3 || bundle.App.Space != "acme" {
		t.Fatalf("app=%+v", bundle.App)
	}
	if bundle.Agent.Name != bundle.App.Agent.Name {
		t.Fatalf("agent name %q != %q", bundle.Agent.Name, bundle.App.Agent.Name)
	}
	report, err := check.New(svc.Config{}, &svc.Services{Sample: sampleGen{}}).Check(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Issues) > 0 {
		t.Fatalf("check=%+v", report.Issues)
	}
	kit := ""
	for _, step := range report.Steps {
		if step.Name == "kit" {
			kit = step.Status
		}
	}
	if kit != "done" {
		t.Fatalf("kit=%q steps=%+v", kit, report.Steps)
	}
}

func TestFactoryDoesNotLetAIDecide(t *testing.T) {
	dir := writeDefinition(t)
	bundle, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.App.Capabilities.AI) != 0 || len(bundle.App.Triggers) != 0 || len(bundle.Runs) != 0 {
		t.Fatalf("ai=%v triggers=%v runs=%v", bundle.App.Capabilities.AI, bundle.App.Triggers, bundle.RunIDs())
	}
	for _, tool := range bundle.Agent.Tools {
		switch tool {
		case "records.query", "records.get":
		default:
			t.Fatalf("tool %q can change a job", tool)
		}
	}
	for typ, record := range bundle.App.Records {
		for name, field := range record.Fields {
			if field.AI != "" {
				t.Fatalf("%s.%s is an AI field", typ, name)
			}
		}
	}
	body := bundle.Agent.Body + bundle.App.Description + bundle.Plan.Agent.Line
	for _, needle := range []string{
		"AI never starts, approves, assigns or ranks a job",
		"Settings → Factory",
		"jobs list",
		"job page",
		"repos",
		"pipelines",
		"runners",
	} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(needle)) {
			t.Errorf("definition does not say %q", needle)
		}
	}
}

func writeDefinition(t *testing.T) string {
	t.Helper()
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type sampleGen struct{}

func (sampleGen) Generate(_ context.Context, b *spec.Bundle) (map[string][]bridge.Record, error) {
	rows := map[string][]bridge.Record{}
	for typ, record := range b.App.Records {
		fields := map[string]any{}
		for name, field := range record.Fields {
			fields[name] = sampleValue(field)
		}
		rows[typ] = []bridge.Record{{ID: typ + "-1", Type: typ, Version: 1, Fields: fields}}
	}
	return rows, nil
}

func sampleValue(field *spec.Field) any {
	switch field.Type {
	case spec.FieldNumber, spec.FieldMoney:
		return float64(1)
	case spec.FieldBool:
		return true
	case spec.FieldEnums, spec.FieldRefs, spec.FieldPeople, spec.FieldFiles:
		return []string{"sample"}
	default:
		if len(field.Values) > 0 {
			return field.Values[0]
		}
		return "sample"
	}
}
