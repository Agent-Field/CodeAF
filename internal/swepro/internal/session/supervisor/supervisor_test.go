package supervisor

import (
	"errors"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
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
