package session

// The room: a task node as a PLACE the person can enter.
//
// Everything else in this slice treats a node as a thing that is handed off and
// reported on — the proposal, the countdown, the worktree, the report that
// arrives minutes later (task.go, task_run.go). That is the right shape for
// work you delegate and stop thinking about, and it is the wrong shape for the
// moment a person changes their mind: the node is running, they can see it is
// going the wrong way, and until now the only thing they could do about it was
// kill it and propose the corrected task again.
//
// A room is four doors on one running node, and no more:
//
//   - [Agent.WatchTask] — the LIVE stream of the child agent's own events, as
//     they happen: its deltas, its tool calls beginning and ending, its errors.
//   - [Agent.SteerTask] — the person's words into the child's steering lane,
//     the same lane a background job's exit note rides (agent.go).
//   - [Agent.RetargetTask] — the person's EXPLICIT pick of another model for
//     this node, from its next turn on.
//   - [Agent.TaskJournal] — the path to the node's whole transcript on disk.
//
// ── LIVE AND HISTORY ARE TWO LANES, FOR DECISION 19's OWN REASON ──
//
// The journal is the node's history and the room's stream is its present, and
// they are deliberately not the same door. A watcher subscribing to a running
// node gets what happens FROM NOW — no history is replayed, exactly as
// [eventHub.subscribe] replays none, because a stream that re-narrated a
// half-hour of somebody else's greps before reaching the live edge would make
// "watch this node" mean "read this node's history slowly". A person who wants
// the history opens the journal, which is a real session file and has been
// since the first node ran. It is the same split as the two update lanes: the
// turn's hub carries what is happening, [Agent.TaskUpdates] carries what
// landed, and neither is asked to be the other.
//
// WITH ONE SEAM BETWEEN THEM, AND IT IS NOT HISTORY. A message is journaled
// when it COMPLETES (agent.go's recordLocked), so the step the node is in the
// middle of is in neither lane: not on disk, because it has not finished, and
// not on the wire, because it happened before the person arrived. A watcher is
// therefore handed that one step on joining and nothing else — see
// [taskCatchup], which states exactly where the line is drawn.
//
// ── AND THIS STEER IS NOT THE CONVERSATION'S OWN ──
//
// There are two things in this package called steering and they are two acts,
// so read the one you meant. THIS file's steer is aimed at A NODE: another
// agent, in another worktree, with a transcript of its own, and the person's
// line arrives on that agent's steering queue and is drained at ITS next step
// boundary. What it promises is that the words are KEPT while the node is still
// running: "it arrived", "it arrived and the node is parked on its own pieces",
// or "there was nobody inside to read it and it is on the task's record"
// ([SteerReceipt]). A node that has FINISHED is a refusal and never a queue
// ([taskRoom.handIn] answers nobody), which is the one outcome that is not a
// receipt.
//
// [Agent.Steer] (steer.go) is the other one: a sentence SPLICED INTO THIS
// CONVERSATION'S RUNNING TURN, part of the question already being worked on. It
// carries an identity, three events, and a record that says whether the model
// actually read it or whether the turn ended first.
//
// They share the mechanism deliberately — one steering lane, drained at a step
// boundary, because that is the only legal place for a user message mid-turn —
// and they keep separate marks on [userMessage] (`steered` here, `steer` there)
// so that neither has to promise the other's outcome. steer.go states the split
// in full; nothing in this file reads that mark and nothing there reads this one.
//
// ── AND STEERING IS NOT ITSELF A REDIRECT ──
//
// Nothing in this file writes the goal. A steer is a line of TALK to the worker
// — "the config lives under etc/, not conf/" — arriving in the transcript as the
// person's own user-role message, which is exactly what it looks like from the
// child's side, and almost all of them stay talk.
//
// WHAT THIS FILE DOES IS RECORD WHO SAID IT. Every line said into a room is
// written onto the node as a DIRECTION with an id and a speaker (assignment.go),
// and one of those — the person's own, cited by the worker in an explicit call —
// can move what the work is judged by. The admitted `spec.brief` and
// `spec.acceptance` still never change; a revision is an overlay on them, at a
// version, carrying the person's verbatim words to whoever checks the work. The
// alternative was the contradiction this slice was written for: a worker told to
// follow the person's correction and then graded, by a checker reading the
// frozen text, for having followed it.
//
// A LINE FROM ANOTHER AGENT IS RECORDED THE SAME WAY AND MAY NOT DO THAT. The
// mechanics are one thing and authority is another, and only the person's own
// direction can move the target.
//
// ── THE ONE FIELD A ROOM DOES MOVE, AND WHY IT IS NOT THE SAME HOLE ──
//
// [Agent.RetargetTask] writes `spec.model`, and nothing else in the spec is
// writable from anywhere. The freeze it relaxes was never a freeze against the
// PERSON: it is a freeze against IMPLICIT DRIFT — a `/model` in the
// conversation silently moving work that was handed over before the switch,
// which is what task_person.go's StartTask exists to stop. An explicit pick
// made while standing in this node's room is the opposite of drift. It names
// one node, it is made by the person looking at that node's own page, and
// nothing else in the session moves with it.
//
// WHAT IS STILL FROZEN IS THE GOAL. `spec.brief` and `spec.acceptance` are what
// the work is graded against, so moving them mid-run would verify nothing;
// which model does the work is not part of that contract, any more than which
// model answered a turn is part of what a person asked for.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNobodyToRead marks the two refusals that mean the node is STILL RUNNING
// and simply has no reader inside it right now — mid-check, or landing. It is
// the fact a surface needs and cannot infer: "there is nobody in there" and
// "the work is over" are opposite things to offer a person, and only the engine
// knows which one it just said. Match it with errors.Is; the sentence to show
// is the refusal's own.
var ErrNobodyToRead = errors.New("nobody is in there to read your line")

// nobodyToRead carries one of those refusals while answering
// errors.Is([ErrNobodyToRead]). It keeps its OWN sentence rather than wrapping
// with %w, because the surface prints that sentence to the person and a wrap
// would append the sentinel's words to a line that already says them.
type nobodyToRead struct{ said error }

func (e nobodyToRead) Error() string { return e.said.Error() }
func (e nobodyToRead) Unwrap() error { return ErrNobodyToRead }

