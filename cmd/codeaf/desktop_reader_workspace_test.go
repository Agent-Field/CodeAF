package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopReaderUsesTheSavedChatsWorkspace(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "terminal-project")
	chat := filepath.Join(root, "chat")
	for _, dir := range []string{project, chat} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	meta, _ := json.Marshal(map[string]string{"id": "saved-chat", "workspace": project})
	if err := os.WriteFile(filepath.Join(chat, "meta.json"), meta, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := desktopReaderWorkspace(filepath.Join(chat, "transcript.jsonl"))
	if err != nil || got != project {
		t.Fatalf("saved project replaced: %q %v", got, err)
	}
	for _, file := range []string{"", filepath.Join(root, "missing", "transcript.jsonl")} {
		if _, err := desktopReaderWorkspace(file); err == nil {
			t.Fatalf("invalid reader allowed: %q", file)
		}
	}
}
