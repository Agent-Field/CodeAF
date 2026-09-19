package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestApplyBatchSecondOpConflictRollsBack(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "collections.db"))
	billing := createTestCollection(t, s, "Billing")
	security := createTestCollection(t, s, "Security")
	chat := Ref{Kind: ConversationKind, ID: "aaaaaaaaaaaaaaaa"}
	_, _, err := s.ApplyBatch(ctx, []BatchOp{
		{
			Kind: ActionAdd, CollectionID: billing.ID, Ref: chat,
			Provenance: Provenance{Origin: OriginOrganizer, ExpectedRevision: billing.Revision, Evidence: "hash-a"},
		},
		{
			Kind: ActionAdd, CollectionID: security.ID, Ref: chat,
			Provenance: Provenance{Origin: OriginOrganizer, ExpectedRevision: security.Revision + 9, Evidence: "hash-a"},
		},
	}, Proposal{ChatID: chat.ID, SourceRev: "1", PlanJSON: "{}", Result: "applied"})
	if err == nil {
		t.Fatal("stale second action must refuse")
	}
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	ids, err := s.Members(ctx, billing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("first action stayed after the second conflicted: %v", ids)
	}
}

func TestRemoveAndSuppressSharesTheWriterTransaction(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "collections.db"))
	billing := createTestCollection(t, s, "Billing")
	chat := Ref{Kind: ConversationKind, ID: "aaaaaaaaaaaaaaaa"}
	if err := s.AddWith(ctx, billing.ID, chat, Provenance{Origin: OriginOrganizer, Evidence: "digest"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveAndSuppress(ctx, billing.ID, chat, "digest", Provenance{Origin: OriginPerson}); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.IsSuppressed(ctx, billing.ID, chat, "digest")
	if err != nil || !blocked {
		t.Fatalf("suppressed=%v err=%v", blocked, err)
	}
	ids, err := s.Members(ctx, billing.ID)
	if err != nil || len(ids) != 0 {
		t.Fatalf("members %v %v", ids, err)
	}
}
