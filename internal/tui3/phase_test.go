package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE PHASE CLOCK ─────────────────────────────────────────────────────────
//
// Every test here drives the real seam — [PostPhaseNews], the function
// internal/session calls — rather than reaching into the desk, because the door
// is where the two laws live: a hidden role is dropped at it, and an empty
// phase clears at it.

// phaseModel is the conversation these tests are pointed at. It is a whole
// address so that nothing here passes by accident on a bare id.
const phaseModel = "moonshot/kimi-k3"

// phaseApp is a working surface with a pinned clock, so that "3.1s" is a
// statement about the news and not about how long the suite took to get here.
func phaseApp(t *testing.T, now time.Time) *app {
	t.Helper()
	t.Cleanup(forgetPhases)
	a := newTestApp(&fakeAgent{model: phaseModel})
	a.model = phaseModel
	a.state = stateWorking
	a.clock = func() time.Time { return now }
	return a
}

// answerArriving puts a streaming answer on the transcript, which is what takes
// the working line off the frame — and therefore what hands the phase words to
// the STATUS LINE's rider (render.go's [app.pulseHoldsThePhase]). Every test
// below that is about the rider's own content stages itself here, because the
// phase has exactly one home per frame and this is the state in which that home
// is the rider.
func answerArriving(a *app) {
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "the parser is"})
	a.live = len(a.entries) - 1
}

