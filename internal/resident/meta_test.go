package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestMetaRetrospectiveFifthRunTunesOneNotchAndReflectsPhrase(t *testing.T) {
	graph := openStore(t)
	for index := 0; index < MetaReversalMinSamples; index++ {
		id := fmt.Sprintf("meta-proposal-%d", index)
		charter, err := store.NewCharter(id, "watch recurring meta request", store.WatchSpec{Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "due", Cadence: time.Hour}}, "due", store.CharterAction{Template: "do it"}, store.CharterRails{PerFiringBudgetUSD: .1, MaxFiringsPerDay: 1}, store.CharterProposed, store.Ratification{})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.CreateCharter(charter.WithProposalShape("shape-" + id)); err != nil {
			t.Fatal(err)
		}
		if err := graph.DeclineCharterProposal(id, "too frequent"); err != nil {
			t.Fatal(err)
		}
	}
	for run := 1; run <= MetaRetrospectiveEvery; run++ {
		if _, err := graph.CheckpointRetrospective(run); err != nil {
			t.Fatal(err)
		}
	}
	reconciler := New(graph, nil, nil)
	if err := reconciler.SessionOpened(context.Background(), "meta-reflection", "tui", time.Hour); err != nil {
		t.Fatal(err)
	}
	after := reconciler.latestEventSeq()
	changes := reconciler.metaRetrospect()
	if len(changes) != 1 {
		t.Fatalf("changes=%+v", changes)
	}
	change := changes[0]
	if change.Name != store.ParameterProposalCadenceRuns || change.New-change.Old != 1 {
		t.Fatalf("one-notch change=%+v", change)
	}
	reconciler.postRetrospectiveDigest(after)
	messages, err := graph.Messages("meta-reflection", after, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, message := range messages {
		if strings.HasPrefix(message.Body, "· reflected —") {
			found = message.Body
		}
	}
	if !strings.Contains(found, "tightened proposal cadence (6/6 reversals)") {
		t.Fatalf("reflected phrase=%q", found)
	}
}
