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

func TestWorkspaceSetRefusesAFolderHoldingHome(t *testing.T) {
	above := t.TempDir()
	home := filepath.Join(above, "person")
	writeFile(t, filepath.Join(home, ".Trash", "private.txt"), "private")
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", t.TempDir())
	place := Place{Dir: t.TempDir(), Workspace: above}
	for _, source := range []string{home, above} {
		request := WorkspaceSetRequest{Roots: []WorkspaceRootRequest{{Source: source}}}
		_, err := validateWorkspaceSetSources(context.Background(), place, request, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "personal home") {
			t.Fatalf("source %s: want home refusal, got %v", source, err)
		}
	}
}

func TestWorkspaceSetAdmitsAFolderInsideHome(t *testing.T) {
	home := t.TempDir()
	folder := filepath.Join(home, "project")
	writeFile(t, filepath.Join(folder, "notes.txt"), "project notes")
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", t.TempDir())
	request := WorkspaceSetRequest{Roots: []WorkspaceRootRequest{{Source: folder}}}
	if _, err := validateWorkspaceSetSources(context.Background(), Place{Dir: t.TempDir(), Workspace: folder}, request, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

// A chat that refers to the home folder keeps today's behaviour: standingAim
// sends the write to the real path when no copy can be cut, so a refusal here
// would turn a kept copy into a live edit of the person's files.
func TestStandingCopyOfHomeIsNotRefused(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "notes.txt"), "notes")
	t.Setenv("HOME", home)
	agent, _, _ := standingLab(t, home)
	if _, err := agent.cutStandingTree(PlaceRef{Path: home}); err != nil {
		t.Fatalf("a standing copy must not be refused here: %v", err)
	}
}