// SteerTask injects the person's words into a running node's loop — the same
// steering lane a job's exit note rides ([Agent.enqueueSteering]). Unknown id
// or a node that is not running is an error naming which.
//
// The note carries NO DECORATION. A job's exit is framed ("task 3 finished: …")
// because the model has to be told what kind of news it is; a person's line
// needs no frame, because from the child's side it is what it looks like — the
// person talking. Wrapping it would teach the node to read the person's words
// as a system event, which is the one thing they are not.
//
// THE RECEIPT THE ENGINE ADDS IS A SECOND MESSAGE AND NEVER A WRAPPER, for
// exactly that reason: the id a revision has to cite is a fact about the
// delivery rather than part of what was said, so it is said separately, in the
// engine's own voice ([taskRoom.steerIn]).
//
// ── AND A NODE THAT IS WAITING ON ITS OWN PIECES STILL HEARS IT ──
//
// The first answer is whether the node was WAITING when the line was taken: it
// has handed part of its work out, said everything it had to say, and parked on
// the reports (task_run.go's [TaskGraph.park]). Nothing about it looks different
// from outside — a parked node is a RUNNING node — but for the person it is the
// difference between an answer in a few seconds and one that reads as silence,
// so the surfaces say which it was in their own words rather than promising the
// same thing about two different waits.
//
// It is a fact and not a refusal, because the line does arrive: the parked
// runner is released by the enqueue below, wakes with the sentence on its queue
// and re-enters the model with it ([taskRoom.steerIn], [runTaskChild]).
// Before that it went onto a queue with nothing to drain it — held for as long
// as the slowest piece ran and dropped outright if the last report arrived
// first, while the room said it had arrived.
//
// A LINE NOBODY CAN READ ANY MORE IS A REFUSAL AND NEVER A DROP. A node whose
// worker closed in the instant between the state check and the enqueue — the
// last piece reported, the parent folded, the agent shut — cannot be talked to,
// and the person is told so in the same breath as every other "there is nobody
// in there".
//
// ── AND A LINE THE CHECK CANNOT BE SHOWN IS HELD, NOT REFUSED ──
//
// While the gate is reading the work there is nobody inside the node, and until
// this returned a receipt that was the end of the story: the person was refused
// and their words stayed in their own box. A correction sent in that window is
// exactly the one that matters — the work is about to land — so it is TAKEN and
// written onto the node's own record instead ([TaskNode.heardDirection]), and
// the landing revalidates against it before anything is published
// (assignment.go, task_ledger.go). [SteerReceipt.Held] is how a surface tells
// that apart from delivery, in the engine's own words.
func (a *Agent) SteerTask(id uint64, text string) (SteerReceipt, error) {
	return a.sayToTask(id, text, fromPerson, spokenSource{})
}

// ErrSendUnanswered marks a send whose fate the sender DOES NOT KNOW: the words
// may be on the node's record and may never have left the machine they were
// typed on, and nothing that can be read from here says which.
//
// IT IS NOT MINTED IN THIS PACKAGE. An engine in this process either takes a
// line or refuses it, and both of those are answers; the uncertainty belongs to
// the wire, so internal/remote wraps a call it got no answer to with this and a
// surface matches it with errors.Is. It lives here because it is the vocabulary
// a surface reads a send's outcome in, beside [ErrNobodyToRead], and because
// internal/tui3 speaks this package and not that one.
//
// A SURFACE MAY NOT DRAW THIS AS A FAILURE. The one honest reading is "we do not
// know", and what follows from it is asking again under the same [SteerSource]
// where the engine recognises one ([Agent.SteerRepeatKnown]), and keeping the
// person's words where they can still see them where it does not.
var ErrSendUnanswered = errors.New("no answer came back, so it is not known whether these words arrived")

// SteerSource NAMES ONE SEND, so that a surface which never heard the answer to
// it can ask again without the worker being told the same thing twice.
//
// THE PROBLEM IT ANSWERS IS NOT DUPLICATE TYPING. A person presses enter, the
// words cross to the engine, the engine takes them — and the answer is lost on
// the way back, because the link died, the deadline ran out, or the window was
// closed and reopened. The surface then holds a sentence it cannot say arrived
// and cannot say did not, and both of the things it can do are wrong: send it
// again and the worker reads one correction twice, drop it and the person's
// words are gone. With an identity on the send there is a third answer — ask
// again with the same one — and [taskAssignment.hear] recognises it, answers
// the receipt already on the record and delivers nothing ([SteerReceipt.Again]).
//
// IT IS THE SEND'S NUMBER AND NEVER A FINGERPRINT OF THE WORDS, which is
// [personSourceID]'s own law and matters more here than anywhere: the same
// sentence typed twice into a room IS two corrections — "try it again" after a
// failure means something the first one did not — so two intentional sends of
// one sentence carry two Seqs and are two directions, while one send asked
// twice carries one Seq and is one.
//
// AND THE SCOPE IS WHAT KEEPS TWO LIVES APART. Seq is only meaningful inside
// it: a surface counts its own sends from 1, so a scope shared with yesterday's
// window would let tomorrow's first send be recognised as a direction this task
// already holds and dropped. A surface mints one random scope per life and
// never persists it (internal/tui3's steersend.go).
type SteerSource struct {
	// Scope names one life of one surface. Empty is a send with no identity,
	// which is exactly what [Agent.SteerTask] has always been.
	Scope string
	// Seq counts that surface's sends, from 1.
	Seq uint64
	// At is when the person pressed enter, which is what the node's record
	// orders its corrections by — the send may be retried minutes later, and the
	// instant that matters is the one they said it at, not the one it landed on.
	At time.Time
	// Conversation is the session file the surface believed it was addressing.
	//
	// A TASK NUMBER MEANS SOMETHING ONLY INSIDE ONE CONVERSATION, and a surface
	// can hold a send across a swap — /resume and /new replace the conversation
	// under a handle that does not change (cmd/aforge's chatv3_host.go keeps the
	// same remote agent). Carried here, the claim is checked where the delivery
	// happens; empty is a caller making no claim, and is checked against nothing.
	Conversation string
}

// ErrNotThatConversation refuses a send whose conversation is no longer the one
// open. It is a REFUSAL AND NOT A DELIVERY ELSEWHERE: task 7 in the conversation
// that replaced it is somebody else's work, and the words stay unsent.
var ErrNotThatConversation = errors.New("that correction was written for another conversation, so it was not sent")

