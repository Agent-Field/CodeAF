package session

import (
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func waitForKnowledgePending(t *testing.T, vault *knowledge.Vault, want int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		snapshot, err := vault.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Pending == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("wanted %d pending sources within %s, got %+v", want, within, snapshot)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestKnowledgeCaptureFollowsTheChatsOwnWrites(t *testing.T) {
	agent := consentChat(t, t.TempDir())
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Explain how the queue keeps work durable"))
	waitForKnowledgePending(t, vault, 2, time.Second)
}

func TestAnIdleChatsCaptureRunsOnlyTheSafetyTick(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		name := "capture off"
		if enabled {
			name = "capture on"
		}
		t.Run(name, func(t *testing.T) {
			agent := consentChat(t, t.TempDir())
			path := agent.file.journalName()
			var scans atomic.Int64
			hook := func(scanned string) {
				if scanned == path {
					scans.Add(1)
				}
			}
			previous := knowledgeScanHook.Swap(&hook)
			t.Cleanup(func() { knowledgeScanHook.Store(previous) })
			if enabled {
				if err := agent.Knowledge().StartBuild(); err != nil {
					t.Fatal(err)
				}
			}
			collect(t, mustSubmit(t, agent, "Explain how the queue keeps work durable"))
			if enabled {
				waitForKnowledgePending(t, agent.Knowledge(), 2, 2*time.Second)
			}
			// Let the completed turn's catch-up finish before measuring idle scans.
			time.Sleep(2200 * time.Millisecond)
			scans.Store(0)
			time.Sleep(6 * time.Second)
			count := scans.Load()
			t.Logf("idle chat (%s): %d scans in 6s", name, count)
			if count > 2 {
				t.Fatalf("idle chat scanned %d times; want <= 2", count)
			}
		})
	}
}

func TestKnowledgeCapturesAnotherWritersLinesWithinTheSafetyTick(t *testing.T) {
	agent := consentChat(t, t.TempDir())
	vault := agent.Knowledge()
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Explain how the queue keeps work durable"))
	waitForKnowledgePending(t, vault, 2, 2*time.Second)
	// No turn or catch-up wake should be left to discover the external append.
	time.Sleep(2200 * time.Millisecond)
	file, err := os.OpenFile(agent.file.journalName(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewEncoder(file).Encode(map[string]any{
		"type": "message", "role": "user", "content": "Another writer keeps this source durable.",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
	closeErr := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	waitForKnowledgePending(t, vault, 3, knowledgeSafetyTick+2*time.Second)
}
