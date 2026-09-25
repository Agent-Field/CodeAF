package remote

// server.go is the ENGINE half: the side that holds a real conversation and
// answers frames about it. It is what `codeaf engine` runs after it has changed
// into the workspace and assembled an agent exactly the way `codeaf chat` does.
//
// The shape is one reader, one writer, and one ORDERED LANE, and everything
// else follows from it. Calls that open a stream or change the conversation's
// shape still arrive and are answered in the order the surface made them — on
// the lane, which is one goroutine that is not this one ([classify] is the one
// predicate, orderedlane.go is the mechanism). A surface that sets a model and
// then submits a message means those two things in that order and nothing here
// may reorder them. The one exception is a turn's events, which arrive on a
// channel the session owns and are pumped by a goroutine of their own — that
// is the whole reason Submit answers with a stream id instead of a transcript.
//
// NOTHING AT ALL RUNS ON THE READER. A listing that held it used to queue a
// person's keystroke behind it until [callDeadline] fired and the surface
// declared the connection gone, while the engine went on to apply the key; a
// SEND held it for the whole of the engine's preamble and did the same thing to
// every frame behind it. Both are the same defect — a person's act queued
// behind work that had nothing to do with it — and the reader now does the one
// job its name claims.
//
// ORDER IS WORTH MORE THAN OVERLAP for the calls on the lane, and the one that
// pays for it is Compact: it is the only method that does the work itself
// rather than starting it, so a compaction pass holds the lane for as long as
// the summarizer takes and the ordered calls behind it wait. Handing Compact a
// goroutine would buy nothing the reader does not already give — the status
// line stays live because the socket stays read — and would cost the guarantee
// that a /model followed by a message is a message on the new model.
//
// ── VERSION 2: THE CONVERSATION IS NOT THE CONNECTION ────────────────────────
//
// Version 1 had one object doing two jobs, because a pipe and a conversation
// had the same lifetime: the thing that read frames also owned the agent, and
// when the pipe died the agent died with it. Version 2 splits them in two,
// which is the whole of this file's new shape:
//
//   - [Session] is the CONVERSATION. It owns the agent, the stream counter, the
//     ring of a running turn's events, the questions raised with nobody
//     watching, and the set of surfaces currently attached. It outlives any one
//     connection when somebody is holding it (internal/enginehost) and is
//     closed with the connection when nobody is (a bare `codeaf engine`).
//   - [server] is one CONNECTION. It reads frames, writes frames, and holds a
//     pointer to the session it attached to. Several of them can point at one
//     session, which is what makes a desk and a phone one room.
//
// The three roads out of a connection are finally three different things, and
// the fork is stated once, at the bottom of [server.serve]: a DETACH keeps the
// conversation, a TORN PIPE keeps it when the engine is persistent and ends it
// when the engine is the conversation's whole life, and a CLOSE ends it always.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/store"
)

// WrappedAgent is the slice of *session.Agent an engine serves. It is the
// union of tui3.Agent and the rewind pair that surface type-asserts for,
// because a REMOTE SURFACE MUST NOT BE A LESSER SURFACE: whatever the local
// one can ask its agent, this one answers over the wire, and a method missing
// here would be a feature that quietly works at home and quietly does not
// away.
//
// It is named WrappedAgent rather than Agent because [remote.Agent] is
// already the OTHER end's name: the client's implementation of tui3.Agent,
// which a surface holds. This is the engine's own view of the same shape,
// declared here rather than imported from internal/tui3 for the reason
// wire.go states about the two halves: the engine knows nothing of the
// surface package and never will. It is an interface rather than the
// concrete agent for the reason session.Completer is one — the tests below
// drive a scripted agent and never open a socket.
type WrappedAgent interface {
	Submit(ctx context.Context, text string) (<-chan session.Event, error)
	SubmitStanding(ctx context.Context, text string) (<-chan session.Event, error)
	SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error)
	FollowUp(text string) (<-chan session.Event, error)
	Steer(text string) (<-chan session.Event, error)
	Interrupt()
	// InterruptFor is the stop with the door it came through on it, for the
	// machinery stops that are not a person (internal/session's stopcause.go).
	InterruptFor(door session.StopDoor)
	Compact(ctx context.Context) error
	Close() error
	Model() string
	SetModel(model string)
	SetContextWindow(tokens int)
	ReasoningFor(model string) string
	// ReasoningLevels is every model somebody has dialled and the level it
	// holds. It is here rather than left to ReasoningFor above because a
	// SURFACE ASKS THAT QUESTION ABOUT MODELS IT HAS NOT SWITCHED TO — a picker
	// row, a ctrl+t on that row — and one round trip per row is exactly the
	// shape [session.Facts] exists to end. The whole map is a handful of short
	// strings and travels in one push.
	ReasoningLevels() map[string]string
	SetReasoningFor(model, level string)
	ResolveConsent(id uint64, allow bool)
	ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope)
	ResolveStanding(id uint64, answer session.StandingAnswer)
	ResolveHarness(id uint64, run bool, model string)
	ResolveConnect(id string, approve bool)
	ResolveConnectKey(id string, key string)
	NoteConnected(service, account string)
	Title() string
	Usage() session.Usage
	ContextTokens() int
	Transcript() []session.DisplayEntry
	EarlierHistory() session.EarlierHistory
	RewindPoints() []session.RewindPoint
	RewindAt(index int) ([]session.DisplayEntry, error)
}

// Engine is one opened conversation and the doors that replace it. The door
// that builds it (cmd/codeaf) owns config resolution, session-file resolution
// and the locked-file fallback; this package owns nothing about how an agent is
// made and everything about how one is spoken to.
type Engine struct {
	// Headless records the approval context the agent was built for. Reusing an
	// interactive agent must not silently give a headless caller its default gate.
	Headless bool
	// Agent is the conversation the surface starts on. Required.
	Agent WrappedAgent
	// RefreshModelSources updates the engine's own agents from its own profile
	// immediately before a model-set call. It is nil for engines with no
	// profile behind them and changes no wire shape: the existing SetModel call
	// is the notification that a local surface has already written the row.
	RefreshModelSources func()
	// RefreshApprovals updates this conversation's running gate from the engine's
	// own profile immediately before a rule-scoped consent answer is applied.
	// It is nil for engines with no profile behind them and changes no wire
	// shape: the existing consent answer is the notification that a local
	// surface has already written the rule.
	RefreshApprovals func()
	// Closed is called once for each conversation this engine shut down, with
	// the agent that was closed, so the door that built it can forget it.
	//
	// IT IS THE PAIR OF WHATEVER REGISTERED THE AGENT, and it exists because
	// [Engine.RefreshModelSources] reaches the conversations its door is
	// holding: a door that registers and never forgets grows that list for as
	// long as the process lives, and an engine process outlives every
	// conversation in it. Nil for a door that retains nothing. It changes no
	// wire shape — a conversation ending is already the end of its stream.
	Closed func(agent WrappedAgent)
	// ProfileDir is the profile directory this engine process resolved. It is
	// carried in Welcome so a linked-local surface writes every local row back
	// to the profile the running conversation actually reads. A remote surface
	// does not use the path for local writes.
	ProfileDir        string
	UnreadProfileKeys []string
	// Workspace is the directory the engine resolved and works in — the answer
	// to the path the hello asked for, which the welcome carries back.
	Workspace string
	// SessionFile is the transcript being written, and Resumed says it was
	// picked up rather than created.
	SessionFile string
	Resumed     bool

	// Place is the session folder on THIS machine (session's place.go, Decision
	// 26). It is here for one reason: a picture arrives on this wire as bytes
	// and has to be written down before it can be journaled (image.go), and
	// where it lands is a question about the engine's disk that only the
	// engine's own session folder can answer. The zero Place is the legacy
	// layout. A pasted picture earns no row in the deliverables index — it is
	// the person's input, not something made for them — so the index path
	// itself is not carried here.
	Place session.Place
	// Note is the one sentence worth showing once: "session open elsewhere —
	// started a new one" travels here, the same words the local door puts on
	// the surface's entry notice.
	Note string
	// Launch is the shape this conversation was built with, echoed on every
	// welcome so a surface can tell the conversation it asked for from the one
	// it joined ([LaunchShape]). Nil is the engine's own defaults.
	Launch *LaunchShape

	// ApprovalMode is this machine's own answer to "does a tool run without
	// asking", read once at boot off the profile the boot closure resolved
	// against, and carried unchanged through a session swap (the profile
	// belongs to the machine, not to the file that happens to be open). It is
	// what turns the YOLO badge honest again over --host (host.go's approvalPosture,
	// tui3.go's ApprovalMode option).
	ApprovalMode string
	// BashBackgroundAfterSeconds is the foreground-command handoff clock this
	// engine armed. It travels for ApprovalMode's reason: a remote surface's
	// profile belongs to another machine, and zero is a meaningful off posture.
	BashBackgroundAfterSeconds int

	// Fresh builds a replacement agent on the same config with a new session
	// file, and returns it with that file's path. It is what Session.New calls,
	// and it is the local surface's /new closure by another name. Nil makes the
	// method fail rather than pretend.
	Fresh func() (WrappedAgent, string, error)

	// Open builds a replacement agent on the same config pointed at an existing
	// transcript, and says whether that file was found. It is Session.Open, and
	// it is the picker's Resume closure by another name.
	Open func(file string) (WrappedAgent, bool, error)

	// Recent lists the conversations this workspace has had. It is the one door
	// that exists only because the surface is remote: a local one reads the
	// session directory off its own disk, and a remote one cannot see it.
	Recent func() []session.Summary

	// StandingItems and StandingSave are this MACHINE'S ambient side, for the
	// same reason Recent is here: the store is a directory of documents under
	// the engine's own state root, a local surface opens it directly, and a
	// remote one has no way to. The workspace is a path on THIS disk, which is
	// the only kind of path an item's own Workspace field ever holds.
	//
	// Nil is the ambient side off for this engine, and it is answered as a
	// refusal rather than as an empty list — a capability that cannot work is
	// absent, and the surface keeps the difference between "no items" and "no
	// door" (internal/remote's Client.StandingItems says what it does with it).
	StandingItems func(workspace string) ([]standing.Item, error)
	// StandingSave writes one item back. THE STORE'S OWN REFUSAL IS THE ERROR:
	// internal/standing validates what it is asked to write, and a surface that
	// redrew a row as paused over a rejected write would be lying about this
	// disk, so nothing here softens it.
	StandingSave func(item standing.Item) error
	// StandingWatch reads this machine's scheduler. Nil means this engine has no
	// scheduler to ask, which the surface renders as no line.
	StandingWatch func() (standing.WatchStatus, bool)

	// ── the places ──────────────────────────────────────────────────────────
	//
	// THE PLACES ARE A LISTING OF THIS MACHINE'S DISK, and until version 4 the
	// surface listed its own instead. Over --host that meant the tasks place
	// walked the LAPTOP's `~/.codeaf/v3` and drew what it found — eight rows and
	// a total in dollars — under a conversation running here. These doors are
	// how it asks the right machine, and they are the same shape Recent and
	// StandingItems already are: nil is the reading absent rather than empty,
	// answered as a refusal, so the surface keeps the difference between "there
	// is nothing there" and "nobody asked".

	// World is the walk of this machine's places root: every project, every
	// conversation in it, and the work each of those ran. It is the reading five
	// of the surface's seven places are built from ([MethodPlacesWorld] names
	// them), which is why one door serves all five.
	World func() session.World

	// Ledger is the spend place's reading: the priced lines of THIS machine's
	// usage ledger at or after a floor the surface named, and whether the file
	// holds any priced line at all outside it ([LedgerReading] says why the
	// second fact cannot be derived from the first). Nil is a refusal, which the
	// surface draws as the sentence spend has always said.
	Ledger func(since time.Time) LedgerReading

	// Search is one full-text query over every message THIS machine has kept.
	// Nil is a refusal for the same reason, and it is the ordinary state of an
	// engine whose memory row is off: the index and the memory store are one
	// database today, and a machine that is not remembering has neither.
	Search func(terms string, limit int) ([]store.ConversationHit, error)

	// Memory is THIS machine's memory store, readings and writes together.
	//
	// IT IS ONE FIELD FOR SEVEN METHODS AND THAT IS THE POINT. The memory place
	// is the only place on the surface that WRITES, so a wire that carried its
	// readings and not its writes would hand a person a page of the far
	// machine's memories whose `e` and `f` keys edited this laptop's. The store
	// crosses whole or it does not cross, and nil is memory off over there —
	// which the surface says in those words rather than in this session's own
	// ([EngineMemory]).
	Memory  EngineMemory
	Archive func(dir string, archived bool) error
	// PlacesRoot is the directory World walked, carried on the welcome so the
	// surface can put THIS conversation back into a walk taken before it existed
	// ([Welcome.PlacesRoot] holds the argument). Empty says nothing about the
	// world door; a build that answers a world and no root simply cannot adopt.
	PlacesRoot string
	// TaskRecord is ONE ROW of that record read deeper than the walk reads it:
	// the last thing that piece of work said, out of the journal it left here
	// ([MethodPlacesTask]). nil is the same absence World's nil is — the door is
	// answered as a refusal and the card says so, rather than the surface reading
	// a path on its own disk that only exists on this one.
	TaskRecord func(uri string, tail int) (session.TaskRecord, error)
}

// Options is what [Serve] needs, which is one function: how to open the
// conversation the hello asked for. The engine cannot assemble it before the
// handshake, because the workspace and the session file are things the surface
// says.
type Options struct {
	Boot func(Hello) (*Engine, error)
}

// AttachOptions is [ServeAttach]'s one function: which conversation this hello
// is attaching to. It differs from [Options] in exactly the way a host differs
// from a pipe — the answer may be a conversation that was already running when
// this connection dialled, and the same [Session] may be handed to several
// connections at once.
type AttachOptions struct {
	Open func(Hello) (*Session, error)

	// Host answers the version exchange (whois.go): which build is holding this
	// socket, whether it has work in flight, and whether it will retire.
	//
	// NIL IS "NOTHING IS HOLDING A CONVERSATION HERE" and it is the honest
	// answer for a pipe engine, which is why [Serve] leaves it unset. A pipe's
	// conversation cannot outlive its connection, so it can never be the stale
	// middle half this exchange exists to find.
	Host func(WhoIs) HostSelf
}

// frameCap is the most one line may weigh. It is the journal reader's bargain
// (internal/tui3's readJournalLines) at a far larger figure, and for a reason
// that is this protocol's own: AN IMAGE UPLOAD RIDES ONE LINE. A ten-megabyte
// photo is base64 in a JSON string by the time it reaches here, so the ceiling
// has to clear a whole message's worth of pictures with room to spare. It is a
// cap on ONE LINE, never on the conversation.
const frameCap = 64 << 20

