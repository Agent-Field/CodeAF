package session

// A SPLIT DRAWING REACHES THE BOUNDARY ITS OWN CUT OPENED (#956).
//
// A mark's drawing rides beside the next step and its one power over the work is
// the interruption: cut the request in flight so that the boundary where the
// drawing can be SPENT arrives at once rather than whenever the step happens to
// end (checkpoint.go's [markAside], sidecar.go). Two orderings took that promise
// away, and both ended the same way — the turn paid for a whole extra step, and
// where that step was its last the drawing was written down as a carry-on and
// nothing was handed anywhere:
//
//   - the answer was published only once the interruption had been DELIVERED, so
//     the boundary the cut bought could read the reading as still in flight;
//   - a cut that arrived between two steps found nothing to cut and was dropped,
//     though a drawing rides no transcript and has nowhere else to be spent.
//
// The tests below are the two orderings, both directions of the claim that keeps
// an interruption and a taker from both happening, the properties that keep an
// owed cut from reaching anybody else's turn or outliving the boundary it was
// owed for, and the journal row on the one road where a drawing genuinely can
// land too late.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE FIRST ORDERING, at the mechanism. A reading whose interruption has been
// delivered is takeable AT ONCE: that instant is the boundary the interruption
// exists to buy, and [sidecar.take] is non-blocking by construction, so a door
// that published its answer afterwards was a door whose own cut bought nothing.
func TestADrawingIsTakeableAtTheBoundaryItsOwnCutOpened(t *testing.T) {
	delivered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)

	side := readBeside(context.Background(),
		func(context.Context) int { return 7 },
		func(answer int) {
			// The interruption is raised here. From this instant the step it cut is
			// unwinding and the loop is on its way to the boundary below — which is
			// the whole of what a mark's `act` is bought for.
			close(delivered)
			<-release
		})
	<-delivered

	answer, ok := side.take()
	if !ok {
		t.Fatal("the boundary the interruption opened read the reading as still in flight — " +
			"the cut was paid for and bought nothing (#956)")
	}
	if answer != 7 {
		t.Errorf("the boundary took %d, want the answer the reading landed with", answer)
	}
}

// AND THE CLAIM IS WHAT THE ORDER IS FOR. Publishing the answer first would be a
// straight trade — one dropped drawing for one interruption delivered into
// whatever the turn does next — if nothing stopped the second. [sidecar.spent] is
// what stops it: a taker that has claimed an answer has already reached the
// boundary the interruption exists to buy, so there is nothing left to bring
// forward and the interruption is never delivered.
//
// This is the suppression direction, which is the whole reason the claim exists.
// It is deterministic: the reading is held on a channel until after the claim,
// and the watch that [readBeside] tells about its own landings is what says the
// reading is done rather than a guess about the scheduler.
func TestAnAnswerAlreadyClaimedAtABoundaryRaisesNoInterruption(t *testing.T) {
	watched := withBesideWatch(context.Background(), &besideWatch{})
	release := make(chan struct{})
	var raised atomic.Bool

	side := readBeside(watched,
		func(context.Context) int {
			<-release
			return 7
		},
		func(int) { raised.Store(true) })

	// The boundary arrives first: the turn claims this answer for itself.
	side.claim()
	close(release)
	besideWatchOn(watched).quiet(watched)

	if raised.Load() {
		t.Error("the reading interrupted the work for an answer a boundary had already claimed — " +
			"a cut raised there lands on whatever the turn does next")
	}
	if answer, ok := side.take(); !ok || answer != 7 {
		t.Errorf("the claimed answer took as (%d, %v), want the reading's own answer: suppressing the "+
			"interruption must not cost the drawing", answer, ok)
	}
}

// AND A CUT OWED FOR A DRAWING THE TURN HAS SINCE TAKEN IS OWED NO LONGER. The
// claim above is one instruction wide, so an interruption can still win it while
// a taker at that same boundary takes anyway. Nothing is lost when that happens —
// the drawing is in hand — but the cut left behind would stop the next step for a
// reading that has already been read, which before #956 was a harmless drop.
// [Agent.dropOwedCut] is the other side of the claim, and it is exact rather than
// racy: the take and the spend are both on the turn's own goroutine.
func TestACutOwedForADrawingTheTurnHasTakenIsNotSpent(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	if !agent.cutGeneration(errMarkCut) {
		t.Fatal("the cut was not owed at all")
	}
	// The boundary arrived without it: the drawing was taken here.
	agent.dropOwedCut(errMarkCut)

	ctx, generation := agent.beginGeneration(context.Background())
	defer agent.endGeneration(generation)
	if ctx.Err() != nil {
		t.Errorf("the next step was cut for a drawing the turn had already read: %v", context.Cause(ctx))
	}
}

