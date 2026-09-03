package tui3

// THE LIVE LINE'S RATE, AND THE TWO WAYS IT LIED.
//
// The status line is the one row on this surface that keeps a zero — `$0.00`,
// so its segments do not jump sideways while somebody is reading them — and
// that exception is the whole of the licence it has. A rate is not money: a
// zero one is the least informative cell on the frame at the exact moment a
// person is deciding whether to interrupt, and a rate quoted beside the pulse's
// own "nothing has come back yet" is this surface contradicting itself out loud.
// These tests hold both, and they hold them at the seam the two rows share:
// [app.awaitingReply] is one reading of one moment, and the pulse and the rate
// both take it.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// A RATE THAT ROUNDS TO NOTHING DRAWS NOTHING. One output token over a long
// turn is a positive count and a zero figure, and the guard that was there
// asked the count rather than the figure — so `0 tok/s` reached the line.
func TestAZeroRateDrawsNothingOnTheLiveStatusLine(t *testing.T) {
	a, _, now := hudApp(t)

	a.state = stateWorking
	a.turnBegan, a.turnOutStart = *now, 2_000
	// One token, sixty seconds: a real count and a rate below the step the line
	// rounds to.
	a.outputTokens = 2_001
	*now = now.Add(60 * time.Second)

	if rate := burnStep(0); rate != 0 {
		t.Fatalf("the fixture does not round to zero: burnStep(0) = %d", rate)
	}
	if got := a.burnSegment(); got != "" {
		t.Fatalf("the burn segment drew %q, want nothing: a zero rate is a zero", got)
	}
	if line := plain(a.status(200)); strings.Contains(line, "tok/s") {
		t.Fatalf("the live line quotes a rate over one token in a minute:\n%q", line)
	}
	// AND THE LINE'S ONE SANCTIONED ZERO IS UNTOUCHED. The money segment holds
	// its width on purpose, and this fix must not take that with it.
	if line := plain(a.status(200)); !strings.Contains(line, "$0.00") {
		t.Fatalf("the live line lost its held money zero:\n%q", line)
	}
}

// TWO FACTS ABOUT ONE MOMENT COME FROM ONE READING. While a request is out with
// nothing back from it, the pulse says so — and the rate, which counts a whole
// turn's output over a whole turn's wall time, went on quoting the paragraph
// that arrived before the silence started.
func TestTheRateIsSilentWhileThePulseSaysNothingHasComeBack(t *testing.T) {
	at := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	a, _, now := hudApp(t)
	*now = at
	a.model = "deepseek/deepseek-v4-flash"
	pinSighting(t, provider.Sighting{
		Model: "deepseek/deepseek-v4-flash", Provider: "quicksilver", Rate: 92, At: at.Add(-time.Second),
	}, true)

	// A turn that wrote a paragraph and is now writing: both rows agree.
	a.state = stateWorking
	a.turnBegan, a.turnOutStart = at.Add(-10*time.Second), 2_000
	a.outputTokens = 2_300
	if got := a.burnSegment(); got == "" {
		t.Fatal("a turn that is writing quotes no rate at all")
	}

	// And now the batch has closed and the next request is out with nothing back
	// from it — the moment the pulse names.
	a.awaited = at.Add(-20 * time.Second)
	if words := a.waitingWords(); words == "" {
		t.Fatal("the fixture is not a wait: the pulse says nothing")
	}
	if got := a.burnSegment(); got != "" {
		t.Fatalf("the line quotes %q while the pulse says %q — one moment, two answers",
			got, a.waitingWords())
	}
	if line := plain(a.status(200)); strings.Contains(line, "tok/s") {
		t.Fatalf("a rate rides the status line through a wait:\n%q", line)
	}
	// AND THE SERVED RIDER'S OWN FIGURE GOES WITH IT, because it is the same
	// contradiction said by a second row. Who answered is attribution and stays.
	if id := a.identity(); strings.Contains(id, "tok/s") {
		t.Fatalf("the served rider quotes a rate through a wait: %q", id)
	}
	if id := a.identity(); !strings.Contains(id, "via quicksilver") {
		t.Fatalf("the wait took the attribution with it: %q", id)
	}

	// The stream speaks again and the figure comes back: the wait clock is
	// cleared on the first delta (app.go), and the rate is a fact once more.
	a.awaited = time.Time{}
	if got := a.burnSegment(); got == "" {
		t.Fatal("the rate never came back after the stream spoke again")
	}
}

// THE RIDER GIVES UP A SPELLING BEFORE THE ROW GIVES UP A NUMBER. The phase
// words on the left grow and shrink several times a turn, and the segments on
// the right were paying for it: the bill and the watch count disappeared and
// came back while a person was reading them, on the one row whose stillness is
// the whole reason it keeps a `$0.00`.
func TestTheRidersSpellingGoesBeforeTheBillOnTheStatusLine(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	a.title = "porting the parser"
	a.cost = 1.12
	PostPhaseNews(richPhase(now))
	// AND THE ANSWER IS ARRIVING, WHICH IS WHEN THE RIDER OWNS THE PHASE.
	// The phase words have exactly one home per frame (render.go's
	// [app.pulseHoldsThePhase]): the pulse holds them while it is on the frame,
	// and the moment text starts landing the pulse goes and the status line
	// takes them up. This test is about the LADDER those words are fitted on,
	// so it is staged where they are on the status line.
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "the parser is"})
	a.live = len(a.entries) - 1

	// Wide enough for everything: both clusters whole.
	wide := plain(a.status(160))
	for _, want := range []string{"via coreweave", "$1.12", "crew balanced"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("the wide row is missing %q:\n%q", want, wide)
		}
	}

	// The cheap segments still go first, and the rider is untouched while they
	// have anything left to give.
	if line := plain(a.status(110)); strings.Contains(line, "crew balanced") ||
		!strings.Contains(line, "via coreweave") {
		t.Fatalf("the crew word should go before the rider's spelling:\n%q", line)
	}

	// And then the rider pays, rather than the bill.
	line := plain(a.status(100))
	if !strings.Contains(line, "$1.12") {
		t.Fatalf("the bill was dropped while the rider kept its widest spelling:\n%q", line)
	}
	if strings.Contains(line, "via coreweave") {
		t.Fatalf("the rider kept a spelling it could have given up:\n%q", line)
	}
	// AND WHAT IT GAVE UP IS A SPELLING AND NEVER A FACT: who answered, the
	// phase and its clock are all still on the row.
	for _, want := range []string{"coreweave", "first word", "3.1s"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the shortened rider lost %q:\n%q", want, line)
		}
	}
	if strings.Contains(line, glyphMore) {
		t.Fatalf("the row was clipped rather than said shorter:\n%q", line)
	}
}
