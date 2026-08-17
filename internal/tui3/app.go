package tui3

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// frameInterval is the repaint ceiling: at most one frame is BUILT per 33ms,
// no matter how many deltas arrive inside it. A provider that streams token by
// token would otherwise re-wrap the live paragraph a hundred times a second to
// show the reader thirty of them, and the thirty it showed would be the same
// thirty.
//
// It is also the only clock on this surface. The spinner and the ellipsis step
// on multiples of it (see spinnerStep, pulseStep) rather than on schedules of
// their own: two animation clocks means two wakeups per second per animation
// and rows that drift out of phase with each other.
const frameInterval = 33 * time.Millisecond

// markdownThrottle is how often a streaming reply's settled prefix is promoted
// from plain wrapped text to rendered markdown. See [app.assistantRows].
const markdownThrottle = 1500 * time.Millisecond

// usageEvery is how many frames pass between asks for the session's running
// cost. The agent answers under a lock, and a lock taken thirty times a second
// to move a figure that changes once a turn is a lock taken for nothing.
const usageEvery = 10

// quietBeforeEllipsis is how long the stream has to be silent before the
// ellipsis appears under a reply that is already streaming. Text arriving in
// chunks a few hundred milliseconds apart is a working model, not a stalled
// one, and a dot that blinks in between every chunk is noise.
const quietBeforeEllipsis = 700 * time.Millisecond

// runState is the word in the status line.
type runState int

const (
	stateIdle runState = iota
	stateWorking
	stateInterrupted
)

func (s runState) String() string {
	switch s {
	case stateWorking:
		return "working"
	case stateInterrupted:
		return "interrupted"
	default:
		return "idle"
	}
}

// entryKind is what one block of the conversation is.
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryTool
	entryDivider
	// entryCompact is ONE compaction pass, from the moment it starts to the
	// moment it stops — a row with a clock rather than a mark left behind.
	//
	// It is a kind of its own rather than a divider written twice because a
	// compaction is the one thing this surface waits on that is neither the
	// model speaking nor a tool running: a paid summarizer call that can hold a
	// turn for ten seconds while the screen says nothing. It opens as a spinning
	// line ("compacting ~84k tokens · 6s") and SETTLES IN PLACE into the rule the
	// divider always drew, so the finished conversation reads exactly as it did
	// before this wave and the running one stops being a silence.
	entryCompact
	entryNote
	// entryThinking is one turn's reasoning (thinking.go): streamed while it
	// arrives, collapsed to a single row the moment the turn says anything else.
	entryThinking
	// entryTask is ONE TASK PROPOSAL — the decision moment, drawn where it
	// happened (task.go). It keeps its verdict afterwards, the way a consent
	// row does, because "the model asked to go and do this and you said yes" is
	// the only record of where a running node came from.
	entryTask
	// entryDone is ONE TASK THAT CAME HOME (taskdone.go): the card that lands
	// when a node reaches its final state, minutes after the turn that proposed
	// it and usually with no turn open at all.
	//
	// It is a kind of its own rather than the note it used to be for the reason
	// entryCompact is not a divider: a note is the surface muttering about its
	// own housekeeping, and this is the only statement of an outcome a person
	// delegated ten minutes of work to get.
	entryDone
	// entryConnect is ONE SIGN-IN (connect.go): the browser opening, the link
	// under it for whoever is not sitting at that browser, and the one line it
	// settles into.
	//
	// It is a kind of its own rather than a note for the reason entryCompact is
	// not a divider: a note is a static sentence, and this is a block that opens
	// waiting — with a spinner, and a link a person may need to copy — and closes
	// minutes later on an event nobody typed.
	entryConnect
)

// toolState is where one call is in its life, and it is the whole of what the
// left of a tool line says (toolview.go draws it):
//
//	◌  toolForming   the model is still SPELLING IT OUT — a dim pulse
//	◌  toolQueued    the model finished asking; NOTHING has started
//	?  toolConsent   it is blocked on a person — the question hue, and the row
//	                 with it
//	⠋  toolRunning   it is executing: the braille spinner, and only here
//	   toolOK        it finished, quietly, with its elapsed time
//	✗  toolFailed    it failed, loudly, and its detail is already open
//
// The middle two states are the fix for one defect: a mutating call sat spinning
// while the RESPONSE streamed (nothing had started), and a call waiting for an
// answer spun identically to one doing work. A spinner is a claim about
// motion, so it is now spent on motion alone.
//
// The zero value is toolQueued and that is deliberate: an entry built from an
// announcement carries no status field, and the only honest default for a call
// nobody has said anything else about is "asked for".
type toolState int

const (
	toolQueued toolState = iota
	toolConsent
	toolRunning
	toolOK
	toolFailed
	// toolForming is the phase BEFORE queued: the call is still ARRIVING, one
	// fragment at a time, and what is on screen is the model writing it rather
	// than anything the harness is doing (session.EventToolForming).
	//
	// It is LAST in this block rather than first, where the life of a call would
	// have put it, because the zero value above is load-bearing: a row built
	// from an announcement sets no status, and "forming" is the one state such
	// a row must never default to — it would claim the model is still typing a
	// call it has finished asking for.
	toolForming
)

// live reports whether a call has not resolved yet — queued, waiting on a
// person, or executing. Every "find the row this event is about" walk asks
// this rather than comparing against toolRunning, which was the whole test back
// when running was the only unresolved state there was.
//
// A FORMING CALL IS NOT LIVE, and that is the deliberate half of this: the
// walks that ask this question are looking for the row a consent question, a
// begin or an end belongs to, and every one of those events is about a call the
// model has FINISHED asking for. A half-sent call cannot be any of them, and a
// row that answered to them would take an event belonging to the call beside it.
func (s toolState) live() bool { return s == toolQueued || s == toolConsent || s == toolRunning }

// entry is one block of the conversation, and its rendered rows.
//
// Tool activity is an entry rather than a decoration on the assistant's block
// because it lands in place, between two paragraphs of a reply, and reads in
// the order it happened.
//
// The cached rows are the second half of the snappiness contract: an entry
// renders when its own content or the width changes, and a frame joins what is
// already there. Nothing in here holds a blank row — spacing is [app.layout]'s
// and only [app.layout]'s.
type entry struct {
	kind entryKind
	text string
	turn int

	// Tool fields.
	tool   string
	status toolState
	detail toolDetail
	open   bool // this call's expansion is showing inline
	// full lifts the expansion's per-tool cap: it is set by a click on the
	// "… N more lines" foot, which is the person saying they want the rest.
	full bool
	// decision is what the person answered when this call was asked about —
	// "allowed" or "denied", dim, beside the row's stat (consent.go). It is
	// empty for every call the policy did not stop.
	decision string

	// callID is the PROVIDER's id for this call (session.Event.CallID), taken
	// off the forming events and kept so the announcement lands on the row that
	// was already drawing the same call. It is empty for every row this surface
	// never saw form — a non-streaming provider announces calls with no id in
	// front of them — and empty on a forming row until the wire says one.
	callID string
	// bytes is how much of a FORMING call's arguments has arrived. It is the
	// row's only figure while nothing else about the call is known yet, and it
	// stops mattering the moment the call is whole.
	//
	// THE ARGUMENTS THEMSELVES ARE NOT KEPT. Half a JSON object is not a
	// payload; the size of it is a fact, and the fact is what the row shows
	// (session.go's ArgsText contract).
	bytes int

	// began and ended are the block's clock, and the two entry kinds that have
	// one read it the same way — the moment the thing started and the moment it
	// stopped:
	//
	//	entryThinking  the first and last reasoning delta (thinking.go)
	//	entryTool      EXECUTION started (EventToolBegin) and ended, which is
	//	               what the line's trailing "1.2s" measures. A call's
	//	               announcement is deliberately not in it: the time a
	//	               response spent streaming is not time the tool ran.
	//
	// settled collapses a thinking block and open expands it again.
	began, ended time.Time

	// latched says THE PERSON decided this block's expansion, rather than the
	// stream deciding it for them (thinking.go). It exists because a reasoning
	// block is the one thing on this surface whose open/closed state is written
	// by two authors: the reader, with ctrl+e or a click, and the turn, which
	// collapses the block the moment it says anything that is not reasoning.
	// Without the latch the second author always wins — a person opens a think
	// mid-stream, the next delta settles the block, and their choice is gone —
	// which is the defect this field exists to make impossible.
	latched bool

	// Assistant fields. settled means the block is finished and renders
	// through renderMarkdown; mdCut is how many bytes of a still-streaming
	// block have already been promoted to markdown.
	settled bool
	mdCut   int

	// card is the proposal this entry draws, for kind entryTask and for nothing
	// else (task.go). It is a POINTER because the answer lane holds the same
	// card: a row and the verdict on it must not be able to disagree.
	card *taskCard
	// conn is the sign-in this entry draws, for kind entryConnect and for
	// nothing else (connect.go). It is a POINTER for the reason the two cards
	// above are: the row opens waiting and a later event settles it in place, and
	// the row and that state must never be able to disagree.
	conn *connectCard
	// done is the card a landed node writes, for kind entryDone and for nothing
	// else (taskdone.go). Like [entry.card] it is a POINTER, because the card
	// carries the one piece of state a person can change about it — whether its
	// full context is showing — and the row and that state must never disagree.
	done *taskDone

	// The row cache. built distinguishes "no rows yet" from "renders to no
	// rows", which an empty slice cannot.
	rows  []string
	width int
	built bool
	stale bool
}

// forming reports whether this row is a call the model is STILL SPELLING OUT.
//
// The ended clock is what takes it back out again: a forming row whose stream
// died is resolved rather than removed ([app.dropForming]), and the row that is
// left says the call was cancelled — so "still arriving" has to mean the state
// AND an unstopped clock, or a dead row would keep pulsing at a model that has
// stopped speaking.
func (e *entry) forming() bool {
	return e.kind == entryTool && e.status == toolForming && e.ended.IsZero()
}

// The messages the surface moves on. Every stream message carries the
// generation of the stream it came from: a turn that was interrupted while its
// channel still had events in flight must not paint over the turn that
// replaced it.
type (
	submittedMsg struct {
		ch  <-chan session.Event
		err error
	}
	streamEventMsg struct {
		gen int
		ev  session.Event
	}
	streamClosedMsg struct{ gen int }
	compactedMsg    struct{ err error }
	// frameMsg is the paint clock: it promotes whatever streamed since the
	// last one into a frame, and steps the animations.
	frameMsg struct{}
	// gitMsg is what the workspace's repository answered (see [gitHead]). It is
	// a message rather than a call because `git status` on a large tree is tens
	// of milliseconds and the model loop is not a place to wait.
	gitMsg struct {
		branch string
		dirty  bool
		ok     bool
	}
	// taskEventMsg is one event off the STANDING task subscription (task.go),
	// which is a second stream and not the turn's: a node's "done" lands
	// minutes after the turn that proposed it, when there is no stream left to
	// land on. Its generation is the same device the turn stream's is — a lane
	// belonging to an agent that has been replaced must not paint into the one
	// that replaced it.
	taskEventMsg struct {
		gen int
		ev  session.Event
	}
	taskLaneClosedMsg struct{ gen int }
	// wokenMsg is one turn THE SESSION STARTED ON ITS OWN, arriving as the
	// stream it will speak on (followup.go). It is the turn stream's shape and
	// not the standing lane's: what comes off the wake lane is a channel, and
	// the events are on it.
	wokenMsg struct {
		gen int
		ch  <-chan session.Event
	}
	wakeLaneClosedMsg struct{ gen int }
	// The THIRD lane on this surface (room.go): one node's own events, while a
	// person is standing in its room. It is neither the turn's stream nor the
	// standing task subscription — those carry what the CONVERSATION is doing and
	// what a node's life has come to; this carries what one node is doing right
	// now, and it exists only for as long as somebody is watching.
	roomEventMsg struct {
		gen int
		ev  session.Event
	}
	roomClosedMsg struct{ gen int }
	// The FOURTH lane, and the only one nobody asked for: one watcher per
	// RUNNING node, held for as long as the node runs, so the rail can say what
	// a node is doing and for how long without a person having to walk into its
	// room to find out (task.go's [taskPilot]). Its generation is per-pilot,
	// because pilots come and go with the nodes and the surface holds several.
	taskPilotMsg struct {
		gen int
		id  uint64
		ev  session.Event
	}
	taskPilotClosedMsg struct {
		gen int
		id  uint64
	}
	// The two messages one sign-in takes (connectpanel.go). Both are messages
	// rather than calls for the reason [gitMsg] is: BeginAuth reaches the
	// network and Wait blocks until somebody finishes in a browser, and the
	// model loop is not a place to do either.
	connectFlowMsg struct {
		service string
		name    string
		flow    *connect.Flow
		err     error
	}
	connectResultMsg struct {
		service string
		name    string
		status  connect.Status
		err     error
	}
	// hudFadeMsg is a BOUNDED catch-up tick: the two wakeups a settled turn
	// schedules so its fresh numbers can go quiet on time (render.go's fade).
	// There is deliberately no idle ticker behind it — a surface with nothing
	// happening on it wakes twice and then stops.
	hudFadeMsg struct{}
)

