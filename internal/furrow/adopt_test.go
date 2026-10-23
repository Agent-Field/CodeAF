package furrow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// A store that predates the state-root rule is carried into the default
// profile once: the timeline and forks a person sealed while the store sat in
// the OS data root must not be orphaned by the move. The rename is recorded,
// not assumed — this test pins WHICH directories move where, because the whole
// fix is that the store's home changes.
func TestAdoptMovesALegacyStoreIntoTheDefaultProfile(t *testing.T) {
	base := t.TempDir()
	legacy := filepath.Join(platformDataRoot(base), "store-v1")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "timeline.db"), []byte("sealed work"), 0o600); err != nil {
		t.Fatal(err)
	}
	current := home.Join("furrow", "store-v1")
	var moved [][2]string
	var links [][2]string
	var lines []string
	adopt(base, func(format string, args ...any) { lines = append(lines, format) }, adoptOps{
		lstat:    os.Lstat,
		mkdirAll: os.MkdirAll,
		rename: func(from, to string) error {
			moved = append(moved, [2]string{from, to})
			return os.Rename(from, to)
		},
		symlink: func(target, name string) error {
			links = append(links, [2]string{target, name})
			return os.Symlink(target, name)
		},
	})
	if len(moved) != 1 || moved[0] != [2]string{legacy, current} {
		t.Fatalf("renames = %v; want exactly %s -> %s", moved, legacy, current)
	}
	if _, err := os.Stat(filepath.Join(current, "timeline.db")); err != nil {
		t.Fatalf("the store did not land under the state root: %v", err)
	}
	// AN OLDER BINARY MUST STILL RESOLVE THE OLD ROOT TO THE SAME STORE: the
	// move is the adoption, and the link left at the old root is its
	// compatibility seam. A pre-fix resident still running would otherwise
	// open a fresh empty store where the old one sat, and the timelines would
	// be split between two stores nobody sees both of.
	target, err := os.Readlink(legacy)
	if err != nil {
		t.Fatalf("no link left at the old root: %v", err)
	}
	if resolved := filepath.Clean(filepath.Join(filepath.Dir(legacy), target)); resolved != current {
		t.Fatalf("the link at %s resolves to %s; want the moved store %s", legacy, resolved, current)
	}
	if len(links) != 1 || links[0][1] != legacy {
		t.Fatalf("links = %v; want exactly one link at %s", links, legacy)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "moved furrow's snapshot store") {
		t.Fatalf("log lines = %v; want one line saying the store moved", lines)
	}
	// A second pass finds the current store and leaves everything alone — the
	// renamed store and the link both stay exactly as they are.
	moved, links = nil, nil
	adopt(base, nil, adoptOps{lstat: os.Lstat, mkdirAll: os.MkdirAll, rename: func(from, to string) error {
		moved = append(moved, [2]string{from, to})
		return nil
	}, symlink: func(target, name string) error {
		links = append(links, [2]string{target, name})
		return nil
	}})
	if len(moved) != 0 || len(links) != 0 {
		t.Fatalf("second pass renamed %v and linked %v; a live store must win", moved, links)
	}
	if _, err := os.Lstat(legacy); err != nil {
		t.Fatalf("the link at the old root was disturbed: %v", err)
	}
}

// With nothing at the OS data root there is nothing to adopt, and a legacy
// store that is a symlink is left alone — the same caution home.Adopt applies
// to its own legacy folder.
func TestAdoptLeavesNothingAndNoSymlinksAlone(t *testing.T) {
	base := t.TempDir()
	legacy := filepath.Join(platformDataRoot(base), "store-v1")
	touched := false
	noOps := adoptOps{
		lstat:    os.Lstat,
		mkdirAll: os.MkdirAll,
		rename:   func(from, to string) error { touched = true; return nil },
		symlink:  func(target, name string) error { touched = true; return nil },
	}
	adopt(base, nil, noOps)
	if touched {
		t.Fatal("adopted a store that was not there")
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(home.Join("furrow", "store-v1"), legacy); err != nil {
		t.Fatal(err)
	}
	touched = false
	adopt(base, nil, noOps)
	if touched {
		t.Fatal("a symlink was adopted as a store")
	}
}