// ringEvents is how many of a RUNNING turn's events one stream keeps in memory
// for a surface that is not there to read them.
//
// IT IS A BOUND ON MEMORY AND NOT A PROMISE ABOUT TIME. A turn's events are
// mostly single-chunk text deltas of a few dozen bytes, so four thousand of
// them is a long reply's worth — several minutes of a fast model — at a few
// hundred kilobytes per running stream, and a session almost always has one
// stream running at a time.
//
// WHAT HAPPENS WHEN A SURFACE WAS AWAY LONGER THAN THE RING IS NOT AN ERROR.
// The oldest events fall off the front, so a returning cursor is answered with
// what is still held rather than with everything it missed, and the [Frame.Seq]
// numbers themselves declare the hole — the surface's cursor was 12 and the
// first frame it gets back is 900. That is the honest shape, because THE
// TRANSCRIPT IS THE AUTHORITY ON A FINISHED TURN: the engine is the only writer
// of the session file, everything that landed is in it, and the surface reads
// it anyway. The ring exists for the one thing the journal cannot answer — a
// turn that is still being written — and for nothing else.
const ringEvents = 4096

// Serve runs one engine on one pipe until the pipe dies or the protocol does.
// It returns nil for an ordinary hang-up and an error for everything the
// surface broke, and the caller turns that into an exit code.
//
// THE CONVERSATION IS THIS PIPE'S WHOLE LIFE, which is what the false below
// says: a bare `codeaf engine` with no host behind it is a legitimate version-2
// engine, it simply ends when the connection does. The welcome says so
// ([Welcome.Persistent]) so that no surface promises a lifetime this shape does
// not have.
func Serve(in io.Reader, out io.Writer, opts Options) error {
	return ServeAttach(in, out, AttachOptions{
		Open: func(hello Hello) (*Session, error) {
			// A JOIN IS REFUSED HERE, BEFORE ANYTHING IS BOOTED. [Hello.Join] asks
			// for a conversation that is ALREADY OPEN and says it will take nothing
			// else, and this door has none to offer: it opens the pipe's own
			// conversation, and that conversation is this pipe's whole life. Booting
			// one to answer a join would do the exact thing the flag exists to
			// prevent — start a model to answer a question about work somebody
			// believes is already running somewhere else — and the connection would
			// then be bound to a transcript nobody asked for.
			if hello.Join {
				return nil, errors.New("this engine opens one conversation for one connection, so there is none already open here to join")
			}
			engine, err := opts.Boot(hello)
			if err != nil {
				return nil, err
			}
			return NewSession(engine, false), nil
		},
	})
}

// ServeAttach runs one connection against whatever conversation [AttachOptions]
// opens for its hello — a fresh one, or one that has been running since before
// this surface existed.
func ServeAttach(in io.Reader, out io.Writer, opts AttachOptions) error {
	s := &server{out: out, open: opts.Open, host: opts.Host}
	return s.serve(in)
}

// ── the conversation ────────────────────────────────────────────────────────

// Session is one conversation on the engine machine: the agent, its streams,
// and everybody currently watching it.
//
// IT IS THE UNIT THAT OUTLIVES A CONNECTION. A host holds one per open session
// and hands the same one to every surface that asks for it; a bare engine makes
// one, gives it to its single pipe, and closes it when the pipe ends. Nothing
// below knows which of those it is except through [Session.persistent], and
// that flag decides exactly one thing — what a torn pipe means.
type Session struct {
	// mu guards everything: the agent, the counters, the rings, the held set
	// and the attached surfaces. It is ONE lock rather than several because
	// every interesting decision here spans two of those things at once — "let
	// this surface in AND tell it what it missed", "record this event AND say
	// who is watching" — and a second lock would only be a second order to get
	// wrong.
	mu sync.Mutex

	engine     *Engine
	agent      WrappedAgent
	persistent bool

	// generation counts the session swaps. Every pump remembers the generation
	// it was born in and goes quiet the moment it is not the current one — see
	// [Session.emit].
	generation uint64
	streams    uint64
	// rings holds one ring per RUNNING stream and nothing else. A stream is
	// removed the instant it closes, which is what makes "the engine no longer
	// holds this stream" the same question as "that turn is over, read the
	// transcript".
	rings map[uint64]*ring
	held  *heldSet

	// surfaces is everybody attached right now. Events fan out to all of them;
	// calls arrive from each independently. Stream-opening and shape-changing
	// calls stay serialized on that connection's reader ([classify]); getters
	// and small acts run off it so a keystroke cannot wait behind a listing.
	surfaces map[*server]struct{}
	pumps    sync.WaitGroup

	// tasklanes is the standing task subscription each surface that asked for
	// one is holding, keyed by that surface. It is ONE PER WINDOW rather than
	// one per session because the subscription replays the graph's roster as it
	// opens, and a window that arrived late needs that replay for itself
	// (tasklane.go states the whole of it).
	tasklanes map[*server]*taskFeed

	// newsfeeds is one outbox per surface for the live status row — the phase
	// clock and the lane sighting, which are pushed at this process by
	// internal/session's two global readers and have to be steered to the
	// connection they belong to (news.go). It is keyed by surface for the task
	// lane's reason and drained by a goroutine per surface for one this file
	// has nowhere else: the fan-out runs on a turn's own stream goroutine, and
	// a status line may not be able to stall the turn it is measuring.
	newsfeeds map[*server]*newsFeed

	// lastLane is the last finished answer's sighting this conversation
	// produced — which machine answered — kept so that a window arriving
	// AFTER the answer is told who answered (news.go's [Session.watchNews]).
	// Without it a window attached to a conversation the host had been
	// holding for an hour drew the model and no machine until the next
	// answer, which the owner read as the provider having gone missing
	// (2026-09-17). Rescues in flight and withdrawals are not kept: they are
	// claims about a moment, and only a landed answer is a fact about the
	// conversation.
	lastLane *session.LaneNews

	// lanes is the same arrangement for the harness subscription version 11
	// added, keyed by lane and then by the surface holding it
	// (standinglane.go). It is a map by lane rather than one field because
	// every road that ends a subscription ends all of them: a surface leaving,
	// a session swap, a conversation closing.
	lanes map[laneName]map[*server]*laneFeed

	// wakeStop is the way out of this conversation's one subscription to the
	// turns it starts itself (wakelane.go). Nil is a conversation whose agent
	// has no wake lane to hold — a scripted engine — or one whose subscription
	// was left by a swap and not yet retaken.
	wakeStop *wakeWatch

	// driver is the one surface that may put words into this conversation, and
	// arrivals is what "newest" means when the keyboard has to find one. Both
	// are driver.go's, and that file is the whole of the rule.
	driver   *server
	arrivals uint64
	// factsRev counts the fact sets this session has stated. It is minted under
	// mu beside the reading it labels, which is what makes it an ORDER and not
	// a timestamp: two facts can move in the same instant and the two pushes
	// race to the writers, so the surface keeps the highest number it has seen
	// and drops anything older (wire.go's [FactsPush]).
	factsRev uint64
	// weighOwed says a tool batch has ended and the facts have not been stated
	// since its results joined the conversation ([weighsAgain]).
	weighOwed bool

	// closed is the conversation deliberately ended — [MethodClose], or the
	// pipe dying on an engine whose life this pipe was.
	closed bool
	// empty is when the last surface left, and zero while somebody is here. It
	// is what an idle policy measures (internal/enginehost).
	empty time.Time
	// acted is when a window last made a call. A stalled road tears the pipe
	// under a window that has not gone anywhere, and this is the reading that
	// keeps that gap from looking like an empty room. Zero is a conversation
	// nobody has called into.
	acted time.Time
	// lastWatch is the last surface name that acted or left, so a stop the
	// unattended door takes can say which window it believed had gone. It is a
	// label, never an identity, and empty when nobody was ever here.
	lastWatch string
}

// watchGrace is how long a window goes on counting as somebody watching after
// its last call. THE GAP IT COVERS IS A STALLED CALL PLUS THE REDIAL AFTER IT,
// so the span is derived from [callDeadline] and never retyped: one deadline
// for the call that did not come back, and one for the link that has not been
// dialled again yet.
const watchGrace = 2 * callDeadline

// WatchFor is that grace as [Session.watchedLocked] applies it, and it is
// [watchGrace] everywhere the product runs. It is a var for the reason
// internal/enginehost's sessionIdle is one: a test asking what the IDLE POLICY
// decides is not asking about a stalled road, and it must be able to take this
// span out of the question rather than wait it out. Nothing in the product
// writes it.
var WatchFor = watchGrace

// NewSession wraps an opened engine as a conversation. Persistent says the
// engine outlives its connections, which is the fact [Welcome.Persistent]
// carries and the fact a torn pipe is read against.
func NewSession(engine *Engine, persistent bool) *Session {
	if engine == nil {
		return nil
	}
	sess := &Session{
		engine:     engine,
		agent:      engine.Agent,
		persistent: persistent,
		rings:      map[uint64]*ring{},
		held:       newHeldSet(),
		surfaces:   map[*server]struct{}{},
		tasklanes:  map[*server]*taskFeed{},
		lanes:      map[laneName]map[*server]*laneFeed{},
		newsfeeds:  map[*server]*newsFeed{},
		empty:      time.Now(),
	}
	// The conversation watches its own turns from the moment it exists, so a
	// wake that runs while nobody is attached still mints the stream an
	// arriving surface reads in its welcome — and the turn itself is journalled
	// either way, because the engine is the only writer of the session file.
	sess.watchOwnTurns()
	// AND THE CONVERSATION IS FILED UNDER THE NAME ITS OWN NEWS ARRIVES UNDER,
	// from the moment it exists, so the phase of a wake that runs before
	// anybody attaches has somewhere to be steered to (news.go).
	sess.fileNews()
	return sess
}

// Workspace is the directory this conversation works in — the answer a host
// files it under and the one a person reads in a listing.
func (sess *Session) Workspace() string {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.engine.Workspace
}

// Attached is how many surfaces are watching right now.
func (sess *Session) Attached() int {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return len(sess.surfaces)
}

// Ended reports that this conversation is over — somebody said goodbye through
// [MethodClose], or the pipe that was its whole life went away. A host reaps
// one that says so.
// File is the transcript this conversation is writing.
//
// IT IS THE ONE IDENTITY A CONVERSATION HAS OUTSIDE THIS PACKAGE. A host keys
// its sessions by whatever the first hello said, which is a launch's word and
// not the conversation's own; the file is what the world scan, the presence
// files, the task index and the surface all name a conversation by. So a caller
// answering "is the conversation in this file already open here" asks this
// ([Hello.Join] is that caller).
//
// It is read under the session's own lock because a swap writes it
// ([Session.swap]).
func (sess *Session) File() string {
	if sess == nil {
		return ""
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.engine == nil {
		return ""
	}
	return sess.engine.SessionFile
}

func (sess *Session) Ended() bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.closed
}

// IdleSince is when this conversation went quiet, and the zero time when it has
// not. Five things make it busy: a surface attached, a turn still streaming, a
// question waiting to be answered, WORK THE CONVERSATION HANDED OFF, and a
// window that acted inside [watchGrace].
//
// The fourth was missing and it had a clock on it. A task, an adaptive run and a
// background job outlive the turn that started them; the turn ends, its ring is
// dropped, and the session read as idle while the workers ran on — thirty minutes
// later the sweep closed it and took the work. So the agent is asked, and
// [session.Agent.WorkingNow] is the authoritative reading: tasks, runs and jobs
// together.
//
// The agent is asked off this lock, because that walk takes locks of its own and
// holding sess.mu across it would put the sweep in front of every event a turn
// records. The answer can age while it is read, so the state is re-checked
// afterwards and a conversation that changed under the reading is reported busy
// for this pass: one sweep too many costs a minute of memory, one too few costs
// somebody's work. [Session.RetireIfIdle] is where that re-check becomes a
// decision.
func (sess *Session) IdleSince() time.Time {
	sess.mu.Lock()
	agent, generation := sess.agent, sess.generation
	idle := sess.idleSinceLocked()
	sess.mu.Unlock()
	if idle.IsZero() || agent == nil {
		return idle
	}
	if workingNow(agent) {
		return time.Time{}
	}
	// The re-check. A surface may have arrived, a turn opened, a card raised or
	// the whole conversation swapped while the walk above ran; a swap makes the
	// reading about an agent this session no longer has, so it is discarded.
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.agent != agent || sess.generation != generation {
		return time.Time{}
	}
	return sess.idleSinceLocked()
}

// idleSinceLocked is the half of the reading this package can answer itself.
func (sess *Session) idleSinceLocked() time.Time {
	if sess.watchedLocked() || len(sess.rings) > 0 || sess.heldOutstandingLocked() > 0 {
		return time.Time{}
	}
	return sess.empty
}

// watchedLocked is somebody being in the room, and it is the one reading the
// unattended sentence may be said on.
//
// A SURFACE ATTACHED IS THE OBVIOUS HALF. The other half is a window that
// ACTED inside [WatchFor] and whose pipe has since gone quiet, because that is
// what a stalled call does to a window nobody has left: the road takes ten
// seconds to give up, the link is torn, the redial has not landed, and for that
// gap a person sitting in front of the conversation looks to this package
// exactly like an empty room. Reading it as one is how a reply was stopped
// under somebody who had just pressed a key in it (#833).
//
// IT IS DELIBERATELY NOT THE IDLE READING. A turn still streaming with nobody
// in front of it is busy AND unwatched, and the two questions have different
// answers there — [Session.idleSinceLocked] asks whether the conversation is
// doing anything, and this asks whether anybody is there to be told about it.
func (sess *Session) watchedLocked() bool {
	if len(sess.surfaces) > 0 {
		return true
	}
	return WatchFor > 0 && !sess.acted.IsZero() && time.Since(sess.acted) <= WatchFor
}

// workingNow asks a conversation whether anything it started is still going.
//
// It is asserted rather than required of [WrappedAgent] on tasklane.go's terms:
// a scripted agent with no graph has no work, and an engine that cannot answer
// is not one to be kept alive on suspicion. Any live root counts — a queued node
// is work that has not started YET, not work that is over.
func workingNow(agent WrappedAgent) bool {
	door, ok := agent.(interface{ WorkingNow() []session.WorkNode })
	if !ok {
		return false
	}
	return len(door.WorkingNow()) > 0
}

// Close ends the conversation: the turn in flight stops where it is and keeps
// its partial reply, and the agent is closed, which flushes the journal.
//
// THE JOURNAL IS THE SAFETY. An engine is the only writer of its session file
// and no surface holds anything that is not in it, so the whole of "did this
// conversation survive" is whether this ran. It is idempotent because every
// road out of a connection may call it and a host may call it again on the way
// down.
// AND THE DOOR IS READ FROM PRESENCE, because this is the road the whole
// machine takes when it goes away — internal/enginehost's shutdown, which is
// `codeaf engine --stop`, a signal, and a stale build letting go. A window
// still in the room means the engine went out from under somebody, and that is
// not the unattended door however the host was asked. An empty room is.
func (sess *Session) Close() error {
	sess.mu.Lock()
	door, name := session.StopByEngineStopped, ""
	if !sess.watchedLocked() {
		door, name = session.StopByRetired, sess.lastWatch
	}
	sess.mu.Unlock()
	return sess.closeFor(door, name)
}

