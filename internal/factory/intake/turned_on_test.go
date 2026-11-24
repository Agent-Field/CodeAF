package intake_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// TURNING A REPOSITORY ON STARTS NOTHING THAT WAS ALREADY OPEN ON IT. Its own
// file says agent-ready issues start on their own; the issue labelled before
// the repository was turned on waits in Backlog for a person, and only an
// issue opened afterwards starts by its label. Turning on a repository once
// queued every open agent-ready issue in the same minute, while its page said
// nothing runs until someone starts them.
func TestTurningARepoOnStartsNoneOfItsOpenIssues(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/repo"}); err != nil {
		t.Fatal(err)
	}
	on, ok := st.OnSince("Owner/Repo")
	if !ok {
		t.Fatal("turning a repository on kept no time")
	}
	f, err := pipeline.Parse("start:\n  labels: [agent-ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	started := map[int]bool{}
	in := intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return f, nil }, Launch: func(id int) error {
		started[id] = true
		return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
	}})
	issue := func(num int, created time.Time) factory.Item {
		return factory.Item{Origin: "fake", Repo: "owner/repo", Num: num, Kind: factory.KindIssue, Tier: factory.TierOwner,
			Title: "Fix the docs", Body: "fix docs/readme.md", Labels: []string{"agent-ready"}, Created: created}
	}
	src := source{[]factory.Item{issue(1, on.Add(-48*time.Hour)), issue(2, on.Add(time.Minute))}}
	if _, err := github.PollOnce(ctx, src, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	if err := in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := st.List()
	if err != nil || len(items) != 2 {
		t.Fatalf("items %+v %v", items, err)
	}
	for _, it := range items {
		switch it.Num {
		case 1:
			if started[it.ID] || it.State != factory.StateNew || !it.SetupHeld {
				t.Fatalf("an issue open before the repository was turned on: started %v, %s, held %v", started[it.ID], it.State, it.SetupHeld)
			}
		case 2:
			if !started[it.ID] || it.SetupHeld {
				t.Fatalf("an issue opened after the repository was turned on did not start by its label: %s, held %v", it.State, it.SetupHeld)
			}
		}
	}

	// Saving the same list again keeps the time it was turned on; turning it
	// off and on again stamps a new one.
	if err := st.SetRepos([]string{"owner/repo", "owner/other"}); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.OnSince("owner/repo"); !again.Equal(on) {
		t.Fatalf("a repository still on was stamped again: %v, was %v", again, on)
	}
	if err := st.SetRepos(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.OnSince("owner/repo"); ok {
		t.Fatal("a repository turned off still says when it was on")
	}
}
