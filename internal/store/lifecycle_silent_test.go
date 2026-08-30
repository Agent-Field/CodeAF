package store

import (
	"strings"
	"testing"
	"time"
)

// A CLAIM IS HELD BY EVIDENCE OF LIFE, NOT BY A CLOCK.
//
// The ink run of 2026-08-29 is the whole of this test. `task-2` was claimed at
// 07:08:03 and released at 07:30:13, then again at 07:52:23, 08:14:34 and
// 08:36:40 — 22m10s, 22m10s, 22m11s, 22m06s, a metronome. Nothing had hung: it
// had flushed a batch of recorded turns thirty seconds before each release. The
// window was measured from the claim's start, which asks how long the worker has
// been ALIVE, and one claim legitimately carries an executor's deadline plus the
// retry that a spent deadline earns — twice this window.
func TestALeafThatKeepsWorkingKeepsItsClaim(t *testing.T) {
	graph := openTestStore(t, t.TempDir()+"/alive.db")
	defer graph.Close()

	claim := startTestLeaf(t, graph, "task-2")
	// Older than any window this reaper will ever be given.
	backdateStart(t, graph, "task-2", 90*time.Minute)

	// And still working: one flush of its own turns, a minute ago. This is the
	// mark a leaf leaves as it goes, not one it has to be asked for.
	if err := graph.RecordTranscript("task-2", "a-model", []TranscriptEntry{
		{Turn: 138, Kind: TranscriptAssistant, Text: "carrying on"},
	}); err != nil {
		t.Fatalf("record transcript: %v", err)
	}

	silent, err := graph.SilentClaims(22 * time.Minute)
	if err != nil {
		t.Fatalf("SilentClaims: %v", err)
	}
	if len(silent) != 0 {
		t.Fatalf("reported %v silent — a leaf that recorded a turn a moment ago is working", silent)
	}
	node, _, err := graph.Node("task-2")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if node.Status != Running || node.ClaimToken != claim.Token {
		t.Fatalf("task-2 is %s on token %d, want running on %d", node.Status, node.ClaimToken, claim.Token)
	}
}

// A billed model call is the other mark, and the one a worker leaves when its
// executor keeps no transcript at all.
func TestABilledCallIsAlsoASignOfLife(t *testing.T) {
	graph := openTestStore(t, t.TempDir()+"/billed.db")
	defer graph.Close()

	startTestLeaf(t, graph, "task-2")
	backdateStart(t, graph, "task-2", 90*time.Minute)
	if err := graph.RecordUsage(NodeUsage{
		NodeID: "task-2", PromptTokens: 11_106_468, CompletionTokens: 92,
		Cost: 0.231, Model: "a-model",
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	silent, err := graph.SilentClaims(22 * time.Minute)
	if err != nil {
		t.Fatalf("SilentClaims: %v", err)
	}
	if len(silent) != 0 {
		t.Fatalf("reported %v silent — a leaf billed for a call a moment ago is working", silent)
	}
}

// And the case the reaper exists for: nobody is behind the claim, so it goes
// back — with the reason on the release, because four of these landed on the ink
// run and the store could not afterwards tell them from a worker's own hand-back.
func TestASilentClaimIsReleasedAndTheReasonIsJournaled(t *testing.T) {
	graph := openTestStore(t, t.TempDir()+"/silent.db")
	defer graph.Close()

	startTestLeaf(t, graph, "task-2")
	// It worked, and then it stopped: the last turn it recorded is as old as the
	// claim itself, so there has been no sign of life for the whole window.
	if err := graph.RecordTranscript("task-2", "a-model", []TranscriptEntry{
		{Turn: 1, Kind: TranscriptAssistant, Text: "starting"},
	}); err != nil {
		t.Fatalf("record transcript: %v", err)
	}
	backdateStart(t, graph, "task-2", 40*time.Minute)
	backdateSigns(t, graph, "task-2", 40*time.Minute)

	silent, err := graph.SilentClaims(22 * time.Minute)
	if err != nil {
		t.Fatalf("SilentClaims: %v", err)
	}
	if len(silent) != 1 || silent[0].ID != "task-2" {
		t.Fatalf("silent = %v, want the one claim nobody is behind", silent)
	}
	if silent[0].Quiet < 22*time.Minute {
		t.Fatalf("the silence was measured at %s, want at least the window", silent[0].Quiet)
	}
	if !strings.Contains(silent[0].Reason, "no sign of life") {
		t.Fatalf("reason = %q, want it to say what was missing", silent[0].Reason)
	}

	// The sweep only reads; taking the claim back is the caller's decision,
	// because only the caller knows whether it has a worker to stop first.
	if err := graph.ReleaseWithReason(silent[0].Claim(), silent[0].Reason); err != nil {
		t.Fatalf("release: %v", err)
	}
	node, _, err := graph.Node("task-2")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if node.Status != Pending {
		t.Fatalf("task-2 is %s, want pending once the claim is back", node.Status)
	}

	// And the reason is on the journal, where an autopsy reads it.
	events, err := graph.Events(0, 200)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	found := ""
	for _, event := range events {
		if event.Kind != EventNodeReleased {
			continue
		}
		found = string(event.Payload)
	}
	if !strings.Contains(found, "no sign of life") {
		t.Fatalf("the release event says %s — a reaped claim must say why", found)
	}
}

// startTestLeaf splices one leaf, claims it and starts it.
func startTestLeaf(t *testing.T, graph *Store, id string) Claim {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: id, Brief: "x"}}},
		Provenance{Origin: OriginSelf, Intent: "liveness test"}); err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
	claim, ok, err := graph.Claim(id, "runner")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	return claim
}

// backdateStart rewrites started_at rather than waiting, exactly as the older
// reaper test does: the sweep reads the column, not the clock it was stamped from.
func backdateStart(t *testing.T, graph *Store, id string, ago time.Duration) {
	t.Helper()
	stamp := formatTime(time.Now().Add(-ago))
	if _, err := graph.db.Exec(`UPDATE nodes SET started_at = ? WHERE id = ?`, stamp, id); err != nil {
		t.Fatalf("backdate start: %v", err)
	}
}

// backdateSigns ages every mark the node's worker left, which is how a test says
// "and then it went quiet" without sleeping through a window.
func backdateSigns(t *testing.T, graph *Store, id string, ago time.Duration) {
	t.Helper()
	stamp := formatTime(time.Now().Add(-ago))
	for _, table := range []string{"usage", "usage_turns", "transcript"} {
		if _, err := graph.db.Exec(`UPDATE `+table+` SET ts = ? WHERE node_id = ?`, stamp, id); err != nil {
			t.Fatalf("backdate %s: %v", table, err)
		}
	}
}
