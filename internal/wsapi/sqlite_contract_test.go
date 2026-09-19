package wsapi

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func TestSQLiteMoveCycleAndConflictRollBack(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	parent := createFolder(t, svc, "Billing")
	child := createFolder(t, svc, "Receipts")
	nest := workspace.Provenance{Origin: workspace.OriginPerson, Reason: "nest"}
	if err := svc.AddPlacement(ctx, parent.ID, collectionRef(child.ID), nest); err != nil {
		t.Fatal(err)
	}
	beforeParent, beforeChild := membershipIDs(t, svc, parent.ID), membershipIDs(t, svc, child.ID)
	beforeEvents := len(storeEvents(t, svc, parent.ID, collectionRef(child.ID))) + len(storeEvents(t, svc, child.ID, collectionRef(parent.ID)))

	err := svc.MovePlacement(ctx, parent.ID, child.ID, collectionRef(parent.ID), workspace.Provenance{Reason: "cycle"})
	if !errors.Is(err, workspace.ErrCycle) {
		t.Fatalf("cycle move: %v", err)
	}
	if got := membershipIDs(t, svc, parent.ID); !sameStrings(got, beforeParent) {
		t.Fatalf("cycle mutated parent members: %v", got)
	}
	if got := membershipIDs(t, svc, child.ID); !sameStrings(got, beforeChild) {
		t.Fatalf("cycle mutated child members: %v", got)
	}
	afterEvents := len(storeEvents(t, svc, parent.ID, collectionRef(child.ID))) + len(storeEvents(t, svc, child.ID, collectionRef(parent.ID)))
	if afterEvents != beforeEvents {
		t.Fatalf("cycle wrote extra events: before %d after %d", beforeEvents, afterEvents)
	}

	chat := conv("shared")
	if err := svc.AddPlacement(ctx, parent.ID, chat, workspace.Provenance{Reason: "file"}); err != nil {
		t.Fatal(err)
	}
	parentSnap, _, err := svc.FolderSnapshot(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	childSnap, _, err := svc.FolderSnapshot(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeChatParent := membershipIDs(t, svc, parent.ID)
	beforeChatChild := membershipIDs(t, svc, child.ID)
	beforeChatEvents := len(storeEvents(t, svc, parent.ID, chat)) + len(storeEvents(t, svc, child.ID, chat))
	stale := workspace.Provenance{Origin: workspace.OriginPerson, ExpectedFrom: parentSnap.Revision + 9, ExpectedTo: childSnap.Revision}
	err = svc.MovePlacement(ctx, parent.ID, child.ID, chat, stale)
	if !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("conflict move: %v", err)
	}
	if got := membershipIDs(t, svc, parent.ID); !sameStrings(got, beforeChatParent) {
		t.Fatalf("conflict mutated parent members: %v", got)
	}
	if got := membershipIDs(t, svc, child.ID); !sameStrings(got, beforeChatChild) {
		t.Fatalf("conflict mutated child members: %v", got)
	}
	afterChatEvents := len(storeEvents(t, svc, parent.ID, chat)) + len(storeEvents(t, svc, child.ID, chat))
	if afterChatEvents != beforeChatEvents {
		t.Fatalf("conflict wrote extra events: before %d after %d", beforeChatEvents, afterChatEvents)
	}
}

func TestSQLiteStaleExpectedFromOrToIsConflict(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	billing := createFolder(t, svc, "Billing")
	receipts := createFolder(t, svc, "Receipts")
	chat := conv("filed")
	if err := svc.AddPlacement(ctx, billing.ID, chat, workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	from, _, err := svc.FolderSnapshot(ctx, billing.ID)
	if err != nil {
		t.Fatal(err)
	}
	to, _, err := svc.FolderSnapshot(ctx, receipts.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents := len(storeEvents(t, svc, billing.ID, chat)) + len(storeEvents(t, svc, receipts.ID, chat))

	wrongFrom := workspace.Provenance{Origin: workspace.OriginPerson, ExpectedFrom: from.Revision + 1, ExpectedTo: to.Revision}
	if err := svc.MovePlacement(ctx, billing.ID, receipts.ID, chat, wrongFrom); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("stale ExpectedFrom: %v", err)
	}
	wrongTo := workspace.Provenance{Origin: workspace.OriginPerson, ExpectedFrom: from.Revision, ExpectedTo: to.Revision + 1}
	if err := svc.MovePlacement(ctx, billing.ID, receipts.ID, chat, wrongTo); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("stale ExpectedTo: %v", err)
	}
	if got := membershipIDs(t, svc, billing.ID); !sameStrings(got, []string{"filed"}) {
		t.Fatalf("stale CAS moved the source: %v", got)
	}
	if got := membershipIDs(t, svc, receipts.ID); len(got) != 0 {
		t.Fatalf("stale CAS wrote the destination: %v", got)
	}
	afterEvents := len(storeEvents(t, svc, billing.ID, chat)) + len(storeEvents(t, svc, receipts.ID, chat))
	if afterEvents != beforeEvents {
		t.Fatalf("stale CAS wrote an event: before %d after %d", beforeEvents, afterEvents)
	}
}

func TestSQLiteMembershipAndEventShareOneTransaction(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "crash.db")
	svc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	billing := createFolder(t, svc, "Billing")
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	installAbortingEventTrigger(t, path)

	blocked, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = blocked.AddPlacement(ctx, billing.ID, conv("crash-chat"), workspace.Provenance{Origin: workspace.OriginPerson, Reason: "should roll back"})
	if err == nil {
		t.Fatal("injected event failure must abort the write")
	}
	if got := membershipIDs(t, blocked, billing.ID); len(got) != 0 {
		t.Fatalf("membership survived the aborted event insert: %v", got)
	}
	if events := storeEvents(t, blocked, billing.ID, conv("crash-chat")); len(events) != 0 {
		t.Fatalf("event survived without a membership: %+v", events)
	}
	if err := blocked.Close(); err != nil {
		t.Fatal(err)
	}
	dropAbortingEventTrigger(t, path)

	svc, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.AddPlacement(ctx, billing.ID, conv("ok-chat"), workspace.Provenance{Origin: workspace.OriginPerson, Reason: "both"}); err != nil {
		t.Fatal(err)
	}
	if got := membershipIDs(t, svc, billing.ID); !sameStrings(got, []string{"ok-chat"}) {
		t.Fatalf("successful add missed membership: %v", got)
	}
	events := storeEvents(t, svc, billing.ID, conv("ok-chat"))
	if len(events) != 1 || events[0].Action != workspace.ActionAdd {
		t.Fatalf("successful add missed the event: %+v", events)
	}
}

