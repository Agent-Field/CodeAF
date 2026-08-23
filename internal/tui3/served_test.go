package tui3

import (
	"strings"
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

// The model segment names the model AND who actually answered as it: one id is
// fanned over many endpoints that write at very different speeds, and the id
// alone names a decision the session did not make.
func TestModelSegmentNamesWhoServedAndHowFast(t *testing.T) {
	at := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	a := newTestApp(&fakeAgent{model: "deepseek/deepseek-v4-flash"})
	a.clock = func() time.Time { return at }
	a.model = "deepseek/deepseek-v4-flash"
	a.title = "porting the parser"
	pinSighting(t, provider.Sighting{
		Model: "deepseek/deepseek-v4-flash", Provider: "quicksilver", Rate: 92, At: at.Add(-time.Second),
	}, true)

	// The rate is a claim about NOW, so it rides only while a turn runs; at
	// rest the attribution stands alone and the figure is cleared.
	a.state = stateWorking
	want := "porting the parser · deepseek-v4-flash · via quicksilver · 92 tok/s"
	if got := a.identity(); got != want {
		t.Fatalf("identity() = %q, want %q", got, want)
	}
	a.state = stateIdle
	want = "porting the parser · deepseek-v4-flash · via quicksilver"
	if got := a.identity(); got != want {
		t.Fatalf("an idle identity() = %q, want the rate cleared: %q", got, want)
	}
}

// The rider is a fact or it is nothing. Each case below is a reason there is no
// fact to state.
func TestModelSegmentStaysQuietWithoutAServedFact(t *testing.T) {
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
			a.title = "porting the parser"
			pinSighting(t, test.sighting, test.known)
			want := "porting the parser · deepseek-v4-flash"
			if got := a.identity(); got != want {
				t.Fatalf("identity() = %q, want %q — %s", got, want, test.why)
			}
		})
	}
}

// An endpoint that has been named but not yet rated still says who served: the
// name is the fact a person acts on, the rate is the one they watch.
func TestModelSegmentNamesTheServerEvenUnrated(t *testing.T) {
	at := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	a := newTestApp(&fakeAgent{model: "deepseek/deepseek-v4-flash"})
	a.clock = func() time.Time { return at }
	a.model = "deepseek/deepseek-v4-flash"
	a.title = "porting the parser"
	pinSighting(t, provider.Sighting{Provider: "quicksilver", At: at}, true)

	got := a.identity()
	if !strings.HasSuffix(got, "· via quicksilver") {
		t.Fatalf("identity() = %q, want it to end at the server's name with no rate", got)
	}
}
