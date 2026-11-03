package workstream

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

func workstreamWakeUndoConflictForTest(t *testing.T, s *Store, run WakeRun) {
	t.Helper()
	ctx := context.Background()
	if len(run.Items) != 1 || run.Items[0].State != "applied" {
		t.Fatalf("expected one applied item: %+v", run)
	}
	if _, c, err := s.ActOnWake(ctx, run.ID, run.Items[0].ID, "undo"); !errors.Is(err, ErrConflict) || c.ID != "" {
		t.Fatalf("named undo: %+v %v", c, err)
	}
	stored, err := s.WakeRun(ctx, run.ID)
	if err != nil || !reflect.DeepEqual(stored, run) {
		t.Fatalf("named conflict changed the wake: %+v %v", stored, err)
	}
	noted, c, err := s.ActOnWake(ctx, run.ID, "", "undo")
	if err != nil || c.ID != run.WorkstreamID || len(noted.Items) != 1 || noted.Items[0].State != "applied" || noted.Items[0].Note != "Changed since; not undone." {
		t.Fatalf("undo all: %+v %+v %v", noted, c, err)
	}
	again, c, err := s.ActOnWake(ctx, run.ID, "", "undo")
	if err != nil || c.ID != "" || !reflect.DeepEqual(again, noted) {
		t.Fatalf("repeated undo all changed the wake: %+v %+v %v", again, c, err)
	}
}

func TestWakeD_M1_ChangedDraftRefusesUndo(t *testing.T) {
	for _, change := range []string{"title", "repo", "text", "size", "budget", "source", "wave"} {
		t.Run(change, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			w := createStream(t, s, "Draft edits", workstreamWakeMember())
			small := "small"
			if _, _, err := s.Update(ctx, w.ID, Patch{Autonomy: &small}); err != nil {
				t.Fatal(err)
			}
			run := workstreamWakeForTest(t, s, w, "draft-edits", WakeProposal{Kind: "draft", Title: "Follow up", Repo: "api", Text: "Original", Size: "S", BudgetUSD: workstreamWakeBudget(3)})
			plan, err := s.Plan(ctx, w.ID)
			if err != nil || len(plan.Waves) != 1 || len(plan.Waves[0].Items) != 1 {
				t.Fatalf("wake plan: %+v %v", plan, err)
			}
			draft := &plan.Waves[0].Items[0]
			switch change {
			case "title":
				draft.Title = "Edited follow-up"
			case "repo":
				draft.Repo = "web"
			case "text":
				draft.Text = "Edited contents"
			case "size":
				draft.Size = "M"
			case "budget":
				draft.BudgetUSD = 4
			case "source":
				draft.Source = &Source{Kind: "chat", ID: w.AgentID, Label: "Person's edit"}
			case "wave":
				plan.Waves = append(plan.Waves, Wave{N: 2, Title: "Person's wave", Items: plan.Waves[0].Items})
				plan.Waves[0].Items = []PlanItem{}
			}
			before, _, err := s.SetPlan(ctx, w.ID, plan.Version, plan.Milestones, plan.Waves, Attribution{})
			if err != nil {
				t.Fatal(err)
			}
			workstreamWakeUndoConflictForTest(t, s, run)
			after, err := s.Plan(ctx, w.ID)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("undo changed the edited draft or its waves: %+v %v", after, err)
			}
		})
	}
}

func TestWakeD_M1_OnlyUnchangedWakeCreatedWaveIsRemoved(t *testing.T) {
	for _, change := range []string{"unchanged", "renamed", "auto_start", "milestone", "another_draft", "person_created"} {
		t.Run(change, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			w := createStream(t, s, "Wave cleanup", workstreamWakeMember())
			small := "small"
			if _, _, err := s.Update(ctx, w.ID, Patch{Autonomy: &small}); err != nil {
				t.Fatal(err)
			}
			if change == "person_created" {
				if _, _, err := s.SetPlan(ctx, w.ID, 0, nil, []Wave{{N: 1, Items: []PlanItem{}}}, Attribution{}); err != nil {
					t.Fatal(err)
				}
			}
			run := workstreamWakeForTest(t, s, w, "wave-cleanup", WakeProposal{Kind: "draft", Title: "Follow up", BudgetUSD: workstreamWakeBudget(3)})
			plan, err := s.Plan(ctx, w.ID)
			if err != nil || len(run.Items) != 1 || run.Items[0].State != "applied" || len(plan.Waves) != 1 || len(plan.Waves[0].Items) != 1 {
				t.Fatalf("wake plan: %+v %+v %v", run, plan, err)
			}
			switch change {
			case "renamed":
				plan.Waves[0].Title = "Person's title"
			case "auto_start":
				plan.Waves[0].AutoStart = true
			case "milestone":
				plan.Milestones = []Milestone{{ID: "m", Title: "Release"}}
				plan.Waves[0].MilestoneID = "m"
			case "another_draft":
				plan.Waves[0].Items = append(plan.Waves[0].Items, PlanItem{Kind: "draft", DraftID: "person-draft", Title: "Person's draft"})
			}
			if change != "unchanged" && change != "person_created" {
				if _, _, err := s.SetPlan(ctx, w.ID, plan.Version, plan.Milestones, plan.Waves, Attribution{}); err != nil {
					t.Fatal(err)
				}
			}
			want := []Wave{}
			if change != "unchanged" {
				want = plan.Waves
				want[0].Items = want[0].Items[1:]
			}
			undone, c, err := s.ActOnWake(ctx, run.ID, "", "undo")
			if err != nil || c.ID != w.ID || len(undone.Items) != 1 || undone.Items[0].State != "undone" {
				t.Fatalf("undo: %+v %+v %v", undone, c, err)
			}
			after, err := s.Plan(ctx, w.ID)
			if err != nil || !reflect.DeepEqual(after.Waves, want) || !reflect.DeepEqual(after.Milestones, plan.Milestones) {
				t.Fatalf("wave cleanup: %+v, want %+v (%v)", after, want, err)
			}
		})
	}
}

