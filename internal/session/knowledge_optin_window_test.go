package session

import (
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// A turn that finished before the person opted in is never captured, even when
// the capture scanner happens to look in the middle of the opt-in itself.
//
// The opt-in is two writes, the profile switch and the vault's own record of
// when it began, and a scanner that looks between them must still see "not yet".
// The scanner looks on its own clock (a second after the chat's last write), so
// on a slow machine that look lands in the middle of StartBuild often enough to
// show: TestKnowledgeCapturesSavedTurnsOnlyAfterOptIn failed on the full suite
// with four pending sources where two were wanted, the first turn included.
//
// The test holds StartBuild at its first use of the vault's database until the
// scanner has made one look and finished it, which is the worst moment, and then
// checks that the turn written before StartBuild was not admitted.
func TestKnowledgeScanInTheMiddleOfOptInDoesNotAdmitEarlierTurns(t *testing.T) {
	profile := t.TempDir()
	if err := config.WriteMemoryEnabled(profile, false); err != nil {
		t.Fatal(err)
	}
	agent := consentChat(t, profile)
	vault := agent.Knowledge()
	if vault == nil {
		t.Fatal("persisted conversation has no knowledge vault")
	}
	collect(t, mustSubmit(t, agent, "Explain how the queue keeps work durable"))
	path := agent.file.journalName()
	var scans atomic.Int64
	scanned := func(looked string) {
		if looked == path {
			scans.Add(1)
		}
	}
	previous := knowledgeScanHook.Swap(&scanned)
	t.Cleanup(func() { knowledgeScanHook.Store(previous) })

	var held atomic.Bool
	var waited atomic.Bool
	hold := func(string) {
		// Only StartBuild's own open is held. The scanner opens the same
		// database while it looks, and holding that would wait on itself.
		if !strings.Contains(string(debug.Stack()), "(*Vault).StartBuild") || !held.CompareAndSwap(false, true) {
			return
		}
		before := scans.Load()
		deadline := time.Now().Add(knowledgeCatchUp + 3*time.Second)
		for scans.Load() == before && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		waited.Store(scans.Load() > before)
		// A scan holds its journal's lock for the whole look.
		agent.knowledgeMu.RLock()
		j := agent.knowledgeJournal
		agent.knowledgeMu.RUnlock()
		j.mu.Lock()
		j.mu.Unlock()
	}
	restore := knowledge.SetDatabaseHookForTest(hold)
	err := vault.StartBuild()
	restore()
	if err != nil {
		t.Fatal(err)
	}
	if !held.Load() || !waited.Load() {
		t.Fatalf("the scanner never looked while StartBuild was held (held %v, scanned %v): the test did not reach the moment it is about", held.Load(), waited.Load())
	}
	snapshot, err := vault.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 0 {
		t.Fatalf("a turn written before the opt-in was captured by a scan in the middle of it: %+v", snapshot)
	}
}
