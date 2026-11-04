package store_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func listStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func listOne(t *testing.T, st *store.Store) factory.Item {
	t.Helper()
	items, err := st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("List: %d items, %v", len(items), err)
	}
	return items[0]
}

func TestListOwnsMutableDocuments(t *testing.T) {
	st := listStore(t)
	stopped := false
	made, err := st.Create(factory.Item{
		Repo: "acme/api", State: factory.StateNew, PersonStopped: &stopped,
		SourceAssignees: []string{}, Labels: []string{"original"},
		BriefSeen: map[string]int{"sam": 2}, Stages: []factory.Stage{{Name: "test", Proof: []string{"original"}}},
		Stream: &factory.Stream{Phases: []factory.Phase{{RoundCosts: map[int]float64{1: 2},
			Result: &factory.StageResult{Files: []string{"original"}, Tests: &factory.TestCounts{Passed: 2}}}},
			Log: []factory.LogLine{{Text: "original"}}},
		AnsweredQuestions: []factory.Question{{Options: []string{"original"}, Answer: &factory.Answer{Text: "original"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := listOne(t, st)
	before := st.DocumentReads()
	mutate := func(it factory.Item) {
		*it.PersonStopped = true
		it.Labels[0] = "mutated"
		it.BriefSeen["sam"] = 99
		it.Stages[0].Proof[0] = "mutated"
		it.Stream.Phases[0].RoundCosts[1] = 99
		it.Stream.Phases[0].Result.Files[0] = "mutated"
		it.Stream.Phases[0].Result.Tests.Passed = 99
		it.Stream.Log[0].Text = "mutated"
		it.AnsweredQuestions[0].Options[0] = "mutated"
		it.AnsweredQuestions[0].Answer.Text = "mutated"
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				mutate(listOne(t, st))
			}
		}()
	}
	wg.Wait()
	if got := listOne(t, st); !reflect.DeepEqual(got, want) {
		t.Fatal("mutating a List result changed a later result")
	}
	if n := st.DocumentReads() - before; n != 0 {
		t.Fatalf("copies read %d documents, want 0", n)
	}
	if got, err := st.Get(made.ID); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("mutating a List result changed the stored document", err)
	}
}

func TestListReappliesOutputRetention(t *testing.T) {
	st := listStore(t)
	_, err := st.Create(factory.Item{Stream: &factory.Stream{Log: []factory.LogLine{
		{At: time.Now(), Text: "older"}, {At: time.Now(), Text: "newer"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	bytes := 100
	st.SetOutputPolicy(func() (int, int) { return 1, bytes })
	if got := listOne(t, st); len(got.Stream.Log) != 2 {
		t.Fatal("initial output missing")
	}
	before := st.DocumentReads()
	bytes = 5
	if got := listOne(t, st); len(got.Stream.Log) != 1 || got.Stream.Log[0].Text != "newer" {
		t.Fatal("cached output ignored stricter retention")
	}
	bytes = 100
	if got := listOne(t, st); len(got.Stream.Log) != 2 || got.Stream.Log[0].Text != "older" {
		t.Fatal("a retained view destroyed the cached output")
	}
	if n := st.DocumentReads() - before; n != 0 {
		t.Fatalf("retention read %d unchanged documents, want 0", n)
	}
}

func TestListReappliesRepositoryMigration(t *testing.T) {
	st := listStore(t)
	made, err := st.Create(factory.Item{Repo: "ledger", Places: []string{"ledger"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := listOne(t, st); got.Repo != "ledger" {
		t.Fatal("unresolved repository changed")
	}
	if err := st.SetRepos([]string{"acme/ledger"}); err != nil {
		t.Fatal(err)
	}
	if got := listOne(t, st); got.Repo != "acme/ledger" || got.Places[0] != "acme/ledger" || !got.Changed.Equal(made.Changed) {
		t.Fatalf("cached legacy document did not migrate: %+v", got)
	}
	if got, err := st.Get(made.ID); err != nil || got.Repo != "acme/ledger" {
		t.Fatal("migration was not persisted", err)
	}
	listOne(t, st) // The migration itself replaced the file.
	before := st.DocumentReads()
	listOne(t, st)
	if n := st.DocumentReads() - before; n != 0 {
		t.Fatalf("migration kept rereading: %d", n)
	}
}

func TestListSeesRenameWithSameSizeAndTime(t *testing.T) {
	st := listStore(t)
	made, err := st.Create(factory.Item{Title: "before"})
	if err != nil {
		t.Fatal(err)
	}
	listOne(t, st)
	path := st.ItemPath(made.ID)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Replace one equal-length string without changing the document's encoding.
	if strings.Count(string(data), `"before"`) != 1 {
		t.Fatal("expected exactly one title to replace")
	}
	data = []byte(strings.Replace(string(data), `"before"`, `"after!"`, 1))
	tmp := filepath.Join(st.Root(), ".replacement")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	if next, err := os.Stat(path); err != nil || next.Size() != info.Size() || !next.ModTime().Equal(info.ModTime()) || os.SameFile(info, next) {
		t.Fatal("replacement did not preserve size/time with a new identity", err)
	}
	before := st.DocumentReads()
	if got := listOne(t, st); got.Title != "after!" {
		t.Fatal("same-size/time replacement was hidden")
	}
	if n := st.DocumentReads() - before; n != 1 {
		t.Fatalf("replacement read %d documents, want 1", n)
	}
}
