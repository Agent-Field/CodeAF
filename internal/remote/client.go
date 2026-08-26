package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
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
// One reader, which is the ONLY thing that touches the routing maps — and, on a
// client that roams (redial.go), the thing that opens the next pipe as well: a
// reader whose link has died has nothing to read, so it is exactly the goroutine
// that should be redialling. And one pump per open stream, which exists for the
// reason stated on [stream]: the reader must never be the thing that blocks.
//
// EVERY EVENT IS DELIVERED EXACTLY ONCE, and that is the whole correctness
// argument for replay. A returning surface tells the engine how far it got
// ([Hello.Resume]) and the engine sends what came after, but the boundary is
// agreed by two machines over a link that just failed, so the surface must be
// able to survive being sent a little of what it already has. So every stream
// remembers the highest [Frame.Seq] it has actually HANDED TO THE SURFACE, and
// an event whose seq is at or below that is DROPPED rather than delivered —
// because the alternative is a turn's text drawn twice on the screen, which is
// the one failure a person would read as the program having lost its mind.
//
// SEQ ZERO IS NOT A DUPLICATE. Numbering starts at 1 (wire.go says so), so a
// zero means this engine does not number its events at all; those are always
// delivered and never move the cursor. A client must not go quiet because the
// far end declined to count.
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
	// conn is the pipe pair, and closing it is what ends the ssh process. A
	// roaming client replaces it with the next one rather than dying with it, so
	// it is read under mu and never cached by anything that outlives a frame.
	conn io.ReadWriteCloser
	// lines is the decoder over the read half, owned by the reader goroutine
	// after the handshake and touched by nothing else.
	lines *json.Decoder

	// welcome is what the engine said at the door, and what the door in
	// cmd/aforge reads its Options out of.
	mu      sync.Mutex
	welcome Welcome

	// hello is the door's own first frame, kept because a redial has to say it
	// again — the same workspace, the same session, the same model and level
	// (redial.go's [Client.resume] fills the cursors in).
	hello Hello

	// roam is the redial policy, and nil is a client that dies with its pipe.
	// See redial.go for everything it means.
	roam *Roaming
	// reconnecting is true between the link dying and the next one answering,
	// which is the whole of what [Client.LinkNote] reports and the reason a call
	// made in that gap is refused rather than written onto a dead pipe.
	reconnecting bool
	// notice is one sentence the surface should show once — the two facts a
	// redial can discover that a person must not be left to guess at. See
	// [Client.TakeNotice].
	notice string
	// stop is closed by Close, and it is what takes a roaming client out of its
	// backoff without waiting for the timer it is sitting on.
	stop     chan struct{}
	stopOnce sync.Once

	// writeMu serializes frames onto the pipe. It is separate from mu because a
	// write must not be held up by a map lookup and vice versa.
	writeMu sync.Mutex

	// made counts every call this client has PUT ON THE WIRE, and it exists for
	// the perf pins and for nothing else (PERF.md's doctrine: a law about a
	// round trip is a count, never a stopwatch). One atomic add behind a door
	// that already existed is the whole cost.
	made atomic.Uint64

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
	c := newClient(host, hello)
	if _, err := c.attach(conn); err != nil {
		return nil, err
	}
	go c.read()
	return c, nil
}

// newClient is the empty client both doors build — [Dial] and [Roam] — before
// anything has been said on a pipe.
func newClient(host string, hello Hello) *Client {
	hello.Version = Version
	return &Client{
		host:    strings.TrimSpace(host),
		hello:   hello,
		calls:   map[uint64]chan result{},
		streams: map[uint64]*stream{},
		done:    make(chan struct{}),
		stop:    make(chan struct{}),
	}
}