// EVERY STATE, IN THE WORDS A PERSON READS. This is the table the surface is
// specified by: if a phase's spelling changes, it changes here first.
func TestThePhaseClockSpellsEveryStateItIsToldAbout(t *testing.T) {
	t.Cleanup(forgetPhases)
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	for _, c := range []struct {
		what string
		news PhaseNews
		want string
	}{{
		what: "the last rung, which changes what was asked",
		news: PhaseNews{Phase: provider.PhaseSwitchingModel, Since: ago(time.Second), Then: "openrouter/qwen3.5-9b"},
		want: "switching model → qwen3.5-9b",
	}, {
		what: "the handshake",
		news: PhaseNews{Phase: provider.PhaseConnecting, Since: ago(1200 * time.Millisecond)},
		want: "connecting · 1.2s",
	}, {
		what: "the queue before the first word",
		news: PhaseNews{Phase: provider.PhaseFirstWord, Since: ago(3100 * time.Millisecond)},
		want: "first word · 3.1s",
	}, {
		what: "the same wait with a real consequence armed",
		news: PhaseNews{
			Phase:    provider.PhaseFirstWord,
			Since:    ago(3100 * time.Millisecond),
			Deadline: ago(3100 * time.Millisecond).Add(4400 * time.Millisecond),
			Then:     "Parasail",
		},
		want: "first word · 3.1s → parasail at 4.4s",
	}, {
		what: "a run of thought",
		news: PhaseNews{Phase: provider.PhaseThinking, Since: ago(12 * time.Second), Lane: "Friendli", Rate: 38},
		want: "thinking · 12s · friendli 38 t/s",
	}, {
		what: "the answer arriving",
		news: PhaseNews{Phase: provider.PhaseWriting, Since: ago(4 * time.Second), Lane: "Friendli", Rate: 61},
		want: "writing · 4s · friendli 61 t/s",
	}, {
		what: "a thinking pass nobody measured a lane or a rate for",
		news: PhaseNews{Phase: provider.PhaseThinking, Since: ago(12 * time.Second)},
		want: "thinking · 12s",
	}, {
		what: "a rate with no lane behind it",
		news: PhaseNews{Phase: provider.PhaseWriting, Since: ago(4 * time.Second), Rate: 61},
		want: "writing · 4s · 61 t/s",
	}, {
		what: "a rate-limit wait with the router's own retry moment",
		news: PhaseNews{Phase: provider.PhasePaced, Since: ago(2 * time.Second), Deadline: now.Add(6 * time.Second)},
		want: "paced · retry in 6s",
	}, {
		what: "a rate-limit wait nobody gave a moment for",
		news: PhaseNews{Phase: provider.PhasePaced, Since: ago(6 * time.Second)},
		want: "paced · 6s",
	}, {
		what: "the relax ladder saying which rung it is on",
		news: PhaseNews{Phase: provider.PhaseRetrying, Since: ago(4 * time.Second), Detail: "2 of 6"},
		want: "trying again · 2 of 6",
	}, {
		what: "the relax ladder that named no rung",
		news: PhaseNews{Phase: provider.PhaseRetrying, Since: ago(4 * time.Second)},
		want: "trying again · 4s",
	}, {
		what: "a rescue in flight, with the stall that caused it",
		news: PhaseNews{Phase: provider.PhaseSwitching, Since: ago(time.Second), Detail: "stalled 9s", Then: "Parasail"},
		want: "stalled 9s · switching to parasail",
	}, {
		what: "a rescue in flight with no stall length to give",
		news: PhaseNews{Phase: provider.PhaseSwitching, Since: ago(time.Second), Then: "Parasail"},
		want: "switching to parasail",
	}, {
		what: "a tool running between requests",
		news: PhaseNews{Phase: provider.PhaseRunning, Since: ago(41 * time.Second), Detail: "go test"},
		want: "running go test · 41s",
	}, {
		what: "a gate reading the answer",
		news: PhaseNews{Phase: provider.PhaseChecking, Since: ago(3 * time.Second)},
		want: "checking · 3s",
	}, {
		what: "a compaction pass",
		news: PhaseNews{Phase: provider.PhaseTidying, Since: ago(6 * time.Second)},
		want: "tidying · 6s",
	}, {
		// THE MARK'S OWN READING, which is a different wait from `checking`: a
		// second mind weighing the whole ask against what has been done, in the
		// middle of the work rather than at the end of an answer.
		what: "the reading a turn stops for at a mark",
		news: PhaseNews{Phase: provider.PhaseTakingStock, Since: ago(14 * time.Second)},
		want: "taking stock · 14s",
	}} {
		news := c.news
		news.Model, news.Role = phaseModel, lane.RoleTalk
		news.At = now
		PostPhaseNews(news)
		live, ok := phaseNewsFor(phaseModel)
		if !ok {
			t.Fatalf("%s: the desk kept nothing", c.what)
		}
		if got := phaseWords(live, now); got != c.want {
			t.Fatalf("%s reads %q, want %q", c.what, got, c.want)
		}
	}
}

// A DEADLINE IS NEVER SHOWN WITHOUT A CONSEQUENCE. A moment with nothing behind
// it is a countdown to nothing — the one thing this file exists to refuse.
func TestADeadlineIsNeverDrawnWithoutSomethingHappeningAtIt(t *testing.T) {
	t.Cleanup(forgetPhases)
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	since := now.Add(-3100 * time.Millisecond)

	// A deadline with no lane to move to — the guard armed on a model with
	// nowhere else to go — says the phase and the clock and stops there.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseFirstWord, Since: since, Deadline: since.Add(4400 * time.Millisecond),
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	live, _ := phaseNewsFor(phaseModel)
	if got := phaseWords(live, now); got != "first word · 3.1s" {
		t.Fatalf("a deadline with nothing behind it drew %q", got)
	}

	// And a name with no moment attached is a promise with no time on it.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseFirstWord, Since: since, Then: "Parasail",
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	live, _ = phaseNewsFor(phaseModel)
	if got := phaseWords(live, now); got != "first word · 3.1s" {
		t.Fatalf("a consequence with no moment drew %q", got)
	}
}

