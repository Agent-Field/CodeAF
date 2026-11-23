package store

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// S6: the unstarted run-as-new cleanup cannot forget a split job with no run.
func TestSplitFromWithoutRunCannotBeForgottenAsCopy(t *testing.T) {
	st := open(t)
	split, err := st.Create(factory.Item{Repo: "acme/api", Title: "Smaller fix", State: factory.StateNew, SplitFrom: 80})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ForgetUnstartedCopy(split.ID); err == nil {
		t.Fatal("unstarted split job was forgotten as a run-as-new copy")
	}
	got, err := st.Get(split.ID)
	if err != nil || got.SplitFrom != 80 || got.FromJob != 0 || got.Stream != nil {
		t.Fatalf("retained split: split_from=%d from_job=%d stream=%v err=%v", got.SplitFrom, got.FromJob, got.Stream, err)
	}
	copy, err := st.Create(factory.Item{Repo: "acme/api", Title: "Run again", State: factory.StateNew, FromJob: 80})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ForgetUnstartedCopy(copy.ID); err != nil {
		t.Fatalf("run-as-new cleanup changed: %v", err)
	}
	if _, err := st.Get(copy.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("provisional copy remains: %v", err)
	}
}
