package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

func TestMetaRetrospectiveFifthRunTunesOneNotchAndReflectsPhrase(t *testing.T) {
	graph := openStore(t)
	// Beliefs the consolidator aged out and the person put straight back: every
	// one of them a reversal, which is the evidence the aging dial reads.
	for index := 0; index < MetaReversalMinSamples; index++ {
		fact, err := graph.RecordFact(store.RootID, "repo:codeaf", store.FactLesson,
			fmt.Sprintf("keep lesson %d about the parser fixtures", index))
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.QuarantineFact(fact.Seq, 0, store.FactOriginConsolidator); err != nil {
			t.Fatal(err)
		}
		if err := graph.RestoreFact(fact.Seq, store.FactOriginUser); err != nil {
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
	if change.Name != store.ParameterBeliefRetentionThreshold || change.Old-change.New != 0.25 {
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
	if !strings.Contains(found, "tightened belief aging (6/6 reversals)") {
		t.Fatalf("reflected phrase=%q", found)
	}
}
