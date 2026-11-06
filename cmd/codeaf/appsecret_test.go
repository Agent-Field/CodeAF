package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A SECRET ASK SAYS SO ON THE WIRE, with the engine's own day, and an ordinary
// one says nothing.
func TestAppAskOfCarriesTheSecretBox(t *testing.T) {
	q := session.Question{ID: 3, Kind: session.QuestionAsk, Head: "Runner token",
		Input: session.InputShape{Kind: session.InputText, Prompt: "Fine-grained GitHub token", Secret: true}}
	ask := appAskOf("a", q)
	if ask.Secret == nil || ask.Secret.Placeholder != "Fine-grained GitHub token" || ask.Secret.KeepsForS != int(session.SecretKeepFor/time.Second) {
		t.Fatalf("secret ask = %+v", ask.Secret)
	}
	q.Kind = session.QuestionConnect
	if ask := appAskOf("a", q); ask.Secret == nil || ask.Secret.KeepsForS != 0 {
		t.Fatalf("a connection's key box promises a kept file: %+v", ask.Secret)
	}
	if ask := appAskOf("a", session.Question{ID: 4, Kind: session.QuestionAsk, Head: "Which?"}); ask.Secret != nil {
		t.Fatalf("an ordinary ask carries a secret box: %+v", ask.Secret)
	}
}

// asks.answer_secret ANSWERS ONLY A SECRET ASK, through the one answer door,
// and the Inbox never takes the value.
func TestAppAnswerSecretOnlyAnswersASecretAsk(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	conv := &fakeConversation{}
	b, rec, agent := workBackend(t, conv)
	b.profileDir = t.TempDir()
	answered := make(chan session.Answer, 2)
	conv.mu.Lock()
	conv.onAnswer = func(answer session.Answer) { answered <- answer }
	conv.mu.Unlock()

	plain := session.Question{ID: 4, Kind: session.QuestionAsk, Head: "Which store?"}
	secret := session.Question{ID: 5, Kind: session.QuestionAsk, Head: "Runner token", Reason: "for the runner",
		Options: []session.AnswerOption{{Key: session.SecretDeclineKey, Label: session.SecretDeclineLabel}},
		Input:   session.InputShape{Kind: session.InputText, Secret: true}}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &plain})
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &secret})
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		e, ok := data.(appwire.InboxAddedEvent)
		return ok && e.Item.Ask.ID == "ask:5"
	})

	if err := b.AnswerAskSecret(agent.ID, "ask:4", "tok-123456"); wireCodeOf(t, err) != appwire.CodeBadParams {
		t.Fatalf("a secret sent at an ordinary ask = %v, want bad_params", err)
	}
	if err := b.AnswerAskSecret(agent.ID, "ask:99", "tok-123456"); wireCodeOf(t, err) != appwire.CodeNotFound {
		t.Fatalf("a secret sent at no ask = %v, want not_found", err)
	}
	if _, err := b.AnswerInbox(appInboxID(agent.ID, "ask:5"), "", "tok-123456", "", ""); wireCodeOf(t, err) != appwire.CodeBadParams {
		t.Fatalf("the Inbox took a secret's value: %v", err)
	}
	if err := b.AnswerAskSecret(agent.ID, "ask:5", "tok-123456"); err != nil {
		t.Fatalf("AnswerAskSecret: %v", err)
	}
	select {
	case answer := <-answered:
		if answer.ID != 5 || answer.Change != "tok-123456" || answer.Key != "" {
			t.Fatalf("the engine was handed %+v", answer)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the secret never reached the engine's answer door")
	}
}
