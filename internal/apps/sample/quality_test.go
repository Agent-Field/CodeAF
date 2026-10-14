package sample

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var leadingVerbs = map[string]bool{"review": true, "update": true, "check": true, "improve": true, "prepare": true, "revise": true, "adjust": true, "organize": true, "complete": true, "confirm": true, "refresh": true, "align": true, "polish": true}

func studioApp() *spec.App {
	return &spec.App{ID: "studio", Name: "Studio desk", Records: map[string]*spec.RecordType{
		"client": {Label: "Client", Title: "name", Fields: map[string]*spec.Field{
			"name":       {Type: spec.FieldText},
			"email":      {Type: spec.FieldEmail},
			"joined_on":  {Type: spec.FieldDate},
			"birth_date": {Type: spec.FieldDate},
		}},
		"brand": {Label: "Brand", Title: "name", Fields: map[string]*spec.Field{
			"name":        {Type: spec.FieldText},
			"website_url": {Type: spec.FieldText},
			"notes":       {Type: spec.FieldLongtext},
		}},
		"project": {Label: "Project", Title: "title", Fields: map[string]*spec.Field{
			"title":      {Type: spec.FieldText},
			"brand":      {Type: spec.FieldRef, To: "brand"},
			"status":     {Type: spec.FieldEnum, Values: []string{"Idea", "Brief", "In progress", "Review", "Done"}},
			"budget":     {Type: spec.FieldMoney},
			"price":      {Type: spec.FieldMoney, Label: "Unit price"},
			"rating":     {Type: spec.FieldNumber},
			"due_on":     {Type: spec.FieldDate},
			"started_at": {Type: spec.FieldDatetime},
			"summary":    {Type: spec.FieldLongtext},
		}},
	}}
}

