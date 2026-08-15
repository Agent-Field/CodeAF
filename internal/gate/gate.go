// Package gate is the commit gate: the one seam every workforce tool call
// crosses on its way to [store.RequestCommand].
//
// A tool call does not mutate the graph the instant the model emits it. It is
// STAGED here, a countdown runs, and only on expiry is the command actually
// requested. A cancel before expiry kills the staging outright — the store
// never hears about it. Staging, cancellation and firing are announced through
// one callback so the surface can render a countdown card and journal the
// three moments; this package owns none of that and imports no UI.
//
// The point of the seam is that the pause is CODE. Asking a model to wait
// before it commits is a behaviour, and behaviours drift; a timer does not.
// The duration is a setting, the audit trail is complete, and the window in
// which a person can say "no, stop" is the same length every single time.
package gate

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// DefaultTTL is the countdown a Commander uses when none is configured. Five
// seconds is long enough to read a card and reach for a key, short enough that
// an unattended surface is not a queue of commands waiting on a human.
const DefaultTTL = 5 * time.Second

// ErrNotStaged is returned by Cancel and FireNow for a sequence number that is
// not currently staged — never was, or has already fired or been cancelled.
// The distinction does not matter to a caller: either way there is nothing
// left to act on, and racing a countdown to zero is an ordinary outcome rather
// than a fault.
var ErrNotStaged = errors.New("gate: not staged")

// RequestFunc is the shape of [store.Store.RequestCommand]. The gate wraps one
// of these rather than a *store.Store so a test — and the surface's own fakes —
// can watch what would have been committed without a database.
type RequestFunc func(store.Command) (store.Command, error)

// Staged is one command waiting out its countdown.
type Staged struct {
	// Seq is this Commander's own number for the staging, unrelated to the
	// journal sequence the command receives if and when it fires. It exists
	// because a staged command has no journal identity yet — that is the whole
	// point of staging — and the surface still needs a handle to cancel by.
	Seq       int
	Command   store.Command
	StageTime time.Time
	TTL       time.Duration
}

// Deadline is when the countdown reaches zero.
func (s Staged) Deadline() time.Time { return s.StageTime.Add(s.TTL) }

// Remaining is how much of the countdown is left at now, floored at zero so a
// renderer never has to reason about a negative width.
func (s Staged) Remaining(now time.Time) time.Duration {
	left := s.Deadline().Sub(now)
	if left < 0 {
		return 0
	}
	return left
}

// lineInstructionBytes bounds the instruction in a one-liner. It is a card
// row, not a transcript; the full words are in Command.Instruction for anyone
// who wants them.
const lineInstructionBytes = 72

// Line is the human one-liner for a countdown card: what kind of command this
// is, what it acts on, and the opening of what was asked.
func (s Staged) Line() string {
	parts := []string{string(s.Command.Kind)}
	if target := strings.TrimSpace(s.Command.Target); target != "" {
		parts = append(parts, target)
	}
	head := strings.Join(parts, " ")
	instruction := clip(firstLine(s.Command.Instruction), lineInstructionBytes)
	if instruction == "" {
		return head
	}
	return head + " — " + instruction
}

// EventKind names the three moments of a staging plus the one failure.
type EventKind string

const (
	// EventStaged is emitted before the countdown is observable anywhere else,
	// and always before this staging's Fired — even at ttl 0, where the fire is
	// already racing the callback. A surface that learned about a command only
	// as it fired could not have drawn the card it was supposed to offer.
	EventStaged EventKind = "staged"
	// EventCancelled means the command was killed inside its window and was
	// never requested.
	EventCancelled EventKind = "cancelled"
	// EventFired means the wrapped RequestFunc accepted the command; Result
	// carries what it returned, journal sequence and all.
	EventFired EventKind = "fired"
	// EventFailed means the countdown ran out and the store refused. The
	// staging is gone either way — a refusal is an answer, not a reason to keep
	// waiting — and Err says why.
	EventFailed EventKind = "failed"
)

// Event is one announcement about a staging. Staged is always populated; the
// other fields belong to particular kinds.
type Event struct {
	Kind   EventKind
	Staged Staged
	// Result is what RequestFunc returned, on EventFired only.
	Result store.Command
	// Err is why the request was refused, on EventFailed only.
	Err error
}

