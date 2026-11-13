package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRestoreParentPlanUsesStructuredIdentityAndOwnedTreeNames(t *testing.T) {
	place := newPlace(t, true)
	chat := "codeaf-workspace://11111111111111111111111111111111/22222222222222222222222222222222"
	project := "codeaf-workspace://11111111111111111111111111111111/33333333333333333333333333333333"
	parent := "codeaf-workspace://11111111111111111111111111111111/44444444444444444444444444444444"
	child := "codeaf-workspace://11111111111111111111111111111111/55555555555555555555555555555555"
	parentDir, childDir := filepath.Join(place.Trees(), "1"), filepath.Join(place.Trees(), "2")
	original := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 2, Runs: []runRecord{{ID: 1, Title: "parent", State: TaskDone, Copy: &TaskCopyRecord{URI: parent, ChatURI: chat, ProjectURI: project, GroundURI: project, Dir: parentDir, Ground: t.TempDir(), Mode: TaskModeMirror}}, {ID: 2, Title: "child", State: TaskDone, Copy: &TaskCopyRecord{URI: child, ChatURI: chat, ProjectURI: project, GroundURI: parent, Dir: childDir, Ground: parentDir, Mode: TaskModeWorktree}}}}
	for _, id := range []string{"1", "2"} {
		dir := filepath.Join(place.Dir, ".cell", "trees", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tree.json"), []byte(`{"head":"fixture"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	save := func(document taskDocument) {
		t.Helper()
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(place.Tasks(), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save(original)
	parents, err := TaskCopyRestoreParents(place)
	if err != nil || !reflect.DeepEqual(parents, map[string]string{"2": "1"}) {
		t.Fatalf("nested ground not mapped: %v %v", parents, err)
	}
	for _, bad := range []string{"missing", "cross-chat", "cross-project", "path", "cycle"} {
		t.Run(bad, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var document taskDocument
			_ = json.Unmarshal(raw, &document)
			switch bad {
			case "missing":
				document.Runs[1].Copy.GroundURI = "codeaf-workspace://11111111111111111111111111111111/66666666666666666666666666666666"
			case "cross-chat":
				document.Runs[1].Copy.ChatURI = child
			case "cross-project":
				document.Runs[1].Copy.ProjectURI = child
			case "path":
				document.Runs[1].Copy.Ground = t.TempDir()
			case "cycle":
				document.Runs[0].Copy.GroundURI = child
				document.Runs[0].Copy.Ground = childDir
			}
			save(document)
			if _, err := TaskCopyRestoreParents(place); err == nil {
				t.Fatal("unsafe restoration topology admitted")
			}
		})
	}
}