// THE WHOLE STORY OF A STALL, in the order a person watches it happen: the wait
// counts up under a moment something will be done at, and then that thing is
// done and the line says where the question went.
func TestAStalledStreamCountsTowardsTheRescueAndThenNamesIt(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	since := now.Add(-3100 * time.Millisecond)

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseFirstWord, Since: since, Deadline: since.Add(4400 * time.Millisecond),
		Then: "Parasail", Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if got := a.servedRider(); got != " · first word · 3.1s → parasail at 4.4s" {
		t.Fatalf("the wait before the rescue reads %q", got)
	}

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseSwitching, Since: now, Detail: "stalled 9s", Then: "Parasail",
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if got := a.servedRider(); got != " · stalled 9s · switching to parasail" {
		t.Fatalf("the rescue reads %q", got)
	}
}

// ONLY A ROLE A PERSON IS READING OWNS THE CLOCK. The naming errand and the
// memory reflex that run beside a talk turn are not the answer anybody is
// waiting for, and a status line that showed whichever of them posted last was
// half of the reported defect.
func TestAHiddenRolesPhaseNeverTouchesTheClock(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	answerArriving(a)

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseThinking, Since: now.Add(-12 * time.Second), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	talk := a.servedRider()
	if talk != " · thinking · 12s · friendli 38 t/s" {
		t.Fatalf("the talk turn's own phase reads %q", talk)
	}

	// A title errand on the same model, finishing fast on another machine.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-time.Second), Lane: "CoreWeave", Rate: 400,
		Model: phaseModel, Role: lane.RoleAuxiliary, At: now,
	})
	if got := a.servedRider(); got != talk {
		t.Fatalf("a side errand moved the clock: %q, was %q", got, talk)
	}
}

// A POSTING LAYER THAT DIED MUST NOT LEAVE A CLOCK RUNNING: past the freshness
// window the segment draws nothing at all.
func TestAStalePhaseDrawsNothing(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseThinking, Since: now.Add(-2 * time.Minute), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, At: now.Add(-(phaseWindow + time.Second)),
	})
	if _, ok := a.livePhase(); ok {
		t.Fatal("a phase nobody has said again for a quarter of a minute was still called live")
	}
	if got := a.servedRider(); strings.Contains(got, "thinking") {
		t.Fatalf("a dead layer left a clock running: %q", got)
	}
}

// AND A STAGE THAT IS STILL RUNNING IS STILL DRAWN, however long it runs.
//
// The window above is one half of a bargain and this is the other: every posting
// layer says its phase again while it lasts — a request off its own stream, a
// turn's stage off a timer (internal/session's phaseHeldBeat) — so what the desk
// judges is the moment it was last HEARD and never the moment the stage began.
// A quarter-hour reading that beats is a quarter-hour clock on the screen; the
// measured defect was the same reading drawn for fifteen seconds of it.
func TestAStageThatKeepsSayingItselfIsNeverDropped(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseChecking, Since: now.Add(-15 * time.Minute),
		Model: phaseModel, Role: lane.RoleTalk, At: now.Add(-time.Second),
	})
	live, ok := a.livePhase()
	if !ok {
		t.Fatal("a stage said again a second ago was dropped as stale")
	}
	if got := phaseWords(live, now); got != "checking · 15m 0s" {
		t.Fatalf("the stage reads %q, want the whole quarter of an hour it has run", got)
	}
}

// AND A TURN THAT STOPPED SAYS SO. The empty phase is the end of the story, not
// a phase called "" — it clears the entry so nothing keeps counting.
func TestAFinishedTurnTakesItsClockOffTheScreen(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-4 * time.Second), Lane: "Friendli", Rate: 61,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if _, ok := a.livePhase(); !ok {
		t.Fatal("a running turn had no clock")
	}
	PostPhaseNews(PhaseNews{Model: phaseModel, Role: lane.RoleTalk, At: now})
	if _, ok := a.livePhase(); ok {
		t.Fatal("a finished turn left its clock on the desk")
	}
	if got := a.servedRider(); strings.Contains(got, "writing") {
		t.Fatalf("a finished turn is still writing: %q", got)
	}
}

