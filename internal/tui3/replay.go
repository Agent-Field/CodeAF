package tui3

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// REPLAY ON RESUME: a resumed conversation opens showing itself.
//
// A session that is picked up rather than created has a transcript the model
// can see and the person cannot, and a surface that opened on an empty screen
// would be asking somebody to hold in their head the thing it is holding on
// disk. So the tail is drawn — in the SAME shapes the live surface draws, from
// the same renderers, because two renderings of one conversation is how a
// replayed screen starts lying about what happened.
//
// THE SAME SHAPES MEANS THE SAME PAYLOAD. A replayed call used to be a line and
// nothing else: the entries carried a gloss, so clicking one opened an expansion
// with nothing in it — the write whose content was the reason to click, gone.
// The journal had it all along (the arguments ride the assistant message's
// tool_calls, the result is the tool message keyed by the same id), so
// [session.DisplayEntry] now carries both and a replayed row expands to the real
// thing: a write to its content, an edit to its diff, a bash to its output.
//
// What is still NOT redrawn is as deliberate: no spinners, no costs, no
// reasoning. A replayed call is a call that finished, and it is drawn quiet.
//
// And a row with NO payload — a file written before either was journaled, a call
// whose result never reached the file — is drawn as the line it always was and
// is NOT interactive: see [replayInert]. An expansion that opens on a blank is
// the defect this wave came to end, and offering one for a row that genuinely
// has nothing behind it would be the same defect wearing the fix's clothes.
//
// AND THE HISTORY IS DRAWN FROM THE WORDS, NOT FROM THE MODEL'S COPY OF THEM.
// A compaction pass does not delete what it shortens: it rewrites the
// conversation in place — a tool result becomes a pointer to its own bytes, a
// long run of the model's work becomes one line — and journals the whole
// rewritten window again below its marker. So the file holds the same
// conversation twice, and the transcript a resumed session opens with is the
// SHORTENED copy. [session.EarlierHistory] is the other one, with the floor that
// says where the copy ends; this file walks the transcript down to that floor and
// then carries on into the region, so the history reads as it was said and reads
// exactly once. The line where the two meet is [seamMark].

// replayTail is how much of a resumed conversation is drawn AT ONCE. Forty
// entries is about two screens of scrollback — enough to remember where you
// were, short of re-rendering an hour of work through markdown layout before
// the first frame.
//
// IT IS NO LONGER A CEILING ON WHAT CAN BE READ, and that distinction is the
// whole of this wave. It used to be both: the surface drew the last forty
// blocks and had no way to ask for the forty above them, so scrolling up hit
// the top of an hour-old conversation's last two screens and stopped, with the
// rest of it sitting in the journal underneath. Now it is the size of ONE
// helping — the opening one, and every one [app.backfill] hands up afterwards.
const replayTail = 40

// replay folds the tail of the agent's transcript into entries. It runs once,
// at construction, before the surface has drawn anything — and again whenever
// the drawn conversation is thrown away and rebuilt from the session's own
// record (rewind.go's [app.rebuildTranscript], welcome.go's resume).
func (a *app) replay() {
	if a.agent == nil {
		a.replayList(nil)
		return
	}
	a.replayList(a.agent.Transcript())
}

