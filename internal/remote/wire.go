// Package remote is the wire between a surface on one machine and an engine on
// another. The surface half dials `ssh <host> codeaf engine …` and speaks this
// protocol over the pipes; the engine half wraps an ordinary *session.Agent and
// answers. Both halves import THIS file and nothing of each other.
//
// THE CONTRACT IS THE ENVELOPE, NOT THE PAYLOADS. Payloads are the session
// package's own types carried as JSON — both ends compile against
// internal/session, so a field added there travels without a wire change. The
// one exception is [EventWire], because error does not survive encoding/json.
//
// Frames are JSON, one per line (a journal's own framing, for a journal's own
// reason: a torn write is one lost line, not a lost stream).
package remote

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// Version is the protocol's version. The hello and the welcome both carry it,
// and a mismatch is a refusal at the door — two builds that might disagree
// about a frame must not guess at each other.
//
// VERSION 2 IS THE PERSISTENT ENGINE. Version 1 married a conversation to a
// pipe: the engine was `ssh … codeaf engine`, it read frames on stdin, and when
// the pipe died so did the turn in flight. Version 2 separates the two — a
// session lives on the engine machine and a surface ATTACHES to it — and the
// four things that separation needs are the whole of the delta:
//
//   - [Frame.Seq] numbers every event of a stream, and [Hello.Resume] says
//     which ones a returning surface already has, so a reattach replays the
//     gap instead of the conversation.
//   - [MethodDetach] tells the engine a surface is leaving ON PURPOSE, which is
//     the fact version 1 could not express: a torn pipe and a closed window
//     were one event, so both had to interrupt the turn to be safe.
//   - [SubmitFilesArgs] and [MethodFetchFile] carry a person's attachments both
//     ways, generalizing the one payload version 1 already remade on arrival
//     (image.go).
//   - [HeldQuestion] lets a card raised with nobody attached WAIT rather than
//     expire, which is what turns half the --host refusals from "the card would
//     land in an empty room" into an answered question.
//
// EVERY VERSION-2 FIELD IS ADDITIVE AND OMITEMPTY, so a version-2 frame read by
// a version-1 decoder is a version-1 frame. That does not make the versions
// compatible — the door still refuses a mismatch, and it must, because a
// version-1 engine would silently interrupt a turn the surface believed was
// detached — but it does mean this file stayed a superset rather than becoming
// a second protocol.
// VERSION 4 IS THREE THINGS THAT LANDED IN ONE WAVE, AND THEY SHARE A NUMBER
// because nobody ever ran a build with only one of them.
//
// THE PLACES FOLLOW THE SESSION'S MACHINE. A place is a
// listing of one machine's disk — the conversations, the work they ran, what
// was learned, what it cost — and every one of them was read under the SURFACE's
// process, which over a connection is the laptop while the conversation lives on
// the server. Version 4 adds the doors that let the surface ask the machine that
// owns the work instead ([MethodPlacesWorld] and its neighbours), and one field
// on the welcome saying which state root those answers were read under.
//
// AND THE ROOM HAS ONE KEYBOARD. Version 2 let several surfaces attach
// to one conversation and version 3 left it at that: every one of them could
// type, and the only arbiter was the engine's own "a turn is already running"
// refusal — so two windows on one conversation raced, and neither screen said
// the other existed. Version 4 names a DRIVER, and the whole delta is three
// things:
//
//   - [Hello.Surface] carries the surface machine's short name, so a screen can
//     say WHICH window has the keyboard rather than that some window does.
//   - [Welcome.Driver] and the "driver" frame say who holds it, told to each
//     surface as that surface should read it, and the engine is the only thing
//     that decides.
//   - [MethodTake] moves it here in one round trip, and [Hello.Back] is how a
//     surface that merely lost its link says "I am not a new window" — because
//     the keyboard follows the newest ARRIVAL, and a redial in the background
//     must not steal it from a machine the person has actually walked to.
//
// AND IT MAKES THE OTHER WINDOWS WINDOWS. [Turn] tells every surface that did
// NOT start a turn that one has started, because a surface only draws a stream
// it knows about: the events have fanned out to the whole room since version 2
// and a watching window had nowhere to put them, so it sat on a still frame
// while the work went on in front of somebody else.
//
// AND INTENT GOES UP, FACTS COME DOWN. Versions 1 to 3 made every fact a
// QUESTION: a surface drew a status line by asking the engine what the model
// was, what had been spent, what the conversation weighed and how hard it was
// being asked to think — four round trips over an ssh pipe, on a frame the
// person expected to be instant. Version 4 turns that around. The engine states
// those facts, unasked, whenever they move; the surface keeps a replica and
// reads it from memory. What still travels UP is intent — a message, a key, an
// answer — because intent is the one thing the far end cannot know on its own.
//
// The delta is two additions and no removals:
//
//   - [FactsPush] is the whole fact set with a revision number, and
//     [Welcome.Facts] is the one a surface arrives holding.
//   - the "facts" frame carries later ones. It belongs to the CONNECTION and
//     not to a stream, so a fact that moves between turns still lands.
//
// Both are additive and omitempty, exactly as version 2's were, and the door
// still refuses a mismatch: a version-3 engine states nothing, so a version-4
// surface reading a replica off it would draw a status line frozen at whatever
// the welcome said.
//
// VERSION 4 ADDS [MethodPing]. It changes no session state and carries no
// payload in either direction; the version still moves because a method one
// half may send and the other half cannot answer is a protocol difference, and
// Decision 3 in docs/REMOTE.md says those differences are refused at the door.
//
// VERSION 5 ADDS [MethodPlacesTask]. Version 4 moved the PLACES onto
// the machine that owns the work ([MethodPlacesWorld]), and the tasks place duly
// listed the far machine's four hundred pieces of work — but the CARD behind one
// of those rows still read the last thing that work said off THIS process's
// disk, at a path that only exists on the other one. The door that ends it is
// one. The method also carries the bounded journal tail a hosted task room
// needs, so both halves must agree on its meaning at the version door.
//
// AND THE OTHER PLACES METHODS RIDE THE SAME NUMBER. Places.Task moved the door once; the
// ledger, search, memory and archive methods in wire_places.go stay on that
// same version because an older engine's no-such-method answer has an explicit
// honest fallback on the surface.
//
// VERSION 6 IS TWO LANES' ONE BUMP, the same way version 4 was: steering and
// the task door landed together and a number that moved twice for one release
// would refuse engines for no reason.
//
// It adds [MethodSteer]. A local surface could put words into a running turn,
// but the client agent did not expose that verb and a hosted surface therefore
// hid the key entirely. Steering is another stream-opening intent: the returned
// stream is the running turn from the correction onward, exactly as
// [session.Agent.Steer] defines it.
//
// And it adds THE TASK DOOR — [MethodTaskStart], [MethodPlannerStart] and
// `Task.Judge`, which version 16 retired (wire_task.go). Every place method before it was a READING,
// which is why they could ride version 5 behind an honest fallback: an engine
// that cannot answer one leaves a page drawing the sentence it has always
// drawn. These calls are not reading. They COMMISSION WORK on the far machine
// and spend that machine's money doing it, so there is no sentence a surface
// could draw instead of an answer — either the far end starts the task or
// nothing happened. A surface that sent [MethodTaskStart] to an engine which
// does not know the method would have told a person their work was under way
// while the far machine refused a name it had never heard, and that is
// precisely the guess Decision 3 refuses to let two builds make three turns
// into a conversation. So the number moves and the mismatch is refused at the
// door, in the same sentence naming the same fix.
//
// VERSION 7 opens a running task's room by id and carries its steer and stop
// verbs. A running node has no record URI yet, so version 5's Places.Task door
// cannot name it; the node id belongs to the current engine conversation and
// exists from admission onward. The room read is bounded and the two writes
// preserve the session agent's own answers.
//
// VERSION 8 PUSHES THE TASK LANE, and it is the last half of version 6's task
// door. Version 6 let a hosted surface COMMISSION work on the far machine and
// left it with no way to watch what it had commissioned: a node's life — queued,
// running, done — is emitted on the session's STANDING task subscription, which
// no frame carried, so `/task solo …` over a connection answered "started", ran
// to completion on the far machine, and never put a row on the rail of the person
// who typed it. A model's proposal looked like it worked only because a proposal
// happens inside a turn and the turn's stream carried its updates by accident of
// where it was raised.
//
// The delta is one intent up and one fact down, on the shape version 4 named:
//
//   - [MethodTaskWatch] is the surface saying it draws tasks. The engine opens
//     one standing subscription per surface that asks, which REPLAYS THE WHOLE
//     ROSTER before its first live event (session's [Agent.WatchTaskUpdates]),
//     so a window that attached an hour into the work still learns every row.
//   - the "task" frame carries each of that lane's events onward. It belongs to
//     the CONNECTION and not to a stream, exactly as "facts" does, because a
//     node's landing happens when no turn is running and there is no stream left
//     for it to land on.
//   - [MethodTaskPending] answers the one question the lane cannot: which
//     proposals are still open. It is asked only while a card is on screen and a
//     turn has just ended, never on a frame and never on a pointer.
//   - [MethodTaskResolve] carries the answer to a proposal, and it is the door
//     whose absence broke everything else. The surface asserts the task seam as
//     ONE interface — the lane, the pending reading, and this — so a wire holding
//     three of the four left a hosted rail unsubscribed rather than partly
//     working, with nothing on any screen saying why. internal/tui3's DrawsTasks
//     is that assertion made checkable, and cmd/codeaf makes it.
//
// The number moves rather than riding version 7 for [MethodTaskStart]'s reason:
// an engine that does not know Task.Watch would answer the surface's one
// subscription with "no such method" and leave the rail permanently empty with
// nothing on the screen saying so.
//
// VERSION 9 CARRIES THE FOREGROUND-COMMAND CLOCK IN [Welcome]. The clock is
// armed from the engine machine's profile, while a hosted surface has a
// different profile of its own. Leaving the field out would make the row draw
// a deadline no process on the far machine was following. The number moves
// because an older same-version engine would otherwise be accepted and answer
// the new field with zero, which is itself a real and different posture.
//
// VERSION 10 CARRIES [MethodTaskHold]. A version-9 engine would reject the
// first rune's hold while its surface already showed "waiting on you", then
// admit the task on the deadline the person believed had stopped.
//
// VERSION 11 CARRIES THE HARNESS LANE. A design card and a subharness intake
// card are raised on a subscription that outlives the turn (internal/session's
// emitHarness), and only a running turn's stream crossed this wire — so
// cmd/codeaf built every hosted session with the designer nilled and the cards
// off, and said so in prose. The delta is one subscription up
// ([MethodDesignWatch]), its frames down ("design"), and the intake card's
// answer ([MethodSubharnessResolve]); the design card's own answer has been
// [MethodHarness] since version 1. A lane without its answer door puts a
// question on a screen that nothing can close, which is the fault version 8
// found in the task rail.
//
// The adaptive run lane is deliberately NOT part of this delta; lanes.go states
// exactly what is missing from it.
//
// The number moves rather than riding version 10 for [MethodTaskWatch]'s
// reason: an older engine answers the new subscription with "no such method"
// and leaves a lane permanently dark with nothing on the screen saying so.
//
// VERSION 12 CARRIES [Hello.Join] AND [Hello.Watch], AND THE NUMBER IS THE
// ENFORCEMENT. Both are SAFETY fields — one says "never start a conversation",
// the other says "never give me the keyboard" — and both are omitempty booleans,
// which is exactly the shape a version-11 engine DISCARDS in silence. That
// engine would then do the two things the fields exist to prevent, before the
// surface ever sees a welcome to check: boot a whole conversation to answer a
// question about work that is running, and hand a reader the keyboard off the
// window that owns the work. Neither is recoverable by a check afterwards.
//
// So the guarantee is the door's, not the flag's. The version is compared before
// [AttachOptions.Open] is called and before [Session.attach] runs, by BOTH
// builds — and a version-11 engine enforces it against a version-12 surface
// using code that has been there since version 1. That is the only mechanism in
// this protocol an old peer can be trusted to run.
//
// VERSION 13 IS THE MOVE, AND IT IS A RULING ABOUT WHAT A SECOND WINDOW MEANS.
// Versions 4 to 12 let several surfaces sit in one conversation and arbitrated
// between them with a keyboard ([Driver]): the newest arrival typed, the others
// watched. The ruling is that a person opening a conversation in the terminal
// they are standing at MEANS TO BE IN IT, and the window they walked away from
// should say so and step back — one conversation, one window, and the way back
// is the same keystroke from the other side.
//
// The delta is one frame down, and nothing goes up:
//
//   - the "moved" frame carries a [Moved] to every OTHER surface in the room
//     when a window arrives that is neither a watcher ([Hello.Watch]) nor a
//     link coming back ([Hello.Back]). The surface hearing it DETACHES — the
//     engine holds the conversation and the work never stops — and lands on
//     home with that row under the cursor.
//
// THE NUMBER MOVES BECAUSE THE OLD BEHAVIOUR WAS A BEHAVIOUR AND NOT A GAP. A
// version-12 surface joined by a version-13 one never hears the frame and stays
// attached, watching, exactly as it did before — which is not broken, and is
// precisely why an engine may not be left to guess: a version-12 ENGINE would
// leave two windows both believing they are the one in the conversation, and
// only the door can tell those two builds apart.
// VERSION 14 CARRIES THE QUESTIONS LANE, and it is the last of the standing
// lanes to cross. A question is one decision handed to a person with its
// evidence attached (docs/design/questions/DESIGN.md), and internal/session
// speaks every one of them on a subscription of its own that outlives the turn
// — [session.Agent.WatchQuestions], which replays everything still open the
// moment a surface attaches. That subscription had no frame here, so a hosted
// surface asserted the questions half of its agent, found no
// [Agent.WatchQuestions] on it, and drew nothing: an `ask` on the road a plain
// `codeaf` takes stopped the turn with no block, no chip and no row on any
// screen, for as long as the person left it. Measured at three minutes.
//
// The delta is one intent up and one fact down, on the shape versions 8 and 11
// named:
//
//   - [MethodQuestionWatch] is the surface saying it draws questions. The
//     engine opens one subscription per surface that asks, which REPLAYS WHAT
//     IS STILL OPEN before its first live event, so a window that attached an
//     hour into the wait still learns the question.
//   - the "question" frame carries each of that lane's events onward —
//     [session.EventQuestion], [session.EventQuestionWithdrawn] and
//     [session.EventQuestionAnswered], each with the whole object on it. It
//     belongs to the CONNECTION and not to a stream, because most questions
//     outlive the turn that raised them and many never had one.
//
// THE ANSWER'S OWN DOOR WAS ALREADY HERE and is unchanged:
// [MethodQuestionResolve] has carried [session.Answer] whole since it landed.
// That is what made the gap so quiet — the half a person presses worked
// perfectly and the half that puts the question on the screen did not exist.
//
// The number moves rather than riding version 13 for [MethodTaskWatch]'s
// reason, and the reason is the whole of the discipline here: a version-13
// engine answers this subscription with "no such method" and leaves the lane
// permanently dark, with nothing on the screen saying why. Refused at the door,
// a person is told their engine is an older codeaf; accepted, they would be told
// nothing at all and their turn would simply stop. NEVER TO SILENCE.
// VERSION 15 IS AN ANSWER THAT IS A MESSAGE (docs/design/questions/DESIGN.md).
// A question the model asks no longer exists only for as long as the call that
// asked is parked on it: the answer is delivered to the conversation, which is
// what lets the model carry on while somebody decides, lets a clock take the
// pick on a question nobody is waiting on, and lets a decision be CHANGED
// afterwards. Two of those cross this wire:
//
//   - [session.Answer.Revises] says an answer is a person changing their mind
//     about a settled question rather than a second click on one somebody else
//     has already answered. It rides [MethodQuestionResolve], which has always
//     carried the answer whole.
//   - [MethodQuestionHold] stops a question's clock without answering it.
//
// THE NUMBER MOVES BECAUSE BOTH FAIL AS SILENCE ON AN OLDER ENGINE. A version-14
// engine reads `revises` as a field it does not know, applies answers.go's own
// law — a late answer is ignored and nothing says so — and the person watches
// their change do nothing; and it answers `Question.Hold` with no such method
// while its clock goes on counting, so the pick is taken under the hand of
// somebody who pressed a key to stop exactly that. NEVER TO SILENCE.
//
// AND VERSION 15 CARRIES THE READ SIDE OF THE QUESTION RULES, which is the other
// half of the same number and travelled with it rather than moving it again.
// [MethodSetAutonomy] crossed alone: this wire could WRITE a project's rules and
// never read them
// back. That was not a remote-only fault — the ordinary launch talks to its own
// engine through this client — so `/autonomy` and the settings rows beside it
// answered `this conversation has no project to keep question rules in` on every
// machine, whatever project it was in, because the door they asserted did not
// exist. [MethodAutonomy] is the missing half and carries
// `map[session.AskKind]session.Policy` down.
//
// AND THE NUMBER MOVES FOR THE SAME REASON VERSION 14'S DID, which is why this
// could not ride 14. A version-14 engine answers this call with "no such method"
// and the client can only report that it could not read the rules; ridden
// silently, an empty answer is INDISTINGUISHABLE FROM A PROJECT THAT KEEPS NO
// RULES, and every row on the settings page would say `ask me` while the engine
// was quietly on `decide`. A wrong account of what may happen without a person
// is the one thing this lane must never give, so the refusal is at the door and
// the nil that comes back is drawn as "not read" rather than as "nothing set"
// (client.Autonomy, and settingsautonomy.go's own reading).
//
// VERSION 16 IS THE TASK DOOR THAT STOPPED WAITING (issue #936). `/task` used to
// be two calls in series — `Task.Judge`, then [MethodTaskStart] held open for
// the engine's shaper — and the engine now admits the work at once and reads its
// width and writes its brief beside the worker. So `Task.Judge` is gone,
// [TaskStartArgs] carries the one fact the engine cannot know (`solo`), and the
// start is an ordinary call with the ordinary deadline. The number moves because
// a version-15 engine would still hold the start behind its shaper for up to
// half a minute while this surface had stopped waiting at ten seconds: the
// person would be told their work did not start while it did. NEVER TO A
// SENTENCE THAT IS FALSE.
// VERSION 17 separates clarification from answering a pending question and adds
// ReplaceQuestion. It also carries whether a caller has no approval resolver.
// Older peers must refuse before a question or an unwatched tool can run under
// semantics the other side does not understand.
// VERSION 18 IS THE CREW PICKED PER TASK. [TaskStartArgs] carries the one-task
// effort word (`/task --best`, `/task --cheap`), and [MethodTaskRedoStronger]
// runs the last task again on a stronger crew. The number moves because both
// fail as silence on an older engine: a version-17 engine reads `effort` as a
// field it does not know and starts the task on the crew it would have had,
// and the person is never told their word did nothing. NEVER TO SILENCE.
const Version = 18