func TestSampleTextReadsLikeRealData(t *testing.T) {
	app := studioApp()
	now := sampleNow()
	rows, err := GenerateAt(context.Background(), app, DefaultCount, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	for typ, list := range rows {
		if len(list) < 6 {
			t.Fatalf("%s has %d rows; a main list needs at least six", typ, len(list))
		}
		schema := app.Records[typ]
		for _, row := range list {
			title := row.Fields[schema.Title].(string)
			if first := strings.ToLower(strings.Fields(title)[0]); leadingVerbs[first] || strings.Contains(title, ": ") {
				t.Errorf("title reads as a chore: %q", title)
			}
			for name, f := range schema.Fields {
				text, ok := row.Fields[name].(string)
				if ok && textual(f.Type) && (strings.Contains(text, "_") || strings.ContainsAny(text, "0123456789")) {
					t.Errorf("%s.%s has an id or counter: %q", typ, name, text)
				}
				if ok && (f.Type == spec.FieldDate || f.Type == spec.FieldDatetime) {
					at, _ := time.Parse(time.RFC3339, text)
					if f.Type == spec.FieldDate {
						at, _ = time.Parse("2006-01-02", text)
					}
					if at.Before(now.AddDate(0, 0, -60)) || at.After(now.AddDate(0, 0, 60)) {
						t.Errorf("%s.%s is %s, too far from now", typ, name, text)
					}
				}
			}
		}
	}
	for _, row := range rows["client"] {
		name := row.Fields["name"].(string)
		want := strings.ToLower(strings.ReplaceAll(name, " ", ".")) + "@"
		if email := row.Fields["email"].(string); !strings.HasPrefix(email, want) || !strings.HasSuffix(email, ".example") {
			t.Errorf("email %q does not belong to %q", email, name)
		}
	}
	for i, row := range rows["project"] {
		brand := rows["brand"][i%len(rows["brand"])].Fields["name"].(string)
		if title := row.Fields["title"].(string); !strings.HasSuffix(title, " — "+brand) {
			t.Errorf("project %q is not named after its brand %q", title, brand)
		}
		if summary := row.Fields["summary"].(string); !strings.Contains(summary, row.Fields["title"].(string)) {
			t.Errorf("summary does not mention its record: %q", summary)
		}
		if budget, price, rating := row.Fields["budget"].(float64), row.Fields["price"].(float64), row.Fields["rating"].(float64); budget < 1000 || price > 1000 || rating < 1 || rating > 5 {
			t.Errorf("magnitudes ignore field meaning: budget %v price %v rating %v", budget, price, rating)
		}
		if due, _ := time.Parse("2006-01-02", row.Fields["due_on"].(string)); !due.After(now) {
			t.Errorf("due date should be upcoming: %v", row.Fields["due_on"])
		}
		if started, _ := time.Parse(time.RFC3339, row.Fields["started_at"].(string)); !started.Before(now) {
			t.Errorf("start time should be past: %v", row.Fields["started_at"])
		}
	}
}

func TestTitlesCarryNoTypePrefixAndTypesDoNotShareATemplate(t *testing.T) {
	for _, app := range []*spec.App{studioApp(), bugTrackerApp(), inventoryApp()} {
		rows, err := GenerateAt(context.Background(), app, DefaultCount, 7, sampleNow())
		if err != nil {
			t.Fatal(err)
		}
		lead := map[int]map[string]string{}
		for typ, list := range rows {
			schema := app.Records[typ]
			for i, row := range list {
				title := row.Fields[schema.Title].(string)
				for _, prefix := range []string{label(schema.Label, typ), typ} {
					if strings.HasPrefix(strings.ToLower(title), strings.ToLower(prefix)+":") {
						t.Errorf("title repeats its type as a prefix: %q", title)
					}
				}
				if kindOf(typ, schema).name != kindItem {
					continue
				}
				first := strings.Fields(title)[0]
				if lead[i] == nil {
					lead[i] = map[string]string{}
				}
				if other, ok := lead[i][first]; ok {
					t.Errorf("row %d of %s and %s start alike: %q", i, typ, other, title)
				}
				lead[i][first] = typ
			}
		}
	}
}

func TestStatusSpreadShowsEveryStateAndFavoursTheMiddle(t *testing.T) {
	rows, err := GenerateAt(context.Background(), studioApp(), DefaultCount, 7, sampleNow())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, row := range rows["project"] {
		counts[row.Fields["status"].(string)]++
	}
	values := studioApp().Records["project"].Fields["status"].Values
	middle := 0
	for i, value := range values {
		if counts[value] == 0 {
			t.Errorf("state %q never appears: %v", value, counts)
		}
		if i > 0 && i < len(values)-1 {
			middle += counts[value]
		}
	}
	if middle*2 <= DefaultCount {
		t.Errorf("most rows should be mid-pipeline: %v", counts)
	}
}

type vocabModel struct {
	reply string
	calls atomic.Int32
}

func (m *vocabModel) Complete(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	m.calls.Add(1)
	return svc.ModelReply{Text: m.reply}, nil
}

func TestModelVocabularyIsUsedOnceAndValidated(t *testing.T) {
	model := &vocabModel{reply: "```json\n" + `{"types":{"project":{"titles":["Autumn lookbook shoot","Packaging refresh","bad_title","Packaging refresh"],"fields":{"summary":["Mood board approved; shoot booked for next week."],"status":["Done"]}},"ghost":{"titles":["Nope there"]}}}` + "\n```"}
	service := New(svc.Config{}, &svc.Services{Model: model})
	service.Now = sampleNow
	def := &spec.Bundle{App: studioApp(), Plan: &spec.Plan{Ask: "A desk for my design studio"}}
	rows, err := service.Generate(context.Background(), def)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Generate(context.Background(), def); err != nil || model.calls.Load() != 1 {
		t.Fatalf("vocabulary should be asked once per definition: %d calls, %v", model.calls.Load(), err)
	}
	project := rows["project"]
	if project[0].Fields["title"] != "Autumn lookbook shoot" || project[1].Fields["title"] != "Packaging refresh" {
		t.Errorf("model titles unused: %v, %v", project[0].Fields["title"], project[1].Fields["title"])
	}
	if title := project[2].Fields["title"].(string); title == "bad_title" || title == "Packaging refresh" {
		t.Errorf("invalid or duplicate model title kept: %q", title)
	}
	if project[0].Fields["summary"] != "Mood board approved; shoot booked for next week." {
		t.Errorf("model longtext unused: %v", project[0].Fields["summary"])
	}
	if project[0].Fields["status"] != "Idea" {
		t.Errorf("model must not fill enum fields: %v", project[0].Fields["status"])
	}
	garbage := &vocabModel{reply: `{"severity":"S1"}`}
	fallback := New(svc.Config{}, &svc.Services{Model: garbage})
	fallback.Now = sampleNow
	got, err := fallback.Generate(context.Background(), def)
	want, _ := GenerateAt(context.Background(), def.App, DefaultCount, 1, sampleNow())
	if err != nil || got["project"][0].Fields["title"] != want["project"][0].Fields["title"] {
		t.Fatalf("unusable vocabulary should fall back to the deterministic sample: %v", err)
	}
}
