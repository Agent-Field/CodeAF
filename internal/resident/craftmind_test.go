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
	return craftSpliceCommand(t, graph, mind, store.Command{
		SessionID: sessionID, Kind: store.CommandSplice, Instruction: instruction,
	})
}

// craftSpliceFresh is the same path with the head's own reading of the person's
// intent riding the work order, which is what "don't use the template this
// time" becomes by the time the craft engine sees it.
func craftSpliceFresh(t *testing.T, graph *store.Store, mind *CraftMind, sessionID, instruction string) (int64, bool) {
	t.Helper()
	return craftSpliceCommand(t, graph, mind, store.Command{
		SessionID: sessionID, Kind: store.CommandSplice, Instruction: instruction, Fresh: true,
	})
}

func craftSplice2(t *testing.T, splice func(*testing.T, *store.Store, *CraftMind, string, string) (int64, bool),
	graph *store.Store, mind *CraftMind, sessionID, instruction string) (int64, bool) {
	t.Helper()
	return splice(t, graph, mind, sessionID, instruction)
}

func craftSpliceCommand(t *testing.T, graph *store.Store, mind *CraftMind, request store.Command) (int64, bool) {
	t.Helper()
	command, err := graph.RequestCommand(request)
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
	if !strings.Contains(receipt.Body, "using your presentation way of doing this — 4 steps, ~$1.50 cap") {
		t.Fatalf("receipt = %q", receipt.Body)
	}
	// The version identity that used to ride this line is machine identity and
	// is kept where machine identity lives.
	if strings.Contains(receipt.Body, "abc1234") {
		t.Fatalf("a commit hash reached the commissioned line: %q", receipt.Body)
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

// The opt-out is a reading of intent now, not a phrase list. The head decides
// what "don't use the template this time" meant and carries the answer on the
// work order; the frozen spellings still work, and either way the person hears
// one plain line saying their sentence changed what happened. Silence was the
// old answer, and silence after asking for something else reads as being
// ignored.
func TestAskingForItFromScratchSetsTheLearnedWayAsideAndSaysSo(t *testing.T) {
	for name, splice := range map[string]func(*testing.T, *store.Store, *CraftMind, string, string) (int64, bool){
		"the head read the intent": craftSpliceFresh,
		"the frozen spelling":      craftSplice,
	} {
		t.Run(name, func(t *testing.T) {
			graph := openStore(t)
			shelf := matchedPresentation(50.0)
			mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

			instruction := "make me a presentation about the Q3 numbers"
			if name == "the frozen spelling" {
				instruction += ", from scratch this time"
			}
			seq, planned := craftSplice2(t, splice, graph, mind, "fresh-session", instruction)
			if !planned {
				t.Fatal("the planner did not run after the user asked for a fresh plan")
			}
			if _, ok, _ := graph.Node(fmt.Sprintf("craft-%d", seq)); ok {
				t.Fatal("a learned way of working ran despite the opt-out")
			}
			receipt := commandReceipt(t, graph, "fresh-session", seq)
			if !strings.Contains(receipt.Body, "Working this one out from scratch, as you asked") {
				t.Fatalf("the opt-out was silent: %q", receipt.Body)
			}
			if strings.Contains(strings.ToLower(receipt.Body), "craft") {
				t.Fatalf("the receipt used the word we invented: %q", receipt.Body)
			}
		})
	}
	// The frozen spelling is still a word test, not a substring test.
	if craftDeclined("refresh the cached numbers and rebuild the deck") {
		t.Fatal("refresh was read as fresh")
	}
	// Nothing was set aside, so nothing is said about setting anything aside.
	graph := openStore(t)
	mind := NewCraftMind(matchedPresentation(CraftDecisiveScore-0.2), "/home/craft", fillsTopic, nil)
	seq, _ := craftSpliceFresh(t, graph, mind, "quiet-session", "summarize this file, from scratch")
	if receipt := commandReceipt(t, graph, "quiet-session", seq); strings.Contains(receipt.Body, "from scratch, as you asked") {
		t.Fatalf("a miss announced an opt-out from nothing: %q", receipt.Body)
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

// The draft trap: a workflow that never fires never becomes proven, and an
// unproven one needed an overwhelming match to fire. Measured against the real
// scorer, a realistic repeat of the phrasing behind a stored workflow scores
// 3.98 against a 4.0 line — so whether a draft ever escaped was decided by the
// name the distiller happened to pick against the words the user happens to
// habitually use, and a draft that fell short could NEVER earn the run that
// would prove it.
//
// The fix is evidence in three states rather than two. Untried means no record
// at all, and an untried draft gets exactly one provisional run at the decisive
// bar, bounded by the workflow's own clamped ceilings and announced as a first
// time. After that the evidence decides: a clean landing makes it ordinary
// know-how, and a failure puts it behind the overwhelming bar where it stays
// until it is asked for by name.
func TestAnUntriedDraftGetsItsFirstRunAndThenTheEvidenceDecides(t *testing.T) {
	// 3.98 against a 4.0 bar is the measured case. It runs now.
	graph := openStore(t)
	shelf := matchedPresentation(CraftOverwhelmingScore - 0.02)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	seq, planned := craftSplice(t, graph, mind, "draft-session", "make me a presentation about Q3")
	if planned {
		t.Fatal("an untried draft two hundredths short of the old bar still never ran")
	}
	if _, ok, err := graph.Node(fmt.Sprintf("craft-%d", seq)); err != nil || !ok {
		t.Fatalf("the provisional first run did not happen: ok=%t err=%v", ok, err)
	}
	receipt := commandReceipt(t, graph, "draft-session", seq)
	if !strings.Contains(receipt.Body, "first time working this way") ||
		!strings.Contains(receipt.Body, `say "from scratch" if you'd rather I plan it`) {
		t.Fatalf("the provisional run did not say it was provisional: %q", receipt.Body)
	}

	// A draft that failed its one chance is back behind the overwhelming bar.
	failed := openStore(t)
	failedMind := NewCraftMind(matchedPresentation(CraftOverwhelmingScore-0.02), "/home/craft", fillsTopic, nil)
	New(failed, nil, nil).recordCraftOutcome(store.Node{
		ID: "earlier", Parent: store.RootID,
		Provenance: store.Provenance{Craft: "presentation@abc1234def"},
	}, false)
	seq, planned = craftSplice(t, failed, failedMind, "failed-session", "make me a presentation about Q3")
	if !planned {
		t.Fatal("a draft that failed its first run was used again unasked")
	}
	if _, ok, _ := failed.Node(fmt.Sprintf("craft-%d", seq)); ok {
		t.Fatal("a draft with a run against it ran on an ordinary match")
	}

	// One settled run makes it ordinary know-how, and the receipt then quotes
	// what that run actually cost instead of announcing a first time.
	proven := openStore(t)
	provenMind := NewCraftMind(matchedPresentation(CraftDecisiveScore), "/home/craft", fillsTopic, nil)
	reconciler := New(proven, nil, nil)
	if err := proven.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "earlier", Brief: "deliver the deck", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "prior", Intent: "a deck",
		Craft: "presentation@abc1234def"}); err != nil {
		t.Fatal(err)
	}
	if err := proven.RecordUsage(store.NodeUsage{NodeID: "earlier", Model: "m", Cost: 0.38}); err != nil {
		t.Fatal(err)
	}
	earlier, _, err := proven.Node("earlier")
	if err != nil {
		t.Fatal(err)
	}
	reconciler.recordCraftOutcome(earlier, true)
	if record := reconciler.craftSurvival("presentation"); record.LastCost != 0.38 {
		t.Fatalf("survival record = %+v, want the prior run's cost", record)
	}

	seq, planned = craftSplice(t, proven, provenMind, "proven-session", "make me a presentation about Q3")
	if planned {
		t.Fatal("a proven way of working still went to the planner")
	}
	receipt = commandReceipt(t, proven, "proven-session", seq)
	if !strings.Contains(receipt.Body, "last time $0.38") {
		t.Fatalf("the receipt did not quote what the last run cost: %q", receipt.Body)
	}
	if strings.Contains(receipt.Body, "first time working this way") {
		t.Fatalf("a proven way of working still announced a first time: %q", receipt.Body)
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
// mentioned — once, on a row of its OWN kind, and as a promise about next time
// rather than a report about this one.
//
// The kind is the half of this that was wrong for a wave: a way of working rode
// as store.BriefSkill, so the brief said the same word about a four-step
// workflow and a twenty-line script, and no lens reading the fold could tell
// them apart or send a reader to the right page for either.
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
			t.Fatalf("a way of working is riding as a skill again: %q", event.Text)
		}
		if event.Kind == store.BriefCraft {
			forged = event.Text
		}
	}
	if !strings.Contains(forged, "Learned how to do presentation from job task-12") ||
		!strings.Contains(forged, "I'll work this way next time") {
		t.Fatalf("forged line = %q", forged)
	}
	messages, err := graph.Messages("arrival", 0, 0)
	if err != nil || len(messages) != 1 || messages[0].Brief == nil {
		t.Fatalf("arrival messages = %+v err=%v", messages, err)
	}
	if !strings.Contains(strings.Join(briefItemBodies(messages[0].Brief.Items), "\n"), "Learned how to do presentation") {
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
