package resident

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// revertingShelf is a fake shelf that can also put a version back — the
// capability *craft.Repo has and the [CraftShelf] interface deliberately does
// not, so a shelf that cannot revert says so rather than pretending.
type revertingShelf struct {
	*fakeShelf
	reverted []string
	fail     error
}

func (r *revertingShelf) Revert(name, toCommit, message string) (string, error) {
	if r.fail != nil {
		return "", r.fail
	}
	r.reverted = append(r.reverted, name+"@"+toCommit+": "+message)
	return "newcommit", nil
}

// applyCraftVerb drains one command through the reconciler exactly as the
// resident's own tick does, and hands back the receipt the person reads.
func applyCraftVerb(t *testing.T, graph *store.Store, mind *CraftMind, request store.Command) (store.Command, string) {
	t.Helper()
	requested, err := graph.RequestCommand(request)
	if err != nil {
		t.Fatalf("request %s: %v", request.Kind, err)
	}
	reconciler := New(graph, nil, nil).WithCraftMind(mind)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	settled, found, err := graph.CommandBySeq(requested.Seq)
	if err != nil || !found {
		t.Fatalf("command %d: found=%v err=%v", requested.Seq, found, err)
	}
	messages, err := graph.Messages(request.SessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	said := make([]string, 0, len(messages))
	for _, message := range messages {
		said = append(said, message.Body)
	}
	return settled, strings.Join(said, "\n")
}

// Running a way of working BY NAME compiles the same subtree recognition
// compiles — same loader, same parameter seam, same compiler — and admits it as
// ordinary work under the spine.
func TestRunningANamedWayOfWorkingCompilesItAndAdmitsIt(t *testing.T) {
	graph := openStore(t)
	shelf := matchedPresentation(0)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	settled, said := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "room", Kind: store.CommandCraftRun, Target: "presentation",
		Instruction: "the Q3 numbers",
	})
	if settled.Status != store.CommandApplied {
		t.Fatalf("the run was %s: %s", settled.Status, settled.Result)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	admitted := 0
	for _, node := range nodes {
		if strings.HasPrefix(node.ID, "craft-") {
			admitted++
			if node.Provenance.Craft != "presentation@abc1234def" {
				t.Fatalf("%s names version %q", node.ID, node.Provenance.Craft)
			}
			if node.Provenance.Origin != store.OriginUser || node.Provenance.SessionID != "room" {
				t.Fatalf("%s came from %+v", node.ID, node.Provenance)
			}
		}
	}
	if admitted == 0 {
		t.Fatalf("nothing was admitted; nodes = %d", len(nodes))
	}
	if !strings.Contains(said, "using your presentation way of doing this") {
		t.Fatalf("the thread says: %s", said)
	}
	// The parameters came from the person's prose through the ordinary filler.
	if len(shelf.requests) != 0 {
		t.Fatalf("a named run went through the matcher: %v", shelf.requests)
	}
}

// A run of something nobody has forged is refused in words, where the
// repository is — the store cannot know which names exist and does not pretend.
func TestRunningAWayOfWorkingNobodyHasIsRefusedInWords(t *testing.T) {
	graph := openStore(t)
	mind := NewCraftMind(matchedPresentation(0), "/home/craft", fillsTopic, nil)

	settled, said := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "room", Kind: store.CommandCraftRun, Target: "never-forged", Instruction: "go on",
	})
	if settled.Status != store.CommandRejected ||
		!strings.Contains(settled.Result, `no way of working called "never-forged"`) {
		t.Fatalf("settled = %+v", settled)
	}
	if !strings.Contains(said, "never-forged") {
		t.Fatalf("the refusal never reached the thread: %s", said)
	}
}

// A named run that cannot fill a required hole asks for it, in the resident's
// own voice. Recognition treats the same gap as a miss and plans instead —
// it may, because nobody asked for that workflow by name.
func TestANamedRunAsksForWhatItCannotWorkOut(t *testing.T) {
	graph := openStore(t)
	mind := NewCraftMind(matchedPresentation(0), "/home/craft", nil, nil)

	settled, said := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "room", Kind: store.CommandCraftRun, Target: "presentation", Instruction: "run presentation",
	})
	if settled.Status != store.CommandRejected {
		t.Fatalf("settled = %+v", settled)
	}
	if !strings.Contains(said, "To run presentation I need topic") {
		t.Fatalf("the question was not asked plainly: %s", said)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if strings.HasPrefix(node.ID, "craft-") {
			t.Fatalf("work was admitted on a question: %s", node.ID)
		}
	}
}

// Reverting puts the version before the current one back as a NEW commit
// carrying the reason. History is never rewritten, and a reason too thin to be
// one is refused rather than papered over.
func TestRevertingAWayOfWorkingCarriesTheReasonAndKeepsHistory(t *testing.T) {
	graph := openStore(t)
	shelf := &revertingShelf{fakeShelf: matchedPresentation(0)}
	shelf.versions = map[string][]craft.Version{"presentation": {
		{Commit: "newer"}, {Commit: "older"},
	}}
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	settled, said := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "room", Kind: store.CommandCraftRevert, Target: "presentation",
		Instruction: "the new link check misses half of them",
	})
	if settled.Status != store.CommandApplied {
		t.Fatalf("settled = %+v", settled)
	}
	if len(shelf.reverted) != 1 ||
		!strings.HasPrefix(shelf.reverted[0], "presentation@older: the new link check") {
		t.Fatalf("the repository was asked for %v", shelf.reverted)
	}
	if !strings.Contains(said, "back to the version before this one") {
		t.Fatalf("the thread says: %s", said)
	}
}

