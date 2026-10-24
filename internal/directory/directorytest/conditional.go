package directorytest

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// RunConditionalTakes proves C7 against either relay through its real HTTP
// client, without the long clock-driven lease-liveness cases.
func RunConditionalTakes(t *testing.T, factory RigFactory) {
	for name, run := range map[string]func(*testing.T, env){
		"OneWinner":      conditionalForcedTakeOneWins,
		"HolderAndFence": conditionalTakeChecksHolderAndFence,
	} {
		t.Run(name, func(t *testing.T) { run(t, envOf(factory(t))) })
	}
}

// conditionalForcedTakeOneWins proves C7: two people who saw A's live lease
// cannot both win, even if the loser asks by force after the winner finishes.
func conditionalForcedTakeRace(t *testing.T, e env) {
	v := start(t, e)
	seen := &directory.LeaseExpectation{Device: v.Cell.Lease.Device, Fence: v.Cell.Lease.Fence}
	win, err := e.as(devB).Acquire(ctx, cell, directory.AcquireOpts{Force: true, Expect: seen})
	must(t, err)
	_, err = e.as(devA).Acquire(ctx, cell, directory.AcquireOpts{Force: true, Expect: seen})
	wantErr(t, err, directory.ErrLeaseHeld)
	if now := get(t, e); now.Lease != win.Cell.Lease {
		t.Fatalf("the losing continuation changed the winner's lease: %+v", now.Lease)
	}
}

// Legacy stores still prove their historical blind-force behavior until P11
// freezes them. The consensus suite below proves R25's safe normalization.
func conditionalForcedTakeOneWins(t *testing.T, e env) {
	conditionalForcedTakeRace(t, e)
	_, err := e.as(devA).Acquire(ctx, cell, directory.AcquireOpts{Force: true})
	must(t, err)
}

// RunConsensus uses every existing verb case, with the R25 compatibility
// contract at the one place where blind legacy force deliberately changes.
func RunConsensus(t *testing.T, factory Factory) {
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			clock := NewFakeClock()
			e := envOf(Rig{Clock: clock, Devices: factory(t, clock)})
			if name == "C7ConditionalForcedTakeOneWins" {
				conditionalForcedTakeRace(t, e)
				_, err := e.as(devA).Acquire(ctx, cell, directory.AcquireOpts{Force: true})
				wantErr(t, err, directory.ErrLeaseHeld)
			} else {
				fn(t, e)
			}
		})
	}
}

// conditionalTakeChecksHolderAndFence proves C7: neither a matching holder
// alone nor a matching fence alone is the lease the person chose.
func conditionalTakeChecksHolderAndFence(t *testing.T, e env) {
	v := start(t, e)
	lease := v.Cell.Lease
	for _, seen := range []directory.LeaseExpectation{
		{Device: e.id(devB), Fence: lease.Fence},
		{Device: lease.Device, Fence: lease.Fence + 1},
	} {
		_, err := e.as(devB).Acquire(ctx, cell, directory.AcquireOpts{Force: true, Expect: &seen})
		wantErr(t, err, directory.ErrLeaseHeld)
	}
	if got := get(t, e).Lease; got != lease {
		t.Fatalf("a mismatched continuation changed the lease: %+v", got)
	}
}
