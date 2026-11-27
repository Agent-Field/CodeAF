package builder

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func savedV9(t *testing.T, name string) map[string]string {
	t.Helper()
	body, err := os.ReadFile("testdata/v9-replays/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var captured struct{ App, Plan json.RawMessage }
	if err := json.Unmarshal(body, &captured); err != nil {
		t.Fatal(err)
	}
	return map[string]string{schema.AppFile: string(captured.App), schema.PlanFile: string(captured.Plan)}
}

func TestNormalizeSavedV9RecordDrafts(t *testing.T) {
	t.Run("built-in metadata is ignored", func(t *testing.T) {
		files := savedV9(t, "my-work-queue-1")
		edit := json.RawMessage(`{"type":"item","name":"updated_at","definition":{"type":"datetime"}}`)
		out, err := (authoring{}).edit(t.Context(), "add_field", files, nil, edit)
		if err != nil || out[schema.AppFile] != files[schema.AppFile] {
			t.Fatalf("generated metadata should leave this draft alone: %v", err)
		}
	})
	t.Run("invalid policy is visible as a note", func(t *testing.T) {
		files := savedV9(t, "change-and-revert-1")
		normalized, notes, err := normalizeDraft(files, nil)
		app, _ := schema.ParseApp([]byte(normalized[schema.AppFile]))
		if err != nil || app.Records["book"].Policy != nil || !slices.ContainsFunc(notes, func(p schema.Problem) bool { return p.Path == "/records/book/policy/fields/title" }) {
			t.Fatalf("normalization should drop the invalid title policy and record a note: %v %v", notes, err)
		}
	})
	t.Run("identity survives the outline", func(t *testing.T) {
		files := savedV9(t, "support-shares-crm-identity-2")
		normalized, _, err := normalizeDraft(files, nil)
		app, _ := schema.ParseApp([]byte(normalized[schema.AppFile]))
		if err != nil || app.Records["contact"].Common != "person" || app.Records["company"].Common != "organization" {
			t.Fatalf("company and contact should carry common identities: %+v (%v)", app.Records, err)
		}
	})
	t.Run("registered owner supplies inherited fields", func(t *testing.T) {
		files := savedV9(t, "hiring-reuses-contacts-2")
		app, _ := schema.ParseApp([]byte(files[schema.AppFile]))
		// The saved outline uses the registered type. The failure receipt
		// records the later redeclaration with the same name.
		contact := app.Records["contact"]
		contact.Uses = false
		contact.Fields = map[string]schema.Field{"name": {Type: "text"}, "recruiting_notes": {Type: "longtext"}}
		app.Records["contact"] = contact
		if err := saveJSON(files, schema.AppFile, app); err != nil {
			t.Fatal(err)
		}
		types := schema.TypeSet{"contact": {Owner: "crm", Common: "person", Fields: map[string]schema.Field{"name": {Type: "text"}}}}
		normalized, notes, err := normalizeDraft(files, types)
		app, _ = schema.ParseApp([]byte(normalized[schema.AppFile]))
		contact = app.Records["contact"]
		if err != nil || !contact.Uses || contact.Fields["name"].Type != "" || contact.Fields["recruiting_notes"].Type != "longtext" ||
			!slices.ContainsFunc(notes, func(p schema.Problem) bool { return p.Path == "/records/contact" }) {
			t.Fatalf("use the source and keep local additions: %+v %v (%v)", contact, notes, err)
		}
	})
}

func TestNormalizeSavedV9UnplacedCommands(t *testing.T) {
	for _, name := range []string{"daily-brief-and-commitments-1", "daily-brief-and-commitments-2"} {
		t.Run(name, func(t *testing.T) {
			files := savedV9(t, name)
			app, err := schema.ParseApp([]byte(files[schema.AppFile]))
			if err != nil {
				t.Fatal(err)
			}
			app.Commands = []schema.Command{{ID: "mark-slack-mention-handled", Label: "Mark Slack mention handled", For: "mention", Do: []schema.Action{
				{"records.update": map[string]any{"type": "mention", "id": "$record.id", "set": map[string]any{"handled": true}}},
			}}}
			if err := saveJSON(files, schema.AppFile, app); err != nil {
				t.Fatal(err)
			}
			files["screens/brief.json"] = `{"screen":"brief","title":"Today","body":{"part":"RecordList","id":"work","label":"Meetings","bind":{"records":"meeting"}}}`
			if got := check.UnplacedCommands(definition(files)); len(got) != 1 {
				t.Fatalf("fixture must expose the missing command: %v", got)
			}
			files, notes, err := normalizeDraft(files, nil)
			screen, _ := schema.ParseScreen([]byte(files["screens/brief.json"]))
			if err != nil || len(check.UnplacedCommands(definition(files))) != 0 || !nodeHasID(screen.Body, "row-actions-mention") ||
				!slices.ContainsFunc(notes, func(p schema.Problem) bool { return p.Path == "row-actions-mention" }) {
				t.Fatalf("the command needs a visible row action and a note: %v (%v)", notes, err)
			}
		})
	}
}

func TestNormalizeSavedV9TriggerExamples(t *testing.T) {
	for _, tc := range []struct{ name, record, title, run string }{
		{"bills-inbox-1", "bill", "invoice_number", "extract-invoice-charges"},
		{"bills-inbox-2", "invoice", "number", "extract-due-date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := savedV9(t, tc.name)
			app, err := schema.ParseApp([]byte(files[schema.AppFile]))
			if err != nil {
				t.Fatal(err)
			}
			record := app.Records[tc.record]
			record.Fields["attachment"] = schema.Field{Type: "file"}
			app.Records[tc.record] = record
			app.Triggers = []schema.Trigger{{ID: tc.run, On: "record.changed", Type: tc.record, Run: tc.run,
				When: `changed("attachment") && status == "` + record.Fields["status"].Default.(string) + `"`}}
			if err := saveJSON(files, schema.AppFile, app); err != nil {
				t.Fatal(err)
			}
			sample := schema.Sample{Records: map[string][]map[string]any{tc.record: {{"id": "01M8D5GTC68QMEJEB0K0NE8DWF", tc.title: "Original"}}}}
			if err := saveJSON(files, schema.SampleFile, sample); err != nil {
				t.Fatal(err)
			}
			files, notes, err := normalizeDraft(files, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(files[schema.SampleFile]), &sample); err != nil {
				t.Fatal(err)
			}
			rows := sample.Records[tc.record]
			if len(rows) != 2 || rows[0]["attachment"] == nil || schema.ValidateFileValue("file", rows[0]["attachment"]) != nil ||
				rows[0]["status"] != record.Fields["status"].Default || rows[1][tc.title] != "Original" ||
				!slices.ContainsFunc(notes, func(p schema.Problem) bool { return p.File == schema.SampleFile }) {
				t.Fatalf("a positive synthetic fixture should be inserted once: %v, %v", rows, notes)
			}
			_, again, err := normalizeDraft(files, nil)
			if err != nil || len(again) != 0 {
				t.Fatalf("second normalization must be a no-op: %v %v", again, err)
			}
		})
	}
}
