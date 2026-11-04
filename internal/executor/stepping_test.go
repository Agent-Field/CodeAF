package executor

import (
	"context"
	"testing"
)

type stepProbeSeat struct {
	Stance
	settled []context.Context
}

func (s *stepProbeSeat) Settle(ctx context.Context) { s.settled = append(s.settled, ctx) }

func TestSavePausesStepMarkerFlowsThroughSeatWrappers(t *testing.T) {
	for _, name := range []string{"plain", "gated", "watched", "gated watched", "watched gated", "environment"} {
		t.Run(name, func(t *testing.T) {
			probe := &stepProbeSeat{}
			var seat Seat = probe
			gate := func() error { return nil }
			switch name {
			case "gated":
				seat = Gated(seat, gate)
			case "watched":
				seat = Watching(seat, &sightings{})
			case "gated watched":
				seat = Gated(Watching(seat, &sightings{}), gate)
			case "watched gated":
				seat = Watching(Gated(seat, gate), &sightings{})
			case "environment":
				seat = WithEnvironment(Watching(Gated(seat, gate), &sightings{}), []string{"CODEAF_AGENT_DID=test"})
			}
			type carriedKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), carriedKey{}, "carried"))
			defer cancel()
			SettleStep(ctx, seat)
			Settle(ctx, seat)
			if err := Drain(ctx, seat); err != nil {
				t.Fatal(err)
			}
			if len(probe.settled) != 3 {
				t.Fatalf("wrapper lost a settle: %d", len(probe.settled))
			}
			for n, seen := range probe.settled {
				if Stepping(seen) != (n == 0) || seen.Value(carriedKey{}) != "carried" {
					t.Fatalf("settle %d lost its kind or context value", n)
				}
			}
			cancel()
			if Stepping(ctx) || probe.settled[0].Err() != context.Canceled {
				t.Fatal("step marker escaped its settle or lost cancellation")
			}
		})
	}
}
