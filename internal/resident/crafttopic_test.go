package resident

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The workflow from the live defect, in the shape it was distilled: three
// steps, a deep dive, a report at the end, and a subject baked into its name.
func spacexCraft() *craft.Workflow {
	return &craft.Workflow{
		Name:        "spacex-investment-research",
		Commit:      "e65f642aa11",
		Description: "A three-step deep dive on SpaceX as an investment, ending in a written report.",
		Params:      []craft.Param{{Name: "angle", Default: "valuation"}},
		Steps: []craft.Step{
			{ID: "gather", Brief: "Research the latest news, filings and coverage and settle what matters."},
			{ID: "analyse", Brief: "Work out what the findings mean for the {{angle}}.", Needs: []string{"gather"}},
			{ID: "write", Brief: "Write the deep-dive report, evidence under each finding.", Needs: []string{"analyse"}},
		},
		Limits: craft.Limits{CostUSD: 2.50},
	}
}

func matchedSpacex(score float64) *fakeShelf {
	return &fakeShelf{
		scored: []craft.Scored{{
			Summary: craft.Summary{Name: "spacex-investment-research"}, Score: score,
		}},
		workflows: map[string]*craft.Workflow{"spacex-investment-research": spacexCraft()},
	}
}

// The defect, end to end. A request to deep-dive a city's AI events scored
// decisively against a workflow distilled from investment research on one
// company — same shape, different subject — and the job ran through it. The
// planner takes it back, and nothing is said about a workflow the person never
// mentioned.
func TestAWrongSubjectDoesNotRunALearnedWayOfWorking(t *testing.T) {
	graph := openStore(t)
	shelf := matchedSpacex(50.0)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	seq, planned := craftSplice(t, graph, mind, "toronto-session",
		"do a deep dive on the AI events happening in Toronto this month and write it up")
	if !planned {
		t.Fatal("a workflow about another subject took the job instead of the planner")
	}
	if _, ok, _ := graph.Node(fmt.Sprintf("craft-%d", seq)); ok {
		t.Fatal("the wrong-subject workflow was compiled")
	}
	receipt := commandReceipt(t, graph, "toronto-session", seq)
	if strings.Contains(strings.ToLower(receipt.Body), "spacex") ||
		strings.Contains(strings.ToLower(receipt.Body), "way of doing this") {
		t.Fatalf("the person was told about a workflow they never mentioned: %q", receipt.Body)
	}
}

// The other half of the bargain: the workflow still answers the requests it is
// actually for, so precision here costs recall only where the subject is wrong.
func TestTheSameWorkflowStillRunsForItsOwnSubject(t *testing.T) {
	graph := openStore(t)
	mind := NewCraftMind(matchedSpacex(50.0), "/home/craft", fillsTopic, nil)

	seq, planned := craftSplice(t, graph, mind, "spacex-session",
		"another deep dive on SpaceX as an investment, same as before")
	if planned {
		t.Fatal("the workflow's own request went to the planner")
	}
	if _, ok, err := graph.Node(fmt.Sprintf("craft-%d", seq)); err != nil || !ok {
		t.Fatalf("the workflow did not run for its own subject: ok=%t err=%v", ok, err)
	}
}

// A craft-run job is named for the ASK, never for the machine that carried it.
// The live report is the user reading their board and asking "what are you
// doing with spacex?" about a job that was an AI-events report.
func TestACraftRunJobIsNamedForTheAskAndNotForTheCraft(t *testing.T) {
	graph := openStore(t)
	mind := NewCraftMind(matchedSpacex(50.0), "/home/craft", fillsTopic, nil)

	instruction := "deep dive on SpaceX as an investment for me"
	command, err := graph.RequestCommand(store.Command{
		SessionID: "named-session", Kind: store.CommandSplice, Instruction: instruction,
	})
	if err != nil {
		t.Fatal(err)
	}
	compile := func(_ context.Context, ask, _ string) (Compiled, error) {
		return Compiled{Goal: ask, Title: "SpaceX investment deep dive"}, nil
	}
	reconciler := New(graph, compile, nil).WithCraftMind(mind)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	root, ok, err := graph.Node(fmt.Sprintf("craft-%d", command.Seq))
	if err != nil || !ok {
		t.Fatalf("craft root: ok=%t err=%v", ok, err)
	}
	if root.Title != "SpaceX investment deep dive" {
		t.Fatalf("job title = %q, want the reading of the ask", root.Title)
	}
	// The craft's identity is not gone — it is where identity belongs.
	if root.Provenance.Craft != CraftRef(spacexCraft()) {
		t.Fatalf("provenance craft = %q", root.Provenance.Craft)
	}
	// And no node of the run wears the workflow's name as its title, whatever
	// the ask happened to say.
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if strings.Contains(strings.ToLower(node.Title), "spacex-investment-research") {
			t.Fatalf("node %q wears the workflow's name: %q", node.ID, node.Title)
		}
	}
}

// A run the person asked for BY NAME has no compiler to read the ask, so the
// ask itself names the job — still their words, still never the workflow's.
func TestANamedRunIsAlsoTitledFromTheRequest(t *testing.T) {
	graph := openStore(t)
	shelf := matchedSpacex(0)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	outcome, _ := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "verb-session", Kind: store.CommandCraftRun,
		Target: "spacex-investment-research", Instruction: "run it on the latest funding round",
	})
	if outcome.Status != store.CommandApplied {
		t.Fatalf("named run settled as %+v", outcome)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	titled := false
	for _, node := range nodes {
		if node.Parent != store.RootID || node.Provenance.Craft == "" {
			continue
		}
		titled = true
		if node.Title != "run it on the latest funding round" {
			t.Fatalf("named run title = %q, want the request's own words", node.Title)
		}
	}
	if !titled {
		t.Fatal("the named run left no job root to name")
	}
}

// Raw commit hashes are banned on every surface. The commissioned line is where
// one was living: "(v e65f642)" in front of a person who can do nothing with it.
func TestTheCommissionedLineCarriesNoVersionHash(t *testing.T) {
	// Seven or more hex characters as a whole word is what a short git hash
	// looks like on a line; nothing in this vocabulary legitimately does.
	hashShaped := regexp.MustCompile(`\b[0-9a-f]{7,}\b`)
	reconciler := &Reconciler{}
	lines := []string{
		craftCompileReceipt(spacexCraft()),
		reconciler.craftUseReceipt(spacexCraft(), true, 0),
		reconciler.craftUseReceipt(spacexCraft(), false, 2.13),
	}
	dirty := spacexCraft()
	dirty.Commit = "e65f642aa11+dirty-9f8e7d6c"
	lines = append(lines, craftCompileReceipt(dirty), reconciler.craftUseReceipt(dirty, false, 0))
	for _, line := range lines {
		if found := hashShaped.FindString(line); found != "" {
			t.Fatalf("a version hash reached the commissioned line as %q: %q", found, line)
		}
		if strings.Contains(line, "(v ") {
			t.Fatalf("the version clause is back: %q", line)
		}
		if !strings.Contains(line, "using your spacex-investment-research way of doing this") {
			t.Fatalf("the method mention was lost: %q", line)
		}
	}
	// The one version fact a person can act on survives, in words.
	if !strings.Contains(craftCompileReceipt(dirty), "an edit you haven't saved") {
		t.Fatalf("an unsaved run stopped saying so: %q", craftCompileReceipt(dirty))
	}
}
