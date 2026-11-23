package intake_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A labelled job of a repository that is off is neither started nor asked
// about, and its rules are not read; turned back on, the same job starts.
func TestIntakePassesByARepoThatIsOff(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/other"}); err != nil {
		t.Fatal(err)
	}
	made, err := st.Create(factory.Item{Repo: "owner/repo", Num: 1, Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateNew,
		Title: "Incoming job", Body: "fix docs/readme.md", Labels: []string{"agent-ready", "size:S"},
		Triage: factory.Triage{StartJudged: true, StartMatched: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Parse("start:\n  labels: [agent-ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	f.DefaultBudget = 1
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	loads, started := 0, 0
	in := intake.New(st, intake.Options{Record: history,
		Load:   func(string) (pipeline.File, error) { loads++; return f, nil },
		Launch: func(int) error { started++; return nil }})

	if err := in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loads != 0 || started != 0 || got.State != factory.StateNew || got.QKind != "" || got.AskedFirstReason != "" {
		t.Fatalf("intake worked on an off repository: loads %d starts %d job %+v", loads, started, got)
	}

	if err := st.SetRepos([]string{"owner/other", "owner/repo"}); err != nil {
		t.Fatal(err)
	}
	if err := in.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if loads != 1 || started != 1 {
		t.Fatalf("turned back on, intake loads %d starts %d", loads, started)
	}
}
