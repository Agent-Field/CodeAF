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
	"github.com/Agent-Field/aforge-v2/internal/store"
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
// NO GETTER MEASURES ANYTHING EXTRA. Every getter is one frame out and one
// frame back. [Client.Ping] is the explicit exception: one empty call on the
// surface's five-second clock, never a second call hidden behind a getter. The
// surface asks Model() on frames it repaints, so a measurement there would have
// doubled the cost of drawing a status line.

// callDeadline is how long any one call waits for its result. See the law above.
const callDeadline = 10 * time.Second

// taskCallDeadline is the shaper's own bounded wait plus room for the two wire
// frames around it. It is derived from the engine's limit so the connection
// cannot declare a healthy shaping call dead before the engine gives up.
const taskCallDeadline = session.TaskShapeWindow + 5*time.Second

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

	// facts is the engine's own account of itself, kept fresh by PUSH and read
	// by every getter a frame or a keystroke asks. It has a lock of its own
	// rather than living under mu because it is read from the update loop on
	// every repaint and mu is held by the reader goroutine's routing — see
	// replica.go for the whole of why this exists.
	facts replica

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

	// driver is who holds the keyboard, as the engine last told this surface.
	// It is set from the welcome and moved by every "driver" frame, and it is
	// the ONLY thing on this side that answers the question — a client that
	// worked it out for itself would be a second authority on a fact that can
	// only have one (driver.go).
	driver Driver
	// driverWake is closed and replaced whenever [Client.driver] changes, which
	// is how a surface drawing on a frame clock learns that an answer arrived
	// on the wire with no keystroke behind it. It is a channel rather than a
	// callback because the surface's loop is a select and a callback would be
	// the wire calling into a draw.
	driverWake chan struct{}

	// tasks is the surface's standing task lane — the far conversation's rail,
	// arriving unasked. It is ONE at a time and replaced rather than added to
	// (tasklane.go's [Agent.WatchTaskUpdates]), and nil is a surface that draws
	// no tasks or a connection that has ended.
	tasks *stream

	// designs is version 11's harness lane, held on exactly the terms tasks is:
	// one at a time, replaced rather than added to, and nil for a surface that
	// draws no cards or a connection that has ended (clientlanes.go).
	designs *stream

	// titles is the naming lane, held on exactly the terms designs is: one at a
	// time, replaced rather than added to, and nil for a surface that does not
	// draw the conversation's name or a connection that has ended
	// (clientlanes.go).
	titles *stream

	// following carries the turns this surface did not start, so the screen can
	// draw one. It is BUFFERED AND DROPS WHEN FULL: the reader goroutine must
	// never block, and a surface that is not draining this is one that does not
	// want it — the events themselves are queued on the stream regardless, and
	// the transcript is the authority on a turn that ended.
	following chan Following

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
	// answered says this came back as a FRAME from the far machine — a payload
	// or its own refusal — rather than being made up here when the connection
	// died under a call that was already outstanding ([Client.bury]).
	//
	// IT IS THE DIFFERENCE BETWEEN "IT SAID NO" AND "NOBODY KNOWS", and only
	// this end can see it: both arrive at the caller as an error, and a caller
	// that may ask again has to be able to tell a decision from a silence
	// ([Client.callAnswered]).
	answered bool
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
	// THIS MACHINE'S NAME IS FILLED IN HERE AND NOT AT THE DOOR, so that no
	// door can forget it: --host, --at and every test all reach this one
	// constructor, and a hello without a name is a screen on the far side
	// saying `another window` about a machine in another building. A door that
	// wants to say something else still can — a name already set is kept.
	if strings.TrimSpace(hello.Surface) == "" {
		hello.Surface = MachineName()
	}
	hello.Encodings = []string{frameEncodingGzip}
	return &Client{
		host:    strings.TrimSpace(host),
		hello:   hello,
		calls:   map[uint64]chan result{},
		streams: map[uint64]*stream{},
		done:    make(chan struct{}),
		stop:    make(chan struct{}),

		driver:     Driver{Yours: true},
		driverWake: make(chan struct{}),
		following:  make(chan Following, followingRoom),
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
	if err := expandFrame(&frame); err != nil {
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
	if welcome.Encoding != "" && welcome.Encoding != frameEncodingGzip {
		return Welcome{}, spokenError{reason: fmt.Sprintf("%s selected a frame encoding this build cannot read", c.where())}
	}
	c.mu.Lock()
	first := c.welcome.Version == 0
	c.welcome = welcome
	c.mu.Unlock()
	// THE FIRST WELCOME AND THE STREAM ARE TWO ROADS FOR THE SAME QUESTION.
	// The surface draws Held separately; suppress its copies in the initial
	// replay without skipping the other events or losing the stream cursor.
	// A redial keeps the existing surface and does not redraw Held, so its
	// newly missed questions must still arrive through the replay.
	if first {
		for _, question := range welcome.Held {
			if key, ok := heldKeyOf(question.Event.Event); ok && question.Stream != 0 {
				stream := c.stream(question.Stream)
				stream.mu.Lock()
				if stream.inWelcome == nil {
					stream.inWelcome = make(map[heldKey]struct{})
				}
				stream.inWelcome[key] = struct{}{}
				stream.mu.Unlock()
			}
		}
	}
	// A TURN ALREADY RUNNING WHEN THIS SURFACE ARRIVED IS ONE IT DID NOT START
	// EITHER, and it reaches the screen by the same road. It carries no sentence:
	// the message that opened it is in the journal, which this surface reads on
	// its way in ([Turn.Said] states which of the two moments needs one).
	if welcome.Live != 0 {
		c.follows(Following{Events: c.stream(welcome.Live).events()})
	}
	// The welcome's word on the keyboard is a driver frame by another road, and
	// it goes through the same door so that a redial that came back as a watcher
	// wakes the surface exactly as a live hand-over would.
	c.drives(welcome.Driver)
	// THE FIRST FRAME IS DRAWN FROM MEMORY. The welcome carries the fact set, so
	// a surface that has only just arrived already knows the model, the name,
	// what has been spent and what the conversation weighs — and a redial fills
	// it again, which is how a replica that went stale behind a dead link comes
	// back current without anybody asking a question (replica.go).
	c.facts.fill(welcome.Facts)
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
	//
	// AND IT IS ALSO HOW THIS SURFACE KNOWS IT HAS BEEN HERE BEFORE. A welcome
	// already in hand is exactly "I have attached to this conversation once",
	// which is what [Hello.Back] means: do not move the keyboard onto me, I am
	// a link coming back and not a person arriving.
	if strings.TrimSpace(open) != "" {
		hello.Session = open
		hello.Back = true
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

// followingRoom is how many unclaimed turns this client will hold for a surface
// that has not asked for them yet. A conversation runs one turn at a time, so
// anything past a couple is a surface that has stopped reading.
const followingRoom = 8

// Following is a turn this surface did not start: the sentence that opened it,
// where the engine sent one, and the channel its events arrive on.
//
// THE CHANNEL IS THE SAME KIND OF CHANNEL A SUBMIT ANSWERS WITH, on purpose and
// for [Client.Live]'s reason: a window watching somebody else's turn should draw
// it with the code that draws every turn, and the only thing it lacks is the
// [StreamRef] it would have got from opening it.
type Following struct {
	// Said is the message that opened the turn, empty when the transcript
	// already has it — see [Turn.Said].
	Said string
	// Events is that turn, arriving.
	Events <-chan session.Event
}

// Follow is the turns started by some other window on this conversation.
//
// IT IS WHAT MAKES A SECOND WINDOW A WINDOW AND NOT A DEAD FRAME. A surface that
// is not holding the keyboard is still watching the work, and the work is a turn
// somebody started somewhere else.
func (c *Client) Follow() <-chan Following { return c.following }

// follows offers one turn to whoever is watching, and drops it when nobody is
// keeping up. See [Client.following] for why dropping is the right failure.
func (c *Client) follows(turn Following) {
	select {
	case c.following <- turn:
	default:
	}
}

// Driver is who holds the keyboard on this conversation right now, as the
// engine last said. It answers from memory with nothing on the wire behind it,
// because the surface asks it on the draw path (internal/tui3's watcher line).
//
// A CLIENT WITH NO ENGINE BEHIND IT YET SAYS `Yours`. That is the honest
// default for the one instant it covers — before the first welcome there is no
// room to be a watcher in — and every road after it is an answer the engine
// gave.
func (c *Client) Driver() Driver {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.driver
}

// DriverChanged is closed the next time the answer to [Client.Driver] moves.
//
// IT IS HOW A HAND-OVER REACHES A SCREEN WITH NOBODY TOUCHING THE KEYBOARD. The
// other machine took the keyboard; nothing happened on this one; and the surface
// still has to stop drawing a composer this instant. So the wait is a channel
// the surface can sit in a select on, replaced rather than reused so a waiter
// that arrives late gets the NEXT change and never a stale one.
func (c *Client) DriverChanged() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.driverWake
}

// drives records who the engine says is driving and wakes whoever is waiting on
// it. An answer that did not change wakes nobody — a surface repainting on every
// restatement of the same fact would be a terminal blinking at a frame nobody
// sent.
func (c *Client) drives(note Driver) {
	c.mu.Lock()
	if c.driver == note {
		c.mu.Unlock()
		return
	}
	c.driver = note
	// The waiters are woken by the CLOSE of the channel they are holding, and
	// the next ones wait on a fresh one. A channel that was sent on instead
	// would wake exactly one waiter, and there is no rule saying only one thing
	// ever watches this.
	woken := c.driverWake
	c.driverWake = make(chan struct{})
	c.mu.Unlock()
	close(woken)
}

// Take asks for the keyboard. It is one round trip and it does not refuse —
// see [MethodTake] for why a person pressing enter on their own work is not a
// thing the engine weighs.
func (c *Client) Take() error {
	payload, err := c.call(nil, MethodTake, nil)
	if err != nil {
		return err
	}
	// The answer is the ENGINE'S word on who drives now, taken from the call
	// this surface made rather than raced against the frame the rest of the room
	// gets. A client that set the field itself would be the second authority
	// this whole lane exists to avoid.
	var note Driver
	if err := json.Unmarshal(payload, &note); err != nil {
		return err
	}
	c.drives(note)
	return nil
}

// Ping measures one empty call to the engine and back.
//
// THE CLOCK STAYS ON THIS MACHINE. Two hosts need not agree about the time,
// while the elapsed time around one call is exactly the path a keystroke and
// its answer use. A reconnecting client refuses before [Client.call], so the
// gentle meter on the surface never adds traffic to a link already trying to
// find its way back.
func (c *Client) Ping() (time.Duration, error) {
	c.mu.Lock()
	if c.reconnecting {
		reason := c.roamingRefusal()
		c.mu.Unlock()
		return 0, errors.New(reason)
	}
	if c.dead != nil {
		dead := c.dead
		c.mu.Unlock()
		return 0, dead
	}
	c.mu.Unlock()

	started := time.Now()
	if _, err := c.call(nil, MethodPing, nil); err != nil {
		return 0, err
	}
	return time.Since(started), nil
}

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
	if err != nil {
		return err
	}
	return flushFrame(conn)
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
		if err := expandFrame(&frame); err != nil {
			c.bury(err)
			return
		}
		switch frame.Kind {
		case "result":
			c.deliver(frame)
		case "event":
			c.stream(frame.ID).push(frame.Seq, frame.Payload)
		case "closed":
			c.stream(frame.ID).finish()
		case "turn":
			var turn Turn
			if err := json.Unmarshal(frame.Payload, &turn); err == nil && turn.Stream != 0 {
				c.follows(Following{Said: turn.Said, Events: c.stream(turn.Stream).events()})
			}
		case "driver":
			var note Driver
			if err := json.Unmarshal(frame.Payload, &note); err == nil {
				c.drives(note)
			}
		case string(laneTitle):
			c.titleFrame(frame.Payload)
		case string(laneDesign):
			// One event off the harness lane: a design card, a subharness intake
			// card, or a note about one. Queued for the surface's loop for the
			// reason a task frame is — the surface DRAWS them — and a lane nobody
			// is holding drops its frames (clientlanes.go).
			c.laneFrame(laneName(frame.Kind), frame.Payload)
		case "task":
			// One task update off the far conversation's standing lane — a node
			// admitted, running, or come home. It is the one push that is NOT
			// taken on the reader goroutine's own terms: the surface draws rows
			// from it, so it is queued onto the lane and drained by the surface's
			// loop, exactly as a turn's events are (tasklane.go).
			c.taskFrame(frame.Payload)
		case "facts":
			// The engine stating something nobody asked for. It is taken on the
			// reader goroutine before the surface is notified of a changed name.
			// Reading the replica never waits for the update loop to catch up.
			c.factsFrame(frame.Payload)
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
		waiting <- result{err: errors.New(frame.Error), answered: true}
		return
	}
	waiting <- result{payload: frame.Payload, answered: true}
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
	// AND THE KEYBOARD COMES BACK TO A SURFACE WHOSE CONNECTION DIED. There is
	// no room left to be a watcher in, the screen is about to say the connection
	// is gone, and a composer replaced by a line about another machine would be
	// a second, wronger sentence in front of the first. Whoever was waiting on
	// the change is woken so the frame that says it is drawn now.
	c.driver = Driver{Yours: true}
	woken := c.driverWake
	c.driverWake = make(chan struct{})
	close(c.done)
	c.mu.Unlock()
	// Nothing is roaming any more either: this is the end, and a backoff still
	// counting down behind it would dial a machine whose conversation has
	// already been declared gone on the screen.
	c.stopOnce.Do(func() { close(c.stop) })
	close(woken)

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
	// AND THE RAIL'S LANE ENDS WITHOUT AN ERROR EVENT ON IT. A task lane is not
	// a turn: nothing on it is mid-sentence, the rows it drew are still true of
	// the far machine, and the one sentence about a connection that died belongs
	// to the connection and is already being drawn. So it simply closes, and the
	// surface reads that as the lane it no longer has (tasklane.go).
	c.buryTasks()
	c.buryLanes()
}

// call is one round trip: a frame out, a result back, or the deadline.
func (c *Client) call(ctx context.Context, method string, args any) (json.RawMessage, error) {
	return c.callWithin(ctx, method, args, callDeadline)
}

func (c *Client) callWithin(ctx context.Context, method string, args any, deadline time.Duration) (json.RawMessage, error) {
	payload, _, err := c.callAnswered(ctx, method, args, deadline)
	return payload, err
}

// callAnswered is [Client.callWithin] with the one fact a caller that may ASK
// AGAIN cannot do without: whether the engine answered this call at all.
//
// THE TWO FAILURES ARE NOT THE SAME FAILURE. A refusal came back from the far
// machine — it read the call, decided, and said so — and repeating it sends the
// same words at the same closed door. A link that died, a deadline that ran
// out, a write onto a pipe that had already gone: those are calls whose fate
// nobody here knows, and the work behind them may be entirely done. Only the
// caller can decide what to do about the second kind, and it cannot decide
// anything while the two arrive as one error (internal/session's
// [session.ErrSendUnanswered] is what a surface reads that difference through).
func (c *Client) callAnswered(ctx context.Context, method string, args any, deadline time.Duration) (json.RawMessage, bool, error) {
	var payload json.RawMessage
	if args != nil {
		encoded, err := json.Marshal(args)
		if err != nil {
			// Nothing was written, so nothing crossed: this one IS decided here.
			return nil, true, err
		}
		payload = encoded
	}
	id := c.seq.Add(1)
	waiting := make(chan result, 1)

	c.mu.Lock()
	if c.dead != nil {
		dead := c.dead
		c.mu.Unlock()
		// Nothing was written onto a connection that is already gone, so this
		// call's own fate is not in doubt: it did not happen.
		return nil, true, dead
	}
	// A CALL MADE IN THE GAP IS REFUSED, NOT QUEUED. There is no pipe to write
	// it onto, and holding it until one exists would turn a keystroke into a
	// thing that hangs for as long as the redialling takes. The refusal says
	// what is happening and that it is worth trying again, which is the truth:
	// every getter on this client is asked again on the next frame, and a
	// message the person typed is still in the composer.
	if c.reconnecting {
		c.mu.Unlock()
		// Refused rather than queued, so nothing crossed and this one is decided.
		return nil, true, errors.New(c.roamingRefusal())
	}
	c.calls[id] = waiting
	c.mu.Unlock()

	if err := c.write(Frame{Kind: "call", ID: id, Method: method, Payload: payload}); err != nil {
		c.mu.Lock()
		delete(c.calls, id)
		roaming := c.roam != nil && !c.closing
		c.mu.Unlock()
		// A WRITE THAT FAILED IS NOT A CALL THAT DID NOT HAPPEN. The frame may
		// have gone onto the pipe in part or in whole before the error, so what
		// this reports is the honest unknown rather than a refusal.
		//
		// A write that failed on a roaming client is the link dying a moment
		// before the reader noticed it. The person is about to see
		// `reconnecting`, so this call says the same thing rather than the
		// sentence that means it is over.
		if roaming {
			return nil, false, errors.New(c.roamingRefusal())
		}
		return nil, false, c.gone(err)
	}
	c.made.Add(1)

	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case answer := <-waiting:
		return answer.payload, answer.answered, answer.err
	case <-ctx.Done():
		// The caller walked away from a call that is still out there.
		c.forget(id)
		return nil, false, ctx.Err()
	case <-timer.C:
		// AND THE DEADLINE IS THE UNKNOWN ITSELF. The engine may be working on
		// this call right now; what ran out is this end's patience.
		c.forget(id)
		return nil, false, c.gone(errors.New("no answer"))
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
// World is the engine machine's places root, walked: what home lists, what the
// tasks place reads its rows out of, and what the standing, spend and search
// places each take one fact from ([MethodPlacesWorld]).
//
// THE ERROR IS ANSWERED AND NOT SWALLOWED, unlike [Client.Recent] next door,
// and the difference matters: a recent-sessions list that came back empty is a
// picker with no rows, which is a small wrong. A WORLD that came back empty is
// every place on the surface saying this machine has nothing on it — so the
// caller has to be able to tell "the engine has no world door" and "the call
// failed" from "there is genuinely nothing there", and only an error can carry
// the first two (cmd/aforge's [hostWorld] is what does the telling).
func (c *Client) World() (session.World, error) {
	payload, err := c.call(nil, MethodPlacesWorld, nil)
	if err != nil {
		return session.World{}, err
	}
	var world session.World
	if err := json.Unmarshal(payload, &world); err != nil {
		return session.World{}, err
	}
	return world, nil
}

// TaskRecord is ONE ROW of the engine machine's record, read deeper than
// [Client.World] reads it: the last thing that piece of work said, and whether
// its journal is still on that machine's disk ([MethodPlacesTask]).
//
// THE ERROR IS ANSWERED AND NOT SWALLOWED, for [Client.World]'s reason narrowed
// to one card: a record that came back empty is a piece of work that said
// nothing at the end, and a call that failed is a card that has not been told
// yet. The surface draws a different line for each, and only an error can carry
// the second.
func (c *Client) TaskRecord(uri string, tail int) (session.TaskRecord, error) {
	payload, err := c.call(nil, MethodPlacesTask, PlacesTaskArgs{Transcript: uri, Tail: tail})
	if err != nil {
		return session.TaskRecord{}, err
	}
	var record session.TaskRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return session.TaskRecord{}, err
	}
	return record, nil
}

func (c *Client) Ledger(since time.Time) (LedgerReading, error) {
	payload, err := c.call(nil, MethodPlacesLedger, LedgerArgs{Since: since})
	if err != nil {
		return LedgerReading{}, err
	}
	var out LedgerReading
	err = json.Unmarshal(payload, &out)
	return out, err
}

func (c *Client) SearchConversations(terms string, limit int) ([]store.ConversationHit, error) {
	payload, err := c.call(nil, MethodPlacesSearch, SearchArgs{Terms: terms, Limit: limit})
	if err != nil {
		return nil, err
	}
	var out []store.ConversationHit
	err = json.Unmarshal(payload, &out)
	return out, err
}

func (c *Client) Archive(dir string, archived bool) error {
	_, err := c.call(nil, MethodPlacesArchive, ArchiveArgs{Dir: dir, Archived: archived})
	return err
}

func (c *Client) Snapshot(limit int) (store.MemoryShelves, error) {
	var out store.MemoryShelves
	payload, err := c.call(nil, MethodMemorySnapshot, limit)
	if err == nil {
		err = json.Unmarshal(payload, &out)
	}
	return out, err
}

func (c *Client) ChangedSince(at time.Time) (int, int, error) {
	payload, err := c.call(nil, MethodMemoryChanged, at)
	if err != nil {
		return 0, 0, err
	}
	var out MemoryChange
	if err := json.Unmarshal(payload, &out); err != nil {
		return 0, 0, err
	}
	return out.Learned, out.LetGo, nil
}

func (c *Client) ListMemories(scope string, limit int) ([]store.Memory, error) {
	var out []store.Memory
	payload, err := c.call(nil, MethodMemoryList, MemoryListArgs{Scope: scope, Limit: limit})
	if err == nil {
		err = json.Unmarshal(payload, &out)
	}
	return out, err
}

func (c *Client) UpdateMemory(id, title, text string, tags []string) error {
	_, err := c.call(nil, MethodMemoryUpdate, MemoryUpdateArgs{ID: id, Title: title, Text: text, Tags: tags})
	return err
}
func (c *Client) ForgetMemory(id string) error {
	_, err := c.call(nil, MethodMemoryForget, id)
	return err
}
func (c *Client) RestoreMemory(id string) error {
	_, err := c.call(nil, MethodMemoryRestore, id)
	return err
}
func (c *Client) MemoryProvenance(id string) (string, string, time.Time, error) {
	payload, err := c.call(nil, MethodMemoryProvenance, id)
	if err != nil {
		return "", "", time.Time{}, err
	}
	var out MemoryOrigin
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", "", time.Time{}, err
	}
	return out.Session, out.Title, out.At, nil
}

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

