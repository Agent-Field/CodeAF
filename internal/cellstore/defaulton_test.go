package cellstore

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/keys"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestD1SharedObjectsKeepIndependentHeads proves D1 through both real engine
// transports: chats sharing a folder must never adopt one another's head.
func TestD1SharedObjectsKeepIndependentHeads(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		e := realEngine(t)
		e.Workspace = r.tree
		e.Guard = r.guard
		r.write("work.txt", "first chat\n")
		a, err := e.Seal(t.Context(), r.cell, TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		second := newCell(t)
		r.write("work.txt", "second chat\n")
		b, err := e.Seal(t.Context(), second, TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		if e.StoreDir(r.cell) != e.StoreDir(second) || e.LocalDir(r.cell) == e.LocalDir(second) {
			t.Fatal("stores are not shared or private state collided")
		}
		if e.target(r.cell).Timeline != r.cell.ID || e.target(second).Timeline != second.ID {
			t.Fatal("missing timeline routing")
		}
		if dirty, err := e.Dirty(t.Context(), r.cell); err != nil || !dirty {
			t.Fatalf("first chat adopted another timeline head: dirty=%v %v", dirty, err)
		}
		if dirty, err := e.Dirty(t.Context(), second); err != nil || dirty {
			t.Fatalf("second chat lost its own head: dirty=%v %v", dirty, err)
		}
		if err := e.Restore(t.Context(), r.cell, a.Turn.ID, nil); err != nil {
			t.Fatal(err)
		}
		if raw, _ := os.ReadFile(filepath.Join(r.tree, "work.txt")); string(raw) != "first chat\n" {
			t.Fatalf("first chat restored %q", raw)
		}
		if err := e.Restore(t.Context(), second, b.Turn.ID, nil); err != nil {
			t.Fatal(err)
		}
		if raw, _ := os.ReadFile(filepath.Join(r.tree, "work.txt")); string(raw) != "second chat\n" {
			t.Fatalf("second chat restored %q", raw)
		}
	})
}

// TestD1OldStoresStayInPlace proves D1 without migration: a store made with the
// old path identity remains readable and receives every subsequent seal.
func TestD1OldStoresStayInPlace(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		c := newCell(t)
		e := realEngine(t)
		if err := os.WriteFile(filepath.Join(c.Root, "old.txt"), []byte("old capture"), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := e.transport().Do(t.Context(), Target{Tree: c.Root, DataDir: e.LocalDir(c)}, sealOp{Turn: "old"})
		if err != nil {
			t.Fatal(err)
		}
		old, err := parseSnapshot(out)
		if err != nil {
			t.Fatal(err)
		}
		target := e.target(c)
		if target.DataDir != e.LocalDir(c) || target.Timeline != "" {
			t.Fatalf("old store moved: %+v", target)
		}
		if _, err := e.Seal(t.Context(), c, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		if err := e.Restore(t.Context(), c, old, nil); err != nil {
			t.Fatal(err)
		}
		if raw, _ := os.ReadFile(filepath.Join(c.Root, "old.txt")); string(raw) != "old capture" {
			t.Fatalf("old snapshot lost: %q", raw)
		}
	})
}

