package filememo

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// THE LAW: a file is read at most once per change. Everything else in this
// package exists to make that sentence true.
func TestAFileIsReadOnceUntilItChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}

	var reads atomic.Int64
	memo := New(func(_ string, data []byte, missing bool) (string, error) {
		reads.Add(1)
		if missing {
			return "", nil
		}
		return string(data), nil
	})

	for range 20 {
		value, err := memo.Read(path)
		if err != nil || value != "first" {
			t.Fatalf("read %q/%v, want the file's contents", value, err)
		}
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("twenty reads of an unchanged file parsed it %d times, want once", got)
	}

	// A CHANGED FILE IS READ AGAIN. The stamp moves with the write, and the size
	// differs too, so either fact alone would have caught this — which is the
	// point of keying on both.
	future := time.Now().Add(time.Second)
	if err := os.WriteFile(path, []byte("second one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if value, _ := memo.Read(path); value != "second one" {
		t.Fatalf("after a write the memo still says %q", value)
	}
	if got := reads.Load(); got != 2 {
		t.Fatalf("a changed file was parsed %d times in all, want twice", got)
	}
}

// ABSENCE IS A VALUE AND IS HELD LIKE ONE. A profile with no config.json is the
// ordinary state of a fresh install, and answering it must not cost a read per
// call either.
func TestAMissingFileIsAnsweredWithoutReadingItAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-there.json")
	var derivations atomic.Int64
	memo := New(func(_ string, _ []byte, missing bool) (bool, error) {
		derivations.Add(1)
		return missing, nil
	})

	for range 10 {
		if gone, _ := memo.Read(path); !gone {
			t.Fatal("a missing file did not read as missing")
		}
	}
	if got := derivations.Load(); got != 1 {
		t.Fatalf("ten reads of a missing file derived %d times, want once", got)
	}

	// And the file APPEARING is a change like any other.
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if gone, _ := memo.Read(path); gone {
		t.Fatal("a file that appeared still read as missing")
	}
}

// A CALLER'S OWN STAMP INVALIDATES, whatever the filesystem's clock says. It is
// what lets a package that writes the file it reads never serve its own stale
// write — the case a timestamp cannot be trusted for.
func TestACallersStampInvalidatesWithoutTouchingTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	var generation atomic.Uint64
	var reads atomic.Int64
	memo := Stamped(generation.Load, func(_ string, data []byte, _ bool) (string, error) {
		reads.Add(1)
		return string(data), nil
	})

	memo.Read(path)
	memo.Read(path)
	if got := reads.Load(); got != 1 {
		t.Fatalf("two reads at one stamp parsed %d times, want once", got)
	}
	// The file is rewritten to the SAME SIZE and its timestamp is put back, which
	// is the whole hazard: nothing about the file says it moved.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	generation.Add(1)
	if value, _ := memo.Read(path); value != "b" {
		t.Fatalf("after the stamp moved the memo still says %q, want the new contents", value)
	}
}