// StandingWatch reads the scheduler on the engine machine.
func (c *Client) StandingWatch() (standing.WatchStatus, bool) {
	payload, err := c.call(nil, MethodStandingWatch, nil)
	if err != nil {
		return standing.WatchStatus{}, false
	}
	var result StandingWatchResult
	if json.Unmarshal(payload, &result) != nil {
		return standing.WatchStatus{}, false
	}
	return result.Status, result.Known
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
	// A SWAP DOES NOT MOVE THE KEYBOARD, and the fresh welcome says who has it
	// so that a surface reading this one does not forget what the last one told
	// it. It goes through the same door a live hand-over does.
	c.drives(welcome.Driver)
	// A SWAP IS A NEW CONVERSATION, so the replica is refilled rather than
	// updated: everything in it — the name, the spending, the weight — belonged
	// to the session that just closed.
	c.facts.fill(welcome.Facts)
	return welcome, nil
}

// ── the agent ───────────────────────────────────────────────────────────────

// Agent is the engine's session as internal/tui3 sees it: every method of
// tui3.Agent, plus the rewind pair that surface type-asserts for
// (internal/tui3's rewind.go). It holds no state of its own — it is a handle on
// whichever session the engine currently has open, which is why /new and
// /resume keep using the same one.
type Agent struct{ c *Client }

