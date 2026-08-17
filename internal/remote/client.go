package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE SURFACE HALF ────────────────────────────────────────────────────────
//
// This file is everything the local half of `aforge chat --host devbox` needs:
// a [Client] over the ssh process's pipes, and an [Agent] that satisfies
// internal/tui3's own Agent interface so the surface cannot tell the difference.
// The surface calls methods; this turns them into lines; the engine answers.
//
// THREE GOROUTINES AND NO MORE. One writer, serialized by a mutex, because two
// halves of two frames interleaved on one pipe is a stream nobody can decode.
// One reader, which is the ONLY thing that touches the routing maps. And one
// pump per open stream, which exists for the reason stated on [stream]: the
// reader must never be the thing that blocks.
//
// EVERY CALL HAS A DEADLINE. The surface asks half of these questions from its
// update loop — Model, Usage, ContextTokens, Title — and an update loop that
// blocks is a terminal that has stopped repainting. A pipe whose far end died
// without closing (a laptop that slept, a network that went away) would hang
// there forever, so a call that has waited [callDeadline] gives up and says the
// connection is gone. That is a true sentence: a round trip to a healthy engine
// is milliseconds, and one that has taken ten seconds is not coming back.
//
// NOTHING HERE MEASURES ANYTHING EXTRA. Every getter is one frame out and one
// frame back. The surface asks Model() on frames it repaints, so a client that
// took a second round trip to "check" something would have doubled the cost of
// drawing a status line.

// callDeadline is how long any one call waits for its result. See the law above.
const callDeadline = 10 * time.Second

// Client is one connection to one engine. It is safe for concurrent use, which
// it has to be: the surface asks synchronous getters from its update loop while
// a turn's events are arriving on the reader.
type Client struct {
	// host is the ssh destination as the person typed it, and it exists for one
	// purpose: the sentence a broken connection says names the machine they were
	// working on. A person with three windows open needs to know WHICH.
	host string
	// conn is the pipe pair, and closing it is what ends the ssh process.
	conn io.ReadWriteCloser
	// lines is the decoder over the read half, owned by the reader goroutine
	// after the handshake and touched by nothing else.
	lines *json.Decoder

	// welcome is what the engine said at the door, and what the door in
	// cmd/aforge reads its Options out of.
	mu      sync.Mutex
	welcome Welcome

	// writeMu serializes frames onto the pipe. It is separate from mu because a
	// write must not be held up by a map lookup and vice versa.
	writeMu sync.Mutex

	// seq mints call ids. The engine mints stream ids, so the two spaces never
	// collide even though both are uint64.
	seq atomic.Uint64

	// calls is every call waiting for its result, and streams every open turn.
	// Both are guarded by mu.
	calls   map[uint64]chan result
	streams map[uint64]*stream

	// dead is the reason this connection stopped, or nil while it is alive.
	// Every method reads it first, so a surface driving a corpse gets an error
	// per call rather than a hang per call. done is closed at the same moment,
	// which is what wakes the calls that were already waiting.
	dead error
	done chan struct{}
	// closing says Close was called here, so the EOF the reader is about to see
	// is expected and not worth a sentence about a lost connection.
	closing bool
}

// result is one answered call, as the reader hands it to the waiting caller.
type result struct {
	payload json.RawMessage
	err     error
}

