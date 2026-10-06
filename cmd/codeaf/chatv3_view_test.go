package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestSavedViewRestoresChosenSessionAndObeysExplicitDoors(t *testing.T) {
	profile, launch, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	chosen := filepath.Join(workspace, "chosen.jsonl")
	for _, name := range []string{chosen, filepath.Join(workspace, "newer.jsonl")} {
		if err := os.WriteFile(name, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	view := config.ViewState{Session: chosen, Workspace: workspace, Place: "home"}
	if err := config.WriteViewState(profile, launch, view); err != nil {
		t.Fatal(err)
	}
	if got := v3SavedView(profile, launch, "", "", false); got.Session != chosen || got.Place != "home" {
		t.Fatalf("did not restore selected chat: %+v", got)
	}
	for _, door := range []struct {
		explicit, once string
		pick           bool
	}{{explicit: "asked.jsonl"}, {once: "hello"}, {pick: true}} {
		if got := v3SavedView(profile, launch, door.explicit, door.once, door.pick); got.Session != "" {
			t.Fatalf("saved view overrode explicit door: %+v", door)
		}
	}
	if err := os.Remove(chosen); err != nil {
		t.Fatal(err)
	}
	if got := v3SavedView(profile, launch, "", "", false); got.Session != "" {
		t.Fatal("missing chat was restored")
	}
	if err := os.WriteFile(chosen, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	view.Workspace = filepath.Join(workspace, "gone")
	if err := config.WriteViewState(profile, launch, view); err != nil {
		t.Fatal(err)
	}
	if got := v3SavedView(profile, launch, "", "", false); got.Session != "" {
		t.Fatal("missing workspace was restored")
	}
}