// TaskRoom reads the bounded tail of a node whose record URI may not exist yet.
func (a *Agent) TaskRoom(id uint64, tail int) (session.TaskRecord, error) {
	payload, err := a.c.call(nil, MethodTaskRoom, TaskRoomArgs{ID: id, Tail: tail})
	if err != nil {
		return session.TaskRecord{}, err
	}
	var record session.TaskRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return session.TaskRecord{}, err
	}
	return record, nil
}

// SteerTask carries a correction to the engine's node and keeps its whole
// receipt: delivered, delivered-and-woke, or held on the task's record while its
// work is being checked (internal/session's [session.SteerReceipt]).
//
// It is the unnamed send — nothing about it can be recognised if it is sent
// twice — and it stays because callers that have no way to number their sends
// still have to be able to steer. [Agent.SteerTaskFrom] is the one a surface
// uses.
func (a *Agent) SteerTask(id uint64, line string) (session.SteerReceipt, error) {
	return a.steerWith(TaskSteerArgs{ID: id, Text: line})
}

// SteerTaskFrom carries the same correction WITH THE SURFACE'S OWN NAME FOR THE
// SEND on it, so that a crossing this end never heard the answer to can be
// asked again without the worker being corrected twice ([TaskSteerArgs] states
// the whole law).
//
// A SEND WITH NO IDENTITY TAKES THE UNNAMED DOOR, exactly as the local engine's
// does: an empty [session.SteerSource] is a caller saying it cannot name this
// send, and inventing one here would be this end promising a guarantee its
// caller cannot keep.
// THE CONVERSATION IT WAS WRITTEN FOR CROSSES WITH IT and is checked there
// ([TaskSteerArgs.Session]), because this handle keeps pointing at the engine
// after /resume or /new have changed which conversation is open behind it.
func (a *Agent) SteerTaskFrom(id uint64, line string, from session.SteerSource) (session.SteerReceipt, error) {
	if from.Scope == "" || from.Seq == 0 {
		return a.SteerTask(id, line)
	}
	// A BOUND SEND IS NOT PUT ON THE WIRE UNLESS THE FAR END ENFORCES THE BINDING.
	// An engine built before [Welcome.SteerOwner] reads Session as an unknown
	// field and delivers anyway, so transmitting here would risk the correction
	// landing in whatever conversation that engine now has open. Nothing crosses;
	// the caller keeps the words ([session.ErrConversationUnchecked]).
	if from.Conversation != "" && !a.c.Welcome().SteerOwner {
		return session.SteerReceipt{}, session.ErrConversationUnchecked
	}
	return a.steerWith(TaskSteerArgs{
		ID: id, Text: line,
		Scope: from.Scope, Seq: from.Seq, Said: from.At,
		Session: from.Conversation,
	})
}