func TestRevertingRefusesWithoutAReasonOrWithoutAnEarlierVersion(t *testing.T) {
	graph := openStore(t)
	shelf := &revertingShelf{fakeShelf: matchedPresentation(0)}
	shelf.versions = map[string][]craft.Version{"presentation": {{Commit: "only"}}}
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	thin, _ := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "room", Kind: store.CommandCraftRevert, Target: "presentation", Instruction: "worse",
	})
	if thin.Status != store.CommandRejected || !strings.Contains(thin.Result, "what the newer version got wrong") {
		t.Fatalf("a reasonless revert settled as %+v", thin)
	}
	lonely, _ := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "room", Kind: store.CommandCraftRevert, Target: "presentation",
		Instruction: "it lost the section about pricing",
	})
	if lonely.Status != store.CommandRejected || !strings.Contains(lonely.Result, "only one version") {
		t.Fatalf("reverting a one-version workflow settled as %+v", lonely)
	}
	if len(shelf.reverted) != 0 {
		t.Fatalf("the repository was written to anyway: %v", shelf.reverted)
	}
}

// Retiring stops the recognizer reaching for it, keeps everything it proved,
// and deletes nothing. The proof is the recognition path itself: the same
// request that used to run the workflow now plans instead.
func TestRetiringAWayOfWorkingStopsItBeingReachedForAndKeepsTheEvidence(t *testing.T) {
	graph := openStore(t)
	shelf := matchedPresentation(5.0)
	mind := NewCraftMind(shelf, "/home/craft", fillsTopic, nil)

	if _, planned := craftSplice(t, graph, mind, "room", "make me a deck about the Q3 numbers"); planned {
		t.Fatal("the workflow was not reached for even before it was retired")
	}

	settled, said := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "room", Kind: store.CommandCraftRetire, Target: "presentation",
		Instruction: "we do decks by hand now",
	})
	if settled.Status != store.CommandApplied {
		t.Fatalf("settled = %+v", settled)
	}
	if !strings.Contains(said, "The file and its history stay where they are") {
		t.Fatalf("the receipt did not say what survives: %s", said)
	}

	reconciler := New(graph, nil, nil).WithCraftMind(mind)
	if retired := reconciler.craftRetirement("presentation"); retired != "we do decks by hand now" {
		t.Fatalf("the retirement reads %q", retired)
	}
	if _, planned := craftSplice(t, graph, mind, "room", "make me a deck about the Q3 numbers"); !planned {
		t.Fatal("a retired way of working was still reached for")
	}
	// And a run by name refuses rather than quietly running a retired workflow.
	run, _ := applyCraftVerb(t, graph, mind, store.Command{
		SessionID: "room", Kind: store.CommandCraftRun, Target: "presentation", Instruction: "the Q3 numbers",
	})
	if run.Status != store.CommandRejected || !strings.Contains(run.Result, "is retired") {
		t.Fatalf("a retired workflow ran anyway: %+v", run)
	}
}

// Retiring twice is not an error and not a second retirement: the person said
// the same thing twice, and the honest answer is that it was already true.
func TestRetiringAWayOfWorkingTwiceSaysItWasAlreadyRetired(t *testing.T) {
	graph := openStore(t)
	mind := NewCraftMind(matchedPresentation(0), "/home/craft", fillsTopic, nil)
	first := store.Command{SessionID: "room", Kind: store.CommandCraftRetire, Target: "presentation",
		Instruction: "we do decks by hand now"}
	if settled, _ := applyCraftVerb(t, graph, mind, first); settled.Status != store.CommandApplied {
		t.Fatalf("first retirement = %+v", settled)
	}
	settled, _ := applyCraftVerb(t, graph, mind, first)
	if settled.Status != store.CommandApplied || !strings.Contains(settled.Result, "already retired") {
		t.Fatalf("second retirement = %+v", settled)
	}
}

// Retiring a tool takes the belief off every retrieval path and the link off
// PATH in the same breath. A belief that went quiet while its command stayed
// runnable is a half-done retirement.
func TestRetiringAToolQuietensTheBeliefAtOnce(t *testing.T) {
	graph := openStore(t)
	skill, err := graph.RecordSkillCandidate(store.RootID, "tool:imgshrink",
		"imgshrink squeezes screenshots", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ActivateSkill(skill.Seq, skill.Artifact); err != nil {
		t.Fatal(err)
	}

	settled, said := applyCraftVerb(t, graph, nil, store.Command{
		SessionID: "room", Kind: store.CommandSkillRetire,
		Target: strconv.FormatInt(skill.Seq, 10), Instruction: "it mangles the colours",
	})
	if settled.Status != store.CommandApplied {
		t.Fatalf("settled = %+v", settled)
	}
	fact, found, err := graph.FactBySeq(skill.Seq)
	if err != nil || !found || fact.Status != store.FactQuarantined {
		t.Fatalf("the belief is %+v", fact)
	}
	if !strings.Contains(said, "off the shelf") {
		t.Fatalf("the thread says: %s", said)
	}
}

// A window assembled without a place to keep learned ways of working says so
// plainly instead of failing the tick.
func TestCraftVerbsWithoutAShelfRefuseInWords(t *testing.T) {
	graph := openStore(t)
	settled, _ := applyCraftVerb(t, graph, nil, store.Command{
		SessionID: "room", Kind: store.CommandCraftRun, Target: "presentation", Instruction: "the Q3 numbers",
	})
	if settled.Status != store.CommandRejected || !strings.Contains(settled.Result, "nothing to run") {
		t.Fatalf("settled = %+v", settled)
	}
}
