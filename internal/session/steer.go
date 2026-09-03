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
// immediately without killing its process. An unmistakable stop phrase adopts
// and kills that job, because preserving work the person just rejected would be
// the harness overruling them.
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
//	           refusal ([Agent.enqueueSteeredLine] answers false) and never a
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
	"syscall"
	"time"
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

// activeGeneration is one provider attempt and its independent stop handle.
// The pointer is its identity: an attempt may finish while the next one starts,
// and only the attempt that installed a handle is allowed to clear it.
type activeGeneration struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}

func (a *Agent) beginGeneration(parent context.Context) (context.Context, *activeGeneration) {
	ctx, cancel := context.WithCancelCause(parent)
	active := &activeGeneration{ctx: ctx, cancel: cancel}
	a.mu.Lock()
	a.generation = active
	a.mu.Unlock()
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
	// A MODEL GENERATION IS CUT, NOT THE TURN. The request's own cancellation
	// handle is distinct from a.cancel, so the loop comes back to its boundary,
	// records only what arrived, drains this steer and continues on the same
	// stream. There is no call to Interrupt here and therefore no follow-up drop.
	if a.generation != nil {
		steer.note.Landing = "stopped the reply here"
		a.generation.cancel(errSteerCut)
	} else if landed, jobs := a.steerRunningBashLocked(words); landed != "" {
		steer.note.Landing = landed
		defer func() {
			for _, started := range jobs {
				a.jobs.announceRow(started)
			}
		}()
	} else {
		// Short tools are allowed to finish. The line is still visible now, and
		// this clause says exactly why its consumed event has not arrived yet.
		steer.note.Landing = "waiting for the running step"
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

// steerRunningBashLocked handles foreground bash calls while Steer holds a.mu.
// The call and job registries have their own locks precisely so this input path
// can reach them while the turn is busy. Every old call is handled: adopting
// only one from a parallel batch would still leave the steer waiting on another.
func (a *Agent) steerRunningBashLocked(words string) (string, []*job) {
	calls := a.inFlightBash.snapshot()
	if len(calls) == 0 {
		return "", nil
	}
	stop := steerStopsBash(words)
	var ids []int
	var adoptedJobs []*job
	for _, call := range calls {
		if call.RunningFor() < steerBashAge {
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
				return steerPromotedSentence(one.id, call.Command(), call.RunningFor())
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
