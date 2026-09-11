package bare

import (
	"context"
	"encoding/json"
	"testing"
)

// recordingStage is a staged call that says which of its two endings it got.
type recordingStage struct {
	committed chan struct{}
	withdrawn chan struct{}
}

func newRecordingStage() *recordingStage {
	return &recordingStage{committed: make(chan struct{}, 2), withdrawn: make(chan struct{}, 2)}
}

func (s *recordingStage) Commit(context.Context) (string, bool, error) {
	s.committed <- struct{}{}
	return "went ahead", false, nil
}

func (s *recordingStage) Withdraw() { s.withdrawn <- struct{}{} }

func (s *recordingStage) endings() (commits, withdrawals int) {
	return len(s.committed), len(s.withdrawn)
}

// A CALL NOBODY STARTED EARLY IS NOT HELD. With no hold on the context the two
// halves run back to back, which is every call a batch dispatches.
func TestAStagedCallWithNoHoldCommitsAtOnce(t *testing.T) {
	stage := newRecordingStage()
	text, isError, err := RunStaged(context.Background(), stage)
	if err != nil || isError || text != "went ahead" {
		t.Fatalf("RunStaged = %q, %v, %v", text, isError, err)
	}
	if commits, withdrawals := stage.endings(); commits != 1 || withdrawals != 0 {
		t.Fatalf("commits %d, withdrawals %d; want one commit", commits, withdrawals)
	}
}

// A HELD CALL WAITS AT THE SEAM, and the first decision about it is the only
// one: released, it commits and a later withdrawal changes nothing.
func TestAHeldCallCommitsOnlyWhenReleased(t *testing.T) {
	stage := newRecordingStage()
	hold := NewHold()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = RunStaged(WithHold(context.Background(), hold), stage)
	}()
	select {
	case <-stage.committed:
		t.Fatal("the held call committed before it was released")
	case <-done:
		t.Fatal("the held call returned before it was decided")
	default:
	}
	hold.Release()
	hold.Withdraw()
	<-done
	if commits, withdrawals := stage.endings(); commits != 1 || withdrawals != 0 {
		t.Fatalf("commits %d, withdrawals %d; want exactly one commit", commits, withdrawals)
	}
}

// A WITHDRAWN CALL NEVER REACHES ITS SECOND HALF, and it returns the one
// sentence a withdrawn call has — an error result, never an empty success.
func TestAWithdrawnCallNeverCommits(t *testing.T) {
	stage := newRecordingStage()
	hold := NewHold()
	hold.Withdraw()
	hold.Release()
	text, isError, err := RunStaged(WithHold(context.Background(), hold), stage)
	if err != nil || !isError || text != withdrawnBeforeItWent {
		t.Fatalf("RunStaged = %q, %v, %v", text, isError, err)
	}
	if commits, withdrawals := stage.endings(); commits != 0 || withdrawals != 1 {
		t.Fatalf("commits %d, withdrawals %d; want one withdrawal", commits, withdrawals)
	}
}

// THE TURN ENDING FIRST IS A WITHDRAWAL: the thing that would have released the
// call is gone. A release that was already made still wins over it.
func TestAHeldCallIsWithdrawnWhenItsTurnEnds(t *testing.T) {
	stage := newRecordingStage()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, isError, _ := RunStaged(WithHold(ctx, NewHold()), stage); !isError {
		t.Fatal("a call held past the end of its turn went ahead")
	}
	if commits, withdrawals := stage.endings(); commits != 0 || withdrawals != 1 {
		t.Fatalf("commits %d, withdrawals %d; want one withdrawal", commits, withdrawals)
	}

	released := newRecordingStage()
	hold := NewHold()
	hold.Release()
	if _, isError, _ := RunStaged(WithHold(ctx, hold), released); isError {
		t.Fatal("a call released before its turn ended was withdrawn")
	}
	if commits, withdrawals := released.endings(); commits != 1 || withdrawals != 0 {
		t.Fatalf("commits %d, withdrawals %d; want one commit", commits, withdrawals)
	}
}

// BEING SAFE TO START EARLY IS A SHAPE. Only [StagedTool] builds a tool that
// answers yes, a nil hold decides nothing, and a settled stage hands its answer
// over whichever way it ends.
func TestOnlyAStagedToolStages(t *testing.T) {
	if (Tool{Name: "plain"}).Stages() {
		t.Fatal("a tool literal claims to stage")
	}
	stage := newRecordingStage()
	tool := StagedTool("staged", "d", json.RawMessage(`{}`), func(context.Context, json.RawMessage) Staged { return stage })
	if !tool.Stages() {
		t.Fatal("a staged tool does not say so")
	}
	if text, _, _ := tool.Execute(context.Background(), nil); text != "went ahead" {
		t.Fatalf("a staged tool's Execute answered %q", text)
	}
	var nothing *Hold
	nothing.Release()
	nothing.Withdraw()
	if text, isError, _ := RunStaged(context.Background(), Settled("no: refused", true)); text != "no: refused" || !isError {
		t.Fatalf("a settled stage answered %q, %v", text, isError)
	}
}