func fixtureGit(t *testing.T, tree string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //codeaf:plumbing Git prepares a local test fixture only.
	cmd.Dir = tree
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

// TestD2IgnoredOutputStaysAndIgnoredSecretsAreScreened proves D2 and D4/C9:
// ignored directories never enter a screen or capture, tracked exceptions move,
// and ignored secret files in ordinary directories still reach the vault path.
func TestD2IgnoredOutputStaysAndIgnoredSecretsAreScreened(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		e := realEngine(t)
		e.Workspace = r.tree
		e.Guard = r.guard
		fixtureGit(t, r.tree, "init", "-q")
		r.write("dist/tracked.txt", "tracked exception\n")
		fixtureGit(t, r.tree, "add", "dist/tracked.txt")
		r.write(".gitignore", "dist/\nnode_modules/\n.env\nconfig/.env.local\n*.pem\n*.log\n")
		r.write("dist/output.txt", "D2_OUTPUT_STAYS_HOME\n")
		r.write("node_modules/.env", "D2_NOT_SCREENED=inside-ignored-directory\n")
		r.write(".env", "D2_ROOT=local-secret\n")
		r.write("config/.env.local", "D2_CONFIG=local-secret\n")
		r.write("key.pem", "-----BEGIN PRIVATE KEY-----\nD2_PRIVATE_MARKER\n")
		r.write("build.log", "D2_LOG_STAYS_HOME\n")
		sealed, err := e.Seal(t.Context(), r.cell, TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(filepath.Join(e.policyDir(r.cell), policyName))
		if !strings.Contains(string(raw), "gitignore\n") {
			t.Fatal("no composed gitignore directive")
		}
		_, held, err := e.Withheld(r.cell)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{".env", "config/.env.local", "key.pem"} {
			if !slices.Contains(held, path) {
				t.Errorf("secret %s not screened: %v", path, held)
			}
		}
		if slices.Contains(held, "node_modules/.env") {
			t.Fatal("screen entered a wholly ignored directory")
		}
		for _, marker := range []string{"D2_OUTPUT_STAYS_HOME", "D2_NOT_SCREENED", "D2_LOG_STAYS_HOME", "D2_PRIVATE_MARKER"} {
			if len(grepStore(t, e.StoreDir(r.cell), marker)) > 0 {
				t.Errorf("ignored or secret data reached engine: %s", marker)
			}
		}
		inv, err := inventory.Open(r.cell.Root)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, w := range inv.Snapshot().Withheld {
			if w.Reason == reasonGitIgnored {
				found[w.Path] = true
			}
		}
		if !found["node_modules/"] || !found["build.log"] {
			t.Fatalf("engine omissions absent from arrival record: %v", found)
		}
		target := e.target(newCell(t))
		target.Tree = t.TempDir()
		target.CellDir = t.TempDir()
		if _, err := e.transport().Do(context.Background(), target, materializeOp{Head: sealed.Turn.ID}); err != nil {
			t.Fatal(err)
		}
		if raw, err := os.ReadFile(filepath.Join(target.Tree, "dist/tracked.txt")); err != nil || string(raw) != "tracked exception\n" {
			t.Fatalf("tracked ignored file lost: %q %v", raw, err)
		}
		for _, path := range []string{"dist/output.txt", "node_modules/.env", "build.log", ".env", "key.pem"} {
			if _, err := os.Stat(filepath.Join(target.Tree, path)); !os.IsNotExist(err) {
				t.Errorf("unexpected arrival %s: %v", path, err)
			}
		}
	})
}

// TestD4DeltaHintsKeepFullSecretScreen proves D3/D4/C9 with the production
// metadata index: an engine hint must never narrow the mandatory secret screen,
// including files changed outside a tool's declared paths and after a restore.
func TestD4DeltaHintsKeepFullSecretScreen(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		real := realEngine(t)
		e := EngineFor(r.tree)
		e.Binary, e.Transport, e.DataRoot = real.Binary, real.Transport, real.DataRoot
		e.Guard.Home, e.Guard.Notify = r.guard.Home, r.guard.Notify
		r.write("safe.txt", "D4_SAFE_CAPTURE\n")
		first, err := e.Seal(t.Context(), r.cell, TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		for _, phase := range []string{"ordinary delta", "after restore"} {
			t.Run(phase, func(t *testing.T) {
				if phase == "after restore" {
					if err := e.Restore(t.Context(), r.cell, first.Turn.ID, nil); err != nil {
						t.Fatal(err)
					}
				}
				r.write("config/.env.local", "D4_LOCAL=value\n")
				r.write("src/config.py", "key = '"+fakeKey+"'\n")
				r.write("safe.txt", phase)
				if _, err := e.Seal(t.Context(), r.cell, TurnInfo{Changed: []string{"safe.txt"}}); err != nil {
					t.Fatal(err)
				}
				_, held, err := e.Withheld(r.cell)
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"config/.env.local", "src/config.py"} {
					if !slices.Contains(held, name) {
						t.Errorf("secret missing from full screen: %s", name)
					}
				}
				if len(grepStore(t, e.StoreDir(r.cell), fakeKey)) != 0 {
					t.Fatal("secret entered indexed engine capture")
				}
			})
		}
	})
}