type app struct {
	ctx   context.Context
	agent Agent
	fresh func() (Agent, string, error)
	// workspace is the directory this conversation is about, whole; place is
	// its base name, which is what the status line has room for. The whole path
	// is what history is keyed by and what the @ completion walks.
	workspace string
	place     string
	file      string
	resumed   bool

	entries []entry
	// live is the assistant entry currently being streamed into, or -1.
	live int
	// turn counts the person's messages. It groups tool calls into clusters
	// and is what ctrl+o folds and unfolds.
	turn int
	// unfolded holds the turns whose tool cluster is showing every call.
	unfolded map[int]bool
	// sel is the selected tool entry, or -1. ↑/↓ move it; enter opens it.
	sel int
	// hot is what the pointer is over (hover.go). The zero value is nothing.
	hot hoverAt

	state runState
	model string
	// title is the name the session gave itself, shown left of the model. Empty
	// until the session has one (session's title.go names it after the first
	// completed turn); a resumed session opens with the name it already had.
	title  string
	cost   float64
	tokens int
	// ctxWindow is the model's context in tokens as this surface last set it,
	// and ctxTokens what the conversation currently weighs. The pair is the
	// meter in the status line. The window is TRACKED rather than asked for
	// because session exposes no getter — the surface is the one that tells the
	// agent (see [app.switchModel]), so the surface is the one that knows.
	ctxWindow int
	ctxTokens int
	// inputTokens is the session's prompt-token total, and cacheRead/cacheWrite
	// its prompt-cache totals. The first two together are the status line's warm
	// share — the fraction of everything this session has sent that came off a
	// cache — and they are held apart rather than as a ratio because the two
	// provider dialects disagree about whether one contains the other
	// (session.Usage.CachedShare owns that reconciliation).
	//
	// cacheWrite is not drawn. It is kept beside its pair because "is the cache
	// working" is one question with two halves, and a surface holding only the
	// half it currently draws is one that has to go back to the session to
	// answer the obvious follow-up.
	inputTokens int
	cacheRead   int
	cacheWrite  int
	// cacheSaved is what those reads have been WORTH, in dollars, summed over
	// every turn that had a published price pair to compute it from (see
	// [app.cacheNote], which is the one place it grows). It is what turns the
	// warm share from a statistic into a fact about the bill: the percentage is
	// the hit RATE, this is what the rate MEANT.
	//
	// It is deliberately NOT derivable from the totals beside it. The price is a
	// property of the model that was answering at the time, and a session that
	// switched models halfway cannot be re-priced afterwards from one number —
	// so it is accumulated per turn, at the moment the price is known.
	cacheSaved float64

	// outputTokens is what the session has WRITTEN, and it is held apart from
	// [app.tokens] (which is input+output) for one reason: the burn rate. Tokens
	// per second is a claim about generation, and a figure with the prompt in it
	// would climb by twenty thousand the moment a large file was read.
	outputTokens int
	// turnBegan is when the turn now running started, and turnOutStart what
	// [app.outputTokens] read at that moment. The pair is the burn segment's
	// whole arithmetic: what this turn has written, over how long it has been
	// writing. Both are cleared when the turn settles — a rate quoted over a
	// finished turn is a rate nobody is watching.
	turnBegan    time.Time
	turnOutStart int
	// turnCostAt is what the session had spent when the turn now running
	// started, and it is the other end of the subtraction a turn footer's price
	// is (timestamps.go). It is kept beside the burn window's pair because it is
	// the same kind of fact — a reading taken at the turn's first moment so the
	// turn's own share can be told from the session's total.
	turnCostAt float64
	// stamps is one frozen receipt per finished turn, keyed by turn number, and
	// timestamps is the ui.timestamps rung deciding whether any of it is drawn.
	// Both are timestamps.go's.
	stamps     map[int]turnStamp
	timestamps string
	// ctxRing is the last [ctxRingSize] TURN-END context readings, oldest first.
	// It is the sparkline's data and the compaction ETA's, and it is sampled at
	// turn end rather than on the frame clock because that is the only moment
	// the figure means the same thing twice: mid-turn it climbs with every tool
	// result and falls back when the batch resolves.
	ctxRing []int
	// ringTurn is the turn the last reading was taken for — see
	// [app.sampleContext] for why a turn can end more than once.
	ringTurn int

	// branch is the git branch the workspace is on and branchDirty whether it
	// has uncommitted work — the right half of the input's legend (render.go).
	// Empty branch means "no answer", which is what a directory that is not a
	// repository, a git that is not installed and a probe that timed out all
	// look like from here.
	branch      string
	branchDirty bool
	// gitProbe is the seam onto that answer, so a test can pin a branch without
	// a repository and the surface can be driven with no git at all. Nil is
	// [gitHead].
	gitProbe func(dir string) (string, bool, bool)
	// home is what "~" abbreviates in the legend's path, read once at boot.
	home string
	// approval is the tool gate's blanket posture — "prompt", "allow", "deny" —
	// as the profile last said. It is on this surface for exactly one reason:
	// "allow" means nothing will ever be asked, and that is the one posture a
	// person must not be able to forget they are in (render.go's YOLO segment).
	approval string
	// mouse is whether the surface reports the mouse at all (config's ui.mouse
	// row): on buys hover and click, off hands every drag back to the
	// terminal, whose native selection is the more fundamental act. Read where
	// approval is read — boot and each turn end.
	mouse bool
	// released is that same handover, made for a moment instead of for good:
	// ctrl+s while the pointer is ours gives it to the terminal so a drag
	// selects text the way it does everywhere else, and the person's next
	// keystroke takes it back (copymode.go). It is a property of the FRAME —
	// [app.View] declares it every paint — so nothing has to be undone.
	released bool

	// The HUD's per-segment change clocks (render.go's [app.freshen]). segText
	// is what each segment last read and segAt when it last CHANGED, which is
	// the whole of the age fade: paint follows recency.
	segText [segCount]string
	segAt   [segCount]time.Time
	// modelSpan is where the model's name was last drawn on the status row, in
	// columns, and it is the whole of what makes that name PRESSABLE: written by
	// the layout, read by the click (render.go's [app.identityParts], and
	// [app.statusPress] below). An empty span means there is nothing to press.
	modelSpan hudSpan
	// stripSpans is where the task strip's chips were last drawn, and stripMore
	// the columns of its overflow mark — the same bargain modelSpan makes, for
	// the same reason: the row that lays the chips out is the row that knows
	// where they landed (taskstrip.go's [app.stripRow] and [app.stripPress]).
	stripSpans []stripSpan
	stripMore  hudSpan
	// jumpSpan is where the jump-to-latest chip was last drawn, in columns — the
	// same bargain again, for a chip that is right-aligned and so knows its own
	// columns only once the frame has chosen a width (jumpchip.go's
	// [app.jumpChip] and [app.jumpPress]).
	jumpSpan hudSpan

	// hud is the cached answer to the two questions the telemetry asks of the
	// whole conversation — how much background work is alive, and what the
	// session has written — and hudStale says the entries have moved since it
	// was computed. Both walks read every tool call's arguments, which is a JSON
	// parse per row: at thirty frames a second, on a session with four hundred
	// rows, that is the status line costing more than the conversation.
	hud      hudStats
	hudStale bool

	// stream is the channel being pumped and gen its generation. gen is
	// bumped by every Submit so that a late event from an abandoned stream can
	// be recognized and dropped.
	stream <-chan session.Event
	gen    int

	// lastDelta is when text last arrived, and mdAt when the live reply's
	// prefix was last promoted to markdown.
	lastDelta time.Time
	mdAt      time.Time

	// The paint clock. dirty says the row list no longer matches the entries;
	// painting says a frameMsg is already on its way, so a burst of deltas
	// schedules one tick and not one each. paints counts frames and drives
	// every animation on this surface; builds counts layouts and exists so a
	// test can assert the coalescing without sleeping.
	dirty    bool
	painting bool
	paints   int
	builds   int

	// rows is the last laid-out screen list, and rowsWidth the width it was
	// laid out for.
	rows      []row
	rowsWidth int

	width, height int
	offset        int
	stick         bool

	pal   palette
	input editor
	// pick is the model overlay (palette.go). Closed, it costs the frame
	// nothing; open, it owns the keyboard and the bottom of the screen.
	pick picker
	// asks are the approval questions waiting for an answer, oldest first
	// (consent.go). While one is up it owns the keyboard: the draft below is
	// suspended untouched, exactly as the model picker suspends it.
	asks []ask
	// askAt is when the question at the head of that queue was RAISED, and it
	// is the near end of the countdown drawn on the offer line (consent.go).
	// askWait is how long that countdown runs — the setting, read at boot and
	// re-read at every turn end — and zero is a clock that is off. askPaused
	// says a key has been pressed since the question came up, which stops the
	// clock for good: a person who has touched the keyboard is a person who is
	// answering, and a prompt that expired under their hands would be the
	// surface deciding something they were in the middle of deciding.
	askAt     time.Time
	askWait   time.Duration
	askPaused bool
	// askTaps is where the question's answers were last drawn, in columns and
	// in rows of the block — the same bargain [app.modelSpan] and the strip's
	// chips make (taskstrip.go's [stripSpan]): the geometry is recorded at
	// layout, because a hit-test that recomputed it would be measuring a block
	// the frame has not drawn. It is what makes every answer a TAP as well as a
	// key, which is the whole of the phone sheet (consent.go).
	askTaps []consentTap
	// THE CONNECT SIDE (connect.go). connAsks are the offers waiting for an
	// answer, oldest first — a question about an ACCOUNT rather than about a
	// call, drawn one slot under the approval question and owning the keyboard
	// on the same terms. connTaps is where that offer's two answers were last
	// drawn, in columns, which is the bargain [app.askTaps] makes one block up.
	//
	// conns is the door onto the accounts themselves (Options.Connections) and
	// connPanel the list /connect opens over it. Nil conns is a surface that
	// cannot manage connections and says so; it does not stop the session's own
	// offer, which needs no handle.
	//
	// connNames is what a service is CALLED, keyed by the id every event
	// carries: the offer is the only event that names one, and the two that
	// follow it have to be able to say the word anyway.
	// connFlows are the sign-ins this surface is waiting on, keyed by service.
	// A flow is held only so it can be ABANDONED — the conversation being
	// replaced, or a second attempt at the same account — because a listener
	// nobody is going to answer is a listener outliving its reason.
	connAsks  []connAsk
	connTaps  []connTap
	conns     Connections
	connPanel connectPanel
	connNames map[string]string
	connFlows map[string]*connect.Flow
	// leftTap is when ← was last pressed over an empty box, and it is the whole
	// of the double-tap (room.go's [app.navBack]). One tap steps back a level;
	// two inside [navDoubleTap] go home.
	leftTap time.Time
	// guard is the question raised by steering a node that is not listening, or
	// nil (room.go). It holds the person's words while they say where those
	// words should go.
	guard *steerGuard

	// THE PASTE BRACKET. pasting says the terminal has opened one and not yet
	// closed it; pasted is what has arrived inside it; pasteAt is when the last
	// thing did, which is the only defence against a bracket that never closes.
	// See [app.paste] for what these three are for — it is the whole of the
	// paste fix, and it is not the obvious mechanism.
	pasting bool
	pasted  []rune
	pasteAt time.Time
	// follows are the messages typed with ctrl+q while a turn ran, each holding
	// the stream the turn it starts will speak on — and the woken turns waiting
	// on the same door, which are streams with no message at all (followup.go).
	follows []queued
	// wakeLane is the standing subscription to turns the session started ON ITS
	// OWN, and wakeGen the generation it belongs to. It is a lane of STREAMS
	// rather than of events (followup.go's wake lane), and its generation is the
	// same device the turn stream's is: a lane from an agent that has been
	// replaced must not start a turn in the one that replaced it.
	wakeLane <-chan (<-chan session.Event)
	wakeGen  int

	// THE TASK SIDE (task.go). task is the proposal that owns the answer lane,
	// or nil; tasks and taskOrder are the rail's nodes, keyed by id and kept in
	// admission order; taskSeen is the (id, state) de-dup, because an in-turn
	// update arrives on both the turn's stream and the standing one; taskLane is
	// that standing subscription and taskGen the generation it belongs to.
	task      *taskCard
	tasks     map[uint64]*taskNode
	taskOrder []uint64
	taskSeen  map[uint64]session.TaskState
	taskLane  <-chan session.Event
	taskGen   int
	// THE ROSTER'S OWN THREE FACTS (task.go's rail). railOpen holds the groups a
	// person has folded AGAINST their default — nil is the design as shipped, and
	// an absent key is a group that has never been touched, which is why this is a
	// map and not a bitfield. railTop is the window's offset into the roster's
	// line list, resolved by the same [listTop] every other list on this surface
	// scrolls with. railWhere is the focused row, named by (group, id) rather than
	// by index because work moves between groups while nobody is looking, and
	// railHold says the roster has been GIVEN the keyboard (ctrl+t) — without it
	// there is no cursor, and every key still belongs to the draft.
	railOpen  map[railGroup]bool
	railTop   int
	railWhere railSpot
	railHold  bool

	// pilots are the watchers on the nodes that are running right now, keyed by
	// id, and pilotGen the counter each one takes its generation from (task.go).
	// Empty is the ordinary state: nothing is running, so nothing is watched.
	pilots   map[uint64]*taskPilot
	pilotGen int
	// room is the node's page, when a person has walked into one (room.go), and
	// roomGen the generation of the lane feeding it. Nil is the ordinary state:
	// the body region is the conversation, and every geometric question about it
	// resolves through the transcript.
	room    *taskRoom
	roomGen int
	// roomPump is the command a freshly opened room's lane needs, PARKED rather
	// than returned.
	//
	// The reason is one call path this file cannot hand a command back through:
	// enter on a selected proposal row runs input.go's [app.enter] into
	// [app.openTool], which returns nothing at all. A door that worked when it
	// was clicked and did nothing when it was pressed would be the worse of the
	// two defects, so every door parks here and the program loop drains it.
	roomPump tea.Cmd
	// think is the reasoning block currently streaming, or -1 (thinking.go).
	think int

	// chips are the pictures attached to the message being written, drawn as a
	// tray above the box (attach.go). sent holds the ones a message in flight
	// took, so a refusal can put them back where the person left them.
	chips []chip
	sent  []chip

	// menu is the command list and comp the @ file completion — the two
	// overlays that open by TYPING rather than by a key (commands.go,
	// files.go). They are not modal: the draft under them keeps the keyboard.
	menu menu
	comp completion

	// history is the recall list, and hist the walk currently in it (recall.go).
	// Nil history is a surface with no ↑, which is what --no-history is.
	history History
	hist    recall

	// draftFile is where the unsent sentence is kept between sessions
	// (draft.go); empty means it is not kept at all.
	draftFile    string
	draftPending bool
	// models is the door's model list, asked for at the moment the picker
	// opens rather than at boot — a lazily warmed catalog may have arrived in
	// between, and it must never be waited for. Nil falls through to the cache
	// and the built-ins (see [app.modelList]).
	models func() []Model

	// sheet is the settings panel (settings.go): the one FULLSCREEN thing this
	// surface draws, and the only overlay that is modal for the pointer as well
	// as for the keyboard. Closed, it costs the frame nothing.
	sheet sheet
	// deck is the phone tier's status sheet (statusdeck.go): the SECOND
	// fullscreen thing, and the only one that exists at one size class only —
	// under sixty columns the status row is a two-row deck, and this is where
	// everything the deck could not hold is listed. Closed, it costs nothing.
	deck deckSheet
	// expand is the phone tier's tool detail (expand.go): the THIRD fullscreen
	// thing this surface draws, and it is fullscreen for the settings panel's
	// reason — a unified diff at forty-four columns needs every line the
	// terminal has. Closed, it costs the frame nothing, and it is only ever
	// opened at tierPhone.
	expand expand
	// profileDir is where the panel's writes land, and settings the registry it
	// edits. The registry is built at the first /settings rather than at boot —
	// it is a door onto a file, and a surface that may never be asked about
	// settings should not open one.
	profileDir string
	settings   *config.Settings
	// saveApproval and saveBashApproval are the door's write seams for the
	// consent card's "always" (consent.go). Nil is a surface that remembers an
	// answer for the session and no longer, which is what this card did before
	// they existed.
	saveApproval     func(tool string) error
	saveBashApproval func(command string) error

	// copy is the frozen viewport a person reads and yanks out of (copymode.go).
	// Closed, it costs the frame nothing.
	copy copyMode
	// rew is the rewind mode: the cut line through the transcript, the points it
	// can sit on, and the draft it is holding (rewind.go). Closed, it costs the
	// frame nothing.
	//
	// escArm is when the first esc landed, or zero — the door's other half, which
	// lives out here rather than inside the mode because it is a fact about the
	// mode being DOWN. rewSay is one sentence the mode could not act on
	// ("nothing to rewind") and rewSayAt when it was said; both run down on the
	// frame clock ([app.rewindSweep]), because this surface has one clock.
	rew      rewindMode
	escArm   time.Time
	rewSay   string
	rewSayAt time.Time
	// tmux says this surface is inside a multiplexer, so a clipboard write has
	// to be wrapped in its passthrough (copymode.go). It is read once, from
	// TERM, because a terminal does not change what it is mid-session.
	tmux bool

	// focused is whether the terminal window has the keyboard, and seenFocus
	// whether it has ever told us (notify.go). The pair is what decides whether
	// a finished turn is worth a notification: a person watching the screen does
	// not need to be told what they are looking at.
	focused   bool
	seenFocus bool

	// linear is the screen-reader tier (Options.Linear): one column, no
	// animation, no hover, ASCII markers. It is read by the rendering branches
	// that draw motion or shape, and by nothing else.
	linear bool

	// clock is where this surface reads the time, and nil is [time.Now].
	//
	// It exists for ONE fact that is now on screen: a running call's age
	// (toolview.go's count-up), which is the first thing this surface draws that
	// is a function of the wall clock rather than of what arrived. A test cannot
	// wait a minute to see "1m 5s", and a render that slept to be tested would
	// be a render tuned to a test.
	clock func() time.Time

	// welcome is the box an empty session opens with (welcome.go). It is the
	// only animation on this surface that is not a spinner, and it runs once.
	welcome welcome
	// roster is the resume picker: the same conversations the box lists, opened
	// on purpose and filterable (resume.go).
	roster roster
	// recentSessions answers the box's right column and the picker's rows, and
	// resume opens one of them. Both are nil on a surface the door did not wire,
	// and then the box says it has no sessions rather than pretending to have
	// lost them.
	recentSessions func() []Session
	resume         func(file string) (Agent, error)
}

