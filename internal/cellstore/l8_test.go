package cellstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// treeDigest lists every path under root with its mode, link target and
// content hash, skipping the engine's own directories and the turn log the
// seal appends after it snapshots.
func treeDigest(t *testing.T, root string) []string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		r, _ := filepath.Rel(root, path)
		if err != nil || r == "." || skippedByDigest(r) {
			if d != nil && d.IsDir() && skippedByDigest(r) {
				return fs.SkipDir
			}
			return err
		}
		lines = append(lines, describe(t, path, r, d))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return lines
}

func skippedByDigest(rel string) bool {
	return rel == ".git" || rel == ".furrow" || rel == TurnsPath
}

func describe(t *testing.T, path, rel string, d fs.DirEntry) string {
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case d.IsDir():
		return rel + "/ " + info.Mode().String()
	case info.Mode()&fs.ModeSymlink != 0:
		to, _ := os.Readlink(path)
		return rel + " -> " + to
	}
	raw, _ := os.ReadFile(path)
	sum := sha256.Sum256(raw)
	return rel + " " + info.Mode().String() + " " + hex.EncodeToString(sum[:8])
}

func writeTree(t *testing.T, root string, files map[string]string, mode os.FileMode) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
}

// L8 (local half): a sealed turn restores byte-for-byte, mode-for-mode, after
// the tree was damaged. The bucket-only half lands with the store in stage 1.
func TestL8SealedTurnRestoresByteIdentical(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e := realEngine(t)
		c := newCell(t)
		writeTree(t, c.Root, map[string]string{
			"src/main.go":       "package main\n",
			"src/deep/a/b/c.md": "# deep\n",
			"empty":             "",
			"bin/blob":          string([]byte{0, 1, 2, 0xff, 0xfe, 0}),
			"café/naïve.txt":    "unicode names\n",
		}, 0o644)
		writeTree(t, c.Root, map[string]string{"run.sh": "#!/bin/sh\necho hi\n"}, 0o755)
		if err := os.Symlink("src/main.go", filepath.Join(c.Root, "link")); err != nil {
			t.Fatal(err)
		}
		transcript, _ := c.Path(cell.TranscriptPath)
		if err := os.WriteFile(transcript, []byte(`{"role":"user"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		sealed, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("bash", "")}})
		if err != nil {
			t.Fatal(err)
		}
		want := treeDigest(t, c.Root)

		damage(t, c.Root)
		if got := treeDigest(t, c.Root); strings.Join(got, "\n") == strings.Join(want, "\n") {
			t.Fatal("damage changed nothing; the test proves nothing")
		}
		if _, err := e.do(context.Background(), c, e.cellDir(c), restoreOp{Snapshot: sealed.Turn.ID}); err != nil {
			t.Fatal(err)
		}
		if got := treeDigest(t, c.Root); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("restored tree differs.\nwant:\n%s\ngot:\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"))
		}
	})
}

func damage(t *testing.T, root string) {
	t.Helper()
	for _, p := range []string{"src", "empty", "link", "café"} {
		if err := os.RemoveAll(filepath.Join(root, p)); err != nil {
			t.Fatal(err)
		}
	}
	writeTree(t, root, map[string]string{"stray.txt": "not sealed", "run.sh": "changed"}, 0o600)
	_ = os.Chmod(filepath.Join(root, "bin/blob"), 0o600)
}
