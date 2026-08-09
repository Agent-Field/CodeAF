package exec

import (
	"context"
	"strings"
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

// Degradation, never failure: a name nobody registered is served by the
// generalist rather than refused, and the registered one is actually reached.
func TestRegistryRoutesBySubharnessAndFallsBack(t *testing.T) {
	ran := make(chan string, 4)
	linear := &namedExecutor{name: LinearSubharness, ran: ran}
	registry := NewRegistry(linear)
	registry.Register(&namedExecutor{name: "swe", ran: ran})

	for _, testCase := range []struct{ asked, want string }{
		{"swe", "swe"},
		{"", LinearSubharness},
		{LinearSubharness, LinearSubharness},
		{"nobody-registered-this", LinearSubharness},
	} {
		if got := registry.For(testCase.asked).Subharness(); got != testCase.want {
			t.Fatalf("For(%q) = %q, want %q", testCase.asked, got, testCase.want)
		}
	}
}

// The menu is the whole choice context, and with one worker there is no choice
// to describe. Everything downstream keys off this emptiness.
func TestMenuIsEmptyUntilThereIsAChoice(t *testing.T) {
	if got := MenuText(); got != "" {
		t.Fatalf("a baseline process has a menu:\n%s", got)
	}
	defer ForgetSubharnesses()
	RegisterSubharness(SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole", PriorAnchors: "swe ruler"})
	UseSubharnessKnowledge(func(name string) string {
		if name == "swe" {
			return "median 12 turns, 90k tokens; n=9"
		}
		return ""
	})
	menu := MenuText()
	for _, want := range []string{
		"swe — software engineering taken whole",
		"measured here so far: median 12 turns",
		"Choose a specialist subharness only when the job's essence matches its purpose",
	} {
		if !strings.Contains(menu, want) {
			t.Fatalf("menu is missing %q:\n%s", want, menu)
		}
	}
	if !KnownSubharness("swe") {
		t.Fatal("a registered subharness is not known")
	}
	for _, baseline := range []string{"", LinearSubharness, "swe-ish"} {
		if KnownSubharness(baseline) {
			t.Fatalf("%q reads as a specialist", baseline)
		}
	}
}

// The scheduler's half of the covenant: the node's choice reaches the task, the
// registry routes on it, and the ledger gives that worker a bucket of its own.
func TestSchedulerRoutesTheNodesChoice(t *testing.T) {
	defer ForgetSubharnesses()
	RegisterSubharness(SubharnessInfo{Name: "swe", Purpose: "coding"})

	ran := make(chan string, 2)
	registry := NewRegistry(&namedExecutor{name: LinearSubharness, ran: ran})
	registry.Register(&namedExecutor{name: "swe", ran: ran})
	scheduler := &Scheduler{registry: registry}

	graph := &plan.Graph{Goal: "g", Nodes: []plan.Node{
		{ID: 1, Kind: plan.KindWork, Title: "fix it", Brief: "fix it", Size: plan.SizeAtomic, Subharness: "swe"},
		{ID: 2, Kind: plan.KindWork, Title: "read it", Brief: "read it", Size: plan.SizeOversized},
	}, NextID: 3}

	for _, testCase := range []struct {
		node            int
		want, wantShape string
	}{
		{1, "swe", "swe"},
		{2, LinearSubharness, "oversized"},
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
		if got := <-ran; got != testCase.want {
			t.Fatalf("node %d ran on %q, want %q", testCase.node, got, testCase.want)
		}
	}
}

// The budget shape is a claim about how long this kind of work takes. Linear's
// is the one every leaf ran on before there was a second worker, and it must
// still be that one exactly.
func TestBudgetShapeIsPerSubharness(t *testing.T) {
	linear := SubharnessFor(LinearSubharness)
	if got := linear.Deadline(0); got != 15*time.Minute {
		t.Fatalf("linear floor = %s", got)
	}
	if got := linear.Deadline(2_000_000); got != 40*time.Minute {
		t.Fatalf("linear scaling = %s", got)
	}
	if SubharnessFor("nobody").Deadline(0) != linear.Deadline(0) {
		t.Fatal("an unknown subharness does not get linear's shape")
	}

	defer ForgetSubharnesses()
	RegisterSubharness(SubharnessInfo{
		Name: "swe", Purpose: "coding",
		DeadlineFloor: time.Hour, DeadlineStep: 5 * time.Minute, DeadlinePerTokens: 100_000,
	})
	swe := SubharnessFor("swe")
	if got := swe.Deadline(0); got != time.Hour {
		t.Fatalf("swe floor = %s", got)
	}
	if got := swe.Deadline(2_000_000); got != 100*time.Minute {
		t.Fatalf("swe scaling = %s", got)
	}
}