func TestWakeDE_M2_PageMetadataChangesRefuseUndo(t *testing.T) {
	for _, operation := range []string{"wake_update", "wake_creation"} {
		t.Run(operation, func(t *testing.T) {
			for _, change := range []string{"page.update_title", "page.update_owner", "page.update_read_by_jobs", "page.move_parent", "page.move_position", "page.delete"} {
				t.Run(change, func(t *testing.T) {
					s, _, _ := testStore(t)
					ctx := context.Background()
					w := createStream(t, s, "Page metadata", workstreamWakeMember())
					small := "small"
					if _, _, err := s.Update(ctx, w.ID, Patch{Autonomy: &small}); err != nil {
						t.Fatal(err)
					}
					parent, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Person's folder", Owner: "you"})
					if err != nil {
						t.Fatal(err)
					}
					proposal := WakeProposal{Kind: "page", Title: "Agent notes", Markdown: "Wake contents"}
					if operation == "wake_update" {
						page, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: proposal.Title, Owner: "agent", Doc: pagedoc.FromMarkdown("Original")})
						if err != nil {
							t.Fatal(err)
						}
						proposal.PageID = page.ID
					}
					run := workstreamWakeForTest(t, s, w, "page-metadata", proposal)
					if len(run.Items) != 1 || run.Items[0].State != "applied" {
						t.Fatal(run)
					}
					id := run.Items[0].PageID
					switch change {
					case "page.update_title":
						title := "Person's title"
						_, _, err = s.UpdatePage(ctx, id, PagePatch{Title: &title})
					case "page.update_owner":
						owner := "you"
						_, _, err = s.UpdatePage(ctx, id, PagePatch{Owner: &owner})
					case "page.update_read_by_jobs":
						read := true
						_, _, err = s.UpdatePage(ctx, id, PagePatch{ReadByJobs: &read})
					case "page.move_parent":
						_, _, err = s.MovePage(ctx, id, parent.ID, "")
					case "page.move_position":
						_, _, err = s.MovePage(ctx, id, "", "")
					case "page.delete":
						_, _, err = s.DeletePage(ctx, id)
					}
					if err != nil {
						t.Fatal(err)
					}
					before, err := s.Page(ctx, id, false)
					if err != nil || before.Version != run.Items[0].Undo.Version {
						t.Fatalf("metadata change advanced the content version: %+v %v", before, err)
					}
					history, historyErr := s.PageHistory(ctx, id, 100)
					if change == "page.delete" {
						if !errors.Is(historyErr, ErrNotFound) {
							t.Fatal(historyErr)
						}
					} else if historyErr != nil {
						t.Fatal(historyErr)
					}
					personBefore, err := s.Page(ctx, parent.ID, false)
					if err != nil {
						t.Fatal(err)
					}
					workstreamWakeUndoConflictForTest(t, s, run)
					after, err := s.Page(ctx, id, false)
					if err != nil || !reflect.DeepEqual(after, before) {
						t.Fatalf("undo changed the later page edit: %+v %v", after, err)
					}
					afterHistory, err := s.PageHistory(ctx, id, 100)
					if !errors.Is(err, historyErr) || !reflect.DeepEqual(afterHistory, history) {
						t.Fatalf("undo wrote page history: %+v %v", afterHistory, err)
					}
					person, err := s.Page(ctx, parent.ID, false)
					if err != nil || !reflect.DeepEqual(person, personBefore) {
						t.Fatalf("undo changed the person's page: %+v %v", person, err)
					}
				})
			}
		})
	}
}