// closeFor is [Session.Close] for a road that already knows which door it is,
// and it is the one place the closed flag is won so that no two endings can
// both interrupt the turn.
func (sess *Session) closeFor(door session.StopDoor, name string) error {
	sess.mu.Lock()
	agent, already := sess.agent, sess.closed
	sess.closed = true
	sess.mu.Unlock()
	return sess.shutDown(agent, already, door, name)
}

// closeLeaving is a person's own goodbye: [MethodClose] said the conversation
// is over, which is leaving it, not retiring it for want of a watcher.
func (sess *Session) closeLeaving() error {
	return sess.closeFor(session.StopByLeaving, sess.lastWatched())
}

// lastWatched is the window this conversation last saw, as a label and never as
// an identity. Empty is a conversation nobody was ever in.
func (sess *Session) lastWatched() string {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.lastWatch
}

// RetireIfIdle ends this conversation if it has been idle for longer than the
// policy allows, and answers whether it is now over.
//
// IT EXISTS FOR THE GAP BETWEEN ASKING AND ACTING. A caller that read
// [Session.IdleSince] and then called Close would be deciding on a photograph:
// a surface can attach, a turn can open and a card can be raised in between, and
// closing then would take down a conversation somebody had just come back to.
// Here the last look and the decision to end it happen under ONE acquisition of
// the session lock, so anything that goes through that lock either arrives
// before the look — and cancels the retirement — or finds the conversation
// already ended and is handed a fresh one (internal/enginehost's open).
//
// WHAT IS STILL A PHOTOGRAPH is the work reading inside IdleSince, which cannot
// be taken under this lock (it walks the graph). The guarantee is therefore:
// nothing that reaches this session's own state can be lost, and a background
// worker that starts something new in the microseconds after the walk is
// interrupted with its journal flushed, exactly as a person's own /quit would.
// The window is that walk, not the whole idle span.
func (sess *Session) RetireIfIdle(olderThan time.Duration) bool {
	idle := sess.IdleSince()
	if idle.IsZero() || time.Since(idle) <= olderThan {
		return false
	}
	sess.mu.Lock()
	if again := sess.idleSinceLocked(); again.IsZero() || time.Since(again) <= olderThan {
		// Somebody came back, a turn opened, or a card was raised while the
		// question was being asked. The conversation stays.
		sess.mu.Unlock()
		return false
	}
	agent, already := sess.agent, sess.closed
	name := sess.lastWatch
	sess.closed = true
	sess.mu.Unlock()
	_ = sess.shutDown(agent, already, session.StopByRetired, name)
	return true
}

// shutDown is what ending a conversation does once the caller has won the
// closed flag: every lane left, the turn interrupted, the journal flushed.
func (sess *Session) shutDown(agent WrappedAgent, already bool, door session.StopDoor, name string) error {
	// The rails are left BEFORE the agent is, so no lane is still delivering off
	// a conversation that is being flushed and shut (tasklane.go), and version
	// 11's subscription goes the same way (standinglane.go).
	sess.closeTaskLanes()
	sess.closeLanes()
	// The wake lane goes with them and for the same reason: it is a rail, and
	// a rail left open on a conversation being flushed is a subscription the
	// sweep has already decided is over (wakelane.go).
	sess.stopWakeLane()
	// AND THE NEWSROOM LOSES THIS CONVERSATION, which is what puts the two
	// global readers back when the last one on this process goes: a host
	// holding nothing has to read as a build with nobody watching, because that
	// is what decides whether a stalled pinned lane is asked about or quietly
	// borrowed against (news.go).
	sess.dropNews()
	if agent == nil || already {
		return nil
	}
	// THE DOOR IS THE ONE THIS ENDING CAME THROUGH, not a single sentence
	// shared by every road out. A named interrupt is preferred when the
	// agent can carry the window; a scripted engine that cannot still
	// hears the door.
	if named, ok := agent.(interface {
		InterruptNamed(session.StopDoor, string)
	}); ok {
		named.InterruptNamed(door, name)
	} else {
		agent.InterruptFor(door)
	}
	err := agent.Close()
	sess.noteClosed(agent)
	return err
}

// noteClosed tells the door that built this agent that its conversation is
// over, so whatever that door registered the agent in lets go of it.
//
// IT RUNS ON THE CLOSE THAT ACTUALLY HAPPENED and after the agent's own Close:
// a door told first would be forgetting a conversation still flushing its
// journal, and the `already` return in [Session.shutDown] never reaches here.
// It is read off the engine rather than captured at construction for the reason
// [Session.folder] gives about its own reading — the engine behind a session
// can be swapped under it.
func (sess *Session) noteClosed(agent WrappedAgent) {
	if agent == nil || sess.engine == nil || sess.engine.Closed == nil {
		return
	}
	sess.engine.Closed(agent)
}

// folder is where a picture arriving on this wire gets written down: the
// engine's workspace and its session folder (image.go). It is a reader on the
// session rather than two fields a caller reaches into, because the engine
// behind a session can be swapped under it.
func (sess *Session) folder() (string, session.Place) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.engine.Workspace, sess.engine.Place
}

func (sess *Session) current() WrappedAgent {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.agent
}

// agentOf is the engine to deliver to WHEN the conversation open here is the one
// the caller named, and it takes both under one hold of the lock [Session.swap]
// replaces them under — the caller cannot check the name and then act on an
// agent the swap moved in between. An empty claim is a caller making none.
func (sess *Session) agentOf(conversation string) (WrappedAgent, bool) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	conversation = strings.TrimSpace(conversation)
	if conversation == "" {
		return sess.agent, true
	}
	// AN UNKNOWN IDENTITY IS NOT A MATCH. A session with no transcript name
	// cannot establish the claim, so the claim is refused — the same reading
	// [session.Agent.thisConversation] takes.
	open := strings.TrimSpace(sess.engine.SessionFile)
	if open == "" || filepath.Clean(conversation) != filepath.Clean(open) {
		return nil, false
	}
	return sess.agent, true
}

// steerConversation is the conversation ONE correction has to be delivered to,
// out of the two names a connection can carry: what it JOINED (bound at the
// door, and never sent again) and what this send CLAIMED ([TaskSteerArgs.Session],
// the conversation the surface believed it was addressing). False is a
// correction that must not be delivered at all.
//
// NEITHER NAME OVERRIDES THE OTHER. A bound connection that claims a different
// conversation is refused rather than served under either reading, because one
// of the two beliefs is wrong and there is no way to tell which. A bound
// connection that claims nothing is checked against its binding, so a swap
// cannot answer it with the replacement's task of the same number. An unbound
// connection — an ordinary window, which FOLLOWS its session — is checked
// against its claim alone, exactly as before, and an empty claim is a caller
// making none.
func steerConversation(joined, claimed string) (string, bool) {
	joined, claimed = strings.TrimSpace(joined), strings.TrimSpace(claimed)
	if joined == "" {
		return claimed, true
	}
	if claimed != "" && !sameTranscript(claimed, joined) {
		return "", false
	}
	return joined, true
}

// ErrJoinedGone is a joined connection finding that the conversation it joined
// is not the one this session is running any more. It is a fact and not a
// failure: the window that owns the session opened something else in it, and a
// reader bound to the old one has nothing left to read.
var ErrJoinedGone = errors.New("engine: that conversation is not open here any more")

// joinRefusal is what a hello that said [Hello.Join] is told when the
// conversation it named is not the one this session is running.
//
// IT NAMES WHAT WAS ASKED FOR, because the surface that asked is holding a row
// it read off a disk a moment ago and the useful fact is which conversation the
// engine could not give it. AND A JOIN THAT NAMED NOTHING MEETS THE SAME
// REFUSAL: an unknown identity is not a match ([Session.agentOf] states the same
// law for a call), so it is answered rather than quietly turned into a surface
// that follows whatever this session opens next.
func joinRefusal(asked string) error {
	if asked = strings.TrimSpace(asked); asked == "" {
		return fmt.Errorf("%w: a join has to name the conversation it wants", ErrJoinedGone)
	}
	return fmt.Errorf("%w: %s", ErrJoinedGone, asked)
}

// serving picks the agent AND the record reader together, under ONE lock, and
// refuses when a joined connection's conversation has been replaced.
//
// THE PAIR IS THE POINT. They used to be taken under two separate locks, so a
// [Session.swap] landing between them handed one call the OLD agent and the NEW
// conversation's record reader — two halves of two different conversations
// answering one question about a task id that means something in each.
//
// AND THE IDENTITY IS RE-CHECKED ON EVERY CALL, not once at the door. A welcome
// proves whose conversation this was at the instant of attaching; another window
// on the same session can call [MethodSessionOpen] a second later and replace it
// underneath, and every read after that would be answered by the replacement —
// under the name the reader is still drawing. `want` is empty for an ordinary
// surface, which FOLLOWS its session wherever the person takes it; it is set
// only for a connection that said [Hello.Join], which asked for one conversation
// and must be told rather than quietly moved.
func (sess *Session) serving(want string) (WrappedAgent, func(string, int) (session.TaskRecord, error), error) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if want != "" && !sameTranscript(want, sess.engine.SessionFile) {
		return nil, nil, ErrJoinedGone
	}
	return sess.agent, sess.engine.TaskRecord, nil
}