// AND THE NEWS FRAMES RIDE THAT SAME NUMBER, for the reason the places methods
// rode version 5's: neither half can be surprised by them. "phase" and "lane"
// are pushes the surface never asks for, and a version-13 surface that does not
// know either kind IGNORES it (client.go's reader, and the law stated on
// [Frame.Kind]); a version-13 engine that does not send them leaves a surface
// drawing exactly what it drew before, which is nothing. There is no state on
// either side that a missing frame can leave wrong, so there is nothing for a
// door to refuse — and refusing an engine over a status line would be this
// protocol turning a live conversation away for a decoration.
//
// [MethodAnswerLaneOffer] rides it too, and that one is a METHOD, which
// Decision 3 in docs/REMOTE.md normally refuses across a version. It is the
// documented exception: an older engine answers "no such method", the surface
// reads that as false, and false is a REAL ANSWER on this door rather than a
// failure — the lane came good, or the request ended, or the offer aged out
// ([provider.AnswerOffer] says so). A person on an old engine presses `y` and
// nothing happens, which is exactly what happened before the door existed.

// Frame is one line on the wire, either direction.
type Frame struct {
	// Kind says what this frame is: "hello", "welcome", "call", "result",
	// "event", "closed", "facts", "task", "design", "question", "phase", "lane",
	// "turn", "driver", "moved", "fatal".
	//
	// "facts", "task", "design", "question", "phase" and "lane" are the KINDS
	// THAT ANSWER NOTHING. The third and fourth are version 11's harness lane
	// and version 14's questions lane, and each carries one [EventWire],
	// exactly as "task" does (standinglane.go); the last two carry the phase
	// clock and the lane sighting (news.go). Every other
	// frame from the engine either replies to a call or belongs to a stream a
	// call opened; these are the engine saying something the surface did
	// not ask for on that frame, because the whole point of them is that the
	// surface never has to ask. "facts" carries a [FactsPush]; "task" carries
	// one [EventWire] off the standing task lane (tasklane.go); "phase" carries
	// a [PhaseWire] and "lane" a [LaneWire], both off the news desk this engine
	// keeps for every conversation it is running (news.go). None of them has an
	// ID, and a build that does not know the kind ignores it, which is what the
	// reader in client.go already does with every kind it has no case for.
	Kind string `json:"kind"`
	// ID correlates a call with its result, and an event with the Submit that
	// opened its stream. The client mints call ids; the server mints stream ids
	// and names the stream in the call's result.
	ID uint64 `json:"id,omitempty"`
	// Method is the call's name, on "call" frames only — a [Method] constant.
	Method string `json:"method,omitempty"`
	// Payload is the frame's body, shaped by Kind and Method.
	Payload json.RawMessage `json:"payload,omitempty"`
	// Encoding and Data carry a large payload after both ends agreed on the
	// encoding at the version door. Payload stays the ordinary representation
	// so an older peer sees exactly the frames it has always seen; Data is only
	// emitted to a peer whose hello named this encoding.
	Encoding string `json:"encoding,omitempty"`
	Data     []byte `json:"data,omitempty"`
	// Error is a call that failed, on "result" frames, and the reason on
	// "fatal" frames.
	Error string `json:"error,omitempty"`

	// Seq is this event's position in its stream, counting from 1, on "event"
	// and "closed" frames only.
	//
	// IT EXISTS SO A REATTACH CAN ASK FOR THE GAP AND NOT FOR THE
	// CONVERSATION. A surface that dropped mid-turn has already drawn some of
	// that turn; the transcript door would give it the finished shape of what
	// it half-has, and the journal does not hold a partial reply's deltas at
	// all. So the engine keeps the turn's events in memory while it runs, the
	// returning surface says how far it got ([Hello.Resume]), and the engine
	// sends what came after. Numbering from 1 makes zero mean "nothing of this
	// stream has been seen", which is the state a surface attaching for the
	// first time is in.
	//
	// A "closed" frame carries the seq of the LAST event it follows, so a
	// surface can tell a stream that ended from one it lost the tail of.
	Seq uint64 `json:"seq,omitempty"`
}

