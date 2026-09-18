package session

import (
	"path/filepath"
	"testing"
)

func TestTaskArchivePreservesItsConversationAndSiblingTasks(t *testing.T) {
	dir := t.TempDir()
	if err := SaveMeta(dir, Meta{ID: "owner", Title: "Keep this conversation", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := SetTaskArchived(dir, "another-owner", "1", true); err == nil {
		t.Fatal("a task belonging to another conversation changed this metadata")
	}
	for _, id := range []string{"1", "2"} {
		if err := SetTaskArchived(dir, "owner", id, true); err != nil {
			t.Fatal(err)
		}
	}
	meta, err := LoadMeta(dir)
	if err != nil || !meta.ArchivedTasks["1"] || !meta.ArchivedTasks["2"] || meta.Archived || meta.Title != "Keep this conversation" || meta.Model != "model" {
		t.Fatalf("task archive changed unrelated metadata: %+v, %v", meta, err)
	}
	if err := SetTaskArchived(dir, "owner", "1", false); err != nil {
		t.Fatal(err)
	}
	meta, _ = LoadMeta(dir)
	if meta.ArchivedTasks["1"] || !meta.ArchivedTasks["2"] {
		t.Fatal("restoring one task changed its sibling's visibility")
	}
	if err := SetTaskArchived(t.TempDir(), "owner", "1", true); err == nil {
		t.Fatal("a missing conversation accepted task metadata")
	}
}

func TestTaskArchiveSurvivesAnEngineMetadataUpdate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	seedMetaLockConversation(t, dir)
	a := metaLockAgent(t, dir)
	a.mu.Lock()
	a.stampUserLocked("keep the task visibility choice")
	a.mu.Unlock()
	meta, _ := LoadMeta(dir)
	if err := SetTaskArchived(dir, meta.ID, "1", true); err != nil {
		t.Fatal(err)
	}
	a.updateMeta(dir, a.metaSnapshot(), func(meta *Meta) { meta.SpentUSD = .5 })
	meta, _ = LoadMeta(dir)
	if !meta.ArchivedTasks["1"] || meta.SpentUSD != .5 {
		t.Fatalf("the engine overwrote the task preference: %+v", meta)
	}
}
