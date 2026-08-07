package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestQuestionSurfacingPolicyHonorsUrgencyAndNaturalMomentCap(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil)

	blocking, err := reconciler.AskQuestion(store.AgentQuestion{
		SessionID: "policy", Text: "I need this answer before I can continue.",
		Urgency: store.QuestionBlocking,
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocking.Status != store.QuestionAsked {
		t.Fatalf("blocking status = %s, want asked", blocking.Status)
	}
	for _, text := range []string{"First saved question?", "Second saved question?"} {
		if _, err := graph.AskQuestion(store.AgentQuestion{
			SessionID: "policy", Text: text, Urgency: store.QuestionNextNaturalMoment,
		}); err != nil {
			t.Fatal(err)
		}
	}
	whenever, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "policy", Text: "Ambient preference?", Urgency: store.QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := reconciler.AttachSession("policy"); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("policy", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].QuestionSeq != blocking.Seq ||
		messages[1].Body != "First saved question?" {
		t.Fatalf("first natural moment messages = %+v", messages)
	}
	pending, err := graph.PendingQuestions("policy", 0)
	if err != nil || len(pending) != 2 || pending[0].Text != "Second saved question?" || pending[1].Seq != whenever.Seq {
		t.Fatalf("pending after first moment = %+v err=%v", pending, err)
	}

	if err := reconciler.AttachSession("policy"); err != nil {
		t.Fatal(err)
	}
	messages, _ = graph.Messages("policy", 0, 0)
	if len(messages) != 3 || messages[2].Body != "Second saved question?" {
		t.Fatalf("second natural moment messages = %+v", messages)
	}
	pending, err = graph.PendingQuestions("policy", 0)
	if err != nil || len(pending) != 1 || pending[0].Seq != whenever.Seq {
		t.Fatalf("whenever question was surfaced: %+v err=%v", pending, err)
	}
}

func TestSettledAnnouncementSurfacesOneSavedQuestionAfterDelivery(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "delivery", Brief: "deliver the result", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "delivery-session", Intent: "deliver it"}); err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Question after delivery one?", "Question after delivery two?"} {
		if _, err := graph.AskQuestion(store.AgentQuestion{
			SessionID: "delivery-session", Text: text, Urgency: store.QuestionNextNaturalMoment,
		}); err != nil {
			t.Fatal(err)
		}
	}
	claim, won, err := graph.Claim("delivery", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "The requested result is ready."); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("delivery-session", 0, 0)
	if err != nil || len(messages) != 2 {
		t.Fatalf("delivery messages = %+v err=%v", messages, err)
	}
	if messages[0].Body != "The requested result is ready." || messages[1].Body != "Question after delivery one?" {
		t.Fatalf("natural-moment ordering = %+v", messages)
	}
	pending, _ := graph.PendingQuestions("delivery-session", 0)
	if len(pending) != 1 || pending[0].Text != "Question after delivery two?" {
		t.Fatalf("natural moment dumped queue: %+v", pending)
	}
}

func TestQuestionExpiresWhenOriginSettlesOrDeadlinePasses(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "origin", Brief: "find another route", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "expiry", Intent: "finish somehow"}); err != nil {
		t.Fatal(err)
	}
	originQuestion, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "expiry", Text: "Do you want route A?", OriginNodeID: "origin",
		Urgency: store.QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadlineQuestion, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "expiry", Text: "Still relevant?", Urgency: store.QuestionWhenever,
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("origin", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "Settled by route B."); err != nil {
		t.Fatal(err)
	}
	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, seq := range []int64{originQuestion.Seq, deadlineQuestion.Seq} {
		question, found, err := graph.AgentQuestionBySeq(seq)
		if err != nil || !found || question.Status != store.QuestionExpired || question.ResolvedAt.IsZero() {
			t.Fatalf("expired question %d = %+v found=%t err=%v", seq, question, found, err)
		}
		if question.Resolution == "" {
			t.Fatalf("expiry reason missing for question %d", seq)
		}
	}
	originResolved, _, _ := graph.AgentQuestionBySeq(originQuestion.Seq)
	if !strings.Contains(originResolved.Resolution, "settled as done") {
		t.Fatalf("origin expiry reason = %q", originResolved.Resolution)
	}
}
