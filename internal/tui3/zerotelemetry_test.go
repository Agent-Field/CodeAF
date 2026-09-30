package tui3

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestLiveRateOmitsRatesThatWouldDisplayZero(t *testing.T) {
	now := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	answerArriving(a)
	for _, phase := range []provider.Phase{provider.PhaseThinking, provider.PhaseWriting} {
		for _, rate := range []float64{0, 0.01, 0.99, 1, 1.99, 38} {
			PostPhaseNews(PhaseNews{Phase: phase, Since: now.Add(-time.Second), Rate: rate,
				Model: phaseModel, Role: lane.RoleTalk, At: now})
			want := ""
			if rate >= 1 {
				want = tokenWord(int(rate)) + " tok/s"
			}
			if got := a.liveRiderAt(-1); got != want {
				t.Fatalf("phase %v rate %v drew %q, want %q", phase, rate, got, want)
			}
		}
	}
}

func TestPhaseRateOmitsRatesThatWouldDisplayZero(t *testing.T) {
	now := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	for _, rate := range []float64{0, 0.01, 0.49, 0.5, 0.99, 1, 1.99, 38} {
		want := ""
		if n := int(math.Round(rate)); n > 0 {
			want = itoa(n) + " t/s"
		}
		if got := laneRateWord(rate); got != want {
			t.Fatalf("phase rate %v drew %q, want %q", rate, got, want)
		}
		if tight := laneRateTight(rate); (want == "") != (tight == "") || tight == "0t/s" {
			t.Fatalf("compact phase rate %v drew %q", rate, tight)
		}
		fields := phaseFields(PhaseNews{Phase: provider.PhaseThinking, Since: now.Add(-time.Second), Rate: rate}, now)
		line := rowLed(fields, rowUnbounded)
		if strings.Contains(line, "0 t/s") || !strings.Contains(line, "thinking") {
			t.Fatalf("phase with rate %v lost its label or drew a zero: %q", rate, line)
		}
	}
}

func TestFallbackLiveRateOmitsRatesThatWouldDisplayZero(t *testing.T) {
	a, _, now := hudApp(t)
	a.model, a.state = phaseModel, stateWorking
	pinSighting(t, provider.Sighting{Model: phaseModel, Provider: "proof-machine", Rate: 0.01, At: *now}, true)
	if line := a.servedRider(); strings.Contains(line, "0 tok/s") || !strings.Contains(line, "via proof-machine") {
		t.Fatalf("fallback lost its attribution or drew a zero rate: %q", line)
	}
}

func TestThinkingOmitsZeroTokenCountsWithoutLosingTheDisclosure(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	for _, body := range []string{"", "T", "The", "Then", "thinking"} {
		for _, settled := range []bool{false, true} {
			e := entry{kind: entryThinking, text: body, settled: settled}
			head := plain(a.thoughtRows(&e, 100, false)[0])
			if !strings.Contains(head, "ctrl+e") || strings.Contains(head, "0 tok") || strings.Contains(head, " ·  · ") {
				t.Fatalf("body %q settled %v has a broken label: %q", body, settled, head)
			}
			if len(body) >= 4 && !strings.Contains(head, itoa(len(body)/4)+" tok") {
				t.Fatalf("positive thought count disappeared: %q", head)
			}
		}
	}
	// The real event road accumulates small deltas before estimating their size.
	a.applyEvent(session.Event{Kind: session.EventReasoning, Text: "The"}, false)
	at := thoughtAt(t, a)
	if head := plain(a.thoughtRows(&a.entries[at], 100, false)[0]); strings.Contains(head, "0 tok") {
		t.Fatalf("streamed short thought displayed a zero: %q", head)
	}
}