// sameTranscript compares two paths to one journal. Both sides of this arrived
// as text — one off a hello, one out of an engine — and neither promises the
// other's spelling.
func sameTranscript(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// ── the ring of a running turn ──────────────────────────────────────────────

// ring is one running stream's recent events, oldest kept first. It is a window
// and not a log: see [ringEvents] for what falls off it and why that is an
// answer rather than a failure.
type ring struct {
	// first is the seq of events[0], so a cursor can be turned into an index
	// without walking. It is 1 on an empty ring, because seq counts from 1 and
	// zero means "nothing of this stream has been seen" (wire.go's Frame.Seq).
	first  uint64
	last   uint64
	events []json.RawMessage
}

func (r *ring) add(payload json.RawMessage) uint64 {
	r.last++
	r.events = append(r.events, payload)
	if len(r.events) > ringEvents {
		r.events = r.events[len(r.events)-ringEvents:]
		r.first = r.last - uint64(len(r.events)) + 1
	}
	return r.last
}

// after is everything this ring still holds past a cursor, with the seq of the
// first frame returned. A cursor ahead of the ring — a surface claiming to have
// seen more than exists, which is a different build or a different engine — is
// answered with nothing rather than with an argument.
func (r *ring) after(seq uint64) (uint64, []json.RawMessage) {
	if seq >= r.last {
		return 0, nil
	}
	from := seq + 1
	if from < r.first {
		from = r.first
	}
	index := int(from - r.first)
	if index < 0 || index > len(r.events) {
		return 0, nil
	}
	return from, r.events[index:]
}

// ── attaching, and what a surface is told on arrival ────────────────────────

// attach lets one connection into the room and answers its hello.
//
// THE CALLER HOLDS THAT CONNECTION'S WRITE LOCK, and that is not an
// optimization — it is the ordering. Between "this surface is attached" and
// "this surface has been sent its welcome" a live turn may emit an event, and a
// surface that received an event before the welcome naming its stream has been
// handed a frame about a conversation it has not been told it is in. Holding
// the write lock across the whole arrival makes the two orders one: a pump that
// snapshotted this surface waits behind the welcome, and a pump that did not is
// already in the replay.
//
// ── AND A JOIN IS CONFIRMED BEFORE ANYTHING IS TOUCHED ──
//
// The host matched [Hello.Session] against the conversations it holds and then
// LET GO OF ITS OWN LOCK (internal/enginehost's Host.open); this is the first
// moment the session's lock is taken. A [MethodSessionOpen] landing in that gap
// used to be absorbed silently, because what was recorded here was the
// conversation that turned out to be open rather than the one the hello asked
// for — after which [Session.serving], [Session.laneIsOwnedLocked] and
// [Session.agentOf] all compared the replacement against itself and passed
// forever. So the hello's own name is what is checked, and a mismatch is a
// refusal at the door: nothing is numbered, nothing is added to the room, and
// the keyboard is not touched, because a connection that is about to be told
// "no" must not first take the keys off the window that owns the work.
var errExecutionMode = errors.New("this conversation is already open in a different interactive or headless mode; close it before retrying")

func (sess *Session) attach(s *server, hello Hello) error {
	sess.mu.Lock()
	if hello.Headless != sess.engine.Headless {
		sess.mu.Unlock()
		return errExecutionMode
	}
	if hello.Join && !sameTranscript(hello.Session, sess.engine.SessionFile) {
		sess.mu.Unlock()
		return joinRefusal(hello.Session)
	}
	// THE ARRIVAL IS NUMBERED BEFORE THE WELCOME IS BUILT, because the welcome
	// says which questions THIS surface is owed and the number is how a card
	// names the surfaces that have already drawn it (held.go).
	s.name = machineLabel(hello.Surface)
	// THE ARRIVAL IS AN ACT. A window that has just been welcomed is
	// watching, and a pipe that tears on the way in — or a getter that
	// no longer crosses the wire — must not read as nobody having been
	// here. Every later call renews the same clock from dispatch.
	sess.acted = time.Now()
	if s.name != "" {
		sess.lastWatch = s.name
	}
	sess.arrivals++
	s.arrived = sess.arrivals
	// Attached counts the OTHERS, so it is read before this one is added
	// (wire.go's Welcome.Attached states why the number is carried at all).
	welcome := sess.welcomeLocked(s)
	welcome.Encoding = s.encoding
	welcome.Attached = len(sess.surfaces)
	sess.surfaces[s] = struct{}{}
	sess.empty = time.Time{}
	// THE NEWEST WINDOW DRIVES, and a window that merely lost its link is not a
	// new one (driver.go states the whole rule, [Hello.Back] states why the
	// difference is load-bearing). A WATCHER IS NEITHER: it never drives, and the
	// flag is recorded so no later hand-on can give it the keyboard either.
	s.watching = hello.Watch
	if hello.Join {
		// WHAT THIS CONNECTION IS BOUND TO is the conversation it named, confirmed
		// against the one open under this same lock a few lines above. The engine's
		// own spelling of the path is what is kept, because every later comparison
		// is against that field and one spelling saves a clean on every call.
		s.joined = sess.engine.SessionFile
	}
	if !s.watching && (!hello.Back || sess.driver == nil) {
		sess.takeLocked(s)
	}
	welcome.Driver = sess.driverForLocked(s)
	replay := sess.replayLocked(hello.Resume)
	sess.mu.Unlock()

	if err := s.sendLocked(Frame{Kind: "welcome", Payload: mustJSON(welcome)}); err != nil {
		return err
	}
	for _, frame := range replay {
		if err := s.sendLocked(frame); err != nil {
			return err
		}
	}
	// THE LIVE ROW OPENS LAST, after the welcome and the replay are on the wire,
	// so a phase measured while this surface was being welcomed cannot overtake
	// the welcome that tells it which conversation it is in (news.go).
	sess.watchNews(s)
	return nil
}

// detach takes one connection out of the room, and — when it was the last one —
// starts the clock an idle policy reads. IT IS NOT WHAT MAKES A CARD WAITING:
// a killed terminal leaves without saying so, and this runs whenever its socket
// gets around to reporting the end, which may be after the window that replaced
// it has already been welcomed (held.go states the whole rule).
func (sess *Session) detach(s *server, deliberate bool) {
	// THE RAIL'S SUBSCRIPTION GOES WITH THE WINDOW. It is left before anything
	// else because leaving it is what ends the goroutine pumping frames at a
	// pipe that is closing (tasklane.go).
	sess.dropTaskLane(s)
	sess.dropLanes(s)
	sess.dropNewsFeed(s)
	sess.mu.Lock()
	if s.name != "" {
		sess.lastWatch = s.name
	}
	delete(sess.surfaces, s)
	// THE KEYBOARD IS NEVER LEFT ON A WINDOW THAT HAS GONE. It goes to the
	// newest surface still here, so the last window standing can always type
	// (driver.go's handOnLocked).
	moved := false
	if sess.driver == s {
		sess.handOnLocked()
		moved = true
	}
	if len(sess.surfaces) == 0 {
		sess.empty = time.Now()
		// A deliberate detach is the window saying it has gone, so its last
		// action needs no torn-link grace. Keeping that grace made a cleanly
		// closed window look attached for another two call deadlines, which in
		// turn kept an in-place update on the old engine it had just left.
		if deliberate {
			sess.acted = time.Time{}
		}
	}
	sess.mu.Unlock()
	if moved {
		sess.tellDriver(s)
	}
}

// steerFromDoor is an engine that takes a correction WITH THE SURFACE'S NAME
// FOR THE SEND on it, and recognises the same send arriving twice
// (internal/session's [session.Agent.SteerTaskFrom]).
//
// IT IS ONE INTERFACE FOR TWO QUESTIONS ASKED AT DIFFERENT MOMENTS: the
// handler asserts it to route a named send, and the welcome asserts it to tell
// the surface, before anything is sent, whether asking twice is safe here
// ([Welcome.SteerRepeat]). Two assertions of one door would be two answers to
// one question the day an engine grew half of it.
type steerFromDoor interface {
	SteerTaskFrom(uint64, string, session.SteerSource) (session.SteerReceipt, error)
	SteerRepeatKnown() bool
}

// steeredOf is the engine's receipt as the frame carries it. It is one function
// because a field added to the receipt and forgotten in one of the handler's
// branches is a fact that arrives on some engines and not others.
func steeredOf(receipt session.SteerReceipt) TaskSteered {
	return TaskSteered{
		Waiting:   receipt.Waiting,
		Held:      receipt.Held,
		Direction: receipt.Direction,
		Landing:   receipt.Landing,
		Again:     receipt.Again,
	}
}

// steerRepeatKnown asks this engine whether it keeps the identity on a send.
// The door answers for itself; an engine without the door answers no, which is
// the reading a surface must take from silence ([Welcome.SteerRepeat]).
func steerRepeatKnown(agent any) bool {
	door, ok := agent.(steerFromDoor)
	return ok && door.SteerRepeatKnown()
}

func (sess *Session) welcomeLocked(s *server) Welcome {
	// A HOSTED START MUST READ THE ENGINE'S FILE, not the surface's. Carrying
	// this reading in the welcome is what makes an old persistent engine say
	// it was replaced before somebody has to spend a turn to discover it.
	note := strings.TrimSpace(sess.engine.Note)
	if newer := buildinfo.StaleNotice(); newer != "" {
		if note != "" {
			note += " · "
		}
		note += newer
	}
	return Welcome{
		Version:                    Version,
		Workspace:                  sess.engine.Workspace,
		SessionFile:                sess.engine.SessionFile,
		Resumed:                    sess.engine.Resumed,
		Model:                      sess.agent.Model(),
		Build:                      buildinfo.String(),
		Title:                      sess.agent.Title(),
		Note:                       note,
		ApprovalMode:               sess.engine.ApprovalMode,
		BashBackgroundAfterSeconds: sess.engine.BashBackgroundAfterSeconds,
		ProfileDir:                 sess.engine.ProfileDir,
		UnreadProfileKeys:          append([]string(nil), sess.engine.UnreadProfileKeys...),
		PlacesRoot:                 sess.engine.PlacesRoot,
		Live:                       sess.liveLocked(),
		Held:                       sess.heldWaitingLocked(s.arrived),
		Persistent:                 sess.persistent,
		Launch:                     sess.engine.Launch,
		Facts:                      sess.factsLocked(),
		SteerRepeat:                steerRepeatKnown(sess.agent),
		// Whether this engine can hold a folder at all, asked of the agent it has
		// open — for [Welcome.Folders]'s stated reason: the surface's own type
		// assertion cannot see across the wire.
		Folders: keepsFolders(sess.agent),
		// This revision checks it in the handler, for every engine behind it
		// ([Session.agentOf]), so the answer is about the wire and not the agent.
		SteerOwner: true,
		TaskSetup:  taskSetupKnown(sess.agent),
		TaskRetry:  taskRetryKnown(sess.agent),
		// Whether this engine has a dial on the conversation's own thinking,
		// asked of the agent it has open — for [Welcome.Effort]'s stated reason:
		// neither a type assertion at the far end nor the rung itself can tell an
		// engine without a dial from a conversation whose dial is off.
		Effort:   effortKnown(sess.agent),
		Approval: approvalKnown(sess.agent),
		// AND THE TWO FACTS ABOUT THE INSTALL THE DRAFT READS, carried once
		// (effort.go's [installEffort], approval.go's [standingApproval]).
		DefaultEffort:    installEffort(sess.agent),
		StandingApproval: standingApproval(sess.agent),
		TaskSettle:       taskSettleKnown(sess.agent),
		// Whether this conversation's news reaches the surface at all, asked the
		// way the newsroom files it ([Session.fileNews]): an engine that cannot
		// name its conversation fans nothing out, and says so here.
		News: newsKeyOf(sess.agent) != "",
		// Whether this conversation can carry skills put in front of it by
		// hand, asked of the agent it has open — for [Welcome.Skills]'s stated
		// reason (skills.go).
		Skills: skillsKnown(sess.agent),
	}
}

// factsLocked is the fact set as it stands, numbered.
//
// THE READS HAPPEN UNDER THE SESSION'S LOCK, which is a deliberate and narrow
// exception to the rule that this file does slow things outside it. The reason
// is the number: a revision minted here and a reading taken there would let a
// higher number carry an older photograph, which is exactly the reversal the
// number exists to prevent. What is being paid for that is one walk of the
// transcript ([session.Agent.ContextTokens]) at a moment that happens once per
// turn and once per model change — never on a frame, never on a keystroke —
// and [Session.welcomeLocked] already reads the model and the title from here.
func (sess *Session) factsLocked() *FactsPush {
	if sess.agent == nil {
		return nil
	}
	sess.factsRev++
	return &FactsPush{Rev: sess.factsRev, Facts: session.FactsOf(sess.agent)}
}

// announce states the fact set to everybody watching, unasked.
//
// IT IS THE WHOLE OF "INTENT UP, FACTS DOWN" (wire.go's version 4). A surface
// that had to ask would be asking on the frame that draws the answer, which
// over an ssh pipe is a terminal that has stopped repainting; so the engine
// says it instead, at the four moments a fact actually moves — a turn ending, a
// name being settled, a compaction landing, and somebody changing the model or
// the level.
//
// IT MIRRORS [Session.emit] EXACTLY: the reading and the watchers are taken
// under one hold of the lock and the writes happen outside it, so a slow
// surface cannot hold up the conversation, and a surface arriving mid-announce
// either is in the snapshot and gets this frame or is not and has the fact in
// its welcome. Never both, and never neither.
func (sess *Session) announce() {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	push := sess.factsLocked()
	watching := sess.watchingLocked()
	sess.mu.Unlock()
	if push == nil || len(watching) == 0 {
		return
	}
	frame := Frame{Kind: "facts", Payload: mustJSON(push)}
	for _, surface := range watching {
		_ = surface.send(frame)
	}
}

// factsMoved says whether one event of a turn changes something a frame reads.
//
// State transitions publish spending, identity and attention. Question and
// settlement events must publish before a hidden reader wakes, while ordinary
// text and reasoning deltas carry no changed frame facts.
func factsMoved(kind session.EventKind) bool {
	switch kind {
	case session.EventTurnDone, session.EventError, session.EventTitleChanged, session.EventCompacted,
		session.EventConsentRequest, session.EventToolEnd, session.EventToolFailed,
		session.EventConnectAsk, session.EventConnectDone,
		session.EventTaskProposal, session.EventTaskUpdate,
		session.EventStandingProposal, session.EventStandingUpdate,
		session.EventHarnessOffer, session.EventHarnessRun, session.EventHarnessDesignDone,
		session.EventHarnessDesignRevising, session.EventOrchestratePause, session.EventOrchestrateFuel,
		session.EventSubharnessAsk, session.EventSubharnessProposal, session.EventSubharnessProposalOff:
		return true
	}
	return false
}

// weighsAgainLocked says whether this event is the first word of the request
// that follows a tool batch, which is when what the conversation weighs is
// stated again (the facts' ContextTokens).
//
// THE END OF A BATCH IS STATED ALREADY AND IS ONE STEP EARLY FOR THE WEIGHT. The
// facts go out on every tool end ([factsMoved]) — the step's usage is banked by
// then — but the session records the batch's RESULTS into the conversation only
// after the last end has been published (internal/session's loop), so a reading
// taken there weighs the conversation without the results that are about to be
// sent. A surface drawing the weight of the request in flight — the chat's live
// token column — saw a big file read join the conversation one whole step late.
// The next request is the first moment the results are certainly in, and its
// first word is the first event this pump sees from it.
func (sess *Session) weighsAgainLocked(kind session.EventKind) bool {
	switch kind {
	case session.EventToolEnd, session.EventToolFailed:
		sess.weighOwed = true
	case session.EventTurnDone, session.EventError:
		// The turn's own end states the facts whole, results and all.
		sess.weighOwed = false
	case session.EventTextDelta, session.EventReasoning, session.EventThinking,
		session.EventToolForming, session.EventToolAnnounced:
		if sess.weighOwed {
			sess.weighOwed = false
			return true
		}
	}
	return false
}

// liveLocked is the stream still running, or zero. The newest wins when two are
// somehow in flight at once: a session runs one turn at a time in practice, and
// "the one that is going on right now" is the newer of two by any reading.
func (sess *Session) liveLocked() uint64 {
	var live uint64
	for id := range sess.rings {
		if id > live {
			live = id
		}
	}
	return live
}

// replayLocked is the gap, and only the gap.
//
// EVERY RUNNING STREAM IS REPLAYED FROM WHERE THIS SURFACE GOT TO, and a stream
// it named no cursor for starts at zero — which is not a special case but the
// same arithmetic, because zero means "nothing of this stream has been seen"
// and that is exactly true of a surface arriving from another machine into a
// turn already in flight. A cursor for a stream the engine no longer holds is
// answered with NOTHING and never with an error: that turn is finished, the
// transcript has it, and [Welcome.Live] not naming it is how the surface knows.
func (sess *Session) replayLocked(cursors []StreamCursor) []Frame {
	seen := make(map[uint64]uint64, len(cursors))
	for _, cursor := range cursors {
		if cursor.Seq > seen[cursor.Stream] {
			seen[cursor.Stream] = cursor.Seq
		}
	}
	ids := make([]uint64, 0, len(sess.rings))
	for id := range sess.rings {
		ids = append(ids, id)
	}
	// Oldest stream first, so a surface that missed two turns draws them in the
	// order they happened.
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var frames []Frame
	for _, id := range ids {
		from, events := sess.rings[id].after(seen[id])
		for offset, payload := range events {
			frames = append(frames, Frame{
				Kind:    "event",
				ID:      id,
				Seq:     from + uint64(offset),
				Payload: payload,
			})
		}
	}
	return frames
}

// ── streams ─────────────────────────────────────────────────────────────────

// mint names a stream and opens its ring. The ring exists from this moment
// rather than from the first event, so that a surface attaching between the
// call and the first token still learns there is a turn in flight.
func (sess *Session) mint() (id, generation uint64) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.mintLocked()
}

// mintLocked is [Session.mint] for a caller already holding sess.mu — the
// wake lane, which has to ask whether the conversation it is about to name a
// stream in is still the one its lane belongs to, and has to ask it in the
// same hold of the lock that does the naming (wakelane.go).
func (sess *Session) mintLocked() (id, generation uint64) {
	sess.streams++
	sess.rings[sess.streams] = &ring{first: 1}
	return sess.streams, sess.generation
}

// pump is one turn's events on the wire, in order, followed by the close.
//
// A STREAM BELONGS TO THE AGENT THAT OPENED IT. When a session swap replaces
// that agent every surface has already been handed a fresh welcome and has
// forgotten everything before it, so a late event from the old conversation is
// not stale news but a lie about the new one. The pump keeps draining — the
// channel must not be left with a writer blocked on it — and says nothing.
func (sess *Session) pump(id, generation uint64, events <-chan session.Event) {
	defer sess.pumps.Done()
	defer guard.Recover("remote/engine stream")
	for event := range events {
		sess.emit(id, generation, event)
	}
	sess.finish(id, generation)
}

// emit records one event and fans it out to everybody watching.
//
// THE RECORD IS TAKEN UNDER THE LOCK AND THE WRITES HAPPEN OUTSIDE IT, which is
// what keeps a slow surface from holding up the conversation and what keeps the
// arrival ordering in [Session.attach] honest: a surface either was in the
// snapshot, and gets this frame directly, or was not, and finds it in the
// replay. Never both, and never neither.
func (sess *Session) emit(id, generation uint64, event session.Event) {
	wire := WireEvent(event)
	payload, err := json.Marshal(wire)
	if err != nil {
		// An event that cannot be encoded is a field somebody added that does
		// not survive JSON. Dropping the one event keeps the turn readable,
		// which is better than ending the stream over it.
		return
	}
	sess.mu.Lock()
	if sess.generation != generation {
		sess.mu.Unlock()
		return
	}
	held := sess.rings[id]
	if held == nil {
		sess.mu.Unlock()
		return
	}
	seq := held.add(payload)
	watching := sess.watchingLocked()
	// A QUESTION WAITS INSTEAD OF EXPIRING FOR EVERY SURFACE THIS FRAME IS NOT
	// GOING TO, which is what [heldSet] is for: the windows in the list below
	// are about to draw it, and any other window — including one that has not
	// dialled yet — is owed it on arrival.
	if _, question := heldKeyOf(event); question {
		// Ordinary text deltas do not need a second copy of the watching set.
		drawn := make([]uint64, 0, len(watching))
		for _, surface := range watching {
			drawn = append(drawn, surface.arrived)
		}
		sess.held.raise(wire, id, drawn)
	}
	weighs := sess.weighsAgainLocked(event.Kind)
	sess.mu.Unlock()

	// THE FACTS GO FIRST, AHEAD OF THE EVENT THAT MOVED THEM. A surface settles
	// its turn on EventTurnDone — it reads the spending and the weight the
	// instant that event lands — so a push sent after it would arrive one turn
	// late and the status line would report the turn before this one. The agent
	// has already sealed the turn by the time this event reaches the pump, so
	// the reading taken here is the finished one.
	if factsMoved(event.Kind) || weighs {
		sess.announce()
	}

	frame := Frame{Kind: "event", ID: id, Seq: seq, Payload: payload}
	for _, surface := range watching {
		_ = surface.send(frame)
	}
}

