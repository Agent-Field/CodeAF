package wsapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func TestNoActionWritesNoMembership(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	plan := ActionPlan{Kind: PlanNoAction, ChatID: "chat-1", SourceRev: "rev-1", Model: "organize-test"}
	got, err := svc.ApplyActionPlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanID == "" {
		t.Fatal("no-action must record a proposed_actions row")
	}
	if len(got.Applied) != 0 {
		t.Fatalf("no-action applied membership: %+v", got.Applied)
	}
	if ids := placementIDs(t, svc, conv("chat-1")); len(ids) != 0 {
		t.Fatalf("no-action filed the chat: %v", ids)
	}
	_, placed, err := svc.FolderSnapshot(ctx, billing.ID)
	if err != nil || len(placed) != 0 {
		t.Fatalf("billing gained members: %v, %v", placed, err)
	}
}

func TestUnknownPlanKindIsInvalid(t *testing.T) {
	svc := testService(t, nil)
	err := svc.ValidateActionPlan(context.Background(), ActionPlan{Kind: "sql"})
	if !errors.Is(err, workspace.ErrInvalid) {
		t.Fatalf("unknown kind: %v", err)
	}
}

func TestKeywordOnlyFilerRefused(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	plan := addPlan(billing, conv("chat-1"), "hash-a")
	plan.Model = ""
	if err := svc.ValidateActionPlan(ctx, plan); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "keyword-only") {
		t.Fatalf("keyword-only: %v", err)
	}
}

func TestDegradedPlanMustNotApplyMembership(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	plan := addPlan(billing, conv("chat-1"), "hash-a")
	plan.Degraded = true
	if err := svc.ValidateActionPlan(ctx, plan); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "degraded") {
		t.Fatalf("degraded: %v", err)
	}
	degradedNone := ActionPlan{Kind: PlanNoAction, Degraded: true, ChatID: "chat-1", SourceRev: "1"}
	if err := svc.ValidateActionPlan(ctx, degradedNone); err != nil {
		t.Fatalf("degraded no-action must be allowed: %v", err)
	}
}

func TestMissingCollectionIDIsInvalid(t *testing.T) {
	svc := testService(t, nil)
	plan := ActionPlan{
		Kind: PlanAdd, Model: "organize-test",
		Actions: []Action{{Kind: PlanAdd, CollectionID: "missing", Ref: conv("chat-1"), ExpectedRevision: 1, Evidence: cite("h")}},
	}
	if err := svc.ValidateActionPlan(context.Background(), plan); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatalf("missing id: %v", err)
	}
}

func TestCreateFolderCycleIsRefused(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	parent := createFolder(t, svc, "Billing")
	child := createFolder(t, svc, "Receipts")
	if err := svc.AddPlacement(ctx, parent.ID, collectionRef(child.ID), workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	plan := ActionPlan{
		Kind: PlanAdd, Model: "organize-test",
		Actions: []Action{{
			Kind: PlanAdd, CollectionID: child.ID, Ref: collectionRef(parent.ID),
			ExpectedRevision: folderRevision(t, svc, child.ID), Evidence: cite("h"),
		}},
	}
	if err := svc.ValidateActionPlan(ctx, plan); !errors.Is(err, workspace.ErrCycle) {
		t.Fatalf("cycle: %v", err)
	}
}

func TestOrganizerApplyNeedsNonZeroRevision(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	plan := addPlan(billing, conv("chat-1"), "hash-a")
	plan.Actions[0].ExpectedRevision = 0
	if err := svc.ValidateActionPlan(ctx, plan); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("zero revision: %v", err)
	}
}

func TestApplyActionPlanStaleRevisionIsConflict(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	billing := createFolder(t, svc, "Billing")
	chat := conv("chat-stale")
	plan := addPlan(billing, chat, "hash-stale")
	plan.Actions[0].ExpectedRevision = billing.Revision + 9
	_, err := svc.ApplyActionPlan(ctx, plan)
	if err == nil {
		t.Fatal("stale apply must refuse")
	}
	if !errors.Is(err, workspace.ErrConflict) || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("stale: %v", err)
	}
	if ids := membershipIDs(t, svc, billing.ID); len(ids) != 0 {
		t.Fatalf("stale apply wrote membership: %v", ids)
	}
}