// Dial performs the handshake on an already-open pipe pair and returns the live
// client. It is separate from spawning ssh on purpose: the spawning belongs to
// the door (cmd/aforge, which owns processes and flags), and a test drives this
// over an io.Pipe with no ssh anywhere.
//
// IT BLOCKS UNTIL THE ENGINE HAS ANSWERED, and that is the whole point of the
// order the door runs things in: the handshake happens while the terminal is
// still the person's, so ssh's own passphrase and host-key questions, and the
// sentence below about a version mismatch, are plain text on a plain screen.
func Dial(conn io.ReadWriteCloser, host string, hello Hello) (*Client, error) {
	hello.Version = Version
	c := &Client{
		host:    strings.TrimSpace(host),
		conn:    conn,
		lines:   json.NewDecoder(conn),
		calls:   map[uint64]chan result{},
		streams: map[uint64]*stream{},
		done:    make(chan struct{}),
	}
	payload, err := json.Marshal(hello)
	if err != nil {
		return nil, err
	}
	if err := c.write(Frame{Kind: "hello", Payload: payload}); err != nil {
		return nil, c.gone(err)
	}
	var frame Frame
	if err := c.lines.Decode(&frame); err != nil {
		return nil, c.gone(err)
	}
	switch frame.Kind {
	case "welcome":
	case "fatal":
		return nil, errors.New(strings.TrimSpace(frame.Error))
	default:
		return nil, fmt.Errorf("%s answered with a %q where a welcome belongs", c.where(), frame.Kind)
	}
	var welcome Welcome
	if err := json.Unmarshal(frame.Payload, &welcome); err != nil {
		return nil, err
	}
	// THE REFUSAL IS AT THE DOOR, which is what wire.go's Version says. Two
	// builds that might disagree about a frame must not find that out three
	// turns into a conversation, and the sentence names the fix — one machine
	// has an older aforge on it, and the person knows which machine is which.
	if welcome.Version != Version {
		return nil, fmt.Errorf("%s runs a different version of aforge than this machine does — update the older one so both ends speak the same protocol", c.where())
	}
	c.welcome = welcome
	go c.read()
	return c, nil
}

// Welcome is what the engine said at the door: the workspace it resolved, the
// session file it opened, and whether it found that file or made it.
func (c *Client) Welcome() Welcome {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.welcome
}

// Host is the ssh destination this client dialled.
func (c *Client) Host() string { return c.host }

// Close ends the connection, which ends the ssh process. It is NOT what the
// surface's /new and /resume call — see [Agent.Close], which flushes the remote
// session file and leaves the connection standing.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return nil
	}
	c.closing = true
	c.mu.Unlock()
	return c.conn.Close()
}

// Err is why this connection stopped, or nil while it is alive.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dead
}

// where names the far end the way a sentence about it should: the destination
// the person typed, or "the engine" when they typed nothing nameable.
func (c *Client) where() string {
	if c.host == "" {
		return "the engine"
	}
	return c.host
}

// gone is THE HONEST SENTENCE, and it is one sentence in one place so that
// every road to a dead connection says the same thing.
//
// It says what happened in the person's own terms — the machine has a name, and
// "the connection" is a thing they can picture — and then it says the ONE thing
// they can do about it, which is to run the command again. That is not advice
// dressed up: the engine journals every turn as it happens, so the conversation
// they were having is on the far machine's disk and the same command opens it
// again. A sentence that only reported the failure would leave a person
// wondering whether their work survived.
func (c *Client) gone(cause error) error {
	sentence := fmt.Sprintf("the connection to %s is gone — run the same command to pick the conversation back up", c.where())
	// A cause worth repeating is one the FAR END chose to say (a "fatal" frame's
	// reason). Transport errors are not: "read |0: file already closed" tells a
	// person nothing they can act on, and this file is not a place to teach them
	// what a pipe is.
	var spoken spokenError
	if errors.As(cause, &spoken) && strings.TrimSpace(spoken.reason) != "" {
		return fmt.Errorf("%s (%s)", sentence, strings.TrimSpace(spoken.reason))
	}
	return errors.New(sentence)
}

// spokenError is a reason the ENGINE gave, as opposed to one the transport did.
// See [Client.gone] for why the two are told apart.
type spokenError struct{ reason string }

func (e spokenError) Error() string { return e.reason }

// write puts one frame on the wire, whole, under the writer's lock.
func (c *Client) write(frame Frame) error {
	line, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.conn.Write(line)
	return err
}