// SteerRepeatKnown answers for THE MACHINE AT THE OTHER END, off what it said
// at the door ([Welcome.SteerRepeat]) — a fact this end could not otherwise
// know until it had already asked twice.
//
// AND IT IS RE-READ RATHER THAN REMEMBERED. /new, /resume and a reconnect all
// replace the welcome, and the engine behind it can change with them.
func (a *Agent) SteerRepeatKnown() bool { return a.c.Welcome().SteerRepeat }

// steerWith is the one crossing both doors take.
//
// A CALL NOBODY ANSWERED IS MARKED AS ONE. It is the only error on this door
// that a caller may respond to by sending the same words again, so it arrives
// wearing [session.ErrSendUnanswered] rather than as bare text, and every other
// failure stays the engine's own sentence exactly as it always was.
func (a *Agent) steerWith(args TaskSteerArgs) (session.SteerReceipt, error) {
	payload, answered, err := a.c.callAnswered(nil, MethodTaskSteer, args, callDeadline)
	if err != nil {
		if !answered {
			return session.SteerReceipt{}, unanswered{said: err}
		}
		return session.SteerReceipt{}, err
	}
	var steered TaskSteered
	if err := json.Unmarshal(payload, &steered); err != nil {
		return session.SteerReceipt{}, err
	}
	// A REFUSAL, AND A DELIVERY DID NOT HAPPEN. The words are still the caller's
	// to keep; what they may not do is aim them at this id again here.
	if steered.Elsewhere {
		return session.SteerReceipt{}, session.ErrNotThatConversation
	}
	// AND AN OUTCOME NOBODY CAN NAME KEEPS THE SEND. It reads exactly as a call
	// nobody answered, because that is what the caller must do with it.
	if steered.Uncertain {
		return session.SteerReceipt{}, unanswered{said: errors.New(steerUncertainWord)}
	}
	receipt := session.SteerReceipt{
		Waiting:   steered.Waiting,
		Held:      steered.Held,
		Direction: steered.Direction,
		Landing:   steered.Landing,
		Again:     steered.Again,
	}
	// An engine too old to send its own sentence still gets one, in the words the
	// local door would have used for the same fact.
	if strings.TrimSpace(receipt.Landing) == "" && !receipt.Held {
		receipt.Landing = session.SteerDelivered(receipt.Waiting)
	}
	return receipt, nil
}