// ── THE INDICATOR'S THREE ANSWERS ───────────────────────────────────────────

// THE PHASE CLOCK OUTRANKS BOTH GUESSES. The wait and the silence are
// inferences from the absence of deltas; the phase is the wire's own account of
// itself, and where it exists neither of the other two is drawn.
func TestThePhaseClockOutranksTheWaitAndTheSilence(t *testing.T) {
	now := time.Now()
	a := phaseApp(t, now)
	// Both fallbacks are armed: a request has been outstanding for a minute and
	// the stream has been silent for one.
	a.awaited = now.Add(-time.Minute)
	a.lastDelta = now.Add(-time.Minute)

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseThinking, Since: now.Add(-12 * time.Second), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	line := plain(mustLine(t, a))
	if !strings.HasSuffix(line, " thinking · 12s · friendli 38 t/s") {
		t.Fatalf("the indicator did not take the phase: %q", line)
	}
	if strings.Contains(line, "waiting for") || strings.Contains(line, stillWorkingWord) {
		t.Fatalf("two answers to one question were on the line: %q", line)
	}
}

// AND WITH NO PHASE THE OLDER WORDS SURVIVE. The emptiness law read downwards:
// "still working" is what is left when nothing better is known, and it is still
// better than three dots.
func TestWithNoPhaseTheOlderIndicatorsStillSpeak(t *testing.T) {
	now := time.Now()
	a := phaseApp(t, now)
	a.lastDelta = now.Add(-(stillWorking + time.Second))

	if got := plain(mustLine(t, a)); !strings.HasSuffix(got, stillWorkingWord) {
		t.Fatalf("with no phase running the silence said %q", got)
	}

	// And a phase for some OTHER model is not this conversation's clock.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseThinking, Since: now.Add(-12 * time.Second),
		Model: "openai/gpt-6", Role: lane.RoleTalk, At: now,
	})
	if got := plain(mustLine(t, a)); !strings.HasSuffix(got, stillWorkingWord) {
		t.Fatalf("another model's phase took this line: %q", got)
	}
}

// THE RATE RIDES ONLY WHILE A TURN IS RUNNING, which is the rule the two riders
// under this one already keep: who is answering is attribution and stays, and
// how fast they are writing is a claim about now.
func TestAnIdleSurfaceKeepsThePhaseAndDropsTheRate(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	a.state = stateIdle

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-4 * time.Second), Lane: "Friendli", Rate: 61,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if got := a.servedRider(); got != " · writing · 4s · friendli" {
		t.Fatalf("an idle surface reads %q", got)
	}
}

// ── THE LADDER ──────────────────────────────────────────────────────────────
//
// A narrow row used to lose the segment by CLIPPING it, and a clip takes the
// tail: the rate went first, which was right, and then the machine's name, the
// phase and the clock went together in one step and left an ellipsis standing
// where a shorter true sentence would have fitted. These tests pin the ladder
// that replaced it — every part in its own spelling, dropped by what it is
// worth.

// richPhase is the fullest thing this clock can say: a queue before the first
// word, on a named machine, with a real moment and a real alternative behind it.
func richPhase(now time.Time) PhaseNews {
	since := now.Add(-3100 * time.Millisecond)
	return PhaseNews{
		Phase: provider.PhaseFirstWord, Since: since,
		Deadline: since.Add(4400 * time.Millisecond), Then: "Parasail", Lane: "CoreWeave",
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	}
}