// TestD4GCKeepsIgnoreRulesAndIgnoredOutput proves D2/D4/C25: collection may
// remove captured files, but must retain ignore rules until restoration can
// make its safety capture without importing the output left on this computer.
func TestD4GCKeepsIgnoreRulesAndIgnoredOutput(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		e := realEngine(t)
		e.Workspace = r.tree
		e.Guard = r.guard
		fixtureGit(t, r.tree, "init", "-q")
		r.write(".gitignore", "dist/\n")
		r.write("dist/out.bin", "D4_IGNORED_OUTPUT_STAYS_HOME")
		r.write("kept.txt", "captured work")
		snap, paths, err := e.CaptureForGC(t.Context(), r.cell)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(paths, ".gitignore") || slices.Contains(paths, "dist") {
			t.Fatalf("collection would remove output or ignore rules: %v", paths)
		}
		if !slices.Contains(paths, "kept.txt") {
			t.Fatalf("fixture did not collect captured work: %v", paths)
		}
		for _, path := range paths {
			if err := os.RemoveAll(filepath.Join(r.tree, path)); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.Restore(t.Context(), r.cell, snap, nil); err != nil {
			t.Fatal(err)
		}
		if len(grepStore(t, e.StoreDir(r.cell), "D4_IGNORED_OUTPUT_STAYS_HOME")) != 0 {
			t.Fatal("undo capture imported ignored output")
		}
	})
}

// TestD4DeathBeforeCaptureKeepsExternalOutput proves D4/L9 through the real
// recovery door: a completed command with no live recorder is captured with its
// receipt, while an incomplete external intent is retained and never rerun.
func TestD4DeathBeforeCaptureKeepsExternalOutput(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		e := realEngine(t)
		e.Workspace = r.tree
		e.Guard = r.guard
		wal, _, err := OpenWAL(e.WALPath(r.cell))
		if err != nil {
			t.Fatal(err)
		}
		intent := Intent{V: walV, Tool: "bash", ArgsHash: "completed", Started: 10, SideEffect: "external"}
		if err := wal.Begin(intent); err != nil {
			t.Fatal(err)
		}
		done := executed(intent, executor.ExecResult{Stdout: []byte("D4_COMMAND_RAN_ONCE"), Exit: 0}, nil, 20)
		if err := wal.Finish(intent, done); err != nil {
			t.Fatal(err)
		}
		incomplete := Intent{V: walV, Tool: "bash", ArgsHash: "unfinished", Started: 30, SideEffect: "external"}
		if err := wal.Begin(incomplete); err != nil {
			t.Fatal(err)
		}
		r.write("result.txt", "D4_COMPLETED_WORK")
		if _, err := e.Capture(t.Context(), r.cell); err != nil {
			t.Fatal(err)
		}
		head, err := Head(r.cell)
		if err != nil || head == nil || len(head.Receipt.Calls) != 1 {
			t.Fatalf("recovered receipt: %+v %v", head, err)
		}
		if head.Receipt.Calls[0].Tool != "bash" || head.Receipt.Calls[0].StdoutHash != hashHex([]byte("D4_COMMAND_RAN_ONCE")) {
			t.Fatalf("lost completed external output: %+v", head.Receipt)
		}
		if len(grepStore(t, e.StoreDir(r.cell), "D4_COMMAND_RAN_ONCE")) == 0 {
			t.Fatal("completed output did not enter the capture")
		}
		_, recovered, err := OpenWAL(e.WALPath(r.cell))
		if err != nil || len(recovered.Completed) != 0 || len(recovered.Incomplete) != 1 {
			t.Fatalf("recovery reran or lost an intent: %+v %v", recovered, err)
		}
	})
}

