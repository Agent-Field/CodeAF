package fsreplace

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Contract 1–3: a held reader keeps its generation while a writer publishes
// another, and a held lock continues to exclude owners after a rename.
func TestPublishContract123OpenHandlesAllowReplacementAndRemoval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	next := filepath.Join(dir, "next")
	if err := os.WriteFile(next, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Rename(next, path); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(reader); err != nil || string(got) != "old" {
		t.Fatalf("held generation = %q, %v", got, err)
	}
	lock, err := OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		t.Fatal(err)
	}
	defer filelock.Unlock(lock)
	if err := Rename(path, next); err != nil {
		t.Fatal(err)
	}
	other, err := OpenFile(next, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := filelock.Lock(other, true, true); !filelock.IsBusy(err) {
		t.Fatalf("renamed lock stopped excluding another owner: %v", err)
	}
	if err := Remove(next); err != nil {
		t.Fatal(err)
	}
	filelock.Unlock(lock)
	other.Close()
	lock.Close()
	reader.Close()
	if err := RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed directory survived: %v", err)
	}
}

// Contract 2–3: opening/creating state retains exclusive creation and errors.
func TestPublishContract23OpenAndRemoveKeepNativeErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	for _, missing := range []string{path, ""} {
		if file, err := Open(missing); !errors.Is(err, os.ErrNotExist) {
			if file != nil {
				file.Close()
			}
			t.Fatalf("missing reader %q: %v", missing, err)
		}
	}
	file, err := OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("state"); err != nil {
		t.Fatal(err)
	}
	if truncated, err := OpenFile(path, os.O_RDWR|os.O_TRUNC, 0); !errors.Is(err, os.ErrInvalid) {
		if truncated != nil {
			truncated.Close()
		}
		t.Fatalf("unsupported state-open flag: %v", err)
	}
	if second, err := OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600); err == nil {
		second.Close()
		t.Fatal("exclusive creation replaced existing state")
	} else if !errors.Is(err, os.ErrExist) {
		t.Fatalf("exclusive creation: %v", err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := Remove(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing remove: %v", err)
	}
}