// The methods, one per door. The Agent group mirrors tui3.Agent exactly (plus
// the rewind pair rewind.go type-asserts for); the Session group is the doors
// only a remote surface needs, because a local one reads the disk directly.
const (
	// Agent — payloads are the method's own argument struct below; results are
	// the return values likewise.
	MethodSubmit          = "Submit"          // SubmitArgs → StreamRef, then "event" frames
	MethodSubmitImage     = "SubmitImage"     // SubmitImageArgs → StreamRef, then "event" frames
	MethodSubmitFiles     = "SubmitFiles"     // SubmitFilesArgs → StreamRef, then "event" frames
	MethodFollowUp        = "FollowUp"        // SubmitArgs → StreamRef, then "event" frames
	MethodQuestionReplace = "ReplaceQuestion" // QuestionArgs → StreamRef
	MethodSteer           = "Steer"           // SubmitArgs → StreamRef, then "event" frames
	// MethodTyping is a person having started writing, and it is the only frame
	// on this wire that nobody waits for ([Agent.Typing]).
	//
	// IT IS ONE OF THE TWO HALVES THAT WERE MISSING FROM THE PROBE.
	// internal/session's [session.Agent.Typing] buys a measurement of the two
	// machines the next turn is most likely to use, and the second thing it
	// buys is a WARM CONNECTION, so the real request's first token is not also
	// paying for a handshake (internal/provider's probe.go says so in its own
	// header). The surface asks for it through an optional interface, and until
	// this door existed the assertion simply failed on the default road — which
	// is every launch that is not `--no-host`. The other half was inside the
	// engine and is mended in the same change (typing.go's header): the
	// completer wrapper was swallowing the probing door, so the measurement had
	// never been bought in process either.
	//
	// IT RIDES VERSION 14 RATHER THAN MOVING THE NUMBER, under the rule stated
	// on [Version]: an engine that does not know it answers "no such method",
	// the surface drops the answer it was never waiting for, and what is lost is
	// a pre-warm nobody can see. Nothing goes dark, so nothing is refused at the
	// door.
	MethodTyping          = "Typing"                 // nothing → nothing, and nothing waits
	MethodStopWork        = "StopWork"               // nothing → nothing; stop this conversation, retaining history
	MethodInterrupt       = "Interrupt"              // InterruptArgs, or nothing → nothing
	MethodCompact         = "Compact"                // nothing → nothing (error carries the failure)
	MethodClose           = "Close"                  // nothing → nothing
	MethodModel           = "Model"                  // nothing → string
	MethodSetModel        = "SetModel"               // string → nothing
	MethodSetContext      = "SetContextWindow"       // legacy version-5 hint; current remote surfaces do not send it
	MethodReasoningFor    = "ReasoningFor"           // string → string
	MethodSetReasoningFor = "SetReasoningFor"        // ReasoningArgs → nothing
	MethodConsent         = "ResolveConsent"         // ConsentArgs → nothing
	MethodConsentRemember = "ResolveConsentRemember" // ConsentArgs → nothing
	MethodStandingResolve = "ResolveStanding"        // StandingArgs → nothing
	// MethodQuestionResolve is the ONE door for an answer to any question, over
	// the wire (docs/design/questions/DESIGN.md). It carries
	// [session.Answer] whole — the lane, the token, the keys, the words beside
	// them, the notes on parts, the exchanges, the blanks, the dial, the scope —
	// and the engine reads the lane off it and applies it through that lane's own
	// resolver ([session.Agent.ResolveQuestion]).
	//
	// IT IS ONE FRAME AND NOT ELEVEN because the object it carries already says
	// which lane it belongs to. The per-lane frames above stay exactly as they
	// are: they are what an older window on the other end of this wire sends, and
	// this one is what a window that has the whole object sends.
	MethodQuestionResolve = "ResolveQuestion" // QuestionArgs → nothing (or a refusal)
	// MethodQuestionHold is the OTHER thing a key on a question can mean: stop
	// the clock, do not answer. A question with a deadline takes the asker's own
	// pick when it runs out ([session.PolicyRecommendThenAuto]), and a person
	// reading it has to be able to stop that without deciding anything —
	// [MethodTaskHold] is the same act for the one lane that had it first, and
	// this is the door for every lane, named the way an answer is named: the lane
	// and the lane's own token.
	//
	// IT NEVER MAKES A KEY WAIT. The surface sends it and carries on; the engine
	// says the question again with its deadline gone, so every window stops
	// counting from the same frame rather than from its own guess.
	MethodQuestionHold = "Question.Hold" // QuestionHoldArgs → nothing
	// MethodQuestionWatch is the surface saying it draws questions, and it buys
	// exactly what [MethodTaskWatch] and [MethodDesignWatch] buy: "question"
	// frames from here on, including everything already open replayed the
	// moment the subscription opens ([session.Agent.WatchQuestions]). It is sent
	// once per conversation the surface takes up, never on a frame.
	//
	// IT IS THE OTHER HALF OF THE DOOR ABOVE. An answer with no way for the
	// question to arrive is a key nobody will ever press; internal/tui3 asserts
	// the lane and the answer as ONE interface for that reason, and a wire
	// carrying one of them leaves a turn stopped on a question no screen shows.
	MethodQuestionWatch = "Question.Watch" // nothing → nothing, then "question" frames
	// MethodSetAutonomy is `D`: let the engine answer every question of one SHAPE
	// from now on ([session.Agent.SetAutonomy]). The setting is kept per project
	// on the engine's side, which is why it is a call and not a local file: a
	// window attached over `--host` is setting the dial on the machine the work
	// is happening on.
	MethodSetAutonomy = "SetAutonomy" // AutonomyArgs → nothing (or a refusal)
	// MethodAutonomy IS THE OTHER HALF OF THE DOOR ABOVE, and it was missing.
	// A surface could WRITE one of these rules over the wire and never read one
	// back, so every window on the ordinary road — the surface talks to its own
	// engine process through exactly this client — asked the question and got
	// "this conversation has no project to keep question rules in", whatever
	// project it was in. `/autonomy` printed that sentence on a machine with the
	// rules sitting in `.codeaf/autonomy.json`, and the settings rows that read
	// the same door drew nothing at all.
	MethodAutonomy      = "Autonomy"          // nothing → map[AskKind]Policy
	MethodHarness       = "ResolveHarness"    // HarnessArgs → nothing
	MethodConnect       = "ResolveConnect"    // ConnectArgs → nothing
	MethodConnectKey    = "ResolveConnectKey" // ConnectArgs → nothing
	MethodNoteConnected = "NoteConnected"     // ConnectedArgs → nothing
	MethodTitle         = "Title"             // nothing → string
	MethodUsage         = "Usage"             // nothing → session.Usage
	MethodContextTokens = "ContextTokens"     // nothing → int
	MethodTranscript    = "Transcript"        // nothing → []session.DisplayEntry
	MethodEarlier       = "EarlierHistory"    // nothing → session.EarlierHistory
	MethodRewindPoints  = "RewindPoints"      // nothing → []session.RewindPoint
	MethodRewindAt      = "RewindAt"          // int → []session.DisplayEntry
	// MethodPlanSpend CARRIES THE RUN'S SPEND-BY-SEAT ACROSS THE WIRE, and it
	// is the spend page's other reading beside the machine's own ledger above.
	// A conversation that seeded a plan writes its workers' calls into the plan
	// store's spend ledger, and the page draws those rolled up by SEAT
	// ([session.PlanSpendLine]); over a connection that store lives on the
	// engine's disk, so a surface that could not ask the engine drew no block
	// at all ([session.Agent.PlanSpend]).
	//
	// IT RIDES [Version] RATHER THAN MOVING IT, under the rule stated there: an
	// engine that does not know it answers "no such method", the surface reads
	// that as the block being absent HERE — which is exactly what it drew before
	// this door existed — and the emptiness law is kept. Nothing that was drawn
	// goes dark, so nothing is refused at the door.
	MethodPlanSpend         = "PlanSpend"         // PlanSpendArgs → []session.PlanSpendLine
	MethodPlanTasks         = "PlanTasks"         // nothing → []session.PlanTaskRow
	MethodPlanTaskPage      = "PlanTaskPage"      // PlanTaskPageArgs → PlanTaskPageResult
	MethodPlanNote          = "PlanNote"          // PlanTextArgs → nothing
	MethodPlanPause         = "PlanPause"         // PlanTaskArgs → nothing
	MethodPlanResume        = "PlanResume"        // PlanTaskArgs → nothing
	MethodPlanCancel        = "PlanCancel"        // PlanTaskArgs → nothing
	MethodPlanAmend         = "PlanAmend"         // PlanTextArgs → nothing
	MethodPlanPriority      = "PlanPriority"      // PlanPriorityArgs → nothing
	MethodPlanRunSummary    = "PlanRunSummary"    // PlanRunSummaryArgs → PlanRunSummaryResult
	MethodRefreshRunSummary = "RefreshRunSummary" // RefreshRunSummaryArgs → PlanRunSummaryResult
	// The conversation's own place on the thinking ladder (internal/session's
	// effort.go). Three doors and not one, because the stored rung and the
	// resolved rung are two different answers: the dial DRAWS the resolved one
	// and a person opening it CHOSE the stored one, and a wire that carried only
	// one of them would make the surface derive the other.
	//
	// The resolved rung also rides [session.Facts] unasked, which is what a frame
	// reads; these are the keystroke's doors (effort.go).
	MethodEffort         = "Effort"         // nothing → string (the stored rung, "" for none)
	MethodResolvedEffort = "ResolvedEffort" // nothing → string (the rung the next turn asks for)
	MethodSetEffort      = "SetEffort"      // string → bool (false when the word is not a rung)

	// The skills a person puts in front of this conversation by hand, and the
	// shelf they are chosen from (internal/session's skillattach.go, and
	// skills.go here). The attachment is the SESSION'S — it is held beside the
	// conversation and read on every message it sends — so a surface on the
	// other end of a socket reaches it through these doors rather than holding
	// a copy of its own.
	MethodAttachSkills   = "AttachSkills"   // []string → []string (the set as it now stands)
	MethodDetachSkill    = "DetachSkill"    // string → bool (whether it was on)
	MethodAttachedSkills = "AttachedSkills" // nothing → []string
	MethodClearSkills    = "ClearSkills"    // nothing → int (how many were on)
	MethodSkillShelf     = "SkillShelf"     // SkillShelfArgs → []store.Fact

	// The conversation's own posture on the tool gate (internal/session's
	// approvalposture.go), the dial above one door over: the resolved posture
	// rides [session.Facts] unasked for the frame, and these are the keystroke's
	// doors (approval.go). The set answers the refusal as a sentence rather than
	// a bool because the local door answers an error and the surface prints it.
	MethodResolvedApproval = "ResolvedApproval" // nothing → string (the posture in force)
	MethodSetApproval      = "SetApproval"      // string → string ("" took, else the refusal)

	// MethodAnswerLaneOffer answers the one question the phase seam can raise:
	// the machine a person PINNED has gone quiet, there is somewhere else to
	// go, and a pin is asked rather than overridden ([provider] offer.go). The
	// surface presses `y` and this is the road that keystroke takes home.
	//
	// IT EXISTS BECAUSE THE PHASE CROSSED. Until "phase" frames did, a hosted
	// engine had nobody reading phases at all and BORROWED the other lane
	// without asking — which was right, since a question nobody can hear is a
	// wait that never ends. With the phase on the wire the question is asked
	// and drawn, so the answer needs the same road back or the surface would be
	// showing `switch to auto? (y)` over a key that does nothing.
	//
	// It takes the ANSWER and not the question: which lane the rescue goes to
	// was settled when the offer was raised, because the moment a rescue is
	// wanted is the worst possible moment to start choosing one. False means
	// there was nothing to answer, which is a real answer and not a failure.
	MethodAnswerLaneOffer = "AnswerLaneOffer" // bool → bool

	// Session doors.
	MethodSessionsRecent = "Sessions.Recent" // nothing → []session.Summary
	MethodSessionNew     = "Session.New"     // nothing → Welcome (the engine swaps to a fresh session)
	MethodSessionOpen    = "Session.Open"    // string (path) → Welcome (the engine swaps to that session)

	// Standing doors. They are in the Session group and not the Agent one
	// because they are about the ENGINE MACHINE'S STORE rather than about the
	// conversation: a local surface opens internal/standing on its own disk and
	// a remote one cannot, which is the same reason Sessions.Recent exists. The
	// items belong to the machine that runs them, so a session swap leaves them
	// exactly where they were.
	MethodStandingItems = "Standing.Items" // string (workspace) → []standing.Item
	MethodStandingSave  = "Standing.Save"  // standing.Item → nothing (the error carries a refused write)
	MethodStandingWatch = "Standing.Watch" // nothing → StandingWatchResult

	// The PLACES doors, and they are in the Session group for the reason
	// Sessions.Recent and Standing.Items are: each one is a reading of THE
	// ENGINE MACHINE'S DISK rather than of the conversation. A local surface
	// walks its own state root and reads its own ledger; a remote one has no
	// way to, and every screen it drew off this laptop's copy was a confident
	// answer about the wrong machine.
	//
	// ONE METHOD PER READING, NEVER ONE THAT ANSWERS EVERYTHING. A single
	// "Places.All" would tie a place that wants the world on the beat to a
	// full-text search nobody asked for, and would have to grow a shape of its
	// own — where these carry internal/session's and internal/store's own types
	// unchanged, which is Decision 1.

	// MethodPlacesWorld is the walk of the engine machine's places root: every
	// project, every conversation in it, and the work each of those ran
	// ([session.ReadWorld]). It is the reading FIVE of the surface's seven
	// places are built from — home lists it, tasks reads the task rows inside
	// it, standing walks its projects to ask what else keeps an eye on that
	// machine, spend joins its titles onto the ledger's ids, and search opens
	// the conversation behind a hit out of it — so one door answers all five.
	MethodPlacesWorld = "Places.World" // nothing → session.World
	// MethodPing is one empty frame out and one empty frame back. The surface
	// times that round trip on its own machine; a timestamp carried between two
	// machines would mix clocks that need not agree and would not measure the
	// path the person is actually waiting on.
	MethodPing = "Ping" // nothing → nothing

	// MethodPlacesTask is ONE ROW of that record, read deeper than the walk
	// reads it: the last thing that piece of work said, out of the journal it
	// left on the engine machine's disk ([session.TaskRecord]).
	//
	// IT IS A SECOND DOOR AND NOT A FIELD ON THE WORLD, for what it costs. The
	// walk is taken on a beat and answers five places; a report is a forward scan
	// of a whole session journal and is wanted for exactly one row — the one
	// somebody just pressed. Putting it on the walk would read four hundred
	// journals to draw a list that shows none of them.
	//
	// AND IT IS NOT [MethodFetchFile]. That door answers under the two-roots law
	// — this conversation's workspace and this conversation's own folder — and a
	// task's journal is in ANOTHER conversation's folder under the state root, so
	// a fetch of it is refused, correctly. This one answers under its own root,
	// the places root, and hands back the sentence rather than the file: a
	// transcript is megabytes and the card draws one paragraph of it.
	MethodPlacesTask = "Places.Task" // PlacesTaskArgs → session.TaskRecord

	// ── version 2 ───────────────────────────────────────────────────────────

	// MethodDetach is a surface LEAVING ON PURPOSE, and it is the one method
	// whose whole value is the difference between it and silence.
	//
	// Version 1 had no way to say this, so a closed window and a dead pipe were
	// the same event and the engine had to treat both as an interrupt. That was
	// the right reading of a closed window and the wrong reading of a dropped
	// connection, and the person could not tell the two apart either — they
	// closed a laptop lid and lost a running turn.
	//
	// Version 2 splits them. Detach says "this surface is going; the turn is
	// yours to finish", and the engine keeps working, keeps the events, and
	// holds any question it raises ([HeldQuestion]). A pipe that simply dies
	// means the same thing — the engine assumes the surface will be back — and
	// the deliberate END of a conversation is what [MethodClose] has always
	// been. So the three roads out finally read as three different things.
	MethodDetach = "Detach" // nothing → nothing

	// MethodFetchFile is the reverse of an attachment: the surface asking for
	// the bytes of a file the ENGINE holds, by a path on the engine's disk.
	//
	// IT IS WHAT MAKES `/export` AND A DOWNLOADED DELIVERABLE HONEST. Version 1
	// had no door for moving a byte from the engine machine to the surface's,
	// so /export assembled what the surface happened to be holding and said
	// `· on this machine`, and a file the session MADE could not be brought
	// here at all. The path is never resolved on this side — it is the engine's
	// path, the way every path on a "result" already is.
	MethodFetchFile = "Fetch.File" // FetchFileArgs → FetchedFile

	// MethodHeldQuestions is what a surface asks the moment it attaches: the
	// questions this session raised while nobody was looking. See
	// [HeldQuestion] for why they wait rather than expire.
	MethodHeldQuestions = "Held.Questions" // nothing → []HeldQuestion

	// MethodListDir is one directory of the engine's, as a listing rather than
	// as bytes: what the browse view and the surface's file picker over a
	// connection read. It answers under the SAME two-roots law as
	// [MethodFetchFile] (file.go's handOver): the workspace and the session's
	// own folder, and nothing outside them crosses.
	MethodListDir = "List.Dir" // ListDirArgs → DirListing

	// MethodStatPaths is the honesty rule of tui3's pathlink.go carried over
	// the wire: nothing on a hosted session becomes a link until the ENGINE
	// says the path exists, because a stat is a fact about the other machine.
	// It is batched — one call per burst of new rows, never one per word.
	MethodStatPaths = "Stat.Paths" // StatPathsArgs → []PathFact

	// MethodDepositFile is [MethodFetchFile] walked backwards: a file going
	// from the surface's machine to the engine's, and NOT AS A MESSAGE.
	//
	// IT EXISTS BECAUSE THE BROWSE PAGE HAS A DRAG-DROP LANE AND THE WIRE HAD
	// NOWHERE TO PUT WHAT LANDED ON IT. [MethodSubmitFiles] already writes a
	// person's files into a session's attachments, but it is a MESSAGE: the
	// engine keeps the bytes and then opens a turn on them. A file dropped on
	// a web page is not a sentence anybody said, so submitting it would start a
	// turn nobody at this end asked for, spend somebody's money on it, and
	// stream its answer into a channel that page is not reading.
	//
	// SO THIS METHOD KEEPS AND DOES NOTHING ELSE. The bytes land in the far
	// session's attachments/ folder — the same place [SubmitFilesArgs]'s files
	// land, under the same name law, and NOWHERE ELSE; an arbitrary path on the
	// engine's disk is not a thing this wire will ever write to. No turn opens,
	// no event is sent, nothing reaches the transcript: a deposit is a FACT ON
	// DISK, and the conversation learns of it only when a person mentions it.
	// The lane for a person's own message with a file on it is still /attach.
	MethodDepositFile = "Deposit.File" // WireFile → DepositedFile

	// ── version 4 ───────────────────────────────────────────────────────────

	// MethodTake is a surface asking for the keyboard back, and it is the whole
	// of what a watcher can do besides watch.
	//
	// IT IS ONE ROUND TRIP AND NEVER A RECONNECT. A person who walked back to a
	// machine and pressed enter must be typing a moment later, not waiting on a
	// handshake — so taking the keyboard moves one field on the engine and fans
	// one frame out to the room. The connection underneath it never moved.
	//
	// The ENGINE decides, and it is the only thing that does: it answers this,
	// it tells every surface what changed ([Driver]), and it refuses a Submit
	// from a surface that is not the driver. A surface that decided locally that
	// it was now driving would be the second authority on a fact that can only
	// have one, and the failure would be two windows both believing they had the
	// keyboard.
	MethodTake = "Take" // nothing → nothing
)

