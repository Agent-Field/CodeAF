package executor

import (
	"context"
	"testing"
)

type settling struct {
	Seat
	n int
}

func (s *settling) Settle(context.Context) { s.n++ }

// Settle reaches the recording seat through the seats wrapped around it.
func TestSettleReachesTheSeatBeneathItsWrappers(t *testing.T) {
	base := &settling{}
	Settle(context.Background(), Gated(Watching(base, nil), func() error { return nil }))
	if base.n != 1 {
		t.Fatalf("settled %d times, want 1", base.n)
	}
	Settle(context.Background(), Stance{}) // a seat that records nothing is left alone
}
