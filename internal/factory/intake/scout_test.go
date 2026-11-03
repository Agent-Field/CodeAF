package intake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// AN AUTOMATIC START GETS THE BETTER READ FIRST: a job the start rules would
// start is looked at closer before it launches, one they hold for approval is
// not (a person will look), and a look that fails (a limit, no answer) never
// stops the start.
func TestAJobTheRulesStartIsLookedAtCloserBeforeItLaunches(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/repo"}); err != nil {
		t.Fatal(err)
	}
	add := func(it factory.Item) factory.Item {
		it.Repo, it.Kind, it.State, it.Labels = "owner/repo", factory.KindIssue, factory.StateNew, []string{"ready", "size:S"}
		got, err := st.Create(it)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	starts := add(factory.Item{Title: "fix the typo", Num: 1})
	failing := add(factory.Item{Title: "fix the other typo", Num: 2})
	held := add(factory.Item{Title: "asks first", Num: 3, Body: "rework auth/login.go"})
	history, err := record.Open(st, record.Options{Now: func() time.Time { return time.Unix(100, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := pipeline.Parse("default_budget: 1\nstart:\n  labels: [ready]\n  ask_first: [touch auth/]\n")
	if err != nil {
		t.Fatal(err)
	}
	var looked []int
	scoutedAtLaunch := map[int]bool{}
	in := intake.New(st, intake.Options{
		Record: history, Load: func(string) (pipeline.File, error) { return rules, nil },
		Scout: func(_ context.Context, id int) error {
			looked = append(looked, id)
			if id == failing.ID {
				return errors.New("monthly limit for AI reached")
			}
			return st.Annotate(id, func(it *factory.Item) error { it.Scout = &factory.Scout{Plan: []string{"edit"}}; return nil })
		},
		Launch: func(id int) error {
			it, _ := st.Get(id)
			scoutedAtLaunch[id] = it.Scout != nil
			return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
		},
	})
	if err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(looked) != 2 || looked[0]+looked[1] != starts.ID+failing.ID || looked[0] == held.ID || looked[1] == held.ID {
		t.Fatalf("looked at %v, want jobs %d and %d once each", looked, starts.ID, failing.ID)
	}
	if !scoutedAtLaunch[starts.ID] {
		t.Fatal("the job launched before its look was on it")
	}
	if got, _ := st.Get(failing.ID); got.State != factory.StateQueued {
		t.Fatalf("a failed look stopped the start: state %s", got.State)
	}
	if got, _ := st.Get(held.ID); got.QKind != factory.QKindAskFirst {
		t.Fatalf("held job: kind %q", got.QKind)
	}
}
