package wsapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func TestOrganizeExistingChatsEnqueuesAndCoalesces(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	first, err := svc.OrganizeExistingChats(ctx)
	if err != nil || first.JobID == "" || first.State != organizeQueued {
		t.Fatalf("enqueue: %+v, %v", first, err)
	}
	second, err := svc.OrganizeExistingChats(ctx)
	if err != nil || second.JobID != first.JobID || second.State != organizeQueued {
		t.Fatalf("coalesce: %+v vs %+v, %v", second, first, err)
	}
	status, err := svc.OrganizeStatus(ctx)
	if err != nil || status.JobID != first.JobID || status.State != organizeQueued {
		t.Fatalf("status: %+v, %v", status, err)
	}
	if err := svc.CancelOrganize(ctx); err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.OrganizeStatus(ctx)
	if err != nil || cancelled.State != organizeCancel || cancelled.JobID != first.JobID {
		t.Fatalf("cancel status: %+v, %v", cancelled, err)
	}
	if strings.EqualFold(cancelled.State, "cancelled") || strings.EqualFold(cancelled.Detail, "checked") {
		t.Fatalf("person-facing status leaked store words: %+v", cancelled)
	}
}

func TestOrganizeExistingDoesNotInventFoldersAndKeepsPlacements(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	billing := createFolder(t, svc, "Billing")
	chat := conv("aaaaaaaaaaaaaaaa")
	if err := svc.AddPlacement(ctx, billing.ID, chat, workspace.Provenance{Origin: workspace.OriginPerson, Reason: "file"}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.OrganizeExistingChats(ctx)
	if err != nil || view.State != organizeQueued {
		t.Fatalf("enqueue wiped or failed: %+v, %v", view, err)
	}
	root, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Folders) != 1 || root.Folders[0].ID != billing.ID {
		t.Fatalf("enqueue mutated folders: %+v", root.Folders)
	}
	ids := placementIDs(t, svc, chat)
	if len(ids) != 1 || ids[0] != billing.ID {
		t.Fatalf("enqueue moved the person placement: %v", ids)
	}
}

func TestFakeStoreRefusesTheOrganizeDoor(t *testing.T) {
	svc := testService(t, nil)
	view, err := svc.OrganizeExistingChats(context.Background())
	if err == nil || !errors.Is(err, errOrganizeUnwired) {
		t.Fatalf("fake store must refuse, got %+v, %v", view, err)
	}
	if view.State == organizeDone || view.State == organizeQueued {
		t.Fatalf("unwired door invented success: %+v", view)
	}
}

func TestOrganizerMayCreateFolderOnBlankRoot(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	if _, err := svc.OrganizeExistingChats(ctx); err != nil {
		t.Fatal(err)
	}
	root, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Folders) != 0 {
		t.Fatalf("enqueue invented folders: %+v", root.Folders)
	}
	plan := ActionPlan{
		Kind: PlanCreateFolder, Model: "organize-test", ChatID: "chat-1", SourceRev: "1",
		Actions: []Action{{
			Kind: PlanCreateFolder, FolderName: "Billing", ExpectedRootRevision: root.Revision,
			Evidence: cite("h"),
		}, {
			Kind: PlanAdd, CollectionID: "Billing", Ref: conv("chat-1"),
			Evidence: cite("h"),
		}},
	}
	if err := svc.ValidateActionPlan(ctx, plan); err != nil {
		t.Fatalf("blank-root create+add: %v", err)
	}
	got, err := svc.ApplyActionPlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Applied) == 0 {
		t.Fatal("blank-root create wrote no membership")
	}
	again, err := svc.RootSnapshot(ctx)
	if err != nil || len(again.Folders) != 1 || again.Folders[0].Name != "Billing" {
		t.Fatalf("created folders %+v, %v", again.Folders, err)
	}
}

func TestEmptyStatusIsNotDone(t *testing.T) {
	svc := openSQLiteService(t)
	view, err := svc.OrganizeStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "" || view.JobID != "" {
		t.Fatalf("no job must be empty, not done: %+v", view)
	}
}
