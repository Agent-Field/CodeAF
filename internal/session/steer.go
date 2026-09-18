package session

// THE SPLICE: a sentence typed INTO the turn that is already running.
//
// A person asks for something, watches the work start, and sees it going the
// wrong way — the wrong directory, the wrong file, a fact the model does not
// have. Until this existed the only two answers were both wrong. Stopping the
// turn threw away work they had already paid for and watched arrive. Waiting for
// the answer and correcting it afterwards spent the whole rest of the turn on
// the wrong thing first.
//
// A STEER INTERRUPTS THE CURRENT GENERATION AND NOT THE TURN. The provider
// request stops where it is, the assistant text that actually arrived remains
// in the transcript, and no half-sent tool call is kept. The person's words then
// land as user content at that new step boundary and the SAME turn makes a fresh
// request. What the model reads is the original question, its work up to the
// cut, and then the correction, in the order they happened.
//
// ── WHERE IT LANDS, AND WHY THERE ──
//
// [Agent.runTurn]'s loop has exactly one legal place for a user message: a step
// boundary. Cancelling the request CREATES that boundary after a partial
// assistant message with no tool calls. During an ordinary tool batch it cannot:
// a user message between tool_calls and their results is a provider-invalid
// shape, so short tools finish first. A foreground bash call older than
// [steerBashAge] is adopted as a job instead, making its tool result available
// immediately without killing its process — and one still YOUNGER than that is
// looked at once more when it crosses the same bound (steer_grace.go), so the
// handoff waits on the command's age and never on the command's length. An
// unmistakable stop phrase adopts and kills that command at ANY age, because
// preserving work the person just rejected would be the harness overruling
// them, and so would making them wait out a grace that exists to let a command
// finish.
//
// ── PER-BOUNDARY, AND BATCHED WHEN THAT IS WHAT HAPPENED ──
//
// Each steer lands at the FIRST boundary after it was typed. Two typed in the
// same step therefore arrive together at that one boundary, as two consecutive
// user messages in the order they were sent; two typed a step apart arrive at
// two successive boundaries. That is one rule and not two: the queue is drained
// whole, under one lock, every time.
//
// It is deliberately not the other shape — one steer per boundary, the rest held
// back. Holding a correction the person has already sent, so that it can be
// spread over later steps, would be this loop deciding to delay them; and the
// step it was held out of is exactly the step it was meant to change.
//
// ── THE FALL-THROUGH LAW ──
//
// A boundary is not promised. A turn whose last request has already gone out has
// no next step, and a turn that is stopped or that faults has none either — so a
// steer can be waiting when the turn ends, having steered nothing.
//
// IT MUST NOT VANISH AND IT MUST NOT PRETEND. So it is neither dropped nor
// recorded into the turn it missed: it is lifted onto the follow-up queue
// (agent.go's [Agent.FollowUp]) — the session's own lane for a message waiting
// for a turn of its own, and the lane internal/tui3's held message copies its
// law from — and the record says it fell through
// ([sessionFile.appendSteerFellThrough]). The stream the person is holding is
// carried across with it, so one channel tells the whole story: the turn they
// steered, then the fall-through, then the turn their own words start.
//
// AND IT IS AN ORDINARY WAITING MESSAGE FROM THAT MOMENT, which includes the
// part nobody enjoys: a follow-up queued behind a turn that was INTERRUPTED is
// dropped, because a drain must never resurrect a turn somebody stopped
// ([Agent.nextFollowUpLocked]). A steer that falls through onto a stopped turn is
// dropped with it — said out loud on the stream first, written down in the
// record, and never silently. A person who pressed stop stopped everything they
// had said to that turn, which is what stop means.
//
// ── A STEER SENT WHEN NOTHING IS RUNNING IS REFUSED ──
//
// [ErrNothingToSteer], rather than quietly becoming an ordinary Submit. The
// caller had a plain send available and did not use it, so the honest answer is
// that there was nothing to steer — a surface that turned this into a normal
// turn would be answering a different question than the one that was asked.
//
// ── THIS IS NOT THE ROOM'S STEER, AND THE TWO STAY SEPARATE ──
//
// [Agent.SteerTask] (task_room.go) puts the person's words into a RUNNING NODE:
// a different agent, in a different worktree, with a transcript of its own, and
// the words arrive as that node's own steering — its runner's loop drains the
// same queue at the same kind of boundary. The two share the MECHANISM (this
// package's one steering lane, drained at a step boundary) and they share the
// VERB, and they are not the same act:
//
//	SteerTask  steers a NODE.  Another agent. Delivery is all it promises — the
//	           answer is "it arrived", or "it arrived and the node is parked".
//	           There is no fall-through, because a node that has finished is a
//	           refusal ([taskRoom.handIn] answers nobody) and never a
//	           queue.
//	Steer      splices THIS TURN. This agent, this conversation, this question.
//	           It carries an identity, three outcomes, and a record that says
//	           which of them happened.
//
// So a node steer keeps [userMessage.steered] and a turn splice keeps
// [userMessage.steer], and neither reads the other's mark. Merging them would
// mean one of the two lying about what it promises.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// ErrNothingToSteer is what [Agent.Steer] answers when no turn is in flight.
// Match it with errors.Is; the sentence is the honest one for a surface that
// has nothing better to say, and a surface with a key to name says so itself.
var ErrNothingToSteer = errors.New("nothing is running to steer")