// entry is a staging plus the machinery that makes it fire.
type entry struct {
	staged Staged
	timer  *time.Timer
	// ready is closed once the Staged event has been announced. The timer
	// goroutine waits on it, which is what makes Staged-before-Fired an
	// ordering guarantee rather than a timing hope at short TTLs. Stage closes
	// it on the way out — including on a panic from the callback — so nothing
	// can be left blocked on it.
	ready chan struct{}
}

// Commander stages commands and fires them when their countdowns expire.
// Safe for concurrent use.
type Commander struct {
	fn      RequestFunc
	ttl     time.Duration
	onEvent func(Event)

	mu     sync.Mutex
	next   int
	staged map[int]*entry
}

// New builds a Commander over fn. A ttl of zero or less means DefaultTTL is
// not wanted rather than unset — the caller gets no countdown at all, which is
// the honest reading of "the gate is configured to zero seconds" and what a
// surface with the setting turned off should get. Pass DefaultTTL explicitly
// for the default. onEvent may be nil, and is called off the lock, so it may
// call back into the Commander.
func New(fn RequestFunc, ttl time.Duration, onEvent func(Event)) *Commander {
	if ttl < 0 {
		ttl = 0
	}
	return &Commander{fn: fn, ttl: ttl, onEvent: onEvent, staged: map[int]*entry{}}
}

// TTL is the countdown this Commander gives every staging.
func (c *Commander) TTL() time.Duration { return c.ttl }

// Stage validates a command, starts its countdown and returns the handle to
// cancel it by. It never blocks on the countdown: the caller — a tool call
// inside a model turn — returns to the model immediately, and the commitment
// happens later or not at all.
func (c *Commander) Stage(command store.Command) (int, error) {
	if err := Validate(command); err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.next++
	seq := c.next
	staged := Staged{Seq: seq, Command: command, StageTime: time.Now(), TTL: c.ttl}
	item := &entry{staged: staged, ready: make(chan struct{})}
	item.timer = time.AfterFunc(c.ttl, func() {
		<-item.ready
		c.fire(seq)
	})
	c.staged[seq] = item
	c.mu.Unlock()

	// After the unlock, and before the timer is allowed to run: the callback is
	// free to re-enter Stage, Cancel or FireNow on this same Commander.
	defer close(item.ready)
	c.emit(Event{Kind: EventStaged, Staged: staged})
	return seq, nil
}

// Cancel kills a staging inside its window. Cancelling something that already
// fired, or was never staged, is ErrNotStaged.
func (c *Commander) Cancel(seq int) error {
	c.mu.Lock()
	item, ok := c.staged[seq]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("%w: %d", ErrNotStaged, seq)
	}
	delete(c.staged, seq)
	// Stop's answer is deliberately ignored. If the timer has already fired,
	// its goroutine will look the staging up under this same lock, find it
	// gone, and do nothing — the map is the authority on whether a command is
	// still live, not the timer.
	item.timer.Stop()
	c.mu.Unlock()

	c.emit(Event{Kind: EventCancelled, Staged: item.staged})
	return nil
}

// FireNow skips the rest of the countdown. The command fires on the timer's
// own goroutine, exactly as an expiry would, so a person hurrying a command
// along and a person letting it lapse produce the same sequence of events.
func (c *Commander) FireNow(seq int) error {
	c.mu.Lock()
	item, ok := c.staged[seq]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("%w: %d", ErrNotStaged, seq)
	}
	item.timer.Reset(0)
	c.mu.Unlock()
	return nil
}

// Pending is every staging still counting down, oldest first, for the surface
// to render. The commands are copies; nothing here aliases live state.
func (c *Commander) Pending() []Staged {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := make([]Staged, 0, len(c.staged))
	for _, item := range c.staged {
		pending = append(pending, item.staged)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Seq < pending[j].Seq })
	return pending
}

