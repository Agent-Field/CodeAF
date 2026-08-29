package resident

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE INK RUN OF 2026-08-29, AT THE MOMENT ITS ONE NODE STOPPED EXISTING.
//
// `bench/deepswe/results/ink-grid-box-layout-…-s8` journaled, at 09:33:14,
// `leaf_exhausted` saying "its work is recorded and the node goes back on the
// queue" — and then, in the same second, `node_failed`. Store.Ready serves
// pending rows only, so the node never went anywhere near the queue; the
// settlement watch saw one terminal node and left with exit 1, no gate verdict,
// seventy-three minutes of unspent wall and a twenty-six kilobyte patch on disk.
//
// The law: an ending that is exhaustion is not a verdict on the work. The claim
// goes back, the node is offered again, and the next claim resumes from the
// record the last one left.
func TestAnExhaustedClaimGoesBackOnTheQueueRatherThanFailing(t *testing.T) {
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-2")
	// The record the abandoned attempt left behind. It is what makes a re-claim
	// a resumption rather than the same cold start again, and the runner reads
	// exactly this before deciding.
	recordTurns(t, graph, "task-2", 3)

	var mu sync.Mutex
	attempts := 0
	starts := make(chan struct{}, 8)
	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		mu.Lock()
		attempts++
		mine := attempts
		mu.Unlock()
		starts <- struct{}{}
		if mine == 1 {
			// Exactly what the node watchdog hands back when it gives up.
			return ExecResult{}, &executor.Abandoned{After: 17 * time.Minute}
		}
		return ExecResult{Summary: "grid layout landed"}, nil
	}, "chat-runner", 1)

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- runner.Serve(ctx) }()

	waitFor(t, starts, "the leaf never started")
	waitFor(t, starts, "the exhausted node was never offered again — it was settled failed")
	// The second attempt lands, which is what closes the node honestly.
	deadline := time.Now().Add(8 * time.Second)
	var final store.Node
	for time.Now().Before(deadline) {
		node, found, err := graph.Node("task-2")
		if err != nil {
			t.Fatalf("read the node: %v", err)
		}
		if found && node.Status == store.Done {
			final = node
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-served
	runner.Wait()

	if final.Status != store.Done {
		t.Fatalf("the node settled %q, want done — an exhausted attempt must not end the node", final.Status)
	}
	if final.Attempt < 2 {
		t.Fatalf("the node was claimed %d times, want the re-claim that resumes the work", final.Attempt)
	}
	// And the release says why, in a sentence a person reads, naming what
	// survived — because "back on the queue" is only worth printing if the next
	// claim can pick up where this one stopped.
	reason := releaseReason(t, graph, "task-2")
	if reason == "" {
		t.Fatal("the exhausted claim was released with no reason; the stream has nothing to say")
	}
	for _, want := range []string{"17m0s", "3 turns"} {
		if !contains(reason, want) {
			t.Fatalf("the release reason %q does not name %q", reason, want)
		}
	}
}

// AND AN ATTEMPT THAT RECORDED NOTHING IS NOT RESUMABLE, so it is not requeued.
//
// This is what keeps the rule above from being an unbounded retry. A re-claim
// that reads an empty record is the same cold start again, and a node whose
// every attempt costs the whole envelope and leaves no turn has told us the
// only thing it is going to.
func TestAnExhaustedClaimWithNothingRecordedIsAFailure(t *testing.T) {
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-2")

	starts := make(chan struct{}, 8)
	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		starts <- struct{}{}
		return ExecResult{}, &executor.Abandoned{After: 17 * time.Minute}
	}, "chat-runner", 1)

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- runner.Serve(ctx) }()

	waitFor(t, starts, "the leaf never started")
	deadline := time.Now().Add(8 * time.Second)
	var final store.Node
	for time.Now().Before(deadline) {
		node, found, err := graph.Node("task-2")
		if err != nil {
			t.Fatalf("read the node: %v", err)
		}
		if found && node.Status == store.Failed {
			final = node
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-served
	runner.Wait()

	if final.Status != store.Failed {
		t.Fatalf("a leaf that recorded nothing settled %q, want failed", final.Status)
	}
}

// recordTurns writes one attempt's worth of turns under a node, so the runner's
// "is there anything to resume from" question has a real record to read.
func recordTurns(t *testing.T, graph *store.Store, nodeID string, turns int) {
	t.Helper()
	entries := make([]store.TranscriptEntry, 0, turns)
	for turn := 1; turn <= turns; turn++ {
		entries = append(entries, store.TranscriptEntry{
			Turn: turn, Kind: store.TranscriptAssistant, Text: "editing the grid layout",
		})
	}
	if err := graph.RecordTranscript(nodeID, "deepseek/deepseek-v4-flash", entries); err != nil {
		t.Fatalf("record the transcript: %v", err)
	}
}

// releaseReason is what the journal says about why a claim went back.
func releaseReason(t *testing.T, graph *store.Store, nodeID string) string {
	t.Helper()
	events, err := graph.Events(0, 500)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	reason := ""
	for _, event := range events {
		if event.NodeID != nodeID || event.Kind != store.EventNodeReleased {
			continue
		}
		var carried struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(event.Payload, &carried) == nil && carried.Reason != "" {
			reason = carried.Reason
		}
	}
	return reason
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