// SteerNote is one steer as the three steer events carry it: which one it is,
// what was said, and when the person sent it.
//
// The ID is this session's own counter and not a provider's anything. It exists
// so a surface can pair an outcome with the row it drew on the acceptance
// without matching text — two identical corrections typed a second apart are two
// steers, and a surface that paired them by words would resolve the wrong row.
type SteerNote struct {
	ID      uint64
	Words   string
	At      time.Time
	Landing string
}

// steerBashAge is how old a foreground bash call must be when a steer arrives
// before waiting becomes the wrong bargain. Short commands finish their batch
// normally; a build, test suite or server past this one bound becomes a job so
// the person's correction can land without throwing the process away.
const steerBashAge = 3 * time.Second

var errSteerCut = errors.New("session: generation cut by steer")

// activeGeneration is one provider attempt, its independent stop handle, and
// what it has produced so far.
// The pointer is its identity: an attempt may finish while the next one starts,
// and only the attempt that installed a handle is allowed to clear it.
type activeGeneration struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	// reached is THE reading of what this request has put in front of the person,
	// shared with every other door that asks the same question (see
	// [reachedThePerson]).
	reached *reachedThePerson
}

// reachedThePerson is THE ONE READING of "has this request put anything in front
// of the person yet", and every door in this package that has to know asks this
// one. There were two, five lines apart in the same stream observer, and they
// disagreed about a thought: the recall's gate counted a reasoning delta and the
// person's word did not, with nothing anywhere saying that was meant.
//
// IT IS A STATE AND NEVER A DURATION, and the state is ON THEIR SCREEN. A
// request parked on a provider's pacing for thirteen minutes, one waiting on a
// machine that has said nothing, one walking a refusal chain: all the same thing
// to the person, because they have read nothing and there is nothing of theirs
// to lose. What ends it is anything DRAWN — a word of the answer, a word of
// visible thinking (#760 made a reasoning-only turn something a person watches:
// [Event] EventReasoning is drawn by internal/tui3's feed), or a tool row formed
// far enough to be spoken. Past that instant a silent re-ask takes something off
// a screen somebody is looking at, and no door in this package may do it.
//
// IT IS SCOPED TO THE ATTEMPT, not to the turn, and that is sound because every
// road out of an attempt withdraws what it drew: the loop empties the four
// buffers at the top of the next one and the room is told on EventRetrying
// (loop.go). So "reached the person" means what is in front of them NOW, which
// is the only reading either door actually wants.
type reachedThePerson struct{ seen atomic.Bool }

// drew is called from the turn's stream observer, on every delta of every kind
// that puts something on the page, so it must stay this cheap.
func (r *reachedThePerson) drew() {
	if r != nil {
		r.seen.Store(true)
	}
}

func (r *reachedThePerson) did() bool { return r != nil && r.seen.Load() }

// reset is the attempt boundary. It sits beside the four buffers the loop
// empties there, because it is the same fact about the same dead attempt.
func (r *reachedThePerson) reset() {
	if r != nil {
		r.seen.Store(false)
	}
}

// productive reports whether this request has produced ANYTHING A PERSON COULD
// USE, and it is the whole of what "unproductive" means in this package.
//
// A nil generation is nothing in flight, which is unproductive by the same
// reading: there is no request to lose.
func (g *activeGeneration) productive() bool {
	return g != nil && g.reached.did()
}

// beginGeneration installs one attempt's stop handle, and SPENDS A CUT THAT
// ARRIVED BEFORE THERE WAS ANYTHING TO CUT.
//
// That second half is the same lock doing the same job one moment earlier. A
// reading beside the work asks for a boundary by cutting the request in flight,
// and the request it means is the one this turn is about to make — so a cut that
// crossed the gap between one attempt ending and the next beginning was, before
// #956, delivered to nothing at all and the drawing behind it waited out a whole
// extra step. The gap is not small: the loop takes its boundary, parks on owed
// jobs, drains steering, guards the request size, compacts tool history and
// assembles the transcript before it reaches this line.
//
// AND IT SPENDS THE OWED CUT THROUGH THE ONE DOOR, never by reaching for the
// handle it is holding. [Agent.cutGeneration] is the only reader of a.generation
// that can tell a live attempt from one that returned a microsecond ago, and an
// installer that cancelled its own handle instead would be the second answer to
// "is there anything to cut" that door exists to prevent.
func (a *Agent) beginGeneration(parent context.Context, reached *reachedThePerson) (context.Context, *activeGeneration) {
	ctx, cancel := context.WithCancelCause(parent)
	active := &activeGeneration{ctx: ctx, cancel: cancel, reached: reached}
	a.mu.Lock()
	a.generation = active
	owed := a.cutOwed
	a.cutOwed = owedCut{}
	spend := owed.cause != nil && owed.turn == a.turnSeq
	a.mu.Unlock()
	if spend {
		a.cutGeneration(owed.cause)
	}
	return ctx, active
}