// ErrConversationUnchecked is the same refusal for a DIFFERENT reason: the
// engine cannot say which conversation it has open, so the claim on the send
// could not be established. It wraps [ErrNotThatConversation] because a caller
// has to treat them identically — nothing was delivered, and the words are
// still theirs. AN UNKNOWN OWNER IS NOT PERMISSION.
var ErrConversationUnchecked error = uncheckedConversation{}

type uncheckedConversation struct{}

func (uncheckedConversation) Error() string {
	return "this engine cannot say which conversation is open, so the correction was not sent"
}

func (uncheckedConversation) Unwrap() error { return ErrNotThatConversation }

// live says this identity can be recognised again. Both halves are required,
// for [personSourceID.live]'s reason.
func (s SteerSource) live() bool { return s.Scope != "" && s.Seq != 0 }

// spoken is the identity as the record keeps it. A send from a room is never
// forwarded, whatever it carries ([spokenSource.forwarded]).
func (s SteerSource) spoken() spokenSource {
	return spokenSource{id: personSourceID{scope: s.Scope, seq: s.Seq}, spoken: s.At}
}

// SteerTaskFrom is [Agent.SteerTask] with that identity on the send. Everything
// else about it is the same door: the same refusals, the same wake for a parked
// node, the same hold while the work is being checked, and the same receipt the
// worker cites to revise.
//
// A SEND WITH NO IDENTITY IS THE OLD DOOR EXACTLY. An empty [SteerSource] makes
// this [Agent.SteerTask], so a caller that has no way to number its sends is not
// made worse off — it simply cannot ask again safely, and the surface that
// cannot is the one that must keep the words and ask the person.
func (a *Agent) SteerTaskFrom(id uint64, text string, from SteerSource) (SteerReceipt, error) {
	// THE CONVERSATION IS CHECKED FIRST, because the id below is only meaningful
	// inside the one the caller meant ([SteerSource.Conversation]).
	if err := a.thisConversation(from.Conversation); err != nil {
		return SteerReceipt{}, err
	}
	if !from.live() {
		return a.SteerTask(id, text)
	}
	return a.sayToTask(id, text, fromPerson, from.spoken())
}

// thisConversation says whether this agent is the conversation the caller
// named, and why not. (The name is not `opened`: [Agent.opened] is a field.)
//
// NO CLAIM IS CHECKED AGAINST NOTHING; AN UNKNOWN IDENTITY IS NOT A MATCH. A
// caller that named no conversation is making no claim and steers as it always
// did. A caller that named one has asked to be refused unless this is that
// conversation — and an agent that does not know its own transcript CANNOT
// establish it, so it refuses. Reading "I have no name" as "your name is mine"
// would answer the question the claim was written to ask.
func (a *Agent) thisConversation(conversation string) error {
	conversation = strings.TrimSpace(conversation)
	if conversation == "" {
		return nil
	}
	mine := strings.TrimSpace(a.config.SessionFile)
	if mine == "" {
		return ErrConversationUnchecked
	}
	if filepath.Clean(conversation) != filepath.Clean(mine) {
		return ErrNotThatConversation
	}
	return nil
}

// SteerRepeatKnown says a send repeated under one [SteerSource] is taken once.
//
// IT IS ASKED BEFORE ANYTHING IS SENT, because the answer decides what a
// surface may do with a send it got no answer to: an engine that recognises the
// repeat can be asked again, and one that does not must not be — a second send
// there is a second correction, and the person would be the one to find out.
// This engine answers yes because this engine IS the record that recognises it
// ([taskAssignment.hear], and the checkpoint that survives a restart carries the
// identity with the direction — task_store.go's sourceRecord). A client
// speaking to another machine answers for THAT machine (internal/remote).
func (a *Agent) SteerRepeatKnown() bool { return true }

// relayToTask is THE OTHER SPEAKER, and it is not the person: the model calling
// `tasks … say` from this conversation or from a parent node
// (tools_tasks.go's [Agent.oneTask]).
//
// IT TAKES THE SAME ROAD AND NOT THE SAME AUTHORSHIP. Delivery is one thing — a
// line onto a running node's queue, read at its next step boundary, waking it if
// it was parked. Who said it is another, and it used to be lost here: the
// model's coordination came through [Agent.SteerTask], so the worker's journal
// drew it as the person's correction and "you may change the schema" read as
// authority nobody with authority had given. A descendant cannot raise its own
// authority by phrasing a request as an instruction, so the origin travels with
// the words ([relayNote] frames them) — and on the node's own record it is what
// keeps a relayed line from ever being cited to move the done-condition
// (assignment.go).
func (a *Agent) relayToTask(id uint64, text string) (SteerReceipt, error) {
	return a.sayToTask(id, text, fromAgent, spokenSource{})
}

