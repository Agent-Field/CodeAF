package tui3

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
	blocks, turns := a.replayBlocks(all[from:], a.turn)
	a.entries = append(a.entries, blocks...)
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
	blocks, turns := a.replayBlocks(entries, 0)
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
	for i := range a.asks {
		move(&a.asks[i].entry)
	}
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

// replayBlocks turns a window of the journal into blocks, numbering the turns
// from `turn`. It returns the blocks and how many of the person's messages were
// in them, which is what the caller needs to keep its own counter straight.
//
// IT IS THE ONE PLACE A TRANSCRIPT BECOMES BLOCKS. The opening replay and every
// backfill above it go through here, so a conversation scrolled back into
// cannot be drawn differently from the same conversation opened onto.
func (a *app) replayBlocks(entries []session.DisplayEntry, turn int) ([]entry, int) {
	blocks := make([]entry, 0, len(entries))
	turns := 0
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
			// So it hangs off the block above it, the turn is NOT counted, and the
			// mark's own instant and outcome come across with it. A file written
			// before steering existed carries no mark and takes the ordinary road
			// below, exactly as it always did.
			if e.Steer != nil && text != "" {
				at := lastTrunk(blocks)
				if at < 0 {
					// THE WINDOW OPENED PART-WAY THROUGH A STEERED TURN and the
					// question is above its top. The corrections are still drawn —
					// they are what the person said — hanging from a trunk with no
					// words of its own rather than promoted into questions of their
					// own, which is the one thing they are not (render.go's
					// entryUser draws a wordless trunk as its elbows alone).
					turn++
					turns++
					blocks = append(blocks, entry{kind: entryUser, turn: turn})
					at = len(blocks) - 1
				}
				blocks[at].steers = append(blocks[at].steers, steerElbow{
					words: text, at: e.Steer.At, consumed: e.Steer.Consumed,
				})
				continue
			}
			// The pictures are part of what was said, so a message that was only
			// a picture is still a message: the markers alone are the line, and
			// only a message with neither words nor attachments is skipped.
			line := replayUserLine(text, e.ImageRefs, a.pal)
			if line == "" {
				continue
			}
			// The turn counter moves with the person's messages, exactly as it
			// does live: it is what groups a cluster and what ctrl+o folds.
			turn++
			turns++
			blocks = append(blocks, entry{kind: entryUser, text: line, turn: turn})

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
			blocks = append(blocks, entry{
				kind: entryTool, tool: e.Tool, text: e.Hint, turn: turn, status: toolOK,
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
			if text == "" {
				continue
			}
			// A LINE THE SESSION WROTE GOES IN THE SESSION'S OWN LANE — the dim
			// "· " row this surface says everything of its own in ([app.note]) —
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
	return blocks, turns
}

// lastTrunk is the person's own block a replayed correction hangs from: the
// newest one in what has been rebuilt so far (steerelbow.go). It is -1 when the
// window has not drawn one yet, which is a window that opened part-way through
// a steered turn and is the case the caller handles.
func lastTrunk(blocks []entry) int {
	for at := len(blocks) - 1; at >= 0; at-- {
		if blocks[at].kind == entryUser {
			return at
		}
	}
	return -1
}

// replayUserLine is a replayed message as the person sent it: their words, and
// the names of the pictures that went with them.
//
// It is [userLine]'s rule applied to what the journal kept — the same markers,
// the same hue, the same separator — because a message drawn one way when it is
// sent and another way when it is resumed is two records of one thing. The paths
// come from the journal (session's DisplayEntry.ImageRefs); the NAME is what is
// drawn, for the reason [chipMarkers] states: a terminal cell is not a place to
// show a picture, and a full path is not a thing anybody reads.
func replayUserLine(text string, refs []string, pal palette) string {
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
	return userLine(text, pictures, pal)
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