// attach says hello on one pipe and reads the welcome back. It is the handshake
// for BOTH doors: the first one and every redial, because a returning surface
// says exactly what an arriving one says plus how far it got.
//
// It runs before the reader goroutine exists — on [Dial]'s caller, and on the
// reader itself once it has stopped reading — so it decodes that one frame
// itself and nothing races it for the pipe.
func (c *Client) attach(conn io.ReadWriteCloser) (Welcome, error) {
	hello := c.helloNow()
	payload, err := json.Marshal(hello)
	if err != nil {
		return Welcome{}, err
	}
	lines := json.NewDecoder(conn)
	c.mu.Lock()
	c.conn, c.lines = conn, lines
	c.mu.Unlock()

	if err := c.write(Frame{Kind: "hello", Payload: payload}); err != nil {
		return Welcome{}, c.gone(err)
	}
	var frame Frame
	if err := lines.Decode(&frame); err != nil {
		return Welcome{}, c.gone(err)
	}
	switch frame.Kind {
	case "welcome":
	case "fatal":
		// A REFUSAL THE FAR END MADE IS CARRIED AS ONE, which is what
		// [spokenError] is for: the door prints the reason unchanged, and a
		// redial that meets it stops trying rather than spending its whole
		// window rediscovering the same no (redial.go).
		return Welcome{}, spokenError{reason: strings.TrimSpace(frame.Error)}
	default:
		return Welcome{}, fmt.Errorf("%s answered with a %q where a welcome belongs", c.where(), frame.Kind)
	}
	var welcome Welcome
	if err := json.Unmarshal(frame.Payload, &welcome); err != nil {
		return Welcome{}, err
	}
	// THE REFUSAL IS AT THE DOOR, which is what wire.go's Version says. Two
	// builds that might disagree about a frame must not find that out three
	// turns into a conversation, and the sentence names the fix — one machine
	// has an older aforge on it, and the person knows which machine is which.
	if welcome.Version != Version {
		return Welcome{}, spokenError{reason: fmt.Sprintf("%s runs a different version of aforge than this machine does — update the older one so both ends speak the same protocol", c.where())}
	}
	c.mu.Lock()
	c.welcome = welcome
	c.mu.Unlock()
	return welcome, nil
}

// helloNow is the hello as it should be said RIGHT NOW: the door's own, plus the
// session this client is actually in and how far it got on every stream still
// open. On the first dial there is nothing open and nothing has been swapped, so
// it is the door's hello unchanged.
func (c *Client) helloNow() Hello {
	c.mu.Lock()
	hello := c.hello
	open := c.welcome.SessionFile
	c.mu.Unlock()
	// The session file the engine last told us about beats the one the door
	// asked for: /new and /resume both move it, and a redial that asked for the
	// launch's file would reopen the conversation the person left behind.
	if strings.TrimSpace(open) != "" {
		hello.Session = open
	}
	if cursors := c.cursors(); len(cursors) > 0 {
		hello.Resume = cursors
	}
	return hello
}

