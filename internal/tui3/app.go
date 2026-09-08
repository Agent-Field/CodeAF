package tui3

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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
//
// It is the LOCAL cadence, and it is the unit the animations are counted in
// either way: a surface being read over a connection builds fewer frames and
// steps the same distance through each one (link.go's [app.frameEvery] and
// [app.frameStride]).
const frameInterval = 33 * time.Millisecond

// markdownThrottle is how often a streaming reply's settled prefix is promoted
// from plain wrapped text to rendered markdown. See [app.assistantRows].
const markdownThrottle = 1500 * time.Millisecond

// usageEvery is how many frame slots pass between asks for the session's
// running cost. The agent answers under a lock, and a lock taken thirty times a
// second to move a figure that changes once a turn is a lock taken for nothing.
//
// SLOTS AND NOT FRAMES, which is a third of a second either way: the question
// is how often the lock is worth taking in wall time, and that answer does not
// change because the frames arrived over a wire (link.go's [app.dueEvery]).
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
	// entrySteer is ONE CORRECTION TYPED INTO A RUNNING TURN, drawn at the point
	// in the transcript where it was said (steerelbow.go).
	//
	// It is a block of its own rather than a list hanging off the question for
	// the reason entryCompact is not a divider: WHERE a thing happened is part of
	// what happened. A correction belongs between the tool rows it interrupted,
	// because that is the moment the person said it and that is the order the
	// engine's own journal keeps it in — and a page that gathered every
	// correction back up under the question would put them above every row of
	// the work, which on a turn long enough to scroll is above the screen.
	entrySteer
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
	// entryHarness is one harness design, live and settled, in the transcript.
	// The same entry changes shape so progress never leaves a dead note behind.
	entryHarness
	// entryConnect is ONE SIGN-IN (connect.go): the browser opening, the link
	// under it for whoever is not sitting at that browser, and the one line it
	// settles into.
	//
	// It is a kind of its own rather than a note for the reason entryCompact is
	// not a divider: a note is a static sentence, and this is a block that opens
	// waiting — with a spinner, and a link a person may need to copy — and closes
	// minutes later on an event nobody typed.
	entryConnect
	// entrySeam is the one line at the boundary a compaction left behind, drawn
	// when scrolling up crosses from the conversation the model still carries
	// into the conversation only the journal does (replay.go's [seamMark]).
	//
	// IT IS A BLOCK AND NOT A ROW, which is the opposite of the choice
	// [earlierMark] makes two doors down, and the difference is what each one is
	// about. That marker is a fact about the SCREEN — "the top of the frame is
	// not the top of the conversation" — and it moves as the frame does, so it is
	// painted at the frame's edge and belongs to nobody. This is a fact about the
	// CONVERSATION: it names the moment the model's memory of it was shortened,
	// and that moment stays where it happened while the reader scrolls on past
	// it. A line that could only be drawn at the top edge could not say it.
	//
	// It is still the surface's own line and not the session's: the journal holds
	// no such message, [fromTranscript] steps over it so a rewind cannot cut it,
	// and a rebuild earns it again from scratch.
	entrySeam
	// entryStanding is ONE STANDING ITEM touching the conversation (standing.go):
	// the ratification card while it is a question, and — in its other shape —
	// the single dim line an item that already stands writes when it has news.
	//
	// It is a kind of its own rather than a second [entryTask] because the two
	// blocks are answered by different engines and settle into different
	// records, and rather than a note because a note is a static sentence where
	// this opens as a question with a clock on it.
	entryStanding
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
	// actedTags are send-door words kept in the displayed sentence after they
	// were stripped from the payload. Mid-sentence slash prose has no ranges,
	// so a demoted tag stays plain in the transcript as promised.
	actedTags []segment
	// replyTags are the finished tasks this assistant block answers. They are
	// empty for every ordinary person-prompted reply.
	replyTags []session.TaskReplyTag

	// facts are the LOAD-BEARING DATA inside a note's own words, named by the
	// text they are spelled with and in the order they appear in it — the model
	// ids in a crew line, the figures in /status, the key chords in /help.
	//
	// THE PAYLOAD RULE (payload.go) is what reads them: a note's prose stays in
	// the dim tier this surface says everything about itself in, and each of
	// these steps up one rung so the answer inside the sentence reads at a
	// glance. It is empty on every note whose builder named nothing, and such a
	// note is drawn exactly as it was drawn before the rule existed.
	facts []string

	// block says this note's OWN LINE STRUCTURE is what it means, so the frame
	// fits each line to the width rather than re-flowing the paragraph
	// ([app.noteBlock] says why, and a subharness card is the only shape that
	// asks for it). It is false on every other note, which is nearly all of them.
	block bool

	// context is the NAMED WORKING CONTEXT this turn was routed into, in the
	// engine's own person-facing words (session's TaskNotice.Context) — and empty
	// for every ordinary turn, which is nearly all of them. It is set on the
	// person's own block and read by nothing else (turncontext.go states the law).
	//
	// IT IS TAKEN AT THE MOMENT THE TURN STARTS AND NEVER DERIVED AFTERWARDS. The
	// context a sentence went into is a fact about the past, and a transcript that
	// asked the live session where its old lines had gone would re-label a whole
	// history every time the person walked into a different room.
	context string

	// pictures are the resolved paths of the PICTURES this message carried, in
	// the tray order its `[image #n]` tokens and `[#n name]` markers use.
	// picturesHere says those paths name this machine rather than the engine's.
	//
	// IT IS A SLICE OF PATHS AND NOT CHIPS FOR [entry.hung]'s BUDGET REASON. A
	// chip's file flag is a fact about the tray and every path here is already a
	// picture; retaining that larger staging shape on every transcript block
	// would make the hot entry carry a distinction no renderer can read.
	pictures     []string
	picturesHere bool

	// steer is THE ONE CORRECTION this block is, on [entrySteer] and nil on every
	// other kind (steerelbow.go). It is a pointer for the reason [entry.card] and
	// [entry.stand] are: the outcome lands on the block minutes after it was
	// drawn, and the lane that lands it holds the block by index rather than
	// copying it.
	//
	// Nil on every block of every conversation nobody steered, which is nearly
	// all of them.
	steer *steerElbow

	// Tool fields.
	tool   string
	status toolState
	detail toolDetail
	// caption is the most recent narrator override for the step this call
	// belongs to. It stays on the call because captions are derived from the
	// entry list on every page, and the list is the one fact both pages share.
	caption string
	open    bool // this call's expansion is showing inline
	// full lifts the expansion's per-tool cap: it is set by a click on the
	// "… N more lines" foot, which is the person saying they want the rest.
	//
	// AND IT IS THE INSTRUCTION'S FOLD on a block that carries [entry.brief]
	// (brieffold.go), because it is the same fact about a block — this one is
	// shown whole — and a second flag saying it would be a second thing to keep
	// in step. The two never meet: one is a tool call and the other is a message.
	full bool
	// brief says this block is THE INSTRUCTION A NODE WAS GIVEN — the first thing
	// said on a task's own page, and the only message on this surface that folds
	// (brieffold.go). It is false on every other block, which is nearly all of
	// them.
	//
	// IT IS SET WHERE THE FACT IS KNOWN (replay.go's [roomReplay]) and
	// never worked out at render time, because [app.renderEntry] paints one block
	// at a time and must not ask what surrounds it — the same law the answer
	// hierarchy is stamped under ([app.deckRows]).
	brief bool
	// decision is what the person answered when this call was asked about —
	// "allowed" or "denied", dim, beside the row's stat (consent.go). It is
	// empty for every call the policy did not stop.
	decision string
	// bg is set when this running call was kept in the background — by ctrl+g,
	// the row's pointer door, or the session's clock (background.go): `job 3`,
	// dim, beside the row's stat, in the slot [entry.decision] already uses
	// because it is the same kind of fact. It is empty for every call nobody
	// promoted.
	bg string

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
	// formed is the TAIL of the one argument a still-arriving call is previewed
	// by — a write's file body, and nothing else on the belt ([formingPreviewField]).
	// It is what the row draws its live block from (toolview.go's [app.formingRows]).
	//
	// IT IS NOT THE ARGUMENTS AND IT IS NOT PARSED. session's [session.PartialString]
	// scans the streamed text for one field's value with the same tolerant scanner
	// the forming hints are built from, and what lands here is the decoded text
	// that scan returned — already bounded by session, already only the end of
	// it. The field above still says how much has arrived; this says what it
	// looks like. It is dropped the moment the announcement brings the whole
	// payload, because from then on [entry.detail] is the better answer.
	formed string

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

	// ran is what THIS call's own work took, measured where it ran and reported
	// the instant it was over (session.EventToolFinished). It is zero on every
	// row that has not had that news yet, and on every kind but entryTool.
	//
	// It exists because began-to-ended is the BATCH's span, not the call's: the
	// calls of one batch start together and their results are delivered together
	// after the last of them returns, so a five-millisecond `cd` beside a
	// fifty-second build spun under a climbing clock and then wrote fifty
	// seconds on its own row. When this is set it is the row's figure and the
	// row's clock stops (toolview.go's [elapsedWord], [app.countClock]).
	ran time.Duration

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
	// edge is how far the LIVE EDGE has been drawn (reveal.go). Zero means the
	// block is not pacing — settled history, anything the wire delivered
	// fine-grained, anything the clock has not been asked to walk. A block with
	// a lump still arriving holds the received bytes in [entry.text] and this
	// cursor is what the frame paints.
	edge int

	// mdHead is the PROMOTED HALF of a still-streaming block, already rendered,
	// kept beside the cut and the width it was rendered at (render.go's
	// [promotedHead]). It is a pointer for [entry.hung]'s reason: at most one
	// block on a deck is streaming, and every other entry would be carrying the
	// fields past three whole-deck passes per frame to say nothing with them.
	//
	// It exists because the two clocks on a streaming reply run at wildly
	// different speeds. A delta lands a hundred times a second and marks the
	// block stale; the promotion that moves [entry.mdCut] runs once every
	// [markdownThrottle], which is a second and a half. Every frame in between
	// re-ran [app.renderMarkdown] over the WHOLE settled prefix — sanitize,
	// goldmark, chroma, the lot — to arrive at rows identical to the ones it drew
	// on the last frame, when the only thing that had actually changed was the
	// plain tail underneath them ([app.assistantRows]).
	//
	// THE CUT AND THE WIDTH ARE THE WHOLE KEY, because they are the whole of what
	// decides these rows. The prefix is `text[:mdCut]` and a prefix cannot change
	// without the cut moving: text only ever GROWS at its end, and a promotion
	// only ever moves the cut forward ([promoteBlock] refuses a cut that did not).
	//
	// The one thing the key cannot see is the PALETTE, for the reason codeview.go's
	// block cache cannot: these are finished strings with escape sequences already
	// inside them, and a re-measured ground changes neither the text nor the width.
	// [app.repaintPalette] drops it by hand, with everything else that holds paint.
	mdHead *promotedHead

	// hung is the memo of the block a TOOL row hangs — its diff, its source, its
	// output — and toolview.go's [app.toolBlock] is the whole of its story.
	//
	// IT IS A POINTER TO KEEP THIS STRUCT SMALL. Every deck is a slice of these
	// and three passes walk the whole of one on every frame ([stampHierarchy],
	// [app.deckRows], THE INDENT LAW's own loop), so a hundred and fifty bytes
	// added to an entry is a hundred and fifty bytes of cache line spent by every
	// block on the screen to carry a memo only tool rows ever read. Measured: as
	// a value it cost the idle frame seven percent, which is most of what the
	// memo was buying.
	hung *toolBlock

	// demoted says THIS PROSE WAS NARRATION AND NOT THE ANSWER, and it is the
	// whole of THE ANSWER HIERARCHY as far as a renderer is concerned
	// (hierarchy.go states the law and [stampHierarchy] writes this field).
	//
	// It is DERIVED and never authored: a block is narration exactly when more
	// work opened after it inside the same turn, which is a fact about the entry
	// list's shape and about nothing else. So it is re-derived on every layout
	// from the list itself — a resumed conversation, a rewound one and the live
	// one all reach the same answer — and stored here only because
	// [app.renderEntry] paints one block at a time and must not walk the list to
	// find out which kind of block it is holding.
	//
	// FLIPPING IT INVALIDATES THE ROW CACHE, which is why nothing sets it by
	// hand: the demoted rendering and the promoted one are different rows, and a
	// block that changed tier while holding the rows it drew in the other one
	// would keep them ([app.entryRows] hands back the cache unless [entry.stale]
	// says otherwise).
	demoted bool
	// capHead says this demoted block lent its first line to the step heading,
	// and capCut is the byte immediately after that line. Both are derived with
	// the hierarchy and invalidate the block when they move.
	capHead bool
	capCut  int
	// cut says THE TURN THIS BLOCK BELONGS TO WAS STOPPED BY THE PERSON, and it
	// is the one part of the hierarchy that cannot be read off the list's shape:
	// a stopped turn and a finished one end with exactly the same blocks in
	// exactly the same order, and only the moment of the interrupt knows which
	// happened ([app.cutTurn] writes it, [app.interrupt] and [app.settle] call
	// it).
	//
	// AN INTERRUPTED TURN PROMOTES NOTHING. The turn ended without producing a
	// structural answer, so its trailing prose keeps the working tier for good —
	// the absence of a flush, full-ink block under the work is itself the
	// statement that no answer was reached.
	//
	// IT IS A FACT ABOUT THIS WINDOW. The journal keeps the words a stopped turn
	// managed to say and keeps no mark saying it was stopped, so a session
	// resumed later rebuilds that turn from its shape alone and reads its last
	// paragraph as an answer. That is the honest limit of a structural rule: the
	// alternative is a heuristic over the text, and this surface does not sniff
	// text to decide what a block is.
	cut bool

	// tables is which of this answer's markdown tables the person has opened,
	// by the ordinal they appear in (mdtable.go). Nil means every one of them is
	// closed, which is what an answer with no table in it stays.
	//
	// It is state on the ENTRY rather than on the surface because it belongs to
	// the block: an answer scrolls, a room is opened and closed, and a table a
	// person opened has to still be open when they come back to the sentence
	// they opened it for.
	tables map[int]bool
	// feet are the table affordances this block drew, keyed by their index in
	// its own rows. They are written by the same render that writes [entry.rows]
	// and cached beside them, because the columns a foot occupies are a fact
	// about a row that has already been laid out — see [tableFoot].
	feet map[int]tableFoot

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
	done    *taskDone
	harness *harnessCard
	// stand is the standing proposal — or the one line of news — this entry
	// draws, for kind entryStanding and for nothing else (standing.go). It is a
	// POINTER for [entry.card]'s reason: the answer lane holds the same card,
	// and the row and the verdict on it must never be able to disagree.
	stand *standingCard

	// pending says this is the PERSON'S OWN LINE, echoed before the engine has
	// agreed to take it — the gap a connection puts between pressing enter and
	// the far end answering (echo.go). It is false on every block of every
	// conversation held at this machine, and false again the instant the engine
	// confirms.
	//
	// It changes ONE thing about the block: the tier its words are painted in
	// (render.go's entryUser). No badge, no spinner, no colour of its own — the
	// ordinary case is a confirmation a few frames later, and a mark loud enough
	// to notice would read as something having gone wrong every time a person
	// sent a message.
	pending bool

	// The row cache. Its key names the three facts that can change painted rows:
	// which block owns them, the width they wrap at, and the ladder that supplied
	// their ink. Staleness is the block itself changing between two readings of
	// that key; built distinguishes "no rows yet" from "renders to no rows".
	rows     []string
	rowKey   renderedEntryKey
	identity uint64
	width    int
	built    bool
	stale    bool
}

// renderedEntryKey keeps the cache contract explicit. The cache lives on the
// entry, so identity looks redundant until a block value is moved or reused;
// keeping it in the key makes a cached slice incapable of answering for the
// next block that happens to occupy the same storage.
type renderedEntryKey struct {
	identity uint64
	width    int
	ink      uint64
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
		// echo names WHICH echoed line this answer settles, and ZERO when none
		// was drawn (echo.go). It is stamped where the line was drawn rather
		// than looked up when the answer lands, because by then the person may
		// have typed again: an answer that settled "whatever is marked now"
		// would take the mark off a message the engine has not seen.
		echo uint64
	}
	streamEventMsg struct {
		gen int
		ev  session.Event
		// then is the event that ENDED a fold and had to travel with it.
		//
		// [waitEvent] coalesces a run of deltas by taking them off the channel,
		// and the only way to find out whether an event folds is to have it in
		// hand — a channel cannot be put back. So the one event that stopped a
		// run rides beside the run it stopped, and [app.Update] applies the two
		// in the order they arrived. It is nil on every message that folded
		// nothing, which is every message a test builds and most of the rest.
		then *session.Event
		// lump says the run this message carries contained a single wire event
		// big enough to be one ([isLump]). It is decided HERE, in the only place
		// that can see the run's parts, because a fold of short deltas is not a
		// lump however long the fold is — the surface paces the wire's lumps and
		// never its own fold (reveal.go).
		lump bool
	}
	streamClosedMsg struct{ gen int }
	compactedMsg    struct{ err error }
	// frameMsg is the paint clock: it promotes whatever streamed since the
	// last one into a frame, and steps the animations.
	frameMsg struct{}
	// pointerMsg is the POINTER's clock, and it is a second one on purpose: it
	// spends the sweep that piled up while the last one was being answered and
	// then stops, where [frameMsg] dirties the frame every time it fires. A
	// pointer crossing a row it is already on must cost no frame at all, so the
	// thing that wakes it up may not draw one (coalesce.go).
	pointerMsg struct{}
	// historyPageMsg is one local page of the mirrored transcript returning to
	// the update loop. Even a hosted session therefore never waits on ssh in the
	// key or wheel path, and a conversation replaced while the command was out
	// rejects the old generation rather than prepending somebody else's words.
	historyPageMsg struct {
		gen     int
		entries []session.DisplayEntry
		from    int
		to      int
		earlier bool
		seam    bool
	}
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
	// designEventMsg is one event off the STANDING harness-design subscription
	// (harness.go), which is a lane of its own for the task lane's reason: a
	// design finishes after the turn that asked for it ended, when there is no
	// stream left for the card to land on.
	designEventMsg struct {
		gen int
		ev  session.Event
	}
	designLaneClosedMsg struct{ gen int }
	// orchEventMsg is one event off the STANDING adaptive-run subscription
	// (the run lane below), which is a lane of its own for the design lane's
	// reason: a run's notes, its gauge and its fuel gate all arrive long after
	// the turn that started it ended.
	orchEventMsg struct {
		gen int
		ev  session.Event
	}
	orchLaneClosedMsg struct{ gen int }
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
	// orchPollMsg is a run's page asking to re-read its run (roomorch.go). An
	// adaptive run publishes a SNAPSHOT rather than streaming its shape, so the
	// page that draws the shape has a clock where the others have a lane — and it
	// carries the room's generation for those lanes' own reason: a tick still in
	// flight when a page closes must not poll on behalf of the page that
	// replaced it.
	orchPollMsg struct{ gen int }
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
	// The two messages the default model provider's browser connection takes
	// (firstrun.go). The first carries the listener after it is standing, so the
	// link can be opened before the second waits for the browser to come back.
	openRouterFlowMsg struct {
		id   uint64
		flow OpenRouterFlow
		err  error
	}
	openRouterKeyMsg struct {
		id  uint64
		key string
		err error
	}
	// hudFadeMsg is a BOUNDED catch-up tick: the two wakeups a settled turn
	// schedules so its fresh numbers can go quiet on time (render.go's fade).
	// There is deliberately no idle ticker behind it — a surface with nothing
	// happening on it wakes twice and then stops.
	hudFadeMsg struct{}
)