func newApp(ctx context.Context, opts Options) *app {
	place := strings.TrimSpace(opts.Workspace)
	if place == "" {
		if cwd, err := os.Getwd(); err == nil {
			place = cwd
		}
	}
	a := &app{
		ctx:              ctx,
		agent:            opts.Agent,
		fresh:            opts.Fresh,
		workspace:        place,
		place:            filepath.Base(place),
		file:             opts.SessionFile,
		resumed:          opts.Resumed,
		models:           opts.Models,
		history:          opts.History,
		draftFile:        opts.DraftFile,
		ctxWindow:        opts.ContextWindow,
		profileDir:       opts.ProfileDir,
		settings:         opts.Settings,
		saveApproval:     opts.SaveApproval,
		saveBashApproval: opts.SaveBashApproval,
		recentSessions:   opts.RecentSessions,
		resume:           opts.Resume,
		conns:            opts.Connections,
		live:             -1,
		sel:              -1,
		think:            -1,
		unfolded:         map[int]bool{},
		stick:            true,
		width:            80,
		height:           24,
		pal:              detectPalette(),
		linear:           opts.Linear,
		tmux:             tmuxTerm(os.Getenv),
		// A terminal that has said nothing is assumed to HAVE the keyboard, which
		// is the quiet assumption: the cost of getting it wrong is a notification
		// nobody got, and the cost of the other default is a notification every
		// turn on a screen somebody is watching (notify.go).
		focused: true,
	}
	a.copy.mark = -1
	a.gitProbe = gitHead
	if home, err := os.UserHomeDir(); err == nil {
		a.home = home
	}
	// The gate's posture is read at boot and re-read at every turn end
	// ([app.settle]): a person who opens the settings panel and turns the asking
	// off sees the YOLO segment appear one turn later, which is soon enough for
	// a fact that only ever changes by hand.
	a.approval = readApproval(a.profileDir)
	a.mouse = config.MouseEnabledAt(a.profileDir)
	a.timestamps = config.TimestampsAt(a.profileDir)
	// And the approval countdown, on the same terms (consent.go).
	a.askWait = a.consentWait()
	if a.linear {
		// The linear tier is a palette question as well as an app one: the two
		// paints that mean motion and pointer stop, and the rail drops to the
		// ASCII it already had a spelling for.
		a.pal.linear, a.pal.ascii = true, true
	}
	if a.agent != nil {
		a.model = a.agent.Model()
		// A resumed session is already named, and the name is a fact about the
		// conversation on screen: it belongs in the first frame, not after the
		// next turn (session's title.go re-names nothing).
		a.title = strings.TrimSpace(a.agent.Title())
	}
	a.hudStale = true
	// The conversation that already happened is drawn BEFORE the surface says
	// anything of its own, so the notices below land where a person's eye
	// already is: at the bottom, next to the box.
	a.replay()
	// The box is decided HERE, between the replay and the first thing the
	// surface says of its own: "empty" has to mean "the conversation is empty",
	// and every line below this one is the surface talking (welcome.go).
	a.openWelcome()
	if a.linear {
		// The box still opens; it just opens FINISHED. Its arrival animation is
		// the one piece of motion on this surface that is not a spinner, and
		// linear mode's rule is the same for both.
		a.welcome.step = welcomeFrames
	}
	a.measureContext()
	if notice := strings.TrimSpace(opts.Notice); notice != "" {
		a.note(notice)
	}
	if a.resumed && a.file != "" {
		a.note("resumed " + a.file)
	}
	// The opening line says the two keys the status line has no room for. The
	// other two — /help and ctrl+o — moved to that line's right end this wave
	// and are on screen permanently, so repeating them here would be the surface
	// saying the same thing twice on the first frame of every session.
	a.note("esc or ctrl+c interrupts")
	a.restoreDraft()
	// LAST, because it reads the surface it opens over: the picker marks the
	// session this window is already in, and that is not known until the agent,
	// the file and the replay above have settled. A door that asked for it on a
	// machine with no conversations yet gets the empty state as a notice rather
	// than a list with nothing in it (resume.go).
	if opts.PickSession {
		a.openResume()
	}
	return a
}

var _ tea.Model = (*app)(nil)

// Init starts the paint clock when — and only when — the first frame has
// something to animate. That is the welcome box's arrival and nothing else: an
// idle surface with no box is a surface with no wakeups at all.
func (a *app) Init() tea.Cmd {
	// The repository is asked ONCE here and then only at turn ends. A branch is
	// a fact that changes when a person changes it, and a person who checks out
	// a branch mid-turn is between two turns by the time it matters.
	// THE STANDING TASK SUBSCRIPTION IS OPENED ONCE, HERE (task.go). It is not
	// the turn's stream and it never closes with one: a node proposed in this
	// turn reports minutes later, with no turn open, and the rail is the only
	// thing on screen that knows it is still alive.
	// THE WAKE LANE IS OPENED IN THE SAME BREATH (followup.go), and it is the
	// other half of the same fact: the node's landing reaches the rail on the
	// task lane, and what the session goes on to SAY about it reaches the
	// transcript on this one.
	if a.welcome.animating() {
		return tea.Batch(a.wake(), a.probeGit(), a.watchTasks(), a.watchWakes())
	}
	return tea.Batch(a.probeGit(), a.watchTasks(), a.watchWakes())
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// A zero size is a terminal that could not say — a headless boot, a
		// window being born. Keeping the last known size draws something;
		// taking the zero draws nothing.
		if msg.Width > 0 && msg.Height > 0 {
			a.width, a.height = msg.Width, msg.Height
			a.touch()
			a.clampScroll()
		}
		return a, nil

	case tea.KeyPressMsg:
		// A KEY INSIDE AN OPEN PASTE BRACKET IS TEXT, and it is read here, before
		// anything else, because the first key of a leaked paste is usually the
		// one that would do the damage (see [app.pasteKey]). It can also hand the
		// key BACK — an abandoned bracket — along with the command that spent
		// what the bracket had collected, which is why that command is carried
		// down every path below instead of being returned here.
		flushed, taken := a.pasteKey(msg)
		if taken {
			return a, flushed
		}
		// THE ROSTER READS NEXT, and only ever once it has been HANDED the
		// keyboard (ctrl+t, task.go). Explicit focus outranks ambient place: a room
		// is where a person is, the roster is what they just asked for, and esc
		// gives the keyboard back to whichever of the two is underneath.
		if cmd, took := a.railKey(msg); took {
			return a, tea.Batch(flushed, cmd)
		}
		// THE ROOM READS NEXT, and only ever while one is open (room.go). It has
		// to be read here rather than inside [app.key] because the two keys it
		// takes — esc to leave, enter to steer — belong to input.go, and it
		// restates that file's precedence law rather than jumping it: everything
		// that outranks the draft there outranks the room here.
		if cmd, took := a.roomKey(msg); took {
			return a, tea.Batch(flushed, cmd)
		}
		// A key can OPEN a room too — enter, on a selected proposal — down a path
		// that returns no command, so whatever that door parked is drained here.
		return a, tea.Batch(flushed, a.key(msg), a.takeRoomPump())

	case tea.FocusMsg:
		// The terminal reports focus (View asks for it in view.go), so the
		// notification has something honest to gate on — see notify.go.
		a.focused, a.seenFocus = true, true
		return a, nil

	case tea.BlurMsg:
		a.focused, a.seenFocus = false, true
		return a, nil

	case tea.PasteStartMsg:
		// The terminal said a paste is starting. Everything until the close is
		// text, whatever shape it arrives in.
		a.pasting, a.pasted, a.pasteAt = true, a.pasted[:0], a.now()
		return a, nil

	case tea.PasteMsg:
		// Bracketed paste, whole, in one message — the parser coalesced the keys
		// between the brackets for us, so the newlines inside it are text and not
		// a stack of enters. Inside an open bracket it JOINS what the bracket has
		// collected rather than landing on its own: the two forms can arrive in
		// the same paste, and one insert per paste is the contract.
		if a.pasting {
			a.pasted = append(a.pasted, []rune(msg.Content)...)
			a.pasteAt = a.now()
			return a, nil
		}
		return a, a.paste(msg.Content)

	case tea.PasteEndMsg:
		// The bracket closes, and everything inside it goes in as ONE edit: one
		// insert, one list sync, one debounce — the same door the coalesced form
		// goes through, so a terminal that leaks keys and a terminal that does
		// not produce the same draft.
		if !a.pasting {
			return a, nil
		}
		a.pasting = false
		text := string(a.pasted)
		a.pasted = a.pasted[:0]
		return a, a.paste(text)

	case filesLoadedMsg:
		a.comp.all, a.comp.loaded, a.comp.loading = msg.paths, true, false
		a.comp.rank()
		a.touch()
		return a, nil

	case tasksLoadedMsg:
		return a, a.tasksLoaded(msg.rows)

	case draftSaveMsg:
		return a, a.saveDraft()

	case tea.MouseWheelMsg:
		// COPY MODE OWNS THE WHEEL while it is up, because the viewport it froze
		// is the thing the wheel would otherwise move (copymode.go).
		if a.copy.on {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.copyScroll(-3)
			case tea.MouseWheelDown:
				a.copyScroll(3)
			}
			return a, nil
		}
		// The settings panel is modal for the pointer too: it is the whole
		// screen, so there is no conversation under it for a wheel to reach.
		if a.sheet.open {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.sheet.move(-3)
			case tea.MouseWheelDown:
				a.sheet.move(3)
			}
			a.touch()
			return a, nil
		}
		// And the status sheet, which is the same claim about the same kind of
		// surface (statusdeck.go). The wheel walks its cursor rather than an
		// offset of its own: the list is short enough that a scroll and a
		// selection are the same gesture.
		if a.deckShowing() {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.deckMove(-3)
			case tea.MouseWheelDown:
				a.deckMove(3)
			}
			return a, nil
		}
		// And the phone's tool detail is the same claim about the same kind of
		// overlay: it is the whole screen, and the wheel is what reads a diff
		// that does not fit on one (expand.go).
		if a.expandShowing() {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.expandScroll(-3)
			case tea.MouseWheelDown:
				a.expandScroll(3)
			}
			return a, nil
		}
		// The roster over the body is the same claim one step earlier: while it
		// is up the transcript is not on screen at all, and the roster's window
		// follows its focus rather than an offset of its own (task.go's
		// [app.railView]), so the wheel walks the cursor.
		if a.railFull() {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.railMove(-3)
			case tea.MouseWheelDown:
				a.railMove(3)
			}
			return a, nil
		}
		// The room is the body region while it is up, so the wheel is the room's:
		// a wheel that moved the transcript under it would scroll a list that is
		// not on screen (room.go).
		if a.roomOpen() {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.roomScroll(-3)
			case tea.MouseWheelDown:
				a.roomScroll(3)
			}
			return a, nil
		}
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			a.scroll(-3)
		case tea.MouseWheelDown:
			a.scroll(3)
		}
		return a, nil

	case tea.MouseClickMsg:
		if a.copy.on {
			// A click in copy mode acts on nothing: the rows under the pointer are
			// a FROZEN snapshot, and expanding a call in it would be expanding a
			// row that is no longer where the conversation says it is.
			return a, nil
		}
		if msg.Mouse().Button == tea.MouseLeft {
			if a.sheet.open {
				a.sheetPress(msg.Mouse().X, msg.Mouse().Y)
				return a, nil
			}
			// The status sheet is modal for the pointer at the same rung and for
			// the same reason: it is the whole screen, and a press outside its
			// list is how a finger closes it (statusdeck.go).
			if a.deckShowing() {
				a.deckSheetPress(msg.Mouse().X, msg.Mouse().Y)
				return a, nil
			}
			// The phone's tool detail takes every press on the frame while it
			// is up, the ones that land on its padding included: a gap that
			// fell through to the conversation underneath would be a tap that
			// expanded a call nobody can see (expand.go).
			if a.expandShowing() {
				a.expandPress(msg.Mouse().Y)
				return a, nil
			}
			// THE APPROVAL QUESTION IS READ FIRST OF THE FRAME'S OWN ROWS, which
			// is the pointer's half of the keyboard's order (input.go): a question
			// the SESSION is blocked on outranks every surface below it. It comes
			// after the three fullscreen overlays above for the reason
			// [app.consentPress] already refuses while the settings panel is up —
			// the block is not on the frame at all while one of them has it, so a
			// press resolved against it would answer a question nobody could see.
			// It claims the whole block and nothing else — a press on any other row
			// falls straight through, exactly as it did before there were targets
			// there.
			if a.consentPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// THE CONNECT OFFER IS READ NEXT, one rung under the approval
			// question for the reason it is drawn one row under it: both are
			// blocks the session is waiting on, and a call parked mid-batch is
			// the more urgent of the two (connect.go).
			if a.connectPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// AND THE CONNECTIONS PANEL TAKES EVERY PRESS WHILE IT IS UP, which
			// is what modal means for a pointer: a press on a row acts on that
			// row, and a press anywhere else closes the list (connectpanel.go).
			if a.connPanel.open {
				return a, a.connectPanelPress(msg.Mouse().Y)
			}
			// A chip is the one thing below the conversation a click can take
			// off, and it is the one thing down there that needs the COLUMN as
			// well as the row (attach.go).
			if a.chipPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// AND THE JUMP CHIP IS THE OTHER ONE, floating in the breathing gap
			// rather than in the box, and column-aware for the same reason: the
			// row it rides is empty everywhere else, and empty space on this
			// surface is not a gesture (jumpchip.go).
			if a.jumpPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// THE TASK STRIP IS READ BEFORE THE RAIL, because the strip spans the
			// WHOLE window and the rail claims every press in its own columns
			// whether or not one landed on a row (room.go) — asked the other way
			// round, a chip in the rail's columns would be swallowed by the column
			// under it (taskstrip.go).
			if cmd, took := a.stripPress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			// THE RAIL IS THE OTHER COLUMN-AWARE TARGET, and it is read before
			// the body for the same reason: the two are drawn side by side, so
			// which one was pressed is a question about x (room.go). A rail row
			// is a door into that node's room.
			if cmd, took := a.railPress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			// AND THE PROPOSAL'S CHOICES ROW IS THE THIRD, for the same reason
			// again: three answers share one line, so which was pressed is a
			// question about x (task.go). It is read before the body because a
			// click on that row answers the question rather than opening the
			// brief — which is what the rest of the card does with a press.
			if cmd, took := a.choicePress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			// AND THE MODEL SEGMENT IS THE FOURTH: the status row's identity
			// cluster carries the name of what is answering, and pressing a name
			// is how a person changes it (render.go's [app.identityParts]).
			if a.statusPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			opened := a.press(msg.Mouse().X, msg.Mouse().Y)
			// A press can open a room — a spawn card is a door now (room.go's
			// [app.openRoomAt]) — and a room that opened without its lane being
			// pumped is a page that never fills. The take is nil in every other
			// case, which is most of them. A press can also open a whole
			// CONVERSATION, from the welcome box's list, and that one arrives
			// carrying its own two lanes (welcome.go).
			return a, tea.Batch(opened, a.takeRoomPump())
		}
		return a, nil

	case tea.MouseMotionMsg:
		// Motion is the cheapest and commonest message this surface gets — a
		// pointer crossing the window sends one per cell — so [app.setHover]
		// repaints only when the row under it actually changed (hover.go).
		//
		// Two surfaces have no hover at all and drop it here rather than paying
		// for a hit-test per cell: the frozen viewport (nothing under the pointer
		// is actionable) and the linear tier (there is no pointer).
		if a.copy.on || a.linear {
			return a, nil
		}
		if a.sheet.open {
			a.sheetHover(msg.Mouse().Y)
			return a, nil
		}
		if a.deckShowing() {
			a.deckSheetHover(msg.Mouse().Y)
			return a, nil
		}
		// The phone's tool detail has no hover at all, by design: nothing on it
		// is revealed by a pointer, because the tier it is drawn for does not
		// have one (expand.go).
		if a.expandShowing() {
			return a, nil
		}
		a.setHover(msg.Mouse().X, msg.Mouse().Y)
		return a, nil

	case submittedMsg:
		return a, a.adopt(msg)

	case streamEventMsg:
		if msg.gen != a.gen {
			return a, nil
		}
		return a, a.event(msg.ev)

	case streamClosedMsg:
		if msg.gen != a.gen {
			return a, nil
		}
		a.stream = nil
		// The turn is over, so a message that was waiting for it starts now
		// (followup.go). Nil when nothing is queued.
		return a, tea.Batch(a.settle(), a.startFollow())

	case taskEventMsg:
		if msg.gen != a.taskGen {
			return a, nil
		}
		return a, a.taskEvent(msg.ev)

	case roomEventMsg:
		if a.room == nil || msg.gen != a.room.gen {
			return a, nil
		}
		return a, a.roomEvent(msg.ev)

	case roomClosedMsg:
		// The node reached its final state, so the lane ended. The room stays
		// open — a person reading what a task did is not finished reading because
		// the task is finished doing — and says so at its foot (room.go).
		if a.room == nil || msg.gen != a.room.gen {
			return a, nil
		}
		a.room.done, a.room.lane = true, nil
		// A call the node was still spelling out when its lane ended never
		// became one: the row says so and stops pulsing (room.go).
		a.roomDropForming()
		a.roomTouched()
		return a, a.wake()

	case taskPilotMsg:
		return a, a.pilotEvent(msg)

	case taskPilotClosedMsg:
		// The node reached its final state, so its lane ended. The update that
		// said so has usually landed the pilot already ([app.landPilot]); this is
		// the other order, and the generation is what keeps it from forgetting a
		// watcher on a node that has since started again.
		if pilot := a.pilots[msg.id]; pilot != nil && pilot.gen == msg.gen {
			a.landPilot(msg.id)
			a.touch()
		}
		return a, nil

	case wokenMsg:
		return a, a.adoptWake(msg)

	case wakeLaneClosedMsg:
		// The agent this lane belonged to is gone, and the generation is what
		// keeps a closed lane from forgetting the one that replaced it.
		if msg.gen == a.wakeGen {
			a.wakeLane = nil
		}
		return a, nil

	case taskLaneClosedMsg:
		// The agent this lane belonged to is gone. A lane from an agent that was
		// replaced is already forgotten by its generation, so only the current
		// one is dropped.
		if msg.gen == a.taskGen {
			a.taskLane = nil
		}
		return a, nil

	case gitMsg:
		if msg.ok {
			a.branch, a.branchDirty = msg.branch, msg.dirty
		} else {
			a.branch, a.branchDirty = "", false
		}
		a.touch()
		return a, nil

	case connectFlowMsg:
		return a, a.adoptConnectFlow(msg)

	case connectResultMsg:
		a.adoptConnectResult(msg)
		return a, nil

	case hudFadeMsg:
		// One of the two catch-up ticks: nothing changed, but a number that was
		// news four seconds ago has stopped being news, and the frame has to be
		// drawn again to say so.
		a.touch()
		return a, nil

	case followMsg:
		return a, a.queueFollow(msg)

	case compactedMsg:
		if msg.err != nil {
			a.note("compact failed: " + msg.err.Error())
		}
		return a, nil

	case frameMsg:
		return a, a.paint()
	}
	return a, nil
}