// read is the reader goroutine: the only thing that decodes frames, and the
// only thing that writes to the routing maps.
func (c *Client) read() {
	for {
		var frame Frame
		if err := c.lines.Decode(&frame); err != nil {
			c.bury(err)
			return
		}
		switch frame.Kind {
		case "result":
			c.deliver(frame)
		case "event":
			c.stream(frame.ID).push(frame.Payload)
		case "closed":
			c.stream(frame.ID).finish()
		case "fatal":
			c.bury(spokenError{reason: frame.Error})
			return
		default:
			// A frame kind this build does not know is IGNORED rather than
			// fatal. The envelope is the contract (wire.go says so) and a newer
			// engine adding a kind must not take the conversation down; the
			// frames this build does understand still arrive.
		}
	}
}

// deliver routes one result to the call waiting for it.
func (c *Client) deliver(frame Frame) {
	c.mu.Lock()
	waiting, ok := c.calls[frame.ID]
	delete(c.calls, frame.ID)
	c.mu.Unlock()
	if !ok {
		// A result for a call that has already given up (its deadline passed).
		// Dropping it is right: the caller has been told the connection is gone
		// and nobody is holding the other end of that channel.
		return
	}
	if frame.Error != "" {
		waiting <- result{err: errors.New(frame.Error)}
		return
	}
	waiting <- result{payload: frame.Payload}
}

// stream is the open turn with this id, created on first sight.
//
// It is GET-OR-CREATE because two goroutines can reach for the same stream and
// the order is not fixed: the reader sees the first "event" frame, and the
// caller of Submit sees the [StreamRef] in its result. Both roads end in the
// same object, and whichever arrives first builds it.
func (c *Client) stream(id uint64) *stream {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streamLocked(id)
}

func (c *Client) streamLocked(id uint64) *stream {
	if s, ok := c.streams[id]; ok {
		return s
	}
	s := newStream()
	c.streams[id] = s
	return s
}

// bury marks the connection dead and fails everything that was waiting on it.
//
// EVERY OUTSTANDING CALL FAILS AND EVERY OPEN STREAM CLOSES. A turn whose events
// stopped arriving is a turn that ended, as far as the screen is concerned, and
// a channel left open would leave the surface spinning on a turn nobody is
// running.
func (c *Client) bury(cause error) {
	c.mu.Lock()
	if c.dead != nil {
		c.mu.Unlock()
		return
	}
	if c.closing {
		// We closed the pipe ourselves, so the EOF is the sound of the door
		// shutting. It is still a dead client — nothing may be called on it —
		// but it is not a lost connection.
		c.dead = errors.New("this connection is closed")
	} else {
		c.dead = c.gone(cause)
	}
	dead := c.dead
	calls, streams := c.calls, c.streams
	c.calls, c.streams = map[uint64]chan result{}, map[uint64]*stream{}
	close(c.done)
	c.mu.Unlock()

	for _, waiting := range calls {
		waiting <- result{err: dead}
	}
	for _, s := range streams {
		// The turn is told WHY it stopped, on the stream, before the stream
		// closes: the surface draws session.EventError as the turn's failure and
		// would otherwise show a turn that simply stopped mid-sentence.
		s.fail(dead)
		s.finish()
	}
}

