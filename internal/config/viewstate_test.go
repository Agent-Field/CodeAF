package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestViewStateKeepsLaunchDirectoriesAndProfilesSeparate(t *testing.T) {
	profile, launch := t.TempDir(), t.TempDir()
	view := ViewState{Session: "/saved/transcript.jsonl", Workspace: "/project", Place: "settings"}
	if err := WriteViewState(profile, launch, view); err != nil {
		t.Fatal(err)
	}
	got, ok := ViewStateAt(profile, launch)
	view.Version = 1
	if !ok || got != view {
		t.Fatalf("got %+v, found %v", got, ok)
	}
	for _, scope := range [][2]string{{profile, t.TempDir()}, {t.TempDir(), launch}} {
		if _, found := ViewStateAt(scope[0], scope[1]); found {
			t.Fatal("another launch directory or profile inherited the view")
		}
	}
	info, err := os.Stat(viewStatePath(profile, launch))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("bookmark permissions: %v, %v", info, err)
	}
}

func TestViewStateIgnoresDamagedAndFutureRecords(t *testing.T) {
	profile, launch := t.TempDir(), t.TempDir()
	path := viewStatePath(profile, launch)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"{", `{ "version": 2, "session": "/a", "workspace": "/b", "place": "chat" }`, `{ "version": 1, "session": "relative", "workspace": "/b", "place": "chat" }`} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, found := ViewStateAt(profile, launch); found {
			t.Fatalf("accepted %s", data)
		}
	}
}
