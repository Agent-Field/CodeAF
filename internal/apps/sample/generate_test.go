package sample

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func golden(t *testing.T) *spec.Bundle {
	t.Helper()
	files, _ := spec.GoldenCRM()
	def, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	return def
}
func TestDeterministicValidSamples(t *testing.T) {
	def := golden(t)
	ctx := context.Background()
	for _, count := range []int{0, 1, 12, 25} {
		a, err := Generate(ctx, def.App, count, 42)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Generate(ctx, def.App, count, 42)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatal("same seed changed sample")
		}
		ids := map[string]map[string]bool{}
		for typ, rows := range a {
			ids[typ] = map[string]bool{}
			for _, row := range rows {
				ids[typ][row.ID] = true
			}
		}
		for typ, rows := range a {
			if len(rows) != count {
				t.Fatal("wrong count")
			}
			for _, row := range rows {
				if err := records.ValidateFields(def.App.Records[typ], row.Fields); err != nil {
					t.Fatal(err)
				}
				for name, f := range def.App.Records[typ].Fields {
					switch f.Type {
					case spec.FieldRef:
						if !ids[f.To][row.Fields[name].(string)] {
							t.Fatal("invalid ref")
						}
					case spec.FieldRefs:
						for _, id := range row.Fields[name].([]any) {
							if !ids[f.To][id.(string)] {
								t.Fatal("invalid refs")
							}
						}
					}
				}
			}
		}
	}
	a, _ := Generate(ctx, def.App, 12, 42)
	b, _ := Generate(ctx, def.App, 12, 43)
	if reflect.DeepEqual(a, b) {
		t.Fatal("seed has no effect")
	}
	service := New(svc.Config{}, nil)
	rows, err := service.Generate(ctx, def)
	if err != nil || len(rows["deal"]) != DefaultCount {
		t.Fatalf("service: %v %v", rows, err)
	}
	if len(rows["deal"][0].AI) == 0 || rows["deal"][0].Fields["weighted"] == nil {
		t.Fatal("sample lost AI label or computed field")
	}
}
func TestAllFieldTypesAndCircularReferences(t *testing.T) {
	fields := map[string]*spec.Field{}
	for _, typ := range []spec.FieldType{spec.FieldText, spec.FieldLongtext, spec.FieldNumber, spec.FieldMoney, spec.FieldDate, spec.FieldDatetime, spec.FieldDuration, spec.FieldEnum, spec.FieldEnums, spec.FieldBool, spec.FieldPerson, spec.FieldPeople, spec.FieldEmail, spec.FieldPhone, spec.FieldURL, spec.FieldFile, spec.FieldFiles, spec.FieldPlace} {
		fields[string(typ)] = &spec.Field{Type: typ, Required: true, Values: []string{"Open", "Closed"}}
	}
	fields["name"] = &spec.Field{Type: spec.FieldText, Required: true, Unique: true}
	fields["self"] = &spec.Field{Type: spec.FieldRef, To: "item", Required: true}
	fields["others"] = &spec.Field{Type: spec.FieldRefs, To: "other", Required: true}
	app := &spec.App{ID: "demo", Records: map[string]*spec.RecordType{"item": {Title: "name", Fields: fields}, "other": {Title: "name", Fields: map[string]*spec.Field{"name": {Type: spec.FieldText}, "item": {Type: spec.FieldRef, To: "item", Required: true}}}}}
	rows, err := Generate(context.Background(), app, 20, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows["item"] {
		if err := records.ValidateFields(app.Records["item"], row.Fields); err != nil {
			t.Fatal(err)
		}
	}
	app.Records["item"].Fields["self"].To = "missing"
	if _, err := Generate(context.Background(), app, 1, 3); err == nil {
		t.Fatal("accepted missing target")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Generate(ctx, app, 1, 3); err == nil {
		t.Fatal("ignored cancellation")
	}
	for _, count := range []int{-1, 10001} {
		if _, err := Generate(context.Background(), app, count, 1); err == nil {
			t.Fatal("accepted invalid count")
		}
	}
}
