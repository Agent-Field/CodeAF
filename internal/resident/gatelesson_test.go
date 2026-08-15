package resident

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// settleWithGate runs one job from splice to done, recording the gates given.
func settleWithGate(t *testing.T, graph *store.Store, reconciler *Reconciler,
	nodeID, intent, summary string, gates ...store.DeliveryGate) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: nodeID, Brief: intent, Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-gate", Intent: intent}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim(nodeID, "worker")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%t err=%v", nodeID, won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	for _, gate := range gates {
		if err := graph.RecordDeliveryGate(nodeID, gate); err != nil {
			t.Fatal(err)
		}
	}
	if err := graph.Complete(claim, summary); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A gate verdict nobody repaired is a claim about the delivery, not a fact
// about it, and the notebook may not learn from it. Measured case: a job that
// delivered twelve complete profiles in one message was failed for delivering
// "a series of separate messages", and that verdict became a standing lesson.
func TestUnrepairedGateVerdictDoesNotReachDistiller(t *testing.T) {
	graph := openStore(t)
	var distilled string
	reconciler := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, _, outcome string, failed bool) ([]Learned, error) {
			distilled = outcome
			if failed {
				t.Fatal("a delivered job was marked as an execution failure")
			}
			return nil, nil
		})
	settleWithGate(t, graph, reconciler, "job", "list the twelve profiles",
		"here are the twelve profiles, in one message",
		store.DeliveryGate{Pass: false, Gap: "the profiles were written to separate files"})

	if strings.Contains(distilled, "Delivery gate evidence") ||
		strings.Contains(distilled, "separate files") {
		t.Fatalf("an unrepaired gate verdict was taught to the notebook: %q", distilled)
	}
	if !strings.Contains(distilled, "here are the twelve profiles") {
		t.Fatalf("the deliverable itself stopped reaching the distiller: %q", distilled)
	}
}

// The two things that bear a verdict out: the polish pass that closed the gap,
// and a later round in the same lineage that passed.
func TestRepairedGateVerdictStillTeaches(t *testing.T) {
	t.Run("polish closed the gap", func(t *testing.T) {
		graph := openStore(t)
		var distilled string
		reconciler := New(graph, nil, nil).WithDistiller(
			func(_ context.Context, _, outcome string, _ bool) ([]Learned, error) {
				distilled = outcome
				return nil, nil
			})
		settleWithGate(t, graph, reconciler, "job", "include the benchmark",
			"the polished answer includes benchmark 42",
			store.DeliveryGate{Pass: false, Gap: "the benchmark result is missing", PolishClosed: true})
		if !strings.Contains(distilled, "the benchmark result is missing") {
			t.Fatalf("a gap the polish pass closed was withheld: %q", distilled)
		}
	})

	t.Run("a later round in the lineage passed", func(t *testing.T) {
		graph := openStore(t)
		var distilled string
		reconciler := New(graph, nil, nil).WithDistiller(
			func(_ context.Context, _, outcome string, _ bool) ([]Learned, error) {
				distilled = outcome
				return nil, nil
			})
		settleWithGate(t, graph, reconciler, "job", "cite the source",
			"the answer, without its citation",
			store.DeliveryGate{Pass: false, Gap: "no source is cited"})
		if strings.Contains(distilled, "no source is cited") {
			t.Fatalf("the gap taught before any repair had been accepted: %q", distilled)
		}
		// The repair round is born inside the job's own "-x" namespace and is
		// accepted. Only now is the original verdict borne out by something
		// other than itself, and only now may it teach.
		settleWithGate(t, graph, reconciler, "job-x1", "add the citation",
			"the citation is now present", store.DeliveryGate{Pass: true})
		node, ok, err := graph.Node("job")
		if err != nil || !ok {
			t.Fatalf("read job: ok=%t err=%v", ok, err)
		}
		reconciler.distillJob(context.Background(), node, false)
		if !strings.Contains(distilled, "no source is cited") {
			t.Fatalf("a gap an accepted repair closed was withheld: %q", distilled)
		}
	})
}

// A working method belongs to the kind of work that earned it. Measured case:
// prose-delivery lessons from a research job were injected into a Go coding
// leaf running the swe pipeline.
func TestNotebookDigestKeepsMethodLessonsInsideTheirHarness(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "prose", Brief: "write twelve profiles", Stage: 1, Subharness: "linear",
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "write twelve profiles"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "coding", Brief: "build a markdown table aligner in Go", Stage: 1, Subharness: "swe",
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "build a markdown table aligner in Go"}); err != nil {
		t.Fatal(err)
	}

	crossHarness, err := graph.RecordFact("prose", "user", store.FactLesson,
		"assemble a markdown table deliverable into one contiguous final message, never separate files")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("coding", "user", store.FactLesson,
		"run go build on the markdown table package before reporting a coding job done"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("prose", "user", store.FactPreference,
		"the person wants a markdown table deliverable kept terse"); err != nil {
		t.Fatal(err)
	}

	digest := NotebookDigest(graph, "coding", "build a markdown table aligner in Go",
		"build a markdown table aligner in Go", 8)
	if strings.Contains(digest, "one contiguous final message") {
		t.Fatalf("a prose-delivery lesson reached a coding leaf: %q", digest)
	}
	if !strings.Contains(digest, "run go build") {
		t.Fatalf("a lesson the swe harness itself earned was withheld: %q", digest)
	}
	if !strings.Contains(digest, "kept terse") {
		t.Fatalf("a standing preference was scoped to a harness: %q", digest)
	}

	// The withheld line is never claimed as read: attribution has to describe
	// what the leaf actually saw, or outcome accounting blames the wrong fact.
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind != store.EventFactInjected || event.NodeID != "coding" {
			continue
		}
		if strings.Contains(string(event.Payload), strconv.FormatInt(crossHarness.Seq, 10)) {
			t.Fatalf("a dropped fact was recorded as injected: %s", event.Payload)
		}
	}

	// The same lesson is still the right advice for its own kind of work.
	back := NotebookDigest(graph, "prose", "write twelve profiles", "write twelve profiles", 8)
	if !strings.Contains(back, "one contiguous final message") {
		t.Fatalf("the lesson stopped reaching the work that earned it: %q", back)
	}
}
