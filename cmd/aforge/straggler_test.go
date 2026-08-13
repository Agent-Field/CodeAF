package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// stragglerProfile writes a real profile file so the threshold is derived the
// way it is derived in a live run: off disk, by profile.Load, from records that
// were journaled as leaves landed.
func stragglerProfile(t *testing.T, tokens ...int) config.Config {
	t.Helper()
	dir := t.TempDir()
	settings := config.Config{ProfileDir: dir, Model: "vendor/model"}
	measured, err := profile.Load(dir, settings.Model, "linear")
	if err != nil {
		t.Fatal(err)
	}
	records := make([]profile.Record, 0, len(tokens))
	for index, count := range tokens {
		records = append(records, profile.Record{
			Title: fmt.Sprintf("sibling %d", index), Size: "atomic", Turns: 6, Tokens: count, Cost: 0.004,
		})
	}
	measured.Add(records...)
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
	return settings
}

// stragglerStore is one job with one leaf, so the evidence has a node to be
// journaled against.
func stragglerStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "straggler.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "write the report", Title: "Report"},
		{ID: "job-n1", Parent: "job", Brief: "draft the findings", Title: "Draft"},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s", Intent: "write the report",
	}); err != nil {
		t.Fatal(err)
	}
	return graph
}

// Nothing is derived from nothing, at the wiring level. A profile below its
// evidence gate installs no watch at all, which is what makes a fresh machine
// behave exactly as it did before any of this existed.
func TestNoWatchIsInstalledWithoutEnoughMeasurement(t *testing.T) {
	settings := stragglerProfile(t, 4_000, 4_100, 4_200)
	if watch := stragglerWatch(settings, settings.Model, stragglerJudge{worker: "linear"}); watch != nil {
		t.Fatalf("a threshold was derived from three leaves: %+v", watch)
	}
	// And an unwritten profile directory is the same answer.
	bare := config.Config{ProfileDir: t.TempDir(), Model: "vendor/model"}
	if watch := stragglerWatch(bare, bare.Model, stragglerJudge{worker: "linear"}); watch != nil {
		t.Fatalf("a threshold was derived from an empty profile: %+v", watch)
	}
}

// The measured incident, end to end through the wiring the runner installs: a
// scripted leaf runs past what its own worker's record supports, the comparison
// is journaled against the node, and the leaf carries on because nobody could
// name a better home for it.
//
// The journal write is the half that matters most here. The incident this was
// built for cost 31% of a run and left no trace at all, so a threshold that
// fires and is overruled must be as visible afterwards as one that fires and is
// upheld — otherwise the record only ever shows the cases where the mechanism
// looked right.
func TestAStragglerJournalsItsEvidenceAndReachesTheJudgePath(t *testing.T) {
	settings := stragglerProfile(t, 12, 13, 14, 14, 15, 16, 18, 24)
	graph := stragglerStore(t)
	node, found, err := graph.Node("job-n1")
	if err != nil || !found {
		t.Fatalf("leaf: found=%v err=%v", found, err)
	}

	chose := &stragglerHandoff{}
	watch := stragglerWatch(settings, settings.Model, stragglerJudge{
		ctx:      context.Background(),
		settings: settings,
		graph:    graph,
		// No client and no menu: there is nowhere to move the work to, so the
		// judge is never called and the answer is "the worker it is with should
		// finish it". The evidence is journaled all the same.
		client: nil,
		node:   node,
		worker: "linear",
		brief:  node.Brief,
		model:  func() string { return settings.Model },
		chose:  chose.set,
	})
	if watch == nil {
		t.Fatal("eight measured siblings derived no threshold")
	}
	if watch.Threshold <= watch.Anchor || watch.Samples != 8 {
		t.Fatalf("the derived watch is not a comparison against the record: %+v", watch)
	}

	// A leaf that keeps working. Each scripted turn costs 15 raw tokens, so a
	// threshold derived from siblings costing a dozen or two is crossed early
	// and the leaf has turns to spare.
	client := &stragglerCompleter{turns: 12}
	linear := exec.NewLinear(client, execWorkspace(t), nil, 20, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), exec.Task{
		NodeID: 1, NodeKey: node.ID, Brief: node.Brief, Overrun: watch,
	})
	if err != nil {
		t.Fatal(err)
	}

	recorded, err := graph.OverrunEvidenceFor(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 1 {
		t.Fatalf("overrun evidence rows = %d, want exactly one", len(recorded))
	}
	evidence := recorded[0]
	if evidence.Spent < watch.Threshold {
		t.Fatalf("the journaled spend %d is under the threshold %d it supposedly crossed",
			evidence.Spent, watch.Threshold)
	}
	// Both sides of the comparison survived into the record, which is what makes
	// it evidence rather than a number.
	if evidence.Anchor != watch.Anchor || evidence.Threshold != watch.Threshold ||
		evidence.Samples != watch.Samples || evidence.Multiple != watch.Multiple {
		t.Fatalf("the record lost the derivation: %+v against %+v", evidence, watch)
	}
	if evidence.Worker != "linear" {
		t.Fatalf("worker = %q, want who was running it", evidence.Worker)
	}
	// "Carry on" is written out. An empty verdict in a journal is
	// indistinguishable from a field nobody filled in.
	if evidence.Verdict != "continue" || evidence.Chosen != "" {
		t.Fatalf("verdict = %q chosen = %q, want an overruled threshold said out loud",
			evidence.Verdict, evidence.Chosen)
	}
	if got := chose.take(); got != "" {
		t.Fatalf("a worker %q was chosen with no judge to choose it", got)
	}
	// And the leaf was not touched: no cap, no kill, no landing.
	if outcome.Stop != exec.StopDone {
		t.Fatalf("stop = %s — an unanswerable threshold acted as a cap", outcome.Stop)
	}
	if outcome.Overran() {
		t.Fatal("a leaf that finished under its own power was recorded as having work left")
	}
}

