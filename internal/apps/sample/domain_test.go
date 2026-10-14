package sample

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func inventoryApp() *spec.App {
	return &spec.App{ID: "inventory", Records: map[string]*spec.RecordType{
		"item": {Label: "Stock item", Title: "summary", Fields: map[string]*spec.Field{
			"summary":       {Type: spec.FieldText, Required: true, Unique: true},
			"category":      {Type: spec.FieldEnum, Values: []string{"Ceramic", "Glass", "Wood"}},
			"sku":           {Type: spec.FieldText, Unique: true},
			"supplier":      {Type: spec.FieldRef, To: "supplier", Default: json.RawMessage(`"old_supplier"`)},
			"alternatives":  {Type: spec.FieldRefs, To: "supplier", Default: json.RawMessage(`["old_supplier"]`)},
			"received_on":   {Type: spec.FieldDate},
			"reorder_due":   {Type: spec.FieldDate, Unique: true},
			"counted_at":    {Type: spec.FieldDatetime, Unique: true},
			"next_delivery": {Type: spec.FieldDatetime, Default: json.RawMessage(`"2020-01-01T09:00:00Z"`)},
			"notes":         {Type: spec.FieldLongtext, Label: "Handling notes"},
		}},
		"supplier": {Label: "Supplier", Title: "name", Fields: map[string]*spec.Field{
			"name":         {Type: spec.FieldText, Required: true},
			"contact_name": {Type: spec.FieldText, Label: "Contact name"},
			"email":        {Type: spec.FieldEmail},
			"website":      {Type: spec.FieldURL},
		}},
	}}
}

func TestInventorySamplesUseTheirOwnVocabularyAndReadableRelations(t *testing.T) {
	app := inventoryApp()
	for _, count := range []int{1, 12, 25} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			rows, err := GenerateAt(context.Background(), app, count, 42, sampleNow())
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(rows)
			for _, word := range []string{"Acme", "Globex", "Northwind", "Initech", "Hooli", "Umbrella", "Vandelay", "Stark Labs", "Pied Piper", "Cedar Works", "Atlas Studio", "Juniper Systems", "pilot", "team plan", "renewal", "expansion", "proposal", "/accounts/"} {
				if strings.Contains(strings.ToLower(string(data)), strings.ToLower(word)) {
					t.Errorf("inventory sample contains CRM vocabulary %q", word)
				}
			}
			for i, row := range rows["item"] {
				title := row.Fields["summary"].(string)
				if !strings.Contains(strings.ToLower(title), "stock item") || strings.ContainsFunc(title, unicode.IsDigit) {
					t.Errorf("title lacks type label or uses a counter: %q", title)
				}
				if row.Fields["sku"] != fmt.Sprintf("ITEM-%04d", i+1) {
					t.Errorf("SKU ignores field semantics: %v", row.Fields["sku"])
				}
			}
			assertReadableRefs(t, app, rows)
		})
	}
}

func assertReadableRefs(t *testing.T, app *spec.App, rows map[string][]bridge.Record) {
	t.Helper()
	byID := map[string]bridge.Record{}
	for _, records := range rows {
		for _, row := range records {
			byID[row.ID] = row
		}
	}
	for typ, records := range rows {
		for _, row := range records {
			for name, f := range app.Records[typ].Fields {
				var ids []any
				switch f.Type {
				case spec.FieldRef:
					ids = []any{row.Fields[name]}
				case spec.FieldRefs:
					ids = row.Fields[name].([]any)
				}
				for _, id := range ids {
					target, ok := byID[id.(string)]
					if !ok || target.Type != f.To {
						t.Fatalf("%s.%s has missing target %v", typ, name, id)
					}
					title, ok := target.Fields[app.Records[f.To].Title].(string)
					if !ok || strings.TrimSpace(title) == "" || strings.Contains(title, "sample_") {
						t.Errorf("ref target lacks readable title: %+v", target)
					}
				}
			}
		}
	}
}