// finish ends a stream. The "closed" frame carries the seq of the LAST event it
// follows, so a surface can tell a stream that ended from one it lost the tail
// of (wire.go's Frame.Seq).
func (sess *Session) finish(id, generation uint64) {
	sess.mu.Lock()
	if sess.generation != generation {
		sess.mu.Unlock()
		return
	}
	var last uint64
	if held := sess.rings[id]; held != nil {
		last = held.last
	}
	// The ring goes with the stream: from here on, a cursor naming it is
	// answered with nothing and the transcript is the authority.
	delete(sess.rings, id)
	watching := sess.watchingLocked()
	sess.mu.Unlock()

	frame := Frame{Kind: "closed", ID: id, Seq: last}
	for _, surface := range watching {
		_ = surface.send(frame)
	}
}

func (sess *Session) watchingLocked() []*server {
	watching := make([]*server, 0, len(sess.surfaces))
	for surface := range sess.surfaces {
		watching = append(watching, surface)
	}
	return watching
}

// swap replaces the conversation and answers with a whole new welcome, because
// everything in one is now different: the file, whether it was resumed, the
// name it has given itself.
//
// The generation moves BEFORE the old agent is closed, so the pumps that are
// about to see their channels close have already gone quiet — and the rings and
// the waiting questions go with it, because both belong to a conversation that
// no surface is being shown any more.
func (sess *Session) swap(asked *server, build func() (WrappedAgent, string, bool, error)) (json.RawMessage, error) {
	next, file, resumed, err := build()
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, errors.New("engine: the session swap opened no conversation")
	}
	sess.mu.Lock()
	previous := sess.agent
	sess.agent = next
	sess.generation++
	sess.rings = map[uint64]*ring{}
	sess.held.forget()
	sess.engine.SessionFile = file
	sess.engine.Resumed = resumed
	// The note belonged to the launch and to nothing after it: a swap the
	// person asked for is not a session that moved under them.
	sess.engine.Note = ""
	welcome := sess.welcomeLocked(asked)
	welcome.Attached = len(sess.surfaces) - 1
	if welcome.Attached < 0 {
		welcome.Attached = 0
	}
	// A SWAP DOES NOT MOVE THE KEYBOARD. The conversation behind the room
	// changed; who is holding the keys to it did not, and the fresh welcome has
	// to say so or the surface reading it would forget.
	welcome.Driver = sess.driverForLocked(asked)
	sess.mu.Unlock()

	if previous != nil {
		previous.InterruptFor(session.StopByLeaving)
		_ = previous.Close()
		// THE SWAP IS A CLOSE LIKE ANY OTHER as far as the door is concerned.
		// /new and /resume retire a conversation without ever reaching
		// [Session.shutDown], so a door told only there would keep every
		// conversation a person opened and left, for the life of the process.
		sess.noteClosed(previous)
	}
	// AND EVERY RAIL IN THE ROOM IS RE-POINTED AT THE CONVERSATION THAT IS
	// ACTUALLY OPEN. The subscriptions above belonged to the agent just closed;
	// each is reopened on the new one, which replays the new graph's roster
	// (tasklane.go). It happens after the close so no lane can be handed rows
	// from a conversation on its way out — the wake lane with the rest of them,
	// because a wake queued on the old conversation's lane must never mint a
	// stream in the one that replaced it.
	sess.retakeTaskLanes()
	sess.retakeLanes()
	sess.retakeWakeLane()
	// AND THE NEWSROOM IS TOLD THE ROOM'S NAME HAS CHANGED. /new and /resume
	// mint a whole new agent, whose news arrives under a name of its own, and a
	// conversation still filed under the old one would draw a status row that
	// stopped moving the moment it was replaced (news.go).
	sess.fileNews()
	return json.Marshal(welcome)
}

// ── one connection ──────────────────────────────────────────────────────────

type server struct {
	// write guards the writer. ONE FRAME IS ONE LINE IS ONE WRITE: a torn frame
	// is not a slow surface, it is a surface that can never parse this stream
	// again, so every writer on this process goes through here.
	write sync.Mutex
	out   io.Writer
	// dead records that the far end stopped listening. A write error is not
	// worth reporting twice and there is nowhere left to report it to.
	dead bool
	// encoding is selected from the hello before the welcome is sent. Empty is
	// the old wire, which keeps a new engine compatible with an older surface.
	encoding string

	open    func(Hello) (*Session, error)
	session *Session
	// host answers a connection that asked which build this is, and asked it to
	// go (whois.go). It is nil on a pipe engine, which is nobody's host.
	host func(WhoIs) HostSelf
	// asked records that this connection was the version exchange and nothing
	// else: it opened no conversation, it has been answered, and the serve loop
	// returns rather than waiting for a line that is not coming.
	asked bool

	// name is the machine this surface is running on, as its hello said and
	// [machineLabel] made it safe to draw. arrived is its place in the order the
	// room filled up, which is how "the newest" is decided (driver.go).
	name    string
	arrived uint64
	// watching is [Hello.Watch]: this surface reads and never drives. It is kept
	// on the connection because the decision is made in three places — arrival,
	// the hand-on when a driver leaves, and the guard in front of every door that
	// types — and all three have to agree (driver.go).
	watching bool
	// joined is the transcript a [Hello.Join] connection asked for, and "" for an
	// ordinary surface. It is re-checked on every call ([Session.serving]),
	// because a welcome proves whose conversation this was at the instant of
	// arriving and another window can replace it a second later.
	joined string

	// pending is the stream a call has just opened and dispatch has not yet let
	// speak. It is one slot rather than a queue, and it needs no lock, because
	// only an ORDERED call opens a stream and every ordered call on this
	// connection runs on the one lane below, one at a time
	// ([callClass.opensAStream]).
	pending *pending
	// ordered is that lane: every call that owes an order, in the order the
	// surface sent it, on one goroutine that is not the reader.
	//
	// IT IS WHY A KEYSTROKE NO LONGER WAITS FOR A SEND. Everything the engine
	// does between Submit and the first request leaving — the client rebind,
	// the standing orders, the system prompt, the journal write, the naming
	// errand — used to run on the reader, and every frame behind it on this
	// socket waited for all of it (callclass.go's header).
	ordered *orderedLane
	// side is every getter and small act this connection has handed off the
	// reader. The serve loop waits for it before leave, so a listing still
	// running does not write into a session that has already been closed.
	side sync.WaitGroup
	// Observers leave with the view, independently of the durable turn pump.
	observersMu sync.Mutex
	observers   map[uint64]*taskFeed

	// leaving records how this connection ends, and it is the whole of version
	// 2's three-roads-out. detached is [MethodDetach] — the surface is going and
	// the turn is the engine's to finish. goodbye is [MethodClose] — the
	// conversation itself is over. Neither set, and a reader that stops, is a
	// TORN PIPE, whose meaning depends on whether anything is holding the
	// conversation (see the bottom of [server.serve]).
	// They are atomics because they are decided on the ordered lane and read by
	// the reader loop, which is the one place in this file where two goroutines
	// look at the same fact. The reader's read is a HINT that lets it stop a
	// frame early; the reading that matters is [server.leave]'s, and that one
	// happens after the lane has drained.
	detached atomic.Bool
	goodbye  atomic.Bool
}

func (s *server) serve(in io.Reader) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// A panic in a handler must not end the process in silence: the
			// surface is sitting on a pipe that stopped answering and would
			// have no idea why. The stack goes to the log, one sentence goes
			// down the wire, and the exit code says it was a fault.
			err = guard.Note("remote/engine", recovered)
			s.fatal(err.Error())
		}
		s.side.Wait()
		s.stopObservers()
		s.leave()
	}()

	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 0, 64*1024), frameCap)

	if !scan.Scan() {
		// A surface that hung up before it said hello opened nothing, so there
		// is nothing to flush and nothing to complain about.
		return scan.Err()
	}
	if err := s.handshake(scan.Bytes()); err != nil {
		return err
	}
	if s.asked {
		// THE QUESTION WAS THE WHOLE CONNECTION. It opened nothing, so there is
		// nothing to flush and nothing to say about it in a log.
		return nil
	}

	// THE ORDERED LANE IS OPENED WITH THE CONNECTION AND DRAINED BEFORE IT
	// LEAVES. The drain is deferred here rather than at the top so that it runs
	// BEFORE the leave the first defer registered — deferred calls run in
	// reverse — which is what makes [server.leave] read a `detached` or a
	// `goodbye` that was decided on the lane a moment ago.
	s.ordered = newOrderedLane()
	go s.ordered.run(s.dispatch)
	defer func() {
		s.ordered.close()
		s.ordered.wait()
	}()

	for scan.Scan() {
		frame, err := readCall(scan.Bytes())
		if err != nil {
			s.fatal(err.Error())
			return err
		}
		// WHERE A CALL RUNS IS ITS CLASS'S PROPERTY AND NOT A DECISION TAKEN
		// HERE (callclass.go's [callClass.road]). This switch is the whole of
		// the reader's part in it, so a method added next year travels the road
		// its class names without anybody touching the loop that reads the
		// socket.
		switch classify(frame.Method).road() {
		case inOrder:
			s.ordered.hand(frame)
		default:
			s.side.Add(1)
			go func(call Frame) {
				defer s.side.Done()
				s.dispatch(call)
			}(frame)
		}
		if s.detached.Load() {
			// THE SURFACE SAID IT WAS GOING, and said so before it went, which
			// is the fact version 1 could not express. The turn keeps running;
			// this connection is simply over.
			//
			// THIS IS A SHORTCUT AND NO LONGER THE ROAD. [MethodDetach] is
			// ordered, so the flag is stored on the lane long after the reader
			// has looped back into Scan, and this test will essentially never
			// be the thing that ends the loop. What ends it is the pipe: the
			// surface closes it the moment the call returns, Scan reaches EOF,
			// the lane drains, and [server.leave] reads a settled flag. Kept
			// because it costs one atomic load and is honest when it does win.
			return nil
		}
		if s.hungUp() {
			// The far end stopped reading. Everything below the exit is the
			// ordinary hang-up path, because it is the ordinary hang-up.
			return nil
		}
	}
	if err := scan.Err(); err != nil {
		// A line too long or a read that failed is the protocol breaking, not a
		// person leaving.
		s.fatal(err.Error())
		return err
	}
	// READER EOF IS THE ORDINARY END OF A CONNECTION, and version 2 is careful
	// about what it is NOT the end of. The person closed the surface, the ssh
	// channel went away, the laptop lid shut — all the same event, and all read
	// as "this surface will be back" when something is holding the conversation.
	// See [server.leave].
	return nil
}

// leave is the road out, and it runs on every one of them — the detach, the
// hang-up, the refusal, the fault.
//
// THE FORK IS THE WHOLE OF THE PERSISTENT ENGINE, said in one condition:
//
//   - A DELIBERATE CLOSE ends the conversation. [MethodClose] is the person
//     saying they are done, and one surface saying it ends it for every surface
//     attached, because it is a statement about the conversation and not about
//     a window.
//   - A TORN PIPE ON A PERSISTENT ENGINE ends nothing. A laptop lid, a dropped
//     wifi, a killed ssh — the engine assumes the surface will be back, keeps
//     the turn running, keeps its events in the ring, and holds any question it
//     raises.
//   - A TORN PIPE ON AN ENGINE THAT IS NOT PERSISTENT ends the conversation,
//     and that is still the honest reading. A bare `codeaf engine` on a pipe
//     with no host behind it IS the conversation's whole life: nothing will
//     ever attach to it again, so a turn left running would burn a person's
//     money into a journal nobody will reopen. The interrupt goes first so the
//     turn stops where it is and keeps its partial reply — exactly what ctrl+c
//     does locally — and then the agent is closed, which flushes the file.
func (s *server) leave() {
	sess := s.session
	if sess == nil {
		return
	}
	sess.detach(s, s.detached.Load())
	if s.goodbye.Load() {
		_ = sess.closeLeaving()
		return
	}
	if !sess.persistent {
		// AND THIS ONE IS THE UNATTENDED DOOR BY CONSTRUCTION, whatever the
		// grace above would say: the pipe that just tore WAS this
		// conversation's whole life, so nothing is ever going to attach to it
		// again and there is no window left to come back. It names the one it
		// had, which is the last thing anybody reading the journal can use.
		_ = sess.closeFor(session.StopByRetired, sess.lastWatched())
	}
}

// handshake reads the first line and answers it. A version mismatch and a line
// that is not a hello are the same refusal for the same reason wire.go gives:
// two builds that might disagree about a frame must not guess at each other.
func (s *server) handshake(line []byte) error {
	var frame Frame
	if err := json.Unmarshal(line, &frame); err != nil {
		return s.refuse("engine: the first frame was not JSON")
	}
	if frame.Kind == "whois" {
		// ASKED BEFORE THE VERSION IS CHECKED, ON PURPOSE. A build that would
		// be refused for its protocol is exactly the build somebody needs an
		// answer from, so this question is the one frame that outranks the
		// door (whois.go states why).
		return s.whois(frame)
	}
	if frame.Kind != "hello" {
		return s.refuse(fmt.Sprintf("engine: the first frame was %q, not a hello", frame.Kind))
	}
	var hello Hello
	if err := json.Unmarshal(frame.Payload, &hello); err != nil {
		return s.refuse("engine: the hello did not parse")
	}
	if hello.Version != Version {
		reason := fmt.Sprintf("engine: this build speaks protocol %d and the surface speaks %d — the two halves have to be the same build", Version, hello.Version)
		if s.host != nil {
			// THE CLAUSE THAT WAS MISSING THE DAY THIS SENTENCE LIED. A host
			// outlives the connection, so the process saying this may be an
			// older codeaf that is still running on a machine whose binary was
			// updated an hour ago — and the sentence above sent the person off
			// to update something that was already updated. When there is a
			// host behind this connection, say the other thing that is true.
			reason += ", and this machine is still running the older one — run codeaf engine --stop here to retire it"
		}
		return s.refuse(reason)
	}
	if supportsEncoding(hello.Encodings, frameEncodingGzip) {
		s.encoding = frameEncodingGzip
	}
	sess, err := s.open(hello)
	if err != nil {
		return s.refuse("engine: " + err.Error())
	}
	// THE AGENT IS READ UNDER THE SESSION'S OWN LOCK. A host hands out sessions
	// that other connections are already driving, and [Session.swap] replaces
	// this field under that lock — so a bare read here is one goroutine reading
	// what another is writing, which is a race whatever the answer turns out to
	// be.
	if sess == nil || sess.current() == nil {
		return s.refuse("engine: the workspace opened no conversation")
	}
	s.session = sess
	// The arrival is one act: attached, welcomed, and caught up, with this
	// connection's writer held throughout so nothing overtakes the welcome.
	s.write.Lock()
	arrived := sess.attach(s, hello)
	s.write.Unlock()
	if arrived != nil {
		// A JOIN REFUSED AT THE DOOR IS A REFUSAL AND NOT A BROKEN PIPE. The
		// sentence goes down the wire the way every other handshake refusal does —
		// it cannot be sent from inside [Session.attach], which runs with this
		// connection's writer held — and the session is let go of first, because
		// this connection never entered the room and [server.leave] must not take
		// it out of one.
		if errors.Is(arrived, ErrJoinedGone) || errors.Is(arrived, errExecutionMode) {
			s.session = nil
			return s.refuse(arrived.Error())
		}
		return arrived
	}
	// AND ONLY THEN IS THE REST OF THE ROOM TOLD who has the keyboard now. It
	// happens outside this connection's writer because telling means writing to
	// the OTHER connections and a surface that had just been handed the welcome
	// would deadlock on its own lock; and it happens after the welcome because
	// an older window learning it is a watcher is news about the window that has
	// arrived, which had better have arrived first.
	sess.tellDriver(s)
	// AND THE WINDOWS THIS ONE WALKED AWAY FROM ARE TOLD THAT IT DID. It is the
	// same ordering and for the same reason as the line above, and it is second
	// because a window learning it has been left should learn it after the room
	// already agrees who is typing (driver.go's [Session.tellMoved]).
	sess.tellMoved(s, hello)
	return nil
}

