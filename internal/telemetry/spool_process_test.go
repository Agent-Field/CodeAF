package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
)

// Pause after opening the append descriptor, while another process flushes.
// The child uses the same profile and local httptest relay as its parent.
func TestSpoolAcceptedWriteDuringOtherProcessFlush(t *testing.T) {
	if probe := os.Getenv("CODEAF_SPOOL_TEST_CHILD"); probe != "" {
		root := os.Getenv("CODEAF_TEST_SPOOL_ROOT")
		if root == "" {
			t.Fatal("missing parent spool root")
		}
		t.Setenv(home.EnvVar, root)
		expected := filepath.Join(root, "telemetry", "spool.jsonl")
		if got := spoolPath(); got != expected {
			t.Fatalf("child spool path = %q, want %q", got, expected)
		}
		fmt.Printf("child spool path: %s\n", spoolPath())
		forceLadderForTest(t, true)
		spoolAfterOpen = func() {
			if err := os.WriteFile(filepath.Join(probe, "opened"), []byte(spoolPath()), 0600); err != nil {
				t.Fatal(err)
			}
			waitSpoolBarrier(t, filepath.Join(probe, "release"))
		}
		defer func() { spoolAfterOpen = nil }()
		if err := SpoolSync(stamped(SessionStarted(ModeChat, false, "child", time.Now()))); err != nil {
			t.Fatal(err)
		}
		return
	}
	testHome(t)
	recorder := newRelay(t)
	warm := stamped(SessionStarted(ModeChat, false, "warm", time.Now()))
	if err := SpoolSync(warm); err != nil {
		t.Fatal(err)
	}
	root := home.Dir()
	expected := filepath.Join(root, "telemetry", "spool.jsonl")
	if got := spoolPath(); got != expected {
		t.Fatalf("parent spool path = %q, want %q", got, expected)
	}
	probe := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpoolAcceptedWriteDuringOtherProcessFlush$", "-test.count=1", "-test.timeout=20s")
	// The inherited home must differ from the chosen root or home's test guard
	// quarantines the child even after t.Setenv restores the parent's root.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, home.EnvVar+"=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, home.EnvVar+"="+t.TempDir(),
		"CODEAF_SPOOL_TEST_CHILD="+probe, "CODEAF_TEST_SPOOL_ROOT="+root)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	release := func() {
		if err := os.WriteFile(filepath.Join(probe, "release"), nil, 0600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() {
		release()
		if !joined {
			_ = cmd.Wait()
		}
	})
	waitSpoolBarrier(t, filepath.Join(probe, "opened"))
	childPath, err := os.ReadFile(filepath.Join(probe, "opened"))
	if err != nil {
		t.Fatal(err)
	}
	if string(childPath) != expected {
		t.Fatalf("child spool path = %q, parent = %q", childPath, expected)
	}
	t.Logf("parent and child spool path: %s", expected)
	flushed := make(chan struct{})
	guard.Go("telemetry.test.flush", func() {
		defer close(flushed)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = Flush(ctx)
	})
	// On base, Flush completes and unlinks the descriptor before release.
	// With locking, Flush waits for the child, so the test releases it instead.
	select {
	case <-flushed:
	case <-time.After(250 * time.Millisecond):
	}
	release()
	err = cmd.Wait()
	joined = true
	if err != nil {
		t.Fatalf("child: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "child spool path: "+expected+"\n") {
		t.Fatalf("child did not print the shared spool path:\n%s", output.String())
	}
	<-flushed
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || seen[id] {
			t.Fatalf("empty or duplicated accepted event ID %q", id)
		}
		seen[id] = true
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	sent := 0
	for _, post := range recorder.posts {
		for _, event := range post.events {
			id, _ := event["event_id"].(string)
			add(id)
			sent++
		}
	}
	left := SpoolContents()
	for _, raw := range left {
		var row jsonEvent
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatal(err)
		}
		add(row.EventID)
	}
	if len(seen) != 2 || !seen[warm.ID] {
		t.Fatalf("accepted event lost: sent=%d disk=%d distinct=%d, want 2", sent, len(left), len(seen))
	}
}

func waitSpoolBarrier(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !fileExists(path); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", filepath.Base(path))
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSpoolSyncMissingPathIsAnError(t *testing.T) {
	testHome(t)
	spoolAfterOpen = func() {
		if err := os.Remove(spoolPath()); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { spoolAfterOpen = nil })
	if err := SpoolSync(SessionStarted(ModeChat, false, "unlinked", time.Now())); !os.IsNotExist(err) {
		t.Fatalf("SpoolSync after unlink = %v, want missing-path error", err)
	}
}