// LaunchShape is the handful of flags that describe how a session is built
// rather than what is said in it: --yolo, --no-compact, --one-model and the two
// unattended ceilings. They are one struct because they are one decision — the
// shape a conversation was opened with — and because comparing two of them is
// how a surface tells "the engine built what I asked for" from "I joined
// somebody else's conversation".
type LaunchShape struct {
	Yolo      bool    `json:"yolo,omitempty"`
	NoCompact bool    `json:"noCompact,omitempty"`
	OneModel  bool    `json:"oneModel,omitempty"`
	MaxHours  float64 `json:"maxHours,omitempty"`
	MaxCost   float64 `json:"maxCost,omitempty"`
	// Interactive says the surface that opened this conversation is a screen
	// somebody is typing into, so the session is steered rather than run to a
	// goal of its own. Only the local dial fills it; a --once probe leaves it
	// unset, and the engine maps it onto the session's own steering fact.
	Interactive bool `json:"interactive,omitempty"`
}

// Same reports whether two shapes describe the same conversation. A nil shape
// is the engine's defaults, so nil and a zero shape are the same thing.
func (l *LaunchShape) Same(other *LaunchShape) bool {
	var mine, theirs LaunchShape
	if l != nil {
		mine = *l
	}
	if other != nil {
		theirs = *other
	}
	return mine == theirs
}

// StandingWatchResult keeps "not installed" distinct from "could not read".
type StandingWatchResult struct {
	Status standing.WatchStatus `json:"status"`
	Known  bool                 `json:"known"`
}

