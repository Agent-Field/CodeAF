package lane

import (
	"context"
	"sync"
	"testing"
	"time"
)

// noteLedger records what it was told and believes nothing, which is all a
// probe test needs from a ledger: the question is what was written, never what
// was concluded.
type noteLedger struct {
	mu        sync.Mutex
	sightings []Sighting
	outcomes  []Outcome
	rows      []Row
}

func (l *noteLedger) Note(sighting Sighting) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sightings = append(l.sightings, sighting)
}

func (l *noteLedger) NoteOutcome(outcome Outcome) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.outcomes = append(l.outcomes, outcome)
}

func (l *noteLedger) Prime(row Row, _ float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rows = append(l.rows, row)
}

func (l *noteLedger) Belief(ID) (Belief, bool) { return Belief{}, false }

func (l *noteLedger) Beliefs(string) []Belief { return nil }

func (l *noteLedger) noted() []Sighting {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Sighting(nil), l.sightings...)
}

// sentProbes is the transport a probe test injects: it records what it was
// asked to measure and answers at once.
type sentProbes struct {
	mu    sync.Mutex
	asked [][2]string
	sent  chan struct{}
}

func (s *sentProbes) send(_ context.Context, model, lane string) (time.Duration, error) {
	s.mu.Lock()
	s.asked = append(s.asked, [2]string{model, lane})
	s.mu.Unlock()
	s.sent <- struct{}{}
	return 250 * time.Millisecond, nil
}

func (s *sentProbes) lanes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.asked))
	for _, one := range s.asked {
		names = append(names, one[1])
	}
	return names
}

// await waits for n probes to have been sent, and fails rather than hanging.
func (s *sentProbes) await(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-s.sent:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d probes were sent, wanted %d", len(s.lanes()), n)
		}
	}
}

// quiet asserts that nothing else went out. A probe is fire-and-forget, so the
// only honest way to say "nothing happened" is to give it a moment in which it
// could have.
func (s *sentProbes) quiet(t *testing.T, was int) {
	t.Helper()
	select {
	case <-s.sent:
		t.Fatalf("a probe went out that the budget should have refused: %v", s.lanes())
	case <-time.After(50 * time.Millisecond):
	}
	if got := len(s.lanes()); got != was {
		t.Fatalf("%d probes were sent, want %d", got, was)
	}
}

func TestAProbePairMeasuresTwoLanesAndLandsInTheLedger(t *testing.T) {
	transport := &sentProbes{sent: make(chan struct{}, 8)}
	ledger := &noteLedger{}
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	prober := NewProber(ProberConfig{
		Send:   transport.send,
		Ledger: ledger,
		Now:    func() time.Time { return now },
	})

	// Three lanes are offered and two are measured: the third would be paying
	// to time a lane the choice was never going to make.
	prober.Probe(context.Background(), "vendor/model", []string{"A", "B", "C"})
	transport.await(t, 2)
	transport.quiet(t, 2)

	// The pair goes out on two goroutines, so which of them was recorded first
	// is not a fact about anything: what is asserted is WHICH lanes were
	// measured and that the third was not.
	probed := map[string]bool{}
	for _, lane := range transport.lanes() {
		probed[lane] = true
	}
	if len(probed) != 2 || !probed["A"] || !probed["B"] {
		t.Fatalf("probed %v, want the head two of the frontier", transport.lanes())
	}
	noted := ledger.noted()
	if len(noted) != 2 {
		t.Fatalf("the ledger holds %d sightings, want two", len(noted))
	}
	for _, sighting := range noted {
		if !sighting.Probe {
			t.Fatalf("sighting %+v is not marked as a probe: the ledger would weigh it as an ordinary answer", sighting)
		}
		if sighting.TTFT != 250*time.Millisecond {
			t.Fatalf("sighting TTFT = %s, want what the transport measured", sighting.TTFT)
		}
		if sighting.At.IsZero() {
			t.Fatalf("sighting %+v carries no moment, and a ledger refuses one that does not", sighting)
		}
		if sighting.Tokens != 0 || sighting.Gen != 0 {
			t.Fatalf("sighting %+v rates a one-token answer; a probe times the handshake and nothing else", sighting)
		}
	}
}

func TestASecondPairInsideTheWindowIsRefused(t *testing.T) {
	transport := &sentProbes{sent: make(chan struct{}, 8)}
	moment := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	prober := NewProber(ProberConfig{
		Send:   transport.send,
		Ledger: &noteLedger{},
		Now:    func() time.Time { return moment },
	})

	prober.Probe(context.Background(), "vendor/model", []string{"A", "B"})
	transport.await(t, 2)

	moment = moment.Add(19 * time.Second)
	prober.Probe(context.Background(), "vendor/model", []string{"A", "B"})
	transport.quiet(t, 2)

	// Another model is another person's typing and has its own budget.
	prober.Probe(context.Background(), "vendor/other", []string{"A", "B"})
	transport.await(t, 2)

	// And past the window the first model may be measured again.
	moment = moment.Add(2 * time.Second)
	prober.Probe(context.Background(), "vendor/model", []string{"A", "B"})
	transport.await(t, 2)
}

func TestAClosedGateStopsEveryProbe(t *testing.T) {
	transport := &sentProbes{sent: make(chan struct{}, 8)}
	prober := NewProber(ProberConfig{
		Send:   transport.send,
		Ledger: &noteLedger{},
		Gate:   func(string) bool { return false },
	})
	prober.Probe(context.Background(), "vendor/model", []string{"A", "B"})
	transport.quiet(t, 0)
}

func TestAProberWithNoTransportSendsNothing(t *testing.T) {
	prober := NewProber(ProberConfig{Ledger: &noteLedger{}})
	// The whole contract: it returns, and nothing happens.
	prober.Probe(context.Background(), "vendor/model", []string{"A", "B"})
}

func TestAFailedProbeTeachesTheLedgerNothing(t *testing.T) {
	ledger := &noteLedger{}
	sent := make(chan struct{}, 4)
	prober := NewProber(ProberConfig{
		Ledger: ledger,
		Send: func(context.Context, string, string) (time.Duration, error) {
			sent <- struct{}{}
			return 0, context.DeadlineExceeded
		},
	})
	prober.Probe(context.Background(), "vendor/model", []string{"A"})
	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("the probe was never sent")
	}
	time.Sleep(20 * time.Millisecond)
	if noted := ledger.noted(); len(noted) != 0 {
		t.Fatalf("a refusal was written as a measurement: %+v", noted)
	}
}