func (a *Agent) endGeneration(active *activeGeneration) error {
	a.mu.Lock()
	if a.generation == active {
		a.generation = nil
	}
	a.mu.Unlock()
	return context.Cause(active.ctx)
}

// cutGeneration is [Agent.Steer]'s own move, offered to the MACHINE: stop the
// request in flight without stopping the turn, so the loop comes back to its
// boundary and re-assembles.
//
// IT IS THE ONE DOOR, and that is why it is here rather than in each caller. A
// steer reaches into a.generation under a.mu because only the lock can tell a
// live attempt from one that returned a microsecond ago; a second reader writing
// the same two lines somewhere else is how a build ends up with two answers to
// "is there anything to cut". The readings in loop.go and checkpoint.go — a
// recall that landed before the first token, a mark that says hand this over —
// both come through here.
//
// It reports whether the cut will be ANSWERED — a live attempt stopped now, or an
// owed cut the next [Agent.beginGeneration] will stop — which is the fact a caller
// with ONE cut to spend has to have.
//
// AND A CUT THAT FINDS NOTHING TO CUT IS NOT ALWAYS A CUT THAT IS DONE WITH. Which
// it is depends on the reading behind it and is declared by the cause, not decided
// here (see [boundaryCut]): a recall's block is written into the transcript before
// the cut is raised, so the request being assembled carries it either way and
// there is genuinely nothing to do — while a mark's DRAWING rides no transcript at
// all. It exists to be spent at a boundary, the cut is the only thing that brings
// that boundary forward, and a cut dropped here left the drawing waiting out a
// whole extra step (#956). So that one is OWED rather than spent on nothing.
func (a *Agent) cutGeneration(cause error) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cutGenerationLocked(cause)
}

// cutGenerationLocked is the body of that door, for the callers that are already
// holding a.mu because the decision they are making needs the lock anyway — a
// steer settling its landing, a person's word reading whether the request has
// spoken. IT IS THE ONLY PLACE IN THIS PACKAGE THAT CANCELS A GENERATION, and
// generation_law_test.go fails the build on a second one.
func (a *Agent) cutGenerationLocked(cause error) bool {
	if a.generation == nil {
		if !opensABoundary(cause) {
			return false
		}
		a.cutOwed = owedCut{cause: cause, turn: a.turnSeq}
		return true
	}
	a.generation.cancel(cause)
	return true
}

// owedCut is a cut that arrived when there was nothing to cut, kept until there
// is ([Agent.beginGeneration] is the one place that spends it).
//
// IT BELONGS TO THE TURN THAT ASKED FOR IT, and the number is what says so. A
// drawing read against one turn's transcript has nothing whatever to say about
// the next thing a person types, so a cut this turn never got to spend is not
// spent on the turn after it — which is also what makes a reading that answers
// late, into a turn that has already stopped waiting for it, harmless by
// construction rather than by timing.
//
// AND ONLY ONE IS EVER OWED. Two boundary cuts inside one gap would be last-wins,
// which is right rather than lossy: they ask for the same thing — the next
// request cut so that a boundary arrives now — and one cut answers both.
type owedCut struct {
	cause error
	turn  uint64
}

// dropOwedCut withdraws a cut owed on a reading's behalf BECAUSE THE BOUNDARY IT
// EXISTED TO OPEN HAS ARRIVED WITHOUT IT.
//
// A cut is owed only to bring forward the moment a reading's answer can be spent.
// When the turn reaches that moment on its own — the loop arrives at a boundary
// and takes the answer there — the owed cut has nothing left to bring, and
// spending it would cancel the next request for a reading that has already been
// read. [sidecar.spent] keeps that from happening in almost every interleaving,
// by refusing to deliver an interruption for an answer a taker has claimed; this
// is the other side of the same fact, for the one instruction-wide window where
// the interruption claims first and the taker takes anyway. It is exact rather
// than racy: the take and the spend are both on the turn's own goroutine, so by
// the time [Agent.beginGeneration] could spend the cut, the take has happened.
//
// It is spelled by cause because the owe is: a reading withdraws its own cut and
// no other reading's.
func (a *Agent) dropOwedCut(cause error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cutOwed.cause != nil && errors.Is(a.cutOwed.cause, cause) {
		a.cutOwed = owedCut{}
	}
}

