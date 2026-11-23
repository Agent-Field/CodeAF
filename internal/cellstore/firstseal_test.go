package cellstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// TestC9SeatScreensAnEmptyChangesTurn proves C9 through the real seat and
// engine: an unread working-tree secret never reaches even the first pack.
// Both a model-only answer and a call with no file changes use this door.
func TestC9SeatScreensAnEmptyChangesTurn(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		for _, path := range []string{".env", "config/settings.py"} {
			for _, door := range []string{"model reply", "empty-changes call", "shell reply"} {
				t.Run(path+"/"+door, func(t *testing.T) {

					r := newRig(t)
					t.Setenv("CODEAF_HOME", r.guard.Home)
					e := realEngine(t)
					e.DataRoot = (Engine{}).Root()
					e.Workspace, e.Guard = r.tree, r.guard
					secret, body := "c9-not-a-real-secret", "DEMO_TOKEN=c9-not-a-real-secret\n"
					if path != ".env" {
						secret, body = fakeKey, "API_KEY = '"+fakeKey+"'\n"
					}
					r.write(".gitignore", ".env\n")
					r.write(".git/HEAD", "ref: refs/heads/main\n")
					r.write(".git/config", "[core]\n\trepositoryformatversion = 0\n\tbare = false\n")
					for _, dir := range []string{".git/objects", ".git/refs"} {
						if err := os.MkdirAll(filepath.Join(r.tree, dir), 0o700); err != nil {
							t.Fatal(err)
						}
					}
					ignored := exec.Command("git", "check-ignore", ".env") //codeaf:plumbing read-only inspection of the isolated fixture.
					ignored.Dir = r.tree
					if out, err := ignored.Output(); err != nil || strings.TrimSpace(string(out)) != ".env" {
						t.Fatalf(".env is not gitignored: %q, %v", out, err)
					}
					r.write(path, body)
					r.write("README.md", "C9_SAFE_CAPTURE_MARKER\n")
					seat, err := SeatOver(executor.HostBound, r.cell, r.tree, nil, func(err error) {
						if err != nil {
							t.Errorf("seal: %v", err)
						}
					}, nil, func(at *Engine) { *at = e })
					if err != nil {
						t.Fatal(err)
					}
					ctx := context.Background()
					if door == "empty-changes call" {
						if err := seat.Around(ctx, executor.Call{Tool: "read", Changed: []string{}}, func() ([]byte, bool) { return []byte("ok"), false }); err != nil {
							t.Fatal(err)
						}
					}
					if door == "shell reply" {
						if _, err := seat.In(r.tree).Exec(ctx, executor.ExecRequest{Argv: []string{testShell(t), "-c", "echo ok"}}, nil); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(filepath.Join(r.cell.Root, cell.TranscriptPath), []byte("assistant: ok\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					executor.Settle(ctx, seat)
					if head, err := Head(r.cell); err != nil || head == nil {
						t.Fatalf("no sealed turn: %v", err)
					}
					assertC9Screened(t, e, r, path, secret)
					if path == ".env" {
						res, err := seat.In(r.tree).Exec(ctx, executor.ExecRequest{Argv: []string{testShell(t), "-c", `test "$DEMO_TOKEN" = c9-not-a-real-secret`}}, nil)
						if err != nil || res.Exit != 0 {
							t.Fatalf("tools lost the vaulted variable: %+v, %v", res, err)
						}
					}
				})
			}
		}
	})
}

// TestC9SealScreensBeyondItsChangedHint proves C9 for index resets after a
// restore, arrival or rematerialization, and for the full-tree fallback.
func TestC9SealScreensBeyondItsChangedHint(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		for _, door := range []string{"restore", "evicted workspace", "arrival", "new workspace path", "large changed list", "policy change", "new hardlink", "workspace root hint"} {
			t.Run(door, func(t *testing.T) {
				r := newRig(t)
				e := realEngine(t)
				e.Workspace, e.Guard = r.tree, r.guard
				r.write("README.md", "C9_SAFE_CAPTURE_MARKER\n")
				ctx := context.Background()
				first, err := e.Seal(ctx, r.cell, TurnInfo{})
				if err != nil {
					t.Fatal(err)
				}
				switch door {
				case "evicted workspace":
					if err := os.RemoveAll(r.tree); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(r.tree, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := e.Restore(ctx, r.cell, first.Turn.ID, nil); err != nil {
						t.Fatal(err)
					}
				case "restore":
					if err := e.Restore(ctx, r.cell, first.Turn.ID, nil); err != nil {
						t.Fatal(err)
					}
				case "arrival":
					if err := e.Sync(SyncKeys{}, "c9").Materialize(ctx, r.cell, first.Turn.ID); err != nil {
						t.Fatal(err)
					}
				case "new workspace path":
					r.tree = t.TempDir()
					e.Workspace = r.tree
					r.write("README.md", "C9_SAFE_CAPTURE_MARKER\n")
				}
				r.write(".env", "DEMO_TOKEN=c9-not-a-real-secret\n")
				changed := []string{}
				if door == "large changed list" {
					changed = make([]string, maxChangedArgs+1)
				}
				if door == "policy change" {
					r.write(policyName, "# the capture policy changed\n")
					changed = []string{policyName}
				}
				if door == "new hardlink" {
					if err := os.Link(filepath.Join(r.tree, "README.md"), filepath.Join(r.tree, "linked-readme")); err != nil {
						t.Fatal(err)
					}
					changed = []string{"linked-readme"}
				}
				if door == "workspace root hint" {
					changed = []string{"."}
				}
				if _, err := e.Seal(ctx, r.cell, TurnInfo{Changed: changed}); err != nil {
					t.Fatal(err)
				}
				assertC9Screened(t, e, r, ".env", "c9-not-a-real-secret")
			})
		}
	})
}

func assertC9Screened(t *testing.T, e Engine, r *guardRig, path, secret string) {
	t.Helper()
	for _, dir := range []string{e.StoreDir(r.cell), stateDir(r.cell)} {
		if hits := grepStore(t, dir, secret); len(hits) != 0 {
			t.Fatalf("working-tree secret reached raw saved files: %v", hits)
		}
	}
	if len(grepStore(t, e.StoreDir(r.cell), "C9_SAFE_CAPTURE_MARKER")) == 0 {
		t.Fatal("the fixture did not capture its safe file in plaintext")
	}
	_, withheld, err := e.Withheld(r.cell)
	if err != nil || !slices.Contains(withheld, path) {
		t.Fatalf("withheld = %v, %v; missing %s", withheld, err, path)
	}
	// Logged once, never said in the chat ([Guard.Kept]).
	if len(r.kept) != 1 || !strings.Contains(r.kept[0], path) || len(r.notice) != 0 {
		t.Fatalf("withholding was not logged once: log %v, notices %v", r.kept, r.notice)
	}
}