// THE FOUR RUNGS, WIDEST TO NARROWEST. Every one of them is a whole true
// sentence; not one of them is a cut one.
func TestTheServedSegmentDegradesByWhatItsPartsAreWorth(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seg := phaseFields(richPhase(now), now)

	for _, c := range []struct {
		width int
		want  string
	}{
		{width: -1, want: "via coreweave · first word 3.1s → parasail at 4.4s"},
		{width: 60, want: "via coreweave · first word 3.1s → parasail at 4.4s"},
		{width: 50, want: "via coreweave · first word 3.1s → parasail at 4.4s"},
		// The lead word is grammar and is the first thing a narrow row spends.
		{width: 49, want: "coreweave · first word 3.1s → parasail at 4.4s"},
		{width: 46, want: "coreweave · first word 3.1s → parasail at 4.4s"},
		// Then every field says the least of itself that is still true — and
		// the lead comes BACK the moment the shorter fields have left room for
		// it, because between two rows that say as much the more identifying
		// name is the better one (rowfit.go's [rowLed]).
		{width: 45, want: "via coreweave · 3.1s → parasail 4.4s"},
		{width: 36, want: "via coreweave · 3.1s → parasail 4.4s"},
		{width: 35, want: "coreweave · 3.1s → parasail 4.4s"},
		{width: 32, want: "coreweave · 3.1s → parasail 4.4s"},
		// Then the consequence goes WHOLE — never a bare countdown.
		{width: 31, want: "via coreweave · 3.1s"},
		{width: 20, want: "via coreweave · 3.1s"},
		{width: 19, want: "coreweave · 3.1s"},
		{width: 16, want: "coreweave · 3.1s"},
		// Then the clock, and the machine's name is the last thing standing.
		{width: 15, want: "via coreweave"},
		{width: 13, want: "via coreweave"},
		{width: 12, want: "coreweave"},
		{width: 9, want: "coreweave"},
		// And a row with no room for the whole name draws NOTHING, because half
		// a machine's name names no machine.
		{width: 8, want: ""},
		{width: 0, want: ""},
	} {
		if got := rowLed(seg, roomFor(c.width)); got != c.want {
			t.Fatalf("at %d columns the segment reads %q, want %q", c.width, got, c.want)
		}
	}
}

// AND WITH NOTHING ARMED BEHIND THE WAIT THE SAME LADDER IS ONE RUNG SHORTER.
// No deadline and no alternative is no arrow at any width, which is the law the
// whole file is built on said again under width pressure.
func TestTheLadderWithNoConsequenceToName(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	news := richPhase(now)
	news.Deadline, news.Then = time.Time{}, ""
	seg := phaseFields(news, now)

	for _, c := range []struct {
		width int
		want  string
	}{
		{width: -1, want: "via coreweave · first word 3.1s"},
		{width: 31, want: "via coreweave · first word 3.1s"},
		{width: 30, want: "coreweave · first word 3.1s"},
		{width: 27, want: "coreweave · first word 3.1s"},
		{width: 26, want: "via coreweave · 3.1s"},
		{width: 20, want: "via coreweave · 3.1s"},
		{width: 19, want: "coreweave · 3.1s"},
		{width: 16, want: "coreweave · 3.1s"},
		{width: 15, want: "via coreweave"},
		{width: 12, want: "coreweave"},
		{width: 8, want: ""},
	} {
		if got := rowLed(seg, roomFor(c.width)); got != c.want {
			t.Fatalf("with nothing armed, %d columns reads %q, want %q", c.width, got, c.want)
		}
	}
}

// WITH NO MACHINE NAMED THERE IS NO `via` AT ANY WIDTH, and the phase's own
// word leads the segment instead. The emptiness law is not suspended by width
// pressure: an empty lead would be a cell spent saying that nobody said who
// was answering.
func TestTheLadderWithNoLaneNamedLeadsWithThePhasesOwnWord(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	news := richPhase(now)
	news.Lane = ""
	seg := phaseFields(news, now)

	for _, c := range []struct {
		width int
		want  string
	}{
		{width: -1, want: "first word · 3.1s → parasail at 4.4s"},
		{width: 36, want: "first word · 3.1s → parasail at 4.4s"},
		{width: 35, want: "first word · 3.1s → parasail 4.4s"},
		{width: 33, want: "first word · 3.1s → parasail 4.4s"},
		{width: 32, want: "first word · 3.1s"},
		{width: 17, want: "first word · 3.1s"},
		{width: 16, want: "first word"},
		{width: 10, want: "first word"},
		{width: 9, want: ""},
	} {
		if got := rowLed(seg, roomFor(c.width)); got != c.want {
			t.Fatalf("with no lane named, %d columns reads %q, want %q", c.width, got, c.want)
		}
	}
	for width := -1; width <= 40; width++ {
		if got := rowLed(seg, roomFor(width)); strings.Contains(got, "via") {
			t.Fatalf("a lane nobody named was announced at %d columns: %q", width, got)
		}
	}
}

