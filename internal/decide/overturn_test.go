package decide

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func overturnFixture(t *testing.T) (*Store, Decision) {
	t.Helper()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	store, err := Open(t.TempDir(), "place", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	d := Decision{ID: "original", PlaceID: "place", AskKind: "permission", Subject: "shell-read", At: now.Add(-time.Hour), Reversible: true, Undo: Undo{Token: "stored-undo"}}
	if err := store.Append(d); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMode(KindKey(d.AskKind, d.Subject), ModeDeciding); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordOutcome(KindKey(d.AskKind, d.Subject), Outcome{Agreed: true, At: d.At}); err != nil {
		t.Fatal(err)
	}
	return store, d
}

func TestOverturnRunsStoredUndoAndResetsOnlyItsKind(t *testing.T) {
	for _, next := range []NextTime{NextAsk, NextKeep} {
		for _, action := range []string{"revoke consent", "stop task"} {
			t.Run(string(next)+"/"+action, func(t *testing.T) {
				store, d := overturnFixture(t)
				otherKey := KindKey("permission", "git")
				other := KindState{Mode: ModeDeciding, Recent: []Outcome{{Agreed: true, At: d.At}}}
				if err := store.SetMode(otherKey, other.Mode); err != nil {
					t.Fatal(err)
				}
				if err := store.RecordOutcome(otherKey, other.Recent[0]); err != nil {
					t.Fatal(err)
				}
				active := true
				o, err := NewOverturner(store, func(_ context.Context, u Undo) error {
					if u != d.Undo {
						t.Fatalf("undo=%+v want stored %+v", u, d.Undo)
					}
					active = false
					return nil
				}, func(context.Context) ([]Dependent, error) { return nil, nil })
				if err != nil {
					t.Fatal(err)
				}
				result, err := o.Overturn(context.Background(), d.ID, next)
				if err != nil {
					t.Fatal(err)
				}
				wantMode := ModeLearning
				if next == NextAsk {
					wantMode = ModeAsk
				}
				if active || result.Refusal != "" || result.Mode != wantMode || result.Summary != "" || len(result.Offers) != 0 {
					t.Fatalf("active=%v result=%+v", active, result)
				}
				rows, err := store.List()
				if err != nil {
					t.Fatal(err)
				}
				if rows[0].OverturnedAt == nil || !rows[0].OverturnedAt.Equal(store.now()) {
					t.Fatalf("not stamped: %+v", rows[0])
				}
				mode, err := store.Mode(KindKey(d.AskKind, d.Subject))
				if err != nil {
					t.Fatal(err)
				}
				if mode.Mode != wantMode || len(mode.Recent) != 0 {
					t.Fatalf("mode=%+v", mode)
				}
				got, err := store.Mode(otherKey)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, other) {
					t.Fatalf("other kind changed: %+v", got)
				}
				reopened, err := Open(filepath.Dir(store.path), "place", store.now)
				if err != nil {
					t.Fatal(err)
				}
				persisted, err := reopened.Mode(KindKey(d.AskKind, d.Subject))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(persisted, mode) {
					t.Fatalf("not persisted: %+v", persisted)
				}
			})
		}
	}
}

func TestOverturnListsDirectLaterDependentsWithoutCascade(t *testing.T) {
	store, d := overturnFixture(t)
	later := d.At.Add(time.Minute)
	child := Decision{ID: "child", PlaceID: d.PlaceID, AskKind: "choice", At: later, Reversible: true, Undo: Undo{Token: "child-undo"}}
	if err := store.Append(child); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMode("choice", ModeDeciding); err != nil {
		t.Fatal(err)
	}
	candidates := []Dependent{
		{ID: "t1", Kind: DependentTask, Title: "First task", At: later, Uses: []string{d.ID}},
		{ID: "t2", Kind: DependentTask, At: later, Uses: []string{d.ID}},
		{ID: child.ID, Kind: DependentDecision, At: later, Uses: []string{d.ID}},
		{ID: "older", Kind: DependentTask, At: d.At.Add(-time.Minute), Uses: []string{d.ID}},
		{ID: "same-time", Kind: DependentTask, At: d.At, Uses: []string{d.ID}},
		{ID: "unrelated", Kind: DependentTask, At: later, Uses: []string{"elsewhere"}},
		{ID: "grandchild", Kind: DependentTask, At: later, Uses: []string{child.ID}},
		{ID: "t1", Kind: DependentTask, At: later, Uses: []string{d.ID}},
		{ID: "", Kind: DependentTask, At: later, Uses: []string{d.ID}},
		{ID: "unknown", Kind: "unknown", At: later, Uses: []string{d.ID}},
	}
	before := append([]Dependent(nil), candidates...)
	calls := 0
	o, err := NewOverturner(store, func(_ context.Context, u Undo) error {
		calls++
		if u != d.Undo {
			t.Fatalf("cascaded undo %+v", u)
		}
		return nil
	}, func(context.Context) ([]Dependent, error) { return candidates, nil })
	if err != nil {
		t.Fatal(err)
	}
	result, err := o.Overturn(context.Background(), d.ID, NextKeep)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !reflect.DeepEqual(result.Dependents, candidates[:3]) || !reflect.DeepEqual(result.Offers, []string{"notify", "pause"}) || result.Summary != "2 tasks and 1 decision used this" {
		t.Fatalf("result=%+v calls=%d", result, calls)
	}
	if !reflect.DeepEqual(candidates, before) {
		t.Fatal("dependency snapshot changed")
	}
	rows, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows[1], child) {
		t.Fatalf("dependent decision changed: %+v", rows[1])
	}
	mode, err := store.Mode("choice")
	if err != nil {
		t.Fatal(err)
	}
	if mode.Mode != ModeDeciding {
		t.Fatal("dependent kind changed")
	}
	result.Dependents[0].Uses[0] = "changed"
	if candidates[0].Uses[0] != d.ID {
		t.Fatal("result aliases dependency source")
	}
}