// paint is one turn of the clock: everything that streamed since the last frame
// becomes a frame, the animations step, and — while the turn is still running —
// the next tick is scheduled. It is the ONLY place a streamed delta becomes
// visible, which is what caps the repaint rate.
func (a *app) paint() tea.Cmd {
	a.paints++
	a.dirty = true
	// A ROOM'S ROWS ARE DROPPED ON THE SAME CLOCK, for the same reason: the page
	// carries the same spinners, count-ups and streaming blocks the conversation
	// does, and a cached row is a still photograph of an animation (room.go).
	if a.room != nil {
		a.room.dirty = true
	}
	if a.paints%usageEvery == 0 {
		a.refreshUsage()
	}
	a.promoteMarkdown()
	// The welcome box's one-shot animation is the second and last reason this
	// clock runs while nothing is being asked of the model. It steps here and
	// stops itself (welcome.go), which is what makes it one-shot rather than a
	// loop with a condition somebody has to remember to write.
	a.welcome.tick()
	// The countdown on an open proposal runs down here, on the clock that is
	// already turning — no ticker of its own (task.go).
	a.tickTasks()
	// And the approval question's, on the same terms (consent.go). It is the one
	// clock here that ANSWERS at expiry rather than stopping asking, because it
	// is the one question the engine is blocked on.
	a.tickAsk()
	// AND THE REWIND ARM RUNS DOWN HERE TOO (rewind.go): the half-second the first
	// esc buys, and the sentence the mode says when there is nothing to cut. Both
	// are windows with an end, and neither is worth a goroutine.
	a.rewindSweep()
	// AND THE CLOCK OUTLIVES THE TURN when a node does. A task runs for minutes
	// with no stream open: its spinner, its count-up and the countdown above are
	// the third reason this surface asks for a frame while the model is idle.
	//
	// A RUNNING COUNTDOWN IS THE FIFTH, and it is named separately from the turn
	// even though a question can only be up mid-turn: the clock that draws it
	// must not depend on a second fact staying true.
	if a.state == stateWorking || a.welcome.animating() || a.tasksAnimating() ||
		a.askAnimating() ||
		// AND THE REWIND ARM IS THE SEVENTH, and the only one of them that turns
		// with nothing on screen moving at all: the hint slot says "esc again to
		// rewind" for half a second, and something has to be drawing the frame
		// that takes it away again (rewind.go).
		a.rewindTicking() ||
		// A BROWSER SOMEBODY IS STANDING IN IS THE SIXTH, and it is the only one
		// of them that can be the whole of what is happening: no turn is
		// running while a person signs in, so without this the waiting line's
		// spinner would be a still photograph (connect.go).
		a.connectAnimating() ||
		// AND A ROOM ON A LIVE NODE IS THE FOURTH: the page is a transcript with a
		// spinner turning on it, and the rail — which is what [app.tasksAnimating]
		// reads — is not always on screen to say so (room.go).
		(a.roomOpen() && !a.room.done) {
		return frameTick()
	}
	a.painting = false
	return nil
}

// promoteMarkdown is the 1.5s throttle: the settled prefix of a streaming reply
// — everything up to its last newline — is rendered as markdown, and the tail
// keeps streaming plain underneath it.
func (a *app) promoteMarkdown() {
	if a.live >= 0 && a.live < len(a.entries) {
		promoteBlock(&a.entries[a.live], &a.mdAt)
	}
	// A NODE'S ANSWER IS PROMOTED ON THE SAME CLOCK. A room draws the assistant
	// block through the same renderer (render.go's [app.assistantRows]), so a page
	// whose live block was never promoted would be the one place on this surface
	// where a heading stays a hash until the turn ends (room.go).
	if a.room != nil && a.room.live >= 0 && a.room.live < len(a.room.entries) {
		promoteBlock(&a.room.entries[a.room.live], &a.room.mdAt)
	}
}

// promoteBlock is the promotion itself, over one live block and the clock that
// throttles it. It is a function rather than a method because there are two
// lists it has to be able to run over and exactly one rule (see above).
func promoteBlock(e *entry, at *time.Time) {
	if time.Since(*at) < markdownThrottle {
		return
	}
	*at = time.Now()
	cut := strings.LastIndexByte(e.text, '\n') + 1
	if cut <= e.mdCut {
		return
	}
	e.mdCut, e.stale = cut, true
}

// adopt takes the channel a Submit returned. Three shapes are possible and all
// three are ordinary: an error (the turn never started), a steering submit
// while a turn is already being pumped (the session agent's event hub already
// broadcasts that turn to the stream we hold, so the second channel is dropped
// and the message rides the first), or a new stream to follow.
//
// The generation is assigned HERE and never at submit time, because a steering
// submit must not invalidate the stream it is steering.
func (a *app) adopt(msg submittedMsg) tea.Cmd {
	// The attachment tray settles on the same answer: a refused message keeps
	// its pictures, an accepted one has spent them (attach.go).
	a.chipsSettled(msg.err)
	if msg.err != nil {
		a.note("submit failed: " + msg.err.Error())
		return a.settle()
	}
	if msg.ch == nil || a.stream != nil {
		return nil
	}
	a.gen++
	a.stream = msg.ch
	a.state = stateWorking
	a.lastDelta = time.Now()
	a.startClock()
	return tea.Batch(waitEvent(msg.ch, a.gen), a.wake())
}

// event folds one session event into the conversation.
//
// Text deltas mark the live entry stale and stop there: they are the flood, and
// the clock decides when a flood becomes a frame. Everything else is discrete
// and paints at once — a tool beginning is a fact a person is waiting for.
func (a *app) event(ev session.Event) tea.Cmd {
	// after is what this event asks the program loop to DO, as opposed to what
	// it asks the screen to say. Two events produce one — a turn ending, which
	// may ring a terminal nobody is looking at (notify.go), and a task node
	// starting, which opens a watcher on it (task.go) — and both are carried out
	// to the batch below rather than returned early, because the stream still
	// has to be waited on afterwards.
	var after tea.Cmd
	// THE BURN WINDOW OPENS ON THE FIRST EVENT of any turn that did not open one
	// itself. A turn starts in three places — a submit, an attached message, a
	// queued follow-up — and only the first of them runs through [app.submit];
	// the other two are a turn like any other and owe the person the same clock.
	// [app.startClock] keeps whichever window is already open.
	if a.state == stateWorking {
		a.startClock()
	}
	// THE COLLAPSE RULE (thinking.go): the first thing a turn says that is not
	// reasoning ends the reasoning block. EventThinking is exempt because it is
	// the marker that OPENED the run — collapsing on it would close the block
	// before its first word arrived.
	if ev.Kind != session.EventReasoning && ev.Kind != session.EventThinking {
		a.collapseThought()
	}

	switch ev.Kind {
	case session.EventTextDelta:
		a.appendText(ev.Text)
		a.lastDelta = time.Now()

	case session.EventThinking:
		a.lastDelta = time.Now()

	case session.EventReasoning:
		a.appendThought(ev.Text)
		a.lastDelta = time.Now()

	case session.EventConsentRequest:
		// A question outranks a panel. The consent block is drawn above the
		// input, and the settings sheet is the whole screen, so a question that
		// arrived while somebody was reading their settings would be a session
		// blocked on a keyboard behind a fullscreen overlay.
		// The phone's tool detail is the whole screen for the same reason and
		// stands down for the same one (expand.go): a question the session is
		// blocked on must not be behind a sheet somebody opened to read a diff.
		a.closeSettings()
		a.closeExpand()
		a.askConsent(ev)

	case session.EventConnectAsk:
		// AN OFFER OUTRANKS A PANEL, on the terms the approval question above
		// states: the block is drawn above the input, and a question drawn under
		// a fullscreen sheet is a session waiting on a keyboard nobody can reach.
		//
		// ev.NeedsKey rides along on the ask and is read where the ANSWER is
		// given (connect.go's [app.answerConnect]) rather than branched on here.
		// The question is the same question either way — may aforge connect this
		// account — and the flag decides only what saying yes DOES: a browser
		// trip, or a box that opens in place and takes a key.
		a.closeSettings()
		a.closeExpand()
		a.askConnect(ev)

	case session.EventConnectAuth:
		// The sign-in has started somewhere else. This opens the browser and puts
		// the waiting block on screen — the one event on this surface that
		// reaches out of the program, and the reason it does is that a person
		// cannot be asked to paste a link they were never shown (connect.go).
		a.connectAuth(ev)

	case session.EventConnectDone:
		a.connectDone(ev)

	case session.EventTaskProposal:
		// A DECISION OUTRANKS A PANEL, for the reason a consent question does:
		// the proposal takes the keyboard's answer lane, and a lane behind a
		// fullscreen sheet is a turn blocked on keys nobody can reach.
		a.closeSettings()
		a.proposeTask(ev)

	case session.EventTaskUpdate:
		// The same event also arrives on the standing lane; [app.taskUpdate]'s
		// (id, state) de-dup is what makes taking both harmless (task.go).
		//
		// The command it hands back is the WATCHER on a node that has just started
		// (task.go's [taskPilot]), and it is carried out through `after` because
		// the de-dup means either lane can be the one that sees "running" first:
		// a surface that armed the pilot only on the standing lane would leave
		// every in-turn node unwatched.
		after = a.taskUpdate(ev)

	case session.EventTitleChanged:
		a.setTitle(ev.Text)

	case session.EventToolForming:
		// THE CALL IS ARRIVING. Nothing has been asked for yet — this is the
		// model writing the instruction, drawn while it writes it.
		a.formTool(ev)

	case session.EventToolAnnounced:
		a.announceTool(ev)

	case session.EventToolBegin:
		a.beginTool(ev)

	case session.EventToolEnd:
		a.closeTool(ev, toolOK, "")

	case session.EventToolFailed:
		a.closeTool(ev, toolFailed, firstNonEmpty(ev.Hint, errText(ev.Err)))

	case session.EventCompacting:
		// THE PASS IS NOW VISIBLE WHILE IT RUNS. The session sends this the
		// moment the cut is made and before the summarizer is called, and that
		// call is the slowest thing on this surface that draws nothing: a turn
		// that stops for eight seconds with no spinner, no text and no tool row
		// is indistinguishable from a hang.
		a.closeLive()
		a.entries = append(a.entries, entry{
			kind:  entryCompact,
			text:  firstNonEmpty(ev.Hint, "compacting"),
			turn:  a.turn,
			began: a.now(),
		})
		a.follow()
		a.touch()

	case session.EventCompacted:
		// ALWAYS the other half of the pair, success or failure — a failed pass
		// says so in its hint and settles the same row, because a row left
		// spinning over a turn that moved on is the defect the pair exists to
		// close.
		a.closeLive()
		a.settleCompaction(firstNonEmpty(ev.Hint, "compacted"))
		// AND RE-READ THE METER HERE. The pass just changed what the
		// conversation weighs by an order of magnitude, and the status line's
		// only other reader is the end of the turn — which is a long way off
		// when compaction fires mid-batch. A meter that keeps showing 168k for
		// another two minutes of tool calls is a meter reporting a conversation
		// that no longer exists.
		a.measureContext()
		a.follow()
		a.touch()

	case session.EventNudge:
		// The loop caught itself repeating a call: a dim one-liner, never an
		// interruption — the model is already being told, the person only
		// needs to see that it was.
		a.note(firstNonEmpty(ev.Hint, "stuck? nudged · "+ev.Tool))

	case session.EventNotice:
		// The adapter had to reshape the request to get it accepted — which
		// attempt it is on, and what it took off (internal/provider's
		// endpoints.go). Same dim one-liner as the nudge, and for the same
		// reason: it is already being handled, the person only needs to see it.
		a.note(ev.Text)

	case session.EventGuardianAllowed:
		// The guardian answered for the person: quiet proof on the row's
		// decision slot, the same place a person's answer would sit.
		a.note("guardian allowed · " + ev.Tool)

	case session.EventTurnDone:
		// Both notes go in BEFORE the turn settles, so they land under the reply
		// they are about rather than above whatever is said next. What was
		// CHANGED comes first and what it COST second: the files are the work,
		// and the money is the surface talking about itself.
		a.changedNote()
		a.cacheNote(ev.Usage)
		a.take(ev.Usage)
		after = tea.Batch(a.settle(), a.notifyDone())

	case session.EventError:
		a.note("error: " + errText(ev.Err))
		// A turn that failed still paid for the steps it took, and its cache
		// reads are as real as a completed turn's.
		a.cacheNote(ev.Usage)
		a.take(ev.Usage)
		after = a.settle()
	}
	if a.stream == nil {
		return after
	}
	// The clock is normally already running (Submit started it), but a stream
	// that outlives its turn's state would otherwise stream into a frame
	// nobody built. Batch drops a nil cmd, so this costs nothing when the
	// clock is up.
	return tea.Batch(after, a.wake(), waitEvent(a.stream, a.gen))
}

// settle ends a turn: the stream is done or abandoned, nothing is live, and the
// state word goes back to idle unless the person interrupted it — an interrupt
// is a fact about the turn that ended and stays on screen until the next one
// starts. The reply it was writing becomes markdown here.
//
// It returns the two commands a settled turn owes the HUD: the repository probe
// (a turn may have committed, branched or dirtied the tree) and the BOUNDED
// fade ticks. Both are the whole of this surface's idle wakeup budget — two
// timers per turn, and nothing at all while nothing is happening.
func (a *app) settle() tea.Cmd {
	a.closeLive()
	// A turn that streamed nothing but reasoning still ends with a block, and a
	// block left open would keep a finished thought expanded over the next turn.
	a.collapseThought()
	// Questions the turn was blocked on died with it. The session already
	// released those calls; a prompt left on screen would be asking about work
	// that is over (consent.go).
	a.dropAsks()
	// And the offers on the same terms: an account the turn wanted is an account
	// nothing is waiting for once the turn is over (connect.go).
	a.dropConnectAsks()
	// A call the model was still spelling out when the turn ended never became
	// one: the row says so and stops pulsing (toolview.go).
	a.dropForming()
	// A proposal the engine is no longer holding stops asking, for the reason
	// the questions above are dropped — except that this one is CHECKED rather
	// than assumed, because the clock may have answered it (task.go).
	a.syncTaskAsk()
	if a.state == stateWorking {
		a.state = stateIdle
	}
	a.refreshUsage()
	a.measureContext()
	// THE RING IS SAMPLED HERE AND NOWHERE ELSE: one reading per turn, taken at
	// the only moment the figure is comparable with the reading before it.
	a.sampleContext()
	// AND THE RECEIPT IS FROZEN HERE, before the clock it is measured from is
	// cleared: what the turn took, what it called, what it cost (timestamps.go).
	a.stampTurn()
	a.turnBegan, a.turnOutStart, a.turnCostAt = time.Time{}, 0, 0
	a.approval = readApproval(a.profileDir)
	a.mouse = config.MouseEnabledAt(a.profileDir)
	a.timestamps = config.TimestampsAt(a.profileDir)
	a.askWait = a.consentWait()
	a.follow()
	a.touch()
	return tea.Batch(a.probeGit(), fadeTicks())
}