// refuse says why on the wire and then hands the same sentence back as the
// error, so the exit code and the surface's message are the one fact.
//
// IT IS TYPED BECAUSE THE SENTENCE HAS ALREADY BEEN DELIVERED. Over ssh the
// engine's stderr is the person's stderr — that is how a passphrase prompt
// reaches them (cmd/codeaf/chatv3_host.go) — so a door that also prints this
// error writes the same line onto the same terminal the wire is about to draw
// it on. [Refusal] is how the engine door knows to exit quietly instead.
func (s *server) refuse(reason string) error {
	s.fatal(reason)
	return &Refusal{Reason: reason}
}

// Refusal is a handshake the engine turned away, with the reason ALREADY on the
// wire. A caller that holds one has nothing left to say: the surface has been
// told, in these words, and the only thing still owed is a non-zero exit.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

// Refuse writes one refusal onto a wire nobody has said hello on yet and hands
// back the same [Refusal] a handshake's own refusals do.
//
// IT EXISTS FOR THE ONE REFUSAL THAT COMES BEFORE THERE IS A SERVER. `codeaf
// engine` decides whether it may splice this connection onto a host before it
// reads a byte of stdin (cmd/codeaf's engine.go), and a reason found there has
// the same audience and travels the same road as any other: the surface is
// holding the terminal, it is waiting for a welcome, and a fatal frame is the
// sentence it prints unchanged.
func Refuse(out io.Writer, reason string) error {
	if line, err := json.Marshal(Frame{Kind: "fatal", Error: reason}); err == nil {
		_, _ = out.Write(append(line, '\n'))
	}
	return &Refusal{Reason: reason}
}

// readCall is the frame check every line after the handshake goes through.
func readCall(line []byte) (Frame, error) {
	var frame Frame
	if err := json.Unmarshal(line, &frame); err != nil {
		return frame, errors.New("engine: a frame did not parse as JSON")
	}
	if frame.Kind != "call" {
		return frame, fmt.Errorf("engine: expected a call and got %q", frame.Kind)
	}
	if strings.TrimSpace(frame.Method) == "" {
		return frame, errors.New("engine: a call named no method")
	}
	return frame, nil
}

// dispatch answers one call. EVERY CALL IS ANSWERED, including the ones whose
// method returns nothing: a surface waiting on a result it will never get is a
// surface that has stopped, and "it worked" is a fact worth a line.
func (s *server) dispatch(call Frame) {
	// PRESENCE IS RENEWED BY THE ACT ITSELF. A stalled call tears this
	// pipe under a window that has not gone anywhere; recording the call
	// here — the one funnel every keystroke takes — is what keeps that
	// gap from reading as an empty room.
	s.noteAct()
	payload, err := s.invoke(call)
	result := Frame{Kind: "result", ID: call.ID, Payload: payload}
	if err != nil {
		result.Payload, result.Error = nil, err.Error()
	}
	_ = s.send(result)
	// And only now does a turn opened by that call begin to speak.
	// ONLY A TURN EVER OPENS A STREAM, so only a turn releases one: two calls
	// of any other class running at once would race the one slot, and one of
	// them could steal a stream a turn had just named ([callClass.opensAStream]).
	if classify(call.Method).opensAStream() {
		s.release()
	}
}

// noteAct records that this window just made a call, which is the fact
// [Session.idleSinceLocked] reads as still being watched.
func (s *server) noteAct() {
	sess := s.session
	if sess == nil {
		return
	}
	sess.mu.Lock()
	sess.acted = time.Now()
	if s.name != "" {
		sess.lastWatch = s.name
	}
	sess.mu.Unlock()
}

