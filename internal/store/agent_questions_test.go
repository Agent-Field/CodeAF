package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestAgentQuestionLifecycleIsJournaledExactlyOnceAndRebuildable(t *testing.T) {
	graph := openThreadStore(t)
	expires := time.Now().UTC().Add(time.Hour).Round(0)
	queued, err := graph.AskQuestion(AgentQuestion{
		SessionID: "questions", Text: "Which release channel should I use?",
		Urgency: QuestionNextNaturalMoment, ExpiresAt: expires,
		Options: []QuestionOption{{Label: "stable", Value: "stable"}, {Label: "beta", Value: "beta"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != QuestionPending || queued.Seq == 0 || queued.CreatedAt.IsZero() {
		t.Fatalf("queued question = %+v", queued)
	}
	pending, err := graph.PendingQuestions("questions", 0)
	if err != nil || len(pending) != 1 || pending[0].Seq != queued.Seq {
		t.Fatalf("pending questions = %+v err=%v", pending, err)
	}

	ask, err := graph.SurfaceQuestion(queued.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if ask.Role != RoleAgent || ask.QuestionSeq != queued.Seq || ask.Body != queued.Text ||
		!reflect.DeepEqual(ask.Options, queued.Options) {
		t.Fatalf("surfaced message = %+v", ask)
	}
	if _, err := graph.SurfaceQuestion(queued.Seq); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second surface error = %v, want ErrInvalid", err)
	}
	if pending, err := graph.PendingQuestions("questions", 0); err != nil || len(pending) != 0 {
		t.Fatalf("surfaced question stayed pending: %+v err=%v", pending, err)
	}

	answer, err := graph.PostMessage(Message{
		SessionID: "questions", Role: RoleUser, Body: "Use beta.", QuestionSeq: queued.Seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	matched, found, err := graph.QuestionForAnswer("questions", answer.Seq, answer.QuestionSeq)
	if err != nil || !found || matched.Seq != queued.Seq {
		t.Fatalf("matched question = %+v found=%t err=%v", matched, found, err)
	}
	if err := graph.ResolveQuestion(queued.Seq, QuestionAnswered, answer.Body, answer.Seq); err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveQuestion(queued.Seq, QuestionExpired, "too late"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("double resolution error = %v, want ErrInvalid", err)
	}
	resolved, found, err := graph.AgentQuestionBySeq(queued.Seq)
	if err != nil || !found || resolved.Status != QuestionAnswered || resolved.Resolution != answer.Body ||
		resolved.AnswerMessageSeq != answer.Seq || resolved.AskedMessageSeq != ask.Seq || resolved.ResolvedAt.IsZero() {
		t.Fatalf("resolved question = %+v found=%t err=%v", resolved, found, err)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := graph.AgentQuestionBySeq(queued.Seq)
	if err != nil || !found || !reflect.DeepEqual(rebuilt, resolved) {
		t.Fatalf("rebuilt question = %+v, want %+v found=%t err=%v", rebuilt, resolved, found, err)
	}
}

func TestAgentQuestionRecencyDoesNotCaptureLaterConversation(t *testing.T) {
	graph := openThreadStore(t)
	question, err := graph.AskQuestion(AgentQuestion{
		SessionID: "recency", Text: "Use the compact layout?", Urgency: QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	first, err := graph.PostMessage(Message{SessionID: "recency", Role: RoleUser, Body: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := graph.QuestionForAnswer("recency", first.Seq, 0); err != nil || !found {
		t.Fatalf("first reply match found=%t err=%v", found, err)
	}
	later, err := graph.PostMessage(Message{SessionID: "recency", Role: RoleUser, Body: "unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := graph.QuestionForAnswer("recency", later.Seq, 0); err != nil || found {
		t.Fatalf("later reply captured old question: found=%t err=%v", found, err)
	}
	if explicit, found, err := graph.QuestionForAnswer("recency", later.Seq, question.Seq); err != nil || !found || explicit.Seq != question.Seq {
		t.Fatalf("explicit reference did not win: %+v found=%t err=%v", explicit, found, err)
	}
}