// Hello is the client's first frame ("hello"). Workspace is the path AS TYPED
// after the colon — empty means the engine's own home — and the engine answers
// with the path it resolved.
type Hello struct {
	// Headless says this caller cannot answer approval questions. The engine
	// requires explicit approval settings instead of the interactive default.
	Headless  bool   `json:"headless,omitempty"`
	Version   int    `json:"version"`
	Workspace string `json:"workspace,omitempty"`
	// Session is an explicit session file to open, empty for the workspace's
	// latest-or-new (the same meaning the --session flag has locally).
	Session string `json:"session,omitempty"`

	// Model and Level are --model and --reasoning, carried in the frame that
	// BUILDS the session rather than applied to it a millisecond later.
	//
	// They retire a stub. Version 1 had no room for them, so the door set them
	// immediately after the handshake (cmd/codeaf's applyHostChoices), which
	// worked for every turn the person could type but left the session file's
	// first line naming the model the session was BORN on rather than the one
	// they asked for. Nobody on the screen could see the difference; the
	// journal could, and the journal is the record.
	Model string `json:"model,omitempty"`
	Level string `json:"level,omitempty"`

	// Launch is how the conversation should be BUILT, when this hello is the
	// one that opens it. Nil asks for the engine's own defaults, which is what
	// every remote surface sends: these are settings of the machine the session
	// runs on, and cmd/codeaf refuses them over --host and --at by name.
	Launch *LaunchShape `json:"launch,omitempty"`

	// Encodings are the optional frame payload encodings this surface can read.
	// They are negotiated INSIDE one protocol version because absence means the
	// old JSON payload and every added field is omitempty: an older engine
	// ignores this offer, and a newer engine sends it nothing encoded.
	Encodings []string `json:"encodings,omitempty"`

	// Resume is how far this surface got before it went away, one entry per
	// stream it still cares about. Empty is a surface that has seen nothing,
	// which is every first attach.
	//
	// THE ENGINE ANSWERS THE GAP AND NOTHING ELSE. Each cursor names a stream
	// and the last [Frame.Seq] this surface actually drew; the engine replays
	// from the one after it and then carries on live. A stream the engine no
	// longer holds — it finished long ago, or this is a different engine — is
	// answered with nothing rather than with an error: the transcript is the
	// authority on a finished turn, and the surface reads that anyway.
	Resume []StreamCursor `json:"resume,omitempty"`

	// ── version 4 ───────────────────────────────────────────────────────────

	// Surface is this machine's short name — `macbook`, `spark` — as
	// [MachineName] reads it off os.Hostname.
	//
	// IT EXISTS SO A SCREEN CAN NAME THE WINDOW THAT HAS THE KEYBOARD. "somebody
	// else is typing" is a sentence that makes a person hunt; "typing from spark
	// now" is one they can act on, because they know where spark is. Two windows
	// on ONE machine send the same name, which is how the engine can tell the
	// far desk from the forgotten terminal behind this one.
	//
	// IT IS A LABEL AND NEVER AN IDENTITY. Nothing is authorized by it — a
	// connection is already whatever ssh or a pinned device key made it — and
	// the engine sanitizes it before it is drawn ([machineLabel]) because it is
	// text one machine sends for another machine's screen.
	Surface string `json:"surface,omitempty"`

	// Back says this surface has been in this conversation before and is coming
	// back from a link that dropped, rather than arriving for the first time.
	//
	// IT IS THE ONE THING THAT KEEPS "THE NEWEST WINDOW DRIVES" HONEST. A redial
	// is an attach the person did not make: they closed a laptop lid in one city
	// and started typing in another, and the lid's machine reconnecting in the
	// background half an hour later must not take the keyboard off the machine
	// they are sitting at. So a returning surface drives only if the keyboard is
	// going spare, and a NEW one always drives.
	Back bool `json:"back,omitempty"`

	// Join says this hello wants a conversation THAT IS ALREADY OPEN and will
	// take nothing else. [Session] names the transcript to look for, and a host
	// that is not running it answers an error rather than starting it.
	//
	// IT EXISTS BECAUSE "OPEN OR CREATE" IS THE WRONG VERB FOR A SECOND VIEW. A
	// surface that wants to READ one task of a conversation running next door is
	// asking about work that exists; booting a whole session so that the question
	// has an answer would start a model, take the transcript's lock away from
	// nobody, and hand back a conversation with none of the work in it. So the
	// two intentions are two flags rather than one hopeful one.
	//
	// AND IT IS MATCHED ON THE TRANSCRIPT AND NOT ON THE KEY. A host keys its
	// conversations by whatever the FIRST hello said — which for the ordinary
	// launch is the empty string, meaning "this workspace's latest" — so a second
	// surface naming the same conversation by its file would miss it and be given
	// a new one. The file is the identity every other part of this program uses
	// for a conversation, so it is the one a join is answered on.
	Join bool `json:"join,omitempty"`

	// New says this hello MINTS A CONVERSATION OF ITS OWN and will not be given
	// one that is already open. It is the exact opposite of [Join], and the two
	// are separate flags for the same reason Join is separate from an ordinary
	// hello: "open or create" is the wrong verb for both intentions.
	//
	// IT IS WHAT MAKES A SECOND TAB A SECOND CONVERSATION. An ordinary hello
	// naming no session means "this workspace's latest-or-new", so two surfaces
	// that both say nothing are asking for the SAME conversation — which is the
	// whole of "sit down somewhere else and be in it" and exactly wrong for a
	// window opening another chat beside the one it already has. Without this
	// flag the only way to mint one was [MethodSessionNew], which SWAPS the
	// conversation on the connection that asked and ends the one it replaced
	// (internal/remote's Session.swap): one connection, one conversation, and the
	// sentence a person read on screen.
	//
	// THE ENGINE CHOOSES THE FILE. This hello carries no session, because the
	// transcript a new conversation lands on is a question about the engine's own
	// disk; the welcome names what it opened, and a host keys the conversation by
	// that answer so a later window can name it and join.
	//
	// A REDIAL NEVER SAYS IT TWICE. [Client.helloNow] clears it once a welcome is
	// in hand, alongside the session and [Back] it already rewrites — a link that
	// dropped is coming back to the conversation it minted, not asking for
	// another one.
	New bool `json:"new,omitempty"`

	// Watch says this surface is HERE TO READ and must never be given the
	// keyboard — not on arrival, not when the driver leaves, not ever.
	//
	// [Back] IS NOT THIS, and reading it as this is the bug that made the flag
	// necessary. A returning surface takes the keyboard when it is going spare,
	// which is right for a redial and wrong for a second view somebody opened to
	// look at one piece of work: the window that owns the conversation may simply
	// have detached for a moment, and it would come back to find a reader driving
	// it. A watcher is refused the keyboard even when there is no driver at all,
	// so the conversation is left with none rather than with the wrong one.
	//
	// IT IS THE SURFACE'S OWN DECLARATION AND THE ENGINE STILL DECIDES. Nothing
	// here is a permission — the engine enforces it, in the one place that owns
	// who drives (driver.go) — and a watcher that tries to type anyway is refused
	// with a sentence rather than dropped.
	Watch bool `json:"watch,omitempty"`
}

// StreamCursor is one "I have seen this stream through here".
type StreamCursor struct {
	Stream uint64 `json:"stream"`
	Seq    uint64 `json:"seq"`
}

// Welcome is the server's answer ("welcome"): the facts a surface needs before
// its first frame, which are the same facts newApp reads off a local agent.
type Welcome struct {
	Version     int    `json:"version"`
	Workspace   string `json:"workspace"`
	SessionFile string `json:"sessionFile"`
	Resumed     bool   `json:"resumed"`
	Model       string `json:"model"`
	Build       string `json:"build,omitempty"`
	Title       string `json:"title,omitempty"`
	ShortTitle  string `json:"shortTitle,omitempty"`
	// Note is a sentence worth showing once — "session open elsewhere, started
	// a new one" travels here.
	Note string `json:"note,omitempty"`
	// ApprovalMode is the engine machine's own answer to "does a tool run
	// without asking" (internal/config's ToolApprovalModeAt) — "allow" or
	// empty. A REMOTE YOLO BADGE MUST NAME THE ENGINE'S POSTURE, NOT THIS
	// LAPTOP'S: the gate that decides whether a tool runs unattended is read
	// from the profile on the machine that runs it, and drawing the badge from
	// the surface's own settings would be a safety claim about a machine
	// nobody consulted.
	ApprovalMode string `json:"approvalMode,omitempty"`
	// BashBackgroundAfterSeconds is the foreground-command clock the ENGINE
	// armed for this session. A HOSTED COUNTDOWN MUST READ THIS MACHINE'S
	// POSTURE, NOT THE SURFACE'S PROFILE: zero is a real off answer, so absence
	// cannot be filled from a local default without inventing a deadline.
	BashBackgroundAfterSeconds int `json:"bashBackgroundAfterSeconds,omitempty"`
	// ProfileDir is the engine process's resolved profile directory. A plain
	// linked-local surface uses it for writes because the daemon may predate the
	// terminal's current profile override. An older peer sends none and the
	// surface falls back to its own resolved directory; linked-local launches
	// retire a daemon whose build differs, so that compatibility reading is
	// theoretical on the road that consumes it. No protocol version moves.
	ProfileDir        string   `json:"profileDir,omitempty"`
	UnreadProfileKeys []string `json:"unreadProfileKeys,omitempty"`
	// Encoding is the one frame payload encoding selected from Hello.Encodings,
	// or empty when this connection stays on ordinary JSON payloads.
	Encoding string `json:"encoding,omitempty"`

	// PlacesRoot is the engine machine's own state root — the directory
	// [MethodPlacesWorld] walked, on the disk it walked it on.
	//
	// IT TRAVELS BECAUSE A WORLD IS A SET OF PATHS AND A PATH NEEDS ITS DISK.
	// [session.World.Adopt] puts the conversation THIS WINDOW is sitting in back
	// into a walk that was taken too early to see it, and it needs the root that
	// walk was taken under to work out which bucket the conversation belongs to.
	// Handing it this laptop's root would file a session that lives on the server
	// under a project on the laptop. Empty is an engine that answers no world,
	// which is version 3 and every build before it.
	PlacesRoot string `json:"placesRoot,omitempty"`

	// ── version 2 ───────────────────────────────────────────────────────────

	// Live is the stream still running when this surface arrived, or zero.
	//
	// IT IS THE WHOLE POINT OF THE PERSISTENT ENGINE, said in one number: a
	// person asked for a long refactor from a café, closed the laptop, and sat
	// down somewhere else — and this field is how the new surface learns there
	// is a turn in flight to reattach to rather than an idle session to type
	// at. The events of that turn arrive as ordinary "event" frames from
	// [Hello.Resume]'s cursor onward, so nothing about drawing it is special.
	Live uint64 `json:"live,omitempty"`

	// Attached is how many OTHER surfaces are on this session right now.
	//
	// It is carried because a surface that is not alone must be able to say so:
	// two people (or one person and their own forgotten window) sharing a
	// conversation is a fact about that conversation, and a screen that hid it
	// would be the one place codeaf lied about who is in the room. Zero is the
	// ordinary case and draws nothing, by the emptiness law.
	Attached int `json:"attached,omitempty"`

	// Held is the questions this session raised while nobody was attached,
	// carried in the welcome so the first frame a returning surface draws
	// already has them. See [HeldQuestion].
	Held []HeldQuestion `json:"held,omitempty"`

	// Facts is the fact set this surface arrives holding — the model, the name,
	// the spending, the weight, the reasoning levels — so the FIRST frame it
	// draws is drawn from memory and not from four round trips.
	//
	// It is a pointer so that "this engine states nothing" is a thing a decoder
	// can see. Nothing on the surface has to handle that case today (the door
	// refuses a version mismatch before the screen exists), but a nil here and a
	// zero-valued fact set are different facts, and a replica filled from the
	// second would draw a conversation with no model and nothing spent.
	Facts *FactsPush `json:"facts,omitempty"`

	// Persistent says the far end is a session HOST — the engine outlives this
	// connection — rather than version 2's other honest shape, a one-shot
	// engine on a pipe.
	//
	// A SURFACE MUST NOT PROMISE A LIFETIME THE ENGINE DOES NOT HAVE. Both
	// shapes speak this protocol and both are legitimate: `codeaf engine`
	// started by hand on a machine with no host is still a conversation, it
	// simply ends when the pipe does. The screen's word for detaching, and
	// whether "close the lid, it keeps going" is true, both hang off this
	// single fact, so it is stated rather than assumed from the transport.
	Persistent bool `json:"persistent,omitempty"`

	// Launch is the shape the conversation actually has, as the engine built
	// it. It is an ECHO and not a confirmation: a hello that asked for one
	// shape and joined a conversation somebody else had already opened gets
	// that conversation's shape here, and the surface is expected to notice.
	Launch *LaunchShape `json:"launch,omitempty"`

	// ── version 4 ───────────────────────────────────────────────────────────

	// Driver is who holds the keyboard the moment this surface arrived, told
	// the way this surface should read it. A first attach is always the driver;
	// a [Hello.Back] one may not be.
	//
	// IT IS CARRIED ON THE WELCOME AND NOT LEFT TO THE FIRST "driver" FRAME,
	// because a frame is only sent when the answer CHANGES and a returning
	// surface can arrive into an answer that did not. A surface that assumed it
	// drove until told otherwise would draw a composer somebody's keystrokes
	// would then be refused into.
	Driver Driver `json:"driver,omitzero"`

	// SteerRepeat says this engine RECOGNISES A SEND IT HAS ALREADY TAKEN: a
	// correction sent into a task's page again under the same identity
	// ([TaskSteerArgs]) answers the receipt already on the record and delivers
	// nothing.
	//
	// IT IS CARRIED BECAUSE THE QUESTION IS ASKED BEFORE ANYTHING IS SENT, and
	// it has to be. A surface holding a send it got no answer to has exactly two
	// moves — ask again, or keep the words and say so — and which one is honest
	// depends on a fact about the far machine that no failed call can report:
	// the call that would have told it is the one that stopped answering.
	//
	// ABSENCE IS false AND false IS THE SAFE READING. An engine that predates
	// this ignores the identity on a send, so a repeat there would be a second
	// correction; the surface therefore keeps the person's words instead of
	// asking twice, which is the degradation that costs a keystroke rather than
	// the one that corrects a worker twice.
	SteerRepeat bool `json:"steerRepeat,omitempty"`

	// SteerOwner says this engine CHECKS THE CONVERSATION A SEND WAS WRITTEN FOR
	// ([TaskSteerArgs.Session]) against the one it actually has open, and refuses
	// rather than delivering to the task with that number over here.
	//
	// IT IS A SEPARATE FACT FROM SteerRepeat and may not be inferred from it: an
	// engine can keep send identities and still have been built before this check
	// existed, and it would then read Session as an unknown field and deliver.
	//
	// ABSENCE IS false, AND false MEANS THE SURFACE MUST NOT SEND A BOUND
	// CORRECTION AT ALL. An unenforced claim is worse than no claim: the surface
	// would believe the engine was guarding something nobody is guarding.
	SteerOwner bool `json:"steerOwner,omitempty"`
	// TaskSetup advertises task-scoped model and thinking controls.
	// TaskRetry advertises retrying incomplete tasks in place.
	TaskRetry bool `json:"taskRetry,omitempty"`
	TaskSetup bool `json:"taskSetup,omitempty"`
	// TaskSettle says this engine can be ASKED TO DECIDE A LANDING — accept, not
	// right, one more merge round, the hand-over and the take-back
	// ([MethodTaskSettle]).
	//
	// IT IS CARRIED FOR [Welcome.Folders]'S REASON, and the cost of not carrying
	// it was measured: internal/tui3 asserts these doors on the agent it holds and
	// draws no answers row at all where the assertion fails, so a window on an
	// engine host drew a landing card with its reason and nothing to press (#706).
	// Every *Agent has the methods; only the welcome knows whether the machine at
	// the far end does.
	TaskSettle bool `json:"taskSettle,omitempty"`

	// Effort says this engine HAS A DIAL ON THE CONVERSATION'S OWN THINKING —
	// that its agent answers [MethodEffort], [MethodResolvedEffort] and
	// [MethodSetEffort] rather than refusing them (effort.go).
	//
	// IT IS CARRIED FOR [Welcome.Folders]'S REASON, WHICH IS THE ONE THAT MATTERS
	// MOST HERE. A surface at this end holds a *remote.Agent, which ALWAYS has
	// the three methods on it, so the type assertion a local surface uses to tell
	// a dial from no dial answers yes for every connection and says nothing about
	// the far machine. And the honest reading cannot be taken from the ANSWER
	// either: "" is a real rung on this ladder — a conversation asking for no
	// thinking at all — so silence and absence are the same string, and the flag
	// is the only thing that separates them.
	//
	// ABSENCE IS false AND false IS THE SAFE READING: A CAPABILITY THAT CANNOT
	// WORK IS ABSENT, NOT BROKEN, so the seam draws no rung, the chord does
	// nothing, and nothing on the screen offers to move a knob the far engine
	// has never heard of.
	Effort bool `json:"effort,omitempty"`

	// Approval says this engine HAS A DIAL ON THE CONVERSATION'S OWN POSTURE ON
	// THE TOOL GATE — that its agent answers [MethodResolvedApproval] and
	// [MethodSetApproval] rather than refusing them (approval.go). It is
	// carried for [Welcome.Effort]'s reason, and false is the safe reading for
	// the same reason: the surface then draws the chip as a reading of
	// [Welcome.ApprovalMode] and says the far machine's rules decide.
	Approval bool `json:"approval,omitempty"`

	// DefaultEffort is the far install's own `thinking` row, and
	// StandingApproval is what a conversation nobody has touched opens at on
	// that install (the rows as they stand, or the launch's `--yolo`). Both
	// are carried ONCE, at the door, because they are facts about the install
	// and not about any conversation: the draft on home and the other places
	// draws them as the rung and the gate the NEXT conversation on that machine
	// would run at (internal/tui3's boxseam.go), and a draft is drawn on every
	// frame. An empty DefaultEffort means auto when Effort is true; Effort
	// itself distinguishes an engine without the control. An empty
	// StandingApproval means there is no gate and draws no approval cell.
	DefaultEffort    string `json:"defaultEffort,omitempty"`
	StandingApproval string `json:"standingApproval,omitempty"`

	// Folders says this engine CAN HOLD THE FOLDERS A CONVERSATION IS ABOUT —
	// that its agent answers [MethodPlacesRefer] and [MethodPlacesRemove] rather
	// than refusing them (wire_places.go).
	//
	// IT IS CARRIED BECAUSE THE QUESTION IS ASKED BEFORE ANYTHING IS CHOSEN, and
	// that is [Welcome.SteerRepeat]'s reason exactly. A surface at this end holds
	// a *remote.Agent, which ALWAYS has the three methods on it — so the type
	// assertion a local surface uses to tell a capable agent from an incapable
	// one answers yes for every connection and says nothing about the machine at
	// the far end. Without this flag the only honest reading arrives as the
	// refusal to the call, which is after the person has already picked a folder
	// out of a list and pressed enter.
	//
	// ABSENCE IS false AND false IS THE SAFE READING: an engine that predates
	// these doors sends no field, and a surface that believed it could attach
	// would open a picker whose every row ends in an error.
	Folders bool `json:"folders,omitempty"`

	// News says this engine SENDS THE STATUS LINE'S NEWS — the "phase" and
	// "lane" frames the live rate and the `via <machine>` rider are drawn from
	// (news.go) — for the conversation this surface arrived in.
	//
	// IT MAKES AN ABSENCE KNOWABLE WITHOUT REFUSING ANYBODY. The news frames
	// rode an existing version on purpose ([Version]'s note says why: a status
	// line must not turn a live conversation away), which left a surface
	// attached to an engine from before them drawing no rate and no machine and
	// no way to say why — and a busy older engine on the same version is
	// attached to rather than retired (cmd/codeaf's clearStaleEngineHost). A
	// surface reads this, or a news frame arriving, as the engine having the
	// news; neither after a whole answer is an older engine, and the surface
	// says so once ([Client.NewsSilent]).
	//
	// ABSENCE IS false, and it is not by itself proof of an old engine: every
	// build between the news frames and this flag sends them without saying so,
	// which is why a frame arriving counts as the same answer.
	News bool `json:"news,omitempty"`

	// Skills says this engine's conversation CAN CARRY SKILLS PUT IN FRONT OF
	// IT BY HAND and can list the shelf they come from — that its agent
	// answers [MethodAttachSkills], [MethodDetachSkill], [MethodAttachedSkills],
	// [MethodClearSkills] and [MethodSkillShelf] rather than refusing them
	// (skills.go).
	//
	// IT IS CARRIED FOR [Welcome.Folders]'S REASON: a surface at this end holds
	// a *remote.Agent, which ALWAYS has the doors on it, so the assertion the
	// picker makes says nothing about the far machine. ABSENCE IS false, and
	// false keeps the picker's own sentence for a conversation that cannot
	// carry attached skills rather than a list whose every choice goes nowhere.
	Skills bool `json:"skills,omitempty"`
}

