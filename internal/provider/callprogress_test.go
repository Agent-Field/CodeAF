package provider

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── WATCHING ONE CALL RUN, THROUGH THE WIRE ─────────────────────────────────
//
// Every scenario here drives the shipped transport against a scripted router
// (internal/lane/lanestub) and asserts on what a watcher was told. A unit test
// over [callProgress] alone would prove the coalescing and nothing about the
// seam, which is the half that has been wrong before: the counts, the first
// token and the ending all come from places in this package that a fake would
// have to re-implement to stand in for.

// progressLog is a watcher that keeps what it was told, which is all a watcher
// on this seam is allowed to do (callprogress.go's no-work law).
type progressLog struct {
	mu   sync.Mutex
	seen []CallProgress
}

func (l *progressLog) watch(progress CallProgress) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, progress)
}

func (l *progressLog) all() []CallProgress {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]CallProgress(nil), l.seen...)
}

// last is the final report, and whether there was one at all.
func (l *progressLog) last() (CallProgress, bool) {
	seen := l.all()
	if len(seen) == 0 {
		return CallProgress{}, false
	}
	return seen[len(seen)-1], true
}

// TestAWatchedCallReportsGoingOutWritingAndAnswering is the whole life of one
// call, from the outside.
//
// The lane writes its router comment lines, takes 200 ms to a first word and
// then forty tokens at forty a second — so the scenario really does span the
// second the coalescing rule is about, and the count below is a count of how
// often a surface would have been asked to redraw.
func TestAWatchedCallReportsGoingOutWritingAndAnswering(t *testing.T) {
	rig := newLaneRig(t, "progress/answered",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 200 * time.Millisecond, Rate: 40, Tokens: 40, Heartbeats: true,
		}},
	)
	// Wide enough that nothing acts on this call: what is being proved is the
	// report, not the policy.
	rig.patience(t, 5*time.Second)

	log := &progressLog{}
	ctx := WithCallProgress(talking(), log.watch)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	seen := log.all()
	if len(seen) == 0 {
		t.Fatal("a watched call reported nothing at all")
	}
	if first := seen[0]; first.Phase != CallStarted {
		t.Fatalf("the first report is %q, want the request going out", first.Phase)
	}
	if first := seen[0]; first.Model != rig.model || first.Started.IsZero() {
		t.Fatalf("the first report names %q at %v, want the model and the moment it went out", first.Model, first.Started)
	}
	if first := seen[0]; !first.FirstToken.IsZero() {
		t.Fatal("a request that has only just gone out reported a first token")
	}

	// A HEARTBEAT IS NOT A FIRST TOKEN, AND THIS IS WHERE THAT IS PROVED. The
	// lane writes its three router comment lines at 66, 133 and 200 ms; a seam
	// that read one of them as the model beginning would report a first token a
	// third of the way into the wait, which is what the floor below catches.
	var firstToken CallProgress
	for _, progress := range seen {
		if !progress.FirstToken.IsZero() {
			firstToken = progress
			break
		}
	}
	if firstToken.FirstToken.IsZero() {
		t.Fatal("no report carried a first token")
	}
	if waited := firstToken.FirstToken.Sub(firstToken.Started); waited < 150*time.Millisecond || waited > time.Second {
		t.Fatalf("the first token landed %v after the request went out, want about the scripted 200ms", waited)
	}
	if firstToken.Phase != CallWriting {
		t.Fatalf("the first token was reported as %q, want the answer arriving", firstToken.Phase)
	}

	// THE COUNT NEVER GOES BACKWARDS. A surface draws it as a total, so a report
	// that lost ground would draw an answer being unwritten.
	tokens := 0
	writing := 0
	for _, progress := range seen {
		if progress.Tokens < tokens {
			t.Fatalf("the token count went from %d to %d", tokens, progress.Tokens)
		}
		tokens = progress.Tokens
		if progress.Phase == CallWriting {
			writing++
		}
	}
	if tokens == 0 {
		t.Fatal("a call that streamed forty tokens reported none")
	}

	// TEN TIMES A SECOND AND NO MORE. Forty deltas arrived over a scripted
	// second, so a seam that forwarded each of them would have asked a surface
	// to redraw forty times. The exact arithmetic is proved against a scripted
	// clock in TestTheBeatHoldsAFastStreamToTenReportsASecond; what is proved
	// here is that the rule survives the wire.
	if writing >= 40 {
		t.Fatalf("%d reports of the answer arriving, want the forty deltas coalesced", writing)
	}

	end, ok := log.last()
	if !ok || end.Phase != CallEnded {
		t.Fatalf("the last report is %q, want the ending", end.Phase)
	}
	if end.End != CallEndAnswered {
		t.Fatalf("the call ended %q, want the model having finished", end.End)
	}
	if end.Served != "A" {
		t.Fatalf("the ending names %q, want the machine that answered", end.Served)
	}
	if end.Tokens != tokens {
		t.Fatalf("the ending carries %d tokens, want the %d the stream delivered", end.Tokens, tokens)
	}
}