func TestSQLiteRevisionsIncrementOnSuccess(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	billing := createFolder(t, svc, "Billing")
	receipts := createFolder(t, svc, "Receipts")
	root, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if root.Revision < 1 {
		t.Fatalf("root revision after create: %d", root.Revision)
	}
	beforeAdd := folderRevision(t, svc, billing.ID)
	if err := svc.AddPlacement(ctx, billing.ID, conv("filed"), workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	afterAdd, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterAdd.Revision != root.Revision+1 {
		t.Fatalf("add root revision %d, want %d", afterAdd.Revision, root.Revision+1)
	}
	if folderRevision(t, svc, billing.ID) != beforeAdd+1 {
		t.Fatalf("add collection revision %d, want %d", folderRevision(t, svc, billing.ID), beforeAdd+1)
	}

	fromRev := folderRevision(t, svc, billing.ID)
	toRev := folderRevision(t, svc, receipts.ID)
	if err := svc.MovePlacement(ctx, billing.ID, receipts.ID, conv("filed"), workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	afterMove, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterMove.Revision != afterAdd.Revision+1 {
		t.Fatalf("move root revision %d, want %d", afterMove.Revision, afterAdd.Revision+1)
	}
	if folderRevision(t, svc, billing.ID) != fromRev+1 || folderRevision(t, svc, receipts.ID) != toRev+1 {
		t.Fatalf("move collection revisions from=%d to=%d", folderRevision(t, svc, billing.ID), folderRevision(t, svc, receipts.ID))
	}
}

func TestSQLiteIdempotencyKeyReplay(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	billing := createFolder(t, svc, "Billing")
	receipts := createFolder(t, svc, "Receipts")
	chat := conv("keyed")
	key := workspace.Provenance{Origin: workspace.OriginPerson, Reason: "file it", IdempotencyKey: "add-1"}
	if err := svc.AddPlacement(ctx, billing.ID, chat, key); err != nil {
		t.Fatal(err)
	}
	root, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, billing.ID, chat, key); err != nil {
		t.Fatalf("same-operation replay: %v", err)
	}
	replayed, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != root.Revision {
		t.Fatalf("replay bumped root from %d to %d", root.Revision, replayed.Revision)
	}
	if events := storeEvents(t, svc, billing.ID, chat); len(events) != 1 {
		t.Fatalf("replay wrote extra events: %+v", events)
	}

	other := workspace.Provenance{Origin: workspace.OriginPerson, Reason: "other folder", IdempotencyKey: "add-1"}
	err = svc.AddPlacement(ctx, receipts.ID, chat, other)
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "different operation") {
		t.Fatalf("different add with same key: %v", err)
	}
	err = svc.MovePlacement(ctx, billing.ID, receipts.ID, chat, workspace.Provenance{Origin: workspace.OriginPerson, IdempotencyKey: "add-1"})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "different operation") {
		t.Fatalf("move with add's key: %v", err)
	}
	if got := membershipIDs(t, svc, billing.ID); !sameStrings(got, []string{"keyed"}) {
		t.Fatalf("refused key reuse moved the chat: %v", got)
	}
	if got := membershipIDs(t, svc, receipts.ID); len(got) != 0 {
		t.Fatalf("refused key reuse wrote a second placement: %v", got)
	}

	moveKey := workspace.Provenance{Origin: workspace.OriginPerson, Reason: "moved", IdempotencyKey: "move-1"}
	if err := svc.MovePlacement(ctx, billing.ID, receipts.ID, chat, moveKey); err != nil {
		t.Fatal(err)
	}
	if err := svc.MovePlacement(ctx, billing.ID, receipts.ID, chat, moveKey); err != nil {
		t.Fatalf("same move replay: %v", err)
	}
	if got := membershipIDs(t, svc, billing.ID); len(got) != 0 {
		t.Fatalf("move left the source: %v", got)
	}
	if got := membershipIDs(t, svc, receipts.ID); !sameStrings(got, []string{"keyed"}) {
		t.Fatalf("move missed the destination: %v", got)
	}
}

