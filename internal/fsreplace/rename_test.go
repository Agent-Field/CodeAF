package fsreplace

import (
	"os"
	"path/filepath"
	"testing"
)

// TestK3ReplacementKeepsTheDestinationUntilSuccess proves K3 without a remove
// window: success replaces the bytes; a failed source leaves them untouched.
func TestK3ReplacementKeepsTheDestinationUntilSuccess(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	if err := os.WriteFile(from, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Rename(from, to); err != nil {
		t.Fatal(err)
	}
	if err := Rename(from, to); err == nil {
		t.Fatal("missing source succeeded")
	}
	if got, err := os.ReadFile(to); err != nil || string(got) != "new" {
		t.Fatalf("destination = %q, %v", got, err)
	}
}