// dropForming resolves every call that was still ARRIVING when the turn ended.
//
// A stream can die mid-call — an interrupt, an error, a connection that went
// away between two fragments — and the row it left behind is the one row on
// this surface with no event coming for it: no announcement, no begin, no end.
// Left alone it would pulse forever at a model that has stopped speaking.
//
// It is RESOLVED rather than removed. The model started asking for something
// and the turn ended before it finished, which is a fact about what happened —
// and a row that vanished would take that fact with it — so the row keeps its
// place, stops its clock, and says the call was cancelled in the dim the rest
// of an ended turn is drawn in.
func (a *app) dropForming() {
	now := a.now()
	for i := range a.entries {
		if e := &a.entries[i]; e.forming() {
			e.ended = now
		}
	}
	// The spawn card is the same event's other half and dies the same death
	// (task.go).
	a.dropFormingCard()
}

// fadeTicks are the two catch-up wakeups a settled turn schedules: one where
// the fresh tier ends and one where the warm tier does. They are tea.Ticks and
// not a ticker on purpose — see [hudFadeMsg].
func fadeTicks() tea.Cmd {
	return tea.Batch(
		tea.Tick(hudFresh, func(time.Time) tea.Msg { return hudFadeMsg{} }),
		tea.Tick(hudWarm, func(time.Time) tea.Msg { return hudFadeMsg{} }),
	)
}

// sampleContext appends this turn's context reading to the ring, keeping the
// last [ctxRingSize]. A reading identical to the one before it is still kept: a
// flat run is exactly what the ETA estimator needs to see to say nothing.
//
// ONE READING PER TURN, and the turn counter is what enforces it: a turn ends
// TWICE on this surface — the session's own EventTurnDone, and then the stream
// closing behind it — and a ring that took both would report half the growth
// per turn and forecast a compaction that is twice as far away as it is.
func (a *app) sampleContext() {
	if a.ctxTokens <= 0 || a.ringTurn == a.turn {
		return
	}
	a.ringTurn = a.turn
	a.ctxRing = append(a.ctxRing, a.ctxTokens)
	if len(a.ctxRing) > ctxRingSize {
		a.ctxRing = a.ctxRing[len(a.ctxRing)-ctxRingSize:]
	}
}

// closeLive ends the assistant block being streamed into. A block nobody is
// writing any more is a finished document, so it renders as one.
func (a *app) closeLive() {
	if a.live >= 0 && a.live < len(a.entries) {
		e := &a.entries[a.live]
		e.settled, e.stale = true, true
	}
	a.live = -1
}

func (a *app) refreshUsage() {
	if a.agent == nil {
		return
	}
	a.take(a.agent.Usage())
}

// take folds one usage report into the status line's figures. Every field takes
// the LARGER of what it holds and what arrived, because a turn's usage and the
// session's total both come through here and only the session's total is
// monotonic — a per-turn event must never shrink a running total.
func (a *app) take(u session.Usage) {
	if u.CostUSD > a.cost {
		a.cost = u.CostUSD
	}
	if n := u.Input + u.Output; n > a.tokens {
		a.tokens = n
	}
	if u.Output > a.outputTokens {
		a.outputTokens = u.Output
	}
	if u.Input > a.inputTokens {
		a.inputTokens = u.Input
	}
	if u.CacheRead > a.cacheRead {
		a.cacheRead = u.CacheRead
	}
	if u.CacheWrite > a.cacheWrite {
		a.cacheWrite = u.CacheWrite
	}
}

// appendText grows the live assistant block, opening one if the last thing on
// screen was a tool line or a user message.
func (a *app) appendText(text string) {
	if text == "" {
		return
	}
	if a.live < 0 || a.live >= len(a.entries) || a.entries[a.live].kind != entryAssistant {
		a.entries = append(a.entries, entry{kind: entryAssistant, turn: a.turn})
		a.live = len(a.entries) - 1
		a.mdAt = time.Now()
	}
	e := &a.entries[a.live]
	e.text += text
	e.stale = true
	a.follow()
}

// formTool draws — and then keeps redrawing — the row for a call that is STILL
// ARRIVING (session.EventToolForming).
//
// THE GAP THIS CLOSES: a `write` whose body is the file and a `propose_task`
// whose brief is three paragraphs take seconds to stream, and until this
// existed the surface said nothing at all for those seconds. The announcement
// fires when the call is WHOLE; the row now exists from the first fragment, and
// says what it honestly can — how much has arrived, then the tool, then what it
// is about — gaining detail rather than appearing finished.
//
// NOTHING HERE IS PARSED. ev.ArgsText is half a JSON object and is deliberately
// not kept: the row holds the SIZE of what has arrived and the gloss session
// built from the fields that have closed, and a surface that unmarshaled a
// prefix would be drawing a call the model has not finished asking for.
func (a *app) formTool(ev session.Event) {
	// The spawn card forms from the same event, because a proposal is a BLOCK
	// rather than a row and a block that popped into existence whole is the
	// defect this wave is about (task.go).
	if ev.Tool == taskTool {
		a.formTask(ev)
	}
	at := claimForming(a.entries, ev)
	if at < 0 {
		a.closeLive()
		a.entries = append(a.entries, entry{
			kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
			status: toolForming, callID: ev.CallID, bytes: ev.Bytes,
		})
		a.follow()
		a.touch()
		return
	}
	e := &a.entries[at]
	// Every field is taken FORWARD only. The id, the name and the gloss each
	// land once and then repeat on every fragment after them, and a later event
	// that happened to carry less than the one before it must not un-say what
	// the row already knows.
	e.callID = firstNonEmpty(ev.CallID, e.callID)
	e.tool = firstNonEmpty(ev.Tool, e.tool)
	e.text = firstNonEmpty(ev.Hint, e.text)
	if ev.Bytes > e.bytes {
		e.bytes = ev.Bytes
	}
	a.touch()
}

// claimForming finds the row this forming event belongs to, or -1 for a call
// nothing has been drawn for yet.
//
// It walks NEWEST FIRST, which is what makes an id landing late harmless: the
// wire sends the id on the first fragment in practice and is not required to,
// so a row can exist with no id at all, and the row that identity belongs to is
// the most recent one still waiting for one.
//
// Once two rows have ids they cannot be confused, which is the whole reason the
// id is kept: a batch of three parallel writes forms three rows that interleave
// fragment by fragment, and matching on the tool name alone would fold all
// three into whichever was drawn first.
//
// IT TAKES THE LIST rather than reading [app.entries], because the task room
// runs the same lane over a list of its own (room.go) and a second copy of this
// walk is a second answer to "which call is this" waiting to drift from the
// first.
func claimForming(es []entry, ev session.Event) int {
	for i := len(es) - 1; i >= 0; i-- {
		e := &es[i]
		if !e.forming() {
			continue
		}
		if e.callID != "" {
			if e.callID == ev.CallID {
				return i
			}
			continue
		}
		// A row with no id yet: this event is that row's if it does not name a
		// different call, which — with no ids on either side — is as far as
		// "same call" can honestly be decided.
		if ev.Tool == "" || e.tool == "" || e.tool == ev.Tool {
			return i
		}
	}
	return -1
}

// claimFormed finds the forming row an ANNOUNCEMENT (or a begin) completes, or
// -1. It is [claimForming]'s mirror and walks the other way: the oldest
// unfinished row of that tool is the one the batch announces first, which is
// the order the ordering law promises them in.
//
// THE ID IS THE ANSWER WHEREVER THERE IS ONE. session's announcement carries the
// call's id (its loop.go), so a batch of three parallel writes pairs exactly;
// the walk by name below is what is left for a provider that streams no ids at
// all, and it is a convention rather than a fact — which is why it is second.
//
// It takes the list for [claimForming]'s reason: the room runs it too.
func claimFormed(es []entry, ev session.Event) int {
	loose := -1
	for i := range es {
		e := &es[i]
		if !e.forming() {
			continue
		}
		if ev.CallID != "" && e.callID != "" {
			if e.callID == ev.CallID {
				return i
			}
			continue
		}
		if e.tool == ev.Tool {
			return i
		}
		// A row whose name never landed can only be matched by position, and it
		// is the LAST resort: a named row for this tool outranks it wherever
		// one exists.
		if e.tool == "" && loose < 0 {
			loose = i
		}
	}
	return loose
}

// announceTool draws the row for a call the model has finished asking for
// (session.EventToolAnnounced). Nothing has started, so the row is queued: a
// dim ◌, no spinner, and — for an edit or a write — the change it is ABOUT to
// make, previewed underneath from the arguments (toolview.go).
//
// IT ADOPTS THE FORMING ROW rather than drawing a second one: the row the
// person has been watching fill in is this call, and the announcement is that
// row's next state — the pulse stops, the arguments arrive, the ink comes up.
// One call, one line, from the first fragment to the last.
//
// A row is only ever announced once, but a surface that attached mid-batch may
// see a begin with no announcement and must not draw a second line for it, so
// the pairing rule lives in [app.claimAnnounced] and both events use it.
func (a *app) announceTool(ev session.Event) {
	if at := claimFormed(a.entries, ev); at >= 0 {
		e := &a.entries[at]
		e.status = toolQueued
		e.tool = firstNonEmpty(ev.Tool, e.tool)
		e.text = firstNonEmpty(ev.Hint, e.text)
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		a.follow()
		a.touch()
		return
	}
	a.closeLive()
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
		status: toolQueued, detail: toolDetail{Args: ev.Args},
	})
	a.follow()
	a.touch()
}

// beginTool is EXECUTION STARTED. It adopts the row the announcement drew —
// the same call, one line, which is the whole point of announcing it — and
// starts that row's clock. A begin nobody announced draws its own row, which is
// every provider that does not stream tool calls and every surface that
// attached late.
func (a *app) beginTool(ev session.Event) {
	at := a.claimAnnounced(ev)
	if at < 0 {
		// A call that formed and then began with no announcement between them.
		// The ordering law says that cannot happen, and a row left pulsing at a
		// call that is already running would be the surface believing the law
		// over the event in its hand.
		at = claimFormed(a.entries, ev)
	}
	if at >= 0 {
		e := &a.entries[at]
		e.status = toolRunning
		e.began = a.now()
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.text = firstNonEmpty(ev.Hint, e.text)
		a.follow()
		a.touch()
		return
	}
	a.closeLive()
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
		status: toolRunning, began: a.now(), detail: toolDetail{Args: ev.Args},
	})
	a.follow()
	a.touch()
}

// claimAnnounced finds the queued row this begin belongs to, or -1.
//
// The payload is matched FIRST and the tool name only after: a batch of three
// edits to three files announces three rows, and pairing by name alone would
// start the clock on whichever of them was drawn first. Identical arguments are
// the one case where the two rules disagree and it does not matter — two calls
// with the same name and the same payload are the same work, in either order.
func (a *app) claimAnnounced(ev session.Event) int {
	fallback := -1
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.status != toolQueued || e.tool != ev.Tool {
			continue
		}
		if ev.Args != "" && e.detail.Args == ev.Args {
			return i
		}
		if fallback < 0 {
			fallback = i
		}
	}
	return fallback
}

// closeTool marks the oldest still-running line for that tool. Oldest rather
// than newest because tools run in parallel and finish in any order, and the
// first one begun is the first one a person watching the column expects to
// resolve.
//
// The end event carries the call's Args as well as its Output — session sends
// a self-contained end — so both are taken from it here rather than kept from
// the begin: a row rebuilt from one event is a row that cannot disagree with
// itself. The failure text is a fallback for the Output, because a tool that
// failed before it ran has a reason and no result.
func (a *app) closeTool(ev session.Event, status toolState, why string) {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || !e.status.live() || e.tool != ev.Tool {
			continue
		}
		e.status = status
		e.ended = a.now()
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.detail.Output = firstNonEmpty(ev.Output, why)
		if why != "" && status == toolFailed {
			e.text = strings.TrimSpace(e.text + " — " + why)
		}
		// A FAILURE OPENS ITSELF. Everything else on this surface waits to be
		// asked, because a quiet line is a success and success has nothing to
		// read; a call that failed is the one row whose detail is the reason the
		// person is looking at the screen, and making them click for it is
		// making them click for the only thing that happened.
		if status == toolFailed {
			e.open = true
		}
		// A CALL THAT CLOSED IS THE ONLY THING THAT MOVES THE AMBIENT COUNTS OR
		// THE SESSION DELTA: both are sums over finished calls, so this is the
		// one place their cache has to be dropped (see [app.hudStats]).
		a.hudStale = true
		a.follow()
		a.touch()
		return
	}
	// A close with no open line still deserves to be seen rather than
	// silently dropped: the session said something happened.
	if status == toolFailed {
		a.entries = append(a.entries, entry{
			kind: entryTool, tool: ev.Tool, text: why, turn: a.turn, status: toolFailed,
			open:   true,
			detail: toolDetail{Args: ev.Args, Output: firstNonEmpty(ev.Output, why)},
		})
		a.follow()
		a.touch()
	}
}

// settleCompaction stops the compaction row's clock: the LAST one still running
// takes the finished hint and the end time, and turns into the rule.
//
// Last rather than first, which is what [app.closeTool] does and for the
// opposite reason: tool calls overlap and resolve in any order, while a session
// compacts one pass at a time (session holds a compacting flag across it), so
// the only unfinished row there can be is the newest one — and walking backwards
// finds it without reading the whole conversation.
//
// A settle with NO row to settle is not an error and is not dropped: a resumed
// session replays a transcript that already contains passes nobody watched, and
// an older session predates the start event entirely. Those get a row born
// finished — the divider they always drew, with no duration claimed, because a
// pass this surface did not see the start of has no honest elapsed time.
func (a *app) settleCompaction(text string) {
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if e.kind != entryCompact || !e.ended.IsZero() {
			continue
		}
		e.text, e.ended = text, a.now()
		e.stale = true
		return
	}
	now := a.now()
	a.entries = append(a.entries, entry{
		kind: entryCompact, text: text, turn: a.turn, began: now, ended: now,
	})
}

// note appends a surface-side line — a slash command's answer, an error, the
// opening hint. It is never sent anywhere.
func (a *app) note(text string) {
	a.closeLive()
	a.entries = append(a.entries, entry{kind: entryNote, text: text, turn: a.turn})
	a.follow()
	a.touch()
}

// setTitle takes the name the session gave itself (session.EventTitleChanged).
// It is a status-line fact and nothing more: no note, no line in the
// transcript. The session named itself, which is not news the conversation
// needs — it is a label, and a label belongs where the labels are.
func (a *app) setTitle(title string) {
	title = strings.TrimSpace(title)
	if title == "" || title == a.title {
		return
	}
	a.title = title
	a.touch()
}

// submit sends one message. It always goes through a command: Submit talks to
// a lock and possibly a provider, and the Update loop is not a place to wait.
func (a *app) submit(text string) tea.Cmd {
	agent, ctx := a.agent, a.ctx
	a.closeLive()
	a.turn++
	// A new turn drops the selection: the calls it was pointing into belong to
	// the turn before this one, and a cursor left on them would answer enter
	// with somebody else's history.
	a.sel = -1
	// The person's own message is the one block on this surface that carries a
	// WALL-CLOCK moment rather than a duration: it is where a sitting starts,
	// and it is what the gap and day marks above it are measured from
	// (timestamps.go).
	a.entries = append(a.entries, entry{kind: entryUser, text: text, turn: a.turn, began: a.now()})
	a.state = stateWorking
	a.lastDelta = time.Now()
	a.startClock()
	a.follow()
	a.touch()
	return tea.Batch(func() tea.Msg {
		ch, err := agent.Submit(ctx, text)
		return submittedMsg{ch: ch, err: err}
	}, a.wake())
}