// call is one round trip: a frame out, a result back, or the deadline.
func (c *Client) call(ctx context.Context, method string, args any) (json.RawMessage, error) {
	var payload json.RawMessage
	if args != nil {
		encoded, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}
	id := c.seq.Add(1)
	waiting := make(chan result, 1)

	c.mu.Lock()
	if c.dead != nil {
		dead := c.dead
		c.mu.Unlock()
		return nil, dead
	}
	c.calls[id] = waiting
	c.mu.Unlock()

	if err := c.write(Frame{Kind: "call", ID: id, Method: method, Payload: payload}); err != nil {
		c.mu.Lock()
		delete(c.calls, id)
		c.mu.Unlock()
		return nil, c.gone(err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(callDeadline)
	defer timer.Stop()
	select {
	case answer := <-waiting:
		return answer.payload, answer.err
	case <-ctx.Done():
		c.forget(id)
		return nil, ctx.Err()
	case <-timer.C:
		c.forget(id)
		return nil, c.gone(errors.New("no answer"))
	}
}

// forget drops a call nobody is waiting for any more.
func (c *Client) forget(id uint64) {
	c.mu.Lock()
	delete(c.calls, id)
	c.mu.Unlock()
}

// ── the typed doors ─────────────────────────────────────────────────────────

// Recent is this workspace's past conversations, as the engine's disk holds
// them. It is the remote answer to the welcome box's right column and the
// /resume picker's rows.
//
// AN ERROR IS AN EMPTY LIST, which is what the local door does with an
// unreadable session directory (cmd/aforge's v3RecentSessions says why): this
// answers a list a person may never look at, and the one thing it must not do
// is take a keystroke away.
func (c *Client) Recent() []session.Summary {
	payload, err := c.call(nil, MethodSessionsRecent, nil)
	if err != nil {
		return nil
	}
	var rows []session.Summary
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil
	}
	return rows
}

// NewSession asks the engine to swap to a fresh session, and answers with the
// facts about it. It is /new, and the Welcome it returns replaces the one Dial
// got — the session file changed, and everything on screen that names it has to
// name the new one.
func (c *Client) NewSession() (Welcome, error) {
	return c.swap(MethodSessionNew, nil)
}

// OpenSession asks the engine to swap to a session it already has, by transcript
// path. It is /resume, and the path came off [Client.Recent], so it is a path on
// the ENGINE's disk and is never resolved here.
func (c *Client) OpenSession(path string) (Welcome, error) {
	return c.swap(MethodSessionOpen, path)
}

func (c *Client) swap(method string, args any) (Welcome, error) {
	payload, err := c.call(nil, method, args)
	if err != nil {
		return Welcome{}, err
	}
	var welcome Welcome
	if err := json.Unmarshal(payload, &welcome); err != nil {
		return Welcome{}, err
	}
	c.mu.Lock()
	c.welcome = welcome
	c.mu.Unlock()
	return welcome, nil
}

// ── the agent ───────────────────────────────────────────────────────────────

// Agent is the engine's session as internal/tui3 sees it: every method of
// tui3.Agent, plus the rewind pair that surface type-asserts for
// (internal/tui3's rewind.go). It holds no state of its own — it is a handle on
// whichever session the engine currently has open, which is why /new and
// /resume keep using the same one.
type Agent struct{ c *Client }

// Agent is the handle onto the engine's current session.
func (c *Client) Agent() *Agent { return &Agent{c: c} }

// Client is the connection under this agent, for a door that needs the session
// seams as well as the conversation ones.
func (a *Agent) Client() *Client { return a.c }

// Submit runs one turn and streams its events.
//
// THE CONTEXT BOUNDS THE CALL AND NOT THE TURN. Locally, cancelling the context
// handed to Submit cancels the work; here it can only cancel the round trip that
// STARTS the work, because the work is on another machine. The surface's own
// cancel is [Agent.Interrupt], which is a frame of its own and travels, so
// nothing a person can press is lost — but a caller reading this method's
// signature should know which of the two it is holding.
func (a *Agent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	return a.open(ctx, MethodSubmit, SubmitArgs{Text: text})
}

// SubmitImage is Submit with pictures. THE BYTES ARE READ HERE, on the machine
// the person is sitting at, because that is the only machine the path means
// anything on: /image points at a file on their laptop and the engine has no way
// to open it. The same two ceilings the local lane applies are applied here
// (internal/session's image.go), for the same reason and one more — an
// unchecked path would put a multi-gigabyte file through an ssh pipe before
// anybody discovered it was too big.
func (a *Agent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	loaded, err := loadImages(images)
	if err != nil {
		return nil, err
	}
	return a.open(ctx, MethodSubmitImage, SubmitImageArgs{Text: text, Images: loaded})
}

// FollowUp queues a message for after this turn and returns the stream that turn
// will run on.
func (a *Agent) FollowUp(text string) (<-chan session.Event, error) {
	return a.open(nil, MethodFollowUp, SubmitArgs{Text: text})
}

// open is the three stream-opening calls' one body: the call, the [StreamRef] it
// answers with, and the channel that turn's events arrive on.
func (a *Agent) open(ctx context.Context, method string, args any) (<-chan session.Event, error) {
	payload, err := a.c.call(ctx, method, args)
	if err != nil {
		return nil, err
	}
	var ref StreamRef
	if err := json.Unmarshal(payload, &ref); err != nil {
		return nil, err
	}
	return a.c.stream(ref.Stream).events(), nil
}

// Interrupt cancels the in-flight turn. IT DOES NOT WAIT and it reports nothing:
// the interface says so, and a key that is pressed to stop something must not
// itself become a thing that blocks. A dead connection swallows it, which is
// exactly what a dead connection does to the turn as well.
func (a *Agent) Interrupt() { _, _ = a.c.call(nil, MethodInterrupt, nil) }

// Compact runs a compaction pass on the far side.
func (a *Agent) Compact(ctx context.Context) error {
	_, err := a.c.call(ctx, MethodCompact, nil)
	return err
}

// Close flushes the REMOTE session file and leaves this connection standing.
//
// That is the whole difference between it and [Client.Close], and it is a
// difference the surface depends on: /new and /resume both close the agent they
// are holding before asking for the next one (internal/tui3's app.go and
// welcome.go), and a Close that hung up the ssh process would make the second
// conversation impossible. The connection is the DOOR; the session is what is
// behind it, and only cmd/aforge shuts the door.
func (a *Agent) Close() error {
	_, err := a.c.call(nil, MethodClose, nil)
	return err
}

// Model is the model the next request will use.
func (a *Agent) Model() string { return a.text(MethodModel) }

// SetModel swaps it.
func (a *Agent) SetModel(model string) { _, _ = a.c.call(nil, MethodSetModel, model) }

// SetContextWindow says how many tokens the model now in use accepts.
func (a *Agent) SetContextWindow(tokens int) { _, _ = a.c.call(nil, MethodSetContext, tokens) }

// ReasoningFor is how hard one model is asked to think.
func (a *Agent) ReasoningFor(model string) string {
	payload, err := a.c.call(nil, MethodReasoningFor, model)
	if err != nil {
		return ""
	}
	var level string
	_ = json.Unmarshal(payload, &level)
	return level
}

// SetReasoningFor sets it.
func (a *Agent) SetReasoningFor(model, level string) {
	_, _ = a.c.call(nil, MethodSetReasoningFor, ReasoningArgs{Model: model, Level: level})
}

// ResolveConsent answers one approval question for this call only.
func (a *Agent) ResolveConsent(id uint64, allow bool) {
	_, _ = a.c.call(nil, MethodConsent, ConsentArgs{ID: id, Allow: allow})
}

// ResolveConsentRemember answers one and says how long the answer lasts.
func (a *Agent) ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope) {
	_, _ = a.c.call(nil, MethodConsentRemember, ConsentArgs{ID: id, Allow: allow, Scope: scope})
}