// replayList is the replay over a list the caller already holds. It is the
// door [app.attachConversation] takes with the entries an atomic attach handed
// back — a reading that already left out the running turn's work, because the
// stream beside it replays that work from its first event (switcher.go's
// attachReplayer). Everything below the drawn window still pages in from
// [Agent.Transcript] ([app.backfill]): the two lists are identical up to where
// the running turn begins, and the backfill never walks past it.
func (a *app) replayList(all []session.DisplayEntry) {
	// THE TRANSCRIPT IS MIRRORED ON THE SURFACE. A hosted agent paid for this
	// reading across ssh; paging the words afterwards must be a local slice read,
	// not another engine call hidden inside a scroll gesture.
	a.transcript = append([]session.DisplayEntry(nil), all...)
	// Capture the opening words before the visible replay tail is trimmed.
	for _, e := range all {
		if e.Role == "user" && e.Steer == nil && strings.TrimSpace(e.Text) != "" {
			a.openingPrompt = e.Text
			break
		}
	}
	a.historyGen++
	a.historyLoading = false
	// THE BACKFILL'S BOOKKEEPING IS SET HERE, on every path including the one
	// with nothing to replay. A fresh session that inherited a mark from the
	// conversation before it would offer to scroll back into somebody else's
	// history — so the mark is cleared first and earned second.
	a.replayFrom, a.replayFloor = 0, a.turn
	// And the same law applied to the region a compaction left behind: it is
	// dropped and asked for again, so a rebuild after a rewind trusts nothing it
	// was holding before the cut.
	a.earlier, a.earlierFloor, a.earlierFrom, a.earlierSeam = nil, 0, 0, false
	if a.agent == nil {
		return
	}
	// THE REGION IS FETCHED HERE, EAGERLY, and it is the one read on this path
	// that is not lazy. The floor it carries decides where the live transcript
	// stops being conversation and starts being the pass's own rewritten copy of
	// the region — and [app.moreHistory] has to know that from the first frame,
	// not from the first scroll.
	history := a.agent.EarlierHistory()
	a.earlier, a.earlierFloor = history.Entries, history.Floor
	a.earlierFrom = len(a.earlier)

	if a.earlierFloor > len(all) {
		a.earlierFloor = len(all)
	}
	// The opening helping is cut from the conversation BELOW the floor, because
	// everything above it is drawn from the region instead — in the words it was
	// said in rather than in the shortened form the model was left holding.
	from := len(all) - replayTail
	if from < a.earlierFloor {
		from = a.earlierFloor
	}
	blocks, turns := a.replayBlocks(all[from:], chatReplay(a.turn))
	// ANYTHING ALREADY ON THE SCREEN WAS SAID AFTER ALL OF THIS, and goes under
	// it. The record this is built from is fetched off the loop and the box is
	// live the whole time it is in flight, so a person who switched conversation
	// and typed straight away had their message appended to and then buried by
	// the history that arrived behind it.
	inTheGap := a.entries
	// AND WHAT THIS SURFACE ALREADY DREW FROM THE RECORD WAS NOT SAID AFTER ANY
	// OF IT. It is the same conversation, and keeping it puts the record on top
	// of itself: the same answers twice, and a second seam. Only the rows said
	// into this window since the last replay are in the gap ([app.recordRows]).
	if drawn := min(a.recordRows, len(inTheGap)); drawn > 0 {
		inTheGap = inTheGap[drawn:]
	}
	a.entries = append(blocks[:len(blocks):len(blocks)], inTheGap...)
	a.recordRows = len(blocks)
	a.turn += turns
	a.replayFrom = from
	// A CONVERSATION THAT WAS COMPACTED AND THEN PUT DOWN HAS ALMOST NO TAIL — a
	// pass that fired on the last turn leaves none at all — and a resumed surface
	// that opened on an empty screen would be the defect replay exists to prevent.
	// So the first helping is filled out of the region, through the seam, exactly
	// as a scroll would fill it.
	for len(a.entries) < replayTail && a.earlierFrom > 0 {
		if !a.backfillEarlier() {
			break
		}
	}
	a.touch()
}

// moreHistory reports whether the conversation on screen starts part-way
// through — whether the journal holds anything ABOVE the first block drawn.
//
// It is the one question three separate things ask: the scroll, which backfills
// rather than stopping; the marker at the top of the frame, which says so; and
// the tests, which is how the two stay one answer.
//
// IT COUNTS THE REGION TOO, and that is this wave's correction. It used to be
// `replayFrom > 0` and nothing else, so at the floor the marker went out and the
// surface declared the conversation finished — while the conversation in the
// words it was said in was still sitting in the journal above it.
func (a *app) moreHistory() bool {
	if a.agent == nil {
		return false
	}
	return a.replayFrom > a.earlierFloor || a.earlierFrom > 0
}