func TestWakeD_m1_DuplicatePageItemsKeepFirstAndUndoOriginal(t *testing.T) {
	for _, autonomy := range []string{"small", "propose"} {
		t.Run(autonomy, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			w := createStream(t, s, "Duplicate pages", workstreamWakeMember())
			if _, _, err := s.Update(ctx, w.ID, Patch{Autonomy: &autonomy}); err != nil {
				t.Fatal(err)
			}
			page, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Agent notes", Owner: "agent", Doc: pagedoc.FromMarkdown("Original")})
			if err != nil {
				t.Fatal(err)
			}
			answer, err := ParseWakeAnswer(`{"summary":"Next","items":[` +
				`{"kind":"page","page_id":"` + page.ID + `","markdown":"First update"},` +
				`{"kind":"page","page_id":"` + page.ID + `","markdown":"Later update"}]}`)
			if err != nil {
				t.Fatal(err)
			}
			run := workstreamWakeForTest(t, s, w, "duplicate-pages", answer.Items...)
			stored, err := s.WakeRun(ctx, run.ID)
			if err != nil || len(stored.Items) != 1 || stored.Items[0].PageID != page.ID || stored.Items[0].Proposal.Markdown != "First update" {
				t.Fatalf("page items were not deduplicated: %+v %v", stored, err)
			}
			if autonomy == "propose" {
				for _, item := range stored.Items {
					if item.State != "proposed" {
						t.Fatal(item)
					}
				}
				stored, _, err = s.ActOnWake(ctx, run.ID, "", "apply")
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, item := range stored.Items {
				if item.State != "applied" {
					t.Fatal(item)
				}
			}
			updated, err := s.Page(ctx, page.ID, false)
			if err != nil || updated.Version != page.Version+1 || string(updated.Doc) != string(pagedoc.FromMarkdown("First update")) {
				t.Fatalf("first update was not the only write: %+v %v", updated, err)
			}
			undone, c, err := s.ActOnWake(ctx, run.ID, "", "undo")
			if err != nil || c.ID != w.ID {
				t.Fatalf("undo: %+v %v", c, err)
			}
			for _, item := range undone.Items {
				if item.State != "undone" || item.Note != "" {
					t.Fatal(item)
				}
			}
			restored, err := s.Page(ctx, page.ID, false)
			if err != nil || restored.Version != page.Version+2 || string(restored.Doc) != string(page.Doc) || restored.Title != page.Title {
				t.Fatalf("undo did not restore the original document: %+v %v", restored, err)
			}
		})
	}
}

func TestWakeD_m1_DuplicateNewPageTitlesKeepFirst(t *testing.T) {
	for _, autonomy := range []string{"small", "propose"} {
		t.Run(autonomy, func(t *testing.T) {
			s, _, _ := testStore(t)
			ctx := context.Background()
			w := createStream(t, s, "Duplicate new pages", workstreamWakeMember())
			if _, _, err := s.Update(ctx, w.ID, Patch{Autonomy: &autonomy}); err != nil {
				t.Fatal(err)
			}
			run := workstreamWakeForTest(t, s, w, "duplicate-new-pages", WakeProposal{Kind: "page", Title: "New notes", Markdown: "First creation"}, WakeProposal{Kind: "page", Title: "New notes", Markdown: "Later creation"})
			stored, err := s.WakeRun(ctx, run.ID)
			if err != nil || len(stored.Items) != 1 || stored.Items[0].Proposal.Markdown != "First creation" {
				t.Fatalf("new-page items were not deduplicated: %+v %v", stored, err)
			}
			if autonomy == "propose" {
				if stored.Items[0].State != "proposed" {
					t.Fatal(stored)
				}
				stored, _, err = s.ActOnWake(ctx, run.ID, "", "apply")
				if err != nil {
					t.Fatal(err)
				}
			}
			pages, err := s.Pages(ctx, w.ID)
			if err != nil || len(pages) != 2 || stored.Items[0].State != "applied" {
				t.Fatalf("expected one new agent page: %+v %+v %v", pages, stored, err)
			}
			page, err := s.Page(ctx, stored.Items[0].PageID, false)
			if err != nil || page.Version != 1 || string(page.Doc) != string(pagedoc.FromMarkdown("First creation")) {
				t.Fatalf("first new-page suggestion was not kept: %+v %v", page, err)
			}
			undone, c, err := s.ActOnWake(ctx, run.ID, "", "undo")
			if err != nil || c.ID != w.ID || undone.Items[0].State != "undone" {
				t.Fatalf("undo: %+v %+v %v", undone, c, err)
			}
			deleted, err := s.Page(ctx, page.ID, false)
			if err != nil || deleted.DeletedAt.IsZero() {
				t.Fatalf("new page was not restorably deleted: %+v %v", deleted, err)
			}
		})
	}
}
