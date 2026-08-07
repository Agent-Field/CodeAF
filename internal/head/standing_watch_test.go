package head

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestStandingWatchQuestionChoicesBecomeGlobalCommandsWithoutProviderCall(t *testing.T) {
	for _, test := range []struct {
		reply string
		kind  store.CommandKind
	}{
		{reply: "yes", kind: store.CommandStandingWatchEnable},
		{reply: "no", kind: store.CommandStandingWatchDecline},
	} {
		t.Run(test.reply, func(t *testing.T) {
			graph := openHeadStore(t)
			charter, err := store.NewCharter("watch-choice", "Keep releases documented.", store.WatchSpec{
				Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for a release", Cadence: time.Hour},
			}, "Did a release land?", store.CharterAction{Template: "Update the release notes"},
				store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
				store.Ratification{Origin: store.OriginUser, SessionID: "watch-head", Evidence: "yes"})
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.CreateCharter(charter); err != nil {
				t.Fatal(err)
			}
			if posted, err := graph.OfferStandingWatch("watch-head", charter.ID); err != nil || !posted {
				t.Fatalf("offer posted=%t err=%v", posted, err)
			}
			user, err := graph.PostMessage(store.Message{SessionID: "watch-head", Role: store.RoleUser, Body: test.reply})
			if err != nil {
				t.Fatal(err)
			}
			client := &fakeClient{}
			if err := New(client, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if client.callCount() != 0 {
				t.Fatal("standing-watch choice fell through to provider")
			}
			commands, err := graph.PendingCommands(0)
			if err != nil || len(commands) != 1 || commands[0].Kind != test.kind || commands[0].Target != "" {
				t.Fatalf("commands = %+v err=%v", commands, err)
			}
		})
	}
}

func TestUnrelatedReplyNeverSpendsTheSingleStandingWatchQuestion(t *testing.T) {
	graph := openHeadStore(t)
	charter, err := store.NewCharter("watch-freetext", "Keep releases documented.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for a release", Cadence: time.Hour},
	}, "Did a release land?", store.CharterAction{Template: "Update the release notes"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "watch-head", Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	if posted, err := graph.OfferStandingWatch("watch-head", charter.ID); err != nil || !posted {
		t.Fatalf("offer posted=%t err=%v", posted, err)
	}
	questions, err := graph.UnresolvedQuestions(10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("unresolved questions = %+v err=%v", questions, err)
	}
	user, err := graph.PostMessage(store.Message{
		SessionID: "watch-head", Role: store.RoleUser, Body: "what does that actually change?",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{
		`{"reply":"It decides whether checks continue with no terminal open.","command":null}`,
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("free text produced commands = %+v err=%v", commands, err)
	}
	decision, err := graph.StandingWatchDecisionState()
	if err != nil || decision != store.StandingWatchOffered {
		t.Fatalf("decision = %q err=%v", decision, err)
	}
	still, err := graph.UnresolvedQuestions(10)
	if err != nil || len(still) != 1 {
		t.Fatalf("standing-watch question was spent by free text: %+v err=%v", still, err)
	}
}

func TestStandingWatchGroundingIsUsedOnlyForPresenceQuestions(t *testing.T) {
	if !asksForStandingWatch("who's keeping watch?") || !asksForStandingWatch("does this run with no terminal open?") {
		t.Fatal("presence question was not recognized")
	}
	if asksForStandingWatch("watch this file for changes") {
		t.Fatal("new work request was mistaken for a status question")
	}
}

func TestStandingWatchGroundingReachesRoutingPrompt(t *testing.T) {
	graph := openHeadStore(t)
	user, err := graph.PostMessage(store.Message{
		SessionID: "watch-grounding", Role: store.RoleUser, Body: "who's keeping watch?",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{`{"reply":"I am, with the next quiet check in three minutes.","command":null}`}}
	if err := New(client, graph).WithStandingWatch(func() string {
		return "standing watch   installed · last wake 2m ago · next check in 3m"
	}).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) != 2 ||
		!strings.Contains(client.seen[1].Content[0].Text, "Standing-watch status (ground truth") ||
		!strings.Contains(client.seen[1].Content[0].Text, "next check in 3m") {
		t.Fatalf("standing-watch evidence did not reach head: %+v", client.seen)
	}
}