// rebase hands the backfill's bookkeeping over to the region a pass has just
// created, and it is what keeps scrolling up honest across a compaction that
// fires while somebody is reading (app.go's session.EventCompacted).
//
// THE TWO LISTS ARE THE SAME LIST, which is what makes the handover exact
// rather than a guess. The session shapes the region it is about to edit with
// the very shaping [Agent.Transcript] uses, from the very messages it was
// holding (internal/session's loop.go) — so a position in the transcript this
// surface drew from is the same position in the region, and replayFrom simply
// becomes earlierFrom. Below the new floor there is nothing left in the live
// transcript that is not already on screen, so replayFrom lands on it.
//
// A READER ALREADY PAST THE OLD SEAM IS AT THE END OF THE HISTORY. What the new
// region holds above the drawn conversation is the pass's rewritten copy of rows
// that are already on the screen — the older region's own words — so there is
// nothing honest left to hand up, and the offer is withdrawn rather than made
// twice.
//
// It is also the one moment the seam is suppressed rather than drawn. The pass
// puts its own row on the screen at exactly this boundary — the entryCompact
// block that says what it stubbed and folded — and a second line saying the same
// thing would be the surface telling the person twice.
func (a *app) rebase() {
	if a.agent == nil {
		return
	}
	crossed := len(a.earlier) > 0 && a.earlierFrom < len(a.earlier)
	// Compaction replaced the engine's live prefix, so both halves of the local
	// mirror are refreshed together before their splice is rebased.
	a.historyGen++
	a.historyLoading = false
	a.transcript = append([]session.DisplayEntry(nil), a.agent.Transcript()...)
	history := a.agent.EarlierHistory()
	a.earlier, a.earlierFloor = history.Entries, history.Floor
	switch {
	case crossed:
		a.earlierFrom = 0
	case a.replayFrom > len(a.earlier):
		// The transcript the mark was taken against is gone and this cannot be
		// squared with what replaced it. Handing up the whole region is the
		// honest end of that: it is history either way, and the alternative is
		// an index into somebody else's list.
		a.earlierFrom = len(a.earlier)
	default:
		a.earlierFrom = a.replayFrom
	}
	a.replayFrom = a.earlierFloor
	a.earlierSeam = true
}

// backfill materializes the helping of conversation immediately ABOVE what is
// drawn, and reports whether it drew anything. It is what a scroll that runs
// out of transcript calls (view.go's [app.scroll]).
//
// IT IS LAZY RATHER THAN EAGER because the cost it is avoiding is real: every
// block goes through markdown layout, and a session with a thousand entries in
// it would spend that on all of them before its first frame, to draw two
// screens. Handing them up a tailful at a time spends it only on the history
// somebody actually walked back into.
//
// THE READER DOES NOT MOVE. Blocks are prepended and nothing else changes, so
// the row a person is reading is exactly as many rows further down as were put
// in front of it — which is the arithmetic [app.scroll] does, and the reason
// this returns rather than adjusting a scroll it does not own.
//
// THE TRANSCRIPT DOWN TO THE FLOOR, AND THE REGION AFTER IT. The two are walked
// by the same paging in the same helping size and prepended by the same hands
// ([app.prepend]); all that changes at the boundary is which list the helping is
// cut from, and the one line drawn where they meet. Below the floor is
// conversation the transcript is the only record of; at and above it the
// transcript holds the pass's shortened copy and the region holds the words, so
// the region is what is drawn and the copy is never a row.
func (a *app) backfill() bool {
	if !a.moreHistory() {
		return false
	}
	// A ROOM IS THE BODY REGION WHILE IT IS OPEN, and while it is, the selection
	// and the phone's detail sheet index ITS list rather than the conversation's
	// (render.go's [app.bodyDeck]). Renumbering the conversation underneath them
	// would move a page nobody is looking at and take the one they are with it.
	// Nothing is lost by refusing: a room routes its own scroll (room.go), so
	// this is unreachable from the keyboard anyway, and the conversation is
	// still there to scroll back into the moment esc gives the frame back.
	if a.room != nil {
		return false
	}
	if a.replayFrom > a.earlierFloor {
		return a.backfillLive()
	}
	return a.backfillEarlier()
}

// backfillLive hands up one helping of the conversation below the floor — the
// part of the transcript that is not a rewritten copy of anything.
func (a *app) backfillLive() bool {
	all := a.transcript
	to := a.replayFrom
	if to > len(all) {
		// A transcript that got SHORTER than the mark is one a rewind cut under
		// us. There is nothing honest to hand up; the next rebuild sets the mark
		// again from what is actually there.
		a.replayFrom = 0
		return false
	}
	from := to - replayTail
	if from < a.earlierFloor {
		from = a.earlierFloor
	}
	if from >= to {
		a.replayFrom = from
		return false
	}
	a.prepend(all[from:to], false)
	a.replayFrom = from
	return true
}

// prefetchHistory asks for the next local page once the viewport is within one
// screen of the oldest materialized row. The command boundary is deliberate
// even though the data is memory-resident: replaying entries can grow into
// markdown work, and no key or wheel handler is allowed to wait for that page.
func (a *app) prefetchHistory() tea.Cmd {
	if a.historyLoading || a.room != nil || !a.moreHistory() {
		return nil
	}
	height := a.viewHeight()
	total := len(a.visible(a.bodyWidth()))
	if a.offsetFor(total, height) > height {
		return nil
	}

	msg := historyPageMsg{gen: a.historyGen}
	var source []session.DisplayEntry
	switch {
	case a.replayFrom > a.earlierFloor:
		msg.to = a.replayFrom
		msg.from = msg.to - replayTail
		if msg.from < a.earlierFloor {
			msg.from = a.earlierFloor
		}
		if msg.to > len(a.transcript) || msg.from >= msg.to {
			return nil
		}
		source = a.transcript
	case a.earlierFrom > 0:
		msg.earlier = true
		msg.to = min(a.earlierFrom, len(a.earlier))
		msg.from = max(0, msg.to-replayTail)
		msg.seam = !a.earlierSeam
		if msg.from >= msg.to {
			return nil
		}
		source = a.earlier
	default:
		return nil
	}

	a.historyLoading = true
	return func() tea.Msg {
		msg.entries = append([]session.DisplayEntry(nil), source[msg.from:msg.to]...)
		return msg
	}
}