// TestD2IgnoredInstallFoldersKeepTheirRebuildInstructions proves D2: Git
// pruning does not erase the old lockfile setup instructions, and an ignored
// untracked lockfile cannot license a rebuild on the receiving computer.
func TestD2IgnoredInstallFoldersKeepTheirRebuildInstructions(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		for _, ignoreLock := range []bool{false, true} {
			t.Run(fmt.Sprint(ignoreLock), func(t *testing.T) {
				r := newRig(t)
				e := realEngine(t)
				e.Workspace = r.tree
				e.Guard = r.guard
				fixtureGit(t, r.tree, "init", "-q")
				rules := "node_modules/\n"
				if ignoreLock {
					rules += "package-lock.json\n"
				}
				r.write(".gitignore", rules)
				r.write("package-lock.json", "{}")
				r.write("node_modules/.env", "D2_IGNORED_INSTALL_SECRET=value\n")
				if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
					t.Fatal(err)
				}
				store, err := inventory.Open(r.cell.Root)
				if err != nil {
					t.Fatal(err)
				}
				rebuilt := false
				for _, entry := range store.Snapshot().Withheld {
					if entry.Path == "node_modules" && entry.Lock == "package-lock.json" {
						rebuilt = true
					}
				}
				if rebuilt == ignoreLock {
					t.Fatalf("rebuild instruction = %v with ignored lock = %v: %+v", rebuilt, ignoreLock, store.Snapshot().Withheld)
				}
				_, held, err := e.Withheld(r.cell)
				if err != nil {
					t.Fatal(err)
				}
				if slices.Contains(held, "node_modules/.env") {
					t.Fatal("rebuild discovery entered the ignored install folder")
				}
			})
		}
	})
}

// TestD2UnreadableIgnoreRulesNeverFallBackToWalking proves D2: losing Git
// cannot turn an ignored directory's secrets into vault contents. A failed save
// is explicit and leaves the entire directory on this computer.
func TestD2UnreadableIgnoreRulesNeverFallBackToWalking(t *testing.T) {
	r := newRig(t)
	fixtureGit(t, r.tree, "init", "-q")
	r.write(".gitignore", "dist/\n")
	r.write("dist/.env", "D2_STAYS_INSIDE_IGNORED_DIRECTORY=value\n")
	t.Setenv("PATH", t.TempDir())
	err := r.guard.Screen(r.cell, r.tree, r.state(), nil)
	if err == nil || !strings.Contains(err.Error(), ignoredReadFailure) {
		t.Fatalf("unreadable ignore rules did not stop saving: %v", err)
	}
	if len(r.notice) != 0 || keys.Exists(r.guard.Home) {
		t.Fatal("failed ignore lookup screened or vaulted an ignored file")
	}
}

// TestD4AtomicReplacementKeepsItsBytesAndItsSecretScreen proves D3/D4/C9:
// replacing a file with the same size and timestamp still changes its identity.
// A clean replacement must be captured, and a secret replacement must be read
// by the guard rather than borrowed from its old clean-file cache.
func TestD4AtomicReplacementKeepsItsBytesAndItsSecretScreen(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		for _, secret := range []bool{false, true} {
			t.Run(fmt.Sprint(secret), func(t *testing.T) {
				r := newRig(t)
				real := realEngine(t)
				e := EngineFor(r.tree)
				e.Binary, e.Transport, e.DataRoot = real.Binary, real.Transport, real.DataRoot
				e.Guard.Home, e.Guard.Notify = r.guard.Home, r.guard.Notify
				old, next := "before", "after!"
				if secret {
					old = strings.Repeat("a", len(fakeKey))
					next = fakeKey
				}
				r.write("source.txt", old)
				if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(r.tree, "source.txt")
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				replacement := filepath.Join(r.tree, "replacement")
				if err := os.WriteFile(replacement, []byte(next), info.Mode()); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
				sealed, err := e.Seal(t.Context(), r.cell, TurnInfo{Changed: []string{}})
				if err != nil {
					t.Fatal(err)
				}
				if secret {
					_, held, err := e.Withheld(r.cell)
					if err != nil || !slices.Contains(held, "source.txt") {
						t.Fatalf("replacement secret was not screened: %v %v", held, err)
					}
					if len(grepStore(t, e.StoreDir(r.cell), fakeKey)) != 0 {
						t.Fatal("replacement secret entered the store")
					}
				} else {
					r.write("source.txt", "changed after capture")
					if err := e.Restore(t.Context(), r.cell, sealed.Turn.ID, nil); err != nil {
						t.Fatal(err)
					}
					if raw, _ := os.ReadFile(path); string(raw) != next {
						t.Fatalf("atomic replacement lost: %q", raw)
					}
				}
			})
		}
	})
}
