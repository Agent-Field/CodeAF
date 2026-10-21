//go:build windows

package workspace

import (
	"context"
	"path/filepath"
	"testing"
)

// R2 opens the collections store through its real door, keeping the native
// filename's space, Unicode, fragment and percent characters after reopening.
func TestWindowsWorkspaceStoreNativePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir with space é", "collections #%25.db")
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
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Collections(context.Background()); err != nil || len(got) != 1 || got[0].Name != "héllo" {
		t.Fatalf("collection = %+v, %v", got, err)
	}
}
