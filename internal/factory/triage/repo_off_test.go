package triage

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// No model is paid to read a job of a repository that is off; the job waits,
// unread, and is read once the repository is on again.
func TestTriageNeverReadsAJobOfARepoThatIsOff(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/web"}); err != nil {
		t.Fatal(err)
	}
	made, err := st.Create(factory.Item{Repo: "acme/api", Num: 3, Kind: factory.KindIssue, Title: "the meter resets", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	call := func(context.Context, string) (string, error) {
		calls++
		return clean, nil
	}
	ReadOne(context.Background(), st, call, Options{})
	if got, _ := st.Get(made.ID); calls != 0 || !Wants(got) {
		t.Fatalf("triage read an off repository's job: %d calls, %+v", calls, got.Triage)
	}

	if err := st.SetRepos([]string{"acme/api", "acme/web"}); err != nil {
		t.Fatal(err)
	}
	ReadOne(context.Background(), st, call, Options{})
	if got, _ := st.Get(made.ID); calls != 1 || Wants(got) {
		t.Fatalf("turned back on, triage made %d calls and left %+v", calls, got.Triage)
	}
}
