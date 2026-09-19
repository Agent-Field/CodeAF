package wsapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func TestInstructFolderPersonOnly(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	security := createFolder(t, svc, "Security")
	_, err := svc.InstructFolder(ctx, InstructRequest{
		ScopeID:    security.ID,
		Text:       "authenticate receipt links",
		Provenance: workspace.Provenance{Origin: workspace.OriginOrganizer},
	})
	if !errors.Is(err, workspace.ErrInvalid) || !strings.Contains(err.Error(), "person") {
		t.Fatalf("organizer instruct: %v", err)
	}
	got, err := svc.InstructFolder(ctx, InstructRequest{
		ScopeID:    security.ID,
		Text:       "authenticate receipt links",
		Provenance: workspace.Provenance{Origin: workspace.OriginPerson, Actor: "me"},
	})
	if err != nil || got.Text != "authenticate receipt links" || got.Origin != workspace.OriginPerson || got.Revision != 1 {
		t.Fatalf("person instruct %+v, %v", got, err)
	}
	again, err := svc.InstructFolder(ctx, InstructRequest{
		ScopeID:    security.ID,
		Text:       "authenticate receipt links; never mail raw URLs",
		Provenance: workspace.Provenance{Origin: workspace.OriginPerson},
	})
	if err != nil || again.Revision != 2 {
		t.Fatalf("supersede %+v, %v", again, err)
	}
}

func TestEffectiveGuidanceRootOnceAndConflict(t *testing.T) {
	ctx := context.Background()
	svc := testService(t, nil)
	billing := createFolder(t, svc, "Billing")
	receipts := createFolder(t, svc, "Receipts")
	security := createFolder(t, svc, "Security")
	nest := workspace.Provenance{Origin: workspace.OriginPerson, Reason: "nest"}
	if err := svc.AddPlacement(ctx, billing.ID, collectionRef(receipts.ID), nest); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InstructFolder(ctx, InstructRequest{Text: "standing for everyone", Provenance: workspace.Provenance{Origin: workspace.OriginPerson}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InstructFolder(ctx, InstructRequest{ScopeID: billing.ID, Text: "invoices stay in billing", Provenance: workspace.Provenance{Origin: workspace.OriginPerson}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InstructFolder(ctx, InstructRequest{ScopeID: receipts.ID, Text: "invoices stay in billing; receipts nested", Provenance: workspace.Provenance{Origin: workspace.OriginPerson}}); err != nil {
		t.Fatal(err)
	}
	chat := conv("child")
	if err := svc.AddPlacement(ctx, receipts.ID, chat, workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.EffectiveGuidance(ctx, "child")
	if err != nil {
		t.Fatal(err)
	}
	if got.Conflict {
		t.Fatalf("ancestor refinements must compose: %+v", got)
	}
	if n := countRoot(got.Items); n != 1 {
		t.Fatalf("root once, got %d items %+v", n, got.Items)
	}
	if err := svc.AddPlacement(ctx, security.ID, chat, workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InstructFolder(ctx, InstructRequest{ScopeID: security.ID, Text: "never mail raw URLs", Provenance: workspace.Provenance{Origin: workspace.OriginPerson}}); err != nil {
		t.Fatal(err)
	}
	conflicted, err := svc.EffectiveGuidance(ctx, "child")
	if err != nil || !conflicted.Conflict {
		t.Fatalf("sibling instructions must conflict: %+v, %v", conflicted, err)
	}
}

func TestSQLiteInstructAndSuppressPersist(t *testing.T) {
	ctx := context.Background()
	svc := openSQLiteService(t)
	security := createFolder(t, svc, "Security")
	got, err := svc.InstructFolder(ctx, InstructRequest{
		ScopeID:    security.ID,
		Text:       "authenticate receipt links",
		Provenance: workspace.Provenance{Origin: workspace.OriginPerson, Actor: "me"},
	})
	if err != nil || got.Origin != workspace.OriginPerson {
		t.Fatalf("instruct %+v, %v", got, err)
	}
	loaded, err := svc.EffectiveGuidance(ctx, "unfiled")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Snapshot.RootRevision < 1 {
		t.Fatalf("snapshot %+v", loaded.Snapshot)
	}
	chat := conv("auto")
	hash := evidenceHash(cite("persist"))
	if err := svc.SuppressPlacement(ctx, security.ID, chat, hash, workspace.Provenance{Origin: workspace.OriginPerson}); err != nil {
		t.Fatal(err)
	}
	security, _, err = svc.FolderSnapshot(ctx, security.ID)
	if err != nil {
		t.Fatal(err)
	}
	plan := addPlan(security, chat, "persist")
	if err := svc.ValidateActionPlan(ctx, plan); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatalf("persisted suppression: %v", err)
	}
}

func countRoot(items []GuidanceItem) int {
	n := 0
	for _, item := range items {
		if item.ScopeID == "" {
			n++
		}
	}
	return n
}