func (s *server) invoke(call Frame) (out json.RawMessage, err error) {
	defer func() {
		// A panic inside one method is that method's failure and not the
		// engine's death — the conversation behind it is intact and the journal
		// is written. It comes back as the call's error, the stack goes to the
		// log, and the surface says what it would say about any refusal.
		if recovered := recover(); recovered != nil {
			out, err = nil, guard.Note("remote/engine "+call.Method, recovered)
		}
	}()

	sess := s.session
	agent, readRecord, err := sess.serving(s.joined)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, errors.New("engine: no conversation is open")
	}
	// A WATCHER MAY DO THE FEW THINGS ON A LIST AND NOTHING ELSE, and the list is
	// the way round it is for the reason every other guard on this surface is a
	// denial: a door added next year cannot forget an allow-list. The driver rule
	// below covers the doors that TYPE, which is not the same set — switching a
	// model, resolving a card, reviving a node, opening another session and
	// closing this one all change the conversation without a word being said, and
	// every one of them was open to a reader before this ([Hello.Watch]).
	if s.watching && !watcherMay(call.Method) {
		return nil, fmt.Errorf("%s (%s)", watchingWord, call.Method)
	}
	// THE DOORS THAT PUT WORDS INTO THE CONVERSATION ARE THE DRIVER'S, and
	// the check is here rather than in each of them so that a door added later
	// cannot forget it. Everything else — reading the transcript, answering a
	// card, switching a model, interrupting a turn — stays open to every surface
	// in the room: a watcher is a person watching their own work, not a guest.
	switch call.Method {
	case MethodSubmit, MethodFollowUp, MethodSteer, MethodQuestionReplace, MethodSubmitImage, MethodSubmitFiles,
		MethodTaskSteer, MethodTaskStop, MethodTaskRetry:
		if err := s.mayDrive(); err != nil {
			return nil, err
		}
	}

	switch call.Method {
	case MethodTaskRetry:
		args, err := arg[TaskSetupArgs](call)
		if err != nil {
			return nil, err
		}
		want, agreed := steerConversation(s.joined, args.Session)
		if !agreed || want == "" {
			return nil, session.ErrNotThatConversation
		}
		owner, mine := sess.agentOf(want)
		if !mine {
			return nil, session.ErrNotThatConversation
		}
		door, ok := owner.(taskRetryDoor)
		if !ok {
			return nil, errors.New("retrying tasks is unavailable in this engine")
		}
		return nil, door.RetryTask(args.ID)
	case MethodTaskModel, MethodTaskEffort, MethodTaskSetEffort:
		args, err := arg[TaskSetupArgs](call)
		if err != nil {
			return nil, err
		}
		want, agreed := steerConversation(s.joined, args.Session)
		if !agreed || want == "" {
			return nil, session.ErrNotThatConversation
		}
		owner, mine := sess.agentOf(want)
		if !mine {
			return nil, session.ErrNotThatConversation
		}
		door, ok := owner.(taskSetupDoor)
		if !ok {
			return nil, errors.New("task setup is unavailable in this engine; update the engine and reconnect")
		}
		switch call.Method {
		case MethodTaskModel:
			landing, err := door.RetargetTask(args.ID, args.Value)
			if err != nil {
				return nil, err
			}
			// WHEN THE PICK LANDED TRAVELS WITH THE ANSWER, so a hosted room says
			// the same true sentence a local one does rather than guessing.
			return json.Marshal(landing)
		case MethodTaskSetEffort:
			return nil, door.SetTaskEffort(args.ID, args.Value)
		default:
			return json.Marshal(door.TaskEffort(args.ID))
		}
	case MethodTaskRoom:
		args, err := arg[TaskRoomArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ TaskJournal(uint64) string })
		if !ok {
			return nil, errors.New("engine: this session has no task rooms")
		}
		path := door.TaskJournal(args.ID)
		if path == "" {
			return json.Marshal(session.TaskRecord{})
		}
		// The reader is the one that came WITH this agent ([Session.serving]), so
		// the journal path and the reading are the same conversation's.
		if readRecord == nil {
			return nil, errors.New("engine: this engine cannot read its record")
		}
		record, err := readRecord("file://"+path, args.Tail)
		if err != nil {
			return nil, err
		}
		return json.Marshal(record)
	case MethodTaskSteer:
		args, err := arg[TaskSteerArgs](call)
		if err != nil {
			return nil, err
		}
		// THE CONVERSATION IS CHECKED BEFORE THE TASK NUMBER IS USED FOR ANYTHING.
		// The agent it will be delivered to is taken under the same lock the swap
		// replaces it under ([Session.swap]), so a `Session.Open` landing beside
		// this call either happens before it — and this is refused — or after it,
		// and this was delivered to the conversation it named. There is no third
		// ordering, and nothing is ever re-aimed at the task with that number in
		// the conversation that replaced it.
		//
		// AND A BOUND CONNECTION IS HELD TO ITS BINDING. [Session.serving] above
		// checked it, but that was a separate hold of the lock: a swap landing
		// between the two would have left this call to be decided by whatever the
		// caller claimed — and a caller claiming nothing would have been answered
		// by the replacement. Both names are enforced, and neither overrides the
		// other ([steerConversation]).
		want, agreed := steerConversation(s.joined, args.Session)
		if !agreed {
			return json.Marshal(TaskSteered{Elsewhere: true})
		}
		steerAgent, mine := sess.agentOf(want)
		if !mine {
			return json.Marshal(TaskSteered{Elsewhere: true})
		}
		agent = steerAgent
		// THE NAMED SEND FIRST. A surface that gave this crossing an identity is a
		// surface that may ask again for it, and the door that keeps the identity
		// is the only one that can answer the second ask with "already on the
		// record" instead of delivering the correction twice
		// ([session.Agent.SteerTaskFrom]). An engine without it falls through to
		// the two doors below and steers exactly as it always did — the surface
		// was told in its welcome that this was so ([Welcome.SteerRepeat]) and does
		// not ask twice there.
		if door, ok := agent.(steerFromDoor); ok && args.Scope != "" {
			receipt, err := door.SteerTaskFrom(args.ID, args.Text,
				session.SteerSource{Scope: args.Scope, Seq: args.Seq, At: args.Said})
			if err != nil {
				// AN UNKNOWN OUTCOME IS NOT AN ERROR STRING. Carried as one it would
				// arrive as an ordinary refusal and the surface would hand the words
				// back, where the next enter renames them ([TaskSteered.Uncertain]).
				if errors.Is(err, session.ErrSendUnanswered) {
					return json.Marshal(TaskSteered{Uncertain: true})
				}
				return nil, err
			}
			return json.Marshal(steeredOf(receipt))
		}
		// THE RECEIPT DOOR FIRST, AND THE OLDER ONE STILL ANSWERED. An engine that
		// carries the whole receipt says whether the line was HELD against a task
		// whose work is being checked (internal/session's [session.SteerReceipt]);
		// one that predates it can still be steered, and its answer is the delivery
		// it always gave. The capability is asserted rather than required, and its
		// absence is visible in the frame rather than hidden behind a default.
		if door, ok := agent.(interface {
			SteerTask(uint64, string) (session.SteerReceipt, error)
		}); ok {
			receipt, err := door.SteerTask(args.ID, args.Text)
			if err != nil {
				return nil, err
			}
			return json.Marshal(steeredOf(receipt))
		}
		older, ok := agent.(interface {
			SteerTask(uint64, string) (bool, error)
		})
		if !ok {
			return nil, errors.New("engine: this session has no task rooms")
		}
		waiting, err := older.SteerTask(args.ID, args.Text)
		if err != nil {
			return nil, err
		}
		return json.Marshal(TaskSteered{Waiting: waiting, Landing: session.SteerDelivered(waiting)})
	case MethodTaskStop:
		args, err := arg[TaskStopArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ Cancel(string) (string, error) })
		if !ok {
			return nil, errors.New("engine: this session has no door onto stopping work")
		}
		line, err := door.Cancel(args.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(TaskStopped{Line: line})
	case MethodTaskWatch:
		// THE RAIL, SUBSCRIBED. It answers nothing — what it buys is every task
		// update from here on arriving as a "task" frame, including the roster
		// replayed the moment the subscription opens (tasklane.go).
		sess.watchTasks(s)
		return nil, nil
	case MethodDesignWatch:
		// THE HARNESS LANE, SUBSCRIBED. Like the rail's own, it answers nothing:
		// what it buys is every design card, every note around one, and every
		// subharness intake card arriving as a "design" frame from here on
		// (standinglane.go).
		sess.watchLane(s, laneDesign)
		return nil, nil
	case MethodQuestionWatch:
		// THE QUESTIONS LANE, SUBSCRIBED. Like the two above it, it answers
		// nothing: what it buys is every question this conversation raises,
		// withdraws or has answered arriving as a "question" frame from here on,
		// including everything still open replayed the moment the subscription
		// opens (internal/session's [Agent.WatchQuestions]).
		sess.watchLane(s, laneQuestion)
		return nil, nil
	case MethodTitleWatch:
		// THE NAMING LANE, SUBSCRIBED. It answers nothing: what it buys is the
		// name this conversation gives itself arriving as a "title" frame,
		// including the one it is already carrying (standinglane.go).
		sess.watchLane(s, laneTitle)
		return nil, nil
	case MethodQuestionResolve:
		args, err := arg[QuestionArgs](call)
		if err != nil {
			return nil, err
		}
		// THE OPTIONAL-DOOR PATTERN, on the terms the two frames below it keep: an
		// engine that cannot resolve a question loses the ANSWERING and not the
		// connection, and the sentence it refuses with is one a person can read.
		door, ok := agent.(interface {
			ResolveQuestion(session.Answer) error
		})
		if !ok {
			return nil, errors.New("engine: this session cannot answer questions from here")
		}
		return nil, door.ResolveQuestion(args.Answer)
	case MethodQuestionHold:
		args, err := arg[QuestionHoldArgs](call)
		if err != nil {
			return nil, err
		}
		// THE OPTIONAL-DOOR PATTERN AGAIN, and here a missing door costs nothing
		// but the clock: an engine that cannot hold a question still answers it.
		door, ok := agent.(interface {
			HoldQuestion(session.QuestionKind, string)
		})
		if !ok {
			return nil, nil
		}
		door.HoldQuestion(args.Kind, args.Token)
		return nil, nil

	case MethodAutonomy:
		// THE READ SIDE OF THE ROW BELOW. It is asserted rather than called on
		// the concrete agent for the reason every other optional door here is:
		// this server fronts more than one kind of engine.
		//
		// AND AN ENGINE WITH NO DOOR REFUSES RATHER THAN ANSWERING `{}`. An empty
		// map is a real answer — a project that keeps no rules yet — and a
		// surface draws it as every kind on `ask me`. An engine that cannot keep
		// rules at all has not said that, and a page that put `ask me` on every
		// row would be telling a person what happens without them on the strength
		// of a question nobody answered. The refusal is a sentence a person can
		// read, in the grammar of the other optional doors here.
		door, ok := agent.(interface {
			Autonomy() map[session.AskKind]session.Policy
		})
		if !ok {
			return nil, errors.New("engine: this session keeps no question rules")
		}
		return json.Marshal(door.Autonomy())

	case MethodSetAutonomy:
		args, err := arg[AutonomyArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			SetAutonomy(session.AskKind, session.Policy) error
		})
		if !ok {
			return nil, errors.New("engine: this session keeps no settings about what may answer by itself")
		}
		return nil, door.SetAutonomy(args.Kind, args.Policy)
	case MethodSubharnessResolve:
		args, err := arg[SubharnessResolveArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			ResolveSubharness(uint64, bool, json.RawMessage)
		})
		if !ok {
			return nil, errors.New("engine: this session has no saved programs to offer")
		}
		door.ResolveSubharness(args.ID, args.Run, args.Input)
		return nil, nil
	case MethodTaskResolve:
		args, err := arg[TaskResolveArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			ResolveTask(uint64, session.TaskAnswer)
		})
		if !ok {
			return nil, errors.New("engine: this session has no task proposals to answer")
		}
		door.ResolveTask(args.ID, session.TaskAnswer{
			Approved: args.Approved, Redirect: args.Redirect, Model: args.Model,
		})
		return nil, nil
	case MethodTaskSettle:
		args, err := arg[TaskSettleArgs](call)
		if err != nil {
			return nil, err
		}
		return settleTask(agent, args)
	case MethodTaskHold:
		args, err := arg[TaskHoldArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ HoldTask(uint64) })
		if !ok {
			return nil, errors.New("engine: this session has no task proposal clock to hold")
		}
		door.HoldTask(args.ID)
		return nil, nil
	case MethodTaskPending:
		ids, known := sess.pendingTasks()
		if !known {
			return nil, errors.New("engine: this session has no task proposals to be waiting on")
		}
		return json.Marshal(TaskPending{IDs: ids})
	case MethodTaskStart:
		door, ok := agent.(interface {
			StartTask(context.Context, string, bool) (uint64, string, string, error)
		})
		if !ok {
			return nil, errors.New("engine: this session has no task door")
		}
		args, err := arg[TaskStartArgs](call)
		if err != nil {
			return nil, err
		}
		id, title, note, err := door.StartTask(context.Background(), args.Brief, args.Solo)
		if err != nil {
			return nil, err
		}
		return json.Marshal(TaskStarted{ID: id, Title: title, Note: note})
	case MethodPlannerStart:
		door, ok := agent.(interface {
			StartPlannerRun(context.Context, string, string) (string, string, error)
		})
		if !ok {
			return nil, errors.New("engine: this session has no planner door")
		}
		args, err := arg[PlannerStartArgs](call)
		if err != nil {
			return nil, err
		}
		id, title, err := door.StartPlannerRun(context.Background(), args.Brief, args.Hint)
		if err != nil {
			return nil, err
		}
		return json.Marshal(PlannerStarted{ID: id, Title: title})
	case MethodTake:
		// The keyboard comes here, and the room is told in the same breath
		// (driver.go's take).
		return json.Marshal(s.take())
	case MethodPing:
		// The empty answer is the point: elapsed time belongs to the surface's
		// clock, so the engine contributes no timestamp and no machine-clock skew.
		return nil, nil

	case MethodTyping:
		// SOMEBODY IS WRITING, WHICH IS THE CHEAPEST THING THIS ENGINE IS EVER
		// TOLD. It buys a measurement of the machines the next turn will use and
		// a connection already open when that turn goes out; it returns before
		// anything is sent, spends nothing when the speed guard is off, and has
		// its own budget inside (internal/session's lanenews.go).
		//
		// THE RESULT FRAME STILL GOES BACK AND IS DROPPED AT THE OTHER END.
		// [server.dispatch] answers every call it runs, and this road does not
		// know that nobody is waiting — [Client.deliver] drops a result whose
		// id has no waiter, which is the same path a call that reached its
		// deadline takes. It is an empty frame once per [typingHush] and buying
		// a second frame KIND to avoid it would be a wire change for nothing.
		//
		// AN ENGINE WHOSE AGENT CANNOT HEAR IT DOES NOTHING, rather than
		// refusing: a door with nothing behind it is a capability that is ABSENT
		// (the design law), and a task node's agent is deliberately silent here.
		if door, ok := agent.(typingDoor); ok {
			door.Typing()
		}
		return nil, nil

	case MethodSubmit:
		args, err := arg[SubmitArgs](call)
		if err != nil {
			return nil, err
		}
		// A MARKED DRAFT IS THE SAME CALL THROUGH THE OTHER DOOR. What differs
		// is the instruction the engine puts in front of the sentence, which
		// lives on this side of the wire (internal/session's standing_mark.go).
		if args.Standing {
			events, err := agent.SubmitStanding(context.Background(), args.Text)
			return s.stream(MethodSubmit, args.Text, events, err)
		}
		events, err := agent.Submit(context.Background(), args.Text)
		return s.stream(MethodSubmit, args.Text, events, err)

	case MethodQuestionReplace:
		args, err := arg[QuestionArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			ReplaceQuestion(context.Context, session.Answer) (<-chan session.Event, error)
		})
		if !ok {
			return nil, errors.New("engine: this session cannot replace a pending request")
		}
		events, err := door.ReplaceQuestion(context.Background(), args.Answer)
		return s.stream(MethodQuestionReplace, args.Answer.Change, events, err)
	case MethodFollowUp:
		args, err := arg[SubmitArgs](call)
		if err != nil {
			return nil, err
		}
		events, err := agent.FollowUp(args.Text)
		return s.stream(MethodFollowUp, args.Text, events, err)

	case MethodSteer:
		args, err := arg[SubmitArgs](call)
		if err != nil {
			return nil, err
		}
		events, err := agent.Steer(args.Text)
		return s.stream(MethodSteer, args.Text, events, err)

	case MethodSubmitImage:
		args, err := arg[SubmitImageArgs](call)
		if err != nil {
			return nil, err
		}
		images, err := s.store(args.Images)
		if err != nil {
			return nil, err
		}
		events, err := agent.SubmitImage(context.Background(), args.Text, images)
		return s.stream(MethodSubmitImage, args.Text, events, err)

	// The other two doors a person's own files come through, both in file.go:
	// what they attached on the way out, and what they asked for on the way
	// back. They are one line each here because the whole of the difficulty is
	// on the other side of them — where a name is made safe, where the bytes
	// land, and what this conversation is allowed to hand over.
	case MethodSubmitFiles:
		return s.submitFiles(call)

	case MethodFetchFile:
		return s.fetchFile(call)

	// And the two read-only questions a surface may ask about that machine's
	// disk without asking for a single byte of it: what is in this directory,
	// and which of these words name something that is really there. They are in
	// the same file and under the same law, because they are the same boundary.
	case MethodListDir:
		return s.listDir(call)

	case MethodStatPaths:
		return s.statPaths(call)

	// And the one door that WRITES without anybody saying anything: a file
	// dropped on the browse page, kept in this session's attachments and
	// nowhere else. It opens no turn, which is the whole of why it is not
	// [MethodSubmitFiles].
	case MethodDepositFile:
		return s.depositFile(call)

	case MethodStopWork:
		door, ok := agent.(interface{ StopWork() error })
		if !ok {
			return nil, fmt.Errorf("this engine cannot stop all conversation work")
		}
		return nil, door.StopWork()

	case MethodInterrupt:
		// A STOP WITH NO DOOR ON IT IS A PERSON'S OWN, which is what every
		// surface older than this argument means by sending nothing.
		args, err := arg[InterruptArgs](call)
		if err != nil {
			return nil, err
		}
		if door := session.StopDoor(strings.TrimSpace(args.Door)); door != "" && door != session.StopByPerson {
			agent.InterruptFor(door)
			return nil, nil
		}
		agent.Interrupt()
		return nil, nil

	case MethodAnswerLaneOffer:
		// THE ANSWER TO THE ONE QUESTION THE PHASE SEAM RAISES, and it is
		// asserted rather than required of [WrappedAgent] for the task lane's
		// reason: an engine with no transport under it has no offer to answer,
		// and false — "there was nothing to answer" — is the honest word for
		// that as much as for a question that aged out (wire.go's
		// [MethodAnswerLaneOffer]).
		yes, err := arg[bool](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(laneOfferDoor)
		if !ok {
			return mustJSON(false), nil
		}
		return mustJSON(door.AnswerLaneOffer(yes)), nil

	case MethodCompact:
		return nil, agent.Compact(context.Background())

	case MethodClose:
		// The surface said goodbye politely, and it is saying it about the
		// CONVERSATION rather than about this window — so [server.leave] closes
		// the session behind it whoever else is attached. The journal is flushed
		// here rather than at the hang-up that follows, which is the same work
		// done a moment earlier and with somebody still listening for the
		// failure.
		s.goodbye.Store(true)
		return nil, sess.closeLeaving()

	case MethodDetach:
		// THE ONE METHOD WHOSE VALUE IS THE DIFFERENCE BETWEEN IT AND SILENCE.
		// Nothing is interrupted and nothing is closed; the reader loop sees
		// this flag on its next pass and ends the connection, leaving the turn
		// to finish (wire.go's MethodDetach states the whole case).
		s.detached.Store(true)
		return nil, nil

	case MethodHeldQuestions:
		sess.mu.Lock()
		waiting := sess.heldWaitingLocked(s.arrived)
		sess.mu.Unlock()
		return json.Marshal(waiting)

	case MethodModel:
		return json.Marshal(agent.Model())

	case MethodSetModel:
		model, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		if refresh := sess.engine.RefreshModelSources; refresh != nil {
			refresh()
		}
		agent.SetModel(model)
		// AND EVERY SURFACE IS TOLD, not only the one that turned the knob. Two
		// windows on one conversation is a shape this protocol supports
		// ([Welcome.Attached]), and a model changed in one of them is a fact the
		// other one's status line is drawing right now.
		s.session.announce()
		return nil, nil

	case MethodSetContext:
		tokens, err := arg[int](call)
		if err != nil {
			return nil, err
		}
		agent.SetContextWindow(tokens)
		return nil, nil

	case MethodReasoningFor:
		model, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		return json.Marshal(agent.ReasoningFor(model))

	case MethodSetReasoningFor:
		args, err := arg[ReasoningArgs](call)
		if err != nil {
			return nil, err
		}
		agent.SetReasoningFor(args.Model, args.Level)
		s.session.announce()
		return nil, nil

	case MethodAttachSkills, MethodDetachSkill, MethodAttachedSkills, MethodClearSkills, MethodSkillShelf:
		payload, err := serveSkills(agent, call)
		// A door that moved the attachment is a fact every window's chip is
		// drawing, so every surface is told, not only the one that asked.
		if err == nil && call.Method != MethodAttachedSkills && call.Method != MethodSkillShelf {
			s.session.announce()
		}
		return payload, err

	case MethodEffort, MethodResolvedEffort, MethodSetEffort:
		door, ok := agent.(effortDoor)
		if !ok {
			// A surface reading [Welcome.Effort] never gets here, and one that
			// asked anyway is told the fact rather than left with a zero value it
			// would draw as a rung of its own (effort.go).
			return nil, errors.New("engine: this conversation has no thinking dial; update the engine and reconnect")
		}
		if call.Method == MethodEffort {
			return json.Marshal(door.ConversationEffort())
		}
		if call.Method == MethodResolvedEffort {
			return json.Marshal(door.ResolvedEffort())
		}
		rung, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		took := door.SetConversationEffort(rung)
		// AND EVERY SURFACE IS TOLD, on [MethodSetModel]'s terms: the rung rides
		// the fact set every window on this conversation draws from, and a dial
		// moved in one of them is a cell the others are painting right now.
		if took {
			s.session.announce()
		}
		return json.Marshal(took)

	case MethodResolvedApproval, MethodSetApproval:
		door, ok := agent.(approvalDoor)
		if !ok || !door.ApprovalDial() {
			// A surface reading [Welcome.Approval] never gets here, and one that
			// asked anyway is told the fact (approval.go).
			return nil, errors.New("engine: this conversation has no dial onto what runs without asking; update the engine and reconnect")
		}
		if call.Method == MethodResolvedApproval {
			return json.Marshal(door.ResolvedApprovalPosture())
		}
		posture, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		refusal := ""
		if err := door.SetApprovalPosture(posture); err != nil {
			refusal = err.Error()
		} else {
			// AND EVERY SURFACE IS TOLD, on [MethodSetEffort]'s terms: the posture
			// rides the fact set every window on this conversation draws from.
			s.session.announce()
		}
		return json.Marshal(refusal)

	case MethodConsent:
		args, err := arg[ConsentArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveConsent(args.ID, args.Allow)
		return nil, nil

	case MethodConsentRemember:
		args, err := arg[ConsentArgs](call)
		if err != nil {
			return nil, err
		}
		if args.Scope == session.ConsentRule {
			// THE RULE MUST STAND BEFORE THE CALL IS RELEASED. A rule-scoped
			// answer deliberately leaves no session-wide memo, because the
			// surface has already written the narrower rule. Re-read that row
			// before delivery so the next matching call sees it.
			if refresh := sess.engine.RefreshApprovals; refresh != nil {
				refresh()
			}
		}
		agent.ResolveConsentRemember(args.ID, args.Allow, args.Scope)
		return nil, nil

	case MethodStandingResolve:
		args, err := arg[StandingArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveStanding(args.ID, args.Answer)
		return nil, nil

	case MethodHarness:
		args, err := arg[HarnessArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveHarness(args.ID, args.Run, args.Model)
		return nil, nil

	case MethodConnect:
		args, err := arg[ConnectArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveConnect(args.ID, args.Approve)
		return nil, nil

	case MethodConnectKey:
		args, err := arg[ConnectArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveConnectKey(args.ID, args.Key)
		return nil, nil

	case MethodNoteConnected:
		args, err := arg[ConnectedArgs](call)
		if err != nil {
			return nil, err
		}
		agent.NoteConnected(args.Service, args.Account)
		return nil, nil

	case MethodTitle:
		return json.Marshal(agent.Title())

	case MethodUsage:
		return json.Marshal(agent.Usage())

	case MethodContextTokens:
		return json.Marshal(agent.ContextTokens())

	case MethodObserve:
		return s.observe(agent, call)
	case MethodUnobserve:
		args, err := arg[observeArgs](call)
		if err != nil {
			return nil, err
		}
		s.dropObserver(args.ID)
		return json.Marshal(struct{}{})

	case MethodTranscript:
		return json.Marshal(agent.Transcript())

	case MethodEarlier:
		return json.Marshal(agent.EarlierHistory())

	case MethodRewindPoints:
		return json.Marshal(agent.RewindPoints())

	case MethodRewindAt:
		index, err := arg[int](call)
		if err != nil {
			return nil, err
		}
		dropped, err := agent.RewindAt(index)
		if err != nil {
			return nil, err
		}
		return json.Marshal(dropped)

	case MethodPlanTasks:
		door, ok := agent.(interface{ PlanTasks() []session.PlanTaskRow })
		if !ok {
			return json.Marshal([]session.PlanTaskRow(nil))
		}
		return json.Marshal(door.PlanTasks())

	case MethodPlanTaskPage:
		args, err := arg[PlanTaskPageArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			PlanTaskPage(string) (session.PlanTaskPage, bool)
		})
		if !ok {
			return json.Marshal(PlanTaskPageResult{})
		}
		page, found := door.PlanTaskPage(args.ID)
		return json.Marshal(PlanTaskPageResult{Page: page, OK: found})

	case MethodPlanTaskWork:
		args, err := arg[PlanTaskArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			PlanTaskWork(string) (session.PlanTaskWork, bool)
		})
		if !ok {
			return json.Marshal(PlanTaskWorkResult{})
		}
		work, found := door.PlanTaskWork(args.ID)
		return json.Marshal(PlanTaskWorkResult{Work: work, OK: found})

	case MethodPlanNote:
		args, err := arg[PlanTextArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ PlanNote(string, string) error })
		if !ok {
			return nil, errors.New("engine: this engine cannot note its plan")
		}
		return nil, door.PlanNote(args.ID, args.Text)

	case MethodPlanPause:
		args, err := arg[PlanTaskArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ PlanPause(string) error })
		if !ok {
			return nil, errors.New("engine: this engine cannot pause its plan")
		}
		return nil, door.PlanPause(args.ID)

	case MethodPlanResume:
		args, err := arg[PlanTaskArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ PlanResume(string) error })
		if !ok {
			return nil, errors.New("engine: this engine cannot resume its plan")
		}
		return nil, door.PlanResume(args.ID)

	case MethodPlanCancel:
		args, err := arg[PlanTaskArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ PlanCancel(string) error })
		if !ok {
			return nil, errors.New("engine: this engine cannot cancel its plan")
		}
		return nil, door.PlanCancel(args.ID)

	case MethodPlanAmend:
		args, err := arg[PlanTextArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ PlanAmend(string, string) error })
		if !ok {
			return nil, errors.New("engine: this engine cannot amend its plan")
		}
		return nil, door.PlanAmend(args.ID, args.Text)

	case MethodPlanPriority:
		args, err := arg[PlanPriorityArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface{ PlanPriority(string, int) error })
		if !ok {
			return nil, errors.New("engine: this engine cannot prioritize its plan")
		}
		return nil, door.PlanPriority(args.ID, args.Priority)

	case MethodPlanRunSummary:
		args, err := arg[PlanRunSummaryArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			PlanRunSummary(string) (session.RunPlanSummary, bool)
		})
		if !ok {
			return json.Marshal(PlanRunSummaryResult{})
		}
		summary, found := door.PlanRunSummary(args.RootID)
		return json.Marshal(PlanRunSummaryResult{Summary: summary, OK: found})

	case MethodRefreshRunSummary:
		args, err := arg[RefreshRunSummaryArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			RefreshRunSummary(context.Context, string, time.Time) (session.RunPlanSummary, bool)
		})
		if !ok {
			return json.Marshal(PlanRunSummaryResult{})
		}
		ctx := context.Background()
		cancel := func() {}
		if args.Budget > 0 {
			ctx, cancel = context.WithTimeout(ctx, args.Budget)
		}
		defer cancel()
		summary, found := door.RefreshRunSummary(ctx, args.RootID, args.LastLook)
		return json.Marshal(PlanRunSummaryResult{Summary: summary, OK: found})

	case MethodPlanSpend:
		// THE READ SIDE OF THE RUN'S SPEND-BY-SEAT, carried across the way
		// [MethodRewindPoints] is. It is ASSERTED rather than called on the
		// concrete agent for the reason every other optional door here is: this
		// server fronts more than one kind of engine, and a scripted one may have
		// no plan store.
		//
		// AND AN ENGINE WITH NO DOOR ANSWERS AN EMPTY ROLLUP, NOT AN ERROR. A
		// conversation that seeded no plan has no workers and no seats, and the
		// spend page draws its seat block from the lines it is handed — an empty
		// slice is the page saying nothing, which is the honest reading and the
		// one the emptiness law draws (the same answer a nil store gives
		// [session.Agent.PlanSpend] itself).
		args, err := arg[PlanSpendArgs](call)
		if err != nil {
			return nil, err
		}
		door, ok := agent.(interface {
			PlanSpend(time.Time) []session.PlanSpendLine
		})
		if !ok {
			return json.Marshal([]session.PlanSpendLine(nil))
		}
		return json.Marshal(door.PlanSpend(args.Since))

	case MethodSessionsRecent:
		sess.mu.Lock()
		recent := sess.engine.Recent
		sess.mu.Unlock()
		if recent == nil {
			return nil, errors.New("engine: this engine cannot list sessions")
		}
		return json.Marshal(recent())

	case MethodPlacesWorld:
		sess.mu.Lock()
		world := sess.engine.World
		sess.mu.Unlock()
		if world == nil {
			// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT EMPTY. An empty world
			// answered here would reach the surface as a machine with no projects
			// on it, which is a claim; the refusal reaches it as no answer at all,
			// and the emptiness law draws that as nothing.
			return nil, errors.New("engine: this engine cannot list its places")
		}
		return json.Marshal(world())

	case MethodPlacesTask:
		args, err := arg[PlacesTaskArgs](call)
		if err != nil {
			return nil, err
		}
		sess.mu.Lock()
		read := sess.engine.TaskRecord
		sess.mu.Unlock()
		if read == nil {
			// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT EMPTY, exactly as the
			// world door above. An empty record answered here would reach the card
			// as a piece of work that said nothing at the end, which is a claim.
			return nil, errors.New("engine: this engine cannot read its record")
		}
		record, err := read(args.Transcript, args.Tail)
		if err != nil {
			return nil, err
		}
		return json.Marshal(record)

	case MethodStandingItems:
		workspace, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		sess.mu.Lock()
		items := sess.engine.StandingItems
		sess.mu.Unlock()
		if items == nil {
			return nil, errors.New("engine: this engine keeps an eye on nothing")
		}
		found, err := items(workspace)
		if err != nil {
			return nil, err
		}
		return json.Marshal(found)

	case MethodStandingSave:
		item, err := arg[standing.Item](call)
		if err != nil {
			return nil, err
		}
		sess.mu.Lock()
		save := sess.engine.StandingSave
		sess.mu.Unlock()
		if save == nil {
			return nil, errors.New("engine: this engine keeps an eye on nothing")
		}
		return nil, save(item)

	case MethodStandingWatch:
		sess.mu.Lock()
		watch := sess.engine.StandingWatch
		sess.mu.Unlock()
		if watch == nil {
			return nil, errors.New("engine: this engine cannot read background checks")
		}
		status, known := watch()
		return json.Marshal(StandingWatchResult{Status: status, Known: known})

	case MethodSessionNew:
		sess.mu.Lock()
		fresh := sess.engine.Fresh
		sess.mu.Unlock()
		if fresh == nil {
			return nil, errors.New("engine: this engine cannot start a new session")
		}
		return sess.swap(s, func() (WrappedAgent, string, bool, error) {
			next, file, err := fresh()
			return next, file, false, err
		})

	case MethodSessionOpen:
		file, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		sess.mu.Lock()
		open := sess.engine.Open
		sess.mu.Unlock()
		if open == nil {
			return nil, errors.New("engine: this engine cannot open another session")
		}
		return sess.swap(s, func() (WrappedAgent, string, bool, error) {
			next, resumed, err := open(file)
			return next, file, resumed, err
		})
	}
	// AND THE THREE PLACES THAT LEARNED TO CROSS LATER ARE ASKED HERE, in a file
	// of their own, ahead of the refusal. They are additive to version 4 and the
	// refusal below is what an engine WITHOUT them answers, which is the whole
	// bargain: a surface that meets it says the sentence its place has always
	// said rather than waiting on a call nobody is going to answer
	// (wire_places.go states the law).
	if payload, handled, err := s.placesCall(call); handled {
		return payload, err
	}
	return nil, fmt.Errorf("engine: no such method %q", call.Method)
}

