package main

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

func workstreamWakeBackend(t *testing.T) (*appBackend, *appRecorder, *time.Time, *[]workstreamWakeTask, *int) {
	t.Helper()
	b, rec, _, _ := workstreamBackend(t)
	now := time.Date(2026, 11, 3, 7, 59, 0, 0, time.Local)
	b.now = func() time.Time { return now }
	tasks := []workstreamWakeTask{}
	b.workstreams.wakeDispatch = func(task workstreamWakeTask) { tasks = append(tasks, task) }
	turns := 0
	b.workstreams.wakeTurn = func(ctx context.Context, ask session.WorkstreamWakeAsk) (session.WorkstreamWakeAnswer, error) {
		turns++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > workstreamWakeTimeout {
			t.Error("wake timeout missing")
		}
		return session.WorkstreamWakeAnswer{Text: `{"summary":"Review next steps.","items":[{"kind":"decision","text":"Review first."},{"kind":"draft","title":"Follow up","budget_usd":3}]}`, CostUSD: workstreamPtr(.04), Model: "fake"}, nil
	}
	return b, rec, &now, &tasks, &turns
}
func workstreamDrainWakes(b *appBackend, tasks *[]workstreamWakeTask) {
	pending := *tasks
	*tasks = nil
	for _, task := range pending {
		b.workstreamRunWake(context.Background(), task)
	}
}
func workstreamWakeListForTest(t *testing.T, b *appBackend, id string) []appwire.WorkstreamWakeRun {
	t.Helper()
	out, err := b.WorkstreamWakeList(appwire.WorkstreamWakeListParams{WorkstreamID: id})
	if err != nil {
		t.Fatal(err)
	}
	return out.Wakes
}

func TestWakeA_MorningOncePerLocalDay(t *testing.T) {
	b, _, now, tasks, turns := workstreamWakeBackend(t)
	w := workstreamCreateForTest(t, b, "Morning")
	if _, err := b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Wake: &appwire.WorkstreamWake{Morning: true}}}); err != nil {
		t.Fatal(err)
	}
	b.workstreamWakeTick(*now)
	if len(*tasks) != 0 {
		t.Fatal("woke before first slot")
	}
	*now = now.Add(time.Minute)
	b.workstreamWakeTick(*now)
	workstreamDrainWakes(b, tasks)
	for range 60 {
		*now = now.Add(time.Minute)
		b.workstreamWakeTick(*now)
	}
	if *turns != 1 || len(*tasks) != 0 {
		t.Fatal("morning repeated")
	}
	*now = now.AddDate(0, 0, 1)
	b.workstreamWakeTick(*now)
	workstreamDrainWakes(b, tasks)
	if *turns != 2 || len(workstreamWakeListForTest(t, b, w.ID)) != 2 {
		t.Fatal("next day did not wake")
	}
	// Closed days produce no rows; opening late admits just the current day's slot.
	*now = now.AddDate(0, 0, 3)
	b.workstreamWakeTick(*now)
	workstreamDrainWakes(b, tasks)
	if *turns != 3 {
		t.Fatal("closed days were replayed")
	}
	off := "off"
	if _, err := b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Autonomy: &off}}); err != nil {
		t.Fatal(err)
	}
	*now = now.AddDate(0, 0, 1)
	b.workstreamWakeTick(*now)
	if len(*tasks) != 0 {
		t.Fatal("off woke")
	}
}

func TestWakeA_DisabledAndCreatedAfterSlotDoNotWake(t *testing.T) {
	b, _, now, tasks, _ := workstreamWakeBackend(t)
	*now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	w := workstreamCreateForTest(t, b, "Late")
	b.workstreamWakeTick(*now)
	if len(*tasks) != 0 {
		t.Fatal("disabled morning woke")
	}
	b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Wake: &appwire.WorkstreamWake{Morning: true, RelatedIssues: true}}})
	b.workstreamWakeTick(*now)
	if len(*tasks) != 0 {
		t.Fatal("slot before creation woke")
	}
}

