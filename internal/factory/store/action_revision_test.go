package store

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// preActionDoc is the complete envelope understood before action_revision existed.
type preActionDoc struct {
	Schema         int          `json:"schema"`
	Revision       int          `json:"revision"`
	BudgetRevision int          `json:"budget_revision"`
	Item           factory.Item `json:"item"`
}

func TestActionRevisionOldDocumentIsStrict(t *testing.T) {
	st := open(t)
	raw := []byte(`{"schema":1,"revision":9,"budget_revision":2,"item":{"Title":"old","Repo":"acme/api"}}`)
	if err := os.WriteFile(st.ItemPath(1), raw, 0600); err != nil {
		t.Fatal(err)
	}
	assertActionStrict(t, st, 1, 9)
	if err := st.Update(1, func(*factory.Item) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckActionExpected(1, 9); err != nil {
		t.Fatal(err)
	}
}

func TestActionRevisionOldEngineRewriteIsStrict(t *testing.T) {
	st := open(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(*factory.Item) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckActionExpected(it.ID, 1); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(st.ItemPath(it.ID))
	if err != nil {
		t.Fatal(err)
	}
	var old preActionDoc
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["action_revision"]; ok {
		t.Fatal("old engine preserved unknown field")
	}
	if err := os.WriteFile(st.ItemPath(it.ID), raw, 0600); err != nil {
		t.Fatal(err)
	}
	assertActionStrict(t, st, it.ID, 2)
}

func assertActionStrict(t *testing.T, st *Store, id, revision int) {
	t.Helper()
	for _, expect := range []int{revision - 1, revision, revision + 1} {
		err := st.CheckActionExpected(id, expect)
		var stale *StaleError
		if expect == revision {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			if !errors.As(err, &stale) {
				t.Fatalf("expect %d at %d: %v", expect, revision, err)
			}
			called := false
			err = st.UpdateExpected(id, expect, func(*factory.Item) error { called = true; return nil })
			if !errors.As(err, &stale) || called {
				t.Fatalf("guarded write at %d: err=%v called=%v", expect, err, called)
			}
			err = st.UpdateActionExpected(id, &expect, func(*factory.Item) error { called = true; return nil })
			if !errors.As(err, &stale) || called {
				t.Fatalf("opted-in guarded write at %d: err=%v called=%v", expect, err, called)
			}
		}
	}
}

func TestActionRevisionSaveCarriesLocalHistory(t *testing.T) {
	st := open(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "job", Cap: 5})
	if err != nil {
		t.Fatal(err)
	}
	it.BriefWriting = &factory.BriefWriting{State: factory.BriefGenerating}
	if err := st.Save(it); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckActionExpected(it.ID, 1); err != nil {
		t.Fatal(err)
	}
	it.Title = "changed"
	if err := st.Save(it); err != nil {
		t.Fatal(err)
	}
	assertActionStrict(t, st, it.ID, 3)
	revision := 1
	if err := st.UpdateBudgetExpected(it.ID, &revision, func(cur *factory.Item) error { cur.Cap = 10; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestActionRevisionRepositoryMigrationFencesActions(t *testing.T) {
	st := open(t)
	raw := []byte(`{"schema":1,"revision":9,"budget_revision":2,"action_revision":3,"item":{"Title":"old","Repo":"api","Product":"acme"}}`)
	if err := os.WriteFile(st.ItemPath(1), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(1); err != nil {
		t.Fatal(err)
	}
	assertActionStrict(t, st, 1, 10)
	revision := 2
	if err := st.UpdateBudgetExpected(1, &revision, func(cur *factory.Item) error { cur.Cap = 10; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestActionRevisionItemMirrorStartsStrict(t *testing.T) {
	source := open(t)
	it, err := source.Create(factory.Item{Repo: "acme/api", Title: "job"})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Update(it.ID, func(*factory.Item) error { return nil }); err != nil {
		t.Fatal(err)
	}
	it, err = source.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	mirror := open(t)
	if err := mirror.Save(it); err != nil {
		t.Fatal(err)
	}
	assertActionStrict(t, mirror, it.ID, 1)
}

func TestUpdateExpectedRemainsStrictAfterBookkeeping(t *testing.T) {
	st := open(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "job"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(*factory.Item) error { return nil }); err != nil {
		t.Fatal(err)
	}
	called := false
	err = st.UpdateExpected(it.ID, 1, func(*factory.Item) error { called = true; return nil })
	var stale *StaleError
	if !errors.As(err, &stale) || called {
		t.Fatalf("strict write: err=%v called=%v", err, called)
	}
}

func TestUpdateActionExpectedOptsIntoBookkeepingOnly(t *testing.T) {
	st := open(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "job"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error {
		cur.BriefWriting = &factory.BriefWriting{State: factory.BriefGenerating}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	revision := 1
	if err := st.UpdateActionExpected(it.ID, &revision, func(cur *factory.Item) error { cur.Title = "accepted"; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, expect := range []int{1, 4} {
		called := false
		err := st.UpdateActionExpected(it.ID, &expect, func(*factory.Item) error { called = true; return nil })
		var stale *StaleError
		if !errors.As(err, &stale) || called {
			t.Fatalf("expect %d: err=%v called=%v", expect, err, called)
		}
	}
	got, err := st.Get(it.ID)
	if err != nil || got.Title != "accepted" {
		t.Fatalf("opted-in write: %+v %v", got, err)
	}
}