// startClock opens the burn window: this turn's start, and the output total it
// started from. A turn that is already running keeps the clock it has —
// steering a turn mid-flight (a plain enter) is not a second turn, and
// restarting the window there would report the rate of the last four seconds as
// the rate of the turn.
func (a *app) startClock() {
	if !a.turnBegan.IsZero() {
		return
	}
	a.turnBegan, a.turnOutStart, a.turnCostAt = a.now(), a.outputTokens, a.cost
}

// now is the time, from the seam rather than from the package: see [app.clock].
func (a *app) now() time.Time {
	if a.clock != nil {
		return a.clock()
	}
	return time.Now()
}

// touch says the rows no longer match the entries, and the next frame rebuilds
// them. Deltas deliberately do NOT call it — see [app.paint].
func (a *app) touch() { a.dirty = true }

// wake starts the paint clock if it is not already running.
func (a *app) wake() tea.Cmd {
	if a.painting {
		return nil
	}
	a.painting = true
	return frameTick()
}

func frameTick() tea.Cmd {
	return tea.Tick(frameInterval, func(time.Time) tea.Msg { return frameMsg{} })
}

// running reports whether any call of the current turn is still unresolved —
// arriving, queued, asked about, or spinning. All four are something on screen
// for the person to watch, which is the question the ellipsis is asking.
//
// A FORMING ROW COUNTS, and it is the newest reason this question is asked at
// all: the ellipsis exists to say "something is happening" while nothing else
// moves, and a row filling in with the model's own call is that something.
func (a *app) running() bool {
	for i := range a.entries {
		e := &a.entries[i]
		if e.turn != a.turn {
			continue
		}
		if e.forming() || (e.kind == entryTool && e.status.live()) {
			return true
		}
	}
	return false
}

// quiet reports whether the stream has been silent long enough to say so.
func (a *app) quiet() bool {
	return !a.lastDelta.IsZero() && time.Since(a.lastDelta) >= quietBeforeEllipsis
}

// waitEvent takes one event from the stream. Re-issued after each one, this is
// the whole bridge between the session's goroutine and the program loop.
func waitEvent(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{gen: gen}
		}
		return streamEventMsg{gen: gen, ev: ev}
	}
}

// unfold is ctrl+o: every call of the current turn on its own line, or back to
// the last [toolWindow] of them.
// It folds the turn of whichever list is on screen — a room's page folds its
// own clusters, from its own map, because the key names the thing being read
// (render.go's [app.bodyDeck], room.go).
func (a *app) unfold(turn int) {
	d := a.bodyDeck()
	if d.unfolded == nil {
		return
	}
	d.unfolded[turn] = !d.unfolded[turn]
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}

// bodyTurn is the turn ctrl+o folds: the last one the list on screen drew. It is
// the conversation's own counter out here, and the room's in there — a key that
// folded the transcript's newest turn while a node's page was up would be
// folding a cluster nobody can see.
func (a *app) bodyTurn() int {
	if a.room != nil {
		return a.room.turn
	}
	return a.turn
}

// selected reports whether the keyboard's selection is on this row of the list
// being drawn. It belongs to the CONVERSATION and to nothing else: a room spends
// enter on steering, so nothing in one is ever picked out (room.go), and a room
// index that happened to equal it would light a row nobody chose.
func (a *app) selected(i int) bool { return a.sel == i && !a.roomOpen() }

// openTool expands one call inline — its tool-shaped expansion, under the rail.
// Closing it also drops a lifted cap: the next opening starts at the window
// again, because "show me everything" was said about a block that is no longer
// on screen.
//
// It is also the KEYBOARD DOOR INTO A ROOM, because it is the one thing enter
// does with a selected row and a proposal is now selectable: a card whose node
// has started opens that node's page instead of an expansion (room.go). The
// branch is here rather than in [app.enter] so that "enter opens what ↑/↓
// picked" stays one sentence with one implementation.
// A ROOM'S CALLS OPEN THE SAME WAY, from the room's own list: the expansion is
// the whole reason a person clicks a row, and a page that drew calls it refused
// to open would be a transcript with its evidence removed (room.go). The door
// into a room is skipped in there for the obvious reason — a proposal is a
// thing the conversation holds, and a room is already open.
func (a *app) openTool(i int) {
	if !a.roomOpen() && a.openRoomAt(i) {
		return
	}
	es := a.bodyDeck().entries
	if i < 0 || i >= len(es) || es[i].kind != entryTool || replayInert(&es[i]) {
		return
	}
	// AND AT tierPhone IT OPENS A SHEET INSTEAD OF AN EXPANSION (expand.go).
	// The gesture is the same gesture and the intent is the same intent; what
	// changes is that forty-four columns have no room to hang a diff under a
	// row, so the answer takes the whole frame. The branch is here, in the one
	// door both the click and enter go through, so the two cannot disagree
	// about what "open this call" means.
	if a.phoneFrame() {
		a.openExpand(i)
		return
	}
	e := &es[i]
	e.open = !e.open
	if !e.open {
		e.full = false
	}
	if a.room != nil {
		a.room.dirty = true
	} else {
		a.sel = i
	}
	a.touch()
}

// showAll lifts one expansion's cap — the click on "… N more lines".
func (a *app) showAll(i int) {
	es := a.bodyDeck().entries
	if i < 0 || i >= len(es) || es[i].kind != entryTool {
		return
	}
	es[i].full = true
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}

// press resolves a click to the row it landed on. A click that lands on
// nothing does nothing: this surface has no empty-space gesture.
//
// The result is named so that the many gestures that start no work keep their
// bare returns: exactly one press on this surface hands work back, and it is the
// one that opens another conversation (welcome.go's [app.resumeSession]).
func (a *app) press(x, y int) (cmd tea.Cmd) {
	// THE REWIND MODE TAKES EVERY PRESS ON THE BODY while it is up, because while
	// it is up the transcript is not a conversation to open things in — it is the
	// picker (rewind.go). A click chooses the cut, the cut line commits, and a
	// press on anything else does nothing rather than expanding a call that is
	// about to be dropped.
	if a.rew.on {
		a.rewindPress(y)
		return
	}
	// The welcome box gets the click first, because while it is up it is the
	// thing between the pointer and everything else: a recent session opens,
	// and anywhere else is the person reaching past the box, which is what
	// dismissal means (welcome.go).
	if a.welcome.open && !a.roomOpen() {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeWelcome {
			return a.welcomePress(a.welcomeSlotAt(mark.index))
		}
		a.dismissWelcome()
	}
	r, ok := a.rowAt(y)
	if !ok {
		// A CLICK ON NOTHING IS THE WAY OUT OF A ROOM. Every interactive row on a
		// node's page now does what the same row does in the conversation, so the
		// gesture that leaves cannot be "press the body" any more — it is pressing
		// the part of the body that answers to nothing, which is the same empty
		// space esc is for. The rail was offered this click first and did not want
		// it (room.go).
		//
		// THE PINNED HEADER IS PART OF THAT SPACE, which is why the test starts at
		// the top of the FRAME rather than at [app.bodyTop]: the header's right end
		// says "esc/←← main", and a row naming the way out that did nothing when it
		// was pressed would be the one dead cell on the page.
		if a.roomOpen() {
			if top := a.bodyTop(); top >= 0 && y >= 0 && y < top+a.viewHeight() {
				a.closeRoom()
			}
		}
		return
	}
	// A TASK LINK IS THE ONE TARGET INSIDE A ROW, so it is resolved before the
	// row's own answer: the prose it sits in has no gesture of its own, and a
	// reference read after the body would be a door the body had already closed
	// the room behind (markdown.go's [linkifyTasks]).
	if a.linkPress(x, r) {
		return
	}
	// AND A CLICK ON A WAITING SIGN-IN COPIES ITS LINK (connect.go). It is read
	// here, beside the thinking block, because it is the same kind of claim: a
	// block with one thing to do, doing it wherever it is pressed.
	if cmd, took := a.connectLinkPress(r.entry); took {
		return cmd
	}
	// A click anywhere on a thinking block toggles it — the whole block is the
	// target, because a collapsed one is a single row and asking somebody to hit
	// a five-cell label is asking them to aim (thinking.go).
	if es := a.bodyDeck().entries; r.entry >= 0 && r.entry < len(es) &&
		es[r.entry].kind == entryThinking {
		a.toggleThought(r.entry)
		return
	}
	if r.hit == hitNone && a.roomOpen() {
		// A row with nothing behind it, on a page: the same empty space as above.
		a.closeRoom()
		return
	}
	switch r.hit {
	case hitTool:
		a.openTool(r.entry)
	case hitFold:
		a.unfold(r.turn)
	case hitMore:
		a.showAll(r.entry)
	case hitTask:
		// A CLICK ON A SPAWN CARD IS THE DOOR INTO THE NODE. It used to open the
		// brief, which is the card's own text one fold down — and the question a
		// person has when they press a card about running work is not "what did I
		// ask for" but "what is it doing", which is a page and not a paragraph.
		// The brief keeps ctrl+o, which is the key this surface already spends on
		// "show me the rest of this" (input.go), and enter on the selected card
		// opens the same room the click does (room.go's [app.openRoomAt]).
		//
		// The guard is the one [app.openTool] states: r.entry indexes whichever
		// list the body is drawing, and a room's proposals are not the
		// conversation's (render.go's [app.bodyDeck]).
		if !a.roomOpen() && a.openRoomAt(r.entry) {
			return
		}
		// No node behind it yet — a proposal nobody has answered, or one the
		// engine has not admitted. The brief is what there is to open.
		a.toggleCardAt(r.entry)
	case hitDone:
		// And a click anywhere on a landed card opens its full context, which is
		// the same gesture answering the same question about the same object one
		// state later (taskdone.go).
		a.toggleDoneAt(r.entry)
	case hitChoice, hitModel:
		// Both rows were offered this click before the body and took it (see
		// [app.choicePress]); reaching here means the pointer was in a column no
		// option occupies, and empty space on this surface does nothing.
	}
	return nil
}

// linkPress resolves a click on an inline task reference, and reports whether it
// took one.
//
// IT IS THE ONLY CLICK ON THIS SURFACE THAT IS MOUSE-ONLY, and that is a
// deliberate refusal rather than an omission. The keyboard's walk through the
// transcript ([app.selectTool]) visits the blocks enter opens into something —
// calls, proposals, landed cards — and adding every paragraph that happens to
// name a node would put the cursor in the middle of the prose a person is
// reading, on a row that has no way to draw that it is selected. The destination
// is not lost to the keyboard either way: ctrl+t opens the roster, and every node
// a link can reach has a row in it (task.go).
//
// A press that lands in the prose AROUND a link falls through to the row's own
// answer, which for a paragraph is nothing at all. The gap between two links is
// a sentence, not a seam, and swallowing a click on it would make the paragraph
// a place where missing costs you the page.
func (a *app) linkPress(x int, r row) bool {
	if len(r.links) == 0 || a.welcome.open {
		return false
	}
	for _, link := range r.links {
		if link.span.holds(x) {
			a.openRoomFor(link.id, link.title)
			return true
		}
	}
	return false
}

// statusPress resolves a click on the status row's MODEL SEGMENT, and reports
// whether it took the click.
//
// THE NAME OF WHAT IS ANSWERING IS THE DOOR TO CHANGING IT. The picker was
// reachable by typing /model and by nothing else, which is a door in a room the
// person is not standing in: the model's name is already on screen, already the
// thing they are looking at when they decide it is the wrong one, and a name
// that cannot be pressed is a label pretending it is not also a control.
//
// A press ANYWHERE ELSE on the status row falls through rather than being
// swallowed, because the rest of that row is telemetry — figures, not controls —
// and the conversation above it has its own gestures.
//
// The keyboard door is unchanged and stays the documented one for a surface with
// the mouse turned off (config's ui.mouse): /model with no argument opens the
// same picker, and the help sheet says so.
func (a *app) statusPress(x, y int) bool {
	if a.copy.on || a.sheet.open || a.pick.open {
		return false
	}
	// THE ROW IS RESOLVED BEFORE THE COLUMN, and that order is load-bearing:
	// [app.chromeAt] lays the chrome out to answer, and laying it out is what
	// writes [app.modelSpan]. Reading the span first would be reading where the
	// name was drawn on the frame before this one.
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeStatus {
		return false
	}
	// AT PHONE WIDTH THE ROW IS A DECK, and the deck answers for both of its rows
	// rather than falling through: the model chip is on the second one, and every
	// other cell of the two opens the sheet that carries what the deck could not
	// (statusdeck.go). It is the one status layout where empty space is NOT
	// nothing — a gap that fell through would land the press in the draft box
	// directly above it.
	if width, _ := a.size(); layoutTier(width) == tierPhone {
		return a.deckPress(x, mark.index)
	}
	// Index zero is the identity's row in both status layouts — the shared row,
	// and the first of the two when the telemetry wraps onto its own (render.go).
	//
	// A ROOM TAKES THE DOOR AWAY AND KEEPS THE NAME, and it does so through the
	// span rather than through a test here: while a room is open the segment names
	// the NODE's model, and the picker moves the CONVERSATION's, so the render
	// records no columns for it and the press falls through to the row it landed
	// on (render.go's [app.identityParts] states the law and why an inert fact
	// beats a door onto the wrong dial). One esc restores both.
	if mark.index != 0 || !a.modelSpan.holds(x) {
		return false
	}
	a.openPicker()
	return true
}

// choicePress resolves a click on a proposal's choices row to the option under
// the pointer, and reports whether it took the click.
//
// A press anywhere on that ROW is the row's, whether or not it landed on an
// option: the alternative is a click in the gap between two answers falling
// through to the card and collapsing the brief, which would make the row a place
// where missing costs you the thing you were reading.
func (a *app) choicePress(x, y int) (tea.Cmd, bool) {
	if a.roomOpen() || a.welcome.open {
		return nil, false
	}
	r, ok := a.rowAt(y)
	if !ok || r.entry < 0 || r.entry >= len(a.entries) {
		return nil, false
	}
	if r.hit != hitChoice && r.hit != hitModel {
		return nil, false
	}
	card := a.entries[r.entry].card
	// The open question is the only one that can be answered, and it is the one
	// the lane holds: an older card still on screen has already settled.
	if card == nil || card != a.task || card.settled() {
		return nil, true
	}
	// Each row is resolved against ITS OWN spans: the models row settles which
	// model, the choices row settles the question (task.go).
	if r.hit == hitModel {
		for _, span := range card.modelSpans {
			if x >= span.from && x < span.to {
				a.takeModel(span.at)
				break
			}
		}
		return nil, true
	}
	for _, span := range card.spans {
		if x >= span.from && x < span.to {
			return a.takeChoice(span.at), true
		}
	}
	return nil, true
}

// selectTool moves the selection through the tool calls that are actually on
// the row list — the folded ones are not selectable, because selecting a row
// nobody can see is a cursor that has vanished. Walking off either end returns
// false, and the key that asked falls through to scrolling.
func (a *app) selectTool(delta int) bool {
	rows := a.visible(a.bodyWidth())
	var calls []int
	for _, r := range rows {
		// A PROPOSAL WALKS WITH THE CALLS, AND SO DOES A LANDED CARD. They are the
		// other blocks on this surface that enter opens into something — a call
		// opens its expansion, a proposal and a finished node open that node's room
		// (room.go) — and a keyboard that could reach one and not the others would
		// make the room a mouse-only place. The walk is also what gives ctrl+o
		// something to act on: the key a card names is spent on the SELECTED card
		// (taskdone.go's [app.openDone]).
		if r.hit != hitTool && r.hit != hitTask && r.hit != hitDone {
			continue
		}
		if len(calls) == 0 || calls[len(calls)-1] != r.entry {
			calls = append(calls, r.entry)
		}
	}
	if len(calls) == 0 {
		return false
	}
	at := -1
	for i, e := range calls {
		if e == a.sel {
			at = i
			break
		}
	}
	switch {
	case at < 0 && delta < 0:
		at = len(calls) - 1 // ↑ from nowhere takes the most recent call
	case at < 0:
		at = 0
	default:
		at += delta
	}
	// The two entries that gain or lose the cursor are marked stale by hand, for
	// the reason [app.setHover] marks its two: a block whose rows are CACHED —
	// which a settled proposal's are (render.go) — would otherwise keep the
	// selection it was drawn with when the cursor was on it.
	a.markStale(a.sel)
	if at < 0 || at >= len(calls) {
		a.sel = -1
		a.touch()
		return false
	}
	a.sel = calls[at]
	a.markStale(a.sel)
	a.touch()
	a.reveal(a.sel)
	return true
}

