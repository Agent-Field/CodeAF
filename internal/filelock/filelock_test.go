package filelock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExclusiveNonBlockingLockReportsBusyAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	if err := Lock(first, true, true); err != nil {
		t.Fatalf("lock first handle: %v", err)
	}
	if err := Lock(second, true, true); !IsBusy(err) {
		t.Fatalf("second lock = %v, want busy", err)
	}
	if err := Unlock(first); err != nil {
		t.Fatalf("unlock first handle: %v", err)
	}
	if err := Lock(second, true, true); err != nil {
		t.Fatalf("lock second handle after release: %v", err)
	}
	if err := Unlock(second); err != nil {
		t.Fatalf("unlock second handle: %v", err)
	}
}