type app struct {
	// ruler measures a string the way the RENDERER will draw it rather than the
	// way this package would prefer to read it. The two disagree about a
	// variation-selector emoji and a flag, and the rail bent two cells wherever
	// one appeared (cellwidth.go).
	ruler cellRuler
	ctx   context.Context
	agent Agent
	fresh func() (Agent, string, error)
	// start and open are the agent-building seam (tui3.go's [Conversation]):
	// they hand back the agent AND the closures minted around it, so /new and a
	// resume rebind the approval trio, the recent list, the draft and the recall
	// list in the same breath as the agent. They are preferred over fresh and
	// resume wherever both are wired; the pair below is what a door that cannot
	// answer the seam still gets ([app.nextConversation], [app.openConversation]).
	start           func(workspace string) (Conversation, error)
	open            func(workspace, transcript string) (Conversation, error)
	anchorWorkspace func(path string) (string, error)
	// shared is [Options.SharedAgent]: this door's fresh and resume seams select
	// a conversation IN PLACE on one handle rather than building a second agent.
	// It gates the keeper (keeper.go's [app.stow]) and the close that follows a
	// swap (welcome.go's [app.openSession], [app.renew]); the option states the
	// whole contract and why each of those two would otherwise be wrong.
	shared bool
	// openTaskOwner attaches a second view onto a conversation that is already
	// running, for the length of one task page (tui3.go's [Options.OpenTaskOwner]
	// states the whole contract, taskowner.go is the only caller). Nil is a
	// window with no engine road, which answers with the card instead.
	openTaskOwner func(TaskOwnerAsk) (TaskOwnerView, error)
	// taskOwnerGen numbers the attaches this window has asked for and taskOwnerAt
	// is the one still in flight. An answer carrying an older number is a view
	// nobody wants any more: it is CLOSED on arrival rather than drawn, which is
	// what keeps a slow engine from opening a page over whatever the person went
	// to instead ([app.tookTaskOwner]).
	taskOwnerGen uint64
	taskOwnerAt  taskOwnerAsk
	// workspace is the directory this conversation is about, whole; place is
	// its base name, which is what the status line has room for. The whole path
	// is what history is keyed by and what the @ completion walks.
	workspace string
	place     string
	file      string
	build     string
	resumed   bool
	// previews holds the pictures this surface has already drawn as half blocks
	// (imagepreview.go), keyed by the file, its mtime and the shape it was drawn
	// for. An open picture call is re-rendered on every frame, and decoding a
	// megapixel png ten times a second is the one thing this surface must not do.
	previews map[string]imagePreview

	// feed is this conversation's transcript and the reducer that grows it
	// (feed.go). It is EMBEDDED and not a field with a name, so that `a.entries`,
	// `a.live`, `a.think` and `a.turn` still mean what they have always meant to
	// the several hundred readers of them in this package — the reducer moved
	// house, and the surface did not have to be respelled to follow it.
	feed
	// transcript is the surface's local mirror of the engine transcript. The
	// opening read may cross ssh; every page walked afterwards is cut from this
	// slice on the local side.
	transcript []session.DisplayEntry
	// echoAt is the person's own line drawn before the engine agreed to it, or
	// -1 when there is none — which is always, on a surface that is not hosted.
	// echoTok is the token that names it, counting from one so that zero means
	// "settles nothing" (echo.go).
	echoAt  int
	echoTok uint64
	// replayFrom is where the DRAWN conversation starts in the session's own
	// transcript, and replayFloor the turn number the first drawn block carries.
	// Together they are everything [app.backfill] needs to hand up the helping
	// above the top of the screen when somebody scrolls into it (replay.go).
	//
	// The index is counted from the START of the transcript on purpose: a
	// journal only ever grows at its end, so an index taken at open still names
	// the same block an hour of turns later — where a count back from the end
	// would have slid forward under every one of them.
	replayFrom  int
	replayFloor int
	// earlier is the conversation a compaction edited away — what
	// [session.Agent.EarlierHistory] hands over — and earlierFrom is where the
	// DRAWN conversation starts inside it, exactly as replayFrom names a place in
	// the live transcript.
	//
	// THE TWO ARE SPLICED, NOT STACKED. earlierFloor is where the live
	// transcript stops being new conversation and starts being the pass's own
	// rewritten copy of the region — stubs where the results were, one line where
	// a long run of work was. The backfill walks the live transcript down to that
	// floor and then carries on into the region, so the conversation is drawn
	// once and drawn in the words it was said in (replay.go).
	//
	// It is fetched once per replay rather than on demand: [app.moreHistory] is
	// asked on every frame, and the floor has to be known from the first one.
	earlier      []session.DisplayEntry
	earlierFloor int
	earlierFrom  int
	// earlierSeam says the seam row has been drawn already, or that there is no
	// seam to draw. A compaction that fires while the surface is open sets it
	// true without drawing anything: the pass puts its OWN row on the screen at
	// exactly that boundary (the entryCompact block), and a second line saying
	// the same thing two rows above it is the surface stuttering.
	earlierSeam bool
	// historyLoading admits one page command at a time, and historyGen makes its
	// answer belong to the replay that asked for it.
	historyLoading bool
	historyGen     int
	// unfolded holds the turns whose tool cluster is showing every call.
	unfolded map[int]bool
	// workOpen is the ephemeral expansion state of completed-turn workfolds.
	workOpen map[int]bool
	// capOpen is the second expansion under the outline, keyed by the caption's
	// start in this page's own entry list.
	capOpen map[int]bool
	// sel is the selected entry, a negative caption key, or -1. ↑/↓ move it;
	// enter opens whichever kind of row supplied it.
	sel int
	// hot is what the pointer is over (hover.go). The zero value is nothing.
	hot hoverAt
	// drag is the left button's gesture in flight — a parked body click, or a
	// sweep whose rows wear the selection — and the flash pair under it is what
	// the status line says the last sweep copied (dragselect.go).
	drag       dragSelect
	dragCopied int
	dragUntil  time.Time
	// dragLit is the selection the last release copied, kept so its cells stay
	// lit while the status line still says "copied", and dragChars how many
	// characters it was when it fit inside one row (dragselect.go's
	// [app.dragSel] and [app.dragWord]).
	dragLit   dragSelect
	dragChars int
	// clickAt, clickX, clickY and clicks are the multi-click count: a press
	// soon and near the last is the same gesture's second or third click
	// (dragselect.go's [app.countClick]).
	clickAt        time.Time
	clickX, clickY int
	clicks         int

	// ── the effort ladder's surfaces (effortscope.go) ──
	//
	// effortLit is the ONE rung [effortKey] most recently moved, and when — the
	// scope name and the instant, so the clause or the chip that states that rung
	// can step up the reading ladder while it is news and come back down on the
	// status line's own fade. It is one and not a map because only one rung can be
	// the newest fact on a screen, and that holds ACROSS the four surfaces: the
	// tray's chip (effortchip.go) records itself here too, so lighting a task's
	// card is the same act as taking the emphasis off the chip.
	effortLit effortMoved

	state runState
	model string
	// title is the name the session gave itself, shown left of the model. Empty
	// until the session has one (session's title.go names it after the first
	// completed turn); a resumed session opens with the name it already had.
	title  string
	cost   float64
	tokens int
	// dayCost is what this MACHINE has spent since midnight and dayCosted
	// whether anything counted it at all — the pair the Spending tab's `today`
	// receipt is drawn from (settingspend.go). It is a reading taken on the way
	// into the settings panel and held for as long as the panel is up, so every
	// row that quotes the day quotes one figure.
	dayCost   float64
	dayCosted bool
	// tree is what this conversation AND the work it started have spent, read
	// off the usage ledger on the frame clock while there is work to read about,
	// and treeCache is the tail-reading cache that makes re-reading it cheap
	// (treespend.go). Both belong to this surface's own goroutine, which is
	// [session.UsageCache]'s own condition for being used at all.
	tree      session.Receipt
	treeCache session.UsageCache
	// spendRail is this conversation's own ceiling as the profile last read it,
	// and railRead whether it has been read at all. The pair is held rather than
	// asked for because the status line's ink consults it on EVERY paint
	// (moneydoor.go's [app.moneyNearRail]).
	spendRail float64
	railRead  bool
	// ctxWindow is the model's context in tokens as this surface last set it,
	// and ctxTokens what the conversation currently weighs. The pair is the
	// meter in the status line. The window is TRACKED rather than asked for
	// because session exposes no getter — the surface is the one that tells the
	// agent (see [app.switchModel]), so the surface is the one that knows.
	ctxWindow int
	ctxTokens int
	// shownCost, shownTokens and shownCtx are the figures the status line
	// paints while a turn is running (reveal.go). The books stay on cost /
	// tokens / ctxTokens; these chase them on the paint clock so a reading
	// that jumped by a thousand tokens writes the new figure rather than
	// popping it. meterChasing is whether this turn has asked them to.
	shownCost    float64
	shownTokens  int
	shownCtx     int
	meterChasing bool
	// revealMoved is when the live edge and the meters last stepped, on this
	// surface's own clock ([app.now]). The walk is a function of TIME and not of
	// how many frames were painted (reveal.go's [app.tickReveal]), and this is
	// the one stamp it is measured from — one per surface, because every edge on
	// it walks on the same clock.
	revealMoved time.Time
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
	// every turn whose model published BOTH a prompt price and a cache-read
	// price to work the difference out from (see [app.cacheNote], which is the
	// one place it grows). It is what turns the
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
	workMode   string
	// bashBackgroundAfter is the foreground command's ARMED session clock in
	// seconds, handed over with the agent at boot. It is never re-read from this
	// surface's profile: a settings change belongs to the next session, and over
	// --host that profile is on another machine. Zero leaves the command's own
	// timeout as the only bound (toolview.go).
	bashBackgroundAfter int
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
	// has uncommitted work — what follows the conversation's name at the left
	// end of the input's legend (render.go's [app.branchWord]).
	// Empty branch means "no answer", which is what a directory that is not a
	// repository, a git that is not installed and a probe that timed out all
	// look like from here.
	branch      string
	branchDirty bool
	// gitProbe is the seam onto that answer, so a test can pin a branch without
	// a repository and the surface can be driven with no git at all. Nil is
	// [gitHead].
	gitProbe func(dir string) (string, bool, bool)
	// tilde is what "~" abbreviates in the place's path, read once at boot.
	// It is NOT the home surface (home.go) — this is one string, the person's
	// home directory, and it was called `home` until a screen by that name
	// existed.
	tilde string
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
	// The burn rate's hold: the figure currently ON the line, and when it was
	// adopted (render.go's [app.holdBurn]). It is a display fact and nothing
	// else — every meter the rate is computed from keeps its exact count.
	burnShown string
	burnAt    time.Time
	// modelSpan is where the model's name was last drawn on the status row, in
	// columns, and it is the whole of what makes that name PRESSABLE: written by
	// the layout, read by the click (render.go's [app.identityParts], and
	// [app.statusPress] below). An empty span means there is nothing to press.
	modelSpan hudSpan
	// keepSpan is where the `keeping an eye on N` segment was last drawn, and
	// keepRow which of the status row's rows it landed on — the same bargain
	// modelSpan makes, for the same reason and one more: that cluster is
	// right-aligned, so where a segment sits depends on every segment beside it
	// and on the frame's width, and only the layout can answer it
	// (standdoor.go, render.go's [app.statusRows]).
	keepSpan hudSpan
	keepRow  int
	// moneySpan is where the money segment was last drawn, and moneyRow which of
	// the status row's rows it landed on — the same bargain keepSpan makes, for
	// the same reason. It is the door onto the Spending tab (moneydoor.go).
	moneySpan hudSpan
	moneyRow  int
	// stripSpans is where the task strip's chips were last drawn, and stripMore
	// the columns of its overflow mark — the same bargain modelSpan makes, for
	// the same reason: the row that lays the chips out is the row that knows
	// where they landed (taskstrip.go's [app.stripRow] and [app.stripPress]).
	stripSpans []stripSpan
	stripMore  hudSpan
	// stripHarn is where the running subharness's chip was last drawn, or the
	// zero span when none is running (harnesspanel.go). It is kept apart from
	// stripSpans because it opens a different door: a node chip opens that
	// node's room, and this one opens the registry.
	stripHarn hudSpan
	// jumpSpan is where the jump-to-latest chip was last drawn, in columns — the
	// same bargain again, for a chip that is right-aligned and so knows its own
	// columns only once the frame has chosen a width (jumpchip.go's
	// [app.jumpChip] and [app.jumpPress]).
	jumpSpan hudSpan
	// steerDoor is where the waiting message's `→ steers it in` clause was last
	// drawn, in columns, or the zero span on a frame that did not draw it — the
	// same bargain jumpSpan makes, for the same reason: the line that lays the
	// clause out is the only thing that knows where it landed, because what sits
	// in front of it on that line depends on how many messages are waiting and how
	// wide the frame is (park.go's [app.parkedRows], steer.go's
	// [app.steerDoorPress]).
	steerDoor hudSpan

	// hud is the cached answer to the two questions the telemetry asks of the
	// whole conversation — how much background work is alive, and what the
	// session has written — and hudStale says the entries have moved since it
	// was computed. Both walks read every tool call's arguments, which is a JSON
	// parse per row: at thirty frames a second, on a session with four hundred
	// rows, that is the status line costing more than the conversation.
	hud      hudStats
	hudStale bool
	// nodeHud is what each of this session's NODES has added to those same two
	// questions (docs/design/lens/DESIGN.md, Decision 4). A task's `bash` with
	// background:true starts a process on this machine exactly as the
	// conversation's does, and until it was counted the Σ segment and the quit
	// guard were silent about every one of them.
	//
	// IT IS KEYED BY NODE AND KEPT FOR THE SESSION because a room's entries are
	// dropped when its page closes ([app.closeRoom]), and a count that vanished
	// when somebody stopped looking would be a count nobody can act on.
	// [app.tallyNode] is the only writer.
	nodeHud map[uint64]hudStats

	// stream is the channel being pumped and gen its generation. gen is
	// bumped by every Submit so that a late event from an abandoned stream can
	// be recognized and dropped.
	stream <-chan session.Event
	gen    int
	// streamStop leaves a turn this surface JOINED rather than started
	// (switcher.go's [app.joinTurn]): the stream came back from
	// [session.Agent.Attach], which hands out a stop precisely because a reader
	// that walks away without one parks a pump for the rest of the turn. It is
	// nil for the ordinary stream, which a Submit handed over and which the hub
	// closes at turn end.
	streamStop func()
	// stops are the standing lanes this surface holds on the conversation in
	// front, each with the function that leaves it (switcher.go).
	stops laneStops

	// behind is every conversation this process holds that is not the one on
	// screen: the agent, still running, the bundle the door built around it, and
	// the few surface readings that would be lost when the surface stops drawing
	// it (keeper.go).
	//
	// THE KEY IS THE CANONICAL TRANSCRIPT PATH ([convKey]), because that is what
	// home names a row by and what the flock is taken on.
	behind map[string]*kept
	// homeGen is home's own clock generation. It belongs to the SURFACE rather
	// than to any conversation, because there is one home — and it is bumped by
	// every close, so a tick armed by a home that has since been closed cannot
	// start a second self-rearming chain (home.go's [homeTickMsg]).
	homeGen int
	// prev is those keys with the most recently in front LAST — what `tab` walks
	// and what a close brings forward. Every close filters it, so a key in here
	// that the keeper no longer has is stepped over rather than trusted.
	prev []string
	// stirs is the one lane that belongs to no conversation: a key, from the
	// watcher of a conversation nobody is drawing, saying "look at this agent
	// again" (keeper.go's [behindStirMsg]).
	stirs chan string

	// lastDelta is when text last arrived. The live reply's own markdown clock
	// sits beside the block it belongs to ([feed.mdAt]).
	lastDelta time.Time

	// awaited is when a model request was last believed to go out with NOTHING
	// back from it yet, or the zero time when the stream has spoken since.
	//
	// THE SESSION EMITS NO "REQUEST SENT" EVENT, so this is inferred rather than
	// reported, and the inference is stated here so that nothing downstream has
	// to guess at it: a turn opening and a tool batch closing are the two moments
	// after which the loop's very next act is a request, and the first thing the
	// stream says afterwards is the request being answered. The surface times
	// from the former and stops at the latter. It cannot see the wire, so it
	// never claims to — see [app.waitingWords] for what it is allowed to say.
	awaited time.Time

	// stopBy is when a turn the person STOPPED gets let go of whether or not the
	// engine has finished with it: the deadline on the winding-down window, set
	// at the keypress and cleared when the stream closes ([app.interruptTurn],
	// [app.stopSweep]).
	//
	// It is the zero time whenever no stop is in flight, and it is the ONE fact
	// this surface holds about the second stage — everything else about it is
	// derived from the clock ([app.stopLeft]) or performed by the engine
	// (session's abandon.go).
	stopBy time.Time

	// retrying says the request the clock above is timing is a SECOND ATTEMPT:
	// the one before it was cut and asked again (session.EventRetrying).
	//
	// It is the one fact that separates "waiting for" from "trying again", and it
	// is REPORTED rather than inferred — the surface is never allowed to guess
	// that a wait is a retry, because a person who reads "trying again" on a
	// first attempt has been told something that did not happen.
	retrying bool

	// The paint clock. dirty says the row list no longer matches the entries;
	// painting says a frameMsg is already on its way, so a burst of deltas
	// schedules one tick and not one each. paints counts frame SLOTS of
	// [frameInterval] — one per frame locally, three per frame over a link
	// (link.go) — and drives every animation on this surface; builds counts
	// layouts and exists so a test can assert the coalescing without sleeping.
	dirty    bool
	painting bool
	paints   int
	builds   int
	// renders counts actual entry renderer calls so allocation laws can prove a
	// viewport move did not repaint a block whose key stayed the same.
	renders uint64
	// renderIdentity mints the identity half of an entry's cache key lazily;
	// inkState advances whenever the terminal's measured ladder changes.
	renderIdentity uint64
	inkState       uint64

	// ptr is the pointer's fold: the sweep's newest position and the notches of
	// a wheel run, kept so that a burst of them costs the surface one answer per
	// frame instead of one per cell (coalesce.go).
	ptr pointerFold

	// drop is the keystroke-shaped drop's fold: where in the draft a run of
	// characters that might spell a dropped file's path began, kept so that a
	// terminal which TYPES a drop instead of pasting it lands the same chip
	// (dropkeys.go).
	drop dropFold
	// wsl is the boot reading that gives every attachment door one answer to a
	// Windows path. Detection and wsl.conf are read once when the process starts;
	// keeping the result here lets tests state either machine without depending
	// on the machine running them (attach.go).
	wsl wslPaths

	// shown is the last frame this surface declared and drawn says there is one.
	// Bubble Tea calls [app.View] after EVERY message — the terminal WRITE is on
	// its own 60Hz clock, but the frame is BUILT per message — so a message that
	// provably changed nothing is a frame built for nothing and thrown away. A
	// folded motion is exactly that message, and a sweep is six hundred of them
	// (coalesce.go's `still`).
	shown tea.View
	drawn bool

	// rows is the last laid-out screen list, and rowsWidth the width it was
	// laid out for.
	rows      []row
	rowsWidth int

	width, height int
	offset        int
	stick         bool
	// sizing says a resize is still settling, so the scroll clamp that a new
	// size asks for is already on its way and a second one would be a second
	// relayout for nothing (see [app.resized]).
	sizing bool

	pal palette
	// mdBase is the painter this surface's environment built at construction
	// ([stylerFor] over [Options.Env]), and the one every measurement re-inks
	// from. It is a field rather than a package once so that the profile prose
	// paints in comes from the same table as every other fact the surface reads
	// off the shell — a suite that hands [newApp] a TERM is handed a styler that
	// believes it (markdown.go says what went wrong when it was not).
	mdBase *tokens.Styler
	// mdStyler is the painter prose is handed when this surface has MEASURED its
	// terminal, and nil is the whole of "it has not" — every surface that never
	// hears back from its terminal reads [app.baseStyler]'s, for the reasons
	// markdown.go states. See [app.styler]: this field is the seam THE GLARE LAW
	// crosses when the ground stops being assumed.
	mdStyler *tokens.Styler
	// codeCache is the painted rows of the last few source blocks this surface
	// lexed (codeview.go). Tool rows are drawn fresh on every frame by design, and
	// this is what stops that from meaning "lex eight hundred lines thirty times a
	// second" the moment somebody lifts a big read's cap.
	codeCache codeBlockCache
	input     editor
	// composerOwner is WHO THE BOX IS TALKING TO, and composers are the boxes of
	// everybody it is not talking to right now (recipient.go). The zero value is
	// the conversation, so a surface that never opens a page pays one comparison
	// and holds an empty map.
	//
	// THEY EXIST BECAUSE THE BOX IS ONE BOX AND ITS READER IS NOT ONE READER. An
	// unsent sentence for the model, a click on a task's row and one enter used to
	// send that sentence to the task: nothing on screen changed, so nothing the
	// person could see was wrong, and the words were read by somebody they were
	// never written for.
	composerOwner recipient
	composers     map[recipient]composerState
	// sends is the recipient-in-front's half of the outbox: its messages that
	// have left the box and not settled (recipient.go's [outboxSnapshot]). It
	// rides beside the box for the reason the tray does — it is part of what that
	// recipient is holding — and goes into the stash with it.
	sends []outboxSnapshot
	// keptElsewhere is what this window's draft record holds for ANOTHER
	// conversation and can neither show nor throw away (draftkeep.go). It is
	// carried through every write untouched, so a name the operating system
	// handed back to a second process does not delete the first one's unsent
	// lines.
	keptElsewhere []draftKeepSlot
	// keptFrom is the draft file [app.keptElsewhere] was read for, so the read
	// happens once per conversation and not once per keystroke.
	keptFrom string
	// pastes hold the documents represented by the compact tokens in input. The
	// text stays beside the composer because only submit needs to cross the agent
	// seam, and a surface-side edit must never become a wire call.
	//
	// They are the RECIPIENT'S, exactly as the text above them is: a compact chip
	// typed into a task's page is part of that page's unsent line and is stashed
	// and laid back out with it (recipient.go).
	pastes []pasteChip
	// pasteEdit is the one modal editor over the composer. Its zero value is
	// closed, so an ordinary frame pays only this boolean check.
	pasteEdit pasteEditor
	// pick is the model overlay (palette.go). Closed, it costs the frame
	// nothing; open, it owns the keyboard and the bottom of the screen.
	pick picker
	// crewPick is the three-row /crew chooser (crew.go). It is separate from the
	// model picker because it has no filter and every item always takes two lines.
	crewPick crewPicker
	// effPick is the five-row thinking chooser the tray's dial opens
	// (effortchip.go). It is the crew chooser's shape for the crew chooser's
	// reason: a fixed ladder is a thing you read rather than a thing you search.
	effPick effortMenu
	// effortSpan is where the thinking dial was last drawn on the tray, in
	// columns from the box's own left edge — the same bargain [app.jumpSpan]
	// makes, because the layout is the only thing that knows where a
	// right-aligned cell landed.
	effortSpan hudSpan
	// wait is the forming block a task command is standing in — its verbatim
	// brief, present phase, and clock (taskcommand.go). It keeps that live region
	// out of the notes lane while driving its shared spinner and count-up.
	// waits are the forming blocks up right now — a task command that is sizing
	// or shaping, and a proposal whose yes is waiting on the same call
	// (taskcommand.go). It is a LIST because two of them can be in flight at
	// once, and three tall blocks stacked at the transcript tail is a wall;
	// several share one block (formingblock.go).
	waits []preflight
	// waitSeq hands out the identity a settle comes back with, and waitAt is the
	// row of the block a person is pointed at — the only one whose preview draws.
	waitSeq uint64
	waitAt  int
	// mem is the memory place's state: the snapshot it is drawing, the shelves that
	// are unrolled, and the filter (place_memory.go).
	mem memoryPlace
	// memory is the store the place reads and changes. It is optional because
	// memory-off sessions must have no capability behind the place.
	memory memoryStore
	// searchStore is the conversation index the search place reads, while
	// searchStatus names the web plug the session's next call will use.
	// usageLedger is the file the spend place reads. Each is optional and absent
	// rather than broken when it is: search says what it is for, /status keeps no
	// empty row, and an empty ledger draws the spend place's own teaching.
	searchStore  SearchStore
	searchStatus func() string
	usageLedger  string
	ledger       func(time.Time) ([]session.UsageLine, bool, bool)
	archive      func(string, bool) error
	// world is the walk of the machine THE SESSION RUNS ON, and farPlaces is the
	// state root it was walked under. Nil and empty are this process's own disk,
	// which is every local launch; over --host the door fills both and the places
	// stop listing the laptop (tui3.go's [Options.World], [app.worldRoot]).
	world     func() (session.World, bool)
	farPlaces string
	// farRecord is ONE ROW of that machine's record, read deeper than the walk
	// reads it: the last thing one piece of work said, out of the journal it left
	// over there. Nil is this process's own disk, which is every local launch —
	// the card opens the journal itself then (tui3.go's [Options.TaskRecord],
	// taskrecord.go's [app.readTaskTail]).
	farRecord     func(uri string, tail int) (session.TaskRecord, error)
	farRoomRecord func(id uint64, tail int) (session.TaskRecord, error)
	farTasks      func() ([]session.TaskIndexEntry, bool)
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
	// askResume is a countdown handed back by a switch: what was LEFT of the
	// clock on a question this surface stopped drawing when it went to another
	// conversation, and askResumePaused whether that question was already
	// paused (switcher.go's [aside]).
	//
	// IT IS CONSUMED BY THE NEXT QUESTION TO RAISE ITS CLOCK and by nothing
	// else ([app.startAskClock]), because a question replayed out of the turn's
	// backlog is the SAME question the person was looking at — the engine is
	// still blocked on it — and giving it a fresh ten seconds would be the
	// surface being generous with somebody's attention rather than honest about
	// it. Zero is the ordinary case and means "stamp the whole clock".
	askResume       time.Duration
	askResumePaused bool
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
	// THE HARNESS SIDE (harness.go). harnessAsks are the subharness offers
	// waiting for an answer, oldest first — a question about the TURN rather
	// than about a call or an account, one row under the connect offer and
	// owning the keyboard on the same terms. harnessTaps is where that row's two
	// answers were last drawn, which is the bargain the two blocks above it make.
	harnessAsks []harnessAsk
	harnessTaps []harnessTap
	// roomApprovalTaps is where the design approval row's two chords were last
	// drawn, on exactly the terms harnessTaps is kept: the spans are written by
	// the layout and read by the pointer, so a press can never answer about a row
	// drawn on an earlier frame (roomapproval.go). There is no queue beside it —
	// a room stands in front of one design and no more.
	roomApprovalTaps []roomApprovalTap
	// harnessStep is the step a running subharness last finished, as one line
	// (harness.go's [app.stepHarness]). It is a FIELD and not an entry because it
	// is replaced in place: the run's report carries the whole trail, and a step
	// left in the transcript would be that trail written twice.
	harnessStep string
	// designLane is the standing subscription to what the harness DESIGNER is
	// doing (harness.go's design lane) and designGen the generation it belongs
	// to. It is a lane of its own rather than the turn's stream because a design
	// outlives the turn that asked for it, exactly as a task node does.
	designLane <-chan session.Event
	designGen  int
	// orchLane is the standing subscription to the ADAPTIVE RUNS this session is
	// driving (roomorch.go draws them) and orchGen the generation it belongs to.
	// It is a third standing lane for the design lane's reason and one more: a
	// run outlives its turn by construction, and the fuel gate — the one event
	// here that is a question — arrives when there is no turn left to carry it.
	orchLane <-chan session.Event
	orchGen  int

	connAsks  []connAsk
	connTaps  []connTap
	conns     Connections
	connPanel connectPanel
	// harn is the subharness registry (Options.Harnesses) and harnPanel the
	// list /harness opens over it (harnesspanel.go). A nil harn is a surface
	// that cannot show harnesses and says so; nothing about the OFFER depends on
	// it, because that path runs entirely on session events (harness.go).
	harn      *subharness.Store
	harnPanel harnessPanel
	// harnPick is the filtering list "/harness " opens over that same registry,
	// and harnChip the name it was answered with — the one harness the next
	// message will run, held in the tray above the box rather than in the draft
	// (harnesspick.go).
	harnPick  harnessPick
	harnChip  string
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
	// stop is the confirmation standing over a request to END work, or nil
	// (stop.go). It shares the guard's slot above the draft and its keyboard
	// rung: both are the surface holding a keystroke back until it is told
	// whether to act on it, and neither can be raised while the other is up.
	stop *stopCard
	// roomStop is where the ✕ was drawn on the room's pinned header, in columns,
	// or the empty span when there is nothing there to stop. Written by
	// [app.roomHead] at layout and read by [app.stopMarkPress], which is the
	// bargain every pointer target on this surface makes.
	roomStop hudSpan
	// roomBackSpan is the padded Back action in the breadcrumb row.
	roomBackSpan hudSpan
	// crumbs is where the breadcrumbs were drawn on the frame's first row, in
	// columns, and what each of them opens (roomcrumbs.go). It is written by the
	// draw — [app.roomHead] — and read by the press and the
	// hover, on [app.roomStop]'s bargain exactly: a click resolves against what
	// was actually laid out, never against a second computation of it.
	crumbs []crumbHit
	// chatTabs is the conversations this window has been in, in the order it
	// first entered them, and chatTabHits is where the strip drew each of them
	// (chattabs.go). The order is the strip's own and not the recency stack's:
	// a row of tabs that re-ordered itself on every switch would be a row a
	// person cannot reach for by position.
	chatTabs    []chatTab
	chatTabHits []tabHit
	// chatTabWho is [app.convKey] for the conversation in front, remembered against
	// the file it was taken from, because that key is a disk question and the
	// strip asks it on every frame (chattabs.go's [app.frontTabKey]).
	chatTabWho tabIdentity
	// chatTabBar is the strip as it was last laid out, kept from frame to frame
	// (chattabs.go's [tabBar] states the whole of why).
	chatTabBar tabBar
	tabView    tabViewport
	// tabShut is the conversations whose TAB has been dismissed — the whole of
	// the new state the ✕ on a tab costs (chattabs.go's [app.tabDismiss]). The
	// conversation itself is untouched: still held, still running, still on the
	// switcher. It is a map rather than a slice because [app.tabList] asks it per
	// tab on every frame, and a key leaves it in [app.rememberOpen], which is the
	// one door every road that brings a conversation forward goes through.
	tabShut map[string]bool
	// closedTabs is the tabs this window has shut, oldest first, and it is what
	// `ctrl+shift+t` walks back through (tabreopen.go's [app.reopenClosedTab]).
	// It is window-local and lives no longer than the session: a reopen is the
	// undo of a gesture made in this window, and nothing about it is written
	// down anywhere. It is bounded by [tabsCap], the row's own cap.
	closedTabs []chatTab

	// THE PASTE BRACKET. pasting says the terminal has opened one and not yet
	// closed it; pasted is what has arrived inside it; pasteAt is when the last
	// thing did, which is the only defence against a bracket that never closes.
	// See [app.paste] for what these three are for — it is the whole of the
	// paste fix, and it is not the obvious mechanism.
	pasting bool
	pasted  []rune
	pasteAt time.Time
	// keysDisambiguated says THIS TERMINAL ANSWERED THE KEYBOARD-ENHANCEMENT
	// QUERY, which is the one honest way to know whether a chord like
	// `shift+enter` can reach this program at all rather than arriving as a bare
	// `enter` (bargein.go). Bubble Tea asks on every frame and hands the answer
	// back as a tea.KeyboardEnhancementsMsg; a terminal that cannot speak the
	// protocol simply never replies, and false is what that silence means.
	//
	// IT GATES AN ADVERTISEMENT AND NOT ONLY A KEY. The capability law's harder
	// half is that a hint naming a chord the terminal will never deliver teaches
	// a person that this surface lies to them, so [app.bargeOffered] reads this
	// before anything else it asks.
	keysDisambiguated bool
	// follows are the messages typed with ctrl+q while a turn ran, each holding
	// the stream the turn it starts will speak on — and the woken turns waiting
	// on the same door, which are streams with no message at all (followup.go).
	follows []queued
	// parks are the messages typed with plain enter while an answer was still
	// coming: held HERE rather than handed to the session, so they can still be
	// edited, taken back, or sent early with esc (park.go). Each one goes as an
	// ordinary turn of its own, oldest first, one per finished turn.
	parks []parked
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
	task *taskCard
	// THE STANDING SIDE (standing.go, homestanding.go). stand is the standing
	// card that owns the answer lane, or nil; stands is the seam onto the store
	// home draws items out of and writes a pause or a stop back through. Both
	// are nil on every surface whose door has not wired the ambient side, which
	// is a surface where no card is ever drawn and home shows no item band — a
	// capability that cannot work is absent, not broken.
	stand  *standingCard
	stands StandingSeam
	// THE LINK SIDE (hostlink.go). link is what the door can tell this surface
	// about the connection the conversation is on the far end of. Its zero value
	// is every local session — no segment, no notice, no waiting room — which is
	// the same absence the seam above draws when the ambient side is off.
	link LinkSeam
	// watchSpaces counts the run of spaces a WATCHER has typed, which is how the
	// door home is reached from a register with no box on the frame
	// (watching.go's [app.watchKey]). It is zero everywhere else.
	watchSpaces int
	// linkLatency is the hosted connection's rolling round trip, and
	// linkPingAsking keeps its slow clock to one call at a time. Both are zero on
	// every local session and before the first hosted answer, which the
	// emptiness law draws as no segment at all.
	linkLatency    time.Duration
	linkPingAsking bool
	// spell is the spell-it-out block under the draft, and the call that made it
	// while one is out (spellout.go). Its resting state is the zero value, which
	// is every frame of a conversation nobody has pressed the chord in.
	spell spellState
	// keepN, keepFiring and keepAt are the status segment's cached reading of
	// the store, and keepAt is when it was taken ([app.keepingCount] says why a
	// segment asked on every frame may not walk a directory).
	keepN      int
	keepFiring bool
	keepAt     time.Time
	// standRail and standRailAt are the MARGIN's cached reading of what stands
	// over this conversation, on the same beat and for the same reason
	// (margin.go's [app.marginStanding]): the column is laid out twice a frame,
	// and what it is asking about is a directory of documents.
	standRail       []StandingItemView
	standRailAt     time.Time
	tasks           map[uint64]*taskNode
	typedTaskBriefs map[uint64]string
	taskOrder       []uint64
	taskSeen        map[uint64]session.TaskState
	// THE JOB SIDE (jobstate.go). jobs is every background job this conversation
	// has started, oldest first, and jobsOpen is whether the column's own jobs
	// section is unfolded. Both are deliberately NOT part of the task side above:
	// a job is not a node, has no room, no branch and no price, and the whole
	// reason it has its own state here is that it used to borrow that one.
	//
	// IT IS A SLICE AND NOT A MAP because it is drawn far more often than it is
	// written and the drawing wants an order. A conversation has jobs in tens at
	// the very most, so the upsert's scan costs nothing and buys one source of
	// truth instead of a map and a slice kept in step with each other.
	jobs     []session.JobNotice
	jobsOpen bool
	// jobPage is the id of the job whose page is open, and 0 is every frame that
	// is not on one. It is a JOB'S OWN NUMBER and not a roster id, which is the
	// point of the whole change: one thing, one number, the same one `jobs kill`
	// takes.
	jobPage int
	// jobDraw is that page's own reading — the log tail it is showing, the beat
	// it is on and where it is scrolled to (jobpage.go). It is nil whenever no
	// page is open, and it is replaced rather than reused when the page moves to
	// another job.
	jobDraw  *jobDraw
	taskLane <-chan session.Event
	taskGen  int
	// railStamp counts the times this window's own row-space MOVED — a node
	// upserted off either lane, or a far index read landing. It is a counter and
	// not a time because it is compared and never displayed, and it exists for
	// the task page: an open page re-files its rows when the reading behind them
	// changed, and this window's live graph is one of the two authorities that
	// can change (place_tasks.go's [tasksPlace.regroup]).
	//
	// THE OTHER ONE ONLY MOVES AT HOME. The page's original stamp is the other
	// windows' reading, which a hosted surface can never take — nothing over a
	// connection answers [session.Agent.Elsewhere] — so away from home that
	// stamp stands still for ever and this is the only thing that says a task
	// somebody just started belongs on the page they are looking at.
	railStamp uint64
	// THE ROSTER'S OWN FACTS (task.go's rail). railOpen holds the FAMILIES a
	// person has folded or opened AGAINST their default — nil is the design as
	// shipped, and an absent key is a family nobody has touched, which is why this
	// is a map keyed by node id and not a flag on the node. railTop is the
	// window's offset into the SCROLLING PART of the roster's line list — the part
	// under the pinned live head ([app.railLiveHead]), because work that is still
	// going never leaves this column — resolved by the same [listTop] every other
	// list on this surface scrolls with. railWhere is the focused row,
	// named by id rather than by index because a fold takes rows out from under a
	// cursor while nobody is looking, and railHold says the roster has been GIVEN
	// the keyboard (alt+t) — without it there is no cursor, and every key still
	// belongs to the draft.
	//
	// railWide is the third width tier, asked for with w and sticky until it is
	// asked for again. railCramped is what earns the offer of it: the last layout
	// cut a title with its own indent, and it is written where that is discovered
	// ([app.railEntryRows]) and read by the footer, the way [app.railTop] is
	// written by the window it resolves.
	//
	// railAway is the person's own standing answer to whether there is a column at
	// all (ctrl+g, [app.railStow]). It outranks every width tier and the roster's
	// own "one node raises it" rule alike — a column somebody put away stays away,
	// through landings and new work and the next session, until they ask for it
	// back — and it is the one piece of this block that survives the process,
	// because it is the only one a person chose deliberately (config's
	// ui.task_column).
	railOpen    map[uint64]bool
	railTop     int
	railWhere   railSpot
	railHold    bool
	railWide    bool
	railCramped bool
	railAway    bool
	// away is the last reading of what the project's OTHER windows have out
	// right now, and when it was taken (taskview.go's [app.refreshElsewhere]).
	// It is a CACHE and not a subscription: the reading is a readdir and a
	// handful of small files, which is cheap on a clock and ruinous on a frame,
	// so it is refreshed on the paint clock while something on screen is drawing
	// it and held between times.
	away elsewhereCache

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
	// Recently visited tasks keep bounded display state across navigation.
	roomReadings     map[roomReadingKey]roomReading
	roomReadingOrder []roomReadingKey
	// roomPump is the command a freshly opened room's lane needs, PARKED rather
	// than returned.
	//
	// The reason is one call path this file cannot hand a command back through:
	// enter on a selected proposal row runs input.go's [app.enter] into
	// [app.openTool], which returns nothing at all. A door that worked when it
	// was clicked and did nothing when it was pressed would be the worse of the
	// two defects, so every door parks here and the program loop drains it.
	roomPump tea.Cmd
	// outbox is every correction typed into a node's page that this surface has
	// not yet heard the engine's answer to, and the numbering that names each
	// send apart from the next (steersend.go). The zero value is a surface
	// nobody has steered a task from, which is most of them.
	outbox steerOutbox
	// orchLive is the adaptive run this session has last heard from, or ""
	// (roomorch.go). It is the surface's only handle on a run: a run is not a
	// node, so it is on no roster and has no row, and the three orchestrate event
	// kinds are what say one exists at all. It is what → opens and what a pause
	// raises its gate on.
	orchLive string

	// chips are the pictures attached to the message being written, drawn as a
	// tray above the box (attach.go). sent holds the ones a message in flight
	// took, so a refusal can put them back where the person left them.
	//
	// The tray belongs to the RECIPIENT the box is talking to, with the rest of
	// that message's state (recipient.go) — and `sent` does not, because a message
	// in flight was the CONVERSATION's however far the person has navigated since:
	// a refusal hands its pictures back to main and never onto a task's page
	// ([app.chipsSettled]).
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
	// artifacts is the deliverables index a finished /export records itself in
	// (export.go). Empty is the door saying nothing, which [app.artifactsIndex]
	// turns into the product's own path.
	artifacts string
	// repoAsking is which workspaces have a `git status` in flight, so a sweep
	// down twenty rows of one project forks one command and not twenty
	// (homeband_repo.go's [app.refreshRepoOf]).
	repoAsking map[string]bool
	// newsAsking and leftOffAsking are the same idea for the two readings a card
	// takes of its own row (homecardread.go).
	newsAsking    map[string]bool
	leftOffAsking map[string]bool
	// homeFrames counts the frames home has BUILT. It is PERF.md's instrument
	// and nothing reads it but the pins (home.go's [app.homeFrame]).
	homeFrames int
	// models is the door's model list, asked for at the moment the picker
	// opens rather than at boot — a lazily warmed catalog may have arrived in
	// between, and it must never be waited for. Nil falls through to the cache
	// and the built-ins (see [app.modelList]).
	models func() []Model

	// sheet is the settings panel (settings.go): the FIRST fullscreen thing this
	// surface drew, and the only overlay that is modal for the pointer as well
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
	// taskSheet is the tasks place (place_tasks.go): the FOURTH fullscreen thing
	// this surface draws, and the second of them that exists at EVERY width — the
	// deck and the tool detail above it are the phone tier's alone. It holds the
	// MACHINE'S whole record of work that ran on its own rather than this
	// session's, which is the one question the roster's column cannot answer.
	// Closed, it costs the frame nothing.
	taskSheet tasksPlace
	// home is /home (home.go): the FIFTH fullscreen thing, the third that exists
	// at every width, and the only one of them that is not about this
	// conversation at all. It is every project on the machine and every
	// conversation in them, read off the disk when it opens and again on a slow
	// tick while it is up. Closed, it costs nothing — no walk happens until
	// somebody asks for one.
	//
	// SETTINGS, THE TASK PAGE AND HOME ARE MUTUALLY EXCLUSIVE. Opening any one
	// of them closes the other two ([app.openSettings], [app.showTaskPlace],
	// [app.openHome]), because two pages that both believe they own the frame is
	// a frame that draws one and takes keys for the other.
	home homeView
	// ── THE ROUTER ──────────────────────────────────────────────────────────
	//
	// page is WHICH PLACE the person is standing in, and [pageNone] — the zero
	// value — is the conversation (pages.go).
	//
	// IT IS THE ONE ANSWER AND NOT A LABEL ON SIX OTHERS. Every place used to
	// carry an `open bool` of its own with this field beside them, which is two
	// answers to "which page is up" and therefore an invariant somebody has to
	// keep; [app.showPage] closes what was standing and opens what was asked for,
	// so the flags are gone and the frame, the keyboard, the pointer and the tab
	// bar all read this.
	page page
	// tabs and tabRow are WHERE THE TAB BAR WAS LAST PAINTED — one span per chip
	// that survived the width ladder, and the row of the terminal the bar landed
	// on (-1 when a short frame cut it off). They are written by the draw
	// (pages.go's [placeFrameWithBar]) and read by the press, which is the same
	// bargain every hit map on this surface strikes: a click resolves against
	// what was actually drawn, never against what a second computation thinks
	// was drawn.
	tabs   []placeTabSpan
	tabRow int
	// boxRow and boxRows are WHERE A PLACE'S COMPOSER WAS LAST PAINTED — the row
	// its first line landed on and how many lines it took — written by the same
	// draw and read by the same press, on [app.chatTabs]'s bargain exactly. A click
	// on the box puts the caret under the pointer wherever a person is standing,
	// which is the ordinary text-field gesture the conversation already answers
	// (draftclick.go) and which every place was silently missing.
	//
	// A REST ROW IS NOT A BOX ROW. boxRows is zero while nothing is typed, and
	// the press falls through to the place underneath — there is no caret to
	// place in a box with no text in it, and the row is carrying a dim sentence
	// about the place rather than anything a pointer can act on.
	boxRow  int
	boxRows int
	// bar is THE CURSOR STANDING ON THE TAB BAR ITSELF, which is a row of the
	// frame a person can walk onto from any place (pages.go's [barCursor] holds
	// the whole law). It sits here beside [app.page] because the bar belongs to
	// the router: it is drawn on all seven places, in the same cells, by one
	// function — so a place that kept a flag of its own about it would be seven
	// answers to one question.
	bar barCursor
	// tabHover is the place whose word the POINTER is resting on, and [pageNone]
	// — the zero value — is "the pointer is not on the bar at all". It is what
	// lifts one word's ink by one tier and changes nothing else on the frame
	// (placemouse.go's [app.placeTabHover]).
	tabHover page
	// searchArm is how the search place's QUIET INTERVAL is armed, and nil — the
	// real 150ms timer — everywhere but a test (place_search.go's
	// [app.searchQuiet] holds the whole argument). It is a seam rather than a
	// clock because what a test needs is not a different duration but no real
	// time at all: the tick is delivered by hand, at the instant the test means.
	searchArm func(gen int) tea.Cmd
	// spend and search are those two places' own state: the ledger window and
	// the lines it is over (spendpage.go), and the query in flight with the
	// results it is answering for (searchpage.go). Closed, both cost the frame
	// nothing and neither has read anything.
	spend  spendPage
	search searchPage
	// places is the seam the tab bar's counts come through: the cached answer
	// per place, recomputed on the clock ([app.refreshPlaceCounts]). It is nil
	// until the first beat, and a nil seam draws no number anywhere, which is the
	// emptiness law rather than a gap (pages.go's [placeCounts]).
	places placeCounts
	// placeGen is the generation of the clock the places that are NOT home run
	// on (placecounts.go's [placeTickMsg]). Home has its own for the same reason
	// and by the same device.
	placeGen int
	// compose is the composer on the places that have no box of their own — the
	// standing place, spend and search. It is app-level rather than per-place on
	// purpose: a sentence half typed on one place is still there after `tab`,
	// which is what makes a permanent bottom line a composer rather than seven
	// boxes that each forget.
	compose editor
	// pageMsg is the one refusal a place that is not home has to say, drawn where
	// the hint would be. It is one field for [homeView.msg]'s reason: pressing a
	// door twice says the same thing once.
	pageMsg string
	// strip is the row's verbs, opened with `→`, and it is the ONE state on this
	// surface in which a bare letter is a verb rather than a character
	// (verbstrip.go). Closed — which is nearly always — every printable key
	// belongs to the composer.
	strip verbStrip
	// mapShowing is `alt+.`: the whole key map drawn in the cells a person was
	// already reading, until the next key (SCREEN 3b). A terminal cannot see a
	// held modifier, so what the mockup drew as "hold alt" is a chord that lasts
	// exactly one keystroke.
	mapShowing bool
	// chords is HOW THIS TERMINAL SPELLS THE CHORD CLASSES and what its option
	// key is called — `alt+` everywhere, `⌥` on a Mac (chords.go). It is decided
	// once at boot from the platform and the environment, because neither of
	// those changes while a process runs, and every sentence a person reads about
	// a chord is drawn through it.
	chords chordSpelling
	// chordLost and chordReal are the macOS option-as-meta check, and they are
	// two flags rather than one because they answer different questions.
	// chordLost is "a character arrived where a chord was aimed", which arms one
	// dim line in the place's note slot; chordReal is "a real `alt+` chord has
	// reached this program", which settles the question for the life of the
	// process and is never unset.
	chordLost bool
	chordReal bool
	// caret says whether the terminal caret should be shown on this frame. It
	// is set by [app.frame] on every render and read by [app.View]: home at rest
	// is a dashboard somebody reads, not a thing they type at, so its empty box
	// hides the caret rather than leaving it blinking over the "home" heading at
	// the frame's origin.
	caret bool
	// landing and pickSession are how this launch was made: whether the door
	// invited home onto the first frame ([app.landHome]) and whether it asked
	// for the resume picker there instead. Both are properties of ONE launch,
	// which is why they are read off the options and never off the profile.
	landing     bool
	pickSession bool
	// takeOverAt is a conversation this launch met a lock on
	// ([Options.TakeOver]). It is a transcript path, and the launch lands on
	// home with that row armed instead of quietly starting a second
	// conversation (takeover.go).
	takeOverAt string
	// takeover is the request this window has out for a conversation another
	// window is holding, and the zero value is a window that has asked for
	// nothing — which is every window almost always (takeover.go).
	takeover takeoverWait
	// homeDoor is where that advertisement was drawn on the last frame, for the
	// pointer — the same arrangement the model segment and the jump chip use
	// (render.go's [hudSpan]).
	homeDoor hudSpan
	// homeRoot is where that screen looks for the projects, and "" means the
	// state root under this machine's home ([app.placesRoot]). It exists for
	// tests, which build a projects directory in a temp dir; nothing on the door
	// sets it, because where sessions live is internal/session's answer and a
	// second one would be a second place for it to be wrong.
	homeRoot string
	// switchGrouped is `alt+g` and switchQuiet is `alt+q`: the two views home's
	// list can be shown in (place_home.go).
	//
	// THEY ARE ON THE APP BECAUSE THEY OUTLIVE THE SCREEN AND NOTHING ELSE. A
	// person who grouped the list expects it grouped the next time they open home
	// in this terminal, and expects to have chosen a view rather than to have
	// found a preference they now own — so the flags live for as long as the
	// process does, and nothing writes them to a disk.
	switchGrouped bool
	switchQuiet   bool
	// composer is the COMPOSER LAYER: `alt+enter` over a composer with something
	// in it, on any place (composerlayer.go, SCREEN 2e). It is the router's own
	// layer rather than any one place's, which is why it is here beside `page`
	// and not on a place's state — the three facts it settles are the same three
	// wherever a person typed the sentence.
	composer composerLayer
	// hop is the conversation switcher — `ctrl+k`, the card over everything
	// (hop.go). It is a field of the app rather than of a place because it
	// belongs to no place: it is drawn over the conversation and over all seven.
	hop hopCard
	// hopQuick is the `quick switch` setting (config.KeyQuickSwitch): whether
	// ctrl+tab switches on each press or opens a card that waits
	// for `enter`. Read at boot and again at each turn's end, the way the
	// other panel rows arrive.
	hopQuick bool
	// hopKnown is how many conversations the last reading of this machine saw,
	// and it is what lets the switcher be ADVERTISED without a disk walk on the
	// frame (hop.go's [app.hopAvailable]). It is refreshed off the loop by
	// [app.countConversations] — once at boot, and again whenever a conversation
	// is opened or closed — and is zero until that first answer lands, which is
	// the honest reading of "nobody has looked yet".
	hopKnown int
	// frontAt is when the conversation on screen came forward, which is the only
	// thing the switcher's own row can measure an age from — every other row
	// measures from the sidecar its detach left (keeper.go's [aside.since]).
	frontAt time.Time
	// errand builds the agent behind `ask here` and standingRoot is where its
	// folder is made ([Options.Errand], [Options.StandingRoot], homeexchange.go).
	// A nil seam is a window that cannot ask from home and says so, which is a
	// capability that is absent rather than broken.
	errand       func(ErrandOrders) (Agent, error)
	standingRoot string
	// exchanges is every errand this window has open, oldest first.
	//
	// IT IS ON THE APP AND NOT ON [homeView] BECAUSE AN EXCHANGE OUTLIVES THE
	// SCREEN IT WAS ASKED ON. It used to be one field on the view, which
	// [app.closeHome] assigns the zero value to — so opening another
	// conversation to check something ended the errand mid-question, and the
	// engine answered the card the person had not got to with "the card was left
	// unanswered — nothing was set up". Here they survive home closing, several
	// are open at once, and the window takes them all with it on the way out
	// ([app.fileEveryExchange]). homeexchange.go's header states the lifecycle.
	exchanges []*homeExchange
	// leaveAnswer leaves one answer on another session's doorstep, and answered
	// is what this window has already sent, by session folder, so the band can
	// say so while it waits for that session to pick it up (homeband_answer.go).
	// A nil seam is a window that can read a question from home and not answer
	// it — the chips are simply not drawn, which is the absence law. It is
	// `leaveAnswer` and not `answer` because [app.answer] is already the consent
	// block's own verb, and one word for two doors is how the wrong one gets
	// called.
	leaveAnswer func(dir string, kind session.QuestionKind, id uint64, key string) error
	answered    map[string]homeAnswered
	// profileDir is where the panel's writes land, and settings the registry it
	// edits. The registry is built at the first /settings rather than at boot —
	// it is a door onto a file, and a surface that may never be asked about
	// settings should not open one.
	profileDir string
	settings   *config.Settings
	// crew is the profile's crew as this surface last read it, so the status
	// line can name it without reading four settings rows off the disk on every
	// frame (crew.go's [app.crewReading]).
	crew crewReading
	// oneModel is the launch's `--one-model`, which OVERRIDES the four rows
	// above rather than being one of them: under it the door seats every text
	// call on the conversation's own model and the crew on disk seats nothing,
	// so the readings built from it name the flag ([Options.OneModel], #444).
	oneModel bool
	// workSeatSaid is whether this session has already asked whether its work
	// seat was inherited (crew.go's [app.sayWorkSeat]). It is a fact about the
	// SESSION and not about the profile: the line is a receipt for work that is
	// starting now, said once where a person is already looking, and a surface
	// that said it again per task — or per node of one task — would be the
	// warning-on-every-call this whole mechanism refused headless (#311, #312).
	workSeatSaid bool
	// notices is what this surface has told the person and may tell them next —
	// the earned hints and the news line, over the profile's ledger (notice.go).
	notices noticeBoard
	// saveApproval and saveBashApproval are the door's write seams for the
	// consent card's "always" (consent.go). Nil is a surface that remembers an
	// answer for the session and no longer, which is what this card did before
	// they existed.
	saveApproval     func(tool string) error
	saveBashApproval func(command string) error
	// saveModel is the same kind of seam for the model in the status line
	// (palette.go's rememberModel): the choice /model, the picker and the
	// settings sheet's talk row all make, written where the next launch reads it
	// again. Nil is a surface whose model change lasts exactly as long as the
	// session does — a test, and the --host door, are both that surface.
	saveModel func(model string) error
	// applyApprovals is the door's LIVE seam for a line taken back in the
	// permissions panel (permissions.go): it re-reads the person's approval rows
	// and hands them to the gate this conversation is already running on. Nil is
	// a surface whose drops land in the config and reach the running gate on the
	// next session, and the receipt says so rather than claiming otherwise.
	//
	// It takes nothing because the config is the record: two callers passing
	// their own reading of it is how a panel and a gate come to disagree about
	// what was answered.
	//
	applyApprovals func() error

	// permPanel is the list /permissions opens over the two approval rows the
	// consent card writes into (permissions.go). It reads and writes the
	// person's own config, so nothing about it depends on the door having wired
	// anything; closed, it costs the frame nothing.
	permPanel permPanel

	// orders is the standing place's state: the shelves of what stands here — this
	// conversation's orders, this project's and the machine's (place_standing.go).
	// It reads the engine's own seam, so a surface whose agent has no ambient
	// side opens on the three sentences saying what a standing order IS; closed,
	// it costs the frame nothing.
	orders standingPlace

	// subPage is /subharness: the list of programs this conversation can run,
	// and the intake card that starts one (subharness.go). It reads the engine's
	// own doors, so a surface whose agent has no subharness side opens nothing at
	// all; closed, it costs the frame nothing.
	subPage subPage

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
	rew rewindMode
	// rewSheet is the DELIBERATE rewind: the whole conversation as a full-frame
	// timeline, with a search, a preview of the pick and a two-stage enter
	// (rewindsheet.go). It is the fourth page on this surface that takes the frame
	// whole, and it is built from the session's own transcript rather than from
	// the drawn blocks — which is why it can reach turns the inline mode cannot.
	rewSheet rewindSheet
	escArm   time.Time
	rewSay   string
	rewSayAt time.Time
	// quitArm is when the first ctrl+c landed, or zero — the door's own arm,
	// and the reason one press no longer ends the session (quitarm.go). It sits
	// beside escArm because it is the same shape of fact for the same kind of
	// reason: a key whose meaning is different for a moment, held out here
	// rather than inside any mode, and run down on the frame clock
	// ([app.quitSweep]) because this surface has one clock.
	quitArm time.Time
	// tmux says this surface is inside a multiplexer, so a clipboard write has
	// to be wrapped in its passthrough (copymode.go). It is read once, from
	// TERM, because a terminal does not change what it is mid-session.
	tmux bool

	// remote says the terminal reading this surface is on the far side of a
	// connection, so the frame clock turns slower (link.go). It is read once,
	// at construction, on the same terms tmux is and for the same reason.
	remote bool

	// pathLinks says a file path drawn on this surface may be wrapped in an
	// OSC 8 hyperlink (pathlink.go). It is two facts folded into one, and both
	// are settled for the whole session at construction: the terminal will take
	// the sequence, and the files being named are on THIS machine's disk. The
	// third gate is not a session fact and lives with the render —
	// [app.linker] turns links off while a task's room is up, because a node
	// works in its own worktree.
	//
	// pathSeen memoizes what has already been looked for, and is emptied at
	// every turn end so that a file written during the turn becomes clickable
	// the moment the turn lands.
	pathLinks bool
	pathSeen  map[string]string

	// levels is how hard each model id is asked to think, as this surface last
	// learned it, and levelWant/levelWanted/levelAsking are the queue that keeps
	// the question off the draw path. The whole law is in reasoninglevel.go: at
	// home the agent answers under a mutex, over a connection it answers over an
	// ssh pipe, and a status row that asked on every frame was a round trip per
	// frame and — because a hover below the conversation rebuilds the chrome —
	// a round trip per POINTER MOTION.
	levels      map[string]string
	levelWant   []string
	levelWanted map[string]bool
	levelAsking bool
	// usageAsking is the same debounce for the session's running cost, which the
	// frame clock reads every [usageEvery] slots. It is asked off the loop for
	// reasoninglevel.go's reason exactly — the agent's answer is a lock at home
	// and a round trip away — and one ask at a time is all a clock can need.
	usageAsking bool

	// rfiles is everything this surface knows about the OTHER machine's disk —
	// which words are real files there, the door that turns them into things
	// this machine can open, and the content cache under it (remotefiles.go).
	//
	// NIL IS THE COMMON CASE AND IT IS THE ABSENCE LAW, not a gap: a local
	// session has no far disk, and a hosted session whose agent hands over no
	// client is a build that cannot do this at all — so it gets no remote links,
	// no browse door and no prefetch, rather than three seams that fail one at a
	// time in front of somebody.
	rfiles *remoteFiles

	// host is the machine the AGENT is on when it is not this one, and it is the
	// other direction entirely from [app.remote] one line above: that one is
	// about the terminal reading the frame, this one is about the session
	// answering it. Empty is an ordinary local conversation. localRoot is this
	// machine's own directory, which is where a path the person types is
	// anchored while the workspace belongs to somebody else's disk. Both are
	// read once, at construction — see host.go for the whole law.
	host      string
	localRoot string
	// owned says the workspace is this session's own work/ directory rather
	// than a project somebody opened aforge inside of (Options.Owned). It is
	// read by [app.placeWord] and by nothing else.
	owned bool
	// handedApproval is the tool-approval posture this launch knows the
	// surface's own profile cannot answer, carried in Options.ApprovalMode. Over
	// --host it is the engine's row; locally it is --yolo's forced allow. Empty
	// leaves the profile live — see [app.approvalPosture].
	handedApproval string

	// focused is whether the terminal window has the keyboard, and seenFocus
	// whether it has ever told us (notify.go). The pair is what decides whether
	// a finished turn is worth a notification: a person watching the screen does
	// not need to be told what they are looking at.
	focused   bool
	seenFocus bool

	// landedAway is a turn that finished while the window was blurred and has
	// not been looked at since — the tab's ✓ (windowtitle.go). It is the
	// notification's fact kept as state: the banner says it once at the moment
	// it becomes true, and this keeps saying it on the tab until the person
	// comes back, at which point the reply on screen says it better.
	landedAway bool

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

	// setup is the first-run screen, which precedes the box below on the one
	// launch that gets it (firstrun.go). Its zero value is every other launch.
	setup setupFlow
	// applyAPIKey is the door's live seam for a key handed over after the
	// launch — on the setup screen or in the settings row — so the running
	// session's next request rides it ([Options.ApplyAPIKey]). Nil is a surface
	// whose key lands on the next launch.
	applyAPIKey func(key string) error
	// routerConnect is the browser half of that same handover, available on
	// a local interactive launch using the default provider. authSerial gives
	// every attempt a name, so a listener that came up after esc can be closed
	// without reopening the screen the person left.
	routerConnect func(context.Context) (OpenRouterFlow, error)
	authSerial    uint64
	// welcome is the box an empty session opens with (welcome.go). It is the
	// only animation on this surface that is not a spinner, and it runs once.
	welcome welcome
	// startBack is what `esc` on the new-chat start page puts back, and startKept
	// is that page's own unsent sentence while the page is down (chatstart.go).
	// startGen discards a recent list that arrives after the page it was asked
	// for closed.
	startBack startBack
	startKept composerState
	startGen  int
	// roster is the resume picker: the same conversations the box lists, opened
	// on purpose and filterable (resume.go).
	roster roster
	// shelf is the deliverables picker /files opens over the global index of
	// what has been made (deliverables.go). It reads the same index /export
	// writes: the artifacts field above, resolved by [app.artifactsIndex].
	shelf shelf
	// folder is the /folder picker: the ONE component for choosing a directory
	// anywhere in this product (folderpick.go).
	folder folderPick
	// folderStore is what that picker remembers between launches — how often
	// each directory was chosen, and the repositories under `~` as the last
	// background scan found them (folderplace.go). folderStoreRead says the read
	// has been ASKED FOR, which is what keeps three opens of the picker in one
	// second from starting three walks of somebody's home directory.
	folderStore     folderStore
	folderStoreRead bool
	// folderAsking is which directories' facts are in flight, so a cursor held
	// down a list forks one git per row rather than one per keypress
	// (homeband_repo.go's [app.repoAsking] states this law).
	folderAsking map[string]bool
	// placeChosen is the last directory this conversation chose, whichever road
	// it came in by ([app.referPlace]). It is what P1 keeps; lane P2 is what
	// turns it into a remembered set on the conversation's meta.
	placeChosen string
	// recentSessions answers the box's right column and the picker's rows, and
	// resume opens one of them. Both are nil on a surface the door did not wire,
	// and then the box says it has no sessions rather than pretending to have
	// lost them.
	recentSessions func() []Session
	resume         func(file string) (Agent, error)
}