func TestWakeB_JobHookBatchesAndDeduplicates(t *testing.T) {
	b, rec, now, tasks, turns := workstreamWakeBackend(t)
	w := workstreamCreateForTest(t, b, "Events", workstreamJobRef("7"))
	ev := workstreamJobEvent{Factory: "f", Job: 7, Kind: "merged", ID: "merge-1", At: *now, Label: "api#7 merged", Detail: "The merge was recorded."}
	id, woke := b.WorkstreamJobEvent(ev)
	if !woke || id == "" || len(*tasks) != 1 {
		t.Fatal(id, woke)
	}
	if _, woke := b.WorkstreamJobEvent(ev); woke {
		t.Fatal("duplicate woke")
	}
	ev.Job = 8
	if _, woke := b.WorkstreamJobEvent(ev); woke {
		t.Fatal("nonmember woke")
	}
	workstreamDrainWakes(b, tasks)
	runs := workstreamWakeListForTest(t, b, w.ID)
	if *turns != 1 || len(runs) != 1 || len(runs[0].Items) != 2 || runs[0].State != "settled" {
		t.Fatal(runs, *turns)
	}
	count := 0
	for _, e := range workstreamEvents(rec) {
		if slices.Contains(e.Parts, "wakes") {
			count++
		}
	}
	if count != 1 {
		t.Fatal("wanted one settle receipt", count)
	}
	off := "off"
	b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Autonomy: &off}})
	ev.Job = 7
	ev.ID = "off"
	if _, woke := b.WorkstreamJobEvent(ev); woke {
		t.Fatal("off job event woke")
	}
}

func TestWakeF_CostIsRecordedAndAddedToAllSpendResults(t *testing.T) {
	for _, mode := range []string{"known", "unknown", "unreadable"} {
		t.Run(mode, func(t *testing.T) {
			b, _, now, tasks, _ := workstreamWakeBackend(t)
			b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
				return api.Queue{Jobs: []api.Job{{ID: 7, State: "done", Cost: 2}}}, nil
			}
			w := workstreamCreateForTest(t, b, "Spend", workstreamJobRef("7"))
			before, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
			if err != nil || before.Workstream.SpendUSD == nil {
				t.Fatal(before, err)
			}
			b.workstreams.wakeTurn = func(context.Context, session.WorkstreamWakeAsk) (session.WorkstreamWakeAnswer, error) {
				a := session.WorkstreamWakeAnswer{Text: `{"summary":"Review","items":[]}`, CostUSD: workstreamPtr(.04)}
				if mode == "unknown" {
					a.CostUSD = nil
				}
				if mode == "unreadable" {
					a.Text = "not JSON"
				}
				return a, nil
			}
			if _, woke := b.WorkstreamJobEvent(workstreamJobEvent{Factory: "f", Job: 7, Kind: "finished", At: *now}); !woke {
				t.Fatal("did not wake")
			}
			workstreamDrainWakes(b, tasks)
			runs := workstreamWakeListForTest(t, b, w.ID)
			after, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
			if err != nil {
				t.Fatal(err)
			}
			list, err := b.WorkstreamList(appwire.WorkstreamListParams{})
			if err != nil {
				t.Fatal(err)
			}
			want := *before.Workstream.SpendUSD
			if mode != "unknown" {
				want += .04
			}
			if after.Workstream.SpendUSD == nil || *after.Workstream.SpendUSD != want || list.Workstreams[0].SpendUSD == nil || *list.Workstreams[0].SpendUSD != want {
				t.Fatal("spend drift", after, list)
			}
			if mode == "unknown" {
				if runs[0].CostUSD != nil {
					t.Fatal("fabricated cost")
				}
			} else if runs[0].CostUSD == nil || *runs[0].CostUSD != .04 {
				t.Fatal("cost lost")
			}
			if mode == "unreadable" && (runs[0].State != "failed" || workstreamValue(runs[0].Error) != workstream.WakeAnswerError) {
				t.Fatal(runs)
			}
		})
	}
}

