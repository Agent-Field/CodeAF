package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentLabelsReadOnlyRequestedMetadataNotSessionJournals(t *testing.T) {
	root := t.TempDir()
	bucket := filepath.Join(root, "project")
	if err := os.Mkdir(bucket, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "unrequested"} {
		dir := filepath.Join(bucket, id)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]string{"id": id, "title": "Name " + id})
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte("not a transcript; never parse this"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reads := 0
	found, partial := readAgentLabelBatch(root, []string{"a", "b"}, time.Now().Add(time.Second), 512, func(path, id string) (string, bool) {
		reads++
		if filepath.Base(path) != "meta.json" || id == "unrequested" {
			t.Fatal("read unrelated source")
		}
		return loadAgentLabelMeta(path, id)
	})
	if partial || reads != 2 || found["a"] != "Name a" || found["b"] != "Name b" {
		t.Fatalf("lookup %#v partial%v reads%d", found, partial, reads)
	}
}
func TestAgentLabelsBudgetDoesNotInventMissingNames(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	calls := 0
	load := func(string, string) (string, bool) { calls++; return "", false }
	got, partial := readAgentLabelBatch(root, []string{"a"}, time.Now().Add(-time.Second), 512, load)
	if !partial || len(got) != 0 || calls != 0 {
		t.Fatalf("expired lookup did work: %#v %v %d", got, partial, calls)
	}
	got, partial = readAgentLabelBatch(root, []string{"a"}, time.Now().Add(time.Second), 1, load)
	if !partial || len(got) != 0 || calls != 1 {
		t.Fatalf("bounded lookup pretended complete: %#v %v %d", got, partial, calls)
	}
}
func TestAgentLabelMetaRequiresTheActualIdentityAndBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.json")
	for _, raw := range []string{`{"id":"another","title":"Wrong"}`, `{"id":"a","title":`, string(make([]byte, labelMetaBytes+1))} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := loadAgentLabelMeta(path, "a"); ok {
			t.Fatal("invalid metadata became a label")
		}
	}
}