// unanswered carries a call nobody answered while answering
// errors.Is([session.ErrSendUnanswered]). It keeps its OWN sentence rather than
// wrapping with %w, for [session.ErrNobodyToRead]'s reason: the sentence is
// shown to a person, and a wrap would append the sentinel's words to a line
// that already says them.
type unanswered struct{ said error }

func (e unanswered) Error() string { return e.said.Error() }
func (e unanswered) Unwrap() error { return session.ErrSendUnanswered }

// steerUncertainWord is what the engine's own unknown outcome reads as here. It
// is spelled on this side because the frame carries the FACT and not a sentence
// ([TaskSteered.Uncertain]).
const steerUncertainWord = "the engine could not say whether that correction was kept"

// Cancel asks the engine to stop the prefixed work id and keeps its sentence.
func (a *Agent) Cancel(id string) (string, error) {
	payload, err := a.c.call(nil, MethodTaskStop, TaskStopArgs{ID: id})
	if err != nil {
		return "", err
	}
	var stopped TaskStopped
	if err := json.Unmarshal(payload, &stopped); err != nil {
		return "", err
	}
	return stopped.Line, nil
}

// StartTask commissions the work on the engine machine and returns its receipt:
// id, title, and the fallback note the engine's shaper asked the surface to
// carry on the started row (empty whenever nothing was cut).
func (a *Agent) StartTask(ctx context.Context, brief string) (uint64, string, string, error) {
	payload, err := a.c.callWithin(ctx, MethodTaskStart, TaskStartArgs{Brief: brief}, taskCallDeadline)
	if err != nil {
		return 0, "", "", err
	}
	var started TaskStarted
	if err := json.Unmarshal(payload, &started); err != nil {
		return 0, "", "", err
	}
	return started.ID, started.Title, started.Note, nil
}