// ResolveHarness answers one sub-harness offer.
func (a *Agent) ResolveHarness(id uint64, run bool, model string) {
	_, _ = a.c.call(nil, MethodHarness, HarnessArgs{ID: id, Run: run, Model: model})
}

// ResolveConnect answers one connect ask.
func (a *Agent) ResolveConnect(id string, approve bool) {
	_, _ = a.c.call(nil, MethodConnect, ConnectArgs{ID: id, Approve: approve})
}

// ResolveConnectKey answers one connect ask that arrived with NeedsKey.
func (a *Agent) ResolveConnectKey(id string, key string) {
	_, _ = a.c.call(nil, MethodConnectKey, ConnectArgs{ID: id, Key: key})
}

// NoteConnected tells the session an account is connected.
func (a *Agent) NoteConnected(service, account string) {
	_, _ = a.c.call(nil, MethodNoteConnected, ConnectedArgs{Service: service, Account: account})
}

// Title is the name the session gave itself.
func (a *Agent) Title() string { return a.text(MethodTitle) }

// Usage is the session's running total.
func (a *Agent) Usage() session.Usage {
	payload, err := a.c.call(nil, MethodUsage, nil)
	if err != nil {
		return session.Usage{}
	}
	var usage session.Usage
	_ = json.Unmarshal(payload, &usage)
	return usage
}