// AND ONE READING DOES NOT WITHDRAW ANOTHER'S CUT. The owe is spelled by cause
// because a drawing that reached its boundary says nothing about a cut somebody
// else is still waiting to spend.
func TestADrawingWithdrawsOnlyItsOwnOwedCut(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	agent.cutGeneration(errMarkCut)
	agent.dropOwedCut(errors.New("session: somebody else's reading"))

	ctx, generation := agent.beginGeneration(context.Background())
	defer agent.endGeneration(generation)
	if ctx.Err() == nil {
		t.Error("another reading's boundary threw away the cut this drawing is still owed")
	}
}

// THE SECOND ORDERING, at the one cut door. A drawing that lands between one
// step ending and the next beginning asks for a boundary while there is nothing
// to cut; the cut is OWED to the request the turn is about to make rather than
// dropped, because a drawing rides no transcript and the boundary is the only
// place it can ever be spent.
func TestACutAskedBeforeTheRequestExistsIsSpentByIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	if !agent.cutGeneration(errMarkCut) {
		t.Fatal("a drawing that asked for a boundary between two steps was told its cut was spent " +
			"on nothing; the drawing then waits out a whole extra step (#956)")
	}
	ctx, generation := agent.beginGeneration(context.Background())
	if ctx.Err() == nil {
		t.Fatal("the request the owed cut was kept for went out uncut")
	}
	if cause := agent.endGeneration(generation); !errors.Is(cause, errMarkCut) {
		t.Errorf("the cut arrived as %v, want the mark's own cause so the loop takes it to the boundary", cause)
	}
}

// AND A CUT WHOSE REASON IS ALREADY IN THE TRANSCRIPT IS NOT OWED. The recall's
// block is recorded before its cut is raised, so the request being assembled
// carries it either way — and a cut kept for that request would throw away a
// request that was already right, which is the whole reason the two cuts declare
// themselves differently (steer.go's [boundaryCut]).
func TestACutWhoseReasonRidesTheTranscriptIsNotOwed(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	if agent.cutGeneration(errRecallCut) {
		t.Error("a recall with nothing to cut was told its one re-ask had been spent")
	}
	ctx, generation := agent.beginGeneration(context.Background())
	defer agent.endGeneration(generation)
	if ctx.Err() != nil {
		t.Errorf("the next request was cut for a block the transcript already carried: %v", context.Cause(ctx))
	}
}

// AND AN OWED CUT BELONGS TO THE TURN THAT ASKED FOR IT. A drawing read against
// one turn's transcript has nothing whatever to say about the next thing a person
// types, so a cut the turn never got to spend dies with it — which is also what
// makes a reading that answers into a turn that has stopped waiting for it
// harmless by construction rather than by timing.
func TestACutTheTurnNeverSpentIsNotSpentOnTheNextTurn(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	agent.cutGeneration(errMarkCut)
	// The next turn opens. [Agent.startTurnLocked] numbers every turn under this
	// same lock, and the number is the whole of the ownership.
	agent.mu.Lock()
	agent.turnSeq++
	agent.mu.Unlock()

	ctx, generation := agent.beginGeneration(context.Background())
	defer agent.endGeneration(generation)
	if ctx.Err() != nil {
		t.Errorf("the first request of the next turn was cut by the last turn's drawing: %v", context.Cause(ctx))
	}
}

// ── AND THE ONE ROAD WHERE A DRAWING GENUINELY LANDS TOO LATE ───────────────
//
// A turn that answers in words is over: there is no boundary after it and no cut
// can make one. The drawing is still waited for and still written down — a
// reading somebody paid for must not be a line nobody can count — and what it is
// written down as is what it SAID. The file called it a carry-on until #956, so a
// turn with independent parts left in it ended in words with the row recording
// the reader as having agreed to that.
func TestASplitDrawingLandingPastTheLastStepIsNotJournaledAsACarryOn(t *testing.T) {
	marks, ceilings := drawingHeldPastTheLastStep(t, checkpointSplitSketch)
	if len(marks) != 1 {
		t.Fatalf("%d mark rows, want the one the turn bought: %+v", len(marks), marks)
	}
	if marks[0].Decision != checkpointDecisionLate {
		t.Errorf("a drawing with independent parts in it, landed past the turn's last step, "+
			"journaled %q; want %q — %q is what the reader decided NOT to do (#956)",
			marks[0].Decision, checkpointDecisionLate, checkpointDecisionContinue)
	}
	// AND NOTHING WAS HANDED ANYWHERE, which is the honest half: the turn had
	// already answered, so the row says what was read and not that work moved.
	if len(ceilings) != 0 {
		t.Errorf("a turn that ended in words wrote %d handover rows: %+v", len(ceilings), ceilings)
	}
}

