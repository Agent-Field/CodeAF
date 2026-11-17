package run

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A FOLDER JOB FINISHES WHERE IT LANDS (owner decision 2, 2026-11-17): done
// on its local branch with its proof, no approval waiting and nothing
// published, whether its own loop or a worker's report lands it, and even
// when a check fell short, since nothing leaves the machine.
func TestAFolderJobFinishesOnItsLocalBranch(t *testing.T) {
	for _, state := range []factory.State{factory.StateLanded, factory.StateShipped} {
		t.Run(string(state), func(t *testing.T) {
			now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
			it := factory.Item{ID: 7, Repo: "/home/me/proj", Kind: factory.KindIssue, Title: "Tidy", State: factory.StateRunning, HeadCommit: strings.Repeat("b", 40),
				Proof:  []factory.Claim{{Text: "tests pass", OK: false}},
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", State: factory.PhaseDone}}, Log: []factory.LogLine{{Text: "branch: factory/7-tidy"}}}}
			if err := FinishReport(&it, state, now); err != nil {
				t.Fatal(err)
			}
			if it.State != factory.StateShipped || factory.JobState(it) != "done" || it.Approval != nil || it.PullRequest != nil {
				t.Fatalf("folder job = state %q (%s) approval %+v pr %+v", it.State, factory.JobState(it), it.Approval, it.PullRequest)
			}
			last := it.Stream.Log[len(it.Stream.Log)-1].Text
			if want := factory.FolderFinishedWords("factory/7-tidy", "/home/me/proj"); !strings.Contains(last, want) {
				t.Fatalf("last line = %q, want %q", last, want)
			}
		})
	}
}

// A job on a repository still waits at its proof gate as it always did.
func TestARepositoryJobStillLandsForItsApproval(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 8, Repo: "acme/api", Kind: factory.KindIssue, Title: "Tidy", State: factory.StateRunning,
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", State: factory.PhaseDone}}}}
	if err := FinishReport(&it, factory.StateLanded, now); err != nil {
		t.Fatal(err)
	}
	if it.State != factory.StateLanded {
		t.Fatalf("repository job = %q, want landed", it.State)
	}
}
