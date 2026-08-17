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

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/session"
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
	ResolveHarness(id uint64, run bool, model string)
	ResolveConnect(id string, approve bool)
	ResolveConnectKey(id string, key string)
	NoteConnected(service, account string)
	Title() string
	Usage() session.Usage
	ContextTokens() int
	Transcript() []session.DisplayEntry
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
	// 26) and ArtifactsIndex the deliverables index beside it. They are here for
	// one reason: a picture arrives on this wire as bytes and has to be written
	// down before it can be journaled (image.go), and where it lands is a
	// question about the engine's disk that only the engine's own session folder
	// can answer. The zero Place is the legacy layout, and an empty index
	// records nothing.
	//
	// STUB(place/layout): cmd/aforge's --host door sets both from the same
	// launch assembly that fills session.Config.Place and
	// session.Config.ArtifactsIndex.
	Place          session.Place
	ArtifactsIndex string
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
}

// Options is what [Serve] needs, which is one function: how to open the
// conversation the hello asked for. The engine cannot assemble it before the
// handshake, because the workspace and the session file are things the surface
// says.
type Options struct {
	Boot func(Hello) (*Engine, error)
}

// frameCap is the most one line may weigh. It is the journal reader's bargain
// (internal/tui3's readJournalLines) at a far larger figure, and for a reason
// that is this protocol's own: AN IMAGE UPLOAD RIDES ONE LINE. A ten-megabyte
// photo is base64 in a JSON string by the time it reaches here, so the ceiling
// has to clear a whole message's worth of pictures with room to spare. It is a
// cap on ONE LINE, never on the conversation.
const frameCap = 64 << 20

// Serve runs the engine loop until the pipe dies or the protocol does. It
// returns nil for an ordinary hang-up and an error for everything the surface
// broke, and the caller turns that into an exit code.
func Serve(in io.Reader, out io.Writer, opts Options) error {
	s := &server{out: out, boot: opts.Boot}
	return s.serve(in)
}

type server struct {
	// write guards the writer. ONE FRAME IS ONE LINE IS ONE WRITE: a torn frame
	// is not a slow surface, it is a surface that can never parse this stream
	// again, so every writer on this process goes through here.
	write sync.Mutex
	out   io.Writer
	// dead records that the far end stopped listening. A write error is not
	// worth reporting twice and there is nowhere left to report it to.
	dead bool

	boot func(Hello) (*Engine, error)

	// state guards everything a session swap moves.
	state  sync.Mutex
	engine *Engine
	agent  WrappedAgent
	// generation counts the swaps. Every pump remembers the generation it was
	// born in and goes quiet the moment it is not the current one — see
	// [server.pump].
	generation uint64
	streams    uint64
	// pending is the stream a call has just opened and dispatch has not yet let
	// speak. It is one slot rather than a queue because one reader makes one
	// call at a time.
	pending *pending
	pumps   sync.WaitGroup
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
		s.shutdown()
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
	// READER EOF IS THE ORDINARY END. The person closed the surface, the ssh
	// channel went away, the laptop lid shut — all the same event, and all
	// answered the same way by the deferred shutdown: interrupt the turn, close
	// the agent, exit 0.
	return nil
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
	engine, err := s.boot(hello)
	if err != nil {
		return s.refuse("engine: " + err.Error())
	}
	if engine == nil || engine.Agent == nil {
		return s.refuse("engine: the workspace opened no conversation")
	}
	s.state.Lock()
	s.engine, s.agent = engine, engine.Agent
	s.state.Unlock()
	return s.send(Frame{Kind: "welcome", Payload: mustJSON(s.welcome())})
}

// refuse says why on the wire and then hands the same sentence back as the
// error, so the exit code and the surface's message are the one fact.
func (s *server) refuse(reason string) error {
	s.fatal(reason)
	return errors.New(reason)
}