// THE LADDER NEVER OVERRUNS AND NEVER ELLIPSES. Both halves of that are the
// point of the thing: a rung is either a whole true sentence inside the width
// it was given, or it is nothing at all.
func TestTheLadderNeverOverrunsItsWidthNorEndsInATruncationGlyph(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	rich := richPhase(now)
	quiet := rich
	quiet.Deadline, quiet.Then, quiet.Lane = time.Time{}, "", ""
	writing := PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-4 * time.Second), Lane: "Friendli", Rate: 61,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	}
	running := PhaseNews{
		Phase: provider.PhaseRunning, Since: now.Add(-41 * time.Second), Detail: "go test",
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	}
	for _, news := range []PhaseNews{rich, quiet, writing, running} {
		seg := phaseFields(news, now)
		widest := rowLed(seg, rowUnbounded)
		for width := 0; width <= len(widest)+8; width++ {
			got := rowLed(seg, roomFor(width))
			if measured := ansi.StringWidth(got); measured > width {
				t.Fatalf("%q ran %d columns over a budget of %d: %q", widest, measured-width, width, got)
			}
			if strings.Contains(got, glyphMore) {
				t.Fatalf("%q was clipped at %d columns rather than said shorter: %q", widest, width, got)
			}
		}
	}
}

// AND THE ROW HANDS THE LADDER THE COLUMNS IT ACTUALLY HAS. The rider is the
// half of the identity cluster that has a shorter true spelling, so it is the
// half the width comes out of — where before this wave the whole cluster was
// handed to a clip, or dropped entire.
func TestTheIdentityClusterShortensItsRiderRatherThanBeingClipped(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	a.title = "porting the parser"
	answerArriving(a)
	PostPhaseNews(richPhase(now))

	// Unbounded, the cluster is everything it has always been plus the ladder's
	// widest rung.
	want := "porting the parser · kimi-k3 · via coreweave · first word 3.1s → parasail at 4.4s"
	if got, _ := a.identityParts(0); got != want {
		t.Fatalf("the unbounded cluster reads %q, want %q", got, want)
	}
	if got := a.identity(); got != want {
		t.Fatalf("identity() reads %q, want %q", got, want)
	}

	// Bounded, it gives up a spelling — and never a fact, and never its shape.
	for _, c := range []struct {
		width int
		want  string
	}{
		{width: 81, want: "porting the parser · kimi-k3 · via coreweave · first word 3.1s → parasail at 4.4s"},
		{width: 77, want: "porting the parser · kimi-k3 · coreweave · first word 3.1s → parasail at 4.4s"},
		{width: 63, want: "porting the parser · kimi-k3 · coreweave · 3.1s → parasail 4.4s"},
		{width: 47, want: "porting the parser · kimi-k3 · coreweave · 3.1s"},
		{width: 40, want: "porting the parser · kimi-k3 · coreweave"},
		{width: 28, want: "porting the parser · kimi-k3"},
	} {
		got, _ := a.identityParts(c.width)
		if got != c.want {
			t.Fatalf("at %d columns the cluster reads %q, want %q", c.width, got, c.want)
		}
		if measured := ansi.StringWidth(got); measured > c.width {
			t.Fatalf("the cluster ran %d columns over %d: %q", measured-c.width, c.width, got)
		}
		if strings.Contains(got, glyphMore) {
			t.Fatalf("the cluster was clipped at %d columns: %q", c.width, got)
		}
	}
}

