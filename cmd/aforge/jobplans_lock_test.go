package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// heldRevisionClient stands in for the sentinel's model. Every call announces
// itself on arrived and then waits for a token on release, which is how a
// round-trip that lasts exactly as long as the test wants gets written without
// sleeping through it.
type heldRevisionClient struct {
	arrived chan struct{}
	release chan struct{}
}

func (c *heldRevisionClient) CompleteWithMessages(_ context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.arrived <- struct{}{}
	<-c.release
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{
			Type: "text", Text: `{"operations":[]}`,
		}}},
	}}}, nil
}

func (c *heldRevisionClient) Model() string { return "test/sentinel" }

func newHeldRevisionClient() *heldRevisionClient {
	return &heldRevisionClient{arrived: make(chan struct{}), release: make(chan struct{}, 8)}
}

// planJobFixture mints one job in the store and the retained plan document for
// it, shaped the way an admitted chat job is: a root over two unstarted leaves.
func planJobFixture(t *testing.T, graph *store.Store, plans *jobPlans, prefix string) store.Node {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: prefix, Brief: "build the thing", Stage: 2},
		{ID: prefix + "-n1", Parent: prefix, Brief: "first half", Stage: 1},
		{ID: prefix + "-n2", Parent: prefix, Brief: "second half", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "build the thing"}); err != nil {
		t.Fatal(err)
	}
	plans.put(prefix, &plan.Graph{Goal: "build the thing", NextID: 3, Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "first half"},
		{ID: 2, Stage: 1, Kind: plan.KindWork, Title: "second half"},
	}}, prefix, "work/model", nil)
	job, found, err := graph.Node(prefix)
	if err != nil || !found {
		t.Fatalf("job %s: %v", prefix, err)
	}
	return job
}

func awaitSentinel(t *testing.T, sentinel *heldRevisionClient, what string) {
	t.Helper()
	select {
	case <-sentinel.arrived:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never reached its model", what)
	}
}

func awaitClose(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not finish", what)
	}
}

// The registry lock used to be held across the sentinel's model call, so one
// leaf landing a revision stopped every leaf of every job in the process — a
// lookup is the first thing a leaf does, and it waited behind a round-trip that
// had nothing to do with it. Nothing on a leaf's hot path may wait on a model.
func TestRevisionDoesNotBlockLeavesWhileTheModelThinks(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	plans := newJobPlans(graph)
	revising := planJobFixture(t, graph, plans, "task-1")
	planJobFixture(t, graph, plans, "task-2")

	sentinel := newHeldRevisionClient()
	client := adoptLiveClient(config.Config{}, "test/sentinel", sentinel)

	revised := make(chan struct{})
	go func() {
		defer close(revised)
		_, _ = plans.reviseForUser(context.Background(), config.Config{}, client, graph, revising,
			"use the v2 API", resident.RevisionRedirect)
	}()
	awaitSentinel(t, sentinel, "the revision")

	// The revision is parked inside its round-trip. Every call below is on a
	// leaf's hot path — an unrelated job's, and the revised job's own.
	moved := make(chan struct{})
	go func() {
		defer close(moved)
		if prefix, document, node, _, _ := plans.lookup("task-2-n1"); prefix == "task-2" && node != nil {
			plans.markClaimed(document, node, "", 0)
			plans.recordOutcome(document, node, nil, nil)
		}
		if prefix, document, node, _, _ := plans.lookup("task-1-n2"); prefix == "task-1" && node != nil {
			plans.markClaimed(document, node, "", 0)
		}
		_ = plans.takeContract("task-1-n2")
		_, _ = plans.get("task-1")
	}()
	awaitClose(t, moved, "leaves moving while the sentinel thinks")

	sentinel.release <- struct{}{}
	awaitClose(t, revised, "the revision")
}

// Handing the document back mid-call must not let two revisions of one plan
// interleave their edits. That ordering is what the old registry-wide lock
// genuinely guaranteed, and it is kept — per plan rather than per process, so
// another job revises alongside instead of queueing behind.
func TestRevisionsOfOnePlanStayInSingleFile(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	plans := newJobPlans(graph)
	first := planJobFixture(t, graph, plans, "task-1")
	second := planJobFixture(t, graph, plans, "task-2")

	sentinel := newHeldRevisionClient()
	client := adoptLiveClient(config.Config{}, "test/sentinel", sentinel)
	revise := func(job store.Node) chan struct{} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = plans.reviseForUser(context.Background(), config.Config{}, client, graph, job,
				"use the v2 API", resident.RevisionRedirect)
		}()
		return done
	}

	firstDone := revise(first)
	awaitSentinel(t, sentinel, "the first revision")

	// Another job's revision runs alongside it: two plans are two documents and
	// share nothing but the registry they are filed in.
	secondDone := revise(second)
	awaitSentinel(t, sentinel, "a second job's revision")

	// A second revision of the SAME plan waits its turn.
	queuedDone := revise(first)
	select {
	case <-sentinel.arrived:
		t.Fatal("two revisions of one plan were in flight at once")
	case <-time.After(250 * time.Millisecond):
	}

	sentinel.release <- struct{}{}
	sentinel.release <- struct{}{}
	sentinel.release <- struct{}{}
	awaitSentinel(t, sentinel, "the queued revision")
	awaitClose(t, firstDone, "the first revision")
	awaitClose(t, secondDone, "the second job's revision")
	awaitClose(t, queuedDone, "the queued revision")
}
