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

	"github.com/Agent-Field/aforge-v2/internal/lane"
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

// THE RATE ON THE ROW IS THE STREAM'S OWN, AND THE WRITING PHASE IS THE RATE
// ALONE. `38 tok/s` is [PhaseNews.Rate] — tokens over elapsed, measured on the
// live stream — and nothing else on this row is allowed to wear that spelling:
// not the last sighting's average, and not the per-turn burn, which counts a
// whole turn's waits and tool calls in its denominator and therefore reads low
// by a factor of several on any turn that ran one.
func TestTheRightEdgeSaysWhatTheStreamIsProducingRightNow(t *testing.T) {
	now := time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	answerArriving(a)
	// A sighting from the last answer, which used to be what this segment drew.
	pinSighting(t, provider.Sighting{
		Model: phaseModel, Provider: "quicksilver", Rate: 92, At: now.Add(-time.Second),
	}, true)

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-4 * time.Second), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if got := a.liveRiderAt(-1); got != "38 tok/s" {
		t.Fatalf("the right edge reads %q, want the stream's own rate alone", got)
	}
	line := plain(a.status(200))
	if !strings.Contains(line, "38 tok/s") {
		t.Fatalf("the row is missing the live rate:\n%q", line)
	}
	// NOT THE SIGHTING'S FIGURE, and not the phase's own words either: the state
	// word two runs to the right already says `working · 4s`, and who is serving
	// is on the seam.
	for _, gone := range []string{"92 tok/s", "writing", "friendli"} {
		if strings.Contains(line, gone) {
			t.Fatalf("the right edge still says %q:\n%q", gone, line)
		}
	}

	// A THINKING PASS IS THE SAME SEGMENT, because it is the same claim: the
	// stream is producing tokens and this is how fast.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseThinking, Since: now.Add(-12 * time.Second), Lane: "Friendli", Rate: 61,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if got := a.liveRiderAt(-1); got != "61 tok/s" {
		t.Fatalf("a thinking pass reads %q", got)
	}

	// AND A PHASE THAT IS NOT PRODUCING ANYTHING KEEPS ITS WORDS. In those the
	// clock is the only thing on the frame saying the turn is alive at all.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhasePaced, Since: now.Add(-6 * time.Second),
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if got := a.liveRiderAt(-1); got != "paced · 6s" {
		t.Fatalf("a paced wait reads %q, want the phase words", got)
	}
}

// AND A RATE NOBODY IS MEASURING IS NOTHING. Not the last answer's, not a zero,
// and not a figure carried through a wait: the emptiness law, and the one
// reading two rows of this surface take of one moment ([app.awaitingReply]).
func TestTheLiveRateIsDrawnOnlyWhileItIsBeingMeasured(t *testing.T) {
	now := time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	answerArriving(a)
	pinSighting(t, provider.Sighting{
		Model: phaseModel, Provider: "quicksilver", Rate: 92, At: now.Add(-time.Second),
	}, true)

	// No phase at all: the last answer's sighting is not a claim about now.
	if got := a.liveRiderAt(-1); got != "" {
		t.Fatalf("an idle right edge reads %q", got)
	}
	if line := plain(a.status(200)); strings.Contains(line, "tok/s") {
		t.Fatalf("a rate rides a row with no live phase:\n%q", line)
	}

	// A writing phase that has not measured a rate yet says nothing rather than
	// `0 tok/s`.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-time.Second), Lane: "Friendli",
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if got := a.liveRiderAt(-1); got != "" {
		t.Fatalf("an unmeasured rate drew %q", got)
	}

	// And a rate is not quoted through a wait this surface is naming two rows up.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-4 * time.Second), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	a.awaited = now.Add(-20 * time.Second)
	if got := a.liveRiderAt(-1); got != "" {
		t.Fatalf("the row quotes %q while the pulse says nothing has come back", got)
	}
}

// THE RIGHT EDGE GOES BEFORE THE ROW GIVES UP A NUMBER. The phase words at the
// right edge grow and shrink several times a turn, and the ledger on the left
// used to pay for it: the bill and the watch count disappeared and came back
// while a person was reading them, on the one row whose stillness is the whole
// reason it keeps a `$0.00`. The rate stands above the bill, the cache and the
// meter on the drop ladder (foot.go's [dropOrder]) — the clock on the state
// word already says the turn is alive.
func TestTheLiveRateGoesBeforeTheBillOnTheStatusLine(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	a.title = "porting the parser"
	a.cost = 1.12
	// A ledger with something in every group it can have here, so the widths
	// below are the ladder biting rather than an empty row fitting.
	a.ctxWindow, a.ctxTokens = 200_000, 100_000
	a.inputTokens, a.cacheRead = 10_000, 6_200
	PostPhaseNews(richPhase(now))
	// AND THE ANSWER IS ARRIVING, WHICH IS WHEN THE RIGHT EDGE OWNS THE PHASE.
	// The phase words have exactly one home per frame (render.go's
	// [app.pulseHoldsThePhase]): the pulse holds them while it is on the frame,
	// and the moment text starts landing the pulse goes and the status line
	// takes them up. This test is about the LADDER those words are fitted on,
	// so it is staged where they are on the status line.
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "the parser is"})
	a.live = len(a.entries) - 1

	// Wide enough for everything: the ledger whole and the right edge whole.
	wide := plain(a.status(200))
	for _, want := range []string{"via coreweave", "$1.12", "⟲ 62% cached", "100k/200k · 50%"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("the wide row is missing %q:\n%q", want, wide)
		}
	}
	// The crew word is not on this row at any width any more (foot.go's
	// [groupOff]), so it is not what pays for the rider either.
	if strings.Contains(wide, "crew") {
		t.Fatalf("the row grew a crew word:\n%q", wide)
	}

	// The row is still whole where both ends fit, and the rider is untouched.
	if line := plain(a.status(110)); !strings.Contains(line, "via coreweave") ||
		!strings.Contains(line, "$1.12") {
		t.Fatalf("a row with room for both ends gave one of them up:\n%q", line)
	}

	// And then the rate pays, rather than the bill.
	line := plain(a.status(100))
	for _, want := range []string{"$1.12", "⟲ 62% cached", "100k/200k · 50%"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the ledger lost %q while the rate kept its place:\n%q", want, line)
		}
	}
	if strings.Contains(line, "via coreweave") {
		t.Fatalf("the rate outlasted the numbers it stands above:\n%q", line)
	}
	// AND WHAT SURVIVES IT IS THE STATE WORD AND ITS CLOCK — the reason the line
	// is there at all, and the part that already says the turn is alive.
	if !strings.Contains(line, "working") {
		t.Fatalf("the row gave up the state word:\n%q", line)
	}
	if strings.Contains(line, glyphMore) {
		t.Fatalf("the row was clipped rather than said shorter:\n%q", line)
	}
}
