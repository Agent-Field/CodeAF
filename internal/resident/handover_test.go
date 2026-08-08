package resident

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func handoverStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}

func requestHandover(t *testing.T, graph *store.Store, instruction string) store.Command {
	t.Helper()
	command, err := graph.RequestCommand(store.Command{
		SessionID: "s", Kind: store.CommandHandover, Instruction: instruction,
	})
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func settled(t *testing.T, graph *store.Store, seq int64) store.Command {
	t.Helper()
	command, ok, err := graph.CommandBySeq(seq)
	if err != nil || !ok {
		t.Fatalf("command %d: ok=%v err=%v", seq, ok, err)
	}
	if command.Status == store.CommandPending {
		t.Fatalf("command %d is still pending", seq)
	}
	return command
}

// A resident that can hand the role over does, and says so in the receipt the
// requester reads.
func TestAHandoverRequestIsHonouredByAResidentThatKnowsTheVerb(t *testing.T) {
	graph := handoverStore(t)
	var asked Handover
	reconciler := New(graph, nil, nil).
		WithHandover(func(request Handover) (bool, string) {
			asked = request
			return true, "standing down"
		}).
		WithResidentSince(time.Now().Add(-time.Minute))
	command := requestHandover(t, graph, "pid 42 has a newer build")

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := settled(t, graph, command.Seq); got.Status != store.CommandApplied {
		t.Fatalf("handover settled as %s: %s", got.Status, got.Result)
	}
	if asked.Reason != "pid 42 has a newer build" || asked.Seq != command.Seq {
		t.Fatalf("the seam was handed %+v", asked)
	}
}

// A process with no surface to demote to — a wake pass, an embedder — settles
// the request in words instead of leaving it pending forever. Silence is what
// would make a requester wait on an answer that is never coming.
func TestAHandoverIsRefusedInWordsWhenThereIsNoSeam(t *testing.T) {
	graph := handoverStore(t)
	reconciler := New(graph, nil, nil)
	command := requestHandover(t, graph, "pid 42 has a newer build")

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := settled(t, graph, command.Seq)
	if got.Status != store.CommandRejected {
		t.Fatalf("handover settled as %s", got.Status)
	}
	if !strings.Contains(got.Result, "cannot hand the resident role over") {
		t.Fatalf("the refusal does not say why: %q", got.Result)
	}
}

// A request journaled before this process took the role was addressed to
// whoever was serving then. Honouring it would make a freshly promoted window
// stand straight back down, and the role would circle the open windows forever.
func TestAHandoverOlderThanTheRoleIsRefused(t *testing.T) {
	graph := handoverStore(t)
	command := requestHandover(t, graph, "an ask for the process that used to be here")
	honoured := false
	reconciler := New(graph, nil, nil).
		WithHandover(func(Handover) (bool, string) { honoured = true; return true, "" }).
		WithResidentSince(time.Now().Add(time.Minute))

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if honoured {
		t.Fatal("a stale handover reached the seam")
	}
	if got := settled(t, graph, command.Seq); got.Status != store.CommandRejected {
		t.Fatalf("a stale handover settled as %s", got.Status)
	}
}

// Draining stops the dispatch loop without cancelling the work in flight: a
// handover must not kill leaves whose claims the store owns.
func TestDrainStopsClaimingWithoutCancellingTheContext(t *testing.T) {
	graph := handoverStore(t)
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{}, nil
	}, "drain-test", 1)

	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- runner.Serve(ctx) }()

	runner.Drain()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a drained runner returned %v, want a clean stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a drained runner never stopped")
	}
	if ctx.Err() != nil {
		t.Fatal("draining cancelled the context the running leaves are using")
	}
}