func TestApplyActionPlanSecondActionConflictRollsBack(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	billing := createFolder(t, svc, "Billing")
	security := createFolder(t, svc, "Security")
	chat := conv("chat-partial")
	plan := ActionPlan{
		Kind: PlanAdd, ChatID: chat.ID, SourceRev: "1", Model: "organize-test",
		Actions: []Action{
			{
				Kind: PlanAdd, CollectionID: billing.ID, Ref: chat,
				ExpectedRevision: billing.Revision, Evidence: cite("hash-a"), Reason: "related",
			},
			{
				Kind: PlanAdd, CollectionID: security.ID, Ref: chat,
				ExpectedRevision: security.Revision + 9, Evidence: cite("hash-a"), Reason: "related",
			},
		},
	}
	_, err := svc.ApplyActionPlan(ctx, plan)
	if err == nil {
		t.Fatal("second-action conflict must refuse")
	}
	if !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if ids := membershipIDs(t, svc, billing.ID); len(ids) != 0 {
		t.Fatalf("first action stayed after the second conflicted: %v", ids)
	}
	if ids := membershipIDs(t, svc, security.ID); len(ids) != 0 {
		t.Fatalf("second action wrote membership: %v", ids)
	}
}

func TestOrganizerCannotRemovePersonPlacement(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	chat := conv("person-chat")
	if err := svc.AddPlacement(ctx, billing.ID, chat, workspace.Provenance{Origin: workspace.OriginPerson, Reason: "filed by me"}); err != nil {
		t.Fatal(err)
	}
	plan := ActionPlan{
		Kind: PlanRemove, Model: "organize-test", ChatID: "person-chat", SourceRev: "1",
		Actions: []Action{{
			Kind: PlanRemove, CollectionID: billing.ID, Ref: chat,
			ExpectedRevision: folderRevision(t, svc, billing.ID), Reason: "undo",
		}},
	}
	if err := svc.ValidateActionPlan(ctx, plan); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "person") {
		t.Fatalf("person placement: %v", err)
	}
}

func TestSuppressPlacementBlocksIdenticalEvidence(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	chat := conv("auto-chat")
	hash := evidenceHash(cite("same-passages"))
	filed := addPlan(billing, chat, "same-passages")
	got, err := svc.ApplyActionPlan(ctx, filed)
	if err != nil || len(got.Applied) != 1 || got.Applied[0].Origin != workspace.OriginOrganizer {
		t.Fatalf("apply %+v, %v", got, err)
	}
	if err := svc.RemovePlacement(ctx, billing.ID, chat, workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SuppressPlacement(ctx, billing.ID, chat, hash, workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	billing, _, err = svc.FolderSnapshot(ctx, billing.ID)
	if err != nil {
		t.Fatal(err)
	}
	again := addPlan(billing, chat, "same-passages")
	again.SourceRev = "2"
	if err := svc.ValidateActionPlan(ctx, again); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "suppressed") {
		t.Fatalf("identical evidence: %v", err)
	}
	fresh := addPlan(billing, chat, "new-passages")
	fresh.SourceRev = "3"
	if err := svc.ValidateActionPlan(ctx, fresh); err != nil {
		t.Fatalf("new evidence must be allowed: %v", err)
	}
}

func TestEquivalentFolderNameIsNoAction(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	createFolder(t, svc, "Security")
	root, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan := ActionPlan{
		Kind: PlanCreateFolder, Model: "organize-test",
		Actions: []Action{{
			Kind: PlanCreateFolder, FolderName: "Security", ExpectedRootRevision: root.Revision,
			Evidence: cite("h"),
		}},
	}
	if err := svc.ValidateActionPlan(ctx, plan); !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "equivalent") {
		t.Fatalf("equivalent: %v", err)
	}
}

func TestApplyActionPlanStampsOrganizerOrigin(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	security := createFolder(t, svc, "Security")
	chat := conv("new-billing")
	got, err := svc.ApplyActionPlan(ctx, addPlan(security, chat, "receipt-auth"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Applied) != 1 || got.Applied[0].Origin != workspace.OriginOrganizer {
		t.Fatalf("origin %+v", got.Applied)
	}
	why, err := svc.WhyHere(ctx, security.ID, chat)
	if err != nil || why.Event.Origin != workspace.OriginOrganizer {
		t.Fatalf("why-here %+v, %v", why, err)
	}
}

func cite(hash string) []EvidenceRef {
	return []EvidenceRef{{SourceRef: "chat:old", PassageHash: hash, Quote: "authenticate receipt links"}}
}

func addPlan(folder Folder, ref workspace.Ref, hash string) ActionPlan {
	return ActionPlan{
		Kind: PlanAdd, ChatID: ref.ID, SourceRev: "1", Model: "organize-test", PromptVersion: "1",
		Actions: []Action{{
			Kind: PlanAdd, CollectionID: folder.ID, Ref: ref,
			ExpectedRevision: folder.Revision, Evidence: cite(hash), Reason: "same receipt links",
		}},
	}
}
