package spec_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestTintSchemaPalette(t *testing.T) {
	want := []string{"iris", "sage", "sand", "tide", "rose", "graphite"}
	if !reflect.DeepEqual(spec.Tints, want) {
		t.Fatalf("palette = %v, want %v", spec.Tints, want)
	}
	for _, url := range []string{spec.AppSchemaURL, spec.PlanSchemaURL} {
		t.Run(url, func(t *testing.T) {
			data, err := spec.SchemaJSON(url)
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]struct {
					Type string   `json:"type"`
					Enum []string `json:"enum"`
				} `json:"properties"`
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(data, &schema); err != nil {
				t.Fatal(err)
			}
			tint := schema.Properties["tint"]
			if tint.Type != "string" || !reflect.DeepEqual(tint.Enum, spec.Tints) {
				t.Fatalf("tint schema = %+v, want string enum %v", tint, spec.Tints)
			}
			for _, name := range schema.Required {
				if name == "tint" {
					t.Fatal("tint must be optional")
				}
			}
		})
	}
}

func tintFiles(t *testing.T, file string, value any, omit bool) spec.Files {
	t.Helper()
	files, _ := spec.GoldenCRM()
	var definition map[string]any
	if err := json.Unmarshal(files[file], &definition); err != nil {
		t.Fatal(err)
	}
	if omit {
		delete(definition, "tint")
	} else {
		definition["tint"] = value
	}
	data, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	files[file] = data
	return files
}

func TestTintValidation(t *testing.T) {
	type tintCase struct {
		name  string
		value any
		omit  bool
		valid bool
	}
	cases := []tintCase{
		{name: "omitted", omit: true, valid: true},
		{name: "unknown", value: "violet"},
		{name: "empty", value: ""},
		{name: "case", value: "Sage"},
		{name: "null", value: nil},
		{name: "number", value: 1},
	}
	for _, tint := range spec.Tints {
		cases = append(cases, tintCase{name: tint, value: tint, valid: true})
	}
	for _, file := range []string{spec.AppFile, spec.PlanFile} {
		for _, tc := range cases {
			t.Run(file+"/"+tc.name, func(t *testing.T) {
				files := tintFiles(t, file, tc.value, tc.omit)
				issues := spec.Validate(files)
				if tc.valid {
					if len(issues) != 0 {
						t.Fatalf("valid tint: %v", issues)
					}
					bundle, err := spec.Decode(files)
					if err != nil {
						t.Fatal(err)
					}
					got := bundle.App.Tint
					if file == spec.PlanFile {
						got = bundle.Plan.Tint
					}
					want, _ := tc.value.(string)
					if got != want {
						t.Fatalf("decoded tint = %q, want %q", got, want)
					}
					return
				}
				if len(issues) == 0 {
					t.Fatalf("invalid tint issues = %v", issues)
				}
				for _, issue := range issues {
					if issue.File != file || issue.Path != "/tint" || issue.Rule != "schema" {
						t.Fatalf("unexpected tint issue = %v", issue)
					}
				}
			})
		}
	}
}

func TestCheckRejectsBadTint(t *testing.T) {
	for _, file := range []string{spec.AppFile, spec.PlanFile} {
		t.Run(file, func(t *testing.T) {
			files := tintFiles(t, file, "violet", false)
			report, err := check.New(svc.Config{}, &svc.Services{}).Run(context.Background(), "crm", files, 1)
			if err != nil {
				t.Fatal(err)
			}
			if report.Pass || len(report.Issues) != 1 || report.Issues[0].File != file || report.Issues[0].Path != "/tint" || report.Issues[0].Rule != "schema" {
				t.Fatalf("bad tint Check = %+v", report)
			}
		})
	}
}

func TestGoldenCRMTint(t *testing.T) {
	files, _ := spec.GoldenCRM()
	bundle, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.App.Tint != "sage" {
		t.Fatalf("golden CRM tint = %q, want sage", bundle.App.Tint)
	}
}

func TestGoldenCRMSeedKeepsTint(t *testing.T) {
	ctx := context.Background()
	cfg := svc.Config{Root: t.TempDir()}
	for _, s := range []*store.Service{store.New(cfg, &svc.Services{}), store.New(cfg, &svc.Services{})} {
		if _, err := s.List(ctx, svc.Caller{Member: svc.Owner}); err != nil {
			t.Fatal(err)
		}
		bundle, err := s.Definition(ctx, "crm")
		if err != nil {
			t.Fatal(err)
		}
		if bundle.App.Tint != "sage" {
			t.Fatalf("seeded CRM tint = %q, want sage", bundle.App.Tint)
		}
	}
}
