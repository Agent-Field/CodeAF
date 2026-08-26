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

	// steers are the corrections typed INTO this turn after this block opened it
	// — the elbow rows drawn under the question (steerelbow.go). They are here
	// rather than in the turn's own run of blocks because THE QUESTION IS WHAT
	// THEY BELONG TO: a turn folded to its `worked` chip still reads back as
	// everything that was asked, and a rewind that drops a turn drops its
	// corrections with it because they are the same block.
	//
	// Empty on every block of every conversation nobody steered, which is nearly
	// all of them, and a block with none renders exactly as it did before this
	// existed.
	steers []steerElbow
	// steerFoldRow is which of this block's rows is the elbows' fold line, so the
	// layout pass can make that one row a door — and ZERO IS NONE, which costs
	// nothing to say: row zero is always the first row of the person's own
	// sentence, so a fold line can never land there. It is written by the render
	// that drew it, for the reason a proposal's choice row is (task.go): the row a
	// thing lands on is decided by the wrap, and a hit-test that recomputed it
	// would be measuring a row the frame has not drawn.
	steerFoldRow int

	// Tool fields.
	tool   string
	status toolState
	detail toolDetail
	open   bool // this call's expansion is showing inline
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
	// IT IS SET WHERE THE FACT IS KNOWN (room.go's [readRoomJournalTail]) and
	// never worked out at render time, because [app.renderEntry] paints one block
	// at a time and must not ask what surrounds it — the same law the answer
	// hierarchy is stamped under ([app.deckRows]).
	brief bool
	// decision is what the person answered when this call was asked about —
	// "allowed" or "denied", dim, beside the row's stat (consent.go). It is
	// empty for every call the policy did not stop.
	decision string
	// bg is set when the person sent this running call to the background with
	// ctrl+g (background.go): `job 3`, dim, beside the row's stat, in the slot
	// [entry.decision] already uses because it is the same kind of fact. It is
	// empty for every call nobody promoted.
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
	// start and open are the agent-building seam (tui3.go's [Conversation]):
	// they hand back the agent AND the closures minted around it, so /new and a
	// resume rebind the approval trio, the recent list, the draft and the recall
	// list in the same breath as the agent. They are preferred over fresh and
	// resume wherever both are wired; the pair below is what a door that cannot
	// answer the seam still gets ([app.nextConversation], [app.openConversation]).
	start func(workspace string) (Conversation, error)
	open  func(workspace, transcript string) (Conversation, error)
	// workspace is the directory this conversation is about, whole; place is
	// its base name, which is what the status line has room for. The whole path
	// is what history is keyed by and what the @ completion walks.
	workspace string
	place     string
	file      string
	resumed   bool
	// previews holds the pictures this surface has already drawn as half blocks
	// (imagepreview.go), keyed by the file, its mtime and the shape it was drawn
	// for. An open picture call is re-rendered on every frame, and decoding a
	// megapixel png ten times a second is the one thing this surface must not do.
	previews map[string]imagePreview

	entries []entry
	// pendingReplyTags arrived before the first words of the answer they label.
	pendingReplyTags []session.TaskReplyTag
	// live is the assistant entry currently being streamed into, or -1.
	live int
	// echoAt is the person's own line drawn before the engine agreed to it, or
	// -1 when there is none — which is always, on a surface that is not hosted.
	// echoTok is the token that names it, counting from one so that zero means
	// "settles nothing" (echo.go).
	echoAt  int
	echoTok uint64
	// turn counts the person's messages. It groups tool calls into clusters
	// and is what ctrl+o folds and unfolds.
	turn int
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
	// unfolded holds the turns whose tool cluster is showing every call.
	unfolded map[int]bool
	// workOpen is the ephemeral expansion state of completed-turn workfolds.
	workOpen map[int]bool
	// steerOpen holds the turns whose elbow list is showing every correction
	// rather than the newest three (steerelbow.go). Like the two folds above it,
	// it is a fact about this WINDOW: nothing journals it, and a conversation
	// re-opened tomorrow opens folded.
	steerOpen map[int]bool
	// sel is the selected tool entry, or -1. ↑/↓ move it; enter opens it.
	sel int
	// hot is what the pointer is over (hover.go). The zero value is nothing.
	hot hoverAt
	// drag is the left button's gesture in flight — a parked body click, or a
	// sweep whose rows wear the selection — and the flash pair under it is what
	// the status line says the last sweep copied (dragselect.go).
	drag       dragSelect
	dragCopied int
	dragUntil  time.Time
	// dragFrom and dragTo are the CONTENT rows the last sweep copied, kept so
	// the selection stays lit while the status line still says "copied"
	// (dragselect.go's [app.dragSpan]).
	dragFrom, dragTo int

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

	// lastDelta is when text last arrived, and mdAt when the live reply's
	// prefix was last promoted to markdown.
	lastDelta time.Time
	mdAt      time.Time

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

	// ptr is the pointer's fold: the sweep's newest position and the notches of
	// a wheel run, kept so that a burst of them costs the surface one answer per
	// frame instead of one per cell (coalesce.go).
	ptr pointerFold

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
	// mdStyler is the painter prose is handed when this surface has MEASURED its
	// terminal, and nil is the whole of "it has not" — every surface that never
	// hears back from its terminal reads [markdownStyler]'s process-wide one, for
	// the reasons that function states. See [app.styler]: this field is the seam
	// THE GLARE LAW crosses when the ground stops being assumed.
	mdStyler *tokens.Styler
	// codeCache is the painted rows of the last few source blocks this surface
	// lexed (codeview.go). Tool rows are drawn fresh on every frame by design, and
	// this is what stops that from meaning "lex eight hundred lines thirty times a
	// second" the moment somebody lifts a big read's cap.
	codeCache codeBlockCache
	input     editor
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
	wait preflight
	// mem is the memory place's state: the snapshot it is drawing, the shelves that
	// are unrolled, and the filter (place_memory.go).
	mem memoryPlace
	// memory is the store the place reads and changes. It is optional because
	// memory-off sessions must have no capability behind the place.
	memory memoryStore
	// searchStore is the conversation index the search place reads, and
	// usageLedger is the file the spend place reads. Both are optional and both
	// are absent rather than broken when they are: search says what it is for,
	// and an empty ledger draws the spend place's own teaching.
	searchStore SearchStore
	usageLedger string
	// world is the walk of the machine THE SESSION RUNS ON, and farPlaces is the
	// state root it was walked under. Nil and empty are this process's own disk,
	// which is every local launch; over --host the door fills both and the places
	// stop listing the laptop (tui3.go's [Options.World], [app.worldRoot]).
	world     func() (session.World, bool)
	farPlaces string
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
	standRail   []StandingItemView
	standRailAt time.Time
	tasks       map[uint64]*taskNode
	taskOrder   []uint64
	taskSeen    map[uint64]session.TaskState
	taskLane    <-chan session.Event
	taskGen     int
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
	// the keyboard (ctrl+t) — without it there is no cursor, and every key still
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
	// roomPump is the command a freshly opened room's lane needs, PARKED rather
	// than returned.
	//
	// The reason is one call path this file cannot hand a command back through:
	// enter on a selected proposal row runs input.go's [app.enter] into
	// [app.openTool], which returns nothing at all. A door that worked when it
	// was clicked and did nothing when it was pressed would be the worse of the
	// two defects, so every door parks here and the program loop drains it.
	roomPump tea.Cmd
	// orchLive is the adaptive run this session has last heard from, or ""
	// (roomorch.go). It is the surface's only handle on a run: a run is not a
	// node, so it is on no roster and has no row, and the three orchestrate event
	// kinds are what say one exists at all. It is what → opens and what a pause
	// raises its gate on.
	orchLive string
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
	// artifacts is the deliverables index a finished /export records itself in
	// (export.go). Empty is the door saying nothing, which [app.artifactsIndex]
	// turns into the product's own path.
	artifacts string
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
	// draw and read by the same press, on [app.tabs]'s bargain exactly. A click
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
	// hostApproval is the engine's own tool-approval posture, carried on the
	// welcome (Options.ApprovalMode) and read only over --host — see
	// [app.approvalPosture].
	hostApproval string

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
	// welcome is the box an empty session opens with (welcome.go). It is the
	// only animation on this surface that is not a spinner, and it runs once.
	welcome welcome
	// roster is the resume picker: the same conversations the box lists, opened
	// on purpose and filterable (resume.go).
	roster roster
	// shelf is the deliverables picker /files opens over the global index of
	// what has been made (deliverables.go). It reads the same index /export
	// writes: the artifacts field above, resolved by [app.artifactsIndex].
	shelf shelf
	// recentSessions answers the box's right column and the picker's rows, and
	// resume opens one of them. Both are nil on a surface the door did not wire,
	// and then the box says it has no sessions rather than pretending to have
	// lost them.
	recentSessions func() []Session
	resume         func(file string) (Agent, error)
}

