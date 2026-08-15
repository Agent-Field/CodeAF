package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// craftJobFailure stands one craft job up under the spine, kills it with the
// error the incident actually produced, and runs the tick that settles it.
//
// The provider's 404 body is carried verbatim on purpose: escaped quotes,
// trailing prose and all. Every assertion below about what a person reads is
// worth exactly as much as the realism of this string.
const providerRefusal = `node craft-7~launch: after 3 node call attempts: API error (404): ` +
	`{"error":{"message":"No endpoints found that support tool use. Try disabling \"sh\" or ` +
	`choosing a different model.","code":404}}`

// craftFallbackFixture builds a reconciler whose planner records what it was
// asked to plan, and splices one craft-provenance job root ready to be failed.
type craftFallbackFixture struct {
	graph      *store.Store
	reconciler *Reconciler
	// asked is every goal the fresh planner was handed, in order. A fallback
	// that fired exactly once leaves exactly one entry, and its content is the
	// person's own ask rather than a step brief.
	asked []string
	// prefixes is the id namespace each of those plans was minted under.
	prefixes []string
}

func newCraftFallbackFixture(t *testing.T, nodeID, ask, craftRef, sessionID string) *craftFallbackFixture {
	t.Helper()
	graph := openStore(t)
	fixture := &craftFallbackFixture{graph: graph}
	plan := func(_ context.Context, goal, prefix string) (store.Subtree, error) {
		fixture.asked = append(fixture.asked, goal)
		fixture.prefixes = append(fixture.prefixes, prefix)
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: prefix, Brief: goal, Title: "Answer it the ordinary way", Stage: 1},
		}}, nil
	}
	fixture.reconciler = New(graph, nil, nil).WithOverrunPlanner(0, plan)
	if err := fixture.reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	fixture.splice(t, nodeID, ask, craftRef, sessionID)
	return fixture
}

func (f *craftFallbackFixture) splice(t *testing.T, nodeID, ask, craftRef, sessionID string) {
	t.Helper()
	if err := f.graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: nodeID, Brief: "Launch the analysis fan", Title: "Launch", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginUser, SessionID: sessionID, Intent: ask, Craft: craftRef,
	}); err != nil {
		t.Fatalf("splice %s: %v", nodeID, err)
	}
}

// fail kills one node with a real transport error and settles the tick.
func (f *craftFallbackFixture) fail(t *testing.T, nodeID, failure string) {
	t.Helper()
	claim, won, err := f.graph.Claim(nodeID, "worker")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%t err=%v", nodeID, won, err)
	}
	if err := f.graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := f.graph.Fail(claim, failure); err != nil {
		t.Fatal(err)
	}
	if err := f.reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("settle tick: %v", err)
	}
}

func (f *craftFallbackFixture) room(t *testing.T, sessionID string) string {
	t.Helper()
	messages, err := f.graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	return strings.Join(bodies, "\n---\n")
}

