package workstream

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

func TestWakeD_R2_M2_UndoKeepsRenameBetweenClaimAndSettlement(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Rename during wake", workstreamWakeMember())
	small := "small"
	if _, _, err := s.Update(ctx, w.ID, Patch{Autonomy: &small}); err != nil {
		t.Fatal(err)
	}
	page, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Old title", Owner: "agent", Doc: pagedoc.FromMarkdown("Original document")})
	if err != nil {
		t.Fatal(err)
	}
	run, woke, err := s.BeginWake(ctx, w.ID, "rename-during-wake", WakeEvent{Kind: "merged", Factory: "f", Job: 7, At: s.now()})
	if err != nil || !woke {
		t.Fatalf("claim: %+v %v %v", run, woke, err)
	}
	title := "Person's title"
	renamed, c, err := s.UpdatePage(ctx, page.ID, PagePatch{Title: &title})
	if err != nil || c.ID != w.ID || renamed.Title != title || renamed.Version != page.Version {
		t.Fatalf("rename between claim and settlement: %+v %+v %v", renamed, c, err)
	}
	history, err := s.PageHistory(ctx, page.ID, 100)
	if err != nil || len(history) != 1 || history[0].Title != page.Title {
		t.Fatalf("rename changed the original content history: %+v %v", history, err)
	}
	run, c, err = s.SettleWake(ctx, run.ID, WakeAnswer{Items: []WakeProposal{{Kind: "page", PageID: page.ID, Markdown: "Wake document"}}}, nil, "")
	if err != nil || c.ID != w.ID || len(run.Items) != 1 || run.Items[0].State != "applied" {
		t.Fatalf("settle: %+v %+v %v", run, c, err)
	}
	applied, err := s.Page(ctx, page.ID, false)
	if err != nil || applied.Title != title || applied.Version != page.Version+1 || string(applied.Doc) != string(pagedoc.FromMarkdown("Wake document")) {
		t.Fatalf("wake did not preserve the rename: %+v %v", applied, err)
	}
	undone, c, err := s.ActOnWake(ctx, run.ID, run.Items[0].ID, "undo")
	if err != nil || c.ID != w.ID || len(undone.Items) != 1 || undone.Items[0].State != "undone" || undone.Items[0].Note != "" {
		t.Fatalf("undo: %+v %+v %v", undone, c, err)
	}
	restored, err := s.Page(ctx, page.ID, false)
	if err != nil || restored.Title != title || restored.Version != page.Version+2 || restored.UpdatedBy != "agent" || string(restored.Doc) != string(page.Doc) {
		t.Fatalf("undo did not restore the original document and keep the rename: %+v %v", restored, err)
	}
	history, err = s.PageHistory(ctx, page.ID, 100)
	if err != nil || len(history) != 3 || history[0].Version != restored.Version || history[0].Title != title || history[0].SavedBy != "agent" || history[2].Title != page.Title {
		t.Fatalf("undo history lost the rename or rewrote the original version: %+v %v", history, err)
	}
	stored, err := s.WakeRun(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(stored, undone) {
		t.Fatalf("undo was not persisted: %+v %v", stored, err)
	}
}

func TestWakeD_R2_m1_CreatedSiblingPagesUndoInAnyOrder(t *testing.T) {
	for _, tc := range []struct {
		name           string
		autonomy       string
		reverseApply   bool
		reorderSibling bool
	}{
		{name: "named_undo_after_sibling_compaction", autonomy: "small"},
		{name: "undo_all_after_reverse_individual_apply", autonomy: "propose", reverseApply: true},
		{name: "named_undo_after_person_reorders_siblings", autonomy: "small", reorderSibling: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			w := createStream(t, s, "Sibling pages", workstreamWakeMember())
			if _, _, err := s.Update(ctx, w.ID, Patch{Autonomy: &tc.autonomy}); err != nil {
				t.Fatal(err)
			}
			before, err := s.Pages(ctx, w.ID)
			if err != nil || len(before) != 1 {
				t.Fatalf("initial pages: %+v %v", before, err)
			}
			run := workstreamWakeForTest(t, s, w, "sibling-pages",
				WakeProposal{Kind: "page", Title: "A", Markdown: "Document A"},
				WakeProposal{Kind: "page", Title: "B", Markdown: "Document B"})
			if len(run.Items) != 2 || run.Items[0].Proposal.Title != "A" || run.Items[1].Proposal.Title != "B" {
				t.Fatalf("expected two pages in card order A, B: %+v", run)
			}
			if tc.reverseApply {
				for _, item := range run.Items {
					if item.State != "proposed" {
						t.Fatalf("page was applied before individual Apply: %+v", item)
					}
				}
				for i := len(run.Items) - 1; i >= 0; i-- {
					var c Change
					run, c, err = s.ActOnWake(ctx, run.ID, run.Items[i].ID, "apply")
					if err != nil || c.ID != w.ID || run.Items[i].State != "applied" {
						t.Fatalf("individual Apply in reverse card order: %+v %+v %v", run, c, err)
					}
				}
			}
			for i, item := range run.Items {
				position := i + 1
				if tc.reverseApply {
					position = len(run.Items) - i
				}
				page, err := s.Page(ctx, item.PageID, false)
				if err != nil || item.State != "applied" || page.ParentID != "" || page.Position != position || page.Owner != "agent" || page.Version != 1 || string(page.Doc) != string(pagedoc.FromMarkdown(item.Proposal.Markdown)) {
					t.Fatalf("applied root page: %+v %+v %v", item, page, err)
				}
			}
			var sibling PageNode
			if tc.reorderSibling {
				page, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Person's page", Owner: "you"})
				if err != nil {
					t.Fatal(err)
				}
				var c Change
				sibling, c, err = s.MovePage(ctx, page.ID, "", before[0].ID)
				if err != nil || c.ID != w.ID || sibling.Position != 1 {
					t.Fatalf("reorder sibling: %+v %+v %v", sibling, c, err)
				}
				for i, item := range run.Items {
					page, err := s.Page(ctx, item.PageID, false)
					if err != nil || page.Position != i+2 || item.Undo.Page == nil {
						t.Fatalf("sibling reorder did not shift the wake page: %+v %v", page, err)
					}
					postImage := *item.Undo.Page
					postImage.Position = page.Position
					if !reflect.DeepEqual(page.PageNode, postImage) {
						t.Fatalf("sibling reorder changed more than position: %+v", page)
					}
				}
			}
			if tc.reverseApply {
				var c Change
				run, c, err = s.ActOnWake(ctx, run.ID, "", "undo")
				if err != nil || c.ID != w.ID {
					t.Fatalf("Undo all: %+v %+v %v", run, c, err)
				}
			} else {
				b, err := s.Page(ctx, run.Items[1].PageID, false)
				if err != nil {
					t.Fatal(err)
				}
				for i := range run.Items {
					var c Change
					run, c, err = s.ActOnWake(ctx, run.ID, run.Items[i].ID, "undo")
					if err != nil || c.ID != w.ID || run.Items[i].State != "undone" || run.Items[i].Note != "" {
						t.Fatalf("named Undo %d: %+v %+v %v", i, run, c, err)
					}
					if i == 0 {
						compacted, err := s.Page(ctx, b.ID, false)
						if err != nil || compacted.Position != b.Position-1 || !compacted.DeletedAt.IsZero() || run.Items[1].State != "applied" {
							t.Fatalf("Undo A did not compact the still-applied B: %+v %+v %v", compacted, run, err)
						}
					}
				}
			}
			for _, item := range run.Items {
				page, err := s.Page(ctx, item.PageID, false)
				if err != nil || item.State != "undone" || item.Note != "" || page.DeletedAt.IsZero() || page.Version != 1 || string(page.Doc) != string(pagedoc.FromMarkdown(item.Proposal.Markdown)) {
					t.Fatalf("wake page was not restorably deleted: %+v %+v %v", item, page, err)
				}
			}
			want := append([]PageNode(nil), before...)
			if tc.reorderSibling {
				want = append(want, sibling)
			}
			after, err := s.Pages(ctx, w.ID)
			if err != nil || !reflect.DeepEqual(after, want) {
				t.Fatalf("Undo changed the person's pages: %+v %v", after, err)
			}
			stored, err := s.WakeRun(ctx, run.ID)
			if err != nil || !reflect.DeepEqual(stored, run) {
				t.Fatalf("Undo was not persisted: %+v %v", stored, err)
			}
			if again, c, err := s.ActOnWake(ctx, run.ID, "", "undo"); err != nil || c.ID != "" || !reflect.DeepEqual(again, run) {
				t.Fatalf("repeated Undo changed the wake: %+v %+v %v", again, c, err)
			}
		})
	}
}