// dropSettledLocked reconciles the waiting room against the engine's own list of
// what is still open, and it is the ONE thing that empties that room.
//
// IT REPLACED A LINE IN EVERY RESOLVE-DOOR. Each door on this wire used to take
// its own card down, which held for exactly as long as every lane had a door of
// its own: the day the surface started answering every lane through
// [MethodQuestionResolve], no door dropped anything and an answered standing
// card came back on every attach (held.go's [heldSet.keepOnly] tells the whole
// story). A question's life is stated in one place now, so this asks THAT.
//
// THE OPTIONAL-DOOR PATTERN, as everything else in this file does it: an engine
// that cannot list its open questions keeps whatever it was holding rather than
// losing it, which is the safe half of the mistake.
//
// The caller holds sess.mu, and this reaches into the agent under it — the same
// hold [Session.welcomeLocked] already takes to ask it for its model and title.
func (sess *Session) dropSettledLocked() {
	if sess.agent == nil {
		return
	}
	door, ok := sess.agent.(interface {
		OpenQuestions() []session.Question
	})
	if !ok {
		return
	}
	open := make(map[heldKey]bool, 4)
	for _, question := range door.OpenQuestions() {
		if key, is := heldKeyOfQuestion(question); is {
			open[key] = true
		}
	}
	sess.held.keepOnly(open)
}

// heldOutstandingLocked is how many questions are really unanswered — the room
// reconciled first, so the number cannot outlive the questions it counts. It is
// what the idle policy reads: a card nobody has answered is a turn that has
// stopped, and a card that answered one is a persistent engine kept alive
// forever. The caller holds sess.mu.
func (sess *Session) heldOutstandingLocked() int {
	sess.dropSettledLocked()
	return sess.held.outstanding()
}

// heldWaitingLocked is what this surface has not been sent yet, AFTER the room
// has been reconciled — so a welcome and a MethodHeldQuestions call can never
// hand over a question the engine already settled. The caller holds sess.mu.
func (sess *Session) heldWaitingLocked(arrived uint64) []HeldQuestion {
	sess.dropSettledLocked()
	return sess.held.waitingFor(arrived)
}

// arg decodes a call's payload. An absent payload decodes as the zero value,
// which is what a method whose argument is optional wants and what a method
// that needed one will refuse for itself.
func arg[T any](call Frame) (T, error) {
	var value T
	if len(call.Payload) == 0 {
		return value, nil
	}
	if err := json.Unmarshal(call.Payload, &value); err != nil {
		return value, fmt.Errorf("engine: %s: %w", call.Method, err)
	}
	return value, nil
}

// stream turns a submitted turn into a stream id. The id is answered
// IMMEDIATELY, before a single event, because the surface has to be able to bind
// events to the turn that caused them and because a result that waited for the
// turn would hold the reader shut for the length of the work.
//
// IT DOES NOT START THE PUMP. The pump is left waiting on the call frame's own
// result (see [server.dispatch]), because a stream whose first event overtook
// the result naming it would be events about a stream the surface has never
// heard of — the one ordering this protocol cannot recover from, and a race that
// would show up as a lost first token on a fast turn and never in a test.
func (s *server) stream(method, said string, events <-chan session.Event, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	if events == nil {
		// A turn that opened no channel is a turn that is already over. It gets
		// a stream anyway, so the surface's bookkeeping ends the way it ends for
		// every other turn: one close, on an id it was given.
		empty := make(chan session.Event)
		close(empty)
		events = empty
	}
	id, generation := s.session.mint()
	s.pending = &pending{id: id, generation: generation, method: method, said: said, events: events}
	return json.Marshal(StreamRef{Stream: id})
}

// pending is a stream that has been named and not yet started.
type pending struct {
	// A view subscription starts after its result without announcing a turn.
	start func()

	id         uint64
	generation uint64
	method     string
	// said is the sentence that opened this turn, carried so the rest of the
	// room can draw it above the reply ([Turn.Said]).
	said   string
	events <-chan session.Event
}

// release starts whatever the call just opened. It runs on the ordered lane,
// after the result is on the wire, and it starts the pump even when
// that write failed: the channel has a session writing into it, and a channel
// nobody drains is a turn that never finishes.
//
// The pump belongs to the SESSION and not to this connection, which is what
// lets the turn outlive the surface that asked for it.
func (s *server) release() {
	waiting := s.pending
	s.pending = nil
	if waiting == nil {
		return
	}
	if waiting.start != nil {
		waiting.start()
		return
	}
	sess := s.session
	// THE ROOM IS TOLD BEFORE THE FIRST EVENT OF IT MOVES. Every other surface
	// is about to receive this turn's events and would otherwise have nowhere to
	// put them, because a surface draws the streams it knows about ([Turn]).
	// A STEER IS WORDS INSIDE THE TURN EVERYBODY ALREADY HAS. Broadcasting it
	// as a fresh turn makes a watcher draw a second user message and adopt the
	// same stream twice; its Accepted and Consumed events are the whole account.
	if waiting.method != MethodSteer {
		sess.tellTurn(Turn{Stream: waiting.id, Said: waiting.said}, s)
	}
	sess.pumps.Add(1)
	go sess.pump(waiting.id, waiting.generation, waiting.events)
}

// ── the pipe ────────────────────────────────────────────────────────────────

func (s *server) send(frame Frame) error {
	s.write.Lock()
	defer s.write.Unlock()
	return s.sendLocked(frame)
}

// sendLocked writes one frame with this connection's writer already held. It is
// what an arrival uses ([Session.attach]) so that the welcome and the replay
// behind it cannot be overtaken by a live event.
func (s *server) sendLocked(frame Frame) error {
	if frame.Kind != "welcome" {
		if err := compressFrame(&frame, s.encoding); err != nil {
			return err
		}
	}
	line, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if s.dead {
		return io.ErrClosedPipe
	}
	if _, err := s.out.Write(line); err != nil {
		s.dead = true
		return err
	}
	return flushFrame(s.out)
}

func (s *server) hungUp() bool {
	s.write.Lock()
	defer s.write.Unlock()
	return s.dead
}

// fatal is the last frame this engine will send. It is best-effort by
// construction: the usual reason to send one is that the far end has stopped
// making sense, and a surface that cannot read this is a surface that will see
// the pipe close instead.
func (s *server) fatal(reason string) {
	_ = s.send(Frame{Kind: "fatal", Error: reason})
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