// ContextTokens is what the conversation weighs right now.
func (a *Agent) ContextTokens() int {
	payload, err := a.c.call(nil, MethodContextTokens, nil)
	if err != nil {
		return 0
	}
	var tokens int
	_ = json.Unmarshal(payload, &tokens)
	return tokens
}

// Transcript is the conversation so far, shaped for display.
func (a *Agent) Transcript() []session.DisplayEntry {
	return a.entries(MethodTranscript, nil)
}

// RewindPoints is every place the conversation can be cut. It is half of the
// OPTIONAL pair internal/tui3's rewind.go type-asserts for, and this agent
// implements it so a remote session rewinds exactly like a local one.
func (a *Agent) RewindPoints() []session.RewindPoint {
	payload, err := a.c.call(nil, MethodRewindPoints, nil)
	if err != nil {
		return nil
	}
	var points []session.RewindPoint
	_ = json.Unmarshal(payload, &points)
	return points
}

// RewindAt cuts at one of them. Its error is SHOWN — the mode stays up and
// prints the sentence — so a dead connection lands there like any other refusal.
func (a *Agent) RewindAt(index int) ([]session.DisplayEntry, error) {
	payload, err := a.c.call(nil, MethodRewindAt, index)
	if err != nil {
		return nil, err
	}
	var entries []session.DisplayEntry
	if err := json.Unmarshal(payload, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// text is the shape four getters share: a call whose result is one string, and
// whose failure is the empty string. THE EMPTY STRING IS THE HONEST ANSWER
// HERE, and it is not a swallowed error: these are drawn on a status line, the
// interface gives them no way to report anything, and the surface's emptiness
// law already draws a missing fact as nothing at all. The error the person needs
// arrives on the next Submit, where there is somewhere to put it.
func (a *Agent) text(method string) string {
	payload, err := a.c.call(nil, method, nil)
	if err != nil {
		return ""
	}
	var value string
	_ = json.Unmarshal(payload, &value)
	return value
}

func (a *Agent) entries(method string, args any) []session.DisplayEntry {
	payload, err := a.c.call(nil, method, args)
	if err != nil {
		return nil
	}
	var entries []session.DisplayEntry
	_ = json.Unmarshal(payload, &entries)
	return entries
}

// ── the streams ─────────────────────────────────────────────────────────────

// stream is one turn's events on their way to the surface.
//
// IT HAS AN UNBOUNDED QUEUE AND A PUMP OF ITS OWN, and that is not an
// optimization — it is what keeps the connection from deadlocking. The surface
// reads events one at a time from its update loop, and that same loop asks
// synchronous getters (Model, Usage, ContextTokens) which are round trips
// waiting on the reader goroutine. If the reader delivered events by blocking on
// the surface's channel, then a loop waiting for a getter's result and a reader
// waiting for the loop to take an event would be waiting for each other for
// ever. So the reader never blocks: it appends, and the pump does the waiting.
//
// The queue's ceiling is a turn's own event count, which is bounded by the turn,
// and the surface drains it continuously. internal/session's own hub hands out
// an UNBUFFERED channel for the same events, so the buffering added here is the
// buffering the wire needs and no more of a promise than the local lane makes.
type stream struct {
	mu     sync.Mutex
	wake   *sync.Cond
	queue  []session.Event
	closed bool
	out    chan session.Event
	once   sync.Once
}

func newStream() *stream {
	s := &stream{out: make(chan session.Event)}
	s.wake = sync.NewCond(&s.mu)
	go s.pump()
	return s
}

// events is the channel the surface ranges over.
func (s *stream) events() <-chan session.Event { return s.out }

// push queues one encoded event. A payload that will not decode is DROPPED
// rather than fatal: one unreadable line is one lost event, which is the bargain
// wire.go's framing was chosen for.
func (s *stream) push(payload json.RawMessage) {
	if len(payload) == 0 {
		return
	}
	var wired EventWire
	if err := json.Unmarshal(payload, &wired); err != nil {
		return
	}
	s.deliver(wired.Unwire())
}

// fail puts one error event on the stream — what a turn says when the
// connection under it died.
func (s *stream) fail(err error) {
	s.deliver(session.Event{Kind: session.EventError, Err: err})
}

func (s *stream) deliver(ev session.Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.queue = append(s.queue, ev)
	s.mu.Unlock()
	s.wake.Signal()
}

// finish is the "closed" frame: no more events, and the channel closes once
// what is queued has been read.
func (s *stream) finish() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.wake.Signal()
}

func (s *stream) pump() {
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.wake.Wait()
		}
		if len(s.queue) == 0 {
			s.mu.Unlock()
			s.once.Do(func() { close(s.out) })
			return
		}
		ev := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		s.out <- ev
	}
}