// TestAnAnswerThatStopsHalfwayIsReportedCut is the other real ending.
//
// The lane writes five words and the connection goes away underneath it — no
// finish frame, no usage, no sentinel ([lanestub.Profile.TearAfter]). What a
// surface has to be told is that the words it drew are all there will be, which
// is a different thing from the model having finished and from the caller
// having left.
func TestAnAnswerThatStopsHalfwayIsReportedCut(t *testing.T) {
	rig := newLaneRig(t, "progress/cut",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 20 * time.Millisecond, Rate: 400, Tokens: 40, TearAfter: 5,
		}},
	)
	rig.patience(t, 5*time.Second)

	log := &progressLog{}
	ctx := WithCallProgress(talking(), log.watch)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))
	// Whether the torn answer reaches the caller is not this test's question;
	// what it was told while it ran is.
	_, _ = rig.client.CompleteWithMessages(ctx, userMessages("hello"))

	end, ok := log.last()
	if !ok {
		t.Fatal("a torn call reported nothing at all")
	}
	if end.Phase != CallEnded {
		t.Fatalf("the last report is %q, want the ending", end.Phase)
	}
	if end.End != CallEndCut {
		t.Fatalf("a reply that stopped halfway ended %q, want it read as cut", end.End)
	}
}

// TestEachArmOfARaceIsWatchedAndTheLoserIsNotAFailure is the hedged shape.
//
// Two requests are in flight for one question and a watcher draws one row per
// request, so each has to name itself — and the one that is cut off when the
// other answers is EXHAUST AND NOT A FAILURE. Its row says `context canceled`,
// which is also what a person pressing stop says, and only the race knows the
// difference (armwatch.go's [streamWatch.lost]).
func TestEachArmOfARaceIsWatchedAndTheLoserIsNotAFailure(t *testing.T) {
	// A IS HELD ON A SIGNAL AND NOT ON A CLOCK. Which arm wins is the rule this
	// asserts, and an A told to resume after some number of milliseconds asserts
	// nothing but the slack between two wall-clock figures — which a loaded box
	// eats, and a correct build then looks broken (hedge_test.go says the same
	// thing at more length). Held open until this channel closes, and it closes
	// only at teardown, A cannot finish first.
	resume := make(chan struct{})
	rig := newLaneRig(t, "progress/race",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000, Tokens: 60,
			StallAfter: 30, StallUntil: resume,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// Released after the assertions and BEFORE the rig closes its server, so a
	// run that never reached the cancel still lets the handler go.
	t.Cleanup(func() { close(resume) })
	// Believed at a quarter of what it really writes at, which is the honest
	// shape of a belief: ordinary jitter is never a surprise and a long silence
	// is nothing else.
	rig.believes("A", 2, 250)

	log := &progressLog{}
	report := &HedgeReport{}
	ctx := WithHedgeReport(WithCallProgress(talking(), log.watch), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("the scenario did not race, so there is nothing here about two arms")
	}

	// BOTH ARMS ARE REPORTED, each under its own number, so a reader can draw the
	// rescue beside the request it was sent to save.
	arms := map[int]bool{}
	for _, report := range log.all() {
		arms[report.Attempt] = true
	}
	if len(arms) < 2 {
		t.Fatalf("%d arm reported, want the primary and the rescue both", len(arms))
	}

	// AND THE QUESTION ENDS ONCE, ON THE ARM THAT ANSWERED. The loser is cut off
	// the instant the winner commits and its row says `context canceled` — which
	// is what a person pressing stop says too. A seam that let that be the
	// question's ending would draw this build's own hedging policy as a call the
	// person abandoned.
	endings := 0
	for _, report := range log.all() {
		if report.Phase != CallEnded {
			continue
		}
		endings++
		if report.End != CallEndAnswered {
			t.Fatalf("the question ended %q, want the answer the race went and got", report.End)
		}
		if report.Served != "B" {
			t.Fatalf("the ending names %q, want the arm that answered", report.Served)
		}
	}
	if endings != 1 {
		t.Fatalf("%d endings, want exactly one however many arms ran", endings)
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
}

// TestNobodyWatchingCostsNothingAndTheCallIsUnchanged is the empty state, which
// is every call in an ordinary run.
func TestNobodyWatchingCostsNothingAndTheCallIsUnchanged(t *testing.T) {
	rig := newLaneRig(t, "progress/unwatched",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 8}},
	)
	rig.patience(t, 5*time.Second)

	ctx := WithLaneChoice(talking(), choiceFor(rig.model, 0))
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if tokens := answerTokens(response); tokens != 8 {
		t.Fatalf("the answer is %d tokens, want the scripted 8", tokens)
	}
	if watcher := callProgressFrom(ctx); watcher != nil {
		t.Fatal("a context nobody attached a watcher to carries one")
	}
}

