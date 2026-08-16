package consent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// consentGraph builds one planned job of the given width, priced by a journal
// that has already measured what a run costs here.
func consentGraph(t *testing.T, leaves int, perRun float64) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "consent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })

	nodes := []store.NodeSpec{{ID: "job", Title: "audit the service", Brief: "audit the service", Stage: 2}}
	for index := 0; index < leaves; index++ {
		nodes = append(nodes, store.NodeSpec{
			ID: leafID(index), Parent: "job", Brief: "read a slice", Stage: 1,
		})
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: nodes}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "audit the service",
	}); err != nil {
		t.Fatal(err)
	}
	if perRun > 0 {
		// Measured history, in the only place a forecast may come from: work
		// that already happened, on a job of its own so this one's estimate is
		// not priced off itself.
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
			{ID: "earlier", Brief: "an earlier errand", Stage: 1},
		}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "an earlier errand"}); err != nil {
			t.Fatal(err)
		}
		if err := graph.RecordUsage(store.NodeUsage{NodeID: "earlier", Cost: perRun, Model: "cheap/one"}); err != nil {
			t.Fatal(err)
		}
	}
	return graph
}

func leafID(index int) string {
	return "job-leaf-" + string(rune('a'+index))
}

func consentSettings(threshold float64) config.Config {
	return config.Config{PlanConsentUSD: threshold}
}

// The big ask. Fifty leaves used to launch on a sentence with no number in
// front of them; now the last free moment carries a count and a price.
func TestConsentGateAsksBeforeALargePlanSpendsAnything(t *testing.T) {
	graph := consentGraph(t, 8, 0.75)
	desk := NewDesk(graph)
	first, found, err := graph.Node(leafID(0))
	if err != nil || !found {
		t.Fatal(err)
	}

	if !desk.Gate(consentSettings(3), &profile.Profile{}, first) {
		t.Fatal("an eight-step job at $0.75 a step started without asking")
	}

	// Held, all of it, before a single worker said anything.
	nodes, err := graph.SubtreeNodes("job")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if !node.Held {
			t.Fatalf("%s was left claimable while the question was outstanding", node.ID)
		}
	}

	questions, err := graph.QuestionsForNode("job", 10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("questions = %+v err=%v", questions, err)
	}
	question := questions[0]
	if question.Urgency != store.QuestionBlocking {
		t.Fatalf("urgency = %q; a price the user has not seen cannot be ambient", question.Urgency)
	}
	// Honest math, both halves measured: eight steps at the journal's own
	// median of $0.75.
	if !strings.Contains(question.Text, "8 steps") || !strings.Contains(question.Text, "$6.00") {
		t.Fatalf("the question does not quote the count and the price: %q", question.Text)
	}
	if len(question.Options) != 2 || question.Options[0].Label != Approve {
		t.Fatalf("options = %+v", question.Options)
	}

	// Asked once. Every other leaf coming up for claim finds the outstanding
	// question and holds quietly rather than asking again.
	second, _, err := graph.Node(leafID(1))
	if err != nil {
		t.Fatal(err)
	}
	if !desk.Gate(consentSettings(3), &profile.Profile{}, second) {
		t.Fatal("a sibling ran while the price was still unanswered")
	}
	if questions, err := graph.QuestionsForNode("job", 10); err != nil || len(questions) != 1 {
		t.Fatalf("the gate asked twice: %+v err=%v", questions, err)
	}
}

// Small work never asks, and neither does a machine with nothing measured: a
// forecast has to be arithmetic over history or it has to be silence.
func TestConsentGateStaysOutOfTheWayOfSmallWork(t *testing.T) {
	cheap := consentGraph(t, 2, 0.20)
	first, _, err := cheap.Node(leafID(0))
	if err != nil {
		t.Fatal(err)
	}
	if NewDesk(cheap).Gate(consentSettings(3), &profile.Profile{}, first) {
		t.Fatal("a two-step job worth $0.40 was held for consent")
	}
	if questions, err := cheap.QuestionsForNode("job", 10); err != nil || len(questions) != 0 {
		t.Fatalf("small work was asked about: %+v", questions)
	}

	unmeasured := consentGraph(t, 40, 0)
	wide, _, err := unmeasured.Node(leafID(0))
	if err != nil {
		t.Fatal(err)
	}
	if NewDesk(unmeasured).Gate(consentSettings(3), &profile.Profile{}, wide) {
		t.Fatal("a job was held on an estimate nothing had measured")
	}

	// A threshold of zero is the operator saying never ask.
	priced := consentGraph(t, 40, 1)
	node, _, err := priced.Node(leafID(0))
	if err != nil {
		t.Fatal(err)
	}
	if NewDesk(priced).Gate(consentSettings(0), &profile.Profile{}, node) {
		t.Fatal("a zero threshold still asked")
	}
}

