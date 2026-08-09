package resident

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The plan slot is silent by default and durable when it is not. A split that is
// only ever a fact about a running process cannot be shown to anyone after that
// process is gone, and a split recorded when there was none would announce a
// second model to every job in a single-model session.
func TestPlanModelIsJournaledOnlyWhenThePlanSlotSplitsFromWork(t *testing.T) {
	plan := func(_ context.Context, compiled Compiled) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: "job", Brief: compiled.Goal, Stage: 1},
			{ID: "job-leaf", Parent: "job", Brief: "do it", Stage: 2},
		}}, nil
	}
	for _, probe := range []struct {
		name       string
		planSlot   string
		workSlot   string
		pinnedWork string
		want       string
	}{
		{"plan follows work", "x-ai/grok-5", "x-ai/grok-5", "", ""},
		{"no slots to speak of", "", "", "", ""},
		{"the slots differ", "anthropic/claude-opus-5", "moonshotai/kimi-k2", "", "anthropic/claude-opus-5"},
		{"a pin makes them differ", "x-ai/grok-5", "x-ai/grok-5", "moonshotai/kimi-k2", "x-ai/grok-5"},
		{"a pin makes them agree", "moonshotai/kimi-k2", "x-ai/grok-5", "moonshotai/kimi-k2", ""},
	} {
		t.Run(probe.name, func(t *testing.T) {
			graph := openStore(t)
			if _, err := graph.RequestCommand(store.Command{
				SessionID: "session-slots", Kind: store.CommandSplice, Instruction: "structure this",
			}); err != nil {
				t.Fatalf("request command: %v", err)
			}
			compile := func(context.Context, string, string) (Compiled, error) {
				return Compiled{Goal: "Structure this.", Scale: "task", WorkModel: probe.pinnedWork}, nil
			}
			reconciler := New(graph, compile, plan).
				WithModelsInForce(func() (string, string) { return probe.planSlot, probe.workSlot })
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatalf("tick: %v", err)
			}
			for _, id := range []string{"job", "job-leaf"} {
				node, found, err := graph.Node(id)
				if err != nil || !found {
					t.Fatalf("read %s: found=%t err=%v", id, found, err)
				}
				if node.Provenance.PlanModel != probe.want {
					t.Fatalf("%s plan model = %q, want %q", id, node.Provenance.PlanModel, probe.want)
				}
			}
		})
	}
}

// A reconciler nobody told about slots records nothing about them: every
// embedding path that predates the question keeps admitting exactly the
// provenance it always did.
func TestPlanModelStaysEmptyWithoutSlots(t *testing.T) {
	graph := openStore(t)
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "session-quiet", Kind: store.CommandSplice, Instruction: "read the file",
	}); err != nil {
		t.Fatalf("request command: %v", err)
	}
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Goal: "Read the file.", Scale: "lookup"}, nil
	}
	plan := func(_ context.Context, compiled Compiled) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{{ID: "quiet", Brief: compiled.Goal, Stage: 1}}}, nil
	}
	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	node, found, err := graph.Node("quiet")
	if err != nil || !found {
		t.Fatalf("read quiet: found=%t err=%v", found, err)
	}
	if node.Provenance.PlanModel != "" {
		t.Fatalf("plan model = %q, want silence", node.Provenance.PlanModel)
	}
}
