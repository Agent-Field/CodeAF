package main

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The lifetime law, stated as a test: a leaf already running when the surface
// goes away keeps the context it started with, finishes, and journals its
// result.
//
// The incident this exists for is in audit-notes/headless-regression-audit.md
// §12. Three nodes of one live job died on the same millisecond —
// `Post ".../chat/completions": context canceled` — five milliseconds after a
// `seen_touched {"surface":"tui","state":"detached"}` edge, with the reattach
// four seconds behind it. Closing the window cancelled the runner's context on
// the same line it cancelled the head's, so ~995k prompt tokens of work in
// flight were bought and thrown away, and the resident then read the wreckage
// as a flaky provider.
//
// The detach signal is driven exactly the way the window drives it — the same
// two calls runChat makes when the surface returns, in the same order — because
// a test that reached past them would prove nothing about the path a person
// takes.
func TestClosingTheWindowLetsRunningWorkLand(t *testing.T) {
	window := testWindow(t, t.TempDir())

	started := make(chan context.Context, 1)
	release := make(chan struct{})
	landed := make(chan struct{})
	// What the leaf's own context said at the moment it finished, which is the
	// only moment the question means anything: after it lands, the run context
	// is supposed to end.
	var atLanding error
	brains := &testBrains{grace: 10 * time.Second}
	brains.exec = func(leaf context.Context, node store.Node) (resident.ExecResult, error) {
		started <- leaf
		<-release
		atLanding = leaf.Err()
		close(landed)
		return resident.ExecResult{Summary: "the answer, written after the window closed"}, nil
	}

	role := testResidency(t, window, brains)
	if err := role.claim(); err != nil {
		t.Fatal(err)
	}
	if err := window.graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "only", Brief: "gather", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: window.session, Intent: "do the thing"}); err != nil {
		t.Fatal(err)
	}

	var leaf context.Context
	select {
	case leaf = <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the runner never claimed the leaf")
	}

	// What the surface does when it closes: journal the detach edge, then take
	// the window down.
	if err := role.sessionClosed(); err != nil {
		t.Fatal(err)
	}
	seen, found, err := window.graph.LastSeen()
	if err != nil || !found || seen.State != store.SeenDetached {
		t.Fatalf("the detach edge was not journaled: %+v found=%v err=%v", seen, found, err)
	}
	if leaf.Err() != nil {
		t.Fatalf("journalling the detach cancelled the leaf: %v", leaf.Err())
	}

	closed := make(chan struct{})
	go func() { defer close(closed); role.stop() }()

	// The window is on its way out and the leaf is still mid-call. This is the
	// millisecond the incident happened on.
	deadline := time.After(2 * time.Second)
	for {
		done := false
		select {
		case <-closed:
			t.Fatal("the window finished closing while a leaf was still running")
		case <-deadline:
			done = true
		case <-time.After(20 * time.Millisecond):
		}
		if leaf.Err() != nil {
			t.Fatalf("closing the window cancelled the running leaf: %v", leaf.Err())
		}
		if done {
			break
		}
	}

	close(release)
	select {
	case <-landed:
	case <-time.After(10 * time.Second):
		t.Fatal("the leaf never finished")
	}
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the window never finished closing")
	}
	if atLanding != nil {
		t.Fatalf("the leaf's context was cancelled before it landed: %v", atLanding)
	}

	node, ok, err := window.graph.Node("only")
	if err != nil || !ok {
		t.Fatalf("read the leaf: ok=%v err=%v", ok, err)
	}
	if node.Status != store.Done {
		t.Fatalf("the leaf did not land: status=%v error=%q", node.Status, node.Error)
	}
	if node.Summary != "the answer, written after the window closed" {
		t.Fatalf("the result was not journalled: %q", node.Summary)
	}
}

// The other half of the same law, and the reason the grace is set where it is
// rather than in the shared builder: a one-shot's shutdown is the wall it was
// given, and a wall means stop now.
//
// `aforge do` returns from buildBrain before the window's grace is assigned, so
// its leafGrace stays zero and the landing wait is skipped entirely. This pins
// that as behaviour instead of a reading of the branch: a brain with no grace
// must take its context away at once and return without waiting on the leaf.
func TestAOneShotShutdownDoesNotWaitForRunningWork(t *testing.T) {
	window := testWindow(t, t.TempDir())

	started := make(chan context.Context, 1)
	// Grace zero is what headless leaves it at; every other residency test
	// takes this same path, which is why none of them pays for a wait.
	brains := &testBrains{}
	brains.exec = func(leaf context.Context, node store.Node) (resident.ExecResult, error) {
		started <- leaf
		// A leaf that honours its context, which is the only kind this asks
		// about: the question is whether the shutdown takes the context away
		// now or holds it for the window's two minutes first.
		<-leaf.Done()
		return resident.ExecResult{}, leaf.Err()
	}

	role := testResidency(t, window, brains)
	if err := role.claim(); err != nil {
		t.Fatal(err)
	}
	if err := window.graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "only", Brief: "gather", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: window.session, Intent: "do the thing"}); err != nil {
		t.Fatal(err)
	}

	var leaf context.Context
	select {
	case leaf = <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the runner never claimed the leaf")
	}

	closed := make(chan struct{})
	go func() { defer close(closed); role.stop() }()
	// Comfortably inside the window's grace and comfortably outside the time a
	// cancelled leaf needs to notice, so a pass here can only mean the landing
	// wait was skipped.
	select {
	case <-closed:
	case <-time.After(leafSettleNotice * 8):
		t.Fatal("a one-shot's shutdown waited for the work its wall was supposed to end")
	}
	if leaf.Err() == nil {
		t.Fatal("the wall passed and the leaf still held a live context")
	}
}
