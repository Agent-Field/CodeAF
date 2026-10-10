package automation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func TestPresenceCountsTheWindowsThatAreHeld(t *testing.T) {
	p := NewPresence(t.TempDir())
	if n, err := p.Count(); err != nil || n != 0 {
		t.Fatalf("an empty machine counts %d, %v", n, err)
	}
	first, err := p.Hold("window")
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Hold("attached")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := p.Count(); n != 2 {
		t.Fatalf("two held windows count %d", n)
	}
	first()
	first() // releasing twice is harmless
	if n, _ := p.Count(); n != 1 {
		t.Fatalf("after one release, %d", n)
	}
	second()
	if n, _ := p.Count(); n != 0 {
		t.Fatalf("after both releases, %d", n)
	}
}

// A WINDOW WHOSE PROCESS IS GONE IS NOT A WINDOW. A file left behind by a crash
// has nobody holding its lock, so the count takes the lock, removes the file
// and does not count it.
func TestPresenceSweepsAWindowNobodyHolds(t *testing.T) {
	root := t.TempDir()
	p := NewPresence(root)
	if err := os.MkdirAll(filepath.Join(root, windowsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, windowsDir, "999999-dead.lock")
	if err := os.WriteFile(stale, []byte(`{"pid":999999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := p.Count(); err != nil || n != 0 {
		t.Fatalf("a crashed window counts %d, %v", n, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("the stale file is still there: %v", err)
	}
}

// A lock held through a different open file — another process, in life — is a
// window that is open.
func TestPresenceCountsALockHeldElsewhere(t *testing.T) {
	root := t.TempDir()
	p := NewPresence(root)
	if err := os.MkdirAll(filepath.Join(root, windowsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, windowsDir, "1-other.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := p.Count(); n != 1 {
		t.Fatalf("a window held elsewhere counts %d", n)
	}
}
