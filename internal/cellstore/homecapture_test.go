package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// useHome makes dir the home folder this process reports.
func useHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}

// fillHome writes files spread over dirs folders under root.
func fillHome(t testing.TB, root string, dirs, files int) {
	t.Helper()
	for d := 0; d < dirs; d++ {
		dir := filepath.Join(root, fmt.Sprintf("d%03d", d))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < files; f++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d.txt", f)), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// planLedger counts what the engine was asked to plan and what it planned.
type planLedger struct {
	mu    sync.Mutex
	trees []string // the tree of every capture-plan asked for
	paths int      // the paths every plan answered with, summed
}

func (l *planLedger) over(inner Transport) Transport {
	return transportFunc(func(ctx context.Context, target Target, op Op) ([]byte, error) {
		out, err := inner.Do(ctx, target, op)
		if op.Verb() == "capture-plan" {
			var plan capturePlan
			_ = json.Unmarshal(out, &plan)
			l.mu.Lock()
			l.trees = append(l.trees, target.Tree)
			l.paths += len(plan.Paths)
			l.mu.Unlock()
		}
		return out, err
	})
}

func (l *planLedger) asked(tree string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, asked := range l.trees {
		if asked == canonicalTree(tree) {
			n++
		}
	}
	return n
}

// fakePlans answers capture-plan with an empty full plan and every other verb
// with a snapshot, so the rule is seen without an engine binary.
func fakePlans() Transport {
	return transportFunc(func(_ context.Context, _ Target, op Op) ([]byte, error) {
		if op.Verb() == "capture-plan" {
			return []byte(`{"token":"t","full":true,"paths":[]}`), nil
		}
		return []byte(`{"snapshot":"s"}`), nil
	})
}

// A FOLDER THAT HOLDS THE HOME FOLDER IS NOT A PROJECT. A chat opened there
// still seals (its own folder), but the engine is never asked to plan the home,
// its parent or a filesystem root, while a project folder beside the home is
// planned exactly as before.
func TestAFolderHoldingHomeIsNeverPlanned(t *testing.T) {
	base := canonicalTree(t.TempDir())
	house := filepath.Join(base, "people", "someone")
	project := filepath.Join(base, "people", "someone-else", "api")
	for _, dir := range []string{house, project} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	useHome(t, house)
	root := filepath.VolumeName(base) + string(filepath.Separator)
	for _, tc := range []struct {
		name, workspace string
		planned         bool
	}{
		{"home", house, false},
		{"home with a trailing separator", house + string(filepath.Separator), false},
		{"parent of home", filepath.Dir(house), false},
		{"filesystem root", root, false},
		{"project beside home", project, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCell(t)
			ledger := &planLedger{}
			e := EngineFor(tc.workspace)
			e.DataRoot, e.Transport = t.TempDir(), ledger.over(fakePlans())
			if _, err := e.Seal(t.Context(), c, TurnInfo{}); err != nil {
				t.Fatal(err)
			}
			if got := ledger.asked(tc.workspace); (got > 0) != tc.planned {
				t.Fatalf("plans of %s = %d, want planned=%v (asked for %v)", tc.workspace, got, tc.planned, ledger.trees)
			}
			if !tc.planned && ledger.asked(c.Root) == 0 {
				t.Fatalf("the chat's own folder was not sealed instead (asked for %v)", ledger.trees)
			}
		})
	}
}

// The seat says once, in plain words, that a home folder's files are not kept.
func TestAHomeSeatSaysOnceThatItsFilesAreNotSaved(t *testing.T) {
	house := t.TempDir()
	useHome(t, house)
	for _, tc := range []struct {
		workspace string
		notices   int
	}{{house, 1}, {t.TempDir(), 0}} {
		var said []string
		_, err := SeatOver(executor.HostBound, newCell(t), tc.workspace, nil, nil, nil,
			WithNotices(func(s string) { said = append(said, s) }),
			func(e *Engine) { e.DataRoot, e.Transport = t.TempDir(), fakePlans() })
		if err != nil {
			t.Fatal(err)
		}
		home := 0
		for _, s := range said {
			if strings.Contains(s, "home folder") {
				home++
			}
		}
		if home != tc.notices {
			t.Fatalf("workspace %s: home notices = %d, want %d (%q)", tc.workspace, home, tc.notices, said)
		}
	}
}

// TestStartupInALargeHomeWalksNoHome is the startup gate: a seat opened with
// warm capture in a home of many files, through the real engine, finishes its
// first capture having planned none of the home's files and having written no
// plan that lists them. It counts paths and plan bytes; it never times.
func TestStartupInALargeHomeWalksNoHome(t *testing.T) {
	const dirs, files = 100, 200 // 20,000 files
	house := t.TempDir()
	fillHome(t, house, dirs, files)
	useHome(t, house)
	real := realEngine(t)
	ledger := &planLedger{}
	c := newCell(t)
	seat, err := SeatOver(executor.HostBound, c, house, nil, func(err error) {
		if err != nil {
			t.Errorf("seal: %v", err)
		}
	}, nil, WithPipeline(), WithWarmCapture(), WithNotices(func(string) {}), func(e *Engine) {
		e.Binary, e.DataRoot, e.Transport = real.Binary, real.DataRoot, ledger.over(real.Transport)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Drain(t.Context(), seat); err != nil {
		t.Fatal(err)
	}
	if head, err := Head(c); err != nil || head == nil {
		t.Fatalf("the chat was not sealed: %v", err)
	}
	if n := ledger.asked(house); n != 0 {
		t.Fatalf("the home was planned %d times", n)
	}
	if ledger.paths >= dirs*files/10 {
		t.Fatalf("startup planned %d paths for a home of %d files", ledger.paths, dirs*files)
	}
	var planBytes int64
	_ = filepath.WalkDir(real.DataRoot, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(filepath.ToSlash(path), "/capture-plans/") {
			if info, err := d.Info(); err == nil {
				planBytes += info.Size()
			}
		}
		return nil
	})
	// One plan line per home file would be at least ten bytes each.
	if planBytes >= int64(dirs*files) {
		t.Fatalf("startup wrote %d bytes of capture plans for a home of %d files", planBytes, dirs*files)
	}
}
