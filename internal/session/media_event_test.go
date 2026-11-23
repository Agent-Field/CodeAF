package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaChangeIsEmittedOnlyAfterTheActualSourceWasRecorded(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "actual.png")
	agent, _ := newMediaAgent(t, nil, func(c *Config) { c.ArtifactsIndex = index; c.SessionFile = filepath.Join(dir, "session.jsonl") })
	seen := 0
	hub := newEventHub()
	hub.observe = func(event Event) {
		if event.Kind != EventMediaUpdate {
			return
		}
		seen++
		if event.Media == nil || event.Media.ID == "" || event.Media.Run != "run_actual" || event.Media.Call != "call_actual" {
			t.Fatalf("invalid source notification: %+v", event.Media)
		}
		if _, err := FindMedia(index, agent.journalID(), event.Media.ID, time.Time{}); err != nil {
			t.Fatalf("event preceded actual commit: %v", err)
		}
	}
	agent.mu.Lock()
	agent.hub = hub
	agent.mu.Unlock()
	fact := GeneratedMedia(path, pngOfSize(t, 4, 3), "painter")
	fact.Run = "run_actual"
	fact.Tool = "call_actual"
	artifact := Artifact{Path: path, Title: "actual file", Kind: "image", Created: time.Now()}
	agent.recordMedia(artifact, fact)
	if seen != 1 {
		t.Fatalf("source produced %d notifications", seen)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	} // A directory cannot be appended as a source index.
	agent.recordMedia(artifact, fact)
	if seen != 1 {
		t.Fatal("a failed write emitted an invented source")
	}
}
func TestMediaChangeReachesTheStandingLaneWithoutAnActiveTurn(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "actual.png")
	agent, _ := newMediaAgent(t, nil, func(c *Config) { c.ArtifactsIndex = index; c.SessionFile = filepath.Join(dir, "session.jsonl") })
	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	agent.recordMedia(Artifact{Path: path, Title: "actual file", Kind: "image", Created: time.Now()}, GeneratedMedia(path, pngOfSize(t, 4, 3), "painter"))
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-lane:
			if event.Kind == EventMediaUpdate {
				if _, err := FindMedia(index, agent.journalID(), event.Media.ID, time.Time{}); err != nil {
					t.Fatal(err)
				}
				return
			}
		case <-deadline:
			t.Fatal("idle subscription missed source append")
		}
	}
}
func TestRecordAttachmentReturnsOnlyAnActualPersistedSourceIdentity(t *testing.T) {
	dir := t.TempDir()
	path := mediaFile(t, dir, "picked.png")
	index := filepath.Join(dir, "artifacts.jsonl")
	if id := RecordAttachment(dir, "s", path); id != "" {
		t.Fatal("failed append returned a source identity")
	}
	id := RecordAttachment(index, "s", path)
	if id == "" {
		t.Fatal("actual picked media lacks identity")
	}
	if _, err := FindMedia(index, "s", id, time.Time{}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("words"), 0600)
	if id := RecordAttachment(index, "s", filepath.Join(dir, "notes.txt")); id != "" {
		t.Fatal("nonmedia attachment produced an invalidation")
	}
}