// prove writes the survival record a landed run would have written, which is
// what turns a draft into a way of working the person relies on.
func (f *craftFallbackFixture) prove(t *testing.T, name string) {
	t.Helper()
	if _, err := f.graph.RecordTrait(CraftSurvivalKey(name), store.TraitMeasurement{
		Value: CraftSurvival{For: 1}, N: 1, Updated: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

// The whole point of a provisional run: the ask survives the experiment.
//
// A never-run craft gets one try at the decisive bar and says so out loud. When
// that try dies, the person's request is still owed an answer — so the same ask
// is planned the ordinary way, beside the dead job, and the room is told in one
// sentence why the work it is watching changed shape.
func TestAFailedProvisionalCraftReplansTheSameAskFromScratch(t *testing.T) {
	fixture := newCraftFallbackFixture(t, "craft-7",
		"put together a deep dive on the Q3 numbers", "multi-level-dag@abc1234", "room")
	fixture.fail(t, "craft-7", providerRefusal)

	// The fresh plan was asked for the PERSON'S ask, not the craft step's brief.
	if len(fixture.asked) != 1 {
		t.Fatalf("planner asked %d times: %q", len(fixture.asked), fixture.asked)
	}
	if fixture.asked[0] != "put together a deep dive on the Q3 numbers" {
		t.Fatalf("the fallback planned %q — a fallback plans the ask, never the step", fixture.asked[0])
	}
	// It lands in the failed job's own split namespace, which is where the
	// overrun path puts every round it grows, and under the spine rather than
	// inside the dead job — a subtree parented into a settled job finishes
	// correctly and is delivered to nobody.
	fresh, ok, err := fixture.graph.Node(fixture.prefixes[0])
	if err != nil || !ok {
		t.Fatalf("fresh plan %q: ok=%t err=%v", fixture.prefixes[0], ok, err)
	}
	if !strings.HasPrefix(fresh.ID, "craft-7") || fresh.ID == "craft-7" {
		t.Fatalf("fresh plan id = %q, want craft-7's own split namespace", fresh.ID)
	}
	if fresh.Parent != store.RootID {
		t.Fatalf("fresh plan parent = %q, want the spine", fresh.Parent)
	}
	if fresh.Provenance.RetryOf != "craft-7" {
		t.Fatalf("fresh plan retry-of = %q, want the run it supersedes", fresh.Provenance.RetryOf)
	}
	// THE NON-LOOP GUARANTEE, stated as a field. The replacement carries no
	// craft, so a second failure cannot reach this path at all.
	if fresh.Provenance.Craft != "" {
		t.Fatalf("the fresh plan inherited the craft %q — that is the loop", fresh.Provenance.Craft)
	}
	if fresh.Provenance.Intent != "put together a deep dive on the Q3 numbers" {
		t.Fatalf("fresh plan intent = %q", fresh.Provenance.Intent)
	}

	// The round was charged to the governor's own journal, under this job.
	growths, err := fixture.graph.JobGrowths("craft-7")
	if err != nil {
		t.Fatal(err)
	}
	if len(growths) != 1 {
		t.Fatalf("job growths = %+v, want the fallback's one round", growths)
	}
	if growths[0].Reason != GrowCraftFallback || !growths[0].Allowed || growths[0].Round != 1 {
		t.Fatalf("growth = %+v, want one allowed craft-fallback round", growths[0])
	}

	// One composed line, and it says what happened next.
	room := fixture.room(t, "room")
	if strings.Count(room, CraftFallbackLine) != 1 {
		t.Fatalf("the fallback line is not said exactly once:\n%s", room)
	}
}

// A PROVEN craft failing is news, not a hiccup to route around. The person has
// watched this way of working land real jobs; quietly replanning behind its back
// would be the system covering for the thing they trust.
func TestAProvenCraftFailsHonestlyWithNoSilentFallback(t *testing.T) {
	fixture := newCraftFallbackFixture(t, "craft-9",
		"put together a deep dive on the Q3 numbers", "multi-level-dag@abc1234", "room")
	fixture.prove(t, "multi-level-dag")
	fixture.fail(t, "craft-9", providerRefusal)

	if len(fixture.asked) != 0 {
		t.Fatalf("a proven craft's failure replanned anyway: %q", fixture.asked)
	}
	growths, err := fixture.graph.JobGrowths("craft-9")
	if err != nil {
		t.Fatal(err)
	}
	if len(growths) != 0 {
		t.Fatalf("a proven craft's failure spent a round: %+v", growths)
	}
	room := fixture.room(t, "room")
	if strings.Contains(room, CraftFallbackLine) {
		t.Fatalf("a proven craft's failure promised a fallback:\n%s", room)
	}
	// Honest, which means composed rather than silent: the ask, the cause, and
	// no machinery.
	if !strings.Contains(room, "put together a deep dive on the Q3 numbers") {
		t.Fatalf("the failure never says what it was:\n%s", room)
	}
	if !strings.Contains(room, "No endpoints found that support tool use.") {
		t.Fatalf("the failure never says why:\n%s", room)
	}
}

// Work that never ran a craft is not this path's business, whatever it dies of.
func TestOrdinaryPlannedWorkNeverFallsBack(t *testing.T) {
	fixture := newCraftFallbackFixture(t, "task-3", "summarize this file", "", "room")
	fixture.fail(t, "task-3", providerRefusal)

	if len(fixture.asked) != 0 {
		t.Fatalf("ordinary planned work replanned itself: %q", fixture.asked)
	}
}

// A leaf inside a craft run is the run's own business — CraftRunner has repair
// rounds for exactly that. Only the death of the whole job means the ask went
// unanswered, which is the same boundary the survival record is charged at.
func TestALeafInsideACraftRunDoesNotTriggerTheJobFallback(t *testing.T) {
	fixture := newCraftFallbackFixture(t, "craft-11",
		"put together a deep dive on the Q3 numbers", "multi-level-dag@abc1234", "room")
	if err := fixture.graph.Splice("craft-11", store.Subtree{Nodes: []store.NodeSpec{{
		ID: "craft-11~launch", Brief: "Launch the analysis fan", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginSelf, SessionID: "room",
		Intent: "put together a deep dive on the Q3 numbers", Craft: "multi-level-dag@abc1234",
	}); err != nil {
		t.Fatal(err)
	}
	fixture.fail(t, "craft-11~launch", providerRefusal)

	if len(fixture.asked) != 0 {
		t.Fatalf("a leaf's failure replanned the whole job: %q", fixture.asked)
	}
}

// THE FALLBACK CANNOT LOOP. The fresh plan that replaces a dead craft run is
// planned without one, so when IT fails there is no craft on its provenance and
// nothing to fall back from: the second failure settles honestly and says so.
func TestAFreshPlanThatAlsoFailsSettlesHonestlyInsteadOfLooping(t *testing.T) {
	fixture := newCraftFallbackFixture(t, "craft-13",
		"put together a deep dive on the Q3 numbers", "multi-level-dag@abc1234", "room")
	fixture.fail(t, "craft-13", providerRefusal)
	if len(fixture.asked) != 1 {
		t.Fatalf("the first fallback did not fire: %q", fixture.asked)
	}

	fixture.fail(t, fixture.prefixes[0], "node "+fixture.prefixes[0]+": the workspace was never written")
	if len(fixture.asked) != 1 {
		t.Fatalf("the fallback fell back: %q — this is the loop", fixture.asked)
	}
	growths, err := fixture.graph.JobGrowths("craft-13")
	if err != nil {
		t.Fatal(err)
	}
	if len(growths) != 1 {
		t.Fatalf("a second round was spent: %+v", growths)
	}
	room := fixture.room(t, "room")
	if !strings.Contains(room, "the workspace was never written") {
		t.Fatalf("the second failure was swallowed:\n%s", room)
	}
	if strings.Count(room, CraftFallbackLine) != 1 {
		t.Fatalf("the fallback line was said again:\n%s", room)
	}
}

// The round cap is the belt behind the non-loop brace. A lineage that has
// already spent its allowance is refused by the same governor every other
// growth path answers to, and the failure is announced with no promise in it.
func TestTheFallbackRespectsTheRoundCap(t *testing.T) {
	fixture := newCraftFallbackFixture(t, "craft-17",
		"put together a deep dive on the Q3 numbers", "multi-level-dag@abc1234", "room")
	// Spend the lineage's whole allowance the way every other grower spends it.
	for round := 1; round <= MaxOverrunRounds; round++ {
		if err := fixture.graph.RecordJobGrowth("craft-17", store.JobGrowth{
			Reason: GrowOverrun, Lineage: "craft-17", Round: round, Allowed: true, Adding: 1,
		}); err != nil {
			t.Fatal(err)
		}
		if err := fixture.graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: fmt.Sprintf("craft-17%s%d", store.SplitNamespace, round), Brief: "an earlier round", Stage: 1,
		}}}, store.Provenance{
			Origin: store.OriginSelf, SessionID: "room", Intent: "an earlier round",
		}); err != nil {
			t.Fatal(err)
		}
	}
	fixture.fail(t, "craft-17", providerRefusal)

	if len(fixture.asked) != 0 {
		t.Fatalf("the fallback planned past the round cap: %q", fixture.asked)
	}
	room := fixture.room(t, "room")
	if strings.Contains(room, CraftFallbackLine) {
		t.Fatalf("a refused fallback was announced as though it had happened:\n%s", room)
	}
	// Refused is not silent: the failure still reaches the person, composed.
	if !strings.Contains(room, "No endpoints found that support tool use.") {
		t.Fatalf("a refused fallback swallowed the failure:\n%s", room)
	}
}

// A fallback with no planner behind it is a fallback that cannot happen, and
// the row must not promise one. This is the build with no planning client —
// every headless surface, and every test in this package before this wave.
func TestWithNoPlannerTheFailureIsAnnouncedWithNoPromise(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "craft-21", Brief: "Launch the analysis fan", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "room",
		Intent: "put together a deep dive on the Q3 numbers", Craft: "multi-level-dag@abc1234",
	}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("craft-21", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fail(claim, providerRefusal); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("room", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	room := ""
	for _, message := range messages {
		room += message.Body + "\n"
	}
	if strings.Contains(room, CraftFallbackLine) {
		t.Fatalf("a build with no planner promised a fresh plan:\n%s", room)
	}
	if !strings.Contains(room, "put together a deep dive on the Q3 numbers") {
		t.Fatalf("the failure never reached the room:\n%s", room)
	}
}
