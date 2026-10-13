package enginehost

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAHostDirectoryIsNamedByTheFolderNotASpelling is the identity-layer
// regression for issue #1761 and the review of #1798: every question this
// package answers — lookup, dial, lock, socket — funnels through [where], and
// where is the one place a symlinked spelling must resolve. Before the fix, a
// surface that reached the folder through a link and a daemon named by the
// resolved path hashed to two host directories, and the launch spent its wait
// failing to meet.
func TestAHostDirectoryIsNamedByTheFolderNotASpelling(t *testing.T) {
	shortHome(t)
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkedrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	socketFromTarget, err := SocketPath(target)
	if err != nil {
		t.Fatalf("SocketPath(target): %v", err)
	}
	socketFromLink, err := SocketPath(link)
	if err != nil {
		t.Fatalf("SocketPath(link): %v", err)
	}
	if socketFromLink != socketFromTarget {
		t.Fatalf("two spellings were given two sockets: link=%q target=%q", socketFromLink, socketFromTarget)
	}

	lockFromTarget, err := LockPath(target)
	if err != nil {
		t.Fatalf("LockPath(target): %v", err)
	}
	lockFromLink, err := LockPath(link)
	if err != nil {
		t.Fatalf("LockPath(link): %v", err)
	}
	if lockFromLink != lockFromTarget {
		t.Fatalf("two spellings were given two locks: link=%q target=%q", lockFromLink, lockFromTarget)
	}

	// And a plain directory still keys to itself: resolving only moves
	// spellings that a link carries.
	plainSocket, err := SocketPath(filepath.Join(t.TempDir(), "plainrepo"))
	if err != nil {
		t.Fatalf("SocketPath(plain): %v", err)
	}
	if plainSocket == socketFromTarget {
		t.Fatal("a different workspace was given the same socket")
	}
}