// Saying yes releases the whole plan; saying anything else leaves it standing
// and held, which is what "trim it first" means. Either way the decision is
// made once — a job the user released by hand is never re-held.
func TestConsentAnswerReleasesOrLeavesTheWorkHeld(t *testing.T) {
	graph := consentGraph(t, 8, 0.75)
	desk := NewDesk(graph)
	first, _, err := graph.Node(leafID(0))
	if err != nil {
		t.Fatal(err)
	}
	if !desk.Gate(consentSettings(3), &profile.Profile{}, first) {
		t.Fatal("the gate did not fire")
	}
	questions, err := graph.QuestionsForNode("job", 10)
	if err != nil || len(questions) != 1 {
		t.Fatal(err)
	}
	seq := questions[0].Seq

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go desk.Serve(ctx)

	if err := graph.ResolveQuestion(seq, store.QuestionAnswered, Approve); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		node, _, err := graph.Node(leafID(0))
		if err != nil {
			t.Fatal(err)
		}
		if !node.Held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the plan stayed held after the user said start it")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The decision is durable: the next leaf up for claim runs rather than
	// being re-priced.
	second, _, err := graph.Node(leafID(1))
	if err != nil {
		t.Fatal(err)
	}
	if desk.Gate(consentSettings(3), &profile.Profile{}, second) {
		t.Fatal("an approved job was held again")
	}
}

func TestConsentHoldAnswerLeavesThePlanForTrimming(t *testing.T) {
	graph := consentGraph(t, 8, 0.75)
	desk := NewDesk(graph)
	first, _, err := graph.Node(leafID(0))
	if err != nil {
		t.Fatal(err)
	}
	if !desk.Gate(consentSettings(3), &profile.Profile{}, first) {
		t.Fatal("the gate did not fire")
	}
	questions, _ := graph.QuestionsForNode("job", 10)
	if err := graph.ResolveQuestion(questions[0].Seq, store.QuestionAnswered, Hold); err != nil {
		t.Fatal(err)
	}
	desk.settle("job", mustQuestion(t, graph, questions[0].Seq))
	node, _, err := graph.Node(leafID(0))
	if err != nil {
		t.Fatal(err)
	}
	if !node.Held {
		t.Fatal("a job the user declined to start was released anyway")
	}
	// And it is never asked about again — the plan is theirs to cancel down.
	if desk.Gate(consentSettings(3), &profile.Profile{}, node) {
		t.Fatal("a settled consent question re-held the job")
	}
}

// The question and the hold are both durable, so a resident that died between
// them comes back knowing what it was waiting for.
func TestConsentRehydratesAfterARestart(t *testing.T) {
	graph := consentGraph(t, 8, 0.75)
	first, _, err := graph.Node(leafID(0))
	if err != nil {
		t.Fatal(err)
	}
	if !NewDesk(graph).Gate(consentSettings(3), &profile.Profile{}, first) {
		t.Fatal("the gate did not fire")
	}
	questions, _ := graph.QuestionsForNode("job", 10)
	if err := graph.ResolveQuestion(questions[0].Seq, store.QuestionAnswered, Approve); err != nil {
		t.Fatal(err)
	}

	// A brand-new desk, as a relaunch would build: nothing in memory, the
	// approval already recorded, the plan still held.
	revived := NewDesk(graph)
	revived.Rehydrate()
	node, _, err := graph.Node(leafID(0))
	if err != nil {
		t.Fatal(err)
	}
	if node.Held {
		t.Fatal("a plan approved while the resident was down stayed held forever")
	}
}

func mustQuestion(t *testing.T, graph *store.Store, seq int64) store.AgentQuestion {
	t.Helper()
	question, found, err := graph.AgentQuestionBySeq(seq)
	if err != nil || !found {
		t.Fatalf("question %d: found=%v err=%v", seq, found, err)
	}
	return question
}