// sayToTask is the one road all three doors take. The refusals are shared
// because they are facts about the NODE — unknown, settled, being checked,
// nobody in the room — and the origin decides only what the words arrive as and
// what they are allowed to do to what the work is judged by.
//
// source is the person's message a FORWARDED line was taken from
// (task_forward.go), and 0 for anything said straight into the node. It travels
// no further than the node's record, where it is what makes a repeated call the
// same instruction rather than a second one.
func (a *Agent) sayToTask(id uint64, text string, origin messageOrigin, source spokenSource) (SteerReceipt, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return SteerReceipt{}, errors.New("nothing to say")
	}
	node := a.taskNode(id)
	if node == nil {
		return SteerReceipt{}, fmt.Errorf("no task %d in this session", id)
	}
	// WHAT THIS TASK ALREADY HOLDS IS ASKED BEFORE WHETHER IT IS STILL RUNNING,
	// and the order is the point.
	//
	// A send whose answer was lost is asked about again LATER — after a
	// reconnect, after the window was reopened — and by then the work it was for
	// may have finished. Asked in the other order, that ask meets `task 7 is
	// done, not running`: a refusal, over words that are on the task's record and
	// were read by its worker. The person would be told their correction never
	// arrived when it did, and would send it again into the next piece of work.
	//
	// ── ONE NAMED CORRECTION AT A TIME PER NODE, AND WRITTEN DOWN BEFORE IT IS
	// ANSWERED FOR ──
	//
	// A named send promises exactly-once admission: the engine may be asked for
	// the same one again after a lost answer, and answers "already on the record"
	// (assignment.go's [taskAssignment.already]). That promise is only worth what
	// the RECORD is worth, and the record lived in memory — so an engine that died
	// between taking a correction and its next transition came back having
	// forgotten the identity, and the person's retry arrived as a second
	// correction. The words are written down as part of being admitted
	// ([admitDirection]).
	//
	// THE LOCK IS WHAT MAKES THAT TRUE UNDER TWO ASKS AT ONCE. Without it the
	// second ask reads an in-memory receipt whose write has not finished, or has
	// already failed and been rolled back, and reports a delivery that is on no
	// disk. It is the node's own and is never held while the graph's is
	// ([TaskNode.admit] states the ordering); an unnamed send takes it not at all
	// and behaves exactly as it always did.
	if source.id.live() {
		node.admit.Lock()
		defer node.admit.Unlock()
	}
	// So the RECORD answers first, because the record outlives the run
	// ([TaskNode.heardBefore]). A name this task has never heard falls straight
	// through to the state check exactly as before, which is every ordinary send.
	if source.id.live() {
		switch direction, order := node.heardBefore(source, text); order {
		case directionAgain:
			return SteerReceipt{Again: true, Direction: direction, Landing: steerAgainWord}, nil
		case directionMismatch:
			return SteerReceipt{}, errSaidUnderThatName
		}
	}
	if state := node.stateNow(); state != TaskRunning {
		return SteerReceipt{}, fmt.Errorf("task %d is %s, not running", id, state)
	}
	// THE CHECK HAS NO READER. The node is TaskRunning across the worker, the
	// check and every repair round — the state is honest about the node, never
	// about who is inside it — and during the check the worker agent is still
	// open while its reading is over, so a line enqueued now would be taken with
	// a receipt and read by nobody (#273: the room drew the person's question as
	// said while the gate was reading the tree, and the words died with the
	// worker). The runner withdraws the speaker on its way out (task_run.go's
	// [runTaskChild]); this is the same refusal said from the phase's side —
	// read off the node's own graph-locked word ([TaskNode.lifeNow]), never the
	// beat file, which is the same fact written for OTHER processes — so a
	// person asking during the check hears what the check is instead of a
	// sentence about a worker.
	//
	// SO IT IS HELD AGAINST THE WORK INSTEAD OF BEING SENT BACK. The words go on
	// the node's record as a direction nobody has read, and the landing may not
	// publish over one ([TaskNode.claimPublication]).
	if node.lifeNow() == TaskPhaseChecking {
		return heldForTask(node, text, origin, source)
	}
	// Read BEFORE the line is handed over, because handing it over is what ends
	// the wait: after the enqueue the honest answer to "was it waiting" has
	// already changed.
	waiting := node.waitingOnItsPieces()
	// The nil-check and the enqueue happen under the room's own lock
	// ([taskRoom.steerIn]) so the runner's withdrawal cannot slip between them.
	// One sentence covers every "nobody" — a worktree still being prepared, a
	// worker whose reading is over, a landing in progress — because they are the
	// same fact from the person's side, and naming the wrong end of the run
	// ("not started yet" about a task that is finishing) is worse than naming
	// neither.
	// The answer travels WITH the line rather than being asked for again at the
	// far end, because by the time the line is written down the node is no longer
	// parked — this is the enqueue that released it — and the record would then
	// say the ordinary thing about the one moment it was not true.
	// THE RECEIPT IS MINTED BEFORE THE DELIVERY because the worker cites it to fold
	// a direction into the assignment, and it rides on the queued line itself
	// (agent.go's [carryingDirection]) so that the drain which carries the words
	// into a request is what marks the direction read. A refusal below forgets it
	// again, so nothing is left on the record for words no reader took.
	//
	// AND IT IS ON THE DISK BEFORE IT IS HANDED TO ANYBODY ([admitDirection]): a
	// worker that had read a correction the checkpoint never heard of would leave
	// the person's retry looking like a new sentence.
	heard, err := admitDirection(node, text, directionOf(origin), source)
	if err != nil {
		return SteerReceipt{}, err
	}
	// AND WHAT THE RECORD SAID ABOUT WHERE THESE WORDS BELONG (assignment.go's
	// [taskAssignment.hear]). The same message forwarded to the same task again
	// answers the receipt already on the record and delivers nothing; a message
	// older than one this task already holds is refused rather than admitted
	// behind it, because the receipt it would take is a later id than the newer
	// correction's (task_forward.go).
	switch heard.order {
	case directionAgain:
		return SteerReceipt{Again: true, Direction: heard.id, Landing: steerAgainWord}, nil
	case directionOutOfOrder:
		return SteerReceipt{}, errSaidLaterAlready
	case directionMismatch:
		return SteerReceipt{}, errSaidUnderThatName
	}
	direction := heard.id
	if !node.openRoom().steerIn(conversationOf(node), a.spoken(text, waiting, origin, direction)) {
		// AND THE ONE INSTANT THE PHASE READ ABOVE CANNOT COVER. The worker's
		// reading can end between that read and this enqueue — the runner withdraws
		// the speaker as it leaves the fold (task_child_run.go) — and the node is
		// still RUNNING, with a check and a landing ahead of it. Sending the words
		// back there would drop a correction on the one work that can still use it,
		// so a refusal from a node that is still going is held exactly as the check
		// window is. A node that has settled since is the honest refusal it always
		// was.
		if node.stateNow() == TaskRunning {
			return SteerReceipt{Held: true, Direction: direction, Landing: heldWord(heard.inTime)}, nil
		}
		// AND THE DISK IS PUT BACK TOO. It was written before the hand-off, so a
		// refusal that only forgot it in memory would leave a direction on the
		// checkpoint that nobody read and the person was told was refused — and a
		// retry after a restart would be answered "already on the record".
		//
		// IF THE DISK WILL NOT TAKE IT BACK, THE REFUSAL IS NOT A REFUSAL. The
		// correction may still be on the checkpoint this engine would resume from,
		// so what goes back is UNCERTAINTY: the surface keeps the send under the
		// name it already has, and asking again lands it exactly once whichever way
		// it turns out ([errDirectionUncertain]).
		if err := dropDirection(node, heard, source); err != nil {
			return SteerReceipt{}, fmt.Errorf("%w: %v", errDirectionUncertain, err)
		}
		return SteerReceipt{}, nobodyToRead{fmt.Errorf("task %d has nobody in it to read your line right now", id)}
	}
	// AND THE ENGINE'S OWN LINE UNDER THE PERSON'S, as a second message rather
	// than a decoration on the first: their words reach the worker exactly as they
	// were typed, and the receipt a revision has to cite is the engine speaking
	// ([directionReceiptLine]). A relayed line is framed already and may revise
	// nothing, so it gets none. A receipt the worker never gets costs it the
	// ability to cite this direction and nothing else, which is not worth refusing
	// the line over.
	if origin == fromPerson && direction != 0 {
		_ = node.openRoom().steerIn(conversationOf(node), delivery{
			origin: fromRuntime, kind: msgNotice, note: briefNote(receiptLineFor(direction, source)),
		})
	}
	return SteerReceipt{
		Waiting:   waiting,
		Direction: direction,
		Landing:   SteerDelivered(waiting),
	}, nil
}

