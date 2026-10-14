package sample

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func bugTrackerApp() *spec.App {
	return &spec.App{ID: "bug-tracker", Records: map[string]*spec.RecordType{
		"bug": {Label: "Bug", Title: "title", Fields: map[string]*spec.Field{
			"title":    {Type: spec.FieldText},
			"severity": {Type: spec.FieldEnum, Values: []string{"S1", "S2", "S3"}},
			"priority": {Type: spec.FieldEnum, Values: []string{"S1", "S2", "S3"}},
			"status":   {Type: spec.FieldEnum, Values: []string{"Open", "Closed"}},
			"browser":  {Type: spec.FieldEnum, Values: []string{"Safari", "Firefox"}},
			"release":  {Type: spec.FieldRef, To: "release"},
			"related":  {Type: spec.FieldRefs, To: "bug"},
		}},
		"release": {Label: "Release", Title: "name", Fields: map[string]*spec.Field{
			"name": {Type: spec.FieldText, Default: json.RawMessage(`"sample_release_0001"`)},
			"bugs": {Type: spec.FieldRefs, To: "bug"},
		}},
	}}
}

func TestPreviewTitlesArePhrasesWithoutEnumEchoes(t *testing.T) {
	for _, tt := range []struct {
		name string
		app  *spec.App
	}{
		{"bugs", bugTrackerApp()},
		{"inventory", inventoryApp()},
		{"people", &spec.App{ID: "people", Records: map[string]*spec.RecordType{
			"contact": {Label: "Contact", Title: "name", Fields: map[string]*spec.Field{"name": {Type: spec.FieldText}}},
		}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := GenerateAt(context.Background(), tt.app, 25, 42, sampleNow())
			if err != nil {
				t.Fatal(err)
			}
			for typ, values := range rows {
				seen := map[string]bool{}
				for _, row := range values {
					title := row.Fields[tt.app.Records[typ].Title].(string)
					if !readableTitle(title) || len(strings.Fields(title)) < 2 || strings.ContainsFunc(title, unicode.IsDigit) || strings.Contains(title, " · ") {
						t.Errorf("title is not a readable phrase: %q", title)
					}
					if seen[title] {
						t.Errorf("duplicate %s title: %q", typ, title)
					}
					seen[title] = true
					for _, code := range []string{"S1", "S2", "S3", "Open", "Closed"} {
						if strings.Contains(title, code) {
							t.Errorf("title echoes classification %q: %q", code, title)
						}
					}
				}
			}
			assertReadableRefs(t, tt.app, rows)
		})
	}
}

func TestTitleVocabularyCoversMaximumPreviewWithoutCounters(t *testing.T) {
	for _, person := range []bool{false, true} {
		t.Run(map[bool]string{false: "phrases", true: "names"}[person], func(t *testing.T) {
			schema := &spec.RecordType{Label: "Record", Title: "title", Fields: map[string]*spec.Field{"title": {Type: spec.FieldText}}}
			if person {
				schema.Label = "Contact"
			}
			seen := map[string]bool{}
			for i := range 10000 {
				title := titleValue("record", schema, schema.Fields["title"], i, 0)
				if seen[title] || !readableTitle(title) {
					t.Fatalf("invalid title at %d: %q", i, title)
				}
				seen[title] = true
			}
		})
	}
}

func TestTitleDefaultsAndLongtextStayReadableAndUnique(t *testing.T) {
	for _, kind := range []spec.FieldType{spec.FieldText, spec.FieldLongtext} {
		for _, def := range []string{`"Gentle flow"`, `"sample_release_0001"`, `"release_0001"`} {
			t.Run(string(kind)+def, func(t *testing.T) {
				app := &spec.App{ID: "defaults", Records: map[string]*spec.RecordType{
					"item": {Label: "Item", Title: "title", Fields: map[string]*spec.Field{
						"title": {Type: kind, Default: json.RawMessage(def)},
					}},
				}}
				rows, err := GenerateAt(context.Background(), app, 12, 42, sampleNow())
				if err != nil {
					t.Fatal(err)
				}
				seen := map[string]bool{}
				for _, row := range rows["item"] {
					title := row.Fields["title"].(string)
					if !readableTitle(title) || seen[title] {
						t.Fatalf("invalid title: %q", title)
					}
					seen[title] = true
				}
				if def == `"Gentle flow"` && rows["item"][0].Fields["title"] != "Gentle flow" {
					t.Fatal("lost readable default")
				}
			})
		}
	}
}

type sampleDefinitionStore struct {
	svc.Store
	def *spec.Bundle
}

func (s sampleDefinitionStore) Definition(context.Context, string) (*spec.Bundle, error) {
	return s.def, nil
}

func TestSampleRefsResolveThroughRecordsGet(t *testing.T) {
	ctx := context.Background()
	app := bugTrackerApp()
	rows, err := GenerateAt(ctx, app, DefaultCount, 42, sampleNow())
	if err != nil {
		t.Fatal(err)
	}
	service := records.New(svc.Config{Root: t.TempDir()}, &svc.Services{Store: sampleDefinitionStore{def: &spec.Bundle{App: app}}})
	if err := service.Seed(ctx, app.ID, rows); err != nil {
		t.Fatal(err)
	}
	scope := svc.Scope{App: app.ID, Data: svc.DataSample, Caller: svc.Caller{Member: svc.Owner}}
	for typ, schema := range app.Records {
		page, err := service.Query(ctx, scope, bridge.QueryParams{Type: typ})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != DefaultCount {
			t.Fatalf("missing sample records: %+v", page)
		}
		for _, row := range page.Rows {
			for name, f := range schema.Fields {
				var ids []any
				switch f.Type {
				case spec.FieldRef:
					ids = []any{row.Fields[name]}
				case spec.FieldRefs:
					ids = row.Fields[name].([]any)
				}
				for _, id := range ids {
					target, err := service.Get(ctx, scope, bridge.GetRecordParams{Type: f.To, ID: id.(string)})
					if err != nil {
						t.Fatal(err)
					}
					title := target.Fields[app.Records[f.To].Title].(string)
					if !readableTitle(title) || title == target.ID {
						t.Fatalf("unreadable ref target: %+v", target)
					}
				}
			}
		}
	}
	// Generating a preview must leave the embedded fixture bytes untouched.
	_, before := spec.GoldenCRM()
	if _, err := GenerateAt(ctx, golden(t).App, 12, 42, sampleNow()); err != nil {
		t.Fatal(err)
	}
	_, after := spec.GoldenCRM()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("CRM fixture changed")
	}
}
