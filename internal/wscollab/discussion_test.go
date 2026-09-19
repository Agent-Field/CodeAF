package wscollab

import (
	"context"
	"testing"
)

func TestConflictDiscussionDedupesAncestorsAndRoot(t *testing.T) {
	ctx := context.Background()
	router, err := New(newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	got, err := router.Invite(ctx, "conflict", []Participant{
		{Represents: "billing", Role: "billing"},
		{Represents: "security", Role: "security"},
		{Represents: "parent-a", Role: "parent"},
		{Represents: "parent-a", Role: "parent-again"},
		{Represents: RootID, Role: "root"},
		{Represents: RootID, Role: "root-again"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Participants) != 4 {
		t.Fatalf("want billing, security, one parent, one root; got %+v", got.Participants)
	}
	seen := map[string]int{}
	for _, p := range got.Participants {
		seen[p.Represents]++
		if p.Origin != OriginAgent {
			t.Fatalf("participant origin=%s", p.Origin)
		}
		if p.ActorID == "" {
			t.Fatal("actor id must be minted by software")
		}
	}
	if seen["parent-a"] != 1 || seen[RootID] != 1 {
		t.Fatalf("dedupe failed: %v", seen)
	}

	again, err := router.Invite(ctx, "conflict", []Participant{
		{Represents: "parent-a", Role: "other-parent-path"},
		{Represents: RootID, Role: "root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Participants) != 4 {
		t.Fatalf("second invite enlarged the room: %+v", again.Participants)
	}
}

func TestSelectedScopeDoesNotGrow(t *testing.T) {
	snap := Scope{Kind: ScopeSelected, IDs: []string{"a", "b", "c", "d"}}
	grown := snap.Add("e")
	if grown.Contains("e") {
		t.Fatal("selected coordination must not absorb a sibling filed later")
	}
	if !grown.Contains("a") || len(grown.IDs) != 4 {
		t.Fatalf("%+v", grown)
	}
}

func TestFolderScopeIncludesLaterDescendant(t *testing.T) {
	folder := Scope{Kind: ScopeFolder, FolderID: "billing", IDs: []string{"a", "b", "c", "d"}}
	grown := folder.Add("e")
	if !grown.Contains("e") || len(grown.IDs) != 5 {
		t.Fatalf("dynamic folder scope should include e: %+v", grown)
	}
	if folder.Contains("e") {
		t.Fatal("Add must not mutate the original folder snapshot")
	}
}

func TestUnbindLeavesDeliveriesPending(t *testing.T) {
	ctx := context.Background()
	router, seam := liveRouter(t)
	router.Unbind("chat-a")
	got, err := router.Deliver(ctx, OriginPerson, Message{From: "mgr", Body: "after unbind", CauseID: "u1"}, []string{"chat-a"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Queue != QueuePending || seam.lines() != 0 {
		t.Fatalf("unbound seam still took a line: %+v append=%d", got[0], seam.lines())
	}
}
