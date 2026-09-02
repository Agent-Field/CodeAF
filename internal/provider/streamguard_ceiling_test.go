package provider

import (
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// TestEveryTransportBoundSitsAboveTheCeilingTheControllerActsAt is Decision 10's
// ORDER, written as arithmetic over every role the table knows.
//
// The transport's own bounds are the LAST RESORT: they exist for the one case
// where no action is possible at all, and the waiting controller — which hedges,
// asks or reports at the role's ceiling — must always get there first. A
// transport bound underneath that ceiling inverts the two, and the inversion is
// not a near-miss: the controller's act is a second request beside a stream that
// is still open, while the transport's is a torn one plus a whole retry that
// pays the prompt again. The cheap rescue has to come first, every time.
//
// It reads the roles out of [lane.Roles] rather than spelling them again here,
// so a role added to that table tomorrow is held to this law without anybody
// remembering to come back.
func TestEveryTransportBoundSitsAboveTheCeilingTheControllerActsAt(t *testing.T) {
	// The rates are the whole range [gapFor] can be handed, absurdities
	// included, because the derivation is the half that broke the order: a lane
	// fast enough drove the mid-stream bound to the lag law's fifteen seconds,
	// underneath the thirty- and sixty-second ceilings of every unattended role.
	rates := []struct {
		what string
		rate float64
	}{
		{"a lane this process has never rated", 0},
		{"a lane crawling at a token every ten seconds", 0.1},
		{"a lane absurdly faster than any endpoint has ever served", 1e9},
	}
	roles := lanes.Roles()
	if len(roles) == 0 {
		t.Fatal("the roles table is empty, so this law was proved of nothing")
	}
	for _, role := range roles {
		t.Run(roleName(role), func(t *testing.T) {
			bounds, ceiling := boundsFor(role), role.Ceiling()
			// A ROLE THAT PRODUCES NO TOKEN STREAM IS OUTSIDE THE TOKEN
			// CONTROLLER AND NOT OUTSIDE PATIENCE (§F of
			// docs/design/waiting/DESIGN.md). Media has no first token, no gap
			// between tokens and no rate to be surprised by, so there is no
			// ceiling for its bounds to clear — what is asked of it here is only
			// that it still HAS all three, because a role that fell through this
			// derivation with zeroes would be a request cut the instant it
			// opened.
			if !role.Facts().Streams {
				for _, bound := range namedBounds(bounds, gapFor(0)) {
					if bound.figure <= 0 {
						t.Fatalf("%s produces no token stream and still lost %s (%s) — it is outside the controller, not outside the guard",
							role, bound.what, bound.figure)
					}
				}
				return
			}
			for _, rate := range rates {
				for _, bound := range namedBounds(bounds, gapFor(rate.rate)) {
					if bound.figure > ceiling {
						continue
					}
					t.Errorf("%s: %s is %s and the controller's ceiling is %s, on %s.\n"+
						"The transport would cut this stream %s before the controller reached the ceiling it would have rescued it at — "+
						"and a cut is a torn request and a whole retry, where the controller's act is a second request beside a stream that is still open.",
						role, bound.what, bound.figure, ceiling, rate.what, ceiling-bound.figure)
				}
			}
		})
	}
}

// namedBounds is the three silence bounds one request is guarded by, each with
// the words the failure above names it in. The mid-stream one is given as the
// figure a request actually ends up on — the lane's own derivation, held inside
// the band the role leaves it ([stallWatch.regap] does exactly this).
func namedBounds(bounds stallBounds, derived time.Duration) []struct {
	what   string
	figure time.Duration
} {
	return []struct {
		what   string
		figure time.Duration
	}{
		{"the first-delta bound", bounds.first},
		{"the mid-stream gap bound", bounds.narrow(derived)},
		{"the buffered-quiet bound", bounds.buffered},
	}
}

// roleName is what a subtest of the law above is called. [lane.RoleUnknown] is
// spelled with the empty string, which would give a subtest no name at all.
func roleName(role lanes.Role) string {
	if role == lanes.RoleUnknown {
		return "unknown"
	}
	return string(role)
}