func (s *server) welcome() Welcome {
	s.state.Lock()
	defer s.state.Unlock()
	return Welcome{
		Version:      Version,
		Workspace:    s.engine.Workspace,
		SessionFile:  s.engine.SessionFile,
		Resumed:      s.engine.Resumed,
		Model:        s.agent.Model(),
		Title:        s.agent.Title(),
		Note:         s.engine.Note,
		ApprovalMode: s.engine.ApprovalMode,
	}
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

	agent := s.current()
	if agent == nil {
		return nil, errors.New("engine: no conversation is open")
	}
	switch call.Method {
	case MethodSubmit:
		args, err := arg[SubmitArgs](call)
		if err != nil {
			return nil, err
		}
		return s.open(agent.Submit(context.Background(), args.Text))

	case MethodFollowUp:
		args, err := arg[SubmitArgs](call)
		if err != nil {
			return nil, err
		}
		return s.open(agent.FollowUp(args.Text))

	case MethodSubmitImage:
		args, err := arg[SubmitImageArgs](call)
		if err != nil {
			return nil, err
		}
		images, err := s.store(args.Images)
		if err != nil {
			return nil, err
		}
		return s.open(agent.SubmitImage(context.Background(), args.Text, images))

	case MethodInterrupt:
		agent.Interrupt()
		return nil, nil

	case MethodCompact:
		return nil, agent.Compact(context.Background())

	case MethodClose:
		// The surface said goodbye politely. The journal is flushed here rather
		// than at the hang-up that follows, which is the same work done a
		// moment earlier and with somebody still listening for the failure.
		return nil, agent.Close()

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
		return nil, nil

	case MethodConsentRemember:
		args, err := arg[ConsentArgs](call)
		if err != nil {
			return nil, err
		}
		agent.ResolveConsentRemember(args.ID, args.Allow, args.Scope)
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

	case MethodTranscript:
		return json.Marshal(agent.Transcript())

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
		s.state.Lock()
		recent := s.engine.Recent
		s.state.Unlock()
		if recent == nil {
			return nil, errors.New("engine: this engine cannot list sessions")
		}
		return json.Marshal(recent())

	case MethodSessionNew:
		s.state.Lock()
		fresh := s.engine.Fresh
		s.state.Unlock()
		if fresh == nil {
			return nil, errors.New("engine: this engine cannot start a new session")
		}
		return s.swap(func() (WrappedAgent, string, bool, error) {
			next, file, err := fresh()
			return next, file, false, err
		})

	case MethodSessionOpen:
		file, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		s.state.Lock()
		open := s.engine.Open
		s.state.Unlock()
		if open == nil {
			return nil, errors.New("engine: this engine cannot open another session")
		}
		return s.swap(func() (WrappedAgent, string, bool, error) {
			next, resumed, err := open(file)
			return next, file, resumed, err
		})
	}
	return nil, fmt.Errorf("engine: no such method %q", call.Method)
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

// ── streams ─────────────────────────────────────────────────────────────────

// open turns a submitted turn into a stream id. The id is answered IMMEDIATELY,
// before a single event, because the surface has to be able to bind events to
// the turn that caused them and because a result that waited for the turn would
// hold the reader shut for the length of the work.
//
// IT DOES NOT START THE PUMP. The pump is left waiting on the call frame's own
// result (see [server.dispatch]), because a stream whose first event overtook
// the result naming it would be events about a stream the surface has never
// heard of — the one ordering this protocol cannot recover from, and a race that
// would show up as a lost first token on a fast turn and never in a test.
func (s *server) open(events <-chan session.Event, err error) (json.RawMessage, error) {
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
	s.state.Lock()
	s.streams++
	s.pending = &pending{id: s.streams, generation: s.generation, events: events}
	id := s.streams
	s.state.Unlock()
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
func (s *server) release() {
	s.state.Lock()
	waiting := s.pending
	s.pending = nil
	s.state.Unlock()
	if waiting == nil {
		return
	}
	s.pumps.Add(1)
	go s.pump(waiting.id, waiting.generation, waiting.events)
}

// pump is one turn's events on the wire, in order, followed by the close.
//
// A STREAM BELONGS TO THE AGENT THAT OPENED IT. When a session swap replaces
// that agent the surface has already been handed a fresh welcome and has
// forgotten everything before it, so a late event from the old conversation is
// not stale news but a lie about the new one. The pump keeps draining — the
// channel must not be left with a writer blocked on it — and says nothing.
func (s *server) pump(id, generation uint64, events <-chan session.Event) {
	defer s.pumps.Done()
	defer guard.Recover("remote/engine stream")
	for event := range events {
		if !s.live(generation) {
			continue
		}
		payload, err := json.Marshal(WireEvent(event))
		if err != nil {
			// An event that cannot be encoded is a field somebody added that
			// does not survive JSON. Dropping the one event keeps the turn
			// readable, which is better than ending the stream over it.
			continue
		}
		if err := s.send(Frame{Kind: "event", ID: id, Payload: payload}); err != nil {
			return
		}
	}
	if s.live(generation) {
		_ = s.send(Frame{Kind: "closed", ID: id})
	}
}

func (s *server) live(generation uint64) bool {
	s.state.Lock()
	defer s.state.Unlock()
	return s.generation == generation
}

// swap replaces the conversation and answers with a whole new welcome, because
// everything in one is now different: the file, whether it was resumed, the
// name it has given itself.
//
// The generation moves BEFORE the old agent is closed, so the pumps that are
// about to see their channels close have already gone quiet.
func (s *server) swap(build func() (WrappedAgent, string, bool, error)) (json.RawMessage, error) {
	next, file, resumed, err := build()
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, errors.New("engine: the session swap opened no conversation")
	}
	s.state.Lock()
	previous := s.agent
	s.agent = next
	s.generation++
	s.engine.SessionFile = file
	s.engine.Resumed = resumed
	// The note belonged to the launch and to nothing after it: a swap the
	// person asked for is not a session that moved under them.
	s.engine.Note = ""
	s.state.Unlock()

	if previous != nil {
		previous.Interrupt()
		_ = previous.Close()
	}
	return json.Marshal(s.welcome())
}

func (s *server) current() WrappedAgent {
	s.state.Lock()
	defer s.state.Unlock()
	return s.agent
}

// ── the pipe ────────────────────────────────────────────────────────────────

func (s *server) send(frame Frame) error {
	line, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	s.write.Lock()
	defer s.write.Unlock()
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

// shutdown is the exit, and it runs on every road out of [server.serve] — the
// hang-up, the refusal, the fault.
//
// THE JOURNAL IS THE SAFETY. An engine is the only writer of its session file
// and the surface holds nothing that is not in it, so the whole of "did this
// conversation survive the pipe dying" is whether Close ran. The interrupt goes
// first so a turn in flight stops where it is and keeps its partial reply,
// which is exactly what ctrl+c does locally; then the agent is closed, which
// flushes the file. Everything else — the pumps, the frames nobody will read —
// is allowed to end however it ends.
func (s *server) shutdown() {
	agent := s.current()
	if agent == nil {
		return
	}
	agent.Interrupt()
	_ = agent.Close()
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
