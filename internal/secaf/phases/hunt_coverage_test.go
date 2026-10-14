package phases

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/agentsession/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// INVOCATIONS ARE COUNTED, including a repeated strategy, and an empty
// successful search must remain distinct from a failed search.
func TestHuntCoverageKeepsDuplicateAttemptsAndSelectionOrder(t *testing.T) {
	spy := &callSpy{answers: map[string]json.RawMessage{"run_auth_hunter": json.RawMessage(`{"findings":[]}`)}, failures: map[string]error{"run_injection_hunter": errors.New("cannot search")}}
	fake := &appx.Fake{CallFn: spy.fn()}
	findings, raw, failed, err := runHunters(t.Context(), fake, testNodeID, "/repo", reconFixture(t, "minimal"),
		[]schemas.HuntStrategy{schemas.HuntStrategyInjection, schemas.HuntStrategyAuth, schemas.HuntStrategyInjection}, "quick", 3, 1)
	if err != nil || len(findings) != 0 || raw != 0 {
		t.Fatalf("findings=%v raw=%d err=%v", findings, raw, err)
	}
	want := []schemas.HuntFailure{{Strategy: "injection", Reason: "cannot search"}, {Strategy: "injection", Reason: "cannot search"}}
	if !reflect.DeepEqual(failed, want) {
		t.Fatalf("failures=%+v want=%+v", failed, want)
	}
}

func TestHuntCancellationStillStopsThePhase(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, _, err := runHunters(ctx, &appx.Fake{}, testNodeID, "/repo", reconFixture(t, "minimal"), []schemas.HuntStrategy{schemas.HuntStrategyAuth}, "quick", 1, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled hunt returned %v", err)
	}
}