// ── pictures ────────────────────────────────────────────────────────────────

// The two ceilings and the accepted types are internal/session's own
// (image.go), restated here because they are unexported there and because this
// is a SECOND door onto the same limits: a picture that the local lane would
// refuse must be refused here too, with the same words, or the same photo would
// be accepted or refused depending on which machine the engine is on.
//
// STUB: if internal/session ever exports its loader, this should call it instead
// of holding a copy of the numbers.
const (
	maxImageBytes        = 10 << 20
	maxMessageImageBytes = 20 << 20
)

var imageMediaTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// loadImages fills every picture's bytes from this machine's disk, so the whole
// message can travel. It refuses the same two ways internal/session does, and
// checks the size against the stat BEFORE the read for the same reason.
func loadImages(images []session.Image) ([]session.Image, error) {
	loaded := make([]session.Image, 0, len(images))
	total := 0
	for _, image := range images {
		path := strings.TrimSpace(image.Path)
		if path == "" {
			return nil, errors.New("session: image has no path")
		}
		mediaType := strings.TrimSpace(image.MIME)
		if mediaType == "" {
			mediaType = imageMediaTypes[strings.ToLower(filepath.Ext(path))]
		}
		if mediaType == "" {
			return nil, fmt.Errorf("session: %s is not an image this surface can send — png, jpeg, webp and gif are", filepath.Base(path))
		}
		data := image.Bytes
		if data == nil {
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				return nil, fmt.Errorf("session: could not read %s", filepath.ToSlash(path))
			}
			if info.Size() > maxImageBytes {
				return nil, oversizeImage(path)
			}
			data, err = os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("session: could not read %s", filepath.ToSlash(path))
			}
		}
		if len(data) > maxImageBytes {
			return nil, oversizeImage(path)
		}
		total += len(data)
		if total > maxMessageImageBytes {
			return nil, fmt.Errorf("session: these images total more than the %dMB a single message may carry — send them across a few messages", maxMessageImageBytes>>20)
		}
		loaded = append(loaded, session.Image{Path: path, MIME: mediaType, Bytes: data})
	}
	return loaded, nil
}

func oversizeImage(path string) error {
	return fmt.Errorf("session: %s is over the %dMB image limit", filepath.ToSlash(path), maxImageBytes>>20)
}
