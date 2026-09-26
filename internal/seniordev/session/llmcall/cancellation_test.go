//go:build !windows

package llmcall

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

func TestWorkDeadlineLeavesLandingRouteImmediatelyUsable(t *testing.T) {
	now := 1000000.0
	t.Cleanup(adaptive.SetClockForTesting(func() float64 { return now }))
	sleeps := 0
	t.Cleanup(adaptive.SetSleeperForTesting(func(ms float64, signal *adaptive.AbortSignal) { sleeps++; now += ms }))
	var events []adaptive.AdaptiveRouteEvent
	router := adaptive.NewAdaptiveModelRouter(adaptive.AdaptiveRouterConfig{
		HighModels: []adaptive.ModelCandidate{{ID: "openrouter/moonshotai/kimi-k3"}},
		OnEvent:    func(e adaptive.AdaptiveRouteEvent) { events = append(events, e) },
	})
	work, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	workCause := errors.New("work budget exhausted")
	fetches := 0
	service := &Service{Router: router, Clients: ClientFactoryFunc(func(_ context.Context, _ Model, choice *adaptive.RouteChoice, r *adaptive.AdaptiveModelRouter) (StreamClient, error) {
		return concreteClient{client: &orclient.Client{BaseURL: "http://model-api.invalid/v1", Router: r, RouteChoice: choice,
			Fetcher: func(request *http.Request) (*http.Response, error) {
				fetches++
				if fetches == 1 {
					cancel(workCause)
					return nil, request.Context().Err()
				}
				if request.Context().Err() != nil {
					t.Fatal("landing inherited expired work context")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}, nil
			}}}, nil
	})}
	input := StreamInput{Model: Model{ProviderID: "openrouter", ID: "moonshotai/kimi-k3"}, Agent: Agent{Name: "coder"}}
	if _, err := service.Stream(work, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("work err=%v", err)
	}
	if len(events) != 1 || events[0].Reason != "caller-canceled-request" || events[0].Attempts != 0 || events[0].Successes != 0 || events[0].Failures != 0 {
		t.Fatalf("caller cancellation credited/penalized provider: %+v", events)
	}
	stream, err := service.Stream(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err = stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}
	if sleeps != 0 || fetches != 2 || now != 1000000 {
		t.Fatalf("landing lost time: sleeps=%d fetches=%d elapsed=%g", sleeps, fetches, now-1000000)
	}
	if len(events) != 2 || events[1].Attempts != 1 || events[1].Successes != 1 || events[1].Failures != 0 {
		t.Fatalf("normal accounting changed: %+v", events)
	}
}
