package fsreplace

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Int-fix contract A1–A2: both state-open APIs retain the old generation while
// relative and long Unicode destination names publish the new generation.
func TestIntFixContractAReplacementKeepsHeldGeneration(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, kind := range []string{"relative", "long-unicode"} {
		for _, api := range []string{"Open", "OpenFile"} {
			t.Run(kind+"/"+api, func(t *testing.T) {
				dir := filepath.Join(kind, api)
				if kind == "long-unicode" {
					dir = filepath.Join(t.TempDir(), strings.Repeat("segment-", 15), strings.Repeat("segment-", 15), "state-😀")
				}
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				path, next := filepath.Join(dir, "state-é"), filepath.Join(dir, "next-é")
				if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
				var reader *os.File
				var err error
				if api == "Open" {
					reader, err = Open(path)
				} else {
					reader, err = OpenFile(path, os.O_RDONLY, 0)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				if err := os.WriteFile(next, []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := Rename(next, path); err != nil {
					t.Fatalf("replace held file: %v", err)
				}
				if got, err := os.ReadFile(path); err != nil || string(got) != "new" {
					t.Fatalf("published generation = %q, %v", got, err)
				}
				if got, err := io.ReadAll(reader); err != nil || string(got) != "old" {
					t.Fatalf("held generation = %q, %v", got, err)
				}
				if _, err := os.Stat(next); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("source survived replacement: %v", err)
				}
			})
		}
	}
}

// Int-fix contract A4: directory renames retain their existing native behavior.
func TestIntFixContractADirectoryRename(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	if err := os.Mkdir(from, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "state"), []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Rename(from, to); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(to, "state")); err != nil || string(got) != "kept" {
		t.Fatalf("renamed directory contents = %q, %v", got, err)
	}
}

// Int-fix contract A2: opening the source as a reparse point must rename the
// link itself and leave its referent intact, as os.Rename does.
func TestIntFixContractARenamesLinkItself(t *testing.T) {
	dir := t.TempDir()
	referent := filepath.Join(dir, "referent")
	if err := os.WriteFile(referent, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	if err := os.Symlink("referent", from); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}
	if err := Rename(from, to); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(to); err != nil || got != "referent" {
		t.Fatalf("renamed link = %q, %v", got, err)
	}
	if got, err := os.ReadFile(referent); err != nil || string(got) != "kept" {
		t.Fatalf("referent changed = %q, %v", got, err)
	}
}

// TestK3ReplacementKeepsTheDestinationUntilSuccess proves K3 without a remove
// window: success replaces the bytes; a failed source leaves them untouched.
func TestK3ReplacementKeepsTheDestinationUntilSuccess(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	if err := os.WriteFile(from, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Rename(from, to); err != nil {
		t.Fatal(err)
	}
	// Int-fix contract A3: retain the native missing-source error as well.
	if err := Rename(from, to); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source error = %v", err)
	}
	if got, err := os.ReadFile(to); err != nil || string(got) != "new" {
		t.Fatalf("destination = %q, %v", got, err)
	}
}
