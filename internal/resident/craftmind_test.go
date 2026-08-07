package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// fakeShelf scripts the craft repository's answers. Recognition is a decision
// about scores and survival, not about git: what a real repository does with a
// version is the repository's own test.
type fakeShelf struct {
	scored    []craft.Scored
	workflows map[string]*craft.Workflow
	summaries []craft.Summary
	versions  map[string][]craft.Version
	requests  []string
	saved     []fakeSaved
}

type fakeSaved struct {
	name    string
	message string
}

func (f *fakeShelf) Match(request string, k int) []craft.Scored {
	f.requests = append(f.requests, request)
	if len(f.scored) > k {
		return f.scored[:k]
	}
	return f.scored
}

func (f *fakeShelf) Load(name string) (*craft.Workflow, error) {
	workflow, ok := f.workflows[name]
	if !ok {
		return nil, fmt.Errorf("no workflow named %s", name)
	}
	return workflow, nil
}

func (f *fakeShelf) List() ([]craft.Summary, error) { return f.summaries, nil }

func (f *fakeShelf) History(name string, limit int) ([]craft.Version, error) {
	return f.versions[name], nil
}

func (f *fakeShelf) Save(workflow *craft.Workflow, message string) (string, error) {
	f.saved = append(f.saved, fakeSaved{name: workflow.Name, message: message})
	return "deadbeef", nil
}

func matchedPresentation(score float64) *fakeShelf {
	return &fakeShelf{
		scored:    []craft.Scored{{Summary: craft.Summary{Name: "presentation"}, Score: score}},
		workflows: map[string]*craft.Workflow{"presentation": presentationCraft()},
	}
}

func fillsTopic(_ context.Context, instruction string, _ *craft.Workflow) (map[string]string, error) {
	return map[string]string{"topic": strings.TrimSpace(instruction)}, nil
}

