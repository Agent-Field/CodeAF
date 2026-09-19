package wsapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func TestAddMoveRemoveAndIdempotency(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, orderedInventory{
		ids:    []string{"chat-1"},
		titles: map[string]string{"chat-1": "Receipts talk"},
	})
	billing := createFolder(t, svc, "Billing")
	receipts := createFolder(t, svc, "Receipts")
	security := createFolder(t, svc, "Security")
	chat := conv("chat-1")
	filed := workspace.Provenance{Origin: workspace.OriginPerson, Reason: "filed by the person"}
	if err := svc.AddPlacement(ctx, billing.ID, chat, filed); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, billing.ID, chat, filed); err != nil {
		t.Fatalf("idempotent add: %v", err)
	}
	if err := svc.AddPlacement(ctx, security.ID, chat, workspace.Provenance{Reason: "also security"}); err != nil {
		t.Fatal(err)
	}
	if got := placementIDs(t, svc, chat); !sameStrings(got, []string{billing.ID, security.ID}) {
		t.Fatalf("after dual add: %v", got)
	}
	_, placed, err := svc.FolderSnapshot(ctx, billing.ID)
	if err != nil || len(placed) != 1 || placed[0].Ref.ID != "chat-1" {
		t.Fatalf("billing snapshot %v, %v", placed, err)
	}
	if err := svc.MovePlacement(ctx, billing.ID, receipts.ID, chat, workspace.Provenance{Reason: "moved into receipts"}); err != nil {
		t.Fatal(err)
	}
	if got := placementIDs(t, svc, chat); !sameStrings(got, []string{receipts.ID, security.ID}) {
		t.Fatalf("after move: %v", got)
	}
	if err := svc.RemovePlacement(ctx, security.ID, chat, workspace.Provenance{Reason: "not security"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemovePlacement(ctx, security.ID, chat, workspace.Provenance{Reason: "not security"}); err != nil {
		t.Fatalf("idempotent remove: %v", err)
	}
	if got := placementIDs(t, svc, chat); !sameStrings(got, []string{receipts.ID}) {
		t.Fatalf("after remove: %v", got)
	}
	why, err := svc.WhyHere(ctx, receipts.ID, chat)
	if err != nil || why.Event.Action != workspace.ActionAdd || why.Event.Reason != "moved into receipts" {
		t.Fatalf("why here %+v, %v", why, err)
	}
}

func TestUniqueCountsWithDualPlacement(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	acme := createFolder(t, svc, "Acme")
	billing := createFolder(t, svc, "Billing")
	security := createFolder(t, svc, "Security")
	nest := workspace.Provenance{Origin: workspace.OriginPerson, Reason: "nest"}
	if err := svc.AddPlacement(ctx, acme.ID, collectionRef(billing.ID), nest); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, acme.ID, collectionRef(security.ID), nest); err != nil {
		t.Fatal(err)
	}
	chat := conv("shared")
	if err := svc.AddPlacement(ctx, billing.ID, chat, workspace.Provenance{Reason: "billing"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, security.ID, chat, workspace.Provenance{Reason: "security"}); err != nil {
		t.Fatal(err)
	}
	billingSnap, placed, err := svc.FolderSnapshot(ctx, billing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if billingSnap.MemberCount != 1 {
		t.Fatalf("billing count %d", billingSnap.MemberCount)
	}
	if len(placed) != 1 || !sameStrings(placed[0].AlsoIn, []string{"Security"}) {
		t.Fatalf("also in %v", placed)
	}
	securitySnap, _, err := svc.FolderSnapshot(ctx, security.ID)
	if err != nil || securitySnap.MemberCount != 1 {
		t.Fatalf("security count %+v, %v", securitySnap, err)
	}
	acmeSnap, _, err := svc.FolderSnapshot(ctx, acme.ID)
	if err != nil || acmeSnap.MemberCount != 1 {
		t.Fatalf("acme unique count %+v, %v", acmeSnap, err)
	}
}

func TestRootSnapshotUnfiledVersusFolders(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, orderedInventory{
		ids: []string{"filed", "loose-a", "loose-b"},
		titles: map[string]string{
			"filed":   "In billing",
			"loose-a": "Unfiled A",
			"loose-b": "Unfiled B",
		},
	})
	billing := createFolder(t, svc, "Billing")
	receipts := createFolder(t, svc, "Receipts")
	if err := svc.AddPlacement(ctx, billing.ID, collectionRef(receipts.ID), workspace.Provenance{Reason: "nest"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddPlacement(ctx, billing.ID, conv("filed"), workspace.Provenance{Reason: "file"}); err != nil {
		t.Fatal(err)
	}
	root, err := svc.RootSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Folders) != 1 || root.Folders[0].ID != billing.ID || root.Folders[0].Name != "Billing" {
		t.Fatalf("root folders %+v", root.Folders)
	}
	if len(root.Folders[0].ParentIDs) != 0 {
		t.Fatalf("billing should be parentless, got %v", root.Folders[0].ParentIDs)
	}
	if got := unfiledIDs(root.Unfiled); !sameStrings(got, []string{"loose-a", "loose-b"}) {
		t.Fatalf("unfiled %v", got)
	}
	for _, folder := range root.Folders {
		if folder.ID == "root" || strings.EqualFold(folder.Name, "root") {
			t.Fatalf("root must not appear as a stored folder: %+v", folder)
		}
	}
	nested, _, err := svc.FolderSnapshot(ctx, receipts.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(nested.ParentIDs, []string{billing.ID}) {
		t.Fatalf("receipts parents %v", nested.ParentIDs)
	}
}

func TestCycleErrorPropagated(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	parent := createFolder(t, svc, "Billing")
	child := createFolder(t, svc, "Receipts")
	if err := svc.AddPlacement(ctx, parent.ID, collectionRef(child.ID), workspace.Provenance{Reason: "nest"}); err != nil {
		t.Fatal(err)
	}
	err := svc.AddPlacement(ctx, child.ID, collectionRef(parent.ID), workspace.Provenance{Reason: "cycle"})
	if !errors.Is(err, workspace.ErrCycle) {
		t.Fatalf("cycle: %v", err)
	}
	folders, err := svc.PlacementsOf(ctx, collectionRef(parent.ID))
	if err != nil || len(folders) != 0 {
		t.Fatalf("parent must not have gained a parent: %v, %v", folders, err)
	}
}

func TestFakeInventoryTitles(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, orderedInventory{
		ids:    []string{"abc123abc123abcd"},
		titles: map[string]string{"abc123abc123abcd": "Q3 invoices"},
	})
	billing := createFolder(t, svc, "Billing")
	if err := svc.AddPlacement(ctx, billing.ID, conv("abc123abc123abcd"), workspace.Provenance{Reason: "title"}); err != nil {
		t.Fatal(err)
	}
	_, placed, err := svc.FolderSnapshot(ctx, billing.ID)
	if err != nil || len(placed) != 1 || placed[0].Title != "Q3 invoices" {
		t.Fatalf("title %v, %v", placed, err)
	}
	root, err := svc.RootSnapshot(ctx)
	if err != nil || len(root.Unfiled) != 0 {
		t.Fatalf("filed chat must not be unfiled: %+v, %v", root.Unfiled, err)
	}
}

func TestExpectedRevisionConflict(t *testing.T) {
	ctx := context.Background()
	fake := newFakeStore()
	svc := &Service{store: fake, now: func() time.Time { return time.Time{} }}
	billing := createFolder(t, svc, "Billing")
	receipts := createFolder(t, svc, "Receipts")
	conflict := fmt.Errorf("%w: revision mismatch", workspace.ErrInvalid)
	fake.revisionErr = conflict
	for _, name := range []string{"add", "remove", "move"} {
		var err error
		switch name {
		case "add":
			err = svc.AddPlacement(ctx, billing.ID, conv("chat"), workspace.Provenance{Reason: "stale"})
		case "remove":
			err = svc.RemovePlacement(ctx, billing.ID, conv("chat"), workspace.Provenance{Reason: "stale"})
		case "move":
			err = svc.MovePlacement(ctx, billing.ID, receipts.ID, conv("chat"), workspace.Provenance{Reason: "stale"})
		}
		if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "revision") {
			t.Fatalf("%s revision conflict: %v", name, err)
		}
	}
	wrapped := wrapStoreError(errors.New("stale revision from peer"))
	if !errors.Is(wrapped, workspace.ErrInvalid) || !strings.Contains(wrapped.Error(), "revision") {
		t.Fatalf("wrap: %v", wrapped)
	}
}

func TestOpenWrapsExistingStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	svc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, ok := svc.store.(*workspace.Store); !ok {
		t.Fatalf("Open must bind *workspace.Store, got %T", svc.store)
	}
	billing, err := svc.CreateFolder(ctx, "Billing")
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := svc.CreateFolder(ctx, "Receipts")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetInventory(orderedInventory{
		ids:    []string{"chat-open"},
		titles: map[string]string{"chat-open": "Opened"},
	})
	filed := workspace.Provenance{
		Origin:         workspace.OriginPerson,
		Reason:         "filed from chat",
		Actor:          "me",
		Evidence:       "note",
		IdempotencyKey: "open-1",
	}
	if err := svc.AddPlacement(ctx, billing.ID, conv("chat-open"), filed); err != nil {
		t.Fatal(err)
	}
	why, err := svc.WhyHere(ctx, billing.ID, conv("chat-open"))
	if err != nil || why.Event.Reason != "filed from chat" || why.Event.Origin != workspace.OriginPerson || why.Event.Actor != "me" {
		t.Fatalf("open must keep provenance: %+v, %v", why, err)
	}
	if err := svc.MovePlacement(ctx, billing.ID, receipts.ID, conv("chat-open"), workspace.Provenance{Reason: "moved on open"}); err != nil {
		t.Fatal(err)
	}
	moved, err := svc.WhyHere(ctx, receipts.ID, conv("chat-open"))
	if err != nil || moved.Event.Action != workspace.ActionAdd || moved.Event.Reason != "moved on open" {
		t.Fatalf("open move must keep provenance: %+v, %v", moved, err)
	}
	root, err := svc.RootSnapshot(ctx)
	if err != nil || len(root.Folders) != 2 {
		t.Fatalf("open snapshot %+v, %v", root, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCorruptStoreIsError(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	if err := os.WriteFile(path, []byte("this is not a collections database"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := Open(path)
	if err == nil {
		t.Fatal("corrupt store must not open")
	}
	if svc != nil {
		t.Fatalf("Open must not construct a service on failure, got %+v", svc)
	}
	root, snapErr := (*Service)(nil).RootSnapshot(ctx)
	if snapErr == nil {
		t.Fatal("a failed Open must not yield a successful RootView")
	}
	if len(root.Folders) != 0 || len(root.Unfiled) != 0 {
		t.Fatalf("must not invent a root view: %+v", root)
	}
}

func testService(t *testing.T, inv Inventory) *Service {
	t.Helper()
	svc := &Service{store: newFakeStore(), inv: inv, now: func() time.Time { return time.Time{} }}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func createFolder(t *testing.T, svc *Service, name string) Folder {
	t.Helper()
	folder, err := svc.CreateFolder(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return folder
}

func conv(id string) workspace.Ref {
	return workspace.Ref{Kind: workspace.ConversationKind, ID: id}
}

func placementIDs(t *testing.T, svc *Service, ref workspace.Ref) []string {
	t.Helper()
	folders, err := svc.PlacementsOf(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(folders))
	for _, folder := range folders {
		ids = append(ids, folder.ID)
	}
	return ids
}

func unfiledIDs(placed []Placement) []string {
	ids := make([]string, 0, len(placed))
	for _, row := range placed {
		ids = append(ids, row.Ref.ID)
	}
	return ids
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