// SkillShelfArgs asks for one reading of the conversation's skill shelf, on
// [store.Store.SkillFacts]'s own two arguments.
type SkillShelfArgs struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Driver is who holds the keyboard on one conversation, as told to ONE surface.
//
// IT CARRIES THE FACT AND THE READING, and that is why it is per-recipient
// rather than one broadcast fact. "The driver is macbook" means two different
// sentences depending on who hears it: to the window sitting on macbook beside
// it, the honest word is the one codeaf already uses at home — `another window`
// — and to a surface on spark it is the machine's name. Only the engine knows
// both names, so only the engine can answer that; and the SURFACE still owns
// the words, because the rest of the line it goes in is about keys on this
// keyboard (Decision 6: the engine machine is the authority, the surface owns
// the screen).
type Driver struct {
	// Yours says the surface reading this frame is the one that drives. It is
	// the ordinary case and the only one that draws nothing.
	Yours bool `json:"yours,omitempty"`
	// Machine is the driver's machine name as [Hello.Surface] gave it, empty
	// when that surface sent none. It is sanitized ([machineLabel]) because it
	// is drawn.
	Machine string `json:"machine,omitempty"`
	// Here says the driver is another window on THIS surface's own machine,
	// which is the case a person reads as a window they forgot rather than as a
	// machine they walked away from.
	Here bool `json:"here,omitempty"`
}

// Moved is the engine telling ONE surface that another window has opened this
// conversation and is now the one in it.
//
// IT IS A FACT ABOUT THE ROOM AND NOT AN INSTRUCTION. The engine goes on holding
// the conversation, running whatever turn is in flight and keeping every task on
// its feet; what has changed is who is sitting in front of it. The surface that
// hears this detaches, which is the road that leaves the work alone
// (internal/tui3's movedAway), and a surface that ignores the kind is left
// attached and reading — the honest floor for a build that predates the frame.
//
// IT NAMES THE MACHINE FOR [Driver]'s REASON, in [Driver]'s words: `another
// window` is the true and weaker claim when the name is missing or is this
// surface's own, and the name is what a person needs when the conversation
// walked to a different computer.
type Moved struct {
	// Machine is the arriving surface's machine name as [Hello.Surface] gave
	// it, sanitized ([machineLabel]) because it is drawn. Empty is a surface
	// that sent none.
	Machine string `json:"machine,omitempty"`
	// Here says the window that took it is on THIS surface's own machine.
	Here bool `json:"here,omitempty"`
}

// SubmitArgs carries Submit and FollowUp.
// InterruptArgs names the door a stop came through, so that a hosted engine can
// write down what ended a turn and say one sentence about a reply that never
// arrived (internal/session's stopcause.go).
//
// AN EMPTY DOOR IS A PERSON'S OWN STOP, which is what an older surface that
// sends no arguments at all means and what it always meant. That is the one
// direction this may fail in that costs nothing: a stop is still a stop.
type InterruptArgs struct {
	Door string `json:"door,omitempty"`
}

type SubmitArgs struct {
	Text string `json:"text"`
	// Standing says the person MARKED this draft as something to keep true
	// (internal/session's standing_mark.go), so the engine opens the turn
	// through SubmitStanding rather than Submit.
	//
	// IT IS A FIELD RATHER THAN A METHOD OF ITS OWN because the two differ in
	// what the engine puts in front of the sentence and in nothing a wire can
	// see: same argument, same StreamRef, same event frames. An older engine
	// that does not read it runs the ordinary turn, which is the one direction
	// this may fail in that leaves the person's words intact.
	Standing bool `json:"standing,omitempty"`
}

// SubmitImageArgs carries SubmitImage. Images travel with their bytes filled
// in — the engine has no way to read a path on the surface's disk — and the
// engine writes them to its own image store before submitting, so the journal
// holds references the way it always does.
type SubmitImageArgs struct {
	Text   string          `json:"text"`
	Images []session.Image `json:"images"`
}

// SubmitFilesArgs carries SubmitFiles: a message with ordinary files attached.
//
// IT IS image.go's LAW, GENERALIZED, and the generalization is the point. A
// picture already travels as BYTES and is remade on the engine's disk, because
// the surface read it off a disk the engine cannot see. Every other thing a
// person drops into the chat — a log, a CSV, a PDF, a stack trace saved to a
// file — has exactly the same problem and had no answer at all in version 1:
// the path was typed here and meant nothing there.
//
// So the contract is one sentence: WHAT A PERSON PUTS INTO THE CHAT IS THE
// SURFACE'S TO READ AND THE ENGINE'S TO KEEP. The bytes ride the message, the
// engine writes them where that session keeps such things, and what reaches the
// journal is a path that is true on the machine that owns the journal — which
// is the same bargain internal/session's image.go already struck, for the same
// reason (a reference is only worth writing if it names a file that exists on
// the machine that wrote it).
//
// The model is TOLD THE PATH rather than the contents: an attached file is a
// file, and the session already has a `read` tool. That keeps a 4MB CSV out of
// the context window until something actually wants it.
type SubmitFilesArgs struct {
	Text  string     `json:"text"`
	Files []WireFile `json:"files"`
	// Images ride along so ONE MESSAGE IS ONE CALL. A person who pastes a
	// screenshot and drops a log file has sent one message, and splitting it
	// into two submits would open two turns.
	Images []session.Image `json:"images,omitempty"`
}

