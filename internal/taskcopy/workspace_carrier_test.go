package taskcopy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func workspaceFixture(t *testing.T) (cell.Cell, WorkspacePlan) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	c := cell.Cell{ID: "mock", Root: filepath.Join(t.TempDir(), "chat")}
	if err := os.MkdirAll(filepath.Join(c.Root, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Register(workspaceidentity.Chat, c.Root)
	if err != nil {
		t.Fatal(err)
	}
	plan := WorkspacePlan{Version: 2, ChatURI: chat.URI, Groups: []WorkspaceGroup{{TaskID: 7, Required: true, Primary: "main"}}}
	for _, slot := range []string{"main", "docs"} {
		project := t.TempDir()
		write(t, filepath.Join(project, "base.txt"), "base\n")
		write(t, filepath.Join(project, "deleted.txt"), "delete\n")
		run(t, project, "git", "init", "-q", "-b", "main")
		run(t, project, "git", "add", ".")
		run(t, project, "git", "commit", "-qm", "base")
		pr, err := registry.Register(workspaceidentity.Project, project)
		if err != nil {
			t.Fatal(err)
		}
		layout := "7/" + slot
		tree := filepath.Join(liveRoot(c), filepath.FromSlash(layout))
		if err := os.MkdirAll(filepath.Dir(tree), 0700); err != nil {
			t.Fatal(err)
		}
		run(t, project, "git", "clone", "-q", "--no-hardlinks", project, tree)
		write(t, filepath.Join(tree, "base.txt"), "edited "+slot+"\n")
		write(t, filepath.Join(tree, "new.txt"), "new "+slot+"\n")
		if err := os.Chmod(filepath.Join(tree, "new.txt"), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(tree, "deleted.txt")); err != nil {
			t.Fatal(err)
		}
		cp, err := registry.Register(workspaceidentity.Run, tree, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: pr.URI})
		if err != nil {
			t.Fatal(err)
		}
		plan.Groups[0].Entries = append(plan.Groups[0].Entries, WorkspaceEntry{CopyURI: cp.URI, ChatURI: chat.URI, ProjectURI: pr.URI, RootURI: pr.URI, GroundURI: pr.URI, Layout: layout, Access: "write"})
	}
	writeWorkspaceCheckpoint(t, c, plan)
	return c, plan
}
func writeWorkspaceCheckpoint(t *testing.T, c cell.Cell, plan WorkspacePlan) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": "tasks", "version": 2, "workspace_transfer": plan})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, cell.StateDir, "tasks.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestWorkspaceCarrierCapturesAllSiblingRoots(t *testing.T) {
	c, plan := workspaceFixture(t)
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := cell.Cell{ID: "receiver", Root: filepath.Join(t.TempDir(), "chat")}
	if err := os.MkdirAll(filepath.Join(receiver.Root, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	run(t, c.Root, "cp", "-R", filepath.Join(c.Root, cell.StateDir, "workspaces-v2"), filepath.Join(receiver.Root, cell.StateDir, "workspaces-v2"))
	writeWorkspaceCheckpoint(t, receiver, plan)
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(liveRoot(receiver)); !os.IsNotExist(err) {
		t.Fatalf("prepare exposed execution roots: %v", err)
	}
	bindings, err := prepared.Promote()
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 {
		t.Fatalf("bindings: %+v", bindings)
	}
	for _, slot := range []string{"main", "docs"} {
		tree := filepath.Join(liveRoot(receiver), "7", slot)
		for name, want := range map[string]string{"base.txt": "edited " + slot + "\n", "new.txt": "new " + slot + "\n"} {
			got, err := os.ReadFile(filepath.Join(tree, name))
			if err != nil || string(got) != want {
				t.Fatalf("%s/%s: %q %v", slot, name, got, err)
			}
		}
		if _, err := os.Stat(filepath.Join(tree, "deleted.txt")); !os.IsNotExist(err) {
			t.Fatal("deleted file restored")
		}
		info, err := os.Stat(filepath.Join(tree, "new.txt"))
		if err != nil || info.Mode().Perm() != 0750 {
			t.Fatalf("mode: %v %v", info, err)
		}
	}
	if err := prepared.Acknowledge(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceCaptureReusesBoundedGenerationsAndRecoversOwnedCandidate(t *testing.T) {
	c, plan := workspaceFixture(t)
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(workspaceCarrierRoot(c), "generation-unrelated")
	write(t, filepath.Join(unknown, "keep"), "unrelated")
	// Record a partially written generation exactly as a killed capture leaves it.
	orphan := "generation-0123456789abcdef0123456789abcdef"
	write(t, filepath.Join(workspaceCarrierRoot(c), orphan, "partial"), "partial")
	manifest, err := readWorkspaceManifest(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeWorkspaceJSON(filepath.Join(c.Root, ".codeaf", "workspace-capture.json"), workspaceCaptureReceipt{Version: 2, Stage: orphan, Previous: manifest.Generation}); err != nil {
		t.Fatal(err)
	}
	if err = (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(workspaceCarrierRoot(c), orphan)); !os.IsNotExist(err) {
		t.Fatal("journaled orphan survived")
	}
	if _, err = os.Stat(filepath.Join(unknown, "keep")); err != nil {
		t.Fatal("unrelated generation removed")
	}
	if err = (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(workspaceCarrierRoot(c))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("unbounded generations: %v", entries)
	}
	got, _, err := ReadWorkspacePlan(c)
	if err != nil || got == nil || got.ChatURI != plan.ChatURI {
		t.Fatal("capture altered checkpoint")
	}
}

func TestWorkspaceCarrierPreservesDirectoriesAndTheirAbsence(t *testing.T) {
	c, plan := workspaceFixture(t)
	for _, entry := range plan.Groups[0].Entries {
		tree := filepath.Join(liveRoot(c), filepath.FromSlash(entry.Layout))
		write(t, filepath.Join(tree, "old-dir", "removed.txt"), "remove this baseline file\n")
		run(t, tree, "git", "add", "old-dir/removed.txt")
		run(t, tree, "git", "commit", "-qm", "old directory")
		if err := os.RemoveAll(filepath.Join(tree, "old-dir")); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(tree, "user-dir", "private.txt"), "private output\n")
		if err := os.Chmod(filepath.Join(tree, "user-dir"), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(tree, "user-dir", "private.txt"), 0640); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(tree, "user-dir", "empty"), 0700); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(tree, ".gitignore"), "cache/\n")
		if err := os.MkdirAll(filepath.Join(tree, "cache", "do-not-carry-empty"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := (Carry{}).Compose(c); err != nil {
		t.Fatal(err)
	}
	receiver := workspaceReceiver(t, c, plan)
	prepared, err := (Carry{}).PrepareWorkspace(receiver, plan)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := prepared.Promote()
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		for path, want := range map[string]os.FileMode{"user-dir": 0750, "user-dir/empty": 0700, "user-dir/private.txt": 0640} {
			info, err := os.Stat(filepath.Join(binding.Path, path))
			if err != nil || info.Mode().Perm() != want {
				t.Fatalf("%s mode/presence: got %v, %v want %o", path, info, err, want)
			}
		}
		for _, path := range []string{"old-dir", "cache"} {
			if _, err = os.Stat(filepath.Join(binding.Path, path)); !os.IsNotExist(err) {
				t.Fatalf("absent or ignored directory %s was manufactured", path)
			}
		}
	}
}