// historyPrefetched materializes one returned page above the viewport without
// moving the line under the reader's eye. A stale answer is harmless: the
// generation and source position must both still describe the current replay.
func (a *app) historyPrefetched(msg historyPageMsg) tea.Cmd {
	if msg.gen != a.historyGen {
		return nil
	}
	a.historyLoading = false
	if msg.earlier {
		if a.earlierFrom != msg.to {
			return a.prefetchHistory()
		}
	} else if a.replayFrom != msg.to {
		return a.prefetchHistory()
	}

	height := a.viewHeight()
	beforeTotal := len(a.visible(a.bodyWidth()))
	beforeOffset := a.offsetFor(beforeTotal, height)
	a.prepend(msg.entries, msg.seam)
	if msg.earlier {
		a.earlierFrom, a.earlierSeam = msg.from, true
	} else {
		a.replayFrom = msg.from
	}
	afterTotal := len(a.visible(a.bodyWidth()))
	if !a.stick {
		a.offset = beforeOffset + afterTotal - beforeTotal
	}
	return a.prefetchHistory()
}

// backfillEarlier hands up one helping from ABOVE the seam — the conversation
// the journal kept and the model let go of — and draws the seam itself the
// first time it does.
//
// The paging is [replayTail] exactly as it is below the seam, which is the
// point: from the reader's side there is one gesture and one rhythm, and the
// boundary is a line they scroll past rather than a wall they hit.
func (a *app) backfillEarlier() bool {
	to := a.earlierFrom
	if to > len(a.earlier) {
		to = len(a.earlier)
	}
	if to <= 0 {
		return false
	}
	from := 0
	if to > replayTail {
		from = to - replayTail
	}
	a.prepend(a.earlier[from:to], !a.earlierSeam)
	a.earlierFrom, a.earlierSeam = from, true
	return true
}

// prepend puts one helping of conversation in front of everything drawn, with
// the seam under it when this is the crossing.
//
// EARLIER TURNS NUMBER DOWNWARD FROM THE ONES ALREADY DRAWN, which is the
// only numbering that can be handed out without renumbering anything. The
// turn is a grouping id — it decides what folds together and what ctrl+o
// opens (render.go) — so shifting the turns already on screen to make room
// would silently move every fold the person had opened onto somebody else's
// cluster. Counting down instead leaves them alone.
//
// The chunk's LAST turn is made to equal the drawn conversation's floor
// because they are the same turn: the blocks just above the old top are the
// beginning of the turn whose tail was already showing.
func (a *app) prepend(entries []session.DisplayEntry, seam bool) {
	blocks, turns := a.replayBlocks(entries, chatReplay(0))
	shift := a.replayFloor - turns
	for i := range blocks {
		blocks[i].turn += shift
	}
	if seam {
		// The seam belongs to the boundary and therefore to the turn BELOW it —
		// the floor the drawn conversation already had — so it sits with the rows
		// it is a statement about rather than with the history above it.
		blocks = append(blocks, entry{kind: entrySeam, text: seamMark, turn: a.replayFloor})
	}
	// AND EVERY POSITION THIS SURFACE HOLDS IN THE BLOCK LIST MOVES WITH IT.
	a.shiftBlockIndices(len(blocks))
	a.entries = append(blocks, a.entries...)
	// AND THEY ARE THE RECORD'S OWN ROWS, which is exactly what they are: one
	// helping of it, and the seam too when this was the crossing. A replay that
	// arrives later has to be able to tell them from a sentence somebody typed
	// ([app.recordRows]).
	a.recordRows += len(blocks)
	a.replayFloor = shift
	// The pointer was over a row of a list that has just been rebuilt around it,
	// which is the same claim [app.dropHover] makes wherever the rows are
	// replaced.
	a.dropHover()
	a.touch()
}

