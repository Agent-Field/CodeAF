package wsapi

import (
	"context"
	"errors"
	"path/filepath"
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
	resumed, err := svc.OrganizeExistingChats(ctx)
	if err != nil || resumed.JobID != first.JobID || resumed.State != organizeQueued {
		t.Fatalf("resume after cancel minted %+v vs %s, %v", resumed, first.JobID, err)
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

func TestOrganizeExistingSurvivesRestartWithoutMinting(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	svc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.OrganizeExistingChats(ctx)
	if err != nil || first.JobID == "" || first.State != organizeQueued {
		t.Fatalf("enqueue: %+v, %v", first, err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	status, err := again.OrganizeStatus(ctx)
	if err != nil || status.JobID != first.JobID || status.State != organizeQueued {
		t.Fatalf("reopen status: %+v, %v", status, err)
	}
	second, err := again.OrganizeExistingChats(ctx)
	if err != nil || second.JobID != first.JobID || second.State != organizeQueued {
		t.Fatalf("reopen enqueue minted %+v vs %s, %v", second, first.JobID, err)
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

func TestOrganizeThisChatCoalescesAndDoesNotOptIn(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	opted := false
	svc.SetReactiveOptIn(func() { opted = true })
	svc.SetChatRevision(func(string) string { return "3:deadbeef" })
	first, err := svc.OrganizeThisChat(ctx, "aaaaaaaaaaaaaaaa")
	if err != nil || first.JobID == "" || first.State != organizeQueued {
		t.Fatalf("OrganizeThisChat: %+v, %v", first, err)
	}
	second, err := svc.OrganizeThisChat(ctx, "aaaaaaaaaaaaaaaa")
	if err != nil || second.JobID != first.JobID {
		t.Fatalf("coalesce: %+v vs %s, %v", second, first.JobID, err)
	}
	if opted {
		t.Fatal("Organize this chat opted the workspace into reactive")
	}
	job, err := svc.Workspace().LookupJob(ctx, workspace.JobOrganize, workspace.OrganizeChatKey("aaaaaaaaaaaaaaaa", "3:deadbeef"))
	if err != nil || job.CauseID != workspace.OrganizeThisCause {
		t.Fatalf("cause %+v, %v", job, err)
	}
}

func TestOrganizeExistingOptsInWhenWired(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	opted := 0
	svc.SetReactiveOptIn(func() { opted++ })
	if _, err := svc.OrganizeExistingChats(ctx); err != nil {
		t.Fatal(err)
	}
	if opted != 1 {
		t.Fatalf("opt-in count %d, want 1", opted)
	}
}

func TestCreateFolderInNestsWithoutARootOrphan(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	billing, err := svc.CreateFolder(ctx, "Billing")
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := svc.CreateFolderIn(ctx, "Receipts", billing.ID)
	if err != nil || receipts.Name != "Receipts" || len(receipts.ParentIDs) != 1 || receipts.ParentIDs[0] != billing.ID {
		t.Fatalf("CreateFolderIn: %+v, %v", receipts, err)
	}
	root, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range root.Folders {
		if folder.ID == receipts.ID {
			t.Fatalf("nested folder appeared at Root: %+v", root.Folders)
		}
	}
	_, err = svc.CreateFolderIn(ctx, "Orphan", "missing")
	if err == nil {
		t.Fatal("missing parent succeeded")
	}
	inbox, err := svc.CreateFolderIn(ctx, "Inbox", "")
	if err != nil || inbox.Name != "Inbox" || len(inbox.ParentIDs) != 0 {
		t.Fatalf("empty parent should be Root: %+v, %v", inbox, err)
	}
}

func TestFakeStoreRefusesCreateFolderInAndOrganizeThisChat(t *testing.T) {
	svc := testService(t, nil)
	_, err := svc.CreateFolderIn(context.Background(), "Receipts", "billing")
	if err == nil || !errors.Is(err, errOrganizeUnwired) {
		t.Fatalf("fake CreateFolderIn: %v", err)
	}
	view, err := svc.OrganizeThisChat(context.Background(), "aaaaaaaaaaaaaaaa")
	if err == nil || !errors.Is(err, errOrganizeUnwired) {
		t.Fatalf("fake OrganizeThisChat: %+v, %v", view, err)
	}
}
