package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskMirrorRefusesHomeWithoutCreatingTree(t *testing.T) {
	for _, mode := range []TaskMode{TaskModeMirror, TaskModeWorktree} {
		t.Run(string(mode), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			writeFile(t, filepath.Join(home, ".Trash", "private.txt"), "private")
			place := Place{Dir: t.TempDir(), Workspace: home}
			if err := SaveMeta(place.Dir, Meta{ID: place.ID(), WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
				t.Fatal(err)
			}
			_, err := prepareTaskTreeOn(context.Background(), place, home, "home-copy", 1, "work", taskStand{dir: home, mode: mode})
			if err == nil || !strings.Contains(err.Error(), "one project's folder") || !strings.Contains(err.Error(), "home folder") {
				t.Fatalf("want home refusal, got %v", err)
			}
			if _, err := os.Stat(place.Trees()); !os.IsNotExist(err) {
				t.Fatalf("created trees before refusal: %v", err)
			}
		})
	}
}

func TestTaskMirrorCopiesFolderUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	folder := filepath.Join(home, "project")
	writeFile(t, filepath.Join(folder, "notes.txt"), "project notes")
	place := Place{Dir: t.TempDir(), Workspace: folder}
	tree, err := prepareTaskTreeOn(context.Background(), place, folder, "project-copy", 1, "work", taskStand{dir: folder, mode: TaskModeMirror})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(tree.dir, "notes.txt")); got != "project notes" {
		t.Fatalf("copied %q", got)
	}
}

func TestStandingCopyRefusesHomeWithoutCreatingTree(t *testing.T) {
	for _, history := range []bool{false, true} {
		name := "plain"
		if history {
			name = "git"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if history {
				home = newTestRepo(t)
			}
			t.Setenv("HOME", home)
			agent, _, place := standingLab(t, home)
			_, err := agent.cutStandingTree(PlaceRef{Path: home})
			if err == nil || !strings.Contains(err.Error(), "home folder") {
				t.Fatalf("want home refusal, got %v", err)
			}
			if _, err := os.Stat((Place{Dir: place}).Trees()); !os.IsNotExist(err) {
				t.Fatalf("created trees before refusal: %v", err)
			}
		})
	}
}
