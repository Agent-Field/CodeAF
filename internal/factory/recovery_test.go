package factory

import (
	"testing"
	"time"
)

func TestRecoveryClassifiesOnlyTransientFailures(t *testing.T) {
	for _, msg := range []string{"connection reset by peer", "i/o timeout", "the connection to the model dropped", "provider rate limit", "http 503 temporarily unavailable"} {
		code, detail := ClassifyFailure(msg)
		if !TransientStepFailure(&Failure{Step: "write", Code: code, Detail: detail}) {
			t.Fatalf("not retryable: %s (%s)", msg, code)
		}
	}
	for _, msg := range []string{"provider unauthorized 401", "permission denied", "context canceled", "the request did not get through", "panic: nil pointer"} {
		code, detail := ClassifyFailure(msg)
		if TransientStepFailure(&Failure{Step: "write", Code: code, Detail: detail}) {
			t.Fatalf("unsafe automatic replay: %s", msg)
		}
	}
	if TransientStepFailure(&Failure{Step: "publishing", Code: CodeMachineLost}) {
		t.Fatal("publication replayed")
	}
	now := time.Now()
	it := Item{Autonomy: AutonomyAuto, State: StateRunning, Cap: 10, Stream: &Stream{Phases: []Phase{{Name: "write", State: PhaseDone}}}, Recovery: &StepRecovery{Step: "write", State: "running"}}
	if !PlanStepRecovery(&it, now) || it.Recovery != nil {
		t.Fatal("successful step kept its old recovery window")
	}
}

func TestRecoveryWindowExcludesTimeSpentRunningAnAttempt(t *testing.T) {
	now := time.Now()
	it := Item{Kind: KindIssue, State: StateNeedsYou, Autonomy: AutonomyAuto, Cap: 10, Stream: &Stream{}, Failure: &Failure{Step: "test", Code: CodeMachineLost, Next: FailureStopped, At: now}, Recovery: &StepRecovery{Step: "test", State: "running", Attempts: 1, Limit: 3, RunningAt: now.Add(-10 * time.Minute), Deadline: now.Add(-9 * time.Minute)}}
	if !PlanStepRecovery(&it, now) || it.Recovery.State != "waiting" || it.Recovery.Attempts != 1 || !it.Recovery.Deadline.After(now) {
		t.Fatalf("productive running consumed reconnect window: %+v", it.Recovery)
	}
}
