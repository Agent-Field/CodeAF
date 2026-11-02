package intake_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A JOB A START RULE STARTS IS THE FACTORY OWNER'S: with nobody on it, it
// starts with the owner as its person, so Auto no longer stops at "A job owner
// is required"; a job that already has a person keeps them; and one a rule
// holds for approval stays nobody's, so any teammate who could approve it
// still can.
func TestARuleStartedJobBelongsToTheFactoryOwner(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/repo"}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	add := func(it factory.Item) factory.Item {
		it.Repo, it.Kind, it.State, it.Autonomy, it.Labels = "owner/repo", factory.KindIssue, factory.StateNew, factory.AutonomyAuto, append([]string{"ready", "size:S"}, it.Labels...)
		got, err := st.Create(it)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	nobody := add(factory.Item{Title: "nobody on it", Num: 1})
	someone := add(factory.Item{Title: "assigned", Num: 2, Assignee: "priya"})
	held := add(factory.Item{Title: "asks first", Num: 3, Body: "rework auth/login.go"})
	history, err := record.Open(st, record.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := pipeline.Parse("default_budget: 1\nstart:\n  labels: [ready]\n  ask_first: [touch auth/]\n")
	if err != nil {
		t.Fatal(err)
	}
	in := intake.New(st, intake.Options{
		Record: history, Load: func(string) (pipeline.File, error) { return rules, nil },
		Launch: func(id int) error {
			return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
		},
		Owner: func(context.Context) string { return "@Owner" },
	})
	if err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(nobody.ID)
	if got.State != factory.StateQueued || got.Person() != "owner" {
		t.Fatalf("rule-started job: state %s, person %q", got.State, got.Person())
	}
	got.State, got.QKind = factory.StateNeedsYou, factory.QKindApprove
	if why := factory.AutonomyBlockReason(got); strings.Contains(why, "owner is required") {
		t.Fatalf("Auto still wants an owner: %q", why)
	}
	if got, _ := st.Get(someone.ID); got.Person() != "priya" {
		t.Fatalf("an assigned job changed hands to %q", got.Person())
	}
	if got, _ := st.Get(held.ID); got.QKind != factory.QKindAskFirst || got.Person() != "" {
		t.Fatalf("a held job: kind %q, person %q", got.QKind, got.Person())
	}
}
