package session

import (
	"strings"
	"testing"
)

// THE LAW HERE IS ABOUT WORK AND NOT ABOUT TIME. An allocation count is the
// same number on a loaded laptop and on idle CI, so red always means somebody
// changed something. PERF.md states the doctrine for the whole repository and
// lists where each of these laws is pinned; a number below that has to move is
// a change to a law, and it belongs in that file in the same commit.

// THE BACKLOG FOLD IS AMORTIZED CONSTANT PER DELTA.
//
// [eventHub] keeps this turn's events so that a surface arriving mid-turn can be
// shown what it missed, and a run of text deltas folds into one entry. The
// obvious way to fold is `backlog[last].Text += delta`, and it is QUADRATIC:
// each delta allocates a new string holding the whole answer so far, under the
// hub's lock, while the provider is still writing. Over the twenty thousand
// deltas of a long reply that is twenty thousand allocations and, at eight bytes
// a delta, about 1.6 GB of copying — for a string nobody may ever read, since
// the backlog is dropped whole when the turn ends.
//
// [eventHub.folding] is a strings.Builder instead, settled into the entry at
// every point the entry can actually be read ([eventHub.foldedLocked]). The
// builder doubles, so the cost of a whole run is logarithmic in its length: the
// measurement below is 19 allocations for two thousand deltas and 27 for twenty
// thousand.
//
// attach_test.go pins that the fold spells the same string as the concatenation
// would have. This pins that it does not spell it the expensive way — and the
// two assertions are deliberately different in kind. The RATIO is the law: ten
// times the deltas for less than twice the allocations is what "amortized
// constant" means, and no re-append can satisfy it. The CEILING is the backstop,
// set where a per-delta allocation cannot hide — twenty thousand against sixty
// four.
func TestTheBacklogFoldIsAmortizedConstantPerDelta(t *testing.T) {
	// Eight bytes is about a word, which is the unit a model streams in.
	delta := strings.Repeat("x", 8)
	fold := func(deltas int) float64 {
		return testing.AllocsPerRun(3, func() {
			hub := newEventHub()
			for index := 0; index < deltas; index++ {
				hub.send(Event{Kind: EventTextDelta, Text: delta})
			}
			hub.close()
		})
	}

	const short, long = 2_000, 20_000
	small, large := fold(short), fold(long)

	if large >= 2*small {
		t.Fatalf("folding %d deltas costs %.0f allocations against %.0f for %d — ten times the reply for "+
			"%.1f times the cost, and the fold is meant to be amortized constant per delta. A fold that grows "+
			"with the answer is `backlog[last].Text += delta` by another name, and it copies the whole reply "+
			"so far once per word, inside the hub's lock, while the model is still writing (agent.go, "+
			"eventHub.folding).", long, large, small, short, large/small)
	}
	if large > 64 {
		t.Fatalf("folding %d deltas costs %.0f allocations, and the ceiling is 64. One allocation per delta "+
			"would be %d of them and about 1.6 GB of copying; the builder's doubling is meant to make this "+
			"logarithmic in the length of the reply.", long, large, long)
	}
}