// TestAnotherWindowsWorkNeverTakesThisRow is the third of the three filters,
// and the one the other two cannot do. The desk is keyed by MODEL, and a task
// node running on the same model id posts phases whose role is perfectly
// visible — visible in ITS room, which is not this row. A conversation's status
// line says what the conversation is doing or it says nothing.
func TestAnotherWindowsWorkNeverTakesThisRow(t *testing.T) {
	t.Cleanup(forgetPhases)
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.state = stateWorking
	answerArriving(a)

	before := a.servedRider()
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting,
		Since: time.Now().Add(-4 * time.Second),
		Lane:  "friendli",
		Rate:  61,
		Model: flash,
		Role:  lane.RoleLeafAttached,
		At:    time.Now(),
	})
	if got := a.servedRider(); got != before {
		t.Fatalf("a task node's phase took the conversation's row: %q", got)
	}
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting,
		Since: time.Now().Add(-4 * time.Second),
		Lane:  "friendli",
		Rate:  61,
		Model: flash,
		Role:  lane.RoleTalk,
		At:    time.Now(),
	})
	if got := a.servedRider(); got == before {
		t.Fatalf("the conversation's own phase did not reach its own row: %q", got)
	}
}

// ── ONE INSTANT FOR THE WHOLE WAIT ──────────────────────────────────────────

// A CLOCK THAT COUNTS THE WRONG WAY IS A SURFACE NOBODY TRUSTS AGAIN. Every
// posting layer is honest about the stage it is announcing — a retry builds a
// whole new clock for its attempt, and the surface's own two sentences about a
// stall carry their own instants — but a person reads one number and it is "how
// long have I been waiting", so a phase change used to halve it while they
// watched. [phaseWaiting] is the family that shares one start, and
// [PostPhaseNews] is the one door that hands it out.
func TestThePhaseClockNeverCountsBackwardsAcrossOneWait(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	now := start
	a := phaseApp(t, now)
	a.clock = func() time.Time { return now }

	read := func() string {
		news, ok := a.livePhase()
		if !ok {
			t.Fatalf("no phase is live at %s", now.Sub(start))
			return ""
		}
		return phaseWords(news, a.now())
	}

	// The request leaves, and the handshake is the first thing anybody is told.
	PostPhaseNews(PhaseNews{Phase: provider.PhaseConnecting, Since: start,
		Model: phaseModel, Role: lane.RoleTalk, At: start})
	PostPhaseNews(PhaseNews{Phase: provider.PhaseFirstWord, Since: start,
		Model: phaseModel, Role: lane.RoleTalk, At: start})

	// Nineteen seconds in, the controller reports every lane slow — and posts
	// its own, later, instant for the sentence it is adding.
	now = start.Add(19 * time.Second)
	PostPhaseNews(PhaseNews{Phase: session.PhaseAllSlow, Since: start.Add(9 * time.Second),
		Model: phaseModel, Role: lane.RoleTalk, At: now})
	if got := read(); !strings.Contains(got, "19s") {
		t.Fatalf("nineteen seconds into one wait the clock reads %q, want the wait's own 19s — a later phase may not restart it", got)
	}

	// Ten seconds after that the retry loop opens a brand new attempt, with a
	// brand new clock of its own. The wait a person is in is twenty-nine
	// seconds old and the line may not say otherwise.
	now = start.Add(29 * time.Second)
	PostPhaseNews(PhaseNews{Phase: provider.PhaseFirstWord, Since: now, Lane: "OpenInference",
		Model: phaseModel, Role: lane.RoleTalk, At: now})
	if got := read(); !strings.Contains(got, "29s") {
		t.Fatalf("ten seconds later the clock reads %q, want 29s — it counted backwards, which is what a person reads as the program losing track of itself", got)
	}
}