// landingKeysWord is the opening line of every session: the keys the status
// line has no room for. It is named because the note that writes it also names
// the chords inside it for THE PAYLOAD RULE (payload.go), and a sentence
// spelled in one place with its keys spelled in another is a sentence that gets
// reworded while the keys stay where they were.
//
// AND THE THIRD CLAUSE IS WHERE `?` IS ADVERTISED. The key is bound over an
// empty box on both roads (commands.go's [helpAskKey]) and SCREEN 3a's clause is
// that no key does anything that is not drawn — so the one line every session
// opens with, which is already about the keys nothing else names, is where it is
// written down. It is the third and last clause because the two in front of it
// are about the session a person is in and this one is about the program.
const landingKeysWord = "esc interrupts · ctrl+c twice quits · ? for help"

func newApp(ctx context.Context, opts Options) *app {
	// THE ENVIRONMENT IS READ THROUGH THE SEAM AND NOWHERE ELSE, so the four
	// facts below that come from the shell all come from the same table when a
	// test hands one in ([Options.Env] says why a test must).
	env := opts.Env
	if env == nil {
		env = os.Getenv
	}
	host := strings.TrimSpace(opts.Host)
	place := strings.TrimSpace(opts.Workspace)
	if place == "" && host == "" {
		// The cwd is the right fallback for a LOCAL session and a lie for a
		// remote one: the workspace belongs to the other machine, and a surface
		// that filled the gap with this machine's directory would be naming a
		// place the conversation has never been. A remote session with no
		// workspace draws no place at all, which the emptiness law handles.
		if cwd, err := os.Getwd(); err == nil {
			place = cwd
		}
	}
	shown := placeShown(place, opts.Owned, host)
	a := &app{
		ctx:                 ctx,
		agent:               opts.Agent,
		fresh:               opts.Fresh,
		start:               opts.Start,
		open:                opts.Open,
		openTaskOwner:       opts.OpenTaskOwner,
		anchorWorkspace:     opts.AnchorWorkspace,
		errand:              opts.Errand,
		standingRoot:        opts.StandingRoot,
		leaveAnswer:         opts.Answer,
		host:                host,
		handedApproval:      strings.TrimSpace(opts.ApprovalMode),
		bashBackgroundAfter: opts.BashBackgroundAfterSeconds,
		owned:               opts.Owned,
		landing:             opts.Landing,
		takeOverAt:          opts.TakeOver,
		pickSession:         opts.PickSession,
		workspace:           place,
		place:               shown,
		file:                opts.SessionFile,
		build:               strings.TrimSpace(opts.Build),
		resumed:             opts.Resumed,
		models:              opts.Models,
		history:             opts.History,
		draftFile:           opts.DraftFile,
		artifacts:           opts.ArtifactsIndex,
		ctxWindow:           opts.ContextWindow,
		profileDir:          opts.ProfileDir,
		oneModel:            opts.OneModel,
		settings:            opts.Settings,
		saveApproval:        opts.SaveApproval,
		saveBashApproval:    opts.SaveBashApproval,
		saveModel:           opts.SaveModel,
		applyAPIKey:         opts.ApplyAPIKey,
		routerConnect:       opts.ConnectOpenRouter,
		applyApprovals:      opts.ApplyApprovals,
		recentSessions:      opts.RecentSessions,
		resume:              opts.Resume,
		shared:              opts.SharedAgent,
		stands:              opts.Standing,
		link:                opts.Link,
		conns:               opts.Connections,
		harn:                opts.Harnesses,
		memory:              opts.Memory,
		searchStore:         opts.Search,
		searchStatus:        opts.SearchStatus,
		usageLedger:         opts.UsageLedger,
		ledger:              opts.Ledger,
		archive:             opts.Archive,
		world:               opts.World,
		farPlaces:           opts.WorldRoot,
		farRecord:           opts.TaskRecord,
		farRoomRecord:       opts.TaskRoom,
		farTasks:            opts.TaskIndex,
		echoAt:              -1,
		sel:                 -1,
		unfolded:            map[int]bool{},
		capOpen:             map[int]bool{},
		stick:               true,
		width:               80,
		height:              24,
		pal:                 detectPalette(env),
		mdBase:              stylerFor(opts.Env),
		linear:              opts.Linear,
		tmux:                tmuxTerm(env),
		remote:              remoteLink(env),
		wsl:                 bootWSLPaths(env),
		// THE CHORD SPELLING IS A BOOT FACT (chords.go). The platform decides
		// whether the modifier is called `alt+` or `⌥`, and the environment names
		// which emulator is running so the one option-as-meta line can name the
		// setting instead of waving at "your terminal".
		chords: detectChords(runtime.GOOS, env),
		// A terminal that has said nothing is assumed to HAVE the keyboard, which
		// is the quiet assumption: the cost of getting it wrong is a notification
		// nobody got, and the cost of the other default is a notification every
		// turn on a screen somebody is watching (notify.go).
		focused: true,
	}
	a.copy.mark = -1
	// AND THE REDUCER IS BUILT WITH WHAT THIS PAGE IS, which is the whole of the
	// difference between a chat's transcript and any other (feed.go states the
	// law the hooks exist to keep). It is built here and not in the literal above
	// because the hooks dispatch through this app's own methods, and the POSTURE
	// is named at the same moment for the same reason (lens.go): what a page
	// does with an event is a fact about the page, so the page says which it is.
	a.feed = newFeed(a.feedHooks(participantLens))
	a.gitProbe = gitHead
	if a.hosted() {
		// THE BRANCH PROBE IS OFF OVER A CONNECTION, and off rather than wrong:
		// `git` would run HERE, in a directory named by the OTHER machine's path,
		// and the two outcomes are a blank (the path does not exist locally) and a
		// lie (it does, and belongs to a different repository). A blank is what an
		// empty branch already draws, so this costs the legend nothing and can
		// never put somebody else's branch name under this conversation. A real
		// remote probe is a wire question and belongs to the lane that owns the
		// contract, not to a guess made here.
		a.gitProbe = nil
		// And this machine's own directory, which is where /image and the
		// completion walk are anchored while the workspace is elsewhere (host.go).
		if cwd, err := os.Getwd(); err == nil {
			a.localRoot = cwd
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		a.tilde = home
	}
	// AND THE FAR MACHINE'S DISK, WHICH IS WHAT DECIDES THE LINE UNDER IT. It is
	// built before the gate is read because the gate now asks it a question, and
	// it is nothing at all on a local session (remotefiles.go).
	a.rfiles = newRemoteFiles(a.host, a.agent)
	// AND THE PATHS ARE CLICKABLE WHEN SOMETHING CAN CONFIRM THEM.
	//
	// This line used to end `&& !a.hosted()`, and the reason it did is still true
	// as far as it goes: a hosted session's files are on the other end of the
	// connection, and `file:///app/main.go` handed to the terminal in front of
	// you names THIS machine's /app/main.go — either nothing at all or somebody
	// else's file. What changed is not the law but who can answer it. At home the
	// confirmation is a syscall; over a connection it is the engine, asked in
	// batches off the render path, and the anchor points at a loopback file door
	// rather than at `file://` (pathlink.go's far-side section). A hosted session
	// with no way to ask is still off, which is where this line started.
	a.pathLinks = terminalTakesLinks(env) && (!a.hosted() || a.rfiles != nil)
	a.pathSeen = make(map[string]string, 256)
	// The gate's posture is read at boot and re-read at every turn end
	// ([app.settle]): a person who opens the settings panel and turns the asking
	// off sees the YOLO segment appear one turn later, which is soon enough for
	// a fact that only ever changes by hand.
	a.approval = a.approvalPosture()
	a.mouse = config.MouseEnabledAt(a.profileDir)
	a.timestamps = config.TimestampsAt(a.profileDir)
	a.workMode = config.WorkAt(a.profileDir)
	// AND THE COLUMN'S POSTURE IS READ HERE AND NOWHERE ELSE — at boot, never at
	// a turn end. The rows above are settings a person changes in the panel, so
	// re-reading them is how the change arrives; this one is normally changed with
	// a keystroke ([app.railStow]), and a re-read would be the surface putting the
	// column back at the end of the turn a person had just closed it in.
	a.railAway = !config.TaskColumnAt(a.profileDir)
	a.hopQuick = config.QuickSwitchAt(a.profileDir)
	// And the approval countdown, on the same terms (consent.go).
	a.askWait = a.consentWait()
	// The ledger of what this profile has been told, and whether this build is
	// news to it (notice.go). The toggle is re-read at every turn end, beside the
	// mouse row above.
	a.notices = newNoticeBoard(noticeLedgerPath(a.profileDir), buildStamp(), config.HintsAt(a.profileDir))
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
		// AND THE LEVEL IS SEEDED HERE, beside the two facts above and for the
		// same reason: the status row spells it onto the model segment, and a
		// level fetched on the frame clock instead would leave the FIRST frame
		// naming a model that is dialled up as though it were not
		// (reasoninglevel.go).
		// The whole table where the agent can hand it over, and this one model
		// where it cannot (reasoninglevel.go).
		a.learnLevel(a.model)
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
	// AND THE FIRST-RUN SETUP IS DECIDED AT THE SAME POINT, for the same
	// reason: "empty" has to mean the conversation is empty, and the notes
	// below are the surface talking. It is drawn over whatever else the first
	// frame decides — the box, the picker, home — and goes away to reveal
	// exactly that (firstrun.go).
	a.openSetup(opts.Setup)
	if a.linear {
		// The box still opens; it just opens FINISHED. Its arrival animation is
		// the one piece of motion on this surface that is not a spinner, and
		// linear mode's rule is the same for both.
		a.welcome.step = welcomeFrames
	}
	a.noteStandingHere()
	a.measureContext()
	// AND WHAT THE CONVERSATION HAS ALREADY SPENT IS ASKED FOR ON THIS FRAME,
	// beside the context it is measured with. The engine restores the total from
	// the journal's usage lines at construction (session's [Agent.Usage], seeded
	// by [sessionFile.RestoredUsage]), but nothing on this surface asked until a
	// frame of the paint clock came round — and the paint clock only turns while
	// something is animating. So a resumed conversation sat idle at the prompt
	// with `$0.00` on its status line until the person sent a turn or typed
	// /cost, under-reporting its own bill in the one direction that erodes trust
	// in every other figure beside it (#128).
	//
	// It is the SYNCHRONOUS reading rather than [app.usageKick] for the reason
	// [app.measureContext] above it is: this is the first frame, the figures have
	// to be right on it rather than one round trip later, and a surface that
	// already asks the agent what the conversation weighs can ask what it cost in
	// the same breath. The emptiness law is unharmed — a conversation that spent
	// nothing restores a zero Usage and every one of these fields stays as it
	// was, so the sheet and /cost still draw nothing.
	a.refreshUsage()
	// The notices get their first look now that the conversation, the box and
	// the directory's facts are all in place: a news line lands here, under the
	// replay and above the door's own notice, and the hints that wait on this
	// directory having an earlier conversation can see the welcome's list.
	a.noticeEvent(eventBoot)
	if notice := strings.TrimSpace(opts.Notice); notice != "" {
		a.note(notice)
	}
	if a.resumed && a.file != "" {
		a.note(a.resumedNote())
	}
	// The opening line says the two keys the status line has no room for. The
	// other two — /help and ctrl+o — moved to that line's right end this wave
	// and are on screen permanently, so repeating them here would be the surface
	// saying the same thing twice on the first frame of every session.
	//
	// IT HAS TO BE TRUE IN EVERY STATE, and the line it replaced was not: it
	// promised an interrupt on the first frame of a session where nothing was
	// running, and at that moment ctrl+c was the door rather than a stop. The
	// two clauses here are each true whatever is happening — esc stops the turn
	// when there is one, and two presses of ctrl+c always leave (quitarm.go).
	//
	// AND IT WAITS FOR THE GREETING TO GO. On an empty session the line lands
	// when the conversation begins rather than above a screen that is asking for
	// its first sentence (welcome.go's [app.dismissWelcome] says why); a session
	// that opens on a transcript gets it here, on its first frame, as it always
	// has.
	if !a.welcome.open {
		a.noteLandingKeys()
	}
	a.restoreDraft()
	// AND THE CORRECTIONS THE LAST LIFE NEVER LEARNED THE FATE OF COME BACK WITH
	// THEM, under the names they were sent with, as rows their pages raise when
	// they are opened (steersend.go's [app.restoreSentDrafts]).
	a.restoreSentDrafts()
	// LAST, because it reads the surface it opens over: the picker marks the
	// session this window is already in, and that is not known until the agent,
	// the file and the replay above have settled. A door that asked for it on a
	// machine with no conversations yet gets the empty state as a notice rather
	// than a list with nothing in it (resume.go).
	if opts.PickSession {
		a.openResume()
	}
	// AND HOME IS DECIDED AFTER BOTH, for the picker's own reason and one more.
	// It reads the surface it opens over — which conversation this window is in,
	// so the cursor can open on it — and it must be able to see that the picker
	// already took the frame, because a launch gets one greeting (home.go).
	a.landHome()
	// AND A LAUNCH THAT MET A LOCK LANDS ON THE ROW IT COULD NOT OPEN, armed, so
	// one enter continues that conversation here rather than leaving somebody
	// with a second one they did not ask for (takeover.go).
	a.landTakeover(a.takeOverAt)
	// AND THE CONVERSATION THIS WINDOW OPENED ON IS STAMPED, so the switcher's
	// own row has a clock like every other row on the card (hop.go). Every LATER
	// conversation is stamped by [app.attachConversation]; this is the first one,
	// which no switch ever brought forward.
	a.frontAt = a.now()
	return a
}

// noteLandingKeys writes the opening line: the two keys the status line has no
// room for.
//
// THE TWO KEYS READ AS KEYS (payload.go). This is the first line of the
// conversation and the only thing in it a person has to remember is the two
// chords, so the chords step to ink and the verbs around them stay in the note's
// own dim. It is spelled in the hint slot's own grammar — chord, then what it
// does — and the facts are named rather than recognized, because a note is prose
// to this surface and only the line that wrote it knows otherwise.
func (a *app) noteLandingKeys() { a.noteFacts(landingKeysWord, "esc", "ctrl+c", helpAskKey) }

// resumedWord opens the line a session says on the frame it opens over a
// conversation that already existed.
const resumedWord = "resumed"

// resumedNote is that whole line, and WHAT IT SAYS IS WHICH CONVERSATION.
//
// It used to say where the journal file lives, absolutely, and that was the
// first thing on the page: four to six wrapped rows of transcript path above the
// person's own first message, a fifth of a sixty-column screen, broken into
// seventeen-character stubs. It is a machine's fact standing where a person's
// first impression goes, and the fact somebody actually wants at that moment is
// that this is the conversation they left off in — which is its NAME.
//
// THE PATH IS NOT LOST, IT IS ASKED FOR: `/status` carries it on its `file` row
// (statusnote.go), whole, with its machine on a remote session, which is where a
// person who wants to go and look for the file goes.
//
// The ladder ends on the path all the same, because a line that named nothing
// would be worse than a long one — and there it is written against `$HOME`
// ([tildePath]) so the commonest journal comes back inside one row, and with its
// machine on a remote session for /status's reason: a path a person is shown is
// a path they may go looking for, and this one is not on their disk.
func (a *app) resumedNote() string {
	if name := a.resumedName(); name != "" {
		return resumedWord + " · " + name
	}
	return resumedWord + " " + a.hostedPath(tildePath(a.file, a.tilde))
}

// resumedName is what to call the conversation that just opened: the name it
// gave itself, and failing that the opening of the first thing the person said
// in it.
//
// IT IS THE RESUME PICKER'S OWN LADDER minus its last rung (resume.go's
// [humanName]), and it stops one rung early on purpose: that page falls back to
// the transcript's file name because it is choosing BETWEEN conversations and
// owes every row something, while this line has a better answer for that case —
// the path itself, said once, below.
func (a *app) resumedName() string {
	if name := a.sessionName(); name != "" {
		return name
	}
	for _, e := range a.entries {
		if e.kind == entryUser && strings.TrimSpace(e.text) != "" {
			return openingName(e.text)
		}
	}
	return ""
}

var _ tea.Model = (*app)(nil)

// Init starts the standing lanes, and starts the paint clock only when the first
// frame has something to animate. An ordinary idle local surface with no box
// has no wakeups; a hosted one also owns hostlink.go's separate five-second
// measurement clock.
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
	// AND THE PROJECT'S TASK RECORD IS READ ONCE, HERE. It used to be paid for by
	// the first "@" (taskmention.go's [app.loadTasks]), which was the right deal
	// while the record had exactly one reader; the column now has to know whether
	// the project has a record at all before it can draw the door onto it
	// ([app.railHasRecord], taskview.go), and that question is asked on the first
	// frame. It is one small file, read off the loop, and the read marks itself
	// done — a session that never grows a task never reads it twice.
	// AND THE STIR LANE, which belongs to no conversation at all (keeper.go). It
	// is opened here rather than at the first switch because the channel has to
	// exist before a watcher can be handed it, and a pump started twice would be
	// two readers on one lane.
	// AND THE TERMINAL IS ASKED WHAT COLOUR IT IS, ONCE, HERE. It is the one
	// standing command on this list that nothing waits for: a terminal that
	// answers gets a palette derived against its real background (adaptive.go),
	// and a terminal that stays silent — which is most of them, and every pipe —
	// simply keeps the authored ladder it has been painting since the first
	// frame. There is no timer behind it and no fallback path to take, because
	// the fallback is what is already on screen.
	// AND THE FAR MACHINE'S WAITING ROOM IS ASKED ABOUT ONCE, HERE. A question
	// raised while nobody was attached has been holding that turn since; this is
	// the moment somebody arrived, so it is the moment to be handed it
	// (hostlink.go's [app.askHeld]). It is nil on every local session, which is
	// the seam saying there is no far machine to have a waiting room.
	// AND THE HOSTED LINK'S SLOW CLOCK STARTS HERE. It is a five-second timer,
	// separate from the paint clock because an idle hosted session still has a
	// round trip to measure and because no frame is permission to call the wire.
	// AND HOW MANY CONVERSATIONS THIS MACHINE HAS, ONCE, HERE. It is what the
	// legend needs before it may name the switcher (hop.go), and it is asked off
	// the loop for the reason every other reading on this list is: the walk opens
	// every project's index, and the paint path may never pay for one.
	standing := []tea.Cmd{a.probeGit(), a.watchTasks(), a.watchWakes(), a.watchDesigns(),
		a.watchRuns(), a.loadTasks(), a.stirLane(), a.askHeld(), a.watchDriving(), a.watchFollowing(),
		a.linkPingTick(), a.prefetchReplayedPictures(), a.countConversations(), tea.RequestBackgroundColor}
	if a.welcome.animating() {
		standing = append(standing, a.wake())
	}
	// AND HOME'S OWN CLOCK, when home is the first frame. It is not the paint
	// clock — home is a still page and asks for a beat every few seconds rather
	// than thirty a second (home.go's [homeEvery]) — so it is started here
	// beside the standing lanes rather than folded into the wake above.
	if a.at(pageHome) {
		standing = append(standing, homeTick(a.homeGen))
		// A landing that greets over running work starts with its spinner
		// already turning — the paint clock's ninth reason ([app.paint]).
		if a.homeAnimating() {
			standing = append(standing, a.wake())
		}
	}
	return tea.Batch(standing...)
}