// craftSplice runs one instruction through the whole splice path and reports
// whether the planner was reached.
func craftSplice(t *testing.T, graph *store.Store, mind *CraftMind, sessionID, instruction string) (int64, bool) {
	t.Helper()
	command, err := graph.RequestCommand(store.Command{
		SessionID: sessionID, Kind: store.CommandSplice, Instruction: instruction,
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	planned := false
	plan := func(ctx context.Context, compiled Compiled) (store.Subtree, error) {
		planned = true
		anchor, _ := PlanAnchorFromContext(ctx)
		return store.Subtree{Nodes: []store.NodeSpec{{ID: anchor.NodeID, Brief: compiled.Goal, Stage: 1}}}, nil
	}
	reconciler := New(graph, nil, plan).WithCraftMind(mind)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	return command.Seq, planned
}

// A request that decisively matches stored know-how runs that know-how: the
// planner is never reached, the nodes are the workflow's steps, and every one
// of them names the version that produced it.
func TestDecisiveMatchCompilesTheCraftInsteadOfPlanning(t *testing.T) {
	graph := openStore(t)
	shelf := matchedPresentation(5.0)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	seq, planned := craftSplice(t, graph, mind, "craft-session", "make me a presentation about the Q3 numbers")
	if planned {
		t.Fatal("the planner ran on a decisive craft match")
	}
	prefix := fmt.Sprintf("craft-%d", seq)
	for _, id := range []string{prefix, prefix + "~research", prefix + "~assemble"} {
		node, ok, err := graph.Node(id)
		if err != nil || !ok {
			t.Fatalf("node %q: ok=%t err=%v", id, ok, err)
		}
		if node.Provenance.Craft != "presentation@abc1234def" {
			t.Fatalf("node %q craft = %q", id, node.Provenance.Craft)
		}
	}
	root, _, err := graph.Node(prefix + "~research")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(root.Brief, "make me a presentation about the Q3 numbers") {
		t.Fatalf("the extracted param did not reach the brief: %q", root.Brief)
	}

	receipt := commandReceipt(t, graph, "craft-session", seq)
	if !strings.Contains(receipt.Body, "using your presentation craft v abc1234 — 4 steps, ~$1.50 cap") {
		t.Fatalf("receipt = %q", receipt.Body)
	}
}

// A match that only brushes the shelf is not an answer. The floor is what
// makes a miss read as a miss, and everything below the decisive line plans.
func TestWeakMatchPlansFreeform(t *testing.T) {
	graph := openStore(t)
	shelf := matchedPresentation(CraftDecisiveScore - 0.2)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	seq, planned := craftSplice(t, graph, mind, "weak-session", "summarize this file for me")
	if !planned {
		t.Fatal("a weak match did not reach the planner")
	}
	if len(shelf.requests) != 1 || shelf.requests[0] != "summarize this file for me" {
		t.Fatalf("shelf saw %+v — recognition reads the user's own words", shelf.requests)
	}
	if _, ok, err := graph.Node(fmt.Sprintf("craft-%d", seq)); err != nil || ok {
		t.Fatalf("a craft ran on a weak match: ok=%t err=%v", ok, err)
	}
}

// "from scratch" is the one guard. It skips matching entirely rather than
// re-ranking it, so the user never has to argue with a score.
func TestFromScratchSkipsCraftMatching(t *testing.T) {
	graph := openStore(t)
	shelf := matchedPresentation(50.0)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	seq, planned := craftSplice(t, graph, mind, "fresh-session",
		"make me a presentation about the Q3 numbers, from scratch this time")
	if !planned {
		t.Fatal("the planner did not run after the user asked for a fresh plan")
	}
	if len(shelf.requests) != 0 {
		t.Fatalf("the shelf was consulted anyway: %+v", shelf.requests)
	}
	if _, ok, _ := graph.Node(fmt.Sprintf("craft-%d", seq)); ok {
		t.Fatal("a craft ran despite the fresh-plan guard")
	}
	// The guard is a word test, not a substring test.
	if craftDeclined("refresh the cached numbers and rebuild the deck") {
		t.Fatal("refresh was read as fresh")
	}
}

// A craft whose required values are not in the request is not what was asked
// for. It falls through to the planner silently: no question, no receipt about
// a workflow the user never mentioned.
func TestMissingParamsFallsThroughSilently(t *testing.T) {
	graph := openStore(t)
	shelf := matchedPresentation(50.0)
	empty := func(context.Context, string, *craft.Workflow) (map[string]string, error) {
		return map[string]string{}, nil
	}
	mind := NewCraftMind(shelf, "/home/craft", empty, nil)

	seq, planned := craftSplice(t, graph, mind, "params-session", "make me a presentation")
	if !planned {
		t.Fatal("the planner did not run when the craft's params were missing")
	}
	receipt := commandReceipt(t, graph, "params-session", seq)
	if strings.Contains(strings.ToLower(receipt.Body), "craft") {
		t.Fatalf("the user was told about a craft that did not run: %q", receipt.Body)
	}
	messages, err := graph.Messages("params-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if len(message.Options) > 0 {
			t.Fatalf("a missing param became a question: %q", message.Body)
		}
	}
}

// A forged craft has never run. Below the overwhelming line it waits to be
// named; one clean landing is all it takes to become ordinary know-how.
func TestDraftCraftWaitsForEvidenceOrAnOverwhelmingMatch(t *testing.T) {
	graph := openStore(t)
	shelf := matchedPresentation(CraftOverwhelmingScore - 0.5)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	seq, planned := craftSplice(t, graph, mind, "draft-session", "make me a presentation about Q3")
	if !planned {
		t.Fatal("an untried draft was used on an ordinary match")
	}
	if _, ok, _ := graph.Node(fmt.Sprintf("craft-%d", seq)); ok {
		t.Fatal("an untried draft ran unasked")
	}

	// One settled run is the evidence the draft was missing.
	New(graph, nil, nil).recordCraftOutcome(store.Node{
		ID: "earlier", Parent: store.RootID,
		Provenance: store.Provenance{Craft: "presentation@abc1234def"},
	}, true)

	seq, planned = craftSplice(t, graph, mind, "draft-session", "make me a presentation about Q3")
	if planned {
		t.Fatal("a proven craft still went to the planner")
	}
	if _, ok, err := graph.Node(fmt.Sprintf("craft-%d", seq)); err != nil || !ok {
		t.Fatalf("proven craft did not run: ok=%t err=%v", ok, err)
	}
}

// Naming a draft outright is the other way in — the user asked for the file,
// so its lack of a record is not an objection.
func TestNamingACraftOutrightUsesTheDraft(t *testing.T) {
	graph := openStore(t)
	shelf := matchedPresentation(CraftDecisiveScore)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	seq, planned := craftSplice(t, graph, mind, "named-session",
		"use the presentation craft for the Q3 numbers")
	if planned {
		t.Fatal("a named craft went to the planner")
	}
	if _, ok, err := graph.Node(fmt.Sprintf("craft-%d", seq)); err != nil || !ok {
		t.Fatalf("named craft did not run: ok=%t err=%v", ok, err)
	}
}

// A craft run's landing is evidence about the version that ran it. A clean
// settle counts for it, a failure against it, and both are readable from the
// store alone.
func TestCraftSurvivalRecordsForAndAgainstOnSettle(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil).WithCraftMind(NewCraftMind(matchedPresentation(0), "", nil, nil))
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	provenance := store.Provenance{
		Origin: store.OriginUser, SessionID: "survival", Intent: "make me a deck",
		Craft: "presentation@abc1234def",
	}
	for _, id := range []string{"settled", "broken"} {
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: id, Brief: "deliver the deck", Stage: 1,
		}}}, provenance); err != nil {
			t.Fatalf("splice %s: %v", id, err)
		}
	}
	claim, won, err := graph.Claim("settled", "worker")
	if err != nil || !won {
		t.Fatalf("claim settled: won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "the deck is in /tmp/deck.md"); err != nil {
		t.Fatal(err)
	}
	claim, won, err = graph.Claim("broken", "worker")
	if err != nil || !won {
		t.Fatalf("claim broken: won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fail(claim, "the verifier never passed"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	version := reconciler.craftSurvival("presentation@abc1234def")
	if version.For != 1 || version.Against != 1 {
		t.Fatalf("version record = %+v, want one for and one against", version)
	}
	// The bare name is what a refinement inherits: a craft that has proven
	// itself does not go back to being a draft when it improves.
	if !reconciler.craftProven("presentation") {
		t.Fatal("a settled run did not make the craft proven")
	}
}

// The arrival brief is where know-how forged in the user's absence is
// mentioned — once, in the same row a learned skill rides, and as a promise
// about next time rather than a report about this one.
func TestBriefCarriesTheForgedCraftLine(t *testing.T) {
	graph := openStore(t)
	if _, err := graph.TouchSeen("tui", "old", store.SeenDetached); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact(store.RootID, "repo:aforge", store.FactPlain,
		"The release branch is stable."); err != nil {
		t.Fatal(err)
	}
	shelf := matchedPresentation(0)
	shelf.summaries = []craft.Summary{{
		Name: "presentation", Description: "a deck about a topic",
		Commit: "abc1234def", When: time.Now().Add(time.Hour),
	}}
	shelf.versions = map[string][]craft.Version{"presentation": {{
		Commit: "abc1234def", Subject: "presentation: forged from job task-12: build the Q3 deck",
	}}}

	var received BriefActivity
	compose := func(_ context.Context, activity BriefActivity) (BriefDraft, error) {
		received = activity
		return BriefDraft{Headline: "While you were away: one craft appeared."}, nil
	}
	reconciler := New(graph, nil, nil).
		WithBriefComposer(compose).
		WithCraftMind(NewCraftMind(shelf, "", nil, nil))
	if err := reconciler.SessionOpened(context.Background(), "arrival", "tui", 0); err != nil {
		t.Fatal(err)
	}
	if received.CraftsForged != 1 {
		t.Fatalf("brief activity = %+v, want one forged craft", received)
	}
	forged := ""
	for _, event := range received.Events {
		if event.Kind == store.BriefSkill {
			forged = event.Text
		}
	}
	if !strings.Contains(forged, "Forged the presentation craft from job task-12") ||
		!strings.Contains(forged, "it'll be used next time") {
		t.Fatalf("forged line = %q", forged)
	}
	messages, err := graph.Messages("arrival", 0, 0)
	if err != nil || len(messages) != 1 || messages[0].Brief == nil {
		t.Fatalf("arrival messages = %+v err=%v", messages, err)
	}
	if !strings.Contains(strings.Join(briefItemBodies(messages[0].Brief.Items), "\n"), "Forged the presentation craft") {
		t.Fatalf("brief items = %+v", messages[0].Brief.Items)
	}
}

func briefItemBodies(items []store.BriefItem) []string {
	bodies := make([]string, 0, len(items))
	for _, item := range items {
		bodies = append(bodies, item.Body)
	}
	return bodies
}

// A shelf that cannot be read is not an error anywhere: craft is dormant and
// the resident plans exactly as it did before there was one.
func TestCraftMindWithoutAShelfIsInert(t *testing.T) {
	if NewCraftMind(nil, "", nil, nil) != nil {
		t.Fatal("a mind was built without a shelf")
	}
	graph := openStore(t)
	if _, planned := craftSplice(t, graph, nil, "inert-session", "make me a presentation"); !planned {
		t.Fatal("the planner did not run without a craft mind")
	}
}
