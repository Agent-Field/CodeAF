package supervisor

import (
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
)

func TestReadRatchetSnapshotUsesDurableInputs(t *testing.T) {
	workspace := t.TempDir()
	ledgers.AppendBlockerRecord(workspace, ledgers.BlockerInput{
		BlockerID: "one", Status: "open", CycleOpened: 1, Text: "first",
	})
	ledgers.AppendBlockerRecord(workspace, ledgers.BlockerInput{
		BlockerID: "two", Status: "verified", CycleOpened: 1, Text: "second",
	})
	snapshot, err := ReadRatchetSnapshot(workspace, VerdictReaderFunc(
		func(gotWorkspace string) (*DiskVerdict, error) {
			if gotWorkspace != workspace {
				t.Fatalf("workspace = %q", gotWorkspace)
			}
			return &DiskVerdict{
				Verdict:  VerdictFail,
				Blockers: []any{"a", "b"},
			}, nil
		},
	))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.OpenBlockers != 1 || snapshot.VerdictBlockers != 2 ||
		snapshot.VerdictStatus == nil || *snapshot.VerdictStatus != VerdictFail {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestReadRatchetSnapshotNoVerdict(t *testing.T) {
	snapshot, err := ReadRatchetSnapshot(t.TempDir(), VerdictReaderFunc(
		func(string) (*DiskVerdict, error) { return nil, nil },
	))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.VerdictStatus != nil || snapshot.VerdictBlockers != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestReadRatchetSnapshotPropagatesReaderError(t *testing.T) {
	want := errors.New("read failed")
	_, got := ReadRatchetSnapshot(t.TempDir(), VerdictReaderFunc(
		func(string) (*DiskVerdict, error) { return nil, want },
	))
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func TestRunSupervisorPropagatesResumeError(t *testing.T) {
	pass := VerdictFail
	want := errors.New("resume failed")
	_, got := RunSupervisor(SupervisorDeps{
		Snapshot: func() (RatchetSnapshot, error) {
			return RatchetSnapshot{OpenBlockers: 1, VerdictStatus: &pass}, nil
		},
		BudgetExhausted: func() BudgetExhaustion { return BudgetExhaustion{} },
		ResumeOnce:      func(int) error { return want },
	}, nil)
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func TestRunSupervisorSnapshotsAfterFailingChildAndContinues(t *testing.T) {
	// Validation contract B1: terminal failure is child state, not a harness
	// error; the supervisor snapshots it and advances the ratchet again.
	failed, passed := VerdictFail, VerdictPass
	resumeCalls := 0
	snapshots := []RatchetSnapshot{
		{OpenBlockers: 3, VerdictStatus: &failed},
		{OpenBlockers: 2, VerdictStatus: &failed},
		{VerdictStatus: &passed},
	}
	snapshotCalls := 0
	result, err := RunSupervisor(SupervisorDeps{
		Snapshot: func() (RatchetSnapshot, error) {
			current := snapshots[snapshotCalls]
			snapshotCalls++
			return current, nil
		},
		BudgetExhausted: func() BudgetExhaustion { return BudgetExhaustion{} },
		ResumeOnce: func(int) error {
			resumeCalls++
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomePassed || result.Attempts != 2 ||
		resumeCalls != 2 || snapshotCalls != 3 {
		t.Fatalf(
			"result=%#v resumes=%d snapshots=%d",
			result, resumeCalls, snapshotCalls,
		)
	}
}

// stalledDeps builds a supervisor whose every resume leaves the ratchet metric
// exactly where it was — the shape of the cobra#2257 run in
// audit-notes/headless-regression-audit.md §14, where each re-entry re-derived
// the same verdict against the same pre-existing red test.
func stalledDeps(resumes *int, prints []string) SupervisorDeps {
	failed := VerdictFail
	call := 0
	return SupervisorDeps{
		Snapshot: func() (RatchetSnapshot, error) {
			return RatchetSnapshot{OpenBlockers: 3, VerdictStatus: &failed}, nil
		},
		BudgetExhausted: func() BudgetExhaustion { return BudgetExhaustion{} },
		ResumeOnce: func(int) error {
			*resumes++
			return nil
		},
		Fingerprint: func() (string, bool) {
			if prints == nil {
				return "", false
			}
			index := call
			if index >= len(prints) {
				index = len(prints) - 1
			}
			call++
			return prints[index], prints[index] != ""
		},
	}
}

func TestRunSupervisorStopsAtAnUnchangedTree(t *testing.T) {
	// One resume that moves neither the tree nor the metric ends it: the
	// second attempt would begin from the inputs that produced the first.
	resumes := 0
	// before-1, after-1
	result, err := RunSupervisor(stalledDeps(&resumes, []string{"sha|abc", "sha|abc"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeNoProgress || result.Attempts != 1 || resumes != 1 {
		t.Fatalf("result=%#v resumes=%d", result, resumes)
	}
	if !strings.Contains(result.Reason, "changed nothing") {
		t.Fatalf("reason = %q", result.Reason)
	}
}

func TestRunSupervisorKeepsStallBudgetWhenTheTreeMoved(t *testing.T) {
	// A resume that edited files but did not close a blocker is not a fixed
	// point: it is the ordinary stall the ported policy already tolerates, and
	// it must still get its second attempt.
	resumes := 0
	result, err := RunSupervisor(stalledDeps(&resumes,
		[]string{"sha|one", "sha|two", "sha|two", "sha|three"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeNoProgress || result.Attempts != 2 || resumes != 2 {
		t.Fatalf("result=%#v resumes=%d", result, resumes)
	}
	if !strings.Contains(result.Reason, "consecutive resumes") {
		t.Fatalf("reason = %q", result.Reason)
	}
}

func TestRunSupervisorWithoutFingerprintKeepsPortedPolicy(t *testing.T) {
	// The seam is optional, and nil means the golden-fixture behaviour: two
	// stalls before the ratchet gives up.
	resumes := 0
	result, err := RunSupervisor(stalledDeps(&resumes, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeNoProgress || result.Attempts != 2 || resumes != 2 {
		t.Fatalf("result=%#v resumes=%d", result, resumes)
	}
}

func TestRunSupervisorIgnoresAnUnavailableFingerprint(t *testing.T) {
	// git could not be asked. An empty answer is not evidence of sameness, so
	// the stall count stays in charge.
	resumes := 0
	result, err := RunSupervisor(stalledDeps(&resumes, []string{"", ""}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeNoProgress || result.Attempts != 2 || resumes != 2 {
		t.Fatalf("result=%#v resumes=%d", result, resumes)
	}
}