// The two machine cuts, beside [errSteerCut] which is the person's.
//
// NEITHER IS A FAILURE AND NEITHER IS SHOWN. A steer says "stop, I am typing";
// these say "what you are about to be sent has changed" and "somebody has read
// this turn and it is being handed over", and both are answered by the loop
// rather than reported to the person — see loop.go's THE ONLY WAIT A PERSON
// EXPERIENCES law for why the readings behind them are allowed to interrupt work
// and never to precede it.
var (
	errRecallCut = errors.New("session: generation cut to carry the recalled memories")
	errMarkCut   = boundaryCut{errors.New("session: generation cut by the mark's reading")}
)

// boundaryCut is a cut WHOSE REASON RIDES NO TRANSCRIPT, and it is a property of
// the reading rather than of the moment the cut happens to arrive — which is why
// it is written down beside the cause instead of being decided at the call.
//
// A cut is asked for because something the model is about to be sent has changed,
// or because a boundary is wanted NOW. The first kind is already recorded by the
// time it is raised and needs a live request or nothing; the second kind has
// nowhere else to live, so [Agent.cutGeneration] owes it rather than dropping it.
type boundaryCut struct{ error }

// opensABoundary reports the second kind. It reads the cause rather than a list
// of names, so a cut added tomorrow declares itself in the one place its cause is
// written and nothing here has to be edited to know about it.
func opensABoundary(cause error) bool {
	var boundary boundaryCut
	return errors.As(cause, &boundary)
}

// errPersonCut is the third of the person's own cuts, beside [errSteerCut]: a
// model they have just named reaching a request that has produced nothing they
// could use. The loop answers it by assembling the request again on the model
// they named, at once and with nothing said about it — there is no failure here
// and nothing was lost (loop.go).
//
// AND IT IS DELIBERATELY NOT A [boundaryCut]. The test that type states is
// whether the cut's REASON rides anything but the cut itself, and this one does:
// the model is written to [Agent.spokenModel] BEFORE the cut is raised
// ([Agent.hearModelLocked]) and the request boundary takes it from there
// ([Agent.takeModelWord]), exactly as a recall's block is in the transcript
// before its cut. So a person's word that arrives between two requests needs no
// cut at all — the request being assembled already carries it — and owing one
// would cancel a request that was about to be right, which costs them a whole
// send for no change.
//
// AND THE LANDING IS THE SECOND REASON, which is this cause's own. Owing the cut
// would make [Agent.cutGeneration] answer `true` where nothing was cut, and that
// answer is what the room says out loud: the person would read `switching now`
// over a pick that is in fact riding the next request. A cut that reports itself
// answered when it changed nothing is worse here than a cut that is dropped.
var errPersonCut = errors.New("let go of because you chose another model")

// ── THE PERSON'S WORD WINS, AND IT WINS AT THE REQUEST ──────────────────────
//
// THE LAW. A model a person names reaches the work within [lane.SpokenWithin].
// If the request in flight has produced nothing they could use
// ([activeGeneration.productive]) it is CUT and asked again on the model they
// named; if their answer is already arriving, that answer finishes and the NEXT
// request carries the new model. Never the next TURN: a task step is one turn
// and can be twenty minutes long, and a person watching a step wait thirteen
// minutes on a pace they cannot see is a person whose word did nothing.
//
// THE MEASURED FAILURE (2026-09-11, 14:40:10). A task step sat on `waiting ·
// rate limited · 13m 37s`. The owner picked another model in the room and typed
// `continue`; the room answered `its next turn takes it`, and the step was still
// talking to the model they had moved off at 14:41:00. The pick was real and
// landed on the agent — what was missing was anything to make the request in
// flight let go of it.

// ModelLanding is WHEN a model a person just named reaches the work, and it is
// the only thing a surface has to know to say something true about the pick.
type ModelLanding string

const (
	// ModelLandsNow is the request in flight let go of, because nothing of it had
	// reached the person, and the same step asking again on the new model.
	ModelLandsNow ModelLanding = "now"
	// ModelLandsNextRequest is the answer already arriving being allowed to
	// finish, with everything the work asks for after it on the new model. It is
	// also what a pick lands as when no request is out at all.
	ModelLandsNextRequest ModelLanding = "next-request"
)