// cursors is how far this surface got on every stream still open, which is the
// only thing an engine needs to send the gap and not the conversation.
func (c *Client) cursors() []StreamCursor {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []StreamCursor
	for id, s := range c.streams {
		if seen, open := s.cursor(); open {
			out = append(out, StreamCursor{Stream: id, Seq: seen})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stream < out[j].Stream })
	return out
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

// Attached is how many OTHER surfaces are on this session, as the engine
// counted them at the door.
//
// IT IS A FACT A PERSON MUST BE ABLE TO LEARN. Two windows on one conversation
// — two people, or one person and their own forgotten laptop — is a thing that
// changes what typing into it means, and a screen that hid it would be the one
// place aforge kept a secret about who is in the room. Zero draws nothing, by
// the emptiness law.
func (c *Client) Attached() int { return c.Welcome().Attached }

// Held is the questions this session raised while nobody was attached, as they
// arrived in the welcome. They are already here on the first frame a returning
// surface draws — see [HeldQuestion] for why they waited rather than expired.
func (c *Client) Held() []HeldQuestion { return c.Welcome().Held }

// Live is the turn that was already running when this surface arrived, and the
// channel its events come out of. It answers zero and nil when the session was
// idle, which is the ordinary case.
//
// THE CHANNEL IS THE SAME KIND OF CHANNEL A SUBMIT ANSWERS WITH, on purpose: a
// surface that reattaches mid-turn should draw that turn with the code that
// draws every turn, and the only thing it lacks is the [StreamRef] it would
// have got from opening it. This hands that back.
func (c *Client) Live() (uint64, <-chan session.Event) {
	id := c.Welcome().Live
	if id == 0 {
		return 0, nil
	}
	return id, c.stream(id).events()
}

// CallsMade is how many calls this client has put on the wire since it was
// dialled, and it is here for ONE reason: the laws that say a frame and a
// pointer cost nothing on the far machine are counts of round trips, and
// PERF.md's doctrine forbids proving such a thing with a clock. It counts calls
// and never stream frames, because a turn's events are the work a person asked
// for and the getters are the work nobody did.
func (c *Client) CallsMade() uint64 { return c.made.Load() }

// LinkNote is the quiet true sentence about the connection right now, and the
// empty string whenever there is nothing to say — which is almost always, and
// is what the emptiness law asks a status line to draw as nothing at all.
//
// THE LOUD SENTENCE IS NOT THIS ONE. A link that has merely dropped is being
// redialled and says `reconnecting to devbox…`; the sentence about a connection
// that is gone belongs to a client that has stopped trying, and [Client.Err] is
// where that one lives.
func (c *Client) LinkNote() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead != nil || !c.reconnecting {
		return ""
	}
	return c.roamingNote()
}

// TakeNotice is one sentence the surface should show once and then forget, and
// the empty string when there is none. IT DRAINS: the sentence is a piece of
// news about something that just happened to this connection, not a condition
// that stays true, so a second reading answers nothing.
//
// Only a redial writes one, and only for the two things a redial can discover
// that a person must not be left to work out from the screen: the engine did not
// keep the turn, and the engine came back with a different conversation open.
func (c *Client) TakeNotice() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	said := c.notice
	c.notice = ""
	return said
}

// note puts one sentence where [Client.TakeNotice] will find it. Two notices
// before anybody reads are joined rather than dropped: both are news, and a
// person who was away for both should be told both.
func (c *Client) note(sentence string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.notice == "" {
		c.notice = sentence
		return
	}
	c.notice += " — " + sentence
}

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
	conn := c.conn
	c.mu.Unlock()
	// A CLOSE ENDS THE ROAMING TOO, and it has to end it now rather than at the
	// end of whatever backoff the redial loop is sitting in: the person quit,
	// and a client that went on dialling a machine nobody is watching would be
	// an ssh process spawned after the terminal was given back.
	c.stopOnce.Do(func() { close(c.stop) })
	if conn == nil {
		return nil
	}
	return conn.Close()
}

// stopped says Close has been called, which is the one answer that outranks
// every reason to keep trying.
func (c *Client) stopped() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
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

// write puts one frame on the wire, whole, under the writer's lock. The pipe is
// read fresh every time because a roaming client replaces it, and a writer
// holding the one it was born with would be writing into a link that is gone.
func (c *Client) write(frame Frame) error {
	line, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("no link")
	}
	_, err = conn.Write(line)
	return err
}