// shiftBlockIndices moves everything this surface stores as a POSITION in the
// block list, because [app.backfill] has just put blocks in front of all of
// them. An index that stayed behind would point at somebody else's row: the
// streaming reply would append into a finished one, and an approval question
// would be asked about the wrong call.
//
// Every field here is an index into [app.entries] and there are no others — the
// hover is dropped rather than moved, the folds are keyed by turn rather than
// by position, and the rewind timeline indexes the SESSION's transcript, which
// this does not touch.
func (a *app) shiftBlockIndices(by int) {
	if by <= 0 {
		return
	}
	move := func(at *int) {
		if *at >= 0 {
			*at += by
		}
	}
	move(&a.live)
	move(&a.think)
	move(&a.sel)
	move(&a.expand.entry)
}

// earlierMark is the one line at the top of a part-drawn conversation, and it
// is there so the seam is honest: a top row with an hour of conversation behind
// it looks exactly like the beginning of the session without it.
//
// It is spelled in the dim "· " lane this surface says everything of its own in
// (render.go's entryNote), and it is a ROW rather than a block on purpose — it
// is a fact about the SCREEN, not about the conversation, so a rewind cannot
// cut it and an export cannot carry it.
const earlierMark = "· earlier · keep scrolling"

// seamMark is the line drawn where the conversation the model carries ends and
// the conversation only the journal holds begins — the boundary a compaction
// pass left behind.
//
// EVERY WORD OF IT IS A LIMIT STATED PLAINLY. Above this line the transcript is
// still complete and still readable, and the model's own copy of it is not: the
// pass replaced tool results with pointers and long runs of its own work with
// one line, so it can be asked about what is up there and will be answering from
// something shorter than what the person is looking at. The row says both halves
// because half of it would be a lie either way — "you can still read it all"
// alone invites the question that has already been answered wrong, and the first
// clause alone reads as loss.
//
// AND THE SECOND CLAUSE IS ONLY TRUE BECAUSE OF WHAT IS DRAWN ABOVE THIS ROW.
// The rows above it come from the region, not from the pass's shortened copy of
// it ([app.backfillEarlier]); a surface that drew the copy could not say "you can
// still read it all" with a straight face.
//
// It carries no leading "· ": the dim lane it is drawn in supplies that, and
// wraps it on a narrow frame rather than cutting it (render.go's entrySeam).
// The manual quotes the row as the person sees it, "· " and all.
const seamMark = "above here the model keeps a shortened record — you can still read it all"

// earlierRow is the marker painted, or "" when the beginning is already drawn.
func (a *app) earlierRow(width int) string {
	if width < 1 || !a.moreHistory() {
		return ""
	}
	return a.pal.dim(fit(earlierMark, width))
}

// replayShape is the whole of what one page asks of the shaping beyond the
// entries themselves — four facts, in one value, so that the difference between
// the conversation's transcript and a node's page is readable in the two
// literals below rather than spread through the walk as flags.
//
// THE SEMANTICS ARE THE SAME ON BOTH PAGES AND ONLY THE FRAMING DIFFERS. Nothing
// here can drop an entry, change a role or re-order anything: a page that wanted
// one of those would be a second reading of one record, which is the defect this
// file's one walk exists to end.
type replayShape struct {
	// turn is the number the first turn in this window takes. A page whose whole
	// life is one turn starts at zero and stays there.
	turn int
	// tail keeps only the last this-many blocks; zero keeps every one. It is the
	// caller's window on a record that can be hundreds of messages long, and it
	// is applied after the walk so the counting is done over the whole of it.
	tail int
	// brief marks the FIRST of the person's blocks as the instruction this page
	// was given — the one message on this surface that folds (brieffold.go). It
	// is false in the conversation, where no message is terms of reference for
	// the ones under it.
	brief bool
	// running draws a call the record left unanswered as a call that has not come
	// back ([session.DisplayEntry.Answered]). It is what a page opened on work
	// that is STILL HAPPENING needs and what a resumed conversation must not
	// have: the record it resumes from was mended on the way in, and a row left
	// spinning there would be waiting for an end that already happened.
	running bool
}

// chatReplay is the participant's page: numbered turns, no window, no brief, and
// nothing left running.
func chatReplay(turn int) replayShape { return replayShape{turn: turn} }

// roomReplay is the overseer's page: ONE turn with elbows hanging off it
// (steerelbow.go and the lens design's Decision 2), a window onto the tail of a
// record that can be hundreds of messages long, the instruction at the top
// folded, and the call that has not come back drawn as running.
func roomReplay(tail int) replayShape {
	return replayShape{tail: tail, brief: true, running: true}
}

