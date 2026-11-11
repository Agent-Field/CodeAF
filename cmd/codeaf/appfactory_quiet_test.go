package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// A closed factory writes nothing more. These are the steps a read-loops test
// watches its factory's folder for, after closing the backend and before the
// temporary home is removed: short, so a quiet factory costs at most their sum.
const (
	quietSteps = 4
	quietStep  = 50 * time.Millisecond
)

// factoryFile is what a late write is known by in a failure message.
type factoryFile struct {
	size int64
	mod  time.Time
}

// snapFactory reads every file under root, by its path relative to root.
func snapFactory(root string) map[string]factoryFile {
	files := map[string]factoryFile{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			rel, _ := filepath.Rel(root, path)
			files[rel] = factoryFile{size: info.Size(), mod: info.ModTime()}
		}
		return nil
	})
	return files
}

// lateFactoryWrites watches root for up to steps x step and answers each file
// that appeared or changed, as "path (size N bytes, mtime T)". It stops at the
// first step that sees a change.
func lateFactoryWrites(root string, steps int, step time.Duration) []string {
	before := snapFactory(root)
	for range steps {
		time.Sleep(step)
		var late []string
		for rel, now := range snapFactory(root) {
			if was, ok := before[rel]; !ok || was != now {
				late = append(late, fmt.Sprintf("%s (size %d bytes, mtime %s)", rel, now.size, now.mod.Format(time.RFC3339Nano)))
			}
		}
		if len(late) > 0 {
			sort.Strings(late)
			return late
		}
	}
	return nil
}

// closeQuietly closes a backend before the test's temporary home is removed,
// and fails the test naming every file its factory wrote after the close.
// Registered after the home exists, it runs before that home's removal.
func closeQuietly(t *testing.T, b *appBackend) {
	t.Helper()
	t.Cleanup(func() {
		var root string
		b.local.mu.Lock()
		if h := b.local.host; h != nil {
			root = h.floor.Root()
		}
		b.local.mu.Unlock()
		_ = b.Close()
		if root == "" {
			return
		}
		if late := lateFactoryWrites(root, quietSteps, quietStep); len(late) > 0 {
			t.Errorf("%s: the factory under %s wrote after the backend closed:\n  %s", t.Name(), root, strings.Join(late, "\n  "))
		}
	})
}

// The watch names a file written after it began, with its size and time, and
// says nothing of a folder that stays still.
func TestALateFactoryWriteIsNamedWithItsSizeAndTime(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "chats"), 0o755); err != nil {
		t.Fatal(err)
	}
	if late := lateFactoryWrites(root, quietSteps, quietStep); late != nil {
		t.Fatalf("a still folder reported late writes: %v", late)
	}
	go func() {
		time.Sleep(quietStep / 2)
		_ = os.WriteFile(filepath.Join(root, "chats", "1.json"), []byte("late"), 0o600)
	}()
	late := lateFactoryWrites(root, quietSteps, quietStep)
	if len(late) != 1 || !strings.HasPrefix(late[0], filepath.Join("chats", "1.json")+" (size 4 bytes, mtime ") {
		t.Fatalf("late write not named with its path, size and time: %v", late)
	}
}
