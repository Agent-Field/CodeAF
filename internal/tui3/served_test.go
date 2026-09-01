package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// pinSighting states what the adapter's ledger would answer for the duration of
// one test, and puts it back afterwards.
func pinSighting(t *testing.T, sighting provider.Sighting, known bool) {
	t.Helper()
	previous := servedSighting
	servedSighting = func(string) (provider.Sighting, bool) { return sighting, known }
	t.Cleanup(func() { servedSighting = previous })
}

// THE RIDER LEFT THE STATUS ROW AND KEPT ITS NAME. ISSUE-126 folded the row to
// two clusters and moved the attribution to the top bar's model chip, where it
// is the same fact in the same words: one id is fanned over many endpoints that
// write at very different speeds, and the id alone names a decision the session
// did not make.
func TestServedRiderNamesWhoServedAndHowFast(t *testing.T) {
	at := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	a := newTestApp(&fakeAgent{model: "deepseek/deepseek-v4-flash"})
	a.clock = func() time.Time { return at }
	a.model = "deepseek/deepseek-v4-flash"
	pinSighting(t, provider.Sighting{
		Model: "deepseek/deepseek-v4-flash", Provider: "quicksilver", Rate: 92, At: at.Add(-time.Second),
	}, true)

	// The rate is a claim about NOW, so it rides only while a turn runs; at
	// rest the attribution stands alone and the figure is cleared.
	a.state = stateWorking
	if got := a.servedRider(); got != " · via quicksilver · 92 tok/s" {
		t.Fatalf("servedRider() = %q, want the server and its live rate", got)
	}
	a.state = stateIdle
	if got := a.servedRider(); got != " · via quicksilver" {
		t.Fatalf("an idle servedRider() = %q, want the rate cleared", got)
	}
}

// The rider is a fact or it is nothing. Each case below is a reason there is no
// fact to state.
func TestServedRiderStaysQuietWithoutAServedFact(t *testing.T) {
	at := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		sighting provider.Sighting
		known    bool
		why      string
	}{
		{
			name: "nothing measured", known: false,
			why: "no answer has been timed yet",
		},
		{
			name:  "unnamed server",
			known: true, sighting: provider.Sighting{Rate: 92, At: at},
			why: "the endpoint did not say who it was",
		},
		{
			name:  "the vendor is the model",
			known: true, sighting: provider.Sighting{Provider: "DeepSeek", Rate: 92, At: at},
			why: "'via deepseek' beside deepseek-v4-flash is a cell of chrome for a word already on the line",
		},
		{
			name:  "stale",
			known: true, sighting: provider.Sighting{Provider: "quicksilver", Rate: 92, At: at.Add(-servedWindow - time.Minute)},
			why: "a rate from a conversation that has since gone to sleep is not a rate",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "deepseek/deepseek-v4-flash"})
			a.clock = func() time.Time { return at }
			a.model = "deepseek/deepseek-v4-flash"
			pinSighting(t, test.sighting, test.known)
			if got := a.servedRider(); got != "" {
				t.Fatalf("servedRider() = %q, want nothing — %s", got, test.why)
			}
		})
	}
}

// An endpoint that has been named but not yet rated still says who served: the
// name is the fact a person acts on, the rate is the one they watch.
func TestServedRiderNamesTheServerEvenUnrated(t *testing.T) {
	at := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	a := newTestApp(&fakeAgent{model: "deepseek/deepseek-v4-flash"})
	a.clock = func() time.Time { return at }
	a.model = "deepseek/deepseek-v4-flash"
	pinSighting(t, provider.Sighting{Provider: "quicksilver", At: at}, true)

	if got := a.servedRider(); got != " · via quicksilver" {
		t.Fatalf("servedRider() = %q, want the server's name with no rate", got)
	}
}

// servedRiderAt is the same rider in the widest spelling that fits the
// columns the row has left for it: the segment degrades by what its parts are
// worth — the rate goes first, then the whole rider — and never by where the
// row happens to end.
func TestServedRiderAtDegradesByWhatItsPartsAreWorth(t *testing.T) {
	at := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	a := newTestApp(&fakeAgent{model: "deepseek/deepseek-v4-flash"})
	a.clock = func() time.Time { return at }
	a.model = "deepseek/deepseek-v4-flash"
	a.state = stateWorking
	pinSighting(t, provider.Sighting{
		Model: "deepseek/deepseek-v4-flash", Provider: "quicksilver", Rate: 92, At: at,
	}, true)

	// Unbounded, the rider is the full sentence.
	if got := a.servedRiderAt(-1); got != " · via quicksilver · 92 tok/s" {
		t.Fatalf("servedRiderAt(-1) = %q, want the full rider", got)
	}
	// A width that cannot hold the rate still holds the name.
	if got := a.servedRiderAt(len(" · via quicksilver")); got != " · via quicksilver" {
		t.Fatalf("servedRiderAt(name-width) = %q, want the rate given up first", got)
	}
	// And a width that cannot hold the name holds nothing, rather than a clip
	// of one.
	if got := a.servedRiderAt(4); got != "" {
		t.Fatalf("servedRiderAt(4) = %q, want nothing rather than a clipped word", got)
	}
}
