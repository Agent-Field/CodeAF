package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSweepAbandonedFurrowFolders(t *testing.T) {
	for _, tc := range []struct {
		name                string
		moving, ready, open bool
	}{
		{name: "session"}, {name: "open", open: true},
		{name: "moving-ready", moving: true, ready: true},
		{name: "moving-incomplete", moving: true},
		{name: "moving-open", moving: true, ready: true, open: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			root := t.TempDir()
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			bucket := "project"
			if tc.moving {
				bucket = filepath.Join(bucket, ".moving")
			}
			dir := newSweptSession(t, root, bucket, "session", Meta{ID: "session", Workspace: "/project", LastUserAt: now})
			if tc.ready {
				writeFile(t, filepath.Join(dir, ".cell", "copy-ready"), "ready")
			}
			trees := filepath.Join(dir, "trees")
			old := now.Add(-8 * 24 * time.Hour)
			removed := []string{".1.furrow-fork-123-0", ".task.name.furrow-fork-123-1", "2.furrow-forks"}
			kept := []string{".1.furrow-fork-x-0", ".1.furrow-fork-1-0-extra", ".furrow-fork-1-0", "x.furrow-forks", "1.furrow-forks", "1", "notes", ".3.furrow-fork-1-0", ".4.furrow-fork-1-0", ".5.furrow-fork-1-0", "3.furrow-forks", "4.furrow-forks", "5.furrow-forks"}
			for _, name := range append(append([]string{}, removed...), kept...) {
				path := filepath.Join(trees, name)
				writeFile(t, filepath.Join(path, "data"), "content")
				stampTree(t, path, old)
			}
			stampTree(t, filepath.Join(trees, ".3.furrow-fork-1-0"), now.Add(-7*24*time.Hour))
			stampTree(t, filepath.Join(trees, ".4.furrow-fork-1-0"), now.Add(-time.Hour))
			writeFile(t, filepath.Join(trees, ".5.furrow-fork-1-0", "recent"), "new")
			stampTree(t, filepath.Join(trees, ".5.furrow-fork-1-0", "recent"), now)
			stampTree(t, filepath.Join(trees, "3.furrow-forks"), now.Add(-7*24*time.Hour))
			stampTree(t, filepath.Join(trees, "4.furrow-forks"), now.Add(-time.Hour))
			writeFile(t, filepath.Join(trees, "5.furrow-forks", "recent"), "new")
			stampTree(t, filepath.Join(trees, "5.furrow-forks", "recent"), now)
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, "data"), "keep")
			stampTree(t, outside, old)
			link := filepath.Join(trees, ".6.furrow-fork-1-0")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			kept = append(kept, filepath.Base(link))
			writeFile(t, filepath.Join(dir, placeLogs, "old.log"), "keep in moving")
			stampTree(t, filepath.Join(dir, placeLogs), old)
			if tc.open {
				held, _, err := openSessionFile(filepath.Join(dir, placeTranscript), dir, "test-model", "")
				if err != nil {
					t.Fatal(err)
				}
				defer held.Close()
			}
			var notes []string
			SweepPlaces(root, now, func(line string) { notes = append(notes, line) })
			wantRemove := !tc.open && (!tc.moving || tc.ready)
			for _, name := range removed {
				_, err := os.Lstat(filepath.Join(trees, name))
				if wantRemove {
					if !os.IsNotExist(err) {
						t.Errorf("abandoned folder %s survived: %v", name, err)
					}
					count := 0
					for _, line := range notes {
						if strings.Contains(line, filepath.Join(trees, name)) {
							count++
						}
					}
					if count != 1 {
						t.Errorf("removal of %s got %d notes: %v", name, count, notes)
					}
				} else if err != nil {
					t.Errorf("protected folder %s removed: %v", name, err)
				}
			}
			for _, name := range kept {
				if _, err := os.Lstat(filepath.Join(trees, name)); err != nil {
					t.Errorf("kept folder %s removed: %v", name, err)
				}
			}
			if _, err := os.Stat(filepath.Join(outside, "data")); err != nil {
				t.Fatal(err)
			}
			if tc.moving {
				if _, err := os.Stat(filepath.Join(dir, placeLogs, "old.log")); err != nil {
					t.Fatalf("moving logs touched: %v", err)
				}
			}
		})
	}
}

func TestSweepForkFoldersDoesNotFollowTreesSymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	dir := newSweptSession(t, root, "project", "session", Meta{ID: "session", Workspace: "/project", LastUserAt: now})
	outside := t.TempDir()
	stale := filepath.Join(outside, ".1.furrow-fork-1-0")
	writeFile(t, filepath.Join(stale, "data"), "keep")
	stampTree(t, stale, now.Add(-8*24*time.Hour))
	if err := os.Symlink(outside, filepath.Join(dir, "trees")); err != nil {
		t.Fatal(err)
	}
	SweepPlaces(root, now, func(line string) { t.Errorf("symlink sweep: %s", line) })
	if _, err := os.Stat(filepath.Join(stale, "data")); err != nil {
		t.Fatalf("followed trees symlink: %v", err)
	}
}

func TestSweepForkFoldersCancellationStopsAfterFirstRemoval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	dir := newSweptSession(t, root, "project", "session", Meta{ID: "session", Workspace: "/project", LastUserAt: now})
	first := filepath.Join(dir, "trees", ".1.furrow-fork-1-0")
	second := filepath.Join(dir, "trees", ".2.furrow-fork-1-0")
	for _, path := range []string{first, second} {
		writeFile(t, filepath.Join(path, "data"), "keep until swept")
		stampTree(t, path, now.Add(-8*24*time.Hour))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	SweepPlacesContext(ctx, root, now, func(string) { cancel() })
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("first removal did not happen: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("removal continued after cancellation: %v", err)
	}
}