// StartPlannerRun opens the adaptive form on the engine machine.
func (a *Agent) StartPlannerRun(ctx context.Context, brief, hint string) (string, string, error) {
	payload, err := a.c.callWithin(ctx, MethodPlannerStart, PlannerStartArgs{Brief: brief, Hint: hint}, taskCallDeadline)
	if err != nil {
		return "", "", err
	}
	var started PlannerStarted
	if err := json.Unmarshal(payload, &started); err != nil {
		return "", "", err
	}
	return started.ID, started.Title, nil
}

// JudgeDecomposable asks the engine's model because the surface's model and
// credentials are not facts about this conversation.
func (a *Agent) JudgeDecomposable(ctx context.Context, brief string) (bool, []string, string) {
	payload, err := a.c.call(ctx, MethodTaskJudge, TaskStartArgs{Brief: brief})
	if err != nil {
		return false, nil, ""
	}
	var judged TaskJudged
	if json.Unmarshal(payload, &judged) != nil {
		return false, nil, ""
	}
	return judged.Parallel, judged.Parts, judged.Why
}

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

// Steer puts words into the running turn on the engine machine and returns the
// same live tail the local agent returns. It is its own method because FollowUp
// promises a later turn while Steer promises the next step boundary of this
// one; making the far end infer which was meant would erase the person's
// intent at the wire.
func (a *Agent) Steer(text string) (<-chan session.Event, error) {
	return a.open(nil, MethodSteer, SubmitArgs{Text: text})
}

