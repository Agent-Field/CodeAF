package tui3

// THE LIVE STATE, AND THE ONE DOOR OUT OF IT.
//
// Every transcript on this surface points at the block it is streaming into —
// [feed.live] and [feed.think] out in the conversation, [taskRoom.live] and
// [taskRoom.think] on a node's page, [homeExchange.live] in an errand's pane —
// and the block under that pointer is the only one on its list that is still
// MOVING. It grows on every delta, its markdown is promoted a paragraph at a
// time ([promoteBlock]), and its live edge is walked onto the page a few
// characters per frame (reveal.go).
//
// A BLOCK STOPS PACING THE MOMENT IT STOPS BEING LIVE.
//
// That law is what this file exists to keep, and it is not tidiness. The live
// edge is a PROMISE that the next few frames will finish writing the bytes, and
// the only blocks the frame clock walks are the ones those pointers name
// ([app.liveRevealing]). So a block that left the live state with an unread
// remainder has nobody left to finish it: it freezes mid-word for the rest of
// the session, and no later frame ever puts it right because no later frame can
// see it. Every reading taken off the block afterwards inherits that truncation:
// [promoteBlock] and [app.assistantRows] both cut on the DRAWN prefix while a
// block is unsettled, so the page goes on drawing the twelve bytes the edge had
// reached at the moment the pointer went.
//
// SO THERE IS ONE DOOR AND NOT SIX. A turn settling, a person's line landing, a
// cut stream, a thought folding, a node's page finishing and an errand's reply
// closing are six events and one act. Each of them used to spell the act out by
// hand — two or three field writes in the file that happened to own the event —
// which is exactly the shape of defect #178 closed in the transcript once
// already, and exactly the shape that came back the moment the blocks gained a
// third field to put right. [TestNothingLeavesTheLiveStateOutsideTheSeam] is
// what stops the seventh door being written by hand.

// blockAt is the bounds check every door needs and none of them should spell.
// A pointer into a list that has been replaced, or one that was never set, is a
// door that does nothing rather than a panic.
func blockAt(entries []entry, at int) *entry {
	if at < 0 || at >= len(entries) {
		return nil
	}
	return &entries[at]
}

// settleBlock ends a block IN PLACE: it is a finished document now, whatever
// the pointer that named it goes on to do. It is the half of the door that a
// thought which folds without being let go of needs on its own
// ([feed.settleThought]).
//
// THE STALE FLAG IS PART OF THE SETTLE — [feed.closeLive] states why: the rows
// a block was drawn with mid-stream are handed back by [app.entryRows] until
// something says they are wrong, so without it a settled answer keeps its
// unrendered markdown and the live ink of its growing edge.
//
// AND SO IS THE EDGE. `edge = 0` is [revealedText]'s "not pacing" rather than
// `len(text)`, and the difference is not cosmetic: a settled block can still be
// GROWN — a late reply tag, the text a note folds into an answer that has
// already ended ([app.replyInto]) — and a cursor parked at the old length would
// start cutting the block off again at exactly the byte it ended on. Zero can
// never be a prefix of anything.
//
// It hands the block back so a caller with one more fact to state about it does
// not have to reach for the index a second time.
func settleBlock(e *entry) *entry {
	if e == nil {
		return nil
	}
	e.settled, e.stale, e.edge = true, true, 0
	return e
}

// leaveLive is the door itself: the block the pointer names is settled, and the
// pointer is let go. Every surface's "the stream ended" comes through here.
func leaveLive(entries []entry, at *int) *entry {
	e := abandonLive(entries, at)
	return settleBlock(e)
}

// abandonLive lets go of the pointer WITHOUT settling the block, and it has
// exactly two kinds of caller: [app.dropLive], which is throwing the text away
// rather than finishing it, and the wholesale resets that replace the entry
// list out from under the pointer ([app.rebuildTranscript],
// [app.clearConversation]). THE EDGE STILL SNAPS, because the law is about
// pacing and not about settling: a block nobody is walking must not be drawn as
// a prefix, whether it ended or was abandoned.
func abandonLive(entries []entry, at *int) *entry {
	if at == nil {
		return nil
	}
	e := blockAt(entries, *at)
	*at = -1
	if e != nil {
		e.edge = 0
	}
	return e
}

// settleReply is the door said about an errand pane's rows, which carry the
// same two facts on a narrower struct ([exchangeRow]). It is one function
// rather than the five assignments [homeExchange.closeReply]'s own note argues
// against, for that note's reason.
func settleReply(rows []exchangeRow, at *int) {
	if at == nil {
		return
	}
	if *at >= 0 && *at < len(rows) {
		rows[*at].settled, rows[*at].edge = true, 0
	}
	*at = -1
}
