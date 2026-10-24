package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/shell"
)

// TestK4MovedPathsUseTheSourceComputersGrammar proves K4's moved-folder rule
// with Linux and Windows origins on either destination OS, through the same
// argument mapper used before tool execution.
func TestK4MovedPathsUseTheSourceComputersGrammar(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ origin, path, command string }{
		{"/srv/project", "/srv/project/sub/file.txt", "cat '/srv/project/sub/file.txt'"},
		{`C:\Users\Person\Project`, `C:\Users\Person\Project\sub\file.txt`, `cat 'C:\Users\Person\Project\sub\file.txt'`},
		{`C:/Users/Person/Project`, `c:/users/person/project/sub/file.txt`, `cat 'C:/Users/Person/Project/sub/file.txt'`},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			chat := t.TempDir()
			if err := SaveMeta(chat, Meta{ID: "k4", Workspace: workspace, Origin: tc.origin}); err != nil {
				t.Fatal(err)
			}
			a := &Agent{config: Config{Workspace: workspace, Place: Place{Dir: chat}}}
			args, _ := json.Marshal(map[string]string{"path": tc.path})
			mapped, notice, err := a.movedToolArguments("read", args)
			if err != nil || notice == "" {
				t.Fatalf("K4 map %q: %s %v", tc.path, notice, err)
			}
			var fields map[string]string
			if err := json.Unmarshal(mapped, &fields); err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(workspace, "sub", "file.txt"); fields["path"] != want {
				t.Fatalf("K4 got %q want %q", fields["path"], want)
			}
			command, err := movedCommand(tc.command, tc.origin, workspace)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(command, shell.ListPath(workspace)+"/sub/file.txt") {
				t.Fatalf("K4 command kept source folder: %s", command)
			}
		})
	}
	for _, value := range []string{"/srv/project-other/file", "/srv/project/../outside", "relative/file"} {
		if _, ok := movedRelative("/srv/project", value); ok {
			t.Fatalf("K4 mapped outside path %q", value)
		}
	}
}

// TestK3MetadataPersistsPortablePaths proves K3 without altering transcript
// bytes: path fields use forward slashes on disk and old Windows paths are
// still mapped by K4 after another platform loads them.
func TestK3MetadataPersistsPortablePaths(t *testing.T) {
	dir := t.TempDir()
	original := Meta{ID: "k3", Workspace: `C:\Users\Person\Project`, Origin: `C:\old\Project`, FromWorkspace: `D:\copy\Project`}
	if err := SaveMeta(dir, original); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, placeMeta))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `\\`) {
		t.Fatalf("K3 persisted native separators: %s", raw)
	}
	loaded, err := LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rel, ok := movedRelative(loaded.Origin, `C:\old\Project\file.txt`); !ok || rel != "file.txt" {
		t.Fatalf("K4 cannot read portable origin %q", loaded.Origin)
	}
}
