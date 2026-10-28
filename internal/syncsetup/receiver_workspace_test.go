package syncsetup

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

// W5 keeps unavailable sender paths out of the receiver's engine target.
func TestReceiverWorkspacePathsW5(t *testing.T) {
	for _, recorded := range []string{
		filepath.Join(t.TempDir(), "missing-project"),
		`C:\Users\sender\missing-codeaf-e6b-project`,
		"/Users/sender/missing-codeaf-e6b-project",
	} {
		t.Run(recorded, func(t *testing.T) {
			at, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
			if err != nil {
				t.Fatal(err)
			}
			if err := session.SaveMeta(at.Root, session.Meta{ID: at.ID, Workspace: recorded}); err != nil {
				t.Fatal(err)
			}
			c := (&Sync{}).Continuer(cellstore.Engine{DataRoot: t.TempDir()}, TakeOptions{
				RootFor: func(string) string { return at.Root },
			})
			target := c.eng.Sync(cellstore.SyncKeys{}, "").Target(at)
			root, err := filepath.EvalSymlinks(at.Root)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(root, "work")
			if target.Tree != want {
				t.Fatalf("W5 receiver tree %q, want %q for sender path %q", target.Tree, want, recorded)
			}
		})
	}
}
