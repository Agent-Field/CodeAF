package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStandingWatchOfferPostsExactlyOnceAcrossRebuilds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standing-watch.db")
	graph := openTestStore(t, path)
	charter := mustTestCharter(t, "first-standing-charter", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.10, MaxFiringsPerDay: 3,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	posted, err := graph.OfferStandingWatch("charter-session", charter.ID)
	if err != nil || !posted {
		t.Fatalf("first offer posted=%t err=%v", posted, err)
	}
	if posted, err := graph.OfferStandingWatch("charter-session", charter.ID); err != nil || posted {
		t.Fatalf("second offer posted=%t err=%v", posted, err)
	}
	assertStandingWatchQuestion(t, graph)
	decision, err := graph.StandingWatchDecisionState()
	if err != nil || decision != StandingWatchOffered {
		t.Fatalf("decision = %q err=%v", decision, err)
	}

	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	graph, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if posted, err := graph.OfferStandingWatch("charter-session", charter.ID); err != nil || posted {
		t.Fatalf("offer after rebuild posted=%t err=%v", posted, err)
	}
	assertStandingWatchQuestion(t, graph)

	if err := graph.RecordStandingWatchDecision(StandingWatchDeclined, "only while I'm around"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	decision, err = graph.StandingWatchDecisionState()
	if err != nil || decision != StandingWatchDeclined {
		t.Fatalf("decision after rebuild = %q err=%v", decision, err)
	}
	if posted, err := graph.OfferStandingWatch("charter-session", charter.ID); err != nil || posted {
		t.Fatalf("offer after decline posted=%t err=%v", posted, err)
	}
}

func assertStandingWatchQuestion(t *testing.T, graph *Store) {
	t.Helper()
	messages, err := graph.Messages("charter-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var questions []Message
	for _, message := range messages {
		if message.QuestionSeq != 0 {
			questions = append(questions, message)
		}
	}
	wantOptions := []QuestionOption{
		{Label: "yes, always", Value: "standing-watch:enable"},
		{Label: "only while I'm around", Value: "standing-watch:decline"},
	}
	if len(questions) != 1 || questions[0].Role != RoleAgent ||
		questions[0].Body != standingWatchBody(standingWatchQuestion, wantOptions) ||
		!reflect.DeepEqual(questions[0].Options, wantOptions) {
		t.Fatalf("standing-watch questions = %+v", questions)
	}
	question, found, err := graph.AgentQuestionBySeq(questions[0].QuestionSeq)
	if err != nil || !found || question.Status != QuestionAsked {
		t.Fatalf("agent question = %+v found=%t err=%v", question, found, err)
	}
}

func TestStandingWatchPassSuppliesLastWake(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "standing-pass.db"))
	if _, found, err := graph.LastStandingWake(); err != nil || found {
		t.Fatalf("initial last wake found=%t err=%v", found, err)
	}
	before := time.Now().UTC()
	if err := graph.RecordStandingWatchPass(StandingWatchPass{Examined: 2, Checked: 1}); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()
	wake, found, err := graph.LastStandingWake()
	if err != nil || !found || wake.Before(before) || wake.After(after) {
		t.Fatalf("last wake = %s found=%t err=%v, bounds %s..%s", wake, found, err, before, after)
	}
}
