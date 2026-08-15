package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// proposeRecurring runs the retrospective over three near-identical asks and
// returns the charter it proposes.
func proposeRecurring(t *testing.T, graph *store.Store) store.Charter {
	t.Helper()
	// Somebody is at the surface: the proposal is a remark, and a remark needs
	// a room to be made in.
	if _, err := graph.TouchSeen("tui", "today", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 3; index++ {
		settleRetrospectiveJob(t, graph, index)
	}
	reconciler := New(graph, nil, nil).
		WithReflector(func(context.Context, []JobSketch) ([]Learned, error) { return nil, nil }).
		WithCharterProposals()
	reconciler.reflectOnJobs(context.Background())
	charters, err := graph.Charters()
	if err != nil {
		t.Fatal(err)
	}
	if len(charters) != 1 || charters[0].Status != store.CharterProposed {
		t.Fatalf("retrospective proposals = %+v", charters)
	}
	return charters[0]
}

func proposalQuestion(t *testing.T, graph *store.Store, charterID string) store.AgentQuestion {
	t.Helper()
	questions, err := graph.UnresolvedQuestions(20)
	if err != nil {
		t.Fatal(err)
	}
	for _, question := range questions {
		if question.OriginCharterID == charterID {
			return question
		}
	}
	t.Fatalf("the proposal asked nothing: %+v", questions)
	return store.AgentQuestion{}
}

func TestRecurringAskProposalIsAskedAndCanBeRatified(t *testing.T) {
	graph := openStore(t)
	proposal := proposeRecurring(t, graph)

	question := proposalQuestion(t, graph, proposal.ID)
	if question.Urgency != store.QuestionNextNaturalMoment {
		t.Fatalf("an unprompted observation interrupted: %+v", question)
	}
	if question.DefaultAnswer != "2" {
		t.Fatalf("an offer nobody asked for did not default to no: %+v", question)
	}
	values := make([]string, 0, len(question.Options))
	for _, option := range question.Options {
		values = append(values, option.Value)
	}
	if strings.Join(values, " ") != "charter:ratify:"+proposal.ID+" charter:retire:"+proposal.ID {
		t.Fatalf("proposal options do not use the codes the head already decodes: %+v", question.Options)
	}
	if question.Status != store.QuestionPending {
		t.Fatalf("an unprompted observation surfaced before its moment: %+v", question)
	}

	// It surfaces at the next natural moment rather than shouting.
	reconciler := New(graph, nil, nil)
	if err := reconciler.AttachSession("today"); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("today", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].QuestionSeq != question.Seq {
		t.Fatalf("the proposal never reached the thread: %+v", messages)
	}

	// "Yes" travels the ratification path a user-uttered charter already takes.
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "today", Kind: store.CommandCharterRatify, Target: proposal.ID,
		Instruction: "yes, stand this up",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	ratified, found, err := graph.Charter(proposal.ID)
	if err != nil || !found || ratified.Status != store.CharterActive {
		t.Fatalf("ratified proposal = %+v found=%t err=%v", ratified, found, err)
	}
	due, err := graph.DueCharters(time.Now().Add(time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != proposal.ID {
		t.Fatalf("a ratified proposal still cannot fire: %+v", due)
	}
}

func TestDecliningAProposalRetiresItAndStopsTheReproposal(t *testing.T) {
	graph := openStore(t)
	proposal := proposeRecurring(t, graph)
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "today", Kind: store.CommandCharterRetire, Target: proposal.ID,
		Instruction: "no, not standing",
	}); err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	declined, found, err := graph.Charter(proposal.ID)
	if err != nil || !found || declined.Status != store.CharterRetired {
		t.Fatalf("declined proposal = %+v found=%t err=%v", declined, found, err)
	}
	suppressed, err := graph.CharterProposalDeclined(proposal.ProposalShape)
	if err != nil || !suppressed {
		t.Fatalf("declining did not record the never-offer-again key: %t %v", suppressed, err)
	}
}

func TestRetiringAStandingCharterIsStillJustRetiring(t *testing.T) {
	graph := openStore(t)
	charter := createProposedStandingCharter(t, graph, "charter-live", "s")
	if err := graph.SetCharterStatus(charter.ID, store.CharterActive, store.Ratification{
		Origin: store.OriginUser, SessionID: "s", Evidence: "yes",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "s", Kind: store.CommandCharterRetire, Target: charter.ID,
		Instruction: "retire it",
	}); err != nil {
		t.Fatal(err)
	}
	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	retired, found, err := graph.Charter(charter.ID)
	if err != nil || !found || retired.Status != store.CharterRetired {
		t.Fatalf("retired charter = %+v found=%t err=%v", retired, found, err)
	}
	// A ratified charter has no proposal shape, so retiring it must not be
	// mistaken for refusing an offer that was never made.
	if declined, err := graph.CharterProposalDeclined(""); err != nil || declined {
		t.Fatalf("retiring a live charter wrote a decline: %t %v", declined, err)
	}
}
