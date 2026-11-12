//go:build windows

package store

import (
	"path/filepath"
	"testing"
)

// R2 opens the real history store at a native path with spaces and non-ASCII,
// writes a graph row, and reads it after reopening the same database.
func TestWindowsStoreNativePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir with space é", "graph #%25.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	if err := s.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "windows", Brief: "héllo"}}}, Provenance{Origin: OriginUser, Intent: "héllo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if node, ok, err := s.Node("windows"); err != nil || !ok || node.Brief != "héllo" {
		t.Fatalf("node = %+v, %v, %v", node, ok, err)
	}
}
