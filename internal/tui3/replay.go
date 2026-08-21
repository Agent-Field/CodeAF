package tui3

import (
	"path/filepath"
	"strings"

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
	// THE BACKFILL'S BOOKKEEPING IS SET HERE, on every path including the one
	// with nothing to replay. A fresh session that inherited a mark from the
	// conversation before it would offer to scroll back into somebody else's
	// history — so the mark is cleared first and earned second.
	a.replayFrom, a.replayFloor = 0, a.turn
	if a.agent == nil {
		return
	}
	all := a.agent.Transcript()
	from := 0
	if len(all) > replayTail {
		from = len(all) - replayTail
	}
	blocks, turns := a.replayBlocks(all[from:], a.turn)
	a.entries = append(a.entries, blocks...)
	a.turn += turns
	a.replayFrom = from
	a.touch()
}

// moreHistory reports whether the conversation on screen starts part-way
// through — whether the journal holds anything ABOVE the first block drawn.
//
// It is the one question three separate things ask: the scroll, which backfills
// rather than stopping; the marker at the top of the frame, which says so; and
// the tests, which is how the two stay one answer.
func (a *app) moreHistory() bool { return a.agent != nil && a.replayFrom > 0 }

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
	all := a.agent.Transcript()
	to := a.replayFrom
	if to > len(all) {
		// A transcript that got SHORTER than the mark is one a rewind cut under
		// us. There is nothing honest to hand up; the next rebuild sets the mark
		// again from what is actually there.
		a.replayFrom = 0
		return false
	}
	from := 0
	if to > replayTail {
		from = to - replayTail
	}
	if from >= to {
		a.replayFrom = 0
		return false
	}
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
	blocks, turns := a.replayBlocks(all[from:to], 0)
	shift := a.replayFloor - turns
	for i := range blocks {
		blocks[i].turn += shift
	}
	// AND EVERY POSITION THIS SURFACE HOLDS IN THE BLOCK LIST MOVES WITH IT.
	a.shiftBlockIndices(len(blocks))
	a.entries = append(blocks, a.entries...)
	a.replayFrom, a.replayFloor = from, shift
	// The pointer was over a row of a list that has just been rebuilt around it,
	// which is the same claim [app.dropHover] makes wherever the rows are
	// replaced.
	a.dropHover()
	a.touch()
	return true
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
