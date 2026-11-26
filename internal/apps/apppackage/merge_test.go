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
