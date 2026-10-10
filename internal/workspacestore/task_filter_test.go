package workspacestore

import (
	"encoding/json"
	"testing"
)

func TestTaskFiltersSurviveCanonicalWriteReadAndReopen(t *testing.T) {
	dir := t.TempDir()
	store := open(t, dir)
	document := json.RawMessage(`{"schema":1,"tabs":[{"id":"a","kind":"conversation","title":"A","draft":"","pinned":false,"tasksFilter":"needs"},{"id":"b","kind":"conversation","title":"B","draft":"","pinned":false,"tasksFilter":"running"}],"groups":[],"closed":[],"nextNumber":3}`)
	saved, err := store.Put("now", 0, "window", document)
	if err != nil {
		t.Fatal(err)
	}
	for _, reader := range []*Store{store, open(t, dir)} {
		record, err := reader.Get("now")
		if err != nil {
			t.Fatal(err)
		}
		if record.Revision != saved.Revision {
			t.Fatalf("revision %d != %d", record.Revision, saved.Revision)
		}
		var actual struct {
			Tabs []struct {
				ID     string `json:"id"`
				Filter string `json:"tasksFilter"`
			} `json:"tabs"`
		}
		if err := json.Unmarshal(record.Workspace, &actual); err != nil {
			t.Fatal(err)
		}
		if len(actual.Tabs) != 2 || actual.Tabs[0].Filter != "needs" || actual.Tabs[1].Filter != "running" {
			t.Fatalf("lost per-tab filters: %+v", actual.Tabs)
		}
	}
}