// TestOneQuestionOpensOneReportHoweverManyArmsRun guards the thing a context
// seam can get wrong. The door that opens the report is re-entered by every arm
// of a race on a child context; an arm that opened a second report would give
// the question a second ending, and two reports over one question would each
// hold their own coalescing beat and redraw a surface at twice the stated rate.
func TestOneQuestionOpensOneReportHoweverManyArmsRun(t *testing.T) {
	log := &progressLog{}
	ctx := WithCallProgress(talking(), log.watch)
	ctx, opened := beginCallProgress(ctx, "openrouter/x")
	if opened == nil {
		t.Fatal("a watched question opened no report")
	}
	if _, again := beginCallProgress(ctx, "openrouter/x"); again != nil {
		t.Fatal("an arm re-entering the door opened a second report, so the question would end twice")
	}
	primary, rescue := &streamWatch{arm: 0}, &streamWatch{arm: 1}
	withStreamWatch(ctx, primary)
	withStreamWatch(ctx, rescue)
	if primary.progress != opened || rescue.progress != opened {
		t.Fatal("two arms of one question report into two rows")
	}
}

// TestTheBeatHoldsAFastStreamToTheSurfacesOwnFrame is the coalescing law,
// judged against a scripted clock so that the arithmetic is the thing under test
// and not the speed of the box.
//
// A hundred deltas arrive over one second — the rate a fast lane really writes
// at. The surface paints on `internal/tui3`'s frameInterval, and two reports
// inside one of those frames differ only in which is thrown away, so the rule is
// that no two reports of a climbing count are closer together than the frame.
func TestTheBeatHoldsAFastStreamToTheSurfacesOwnFrame(t *testing.T) {
	const gap = 10 * time.Millisecond
	log := &progressLog{}
	progress := newCallProgress(log.watch, "openrouter/x")
	began := time.Now()
	progress.opened(0, began)
	for delta := 1; delta <= 100; delta++ {
		progress.note(0, began.Add(time.Duration(delta)*gap), delta, 0)
	}

	// The moment of each report is derived from its own count, because the count
	// IS the delta index here — which is what lets this assert the spacing rule
	// rather than a total somebody has to recompute when the frame moves.
	reports := 0
	previous := time.Time{}
	tokens := 0
	for _, seen := range log.all() {
		if seen.Phase != CallWriting {
			continue
		}
		if seen.Tokens <= tokens {
			t.Fatalf("the count went from %d to %d", tokens, seen.Tokens)
		}
		tokens = seen.Tokens
		at := began.Add(time.Duration(seen.Tokens) * gap)
		if reports > 0 {
			if spacing := at.Sub(previous); spacing < callProgressBeat {
				t.Fatalf("two reports %v apart, want no closer than the frame (%v)", spacing, callProgressBeat)
			}
		}
		previous, reports = at, reports+1
	}
	if reports == 0 || reports >= 100 {
		t.Fatalf("a hundred deltas drew %d reports, want them coalesced onto the frame", reports)
	}
}

// TestThePacingParkIsAPhaseOfTheCall pins the fold that keeps this build with
// ONE account of what a call is doing. [WithPacingNotice] is the older, bool
// spelling of the same fact, and dispatch.go flips both from one site — so a
// park has to reach a progress watcher as [CallPaced] and leaving it has to put
// the call back where it was.
func TestThePacingParkIsAPhaseOfTheCall(t *testing.T) {
	log := &progressLog{}
	progress := newCallProgress(log.watch, "openrouter/x")
	began := time.Now()
	progress.opened(0, began)
	progress.paced(0, true, began.Add(time.Second))
	progress.paced(0, true, began.Add(2*time.Second))
	progress.paced(0, false, began.Add(3*time.Second))

	phases := []CallPhase{}
	for _, seen := range log.all() {
		phases = append(phases, seen.Phase)
	}
	want := []CallPhase{CallStarted, CallPaced, CallStarted}
	if len(phases) != len(want) {
		t.Fatalf("the park reported %v, want %v — a park is said once and so is leaving it", phases, want)
	}
	for index, phase := range want {
		if phases[index] != phase {
			t.Fatalf("report %d is %q, want %q", index, phases[index], phase)
		}
	}
}