// landingKeysWord is the opening line of every session: the two keys the status
// line has no room for. It is named because the note that writes it also names
// the two chords inside it for THE PAYLOAD RULE (payload.go), and a sentence
// spelled in one place with its keys spelled in another is a sentence that gets
// reworded while the keys stay where they were.
const landingKeysWord = "esc interrupts · ctrl+c twice quits"

func newApp(ctx context.Context, opts Options) *app {
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
		ctx:              ctx,
		agent:            opts.Agent,
		fresh:            opts.Fresh,
		start:            opts.Start,
		open:             opts.Open,
		errand:           opts.Errand,
		standingRoot:     opts.StandingRoot,
		leaveAnswer:      opts.Answer,
		host:             host,
		hostApproval:     strings.TrimSpace(opts.ApprovalMode),
		owned:            opts.Owned,
		landing:          opts.Landing,
		pickSession:      opts.PickSession,
		workspace:        place,
		place:            shown,
		file:             opts.SessionFile,
		resumed:          opts.Resumed,
		models:           opts.Models,
		history:          opts.History,
		draftFile:        opts.DraftFile,
		artifacts:        opts.ArtifactsIndex,
		ctxWindow:        opts.ContextWindow,
		profileDir:       opts.ProfileDir,
		settings:         opts.Settings,
		saveApproval:     opts.SaveApproval,
		saveBashApproval: opts.SaveBashApproval,
		saveModel:        opts.SaveModel,
		applyAPIKey:      opts.ApplyAPIKey,
		applyApprovals:   opts.ApplyApprovals,
		recentSessions:   opts.RecentSessions,
		resume:           opts.Resume,
		stands:           opts.Standing,
		link:             opts.Link,
		conns:            opts.Connections,
		harn:             opts.Harnesses,
		memory:           opts.Memory,
		searchStore:      opts.Search,
		usageLedger:      opts.UsageLedger,
		world:            opts.World,
		farPlaces:        opts.WorldRoot,
		live:             -1,
		echoAt:           -1,
		sel:              -1,
		think:            -1,
		unfolded:         map[int]bool{},
		stick:            true,
		width:            80,
		height:           24,
		pal:              detectPalette(),
		linear:           opts.Linear,
		tmux:             tmuxTerm(os.Getenv),
		remote:           remoteLink(os.Getenv),
		// THE CHORD SPELLING IS A BOOT FACT (chords.go). The platform decides
		// whether the modifier is called `alt+` or `⌥`, and the environment names
		// which emulator is running so the one option-as-meta line can name the
		// setting instead of waving at "your terminal".
		chords: detectChords(runtime.GOOS, os.Getenv),
		// A terminal that has said nothing is assumed to HAVE the keyboard, which
		// is the quiet assumption: the cost of getting it wrong is a notification
		// nobody got, and the cost of the other default is a notification every
		// turn on a screen somebody is watching (notify.go).
		focused: true,
	}
	a.copy.mark = -1
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
	a.pathLinks = terminalTakesLinks(os.Getenv) && (!a.hosted() || a.rfiles != nil)
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
	// The notices get their first look now that the conversation, the box and
	// the directory's facts are all in place: a news line lands here, under the
	// replay and above the door's own notice, and the hints that wait on this
	// directory having an earlier conversation can see the welcome's list.
	a.noticeEvent(eventBoot)
	if notice := strings.TrimSpace(opts.Notice); notice != "" {
		a.note(notice)
	}
	if a.resumed && a.file != "" {
		// The journal is named with its machine on a remote session, for /status's
		// reason (statusnote.go): a path a person is shown is a path they may go
		// looking for, and this one is not on their disk.
		a.note("resumed " + a.hostedPath(a.file))
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
func (a *app) noteLandingKeys() { a.noteFacts(landingKeysWord, "esc", "ctrl+c") }

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
	standing := []tea.Cmd{a.probeGit(), a.watchTasks(), a.watchWakes(), a.watchDesigns(),
		a.watchRuns(), a.loadTasks(), a.stirLane(), a.askHeld(), a.watchDriving(), a.watchFollowing(),
		a.linkPingTick(), tea.RequestBackgroundColor}
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
	model, cmd := a.update(msg)
	if a.levelsWaiting() {
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

	case sigQuitMsg:
		// A REAL SIGINT OR SIGTERM, forwarded by this package's own handler
		// (tui3.go's [forwardSignals]) because Bubble Tea's answers SIGINT by
		// returning an error without ever calling this function. It takes the
		// ordinary door: the draft and anything parked go to disk, the session
		// closes, and the program exits zero. NO SECOND PRESS IS ASKED FOR — the
		// two-press rule is about a keystroke that can be struck by accident, and
		// a signal is somebody naming this process on purpose.
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
		// THE STOP CONFIRMATION READS FIRST of the three below, and only ever
		// while it is up or while `x` is being pressed at something stoppable
		// (stop.go). It is a question about ENDING the work the roster and the
		// room are pages onto, so a key that reached either of them would be a
		// key aimed at the very thing being stopped.
		if cmd, took := a.stopKey(msg); took {
			return a, tea.Batch(flushed, cmd)
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
		return a, a.tasksLoaded(msg.rows)

	case taskTailMsg:
		// One node's journal, read off the loop for the record card
		// (taskrecord.go). A read that came back about a task the person has
		// already walked away from is dropped there.
		a.taskTailRead(msg)
		return a, nil

	case draftSaveMsg:
		return a, a.saveDraft(msg.file)

	case behindStirMsg:
		// A conversation this process holds and is not drawing has something to
		// say about itself. The message carries no content — the surface reads
		// the agent it already has a pointer to (keeper.go).
		return a, a.behindStir(msg.key)

	case exportedMsg:
		a.exportDone(msg)
		return a, nil

	case copiedMsg:
		// A deliverable taken out of the session that made it (deliverables.go),
		// coming back from the disk the way an export does.
		a.copiedFile(msg)
		return a, nil

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
			a.scroll(-3)
		case tea.MouseWheelDown:
			a.scroll(3)
		}
		return a, nil

	case tea.MouseClickMsg:
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
				return a, a.homePress(msg.Mouse().X, msg.Mouse().Y)
			}
			// AND THE FOUR PLACES THE ROUTER PROMOTED AT THE SAME RUNG AND FOR
			// THE SAME REASON: each is the whole screen, so a press that fell
			// through to the conversation underneath would open a tool call
			// nobody can see. A press on one of their rows moves that place's
			// cursor and never acts (pages.go's [app.placeBodyPress]).
			if cmd, took := a.placeBodyPress(msg.Mouse().Y); took {
				return a, cmd
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
			// AND THE ROOM'S HEADER IS READ DIRECTLY UNDER THE ✕ THAT RIDES IT, which
			// is the pointer's share of the way out: the row says "esc/← main", and a
			// row that named the exits and did nothing when it was pressed would be the
			// one dead cell on the page (room.go's [app.roomBackPress]).
			//
			// It is read HERE, above the strip and the rail, because the header spans
			// the whole window while both of those claim columns of it — the rail takes
			// every press in its own columns whether or not a row was under it, so a
			// header read after it would be dead at exactly the end where the words are
			// printed.
			if a.roomBackPress(msg.Mouse().Y) {
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
			return a, nil
		}
		return a, nil

	case tea.MouseReleaseMsg:
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
			a.homeHover(msg.Mouse().X, msg.Mouse().Y)
			return a, nil
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
		after := a.apply(msg.ev)
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
		a.roomResolveUnfinished()
		a.roomTouched()
		return a, a.wake()

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

	case hudFadeMsg:
		// One of the two catch-up ticks: nothing changed, but a number that was
		// news four seconds ago has stopped being news, and the frame has to be
		// drawn again to say so.
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
		a.settleSizing()
		door, ok := a.agent.(taskCommandAgent)
		if !ok {
			a.note("could not start the task · this session has no task door")
			return a, nil
		}
		// NOTHING TO SPLIT IS ONE WORKER, whatever the row says. A planner over
		// work with no independent parts in it is a second model deciding to do
		// the one thing there was to do, and the worker can still split its own
		// brief later if it finds parts the sizing call did not.
		if !msg.parallel {
			return a, a.startTaskDoor(door, "single", msg.brief, "")
		}
		// A PLANNER ONLY WHERE SOMEBODY ASKED FOR ONE. The row set to `adaptive`
		// is that asking, said once instead of on every command.
		if msg.preset == config.TaskStartAdaptive {
			return a, a.startTaskDoor(door, "adaptive", msg.brief, msg.hint())
		}
		// AND OTHERWISE THE WIDE WORK STARTS AS ONE WORKER, ARMED. This used to be
		// the moment a two-row card opened and asked which shape to run, and the
		// card was the wrong question: it wanted a decision about width before
		// anybody had opened the material, from the one person in the room who had
		// not read it. The measured road answers it later and from evidence — the
		// judge's yes here arms this task to divide (internal/session's
		// [Agent.armDivision]), the worker hands the parts out only once it has
		// seen how many there really are, and they ride the same frontier the rest
		// of the graph does. So the command starts the work, and the note says the
		// one thing the person could not otherwise know: it may not stay one task.
		a.note(taskWideNote)
		return a, a.startTaskDoor(door, "single", msg.brief, "")

	case taskStartedMsg:
		a.settleShaping()
		if msg.err != nil {
			a.note("could not start the task · " + msg.err.Error())
		} else {
			// WHAT LANDED, AND WHAT IT IS CALLED (payload.go). The id is how a
			// person names this node to any other command on the surface and the
			// title is how they recognize it in the roster, so those two step up
			// while the mode word and `started` stay in the note's own dim.
			a.noteFacts(msg.kind+" task "+msg.id+" started · "+msg.title, msg.id, msg.title)
			a.noticeEvent(eventTaskStarted)
		}
		return a, nil

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
	}
	// WHAT THE OTHER WINDOWS HAVE OUT IS RE-READ HERE, and only while something
	// on the frame is drawing it: the roster's record rows say `running` or
	// `incomplete` off that reading, and the history page draws a row per piece
	// of work another window is holding (taskview.go's [app.refreshElsewhere],
	// which keeps its own short window so this clock cannot outpace the disk).
	if a.railStanding() || a.at(pageTasks) {
		a.refreshElsewhere()
	}
	a.promoteMarkdown()
	// The welcome box's one-shot animation is the second and last reason this
	// clock runs while nothing is being asked of the model. It steps here and
	// stops itself (welcome.go), which is what makes it one-shot rather than a
	// loop with a condition somebody has to remember to write.
	a.welcome.tick(a.frameStride())
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
		a.wait.live() ||
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
		a.levelsWaiting() {
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
		return a.settle()
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
func (a *app) apply(ev session.Event) tea.Cmd {
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
	// is not [app.dropLive]'s removal and does not disturb its asymmetry: nothing
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
	// block through [app.settleThought], and only a real boundary — a tool
	// call, the turn ending — lets go of it ([app.collapseThought]).
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

	switch ev.Kind {
	case session.EventTaskReplyTags:
		a.pendingReplyTags = append(a.pendingReplyTags, ev.TaskReplyTags...)
		if a.live >= 0 && a.live < len(a.entries) && a.entries[a.live].kind == entryAssistant {
			a.entries[a.live].replyTags = append(a.entries[a.live].replyTags, a.pendingReplyTags...)
			a.pendingReplyTags = nil
			a.entries[a.live].stale = true
		}

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
		// AND HOME WITH IT: the answer lane is y, r, n and the digits, and home
		// owns every letter while it is up (task.go), so a proposal left behind
		// it would be a decision with no key that reaches it.
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

	case session.EventToolForming:
		// THE CALL IS ARRIVING. Nothing has been asked for yet — this is the
		// model writing the instruction, drawn while it writes it.
		a.formTool(ev)

	case session.EventToolAnnounced:
		a.announceTool(ev)

	case session.EventToolBegin:
		a.beginTool(ev)

	case session.EventToolFinished:
		a.finishTool(ev)
	case session.EventToolEnd:
		a.closeTool(ev, toolOK, "")
		// AND A FILE THE MODEL JUST WROTE ON THE OTHER MACHINE IS FETCHED NOW,
		// speculatively, silently, before anybody has clicked anything. It is the
		// wave's whole answer to movement: no push was added to the wire, the
		// engine does not know this is happening, and the only difference is that
		// the click which used to be a round trip usually is not (remotefiles.go).
		// Nil on every local session.
		after = tea.Batch(after, a.prefetchWritten(ev))

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
		// AND THE SCROLLBACK'S BOOKKEEPING MOVES WITH THE PASS. Everything above
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

	case session.EventRetrying:
		// The request was cut and is being asked again (internal/provider's
		// streamguard.go). EVERYTHING THE DEAD ATTEMPT DREW GOES, because the
		// engine has already thrown away everything the dead attempt SAID: the
		// text belongs to a response that will never exist, and half a dead
		// answer sitting above the live one is the surface telling a story the
		// transcript does not contain.
		a.dropLive()
		a.resolveUnfinished()
		a.retrying = true
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
// SCREEN and can open nothing — [app.closeTool] walks the drawn rows and writes
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
	a.closeLive()
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
	// spinning again the moment the next turn does ([app.resolveUnfinished]).
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

// resolveUnfinished stops the clock on every call that was still in the air when
// the turn ended. It is [app.dropForming] widened by two states, and it is the
// conversation's copy of the law room.go already keeps at a node's lane close
// ([app.roomResolveUnfinished]).
//
// THE DEFECT IT CLOSES IS A ROW THAT COMES BACK TO LIFE. A row that never got
// its end — an interrupt between the begin and the result, a retry that threw
// away the attempt those announcements belonged to, a stream that died — kept
// `toolRunning` with no end stamped on it. That looked settled for as long as
// the surface was idle, because both the spinner and the age are drawn only
// while the session is working (toolview.go's [app.mark] and [app.countClock]).
// Then the NEXT turn started, the session was working again, and the abandoned
// row began spinning a second time — with an age measured from a beginning
// minutes or hours earlier. "view_image always seems to be running" is that row.
//
// The end stamp is what makes it permanent: both of those renderers stop at a
// row that has an end on it, whatever the session is doing afterwards.
//
// Every row is RESOLVED, never removed, and the STATUS IS LEFT ALONE, for
// [app.roomResolveUnfinished]'s reasons exactly: the call was asked for, which
// is a fact about what happened, and "failed" would be a claim about something
// nobody watched.
func (a *app) resolveUnfinished() {
	now := a.now()
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || !e.ended.IsZero() {
			continue
		}
		if e.forming() || e.status.live() {
			e.ended = now
			e.stale = true
		}
	}
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
//
// THE STALE FLAG IS THE WHOLE OF THE SETTLE, and it is load-bearing rather than
// tidy. [app.entryRows] hands back the rows it built last time unless something
// says otherwise, and a settled entry is not one of the shapes that bypass the
// cache — so without marking it here the block would keep the rows it was drawn
// with mid-stream: unrendered markdown, and the live ink of render.go's growing
// edge left bright on an answer that finished minutes ago. Setting both in one
// statement is deliberate: the two facts are one event.
func (a *app) closeLive() {
	if a.live >= 0 && a.live < len(a.entries) {
		e := &a.entries[a.live]
		e.settled, e.stale = true, true
	}
	a.live = -1
}

// dropLive throws away the assistant block the CURRENT attempt was streaming
// into, because that attempt has been cut and its text is void.
//
// It is the one place on this surface where something a person watched arrive is
// REMOVED rather than settled, and the asymmetry is the point: an interrupt
// leaves the partial reply on screen because the engine keeps it in the
// transcript, while a cut stream leaves nothing anywhere. A row the transcript
// does not contain must not stay on the page — the next question would be
// answered underneath somebody else's abandoned sentence, and the person would
// have no way of telling which of the two the model actually read.
//
// The block is truncated when it is the last thing on screen, which is what a
// cut mid-text always leaves, and emptied otherwise: removing an entry from the
// middle would move every index after it, and the forming rows, the selection
// and the thought marker are all held by index.
func (a *app) dropLive() {
	if a.live < 0 || a.live >= len(a.entries) || a.entries[a.live].kind != entryAssistant {
		a.live = -1
		return
	}
	if a.live == len(a.entries)-1 {
		a.entries = a.entries[:a.live]
	} else {
		a.entries[a.live].text = ""
		a.entries[a.live].stale = true
	}
	a.live = -1
	a.touch()
}

// said puts one of the PERSON'S OWN lines into the transcript without cutting
// the answer that is still streaming in two.
//
// THE DEFECT IT FIXES. A message sent while a reply was streaming went in the
// obvious way — close the live block, append the line — and the very next delta
// found no live block and opened a second one under it. What the reader saw was
// one flowing answer with somebody else's sentence wedged between two of its
// paragraphs, as though the model had quoted them mid-thought. The words were in
// the right place in TIME and in the wrong place on the PAGE, and the page is
// the only record anybody reads back.
//
// SO THE STREAMED BLOCK STAYS WHOLE. The line is appended after it and the live
// index is left where it was, which is still valid — appending never moves an
// earlier entry — so the next delta grows the block it was already growing and
// the person's line stays below it. A tool row is deliberately NOT treated this
// way: a call lands in place, between two paragraphs, because that is where it
// happened and the reply is written around it.
//
// The room's own transcript takes the same rule from [app.roomSaid] (room.go).
func (a *app) said(e entry) {
	live := a.live
	a.entries = append(a.entries, e)
	if live < 0 || live >= len(a.entries)-1 || a.entries[live].kind != entryAssistant {
		a.live = -1
		return
	}
	a.live = live
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
		a.entries = append(a.entries, entry{kind: entryAssistant, turn: a.turn,
			replyTags: append([]session.TaskReplyTag(nil), a.pendingReplyTags...)})
		a.pendingReplyTags = nil
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
// NOTHING HERE IS UNMARSHALED. ev.ArgsText is half a JSON object, and half a
// JSON object is not a payload: the row holds the SIZE of what has arrived, the
// gloss session built from the fields that have closed, and — for the one tool
// whose whole substance is one string — the tail of that string as it streams
// ([formingPreviewField]). A surface that unmarshaled a prefix would be drawing
// a call the model has not finished asking for; [session.PartialString] is the
// other thing, a tolerant scan of one field that answers with what has arrived
// and never invents the rest.
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
			formed: formingPreview(ev.Tool, ev.ArgsText),
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
	// The live text is taken forward the same way, and for the same reason: the
	// name arrives on one fragment and the body on the ones after it, so the
	// FIRST fragment of a write is a text this cannot read yet — and an empty
	// answer must not wipe what the row was already showing.
	e.formed = firstNonEmpty(formingPreview(firstNonEmpty(ev.Tool, e.tool), ev.ArgsText), e.formed)
	a.touch()
}

// formingPreviewField names the argument a call that is still ARRIVING is shown
// the contents of, per tool. It has one entry, and the shortness of the table is
// the decision rather than an omission.
//
// `write` qualifies because its content is APPENDED TO and never revised: what
// has arrived is the beginning of the file and will still be the beginning of
// the file when the call is whole, so a person reading it is reading something
// true. Nothing else on the belt is like that.
//
// `edit` is the near miss and it is deliberately absent. Its block is a DIFF,
// and a diff needs both sides whole — half an old_string against a new_string
// nobody has started sending is not a change, it is a claim about one — so a
// live edit block would redraw itself into a different diff as the second half
// arrived, which is the exact "read the same diff twice" defect [app.previewHead]
// is written to avoid. And mechanically it could not be had cheaply anyway: bare
// spells an edit as {path, edits:[{oldText, newText}]}, and the streamed strings
// are therefore NESTED, where session's forming scanner deliberately does not
// look (its toolhint.go). The whole diff still lands the instant the call is
// announced, which is the moment it becomes true.
var formingPreviewField = map[string]string{"write": "content"}

// formingPreview is the streamed text a forming row draws, or "" for a call this
// surface previews nothing of.
func formingPreview(tool, argsText string) string {
	field, previewed := formingPreviewField[tool]
	if !previewed {
		return ""
	}
	text, _ := session.PartialString(argsText, field)
	return text
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
		// The streamed tail is let go here: the whole payload has landed, so the
		// preview under this row is now drawn from the arguments, and holding the
		// last eight kilobytes of every file the session ever wrote would be the
		// transcript keeping a copy nothing reads.
		e.formed = ""
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
		// Let the streamed tail go, for [app.announceTool]'s reason — this is the
		// other door a forming row leaves by.
		e.formed = ""
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

// finishTool stops one row's clock at ITS OWN finish and writes what the call
// took (session.EventToolFinished).
//
// It closes nothing. The row keeps its spinner and its live status until the
// result arrives with the batch, because until then the surface genuinely does
// not know whether the call succeeded — what it knows, and what this writes, is
// that this call is no longer the reason anybody is waiting.
//
// The pairing is [app.claimAnnounced]'s rule, one state later: the payload
// first and the tool name only after, because a batch of three bash calls
// finishing in any order pairs by name alone onto whichever row was drawn
// first, and the two identical calls where the rules disagree are the same work
// either way. A row that already has its figure is never taken twice.
func (a *app) finishTool(ev session.Event) {
	if ev.Took <= 0 {
		return
	}
	fallback := -1
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || !e.status.live() || e.ran > 0 || e.tool != ev.Tool {
			continue
		}
		if ev.Args != "" && e.detail.Args == ev.Args {
			fallback = i
			break
		}
		if fallback < 0 {
			fallback = i
		}
	}
	if fallback < 0 {
		return
	}
	a.entries[fallback].ran = ev.Took
	a.touch()
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
//
// THE SAME SENTENCE TWICE RUNNING IS ONE SENTENCE. Half the lines in this lane
// are the surface answering an act a person repeats while they work out what to
// do next: /files on a machine that has made nothing answers [filesNothingWord]
// every single time, /subharness on a build with none answers [subNothingWord],
// and a refusal answers whatever it refused.
// Four presses used to leave four identical lines stacked in the transcript,
// which is the emptiness law's own complaint said about repetition — the screen
// counting how many times it had nothing to report. So a note whose words are
// already the last thing in the transcript is not written again; it is brought
// back into view, which is the whole of what the person was going to read.
//
// IT ASKS ABOUT THE LAST ENTRY AND NEVER ABOUT THE WHOLE TRANSCRIPT. Anything at
// all landing in between — an answer, a tool call, another note — puts the
// repeat in a new place, where it is news again: "nothing stands here yet" under
// the reply that just talked about standing orders is a different sentence from
// the one four lines up, and a transcript that swallowed it would be answering a
// deliberate command with silence.
func (a *app) note(text string) { a.noteFacts(text) }

// noteFacts is [app.note] with THE PAYLOAD RULE's data named: the words inside
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

// noteWritten is the one body behind both, so the repeat rule, the fact list and
// the block flag cannot disagree about what a note is.
func (a *app) noteWritten(text string, block bool, facts []string) {
	a.closeLive()
	if n := len(a.entries); n > 0 && a.entries[n-1].kind == entryNote && a.entries[n-1].text == text {
		// The repeat is brought back into view rather than written again (above),
		// and its data are refreshed with it: the same sentence built a second time
		// may have been built from a different reading, and a stale fact list would
		// lift the words of the frame before this one.
		a.entries[n-1].facts, a.entries[n-1].block = facts, block
		a.follow()
		a.touch()
		return
	}
	a.entries = append(a.entries, entry{kind: entryNote, text: text, turn: a.turn, facts: facts, block: block})
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
	return a.submitting(text, func() (<-chan session.Event, error) { return agent.Submit(ctx, text) })
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
			run.WriteString(next.Text)
		}
		if run.Len() > 0 {
			ev.Text += run.String()
		}
		return streamEventMsg{gen: gen, ev: ev, then: then}
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
	case hitWorkFold:
		a.toggleWorkfold(r.turn)
	case hitSteerFold:
		a.toggleSteerFold(r.turn)
	case hitMore:
		a.showAll(r.entry)
	case hitBrief:
		a.toggleBriefFoldAt(r.entry)
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
		if r.hit != hitTool && r.hit != hitTask && r.hit != hitDone && r.hit != hitHarness {
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
		help := helpText(a.hostedPath(a.file), a.chords)
		a.noteFacts(help, columnFacts(help, true)...)
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
		return a.renew()

	default:
		a.note("unknown command: /" + name + " · try /help")
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
	if !whole {
		return
	}
	if workspace := strings.TrimSpace(conv.Workspace); workspace != "" {
		a.workspace = workspace
	}
	a.owned = conv.Owned
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
func (a *app) renew() tea.Cmd {
	if !a.canStart() {
		a.note(newUnavailableWord)
		return nil
	}
	replacing := a.freshAndEmpty()
	if !replacing {
		if word, room := a.roomForAnother(); !room {
			a.note(word)
			return nil
		}
	}
	conv, whole, err := a.nextConversation()
	if err != nil {
		a.note("new session failed: " + err.Error())
		return nil
	}
	// THE DOOR IS ASKED BEFORE ANYTHING IS PUT DOWN, which is [app.openSession]'s
	// own repair: a /new that failed used to leave the surface holding a closed
	// session with nothing to fall back on, and now a refusal costs nothing at
	// all.
	leaving, side := a.agent, a.detachConversation()
	if replacing {
		if leaving != nil {
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
	if key := convKey(a.file); key != "" {
		a.rememberOpen(key)
	}
	return cmd
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

// quit is the door out of the PROGRAM, and it takes every conversation this
// terminal is holding with it.
func (a *app) quit() tea.Cmd {
	// The draft goes to disk on the way out, synchronously and before anything
	// else: the debounce may be mid-window, and a sentence typed in the last
	// three hundred milliseconds of a session is exactly the one a person would
	// be most surprised to lose (draft.go).
	//
	// AND WHAT IS WRITTEN IS THE DRAFT PLUS WHATEVER IS STILL PARKED
	// (quitarm.go's [app.leavingDraft]): a message waiting for an answer that is
	// never now going to land is a message the person typed and pressed enter
	// on, and it comes back next launch rather than going quietly.
	if a.draftFile != "" {
		writeDraft(a.draftFile, a.leavingDraft())
	}
	// AND EVERY CONVERSATION, IN PARALLEL (keeper.go's [app.closeEverything]).
	// The ones this terminal is holding behind the screen have their own drafts
	// already on disk — written when they were left — so what is above is the
	// only box that still needs saving.
	a.closeEverything()
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
	// THE AGENT IS ASKED FOR RATHER THAN ASSUMED, on [app.quit]'s own terms: a
	// surface can be standing with no session under it, and a stop that panicked
	// on the way to stopping nothing would be the worst possible answer to the
	// key a person presses when they want something to stop.
	if a.state != stateWorking || a.agent == nil {
		return
	}
	a.agent.Interrupt()
	a.state = stateInterrupted
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
	// The session drops its follow-up queue on an interrupt — a stop that was
	// followed by the session working again is not a stop — so the surface says
	// so rather than leaving a count above the box for turns that will never run.
	a.dropFollows()
	// AND THIS TURN PROMOTES NOTHING (hierarchy.go's [app.cutTurn]). The mark goes
	// on the blocks at the keypress so the demotion is on screen the moment the
	// person presses esc, and again when the stream finally closes ([app.settle]),
	// because events in flight land between the two.
	a.cutTurn(a.turn)
}

// windingDown reports that the turn on screen was STOPPED BY HAND and its stream
// has not closed yet: the seconds between a person's esc and the engine letting
// go of the turn.
//
// IT IS A REAL WINDOW AND IT IS NOT SHORT. [session.Agent.Interrupt] cancels the
// turn's context and returns at once, but the turn goroutine does not close its
// event hub until [session.Agent]'s loop returns, and the loop cannot look at
// the context until the tool batch it is inside has finished — `wg.Wait()` on
// every call, with no escape for a cancelled context, which is deliberate and
// documented there. Two ordinary calls outlast the cancel by seconds: a `bash`
// whose command left a grandchild holding the output pipe waits the exec
// package's own three-second `WaitDelay` before the pipes are forced shut, and a
// `jobs` kill spends two seconds on a SIGTERM grace and two more on the SIGKILL
// that follows without ever consulting the context. Three to four seconds of
// "nothing appears to have happened" is what this window is worth avoiding.
//
// It is DERIVED and not stored, from the two facts that already exist: the state
// word is only [stateInterrupted] because [app.interrupt] put it there, and the
// stream is only non-nil between a turn opening and its close (app.go's Update).
// A third field holding the same fact is a third thing to keep in step with the
// two.
func (a *app) windingDown() bool { return a.state == stateInterrupted && a.stream != nil }

// ── WHY THERE IS NO SECOND STAGE, AND WHAT WOULD HAVE TO EXIST FIRST ────────
//
// The obvious next thing to build on top of the window above is a HARD STOP: a
// key pressed while the surface is still winding down that ends the turn for
// real rather than politely. It is not built, and it is not built for two
// reasons, either of which would be enough on its own.
//
// THE FIRST IS THAT THERE IS NO KEY LEFT. esc's grammar in the conversation is
// read in a fixed order (input.go, rewind.go): a recall walk takes it, then
// [app.escRewind] — where the first esc ARMS the rewind on its way past and a
// second one inside [rewindArmWindow] OPENS it — and only then [app.interrupt].
// So every esc that lands within half a second of another esc already belongs to
// rewind, and THE INTERRUPT IS NOT FOR SALE cuts the other way just as hard: a
// hard stop inside the window would be taking the door rewind is behind. Putting
// it AFTER the window does not save it either, because an esc past the window is
// a FIRST esc again — it arms the rewind on its way past exactly as before — so
// the key would mean "stop harder" or "open the rewind" depending on what the
// person did half a second later, which is one keypress with two readings and
// the one thing this keyboard cannot have. ctrl+c is spoken for on both sides of
// the same moment: mid-turn it is the interrupt, and at rest — which is what
// winding down IS, since [app.interrupt] leaves stateWorking on the spot — it is
// the quit arm (quitarm.go). A third key, bound for a state that lasts three
// seconds and occurs on a minority of stops, is furniture.
//
// THE SECOND REASON IS THE DECIDING ONE: there would be nothing behind it. A
// CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN, and the session exposes no
// second door to stop with — [session.Agent.Interrupt] cancels the turn's
// context and that context has ALREADY been cancelled by the time this window
// opens, so calling it twice is calling it once. The waits that make the window
// long are waits that do not look at the context at all: the tool loop's
// unconditional `wg.Wait()`, `bash`'s three-second `WaitDelay` on a leaked pipe,
// the `jobs` kill's two SIGTERM-and-SIGKILL graces. A key wired to any of those
// would be a key that says "stopping harder" while the wait ends exactly when it
// was always going to end — which is the worst thing this surface could put
// under the key a person presses when they want something to stop.
//
// WHAT A PERSON ACTUALLY HAS is the program's own door, and it is already on the
// screen and already learnable: ctrl+c twice quits ([landingKeysWord]), and
// quitting takes the process and its children with it. Nothing smaller than that
// can end a wait the engine does not check.
//
// SO THE ORDER OF WORK, if a second stage is ever wanted, is the engine first: a
// context arm on the jobs registry's grace waits, a `ctx.Done()` case on bash's
// wait that hands back what the call has accumulated and lets the reaper finish
// behind it, and then a door on [session.Agent] to reach them by. A key comes
// last, and it comes with something behind it.

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
	if a.at(pageHome) {
		if ex := a.paneExchange(); ex != nil && ex.focused {
			ex.box.insert(text)
		} else {
			a.home.box.insert(text)
			a.home.build()
		}
		a.touch()
		return nil
	}
	// A DROPPED PICTURE IS A PICTURE. A terminal writes a drag-and-drop into the
	// clipboard as the file's PATH, and a paste that is nothing but paths to
	// pictures attaches them and leaves `[image #1]` in the sentence instead —
	// so the model gets the pixels rather than a string it has to guess about
	// (imagepaste.go). Anything else falls through and is inserted as the text
	// it plainly is.
	if !a.pasteImages(text) {
		at := a.input.cursor
		a.input.insert(text)
		a.editTags(at, at, len([]rune(text)))
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
			adds, dels, _ := editStat(e.detail.Args)
			out.adds, out.dels = out.adds+adds, out.dels+dels
		case "write":
			content, _ := argBody(argString(fields, "content"))
			out.adds += lineCount(content)
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

// approvalPosture is [readApproval] asked by this surface, live, for a local
// session — and the engine's own answer, carried once on the welcome, for a
// remote one.
//
// THIS MACHINE'S PROFILE IS NOT THE SESSION'S POSTURE over --host: the gate
// that decides whether a tool runs without asking is the ENGINE's, read from
// the profile on the engine's machine. A YOLO badge drawn from this laptop's
// settings would be a safety claim about a machine nobody consulted, so a
// remote session reads [app.hostApproval] instead of [readApproval] — the
// same answer, asked of the right machine (internal/remote's wire.go
// Welcome.ApprovalMode, set once at boot rather than re-read live, because
// there is nothing on this side left to re-read).
func (a *app) approvalPosture() string {
	if a.hosted() {
		return a.hostApproval
	}
	return readApproval(a.profileDir)
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