// WireFile is one attachment travelling with its bytes.
type WireFile struct {
	// Name is the file's own name as the surface saw it, and NEVER a path: the
	// engine joins it to a directory of the engine's choosing, so a "name" that
	// walked out of that directory would be this wire handing a remote machine
	// an arbitrary write. The engine sanitizes it regardless — a boundary that
	// trusts its input is not a boundary — but the field is documented as a
	// name so that nothing on this side is tempted to send a path.
	Name string `json:"name"`
	// MIME is what the surface believed this was, empty when it could not tell.
	// It is a hint for the engine's naming and nothing is refused for lacking
	// it — unlike an image, whose type the provider genuinely needs.
	MIME string `json:"mime,omitempty"`
	// Bytes is the file itself. The frame ceiling (server.go's frameCap) is the
	// only limit this wire imposes; the SIZE the person is allowed to attach is
	// a surface question, asked on the surface, in the surface's own words —
	// the same division images already use.
	Bytes []byte `json:"bytes"`
}

// FetchFileArgs is the surface asking for a file the engine holds.
//
// THE PATH IS THE ENGINE'S AND IS NEVER RESOLVED HERE, which is the same law
// every path on this wire obeys. It comes off something the engine already
// said — a deliverable's row, a tool result, the session file itself — and the
// engine is free to refuse a path outside what this session may hand over.
// THE REFUSAL IS THE ENGINE'S TO MAKE: a surface cannot know that machine's
// boundaries, and a client-side check would be a permission decision taken on
// the wrong machine.
type FetchFileArgs struct {
	Path string `json:"path"`
}

// PlacesTaskArgs names the row of the record a card was opened on.
//
// IT IS THE URI OFF THE ROW, WHICH THE ENGINE ITSELF WROTE. The row travelled
// here on the world walk carrying the journal's own address on that machine
// ([session.TaskIndexEntry.TranscriptURI]), and this hands it straight back —
// the same law every path on this wire obeys, and the same reason
// [FetchFileArgs] does not resolve one either. The engine checks it against its
// own places root before it opens anything ([session.ReadTaskRecordUnder]),
// because a boundary the surface asserted would be a permission decision taken
// on the wrong machine.
//
// IT IS A STRUCT AND NOT A BARE STRING so the card can learn to ask for a second
// fact about the same row without a second door and without a second version.
type PlacesTaskArgs struct {
	Transcript string `json:"transcript"`
	Tail       int    `json:"tail,omitempty"`
}

// FetchedFile is one file coming back the other way.
type FetchedFile struct {
	// Name is what the surface should call it when it writes it down. It is the
	// base name of the engine's path, and it is the engine's answer rather than
	// something this side derives, for the reason [WireFile.Name] states in the
	// other direction.
	Name string `json:"name"`
	MIME string `json:"mime,omitempty"`
	// Size and Hash describe the whole file the bytes came from. Hash is the
	// lowercase hex SHA-256 of Bytes — the same digest internal/cas keys on —
	// so a surface that caches by content can ask "do I already have this"
	// before it writes anything down.
	Size  int64  `json:"size,omitempty"`
	Hash  string `json:"hash,omitempty"`
	Bytes []byte `json:"bytes"`
}

// DepositedFile is the engine's word on a file it just kept: the path ON THE
// ENGINE'S DISK where the bytes landed.
//
// IT IS THE ENGINE'S ANSWER AND IS NEVER DERIVED HERE, the same law
// [FetchedFile.Name] and [DirListing.Path] state from their own directions.
// The surface named the file and the ENGINE chose the directory, stamped the
// name and made it unique ([writeAttachment]), so the only machine that can say
// where the thing now is is the one it is now on — a surface that guessed would
// be showing a person a path that is nearly right.
type DepositedFile struct {
	Path string `json:"path"`
}

// ListDirArgs names the directory the surface wants to read. A relative path
// is resolved against the workspace, exactly as [FetchFileArgs.Path] is.
type ListDirArgs struct {
	Path string `json:"path"`
}

// DirEntry is one row of a listing. ModTime is unix seconds because a listing
// is drawn, not computed with, and a whole time.Time per row is frame weight.
type DirEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir,omitempty"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"mtime,omitempty"`
	MIME    string `json:"mime,omitempty"`
}

// DirListing is the engine's answer: the path AS THE ENGINE RESOLVED IT — the
// surface must never derive it — and the entries, directories first, then
// files, each half sorted by name. Truncated says the cap (listDirMax,
// file.go) cut the tail rather than the directory ending there.
type DirListing struct {
	Path      string     `json:"path"`
	Entries   []DirEntry `json:"entries"`
	Truncated bool       `json:"truncated,omitempty"`
}

// StatPathsArgs is a bounded batch of candidate paths, relative ones meaning
// the workspace. Over statPathsMax (file.go) the engine refuses the call
// rather than trimming it silently.
type StatPathsArgs struct {
	Paths []string `json:"paths"`
}

// PathFact is the engine's word on one candidate: it exists under the
// two-roots law, whether it is a directory, and HOW THE FILE STANDS RIGHT NOW.
// A path outside the roots reports Exists false — to a surface deciding whether
// to draw a door, a file that will refuse to open IS absent.
//
// SIZE AND MODTIME ARE HERE SO THAT A CACHE CAN BE WRONG AND FIND OUT. A
// surface holding a copy of a far file has exactly one cheap way to learn that
// the file was rewritten under it: ask this machine what the file is now and
// compare. Without these two numbers the only honest answers are "fetch the
// whole thing again every time" and "serve the old bytes forever", and the
// second is the one a cache keyed by path quietly becomes. They cost nothing —
// the stat that answers Exists already has them in hand — and they turn a
// freshness question into one small frame instead of a transfer.
type PathFact struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists,omitempty"`
	Dir    bool   `json:"dir,omitempty"`
	// Size is the file's byte count as this machine sees it. A directory
	// reports its own, which is a filesystem artifact rather than a fact about
	// what is inside it — the surface reads it for files and nothing else.
	Size int64 `json:"size,omitempty"`
	// ModTime is the last modification in whole seconds since the epoch, which
	// is the resolution every filesystem and every archive format agrees on.
	// It is a NUMBER and not a time.Time because it is compared and never
	// drawn: a surface asking "is this the file I already have" wants equality,
	// not a moment in a person's timezone.
	ModTime int64 `json:"mtime,omitempty"`
}

// HeldQuestion is a card this session raised while nobody was attached.
//
// THE EMPTY ROOM BECOMES A WAITING ROOM, and that is the single change that
// unlocks most of what --host could not do. Version 1's refusals — no harness
// design, no adaptive run, no "always" on a consent card — all had the same
// root: the answer to those questions travels on a lane a connection did not
// carry, so a question raised with nobody there would sit in an empty room and
// expire. A persistent engine is exactly the machine that CAN hold one: the
// card is asked, nothing proceeds, and the next surface to attach is handed it
// with the time it has been waiting.
//
// It is deliberately NOT a copy of each card's own type. The card itself
// already crosses as an ordinary event ([EventWire]) and the surface already
// knows how to draw every kind of card there is; what a returning surface
// lacks is the KNOWLEDGE THAT ONE IS OUTSTANDING and the event that raised it.
// So this carries the raw event and the identity needed to answer it, and the
// surface replays it through the same door a live one goes through.
type HeldQuestion struct {
	// Kind is which resolve-door answers this: "consent", "standing",
	// "harness", "connect". It is a string rather than an enum because the
	// envelope is the contract and a newer engine holding a kind this build
	// does not draw must not be a broken conversation — an unknown kind is
	// SKIPPED by the surface, which leaves the question waiting for a build
	// that knows it, exactly as it was.
	Kind string `json:"kind"`
	// Event is the frame that raised it, verbatim, so the surface draws the
	// card it would have drawn live.
	Event EventWire `json:"event"`
	// Stream is the turn it belongs to, so a surface reattaching mid-turn puts
	// the card back where it was rather than at the end of the room.
	Stream uint64 `json:"stream,omitempty"`
	// Since is when it was raised. THE SCREEN SHOULD SAY HOW LONG SOMETHING HAS
	// WAITED: a consent card from four hours ago is a different thing to answer
	// than one from four seconds ago, and only the engine knows which it is.
	Since time.Time `json:"since"`
}

// Turn is a turn that has just started in this conversation, told to every
// surface that did NOT start it.
//
// IT IS WHAT MAKES A SECOND WINDOW A WINDOW ONTO THE WORK RATHER THAN A DEAD
// FRAME. The events of a turn have always fanned out to everybody attached, but
// a surface only draws a stream it knows about — the one its own Submit named,
// or [Welcome.Live] at the door — so a turn started on ANOTHER machine after
// this surface arrived went past it in silence. The room could not watch itself.
//
// IT IS SENT BY THE ENGINE AND NEVER INFERRED HERE, because the engine is the
// only thing that knows who called Submit. A surface that guessed from "an event
// on a stream I have not seen" would race its own submit's result and draw its
// own turn twice.
//
// Said is the sentence that opened it, and it is carried for the one moment the
// transcript cannot answer: a surface that has already read the transcript and
// is sitting there watching. A turn already running when a surface ATTACHES
// needs none of it — that message is in the journal the surface reads on its
// way in — so [Welcome.Live] carries no sentence and this does.
type Turn struct {
	Stream uint64 `json:"stream"`
	Said   string `json:"said,omitempty"`
}

// StreamRef is the result of the three stream-opening calls: the id every
// "event" frame of that turn carries. The stream ends with a "closed" frame
// bearing the same id, which is the channel close.
// FactsPush is one statement of the whole fact set, and the revision that
// orders two of them.
//
// IT IS THE WHOLE SET AND NEVER A DELTA. A push naming only what changed would
// be smaller and would be wrong the first time one went missing: a surface that
// had lost a frame would carry a stale field forever with nothing able to tell
// it so. The set is five short fields and a tiny map — smaller than one line of
// a reply — so every push is complete and the newest one is always the truth.
//
// REV IS WHY IT CAN BE READ OUT OF ORDER SAFELY. Two facts can move at almost
// the same instant on the engine, and the two pushes race to the writer; the
// number is minted where the order is decided (under the session's own lock),
// so a surface keeps the highest it has seen and drops anything older. Without
// it a late push would overwrite a newer one and the status line would go
// backwards, which is the one thing a live row must never do.
type FactsPush struct {
	Rev   uint64        `json:"rev"`
	Facts session.Facts `json:"facts"`
}