func TestSQLiteOpenRefusesForeignAndDamagedFiles(t *testing.T) {
	ctx := context.Background()
	foreign := filepath.Join(t.TempDir(), "foreign.db")
	db, err := sql.Open("sqlite", foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE memories(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := Open(foreign)
	if err == nil {
		_ = svc.Close()
		t.Fatal("foreign store must not open")
	}
	if svc != nil {
		t.Fatalf("Open must not construct a service on a foreign file, got %+v", svc)
	}
	root, snapErr := (*Service)(nil).RootSnapshot(ctx)
	if snapErr == nil || len(root.Folders) != 0 || len(root.Unfiled) != 0 {
		t.Fatalf("foreign Open must not invent a RootView: %+v, %v", root, snapErr)
	}

	damaged := filepath.Join(t.TempDir(), "damaged.db")
	if err := os.WriteFile(damaged, []byte("this is not a collections database"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err = Open(damaged)
	if err == nil {
		_ = svc.Close()
		t.Fatal("damaged store must not open")
	}
	if svc != nil {
		t.Fatalf("Open must not construct a service on a damaged file, got %+v", svc)
	}
}

func TestSQLiteOpenBindsWorkspaceStore(t *testing.T) {
	svc := openSQLiteService(t)
	if _, ok := svc.store.(*workspace.Store); !ok {
		t.Fatalf("Open must bind *workspace.Store, got %T", svc.store)
	}
}

func openSQLiteService(t *testing.T) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collections.db")
	svc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func membershipIDs(t *testing.T, svc *Service, collectionID string) []string {
	t.Helper()
	_, placed, err := svc.FolderSnapshot(context.Background(), collectionID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(placed))
	for _, row := range placed {
		ids = append(ids, row.Ref.ID)
	}
	return ids
}

func storeEvents(t *testing.T, svc *Service, collectionID string, ref workspace.Ref) []workspace.MembershipEvent {
	t.Helper()
	events, err := svc.store.Events(context.Background(), collectionID, ref)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func folderRevision(t *testing.T, svc *Service, id string) int {
	t.Helper()
	folder, _, err := svc.FolderSnapshot(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return folder.Revision
}

func installAbortingEventTrigger(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TRIGGER abort_membership_events BEFORE INSERT ON membership_events
BEGIN SELECT RAISE(ABORT, 'injected event failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
}

func dropAbortingEventTrigger(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TRIGGER abort_membership_events"); err != nil {
		t.Fatal(err)
	}
}
