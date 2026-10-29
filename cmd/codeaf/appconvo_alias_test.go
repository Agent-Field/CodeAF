package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// aliasBackend is a desktop backend with one chat whose workspace is spelled
// through a symbolic link the test made, so the alias exists on every platform
// and not only where the temporary folder is reached through one (macOS /var).
func aliasBackend(t *testing.T, spelledThroughLink bool) (backend *appBackend, agent appwire.Agent, real, link string) {
	t.Helper()
	base := t.TempDir()
	real = filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("this filesystem cannot make a symbolic link: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	real = resolved
	if err := os.WriteFile(filepath.Join(real, "notes.md"), []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var opens, releases int
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	backend = newFakeBackend(newAppRecorder(), fakeOpener(conv, &opens, &releases, &mu))
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	workspace := real
	if spelledThroughLink {
		workspace = link
	}
	agent, err = mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	return backend, agent, real, link
}

// A file the picker names in its resolved spelling is inside a workspace typed
// through a link, and the other way round; a missing file there is missing,
// not outside; a file that is really elsewhere stays outside.
func TestAppFileStatUsesResolvedWorkspaceContainment(t *testing.T) {
	for _, throughLink := range []bool{true, false} {
		backend, agent, real, link := aliasBackend(t, throughLink)
		other := real
		if !throughLink {
			other = link
		}
		elsewhere := filepath.Join(t.TempDir(), "elsewhere.md")
		if err := os.WriteFile(elsewhere, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := backend.StatFiles(agent.ID, []string{
			"notes.md",
			filepath.Join(other, "notes.md"),
			filepath.Join(other, "gone.md"),
			elsewhere,
		})
		if err != nil || len(stats) != 4 {
			t.Fatalf("workspace %q: files.stat = %+v %v", agent.Workspace, stats, err)
		}
		want := []string{appwire.FileExists, appwire.FileExists, appwire.FileMissing, appwire.FileOutside}
		for i, stat := range stats {
			if stat.State != want[i] {
				t.Errorf("workspace %q: %s is %q, want %q", agent.Workspace, stat.Path, stat.State, want[i])
			}
		}
		if stats[1].Path != filepath.Join(other, "notes.md") {
			t.Errorf("the stat renamed the path it was asked about: %q", stats[1].Path)
		}
		chip, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachFile, Path: filepath.Join(other, "notes.md")}, "")
		if err != nil || chip.State != appwire.FileExists {
			t.Errorf("workspace %q: the attachment chip = %+v %v", agent.Workspace, chip, err)
		}
	}
}

// A change recorded under the workspace's own spelling is found for the same
// file named in the other spelling.
func TestAppFileStatFindsAChangeRecordedUnderTheWorkspaceSpelling(t *testing.T) {
	backend, agent, real, link := aliasBackend(t, true)
	changed := map[string]appwire.FileChange{
		filepath.Join(link, "notes.md"): {Path: filepath.Join(link, "notes.md"), Add: 3, Del: 1},
	}
	stat := backend.statFile(agent.Workspace, changed, filepath.Join(real, "notes.md"))
	if stat.State != appwire.FileChanged || stat.Add == nil || *stat.Add != 3 || stat.Del == nil || *stat.Del != 1 {
		t.Fatalf("the change was not found through the other spelling: %+v", stat)
	}
	// A tool call that named the file relative to the workspace records it so.
	relative := map[string]appwire.FileChange{"notes.md": {Path: "notes.md", Add: 2, Del: 0}}
	for _, spelled := range []string{filepath.Join(real, "notes.md"), filepath.Join(link, "notes.md")} {
		stat := backend.statFile(agent.Workspace, relative, spelled)
		if stat.State != appwire.FileChanged || stat.Add == nil || *stat.Add != 2 {
			t.Fatalf("a change recorded as notes.md was not found for %s: %+v", spelled, stat)
		}
	}
}

// A link inside the workspace that points elsewhere keeps the answer it had:
// it is inside by spelling, and this change only adds what is inside by place.
func TestAppFileStatKeepsALinkInsideTheWorkspaceInside(t *testing.T) {
	backend, agent, real, _ := aliasBackend(t, false)
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "far.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(real, "far")); err != nil {
		t.Skipf("this filesystem cannot make a symbolic link: %v", err)
	}
	stats, err := backend.StatFiles(agent.ID, []string{"far/far.md"})
	if err != nil || len(stats) != 1 || stats[0].State != appwire.FileExists {
		t.Fatalf("a file through a link inside the workspace = %+v %v", stats, err)
	}
}
