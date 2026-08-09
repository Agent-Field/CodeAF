package agentjson

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The exact sequence from finding 6: attempt 1 outlives its grace window, then
// attempt 2 runs and publishes a good verdict at the same configured path. The
// orphan must never destroy or replace attempt 2's artifact.
func TestOrphanCannotClobberALaterDispatchArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.json")
	release := make(chan struct{})
	started := make(chan struct{})

	slow := &recordingClient{run: func(_ context.Context, request Request) error {
		close(started)
		<-release
		_ = os.WriteFile(request.OutputPath, []byte(`{"decision":"STALE","reason":"orphan"}`), 0o600)
		return nil
	}}
	input := testInput(path)
	input.MaxRetries = intPointer(0)
	timeoutMS := int64(5)
	input.TimeoutMS = &timeoutMS

	first := make(chan struct{})
	go func() {
		_, _ = DispatchJSON(context.Background(), input, testDeps(slow))
		close(first)
	}()
	<-started
	<-first // attempt 1 returned while its client is still live

	// Attempt 2 completes normally and publishes a good verdict.
	good := &recordingClient{run: func(_ context.Context, request Request) error {
		return os.WriteFile(
			request.OutputPath, []byte(`{"decision":"yes","reason":"fresh"}`), 0o600,
		)
	}}
	second := testInput(path)
	result, err := DispatchJSON(context.Background(), second, testDeps(good))
	if err != nil {
		t.Fatalf("attempt 2 failed: %v", err)
	}
	if result.Data.Decision != "yes" {
		t.Fatalf("attempt 2 consumed the orphan's artifact: %#v", result.Data)
	}

	// Now let the orphan finish and write. It must not affect attempt 2.
	close(release)
	deadline := time.After(3 * time.Second)
	for ActiveOrphanedClients() > 0 {
		select {
		case <-deadline:
			t.Fatal("orphan never released")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if result.Data.Decision != "yes" {
		t.Fatalf("orphan corrupted the completed result: %#v", result.Data)
	}
}