// TestALatchedEndingIsNotAnEndingYet is the ordering the dispatcher really
// produces on a call that was paced and then given up on, and it is the shape
// that would have a surface settle a row on a call that is still running.
//
// [Client.send] takes the pacing park back from a deferred call on its way out,
// which runs AFTER the row that ended the last attempt. So the park being taken
// back speaks while an ending is already latched, and the ending itself is said
// once, at the end, by the door the request returns through.
func TestALatchedEndingIsNotAnEndingYet(t *testing.T) {
	log := &progressLog{}
	progress := newCallProgress(log.watch, "openrouter/x")
	began := time.Now()
	progress.opened(0, began)
	progress.paced(0, true, began.Add(time.Second))
	progress.landed(0, CallEndRefused, errors.New("no"))
	progress.paced(0, false, began.Add(2*time.Second))
	progress.finished()
	progress.finished()

	seen := log.all()
	endings := 0
	for _, report := range seen {
		if report.Phase == CallEnded {
			endings++
			continue
		}
		if report.End != "" || report.Err != nil {
			t.Fatalf("a %q report carried the ending %q (%v) — only the last report may", report.Phase, report.End, report.Err)
		}
	}
	if endings != 1 {
		t.Fatalf("%d endings, want exactly one however many attempts landed", endings)
	}
	last := seen[len(seen)-1]
	if last.Phase != CallEnded || last.End != CallEndRefused || last.Err == nil {
		t.Fatalf("the call ended %q/%q (%v), want the last attempt's own refusal", last.Phase, last.End, last.Err)
	}
}

// TestAStragglerFromACancelledArmCannotSpeakAfterTheEnding is the race the
// reporter really runs in, and the reason the ending LATCHES rather than merely
// clearing a flag.
//
// A race's losers are cancelled by the deferred stop in [hedgeRace.run] and are
// still running when the door above says the question is over. A frame already
// sitting in a loser's SSE decoder reaches this a moment later: a late note
// would repaint a settled row as running, and a late opened would re-open a
// report nothing will ever close again — so "said exactly once" would be true of
// the code and false of the build.
func TestAStragglerFromACancelledArmCannotSpeakAfterTheEnding(t *testing.T) {
	log := &progressLog{}
	progress := newCallProgress(log.watch, "openrouter/x")
	began := time.Now()
	progress.opened(0, began)
	progress.note(0, began.Add(time.Second), 4, 0)
	progress.landed(0, CallEndAnswered, nil)
	progress.finished()

	settled := len(log.all())
	// Everything a loser's goroutine could still do on its way down.
	progress.note(1, began.Add(2*time.Second), 9, 0)
	progress.serving(1, "B")
	progress.opened(1, began.Add(3*time.Second))
	progress.paced(1, true, began.Add(4*time.Second))
	progress.landed(1, CallEndCancelled, nil)
	progress.finished()

	if after := len(log.all()); after != settled {
		t.Fatalf("a straggler spoke %d times after the ending", after-settled)
	}
	last, _ := log.last()
	if last.Phase != CallEnded || last.End != CallEndAnswered {
		t.Fatalf("the last word is %q/%q, want the ending to have stood", last.Phase, last.End)
	}
	endings := 0
	for _, report := range log.all() {
		if report.Phase == CallEnded {
			endings++
		}
	}
	if endings != 1 {
		t.Fatalf("%d endings, want exactly one — that is the promise the seam makes", endings)
	}
}

// TestACallThatWalksToAnotherMachineEndsOnce is the same law through the wire,
// and it is the one a reader of this seam would otherwise get wrong: the log
// writes a closing row for every attempt, so a call refused by its first machine
// and answered by its second must report going out TWICE and ending ONCE.
func TestACallThatWalksToAnotherMachineEndsOnce(t *testing.T) {
	rig := newLaneRig(t, "progress/walk",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{FailWith: 500}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 12}},
	)
	rig.patience(t, 5*time.Second)

	log := &progressLog{}
	ctx := WithCallProgress(talking(), log.watch)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	starts, endings := 0, 0
	for _, report := range log.all() {
		switch report.Phase {
		case CallStarted:
			starts++
		case CallEnded:
			endings++
		}
	}
	if starts < 2 {
		t.Fatalf("%d reports of the request going out, want one per machine it was sent to", starts)
	}
	if endings != 1 {
		t.Fatalf("%d endings, want exactly one — an attempt ending is not the call ending", endings)
	}
	last, _ := log.last()
	if last.End != CallEndAnswered {
		t.Fatalf("the call ended %q, want the machine that answered to have the last word", last.End)
	}
}