// roomRecord is a whole record shaped for a node's page: the region a compaction
// pass edited away, the seam that says where it ends, and the conversation the
// model still carries. It answers the blocks and how many of the person's
// messages were in them, exactly as [app.replayBlocks] does.
//
// THE REGION IS DRAWN AND NOT DROPPED. A pass rewrites the work it shortens and
// journals the rewritten copy again, so the same conversation is in the file
// twice — once as it happened, above the marker, and once with its results
// stubbed and its long runs folded, below. A page that drew only the copy would
// be showing a person a summary of their own work as though it were the work,
// and every call above the marker would open onto a stub. So the region is drawn
// in the words it was said in and the copy it replaces is skipped
// ([session.Record]'s contract), which is what the conversation already does
// with the same two halves ([app.replay]).
//
// THE SEAM IS THE ROW THAT MAKES IT HONEST, and it is the conversation's own
// ([app.prepend], render.go's entrySeam): above it the page is complete and the
// model's copy of it is not, and the row says both halves because either half
// alone is a lie.
//
// THE WINDOW IS TAKEN ONCE, over the two halves joined, because it is a budget
// for the PAGE — a helping cut from each half would keep a screenful of a region
// nobody scrolled to and drop the work that is happening now.
func (a *app) roomRecord(record session.Record, tail int) ([]entry, int) {
	blocks, turns := a.recordBlocks(record, 0)
	blocks = keepRoomTail(blocks, tail)
	// AND WHAT THE READING COULD NOT DO IS SAID AT THE TOP, above the window
	// rather than inside it. It is a fact about the READING and not about the
	// work, so a page long enough to be windowed must not quietly stop saying it
	// — which is the whole reason it is added after the tail is taken.
	//
	// It is drawn in the seam's own shape and register for the seam's own reason:
	// it is a LIMIT ON WHAT IS ON SCREEN, stated plainly, in the dim lane this
	// surface says everything of its own in. The words are the session's
	// ([session.Record.Unreadable]) and are drawn as written — one reading, one
	// sentence, one place it can be wrong.
	if record.Unreadable != "" {
		blocks = append([]entry{{kind: entrySeam, text: record.Unreadable}}, blocks...)
	}
	return blocks, turns
}

// recordBlocks is the record's own two halves, joined and windowed.
func (a *app) recordBlocks(record session.Record, tail int) ([]entry, int) {
	live := record.Entries
	if record.Floor > 0 && record.Floor <= len(live) {
		live = live[record.Floor:]
	}
	if len(record.Earlier) == 0 {
		return a.replayBlocks(live, roomReplay(tail))
	}
	// Neither half is windowed on its own; the join below is.
	//
	// AND NOTHING ABOVE THE MARKER IS STILL RUNNING. A call is drawn as in flight
	// because the record names it with no result under it, and the end that would
	// settle it arrives on the LIVE lane — which reaches the tail of the record
	// and nothing above a pass that finished minutes or days ago. A row left
	// spinning up there would spin for ever, waiting for an end that already
	// happened and was then folded away.
	shape := roomReplay(0)
	shape.running = false
	blocks, turns := a.replayBlocks(record.Earlier, shape)
	blocks = append(blocks, entry{kind: entrySeam, text: seamMark, turn: turns})
	// The instruction is the FIRST of the person's messages and it is above the
	// marker — a pass never folds what somebody said — so the half below opens no
	// second one, and its turns carry on from the half above.
	shape.turn, shape.brief, shape.running = turns, false, true
	rest, more := a.replayBlocks(live, shape)
	return keepTail(append(blocks, rest...), tail), turns + more
}

// keepTail is the window a page holds: the last `tail` blocks, or every one of
// them when the caller set no budget.
func keepTail(blocks []entry, tail int) []entry {
	if tail > 0 && len(blocks) > tail {
		return blocks[len(blocks)-tail:]
	}
	return blocks
}

