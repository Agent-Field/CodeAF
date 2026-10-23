//go:build windows

package automation

import (
	"path/filepath"
	"testing"
	"time"
)

// N2 writes and reopens an automation in a native Windows folder whose spaces,
// Unicode, fragment and percent characters must remain part of the filename.
func TestWindowsAutomationStoreNativePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dir with space é", "automations #%25")
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	item, err := s.Create(Automation{Title: "héllo", Workspace: t.TempDir(), Schedule: Schedule{At: time.Now().Add(time.Hour)}, Action: Action{Say: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(item.ID); err != nil || got.Title != "héllo" {
		t.Fatalf("automation = %+v, %v", got, err)
	}
}