// open is the stream-opening calls' one body: the call, the [StreamRef] it
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

// WorkOutlivesExit says whether this conversation keeps working once the view
// goes. It is [Welcome.Persistent] — the engine's own statement of its lifetime,
// which is the only honest source: a conversation hosted by the daemon on this
// laptop names no machine at all, so nothing about the transport or the host
// name can be read for it.
func (a *Agent) WorkOutlivesExit() bool { return a.c.Welcome().Persistent }

// Detach lets go of this VIEW of the conversation, which is what a terminal
// closing means: the window is gone and the work need not be.
//
// Against a session host it sends [MethodDetach] — nothing interrupted, nothing
// closed, the connection ended by the far side's reader loop with the turn left
// to finish — because [MethodClose] would end a running task on behalf of
// somebody who only shut a window. Against a one-shot engine, whose whole life
// is this pipe, leaving IS ending, so the interrupt and the flush stand.
//
// Every call below is bounded ([Client.call] carries callDeadline) and safe on a
// dead connection, so this cannot hold a quit open.
func (a *Agent) Detach() error {
	if a.WorkOutlivesExit() {
		_, err := a.c.call(nil, MethodDetach, nil)
		return err
	}
	a.Interrupt()
	return a.Close()
}

// Model is the model the next request will use.
//
// IT IS A MEMORY READ. The engine states this at the door and again whenever it
// moves, so the status line asks nothing (replica.go states the whole law, and
// PERF.md pins it: a View over --host issues zero far calls).
func (a *Agent) Model() string { return a.c.facts.read().Model }