func TestWakeG_WireActionsSendOneReceiptPerChange(t *testing.T) {
	b, rec, now, tasks, _ := workstreamWakeBackend(t)
	w := workstreamCreateForTest(t, b, "Actions", workstreamJobRef("7"))
	id, _ := b.WorkstreamJobEvent(workstreamJobEvent{Factory: "f", Job: 7, Kind: "failed", At: *now})
	workstreamDrainWakes(b, tasks)
	runs := workstreamWakeListForTest(t, b, w.ID)
	item := runs[0].Items[0].ID
	events := func() int { return len(workstreamEvents(rec)) }
	before := events()
	first, err := b.WorkstreamWakeApply(appwire.WorkstreamWakeApplyParams{ID: id, ItemID: &item})
	if err != nil || events() != before+1 {
		t.Fatal(first, err)
	}
	again, err := b.WorkstreamWakeApply(appwire.WorkstreamWakeApplyParams{ID: id, ItemID: &item})
	if err != nil || events() != before+1 || !reflect.DeepEqual(first, again) {
		t.Fatal("repeated apply wrote")
	}
	if _, err := b.WorkstreamWakeNotNow(appwire.WorkstreamWakeNotNowParams{ID: id}); err != nil || events() != before+2 {
		t.Fatal(err)
	}
	b.WorkstreamWakeNotNow(appwire.WorkstreamWakeNotNowParams{ID: id})
	if events() != before+2 {
		t.Fatal("repeated dismissal wrote")
	}
	if _, err := b.WorkstreamWakeUndo(appwire.WorkstreamWakeUndoParams{ID: id, ItemID: &item}); err != nil || events() != before+3 {
		t.Fatal(err)
	}
	b.WorkstreamWakeUndo(appwire.WorkstreamWakeUndoParams{ID: id, ItemID: &item})
	if events() != before+3 {
		t.Fatal("repeated undo wrote")
	}
	_, err = b.WorkstreamWakeApply(appwire.WorkstreamWakeApplyParams{ID: "missing"})
	workstreamCheckError(t, err, appwire.CodeNotFound)
	_, err = b.WorkstreamWakeUndo(appwire.WorkstreamWakeUndoParams{ID: id, ItemID: workstreamPtr("missing")})
	workstreamCheckError(t, err, appwire.CodeNotFound)
	b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID})
	_, err = b.WorkstreamWakeApply(appwire.WorkstreamWakeApplyParams{ID: id})
	workstreamCheckError(t, err, appwire.CodeConflict)
	_, err = b.WorkstreamWakeUndo(appwire.WorkstreamWakeUndoParams{ID: id})
	workstreamCheckError(t, err, appwire.CodeConflict)
}

func TestWakeH_DoneAndOffAreRereadAtSettlement(t *testing.T) {
	for _, choice := range []string{"done", "off"} {
		t.Run(choice, func(t *testing.T) {
			b, _, now, tasks, turns := workstreamWakeBackend(t)
			w := workstreamCreateForTest(t, b, "Finish", workstreamJobRef("7"))
			small := "small"
			b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Autonomy: &small, Wake: &appwire.WorkstreamWake{Morning: true}}})
			b.workstreams.wakeTurn = func(context.Context, session.WorkstreamWakeAsk) (session.WorkstreamWakeAnswer, error) {
				*turns++
				if choice == "done" {
					_, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: w.ID})
					if err != nil {
						t.Fatal(err)
					}
				} else {
					off := "off"
					_, err := b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Autonomy: &off}})
					if err != nil {
						t.Fatal(err)
					}
				}
				return session.WorkstreamWakeAnswer{Text: `{"summary":"Follow up","items":[{"kind":"draft","title":"Follow up","budget_usd":3},{"kind":"page","title":"Notes","markdown":"Next"}]}`}, nil
			}
			b.WorkstreamJobEvent(workstreamJobEvent{Factory: "f", Job: 7, Kind: "needs_you", At: *now})
			workstreamDrainWakes(b, tasks)
			runs := workstreamWakeListForTest(t, b, w.ID)
			for _, i := range runs[0].Items {
				if i.State != "proposed" {
					t.Fatal(i)
				}
			}
			*now = now.AddDate(0, 0, 1)
			b.workstreamWakeTick(*now)
			if _, woke := b.WorkstreamJobEvent(workstreamJobEvent{Factory: "f", Job: 7, Kind: "finished", At: *now}); woke || len(*tasks) != 0 || *turns != 1 {
				t.Fatal("stopped workstream woke")
			}
		})
	}
}

