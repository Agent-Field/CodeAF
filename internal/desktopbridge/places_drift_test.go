package desktopbridge

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/placeorg/stream"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSettledDriftEarnsAHomeButMovesThePrimaryOnlyOnAcceptance(t *testing.T) {
	rig := newAdviceRig(t, false)
	primary, destination := rig.place("Beginning"), rig.place("Ending")
	if _, _, err := rig.store.AddChat("chat-1", primary, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	tree := placeorg.TreeFromNodes([]placeorg.Node{
		{ID: primary, Name: "Beginning", Depth: 1, Members: []string{"chat-1"}, Centroids: map[placeorg.SpaceID]placeorg.Centroid{placeorg.SpacePrimary: {Sum: []float32{1, 0}, Count: 1}}},
		{ID: destination, Name: "Ending", Depth: 1, Centroids: map[placeorg.SpaceID]placeorg.Centroid{placeorg.SpacePrimary: {Sum: []float32{0, 1}, Count: 1}}},
	})
	var mu sync.Mutex
	var received []placeorg.Chat
	var proposal string
	rig.advice.Settled = func(ctx context.Context, chat placeorg.Chat) error {
		mu.Lock()
		received = append(received, chat)
		mu.Unlock()
		ops, err := stream.New().Drift(ctx, tree, placeorg.Arrival{Chat: chat, Vector: []float32{1, 0}}, []float32{0, 1}, nil)
		if err != nil {
			return err
		}
		rec := rig.advice.recommender(nil)
		for _, op := range ops {
			if op.Kind == placeorg.OpLink {
				_, err = rig.store.ApplyPlan([]placegraph.PlanOp{{Kind: placegraph.PlanLink, PlaceID: op.Place, ChatID: op.Chat}})
			} else if op.Proposed {
				p, e := rec.ProposePlan("drift-chat-1", []placegraph.PlanOp{{Kind: placegraph.PlanMove, PlaceID: op.Place, FromPlaceID: primary, ChatID: op.Chat}}, op.Why)
				err, proposal = e, p.ID
			} else {
				t.Errorf("primary move did not wait: %+v", op)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	id := rig.open()
	rig.settle(id, true)
	rig.idle()
	mu.Lock()
	if len(received) != 0 {
		t.Fatal("single-message chat supplied an END")
	}
	mu.Unlock()
	rig.agent.mu.Lock()
	rig.agent.entries = append(rig.agent.entries,
		session.DisplayEntry{Role: "user", Text: "Now we are discussing the ending."},
		session.DisplayEntry{Role: "assistant", Text: "Here is the final answer."},
		session.DisplayEntry{Role: "assistant", Text: "interrupted output", Interrupted: true},
		session.DisplayEntry{Role: "assistant", Text: "tool output", Tool: "read"},
	)
	rig.agent.mu.Unlock()
	rig.settle(id, false)
	rig.idle()
	mu.Lock()
	if len(received) != 0 {
		t.Fatal("interrupted stream reached drift")
	}
	mu.Unlock()
	rig.settle(id, true)
	rig.idle()
	mu.Lock()
	if len(received) != 1 || !strings.Contains(received[0].EndText, "final answer") || strings.Contains(received[0].EndText, "tool output") || strings.Contains(received[0].EndText, "interrupted output") {
		t.Fatalf("settled evidence: %+v", received)
	}
	if !strings.Contains(received[0].Text, "release pipeline fails") {
		t.Fatalf("settled hook lost START evidence: %+v", received[0])
	}
	mu.Unlock()
	snap, err := rig.store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	homes, links := 0, 0
	for _, m := range snap.Memberships {
		if m.ChatID != "chat-1" {
			continue
		}
		if m.Link && m.PlaceID == destination {
			links++
		}
		if !m.Link && m.PlaceID == primary {
			homes++
		}
	}
	if homes != 1 || links != 1 || proposal == "" {
		t.Fatalf("homes %d links %d proposal %q", homes, links, proposal)
	}
	if _, err := rig.advice.recommender(nil).Accept(proposal, placegraph.AcceptEdit{}); err != nil {
		t.Fatal(err)
	}
	snap, _ = rig.store.Snapshot()
	for _, m := range snap.Memberships {
		if m.ChatID == "chat-1" && !m.Link && m.PlaceID != destination {
			t.Fatalf("accepted primary did not move: %+v", m)
		}
	}
	rig.settle(id, true)
	rig.idle()
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("considered chat suppressed later drift: %d", len(received))
	}
}