// fire commits one staging, if it is still live.
func (c *Commander) fire(seq int) {
	c.mu.Lock()
	item, ok := c.staged[seq]
	if !ok {
		// Cancelled between the timer going off and this line. The cancel wins:
		// the window is the person's, and the last word inside it is theirs.
		c.mu.Unlock()
		return
	}
	delete(c.staged, seq)
	c.mu.Unlock()

	// The request runs off the lock. It talks to a database and can take as
	// long as it likes; other stagings must still be able to start, cancel and
	// expire while it does.
	fn := c.fn
	if fn == nil {
		c.emit(Event{Kind: EventFailed, Staged: item.staged, Err: errors.New("gate: no request func")})
		return
	}
	result, err := fn(item.staged.Command)
	if err != nil {
		c.emit(Event{Kind: EventFailed, Staged: item.staged, Err: err})
		return
	}
	c.emit(Event{Kind: EventFired, Staged: item.staged, Result: result})
}

func (c *Commander) emit(event Event) {
	if c.onEvent == nil {
		return
	}
	c.onEvent(event)
}

// ── validation ──────────────────────────────────────────────────────────────

// Validate is the store's own admission rules for a command request, applied
// before a countdown starts rather than after it ends. A command that the
// store would refuse must be refused to the model's face, in the tool result
// it can still do something about — not five seconds later, as a failure
// notice on a card the person already watched tick away.
//
// The store re-runs all of this at fire time and remains the authority; this
// copy is allowed to be the more permissive of the two but never the stricter,
// which is why an unknown kind is the only kind-level judgement made here.
func Validate(command store.Command) error {
	kind := store.CommandKind(strings.TrimSpace(string(command.Kind)))
	if kind == "" {
		return fmt.Errorf("gate: %w: empty kind", store.ErrInvalid)
	}
	if !validKind(kind) {
		return fmt.Errorf("gate: %w: unknown kind %q", store.ErrInvalid, command.Kind)
	}
	if strings.TrimSpace(command.Instruction) == "" {
		return fmt.Errorf("gate: %w: empty instruction", store.ErrInvalid)
	}
	if command.Reflex && (kind != store.CommandSplice || strings.TrimSpace(command.Target) != "") {
		return fmt.Errorf("gate: %w: reflex must be an untargeted splice", store.ErrInvalid)
	}
	if kind != store.CommandSplice && !globalKind(kind) && strings.TrimSpace(command.Target) == "" {
		return fmt.Errorf("gate: %w: %s requires a target", store.ErrInvalid, kind)
	}
	return nil
}

// validKind mirrors the store's validCommandKind, which is unexported. The
// duplication is deliberate and one-directional: a kind the store learns and
// this list has not is a command that stages, fires and is judged by the store
// — the correct failure — whereas guessing kinds are fine here would let the
// surface promise a countdown for something that can never commit.
func validKind(kind store.CommandKind) bool {
	switch kind {
	case store.CommandSplice, store.CommandAmend, store.CommandCancel, store.CommandRedirect,
		store.CommandExpedite, store.CommandPause, store.CommandResume, store.CommandReprioritize,
		store.CommandRestart, store.CommandSetModel,
		store.CommandCharterRatify, store.CommandCharterPause, store.CommandCharterRetire,
		store.CommandCharterCadence, store.CommandCharterWording, store.CommandCharterOnce,
		store.CommandCharterFire, store.CommandCharterDecline, store.CommandCharterAlways,
		store.CommandCharterNever, store.CommandCharterProbation,
		store.CommandServiceStop, store.CommandServiceRestart, store.CommandServiceAutoRestart,
		store.CommandCraftRun, store.CommandCraftRevert, store.CommandCraftRetire, store.CommandSkillRetire,
		store.CommandStandingWatchEnable, store.CommandStandingWatchDecline,
		store.CommandHandover, store.CommandHeadInterrupt:
		return true
	default:
		return false
	}
}

// globalKind mirrors the store's isGlobalCommand: the kinds that act on the
// run itself and so name no node.
func globalKind(kind store.CommandKind) bool {
	switch kind {
	case store.CommandStandingWatchEnable, store.CommandStandingWatchDecline,
		store.CommandHandover, store.CommandHeadInterrupt:
		return true
	default:
		return false
	}
}

// ── text ────────────────────────────────────────────────────────────────────

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return strings.TrimSpace(text[:index])
	}
	return text
}

// clip bounds a string to n bytes on a rune boundary, marking the cut.
func clip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := n - len("…")
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return text[:cut] + "…"
}