func TestWakeE_NoFactoryActionsAndPersonPageIsUntouched(t *testing.T) {
	for _, autonomy := range []string{"propose", "small"} {
		t.Run(autonomy, func(t *testing.T) {
			b, _, now, tasks, _ := workstreamWakeBackend(t)
			w := workstreamCreateForTest(t, b, "Pages", workstreamJobRef("7"))
			queueReads, jobReads := 0, 0
			b.workstreams.queue = func(context.Context, string) (api.Queue, error) {
				queueReads++
				return api.Queue{Jobs: []api.Job{{ID: 7, State: "backlog"}}}, nil
			}
			b.workstreams.job = func(context.Context, string, int) (api.Job, error) { jobReads++; return api.Job{}, nil }
			b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Autonomy: &autonomy}})
			pages, _ := b.WorkstreamPageList(appwire.WorkstreamPageListParams{WorkstreamID: w.ID})
			person, _ := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: pages.Pages[0].ID})
			b.workstreams.wakeTurn = func(context.Context, session.WorkstreamWakeAsk) (session.WorkstreamWakeAnswer, error) {
				raw, _ := json.Marshal(map[string]any{"summary": "Next", "items": []any{map[string]any{"kind": "page", "page_id": person.Page.ID, "markdown": "Replace"}, map[string]any{"kind": "draft", "title": "Follow up", "budget_usd": 3}}})
				return session.WorkstreamWakeAnswer{Text: string(raw)}, nil
			}
			id, _ := b.WorkstreamJobEvent(workstreamJobEvent{Factory: "f", Job: 7, Kind: "finished", At: *now})
			workstreamDrainWakes(b, tasks)
			runs := workstreamWakeListForTest(t, b, w.ID)
			if len(runs[0].Items) != 1 || runs[0].Items[0].Kind != "draft" {
				t.Fatal(runs)
			}
			b.WorkstreamWakeApply(appwire.WorkstreamWakeApplyParams{ID: id})
			b.WorkstreamWakeUndo(appwire.WorkstreamWakeUndoParams{ID: id})
			after, _ := b.WorkstreamPageGet(appwire.WorkstreamPageGetParams{ID: person.Page.ID})
			if !reflect.DeepEqual(person, after) {
				t.Fatal("person page changed")
			}
			// These seams are read-only Factory doors. No create/start door is reachable here.
			if queueReads == 0 || jobReads != 0 {
				t.Fatal("unexpected Factory door use", queueReads, jobReads)
			}
		})
	}
}

func TestWakeB_ConcurrentDuplicateClaimsHaveOneTurn(t *testing.T) {
	b, _, now, _, _ := workstreamWakeBackend(t)
	w := workstreamCreateForTest(t, b, "Race", workstreamJobRef("7"))
	var mu sync.Mutex
	tasks := []workstreamWakeTask{}
	b.workstreams.wakeDispatch = func(task workstreamWakeTask) { mu.Lock(); defer mu.Unlock(); tasks = append(tasks, task) }
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		guard.Go("app/workstream-wake-claim-test", func() {
			defer wg.Done()
			b.WorkstreamJobEvent(workstreamJobEvent{Factory: "f", Job: 7, Kind: "merged", ID: "one", At: *now})
		})
	}
	wg.Wait()
	if len(tasks) != 1 {
		t.Fatal("multiple claims", len(tasks))
	}
	workstreamDrainWakes(b, &tasks)
	if runs := workstreamWakeListForTest(t, b, w.ID); len(runs) != 1 {
		t.Fatal(runs)
	}
}

func TestWakeB_EventFallbackKeyUsesUTCSecondsAndValidatesWords(t *testing.T) {
	b, _, now, tasks, _ := workstreamWakeBackend(t)
	w := workstreamCreateForTest(t, b, "Keys", workstreamJobRef("7"))
	ev := workstreamJobEvent{Factory: "f", Job: 7, Kind: "finished", At: now.Add(100 * time.Millisecond)}
	if _, woke := b.WorkstreamJobEvent(ev); !woke {
		t.Fatal("first did not wake")
	}
	ev.At = ev.At.UTC().Add(200 * time.Millisecond)
	if _, woke := b.WorkstreamJobEvent(ev); woke {
		t.Fatal("same UTC second was not deduplicated")
	}
	for _, kind := range []string{"unknown", "morning"} {
		ev.Kind = kind
		if _, woke := b.WorkstreamJobEvent(ev); woke {
			t.Fatal("invalid kind woke")
		}
	}
	ev.Kind = "failed"
	ev.Label = string(make([]byte, 201))
	if _, woke := b.WorkstreamJobEvent(ev); woke {
		t.Fatal("oversize label woke")
	}
	workstreamDrainWakes(b, tasks)
	if len(workstreamWakeListForTest(t, b, w.ID)) != 1 {
		t.Fatal("invalid event made a row")
	}
}