// AND A STAGE OF WORK KEEPS ITS OWN CLOCK, because that number answers a
// different question: "running go test · 41s" is about the test, not about the
// turn, and a tool that started ten seconds ago has been running ten seconds
// however long the wait before it was.
func TestWorkInProgressKeepsItsOwnClockAndDoesNotInheritTheWait(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	now := start
	a := phaseApp(t, now)
	a.clock = func() time.Time { return now }

	PostPhaseNews(PhaseNews{Phase: provider.PhaseFirstWord, Since: start,
		Model: phaseModel, Role: lane.RoleTalk, At: start})
	now = start.Add(40 * time.Second)
	PostPhaseNews(PhaseNews{Phase: provider.PhaseRunning, Detail: "go test", Since: now.Add(-4 * time.Second),
		Model: phaseModel, Role: lane.RoleTalk, At: now})

	news, ok := a.livePhase()
	if !ok {
		t.Fatal("no phase is live")
	}
	if got := phaseWords(news, a.now()); got != "running go test · 4s" {
		t.Fatalf("a tool four seconds old reads %q, want %q — a stage's clock is its own", got, "running go test · 4s")
	}

	// And the wait that follows the work is a NEW wait, measured from itself.
	now = start.Add(50 * time.Second)
	PostPhaseNews(PhaseNews{Phase: provider.PhaseFirstWord, Since: now,
		Model: phaseModel, Role: lane.RoleTalk, At: now})
	now = start.Add(53 * time.Second)
	news, _ = a.livePhase()
	if got := phaseWords(news, a.now()); got != "first word · 3.0s" {
		t.Fatalf("the wait after a tool ran reads %q, want %q — it must not inherit the wait before the tool", got, "first word · 3.0s")
	}
}

// ── ONE HOME FOR THE PHASE WORDS ────────────────────────────────────────────

// THE SAME SENTENCE WAS DRAWN TWICE ON ONE FRAME, VERBATIM, TWO ROWS APART:
// `·· paced · retry in 2s` on the pulse and `… · paced · retry in 2s` on the
// status line. Two live things moving in lockstep saying one fact, and the
// second copy was spending the cells the bill and the watch count needed.
//
// The rule is [app.pulseHoldsThePhase]: the pulse owns the words while it is on
// the frame, and the rider takes them up the moment it is not — so the phase is
// on exactly one row in every state of a turn, and never on two.
func TestThePhaseWordsAreDrawnOnOneRowAndNeverTwice(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	a := phaseApp(t, now)
	a.title = "porting the parser"
	PostPhaseNews(PhaseNews{Phase: provider.PhasePaced, Since: now.Add(-2 * time.Second),
		Deadline: now.Add(2 * time.Second), Model: phaseModel, Role: lane.RoleTalk, At: now})

	const words = "paced · retry in 2s"

	// NOTHING HAS COME BACK YET: the pulse is on the frame and it has the words.
	pulse := plain(mustLine(t, a))
	if !strings.Contains(pulse, words) {
		t.Fatalf("the pulse does not carry the phase: %q", pulse)
	}
	if line := plain(a.status(160)); strings.Contains(line, "paced") {
		t.Fatalf("the phase is on the status line as well as the pulse — one fact on two rows:\npulse  %q\nstatus %q", pulse, line)
	}

	// THE ANSWER STARTS ARRIVING: the pulse goes, and the rider takes the words
	// up rather than leaving the frame with no phase on it at all.
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "the parser is"})
	a.live = len(a.entries) - 1
	if _, on := a.ellipsis(); on {
		t.Fatal("the pulse is still drawn under a streaming answer, so this case tests nothing")
	}
	if line := plain(a.status(160)); !strings.Contains(line, words) {
		t.Fatalf("with the pulse gone the phase is on no row at all: %q", line)
	}
}