// SetModel swaps it, and the surface's own copy moves with it.
//
// THE LOCAL WRITE IS NOT A SECOND AUTHORITY. The engine announces the change to
// every surface on this conversation, with a revision that lands over the top of
// what is assumed here; the assumption only covers the round trip, which is the
// gap in which a person who pressed a key is looking at the row it changed.
func (a *Agent) SetModel(model string) {
	a.c.facts.setModel(model)
	_, _ = a.c.call(nil, MethodSetModel, model)
}

// SetContextWindow is deliberately a no-op here. The surface's catalog belongs
// to the laptop; SetModel makes the engine consult its own catalog and move its
// own compaction point. The method remains on the interface for local agents
// and on the version-5 wire for compatibility with builds already in flight.
func (a *Agent) SetContextWindow(int) {}

// ReasoningFor is how hard one model is asked to think.
//
// IT IS THE READ THAT MADE THIS WHOLE FILE NECESSARY. internal/tui3's view.go
// asks it on every frame it draws — the level rides the model segment of the
// status row — so as a round trip it set the repaint rate of the terminal to the
// round-trip time of the link. The engine pushes the whole level map, keyed the
// way internal/session keys it, and this is a lookup in it.
func (a *Agent) ReasoningFor(model string) string {
	return a.c.facts.read().LevelFor(model)
}

// ReasoningLevels is every level this conversation holds, in one answer.
//
// IT IS THE DOOR THAT MAKES THE SURFACE'S OWN TABLE COMPLETE AT BOOT
// (internal/tui3's reasoninglevel.go). Asked one model at a time, a picker
// drawing a three-hundred-row catalog had three hundred questions to get
// through; the engine states the whole map instead, so this is one memory read
// of the replica and the surface has nothing left to discover.
func (a *Agent) ReasoningLevels() map[string]string {
	held := a.c.facts.read().Reasoning
	if len(held) == 0 {
		return nil
	}
	// A COPY, for [session.Agent.ReasoningLevels]' reason: the map inside the
	// replica is replaced under a lock by every push, and a caller ranging over
	// the live one while a fact frame lands is a race.
	levels := make(map[string]string, len(held))
	for model, level := range held {
		levels[model] = level
	}
	return levels
}

// SetReasoningFor sets it, moving the surface's own copy on [Agent.SetModel]'s
// terms — the picker's ctrl+t reads the level back the moment it sets one.
func (a *Agent) SetReasoningFor(model, level string) {
	a.c.facts.setLevel(model, level)
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

// Title is the name the session gave itself, read from memory. The engine
// states it when the naming errand settles, which is the only moment it ever
// changes — so a surface that has one has the one the session earned, and one
// that has none is looking at a conversation that has not earned one yet.
func (a *Agent) Title() string { return a.c.facts.read().Title }

// Usage is the session's running total, read from memory. The engine states it
// at every turn end, ahead of the EventTurnDone that the surface settles on
// (server.go's emit), so the figures a settle reads are that turn's.
func (a *Agent) Usage() session.Usage { return a.c.facts.read().Spent }

// ContextTokens is what the conversation weighs right now, read from memory. It
// is stated at a turn end and after a compaction — the two moments the figure
// moves — which is exactly where internal/tui3 asks for it (app.go's
// measureContext, which is written never to ask on the frame clock).
func (a *Agent) ContextTokens() int { return a.c.facts.read().ContextTokens }

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

// THE FOUR STRING GETTERS USED TO SHARE A ROUND TRIP AND NO LONGER MAKE ONE.
// Model, Title, Usage and ContextTokens were each one frame out and one frame
// back, drawn on a status line with no way to report a failure — so the empty
// string was the honest answer to a link that had stopped answering. They read
// the replica now (replica.go), and the failure they used to swallow is the one
// a status line should show anyway: a connection that is gone says so on its own
// row ([Client.LinkNote], [Client.Err]) rather than by drawing a conversation
// with no model.
//
// The engine still SERVES those methods (wire.go, server.go) and must: they are
// the protocol's doors, and nothing here decides what another build asks for.

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
	// inWelcome identifies questions already handed to this surface outside
	// the stream. It lasts only as long as this turn's stream does.
	inWelcome map[heldKey]struct{}
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
	if key, question := heldKeyOf(wired.Event); question {
		s.mu.Lock()
		_, inWelcome := s.inWelcome[key]
		s.mu.Unlock()
		if inWelcome {
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