func TestWakeA_LatestSlotIncludesExactEight(t *testing.T) {
	now := time.Date(2026, 11, 3, 8, 0, 0, 0, time.Local)
	for _, tc := range []struct{ at, want time.Time }{{now, now}, {now.Add(7 * time.Hour), now}, {now.Add(-time.Minute), time.Time{}}} {
		slot, err := workstreamMorningSlot(tc.at)
		if err != nil || !slot.Equal(tc.want) {
			t.Fatalf("%s: slot %s want %s (%v)", tc.at, slot, tc.want, err)
		}
	}
}

func TestWakeA_M3_PreEightOpeningNeverCatchesUpClosedDay(t *testing.T) {
	b, _, now, tasks, turns := workstreamWakeBackend(t)
	*now = time.Date(2026, 11, 1, 7, 59, 0, 0, time.Local)
	w := workstreamCreateForTest(t, b, "Closed day")
	if _, err := b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: w.ID, Patch: appwire.WorkstreamPatch{Wake: &appwire.WorkstreamWake{Morning: true}}}); err != nil {
		t.Fatal(err)
	}
	// No tick happens on the intervening day while the app is closed.
	*now = time.Date(2026, 11, 3, 7, 0, 0, 0, time.Local)
	b.workstreamWakeTick(*now)
	if len(*tasks) != 0 || *turns != 0 {
		t.Fatal("opening before 08:00 queued a closed day's wake")
	}
	if runs := workstreamWakeListForTest(t, b, w.ID); len(runs) != 0 {
		t.Fatalf("opening before 08:00 created a wake row: %+v", runs)
	}
	*now = time.Date(2026, 11, 3, 8, 0, 0, 0, time.Local)
	b.workstreamWakeTick(*now)
	if len(*tasks) != 1 {
		t.Fatalf("08:00 queued %d wakes, want one", len(*tasks))
	}
	workstreamDrainWakes(b, tasks)
	b.workstreamWakeTick(*now)
	if *turns != 1 || len(*tasks) != 0 {
		t.Fatalf("today did not wake exactly once: %d turns, %d queued", *turns, len(*tasks))
	}
	s, err := b.workstreamStore(false)
	if err != nil || s == nil {
		t.Fatalf("store: %v", err)
	}
	runs, err := s.Wakes(context.Background(), w.ID, 100)
	if err != nil || len(runs) != 1 || runs[0].Event.Kind != "morning" || !runs[0].Event.At.Equal(*now) || !runs[0].StartedAt.Equal(*now) || runs[0].State != "settled" {
		t.Fatalf("expected only today's 08:00 wake: %+v %v", runs, err)
	}
}

func TestWakeF_RecordedWakeDoesNotInventUnknownSpend(t *testing.T) {
	b, _, now, tasks, _ := workstreamWakeBackend(t)
	w := workstreamCreateForTest(t, b, "Unknown spend", workstreamJobRef("7"))
	b.workstreams.spend = func(string) *float64 { return nil }
	b.WorkstreamJobEvent(workstreamJobEvent{Factory: "f", Job: 7, Kind: "finished", At: *now})
	workstreamDrainWakes(b, tasks)
	got, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: w.ID})
	if err != nil || got.Workstream.SpendUSD != nil {
		t.Fatal(got, err)
	}
	listed, err := b.WorkstreamList(appwire.WorkstreamListParams{})
	if err != nil || listed.Workstreams[0].SpendUSD != nil {
		t.Fatal(listed, err)
	}
	if runs := workstreamWakeListForTest(t, b, w.ID); runs[0].CostUSD == nil || *runs[0].CostUSD != .04 {
		t.Fatal(runs)
	}
}

func TestWakeB_ExpiredQueuedClaimNeverCallsTheModel(t *testing.T) {
	b, _, now, tasks, turns := workstreamWakeBackend(t)
	w := workstreamCreateForTest(t, b, "Stopped", workstreamJobRef("7"))
	b.WorkstreamJobEvent(workstreamJobEvent{Factory: "f", Job: 7, Kind: "finished", At: *now})
	*now = now.Add(workstream.WakeAbandonedAfter + time.Millisecond)
	workstreamDrainWakes(b, tasks)
	runs := workstreamWakeListForTest(t, b, w.ID)
	if *turns != 0 || runs[0].State != "failed" || workstreamValue(runs[0].Error) != workstream.WakeStoppedError || runs[0].CostUSD != nil {
		t.Fatal(runs, *turns)
	}
}