// Update is the loop's one door, and it does exactly two things of its own before
// the switch sees anything: it FOLDS THE POINTER'S STORMS (coalesce.go's
// [app.update]), and it keeps the clock turning while the surface owes itself a
// question.
//
// A SURFACE THAT OWES ITSELF A QUESTION KEEPS ITS CLOCK TURNING UNTIL IT HAS
// ASKED IT. The background asks are sent from the frame clock and from nowhere
// else, which is what makes them debounced ([app.paint]) — and the clock stops
// itself the moment nothing on screen is moving. So a list that queued a question
// while the surface was still (the model picker opening on a keypress is exactly
// that) would have queued it into a clock that was not turning, and the rows would
// have been drawn without their answers until something unrelated woke it. Arming
// here costs one tick on the frames where anything is owed and nothing at all on
// the rest, because [app.wake] answers nil to a clock that is already running.
func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// THE TERMINAL'S ANSWER ABOUT HOW WIDE AN EMOJI IS, taken before anything
	// else looks at the message. bubbletea acts on this same report to switch
	// its own renderer and passes it through to us, so reading it here is how
	// the layout and the paint end up measuring one frame the same way.
	if mode, ok := msg.(tea.ModeReportMsg); ok {
		a.ruler.noteModeReport(mode)
	}
	model, cmd := a.update(msg)
	// A TASK BRIEF BEING SHAPED IS THE SECOND THING ARMED HERE, and it is the
	// colder start of the two. Both doors onto the forming block — `/task` typed
	// into a still surface, and a yes on a proposal card — are answered while
	// nothing else on screen is moving, so the paint clock's eighth reason to
	// KEEP turning ([app.paint]) had nothing to keep: the block's spinner and its
	// count-up stood still for the whole shaping call, which is the exact dead
	// air the block was built to end (taskcommand.go's [preflight]). The two
	// places read ONE fact, [preflight.live], so a clock that starts and a clock
	// that keeps going cannot disagree about whether a wait is up.
	// AN ARRIVING PROPOSAL CARD IS THE THIRD THING ARMED HERE. Its own event
	// stream normally wakes the surface, but both clock lists read the derived
	// card fact so a start and a keep can never disagree (task.go).
	if a.levelsWaiting() || a.waiting() || a.formingCardLive() {
		cmd = tea.Batch(cmd, a.wake())
	}
	return model, cmd
}