// AND THE CONTROL. A drawing that did not split lands in the same window and is
// still a carry-on, exactly as it always was.
func TestACarryOnDrawingLandingPastTheLastStepStillSaysContinue(t *testing.T) {
	marks, ceilings := drawingHeldPastTheLastStep(t, checkpointChainSketch)
	if len(marks) != 1 {
		t.Fatalf("%d mark rows, want the one the turn bought: %+v", len(marks), marks)
	}
	if marks[0].Decision != checkpointDecisionContinue {
		t.Errorf("a chain landed past the turn's last step journaled %q, want %q",
			marks[0].Decision, checkpointDecisionContinue)
	}
	if len(ceilings) != 0 {
		t.Errorf("a turn that ended in words wrote %d handover rows: %+v", len(ceilings), ceilings)
	}
}

// drawingHeldPastTheLastStep runs one grinding turn that reaches THE RUNAWAY NET
// and HOLDS THE DRAWING until the turn's last step has already answered, which is
// the one window where no boundary can follow. It returns the mark row the
// reading was bought for and the journaled handover rows.
//
// IT IS THE NET AND NOT THE FIRST MARK BECAUSE THE FIRST MARK BUYS NOTHING NOW
// (#923): the rungs below the last one tell the turn what it has run up and ask
// nobody anything, so the only reading a turn buys is the one at the net. The
// context is what takes it there — every scripted round reports a fuller window
// ([checkpointFillAt]) — and the note rungs' own rows are dropped below, because
// a row that cost no call is not the row this fixture is about.
//
// THE DRAWING IS RELEASED BY THE QUESTION A TURN'S END ASKS, and that is what
// makes this deterministic rather than a race: the remains reading is made after
// the turn's last generation has ended and its answer is what seals the turn, so a
// drawing released there cannot be taken at any boundary — there are none left.
func drawingHeldPastTheLastStep(t *testing.T, sketch string) ([]journalMark, []journalCeiling) {
	t.Helper()
	release := make(chan struct{})
	var loosen sync.Once
	var rounds atomic.Int64
	toolRounds := int64(checkpointMarkAt(checkpointMarks))

	// EVERY STEP ANSWERS BY SHAPE, so it does not matter which request rides which
	// slot — the drawing's own call takes one of them while it waits.
	answer := func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		switch {
		case askedForSketch(messages):
			select {
			case <-release:
				return textResponse(sketch), nil
			case <-ctx.Done():
				// The fixture consults step zero with a context that is already over to
				// see what it would draw ([answerTheReadingsOffTheQueue]); answering with
				// an error there puts the drawing back on the queue, where it can wait
				// without holding the completer's lock.
				return nil, ctx.Err()
			}
		case askedForRemains(messages):
			// The turn's last generation is behind us. The drawing lands from here,
			// into a turn with no boundary left to spend it at.
			loosen.Do(func() { close(release) })
			return textResponse(checkpointNothingLeft), nil
		case askedForHandoff(messages):
			return textResponse("Finish it\nwhat is left"), nil
		case askedToWriteHandoff(messages):
			return toolResponse("no-writer", "ls", `{"path":"."}`), nil
		}
		// A DIFFERENT PATH EVERY ROUND, so the turn is moved by its mark rather than
		// by the silent-turn ladder having something to say about repetition.
		round := rounds.Add(1)
		if round > toolRounds {
			return textResponse("done"), nil
		}
		return filling(toolResponseWithText(fmt.Sprintf("call-%d", round), "ls",
			fmt.Sprintf(`{"path":"./%d"}`, round), "Working through the next path."), int(round)), nil
	}
	steps := make([]step, checkpointMarkAt(checkpointMarks)+checkpointSlack+4)
	for index := range steps {
		steps[index] = answer
	}

	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, &scriptedCompleter{steps: steps}, func(config *Config) { config.SessionFile = path })
	stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	return marksThatBoughtAReading(journaledMarks(t, path)), journaledCeilings(t, path)
}

// marksThatBoughtAReading drops the note rungs' rows. They are journaled with
// [checkpointDecisionTold] and cost no call at all, so they are not rows about a
// drawing and cannot be rows about one landing late.
func marksThatBoughtAReading(marks []journalMark) []journalMark {
	kept := make([]journalMark, 0, len(marks))
	for _, mark := range marks {
		if mark.Decision != checkpointDecisionTold {
			kept = append(kept, mark)
		}
	}
	return kept
}
