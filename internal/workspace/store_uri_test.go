package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Literal filename characters must survive writing and reopening on every platform.
func TestWorkspaceStorePreservesLiteralDatabasePath(t *testing.T) {
	name := "collections %23 #é.db"
	if runtime.GOOS != "windows" {
		name = "collections %23 #é?.db"
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
	_, err = s.Create(context.Background(), "héllo")
	if err != nil {
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
	if got, err := s.Collections(context.Background()); err != nil || len(got) != 1 || got[0].Name != "héllo" {
		t.Fatalf("collection = %+v, %v", got, err)
	}
}
