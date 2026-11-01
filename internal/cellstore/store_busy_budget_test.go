package cellstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// C4.4: classification shares one total budget across all candidates, and
// preserves the plain busy sentence when no holder can be found in time.
func TestStoreBusyClassificationSharesOneBudget(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 30; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%02d.lock", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := inspectStoreLock
	t.Cleanup(func() { inspectStoreLock = old })
	calls := 0
	inspectStoreLock = func(ctx context.Context, _, _ string) (StoreHolder, bool) {
		calls++
		select {
		case <-time.After(storeBusyInspectWithin / 4):
			return StoreHolder{}, false
		case <-ctx.Done():
			return StoreHolder{}, false
		}
	}
	start := time.Now()
	err := classifyStoreBusyError(root, "", errors.New("engine store is busy"))
	if elapsed := time.Since(start); elapsed > storeBusyInspectWithin+500*time.Millisecond {
		t.Fatalf("classification took %s, budget %s", elapsed, storeBusyInspectWithin)
	}
	if calls >= 30 || calls == 0 {
		t.Fatalf("inspected %d candidates", calls)
	}
	if err.Error() != "engine store is busy" {
		t.Fatal(err)
	}
	// A timely holder ends the scan immediately and keeps the existing message.
	calls = 0
	inspectStoreLock = func(context.Context, string, string) (StoreHolder, bool) {
		calls++
		return StoreHolder{PID: 42, Command: "codeaf chat"}, true
	}
	err = classifyStoreBusyError(root, "", errors.New("engine store is busy"))
	if calls != 1 || err.Error() != "engine store is busy (held by codeaf chat, pid 42)" {
		t.Fatalf("calls=%d: %v", calls, err)
	}
	calls = 0
	_ = classifyStoreBusyError(root, "", errors.New("unrelated failure"))
	if calls != 0 {
		t.Fatal("healthy path inspected locks")
	}
}
