package filelock

import (
	"os"
	"path/filepath"
	"testing"
)

// TestK3LockCoordinatesWithoutLockingContents proves K3 on NTFS: holding the
// chat's lease excludes another holder while independent readers and appenders
// can still access its journal, as with advisory locks on Unix.
func TestK3LockCoordinatesWithoutLockingContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	if err := os.WriteFile(path, []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	holder, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := Lock(holder, true, true); err != nil {
		t.Fatal(err)
	}
	defer Unlock(holder)
	other, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := Lock(other, true, true); !IsBusy(err) {
		t.Fatalf("K3 competing holder = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "first\n" {
		t.Fatalf("K3 lock blocked reader: %q %v", got, err)
	}
	if _, err := other.WriteString("second\n"); err != nil {
		t.Fatalf("K3 lock blocked appender: %v", err)
	}
	if err := Unlock(holder); err != nil {
		t.Fatal(err)
	}
	if err := Lock(other, true, true); err != nil {
		t.Fatal(err)
	}
	if err := Unlock(other); err != nil {
		t.Fatal(err)
	}
}