// replayBlocks turns a window of the record into blocks, on the shape the page
// asked for. It returns the blocks and how many of the person's messages were in
// them, which is what the caller needs to keep its own counter straight.
//
// IT IS THE ONE PLACE A TRANSCRIPT BECOMES BLOCKS. The opening replay, every
// backfill above it and every node's page go through here, so a conversation
// scrolled back into cannot be drawn differently from the same conversation
// opened onto — and a node's record cannot be drawn differently from either.
// A second shaping is what let a room reopen a correction as a fresh question
// for as long as rooms have existed (#252).
func (a *app) replayBlocks(entries []session.DisplayEntry, shape replayShape) ([]entry, int) {
	blocks := make([]entry, 0, len(entries))
	turn, turns := shape.turn, 0
	for _, e := range entries {
		text := strings.TrimSpace(e.Text)
		switch e.Role {
		case "user":
			// A LINE TYPED INTO THE TURN ABOVE IT IS NOT A QUESTION AND NEVER WAS
			// (steerelbow.go). The journal is the only thing that remembers the
			// difference — the message itself is an ordinary user message, because
			// that is what the model had to read it as — and a replay that drew it
			// as one would put a question in the transcript that nobody asked, in
			// the middle of the turn it was correcting.
			//
			// SO IT COMES BACK AS THE BLOCK IT WAS, IN THE PLACE IT WAS. The
			// journal keeps a steer where it happened — between the turn's own
			// messages — so replaying it in order is the surface agreeing with the
			// record rather than gathering the corrections back up under a question
			// they were said minutes after. The turn is NOT counted, because a steer
			// never opened one, and the mark's own instant and outcome come across
			// with it. A file written before steering existed carries no mark and
			// takes the ordinary road below, exactly as it always did.
			//
			// A REPLAYED CORRECTION IS SETTLED AND UNFADED. [steerElbow.landed] is
			// left zero on purpose: the journal keeps the instant the person SENT
			// the words and not the boundary the model was given them at, and a
			// fade measured from the wrong instant would light a row from yesterday
			// as though it had just landed.
			//
			// THE ENGINE'S OWN ACCOUNT OF WHERE IT LANDED comes back with it
			// ([session.SteerMark.Landing]) — the reply it cut, the bash it adopted
			// — so a mark the journal wrote as still waiting reads on the page
			// exactly as it read live. On a landed correction the clause is gone
			// already, because the block's position is what says where it went.
			if e.Steer != nil && text != "" {
				blocks = append(blocks, entry{
					kind: entrySteer, turn: turn,
					steer: &steerElbow{
						words: text, at: e.Steer.At, consumed: e.Steer.Consumed,
						landing: strings.TrimSpace(e.Steer.Landing),
					},
				})
				continue
			}
			// The pictures are part of what was said, so a message that was only
			// a picture is still a message: the markers alone are the line, and
			// only a message with neither words nor attachments is skipped.
			line, pictures := replayUserLine(text, e.ImageRefs, a.pal)
			if line == "" {
				continue
			}
			// The turn counter moves with the person's messages, exactly as it
			// does live: it is what groups a cluster and what ctrl+o folds.
			turn++
			turns++
			blocks = append(blocks, entry{
				kind: entryUser, text: line, turn: turn,
				// AND THE FIRST OF THEM IS THE INSTRUCTION THIS PAGE WAS GIVEN,
				// marked here because here is where it is knowable: it is the
				// message that opened the first turn, and everything the person says
				// after it on such a page is a correction to work already running.
				brief:    shape.brief && turns == 1,
				pictures: pictures, picturesHere: !a.hosted(),
			})

		case "assistant":
			if text == "" {
				continue // a step that only called tools; its calls follow
			}
			blocks = append(blocks, entry{
				kind: entryAssistant, text: text, turn: turn, settled: true,
				replyTags: append([]session.TaskReplyTag(nil), e.ReplyTags...),
			})

		case "tool":
			// A tool entry with no name is a RESULT message from the wire, not
			// a call. The cluster shows calls.
			if strings.TrimSpace(e.Tool) == "" {
				continue
			}
			// A CALL WITH NO RESULT UNDER IT HAS NOT COME BACK, and the record is
			// the only thing that can say so: internal/session writes the assistant
			// message BEFORE the batch runs, so work caught mid-call records the
			// asking and nothing else. The clock is NOT invented to go with it —
			// began stays zero, so the row shows no age (toolview.go) — because
			// nobody measured when it started.
			status := toolOK
			if shape.running && !e.Answered {
				status = toolRunning
			}
			blocks = append(blocks, entry{
				kind: entryTool, tool: e.Tool, text: e.Hint, turn: turn, status: status,
				// THE CALL'S OWN IDENTITY IS KEPT because it is what a live end has
				// to land on: a page drawn out of the record and then kept listening
				// pairs the end that arrives a second later with the row already
				// standing, or the same call is drawn twice.
				callID: e.CallID,
				// AND THE STEP'S OWN TITLE COMES BACK WITH IT. The narration and
				// the family it named were journaled against this call
				// (session's DisplayEntry.Caption), so a reopened conversation
				// draws the sentence the person was reading and the mark beside
				// it — rather than recomposing "running 1 command" out of the
				// tool names and demoting a `test` to a `run`. Both are empty on
				// every call that was not a batch's anchor and on every file
				// written before the line existed, which is the ordinary case
				// and falls back exactly as it always did.
				caption:    e.Caption,
				captionCat: e.CaptionCategory,
				// AND THE CALL'S OWN DURATION COMES BACK WITH IT. The live stream
				// wrote EventToolFinished onto the row; the journal kept the same
				// figure on a `took` line (session's DisplayEntry.Took), so a
				// page opened after the batch still says what each call took —
				// rather than drawing finished rows with no figure at all.
				ran: e.Took,
				// The detail is carried through UNPARSED, which is what makes a
				// replayed row the same row: everything the expansion shows — the
				// diff, the content preview, the highlighted command and its
				// output — is derived from these two fields at render time
				// (toolview.go), so a replayed call and a live one go through one
				// renderer and cannot disagree.
				detail: toolDetail{Args: e.Args, Output: e.Output},
			})

		case "note":
			if text == "" {
				continue
			}
			blocks = append(blocks, entry{
				kind: entryDivider, text: firstLine(text), turn: turn,
			})

		case "aside":
			if shape.brief && len(blocks) == 0 && canonicalTaskRequest(text) {
				turn++
				turns++
				blocks = append(blocks, entry{kind: entryUser, text: text, turn: turn, brief: true})
				continue
			}
			if text == "" {
				continue
			}
			// A LINE THE SESSION WROTE GOES IN THE SESSION'S OWN LANE — the dim
			// "· " row this surface says everything of its own in ([feed.note]) —
			// and NOT above a "›" as though somebody had typed it.
			//
			// The commonest one is the note that wakes a turn: work landed while
			// the room was idle, the session told the model, and the model
			// answered. Live, that note is never drawn as the person's words
			// (followup.go's [app.startFollow] deliberately writes no user line for
			// a woken turn) — and replayed, it WAS, because the journal keeps it as
			// the user-role message the model has to read. session marks the line
			// now (its sessionfile.go), so the two views of one conversation agree.
			// A note from a file written before the mark arrives as "user" and
			// draws exactly as it always did.
			blocks = append(blocks, entry{
				kind: entryNote, text: firstLine(text), turn: turn,
			})
		}
	}
	// THE WINDOW IS TAKEN LAST, so that the turn numbering and the count above are
	// done over the whole of what was read: a page showing the tail of a record
	// still knows which turn it is standing in.
	return keepTail(blocks, shape.tail), turns
}