// heldForTask writes one line onto the node's record when there is nobody in the
// room to read it and the work is not over. It is a RECEIPT and not a refusal:
// the words are kept, the landing revalidates against them, and the sentence
// says both of those things to the person who typed them.
func heldForTask(node *TaskNode, text string, origin messageOrigin, source spokenSource) (SteerReceipt, error) {
	// A HELD CORRECTION IS STILL A CORRECTION THAT WAS ACCEPTED, so it goes on the
	// disk as part of being admitted, for [admitDirection]'s reason.
	heard, err := admitDirection(node, text, directionOf(origin), source)
	if err != nil {
		return SteerReceipt{}, err
	}
	switch heard.order {
	case directionAgain:
		return SteerReceipt{Again: true, Direction: heard.id, Landing: steerAgainWord}, nil
	case directionOutOfOrder:
		return SteerReceipt{}, errSaidLaterAlready
	case directionMismatch:
		return SteerReceipt{}, errSaidUnderThatName
	}
	return SteerReceipt{Held: true, Direction: heard.id, Landing: heldWord(heard.inTime)}, nil
}

// admitDirection puts one line on the node's record, and for a NAMED one puts it
// on the disk in the same breath ([TaskGraph.admitWritten]).
//
// A NAMED SEND IS THE ONLY ONE THAT NEEDS IT. The identity is what lets a person
// ask again for a crossing nobody answered, and the engine answers that ask off
// the record — so a record that has not reached the disk makes the promise only
// for as long as the process lives. Nothing about an unnamed send can be
// recognised on a second ask, so there is nothing a durable write would protect
// and it takes the road it always took.
//
// A WRITE THAT FAILED ADMITTED NOTHING. The direction comes back off the record
// inside the same hold, so no reader and no later checkpoint ever sees it, and
// the person is told plainly that nothing was sent.
//
// It is called with the node's admission lock held and the graph's released.
func admitDirection(node *TaskNode, text string, from directionFrom, source spokenSource) (directionHeard, error) {
	if node == nil {
		return directionHeard{}, nil
	}
	if !source.id.live() || node.graph == nil {
		return node.heardDirection(text, from, source), nil
	}
	// The two closures run with the graph's lock held, which is what makes the
	// admission and its write one act nobody can read between.
	heard, err := node.graph.admitWritten(
		func() directionHeard { return node.heardDirectionLocked(text, from, source) },
		func(h directionHeard) { node.forgetDirectionLocked(h.id) },
	)
	if err != nil {
		return heard, fmt.Errorf("%w: %v", errNotWrittenDown, err)
	}
	return heard, nil
}

// dropDirection takes a direction nobody could be given back off the record —
// and, for a named one, off the disk in the same hold. The error says the disk
// did NOT agree, which is why its caller stops calling the outcome a refusal.
func dropDirection(node *TaskNode, heard directionHeard, source spokenSource) error {
	if node == nil {
		return nil
	}
	if !source.id.live() || !heard.fresh() || node.graph == nil {
		node.forgetDirection(heard.id)
		return nil
	}
	return node.graph.dropWritten(func() { node.forgetDirectionLocked(heard.id) })
}

// errNotWrittenDown is the refusal a correction gets when the engine could not
// write it down. It says the two things that decide what the surface does with
// the words: nothing was delivered, and they are still the person's.
var errNotWrittenDown = errors.New("that correction could not be written down, so it was not sent and nothing was delivered")

// errDirectionUncertain is the OTHER ending, and it is not a refusal: the
// correction reached the engine's own record and the engine could not then take
// it back off the disk, so nobody can say whether a resume would find it.
//
// IT WEARS [ErrSendUnanswered] because that is exactly what a surface must do
// with it — keep the send under the identity it already has and offer to ask
// again, which lands it once however it turns out. Reporting a definite refusal
// here would send the person's next enter as a NEW correction.
var errDirectionUncertain error = uncertainDirection{}

type uncertainDirection struct{}

func (uncertainDirection) Error() string {
	return "it is not known whether that correction was kept — nothing has read it"
}

func (uncertainDirection) Unwrap() error { return ErrSendUnanswered }

// receiptLineFor is which of the engine's two lines goes under the words: the
// one for a person standing in this room, or the one that says they were not
// (assignment.go).
//
// IT READS THE DOOR THE WORDS CAME THROUGH AND NOT WHETHER THEY CARRY AN
// IDENTITY ([spokenSource.forwarded] says why): a line typed into this room and
// sent under a message id is still a line said in this room.
func receiptLineFor(direction uint64, source spokenSource) string {
	if source.forwarded {
		return forwardedReceiptLine(direction)
	}
	return directionReceiptLine(direction)
}

// heldWord is which of the two keepings this was: one the landing must still
// answer for, or one that arrived after the boundary was taken and belongs to
// the round after this (assignment.go).
func heldWord(inTime bool) string {
	if inTime {
		return steerHeldWord
	}
	return steerLateWord
}

// spoken is the line as the recipient will read it, built from WHO IS SPEAKING.
// A person's words go in undecorated, because from the worker's side that is
// exactly what they are; another agent's coordination is framed and named, so
// that a worker can act on it without mistaking it for the person's authority.
func (a *Agent) spoken(text string, waiting bool, origin messageOrigin, direction uint64) delivery {
	note := steerNote(text, waiting)
	if origin != fromPerson {
		note = relayNote(text, a.address())
	}
	// The node's receipt rides on the line and changes nothing else about it.
	return delivery{origin: origin, kind: msgDirection, note: carryingDirection(note, direction)}
}