// hearTheWordLocked is THE ONE DOOR a person's word reaches work already in
// flight through, whatever the word was. The caller has already RECORDED it —
// a model on [Agent.spokenModel], a direction on the worker's own queue — and
// this is the other two thirds: ask whether the request in flight has reached
// the person, and let go of it when it has not, so the loop comes back to a
// boundary and assembles the next request carrying what they said.
//
// ONE DOOR BECAUSE IT IS ONE RULE. A model, a `continue`, a `stop`: the product
// rule the owner ruled on 2026-09-11 gives all three the same clock and the same
// question, and a second reading of it somewhere else is how the model half came
// to work while the direction half still waited out thirteen minutes.
//
// THE CAUSE IS THE CALLER'S BECAUSE IT NAMES WHICH BOUNDARY THE WORD NEEDS, and
// that is the one thing that genuinely differs between two words. A model
// changes what the NEXT REQUEST is sent to, so [errPersonCut] is answered at the
// request boundary inside the ladder and the same step simply asks again. Words
// change what the next STEP is sent, so a direction takes [errSteerCut] — the
// conversation's own splice, unchanged — which returns through the turn loop to
// the step boundary where queued lines are drained. A direction cut to the
// request boundary would re-send the identical messages and the person's line
// would still be sitting in the queue.
//
// a.mu is held: the productive reading and the cut have to be one decision, or a
// first token arriving between them cuts an answer somebody had started reading.
func (a *Agent) hearTheWordLocked(cause error) ModelLanding {
	if !a.running {
		return ModelLandsNextRequest
	}
	if a.generation.productive() {
		return ModelLandsNextRequest
	}
	if !a.cutGenerationLocked(cause) {
		// A turn between two requests has nothing to cut and needs none: the word
		// is taken when it assembles the next one.
		return ModelLandsNextRequest
	}
	return ModelLandsNow
}

// hearTheWord is that door for the callers not already holding a.mu — a room
// handing a direction to the worker inside it.
func (a *Agent) hearTheWord(cause error) ModelLanding {
	if a == nil {
		return ModelLandsNextRequest
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hearTheWordLocked(cause)
}

// hearModelLocked records the model and then hears it, which is the whole of
// what a pick is.
func (a *Agent) hearModelLocked(model string) ModelLanding {
	if !a.running {
		// NOTHING IS OWED WHEN NOTHING IS RUNNING. The next turn latches a.model,
		// which this caller has already set, so a word left here would be a word
		// the turn after would take a second time and re-ask on the model it is
		// already talking to.
		return ModelLandsNextRequest
	}
	a.spokenModel = model
	return a.hearTheWordLocked(errPersonCut)
}

// latchTheModel is the model a turn starts on, TAKING THE PERSON'S WORD WITH IT.
// See the law above for why the word must not outlive the turn it was said to.
func (a *Agent) latchTheModel() string {
	return a.latchModelAs("")
}

// latchModelAs starts a role-bound turn on its named seat without changing the
// conversation model. Empty keeps the ordinary conversation seat.
func (a *Agent) latchModelAs(model string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.spokenModel = ""
	if model == "" {
		model = a.model
	}
	a.riding = model
	return model
}

// rideModel publishes THE MODEL THE WORK IS ACTUALLY TALKING TO. The ladder has
// always known it — it is the local the rescue chain moves — and until now
// nothing outside the loop could read it, so the door that decides whether a
// pick is news compared against [Agent.model], which is the SESSION's model and
// not the step's.
//
// TWO FACTS, NOT ONE, and the difference is the whole bug it closes. A step that
// has been rescued onto a fallback is riding F while the session still says M.
// A person picking M is then picking a model the work is NOT on — real news that
// the old comparison read as none. A person picking F is picking the model the
// work is already on — no news at all, which the old comparison read as a change
// and spent a whole request cutting for nothing, under a room line that said
// `switching now`.
func (a *Agent) rideModel(model string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.riding = model
}

// ridingNowLocked is that fact when there is a step to have it, and the
// session's own model when there is not — a pick made between two turns is news
// exactly when it differs from the model the next turn would latch.
func (a *Agent) ridingNowLocked() string {
	if a.running && a.riding != "" {
		return a.riding
	}
	return a.model
}

// takeModelWord is the step asking whether the person has named a model since
// the last request went out, and TAKING it: a word taken twice would restart a
// budget the step is already spending on it.
//
// TWO ROADS TAKE IT AND THEY ARE THE SAME DOOR. The request boundary takes it
// when the step is simply making its next request; the rescue chain takes it
// when a failure has decided the step must move, because a hop that announced
// the ladder's next rung and was then overruled at the boundary would have named
// a model the reply never went to — and a sentence a person reads has to name
// where their answer actually went. Only one of the two can win, because the
// first to take it leaves nothing behind.
func (a *Agent) takeModelWord() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	word := a.spokenModel
	a.spokenModel = ""
	return word, word != ""
}

// peekModelWord is the same question WITHOUT taking the answer. Two readings
// need it and neither may consume: whether this step has anywhere left to go —
// which runs on every failure, including the ones that go on to ask the same
// model again, so a word taken there would be a word swallowed by a move that
// never happened — and what to NAME in the line a person reads when their own
// word is what let go of the request.
//
// A PERSON WHO HAS NAMED A MODEL IS SOMEWHERE LEFT TO GO. A step with an empty
// chain used to end the turn on "there is nowhere else to try" while the model
// they had just chosen sat unasked.
func (a *Agent) peekModelWord() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.spokenModel, a.spokenModel != ""
}