func TestOverturnRefusesOrFailsWithoutChangingLedger(t *testing.T) {
	for _, failure := range []string{"irreversible", "missing token", "owner refuses", "undo fails", "dependency fails", "cancelled", "unknown", "invalid next", "empty id"} {
		t.Run(failure, func(t *testing.T) {
			store, d := overturnFixture(t)
			if failure == "irreversible" || failure == "missing token" {
				if err := store.update(func(c *doc) error {
					if failure == "irreversible" {
						c.Decisions[0].Reversible = false
					} else {
						c.Decisions[0].Undo.Token = ""
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := store.List()
			if err != nil {
				t.Fatal(err)
			}
			modeBefore, err := store.Mode(KindKey(d.AskKind, d.Subject))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			boom := errors.New("engine unavailable")
			o, err := NewOverturner(store, func(context.Context, Undo) error {
				calls++
				if failure == "owner refuses" {
					return fmt.Errorf("The started task has already finished: %w", ErrNotUndoable)
				}
				if failure == "undo fails" {
					return boom
				}
				return nil
			}, func(context.Context) ([]Dependent, error) {
				if failure == "dependency fails" {
					return nil, boom
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id, next := d.ID, NextKeep
			if failure == "cancelled" {
				cancel()
			}
			if failure == "unknown" {
				id = "missing"
			}
			if failure == "empty id" {
				id = ""
			}
			if failure == "invalid next" {
				next = "deciding"
			}
			result, err := o.Overturn(ctx, id, next)
			switch failure {
			case "irreversible", "missing token", "owner refuses":
				if err != nil || result.Refusal == "" {
					t.Fatalf("refusal=%+v err=%v", result, err)
				}
			case "unknown":
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err=%v", err)
				}
			case "empty id", "invalid next":
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("err=%v", err)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("err=%v", err)
				}
			default:
				if !errors.Is(err, boom) {
					t.Fatalf("err=%v", err)
				}
			}
			wantCalls := 0
			if failure == "owner refuses" || failure == "undo fails" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("undo calls=%d want=%d", calls, wantCalls)
			}
			after, err := store.List()
			if err != nil {
				t.Fatal(err)
			}
			modeAfter, err := store.Mode(KindKey(d.AskKind, d.Subject))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(modeBefore, modeAfter) {
				t.Fatal("failed reversal changed ledger")
			}
		})
	}
}

func TestOverturnConcurrentRetriesUndoOnceAndKeepNewLearning(t *testing.T) {
	store, d := overturnFixture(t)
	second, err := Open(filepath.Dir(store.path), "place", store.now)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	undo := func(context.Context, Undo) error { calls.Add(1); return nil }
	reader := func(context.Context) ([]Dependent, error) { return nil, nil }
	a, err := NewOverturner(store, undo, reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewOverturner(second, undo, reader)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, o := range []*Overturner{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := o.Overturn(context.Background(), d.ID, NextKeep); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("undo calls=%d", calls.Load())
	}
	answer := ProposalAnswer{AskKind: d.AskKind, SubjectClass: d.Subject, ProposedKey: "yes", ChosenKey: "yes", At: store.now()}
	if _, err := store.RecordProposal(answer); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Overturn(context.Background(), d.ID, NextAsk)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := store.Mode(KindKey(d.AskKind, d.Subject))
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || result.Mode != ModeLearning || len(mode.Recent) != 1 || !reflect.DeepEqual(rows, after) {
		t.Fatalf("retry changed result: calls=%d mode=%+v result=%+v", calls.Load(), mode, result)
	}
}

func TestOverturnRequiresEngineDoors(t *testing.T) {
	store, _ := overturnFixture(t)
	undo := func(context.Context, Undo) error { return nil }
	reader := func(context.Context) ([]Dependent, error) { return nil, nil }
	for _, args := range []struct {
		store  *Store
		undo   UndoAction
		reader ReadDependents
	}{{nil, undo, reader}, {store, nil, reader}, {store, undo, nil}} {
		if _, err := NewOverturner(args.store, args.undo, args.reader); !errors.Is(err, ErrInvalid) {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestOverturnDependencySummary(t *testing.T) {
	for _, tc := range []struct {
		deps []Dependent
		want string
	}{
		{nil, ""},
		{[]Dependent{{Kind: DependentTask}}, "1 task used this"},
		{[]Dependent{{Kind: DependentTask}, {Kind: DependentTask}}, "2 tasks used this"},
		{[]Dependent{{Kind: DependentDecision}}, "1 decision used this"},
		{[]Dependent{{Kind: DependentDecision}, {Kind: DependentDecision}}, "2 decisions used this"},
	} {
		if got := dependentSummary(tc.deps); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestOverturnKnownRefusalDoesNotNeedDependencyReader(t *testing.T) {
	store, d := overturnFixture(t)
	if err := store.update(func(c *doc) error { c.Decisions[0].Reversible = false; return nil }); err != nil {
		t.Fatal(err)
	}
	o, err := NewOverturner(store, func(context.Context, Undo) error { t.Fatal("undo ran"); return nil }, func(context.Context) ([]Dependent, error) {
		t.Fatal("reader ran for known refusal")
		return nil, errors.New("unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := o.Overturn(context.Background(), d.ID, NextKeep)
	if err != nil || result.Refusal != ErrNotUndoable.Error() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