// RetargetTask moves ONE RUNNING NODE onto another model, from its next turn on.
// Unknown id, a node that is not running, and a word no model here answers to are
// each an error naming which.
//
// IT IS THE SANCTIONED EXCEPTION TO THE FREEZE, and the header of this file says
// why in full: the id is settled at admission so that a `/model` in the
// conversation cannot move work nobody chose it for, and a person standing in
// this node's room choosing a model for THIS node is not that. The conversation
// stays on its own model, every other node stays on its own, and a task admitted
// after this one still takes the ordinary ladder — `task.model` from settings,
// else the conversation's ([Agent.defaultTaskModel]).
//
// A SETTLED NODE IS REFUSED IN THE SAME WORDS EVERY OTHER ROOM DOOR REFUSES ONE:
// `task 7 is done, not running`. There is nothing left to move it onto — the run
// is over, the child agent is closed, the report is written — and the model on a
// landed row is a fact about what happened, which a person may read and must not
// be able to edit.
//
// THE WORD RESOLVES THROUGH ADMISSION'S OWN LADDER (taskmodel.go), so a room and
// a proposal cannot disagree about what "opus 5" means. A word that fits more
// than one model is a QUESTION and this door has nobody to ask — the shortlist is
// a thing a proposal card carries, and there is no card here — so it comes back
// as the same refusal a too-vague proposal gets, naming the candidates. The
// surface's own door never raises it: its picker offers concrete catalog ids, so
// every word that reaches here from a room is already exactly one model.
//
// THE SWITCH LANDS ON THE NEXT TURN, and that is [Agent.SetModel]'s own contract
// rather than a second mechanism: a turn in flight latched its model once at the
// start (loop.go), so the call the node is making right now finishes on the model
// it began on and the one after it is on the new one. That is the correct
// behaviour and not a limitation — killing a request in flight to change models
// would throw away work the person is paying for and has already waited for.
func (a *Agent) RetargetTask(id uint64, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("no model to move to")
	}
	node := a.taskNode(id)
	if node == nil {
		return fmt.Errorf("no task %d in this session", id)
	}
	if state := node.stateNow(); state != TaskRunning {
		return fmt.Errorf("task %d is %s, not running", id, state)
	}
	choice := a.resolveTaskModel(model)
	switch {
	case choice.problem != "":
		return errors.New(choice.problem)
	case len(choice.options) > 0:
		return errors.New(taskModelVague(model, choice.options))
	}
	node.retarget(choice.model)
	// The child is told directly as well as through the spec, because the two
	// answer for two different moments. A node whose worker is already up reads
	// its model off the agent, so the agent has to be moved; a node that is
	// RUNNING but whose worker is still being prepared has no agent yet, and
	// [Agent.newTaskAgent] will read the spec this just wrote. Neither is a
	// fallback for the other — the ordinary case is the first, and the second is
	// the only reason a nil child here is silence rather than the error
	// [Agent.SteerTask] returns for it: there is nobody to talk to, but there is
	// something to change, and it has just been changed.
	if child := node.openRoom().speaker(); child != nil {
		child.SetModel(choice.model)
	}
	// The checkpoint is what makes the pick survive the session, exactly as the
	// admitted id does (task_store.go writes spec.model), and the update is what
	// makes every row saying the old id say the new one: the roster, the room's
	// own status line, the card this node lands as.
	node.graph.checkpoint()
	a.emitTaskUpdate(node.notice())
	return nil
}

// WatchTask subscribes to one node's LIVE event stream: the child agent's own
// events — text deltas, tool begins and ends, errors — as they happen, opening
// with the step the node is in the middle of ([taskCatchup]). The channel closes
// at the node's final state and on [Agent.Close]. Unknown id is an error.
//
// A FINISHED NODE ANSWERS WITH A CLOSED CHANNEL rather than an error: the id is
// real, the work is over, and a channel that closes immediately is how every
// other stream in this package says "that is the whole of it" ([eventHub.
// subscribe] does exactly this for a turn that has ended). The history is the
// journal; this door is only ever the present.
func (a *Agent) WatchTask(id uint64) (<-chan Event, error) {
	lane, _, err := a.WatchTaskRoom(id)
	return lane, err
}

// WatchTaskRoom is [Agent.WatchTask] with a way to stop.
//
// It is the same door onto the same room — the step in flight, then the node's
// events from now on — and stop is a watcher walking out: the room stops
// publishing to it, its pump ends and its channel closes. A surface needs it
// because a person leaves a node's page long before the node leaves, and
// because a surface that shows one conversation at a time detaches from every
// node it was watching in the ones behind (see [taskRoom.leave]).
//
// It is a SECOND DOOR rather than a changed one, for [Agent.WatchTaskUpdates]'
// reason. stop is never nil, and calling it twice is calling it once.
func (a *Agent) WatchTaskRoom(id uint64) (<-chan Event, func(), error) {
	node := a.taskNode(id)
	if node == nil {
		return nil, nil, fmt.Errorf("no task %d in this session", id)
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return closedEventStream(), func() {}, nil
	}
	room := node.openRoom()
	stream, joined := room.joinStream()
	if !joined {
		return stream.out, func() {}, nil
	}
	var once sync.Once
	return stream.out, func() { once.Do(func() { room.leave(stream) }) }, nil
}

// TaskJournal is the node's journal path — its whole transcript on disk — or ""
// for an unknown id, and for a node whose transcript cannot be found.
//
// The path is recorded when the node's child agent is built, because that is
// where it is minted: [taskJournalPath] stamps the current time into the name,
// so recomputing it later would name a file nobody ever wrote. It survives the
// process on the checkpoint (task_store.go's taskRecord.Journal), which is what
// lets a finished task's room replay after a restart.
//
// A NODE FROM A CHECKPOINT THAT NEVER CARRIED THE PATH IS LOOKED UP BY ITS ID.
// The file is named with the node's id in the session's own journal directory
// — the same directory [taskJournalPath] mints into — so it is found rather
// than guessed ([findTaskJournal]); a name that is not on disk answers "" as it
// always did. What is found is written onto the node and checkpointed, so the
// lookup happens once per node per life rather than on every open.
func (a *Agent) TaskJournal(id uint64) string {
	node := a.taskNode(id)
	if node == nil {
		return ""
	}
	if path := node.journalPath(); path != "" {
		return path
	}
	a.mu.Lock()
	dir := taskJournalDir(a.familyPlace(node), a.sessionID())
	a.mu.Unlock()
	path := findTaskJournal(dir, node.id)
	if path == "" {
		return ""
	}
	node.setJournal(path)
	node.graph.checkpoint()
	return path
}

