package exec

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

type namedExecutor struct {
	name string
	ran  chan string
}

func (n *namedExecutor) Subharness() string { return n.name }

func (n *namedExecutor) Run(_ context.Context, task Task) (*Outcome, error) {
	n.ran <- n.name
	return &Outcome{Text: n.name, Stop: StopDone}, nil
}

// Degradation, never failure. The registry routes saved programs by name, and a
// name nobody registered — including one a graph written by an older build
// carries — is served by the worker rather than refused. Work still has to get
// done.
func TestTheRegistryServesAnUnregisteredNameWithTheWorker(t *testing.T) {
	ran := make(chan string, 4)
	linear := &namedExecutor{name: LinearSubharness, ran: ran}
	registry := NewRegistry(linear)
	registry.Register(&namedExecutor{name: "spelling", ran: ran})

	for _, testCase := range []struct{ asked, want string }{
		{"spelling", "spelling"},
		{"", LinearSubharness},
		{LinearSubharness, LinearSubharness},
		{"a-worker-this-build-does-not-have", LinearSubharness},
	} {
		if got := registry.For(testCase.asked).Subharness(); got != testCase.want {
			t.Fatalf("For(%q) = %q, want %q", testCase.asked, got, testCase.want)
		}
	}
}

// There is one worker, so the only name this build can run is its own — said
// out loud, or left blank by a plan that never wrote the column. Everything a
// stored graph might name instead reads as a name this build does not have, and
// that is what makes an old store run rather than fail.
func TestTheOnlyWorkerThisBuildKnowsIsTheGeneralist(t *testing.T) {
	for _, name := range []string{"", "  ", LinearSubharness, " Linear "} {
		if !KnownSubharness(name) {
			t.Errorf("%q is not a name this build can run, and it is the only one there is", name)
		}
	}
	for _, name := range []string{"retired", "engine-x", "linear-ish", "research"} {
		if KnownSubharness(name) {
			t.Errorf("%q reads as a worker this build has", name)
		}
	}
	// Named and blank are still two different facts, and only the blank one
	// may be filled in from somewhere else.
	if !GeneralistSubharness(LinearSubharness) || GeneralistSubharness("") {
		t.Error("the generalist named and the column never written read as the same fact")
	}
	if !SubharnessChosen(LinearSubharness) || SubharnessChosen("") {
		t.Error("SubharnessChosen does not separate a written column from an empty one")
	}
}

// The scheduler's half of the covenant: a node's stored worker column reaches
// the task, the registry routes on it, and the ledger buckets the leaf by the
// only thing left that distinguishes one leaf from another — its shape.
func TestTheSchedulerRoutesEveryLeafToTheWorker(t *testing.T) {
	ran := make(chan string, 2)
	registry := NewRegistry(&namedExecutor{name: LinearSubharness, ran: ran})
	scheduler := &Scheduler{registry: registry}

	graph := &plan.Graph{Goal: "g", Nodes: []plan.Node{
		{ID: 1, Kind: plan.KindWork, Title: "fix it", Brief: "fix it", Size: plan.SizeAtomic, Subharness: "a-worker-this-build-does-not-have"},
		{ID: 2, Kind: plan.KindWork, Title: "read it", Brief: "read it", Size: plan.SizeOversized},
	}, NextID: 3}

	for _, testCase := range []struct {
		node      int
		wantShape string
	}{
		{1, "atomic"},
		{2, "oversized"},
	} {
		node := graph.Node(testCase.node)
		task := scheduler.taskFor(graph, node)
		if task.Subharness != node.Subharness {
			t.Fatalf("node %d: task carries %q, node says %q", testCase.node, task.Subharness, node.Subharness)
		}
		if got := LeafShape(node); got != testCase.wantShape {
			t.Fatalf("node %d: ledger bucket = %q, want %q", testCase.node, got, testCase.wantShape)
		}
		done := make(chan completion, 1)
		scheduler.work(context.Background(), node.ID, task, 0, LeafShape(node), done)
		<-done
		if got := <-ran; got != LinearSubharness {
			t.Fatalf("node %d ran on %q, want the worker", testCase.node, got)
		}
	}
}

// The budget shape is a claim about how long this kind of work legitimately
// takes, and it is one claim: whatever name a node carries, the room it gets is
// the worker's.
func TestEveryLeafGetsTheWorkersRoom(t *testing.T) {
	linear := SubharnessFor(LinearSubharness)
	if got := linear.Deadline(0); got != 15*time.Minute {
		t.Fatalf("the floor is %s", got)
	}
	if got := linear.Deadline(2_000_000); got != 40*time.Minute {
		t.Fatalf("the scaling is %s", got)
	}
	if SubharnessFor("a-worker-this-build-does-not-have").Deadline(0) != linear.Deadline(0) {
		t.Fatal("a name this build does not have was given a different room")
	}
	if SubharnessFor("").Deadline(0) != linear.Deadline(0) {
		t.Fatal("a node with no worker column was given a different room")
	}
}