// route is the message switch: every message this surface handles, handled once.
// It is reached through [app.update], which folds the pointer's storms before the
// switch ever sees them and hands on everything else untouched and in the order
// it arrived (coalesce.go).
func (a *app) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	// THE LINK'S ONE-SHOT NEWS IS DRAINED HERE AND NOWHERE ELSE (hostlink.go).
	// The seam forgets the sentence as it hands it over, so a second caller
	// would not show it twice — it would swallow it. This is the one place the
	// surface sees every message there is, which is what makes the drain prompt
	// on a window with nothing running: a redial that discovered the turn did
	// not survive has news, and a person who presses a key gets it rather than
	// waiting for whatever repaints next.
	a.takeLinkNotice()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// A zero size is a terminal that could not say — a headless boot, a
		// window being born. Keeping the last known size draws something;
		// taking the zero draws nothing.
		if msg.Width > 0 && msg.Height > 0 {
			return a, a.resized(msg.Width, msg.Height)
		}
		return a, nil

	case tea.BackgroundColorMsg:
		// THE TERMINAL ANSWERED [app.Init]'s one unanswerable question. Everything
		// that follows from it is adaptive.go's; this arm exists so that nothing
		// else in this function has to know the surface can be re-coloured.
		return a, a.groundReply(msg)

	case resizeSettledMsg:
		// The drag stopped moving, so the scroll is clamped once, against the
		// size it stopped at (see [app.resized]).
		a.sizing = false
		a.clampScroll()
		return a, nil

	case laneNewsMsg:
		// THE LANE LAYER SAID SOMETHING (lanes.go's [PostLaneNews]). Nothing is
		// read out of the message and nothing is stored from it: the news is
		// already on the desk by the time this arrives, and this exists only to
		// ask for the frame that draws it. A rescue that was drawn at the next
		// keystroke instead would be a rescue nobody saw happen.
		return a, nil

	case phaseNewsMsg:
		// AND THE PHASE CLOCK SAID SOMETHING (phase.go's [PostPhaseNews]). It is
		// the lane message's twin and it is empty for the same reason: the desk
		// already holds the news, and a frame is the only thing this can add.
		return a, nil

	case sigQuitMsg:
		// A REAL SIGNAL, forwarded by this package's own handler (tui3.go's
		// [forwardSignals]) because Bubble Tea answers an interrupt by returning
		// an error without ever calling this function. It takes the ordinary
		// door: the draft and anything parked go to disk, the session closes, and
		// the program exits zero. NO SECOND PRESS IS ASKED FOR — the two-press
		// rule is about a keystroke that can be struck by accident, and a signal
		// is somebody naming this process on purpose.
		return a, a.quit()

	case tea.KeyPressMsg:
		// THE DOOR DISARMS ON ANY KEY BUT ITS OWN, and it is done HERE rather
		// than at the top of [app.key] — where the pointer handover is — because
		// this is the only line every keypress passes through. The stop
		// confirmation, the roster and the room are all read below and above
		// [app.key], and a person who armed the door and then pressed `x` at a
		// running node would otherwise have had the arm still warm underneath
		// them. The first ctrl+c puts a sentence in the hint slot promising what
		// the NEXT keystroke does (quitarm.go); reaching for any other key is
		// that promise being answered. ctrl+c itself is excepted, because it is
		// the key the state is about.
		if msg.String() != "ctrl+c" {
			a.disarmQuit()
		}
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
		if a.pasteEdit.open && msg.String() != "ctrl+c" {
			return a, tea.Batch(flushed, a.pasteEditorKey(msg))
		}
		// THE STOP CONFIRMATION READS FIRST of the three below, and only ever
		// while it is up or while `x` is being pressed at something stoppable
		// (stop.go). It is a question about ENDING the work the roster and the
		// room are pages onto, so a key that reached either of them would be a
		// key aimed at the very thing being stopped.
		if cmd, took := a.stopKey(msg); took {
			return a, tea.Batch(flushed, cmd)
		}
		// An open switcher owns navigation before the page underneath it. A
		// pointer can open it while the roster or task still holds focus.
		if a.hopShowing() {
			if cmd, took := a.hopKey(msg); took {
				return a, tea.Batch(flushed, cmd)
			}
		}
		// THE ROSTER READS NEXT, and only ever once it has been HANDED the
		// keyboard (alt+t, task.go). Explicit focus outranks ambient place: a room
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
		// The tab's ✓ has been seen by the act of coming back to it; from here
		// the reply itself is on screen (windowtitle.go).
		a.landedAway = false
		// AND A QUESTION THAT WAS WAITING GETS ITS WHOLE COUNTDOWN BACK. The ten
		// seconds are ten seconds of a person reading, and this is the first
		// frame there has been anybody to read it (consent.go's [app.tickAsk]).
		a.refocusAsk()
		return a, nil

	case tea.BlurMsg:
		a.focused, a.seenFocus = false, true
		return a, nil

	case hopCountMsg:
		// HOW MANY CONVERSATIONS THIS MACHINE HAS (hop.go). It decides one thing
		// and nothing else — whether the legend may name the switcher — so
		// nothing repaints for it: it lands in the first moments of a session and
		// the frame that reads it is whatever frame comes next.
		a.hopKnown = msg.n
		return a, nil

	case hopSettleMsg:
		// THE PAUSE AFTER THE LAST PRESS OF THE CHORD (hop.go). While quick
		// switching, the card is a receipt for a switch that has already
		// happened, and this is the moment it fades.
		a.hopSettled(msg)
		return a, nil

	case tea.KeyboardEnhancementsMsg:
		// THE TERMINAL SAID WHICH CHORDS IT CAN SPELL. Bubble Tea enables basic
		// key disambiguation on every frame and asks the terminal to report what
		// it took; this is that report, and a non-zero set of flags is the whole
		// of what [app.keysDisambiguated] means — `shift+enter` arrives here as
		// itself rather than as a bare `enter` (bargein.go).
		//
		// IT IS RECORDED AND NOTHING IS REQUESTED. Nothing on this surface is
		// bound to a key RELEASE or a repeat, which is deliberate — a chord that
		// needs one is a chord a person behind a multiplexer does not have — so
		// there is no enhancement to ask for beyond the one already on.
		//
		// Nothing repaints for it: it lands in the first moments of a session,
		// before there is a turn to run or a draft to hint about, and the frame
		// that reads it is whatever frame comes next.
		a.keysDisambiguated = msg.SupportsKeyDisambiguation()
		return a, nil

	case dropMsg:
		// A RUN OF TYPED CHARACTERS WENT QUIET, and it may have been a file
		// somebody dropped on a terminal that types drops rather than pasting
		// them (dropkeys.go). A run that names nothing changes nothing here.
		return a, a.dropSettled()

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
		// THE SETUP SCREEN TAKES A PASTE WHOLE, because a paste is how the key
		// arrives (firstrun.go). It is read before the ordinary paste, which
		// would put the key into a draft box that is not on screen.
		if a.setupPaste(msg.Content) {
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
		if a.setupPaste(text) {
			return a, nil
		}
		return a, a.paste(text)

	case filesLoadedMsg:
		a.comp.all, a.comp.loaded, a.comp.loading = msg.paths, true, false
		a.comp.rank()
		a.touch()
		return a, nil

	case tasksLoadedMsg:
		return a, a.tasksLoaded(msg.rows, msg.known)

	case taskTailMsg:
		// One node's journal, read off the loop for the record card
		// (taskrecord.go). A read that came back about a task the person has
		// already walked away from is dropped there.
		a.taskTailRead(msg)
		return a, nil

	case draftSaveMsg:
		return a, a.saveDraft(msg.file)

	case draftKeptMsg:
		// One record write has finished (draftkeep.go). A failure is said rather
		// than swallowed — the words are still on the screen, and what has just
		// stopped being true is that they would survive this window — and a send
		// waiting for that write to land is released here, or refused, because
		// nothing crosses the wire before its own snapshot is on the disk
		// (steersend.go's [app.sendsKeptAt]).
		return a, a.draftKept(msg)

	case takeoverTickMsg:
		// One look at the flock of a conversation this window has asked another
		// window to let go of (takeover.go).
		return a, a.takeoverTick(msg)

	case behindStirMsg:
		// A conversation this process holds and is not drawing has something to
		// say about itself. The message carries no content — the surface reads
		// the agent it already has a pointer to (keeper.go).
		return a, a.behindStir(msg.key)

	case startRecentsMsg:
		// This directory's earlier conversations, read off the loop for the
		// new-chat start page (chatstart.go). It never touches the box.
		a.takeStartRecents(msg)
		return a, nil

	case exportedMsg:
		a.exportDone(msg)
		return a, nil

	case copiedMsg:
		// A deliverable taken out of the session that made it (deliverables.go),
		// coming back from the disk the way an export does.
		a.copiedFile(msg)
		return a, nil

	case historyPageMsg:
		return a, a.historyPrefetched(msg)

	case tea.MouseWheelMsg:
		if a.hopShowing() {
			a.hop.live = false
			if msg.Mouse().Button == tea.MouseWheelUp {
				a.hopWalk(-1)
			}
			if msg.Mouse().Button == tea.MouseWheelDown {
				a.hopWalk(1)
			}
			return a, nil
		}
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
		// THE TAB BAR IS READ BEFORE EVERY PLACE'S OWN ROWS, exactly as it is for
		// the press: it is the router's row, drawn on all seven places in the same
		// cells, so a wheel answered by the place under it would scroll a list for
		// a gesture made over a row that is not that list's. Over the bar the
		// wheel walks the PLACES, one room a tick (placemouse.go's
		// [app.placeTabWheel]).
		if cmd, took := a.placeTabWheel(msg.Mouse().Y, placeWheelDelta(msg.Mouse().Button)); took {
			return a, cmd
		}
		// The settings panel is modal for the pointer too: it is the whole
		// screen, so there is no conversation under it for a wheel to reach.
		if a.at(pageSettings) {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.sheet.move(-3)
			case tea.MouseWheelDown:
				a.sheet.move(3)
			}
			a.touch()
			return a, nil
		}
		// And the task page, which is the same claim about the same kind of page:
		// it is the whole screen, and its window follows its cursor rather than an
		// offset of its own, so the wheel walks the cursor (taskview.go).
		if a.at(pageTasks) {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.taskSheetScroll(-3)
			case tea.MouseWheelDown:
				a.taskSheetScroll(3)
			}
			return a, nil
		}
		// And home, on the same terms as both of those (home.go). It is claimed
		// HERE and not left to fall through, because a wheel that reached the
		// conversation from a screen drawn over the top of it would scroll
		// something nobody can see — and put them back on a transcript that has
		// silently moved when esc gives the frame back.
		if a.at(pageHome) {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.home.move(-3)
			case tea.MouseWheelDown:
				a.home.move(3)
			}
			a.touch()
			return a, nil
		}
		// AND THE FOUR PLACES THE ROUTER PROMOTED, on exactly the same terms as
		// the three above (pages.go's [app.placeBodyWheel]). Each of them is the
		// whole screen and each has a window that follows its cursor rather than
		// an offset of its own, so the wheel walks the cursor — and a wheel that
		// fell through from one of them would scroll a transcript nobody can see,
		// which is what a person turning it over the standing list actually got.
		if delta := placeWheelDelta(msg.Mouse().Button); delta != 0 && a.placeBodyWheel(delta) {
			return a, nil
		}
		// And the rewind timeline, on the same terms as all three: it is the whole
		// screen, and its window follows its cursor rather than an offset of its
		// own, so the wheel walks the cursor (rewindsheet.go).
		if a.rewSheet.open {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.rewindSheetScroll(-3)
			case tea.MouseWheelDown:
				a.rewindSheetScroll(3)
			}
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
		// A wheel over conversation tabs browses their names without moving the
		// transcript or changing the conversation below them.
		if a.tabWheel(msg) {
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
		// AND THE SIDE COLUMN ANSWERS THE WHEEL OVER ITS OWN CELLS. It is the
		// oldest thing a pointer does — the list under it moves — and this column
		// was the one list on the surface that did not do it: a wheel turned over
		// thirty columns of roster scrolled the conversation beside it instead, so
		// the rows a person was reaching for stood still while the paragraph they
		// were not looking at moved. It is claimed here, above the room, because
		// the room is the BODY region and the column is beside it, not under it.
		if a.railWheelAt(msg.Mouse().X, msg.Mouse().Y) {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.railScroll(-3)
			case tea.MouseWheelDown:
				a.railScroll(3)
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
			return a, a.scroll(-3)
		case tea.MouseWheelDown:
			return a, a.scroll(3)
		}
		return a, nil

	case tea.MouseClickMsg:
		if a.hopShowing() {
			if msg.Mouse().Button == tea.MouseLeft {
				return a, a.hopPress(msg.Mouse().X, msg.Mouse().Y)
			}
			return a, nil
		}
		if a.pasteEdit.open {
			return a, nil
		}
		if a.copy.on || a.setup.open {
			// A click in copy mode acts on nothing: the rows under the pointer are
			// a FROZEN snapshot, and expanding a call in it would be expanding a
			// row that is no longer where the conversation says it is. The setup
			// screen is the same for the pointer's own reason: it is three
			// keystrokes, and a press through it would land on a frame that is
			// not being drawn (firstrun.go).
			return a, nil
		}
		if msg.Mouse().Button == tea.MouseLeft {
			// THE TAB BAR IS READ BEFORE EVERY PLACE'S OWN ROWS, because it is
			// the router's row and not any place's: it is drawn on every one of
			// them, in the same cells, and a press answered by the place under it
			// would be the one row of the frame that means something different
			// depending on which room you happen to be standing in
			// (placemouse.go's [app.placeTabPress]).
			if cmd, took := a.placeTabPress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			// AND THE COMPOSER AT THE FOOT IS READ BEFORE EVERY PLACE'S OWN ROWS
			// FOR THE SAME REASON: it is the router's row, drawn by the same
			// frame on all seven places, and a press on the box is a press on
			// the box whichever room somebody is standing in. It is the ordinary
			// text-field gesture the conversation already answers
			// (placemouse.go's [app.placeBoxPress], draftclick.go).
			if a.placeBoxPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			if a.at(pageSettings) {
				return a, a.sheetPress(msg.Mouse().X, msg.Mouse().Y)
			}
			// The task page and home are modal for the pointer at the same rung and
			// for the same reason: each is the whole screen, so a press that fell
			// through to the conversation underneath would open a tool call nobody
			// can see (taskview.go, home.go).
			if a.at(pageTasks) {
				// phone lane: the record card's foot is two bands, so the press
				// needs the column as well as the row (taskphone.go).
				return a, a.taskSheetPress(msg.Mouse().X, msg.Mouse().Y)
			}
			if a.at(pageHome) {
				// A CLICK MOVES THE CURSOR, so it is an arrival like a key
				// (homecardread.go).
				pressed := a.homePress(msg.Mouse().X, msg.Mouse().Y)
				return a, tea.Batch(pressed, a.refreshHomeCard(a.now()))
			}
			// AND THE FOUR PLACES THE ROUTER PROMOTED AT THE SAME RUNG AND FOR
			// THE SAME REASON: each is the whole screen, so a press that fell
			// through to the conversation underneath would open a tool call
			// nobody can see. A press on one of their rows moves that place's
			// cursor and never acts (pages.go's [app.placeBodyPress]).
			if cmd, took := a.placeBodyPress(msg.Mouse().Y); took {
				return a, cmd
			}
			// And a background job's page at the same rung and for the same
			// reason: it is the whole screen, so a press that fell through would
			// open a tool call in a conversation that is not even on the frame
			// (jobpage.go). Its edges are the way back and its body is read.
			if a.jobPageOpen() {
				a.jobPagePress(msg.Mouse().X, msg.Mouse().Y)
				return a, nil
			}
			// And the rewind timeline at the same rung and for the same reason: a
			// press that fell through to the conversation underneath would open a
			// tool call nobody can see, in a conversation somebody is about to cut
			// (rewindsheet.go).
			if a.rewSheet.open {
				a.rewindSheetPress(msg.Mouse().Y)
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
			// AND THE HARNESS OFFER LAST OF THE THREE, drawn last and read last
			// (harness.go).
			if a.harnessPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// AND A DESIGN ROOM'S APPROVAL ROW UNDER ALL THREE, drawn under them
			// and read under them (roomapproval.go). It is the same answer the
			// card in the conversation takes, offered where the person is standing.
			if a.roomApprovalPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// AND THE TWO REGISTRY PANELS TAKE EVERY PRESS WHILE THEY ARE UP,
			// which is what modal means for a pointer: a press on a row acts on
			// that row, and a press anywhere else closes the list
			// (connectpanel.go, harnesspanel.go).
			if a.harnPanel.open {
				return a, a.harnessPanelPress(msg.Mouse().Y)
			}
			// AND THE PERMISSIONS PANEL IS THE THIRD OF THEM, on the same terms
			// (permissions.go): a press on a row acts on that row, and a press
			// anywhere else closes the list.
			if a.permPanel.open {
				return a, a.permPanelPress(msg.Mouse().Y)
			}
			// THE STANDING PAGE USED TO BE READ HERE, under the two registry
			// panels. It is a PLACE now and is read with the other three of them,
			// above — one rung for every surface that takes the whole frame,
			// rather than one place resolved among the overlays that are drawn
			// inside a conversation.
			//
			// AND /subharness IS THE FOURTH OF THESE PANELS, on the standing
			// page's old terms and for a sharper version of its reason: one of
			// the card's rows starts work and spends money, so a press moves the
			// cursor and never acts (subharness.go).
			if a.subPage.open {
				return a, a.subPagePress(msg.Mouse().Y)
			}
			if a.connPanel.open {
				return a, a.connectPanelPress(msg.Mouse().Y)
			}
			// AND THE HARNESS PICKER TAKES A PRESS ON ITS OWN ROWS AND NOTHING
			// ELSE, because it is not modal: it hangs under a draft somebody is
			// still typing, so a press anywhere else is a press on whatever is
			// there (harnesspick.go).
			if cmd, took := a.harnessPickPress(msg.Mouse().Y); took {
				return a, cmd
			}
			// AND THE THINKING LADDER TAKES A PRESS ON ITS OWN ROWS AND NOTHING
			// ELSE, on exactly the harness picker's terms and for its reason: it
			// hangs over a draft somebody is still writing, so a press anywhere
			// else is a press on whatever is there (effortchip.go).
			if cmd, took := a.effortMenuPress(msg.Mouse().Y); took {
				return a, cmd
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
			// AND THE DOOR HOME IS THE THIRD, in the hint slot at the right end
			// of the legend. Column-aware for the same reason again: the rest of
			// that rule is a rule, and pressing a rule means nothing (home.go).
			if cmd, took := a.homeDoorPress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			// THE STOP TARGETS ARE READ BEFORE EVERY OTHER COLUMN-AWARE PRESS
			// (stop.go). The card's answers sit over the draft, and the ✕ sits at
			// the right end of the room's pinned header with a hit box three rows
			// tall on a phone — which overlaps the strip and the top of the body,
			// deliberately, because a finger that misses this one either ends work
			// nobody meant to end or leaves a person with no way to end it at all.
			if a.stopPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// Header targets precede the sidebar because they span the window.
			// Breadcrumbs and the Back label use only their rendered cells;
			// punctuation and whitespace do not become navigation shortcuts.
			if a.crumbPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, a.takeRoomPump()
			}
			// AND THE TAB STRIP IS THE ROW ABOVE THE TRAIL, which is a switch
			// between CONVERSATIONS rather than a walk inside one (chattabs.go).
			// It is read with the rest of the pinned rows and above the strip and
			// the rail for the same reason they are: it spans the whole window
			// while both of those claim columns of it.
			if cmd, took := a.tabPress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			if a.roomBackPress(msg.Mouse().X, msg.Mouse().Y) {
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
			// AND THE STANDING CARD'S ANSWERS ROW, which is the same gesture over
			// the same shape of row and is resolved against ITS OWN spans
			// (standing.go's [app.standingPress]).
			if cmd, took := a.standingPress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			// AND THE `keeping an eye on N` SEGMENT IS A DOOR ONTO /standing,
			// read directly before the model's name because they are two segments
			// of the same row and neither swallows the other's columns
			// (standdoor.go).
			if a.keepingPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// AND THE MONEY SEGMENT IS A DOOR ONTO THE SPENDING TAB, read here
			// for the same reason and in the same way: three segments of one row,
			// none of them swallowing another's columns (moneydoor.go).
			if cmd := a.moneyPress(msg.Mouse().X, msg.Mouse().Y); cmd != nil {
				return a, cmd
			}
			// AND THE MODEL SEGMENT IS THE FOURTH: the status row's identity
			// cluster carries the name of what is answering, and pressing a name
			// is how a person changes it (render.go's [app.identityParts]).
			if a.statusPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// AND A MESSAGE WAITING FOR THE ANSWER IS PRESSABLE, which is the
			// whole of its edit gesture: the block says "click to edit" and a
			// block that printed a gesture it did not answer to would be the one
			// dead cell on the screen (park.go). It is read here, with the rest of
			// the chrome, and above the body's own hit-testing for the reason
			// every chrome target is.
			if cmd, took := a.parkPress(msg.Mouse().Y); took {
				return a, cmd
			}
			// AND THE DIM LINE UNDER THAT BLOCK CARRIES ONE DOOR OF ITS OWN:
			// `→ steers it in` puts the waiting message into the answer that is
			// still running (steer.go). It is read directly after the block for the
			// reason it is a separate call at all — the block answers by row and
			// this answers by row AND column, because the rest of that line is
			// statements rather than gestures.
			if cmd, took := a.steerDoorPress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			// AND THE GREETING'S ROWS ACT ON PRESS, with the rest of the chrome
			// they are built with. They are lifted into the body's region and
			// centred in its slack (view.go's [welcomeLift]), which can put them
			// past the end of the conversation's own window — and a press parked
			// for the body's release is measured against that window and dropped
			// outside it. A recent session's row is a door, and a door that only
			// opened when it happened to sit high enough would be the one dead row
			// on the screen.
			if a.welcome.open && !a.roomOpen() {
				if mark, ok := a.chromeAt(msg.Mouse().Y); ok && mark.kind == chromeWelcome {
					return a, a.welcomeRowPress(mark.index)
				}
			}
			// A CLICK ON THE BOX PUTS THE CARET UNDER THE POINTER (draftclick.go).
			// It is read after every chrome target that can stand over or inside
			// the block — the chips, the parked messages, the guard, the greeting
			// above — and before the body's drag parking, because a press on the
			// draft is a press on the draft even when the drag machinery would
			// happily park it.
			if a.draftPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// THE BODY'S CLICK IS PARKED, NOT SPENT. It fires on release — from
			// [app.dragRelease], where the batch this line used to build now
			// lives — unless the pointer sweeps first and the gesture turns out
			// to be a selection (dragselect.go says why a drag that begins on a
			// thinking block must not collapse it).
			a.drag = dragSelect{parked: true, px: msg.Mouse().X, py: msg.Mouse().Y,
				prow: a.bodyContentRow(msg.Mouse().Y)}
			// A new press retires the lit remnant of the last copy: one
			// selection on screen at a time.
			a.dragCopied = 0
			// A SECOND PRESS ON THE SAME SPOT IS A DOUBLE-CLICK, and it takes
			// the word under the pointer; a third takes the row. Both are
			// copied on release exactly as a sweep is (dragselect.go).
			a.dragTake(a.countClick(msg.Mouse().X, msg.Mouse().Y))
			return a, nil
		}
		return a, nil

	case tea.MouseReleaseMsg:
		if a.hopShowing() {
			return a, nil
		}
		if msg.Mouse().Button != tea.MouseLeft {
			return a, nil
		}
		return a, a.dragRelease()

	case dragFlashMsg:
		// The "copied · N lines" word expiring on an idle status line: one
		// repaint, so it comes down (dragselect.go).
		a.touch()
		return a, nil

	case effortFlashMsg:
		// The thinking chip's change expiring on an idle frame: one repaint, so
		// the emphasis comes down and the dial goes back to being furniture
		// (effortchip.go). It is the message above read for the other flash.
		a.touch()
		return a, nil

	case tea.MouseMotionMsg:
		if a.hopShowing() {
			a.setHover(msg.Mouse().X, msg.Mouse().Y)
			if a.hot.kind == hoverHop {
				// Reading a row with the pointer keeps the quick-switch card open.
				a.hop.live = false
			}
			return a, nil
		}
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
		// A MOVE WITH THE LEFT BUTTON DOWN IS THE SWEEP, read before every hover:
		// the rows under it wear the selection and the hover stays where the
		// press left it (dragselect.go).
		if msg.Mouse().Button == tea.MouseLeft && a.dragMotion(msg.Mouse().X, msg.Mouse().Y) {
			return a, nil
		}
		// AND THE TAB BAR IS READ BEFORE EVERY PLACE'S OWN ROWS HERE TOO, for the
		// press's own reason: the bar is the router's row and means the same thing
		// on all seven places, so the word under the pointer lifts wherever a
		// person is standing (placemouse.go's [app.placeTabHover]).
		if a.placeTabHover(msg.Mouse().X, msg.Mouse().Y) {
			return a, nil
		}
		if a.at(pageSettings) {
			a.sheetHover(msg.Mouse().Y)
			return a, nil
		}
		if a.at(pageTasks) {
			a.taskSheetHover(msg.Mouse().Y)
			return a, nil
		}
		if a.at(pageHome) {
			return a, a.homeHover(msg.Mouse().X, msg.Mouse().Y)
		}
		// AND THE FOUR PLACES THE ROUTER PROMOTED, on home's own law: THE POINTER
		// PREVIEWS AND THE CURSOR SELECTS (pages.go's [app.placeBodyHover]). They
		// had no hover at all — the standing list's map answered -1 and the other
		// three had none — so a pointer crossing them lit nothing, on the four
		// screens whose whole shape is a list of rows to aim at.
		if a.placeBodyHover(msg.Mouse().Y) {
			return a, nil
		}
		if a.rewSheet.open {
			a.rewindSheetHover(msg.Mouse().Y)
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

	case steeredMsg:
		// What the session did with a sentence sent INTO the running turn
		// (steer.go). It is beside the submit above because it is the same shape of
		// answer to the same shape of question — a door that took the agent's lock
		// off the Update loop and is reporting back.
		return a, a.tookSteer(msg)

	case streamEventMsg:
		if msg.gen != a.gen {
			return a, nil
		}
		// THE STREAM IS WAITED ON ONCE, however many events this message
		// carried. Two [app.event] calls would be two [app.streamOn] calls and
		// so two goroutines reading one channel, which is two events delivered
		// in whichever order they happened to win — so the applying and the
		// re-arming are separated here and nowhere else ([app.apply]).
		after := a.applyEvent(msg.ev, msg.lump)
		if msg.then != nil {
			after = tea.Batch(after, a.apply(*msg.then))
		}
		return a, a.streamOn(after)

	case streamClosedMsg:
		if msg.gen != a.gen {
			return a, nil
		}
		a.stream = nil
		// The turn is over, so a message that was waiting for it starts now
		// (followup.go). Nil when nothing is queued.
		//
		// AND A PARKED MESSAGE GOES HERE TOO, one per finished turn and after the
		// follow-up queue is offered the same moment (park.go): a ctrl+q message
		// was handed to the session before this one was parked, and a surface that
		// let the newer sentence jump the older one would be reordering what the
		// person said. [app.sendParked] stands down when the follow-up above it
		// has already taken the turn.
		return a, tea.Batch(a.settle(), a.startFollow(), a.sendParked())

	case taskEventMsg:
		if msg.gen != a.taskGen {
			return a, nil
		}
		return a, a.taskEvent(msg.ev)

	case designEventMsg:
		if msg.gen != a.designGen {
			return a, nil
		}
		return a, a.designEvent(msg.ev)

	case designLaneClosedMsg:
		// The agent this lane belonged to is gone, on the task lane's own terms:
		// a lane from an agent that was replaced is already forgotten by its
		// generation, so only the current one is dropped.
		if msg.gen == a.designGen {
			a.designLane = nil
		}
		return a, nil

	case orchEventMsg:
		if msg.gen != a.orchGen {
			return a, nil
		}
		return a, a.runEvent(msg.ev)

	case orchLaneClosedMsg:
		// The agent this lane belonged to is gone, on the other two lanes' terms.
		if msg.gen == a.orchGen {
			a.orchLane = nil
		}
		return a, nil

	case roomEventMsg:
		if a.room == nil || msg.gen != a.room.gen {
			return a, nil
		}
		return a, a.roomEvent(msg.ev)

	case orchPollMsg:
		// A run's page re-reading its run, four times a second (roomorch.go). The
		// generation check is inside [app.orchPoll], which is also what re-arms the
		// clock — a tick for a page that is gone stops rather than reschedules.
		return a, a.orchPoll(msg.gen)

	case roomClosedMsg:
		// The node reached its final state, so the lane ended. The room stays
		// open — a person reading what a task did is not finished reading because
		// the task is finished doing — and says so at its foot (room.go).
		if a.room == nil || msg.gen != a.room.gen {
			return a, nil
		}
		a.room.done, a.room.lane = true, nil
		// A call that was still being spelled out when the lane ended never
		// became one, and one the journal left running will never come back: both
		// rows say so and stop pulsing (room.go).
		a.room.resolveUnfinished()
		a.roomTouched()
		return a, a.wake()

	case roomRecordMsg:
		return a, tea.Batch(a.farRoomRead(msg), a.wake())

	case taskOwnerMsg:
		// A SECOND VIEW ONTO ANOTHER CONVERSATION HAS ARRIVED, or has not
		// (taskowner.go). It is delivered here rather than waited for on the
		// keystroke because reaching the engine is a round trip and this loop draws
		// the frames.
		return a, tea.Batch(a.tookTaskOwner(msg), a.wake())

	case taskGuestNoticeMsg:
		// AND THE CONVERSATION THAT OWNS THE WORK HAS SAID SOMETHING ABOUT IT. It
		// is the only authority for what another window's task is doing, and the
		// only thing on this surface allowed to move a guest page's state
		// (taskowner.go's [app.tookGuestNotice]).
		return a, tea.Batch(a.tookGuestNotice(msg), a.wake())

	// THE ENGINE'S ANSWER TO A CORRECTION SOMEBODY SENT INTO A TASK'S PAGE
	// (steersend.go). It arrives here rather than being waited for inside the
	// keypress, which is the whole of why the keyboard stays live while a send
	// crosses to another machine.
	case steerSentMsg:
		// AND THE RECORD IS ARMED WHATEVER THE ANSWER WAS. Every branch below
		// changes what a recipient is holding — the send is gone, or it is held
		// under a new state, or its words are back on its page — and nobody typed
		// for any of it, so the debounce is armed here rather than in each branch
		// (draft.go's [app.armDraftKeep]).
		return a, tea.Batch(a.steerSent(msg), a.armDraftKeep(), a.wake())

	case farRoomTickMsg:
		return a, a.farRoomPoll(msg.gen)

	case jobLogMsg:
		// A BACKGROUND JOB'S LOG, ONE READING LATER (roomjoblog.go). The reader
		// itself decides whether another beat is owed, because the row it watches
		// is what says the work is over.
		return a, tea.Batch(a.jobPageRead(msg), a.wake())

	case homeNewsMsg:
		a.tookHomeNews(msg)
		return a, nil

	case homeLeftOffMsg:
		a.tookHomeLeftOff(msg)
		return a, nil

	case homeRepoMsg:
		// A REPOSITORY'S READING, COMING BACK. It was asked for on the keystroke
		// that brought a card up and answered here, off the update loop, because
		// a `git status` on a big worktree is the one thing on this screen that
		// can hold a key (homeband_repo.go).
		a.tookHomeRepo(msg)
		return a, nil

	case folderFactsMsg:
		// WHAT THE MACHINE KNOWS ABOUT ONE DIRECTORY, COMING BACK. It was asked
		// for on the keystroke that moved the picker's cursor onto that row and
		// is answered here for the reason above: the branch and the dirty flag
		// come out of git, and a keystroke may not wait for git (folderplace.go).
		a.tookFolderFacts(msg)
		return a, nil

	case folderStoreMsg:
		// THE PICKS AND THE INDEX, COMING BACK. The picker opened from memory
		// without either; this is what turns "the order the sources handed these
		// over" into "the order you actually use them", and it lands mid-list
		// without touching the filter somebody is typing (folderplace.go).
		a.tookFolderStore(msg)
		return a, nil

	case homeTickMsg:
		// HOME IS LIVE, and this is the whole of how: read the folders again,
		// then ask for one more beat. It rides its own clock rather than the
		// paint clock for the reason home.go's [homeEvery] gives (home.go).
		//
		// AND THE TAB BAR'S NUMBERS RIDE THE SAME BEAT. They are a reading of the
		// per-place look stamps and of the records behind each place, so they
		// belong on the clock that already reads the disk rather than on a second
		// one (placecounts.go).
		if a.at(pageHome) {
			a.refreshPlaceCounts(a.now())
		}
		return a, a.homeBeat(msg.gen)

	case placeTickMsg:
		// AND THE PLACES THAT ARE NOT HOME HAVE THE SAME CLOCK, at the same
		// period, re-armed only while one of them is standing (placecounts.go).
		return a, a.placeBeat(msg.gen)

	case searchTickMsg:
		// The quiet interval after a keystroke, arriving. It becomes a store read
		// only when the words have not moved on since (searchpage.go).
		return a, a.searchTick(msg)

	case searchDoneMsg:
		a.searchDone(msg)
		return a, nil

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

	case openRouterFlowMsg:
		return a, a.adoptOpenRouterFlow(msg)

	case openRouterKeyMsg:
		return a, a.adoptOpenRouterKey(msg)

	case hudFadeMsg:
		// One of the two catch-up ticks: nothing changed, but a number that was
		// news four seconds ago has stopped being news, and the frame has to be
		// drawn again to say so.
		//
		// AND A ROOM'S OWN ROW CACHE IS TOLD, because it keeps the whole page
		// rather than the block ([app.roomRows]): the per-block bypass that lets a
		// moving correction repaint ([app.entryRows]) is never reached while that
		// list is being reused, so a delivery receipt would sit on the page for
		// ever instead of fading off it.
		a.roomFading()
		a.touch()
		return a, nil

	case followMsg:
		return a, a.queueFollow(msg)

	// A correction that missed its boundary is an ordinary waiting message from
	// that moment on, and it rides the queue every other one does (steerelbow.go).
	case steerFellMsg:
		return a, a.steerFell(msg)

	case compactedMsg:
		if msg.err != nil {
			a.note("compact failed: " + msg.err.Error())
		} else {
			a.noticeEvent(eventCompacted)
		}
		return a, nil

	case landNoteMsg:
		// A landing's whole answer is one line, the clean one and the one that
		// could not go in alike (landcmd.go).
		a.note(msg.line)
		return a, nil

	case cacheNoteMsg:
		// A cache errand's whole answer is one line, success and refusal alike
		// (cachecmd.go).
		a.note(msg.line)
		return a, nil

	case spelledMsg:
		// The expansion, or nothing at all — a failure, a timeout and a model that
		// ignored the format all arrive here as an empty block and are answered by
		// putting the hint back (spellout.go).
		a.spelled(msg)
		return a, nil

	case taskSizedMsg:
		door, ok := a.agent.(taskCommandAgent)
		if !ok {
			a.note("could not start the task · this session has no task door")
			return a, nil
		}
		// EITHER ANSWER STARTS THE SAME THING, AND ONLY ONE OF THEM SAYS ANYTHING.
		// The sizing call does not pick a road any more — there is one — so what
		// its yes does is ARM this task to divide (internal/session's
		// [Agent.armDivision]) and earn the person a line about it. This used to be
		// the moment a two-row card opened and asked which shape to run, and the
		// card was the wrong question: it wanted a decision about width before
		// anybody had opened the material, from the one person in the room who had
		// not read it. The measured road answers it later and from evidence — the
		// worker hands the parts out only once it has seen how many there really
		// are, and they ride the same frontier the rest of the graph does. So the
		// command starts the work either way, and the note says the one thing the
		// person could not otherwise know: it may not stay one task. A no says
		// nothing, because nothing about narrow work is news.
		if msg.parallel {
			a.note(taskWideNote)
		}
		// THE PHASE CHANGES IN PLACE ON THE BLOCK ALREADY ON SCREEN. Sizing and
		// shaping are two phases of one command, so the wait's own identity is
		// handed on rather than settled and re-raised — a collapse and a second
		// block opening under it would read as two commands (taskcommand.go's
		// [app.beginPreflight]).
		return a, a.startTaskDoor(door, msg.brief, msg.wait)

	case shapingTailMsg:
		// THE BRIEF, AS FAR AS IT HAS BEEN WRITTEN. The lane closing says the
		// shaping call has returned, and the block that was drawing the tail is
		// about to be settled by the answer itself — so nothing is asked for after
		// it and the pump stops (taskcommand.go).
		if msg.done {
			return a, nil
		}
		a.shapingTail(msg.wait, msg.read)
		return a, a.pumpShaping(msg.wait, msg.stream)

	case taskStartedMsg:
		if msg.conv != "" && msg.conv != a.taskBriefConv() {
			return a, nil
		}
		a.settleShaping(msg.wait)
		if msg.err != nil {
			a.note("could not start the task · " + msg.err.Error())
		} else {
			// WHAT LANDED, AND WHAT IT IS CALLED (payload.go). The id is how a
			// person names this node to any other command on the surface and the
			// title is how they recognize it in the roster, so those two step up
			// while the mode word and `started` stay in the note's own dim.
			a.noteFacts(msg.kind+" task "+msg.id+" started · "+msg.title, msg.id, msg.title)
			// AND THE ONE HONEST LINE ABOUT A CUT SHAPER (path (a) of issue
			// #133, [session.TaskShapeFallbackNote]). Its own dim line under the
			// started one, in the same slot [app.noteFacts] already carries facts
			// in; empty is every ordinary start and says nothing.
			if msg.note != "" {
				a.note(msg.note)
			}
			a.noticeEvent(eventTaskStarted)
		}
		// AND THE WORDS THE PERSON TYPED GO ONTO THE NODE. `/task` draws no
		// proposal card, so this is the one moment the surface can keep the
		// instruction it just sent, retained until the node arrives (taskbrief.go).
		return a, a.adoptTypedBrief(msg)

	case subStartedMsg:
		// A subharness launched off the card, answered. The run itself is a task
		// node from here on, so this case says the receipt and the task road draws
		// everything after it (subharness.go).
		a.settleSubharnessRun(msg)
		return a, nil

	case errandMsg:
		// Everything the errand lane moves on, in ONE case rather than three
		// (homeexchange.go's [errandMsg] says why): the stream a Submit answered,
		// one event off it, and the stream ending.
		return a, a.errandUpdate(msg)

	case followingMsg:
		// A turn some other window on this conversation started (watching.go).
		// It is drawn by the code that draws every turn.
		return a, a.followTurn(msg)

	case drivingMsg:
		// The keyboard moved, and nothing on this machine did it (watching.go).
		// The frame that follows draws a composer or the watcher's line, and the
		// wait re-arms itself.
		return a, a.drivingMoved(msg)

	case heldMsg:
		// The far machine's waiting room, answered. Each question is redrawn
		// through the door its live twin comes through, and a kind this build
		// does not know is left waiting (hostlink.go).
		return a, a.replayHeld(msg)

	case linkPingTickMsg:
		// The next timer is armed immediately when this one finds a reconnect in
		// progress; after a real call, its answer arms the next one instead, so
		// calls cannot overlap even when a link is slow.
		if kick := a.linkPingKick(); kick != nil {
			return a, kick
		}
		return a, a.linkPingTick()

	case linkPingMsg:
		a.linkPingBack(msg)
		return a, a.linkPingTick()

	case levelsMsg:
		// What each of a batch of models is dialled to, asked off this loop
		// because over a connection the agent is another machine and the draw
		// path reads the answer three times a frame (reasoninglevel.go).
		return a, a.levelsBack(msg)

	case usageMsg:
		// The session's running cost, on the same terms and for the same reason
		// ([app.usageKick]).
		a.usageBack(msg)
		return a, nil

	case remoteFactsMsg:
		// The other machine's word on a batch of candidate paths. Confirmed
		// files become doors on the next frame; everything else stays the plain
		// text it already was (remotefiles.go).
		return a, a.remoteFactsBack(msg)

	case remotePrefetchedMsg:
		// A file the model wrote, already in this machine's cache before anybody
		// clicked it. It is silent by construction — the only thing it changes on
		// screen is that a row is a link a moment earlier (remotefiles.go).
		return a, a.remotePrefetched(msg)

	case remoteOpenedMsg:
		// One open flow finishing. A success says nothing at all; a failure says
		// the engine's own sentence, once (remoteopen.go).
		return a, a.remoteOpened(msg)

	case remoteOpenSlowMsg:
		// The quiet window closing on a fetch that is still out — the emptiness
		// law's own clock, and the only line this surface draws about a wait.
		return a, a.remoteOpenSlow(msg)

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
	// ONE FRAME IS A WHOLE STRIDE OF THE ANIMATION, which is the whole of what a
	// slower link costs the picture: three slots pass in one frame over a
	// connection, so the spinner arrives where it would have been anyway and it
	// got there in one step instead of three (link.go).
	a.paints += a.frameStride()
	a.dirty = true
	// AND THE FAR DISK IS ASKED ABOUT ON THIS CLOCK AND NO OTHER. The render pass
	// collects candidate paths and this is the only place they are sent, which is
	// what makes the batch a batch: a burst of new rows costs ONE round trip, not
	// one per word and not one per row (remotefiles.go). It is nil on every local
	// session and on every hosted one with nothing to ask.
	kick := a.remoteStatKick()
	// AND WHAT EACH MODEL IS DIALLED TO IS ASKED ON THIS CLOCK TOO, and off the
	// loop, for the reason reasoninglevel.go's header states at length: the draw
	// path reads that fact three times a frame and asking the agent for it there
	// was a round trip per frame and a round trip per pointer motion over a
	// connection. Nil on every surface that has already been told.
	kick = tea.Batch(kick, a.levelKick())
	// A ROOM'S ROWS ARE DROPPED ON THE SAME CLOCK, for the same reason: the page
	// carries the same spinners, count-ups and streaming blocks the conversation
	// does, and a cached row is a still photograph of an animation (room.go).
	if a.room != nil {
		a.room.dirty = true
	}
	if a.dueEvery(usageEvery) {
		// THE COST IS ASKED OFF THE LOOP AND NOT ON IT. Locally the agent answers
		// under a lock; over a connection this is a round trip with a ten-second
		// deadline, and made from here it was made INSIDE the update loop — three
		// times a second, while a turn's events were arriving on the same pipe.
		// The answer lands as a message and folds in there ([app.usageBack]).
		kick = tea.Batch(kick, a.usageKick())
		// AND WHAT THE WORK THIS CONVERSATION STARTED IS SPENDING, on the same
		// clock and ON this loop, because that reading is a tail read of a file
		// and not a lock or a round trip (treespend.go). A node's money reaches
		// the conversation's own books only when the node closes, so without this
		// the figure on the row is hours behind exactly while somebody is
		// watching it — which is issue #145. It is asked only while this
		// conversation HAS work: with no roster there is nothing in the ledger
		// this reading could find, and the file is left alone.
		if a.railAvail() {
			a.readTreeSpend()
		}
	}
	// WHAT THE OTHER WINDOWS HAVE OUT IS RE-READ HERE, and only while something
	// on the frame is drawing it: the roster's record rows say `running` or
	// `incomplete` off that reading, and the history page draws a row per piece
	// of work another window is holding (taskview.go's [app.refreshElsewhere],
	// which keeps its own short window so this clock cannot outpace the disk).
	//
	// A PAGE READ THROUGH ANOTHER CONVERSATION IS DELIBERATELY NOT ON THIS LIST.
	// It briefly was, because that page had to guess whether the work it was
	// showing was still going and this was the nearest reading to guess from — and
	// it was the wrong reading: presence is written per WORKSPACE, so a guest page
	// onto a conversation in another project looked for it here and found nothing,
	// then read the absence as the work being over. That page now hears it from
	// the conversation that owns it (taskowner.go's [app.tookGuestNotice]), so
	// this clock has nothing to tell it and does not spin the disk for it.
	if a.railStanding() || a.at(pageTasks) {
		a.refreshElsewhere()
	}
	a.promoteMarkdown()
	// The welcome box's one-shot animation is the second and last reason this
	// clock runs while nothing is being asked of the model. It steps here and
	// stops itself (welcome.go), which is what makes it one-shot rather than a
	// loop with a condition somebody has to remember to write.
	a.welcome.tick(a.frameStride())
	// AND THE LIVE EDGE WALKS HERE, on the same clock: a lumped stream and a
	// jumped meter become a few frames of writing rather than a paragraph
	// that pops (reveal.go).
	a.tickReveal(a.now())
	// The countdown on an open proposal runs down here, on the clock that is
	// already turning — no ticker of its own (task.go).
	a.tickTasks()
	// AND THE STANDING CARD'S, which drains toward a decline rather than toward
	// an approval (standing.go).
	a.tickStanding()
	// And the approval question's, on the same terms (consent.go). It is the one
	// clock here that ANSWERS at expiry rather than stopping asking, because it
	// is the one question the engine is blocked on.
	a.tickAsk()
	// AND THE REWIND ARM RUNS DOWN HERE TOO (rewind.go): the half-second the first
	// esc buys, and the sentence the mode says when there is nothing to cut. Both
	// are windows with an end, and neither is worth a goroutine.
	a.rewindSweep()
	// AND THE DOOR'S OWN ARM RUNS DOWN HERE ON THE SAME TERMS (quitarm.go): the
	// second and a half the first ctrl+c buys, and the sentence in the hint slot
	// that has to leave the screen when it lapses.
	a.quitSweep()
	// AND A STOP'S OWN DEADLINE RUNS DOWN HERE, on the same terms as the two
	// above and for the same reason: it is a window with an end, the countdown
	// beside `stopping` has to be redrawn while it runs, and something has to be
	// turning the clock that fires it. Past the bound it lets go of the turn
	// whether the engine has or not (see the block below [app.windingDown]).
	//
	// WHAT IT RETURNS IS THE SETTLE'S OWN TWO COMMANDS, and they are folded into
	// this frame's batch rather than dropped: a turn let go of at the bound is a
	// turn that settled, and it is owed the repository probe and the fade ticks
	// every other settled turn is owed.
	kick = tea.Batch(kick, a.stopSweep())
	// AND THE CLOCK OUTLIVES THE TURN when a node does. A task runs for minutes
	// with no stream open: its spinner, its count-up and the countdown above are
	// the third reason this surface asks for a frame while the model is idle.
	//
	// A RUNNING COUNTDOWN IS THE FIFTH, and it is named separately from the turn
	// even though a question can only be up mid-turn: the clock that draws it
	// must not depend on a second fact staying true.
	if a.state == stateWorking || a.welcome.animating() || a.tasksAnimating() ||
		a.askAnimating() ||
		// AND THE STANDING SIDE IS THE NINTH: a card's meter draining toward a
		// decline, and the status segment breathing while a firing is in flight.
		// The second of them is the only thing on this list that is happening in
		// ANOTHER PROCESS — the loop closes because a firing emits an update, the
		// update wakes this clock, and the clock keeps turning while the store
		// says the run is still out (homestanding.go, standing.go).
		a.standingAnimating() ||
		// AND THE REWIND ARM IS THE SEVENTH, and the only one of them that turns
		// with nothing on screen moving at all: the hint slot says "esc again to
		// rewind" for half a second, and something has to be drawing the frame
		// that takes it away again (rewind.go).
		a.rewindTicking() ||
		// AND THE ARMED DOOR IS THE NINTH, and it is the second one that turns
		// with nothing on screen moving at all: the hint slot says "ctrl+c again
		// to quit" for a second and a half, and something has to be drawing the
		// frame that takes it away again (quitarm.go).
		a.quitArmed() ||
		// AND A STOP BEING LET GO OF IS THE TENTH, and it is the third that turns
		// with nothing on screen moving at all — a stopped turn draws nothing new
		// by design (a.apply's own guard). The countdown beside `stopping` has to
		// tick down and the deadline has to be able to fire, so the clock must
		// keep turning for exactly as long as the window lasts and no longer.
		a.stopBounded() ||
		// A BROWSER SOMEBODY IS STANDING IN IS THE SIXTH, and it is the only one
		// of them that can be the whole of what is happening: no turn is
		// running while a person signs in, so without this the waiting line's
		// spinner would be a still photograph (connect.go).
		a.connectAnimating() ||
		// AND A TASK COMMAND'S PRE-FLIGHT IS THE EIGHTH, and it is the second of
		// them that can be the whole of what is happening: no turn runs while
		// `/task` sizes and shapes a brief, and without this the wait's spinner and
		// its count-up would be a still photograph for twenty-five seconds — which
		// is exactly what they were (taskcommand.go's [preflight]).
		a.waiting() ||
		// AND HOME WITH A ROW RUNNING IS THE NINTH, and the third that can be the
		// whole of what is happening: the work is another window's, so no turn of
		// ours runs while its spinner turns. Home is otherwise a still page on its
		// own slow beat, and this test falling false is exactly how it goes still
		// again (home.go's [app.homeAnimating]).
		a.homeAnimating() ||
		// AND A ROOM ON A LIVE NODE IS THE FOURTH: the page is a transcript with a
		// spinner turning on it, and the rail — which is what [app.tasksAnimating]
		// reads — is not always on screen to say so (room.go).
		(a.roomOpen() && !a.room.done) ||
		// AND AN ERRAND ASKED FROM HOME IS THE TENTH. Its turn runs against its
		// own session, so [app.state] says nothing about it — and without this
		// the pane's `⠹ thinking · 4s`, the strip under it and the row's own
		// `⠹ working · 4s` would all be still photographs of the second the last
		// event arrived, which is exactly the complaint the liveness was built
		// for (homeexchange.go). Only while home is up: the whole of what turns
		// is drawn on that screen.
		a.exchangeAnimating() ||
		// AND A DRAFT BEING SPELLED OUT IS THE ELEVENTH, and the fourth that can
		// be the whole of what is happening: no turn runs while the expansion call
		// is out, so without this the spinner in the hint slot would be a still
		// photograph for the ten seconds the call is allowed (spellout.go).
		a.spell.asking ||
		// AND A LINK BEING REDIALLED IS THE TWELFTH, and the fifth that can be
		// the whole of what is happening: the redialling runs in another
		// goroutine on a connection nothing here is waiting on, and without this
		// the `reconnecting to devbox` segment would arrive on one frame and
		// then sit there after the link came back, until something unrelated
		// repainted the row (hostlink.go).
		a.linkNoting() ||
		// AND A QUESTION ABOUT THE FAR MACHINE'S DISK IS THE THIRTEENTH, and the
		// sixth that can be the whole of what is happening. The batch above is
		// sent on this clock, so a clock that stopped the moment the last row
		// landed would leave the words of a turn's final sentence plain text
		// until something unrelated repainted them — and the answer, when it
		// comes, has rows to turn into doors (remotefiles.go).
		a.rfiles.waiting() ||
		// AND A MODEL WHOSE DIAL THIS SURFACE HAS NOT BEEN TOLD ABOUT IS THE
		// FOURTEENTH, on the line above's reasoning exactly: the ask is sent on
		// this clock, so a clock that stopped the moment a list was drawn would
		// leave every row of it spelled without its level until something
		// unrelated repainted them (reasoninglevel.go).
		a.levelsWaiting() ||
		// AND A FORMING PROPOSAL CARD IS THE FIFTEENTH: its spinner and count-up
		// are functions of this frame, not of the fragments that fill its brief.
		// There is deliberately no page gate. The card lives for seconds, and its
		// forming stream already wakes the surface up to ten times a second; a
		// second visibility fact would only let the clock disagree with the card
		// about whether its live row still exists (task.go).
		a.formingCardLive() ||
		// AND AN UNREAD EDGE IS THE SIXTEENTH: the stream may have gone quiet
		// with a lump still walking onto the page, and without this the last
		// paragraph would freeze mid-word until something unrelated asked
		// for a frame (reveal.go).
		a.liveRevealing() {
		return tea.Batch(kick, a.frameTick())
	}
	a.painting = false
	return kick
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
	cut := strings.LastIndexByte(e.revealed(), '\n') + 1
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
		// A MESSAGE THE ENGINE REFUSED NEVER HAPPENED, so the line the surface
		// drew for it comes off the page and the refusal is put where it was
		// (echo.go). The sentence is the engine's own, unchanged: it knows why —
		// a turn is already running, another window is driving — and this
		// surface is not the place to invent a second wording for it.
		//
		// A LOCAL SUBMIT KEEPS THE BEHAVIOUR IT ALWAYS HAD: nothing was echoed,
		// so nothing is withdrawn, and the note lands under the person's line
		// exactly as before.
		if a.echoWithdrawn(msg.echo) {
			a.note(msg.err.Error())
		} else {
			a.note("submit failed: " + msg.err.Error())
		}
		// AND THE TRAY THAT CAME BACK IS WRITTEN DOWN WITH EVERYTHING ELSE
		// ([app.chipsSettled] put it back on the CONVERSATION's composer, wherever
		// the person is standing). Without this the pictures of a refused message
		// were on the screen and in nothing else, so a window closed on that
		// refusal opened again with an empty tray (draftkeep.go).
		return tea.Batch(a.edited(), a.settle())
	}
	// THE ENGINE HAS IT. The mark comes off the SAME line — nothing is appended
	// — which is why a message can never appear twice however the answer and the
	// turn's own events happen to interleave.
	a.echoConfirmed(msg.echo)
	if msg.ch == nil || a.stream != nil {
		return nil
	}
	return a.takeStream(msg.ch)
}

// takeStream is the surface taking up one turn's events: the generation, the
// stream, the working state and the clock.
//
// IT IS ITS OWN FUNCTION BECAUSE THERE ARE TWO WAYS INTO A TURN NOW. One is the
// submit this window made; the other is a turn some OTHER window on the same
// conversation started, which reaches here through watching.go's [Following].
// Both are the same turn drawn by the same code, and the day those two spellings
// drift is the day a watched turn looks different from a typed one.
func (a *app) takeStream(ch <-chan session.Event) tea.Cmd {
	a.gen++
	a.stream = ch
	a.state = stateWorking
	a.lastDelta = time.Now()
	// The turn is open and the first request is out with nothing back from it.
	a.awaited = time.Now()
	a.startClock()
	return tea.Batch(waitEvent(ch, a.gen), a.wake())
}

// feedHooks is the conversation's whole declaration of what it is, as far as the
// reducer that grows its transcript is concerned (feed.go).
//
// THREE OF THESE ARE THE PAGE AND THE REST ARE THE POSTURE. The clock, the
// follow and the touch are what any surface with a screen owes the reducer; the
// spawn card is something a page either draws or does not, and THE LENS SAYS
// WHICH (lens.go's [lens.spawnCards]) — installed here rather than known in
// there, so that a room can adopt the same reducer without inheriting a card it
// has nowhere to draw. The event itself is ingested either way: a lens may lower
// salience and may not drop a fact.
//
// It is read once, at construction, and the closures dispatch through the
// methods rather than capturing what they answer: a test that pins the clock
// after the app is built still gets its clock ([app.now]).
func (a *app) feedHooks(l lens) feedHooks {
	hooks := feedHooks{
		now:    a.now,
		follow: a.follow,
		touch:  a.touch,
		// THE SCREEN-READER TIER DRAWS EVERY BURST WHOLE. It is the only reason
		// this hook exists, and both surfaces answer it the same way (reveal.go).
		// It is one of THE PAGE's and not one of the card's: every surface with a
		// screen owes the reducer an answer to it, so it sits above the lens gate.
		snap: func() bool { return a.linear },
		closed: func(e *entry, ev session.Event) {
			a.learnBackground(e, ev.Output)
			// A CALL THAT CLOSED IS THE ONLY THING THAT MOVES THE AMBIENT COUNTS
			// OR THE SESSION DELTA: both are sums over finished calls, so this is
			// the one place their cache has to be dropped (see [app.hudStats]).
			a.hudStale = true
		},
	}
	if !l.spawnCards {
		return hooks
	}
	// A PROPOSAL FORMS AS A BLOCK, not as a row (task.go). Only propose_task
	// earns one, which is a fact about this page's vocabulary rather than about
	// the event, so the test for it lives on this side of the seam.
	hooks.forming = func(ev session.Event) {
		if ev.Tool == taskTool {
			a.formTask(ev)
		}
	}
	// AND ITS RESULT ARRIVING WITH THE CARD STILL FORMING IS A REFUSAL, for the
	// reason [feed.closeTool] states: a proposal that landed has already replaced
	// the block with its question.
	hooks.closing = func(ev session.Event) {
		if ev.Tool == taskTool {
			a.refuseFormingCard()
		}
	}
	// AND A CUT ATTEMPT TAKES ITS HALF-ARRIVED PROPOSAL WITH IT: the session
	// throws away a partial call before it asks again, so keeping the card would
	// join fragments from two different requests into one proposal
	// ([feedHooks.retrying], task.go).
	//
	// IT IS ONE OF THE CARD'S THREE AND NOT ONE OF THE PAGE'S, which is why it
	// sits below the gate with the other two: the whole of what it does is throw
	// a forming CARD away, and a page that never draws one has nothing to throw.
	// The retry itself is still ingested by the same reducer for everybody.
	hooks.retrying = a.dropRetryingFormingCard
	return hooks
}

// event folds one session event into the conversation and waits on the stream
// for the next one.
//
// Text deltas mark the live entry stale and stop there: they are the flood, and
// the clock decides when a flood becomes a frame. Everything else is discrete
// and paints at once — a tool beginning is a fact a person is waiting for.
//
// IT IS THE ONE-EVENT DOOR. The message pump takes the two halves separately,
// because a message may carry two events and the stream must be waited on once
// however many it carried ([streamEventMsg.then]).
func (a *app) event(ev session.Event) tea.Cmd {
	return a.streamOn(a.apply(ev))
}

// apply is the whole of the above except the wait: it answers what this event
// asks the program loop to DO, and nothing about listening for the next one.
// apply is one wire event delivered ALONE — a lane that does not fold
// (room.go, homeexchange.go), the event that ended a fold and travelled beside
// it, a test's own event. Its whole text arrived in one piece, so it is a lump
// exactly when it is long enough to be one.
func (a *app) apply(ev session.Event) tea.Cmd {
	return a.applyEvent(ev, isLump(len(ev.Text)))
}

// applyEvent is the reducer, told whether the text it carries is a lump the
// clock should walk (reveal.go). Only [waitEvent] can answer that for a folded
// run, which is why the bit is a parameter rather than a length read here.
func (a *app) applyEvent(ev session.Event, lump bool) tea.Cmd {
	// after is what this event asks the program loop to DO, as opposed to what
	// it asks the screen to say. Two events produce one — a turn ending, which
	// may ring a terminal nobody is looking at (notify.go), and a task node
	// starting, which opens a watcher on it (task.go) — and both are carried out
	// to the batch below rather than returned early, because the stream still
	// has to be waited on afterwards.
	var after tea.Cmd
	// THE STOP IS THE LAST THING THAT TURN WRITES ON THIS SCREEN. Between a
	// person's esc and the stream closing the engine is still winding the turn
	// down ([app.windingDown]) and still speaking: the tail of a reply the
	// provider had already buffered, a call the model was half-way through
	// spelling out, a nudge about a request nobody is waiting for any more. Every
	// one of those drew — a fresh assistant block UNDER the `interrupted` note, a
	// new tool row for a call that is never going to run — and what a person read
	// was a model that carried on talking after they stopped it.
	//
	// So nothing new is drawn from here to the close. The events are still TAKEN
	// — the stream is still waited on below, and the short list that closes
	// something already on the screen or carries the turn's accounting still
	// lands ([keptAfterStop]) — and everything else is spent without a mark. This
	// is not [feed.dropLive]'s removal and does not disturb its asymmetry: nothing
	// that arrived before the key is taken away, and the partial reply the engine
	// keeps is the partial reply on screen.
	if a.windingDown() && !keptAfterStop(ev.Kind) {
		return nil
	}
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
	// before its first word arrived. A TEXT DELTA SETTLES, IT DOES NOT SEAL:
	// providers that interleave reasoning with the answer keep growing the same
	// block through [feed.settleThought], and only a real boundary — a tool
	// call, the turn ending — lets go of it ([feed.collapseThought]).
	switch ev.Kind {
	case session.EventReasoning, session.EventThinking:
	case session.EventTextDelta:
		a.settleThought()
	default:
		a.collapseThought()
	}
	// THE WAIT CLOCK IS ANCHORED HERE, on both edges, before anything else reads
	// it. The two lists below are the whole of what the surface knows about a
	// model request's life, and they are kept together so the pair cannot drift.
	switch ev.Kind {
	case session.EventTextDelta, session.EventReasoning, session.EventThinking,
		session.EventToolForming, session.EventToolAnnounced, session.EventToolBegin:
		// The stream has spoken. Whatever it says next it is no longer a request
		// with nothing back from it, which is the only thing the clock is about.
		a.awaited = time.Time{}
		// And it is no longer a SECOND attempt either. "trying again" is only
		// honest while the trying is what is happening; the moment the new
		// stream speaks, this is the reply and nothing else needs saying.
		a.retrying = false
	case session.EventCompacting:
		// A pass that is running has a row of its own saying so, and two answers
		// to one question is one too many — the rule [app.ellipsis] is written
		// to. The clock stands down for it and picks up again when it ends.
		a.awaited = time.Time{}
	case session.EventRetrying:
		// The request that was open has been cut and is being sent again, so the
		// wait starts over from HERE — the previous anchor measured a request
		// that no longer exists.
		a.awaited = time.Now()
	case session.EventToolEnd, session.EventToolFailed, session.EventCompacted:
		// The batch has closed, or the pass has, and in both cases the loop's
		// very next act is another request. Timing from HERE and not from the
		// turn's start is the difference between a wait and a three-minute
		// `go test` the person watched run: [app.lastDelta] would carry the
		// call's whole runtime into the figure and open with "180s".
		a.awaited = time.Now()
	}

	// WHAT THIS EVENT DOES TO THE TRANSCRIPT HAPPENS HERE, UNCONDITIONALLY, AND
	// IT IS THE REDUCER'S LIST AND NOT THIS ONE (feed.go's [feed.ingest]). An
	// event the reducer has nothing to say about writes nothing, so there is no
	// gate to keep in step — and a gate is exactly what this was: a second
	// spelling of the kinds feed.go handles, which a new event wired in the
	// reducer would pass tests and the task room and never reach the chat.
	a.ingestStream(ev, lump)
	switch ev.Kind {
	case session.EventTextDelta, session.EventThinking, session.EventReasoning:
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
		// AND HOME, for the same reason in the same words: it is the whole
		// screen, and a session blocked on an answer behind it would be a
		// question nobody can see to answer (home.go).
		a.closeHome()
		a.askConsent(ev)
		// AND A WINDOW NOBODY IS LOOKING AT SAYS SO OUT LOUD. The session is
		// blocked from here until somebody answers, and the countdown no longer
		// answers for them on a blurred window, so the desktop is told the moment
		// the question goes up rather than at a turn end that is not coming
		// (notify.go's [app.notifyAsk], consent.go's [app.tickAsk]).
		after = a.notifyAsk()

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
		// AND HOME, for the reason the question above closes it: it is the whole
		// screen, and an offer nobody can see is an offer nobody can answer —
		// which is exactly what it was, because the keys are refused up there
		// now rather than swallowed (connect.go).
		a.closeHome()
		a.askConnect(ev)

	case session.EventHarnessOffer:
		// A QUESTION OUTRANKS A PANEL, on the terms the two above it state: the
		// row is drawn over the input, and a question drawn under a fullscreen
		// sheet is a turn waiting on a keyboard nobody can reach.
		a.closeSettings()
		a.closeExpand()
		// AND HOME, in the same words (harness.go).
		a.closeHome()
		a.askHarness(ev)

	case session.EventHarnessRun:
		// The person said yes and the harness has the turn. What follows is its
		// report as ordinary text, so this is a note and not an entry of its own
		// (harness.go).
		a.noteHarness(ev.Text)

	case session.EventHarnessStep:
		// One step of that run, as it lands. It is the only thing on screen
		// between the announcement above and the report below, and it replaces
		// itself rather than piling up: the report carries the whole trail
		// (harness.go).
		a.stepHarness(ev)

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
		// AND HOME WITH IT: the answer lane and its model digits belong to the
		// conversation, while home owns every letter while it is up (task.go), so
		// a proposal left behind it would be a decision with no key that reaches it.
		a.closeHome()
		a.proposeTask(ev)

	case session.EventStandingProposal:
		// A DECISION OUTRANKS A PANEL, for the reason the task proposal above
		// states: the card takes the keyboard's answer lane, and a lane behind a
		// fullscreen sheet is a turn blocked on keys nobody can reach.
		a.closeSettings()
		// AND HOME, in the words the task proposal above states.
		a.closeHome()
		a.proposeStanding(ev)

	case session.EventStandingUpdate:
		// One dim line and never two (standing.go), and THE ENGINE IS WHAT MAKES
		// THAT TRUE: an item being set up, paused or stopped is the direct result
		// of a `stand` call and comes down the turn's own stream — this case —
		// while a FIRING arrives when no turn is running and comes down the
		// standing lane instead (session's tools_standing.go, emitStandingNews
		// beside emitStandingUpdate). The two are exclusive, so this fold has no
		// de-dup to do; the task lane below is the case that does.
		a.standingUpdate(ev)

	case session.EventTaskUpdate:
		// A PROPOSAL'S FORMING BLOCK ENDS HERE, because this is the first breath
		// the approved task takes on any lane: an update for its id means the
		// shaping pause the block was about is over (taskcommand.go's
		// [app.settleProposalWait]). It runs before the fold so the collapse and
		// the row it collapses into land in the same frame.
		a.settleProposalWait(ev.Task.ID)
		// The same event also arrives on the standing lane; [app.taskUpdate]'s
		// (id, state) de-dup is what makes taking both harmless (task.go).
		//
		// The command it hands back is the WATCHER on a node that has just started
		// (task.go's [taskPilot]), and it is carried out through `after` because
		// the de-dup means either lane can be the one that sees "running" first:
		// a surface that armed the pilot only on the standing lane would leave
		// every in-turn node unwatched.
		after = a.taskUpdate(ev)

	case session.EventJobUpdate:
		// A BACKGROUND JOB'S OWN LANE (jobstate.go). It touches nothing the task
		// side owns: no proposal to settle, no pilot to arm, no fold to collapse
		// and no card to land. A job starts, is given a name, and ends — and the
		// only thing this window does about any of those is file it and repaint.
		if ev.Job != nil && a.jobUpdate(*ev.Job) {
			a.touch()
		}

	case session.EventTaskPhase:
		// WHICH OF ITS THREE LIVES A RUNNING NODE IS IN (taskphase.go). It rides
		// both lanes for the reason the update above does — a node proposed in
		// this turn is checked long after the turn ends — and the fold is
		// idempotent, so taking it on both is harmless.
		a.taskPhaseMoved(ev)

	case session.EventOrchestrateNote, session.EventOrchestrateFuel, session.EventOrchestratePause:
		// The three things an adaptive run says. They arrive on the STANDING lane
		// in every real session — the run outlives the turn that asked for it, so
		// the session emits them nowhere else — and they are folded here as well
		// because the fold is one function either way ([app.orchestrateEvent]) and
		// a surface that read them on one lane only would be a surface that quietly
		// stopped drawing runs the day a turn carried one.
		after = a.orchestrateEvent(ev)

	// ── A SENTENCE TYPED INTO THIS TURN (steerelbow.go) ──────────────────────
	//
	// The three of them are one story about one row, so they are read together:
	// the correction hangs off the question, then it lands, or the turn ends
	// first and it was never part of that question at all.
	case session.EventSteerAccepted:
		after = a.steerAccepted(ev.Steer)

	case session.EventSteerConsumed:
		after = a.steerConsumed(ev.Steer)

	case session.EventSteerFellThrough:
		after = a.steerFellThrough(ev.Steer)

	case session.EventTitleChanged:
		a.setTitle(ev.Text)

	case session.EventToolEnd:
		// A FILE THE MODEL JUST WROTE ON THE OTHER MACHINE IS FETCHED NOW,
		// speculatively, silently, before anybody has clicked anything. It is the
		// wave's whole answer to movement: no push was added to the wire, the
		// engine does not know this is happening, and the only difference is that
		// the click which used to be a round trip usually is not (remotefiles.go).
		// Nil on every local session.
		after = tea.Batch(after, a.prefetchWritten(ev))

	case session.EventCompacted:
		// THE SCROLLBACK'S BOOKKEEPING MOVES WITH THE PASS. Everything above
		// this moment is now history the session keeps outside the transcript, and
		// the mark this surface holds into the transcript was taken against a list
		// that no longer exists — left alone it would hand up somebody else's
		// blocks the next time a person scrolled off the top. [app.rebase] carries
		// the place over into the region the pass just created, so the history
		// stays reachable and stays in order.
		a.rebase()
		// AND RE-READ THE METER HERE. The pass just changed what the
		// conversation weighs by an order of magnitude, and the status line's
		// only other reader is the end of the turn — which is a long way off
		// when compaction fires mid-batch. A meter that keeps showing 168k for
		// another two minutes of tool calls is a meter reporting a conversation
		// that no longer exists.
		a.measureContext()
		a.follow()
		a.touch()

	case session.EventRetrying:
		// The rows the dead attempt drew are gone already ([feed.retry], which
		// the card hook is installed on). What is left is this page's own word
		// for itself: the status line says "trying again" until the new stream
		// speaks (the wait clock above clears it).
		a.retrying = true

	case session.EventTurnDone:
		// Both notes go in BEFORE the turn settles, so they land under the reply
		// they are about rather than above whatever is said next. What was
		// CHANGED comes first and what it COST second: the files are the work,
		// and the money is the surface talking about itself.
		a.changedNote()
		a.cacheNote(ev.Usage)
		a.take(ev.Usage)
		// A turn that ends on a blurred window marks the tab as well as sending
		// the banner (windowtitle.go): the banner is gone in seconds, and the
		// tab is what the person scans when they come back to the terminal.
		// Gated on focus alone, not on [app.seenFocus] — a terminal that never
		// reports focus leaves focused true, so the flag never sets and the tab
		// stays quiet, which is the same honest default the banner takes.
		if !a.focused {
			a.landedAway = true
		}
		after = tea.Batch(a.settle(), a.notifyDone())

	case session.EventError:
		a.note("error: " + errText(ev.Err))
		// A turn that failed still paid for the steps it took, and its cache
		// reads are as real as a completed turn's.
		a.cacheNote(ev.Usage)
		a.take(ev.Usage)
		after = a.settle()
	}
	return after
}

// streamOn is what every road out of [app.event] owes the program loop: the next
// wait on the turn's stream, batched with whatever the event itself asked for.
//
// It is a function rather than the tail of one because the stop guard at the top
// of [app.event] leaves early and MUST NOT leave the stream unwaited — a turn
// whose events stopped being read would never reach its close, and the close is
// where the turn settles, the parked message goes and the follow-up queue
// starts. One account of the obligation is the only way two exits can be sure
// they are paying the same one.
func (a *app) streamOn(after tea.Cmd) tea.Cmd {
	if a.stream == nil {
		return after
	}
	// The clock is normally already running (Submit started it), but a stream
	// that outlives its turn's state would otherwise stream into a frame
	// nobody built. Batch drops a nil cmd, so this costs nothing when the
	// clock is up.
	return tea.Batch(after, a.wake(), waitEvent(a.stream, a.gen))
}

// keptAfterStop is the short list of events that still mean something once a
// person has stopped the turn, and it is short on purpose: everything not named
// here would DRAW, and after the key nothing new is drawn ([app.event]).
//
// The three tool closes are kept because they close a row THAT IS ALREADY ON THE
// SCREEN and can open nothing — [feed.closeTool] walks the drawn rows and writes
// the result into the one that is still live, so a `go test` that finished in
// the instant before the cancel reached it reports what it actually did instead
// of standing forever as a call nobody knows the end of. The end stamp
// [app.interrupt] already put on that row is replaced by the call's own, which
// is the truer of the two.
//
// The turn's end and its error are kept because they carry the USAGE, and money
// the turn spent is money the turn spent whether or not anybody waited for the
// answer. Both also settle the turn, and a settle that fell through here would
// simply arrive a moment later with the stream's close.
func keptAfterStop(kind session.EventKind) bool {
	switch kind {
	case session.EventToolFinished, session.EventToolEnd, session.EventToolFailed,
		session.EventTurnDone, session.EventError:
		return true
	}
	return false
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
	// Freeze the reader's place before render-time folding removes the work.
	// Bottom following is already identity-by-edge through stick; a reader above
	// it keeps the same row by moving the raw offset by the layout's height delta.
	oldRows := a.visible(a.bodyWidth())
	oldTotal := len(oldRows)
	oldOffset := a.offsetFor(oldTotal, a.viewHeight())
	wasFollowing := a.stick
	anchor := row{entry: -2}
	if !wasFollowing && oldOffset >= 0 && oldOffset < len(oldRows) {
		anchor = oldRows[oldOffset]
	}
	// THE WHOLE TURN SETTLES, and not only the block the stream was last writing
	// into ([app.settleTurn]).
	a.settleTurn()
	// A turn that streamed nothing but reasoning still ends with a block, and a
	// block left open would keep a finished thought expanded over the next turn.
	a.collapseThought()
	// AND WHAT WAS NOT A FILE MAY HAVE BECOME ONE. The path memo is emptied at
	// the turn boundary rather than never or every frame (pathlink.go): a name
	// the model wrote in its first sentence and only created in its last tool
	// call was correctly plain text all the way through the turn, and is a
	// clickable file from the moment the turn lands.
	clear(a.pathSeen)
	// Questions the turn was blocked on died with it. The session already
	// released those calls; a prompt left on screen would be asking about work
	// that is over (consent.go).
	a.dropAsks()
	// And the offers on the same terms: an account the turn wanted is an account
	// nothing is waiting for once the turn is over (connect.go).
	a.dropConnectAsks()
	// And the harness offer on exactly those terms: a turn that is over is a
	// turn nothing can be run instead of (harness.go).
	a.dropHarnessAsks()
	// A call the model was still spelling out when the turn ended never became
	// one: the row says so and stops pulsing (toolview.go).
	a.dropForming()
	// And a call that STARTED and never came back stops here too, or it starts
	// spinning again the moment the next turn does ([feed.resolveUnfinished]).
	a.resolveUnfinished()
	// AND A CORRECTION THAT NEVER REACHED THE MODEL LEAVES THE QUESTION. A turn
	// that is over gave the model everything it was ever going to; an elbow still
	// waiting under it would claim the opposite forever (steerelbow.go).
	a.settleSteers()
	// And a harness run's live step goes with the turn that was running it: the
	// report is in the transcript by now with every step on it (harness.go).
	a.dropHarnessStep()
	// A proposal the engine is no longer holding stops asking, for the reason
	// the questions above are dropped — except that this one is CHECKED rather
	// than assumed, because the clock may have answered it (task.go).
	a.syncTaskAsk()
	// AND A TURN THE PERSON STOPPED IS MARKED AGAIN, over whatever the dying
	// stream appended after the keypress (hierarchy.go's [app.cutTurn]). It is
	// asked while [app.state] can still answer it — the line below is where the
	// working state is dropped, and stateInterrupted survives to here precisely
	// so this question has an answer.
	if a.state == stateInterrupted {
		a.cutTurn(a.turn)
	}
	if a.state == stateWorking {
		a.state = stateIdle
	}
	// A turn that is over is a turn nothing is outstanding on: the clock stops
	// here rather than at the next turn's start, so a session left idle for an
	// hour cannot open its next turn holding an hour-old anchor.
	a.awaited = time.Time{}
	// AND THE STOP DEADLINE GOES WITH IT, by whichever of its two roads the turn
	// arrived here on: the engine let go inside the window, or the window ran out
	// and [app.stopSweep] let go for it. Either way there is nothing left to
	// bound, and a deadline left standing would draw a countdown over the next
	// turn.
	a.stopBy = time.Time{}
	// THESE TWO ARE MEMORY READS OVER A CONNECTION AND NOT ROUND TRIPS. They
	// used to be the last two synchronous questions a turn's ending put on the
	// wire — the spending and the weight, asked the instant EventTurnDone landed
	// — and the engine now STATES both ahead of that event (internal/remote's
	// server.go), so what they read is this turn's own figures and nothing waits
	// (PERF.md's connection laws).
	a.refreshUsage()
	a.measureContext()
	// AND THE EFFORT TABLE IS REFRESHED ON THE SAME BEAT, which is what carries a
	// rung dialled in ANOTHER window on the same hosted conversation across to
	// this one (reasoninglevel.go states the bound). It costs nothing over a
	// connection — the whole map is in the replica — and one small copy at home.
	a.levelsSeed()
	// THE RING IS SAMPLED HERE AND NOWHERE ELSE: one reading per turn, taken at
	// the only moment the figure is comparable with the reading before it.
	a.sampleContext()
	// AND THE RECEIPT IS FROZEN HERE, before the clock it is measured from is
	// cleared: what the turn took, what it called, what it cost (timestamps.go).
	a.stampTurn()
	a.turnBegan, a.turnOutStart, a.turnCostAt = time.Time{}, 0, 0
	a.approval = a.approvalPosture()
	a.mouse = config.MouseEnabledAt(a.profileDir)
	a.timestamps = config.TimestampsAt(a.profileDir)
	a.workMode = config.WorkAt(a.profileDir)
	a.hopQuick = config.QuickSwitchAt(a.profileDir)
	a.askWait = a.consentWait()
	a.notices.enabled = config.HintsAt(a.profileDir)
	// A turn ending is the moment most hints become true — the answer was long,
	// the window is half full, the money is real — so it is the event they are
	// decided on (notice.go).
	a.noticeEvent(eventTurnEnded)
	a.follow()
	a.touch()
	if !wasFollowing {
		newRows := a.visible(a.bodyWidth())
		a.offset = oldOffset + len(newRows) - oldTotal
		for i, r := range newRows {
			if anchor.entry >= 0 && r.entry == anchor.entry && r.text == anchor.text {
				a.offset = i
				break
			}
		}
		if a.offset < 0 {
			a.offset = 0
		}
		a.stick = false
	}
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

// settleTurn is THE SETTLE A TURN BOUNDARY OWES: every assistant block of the
// turn that just ended is a finished document, whichever ending got here first.
//
// IT IS A PROPERTY OF THE BOUNDARY AND NOT OF THE EVENT THAT REACHED IT. A turn
// ends TWICE on this surface — the session's own EventTurnDone, and then the
// stream closing behind it ([app.sampleContext] states the law and counts by it)
// — and both endings come through [app.settle], so the settle has to be
// idempotent and has to cover the turn rather than the last thing touched. A
// second pass finds every block already settled and writes nothing, which is
// what makes taking both endings free.
//
// [feed.closeLive] settles the block the stream was GROWING, and that is enough
// only while "an assistant block stops being live exactly when something settles
// it" holds — an invariant kept by a dozen scattered call sites (a tool row
// opening, a note, a person's line) and stated nowhere. It is stated here. A
// block that misses its settle draws its markdown raw for the rest of the
// session ([app.assistantRows] renders through [app.settledMarkdown] on
// [entry.settled] alone, and the promotion that moves [entry.mdCut] stops with
// the stream), so the cost of the invariant being wrong once is permanent and
// the cost of stating it is one walk per turn.
//
// IT WALKS THE ENDING TURN'S OWN BLOCKS AND NOTHING MORE. Entries are appended
// in order and a turn number never goes backwards, so that turn is the tail of
// the list: the walk runs from the end and stops at the first entry belonging to
// an older one. Once per turn, never on a frame (PERF.md).
func (a *app) settleTurn() {
	a.closeLive()
	// AND THE BOUNDARY IS REMEMBERED, so that a delta arriving after it knows it
	// is late ([feed.settledTurn], [feed.say]). Stating the boundary is
	// what makes "nothing streamed outlives the settle" a property of this
	// function rather than a hope about the order events happen to arrive in.
	a.settledTurn = a.turn
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if e.turn != a.turn {
			return
		}
		if e.kind != entryAssistant || e.settled {
			continue
		}
		// THE STALE FLAG IS THE WHOLE OF THE SETTLE, in [feed.closeLive]'s words:
		// the rows a block was drawn with mid-stream are handed back by
		// [app.entryRows] until something says they are wrong — and the edge
		// snaps with it, which is livestate.go's law.
		settleBlock(e)
	}
}

// refreshUsage asks the session what it has spent and folds the answer in. It is
// the SYNCHRONOUS reading, and its two callers are the two moments where waiting
// is correct: a turn ending ([app.settle]) and /status, both of which are
// already asking the agent several questions with somebody waiting on the
// answer. The frame clock uses [app.usageKick] instead — see there.
func (a *app) refreshUsage() {
	if a.agent == nil {
		return
	}
	a.take(a.agent.Usage())
}

// usageMsg is one reading of the session's running cost, with the AGENT it was
// read from: a figure about a conversation that has since been replaced is
// somebody else's bill and is dropped rather than folded in.
type usageMsg struct {
	agent Agent
	usage session.Usage
}

// usageKick asks the session what it has spent, off the update loop. See the
// call site in [app.paint] for why it is not asked on it.
func (a *app) usageKick() tea.Cmd {
	if a.agent == nil || a.usageAsking {
		return nil
	}
	a.usageAsking = true
	agent := a.agent
	return func() tea.Msg { return usageMsg{agent: agent, usage: agent.Usage()} }
}

// usageBack folds one reading into the status line's figures.
func (a *app) usageBack(msg usageMsg) {
	a.usageAsking = false
	if msg.agent != a.agent {
		return
	}
	a.take(msg.usage)
	a.touch()
}

// take folds one usage report into the status line's figures. Every field takes
// the LARGER of what it holds and what arrived, because a turn's usage and the
// session's total both come through here and only the session's total is
// monotonic — a per-turn event must never shrink a running total.
func (a *app) take(u session.Usage) {
	prevCost, prevTok := a.cost, a.tokens
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
	// A JUMP WHILE THE TURN IS RUNNING IS WALKED, not popped. The first
	// reading of a working turn pins the drawn figures where they were so
	// the clock has somewhere to ease from; a restore or a switch lands
	// on the exact bill, because nobody is watching those numbers grow
	// (reveal.go).
	if a.cost != prevCost || a.tokens != prevTok {
		// The figures ease FROM where they were, which is why the previous
		// readings are carried in rather than read back off the fields the lines
		// above have already moved (reveal.go's [app.armMeters]). The context
		// weight is not one of them: [app.take] never touches it, so the field
		// still holds the reading the person is looking at.
		a.armMeters(prevCost, prevTok, a.ctxTokens)
	}
	if a.state != stateWorking || a.linear {
		a.snapMeters()
	}
}

// shapingPreviewField names the argument the BRIEF BEING SHAPED is previewed
// by, and it is the sibling of [formingPreviewField] (feed.go) rather than a
// second idea. It stayed on this side of the extraction because the shaper is a
// stream the CHAT watches and not an event the reducer takes.
//
// The shaper answers with one JSON object — {"title","brief","acceptance",
// "where"} (internal/session's task_shape.go) — and `brief` is the field for the
// same reason `content` is a write's: it is appended to and never revised, so
// what has arrived is the beginning of the document and will still be the
// beginning of it when the call is whole. A title is three words that land at
// once and say nothing about progress; an acceptance is written last and is
// blank for most of the wait.
const shapingPreviewField = "brief"

// shapingPreview is [formingPreview] for the other stream this surface watches:
// the shaper's answer as it arrives, rather than a tool call's arguments.
//
// IT IS THE SAME SCANNER, deliberately. Both are prefixes of a JSON object that
// nothing may unmarshal, and [session.PartialString] is the one tolerant read of
// one field of such a prefix in this tree — a second parser here would be a
// second answer to "what has arrived" waiting to disagree with the first.
func shapingPreview(text string) string {
	preview, _ := session.PartialString(text, shapingPreviewField)
	return preview
}

// noteFacts is [feed.note] with THE PAYLOAD RULE's data named: the words inside
// this line that are the answer rather than the sentence around it, in the order
// they appear in the text (payload.go states the rule and does the painting).
//
// It is the same door and not a second one, because a note is a note — what
// changes is only that this one knows which of its own words the person came for.
// A builder that names nothing gets exactly the line it always got.
func (a *app) noteFacts(text string, facts ...string) { a.noteWritten(text, false, facts) }

// noteBlock is a note whose LINE STRUCTURE IS ITS MEANING, and it exists for
// exactly one shape: a subharness card printed into the conversation
// (harnesspanel.go). The card says which step belongs to which lane by INDENTING
// it, so the ordinary note's wrap — which re-flows every paragraph to the frame
// — took a nested lane and laid it flat against the margin, and a person reading
// the result could not tell a step of the run from a step of one arm of a
// choice.
//
// A LINE TOO WIDE IS CUT, NEVER RE-FLOWED. Half a step's detail with an ellipsis
// after it still sits in its own lane; the same detail wrapped is two rows, the
// second of which claims to be a row of the card.
func (a *app) noteBlock(text string) { a.noteWritten(text, true, nil) }

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
	return a.submitting(text, func() (<-chan session.Event, error) { return agent.Submit(ctx, text) })
}

func (a *app) submitShown(text, shown string) tea.Cmd {
	agent, ctx := a.agent, a.ctx
	return a.submittingShown(text, shown, func() (<-chan session.Event, error) { return agent.Submit(ctx, text) })
}

// submitting is that body with the CALL left to the caller: everything a
// message does to this surface — the transcript line, the turn number, the
// clock, the stream it waits on — happens here once, and what differs between
// the doors onto it is one method on the seam.
//
// The second door is a picked harness, which is a turn in every respect except
// which function starts it (harnesspick.go's [app.runPickedHarness]).
func (a *app) submitting(text string, start func() (<-chan session.Event, error)) tea.Cmd {
	return a.submittingShown(text, text, start)
}

// submittingShown separates the words a door receives from the honest line
// the transcript keeps. Slash tags are stripped from the payload but remain in
// the person's message as the chipped token that explains which door acted.
func (a *app) submittingShown(text, shown string, start func() (<-chan session.Event, error)) tea.Cmd {
	// STEERING IS NOT A SECOND TURN, and this is [app.startClock]'s law said
	// about the transcript rather than about the burn window: a plain enter with
	// a turn already streaming is a message spliced into THAT turn, queued by the
	// session and drained at its next step boundary (internal/session's loop.go).
	//
	// Bumping the number here aged out every row of the turn still running —
	// [app.running] asks only about the current turn — so the surface drew
	// "··· still working" underneath calls that were visibly working. The rows
	// have to belong to the turn they are part of.
	if a.stream == nil {
		a.turn++
	}
	// AND A TURN STARTING DISARMS THE DOOR (quitarm.go). The arm is a promise
	// about what the NEXT ctrl+c does, and from here that key is the interrupt
	// again — a hint slot still offering to quit would be naming the wrong verb
	// for the key on top of a turn somebody just started.
	a.disarmQuit()
	// A new turn drops the selection: the calls it was pointing into belong to
	// the turn before this one, and a cursor left on them would answer enter
	// with somebody else's history.
	a.sel = -1
	// The person's own message is the one block on this surface that carries a
	// WALL-CLOCK moment rather than a duration: it is where a sitting starts,
	// and it is what the gap and day marks above it are measured from
	// (timestamps.go).
	// AND THE BLOCK CARRIES THE CONTEXT THE TURN RUNS IN (turncontext.go), which
	// out here is nothing: a message typed into the conversation goes to the
	// conversation. It is asked rather than assumed so that the day the engine
	// routes a conversation's turn into a named thread, the line that says so is
	// already being drawn — one mechanism, keyed off what the session exposes.
	var acted []segment
	if shown != text {
		for _, s := range commandSpans([]rune(shown), true) {
			if s.from > 0 {
				acted = append(acted, s)
			}
		}
	}
	a.said(entry{kind: entryUser, text: shown, turn: a.turn, actedTags: acted, began: a.now(), context: a.turnContext()})
	// AND OVER A CONNECTION THE LINE IS MARKED UNTIL THE ENGINE HAS IT. The
	// sentence is already on the page — the line above put it there, in the place
	// it will keep — and what a connection adds is a gap between that and the far
	// end agreeing to run it. Marking that gap is the whole of echo.go, and it is
	// nothing at all on a surface that is not hosted.
	mark := a.echoPending()
	a.state = stateWorking
	a.lastDelta = time.Now()
	// The turn is open and the first request is out with nothing back from it.
	a.awaited = time.Now()
	a.startClock()
	a.follow()
	a.touch()
	return tea.Batch(func() tea.Msg {
		ch, err := start()
		return submittedMsg{ch: ch, err: err, echo: mark}
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
	// AND THE WALK'S CLOCK STARTS WITH THE FRAMES. The live edge advances by the
	// time that has passed since it last moved (reveal.go), so a surface that sat
	// idle between turns would hand the first lump of the next one every second
	// it was still and dump it whole on the first frame.
	a.revealMoved = a.now()
	return a.frameTick()
}

// frameTick asks for the next frame, at whatever cadence the link earns
// (link.go's [app.frameEvery]).
func (a *app) frameTick() tea.Cmd {
	return tea.Tick(a.frameEvery(), func(time.Time) tea.Msg { return frameMsg{} })
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

// waitEvent takes the next event off the stream — and, while more of the same
// kind are ALREADY QUEUED behind it, takes those too and folds them into one.
// Re-issued after each message, this is the whole bridge between the session's
// goroutine and the program loop.
//
// ── WHY ONE MESSAGE PER DELTA WAS THE WRONG SHAPE ───────────────────────────
//
// bubbletea runs Update and then View for every message it is handed. A provider
// streaming a reply sends a hundred to two hundred text deltas a second, and each
// one used to be a message: a hundred and fifty whole frames a second built for a
// screen the paint clock repaints thirty times a second, of which a hundred and
// twenty were laid out, styled and thrown away without ever reaching a terminal.
//
// THE FOLD IS THE HUB'S OWN LAW, borrowed rather than invented — see
// [foldsInto], and internal/session's function of the same name, which the hub
// already folds a slow subscriber's backlog by.
//
// IT DRAINS AND NEVER WAITS. The loop takes only what the channel is already
// holding and stops the instant it would block, so a stream that has gone quiet
// delivers its one event exactly as promptly as before. What it coalesces is the
// backlog that piled up while the previous frame was being built, which is the
// only moment there is anything to coalesce — the fold is therefore self-limiting:
// a surface that is keeping up folds nothing.
//
// THE TIMING SEMANTICS ARE THE LAST FOLDED DELTA'S. [app.lastDelta] and a
// reasoning block's own end stamp are written when the MESSAGE is handled, so a
// folded run stamps once, at the moment the run's last delta was taken off the
// channel — which is the moment [app.quiet] and [elapsedWord] are asking about.
func waitEvent(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{gen: gen}
		}
		// The run is accumulated rather than appended to in place: a backlog is
		// bounded by the channel and not by anything this side controls, and
		// growing one string per fold would be quadratic in a burst.
		var run strings.Builder
		var then *session.Event
		// THE BIT IS ABOUT THE PARTS AND NOT THE TOTAL. Each event this loop
		// takes is one thing the wire delivered, so the run is a lump exactly
		// when one of them was (reveal.go).
		lump := isLump(len(ev.Text))
		// An event only folds into one of its own kind, so an event that does not
		// fold into itself cannot start a run and the drain is skipped entirely.
	drain:
		for foldsInto(ev, ev) {
			var next session.Event
			var open bool
			select {
			case next, open = <-ch:
			default:
				// Nothing queued. The surface is keeping up, so there is nothing
				// to fold and nothing to wait for.
				break drain
			}
			if !open {
				// The stream ended behind the run. The fold is delivered and the
				// close comes back on the next wait, which is where every other
				// close on this surface comes from.
				break drain
			}
			if !foldsInto(ev, next) {
				held := next
				then = &held
				break drain
			}
			lump = lump || isLump(len(next.Text))
			run.WriteString(next.Text)
		}
		if run.Len() > 0 {
			ev.Text += run.String()
		}
		return streamEventMsg{gen: gen, ev: ev, then: then, lump: lump}
	}
}

// foldsInto reports whether two consecutive stream events are exactly their two
// texts joined, which is true for the two kinds that are A STREAM OF TEXT and
// carry nothing else.
//
// IT IS internal/session's OWN [foldsInto] SAID ON THIS SIDE OF THE CHANNEL. The
// hub folds a slow subscriber's backlog by that rule; [waitEvent] folds a fast
// stream's arrivals by this one, and both rest on the same law: EventTextDelta
// and EventReasoning are each emitted as a kind and a text and nothing more, at
// every one of the places that emit them. A kind that grows a second field comes
// off BOTH lists in the same change, or each fold quietly drops it.
func foldsInto(prev, next session.Event) bool {
	if prev.Kind != next.Kind {
		return false
	}
	return prev.Kind == session.EventTextDelta || prev.Kind == session.EventReasoning
}

// unfold is ctrl+o: every call of the current turn on its own line, or back to
// the last [toolWindow] of them — or, on a task's page, the last screenful
// (render.go's [deck.window]). A scroll up at the top of a room comes through
// here too (room.go's [app.roomUnfoldAtTop]), so the key, the click and the
// wheel all write the same map.
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
			return a.welcomeRowPress(mark.index)
		}
		a.dismissWelcome()
	}
	// A RUN'S PAGE ANSWERS FOR ITS OWN ROWS, before the transcript's hit-testing
	// is asked anything: its rows are chips and links and a gate rather than
	// blocks, so a press on one is resolved by column against the targets the
	// layout recorded (roomorch.go). A press that hits none of them falls through
	// untouched and then does nothing at all, which is what the empty parts of
	// any page on this surface do.
	if a.orchOpen() && a.orchPress(x, y) {
		return
	}
	r, ok := a.rowAt(y)
	if !ok {
		// A CLICK ON NOTHING IS NOTHING, IN A ROOM AS MUCH AS IN THE CONVERSATION.
		// It used to be the way out of a room — press the part of the body that
		// answers to nothing and the page closed behind you — and that made every
		// miss inside a node's page a door: a person reading a transcript who
		// clicked on a blank row, or on the gap beside a paragraph, was thrown back
		// to the conversation without having asked for anything. Empty space is not
		// a gesture on this surface, and a page you are standing in is the last
		// place it should become one.
		//
		// THE WAY OUT IS UNCHANGED AND IT IS NAMED WHERE IT ALWAYS WAS: esc and ←,
		// on the legend at the foot of the frame and on the pinned header at the
		// top of it — and that header row is pressable in its own right
		// ([app.roomBackPress]), which is the pointer's share of the same exit.
		return
	}
	// A TASK LINK IS THE ONE TARGET INSIDE A ROW, so it is resolved before the
	// row's own answer: the prose it sits in has no gesture of its own, and a
	// reference read after the body would be a door the body had already closed
	// the room behind (markdown.go's [linkifyTasks]).
	if a.linkPress(x, r) {
		return
	}
	// AND THE FOOT UNDER A TABLE THAT WAS CUT IS THE OTHER ONE, resolved here for
	// the same reason and in the same breath: it is a phrase inside a row of the
	// model's prose, and the prose around it has no gesture of its own
	// (mdtable.go's [app.footPress]).
	if a.footPress(x, r) {
		return
	}
	// AND THE NARROW DOOR INSIDE A RUNNING BASH ROW, resolved before the row's
	// own answer for the same reason: these words keep the process, while the
	// rest of the row opens its expansion (background.go's [app.keepPress]).
	if a.keepPress(x, r) {
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
	switch r.hit {
	case hitNone:
		// A ROW WITH NOTHING BEHIND IT — a paragraph, a blank, a rule. It does
		// nothing here whether the body is the conversation or a node's page, which
		// is the same law the miss above states: this surface has no empty-space
		// gesture, and the way out of a room is esc, ← and the pinned header.
	case hitTool:
		a.openTool(r.entry)
	case hitFold:
		a.unfold(r.turn)
	case hitCaption:
		a.toggleCap(r.turn)
	case hitWorkFold:
		a.toggleWorkfold(r.turn)
	case hitMore:
		a.showAll(r.entry)
	case hitBrief:
		a.toggleBriefFoldAt(r.entry)
	case hitForming:
		// THE FORMING BLOCK AT THE TAIL (formingblock.go). Its rows belong to no
		// entry — the block is not part of the conversation, it is what stands
		// where one is about to be — so the wait is named by its place in the list,
		// which is what the row carries.
		a.formingPress(r.turn)
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
	case hitSettle:
		// EXCEPT ON THE ONE ROW OF THAT CARD THAT IS A QUESTION. Four answers
		// share the line, so which of them was pressed is a question about x, and
		// a press that missed them all is swallowed rather than expanding the card
		// under somebody who was reaching for one (tasksettle.go).
		a.settlePress(r.entry, x)
	case hitHarness:
		a.harnessCardPress(r.entry, x)
		// The middle column of that card opens the design's ROOM now
		// (harnesscard.go), so whatever door it parked has to be handed on — a
		// room whose lane was never started is a page that never updates.
		cmd = a.takeRoomPump()
	case hitChoice, hitModel, hitStandChoice:
		// All three rows were offered this click before the body and took it (see
		// [app.choicePress] and [app.standingPress]); reaching here means the
		// pointer was in a column no option occupies, and empty space on this
		// surface does nothing.
	}
	// THE NAMED RESULT, AND NOT nil. This used to end `return nil`, which threw
	// away the one command this switch parks — the design room's pump above —
	// and took the whole frame clock with it: [app.openRoom] parks
	// `tea.Batch(waitRoom(…), a.wake())`, [app.wake] sets `a.painting` as it
	// builds its tick, and only [app.paint] ever clears it again. A dropped batch
	// therefore left the surface claiming to paint with no frame on the way, and
	// every later wake answered nil for the rest of the process.
	return cmd
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
// is not lost to the keyboard either way: alt+t opens the roster, and every node
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

// linkHoverAt is which reference of this row's BLOCK the pointer is over, and -1
// for none. It is the ordinal rather than the position on the row for the reason
// [taskLink.ord] carries one (markdown.go), and it is the same test [app.linkPress]
// makes so the words that light are the words that open something.
func (a *app) linkHoverAt(x int, r row) int {
	if len(r.links) == 0 || a.welcome.open {
		return -1
	}
	for _, link := range r.links {
		if link.span.holds(x) {
			return link.ord
		}
	}
	return -1
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
	if a.copy.on || a.at(pageSettings) || a.pick.open {
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
	if mark.index != 0 || !a.modelSpan.holds(x) {
		return false
	}
	// A ROOM POINTS THE SAME DOOR AT THE NODE THE ROW NAMES, and it does so
	// through the span rather than through a second gesture: the segment in there
	// is the task's model, so the picker it opens moves the task's model and
	// nothing else. Which nodes may be moved at all is settled by the render, in
	// the columns it recorded — a node past being moved has no span, so this never
	// sees the press (render.go's [app.identityParts], room.go's
	// [app.roomModelMovable]). One esc puts the door back on the conversation.
	if a.roomOpen() {
		a.openTaskPicker(a.room.id)
		return true
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
		if r.hit != hitTool && r.hit != hitCaption && r.hit != hitTask && r.hit != hitDone && r.hit != hitHarness {
			continue
		}
		key := r.entry
		if r.hit == hitCaption {
			key = captionSelection(r.turn)
		}
		if len(calls) == 0 || calls[len(calls)-1] != key {
			calls = append(calls, key)
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

func captionSelection(key int) int { return -key - 2 }

func selectedCaption(sel int) (int, bool) {
	if sel >= -1 {
		return 0, false
	}
	return -sel - 2, true
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
		// /quit CLOSES THE CONVERSATION IN FRONT, and leaves only when it was the
		// last one this terminal was holding (keeper.go's [app.closeFront]). It
		// keeps its "leaves at once, it is typed out on purpose" property either
		// way: closing one conversation is not something a person types three
		// characters by accident.
		if cmd, more := a.closeFront(); more {
			return cmd
		}
		return a.quit()

	case "help":
		// THE KEY SHEET CARRIES ITS PAYLOAD ON THE LEFT (payload.go): the chord is
		// the thing a person came here to find and the sentence beside it is the
		// explanation, so the first column steps to ink while the second stays in
		// the note's own dim. The rows that name a slash command need nothing from
		// the list — a command wears its chip wherever it is written.
		// AND THE PATH ON ITS LAST ROW IS WRITTEN AGAINST $HOME, for the reason
		// the opening line of every resumed session is ([app.resumedNote]): an
		// absolute journal path is four wrapped rows at eighty columns and seven
		// at sixty, and `~/.aforge/v3/…` is the one shortening that survives being
		// pasted into a shell. /status still prints it whole.
		help := helpText(a.hostedPath(tildePath(a.file, a.tilde)), a.chords)
		a.noteFacts(help, columnFacts(help, true)...)
		return nil

	case "budget":
		// WHAT IT MAY SPEND, AND THE ONE EDITOR FOR IT. Bare it is a door onto
		// the Spending tab; with a figure it writes through the very row that tab
		// writes through, so there is no second answer to what the limit is
		// (budget.go).
		return a.budget(rest)

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

	case "export":
		// The other two doors hand over what is on the screen; this one writes
		// the WHOLE conversation to a file, and the file is written off the loop
		// (export.go). A path is where it goes; without one it goes to the
		// workspace under a name this session derives for itself.
		return a.exportTranscript(rest)

	case "files":
		// WHAT HAS BEEN MADE FOR YOU, AND WHERE IT IS — and over a connection
		// those are two different machines, which is what this command grew a
		// second shape for.
		//
		// A PATH IS THE REMOTE FORM. It fetches that file off the engine's disk,
		// keeps it by content, and hands a copy under its own name to this
		// machine's viewer (remoteopen.go). On a local session there is nothing
		// to fetch — the paths in this conversation are already yours — and it
		// says so.
		if rest != "" {
			return a.openRemotePath(rest)
		}
		// AND BARE, ON A CONNECTION, IT IS THE FAR WORKSPACE AS A PAGE. The list
		// below reads THIS machine's index, and what the session over there has
		// made is written down over there — which used to be an apology printed
		// under the list ([filesRemoteWord]) and is now the browse door
		// (remotefiles.go). A hosted session with no way to ask keeps the list
		// and the apology, which is still the truth for it.
		if a.rfiles != nil {
			a.openBrowse()
			return nil
		}
		// What this conversation and every other one have MADE, as a list, read
		// off the global index (deliverables.go). A deliverable is named by a
		// title a model wrote and lives at a path nobody types, so the only
		// honest way to ask for one is to be shown them.
		a.openFiles()
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
		// AND THE WORDS AFTER IT ARE READ FOR SHAPE (commands.go's [modelArg]):
		// a machine to pin, the word that un-pins, a question for the list, or
		// — still, and as the fall-through — a slug to switch to.
		switch intent, value := modelArg(rest); intent {
		case modelPinLane:
			a.pinLane(a.model, value)
			return nil
		case modelAutoLane:
			a.clearLanePin(a.model)
			return nil
		case modelQuery:
			a.openPickerFiltered(value)
			return nil
		}
		// AND A SLUG THE CATALOG KNOWS IS CHECKED BEFORE IT IS TAKEN. Since the
		// door carries the whole catalog, "openai/gpt-4o-mini-tts" is a name
		// this surface can look up and know is a speaker rather than a
		// conversation — and accepting it would leave somebody talking to a
		// model that answers in mp3, with the failure arriving a turn later as a
		// provider error nobody could connect to what they typed.
		if warning := a.nonChatWarning(rest); warning != "" {
			a.note(warning)
			return nil
		}
		a.switchModel(rest, 0)
		return nil

	case "workspace":
		if rest == "" {
			a.note("usage: /workspace <path>")
			return nil
		}
		if a.anchorWorkspace == nil {
			a.note("this conversation already has a workspace")
			return nil
		}
		resolved, err := a.anchorWorkspace(rest)
		if err != nil {
			a.note("could not set the workspace: " + err.Error())
			return nil
		}
		a.workspace = resolved
		a.owned = false
		a.place = placeShown(resolved, false, a.host)
		a.branch, a.branchDirty, _ = a.gitProbe(resolved)
		a.anchorWorkspace = nil
		a.note("workspace · " + a.hostedPath(resolved))
		a.touch()
		return nil

	case "folder":
		// WHICH FOLDER DO YOU MEAN, asked at any moment. Bare, it is the picker
		// opened on what is already known (folderpick.go); with words after it,
		// the same picker with those words already in its box — which for a path
		// means the columns land inside it, and for a word means the list is
		// already narrowed. One list, one gesture, and the argument only decides
		// where it starts.
		return a.openFolderPick(rest)

	case "land":
		// The other end of choosing a folder: what was written for a folder this
		// conversation only refers to, put into it. Shown first and done second
		// (landcmd.go), and the landing itself runs off the loop.
		return a.runLandCommand(rest)

	case "image":
		// The other door onto the tray, for a picture that is not under this
		// directory or not in the walk: a path, attached (attach.go).
		a.attachPath(rest)
		return nil

	case "attach":
		// The same tray, for everything that is not a picture: a log, a CSV, a
		// stack trace saved to a file. The model is handed the PATH rather than
		// the contents, because an attached file is a file and the session
		// already has a `read` tool (attach.go).
		a.attachFilePath(rest)
		return nil

	case "settings":
		// EVERY DOOR ONTO A PLACE GOES THROUGH THE ROUTER (pages.go). It is one
		// line's difference and it buys the whole of the tab bar being true: the
		// band lands on the place that actually opened, whatever refused, and the
		// verb strip and the map are put away on the way in.
		return a.showPage(pageSettings)

	case "home":
		// The one command on this surface that is not about this conversation.
		// It has no argument form: the screen IS the way of naming what you
		// want, and a command that took a project name would be asking a person
		// to remember what home exists to show them (home.go).
		return a.showPage(pageHome)

	case "search":
		// THE TYPED DOOR ONTO THE SEARCH PLACE, and it takes no argument on
		// purpose. The place IS a box — typing in it searches and the read goes
		// out when the box has been quiet for a moment (place_search.go) — so a
		// query handed in at the command line would be a second way of asking the
		// same question that could rank its answers differently from the one the
		// person then keeps typing into.
		return a.showPage(pageSearch)

	case "spend":
		// AND THE WHOLE MACHINE'S BILL, which is a place and not a note. This word
		// was an alias of /cost until this wave, so the one guess a developer makes
		// for "what has this cost" printed one conversation's figures and never
		// mentioned the machine-wide ledger. /cost still answers this conversation
		// and says so on its own row (commands.go).
		return a.showPage(pageSpend)

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

	case "permissions":
		// What has already been answered, as a list, with the way to take one
		// back on it. No argument form, for /connect's reason and one more: the
		// lines here are globs and tool names a person banked by pressing a key
		// on a card, so the only way anybody could name one at a command line is
		// by reading it off this list first (permissions.go).
		a.openPermissions()
		return nil

	case "standing":
		// WHAT IS ALREADY TRUE HERE, as a list, with the three keys that take one
		// back on it (standingpage.go). Nothing on that page is NAMED at the command
		// line: an order is a sentence somebody said out loud months ago, and the
		// only way anybody could name one is by reading it off this page first.
		//
		// SO THE WORDS AFTER IT ARE A NEW ORDER AND NEVER A QUERY, which is the one
		// thing an argument here could honestly mean. They go through the deliberate
		// door — the same road ctrl+enter takes, with the same guarantee that they
		// are shaped into a card and never carried out as one-off work (standmark.go)
		// — because a person who typed the word for the thing has said what they
		// meant at least as plainly as a chord does.
		if rest == "" {
			return a.showPage(pageStanding)
		}
		return a.standingSay(rest)

	case "harness", "harnesses":
		// The registry, as a list. No argument form, for /connect's reason: a
		// harness is picked from rows a person recognizes, and a name typed at a
		// command line is a name that can be typed wrong — while BUILDING one is
		// a conversation, not a command, and happens in the box above this list
		// (harnesspanel.go).
		a.openHarness()
		return nil

	case "subharness":
		// THE PROGRAMS THIS CONVERSATION CAN RUN, as a filterable list, and the
		// intake card behind each of them (subharness.go). Unlike /harness this
		// one DOES take a name: a subharness's name is its identity across the
		// binary, the store and the command line — one lowercase word, written
		// down in the manifest (exec's validSubharnessName says why the rules are
		// what they are) — so somebody who knows which one they want should not
		// have to find it in a list first. A name nothing answers to becomes the
		// list's filter rather than an error, because a near miss on a surface
		// holding the whole list is a search.
		a.openSubharness(rest)
		return nil

	case "memory", "memories":
		// Bare is the inspect-and-change panel; a query is the transcript form,
		// for somebody who wants matching rows to remain scrollable. The plural
		// alias keeps its older print posture even when it has no query.
		// AND /memories NOW OPENS THE PLACE TOO. The plural used to keep an older
		// print posture — a bare /memories wrote the whole list into the
		// transcript — which was the right answer while memory was a twelve-row
		// overlay and the wrong one the moment it became a place a person can
		// walk into, filter and act on. With a query BOTH spellings still print,
		// because a query is a question rather than a door.
		//
		// AND THE PRINT POSTURE IS STILL THE ANSWER WHERE THE PLACE CANNOT OPEN.
		// The place needs a memory store on this surface; the printed list needs
		// only an agent that keeps memories, and there are surfaces with the
		// second and not the first. A person who typed the plural on one of those
		// gets the list rather than a refusal, which is what the plural has
		// always been for.
		if rest == "" && (name != "memories" || a.memoryReady()) {
			return a.showPage(pageMemory)
		}
		a.runMemories(rest)
		return nil

	case "remember":
		a.runRemember(rest)
		return nil

	case "forget":
		a.runForget(rest)
		return nil

	case "crew":
		// The four models aforge uses on your own behalf, as one word (crew.go).
		// The bare form is the three presets with yours marked; a word applies
		// one. An unknown word shows the three and changes nothing, which is the
		// shape every choice row on this surface refuses in.
		a.runCrew(rest)
		return nil

	case "task":
		return a.runTaskCommand(rest)

	case "history":
		// The place onto every task this MACHINE has run, this session's and every
		// conversation's before it, across every project (place_tasks.go). It is
		// NOT spelled /tasks: the three /task rows all mean give aforge work, and a
		// plural among them was a command that answered the muscle memory for
		// starting one (commands.go says it at more length).
		//
		// IT OPENS ON A MACHINE THAT HAS RUN NOTHING, exactly as the tab bar does.
		// The command used to refuse there, on the argument that a page with a
		// title and nothing under it is the emptiness law broken — and what the
		// page draws with nothing under it is three sentences saying what tasks are
		// ([tasksTeach]), which is an answer. Every door onto this place is the one
		// door now.
		//
		// Both commands go through one function, because a bare /task opens it too
		// (taskcommand.go), and two copies of that line are two ways for one place
		// to differ from itself.
		return a.openTaskPage()

	case "status":
		// The status line's whole list, said in the transcript. It is an ANSWER
		// rather than a panel: a person who asked a question about their session
		// wants it where they can scroll back to it, not on a fullscreen sheet they
		// have to leave before they can act on it (statusnote.go).
		//
		// --json is the same list as one wire-ready object. The whole object is
		// the payload, so it lands through [app.note] and not [app.noteFacts] —
		// there is no third column to lift, and re-highlighting inside a
		// serialized object would corrupt the JSON. Any other words after the
		// name fall through to the bare form, the route this command has always
		// answered on.
		if rest == "--json" {
			a.note(a.statusJSON())
			return nil
		}
		text := a.statusText()
		a.noteFacts(text, a.statusFacts(text)...)
		return nil

	case "cost":
		// THE FIGURE IS THE PAYLOAD AND THE LABEL IS THE FURNITURE (payload.go).
		// Both of these notes are laid out label-then-fact down a column
		// ([labelledLines]), which is the same hierarchy this rule states — so the
		// second column steps to ink and the first stays where it was. Nothing is
		// brightened into existence: every line here is already one the emptiness
		// law let through.
		spent := a.costText()
		a.noteFacts(spent, columnFacts(spent, false)...)
		a.noticeEvent(eventCostShown)
		return nil

	case "cache":
		// The shared build cache — reading it, and the guarded road to deleting
		// it. Every branch runs off the loop and answers as a note; the guard
		// itself, and why it is typed rather than a card, is cachecmd.go.
		return a.runCacheCommand(rest)

	case "manual":
		// Aforge's own manual, in the conversation, AS WRITTEN (manualcmd.go).
		// It is an answer rather than a place for /status' reason — a person who
		// asked a question about the product wants it where they can scroll back
		// to it — and it is a lookup rather than a turn, so it makes no model
		// call and spends nothing.
		a.runManualCommand(rest)
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

	case "debug":
		// The third door onto one switch — the other two are --debug and
		// AFORGE_DEBUG — and the only one that can be reached from inside a
		// conversation that is already open (commands.go's [app.runDebugCommand]).
		// It is also the only one whose scope is this conversation alone: the
		// pin and the flag were given to the whole process, this was typed here.
		a.runDebugCommand()
		return nil

	case "compact":
		agent, ctx := a.agent, a.ctx
		a.note("compacting…")
		return func() tea.Msg { return compactedMsg{err: agent.Compact(ctx)} }

	case "rewind":
		// THE COMMAND IS THE DELIBERATE DOOR AND IT OPENS THE TIMELINE
		// (rewindsheet.go), while esc esc keeps the quick inline gesture
		// (rewind.go). Somebody who typed six letters to get here has already told
		// this surface that the answer is not the message they just sent — it is
		// somewhere back in the conversation, and finding it wants the whole of the
		// conversation, a search over it, and a look at the point before the cut.
		return a.openRewindSheet()

	case "new":
		// The command that replaces the agent is the one command here that
		// returns work: the standing task lane belongs to the agent that handed
		// it over, so the next conversation subscribes to its own (task.go).
		//
		// The bool is for a caller with a sentence to send afterwards; /new has
		// none — it is the whole of what was asked for — and the refusal is
		// already a note in the conversation it was typed in.
		cmd, _ := a.renew()
		return cmd

	default:
		// A DROPPED FILE IS NOT AN UNKNOWN COMMAND. A terminal that delivers a
		// drop as keystrokes writes the path straight into the box, its leading
		// `/` puts the composer into command mode, and this arm used to answer
		// `unknown command: /var/folders/…/Screenshot · try /help` — a surface
		// telling somebody their screenshot does not exist. dropkeys.go's fold
		// catches nearly all of those before enter; this is the net under it,
		// and it is the last one there is: if the whole line names files that
		// are really on this disk, it was a drop and it becomes chips.
		if a.droppedLine(line) {
			return a.edited()
		}
		a.note(unknownCommandWord(name))
		return nil
	}
}

// newUnavailableWord is what /new says where no fresh-session seam was wired —
// a headless frame, or a launcher that did not supply one. It is a constant
// because home's door opens a new conversation through this same seam and has
// to refuse in the same words when it is not there (home.go).
const newUnavailableWord = "/new is unavailable here"

// canStart and canOpen report whether this surface has a door onto a NEW
// conversation and onto an EARLIER one.
//
// Two seams answer each: the older pair that hands back an agent alone
// ([Options.Fresh], [Options.Resume]) and the pair that hands back the whole
// conversation ([Options.Start], [Options.Open]). Every refusal in the surface
// asks these rather than one field, so a door that wires only one of the two
// still has working rows instead of a /new that says it is unavailable while
// the seam behind it is sitting there.
func (a *app) canStart() bool { return a.start != nil || a.fresh != nil }
func (a *app) canOpen() bool  { return a.open != nil || a.resume != nil }

// nextConversation asks the door for a fresh conversation in THIS
// conversation's own workspace, which is what the empty string means to the
// seam (tui3.go's [Options.Start]).
//
// The bool is whether what came back is a WHOLE conversation. The fallback
// builds one carrying nothing but the agent and its file, and [app.takeUp] then
// leaves every other seam standing — which is exactly what the older Fresh door
// has always done, said once here instead of branched around downstream.
func (a *app) nextConversation() (Conversation, bool, error) {
	if a.start != nil {
		conv, err := a.start("")
		return conv, true, err
	}
	agent, file, err := a.fresh()
	if err != nil {
		return Conversation{}, false, err
	}
	return Conversation{Agent: agent, SessionFile: file}, false, nil
}

// openConversation is the same question about a transcript somebody picked.
func (a *app) openConversation(file string) (Conversation, bool, error) {
	if a.open != nil {
		conv, err := a.open("", file)
		return conv, true, err
	}
	agent, err := a.resume(file)
	if err != nil {
		return Conversation{}, false, err
	}
	return Conversation{Agent: agent, SessionFile: file}, false, nil
}

// takeUp takes up a conversation the door built: the agent, and — when the door
// answered the whole seam — the per-agent closures that came with it.
//
// THIS IS WHERE THE APPROVAL BUG IS FIXED. The trio below used to be wired once,
// at boot, around the agent the door opened before the surface existed, and it
// stayed wired to that agent through every /new and every resume. So an "always"
// answered in a conversation opened later was written to the profile correctly
// and pushed into a closed session (cmd/aforge's chatv3_approval.go says what the
// push is for), which the person had no way to see: the card said saved, it was
// saved, and the very next call asked again. Rebinding here means the closure a
// keystroke reaches is always the one minted around the agent that keystroke is
// about.
//
// WHAT THE OLDER SEAM RETURNS IS NOT A CONVERSATION and must not be treated as
// one — a bundle with nine zero fields would silently clear the recent list, the
// draft and the trio. That is what `whole` says, and it is the caller's own fact
// rather than something guessed from the fields.
func (a *app) takeUp(conv Conversation, whole bool) {
	if conv.Agent != nil {
		a.agent = conv.Agent
	}
	a.file = conv.SessionFile
	// AND THE SENDS ARE NOT RE-KEYED HERE. They are held under the drafts lane's
	// own identity for the conversation they were typed in (steersend.go's
	// [app.steerOwner]), which the new file does not change: a correction crossing
	// for the conversation this window just left settles onto that conversation's
	// own composer, wherever it now is (recipient.go's [app.atOwnedComposer]).
	if !whole {
		return
	}
	if workspace := strings.TrimSpace(conv.Workspace); workspace != "" {
		a.workspace = workspace
	}
	a.owned = conv.Owned
	a.anchorWorkspace = conv.AnchorWorkspace
	if shown := strings.TrimSpace(conv.Place); shown != "" {
		a.place = shown
	} else {
		// The door usually leaves this to the surface, because what a place is
		// CALLED is a rendering question and this is the package that answers it
		// (host.go's [placeShown]).
		a.place = placeShown(a.workspace, a.owned, a.host)
	}
	if conv.ContextWindow > 0 {
		// Zero is nobody knowing, and a meter drawn against an unknown window
		// means nothing — so the one this surface already has stands.
		a.ctxWindow = conv.ContextWindow
	}
	// These four ARE cleared by a zero, and deliberately: a project that keeps no
	// draft and a workspace that records no history are answering about their own
	// directory, and carrying the previous conversation's answer into them would
	// be keeping what somebody's repository asked us not to keep (draft.go, and
	// the door's per-workspace read of the same two rows).
	a.draftFile = conv.DraftFile
	a.history = conv.History
	a.saveApproval = conv.SaveApproval
	a.saveBashApproval = conv.SaveBashApproval
	a.applyApprovals = conv.ApplyApprovals
	if conv.RecentSessions != nil {
		a.recentSessions = conv.RecentSessions
	}
}

// freshAndEmpty is the conversation nobody has used yet: nothing on screen, no
// turn ever run, no work out and nothing waiting.
//
// IT IS THE ONE STATE /new MAY REPLACE. Closing it costs nothing — there is
// nothing in it — and it is what a conversation is in when somebody typed /new
// because they had not started yet, or pressed enter on a row of the welcome
// box. Keeping it would spend a slot on a conversation nobody typed in.
func (a *app) freshAndEmpty() bool {
	if a.turn != 0 || a.state == stateWorking {
		return false
	}
	// A NOTE IS NOT A CONVERSATION. Every surface opens with the surface's own
	// lines on it — `esc interrupts · ctrl+c twice quits`, a door's notice, a
	// refusal somebody read — and counting those would make "fresh and empty"
	// false on the very first frame of every session, which is the one state
	// this test exists to recognise.
	for i := range a.entries {
		if a.entries[i].kind != entryNote {
			return false
		}
	}
	if len(a.taskOrder) > 0 || a.asking() || len(a.parks) > 0 {
		return false
	}
	return a.hudStats().jobs == 0
}

// renewReplaces is whether the next create TAKES THE PLACE of the conversation
// on screen rather than opening beside it.
//
// IT IS ONE READING AND NOT TWO. The door acts on this answer and the new-chat
// start page has to know the same answer BEFORE the door acts — what it does
// with the leaving box depends on whether that conversation will still exist
// (chatstart.go's [app.startChatEnter]) — and two copies of the question are two
// answers waiting to disagree about whether somebody's conversation was closed.
func (a *app) renewReplaces() bool { return a.freshAndEmpty() && !a.startKeepsLeaving() }

// renew is /new: ANOTHER CONVERSATION IN THIS PROJECT, unless the one on screen
// is fresh and empty, in which case it takes its place.
//
// IT ADDS RATHER THAN REPLACES, and three things say so. Every neighbouring door
// adds — home's enter, home's typed path, the welcome box's rows — and a /new
// that closed a conversation with three tasks running would be the one place the
// surface still punished somebody for using it. It makes home's action row the
// same act whether or not a path was typed, with the branch only about WHICH
// workspace. And nothing is lost by the exception, because the exception is the
// empty case.
//
// The transcript is cleared because it belongs to the conversation being left:
// a fresh session file with the old conversation still on screen would be the
// surface claiming context the model does not have.
//
// It returns the commands the next conversation owes itself: its own standing
// lanes and the project's record (task.go, taskmention.go).
//
// AND IT SAYS WHETHER THE CONVERSATION ACTUALLY CHANGED. Every refusal below
// returns no work, and no work is also what a successfully renewed conversation
// with nothing to subscribe to returns — so a caller that read the nil as "the
// door held" was reading a coincidence. The callers that matter are the two that
// SEND a sentence into whatever the renew left in front of them (home.go's
// [app.homeStart], placekeys.go's [app.placeTalkAbout]), and sending it into the
// conversation somebody was already in is the one outcome neither of them may
// have.
func (a *app) renew() (tea.Cmd, bool) { return a.renewRefusing(a.note) }

// renewRefusing is [app.renew] with ONE THING MOVED: where its two refusals are
// said.
//
// THE TRANSCRIPT IS NOT ALWAYS ON THE FRAME. Every caller but one is standing in
// a conversation, and a refusal noted into it lands where the person is already
// looking. The new-chat start page is drawn OVER the conversation and hides it
// (chatstart.go, view.go's [app.bodyRows]), so the same sentence noted the same
// way would be written onto a screen nobody can see — and the one thing a person
// who just pressed enter needs is to be told why nothing happened.
//
// ONLY THE REFUSALS MOVE. Everything the successful road says — which of the two
// things happened, the door's own notice, a close that failed — still goes into
// the new conversation's entry line, because by then that conversation is what is
// on screen.
func (a *app) renewRefusing(say func(string)) (tea.Cmd, bool) {
	if !a.canStart() {
		say(newUnavailableWord)
		return nil, false
	}
	replacing := a.renewReplaces()
	conv, whole, err := a.nextConversation()
	if err != nil {
		say("new session failed: " + err.Error())
		return nil, false
	}
	// THE DOOR IS ASKED BEFORE ANYTHING IS PUT DOWN, which is [app.openSession]'s
	// own repair: a /new that failed used to leave the surface holding a closed
	// session with nothing to fall back on, and now a refusal costs nothing at
	// all.
	leaving, side := a.agent, a.detachConversation()
	if replacing {
		// AND NOT ON A SHARED HANDLE, for [app.openSession]'s reason: that agent
		// is the same object the door just handed back, now naming the session
		// the engine swapped to, so this close would land on the conversation
		// /new had just made ([Options.SharedAgent]).
		if leaving != nil && !a.shared {
			leaving.Interrupt()
			if err := leaving.Close(); err != nil {
				a.note("close failed: " + err.Error())
			}
		}
	} else {
		// AND THE CONVERSATION GOES ON RUNNING, in the keeper (keeper.go). Its
		// draft file is written there; the sentence in the box goes with the
		// PERSON, which is what this door has always promised.
		a.stow(a.front(), side)
	}
	if !whole {
		// The older seam hands back an agent alone, so the surface keeps every
		// other seam it was holding ([app.takeUp] states this).
		conv = Conversation{Agent: conv.Agent, SessionFile: conv.SessionFile,
			Workspace: a.workspace, Place: a.place, Owned: a.owned,
			ContextWindow: a.ctxWindow, DraftFile: a.draftFile, History: a.history,
			RecentSessions: a.recentSessions, SaveApproval: a.saveApproval,
			SaveBashApproval: a.saveBashApproval, ApplyApprovals: a.applyApprovals}
	}
	cmd := a.attachConversation(conv, nil)
	a.resumed = false
	// THE DRAFT GOES WITH THE PERSON AND NOT WITH THE CONVERSATION, which is what
	// this door has always promised in those words: /new starts something else,
	// and the sentence in the box is the person's NEXT one. The messages that
	// were parked behind a turn come with it, in the order they would have been
	// sent — nobody is left to send them, and they are still what somebody typed
	// (park.go, quitarm.go's [app.leavingDraft]).
	if side.draft != "" {
		a.input.setText(side.draft)
	}
	a.chips = side.chips
	// AND THE COMPACT PASTES GO WITH THE SENTENCE THEY ARE IN, copied rather than
	// shared because the conversation being kept is holding the same chips
	// (recipient.go): the tokens in the box mean nothing without the documents
	// behind them, and a draft that arrived with the tags alone would send
	// `[paste 1 · 42 lines]` to the model as though those were the words. The task
	// pages' own lines stay with the conversation they were typed into.
	a.pastes = append([]pasteChip(nil), side.pastes...)
	// AND THE NOTE SAYS WHICH OF THE TWO HAPPENED. A count appearing on the
	// status line is not enough on its own to tell somebody whether the
	// conversation they were in is still running.
	switch {
	case replacing && a.file != "":
		a.note("new session · " + a.hostedPath(a.file))
	case replacing:
		a.note("new session")
	default:
		a.note("new conversation · " + a.place)
	}
	if conv.Notice != "" {
		// The door had something to say about HOW this conversation came to be
		// open — "session open elsewhere — started a new one" is the sentence
		// that exists — and the entry line is where the first conversation's own
		// notice lands too ([Options.Notice]).
		a.note(conv.Notice)
	}
	if key := a.convKey(a.file); key != "" {
		a.rememberOpen(key)
	}
	return cmd, true
}

// ── the adaptive-run lane ───────────────────────────────────────────────────
//
// THE THIRD STANDING SUBSCRIPTION, and the one without which the run room could
// never open. A run is not a node: it is on no roster, it has no row, and the
// three events it sends are the only things that say one exists at all
// (roomorch.go). They are emitted on a lane of the session's own and never on a
// turn's stream, because a run outlives the turn that asked for it by
// construction — and the fuel gate, which is a QUESTION, arrives latest of all.

// runAgent is the slice of *session.Agent this lane needs, asserted rather than
// added to [Agent] for [designAgent]'s reason: adaptive runs are OPTIONAL, and a
// scripted agent in this package's tests has never heard of one.
type runAgent interface {
	// Orchestrations is the standing subscription: the planner's notes, the
	// fuel gauge crossing its warning mark, and the gate.
	Orchestrations() <-chan session.Event
}

// runner is the agent under this surface, when it drives runs at all.
func (a *app) runner() (runAgent, bool) {
	agent, ok := a.agent.(runAgent)
	return agent, ok
}

// watchRuns opens the lane and starts pumping it. It is called wherever
// [app.watchDesigns] is, and for the same reason: the channel belongs to the
// agent that handed it over, so a replaced conversation gets a new one.
func (a *app) watchRuns() tea.Cmd {
	agent, ok := a.runner()
	if !ok {
		return nil
	}
	a.orchGen++
	if leavable, ok := agent.(leavableRunner); ok {
		a.orchLane, a.stops.runs = leavable.WatchOrchestrations()
	} else {
		a.orchLane, a.stops.runs = agent.Orchestrations(), nil
	}
	return waitRun(a.orchLane, a.orchGen)
}

// leavableRunner is the orchestration lane WITH A WAY OUT OF IT (session's
// orchestrate.go). It is asserted separately from [runAgent] for that
// interface's own reason, and a nil stop is an agent that can only be abandoned
// (switcher.go's [laneStops]).
type leavableRunner interface {
	WatchOrchestrations() (<-chan session.Event, func())
}

// waitRun takes one event off the lane and asks for the next.
func waitRun(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return orchLaneClosedMsg{gen: gen}
		}
		return orchEventMsg{gen: gen, ev: ev}
	}
}

// runEvent folds one event from the lane in and re-arms the pump.
func (a *app) runEvent(ev session.Event) tea.Cmd {
	return tea.Batch(a.orchestrateEvent(ev), waitRun(a.orchLane, a.orchGen), a.wake())
}

// orchestrateEvent is what an adaptive run's three kinds DO, in one place,
// because both lanes that can carry them fold them identically.
func (a *app) orchestrateEvent(ev session.Event) tea.Cmd {
	switch ev.Kind {
	case session.EventOrchestrateNote:
		// One line of what the planner is thinking, between two completions, and
		// the sentence a finished run signs off with. A REPORT: it lands on the
		// run's page when one is open and in the transcript when none is.
		a.orchNoteEvent(ev)
	case session.EventOrchestrateFuel:
		// The tank, and its warning at the 80% mark. A REPORT too, and it never
		// blocks anything: the gauge it feeds is pinned at the top of the run's
		// own page.
		a.orchFuelEvent(ev)
	case session.EventOrchestratePause:
		// THE RUN HAS SPENT ITS TANK, and this is the one orchestrate kind that is
		// a QUESTION. It is answered on the run's own page, so raising it brings
		// that page with it — the command handed back is the poll that page opens
		// with (roomorch.go).
		return a.orchPauseEvent(ev)
	}
	return nil
}

// quit is the door out of the PROGRAM. It takes this terminal off every
// conversation it is holding — ending the ones this process runs, detaching from
// the ones a host runs (keeper.go's [app.leaveEverything]).
func (a *app) quit() tea.Cmd {
	// A provider connection owns a loopback listener. It leaves with the
	// surface even when the browser is still open, just as account connections
	// and file doors below do.
	a.cancelSetupAuth()
	// The draft goes to disk on the way out, synchronously and before anything
	// else: the debounce may be mid-window, and a sentence typed in the last
	// three hundred milliseconds of a session is exactly the one a person would
	// be most surprised to lose (draft.go).
	//
	// AND WHAT IS WRITTEN IS THE DRAFT PLUS WHATEVER IS STILL PARKED
	// (quitarm.go's [app.leavingDraft]): a message waiting for an answer that is
	// never now going to land is a message the person typed and pressed enter
	// on, and it comes back next launch rather than going quietly.
	// AND THE WHOLE COMPOSER GOES, not only the conversation's sentence: the
	// caret, the documents behind its compact tokens, its tray, and every task
	// page's own unsent line (draftkeep.go). A window closed on a half-typed
	// correction opens again with it (#653).
	a.writeDraftsNow(a.leavingDraft())
	// AND THIS TERMINAL COMES OFF EVERY CONVERSATION, IN PARALLEL (keeper.go's
	// [app.leaveEverything]). A hosted conversation is DETACHED and keeps
	// working; an in-process one ends here, because this process was the only
	// thing running it. The ones held behind the screen have their own drafts
	// already on disk — written when they were left — so what is above is the
	// only box that still needs saving.
	a.leaveEverything()
	// AND THE FILE DOOR GOES WITH THE SURFACE THAT OPENED IT. Every minted id and
	// the browse token die here, which is the whole of the capability bargain: a
	// URL that outlived the conversation would be a door onto somebody else's
	// disk with nobody left holding it (remotefiles.go).
	a.closeFileDoor()
	// AND EVERY ERRAND WITH IT. An exchange is a session with a lock on a
	// transcript; one left open by a process that has gone is a conversation
	// nobody can reopen, and a stood one's folder would never reach the item it
	// made (homeexchange.go's [app.fileEveryExchange]).
	a.fileEveryExchange()
	return tea.Quit
}

// interrupt is esc: stop the turn, keep what it said.
//
// EVERYTHING A STOP OWES THE SCREEN IS PAID AT THE KEY, and that is the whole of
// what this function changed when the interruption wave went through it. The
// acts below were all already performed — every one of them is [app.settle]'s,
// done when the stream finally closed — so the only thing that is different is
// WHEN, and "when" is the entire subject. The engine's teardown is not
// instantaneous and is not bounded (see [app.windingDown]); a surface that
// waited for it left tool rows spinning, questions standing and a reply
// arriving for seconds after a person had stopped the turn, which is the screen
// disagreeing with the one fact the person is certain of — they pressed the key.
func (a *app) interrupt() {
	a.interruptTurn()
	// ESC STOPS EVERYTHING, including both ways a later turn can already be
	// waiting. The session drops its follow-up queue on interrupt; the surface
	// drops that mirror and its editable parked queue in the same keypress so the
	// stream close cannot orphan or unexpectedly send either one.
	a.dropFollows()
	a.dropParked()
}

// interruptForBarge stops the current turn but preserves the draft
// [app.bargeIn] just parked. shift+enter promises stop-and-send; it shares the
// stop machinery with esc without sharing esc's queue-clearing decision.
func (a *app) interruptForBarge() {
	a.interruptTurn()
	a.dropFollows()
}

func (a *app) interruptTurn() {
	// THE AGENT IS ASKED FOR RATHER THAN ASSUMED, on [app.quit]'s own terms: a
	// surface can be standing with no session under it, and a stop that panicked
	// on the way to stopping nothing would be the worst possible answer to the
	// key a person presses when they want something to stop.
	if a.state != stateWorking || a.agent == nil {
		return
	}
	a.agent.Interrupt()
	a.state = stateInterrupted
	// AND THE STOP IS BOUNDED FROM THIS INSTANT. See the block below
	// [app.windingDown]: the letting-go is the engine's and it takes as long as
	// it takes, so the person's stop is given a deadline of its own and the
	// surface says what it is while it runs down. It is measured from the KEY and
	// not from the last event, because the key is the moment the person is
	// certain of and the only one they are timing from.
	a.stopBy = a.now().Add(stopGrace)
	// THE ROWS STOP AT THE KEY. Leaving the state word was already enough to
	// still the spinners and the count-ups — both renderers stand down outside
	// stateWorking (toolview.go's [app.mark] and [app.countClock]) — but only for
	// as long as nothing else starts working, and only as a consequence of the
	// state rather than as a fact about the call. The end stamp is the fact, and
	// stamping it here rather than at the close says the true thing about each
	// row: this call ran until the person stopped it. Both are idempotent, so
	// [app.settle] repeating them a few seconds later changes nothing.
	a.dropForming()
	a.resolveUnfinished()
	// AND THE QUESTIONS GO WITH THE TURN THEY WERE ASKED INSIDE. The engine's
	// own cancellation releases a parked call and it refuses (session's
	// consent.go), so a block still on screen is asking about work that is over
	// — and until the stream closed the status line read "waiting · your call"
	// over a turn the person had already stopped. These are settle's three drops,
	// on settle's reasons, moved to the moment the answer stopped mattering.
	a.dropAsks()
	a.dropConnectAsks()
	a.dropHarnessAsks()
	// AND THE LINE IN THE CONVERSATION SAYS IT IN THE SAME WORD THE REST OF THE
	// SCREEN SAYS IT IN. This note read `interrupted` for as long as that was the
	// only word the surface had for the act, and after two waves it was the third
	// one: the status line says `stopping` while the engine lets go (render.go's
	// [stoppingWord]) and the chip that stands in for the stopped turn says
	// `stopped by you` two rows above this note (workfold.go's
	// [app.workfoldLabel]). One keypress narrated in three vocabularies on one
	// screen reads as three things that happened, and the redundant pair — a chip
	// and a note about the same stop, drawn together in fold mode — is where it
	// showed worst. `stopped` is the past tense of the word the other two use and
	// it is the person's own: they stopped it.
	//
	// The STATUS WORD is deliberately left as `interrupted`. That slot is a
	// documented two-rung ladder of its own — `stopping` while the turn is being
	// let go, `interrupted` once it is gone (screen.md states both) — and it is
	// the state the session is IN rather than a line about what happened, which is
	// what this note is.
	a.note("stopped")
	// AND THIS TURN PROMOTES NOTHING (hierarchy.go's [app.cutTurn]). The mark goes
	// on the blocks at the keypress so the demotion is on screen the moment the
	// person presses esc, and again when the stream finally closes ([app.settle]),
	// because events in flight land between the two.
	a.cutTurn(a.turn)
}

// ── THE SECOND STAGE, AND WHY IT IS A CLOCK RATHER THAN A KEY ───────────────
//
// The window above used to be UNBOUNDED, and this block used to argue that it
// had to be. The argument was sound and it is kept below, because both halves of
// it are still true and the second one is what dictated how this was built. What
// changed is that the thing it said did not exist now does.
//
// WHAT THE ARGUMENT GOT RIGHT, FIRST HALF: THERE IS NO KEY LEFT. esc's grammar
// in the conversation is read in a fixed order (input.go, rewind.go): a recall
// walk takes it, then [app.escRewind] — where the first esc ARMS the rewind on
// its way past and a second one inside [rewindArmWindow] OPENS it — and only
// then [app.interrupt]. So every esc that lands within half a second of another
// esc already belongs to rewind, and THE INTERRUPT IS NOT FOR SALE cuts the
// other way just as hard. Putting a hard stop AFTER the window does not save it
// either, because an esc past the window is a FIRST esc again, so the key would
// mean "stop harder" or "open the rewind" depending on what the person did half
// a second later — one keypress with two readings, which is the one thing this
// keyboard cannot have. ctrl+c is spoken for on both sides of the same moment:
// mid-turn it is the interrupt, and at rest — which is what winding down IS —
// it is the quit arm (quitarm.go).
//
// THAT REMAINS TRUE, SO THE SECOND STAGE TAKES NO KEY AT ALL. It is a CLOCK,
// started by the esc the person already pressed, and it needs no grammar because
// it asks for no gesture. A person who wants a turn to stop has said so once;
// making them say it twice, harder, into a surface that already heard them is
// the exact experience issue #265 was filed about.
//
// WHAT THE ARGUMENT GOT RIGHT, SECOND HALF, AND WHY IT DICTATED THE ORDER OF
// WORK: A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. A deadline whose
// only act was to look away would be the worst possible thing to put under a
// person's stop — a surface saying "detached" over a goroutine that keeps
// spending. So the door was built from the bottom up before this clock was
// wired to anything, in the order the old block itself named:
//
//   - the jobs registry's SIGTERM and SIGKILL graces take the call's context
//     (session's jobs.go, [waitDoneUnder]);
//   - bash's wait has a cancellation arm that hands back what the command had
//     already written and lets the reaper finish behind it (internal/exec/bare);
//   - the tool batch's `wg.Wait()` has a second stage of its own, cut by an
//     abandon signal rather than by the turn's context, so an ordinary stop
//     still gives every tool the seconds it needs to hand back what it did
//     (session's loop.go and abandon.go's [waitBatch]);
//   - and [session.Agent.Abandon] is the door onto all of them, which also cuts
//     the turn's context a second time — aborting whatever HTTP is in flight,
//     since every provider request is built on it — closes the turn's hub so
//     this surface is genuinely free, and writes ONE journal line marking the
//     turn abandoned with its last known spend.
//
// THE JOURNAL LINE IS THE POINT AND NOT THE DECORATION. A turn nobody waits for
// is a turn that never writes the seal carrying its cost, so without that line a
// turn let go of at the bound would be money spent with no record that anything
// had been let go of. The money itself already reaches the machine's ledger per
// call rather than per turn (session's usage_ledger.go), so the bill is not lost
// — what would have been lost is the fact.
//
// WHAT A PERSON READS while the clock runs is [app.stoppingSegment]: the status
// line says `stopping` as it always did, and beside it, how long until the
// surface detaches. A silent countdown is not a stop anybody can trust, so the
// bound is on the screen before it fires and never only after.

// stopGrace is HOW LONG A STOP WAITS FOR THE ENGINE before the surface lets go
// of the turn without it ([app.stopSweep]).
//
// TEN SECONDS, AND THE FIGURE IS ARGUED RATHER THAN PICKED. It is NOT argued
// from bash's three-second `WaitDelay` or the `jobs` kill's two-plus-two second
// graces any more: this change put the call's context on both of those, so a
// stop ends them at once rather than waiting them out. What is left behind the
// bound is the class this change cannot reach from outside — a wait that never
// looks at its context at all, a command whose output a grandchild still holds,
// anything a tool blocks on that was written before cancellation existed. Ten
// seconds is chosen to sit far above every settle we can measure, so the
// deadline fires only on that class and never on a turn that was about to end
// tidily and hand back what it did.
//
// IT IS MEASURED FROM THE KEYPRESS and not from the last event, because the key
// is the only moment the person is timing from.
const stopGrace = 10 * time.Second

// windingDown reports that the turn on screen was STOPPED BY HAND and its stream
// has not closed yet: the seconds between a person's esc and the engine letting
// go of the turn.
//
// IT IS A REAL WINDOW, THOUGH IT IS NO LONGER A LONG ONE FOR ORDINARY WORK.
// [session.Agent.Interrupt] cancels the turn's context and returns at once, and
// since this change the tool batch's wait and the jobs graces take that context
// too, so the ordinary settle is now the time a cancelled call needs to unwind.
// The window survives because the turn goroutine does not close its
// event hub until [session.Agent]'s loop returns, and the loop cannot look at
// the context until the tool batch it is inside has finished. What can still
// outlast the cancel is the context-blind class — a wait that never looks at the
// context it was given — and that is what the bound is for. [stopGrace] above
// carries the argument in full; it is not repeated here, so that moving the
// reasoning cannot leave two versions of it disagreeing.
//
// It is DERIVED and not stored, from the two facts that already exist: the state
// word is only [stateInterrupted] because [app.interrupt] put it there, and the
// stream is only non-nil between a turn opening and its close (app.go's Update).
// A third field holding the same fact is a third thing to keep in step with the
// two.
func (a *app) windingDown() bool { return a.state == stateInterrupted && a.stream != nil }

// abandonAgent is the second stage of a stop as this surface reaches it
// (session's abandon.go). It is an optional assertion on the agent for
// [stopAgent]'s reason exactly: a surface driven by something that has never
// heard of abandoning a turn keeps every other thing it had, and says nothing
// about a bound it cannot enforce.
type abandonAgent interface {
	// Abandon lets go of the in-flight turn and reports what it had spent. It
	// answers false when there was nothing to let go of.
	Abandon(reason session.AbandonReason) (session.Usage, bool)
}

// abandonDoor is the abandoning half of the agent under this surface, when it
// has one.
func (a *app) abandonDoor() (abandonAgent, bool) {
	if a.agent == nil {
		return nil, false
	}
	door, ok := a.agent.(abandonAgent)
	return door, ok
}

// stopBounded reports whether the stop on screen has a deadline a person can
// read and this surface can actually keep.
//
// BOTH HALVES ARE REQUIRED, and the second is the design law rather than a
// guard: a countdown drawn over an agent with no door behind it would be the
// surface promising something it has no way to do, which is the one thing a stop
// may never be.
func (a *app) stopBounded() bool {
	if !a.windingDown() || a.stopBy.IsZero() {
		return false
	}
	_, ok := a.abandonDoor()
	return ok
}

// stopLeft is how long the winding-down window has to run, rounded UP to the
// second and never below one.
//
// IT ROUNDS UP because this figure is a promise about the future and the person
// reads it as one: a window with 400ms left that said "0s" would be a countdown
// that reaches zero and then keeps standing, which is the same broken promise as
// the unbounded window in miniature. It reaches zero exactly once — when the
// sweep has already fired — and by then nothing is drawing it.
func (a *app) stopLeft() time.Duration {
	left := a.stopBy.Sub(a.now())
	if left <= 0 {
		return 0
	}
	return (left + time.Second - 1).Truncate(time.Second)
}

// stoppingSegment is the status line while a stop is being let go of: the word,
// and — while the bound is real — when the surface will detach.
//
//	stopping                      no door behind the bound: the word alone
//	stopping · detaching in 7s    the bound, on screen before it fires
//
// IT IS DIM THROUGHOUT, keeping [stoppingWord]'s own reasoning: winding down is
// the quietest thing this surface does, nothing is wrong and nothing is wanted.
// The countdown is dimmer still rather than accented, because it is not a thing
// to act on — there is no key it is asking for — it is the surface stating its
// own bound so the person can stop wondering whether the key landed.
func (a *app) stoppingSegment() (string, string) {
	if !a.stopBounded() {
		return stoppingWord, a.pal.dim(stoppingWord)
	}
	left := stopDetachWord + itoa(int(a.stopLeft()/time.Second)) + "s"
	return stoppingWord + " · " + left, a.pal.dim(stoppingWord + " · " + left)
}

// stopDetachWord is the promise the countdown is counting down to, in the word
// the note at the deadline uses for the same act. One act, one name.
const stopDetachWord = "detaching in "

// stopSweep is the deadline firing: the window ran out and the turn is let go of
// without the engine's agreement. It runs from [app.paint], on the clock that is
// already turning, exactly as the rewind arm and the quit arm run down there —
// a bound this short is not worth a timer of its own, and a bound driven by the
// frame is a bound that cannot outlive the surface drawing it.
//
// NOTHING HAPPENS WHILE THE STOP IS ORDINARY. This is a clock read and a zero
// check on every frame of a winding-down turn, and nothing at all on every other
// frame this surface ever draws.
func (a *app) stopSweep() tea.Cmd {
	if !a.stopBounded() || a.now().Before(a.stopBy) {
		return nil
	}
	return a.detachTurn()
}

// detachTurn is the whole of what the deadline does, and the order of it is the
// argument.
//
// THE ENGINE IS ASKED FIRST. Freeing the screen before the waits had been ended
// and the request aborted would be this surface looking away from a turn that
// was still running and still spending, which is the failure the second stage
// exists to prevent rather than to perform. The door ends the waits, cuts the
// request, and writes the one line that says the turn was let go of with what it
// had spent (session's abandon.go).
//
// THEN THE SURFACE LETS GO OF THE STREAM, by the same two acts every other
// letting-go on this surface uses (detach.go): the channel is dropped and the
// generation is bumped, so an event already in flight on it discards itself
// rather than landing in a conversation that has moved on. The turn then settles
// exactly as an ordinary stopped turn settles — the same sweep, the same marks,
// the same receipt — because from here on it IS one.
func (a *app) detachTurn() tea.Cmd {
	door, ok := a.abandonDoor()
	if !ok {
		return nil
	}
	spend, letGo := door.Abandon(session.AbandonStopTimeout)
	a.stopBy = time.Time{}
	if !letGo {
		// THERE WAS NOTHING LEFT TO DETACH. The engine let go between the last
		// frame and this one, so the stream's own close is already on its way and
		// it settles the turn as an ordinary stop. The surface stands down here
		// rather than freeing anything: a note saying "detached" over a turn that
		// ended by itself would be this surface claiming an act it did not
		// perform, and dropping a stream that is about to close cleanly would
		// throw away the turn's own last events for nothing.
		return nil
	}
	a.stream = nil
	a.gen++
	// AND THE PERSON IS TOLD, in the conversation, in the words the countdown
	// they were reading promised. A surface that detached silently would have
	// spent ten seconds announcing a bound and then said nothing when it fired,
	// which is a promise kept invisibly and therefore not kept.
	a.note(stopDetachedNote(spend))
	return a.settle()
}

// stopDetachedNote is the line the conversation keeps about a detached turn.
//
// IT NAMES THE MONEY WHEN THERE IS ANY, because what a person wants to know
// about work nobody waited for is what it cost them, and it is the same figure
// the journal line carries. It says nothing about a turn that spent nothing,
// which is the emptiness law — a `$0.00` here would be the surface reporting a
// measurement where it has only an absence.
func stopDetachedNote(spend session.Usage) string {
	if spend.CostUSD <= 0 {
		return stopDetachedWord
	}
	return stopDetachedWord + " — it spent " + spendMoneyWord(spend.CostUSD)
}

// stopDetachedWord is what a detached turn is called, once, wherever it is
// named. It is the person's own reading of what happened: they stopped it, the
// engine did not let go inside the window, and the surface stopped waiting.
const stopDetachedWord = "detached — the turn was let go of and nothing is waiting for it"

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
	if a.pasteEdit.open {
		before := a.pasteEdit.box.String()
		a.pasteEdit.box.insert(text)
		for i := range a.pastes {
			if a.pastes[i].n == a.pasteEdit.n {
				a.pastes[i].text = a.pasteEdit.box.String()
				a.rewritePasteToken(a.pastes[i].n, before, a.pastes[i].text)
				break
			}
		}
		a.touch()
		return nil
	}
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
	if a.at(pageMemory) {
		flat := strings.ReplaceAll(text, "\n", " ")
		if a.mem.edit != nil {
			a.mem.edit.insert(flat)
		} else {
			a.mem.filter.insert(flat)
			a.mem.rank()
		}
		a.touch()
		return nil
	}
	// The settings sheet owns the clipboard while it is up, the same way it
	// owns the keyboard: into the text row being answered first (an API key
	// is the paste this path exists for), into the select row's filter next,
	// into the search box otherwise. All three are one-line boxes — newlines
	// flatten to spaces.
	if a.at(pageSettings) {
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
	// THE HOME SCREEN IS FULLSCREEN, so while it is up the chat's own draft is
	// not on the page at all — and this is where a paste used to go anyway,
	// which read as the paste doing nothing: the text sat in a box nobody could
	// see until home was closed. It goes into the box the caret is actually in
	// — the exchange pane's while that holds the keyboard, home's own otherwise
	// — and home's list re-filters exactly as it does for a typed character.
	//
	// AND THE DROP DOOR IS ASKED FIRST, BY BOTH OF THOSE BOXES. It used to be
	// reached only on the fall-through below, which is the conversation's draft
	// — so a screenshot dragged onto home became the raw escaped path it arrived
	// as, while the same gesture one screen away became a picture on the tray.
	// One door, told which box it is writing into (imagepaste.go's
	// [app.pasteFilesInto]); when it says the text was not files, the text goes
	// in exactly as it always did.
	if a.at(pageHome) {
		// WHICH OF THE TWO BOXES THAT IS, IS ASKED ONCE AND IN ONE PLACE
		// (imagepaste.go's [app.keyboardBox]), because the keystroke fold has to
		// ask the same question of the same keyboard and get the same answer.
		box, chips := a.keyboardBox()
		if !a.pasteFilesInto(box, chips, text) {
			box.insert(text)
		}
		a.dropLanded(box)
		a.touch()
		return nil
	}
	// A DROPPED PICTURE IS A PICTURE. A terminal writes a drag-and-drop into the
	// clipboard as the file's PATH, and a paste that is nothing but paths to
	// pictures attaches them and leaves `[image #1]` in the sentence instead —
	// so the model gets the pixels rather than a string it has to guess about
	// (imagepaste.go). Anything else falls through and is inserted as the text
	// it plainly is.
	before := a.input.String()
	if !a.pasteFiles(text) {
		if !a.pasteText(text) {
			at := a.input.cursor
			a.input.insert(text)
			a.editTags(at, at, len([]rune(text)))
		}
	}
	// A PASTE INTO THE CONVERSATION BOX IS THE SAME REACH FOR THE KEYBOARD AS
	// a typed rune. Compare the box rather than the clipboard so settings, home,
	// key boxes, refused drops, and other overlays do not hold a proposal they
	// never edited; folded pastes and image tokens do because they changed it.
	if a.input.String() != before {
		a.holdTask()
	}
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
	// The harness picker answers for itself, because the keys it takes are its
	// own: the browse row is a commit that opens a panel rather than one that
	// writes a chip, and enter on a filter that matched nothing leaves the typed
	// line alone (harnesspick.go).
	if a.harnPick.open {
		if cmd, taken := a.harnessPickKey(msg); taken {
			return cmd, true
		}
	}
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
		// It SEALS the word it was pressed over, so the list does not reappear
		// on the next letter of it ([app.dismissLists]).
		a.dismissLists()
		a.touch()
		return nil, true

	case "enter":
		if a.menu.open {
			// A COMPLETE LIVE TAG OWNS ENTER, even while the spelling list is
			// still visible under it. Choosing the row merely rewrote the word in
			// the old mention doctrine; the chip now promises this send instead.
			if len(a.liveTags()) > 0 || len(a.input.demotedTags) > 0 {
				a.menu.close()
				return a.enter(), true
			}
			if _, ok := a.menu.choice(); !ok {
				// Nothing matched what was typed. The line is still a line, and
				// enter is still submit — /nonsense gets its answer.
				a.menu.close()
				return nil, false
			}
			if a.startingChat() {
				// AND ON THE NEW-CHAT START PAGE THE ROW GOES THROUGH THE PAGE
				// (chatstart.go). [app.runMenu] dispatches straight into
				// [app.slash], which acts on the conversation in front — and on
				// that page the conversation in front is the one the page is
				// drawn over and HIDING. /quit closed it, /clear replaced it and
				// /model retargeted it, all from a page whose whole promise is
				// that it touches nothing until a first message is sent.
				return a.startMenuEnter(), true
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
// the other way round.
//
// AT MOST ONE IS OPEN, and what decides which is now the CARET rather than the
// first character of the line. Both typed lists answer the same question — the
// word the caret is standing in, and what it opens with — so a caret in a "/"
// word is the command list's and a caret in an "@" word is the completion's, and
// no draft can put the caret in both at once (slashchip.go's [slashToken],
// files.go's [atToken]).
func (a *app) syncLists() tea.Cmd {
	wasOpen := a.menu.open
	a.menu.sync(&a.input)
	if a.menu.open && !wasOpen {
		// The list coming up is the proof that "/" has been found (notice.go).
		a.noticeEvent(eventMenuOpened)
	}
	if a.menu.open {
		a.comp.close()
		a.harnPick.close()
		return nil
	}
	// AND THE HARNESS PICKER IS THE THIRD OF THEM, asked after the command list
	// and before the completion for the reason the command list closes at all: a
	// space ends the choosing of a command and begins its argument, and for this
	// one command the argument has a list of its own (harnesspick.go).
	if a.syncHarnessPick() {
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
	a.harnPick.close()
}

// dismissLists is esc over a typed list, which is [app.closeLists] plus the one
// thing esc means that a close does not: the person MEANT the word they are
// typing. Without the seal the list is back on the next keystroke — the overlays
// are derived from the draft, so closing one over a word that still matches is a
// dismissal that lasts exactly until the next letter — and a slash word inside a
// sentence would be uncloseable. See [menu.dismiss].
func (a *app) dismissLists() {
	sealed, at := a.menu.open, a.menu.at
	a.closeLists()
	if sealed {
		a.menu.dismiss(at)
	}
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
	// AND THE TREE'S TOTAL, which is a fact about one conversation exactly as
	// a.cost above it is: it is what THIS conversation and the work it started
	// have spent (treespend.go). It was left standing here until #210, and
	// because the ledger is re-read only on the frame clock while the task
	// column is up, the figure of the conversation being LEFT stayed on the row
	// of the one being taken up — a switch onto a conversation holding $1.11
	// read $50.00 until something happened to recompute it, and /cost printed
	// the true figure while the row beside it printed the other one, which is
	// the surface disagreeing with itself about the only number on it a person
	// acts on.
	a.tree = session.Receipt{}
	// THE CACHE UNDER IT IS DELIBERATELY KEPT. It is not a figure about a
	// conversation but a parse of a file — [session.UsageCache] holds the lines
	// it has already read off the machine's one ledger and re-derives the tree
	// from them per conversation, so nothing in it is about the conversation
	// being left. Dropping it would make the next reading a cold walk of the
	// whole ledger on the switch frame: measured at 610ms for a ledger at the
	// cache's own 200,000-line ceiling, against 17µs for the tail read that
	// keeping it buys. That stall would land on precisely the frame this reset
	// exists to make right.
	a.ctxTokens = 0
	a.shownCost, a.shownTokens, a.shownCtx = 0, 0, 0
	a.meterChasing = false
	a.revealMoved = time.Time{}
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
	a.burnShown, a.burnAt = "", time.Time{}
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
	// AND A WEIGHT THAT MOVED WHILE THE TURN RUNS IS WALKED, from the reading
	// that is on the screen right now. This is the only place the weight ever
	// changes, so it is the only place that can arm the walk for it — and
	// [app.take]'s arming cannot do it, because a turn's usage lands long before
	// the pass that changes what the conversation weighs. The compaction call
	// site is the one this is really for: it changes the meter by an order of
	// magnitude in the middle of a turn (see [app.compacted]). At the settle the
	// turn is no longer running, so [app.armMeters] declines and the exact figure
	// is drawn — which is the snap rule, not an exception to it.
	was := a.ctxTokens
	a.ctxTokens = a.agent.ContextTokens()
	switch {
	case a.ctxTokens > was:
		// A WEIGHT THAT GREW IS TELEMETRY AND WALKS, from the reading that is on
		// the screen right now. This is the only place the weight ever changes,
		// so it is the only place that can arm the walk for it: [app.take]'s
		// arming cannot, because a turn's usage lands long before the pass that
		// changes what the conversation weighs.
		a.armMeters(a.spendShown(), a.tokens, was)
	case a.ctxTokens < was:
		// A WEIGHT THAT FELL IS AN EVENT, AND THE EVENT IS THE POINT. Only a
		// compaction takes weight off a conversation, and the whole reason this
		// is re-read there rather than at the end of the turn is that the figure
		// must say so AT ONCE — a meter easing down from 168k over a third of a
		// second is a meter animating the one fact a person is waiting to see
		// ([TestCompactionRereadsTheContextMeterImmediately]). It is written past
		// any walk already in flight rather than through [app.armMeters], because
		// a chase the usage started is holding the old figure and would go on
		// drawing it.
		a.shownCtx = a.ctxTokens
	}
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
	threshold := session.CompactThresholdFor(a.model, a.ctxWindow)
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
//
// THE BOOL IS ABOUT THE PROMPT PRICE ONLY. A caller working out what a cache
// read SAVED needs the pair and must check CacheReadPrice itself, at its own
// site: a row with a prompt price and no cache-read price is common and true is
// the right answer here, because the prompt price it publishes is real.
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
//
// BOTH PRICES OR NO MONEY, and the second half of that guard is not decoration.
// Around two rows in five publish a prompt price and no cache-read price at all
// (internal/catalog's CacheReadPrice: "zero is the provider did not say", and a
// cache read is never free) — and treating that absence as a zero books the
// WHOLE prompt price as a saving, which is this surface claiming the cache made
// those tokens free. The token count alone is what it actually knows.
func (a *app) cacheNote(u session.Usage) {
	if u.CacheRead <= 0 {
		return
	}
	line := "⟲ " + tokenWord(u.CacheRead) + " cached"
	if model, known := a.priceFor(a.model); known && model.CacheReadPrice > 0 {
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
			adds, dels, _ := editStat(e.detail.Args)
			add(argString(fields, "path"), adds, dels)
		case "write":
			content, _ := argBody(argString(fields, "content"))
			add(argString(fields, "path"), lineCount(content), 0)
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
	var walk statWalk
	for i := range a.entries {
		walk.fold(&a.entries[i])
	}
	// AND WHAT THE SESSION'S NODES STARTED, which is the other half of the same
	// sentence (docs/design/lens/DESIGN.md, Decision 4). A node runs `bash` with
	// background:true exactly as the conversation does, on this machine, out of
	// this session — and until this landed the Σ segment said nothing about it
	// and the quit guard let a person walk away from three servers a task had
	// started ([app.quitArmed]).
	//
	// IT IS A TALLY AND NOT A SECOND WALK, and that is the whole of why the
	// numbers do not flicker. A room's entries live only while its page is open
	// ([app.closeRoom] drops them), so a walk over them would have counted a
	// node's jobs on the frames somebody was LOOKING at that node and not on the
	// others — a count that changes because you opened a page is a count nobody
	// can act on. The tally is folded once, by the room's reducer, at the instant
	// a call closes ([app.roomFeedHooks]), and it stays folded.
	out := walk.out
	for _, node := range a.nodeHud {
		out.jobs += node.jobs
		out.watches += node.watches
		out.adds += node.adds
		out.dels += node.dels
	}
	return out
}

// callClosed reports whether this row is A CALL THAT FINISHED, either way — the
// one event on this surface that can move a count.
//
// IT IS ONE FUNCTION BECAUSE TWO COUNTS ASK IT. The ambient sums walk it here
// ([statWalk.fold]) and a room's header counts calls with it (room.go's
// [roomWorkOf]), and a header that said `14 tool calls` beside a Σ segment
// summing a different fourteen would be the surface keeping two clocks about
// one fact (docs/design/lens/DESIGN.md, Decision 4).
func callClosed(e *entry) bool {
	return e.kind == entryTool && (e.status == toolOK || e.status == toolFailed)
}

// statWalk is the ambient counts being summed, and the state that sum carries
// between entries. It is a type rather than a loop body because TWO CALLERS FOLD
// THE SAME ARITHMETIC: the conversation walks its whole list on demand
// ([app.computeStats]), and a node's page folds one call at a time as its
// reducer closes it ([app.tallyNode]) — and two spellings of "what a finished
// call adds to the counts" is two spellings that drift.
type statWalk struct {
	out hudStats
	// live holds the ids of the background jobs this walk watched start, so a
	// kill can take away the one it names rather than the newest.
	live []string
}

// fold adds one entry to the counts, and ignores everything that is not a call
// that finished cleanly.
func (w *statWalk) fold(e *entry) {
	if !callClosed(e) {
		return
	}
	// AND ONLY A CALL THAT WORKED CHANGED ANYTHING. A failed edit wrote no lines
	// and a failed `bash` started no process, so a finished call still has to
	// have succeeded before it moves a count — which is the one place these sums
	// narrow what [callClosed] admits, and it is narrower on purpose rather than
	// by a second definition of "finished".
	if e.status != toolOK {
		return
	}
	fields := argsOf(e.detail.Args)
	switch e.tool {
	case "edit":
		adds, dels, _ := editStat(e.detail.Args)
		w.out.adds, w.out.dels = w.out.adds+adds, w.out.dels+dels
	case "write":
		content, _ := argBody(argString(fields, "content"))
		w.out.adds += lineCount(content)
	case "bash":
		if argString(fields, "background") != "true" {
			return
		}
		w.out.jobs++
		w.live = append(w.live, jobID(e.detail.Output))
	case "watch":
		w.out.watches++
	case "jobs":
		if argString(fields, "action") != "kill" {
			return
		}
		w.kill(argString(fields, "id"))
	}
}

// kill takes one job away from the counts, by name where this walk saw it start.
func (w *statWalk) kill(id string) {
	if at := indexOf(w.live, id); id != "" && at >= 0 {
		w.live = append(w.live[:at], w.live[at+1:]...)
		w.out.jobs--
		return
	}
	// An id this surface never saw start is a watch's — watches are jobs too
	// (kind watch) and their start line publishes no id — and failing that it is
	// a job from before we were looking.
	if w.out.watches > 0 {
		w.out.watches--
		return
	}
	if w.out.jobs > 0 {
		w.out.jobs--
	}
}

// tallyNode works out what ONE node has added to the ambient counts, from the
// rows its page is holding, and remembers the answer.
//
// IT RE-COUNTS RATHER THAN ADDING ONE CALL AT A TIME, and that is what makes it
// safe to call from two places. A room learns its history two ways — the journal
// it replays when the page opens ([app.roomRecord]) and its lane while the page
// is up — and only the second goes through the reducer. A tally that folded each
// closed call as it arrived therefore missed every job the node had already
// started before anybody looked, which is most of them: the first thing a person
// does about a task is open it AFTER it has been working. Counting the whole list
// instead answers for both halves, and re-answering is idempotent — opening the
// same page twice cannot count the same job twice, which an accumulator could
// not promise.
//
// WHAT IT STILL CANNOT SEE, said plainly: a node whose page nobody has ever
// opened. Its journal is on disk and this surface has not read it, so its jobs
// are not in the count. That is the same honesty [hudStats] already states about
// its own numbers — the count is what this session has SEEN — and it is the
// right direction to be wrong in: a job that turns up when you open the page is
// a job you learn about, where a count that guessed at unread journals would be
// a number nobody could check.
func (a *app) tallyNode(id uint64, es []entry) {
	var walk statWalk
	for i := range es {
		walk.fold(&es[i])
	}
	if a.nodeHud == nil {
		a.nodeHud = map[uint64]hudStats{}
	}
	if was, ok := a.nodeHud[id]; ok && was == walk.out {
		return
	}
	a.nodeHud[id] = walk.out
	// THE COUNTS THE SURFACE IS SHOWING ARE NOW OLD, and this is the one place a
	// node's page can say so: the cache is the conversation's and nothing else
	// drops it on a task's event (see [app.hudStats]).
	a.hudStale = true
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

// approvalPosture is the gate's posture as the YOLO segment may state it: an
// answer the launch had to hand down, or the profile's own answer read live.
//
// THIS IS A SAFETY CLAIM AND IT MUST MATCH THE POSTURE IN FORCE. The segment is
// drawn only when the gate is open (render.go's NEGATIVE-SPACE SAFETY), so its
// ABSENCE is the claim that every tool call will be asked about — and the gate
// it is claiming about is the one cmd/aforge built from
// [config.ToolApprovalModeAt] on the very same profile directory (chatv3.go's
// v3Policy). The two must be one reading, because a segment that is quiet over
// an open gate is the surface telling somebody they will be asked before their
// disk is written to, and then not asking.
//
// IT WAS NOT ONE READING. This resolved through a helper that answered "" on an
// empty [app.profileDir], on the reasoning that a surface booted without a
// profile has not been told the gate is open. But AN EMPTY PROFILE DIRECTORY IS
// THE NORMAL CASE, NOT THE ABSENT CASE: AFORGE_PROFILE_DIR is the rare export,
// the empty string has always meant this process's own profile in the state
// root ([config.ProfilePath]), and the policy the tools actually ran under read
// that profile. So a person who had turned the asking off — the one posture
// this segment exists to remind them of — was shown NOTHING on every ordinary
// launch while their gate stood open (#322). The profile is read here the way
// every other persisted row on this surface is read.
//
// THE PROFILE ROW IS NOT THE ONLY THING THAT OPENS THIS GATE. `aforge chat
// --yolo` replaces the gate's default for the session and writes nothing down
// (cmd/aforge's v3Policy), so a surface that read only the profile drew nothing
// over a gate that was open for the whole run (#325). The launch hands that
// posture down instead, and a launch that hands nothing down is read from the
// profile, live, as before. Locally the handed-down answer can ONLY ever be
// `allow`: a posture handed down that silenced the segment would make the same
// false claim by the other route.
//
// THE HOSTED WINDOW IS STILL THE ONE ABSENCE. This machine's profile is not the
// session's posture over --host: the gate that decides whether a tool runs
// without asking is the ENGINE's, read from the profile on the engine's
// machine. A YOLO badge drawn from this laptop's settings would be a safety
// claim about a machine nobody consulted, so a remote session reads
// [app.handedApproval] — the same answer, asked of the right machine
// (internal/remote's wire.go Welcome.ApprovalMode, set once at boot rather than
// re-read live, because there is nothing on this side left to re-read), and an
// engine that carried none leaves the segment absent.
func (a *app) approvalPosture() string {
	if a.hosted() {
		return a.handedApproval
	}
	if a.handedApproval != "" {
		return a.handedApproval
	}
	return config.ToolApprovalModeAt(a.profileDir)
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
//
// A POSITIVE AMOUNT NEVER DRAWS AS ZEROS. Under a cent this reads
// settingspend.go's [subCent], which is the same rule a limit is written by —
// four places, and a floor under them so a cost too small for four places says
// `<$0.0001` rather than `$0.0000`. `$0.00` above stays: it is this line's one
// sanctioned zero and it means nothing has been spent.
func dollars(usd float64) string {
	switch {
	case usd <= 0:
		return "$0.00"
	case usd < 0.01:
		return subCent(usd)
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
