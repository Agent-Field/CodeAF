package session

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Unix parity contract 2: an expired Unix wait still reports the signal sent.
func TestUnixParityJobKillReceipt(t *testing.T) {
	target := &job{id: 1, done: make(chan struct{}), stop: func() {}, sink: &jobSink{}}
	jobs := &jobRegistry{jobs: []*job{target}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	line, result := jobs.killFor(ctx, 1)
	wantLine, wantResult := "job 1 killed", killSignalled
	if runtime.GOOS == "windows" {
		wantLine, wantResult = "job 1 was told to stop and had not ended when the wait ran out", killUnproven
	}
	if line != wantLine || result != wantResult {
		t.Fatalf("kill = %q, %v; want %q, %v", line, result, wantLine, wantResult)
	}
}

// Unix parity contracts 2 and 3: append locks the original writer descriptor,
// retaining flock ownership and the existing ledger bytes.
func TestUnixParityUsageWriterLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix flock ownership")
	}
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "usage.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := filelock.Lock(file, true, false); err != nil {
		t.Fatal(err)
	}
	defer filelock.Unlock(file)
	done := make(chan error, 1)
	go func() { _, err := (&usageIdentityCursor{}).append(file, nil); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		_ = filelock.Unlock(file)
		<-done
		t.Fatal("append opened a conflicting lock instead of retaining the writer lock")
	}
}