// replayUserLine is a replayed message as the person sent it: their words, and
// the names of the pictures that went with them.
//
// It is [userLine]'s rule applied to what the journal kept — the same markers,
// the same hue, the same separator — because a message drawn one way when it is
// sent and another way when it is resumed is two records of one thing. The paths
// come from the journal (session's DisplayEntry.ImageRefs); their names stay in
// the marker above the same thumbnail the live message draws, while a full path
// remains a file identity rather than transcript prose.
func replayUserLine(text string, refs []string, pal palette) (string, []string) {
	pictures := make([]chip, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if base := filepath.Base(ref); ref != "" && base != "." && base != string(filepath.Separator) {
			// A chip is exactly what the live tray holds, so the markers are drawn
			// by the same function from the same shape (attach.go): the path in,
			// the base name out.
			pictures = append(pictures, chip{path: ref})
		}
	}
	return userLine(text, pictures, pal), chipPaths(pictures)
}

// replayInert reports whether one entry is a tool row with NOTHING behind it:
// a call replayed from a journal that carried neither its arguments nor its
// result.
//
// It is the one row on this surface that must not answer the pointer. Every
// other tool row expands into something — the diff, the content, the output, or
// at worst the honest "—" of a call that returned nothing — but a row from an
// older file has no payload at all, and a hover that brightens and a click that
// opens a blank are a surface promising an answer it does not have.
//
// It is derived rather than flagged, so nothing has to be remembered: a live
// call always arrives with its arguments (they are what the model sent, and even
// a no-argument call sends `{}`), so an unresolved-status row is never inert and
// an empty payload on a finished row means exactly one thing.
//
// The two places that must ask it are the row builder — a row that is inert
// takes hitNone rather than hitTool, which takes it out of hover, click and the
// ↑/↓ walk in one move — and [app.openTool], which is reachable by key.
func replayInert(e *entry) bool {
	return e != nil && e.kind == entryTool && !e.status.live() &&
		e.detail.Args == "" && e.detail.Output == ""
}