// taskNode finds one admitted node, without BUILDING a graph that a question
// about tasks does not need: a session that never groomed one answers every
// door in this file with "no such task", which is the truth.
func (a *Agent) taskNode(id uint64) *TaskNode {
	graph := a.tasker()
	if graph == nil {
		return nil
	}
	return graph.node(id)
}

// ── the room itself ─────────────────────────────────────────────────────────

// taskRoom is one running node's presence: the child agent that IS the node,
// and the live subscribers watching it work.
//
// It is opened by whoever arrives first — the runner attaching its child, or a
// person watching a node that has not started yet — and it closes exactly once,
// when the node reaches its final state ([TaskNode.openRoom] hangs that on the
// node's own done channel, so every path to a final state closes it: the
// runner's, the frontier's cascade, and a recovery's).
//
// The subscribers are [eventStream]s, the same unbounded queue every other
// fan-out in this package uses (agent.go's eventHub, [Agent.TaskUpdates]'s
// watchers): the producer here is the child's event loop, which must never wait
// on a surface that is redrawing, and must never drop a text delta, which is the
// one event whose loss reads as corruption rather than as lag.
type taskRoom struct {
	mu     sync.Mutex
	child  *Agent
	closed bool
	// pay is the agent the node's price still owes for after the speaker is
	// withdrawn — see [taskRoom.bill].
	pay      *Agent
	watchers map[*eventStream]struct{}
	// live is the recorder every published event also goes through
	// (task_live.go): what the node is doing and the tail of what it has said,
	// kept for the reader who was NOT subscribed while it happened — the model,
	// asking after the fact. It is set once here and never reassigned, so it is
	// read without this lock, and it OUTLIVES the close: a node that landed a
	// second ago still answers with the last thing it was doing.
	live *taskLive
	// catchup is the step in flight, handed to each new watcher once. It is
	// under THIS lock rather than one of its own, because what it holds and who
	// is watching have to be decided in the same breath (see [taskRoom.join]).
	catchup taskCatchup
}

func newTaskRoom() *taskRoom {
	return &taskRoom{watchers: make(map[*eventStream]struct{}, 1), live: &taskLive{}}
}

// join returns a fresh channel carrying the step this node is in the middle of,
// and then its events from now on. A room that has already closed answers with a
// channel that closes immediately, exactly as a subscription to a finished node
// does — the two are the same event arriving on either side of the close, and
// they must not be two behaviours.
//
// THE CATCH-UP AND THE SUBSCRIPTION ARE ONE ACT, under one hold of the lock: a
// delta that landed between reading the step and being added to the watchers
// would be a delta nobody ever sees, and one that landed the other way round
// would be drawn twice. The seed is safe to do while holding it for [publish]'s
// own reason — a send is an append and a signal, never a wait.
//
// A nil room is a node with no room to enter: the same answer, so no caller has
// to check.
func (r *taskRoom) join() <-chan Event {
	stream, _ := r.joinStream()
	return stream.out
}

// joinStream is join with the stream itself in hand, for a caller that will
// need to hand it back ([taskRoom.leave]). false says the room was closed and
// the stream with it, so nothing has to be handed back at all.
func (r *taskRoom) joinStream() (*eventStream, bool) {
	stream := newEventStream()
	if r == nil {
		stream.close()
		return stream, false
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		stream.close()
		return stream, false
	}
	for _, event := range r.catchup.replay() {
		stream.send(event)
	}
	r.watchers[stream] = struct{}{}
	r.mu.Unlock()
	return stream, true
}

// leave is a watcher WALKING OUT of the room before the node is over: it stops
// being published to and its pump ends.
//
// A room is not a session. A person opens a node's page, reads it and presses
// esc, and the node goes on running for another twenty minutes — so a surface
// that could only ever join would collect one parked goroutine per look
// (agent.go's [eventStream.pump]), and a surface holding several conversations
// would collect them per conversation. A watcher this room does not hold is
// left alone, which is the ordinary case for a caller that leaves twice or
// leaves after the node landed.
func (r *taskRoom) leave(stream *eventStream) {
	if r == nil || stream == nil {
		stream.leave()
		return
	}
	r.mu.Lock()
	delete(r.watchers, stream)
	r.mu.Unlock()
	stream.leave()
}

// speaking hands the room the agent the person will be talking to.
func (r *taskRoom) speaking(child *Agent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.closed {
		r.child = child
	}
	r.mu.Unlock()
}

// speaker is who is in the room, or nil once the node is over. It is cleared at
// close so a line typed into a landed node's page is refused rather than queued
// onto an agent nothing will drain — and, since #273, also withdrawn by the
// runner the moment the worker's reading is over (task_run.go's
// [runTaskChild]), because the swallow lived in the gap between the two.
func (r *taskRoom) speaker() *Agent {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.child
}

// steerIn hands one spoken line to whoever is in the room, and answers whether
// a live reader took it. The resolve-and-append is one step
// ([taskRoom.handIn]), which is what keeps the runner's withdrawal from
// slipping between the two into a swallow.
//
// THERE IS NO SECOND ADDRESS TO TRY. A landed node's report falls back to the
// person's conversation because news with nowhere to go still belongs to
// somebody ([Agent.deliverTaskNote]); a line said INTO a room does not, because
// putting somebody's words in front of a reader they did not address is worse
// than telling them nobody was there to hear it.
func (r *taskRoom) steerIn(at conversationID, said delivery) bool {
	return deliverTo(said, roomSeat{at: at, room: r}).accepted()
}

// bill is the agent whose unfolded usage the node's price still owes: the
// worker, from the moment it starts until retire folds it into the frozen
// figure. It outlives the speaker on purpose — the speaker answers "who can
// read a line", the bill answers "whose meter is still running", and the check
// is exactly the stretch where those are different agents' answers
// ([TaskNode.spend]).
func (r *taskRoom) bill(child *Agent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.pay = child
	r.mu.Unlock()
}

func (r *taskRoom) billed() *Agent {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pay
}