// PhaseWire is one moment of one far turn's life: what the request is doing
// right now, on which machine, and how fast it is writing
// ([provider.PhaseNews]).
//
// IT CARRIES NO CONVERSATION AND NO OFFER TOKEN. The engine files its news by
// conversation to decide WHICH connection each piece goes down (news.go), and
// once it is on a connection the connection IS the conversation — a name on the
// frame would be a second answer to a question already settled. The offer token
// stays behind for the same reason: the token is the engine's own bookkeeping,
// and the surface answers by pressing `y` at the conversation it is sitting in
// ([MethodAnswerLaneOffer]), never by naming a token it was handed.
//
// IT DOES CARRY THE SUBJECT, AND THAT IS NOT THE CONVERSATION SAID TWICE. The
// conversation is WHOSE this news is, which the connection answers; the subject
// is WHAT IT IS ABOUT — the conversation itself, or one task node inside it —
// which nothing on this side of the pipe can answer. A surface holds one desk
// for every window it draws, so a node's phase and its parent conversation's
// arrive down one connection and have to be told apart at the desk
// (internal/tui3's phase.go). Until this field crossed, a node's room over a
// connection could draw no clock at all, and two nodes on one model id
// overwrote each other's.
//
// THE MOMENTS ARE ELAPSED TIMES AND NEVER WALL CLOCKS. A surface ages a phase
// out fifteen seconds after it was said ([provider.PhaseWindow]) and counts a
// clock up from when it began, both against ITS OWN now — so a wall clock from
// another machine, even a few seconds out, would either drop every phase on
// arrival or draw one that had been running since before it started. SinceMS is
// how long the phase had already lasted when the engine said so and DeadlineMS
// how long was left; the surface adds both to the moment the frame landed
// (client.go's [Client.phaseFrame]).
type PhaseWire struct {
	// Phase is [provider.Phase] — the person's own word for what is happening.
	// An empty phase is the end of the story and is carried as such, because a
	// stale clock left running is the defect the phase seam exists to fix.
	Phase string `json:"phase,omitempty"`
	// Model is the model this request is on and Role who it is for
	// ([lane.Role]). EVERY ROLE CROSSES AND THE SURFACE DECIDES: a task node's
	// phases are drawn where a node is drawn and a conversation's on the status
	// row, and that filter is already written on the surface (internal/tui3's
	// phase.go). An engine that filtered here would be a second opinion about
	// the same question.
	Model string `json:"model,omitempty"`
	Role  string `json:"role,omitempty"`
	// Subject is which piece of work this news is about: empty for the
	// conversation, and one node's own name for a node
	// ([session.NewsSubject]). AN OLDER PEER SENDS NONE, WHICH READS AS THE
	// CONVERSATION — the same absence every producer that predates the field
	// means, so a surface talking to a build without it behaves exactly as it
	// always did.
	Subject string `json:"subject,omitempty"`
	// Session is WHOSE news this is — the conversation, in the engine's own
	// spelling ([session.Agent.NewsKey]) — and the name the surface files the
	// conversation's own news under first (internal/tui3's newsDeskKeys).
	//
	// IT CROSSES BECAUSE THE MODEL IS NOT AN IDENTITY. Without it the surface
	// filed the conversation's phases under the model id, and the model is
	// exactly the thing that moves between an engine and a window: a pick made
	// mid-turn, a stream-cut hop onto another model, a change made from another
	// window. Each put the live rate and the machine under a name that window
	// was not asking for.
	//
	// AN OLDER PEER SENDS NONE, and the surface files what it gets under the
	// model alone, which is what it always did — so no version moves for it.
	Session string `json:"session,omitempty"`
	// Lane is the machine answering when one has named itself, and Rate how
	// fast it is writing in tokens a second. Zero for both is "not measured",
	// never "nothing" — the emptiness law, carried across the wire intact.
	Lane string  `json:"lane,omitempty"`
	Rate float64 `json:"rate,omitempty"`
	// Door is the billing road in use. It stays separate from Lane so a direct
	// service is never presented as a router's serving machine.
	Door string `json:"door,omitempty"`
	// Detail is the phase's own noun, already in a person's words, and Then
	// what will be done about the wait when a deadline is real.
	Detail string `json:"detail,omitempty"`
	Then   string `json:"then,omitempty"`
	// SinceMS is how long this phase had lasted when the engine said so, and
	// DeadlineMS how long was left before something is done about it. Both in
	// milliseconds; zero DeadlineMS is no deadline, which is the honest answer
	// wherever no alternative lane exists.
	SinceMS    int64 `json:"sinceMs,omitempty"`
	DeadlineMS int64 `json:"deadlineMs,omitempty"`
}

// LaneWire is one finished far answer's lane story: which machine it went to,
// which one finished it, and whether a rescue went out while somebody was
// waiting ([session.LaneNews]).
//
// It carries no moment, for [PhaseWire]'s reason one step further: the sheet's
// `served` row draws a sighting for ten minutes after it was taken
// (internal/tui3's servedWindow) and the only clock that reading can be taken
// against is the surface's own, so the surface stamps it when the frame lands.
// What it carries instead is an AGE, and only when it has one: a sighting
// replayed to a window that arrived after the answer (news.go's
// [Session.watchNews]) says how long ago the answer landed, so the surface
// files it as the old sighting it is. A live sighting's age is nothing, and
// an older peer that does not know the field reads it as nothing.
type LaneWire struct {
	// AgeMS is how long before this frame was sent the answer landed — zero
	// for a sighting that is crossing as it happens.
	AgeMS int64 `json:"ageMs,omitempty"`
	// Model is the model the answer came back on. A news with no model belongs
	// to nobody and is dropped on both sides of the wire.
	Model string `json:"model"`
	// Lane is the machine the request WENT TO and Winner the one that finished
	// it; they differ only when a rescue landed, which is the whole of what a
	// surface means by "rescued". Alt is whichever of the pair was not asked
	// first.
	Lane   string `json:"lane,omitempty"`
	Alt    string `json:"alt,omitempty"`
	Winner string `json:"winner,omitempty"`
	// TTFTMS is the wait before the first token in milliseconds, and Rate how
	// fast the answer was written.
	TTFTMS int64   `json:"ttftMs,omitempty"`
	Rate   float64 `json:"rate,omitempty"`
	// Hedged says a second request went out for this answer; Trying says one is
	// out RIGHT NOW and nobody has committed yet.
	Hedged bool `json:"hedged,omitempty"`
	Trying bool `json:"trying,omitempty"`
	// Reason is why the rescue went out, in the transport's own two words, and
	// Failed WITHDRAWS a claim already made: the lane in Alt was what a
	// `trying X…` was about and it has now failed.
	Reason string `json:"reason,omitempty"`
	Failed bool   `json:"failed,omitempty"`
	// Role is who the answer was for. It crosses unfiltered for [PhaseWire.Role]'s
	// reason.
	Role string `json:"role,omitempty"`
	// Subject is which piece of work the sighting is about, and it crosses for
	// [PhaseWire.Subject]'s reason and with its reading of absence: empty is the
	// conversation. It is spelled the same on both wire shapes because they are
	// twins, and a surface keys both desks with one function.
	Subject string `json:"subject,omitempty"`
	// Session is whose sighting it is, for [PhaseWire.Session]'s reason and with
	// its reading of absence: an older peer sends none, and the sighting is
	// filed under its model as it always was.
	Session string `json:"session,omitempty"`
}

type StreamRef struct {
	Stream uint64 `json:"stream"`
}

type ReasoningArgs struct {
	Model string `json:"model"`
	Level string `json:"level"`
}

type ConsentArgs struct {
	ID    uint64               `json:"id"`
	Allow bool                 `json:"allow"`
	Scope session.ConsentScope `json:"scope,omitempty"`
}

// StandingArgs carries ResolveStanding: which card, and what the person said to
// it. It is the standing lane's ConsentArgs — one id and one answer — and the
// answer travels WHOLE rather than field by field, because
// [session.StandingAnswer] is the engine's own type and a field added there must
// arrive without a wire change (this file's header states that bargain).
//
// KEEPWATCH'S THIRD STATE IS LOAD-BEARING AND SURVIVES BECAUSE IT IS A POINTER.
// The field answers a question that is only ever ASKED of a person's first
// standing item, so nil means nobody was asked, and encoding/json writes a nil
// pointer as null and reads null back as nil. A bool would have turned "never
// asked" into "said no" on the far machine.
type StandingArgs struct {
	ID     uint64                 `json:"id"`
	Answer session.StandingAnswer `json:"answer"`
}

// AutonomyArgs is one shape of question and what may answer it from now on.
type AutonomyArgs struct {
	Kind   session.AskKind `json:"kind"`
	Policy session.Policy  `json:"policy"`
}

// QuestionArgs is one answer, whole. It carries [session.Answer] rather than a
// flattened set of fields for the reason the object exists at all: the fields on
// it are what a person's intent looks like, and a wire that carried only the key
// would be the wire deciding that the notes, the exchange and the words beside
// the pick are not part of the answer.
type QuestionArgs struct {
	Answer session.Answer `json:"answer"`
}

// QuestionHoldArgs names one question whose clock a person has stopped, the way
// an answer names it: the lane, and the lane's own token ([session.Question.Token]).
type QuestionHoldArgs struct {
	Kind  session.QuestionKind `json:"kind"`
	Token string               `json:"token"`
}

type HarnessArgs struct {
	ID    uint64 `json:"id"`
	Run   bool   `json:"run"`
	Model string `json:"model,omitempty"`
}

type ConnectArgs struct {
	ID      string `json:"id"`
	Approve bool   `json:"approve,omitempty"`
	Key     string `json:"key,omitempty"`
}

type ConnectedArgs struct {
	Service string `json:"service"`
	Account string `json:"account"`
}

// PlanSpendArgs is how far back the spend page's seat rollup is looking.
//
// SINCE IS THE SAME FLOOR [LedgerArgs.Since] IS, and it is a plain time.Time
// for the same reason: it is a moment the wire already knows how to encode, cut
// in Go on the far side against the ledger's own RFC3339Nano stamps
// ([session.Agent.PlanSpend]). A zero Since is the whole rollup, which is what
// a caller with no window yet means and what a test means.
type PlanSpendArgs struct {
	Since time.Time `json:"since,omitempty"`
}

// EventWire is a session.Event that survives JSON. Err is an interface and
// marshals to nothing, so the string rides beside it and shadows it on the
// wire; [EventWire.Event] restores the one field that needs restoring.
type EventWire struct {
	session.Event
	Err string `json:"Err,omitempty"`
}

// WireEvent wraps an event for sending.
func WireEvent(ev session.Event) EventWire {
	w := EventWire{Event: ev}
	if ev.Err != nil {
		w.Err = ev.Err.Error()
	}
	w.Event.Err = nil
	return w
}

// Unwire unwraps a received event.
func (w EventWire) Unwire() session.Event {
	ev := w.Event
	if w.Err != "" {
		ev.Err = errors.New(w.Err)
	}
	return ev
}

// PlanTaskArgs names one task for a steering verb.
type PlanTaskArgs struct {
	ID string `json:"id"`
}

// PlanTextArgs carries the task and prose for note and amend.
type PlanTextArgs struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// PlanPriorityArgs carries the task and its new scheduling priority.
type PlanPriorityArgs struct {
	ID       string `json:"id"`
	Priority int    `json:"priority"`
}

// PlanTaskPageArgs names the task whose complete page is requested.
type PlanTaskPageArgs struct {
	ID string
}

// PlanTaskPageResult preserves both the page and whether the task belongs to the plan.
type PlanTaskPageResult struct {
	Page session.PlanTaskPage
	OK   bool
}

// PlanRunSummaryArgs names the run whose stored summary is read.
type PlanRunSummaryArgs struct {
	RootID string `json:"root_id"`
}

// RefreshRunSummaryArgs carries the refresh window and caller deadline to the engine.
type RefreshRunSummaryArgs struct {
	RootID   string    `json:"root_id"`
	LastLook time.Time `json:"last_look,omitempty"`
	// Budget is HOW LONG the caller will wait, never the instant it stops
	// waiting: the engine may be on another machine whose clock is not this
	// one's, and an instant read against a clock a minute ahead is a refresh
	// that is cut before it starts. Zero is a caller with no deadline.
	Budget time.Duration `json:"budget,omitempty"`
}

// PlanRunSummaryResult preserves both the summary and whether one exists.
type PlanRunSummaryResult struct {
	Summary session.RunPlanSummary `json:"summary"`
	OK      bool                   `json:"ok"`
}
