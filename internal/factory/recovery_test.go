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