// The other arm: a judge that names somewhere better for the work lands the
// leaf on the ending the escalation path reads, and the choice is handed to the
// runner so the same question is not paid for twice.
func TestAJudgedHandBackRecordsTheChoiceAndLandsForEscalation(t *testing.T) {
	settings := stragglerProfile(t, 12, 13, 14, 14, 15, 16, 18, 24)
	graph := stragglerStore(t)
	node, _, err := graph.Node("job-n1")
	if err != nil {
		t.Fatal(err)
	}
	chose := &stragglerHandoff{}
	judge := stragglerJudge{
		ctx: context.Background(), settings: settings, graph: graph, node: node,
		worker: "linear", brief: node.Brief,
		chose: chose.set,
	}
	watch := stragglerWatch(settings, settings.Model, judge)
	if watch == nil {
		t.Fatal("eight measured siblings derived no threshold")
	}
	// Stand in for the retry judge naming a specialist. The wiring under test is
	// everything either side of that call: the journal, the choice handed back,
	// and the ending the leaf lands on.
	watch.Judge = func(evidence exec.OverrunEvidence) exec.OverrunVerdict {
		judge.journal(evidence, "hand-back", "swe")
		judge.chose("swe")
		return exec.OverrunHandBack
	}

	client := &stragglerCompleter{turns: 30}
	linear := exec.NewLinear(client, execWorkspace(t), nil, 40, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), exec.Task{
		NodeID: 1, NodeKey: node.ID, Brief: node.Brief, Overrun: watch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := chose.take(); got != "swe" {
		t.Fatalf("the runner was handed %q, so the retry loop will ask the same question again", got)
	}
	if outcome.Stop != exec.StopOverrun || !outcome.Overran() || !outcome.Verdict.Escalates() {
		t.Fatalf("a handed-back leaf does not reach the escalation path: %+v", outcome)
	}
	recorded, err := graph.OverrunEvidenceFor(node.ID)
	if err != nil || len(recorded) != 1 {
		t.Fatalf("overrun evidence rows = %d err = %v", len(recorded), err)
	}
	if recorded[0].Verdict != "hand-back" || recorded[0].Chosen != "swe" {
		t.Fatalf("the record does not say where the work went: %+v", recorded[0])
	}
}

// The finding handed to the judge is the comparison, not a token count. A judge
// told only "this leaf spent 150,000 tokens" is being asked to decide against a
// number with no scale behind it.
func TestTheFindingGivenToTheJudgeCarriesBothSides(t *testing.T) {
	finding := stragglerFinding(exec.OverrunEvidence{
		Spent: 150_000, Turns: 34, Anchor: 4_200, Threshold: 9_800, Samples: 11,
	}).Error()
	for _, want := range []string{"still running", "150000", "34 turns", "4200", "11 runs", "9800"} {
		if !strings.Contains(finding, want) {
			t.Fatalf("the finding is missing %q:\n%s", want, finding)
		}
	}
}

// The invoice reaches the planner only from real measurements, and it reaches it
// through the same profile files everything else reads.
func TestTheMeasuredInvoiceIsEmptyUntilThereIsSomethingToInvoice(t *testing.T) {
	bare := config.Config{ProfileDir: t.TempDir(), Model: "vendor/model"}
	if got := measuredInvoice(bare, bare.Model); got != "" {
		t.Fatalf("an unmeasured machine rendered a price list:\n%s", got)
	}
	settings := stragglerProfile(t, 40_000, 40_000, 40_000, 40_000, 40_000, 40_000, 40_000, 40_000)
	rendered := measuredInvoice(settings, settings.Model)
	if !strings.Contains(rendered, "MEASURED HERE") || !strings.Contains(rendered, "40,000 tokens") {
		t.Fatalf("a measured machine did not render its own numbers:\n%s", rendered)
	}
}

// stragglerCompleter keeps a leaf working for a fixed number of turns and then
// finishes, so the only thing that can end it sooner is the straggler question.
type stragglerCompleter struct {
	turns int
	seen  int
}

func (s *stragglerCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	index := s.seen
	s.seen++
	message := ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "working"}}}
	if index < s.turns {
		message.ToolCalls = []ai.ToolCall{{
			ID: fmt.Sprintf("c%d", index), Type: "function",
			Function: ai.ToolCallFunction{
				Name:      "write",
				Arguments: fmt.Sprintf(`{"path":"out-%d.txt","text":"x"}`, index),
			},
		}}
	} else {
		message.Content = []ai.ContentPart{{Type: "text", Text: "done"}}
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: message, FinishReason: "stop"}},
		Usage:   &ai.Usage{PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

// execWorkspace is a scratch workspace for a scripted leaf, built the way the
// runner builds one.
func execWorkspace(t *testing.T) *exec.Workspace {
	t.Helper()
	root := filepath.Join(t.TempDir(), "space")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	space, err := exec.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return space
}
