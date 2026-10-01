//go:build !windows

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

type gatewayAuthSeat struct {
	status int
	calls  atomic.Int32
}

func (s *gatewayAuthSeat) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	s.calls.Add(1)
	return nil, &provider.APIError{Status: s.status, Message: "account refused"}
}

// The real gateway and program engine must preserve both facts: the local
// token works, and the upstream account refusal cannot be cured by retrying.
func TestGatewayAuthRefusalReachesTheRealProgramEngineWithoutRetry(t *testing.T) {
	for _, status := range []int{401, 403} {
		seat := &gatewayAuthSeat{status: status}
		server, err := modelapi.Open(modelapi.Config{
			TaskDir: t.TempDir(), AuthKeySource: func(string) string { return "the key saved in your profile" },
			CompleterFor: func(string) modelapi.Completer { return seat },
		})
		if err != nil {
			t.Fatal(err)
		}
		backend := &modelAPIBackend{api: server.API()}
		_, failure := backend.Run(context.Background(), retryTurn())
		_ = server.Close()
		var turnFailure *modelTurnError
		if !errors.As(failure, &turnFailure) || turnFailure.statusCode == nil || *turnFailure.statusCode != 502 {
			t.Fatalf("upstream %d lost its gateway response: %v", status, failure)
		}
		if info, retry := transientTurnError(failure); retry {
			t.Fatalf("upstream %d reached the real engine as a retry: %+v", status, info)
		}
		if !strings.Contains(failure.Error(), "the key saved in your profile") {
			t.Errorf("upstream %d lost the key source: %v", status, failure)
		}
		if calls := seat.calls.Load(); calls != 1 {
			t.Errorf("upstream %d made %d calls; want one", status, calls)
		}
	}
}

// The whole pipeline must stop spending after refusal while still finalizing locally.
func TestGatewayAuthRefusalStopsTheCompleteSeniorDevPipeline(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			workspace, base := guardWorkspace(t)
			seat := &gatewayAuthSeat{status: status}
			server, err := modelapi.Open(modelapi.Config{TaskDir: t.TempDir(), AuthKeySource: func(string) string { return "the key saved in your profile" }, CompleterFor: func(string) modelapi.Completer { return seat }})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			runner := newPipeline(cliArgs{High: "test/model"}, workspace, pipelineDeps{Backend: &modelAPIBackend{api: server.API()}, Events: newEventWriter(io.Discard), Notes: io.Discard})
			defer runner.runtime.Close()
			outcome, failure := runner.runSolo(context.Background(), "Add a line.", base)
			if failure == nil {
				t.Fatal("the auth refusal was lost")
			}
			if seat.calls.Load() != 1 {
				t.Errorf("upstream %d made %d calls, want one", status, seat.calls.Load())
			}
			if outcome.LandingTurns != 0 {
				t.Errorf("auth refusal started %d model landing turns", outcome.LandingTurns)
			}
			if outcome.TerminalReason == "" || outcome.FinalTree == "" {
				t.Errorf("local finalization did not run: %+v", outcome)
			}
		})
	}
}

// A work-window deadline racing with refusal cannot revive the refused key.
func TestAuthenticationRefusalCannotEnterTheLandingWindow(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			runner, state, _, _ := soloPipeline(t)
			// The work context is expired while the deterministic budget clock still
			// leaves a landing allowance. No real sleep is needed to exercise the race.
			instant := time.Unix(1, 0)
			runner.wallStart = instant
			runner.now = func() time.Time { return instant }
			allowance := float64(4000)
			runner.budget.MaxWallMS = &allowance
			calls := 0
			runner.turnForTest = func(context.Context, string, string) (turnResult, error) {
				calls++
				return turnResult{}, &provider.APIError{Status: status, Message: "account refused"}
			}
			var outcome soloOutcome
			failure := runner.soloConverse(context.Background(), "Add a line.", state, &outcome)
			if !authenticationTurnError(failure) || calls != 1 || outcome.LandingTurns != 0 {
				t.Fatalf("auth refusal crossed into landing: calls=%d turns=%d error=%v", calls, outcome.LandingTurns, failure)
			}
		})
	}
}
