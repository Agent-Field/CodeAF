package automation

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Literal filename characters must survive writing and reopening on every platform.
func TestAutomationStorePreservesLiteralDatabasePath(t *testing.T) {
	directory := "automations %23 #é"
	if runtime.GOOS != "windows" {
		directory += "?"
	}
	root := filepath.Join(t.TempDir(), directory)
	path := filepath.Join(root, DatabaseName)
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
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("literal database missing or empty: %v", err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(item.ID); err != nil || got.Title != "héllo" {
		t.Fatalf("automation = %+v, %v", got, err)
	}
}