// publish fans one of the child's events out to everyone watching. The lock is
// held across the fan-out for [eventHub.send]'s reason: a send is an append and
// a signal, never a wait, so every watcher sees the same events in the same
// order and one arriving mid-fan-out lands cleanly before or after this event
// rather than inside it.
//
// IT IS ALSO WHERE THE EVENT IS RECORDED (task_live.go). This is the one funnel
// the child's whole narrative passes through, so it is the one place a
// recording can be in the order the work happened; a second tap on the stream
// would be a second ordering of the same events, disagreeing exactly under the
// load that makes the question worth asking.
func (r *taskRoom) publish(event Event) {
	if r == nil {
		return
	}
	r.live.record(event)
	r.mu.Lock()
	defer r.mu.Unlock()
	// The step in flight is folded in BEFORE the fan-out and whether or not the
	// room is still open, so what the next joiner is handed is exactly what the
	// watchers already have — one funnel, one order, two readers.
	r.catchup.record(event)
	if r.closed {
		return
	}
	for stream := range r.watchers {
		stream.send(event)
	}
}

// close ends every watcher's channel and empties the room. It is idempotent —
// the node's done channel is closed once, but a room may also be closed by a
// caller that got there first — and every subscriber added after it gets a
// closed channel from join.
func (r *taskRoom) close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	watchers := r.watchers
	r.watchers = nil
	r.child = nil
	r.mu.Unlock()
	for stream := range watchers {
		stream.close()
	}
}

// ── the step in flight ──────────────────────────────────────────────────────

// taskCatchup is the one thing neither of the room's two lanes can carry: the
// step the node is IN THE MIDDLE OF.
//
// A message reaches the journal when it COMPLETES (agent.go's recordLocked), and
// the live lane carries what happens from the instant somebody subscribes. So a
// person who walks into a node while it is thinking through a long answer used
// to be shown the last thing that FINISHED and nothing else — a page that sits
// still until the next delta lands, which reads either as a task that has frozen
// or as a task whose work is not being displayed at all. The reasoning is worse
// than absent: it is never journaled anywhere, so before this it existed only
// for whoever happened to be subscribed while it streamed.
//
// So the room keeps that step and [taskRoom.join] hands it over once, before the
// live events. It is a CATCH-UP AND NOT A REPLAY: everything the journal already
// holds is deliberately missing from it, because the surface reads the history
// off the file and drawing the same paragraph from both lanes is worse than
// drawing it from neither.
//
// ── WHERE THE LINE IS DRAWN, AND WHY IT IS DRAWN THERE ──
//
// The boundary is loop.go's own, not a guess about it. A response is recorded
// the moment it completes: with no tool calls it is recorded and the turn ends,
// and with tool calls it is recorded BEFORE the batch runs. EventToolBegin and
// EventTurnDone are therefore each the first event after a journal write, and
// both clear everything kept here — what they closed is on disk now.
//
// It is the same fact that keeps a BEGUN call out of the catch-up. The assistant
// message naming it was written before it started, so the journal has the call
// already; what the file cannot say is that it has not come back, and a surface
// reads that off the missing tool result rather than off an event (internal/tui3
// ReadTranscript). An ANNOUNCED call is the other half of that: the model has
// finished spelling it out and the batch has not started, so nothing has been
// written yet and the announcement is carried.
type taskCatchup struct {
	// thinking is the marker that opened the run — a model that is reasoning
	// with nothing on the wire is still a fact worth arriving to.
	thinking bool
	// thought and answer are what the model has streamed since the last journal
	// write: its reasoning, and its reply.
	thought strings.Builder
	answer  strings.Builder
	// announced are the calls asked for and not yet started, in arrival order.
	announced []Event
}

// record folds one published event in. It is the whole write side, and it runs
// under the room's lock.
func (c *taskCatchup) record(event Event) {
	switch event.Kind {
	case EventThinking:
		c.thinking = true
	case EventReasoning:
		c.thought.WriteString(event.Text)
	case EventTextDelta:
		c.answer.WriteString(event.Text)
	case EventToolAnnounced:
		c.announced = append(c.announced, event)
	case EventToolBegin, EventTurnDone, EventError:
		// THE STEP IS ON DISK NOW. The begins of a batch are emitted together,
		// after the assistant message that made every one of them was recorded
		// (loop.go), so the first of them settles the whole of what is kept here —
		// which is why nothing has to be dropped call by call, and why nothing in
		// here needs an id that EventToolBegin does not carry.
		c.reset()
	case EventCompacting:
		// A COMPACTION PASS RUNS AT A STEP BOUNDARY, and it is the one boundary
		// that takes SECONDS — the summarizer is a model call — so without this the
		// window between "the answer was written" and "the turn is done" is long
		// enough to walk into, and a person who did would read the same paragraph
		// twice: once off the file and once out of here.
		//
		// The known exception is the OVERFLOW pass, which compacts and retries
		// mid-step (loop.go), where this drops a partial reply the journal has not
		// got yet. That is a delta's worth of lateness on a page that is about to
		// be re-streamed anyway, and it is the quieter of the two mistakes.
		c.reset()
	}
}

// reset empties the step. It is not a method on the room because the room never
// calls it: the events say when a step ends.
func (c *taskCatchup) reset() {
	c.thinking = false
	c.thought.Reset()
	c.answer.Reset()
	c.announced = nil
}

// replay is the step as events, in the order it happened: the reasoning, then
// the reply, then the calls that are waiting to start. An empty step replays
// nothing, which is the ordinary case of a node between steps.
func (c *taskCatchup) replay() []Event {
	var out []Event
	if c.thinking || c.thought.Len() > 0 {
		// The marker first, because that is the order a live watcher saw it in
		// and the surfaces fold the two together (internal/tui3's thinking.go).
		out = append(out, Event{Kind: EventThinking})
	}
	if text := c.thought.String(); text != "" {
		out = append(out, Event{Kind: EventReasoning, Text: text})
	}
	if text := c.answer.String(); text != "" {
		out = append(out, Event{Kind: EventTextDelta, Text: text})
	}
	return append(out, c.announced...)
}

// closedEventStream is an already-ended channel: what a door answers when the
// thing behind it is over.
func closedEventStream() <-chan Event {
	stream := newEventStream()
	stream.close()
	return stream.out
}

// settled says a node's life is over — nothing more will happen in its room.
//
// UNVERIFIED IS SETTLED. Its run ended, its child agent is closed, its slot is
// back on the frontier: there is nobody in the room to watch or talk to, which
// is the only question this predicate answers. That it is still waiting on a
// person to decide what it MEANS ([Agent.ResolveUnverified]) is a fact about the
// graph, not about the room — and a resolution reaches the node through
// [TaskGraph.resettle], never through a door in here.
func (s TaskState) settled() bool {
	return s == TaskDone || s == TaskFailed || s == TaskUnverified
}
