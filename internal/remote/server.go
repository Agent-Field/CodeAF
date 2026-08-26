package remote

// server.go is the ENGINE half: the side that holds a real conversation and
// answers frames about it. It is what `aforge engine` runs after it has changed
// into the workspace and assembled an agent exactly the way `aforge chat` does.
//
// The shape is one reader and one writer, and everything else follows from it.
// Calls arrive in the order the surface made them and are answered in that same
// order, because a surface that sets a model and then submits a message means
// those two things in that order and nothing here may reorder them. The one
// exception is a turn's events, which arrive on a channel the session owns and
// are pumped by a goroutine of their own — that is the whole reason Submit
// answers with a stream id instead of a transcript.
//
// ORDER IS WORTH MORE THAN OVERLAP, and the one call that pays for it is
// Compact: it is the only method that does the work itself rather than starting
// it, so a compaction pass holds the reader for as long as the summarizer takes
// and the calls behind it wait. Handing it a goroutine would buy a live status
// line during a compaction and cost the guarantee that a /model followed by a
// message is a message on the new model — a bad trade, and a bug nobody would
// reproduce twice.
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
//     closed with the connection when nobody is (a bare `aforge engine`).
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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
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
	Interrupt()
	Compact(ctx context.Context) error
	Close() error
	Model() string
	SetModel(model string)
	SetContextWindow(tokens int)
	ReasoningFor(model string) string
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
// that builds it (cmd/aforge) owns config resolution, session-file resolution
// and the locked-file fallback; this package owns nothing about how an agent is
// made and everything about how one is spoken to.
type Engine struct {
	// Agent is the conversation the surface starts on. Required.
	Agent WrappedAgent
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

	// ApprovalMode is this machine's own answer to "does a tool run without
	// asking", read once at boot off the profile the boot closure resolved
	// against, and carried unchanged through a session swap (the profile
	// belongs to the machine, not to the file that happens to be open). It is
	// what turns the YOLO badge honest again over --host (host.go's approvalPosture,
	// tui3.go's ApprovalMode option).
	ApprovalMode string

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

	// ── the places ──────────────────────────────────────────────────────────
	//
	// THE PLACES ARE A LISTING OF THIS MACHINE'S DISK, and until version 4 the
	// surface listed its own instead. Over --host that meant the tasks place
	// walked the LAPTOP's `~/.aforge/v3` and drew what it found — eight rows and
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
	// PlacesRoot is the directory World walked, carried on the welcome so the
	// surface can put THIS conversation back into a walk taken before it existed
	// ([Welcome.PlacesRoot] holds the argument). Empty says nothing about the
	// world door; a build that answers a world and no root simply cannot adopt.
	PlacesRoot string
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
// says: a bare `aforge engine` with no host behind it is a legitimate version-2
// engine, it simply ends when the connection does. The welcome says so
// ([Welcome.Persistent]) so that no surface promises a lifetime this shape does
// not have.
func Serve(in io.Reader, out io.Writer, opts Options) error {
	return ServeAttach(in, out, AttachOptions{
		Open: func(hello Hello) (*Session, error) {
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
	s := &server{out: out, open: opts.Open}
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
	// calls arrive from each independently and are serialized at the agent by
	// the same one-call-at-a-time reader every connection has.
	surfaces map[*server]struct{}
	pumps    sync.WaitGroup

	// closed is the conversation deliberately ended — [MethodClose], or the
	// pipe dying on an engine whose life this pipe was.
	closed bool
	// empty is when the last surface left, and zero while somebody is here. It
	// is what an idle policy measures (internal/enginehost).
	empty time.Time
}

// NewSession wraps an opened engine as a conversation. Persistent says the
// engine outlives its connections, which is the fact [Welcome.Persistent]
// carries and the fact a torn pipe is read against.
func NewSession(engine *Engine, persistent bool) *Session {
	if engine == nil {
		return nil
	}
	return &Session{
		engine:     engine,
		agent:      engine.Agent,
		persistent: persistent,
		rings:      map[uint64]*ring{},
		held:       newHeldSet(),
		surfaces:   map[*server]struct{}{},
		empty:      time.Now(),
	}
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
func (sess *Session) Ended() bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.closed
}

// IdleSince is when this conversation went quiet, and the zero time when it has
// not: a surface is attached, a turn is still running, or a question is waiting
// for somebody to come back and answer it.
//
// A TURN WITH NOBODY WATCHING IS NOT IDLE, which is the entire point of the
// persistent engine — the long refactor asked for from a café keeps the session
// alive while it runs, and a held card keeps it alive until it is answered.
func (sess *Session) IdleSince() time.Time {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if len(sess.surfaces) > 0 || len(sess.rings) > 0 || len(sess.held.waiting()) > 0 {
		return time.Time{}
	}
	return sess.empty
}

// Close ends the conversation: the turn in flight stops where it is and keeps
// its partial reply, and the agent is closed, which flushes the journal.
//
// THE JOURNAL IS THE SAFETY. An engine is the only writer of its session file
// and no surface holds anything that is not in it, so the whole of "did this
// conversation survive" is whether this ran. It is idempotent because every
// road out of a connection may call it and a host may call it again on the way
// down.
func (sess *Session) Close() error {
	sess.mu.Lock()
	agent, already := sess.agent, sess.closed
	sess.closed = true
	sess.mu.Unlock()
	if agent == nil || already {
		return nil
	}
	agent.Interrupt()
	return agent.Close()
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
func (sess *Session) attach(s *server, hello Hello) error {
	sess.mu.Lock()
	// Attached counts the OTHERS, so it is read before this one is added
	// (wire.go's Welcome.Attached states why the number is carried at all).
	welcome := sess.welcomeLocked()
	welcome.Attached = len(sess.surfaces)
	sess.surfaces[s] = struct{}{}
	sess.empty = time.Time{}
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
	return nil
}

// detach takes one connection out of the room, and — when it was the last one —
// starts the clock an idle policy reads and turns every unanswered card into a
// question nobody is looking at.
func (sess *Session) detach(s *server) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	delete(sess.surfaces, s)
	if len(sess.surfaces) == 0 {
		sess.empty = time.Now()
		sess.held.roomEmptied()
	}
}

func (sess *Session) welcomeLocked() Welcome {
	return Welcome{
		Version:      Version,
		Workspace:    sess.engine.Workspace,
		SessionFile:  sess.engine.SessionFile,
		Resumed:      sess.engine.Resumed,
		Model:        sess.agent.Model(),
		Title:        sess.agent.Title(),
		Note:         sess.engine.Note,
		ApprovalMode: sess.engine.ApprovalMode,
		PlacesRoot:   sess.engine.PlacesRoot,
		Live:         sess.liveLocked(),
		Held:         sess.held.waiting(),
		Persistent:   sess.persistent,
	}
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
	// A QUESTION RAISED INTO AN EMPTY ROOM WAITS INSTEAD OF EXPIRING, which is
	// what [heldSet] is for; a question raised while somebody is watching is on
	// their screen and only becomes a waiting one if they leave without
	// answering it.
	sess.held.raise(wire, id, len(sess.surfaces) == 0)
	watching := sess.watchingLocked()
	sess.mu.Unlock()

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
func (sess *Session) swap(build func() (WrappedAgent, string, bool, error)) (json.RawMessage, error) {
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
	welcome := sess.welcomeLocked()
	welcome.Attached = len(sess.surfaces) - 1
	if welcome.Attached < 0 {
		welcome.Attached = 0
	}
	sess.mu.Unlock()

	if previous != nil {
		previous.Interrupt()
		_ = previous.Close()
	}
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

	open    func(Hello) (*Session, error)
	session *Session

	// pending is the stream a call has just opened and dispatch has not yet let
	// speak. It is one slot rather than a queue because one reader makes one
	// call at a time.
	pending *pending

	// leaving records how this connection ends, and it is the whole of version
	// 2's three-roads-out. detached is [MethodDetach] — the surface is going and
	// the turn is the engine's to finish. goodbye is [MethodClose] — the
	// conversation itself is over. Neither set, and a reader that stops, is a
	// TORN PIPE, whose meaning depends on whether anything is holding the
	// conversation (see the bottom of [server.serve]).
	detached bool
	goodbye  bool
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

	for scan.Scan() {
		frame, err := readCall(scan.Bytes())
		if err != nil {
			s.fatal(err.Error())
			return err
		}
		s.dispatch(frame)
		if s.detached {
			// THE SURFACE SAID IT WAS GOING, and said so before it went, which
			// is the fact version 1 could not express. The turn keeps running;
			// this connection is simply over.
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
//     and that is still the honest reading. A bare `aforge engine` on a pipe
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
	sess.detach(s)
	if s.goodbye || !sess.persistent {
		_ = sess.Close()
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
	if frame.Kind != "hello" {
		return s.refuse(fmt.Sprintf("engine: the first frame was %q, not a hello", frame.Kind))
	}
	var hello Hello
	if err := json.Unmarshal(frame.Payload, &hello); err != nil {
		return s.refuse("engine: the hello did not parse")
	}
	if hello.Version != Version {
		return s.refuse(fmt.Sprintf("engine: this build speaks protocol %d and the surface speaks %d — the two halves have to be the same build", Version, hello.Version))
	}
	sess, err := s.open(hello)
	if err != nil {
		return s.refuse("engine: " + err.Error())
	}
	if sess == nil || sess.agent == nil {
		return s.refuse("engine: the workspace opened no conversation")
	}
	s.session = sess
	// The arrival is one act: attached, welcomed, and caught up, with this
	// connection's writer held throughout so nothing overtakes the welcome.
	s.write.Lock()
	defer s.write.Unlock()
	return sess.attach(s, hello)
}

// refuse says why on the wire and then hands the same sentence back as the
// error, so the exit code and the surface's message are the one fact.
//
// IT IS TYPED BECAUSE THE SENTENCE HAS ALREADY BEEN DELIVERED. Over ssh the
// engine's stderr is the person's stderr — that is how a passphrase prompt
// reaches them (cmd/aforge/chatv3_host.go) — so a door that also prints this
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
	payload, err := s.invoke(call)
	result := Frame{Kind: "result", ID: call.ID, Payload: payload}
	if err != nil {
		result.Payload, result.Error = nil, err.Error()
	}
	_ = s.send(result)
	// And only now does a turn opened by that call begin to speak.
	s.release()
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
	agent := sess.current()
	if agent == nil {
		return nil, errors.New("engine: no conversation is open")
	}
	switch call.Method {
	case MethodSubmit:
		args, err := arg[SubmitArgs](call)
		if err != nil {
			return nil, err
		}
		// A MARKED DRAFT IS THE SAME CALL THROUGH THE OTHER DOOR. What differs
		// is the instruction the engine puts in front of the sentence, which
		// lives on this side of the wire (internal/session's standing_mark.go).
		if args.Standing {
			return s.stream(agent.SubmitStanding(context.Background(), args.Text))
		}
		return s.stream(agent.Submit(context.Background(), args.Text))

	case MethodFollowUp:
		args, err := arg[SubmitArgs](call)
		if err != nil {
			return nil, err
		}
		return s.stream(agent.FollowUp(args.Text))

	case MethodSubmitImage:
		args, err := arg[SubmitImageArgs](call)
		if err != nil {
			return nil, err
		}
		images, err := s.store(args.Images)
		if err != nil {
			return nil, err
		}
		return s.stream(agent.SubmitImage(context.Background(), args.Text, images))

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

	case MethodInterrupt:
		agent.Interrupt()
		return nil, nil

	case MethodCompact:
		return nil, agent.Compact(context.Background())

	case MethodClose:
		// The surface said goodbye politely, and it is saying it about the
		// CONVERSATION rather than about this window — so [server.leave] closes
		// the session behind it whoever else is attached. The journal is flushed
		// here rather than at the hang-up that follows, which is the same work
		// done a moment earlier and with somebody still listening for the
		// failure.
		s.goodbye = true
		return nil, agent.Close()

	case MethodDetach:
		// THE ONE METHOD WHOSE VALUE IS THE DIFFERENCE BETWEEN IT AND SILENCE.
		// Nothing is interrupted and nothing is closed; the reader loop sees
		// this flag on its next pass and ends the connection, leaving the turn
		// to finish (wire.go's MethodDetach states the whole case).
		s.detached = true
		return nil, nil

	case MethodHeldQuestions:
		sess.mu.Lock()
		waiting := sess.held.waiting()
		sess.mu.Unlock()
		return json.Marshal(waiting)

	case MethodModel:
		return json.Marshal(agent.Model())

	case MethodSetModel:
		model, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		agent.SetModel(model)
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
		return nil, nil

	case MethodConsent:
		args, err := arg[ConsentArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveConsent(args.ID, args.Allow)
		s.answered(HeldConsent, args.ID, "")
		return nil, nil

	case MethodConsentRemember:
		args, err := arg[ConsentArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveConsentRemember(args.ID, args.Allow, args.Scope)
		s.answered(HeldConsent, args.ID, "")
		return nil, nil

	case MethodStandingResolve:
		args, err := arg[StandingArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveStanding(args.ID, args.Answer)
		s.answered(HeldStanding, args.ID, "")
		return nil, nil

	case MethodHarness:
		args, err := arg[HarnessArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveHarness(args.ID, args.Run, args.Model)
		s.answered(HeldHarness, args.ID, "")
		return nil, nil

	case MethodConnect:
		args, err := arg[ConnectArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveConnect(args.ID, args.Approve)
		s.answered(HeldConnect, 0, args.ID)
		return nil, nil

	case MethodConnectKey:
		args, err := arg[ConnectArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveConnectKey(args.ID, args.Key)
		s.answered(HeldConnect, 0, args.ID)
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

	case MethodSessionNew:
		sess.mu.Lock()
		fresh := sess.engine.Fresh
		sess.mu.Unlock()
		if fresh == nil {
			return nil, errors.New("engine: this engine cannot start a new session")
		}
		return sess.swap(func() (WrappedAgent, string, bool, error) {
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
		return sess.swap(func() (WrappedAgent, string, bool, error) {
			next, resumed, err := open(file)
			return next, file, resumed, err
		})
	}
	return nil, fmt.Errorf("engine: no such method %q", call.Method)
}

// answered drops a held question because its resolve-door was just called. It
// runs whether or not that question was ever held: the set is keyed, so
// answering something nobody was holding is a lookup that finds nothing.
func (s *server) answered(kind string, id uint64, text string) {
	sess := s.session
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.held.answered(kind, id, text)
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
func (s *server) stream(events <-chan session.Event, err error) (json.RawMessage, error) {
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
	s.pending = &pending{id: id, generation: generation, events: events}
	return json.Marshal(StreamRef{Stream: id})
}

// pending is a stream that has been named and not yet started.
type pending struct {
	id         uint64
	generation uint64
	events     <-chan session.Event
}

// release starts whatever the call just opened. It runs on the reader
// goroutine, after the result is on the wire, and it starts the pump even when
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
	sess := s.session
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
	return nil
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