// slash consumes a command line. Everything starting with "/" is answered
// here and nothing starting with "/" is ever sent to the model — including a
// command nobody defined, which gets a hint instead of a turn.
//
// THE WORD IS RESOLVED THROUGH THE TABLE BEFORE IT IS SWITCHED ON. The other
// words a command answers to — /clear for /new, /exit and /q for /quit, /? for
// /help — live on the table's rows (commands.go's [command.alias]), so this
// switch has one case per COMMAND rather than one per spelling, and a synonym
// cannot exist here without also appearing in the list and in /help. The line
// that is typed in full and entered arrives here too, so an alias typed out and
// an alias chosen from the list run the same road.
func (a *app) slash(line string) tea.Cmd {
	name, rest, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	rest = strings.TrimSpace(rest)
	// The unknown-command hint below says back what was typed and not what it
	// resolved to, so the name as written is kept.
	switch canonicalCommand(name) {
	case "quit":
		return a.quit()

	case "help":
		a.note(helpText(a.file))
		return nil

	case "copy":
		a.enterCopy()
		return nil

	case "select":
		// It ANSWERS when there is nothing to hand over, because this one was
		// typed out on purpose: silence after a deliberate command reads as a
		// command that broke, and the truth is short and is good news.
		if !a.releaseMouse() {
			a.note("your terminal already has the pointer — drag to select.")
		}
		return nil

	case "model":
		// Bare /model is a question — "which ones are there" — and the picker
		// is the answer. A slug is an instruction, and an instruction that
		// opened a list to confirm itself would be the surface asking a person
		// to say something twice.
		if rest == "" {
			a.openPicker()
			return nil
		}
		a.switchModel(rest, 0)
		return nil

	case "image":
		// The other door onto the tray, for a picture that is not under this
		// directory or not in the walk: a path, attached (attach.go).
		a.attachPath(rest)
		return nil

	case "settings":
		a.openSettings()
		return nil

	case "connect":
		// Two words for one list, the way /settings answers to three (the second
		// is /connections, on the table's row): a person asking what they have
		// connected and a person wanting to connect something are looking at the
		// same panel, and neither should have to find out which word this build
		// chose.
		//
		// No argument form. A service is picked from a list of two or three, and
		// a name typed at a command line is a name that can be typed wrong.
		a.openConnect()
		return nil

	case "resume":
		// Two words for one list, the way /settings also answers to /set and
		// /config: docs/CHAT-V3.md calls this the sessions picker and a person
		// coming back to work calls it resuming, and neither of them should have
		// to find out which word this build chose.
		//
		// No argument form on purpose. A session is named by a title a model
		// wrote and lives in a file named after a timestamp; neither is a thing
		// anybody types, so the only honest way to ask for one is to be shown
		// them (resume.go).
		a.openResume()
		return nil

	case "compact":
		agent, ctx := a.agent, a.ctx
		a.note("compacting…")
		return func() tea.Msg { return compactedMsg{err: agent.Compact(ctx)} }

	case "rewind":
		// The keys are esc esc and the row says so; the command exists because a
		// gesture nobody can see is a gesture nobody finds (commands.go).
		return a.enterRewind()

	case "new":
		// The command that replaces the agent is the one command here that
		// returns work: the standing task lane belongs to the agent that handed
		// it over, so the next conversation subscribes to its own (task.go).
		return a.renew()

	default:
		a.note("unknown command: /" + name + " · try /help")
		return nil
	}
}

// renew closes this conversation and opens the next one on the same config.
// The transcript is cleared because it belongs to the agent that just closed:
// a fresh session file with the old conversation still on screen would be the
// surface claiming context the model does not have.
// It returns the one command the next conversation owes itself: its own
// standing task subscription (task.go).
func (a *app) renew() tea.Cmd {
	if a.fresh == nil {
		a.note("/new is unavailable here")
		return nil
	}
	if a.state == stateWorking {
		a.agent.Interrupt()
	}
	if err := a.agent.Close(); err != nil {
		a.note("close failed: " + err.Error())
	}
	agent, file, err := a.fresh()
	if err != nil {
		a.note("new session failed: " + err.Error())
		return nil
	}
	a.agent, a.file = agent, file
	a.entries = nil
	a.live, a.sel, a.think = -1, -1, -1
	a.asks, a.follows = nil, nil
	// The offers and the sign-ins belong to the conversation that raised them,
	// and a browser still standing open on one of them is a browser nobody is
	// coming back to (connect.go).
	a.connAsks, a.connPanel = nil, connectPanel{}
	a.abandonConnects()
	a.title = strings.TrimSpace(agent.Title())
	a.turn = 0
	a.unfolded = map[int]bool{}
	a.dropHover()
	// A frozen viewport is a snapshot of a conversation that no longer exists
	// (copymode.go), for the same reason the hover is dropped one line above.
	a.copy = copyMode{mark: -1}
	// The rail goes with the conversation: its nodes died with the agent that
	// started them, and a row left standing would be presence claimed for work
	// nobody is doing (task.go).
	a.dropTasks()
	a.stream = nil
	a.gen++
	a.state = stateIdle
	a.resetMeters()
	a.model = agent.Model()
	// The draft is NOT cleared: /new closes a conversation, and the sentence in
	// the box is the person's next one (draft.go).
	a.endRecall()
	a.offset, a.stick = 0, true
	a.touch()
	if file != "" {
		a.note("new session · " + file)
	} else {
		a.note("new session")
	}
	return tea.Batch(a.watchTasks(), a.watchWakes())
}

func (a *app) quit() tea.Cmd {
	// The draft goes to disk on the way out, synchronously and before anything
	// else: the debounce may be mid-window, and a sentence typed in the last
	// three hundred milliseconds of a session is exactly the one a person would
	// be most surprised to lose (draft.go).
	if a.draftFile != "" {
		writeDraft(a.draftFile, a.input.String())
	}
	if a.agent != nil {
		a.agent.Interrupt()
		_ = a.agent.Close()
	}
	return tea.Quit
}

// interrupt is esc: stop the turn, keep what it said.
func (a *app) interrupt() {
	if a.state != stateWorking {
		return
	}
	a.agent.Interrupt()
	a.state = stateInterrupted
	a.note("interrupted")
	// The session drops its follow-up queue on an interrupt — a stop that was
	// followed by the session working again is not a stop — so the surface says
	// so rather than leaving a count above the box for turns that will never run.
	a.dropFollows()
}

// ── the paste bracket ───────────────────────────────────────────────────────
//
// THE BUG THIS FIXES, AND WHY IT WAS NOT THE OBVIOUS ONE.
//
// Bracketed paste is on (view.go) and the parser usually hands the whole paste
// over as one tea.PasteMsg, which this surface has always handled. The comment
// at the bottom of input.go's key router claimed the other case was covered too:
//
//	// A key event carrying a newline is a paste on a terminal that does not
//	// speak bracketed paste (or one whose paste arrived as keystrokes). It
//	// is inserted as typed — the newlines are the person's.
//
// That was FALSE, and it was false in the one direction that costs something. A
// newline never reaches that line: it arrives as a key whose name is "enter",
// and "enter" is matched twelve cases higher up, where it SUBMITS. So a paste
// that arrived as keystrokes did not become a multi-line draft — it sent the
// first line to the model, then the second, then the third. Ten lines of a stack
// trace became ten turns. The fallback the comment described could not run,
// because the key it was written for was taken before it.
//
// Keys leak between the brackets more often than the coalescing path suggests:
// the parser gives up on its buffer and passes an event through when a sequence
// inside a paste does not decode, and win32-input and the kitty protocol encode
// the newlines in a paste as key events by construction.
//
// So the fix is not a better fallback. It is to trust the BRACKET rather than
// the coalescing: PasteStartMsg opens it, everything until PasteEndMsg is text —
// keys included, read before every other claim on the keyboard — and the close
// spends the whole of it as one edit. Nothing between the brackets can submit,
// interrupt, answer a question, or open an overlay, because nothing between the
// brackets is a keystroke: it is a document somebody copied.

// pasteGrace is how long an open bracket may go quiet before it is treated as
// abandoned.
//
// It exists because the alternative is a dead keyboard. A terminal that sends
// the open and then dies, a paste cut short by a disconnect, a multiplexer that
// swallows the close — any of them would leave this surface reading every key as
// text forever, which is the one failure worse than the one being fixed. Two
// seconds is far longer than the gap between two keys of the same paste (they
// arrive in one read) and far shorter than the gap between two keys a person
// typed.
const pasteGrace = 2 * time.Second

// pasteKey takes one keypress that arrived inside an open bracket, and reports
// whether it took it.
//
// ctrl+c is the exception it makes for itself, for the reason every modal on
// this surface makes it: leaving is never modal, and a bracket that trapped the
// door would be the abandoned-paste failure with no way out of it.
func (a *app) pasteKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.pasting {
		return nil, false
	}
	if msg.String() == "ctrl+c" {
		return nil, false
	}
	// AN ABANDONED BRACKET IS NOT A BRACKET. Past the grace the close is assumed
	// lost, what was collected is spent, and this key is handed back to be the
	// keystroke it plainly is — with the flush's own command, which the caller
	// batches rather than drops.
	if !a.pasteAt.IsZero() && a.now().Sub(a.pasteAt) > pasteGrace {
		a.pasting = false
		text := string(a.pasted)
		a.pasted = a.pasted[:0]
		return a.paste(text), false
	}
	a.pasteAt = a.now()
	switch msg.String() {
	case "enter", "ctrl+j":
		// The newline this whole mechanism exists for.
		a.pasted = append(a.pasted, '\n')
	case "tab":
		a.pasted = append(a.pasted, '\t')
	default:
		// Everything else is text or it is nothing. A key with no text inside a
		// paste is a control sequence the sender's terminal emitted and the
		// receiver's could not name, and putting an unnamed control code into a
		// person's draft is worse than dropping it.
		a.pasted = append(a.pasted, []rune(msg.Key().Text)...)
	}
	return nil, true
}

// paste inserts pasted text into the draft. It is a method rather than an
// inline insert because a paste is an edit like any other: the overlays follow
// it, and the draft debounce is armed by it.
func (a *app) paste(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	// COPY MODE IS A READER, and it is modal for the clipboard exactly as it is
	// for the keyboard (copymode.go): the box a paste would land in is off
	// screen behind a frozen viewport, so the text would go somewhere nobody can
	// see it. The clipboard still holds it, which is the difference between
	// declining a paste and losing one.
	if a.copy.on {
		return nil
	}
	// A PASTE IS SOMEBODY STARTING WORK, so it dismisses the welcome box on the
	// same terms every other input does (welcome.go): everything puts the box
	// away except the two keys that walk its list, and a paste is not one of
	// them.
	a.dismissWelcome()
	// Bracketed paste arrives with the SENDER's line endings, and tmux sends
	// CR: an editor that breaks rows on LF alone would hold one "line" whose
	// carriage returns paint each logical line over the last. Normalize once,
	// at the door — CRLF first, then bare CR.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	// A KEY BOX IS THE PASTE THIS SURFACE MOST EXPECTS, and it reads first. A
	// key is a thing nobody types — it comes out of a clipboard — so the two
	// boxes that collect one take the clipboard before anything else does: the
	// offer's own row (connect.go) and the panel's (connectpanel.go).
	//
	// NEWLINES ARE DROPPED RATHER THAN FLATTENED TO SPACES. A key copied out of
	// a web page usually brings a trailing newline with it, and a space in the
	// middle of a secret is a secret that does not work — which the far end
	// would report as a bad key, about the one thing the person did right.
	if box := a.keyBox(); box != nil {
		box.insert(strings.ReplaceAll(text, "\n", ""))
		a.touch()
		return nil
	}
	// The model overlay is modal for the keyboard, so it is modal for the
	// clipboard: a paste while it is up is a filter somebody copied.
	if a.pick.open {
		a.pick.filter.insert(strings.ReplaceAll(text, "\n", " "))
		a.pick.rank()
		a.touch()
		return nil
	}
	// The settings sheet owns the clipboard while it is up, the same way it
	// owns the keyboard: into the text row being answered first (an API key
	// is the paste this path exists for), into the select row's filter next,
	// into the search box otherwise. All three are one-line boxes — newlines
	// flatten to spaces.
	if a.sheet.open {
		flat := strings.ReplaceAll(text, "\n", " ")
		switch {
		case a.sheet.edit != nil:
			a.sheet.edit.box.insert(flat)
		case a.sheet.sel != nil:
			a.sheet.sel.pick.filter.insert(flat)
			a.sheet.sel.pick.rank()
		default:
			a.sheet.query.insert(flat)
		}
		a.touch()
		return nil
	}
	a.input.insert(text)
	cmd := a.edited()
	// A QUESTION SUSPENDS THE LISTS, and it suspends them against the clipboard
	// too. consent.go closes both the moment a question arrives, on the grounds
	// that a list left open under a modal is a list answering keys nobody is
	// pressing — and a pasted "/" or "@" would otherwise re-open one underneath
	// a block whose keys the person is about to press. The text still lands: the
	// draft is where it was going, and it is waiting when the question is
	// answered.
	if a.asking() {
		a.closeLists()
	}
	return cmd
}

// ── the two typed overlays ──────────────────────────────────────────────────

// listKey routes the keys that belong to an open command list or file
// completion, and reports whether it took the key. Everything it does not take
// falls through to the editor, which is the whole difference between these two
// overlays and the modal model picker: the person is still typing a sentence.
func (a *app) listKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.menu.open && !a.comp.open {
		return nil, false
	}
	switch msg.String() {
	case "up", "ctrl+p":
		if a.menu.open {
			a.menu.move(-1)
		} else {
			a.comp.move(-1)
		}
		a.touch()
		return nil, true

	case "down", "ctrl+n":
		if a.menu.open {
			a.menu.move(1)
		} else {
			a.comp.move(1)
		}
		a.touch()
		return nil, true

	case "esc":
		a.closeLists()
		a.touch()
		return nil, true

	case "enter":
		if a.menu.open {
			if _, ok := a.menu.choice(); !ok {
				// Nothing matched what was typed. The line is still a line, and
				// enter is still submit — /nonsense gets its answer.
				a.menu.close()
				return nil, false
			}
			return a.runMenu(), true
		}
		if !a.comp.picked() {
			a.comp.close()
			return nil, false
		}
		a.completeMention()
		return a.edited(), true
	}
	return nil, false
}

// syncLists is what every edit runs: the overlays follow the draft, and never
// the other way round. At most one is open — a line that starts with "/" is a
// command being chosen, and an @ inside it would be an argument to a command
// this surface does not have.
func (a *app) syncLists() tea.Cmd {
	a.menu.sync(a.input.String())
	if a.menu.open {
		a.comp.close()
		return nil
	}
	was := a.comp.open
	a.comp.sync(&a.input)
	if a.comp.open && !was {
		// Both halves of the list are asked for at the same moment, and neither
		// waits for the other: the index is one small file and lands first, the
		// walk lands when it lands (taskmention.go, files.go).
		return tea.Batch(a.loadFiles(), a.loadTasks())
	}
	return nil
}

func (a *app) closeLists() {
	a.menu.close()
	a.comp.close()
}

// ── the context meter ───────────────────────────────────────────────────────

// accentAtThresholdPercent is when the meter stops being dim: at 80% of the
// COMPACTION THRESHOLD, not of the window.
//
// The window is not the thing that happens to you. Compaction is — the summary
// call, the paid-for pass, the tail that survives and the middle that does not —
// and it fires well below the window (session.CompactThreshold). A meter that
// waited for 80% of the window would go quiet through the whole approach and
// then light up after the event it was warning about.
const accentAtThresholdPercent = 80

