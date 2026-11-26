package apppackage_test

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
)

func TestMergeRequiresAnExplicitPermissionChoice(t *testing.T) {
	base := map[string]string{"app.json": `{"version":1,"capabilities":{"records":{"contact":"r"}}}`}
	local := map[string]string{"app.json": `{"version":4,"capabilities":{"records":{"contact":"r"}}}`}
	upstream := map[string]string{"app.json": `{"version":1,"capabilities":{"records":{"contact":"rw"}},"name":"New CRM"}`}
	merged, conflicts, err := apppackage.Merge(base, local, upstream, nil)
	if err != nil || len(conflicts) != 1 || conflicts[0].ID != "app.json#/capabilities" {
		t.Fatalf("permission merge: %v, %+v", err, conflicts)
	}
	assertGrant(t, merged, "r")
	chosen, _, err := apppackage.Merge(base, local, upstream, map[string]string{conflicts[0].ID: "upstream"})
	if err != nil {
		t.Fatal(err)
	}
	assertGrant(t, chosen, "rw")
}

func assertGrant(t *testing.T, files map[string]string, access string) {
	t.Helper()
	var app struct {
		Name         string
		Capabilities struct{ Records map[string]string }
	}
	if err := json.Unmarshal([]byte(files["app.json"]), &app); err != nil || app.Name != "New CRM" ||
		app.Capabilities.Records["contact"] != access {
		t.Fatalf("merged app with grant %q: %s, %v", access, files["app.json"], err)
	}
}

func TestMergeKeepsUnrelatedNodeEditsAndReportsDeletion(t *testing.T) {
	key := "screens/main.json"
	base := map[string]string{key: `{"children":[{"id":"left","label":"L","value":1},{"id":"right","label":"R","obsolete":true}]}`}
	local := map[string]string{key: `{"children":[{"id":"left","label":"Left","value":1},{"id":"right","label":"R","obsolete":false}]}`}
	upstream := map[string]string{key: `{"children":[{"id":"left","label":"L","value":2},{"id":"right","label":"R"}]}`}
	_, conflicts, err := apppackage.Merge(base, local, upstream, nil)
	if err != nil || len(conflicts) != 1 || conflicts[0].ID != key+"#/children/right/obsolete" || conflicts[0].Upstream != nil {
		t.Fatalf("node merge: %v, %+v", err, conflicts)
	}
	merged, _, err := apppackage.Merge(base, local, upstream, map[string]string{conflicts[0].ID: "upstream"})
	var screen struct{ Children []map[string]any }
	if err != nil || json.Unmarshal([]byte(merged[key]), &screen) != nil || len(screen.Children) != 2 ||
		screen.Children[0]["label"] != "Left" || screen.Children[0]["value"] != float64(2) || screen.Children[1]["obsolete"] != nil {
		t.Fatalf("resolved nodes: %v, %s", err, merged[key])
	}
}

func TestMergeRequiresAChoiceBeforeRelaxingRecordPolicy(t *testing.T) {
	base := map[string]string{"app.json": `{"records":{"contact":{"policy":{"row":{"field":"assignee","attribute":"id"}},"fields":{"email":{"type":"email","class":"pii","masked":true}}}}}`}
	upstream := map[string]string{"app.json": `{"records":{"contact":{"fields":{"email":{"type":"email","class":"public"},"website":{"type":"url"}}}}}`}
	merged, conflicts, err := apppackage.Merge(base, base, upstream, nil)
	if err != nil || len(conflicts) != 3 {
		t.Fatalf("protected policy changes: %v, %+v", err, conflicts)
	}
	want := []string{"/records/contact/fields/email/class", "/records/contact/fields/email/masked", "/records/contact/policy"}
	for i, conflict := range conflicts {
		if conflict.Path != want[i] {
			t.Fatalf("conflict %d = %s, want %s", i, conflict.Path, want[i])
		}
	}
	var app struct {
		Records map[string]struct {
			Policy struct{ Row struct{ Field string } }
			Fields map[string]struct {
				Type   string
				Class  string
				Masked bool
			}
		}
	}
	if err := json.Unmarshal([]byte(merged["app.json"]), &app); err != nil {
		t.Fatal(err)
	}
	contact := app.Records["contact"]
	if contact.Fields["website"].Type != "url" || contact.Fields["email"].Class != "pii" ||
		!contact.Fields["email"].Masked || contact.Policy.Row.Field != "assignee" {
		t.Fatalf("candidate lost protection or blocked an unrelated field: %s", merged["app.json"])
	}
}

func TestMergeRequiresAChoiceBeforeRemovingAProtectedField(t *testing.T) {
	base := map[string]string{"app.json": `{"records":{"contact":{"fields":{"email":{"type":"email","class":"pii","masked":true},"old":{"type":"text"}}}}}`}
	upstream := map[string]string{"app.json": `{"records":{"contact":{"fields":{}}}}`}
	_, conflicts, err := apppackage.Merge(base, base, upstream, nil)
	if err != nil || len(conflicts) != 1 || conflicts[0].Path != "/records/contact/fields/email" || conflicts[0].Upstream != nil {
		t.Fatalf("protected field removal: %v, %+v", err, conflicts)
	}
	merged, _, err := apppackage.Merge(base, base, upstream, map[string]string{conflicts[0].ID: "upstream"})
	if err != nil || merged["app.json"] != upstream["app.json"] {
		t.Fatalf("chosen removal did not match upstream: %s, %v", merged["app.json"], err)
	}
}
