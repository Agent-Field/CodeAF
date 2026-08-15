package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// correctionInstruction is what the head journals: the user's verbatim words,
// then the deterministic block naming the delivery being corrected.
func correctionInstruction(words, jobID, delivered string) string {
	return words + "\n\n" + CorrectionMarker + " " + jobID +
		"\nWhat was delivered:\n" + delivered +
		"\nFiles it wrote: /tmp/ws/report.md"
}

func landDeliveredJob(t *testing.T, graph *store.Store, id, summary string) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Brief: "write the quarterly report", Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "write me the quarterly report",
	}); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, id, summary)
}

func TestCorrectionOfDeliveredWorkBuildsOnWhatItCorrects(t *testing.T) {
	for _, test := range []struct {
		name   string
		folded bool
	}{
		{name: "inside the fold grace window"},
		{name: "after the job has been filed", folded: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := openStore(t)
			landDeliveredJob(t, graph, "report", "Q3 report written. Files:\n/tmp/ws/report.md")
			reconciler := New(graph, nil, nil)
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if test.folded {
				reconciler.now = func() time.Time { return time.Now().Add(2 * settledFoldGrace) }
				if err := reconciler.Tick(context.Background()); err != nil {
					t.Fatal(err)
				}
				node, _, err := graph.Node("report")
				if err != nil || !node.FoldRoot {
					t.Fatalf("setup: job did not fold: %+v err=%v", node, err)
				}
			}

			command, err := graph.RequestCommand(store.Command{
				SessionID: "s1", Kind: store.CommandSplice, Target: "report",
				Instruction: correctionInstruction("no, the margins are wrong", "report", "Q3 report written."),
			})
			if err != nil {
				t.Fatal(err)
			}
			newID := fmt.Sprintf("task-%d", command.Seq)
			var buildsOn []string
			compile := func(context.Context, string, string) (Compiled, error) {
				return Compiled{Goal: "Rewrite the quarterly report with correct margins", Scale: "task"}, nil
			}
			plan := func(_ context.Context, compiled Compiled) (store.Subtree, error) {
				buildsOn = append([]string(nil), compiled.BuildsOn...)
				return store.Subtree{Nodes: []store.NodeSpec{
					{ID: newID, Brief: compiled.Goal, Stage: 1},
				}}, nil
			}
			corrector := New(graph, compile, plan)
			corrector.now = reconciler.now
			if err := corrector.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if settled := commandBySeq(t, graph, command.Seq); settled.Status != store.CommandApplied {
				t.Fatalf("correction command = %+v", settled)
			}
			if len(buildsOn) != 1 || buildsOn[0] != "report" {
				t.Fatalf("a revision that cannot see what it revises: builds_on = %v", buildsOn)
			}
			// The edge is real, so the predecessor's digest and file pointers
			// reach the revision's leaves as dependency input.
			digests, err := graph.DependencyDigests(newID, 4096)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(digests, "\n")
			if !strings.Contains(joined, "/tmp/ws/report.md") {
				t.Fatalf("the revision was handed no sight of the files it is correcting: %q", joined)
			}
		})
	}
}

func TestOrdinarySpliceAtASettledJobStillCostsOnlyTheContinuity(t *testing.T) {
	graph := openStore(t)
	landDeliveredJob(t, graph, "report", "Q3 report written.")
	command, err := graph.RequestCommand(store.Command{
		SessionID: "s1", Kind: store.CommandSplice, Target: "report",
		Instruction: "now do the same for Q4",
	})
	if err != nil {
		t.Fatal(err)
	}
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Goal: "Write the Q4 report", Scale: "task"}, nil
	}
	seen := false
	plan := func(_ context.Context, compiled Compiled) (store.Subtree, error) {
		seen = true
		if len(compiled.BuildsOn) != 0 {
			return store.Subtree{}, fmt.Errorf("builds_on = %v, want none", compiled.BuildsOn)
		}
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: fmt.Sprintf("task-%d", command.Seq), Brief: compiled.Goal, Stage: 1},
		}}, nil
	}
	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("the splice never reached the planner")
	}
}

func TestTheDistillerIsShownTheCorrectionInTheUsersOwnWords(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "revision", Brief: "rewrite it", Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1",
		Intent: correctionInstruction("no, the margins are wrong", "report", "Q3 report written."),
	}); err != nil {
		t.Fatal(err)
	}

	var outcomes []string
	reconciler := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, _, outcome string, _ bool) ([]Learned, error) {
			outcomes = append(outcomes, outcome)
			return nil, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, "revision", "Rewritten with correct margins.")
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("distillations = %d", len(outcomes))
	}
	if !strings.Contains(outcomes[0], "no, the margins are wrong") {
		t.Fatalf("the distiller was never shown the correction: %q", outcomes[0])
	}
	if !strings.Contains(outcomes[0], "Record the standard, not the episode.") {
		t.Fatalf("the correction block lost its instruction: %q", outcomes[0])
	}
	// The deterministic block is machinery, not the user's standard.
	if strings.Contains(outcomes[0], CorrectionMarker) {
		t.Fatalf("the head's marker leaked into the notebook prompt: %q", outcomes[0])
	}
}