// resetMeters zeroes everything the status line counts. It is called wherever
// the agent underneath this surface is REPLACED — /new, and opening a recent
// session from the welcome box — because every one of these figures is a fact
// about one conversation and carrying any of them into the next one would be a
// bill for somebody else's work.
//
// It is a method rather than the tuple assignment it replaced so that a figure
// added here is reset in both places rather than in whichever one was edited.
func (a *app) resetMeters() {
	a.cost = 0
	a.tokens, a.inputTokens, a.outputTokens = 0, 0, 0
	a.cacheRead, a.cacheWrite = 0, 0
	a.cacheSaved = 0
	a.ctxTokens = 0
	// The HUD's own state is a fact about one conversation too: a sparkline
	// carried across /new would be a graph of somebody else's context, and an
	// ambient count would be claiming jobs that died with the agent.
	a.ctxRing, a.ringTurn = nil, 0
	a.turnBegan, a.turnOutStart, a.turnCostAt = time.Time{}, 0, 0
	// The receipts go with the conversation they were written for: turn 1 of the
	// session that replaced this one is not the turn 1 those figures describe
	// (timestamps.go).
	a.stamps = nil
	a.hud, a.hudStale = hudStats{}, true
	a.segText, a.segAt = [segCount]string{}, [segCount]time.Time{}
}

// measureContext asks the agent what the conversation now weighs. It is called
// where the answer CHANGES — a turn ending, a session opening or being replaced
// — and never on the frame clock: the agent takes its own lock to answer, and
// doing that thirty times a second to move a figure that changes once a turn is
// a lock taken for nothing.
func (a *app) measureContext() {
	if a.agent == nil {
		return
	}
	a.ctxTokens = a.agent.ContextTokens()
	if a.ctxWindow <= 0 {
		// The door may not have known the window at boot: a cold catalog
		// resolves in the background AFTER this surface is already up, and it
		// tells the agent directly (cmd/aforge's warmV3Models) where there is
		// no seam back to here. Asking the model list again is that seam, and
		// it cannot block — [app.modelList] falls through to the disk cache and
		// then to the built-ins. Once it answers, it is never asked again.
		a.ctxWindow = a.windowFor(a.model)
	}
}

// ctxPercent is the meter: how much of the model's window the conversation is
// using, to the NEAREST whole percent. False when nobody has said what the
// window is, because a percentage of an unknown is a number that means nothing.
//
// Nearest rather than floored, which is what this did while it was a percentage
// of a byte estimate. Flooring a figure that is already an estimate rounds the
// same direction every time, and the direction it rounds is the reassuring one:
// a conversation at 9.7% of its window reads as 9%, and one at 79.9% of the
// threshold reads as under it.
func (a *app) ctxPercent() (int, bool) {
	if a.ctxWindow <= 0 || a.ctxTokens <= 0 {
		return 0, false
	}
	return (a.ctxTokens*200/a.ctxWindow + 1) / 2, true
}

// ctxHeat is the meter's THREE-RUNG RAMP, and every rung is measured against
// the compaction threshold rather than against the window (see
// [accentAtThresholdPercent] for why):
//
//	ctxCalm  dim       nothing is approaching
//	ctxNear  accent    past 80% of the threshold — compaction is coming
//	ctxDue   pal.bad   past the threshold ITSELF — it is due or overdue, and the
//	                   next turn will pay for a summarizer call
//
// The third rung exists because the second one used to be the end of the ramp:
// a conversation at 81% of the threshold and one 40k past it were painted
// identically, and the second is the only one where the person can still act —
// finish the thought, /new, split the work — before a pass takes the middle of
// the conversation away.
//
// Every rung is ctxCalm whenever the window is unknown: a surface that does not
// know the threshold must not guess that one has been crossed.
type ctxHeat int

const (
	ctxCalm ctxHeat = iota
	ctxNear
	ctxDue
)

func (a *app) ctxHeat() ctxHeat {
	threshold := session.CompactThreshold(a.ctxWindow)
	if threshold <= 0 || a.ctxTokens <= 0 {
		return ctxCalm
	}
	switch {
	case a.ctxTokens >= threshold:
		return ctxDue
	case a.ctxTokens*100 >= threshold*accentAtThresholdPercent:
		return ctxNear
	}
	return ctxCalm
}

// ctxCrowded says the conversation is close enough to compaction that the meter
// should stop being furniture — the bottom of the ramp, kept as the one-bit
// question the rest of the surface asks.
func (a *app) ctxCrowded() bool { return a.ctxHeat() >= ctxNear }

// priceFor is what this surface knows a model's tokens cost, from the same list
// the picker draws (models.go's three rungs). The bool is false when nobody has
// published a prompt price — an id off the built-ins, a cache written before the
// prices were kept, one of OpenRouter's own routers — and a caller must then
// show the tokens alone rather than a saving computed from zero.
func (a *app) priceFor(id string) (Model, bool) {
	id = strings.TrimSpace(id)
	for _, model := range a.modelList() {
		if strings.EqualFold(model.ID, id) && model.PromptPrice > 0 {
			return model, true
		}
	}
	return Model{}, false
}

// cacheNote is the per-turn savings line: what this turn read off a warm prefix
// and what that was worth.
//
//	⟲ 9.8k cached · saved .0041
//
// It is written only when there were cache reads, so a session on a provider
// that caches nothing — or a first turn, which can only write — says nothing at
// all rather than reporting a zero every turn.
//
// The saving is cached tokens × (prompt price − cache-read price), which is the
// honest figure: a cache read is CHEAPER, never free, and the difference is what
// the cache actually bought. Without a published price pair the line degrades to
// the token count, because "9.8k cached" is a true thing this surface knows and
// "saved $0.0000" is not.
func (a *app) cacheNote(u session.Usage) {
	if u.CacheRead <= 0 {
		return
	}
	line := "⟲ " + tokenWord(u.CacheRead) + " cached"
	if model, known := a.priceFor(a.model); known {
		if saved := float64(u.CacheRead) * (model.PromptPrice - model.CacheReadPrice); saved > 0 {
			// The same figure, twice: once for this turn, and once into the
			// session's running total behind the status line's warm share. It is
			// summed HERE — under the same price guard — so the total can never
			// contain a turn the note itself could not price.
			a.cacheSaved += saved
			line += " · saved " + savedWord(saved)
		}
	}
	a.note(line)
}

// ── WHAT CHANGED ────────────────────────────────────────────────────────────
//
// A turn that touched files ends with one dim line saying which:
//
//	· 2 files · loop.go +32 −2 · agent.go +18 −0
//
// It exists because of what a tool cluster looks like AFTER it has scrolled. A
// turn that edits four files across nine calls draws nine rows, three of which
// are visible by then, and the question a person actually has when the turn
// stops — "so what did it change?" — is answered nowhere on the screen. The
// individual +N −M stats are on rows that folded; the reply above says what the
// model meant to do, which is not the same claim.
//
// It is derived from the SAME arguments the expansions are (toolstat.go), so
// the figures cannot disagree with the diffs a click opens: an edit's stat is
// its replacements diffed, a write's is the lines it laid down.
//
// What is excluded, and why: reads and bashes and searches. A read changes
// nothing, and a bash MAY change everything but says so nowhere a surface can
// see — a line that reported four files after a `make` that rewrote two hundred
// would be a lie with a number in it. This line's claim is narrow on purpose:
// these are the files this turn wrote THROUGH THE TOOLS THAT SAY WHAT THEY
// WROTE.

// changedFiles is how many files the line names before it stops naming them.
// Four is the width a dim line can carry at eighty columns; past it the count
// at the front is doing the work anyway.
const changedFiles = 4

// fileStat is one file's share of a turn.
type fileStat struct {
	path       string
	adds, dels int
}

// changedNote appends the line, or nothing at all when the turn wrote nothing.
func (a *app) changedNote() {
	if stats := a.turnStats(a.turn); len(stats) > 0 {
		a.note(changedWord(stats))
	}
}

// turnStats gathers one turn's file writes, in the order they were first
// touched — which is the order they happened, and the only order that does not
// need a rule.
//
// A failed call is skipped: an edit that did not apply changed nothing, and a
// line that counted it would be reporting a file that is on disk as its author
// left it. A call still running is skipped for the same reason — though by the
// time this runs the turn is over, so that is a belt on a done deal.
func (a *app) turnStats(turn int) []fileStat {
	var out []fileStat
	at := map[string]int{}
	add := func(path string, adds, dels int) {
		if path == "" {
			return
		}
		if i, seen := at[path]; seen {
			out[i].adds += adds
			out[i].dels += dels
			return
		}
		at[path] = len(out)
		out = append(out, fileStat{path: path, adds: adds, dels: dels})
	}
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.turn != turn || e.status != toolOK {
			continue
		}
		fields := argsOf(e.detail.Args)
		switch e.tool {
		case "edit":
			adds, dels := editStat(e.detail.Args)
			add(argString(fields, "path"), adds, dels)
		case "write":
			add(argString(fields, "path"), lineCount(argString(fields, "content")), 0)
		}
	}
	return out
}

// changedWord spells the line. The files are named by their BASE names
// (welcome.go's [baseName], which is the package's one answer to that question):
// the directory is what the tool rows above already showed, and a line of full
// paths at eighty columns is one file per line.
func changedWord(stats []fileStat) string {
	head := itoa(len(stats)) + " files"
	if len(stats) == 1 {
		head = "1 file"
	}
	parts := []string{head}
	shown := stats
	if len(shown) > changedFiles {
		shown = shown[:changedFiles]
	}
	for _, s := range shown {
		parts = append(parts, baseName(s.path)+" "+
			glyphAdd+itoa(s.adds)+" "+glyphDel+itoa(s.dels))
	}
	if rest := len(stats) - len(shown); rest > 0 {
		parts = append(parts, "+"+itoa(rest)+" more")
	}
	return strings.Join(parts, " · ")
}

// ── WHAT IS ALIVE, AND WHAT WAS WRITTEN ─────────────────────────────────────
//
// hudStats is the pair of sums the right cluster reports about the whole
// conversation: how much background work this session started and has not
// watched stop, and what it has written to disk across every turn.
type hudStats struct {
	// jobs and watches are ALIVE, by the only definition this surface can hold
	// honestly: it saw them start and it has not seen them killed.
	//
	// The session owns the real job table (internal/session's jobs.go) and does
	// not publish it, so this is read off the transcript the surface already
	// drew — a `bash` call that succeeded with background:true is a job, a
	// `watch` call that succeeded is a watch, and a `jobs` call with action
	// "kill" takes one of them away. The id is matched where the output gave one
	// ("job 3 started"), because a batch that starts three jobs and kills the
	// second must not decrement the first.
	//
	// WHAT IT CAN GET WRONG, said out loud: a job that exited on its own is
	// still counted, because nothing on the wire says so. That is the honest
	// direction to be wrong in — the count is "what you started", and a person
	// who sees "1 job" and finds it finished has lost nothing, where a count
	// that silently dropped a live server would have.
	jobs, watches int
	// adds and dels are the session's diffstat, summed from the same arguments
	// the tool rows' own stats are derived from (toolstat.go), so the Σ segment
	// and the per-turn "what changed" lines can never disagree.
	adds, dels int
}

// hudStats answers both, from the cache when the calls have not moved.
func (a *app) hudStats() hudStats {
	if !a.hudStale {
		return a.hud
	}
	a.hud, a.hudStale = a.computeStats(), false
	return a.hud
}

func (a *app) computeStats() hudStats {
	var out hudStats
	// live holds the ids of the background jobs this surface watched start, so a
	// kill can take away the one it names rather than the newest.
	var live []string
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.status != toolOK {
			continue
		}
		fields := argsOf(e.detail.Args)
		switch e.tool {
		case "edit":
			adds, dels := editStat(e.detail.Args)
			out.adds, out.dels = out.adds+adds, out.dels+dels
		case "write":
			out.adds += lineCount(argString(fields, "content"))
		case "bash":
			if argString(fields, "background") != "true" {
				continue
			}
			out.jobs++
			live = append(live, jobID(e.detail.Output))
		case "watch":
			out.watches++
		case "jobs":
			if argString(fields, "action") != "kill" {
				continue
			}
			id := argString(fields, "id")
			if at := indexOf(live, id); id != "" && at >= 0 {
				live = append(live[:at], live[at+1:]...)
				out.jobs--
				continue
			}
			// An id this surface never saw start is a watch's — watches are
			// jobs too (kind watch) and their start line publishes no id — and
			// failing that it is a job from before we were looking.
			if out.watches > 0 {
				out.watches--
				continue
			}
			if out.jobs > 0 {
				out.jobs--
			}
		}
	}
	return out
}

// jobID reads the id out of a background bash call's own answer, which session
// spells "job 3 started; log at …". Empty when it said something else.
func jobID(output string) string {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 2 || fields[0] != "job" {
		return ""
	}
	return strings.TrimSuffix(fields[1], ";")
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}

// ── THE REPOSITORY ──────────────────────────────────────────────────────────

// gitTimeout is how long the legend will wait for a repository to answer.
//
// It is short because the answer is FURNITURE: a branch name in a border is
// worth a quarter of a second of a background goroutine and not one frame of
// the surface. A tree so large that `git status` cannot answer in that time
// draws no branch, which is exactly what a directory that is not a repository
// draws — and neither of them makes the person wait.
const gitTimeout = 400 * time.Millisecond

// probeGit asks the workspace what it is, off the model loop.
func (a *app) probeGit() tea.Cmd {
	dir, probe := a.workspace, a.gitProbe
	if dir == "" || probe == nil {
		return nil
	}
	return func() tea.Msg {
		branch, dirty, ok := probe(dir)
		return gitMsg{branch: branch, dirty: dirty, ok: ok}
	}
}

// gitHead is the default probe: the branch, and whether the tree is dirty.
//
// Two commands rather than one because they answer two questions and only the
// first one is cheap. A detached HEAD answers "HEAD", which is reported as it
// is — it is the truth about where the work is going, and inventing a short sha
// here would be this surface deciding what a repository means.
//
// `--no-optional-locks` is the one flag that matters: `git status` normally
// refreshes the index, which takes a write lock, and a status bar must never be
// the reason a person's own `git commit` in the next pane blocks.
func gitHead(dir string) (string, bool, bool) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	run := func(args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	branch, ok := run("rev-parse", "--abbrev-ref", "HEAD")
	if !ok || branch == "" {
		return "", false, false
	}
	// An unreadable status is reported as CLEAN rather than dirty: the star is a
	// claim, and a claim this surface could not check is one it should not make.
	status, _ := run("--no-optional-locks", "status", "--porcelain", "--untracked-files=no")
	return branch, status != "", true
}

// readApproval is the gate's posture, or "" where there is no profile to ask.
//
// The empty answer is deliberately not [config.DefaultToolApprovalMode]: a
// surface booted without a profile (every test, and any embedding that wires
// its own policy) has not been told the gate is open, and the YOLO segment's
// whole law is that it appears only when somebody said so.
func readApproval(profileDir string) string {
	if strings.TrimSpace(profileDir) == "" {
		return ""
	}
	return config.ToolApprovalModeAt(profileDir)
}

func errText(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// dollars formats a running cost the way the status line wants it: cents while
// the session is cheap, so a first turn is not rendered as $0.00.
func dollars(usd float64) string {
	switch {
	case usd <= 0:
		return "$0.00"
	case usd < 0.01:
		return fmt.Sprintf("$%.4f", usd)
	default:
		return fmt.Sprintf("$%.2f", usd)
	}
}

// savedWord formats what a cache read was worth, and it is deliberately NOT
// [dollars]: the savings note is a dim aside, the amount beside it is a fraction
// of a cent for most turns, and "$0.0041" spends three cells on a zero and a
// point that carry nothing. The leading zero goes; the figure does not.
//
// Four places below a dollar and two above, because those are the two scales the
// number actually lives at — a turn saves thousandths, a long session saves
// dollars, and nothing useful sits between them.
func savedWord(usd float64) string {
	if usd >= 1 {
		return fmt.Sprintf("$%.2f", usd)
	}
	return fmt.Sprintf("$%.4f", usd)
}

// tokenWord is a token count at a glance: "842", "12.4k", "1.2M". One
// significant decimal and no more — the meter is read in passing, and a figure
// that changes in its fourth digit every step is a figure nobody can read.
//
// The trailing ".0" is dropped so a round number is round: a 128k window is
// "128k" and never "128.0k".
func tokenWord(tokens int) string {
	switch {
	case tokens <= 0:
		return "0"
	case tokens < 1000:
		return strconv.Itoa(tokens)
	// 999_950 and not 1_000_000: one decimal rounds anything above it to
	// "1000.0k", which is a figure with the wrong unit on it.
	case tokens < 999_950:
		return trimUnit(float64(tokens)/1000, "k")
	default:
		return trimUnit(float64(tokens)/1_000_000, "M")
	}
}

func trimUnit(value float64, unit string) string {
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0") + unit
}
