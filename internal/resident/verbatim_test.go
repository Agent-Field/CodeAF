package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The ask a script submits is the specification, and `aforge do` promises to
// run it rather than a better-worded version of it.
//
// The seam this holds is the one a benchmark cannot see from outside: the
// compiler's reading is the only goal the journal ever keeps, so a compile that
// "improves" the sentence means the harness measured something nobody wrote and
// has no record that it happened. The chat half of the same seam must not move
// — a conversation's whole value at this point is that it re-asks the question
// better, with its assumptions declared.
func TestAHeadlessErrandKeepsTheSubmittedAskAsItsGoalAndChatStillCompilesOne(t *testing.T) {
	const submitted = "Reconcile bank_export.csv against ledger.csv and flag every discrepancy"
	const reworded = "Produce a reconciliation report comparing the two ledgers"

	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{
			Goal:  reworded,
			Scale: "task", Parts: []string{"read both files", "compare them"},
			Title:       "Ledger reconciliation",
			Assumptions: []string{"assumed CSV headers on row one"},
		}, nil
	}

	for _, surface := range []struct {
		name    string
		oneShot bool
		goal    string
	}{
		{"errand", true, submitted},
		{"chat", false, reworded},
	} {
		t.Run(surface.name, func(t *testing.T) {
			graph := openStore(t)
			command, err := graph.RequestCommand(store.Command{
				SessionID: "s", Kind: store.CommandSplice, Instruction: submitted,
			})
			if err != nil {
				t.Fatal(err)
			}
			reconciler := New(graph, compile, nil)
			if surface.oneShot {
				reconciler = reconciler.WithOneShotErrands()
			}
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if resolved := commandBySeq(t, graph, command.Seq); resolved.Status != store.CommandApplied {
				t.Fatalf("command %s: %s", resolved.Status, resolved.Result)
			}
			work := spliceWork(t, graph)
			// Byte-equal on the errand surface, and it is the whole point: the
			// goal is not "close to" the ask, it is the ask. Chat's is the
			// compiler's sentence with its declared decisions anchored under it.
			if surface.oneShot && work.Brief != surface.goal {
				t.Fatalf("goal on the %s surface:\n got %q\nwant %q", surface.name, work.Brief, surface.goal)
			}
			if !strings.HasPrefix(work.Brief, surface.goal) {
				t.Fatalf("goal on the %s surface:\n got %q\nwant %q", surface.name, work.Brief, surface.goal)
			}
			// The compile is still consulted for everything the planner reads;
			// only the wording of the ask is refused.
			if work.Provenance.Intent != submitted {
				t.Fatalf("provenance intent %q", work.Provenance.Intent)
			}
			// An errand carries no declared assumption because there was no
			// question; chat's speculative one is the thing being kept.
			decisions := strings.Contains(work.Brief, WorkingDecisionsHeader)
			if surface.oneShot && decisions {
				t.Fatalf("an errand's goal carries injected assumptions:\n%s", work.Brief)
			}
			if !surface.oneShot && !decisions {
				t.Fatalf("chat dropped its declared assumptions:\n%s", work.Brief)
			}
		})
	}
}

// A compiler askback is a dead end on a surface with no keyboard: the run
// compiles, asks into an empty room, and hands its caller an interactive card
// where work was expected. Headless assumes the answer the compiler itself
// ranked first, declares it as a working decision, and does the job. Chat still
// stops and asks, because there someone can answer.
func TestAHeadlessErrandAssumesTheAnswerItCannotAskForAndChatStillAsks(t *testing.T) {
	const submitted = "Clean up the stale branches in this repository"
	const question = "Delete merged branches on the remote too?"

	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{
			Goal: "Prune stale git branches", Scale: "task", Question: question,
			QuestionOptions: []store.QuestionOption{
				{Label: "local only", Value: "local only"},
				{Label: "local and remote", Value: "local and remote"},
			},
		}, nil
	}

	t.Run("errand", func(t *testing.T) {
		graph := openStore(t)
		command, err := graph.RequestCommand(store.Command{
			SessionID: "s", Kind: store.CommandSplice, Instruction: submitted,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := New(graph, compile, nil).WithOneShotErrands().Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		resolved := commandBySeq(t, graph, command.Seq)
		if resolved.Status != store.CommandApplied {
			t.Fatalf("a headless errand stopped on a question it could have assumed: %s — %s",
				resolved.Status, resolved.Result)
		}
		work := spliceWork(t, graph)
		// The ask itself is untouched; the assumption arrives beside it as a
		// decision the leaf is held to, never as a rewrite of what was asked.
		if !strings.HasPrefix(work.Brief, submitted) {
			t.Fatalf("the submitted ask did not open the goal:\n%s", work.Brief)
		}
		if !strings.Contains(work.Brief, question) || !strings.Contains(work.Brief, "local only") {
			t.Fatalf("the assumed answer was not declared on the work:\n%s", work.Brief)
		}
	})

	t.Run("chat", func(t *testing.T) {
		graph := openStore(t)
		command, err := graph.RequestCommand(store.Command{
			SessionID: "s", Kind: store.CommandSplice, Instruction: submitted,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := New(graph, compile, nil).Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		resolved := commandBySeq(t, graph, command.Seq)
		if resolved.Status != store.CommandRejected || !strings.Contains(resolved.Result, "asked the user") {
			t.Fatalf("chat did not ask its question: %s — %s", resolved.Status, resolved.Result)
		}
	})
}

// spliceWork is the one node a bare splice admits — the work itself, never the
// spine and never a charter draft journaled beside it.
func spliceWork(t *testing.T, graph *store.Store) store.Node {
	t.Helper()
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	work := make([]store.Node, 0, 1)
	for _, node := range nodes {
		if node.ID == store.RootID || strings.HasPrefix(node.ID, "charter-") {
			continue
		}
		work = append(work, node)
	}
	if len(work) != 1 {
		t.Fatalf("expected one work node, got %d", len(work))
	}
	return work[0]
}
