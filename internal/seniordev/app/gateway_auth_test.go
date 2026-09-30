//go:build !windows

package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

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
			TaskDir: t.TempDir(), AuthKeySource: "the key saved in your profile",
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
