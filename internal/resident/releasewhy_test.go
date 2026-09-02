package resident

import (
	"strings"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
)

// THE CUT AND THE JOIN ARE ONE FACT, SO THEY ARE TESTED AGAINST EACH OTHER.
//
// The headless ↻ line says how many turns it picked up in its own words and then
// names the bound that fired, which it reads off the release reason — and the
// reason ends with a clause saying that same count. If the cut and the clause
// ever spell the seam differently, nothing goes red: the line simply starts
// carrying the number twice, three words apart, which is exactly the shape of
// the defect this whole sentence exists to avoid. So every reason this package
// composes is asked to survive the round trip.
func TestTheWhyOfAReleaseKeepsTheBoundAndDropsTheTurnCount(t *testing.T) {
	ranOut := ExecResult{
		Stop: executor.StopBudget,
		Meter: executor.Meter{
			Name: executor.MeterCost, Reached: 178086, Allowed: 176834,
			Unit: "tokens of billed work",
		},
	}
	for _, probe := range []struct {
		name   string
		reason string
		want   string
	}{
		{
			name:   "a leaf that ran out of its room",
			reason: outOfRoomClaimReason(ranOut, 3),
			want:   "it was still working when it ran out of its token budget (cost: 178086 of 176834 tokens of billed work)",
		},
		{
			name:   "one recorded turn, spelled singular",
			reason: outOfRoomClaimReason(ranOut, 1),
			want:   "it was still working when it ran out of its token budget (cost: 178086 of 176834 tokens of billed work)",
		},
		{
			name:   "a claim taken back from a worker that stopped answering",
			reason: exhaustedClaimReason(17*time.Minute, 45),
			want:   "the worker did not come back within 17m0s and was stopped",
		},
		{
			// A journal written before the two endings shared one clause. The
			// store outlives the binary, so the cut has to read this spelling
			// or an old --db replays with the turn count on the line twice.
			name:   "a reason journalled before the clause was joined",
			reason: "the worker did not come back within 17m0s and was stopped — 45 turns of its work is recorded, and the next one carries on from there",
			want:   "the worker did not come back within 17m0s and was stopped",
		},
		{
			name:   "a reason with no such clause is the why entire",
			reason: "no sign of life for 24m3s",
			want:   "no sign of life for 24m3s",
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			why := ReleaseWhy(probe.reason)
			if why != probe.want {
				t.Errorf("the why reads\n\t%q\nwant\n\t%q", why, probe.want)
			}
			if strings.Contains(why, "of its work is recorded") {
				t.Errorf("the why still carries the count clause: %q", why)
			}
		})
	}
}
