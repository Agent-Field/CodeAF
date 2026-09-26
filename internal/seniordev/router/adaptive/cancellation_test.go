//go:build !windows

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCanceledRouteReleasesWithoutHealthCredit(t *testing.T) {
	pinRuntime(t)
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{HighModels: []ModelCandidate{cand("p/model")}})
	choice := mustPick(t, router, "coder", ModelTierHigh)
	st := router.statsFor("coder", choice.Candidate)
	before := *st
	router.RegisterCanceled(choice)
	before.Inflight--
	if *st != before {
		t.Fatalf("cancellation changed health: before=%+v after=%+v", before, *st)
	}
	result := router.TryPick("coder", ModelTierHigh)
	if !result.Ok {
		t.Fatalf("canceled call created cooldown: %+v", result)
	}
	router.RegisterCanceled(result.Choice)
}

func TestPickContextStopsDuringRealProviderCooldown(t *testing.T) {
	var events []AdaptiveRouteEvent
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
		HighModels: []ModelCandidate{cand("p/model")},
		OnEvent:    func(e AdaptiveRouteEvent) { events = append(events, e) },
	})
	choice := mustPick(t, router, "coder", ModelTierHigh)
	router.Register(choice, 1, 0, errors.New("SSE read timed out"))
	before := *router.statsFor("coder", choice.Candidate)
	cause := errors.New("wall-clock budget exhausted")
	ctx, cancel := context.WithTimeoutCause(context.Background(), 20*time.Millisecond, cause)
	defer cancel()
	started := time.Now()
	_, err := router.PickContext(ctx, "coder", ModelTierHigh)
	if !errors.Is(err, cause) || time.Since(started) > time.Second {
		t.Fatalf("pick cancellation err=%v elapsed=%s", err, time.Since(started))
	}
	if got := *router.statsFor("coder", choice.Candidate); got != before {
		t.Fatalf("canceled wait modified prior provider health: %+v -> %+v", before, got)
	}
	if len(events) != 2 || events[1].Reason != "caller-canceled-pick" {
		t.Fatalf("events=%+v", events)
	}
}