// turnSteer is one steer as the AGENT holds it while it waits: the note the
// events carry, and the stream the person who sent it is reading.
//
// The stream is here rather than in [SteerNote] because it is machinery and the
// note is a fact — the note crosses the package boundary onto a surface and the
// stream never does. It is what makes the fall-through one continuous story:
// the same stream is a subscriber of the turn being steered, and then, if that
// turn ends first, the subscriber of the turn the person's own words start.
type turnSteer struct {
	note   SteerNote
	stream *eventStream
}

// steerMessage is the queue entry a steer rides in. It is an ordinary user
// message on the ordinary steering lane, with the slip on it — so every drain,
// every ordering rule and every transcript shape it meets is the one that was
// already there, and this file adds only what happens at the two ends.
func steerMessage(steer *turnSteer) userMessage {
	return userMessage{message: textMessage("user", steer.note.Words), steer: steer}
}

// Steer puts one sentence into the turn that is running.
//
// The returned channel is a live view of that turn from this moment on, exactly
// as a steering [Agent.Submit]'s is: the caller watches what its correction does
// rather than being told it was queued and left staring at nothing. Three
// events on it are this steer's own — [EventSteerAccepted] at once, then
// [EventSteerConsumed] when the model is given the words, or
// [EventSteerFellThrough] when the turn ends before a boundary comes.
//
// ON A FALL-THROUGH THE SAME CHANNEL CARRIES ON, into the turn the words then
// start of their own accord. That is the whole reason the stream is built here
// rather than taken from the hub: a subscription belongs to one turn and closes
// with it, and a person whose correction arrived one step too late is owed the
// answer to it on the channel they are already holding, not on a second one they
// would have to know to ask for.
//
// [ErrNothingToSteer] when no turn is in flight — the caller should have sent
// the message normally, and this refuses rather than silently becoming that.
//
// WORDS ONLY. Pictures reach a running turn by their own door
// ([Agent.SubmitImage], image.go), which assembles parts, reads files and
// journals durable references; a steer that took attachments would be a second
// copy of that assembly, and it is not cheap enough to carry along. A surface
// with a picture to add sends it that way, and it lands at the same boundary.
func (a *Agent) Steer(words string) (<-chan Event, error) {
	words = strings.TrimSpace(words)
	if words == "" {
		return nil, errors.New("session: empty message")
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, errors.New("session: agent is closed")
	}
	if !a.running || a.hub == nil {
		a.mu.Unlock()
		return nil, ErrNothingToSteer
	}
	steer := &turnSteer{
		note:   SteerNote{ID: a.nextSteerID(), Words: words, At: time.Now()},
		stream: newEventStream(),
	}
	a.steering = append(a.steering, steerMessage(steer))
	hub := a.hub
	// A QUESTION THE PERSON TALKED PAST IS LET GO OF BEFORE ANYTHING ELSE IS
	// DECIDED. `ask` parks the turn on the person, so a correction queued behind
	// it waits for a step that is itself waiting for the correction — which is
	// not a slow boundary but no boundary at all (steerquestion.go states the
	// measurement). Retiring it here opens the boundary this splice needs.
	//
	// IT IS ASKED BEFORE THE THREE ARMS BELOW AND CANNOT COLLIDE WITH THEM. A
	// parked `ask` holds the whole tool batch, so no request is out while one
	// stands — `a.generation` is nil and the first arm is unreachable — and the
	// batch it holds is the one no bash can be running inside. The arm this
	// really replaces is the last one, and that is where its landing is set.
	retired := a.talkedPastQuestionsLocked()
	if len(retired) > 0 {
		// Outside the lock, with the announcements, for the reason they are:
		// this is person-visible news and the claim above is not.
		defer a.sayTheQuestionsCameDown(retired)
	}
	// A MODEL GENERATION IS CUT, NOT THE TURN. The request's own cancellation
	// handle is distinct from a.cancel, so the loop comes back to its boundary,
	// records only what arrived, drains this steer and continues on the same
	// stream. There is no call to Interrupt here and therefore no follow-up drop.
	if a.cutGenerationLocked(errSteerCut) {
		steer.note.Landing = "stopped the reply here"
	} else if landed, jobs := a.steerRunningBashLocked(words, a.inFlightBash.snapshot()); landed != "" {
		steer.note.Landing = landed
		if steerStopsBash(words) {
			// A STOP HAS DEALT WITH EVERY COMMAND IN FLIGHT, at every age, so
			// there is nothing left to come back to — and a watch armed by an
			// earlier correction is released rather than left to fire into the
			// wreckage of commands this one just ended (steer_grace.go).
			a.stopSteerGraceLocked()
		} else {
			// AND A SIBLING STILL TOO YOUNG TO ADOPT IS COME BACK TO. Adopting
			// one call out of a parallel batch leaves the step waiting on the
			// rest of it, so this road arms the second look for the same reason
			// the one below does (steer_grace.go).
			a.armSteerGraceLocked()
		}
		defer func() {
			for _, started := range jobs {
				a.jobs.announceRow(started)
			}
		}()
	} else if len(retired) > 0 {
		// AND A QUESTION IS NOT A STEP THAT FINISHES ON ITS OWN, so this landing
		// is not the one below. The person's sentence is what ended it, which is
		// what they are told (steerquestion.go).
		steer.note.Landing = steerTookTheQuestion
	} else {
		// Short tools are allowed to finish. The line is still visible now, and
		// this clause says exactly why its consumed event has not arrived yet.
		steer.note.Landing = "waiting for the running step"
		// AND THE AGE IS MEASURED ONCE MORE WHEN IT CAN ANSWER DIFFERENTLY. A
		// command that is young now may be a build; the person's words must not
		// wait for its ending or for the background clock because of the instant
		// they were typed in (steer_grace.go).
		a.armSteerGraceLocked()
	}
	// Adopted under a.mu, for the reason a steering Submit subscribes under it:
	// the turn's goroutine clears running with this same lock held BEFORE it
	// closes the hub, so running == true here means the hub cannot already have
	// closed under us and the stream cannot be adopted onto a dead turn.
	hub.adopt(steer.stream)
	// AND THE ACCEPTANCE IS SENT UNDER THE SAME LOCK, which is what makes the
	// three events an order rather than a race. Both outcomes are sent with a.mu
	// held — the drain that consumes ([Agent.drainSteering]) and the lift that
	// lets go ([Agent.liftSteersLocked]) — so a promise released here before
	// either could not be overtaken by the answer to it. It costs nothing to
	// hold: [eventHub.send] is an append and a signal and never waits.
	//
	// It goes through the hub rather than onto the stream, so the pending
	// correction sits in this turn's backlog with everything else: a surface that
	// attaches mid-turn ([eventHub.attach]) needs to see what is about to change
	// the work as much as it needs the work.
	hub.send(Event{Kind: EventSteerAccepted, Steer: noteOf(steer)})
	a.mu.Unlock()
	return steer.stream.out, nil
}