// read is the reader goroutine: the only thing that decodes frames, and the
// only thing that writes to the routing maps.
func (c *Client) read() {
	for {
		c.mu.Lock()
		lines := c.lines
		c.mu.Unlock()
		var frame Frame
		if err := lines.Decode(&frame); err != nil {
			// The link under this reader stopped. On a roaming client that is a
			// pause and not an ending, so [Client.lost] is what decides which of
			// the two this was, and it comes back true holding a live pipe.
			if c.lost(err) {
				continue
			}
			return
		}
		switch frame.Kind {
		case "result":
			c.deliver(frame)
		case "event":
			c.stream(frame.ID).push(frame.Seq, frame.Payload)
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
	c.reconnecting = false
	dead := c.dead
	calls, streams := c.calls, c.streams
	c.calls, c.streams = map[uint64]chan result{}, map[uint64]*stream{}
	close(c.done)
	c.mu.Unlock()
	// Nothing is roaming any more either: this is the end, and a backoff still
	// counting down behind it would dial a machine whose conversation has
	// already been declared gone on the screen.
	c.stopOnce.Do(func() { close(c.stop) })

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
	c.made.Add(1)
	waiting := make(chan result, 1)

	c.mu.Lock()
	if c.dead != nil {
		dead := c.dead
		c.mu.Unlock()
		return nil, dead
	}
	// A CALL MADE IN THE GAP IS REFUSED, NOT QUEUED. There is no pipe to write
	// it onto, and holding it until one exists would turn a keystroke into a
	// thing that hangs for as long as the redialling takes. The refusal says
	// what is happening and that it is worth trying again, which is the truth:
	// every getter on this client is asked again on the next frame, and a
	// message the person typed is still in the composer.
	if c.reconnecting {
		c.mu.Unlock()
		return nil, errors.New(c.roamingRefusal())
	}
	c.calls[id] = waiting
	c.mu.Unlock()

	if err := c.write(Frame{Kind: "call", ID: id, Method: method, Payload: payload}); err != nil {
		c.mu.Lock()
		delete(c.calls, id)
		roaming := c.roam != nil && !c.closing
		c.mu.Unlock()
		// A write that failed on a roaming client is the link dying a moment
		// before the reader noticed it. The person is about to see
		// `reconnecting`, so this call says the same thing rather than the
		// sentence that means it is over.
		if roaming {
			return nil, errors.New(c.roamingRefusal())
		}
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

// StandingItems is the engine machine's standing items for one workspace: the
// far half of what a local surface reads straight off its own disk
// (cmd/aforge's [v3StandingSeam]).
//
// IT ANSWERS AN ERROR RATHER THAN AN EMPTY LIST, which is the one place it
// differs from [Client.Recent], and the difference is what the caller does with
// it: this list is asked for again and again on a beat, so a caller that keeps
// the last good answer must be able to tell "there is nothing here" from "the
// round trip failed" — a fault redrawn as an empty band would be the screen
// saying the person's watches had gone away.
func (c *Client) StandingItems(workspace string) ([]standing.Item, error) {
	payload, err := c.call(nil, MethodStandingItems, workspace)
	if err != nil {
		return nil, err
	}
	var items []standing.Item
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// SaveStanding writes one item back to the engine machine's store — the pause
// and the stop keys, and nothing else on this surface.
//
// THE REFUSAL TRAVELS. internal/tui3's StandingSeam.Save returns the write's
// error and home prints it rather than swallowing it, because a row that redrew
// as paused over a store that refused the write would be the screen lying about
// somebody else's disk. So the engine's error comes back as this call's error
// and nothing is invented here.
func (c *Client) SaveStanding(item standing.Item) error {
	_, err := c.call(nil, MethodStandingSave, item)
	return err
}

// HeldQuestions is what this session asked while nobody was attached, asked for
// over the wire rather than read off the welcome.
//
// THERE ARE TWO DOORS ONTO THE SAME LIST BECAUSE THERE ARE TWO MOMENTS. The
// welcome carries them so the first frame a returning surface draws already has
// them ([Client.Held]); this asks again, which is what a surface wants after it
// has answered one, after a session swap, or when it has been sitting attached
// for a while and something was raised on another surface's watch.
//
// AN ERROR IS AN ERROR HERE and not an empty list, for [Client.StandingItems]'
// reason: "nothing is waiting" and "the far end did not answer" are different
// facts, and a surface that drew the second as the first would be quietly
// telling a person there is nothing to answer.
func (c *Client) HeldQuestions() ([]HeldQuestion, error) {
	payload, err := c.call(nil, MethodHeldQuestions, nil)
	if err != nil {
		return nil, err
	}
	var held []HeldQuestion
	if err := json.Unmarshal(payload, &held); err != nil {
		return nil, err
	}
	return held, nil
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

// SubmitStanding is Submit for a draft the person marked as something to keep
// true. It rides the same method as an ordinary send with one flag on it, for
// the reason [SubmitArgs.Standing] states: the two turns differ only in what the
// ENGINE puts in front of the sentence, which is not a thing a wire can carry
// halfway.
func (a *Agent) SubmitStanding(ctx context.Context, text string) (<-chan session.Event, error) {
	return a.open(ctx, MethodSubmit, SubmitArgs{Text: text, Standing: true})
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

// ResolveStanding answers one standing card: set it up, set it up once, or a
// correction in the person's own words.
//
// IT IS THE METHOD THAT MAKES A STANDING CARD ANSWERABLE OVER A CONNECTION.
// internal/tui3's standing.go asserts an OPTIONAL interface on whatever agent it
// is holding ([standingAgent]) and draws no chips at all for one that does not
// implement it, so a remote handle without this would have shown the person a
// proposal they could look at and could not answer. Adding it here is the whole
// of the difference.
func (a *Agent) ResolveStanding(id uint64, answer session.StandingAnswer) {
	_, _ = a.c.call(nil, MethodStandingResolve, StandingArgs{ID: id, Answer: answer})
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

// EarlierHistory is the conversation above the session's latest compaction and
// where the pass's rewritten copy of it ends, which is what the surface scrolls
// back into. Empty for a session that has never been compacted, and empty for a
// connection that has dropped — the same answer as every other read on this
// agent, and the honest one either way: what cannot be fetched cannot be drawn,
// and an empty region leaves the transcript drawn exactly as it always was.
func (a *Agent) EarlierHistory() session.EarlierHistory {
	payload, err := a.c.call(nil, MethodEarlier, nil)
	if err != nil {
		return session.EarlierHistory{}
	}
	var history session.EarlierHistory
	_ = json.Unmarshal(payload, &history)
	return history
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
	// seen is the highest [Frame.Seq] this stream has QUEUED FOR THE SURFACE,
	// and it is the whole of the replay law stated at the top of this file: an
	// event at or below it has already been drawn once and is dropped. It is
	// also what a redial's [Hello.Resume] cursor carries, so the number the
	// engine resumes from is the number a person actually saw.
	seen uint64
	// count is how many events have been queued for the surface, ever. It is
	// what a resumed stream is watched by (redial.go's [Client.watchTail]),
	// because "has this turn said anything since the link came back" is a
	// question [stream.seen] cannot answer about an engine that does not
	// number its events.
	count uint64
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
//
// AND AN EVENT THIS STREAM HAS ALREADY DELIVERED IS DROPPED TOO, which is the
// law the file header states: a replay after a redial overlaps by however much
// the two ends disagree about, and drawing that overlap would repeat a turn's
// text on the screen. Seq zero is an engine that does not number and is always
// delivered — see the header for why that is not a duplicate.
func (s *stream) push(seq uint64, payload json.RawMessage) {
	if len(payload) == 0 {
		return
	}
	var wired EventWire
	if err := json.Unmarshal(payload, &wired); err != nil {
		return
	}
	if seq != 0 {
		s.mu.Lock()
		already := seq <= s.seen
		if !already {
			s.seen = seq
		}
		s.mu.Unlock()
		if already {
			return
		}
	}
	s.deliver(wired.Unwire())
}

// cursor is how far the surface got and whether this turn is still open — the
// pair a redial's [Hello.Resume] is made of. A stream that has closed is not
// asked about again: it ended, and the transcript is the authority on a turn
// that ended.
func (s *stream) cursor() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen, !s.closed
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
	s.count++
	s.mu.Unlock()
	s.wake.Signal()
}

// delivered is how many events this stream has handed the surface.
func (s *stream) delivered() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
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
