//go:build !windows

// K6 keeps Unix process, lock and mode assertions on the systems that provide them.
package lease

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestALiveLegacyHolderStillOwnsTheRole(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "graph.db")
	legacy, err := os.OpenFile(filepath.Join(dir, legacyLockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if err := syscall.Flock(int(legacy.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := writeResident(legacy, Resident{PID: os.Getpid(), Host: "here", Surface: "chat"}); err != nil {
		t.Fatal(err)
	}

	release, holder, err := AcquireResident(store, "do")
	if err != nil || release != nil || holder == nil {
		t.Fatalf("acquire beside a live legacy holder = release %v, holder %+v, err %v", release != nil, holder, err)
	}
	probed, err := ProbeResident(store)
	if err != nil || probed == nil || probed.PID != os.Getpid() {
		t.Fatalf("probe beside a live legacy holder = %+v, %v", probed, err)
	}

	// Once it lets go, the role is free and store-keyed like everything else.
	if err := syscall.Flock(int(legacy.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	release, holder, err = AcquireResident(store, "do")
	if err != nil || release == nil || holder != nil {
		t.Fatalf("acquire after the legacy holder exited = release %v, holder %+v, err %v", release != nil, holder, err)
	}
	_ = release()
}