func TestSampleDatesFollowInjectedClock(t *testing.T) {
	for _, now := range []time.Time{sampleNow(), time.Date(2030, 1, 1, 23, 30, 0, 0, time.FixedZone("west", -7*3600))} {
		t.Run(now.String(), func(t *testing.T) {
			service := New(svc.Config{}, nil)
			calls := 0
			service.Now = func() time.Time { calls++; return now }
			app := inventoryApp()
			rows, err := service.Generate(context.Background(), &spec.Bundle{App: app})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("clock called %d times; one sample needs one clock", calls)
			}
			again, err := service.Generate(context.Background(), &spec.Bundle{App: app})
			if err != nil || !reflect.DeepEqual(rows, again) {
				t.Fatalf("fixed clock and seed did not repeat: %v", err)
			}
			for _, row := range rows["item"] {
				for _, field := range []struct {
					name, layout string
					future       bool
				}{
					{"received_on", "2006-01-02", false},
					{"reorder_due", "2006-01-02", true},
					{"counted_at", time.RFC3339, false},
					{"next_delivery", time.RFC3339, true},
				} {
					value := row.Fields[field.name].(string)
					at, err := time.Parse(field.layout, value)
					if err != nil {
						t.Fatalf("%s is malformed: %q (%v)", field.name, value, err)
					}
					if at.Before(now.AddDate(0, 0, -60)) || at.After(now.AddDate(0, 0, 60)) {
						t.Errorf("%s outside clock spread: %s vs %s", field.name, at, now)
					}
					if field.future && !at.After(now) {
						t.Errorf("%s should be upcoming: %s vs %s", field.name, at, now)
					}
				}
				for _, value := range []string{row.CreatedAt, row.UpdatedAt} {
					at, err := time.Parse(time.RFC3339, value)
					if err != nil || at.Before(now.AddDate(0, 0, -60)) || at.After(now) {
						t.Errorf("metadata ignores clock: %q", value)
					}
				}
			}
		})
	}
}

func TestPersonTextSamplesAreNamesAndMemberFieldsKeepIDs(t *testing.T) {
	for _, tt := range []struct{ typ, typeLabel, title, field, fieldLabel string }{
		{"teacher", "Teacher", "name", "name", "Name"},
		{"instructor", "Instructor", "full_name", "full_name", "Full name"},
		{"contact", "Contact", "name", "name", "Name"},
		{"supplier", "Supplier", "name", "contact_name", "Contact name"},
		{"class", "Class", "name", "teacher", "Teacher"},
		{"staff_member", "Staff member", "name", "name", "Name"},
	} {
		t.Run(tt.typ+"/"+tt.field, func(t *testing.T) {
			fields := map[string]*spec.Field{
				tt.title: {Type: spec.FieldText}, tt.field: {Type: spec.FieldText, Label: tt.fieldLabel, Unique: true},
				"owner": {Type: spec.FieldPerson}, "assistants": {Type: spec.FieldPeople},
			}
			app := &spec.App{ID: "names", Records: map[string]*spec.RecordType{
				tt.typ: {Label: tt.typeLabel, Title: tt.title, Fields: fields},
			}}
			rows, err := GenerateAt(context.Background(), app, 160, 42, sampleNow())
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows[tt.typ] {
				name := row.Fields[tt.field].(string)
				if len(strings.Fields(name)) < 2 || strings.ContainsFunc(name, unicode.IsDigit) || strings.Contains(name, tt.typeLabel) {
					t.Errorf("person-like text is not a plausible name: %q", name)
				}
				if row.Fields["owner"] != svc.Owner || !reflect.DeepEqual(row.Fields["assistants"], []any{svc.Owner}) {
					t.Errorf("member field no longer stores ids: %v", row.Fields)
				}
			}
		})
	}
}

func TestTextSamplesRespectDefaultsAndDoNotTreatHumanRelatedObjectsAsPeople(t *testing.T) {
	for _, tt := range []struct {
		typ, typeLabel, field, fieldLabel string
		def, want                         string
	}{
		{"class", "Class", "name", "Name", `"Gentle flow"`, "Gentle flow"},
		{"customer_order", "Customer order", "name", "Name", "", "January customer order"},
		{"item", "Stock item", "customer_notes", "Customer notes", "", "Kicked off last week and the first draft is with the team"},
		{"teacher", "Teacher", "first_name", "First name", "", "Priya Shah"},
		{"teacher", "Teacher", "last_name", "Last name", "", "Priya Shah"},
	} {
		t.Run(tt.typ+"/"+tt.field, func(t *testing.T) {
			f := &spec.Field{Type: spec.FieldText, Label: tt.fieldLabel, Default: json.RawMessage(tt.def)}
			title := tt.field
			if tt.field == "customer_notes" {
				title = "name"
			}
			fields := map[string]*spec.Field{"name": {Type: spec.FieldText}}
			fields[tt.field] = f
			app := &spec.App{ID: "defaults", Records: map[string]*spec.RecordType{
				tt.typ: {Label: tt.typeLabel, Title: title, Fields: fields},
			}}
			rows, err := GenerateAt(context.Background(), app, 1, 1, sampleNow())
			if err != nil {
				t.Fatal(err)
			}
			if got := rows[tt.typ][0].Fields[tt.field]; got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