// steerRunningBashLocked handles foreground bash calls while a.mu is held.
// The call and job registries have their own locks precisely so this input path
// can reach them while the turn is busy. Every old call is handled: adopting
// only one from a parallel batch would still leave the steer waiting on another.
//
// THE CALLS ARE PASSED IN RATHER THAN READ HERE, because the second look
// (steer_grace.go) may adopt only the calls it was armed for — a set it holds
// and this pass has no way to know. [Agent.Steer]'s own road passes the whole
// registry, which is the same question it used to ask itself.
func (a *Agent) steerRunningBashLocked(words string, calls []*bare.BashCall) (string, []*job) {
	if len(calls) == 0 {
		return "", nil
	}
	stop := steerStopsBash(words)
	var ids []int
	var adoptedJobs []*job
	for _, call := range calls {
		// THE AGE IS A BARGAIN ABOUT LETTING A SHORT COMMAND FINISH, and a stop
		// is the person saying they do not want it to. So a stop reaches a call
		// of any age at once — waiting three seconds to obey `stop` would be the
		// harness holding a cancellation the way it holds a correction — while
		// every other sentence still lets a young call have its few seconds and
		// is looked at again when they are up (steer_grace.go).
		if !stop && a.steerBashRunningFor(call) < steerBashAge {
			continue
		}
		var started *job
		var adopted bool
		// NEITHER ARM IS OWED, AND THAT IS THE DIFFERENCE FROM A CLOCK'S
		// PROMOTION. A promotion is a command the work is still waiting for, so
		// the work waits for its ending rather than being asked what to do next
		// (task_job_park.go). A steer is the PERSON redirecting the work: their
		// words are the next step, and a model made to wait for the command they
		// just talked over would be answering them minutes late — or, where they
		// stopped it, answering an ending nobody wants.
		if stop {
			started, adopted = a.adoptRunningBashAs(call, func(*job) string {
				return "stopped by the person: " + words
			}, adoption{quiet: true})
		} else {
			started, adopted = a.adoptRunningBashAs(call, func(one *job) string {
				return steerPromotedSentence(one.id, call.Command(), a.steerBashRunningFor(call))
			}, adoption{quiet: true})
		}
		if !adopted {
			continue
		}
		ids = append(ids, started.id)
		adoptedJobs = append(adoptedJobs, started)
		if stop {
			a.stopAdoptedBash(started)
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	sort.Ints(ids)
	if stop {
		return "stopped the running command", adoptedJobs
	}
	if len(ids) == 1 {
		return "kept bash running as job " + strconv.Itoa(ids[0]), adoptedJobs
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return "kept bash running as jobs " + strings.Join(parts, ", "), adoptedJobs
}

// steerStopsBash is intentionally tiny. Only unmistakable command-stopping
// phrases take the destructive arm; everything else preserves the process as a
// job and lets the model read the person's actual words before deciding more.
func steerStopsBash(words string) bool {
	normal := strings.ToLower(strings.TrimSpace(words))
	normal = strings.Trim(normal, ".!?")
	switch normal {
	case "stop", "stop it", "kill", "kill it", "cancel", "cancel it", "abort", "abort it", "ctrl-c", "ctrl+c", "^c":
		return true
	}
	return false
}

func steerPromotedSentence(id int, command string, elapsed time.Duration) string {
	command = strings.ReplaceAll(strings.TrimSpace(command), "`", "'")
	if command == "" {
		command = "bash"
	}
	return fmt.Sprintf("%s%d (`%s`, %s so far); output via jobs output %d; you will be told when it exits",
		BashPromotedLead, id, command, formatElapsed(elapsed), id)
}

// stopAdoptedBash marks the new job as deliberately stopped before signalling
// it, so its reaper never produces an exit note for news the person already
// supplied. SIGKILL is scheduled after the registry's one shared grace without
// making the steer wait through that grace.
func (a *Agent) stopAdoptedBash(started *job) {
	if started == nil || !started.requestKill() {
		return
	}
	started.signal(syscall.SIGTERM)
	go func() {
		if !waitDone(started.done, jobTermGrace) {
			started.signal(syscall.SIGKILL)
		}
	}()
}

// nextSteerID mints one steer's identity. Ids start at 1, so a zero [SteerNote]
// is recognisably not one.
func (a *Agent) nextSteerID() uint64 { return a.steerSeq.Add(1) }

// noteOf copies the note out for an event. The events cross into a surface's
// hands and the slip does not, so what rides them is a value nobody on the other
// side can hold a pointer into the session through.
func noteOf(steer *turnSteer) *SteerNote {
	note := steer.note
	return &note
}

// consumedSteer is the transcript's account of one steer that LANDED, and it is
// written at exactly the moment that becomes true.
//
// The drain in [Agent.runTurn] is the one that answers — it runs immediately
// before the next provider request, so a message it records is a message that
// request carries (see [Agent.drainSteering]) — which is why the mark is written
// there and not when the words were queued. A steer marked consumed at the
// moment somebody typed it would be a record of an intention.
func (a *Agent) consumedSteerLocked(hub *eventHub, user userMessage) {
	if user.steer == nil {
		return
	}
	hub.send(Event{Kind: EventSteerConsumed, Steer: noteOf(user.steer)})
}

// liftSteersLocked is the fall-through: every steer still waiting when the turn
// ends leaves the steering queue and becomes an ordinary message waiting for a
// turn of its own.
//
// It runs with a.mu held, from the turn's own cleanup, BEFORE the drain that
// records what is left ([Agent.drainSteeringLocked]) — which is the whole of why
// it is a separate pass. That drain writes the queue into the transcript of the
// turn that is ending, and a steer written there would be this build claiming
// the model was told something it never saw.
//
// The hub is still open here: the cleanup runs before [eventHub.close], because
// the deferred calls unwind in that order (agent.go's [Agent.startTurnLocked]).
// So the sentence a surface reads arrives before the stream it is reading ends,
// which is the only order in which it can be read at all.
//
// Everything else on the queue is left exactly where it was. A task's landing
// note, a job's exit and a line steered at a NODE all keep their own law and
// their own drain, and this pass is invisible to them. A watch delta is on the
// ambient boundary queue and never enters this pass at all.
func (a *Agent) liftSteersLocked(hub *eventHub) {
	if len(a.steering) == 0 {
		return
	}
	kept := a.steering[:0]
	var fell []userMessage
	for _, message := range a.steering {
		if message.steer == nil {
			kept = append(kept, message)
			continue
		}
		fell = append(fell, message)
	}
	if len(fell) == 0 {
		return
	}
	a.steering = kept
	for _, message := range fell {
		steer := message.steer
		hub.send(Event{Kind: EventSteerFellThrough, Steer: noteOf(steer)})
		// THE RECORD SAYS IT FELL THROUGH, and it is its own line rather than a
		// mark on a message, because there is no message to mark: these words are
		// not in this turn's transcript and never were. What replays into the
		// conversation is the ordinary question the follow-up below asks a moment
		// later, which is what actually happened; this line is the part of the
		// truth the conversation alone cannot tell.
		a.file.appendSteerFellThrough(steer.note)
		// The stream is taken off the dying turn WITHOUT being closed and handed
		// to the follow-up as its own, so the person keeps one channel across the
		// seam. [eventHub.drop] is the wrong door for this — it ends the reader —
		// and it is the only other way off a hub.
		hub.release(steer.stream)
		a.followups = append(a.followups, followUp{
			message: userText(steer.note.Words),
			stream:  steer.stream,
		})
	}
}
