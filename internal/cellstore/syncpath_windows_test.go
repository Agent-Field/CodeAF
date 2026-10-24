package cellstore

import (
	"os"
	"path/filepath"
	"testing"
)

// TestU4NativeWindowsFlushKeepsBytesAndReportsMissingFiles proves U4/C28:
// native durability uses writable handles without truncation, tolerates the
// platform's unsupported directory flush, and never hides a missing record.
func TestU4NativeWindowsFlushKeepsBytesAndReportsMissingFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "records")
	if err := os.WriteFile(path, []byte("ordered records"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := syncPath(path); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "ordered records" {
		t.Fatalf("flush changed bytes: %q %v", raw, err)
	}
	if err := syncPath(root); err != nil {
		t.Fatal(err)
	}
	if err := syncPath(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing file was acknowledged: %v", err)
	}
}
