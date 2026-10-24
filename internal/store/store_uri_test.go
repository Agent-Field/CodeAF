package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Literal filename characters must survive writing and reopening on every platform.
func TestStorePreservesLiteralDatabasePath(t *testing.T) {
	name := "graph %23 #é.db"
	if runtime.GOOS != "windows" {
		name = "graph %23 #é?.db"
	}
	path := filepath.Join(t.TempDir(), "dir with space %23 #é", name)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	if err := s.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "literal-path", Brief: "héllo"}}}, Provenance{Origin: OriginUser, Intent: "héllo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("literal database missing or empty: %v", err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if node, ok, err := s.Node("literal-path"); err != nil || !ok || node.Brief != "héllo" {
		t.Fatalf("node = %+v, %v, %v", node, ok, err)
	}
}
