// Package remote is the wire between a surface on one machine and an engine on
// another. The surface half dials `ssh <host> aforge engine …` and speaks this
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

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// Version is the protocol's version. The hello and the welcome both carry it,
// and a mismatch is a refusal at the door — two builds that might disagree
// about a frame must not guess at each other.
//
// VERSION 2 IS THE PERSISTENT ENGINE. Version 1 married a conversation to a
// pipe: the engine was `ssh … aforge engine`, it read frames on stdin, and when
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
const Version = 4

// Frame is one line on the wire, either direction.
type Frame struct {
	// Kind says what this frame is: "hello", "welcome", "call", "result",
	// "event", "closed", "facts", "fatal".
	//
	// "facts" is the ONE KIND THAT ANSWERS NOTHING. Every other frame from the
	// engine either replies to a call or belongs to a stream a call opened;
	// this one is the engine saying something the surface did not ask for,
	// because the whole point of it is that the surface never has to ask. It
	// carries a [FactsPush] and no ID, and a build that does not know the kind
	// ignores it, which is what the reader in client.go already does with every
	// kind it has no case for.
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
	MethodSubmit          = "Submit"                 // SubmitArgs → StreamRef, then "event" frames
	MethodSubmitImage     = "SubmitImage"            // SubmitImageArgs → StreamRef, then "event" frames
	MethodSubmitFiles     = "SubmitFiles"            // SubmitFilesArgs → StreamRef, then "event" frames
	MethodFollowUp        = "FollowUp"               // SubmitArgs → StreamRef, then "event" frames
	MethodInterrupt       = "Interrupt"              // nothing → nothing
	MethodCompact         = "Compact"                // nothing → nothing (error carries the failure)
	MethodClose           = "Close"                  // nothing → nothing
	MethodModel           = "Model"                  // nothing → string
	MethodSetModel        = "SetModel"               // string → nothing
	MethodSetContext      = "SetContextWindow"       // int → nothing
	MethodReasoningFor    = "ReasoningFor"           // string → string
	MethodSetReasoningFor = "SetReasoningFor"        // ReasoningArgs → nothing
	MethodConsent         = "ResolveConsent"         // ConsentArgs → nothing
	MethodConsentRemember = "ResolveConsentRemember" // ConsentArgs → nothing
	MethodStandingResolve = "ResolveStanding"        // StandingArgs → nothing
	MethodHarness         = "ResolveHarness"         // HarnessArgs → nothing
	MethodConnect         = "ResolveConnect"         // ConnectArgs → nothing
	MethodConnectKey      = "ResolveConnectKey"      // ConnectArgs → nothing
	MethodNoteConnected   = "NoteConnected"          // ConnectedArgs → nothing
	MethodTitle           = "Title"                  // nothing → string
	MethodUsage           = "Usage"                  // nothing → session.Usage
	MethodContextTokens   = "ContextTokens"          // nothing → int
	MethodTranscript      = "Transcript"             // nothing → []session.DisplayEntry
	MethodEarlier         = "EarlierHistory"         // nothing → session.EarlierHistory
	MethodRewindPoints    = "RewindPoints"           // nothing → []session.RewindPoint
	MethodRewindAt        = "RewindAt"               // int → []session.DisplayEntry

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

// Hello is the client's first frame ("hello"). Workspace is the path AS TYPED
// after the colon — empty means the engine's own home — and the engine answers
// with the path it resolved.
type Hello struct {
	Version   int    `json:"version"`
	Workspace string `json:"workspace,omitempty"`
	// Session is an explicit session file to open, empty for the workspace's
	// latest-or-new (the same meaning the --session flag has locally).
	Session string `json:"session,omitempty"`

	// Model and Level are --model and --reasoning, carried in the frame that
	// BUILDS the session rather than applied to it a millisecond later.
	//
	// They retire a stub. Version 1 had no room for them, so the door set them
	// immediately after the handshake (cmd/aforge's applyHostChoices), which
	// worked for every turn the person could type but left the session file's
	// first line naming the model the session was BORN on rather than the one
	// they asked for. Nobody on the screen could see the difference; the
	// journal could, and the journal is the record.
	Model string `json:"model,omitempty"`
	Level string `json:"level,omitempty"`

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
	Title       string `json:"title,omitempty"`
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
	// would be the one place aforge lied about who is in the room. Zero is the
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
	// shapes speak this protocol and both are legitimate: `aforge engine`
	// started by hand on a machine with no host is still a conversation, it
	// simply ends when the pipe does. The screen's word for detaching, and
	// whether "close the lid, it keeps going" is true, both hang off this
	// single fact, so it is stated rather than assumed from the transport.
	Persistent bool `json:"persistent,omitempty"`

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
}

// Driver is who holds the keyboard on one conversation, as told to ONE surface.
//
// IT CARRIES THE FACT AND THE READING, and that is why it is per-recipient
// rather than one broadcast fact. "The driver is macbook" means two different
// sentences depending on who hears it: to the window sitting on macbook beside
// it, the honest word is the one aforge already uses at home — `another window`
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

// SubmitArgs carries Submit and FollowUp.
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
