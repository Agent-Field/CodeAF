package tui3

import (
	"path/filepath"
	"strings"
	"time"
)

// THE BOX BELONGS TO WHOEVER IT IS TALKING TO.
//
// There is one editor on this surface and there always has been: the same `› `
// under the conversation is the one a room steers a node with (room.go's header
// says so in as many words, and it is the right design — a person types into one
// box and learns one set of keys). What was missing is WHOSE WORDS ARE IN IT.
//
// A window with a half-written sentence to the model, a click on a task's row and
// one enter sent that sentence to the task. The box never changed, so the words
// never changed, and the only thing that moved was where enter pointed — which is
// the worst possible shape for this failure: nothing on screen was wrong, and a
// message the person had written for one reader was read by another.
//
// So the box is a VIEW onto one recipient's composer, and every recipient owns
// its own:
//
//	main            the conversation — the sentence for the model
//	task 7          the line you are steering into that node
//	an adaptive run the line the planner reads on its next call
//
// Opening a page stashes what the box was holding under the recipient it was
// holding it FOR, and lays out that page's own — its text, its caret, its compact
// paste chips and its attachment tray. Escape puts the first one back exactly.
// Nothing is ever carried across: a draft that has not been sent has not changed
// its mind about who it is for.
//
// ── WHERE THE STASH LIVES ──
//
// In this window while it is open, in the keeper when the conversation is put
// down (detach.go's [aside]), and ON DISK either way: every recipient's composer
// is written into this conversation's own record and read back to the same
// recipient of the same conversation (draftkeep.go). What is read off the SCREEN
// is nothing — the file is written from here, through [app.mainComposer] and
// [app.everyComposer], because a line typed into a task's page is otherwise the
// line the crash file keeps and the next launch pastes a message for a worker
// into the conversation.

// recipient names one reader of the box. Its zero value is the conversation
// itself, which is what makes "who has the box" answerable before anything has
// been opened.
//
// A NODE AND A RUN ARE DIFFERENT KINDS OF READER AND NEVER COLLIDE. A run's page
// is built with the node id zero on purpose (roomorch.go), so keying a run by its
// id STRING rather than by that zero is what keeps its draft out of the
// conversation's slot.
type recipient struct {
	task uint64
	run  string
	// guest is the OTHER conversation a page belongs to, and it is empty for
	// every page of this one.
	//
	// IT IS THE HOOK FOR A READ-ONLY PAGE ONTO SOMEBODY ELSE'S WORK. Navigation
	// opens a task page whose rows come from another conversation's journal, and
	// `task 7` there is not this conversation's task 7: one number, two pieces of
	// work, and a shared slot would be the misdelivery this file exists to end
	// with a page in between. The lane that opens such a page calls
	// [guestRecipient] with the session that page is reading, and everything else
	// — the stash, the record, the reunion — already keys on it.
	guest string
}

// mainRecipient is the conversation: the reader the box has when no page is open.
var mainRecipient = recipient{}

func taskRecipient(id uint64) recipient { return recipient{task: id} }

func runRecipient(id string) recipient { return recipient{run: id} }

// guestRecipient is a page of ANOTHER conversation, named by the transcript that
// page is reading. It is the one door the navigation lane needs: opening such a
// page calls `a.retargetComposer(guestRecipient(<that session>, <id>))` where a
// page of this conversation calls [taskRecipient], and closing it retargets back
// exactly as it already does.
func guestRecipient(session string, id uint64) recipient {
	if strings.TrimSpace(session) == "" {
		return taskRecipient(id)
	}
	return recipient{task: id, guest: filepath.Clean(session)}
}

// composerState is everything one recipient's box is holding — and it is the
// WHOLE of it, because a half-restored draft is its own defect: a person who
// comes back to their sentence with the caret at the end of it, or with the
// screenshot they attached gone, has been given something other than what they
// left.
type composerState struct {
	// box is the editor by value, with its own copy of the runes: the live
	// editor reuses its slice on every keystroke ([editor.setText] appends into
	// value[:0]), so a stash that shared it would be rewritten by the next
	// character typed into the page that replaced it.
	box editor
	// pastes are the documents the box's compact tokens stand for (pastechip.go).
	// They travel WITH the text because the token means nothing without them: a
	// draft restored without its chips would send `[paste 1 · 42 lines]` to a
	// model as though those were the words.
	pastes []pasteChip
	// chips are the pictures and files on the tray above the box (attach.go).
	chips []chip
	// sends are this recipient's messages that have left the box and not
	// settled, and the ones a crossing handed back for the person to send again
	// ([outboxSnapshot]). They ride with the composer because they are the same
	// fact as the text above them — words the person typed that have not landed
	// anywhere — and because a recipient's unsent line and its uncertain message
	// have to be picked up together or the same sentence appears twice.
	sends []outboxSnapshot
}

// empty reports whether this state is worth keeping at all. It asks about the
// RUNES and not about [editor.empty], which calls whitespace nothing: a caret
// parked inside the indentation somebody typed is still where they left it.
func (s composerState) empty() bool {
	return len(s.box.value) == 0 && len(s.pastes) == 0 &&
		len(s.chips) == 0 && len(s.sends) == 0
}

// ── THE OUTBOX SEAM ─────────────────────────────────────────────────────────
//
// A send is not a keystroke: the words leave the box, cross to another machine,
// and are answered later or not at all. The lane that owns that crossing owns
// the transport, the identity it mints and every word drawn about it; what it
// needs from HERE is the one thing this file is about — somewhere to put a
// person's sentence that belongs to the recipient it was typed at.
//
// So this is the whole interface, and it is deliberately four small functions:
//
//	[app.keepSend]       a message has left this recipient's box and not settled.
//	                     Written down under its own durable name, so a restart
//	                     can ask again as the SAME message rather than a second
//	                     one (draftkeep.go).
//	[app.dropSend]       it settled. Forget it.
//	[app.recoverDraft]   it failed, or nobody answered. Give the words back to
//	                     the recipient they were typed at.
//	[app.takeRecovered]  the words that were given back, for a page that is
//	                     drawing itself.
//
// AND THE ONE RULE [app.recoverDraft] KEEPS, which is the contract the sending
// lane relies on and the reason it is here rather than there:
//
//   - the sentence goes to the recipient it was typed at and to no other, open
//     page or not;
//   - a draft the person has typed since is NEVER overwritten — the recovered
//     words wait beside it instead, and are offered the next time that
//     recipient's box is empty;
//   - it is spelled as they typed it, compact tags and all, with the documents
//     behind them.

// The two states a written-down send can be in. They are STORAGE WORDS and are
// never drawn: what a person reads about a crossing is the engine's own
// sentence, which is the sending lane's law and not this file's.
const (
	draftSendCrossing   = "crossing"
	draftSendUnanswered = "unanswered"
)

// outboxSnapshot is one message as it left the box: everything needed to draw
// it, send it again under the same name, or give it back.
type outboxSnapshot struct {
	// scope and seq are the durable name — [session.SteerSource]'s two halves.
	// This package writes them down and never mints one.
	scope string
	seq   uint64
	// at is when it was handed over.
	at time.Time
	// state is one of the two words above.
	state string
	// line is what the person typed, tags and all; words is what the far side
	// reads, with the documents in place.
	line  string
	words string
	// pastes and chips are what the message was carrying, so that giving it back
	// gives back the whole message and not a tag with nothing behind it.
	pastes []pasteChip
	chips  []chip
}

// keepSend writes one uncertain message down against its recipient.
func (a *app) keepSend(who recipient, snap outboxSnapshot) {
	if strings.TrimSpace(snap.line) == "" {
		return
	}
	if snap.state == "" {
		snap.state = draftSendCrossing
	}
	a.atComposer(who, func(state *composerState) {
		state.sends = append(withoutSend(state.sends, snap.scope, snap.seq), snap)
	})
}

// dropSend forgets one: it landed, or its answer arrived.
func (a *app) dropSend(who recipient, scope string, seq uint64) {
	a.atComposer(who, func(state *composerState) {
		state.sends = withoutSend(state.sends, scope, seq)
	})
}

func withoutSend(sends []outboxSnapshot, scope string, seq uint64) []outboxSnapshot {
	out := sends[:0]
	for _, send := range sends {
		if send.scope == scope && send.seq == seq {
			continue
		}
		out = append(out, send)
	}
	return out
}

// recoverDraft gives a failed or unanswered message back to the recipient it was
// typed at, and reports whether it went straight into that recipient's box.
//
// A false is not a failure: it means the person has been typing there since, so
// the sentence is held beside their new one and offered the moment that box is
// empty again ([app.offerRecovered]). Losing the second sentence to save the
// first would be the same defect one layer along.
func (a *app) recoverDraft(who recipient, snap outboxSnapshot) bool {
	if strings.TrimSpace(snap.line) == "" {
		return false
	}
	landed := false
	a.atComposer(who, func(state *composerState) {
		state.sends = withoutSend(state.sends, snap.scope, snap.seq)
		if len(state.box.value) > 0 {
			snap.state = draftSendUnanswered
			state.sends = append(state.sends, snap)
			return
		}
		state.box.value = []rune(snap.line)
		state.box.cursor = len(state.box.value)
		state.box.demotedTags = nil
		state.pastes = append([]pasteChip(nil), snap.pastes...)
		for _, held := range snap.chips {
			attachChipTo(&state.chips, held)
		}
		landed = true
	})
	a.touch()
	return landed
}

// takeRecovered is the newest sentence handed back for one recipient, taken off
// its composer. A page drawing itself asks once.
func (a *app) takeRecovered(who recipient) (outboxSnapshot, bool) {
	var found outboxSnapshot
	held := false
	a.atComposer(who, func(state *composerState) {
		for i := len(state.sends) - 1; i >= 0; i-- {
			if state.sends[i].state != draftSendUnanswered {
				continue
			}
			found, held = state.sends[i], true
			state.sends = append(state.sends[:i], state.sends[i+1:]...)
			return
		}
	})
	return found, held
}

// offerRecovered puts a handed-back sentence into the box of the recipient in
// front, if that box is empty. It is called on the way INTO a page, which is the
// moment a person can see it happen and the only moment it cannot overwrite
// anything: they have not typed here yet.
func (a *app) offerRecovered() {
	if len(a.input.value) > 0 {
		return
	}
	snap, held := a.takeRecovered(a.composerOwner)
	if !held {
		return
	}
	a.input.setText(snap.line)
	a.pastes = append([]pasteChip(nil), snap.pastes...)
	for _, chip := range snap.chips {
		attachChipTo(&a.chips, chip)
	}
}

// liveComposer reads the box as it stands, copying everything a keystroke could
// write through.
func (a *app) liveComposer() composerState {
	return composerState{
		box: editor{
			value:       append([]rune(nil), a.input.value...),
			cursor:      a.input.cursor,
			demotedTags: append([]segment(nil), a.input.demotedTags...),
		},
		pastes: append([]pasteChip(nil), a.pastes...),
		chips:  append([]chip(nil), a.chips...),
		sends:  append([]outboxSnapshot(nil), a.sends...),
	}
}

// putComposer lays one recipient's state out in the box. The caret is CLAMPED
// rather than trusted: every other door that replaces the whole draft clamps it
// ([editor.rewrite]), and a stash restored into an editor is the same act.
func (a *app) putComposer(state composerState) {
	a.input.value = append(a.input.value[:0], state.box.value...)
	a.input.cursor = max(0, min(state.box.cursor, len(a.input.value)))
	a.input.demotedTags = state.box.demotedTags
	a.pastes = state.pastes
	a.chips = state.chips
	a.sends = state.sends
}

// keepComposer puts one recipient's state away, and FORGETS AN EMPTY ONE. The
// map is a record of unfinished sentences; a slot per task somebody merely
// looked at would be a map that grows with navigation and never shrinks.
func (a *app) keepComposer(who recipient, state composerState) {
	if state.empty() {
		delete(a.composers, who)
		return
	}
	if a.composers == nil {
		a.composers = map[recipient]composerState{}
	}
	a.composers[who] = state
}

// retargetComposer is the whole of this file: the box stops speaking to one
// reader and starts speaking to another, and each keeps its own words.
//
// IT IS KEYED ON THE OWNER AND NEVER ON WHAT IS ON SCREEN. The stash goes under
// [app.composerOwner] — whoever the box was talking to — rather than under
// anything derived from the room, so a page that was closed before this is
// called, or one whose kind changed after it was built (a run's, roomorch.go),
// cannot be stashed under a name that is not its own.
func (a *app) retargetComposer(to recipient) {
	if a.composerOwner == to {
		return
	}
	// A HISTORY WALK ENDS AT THE DOOR, with the person's own line given back
	// first (recall.go). The walk holds a copy of the draft it interrupted, and
	// that draft belongs to the page being left — carried across, an esc pressed
	// on the next page would put one recipient's sentence into another's box,
	// which is the exact failure this file exists to prevent.
	if a.recalling() {
		a.recallCancel()
	}
	// AND SO DOES THE PASTE EDITOR, for the reason every mode on this surface is
	// dropped at a switch (detach.go's [app.closeForSwitch]): it is an editor over
	// ONE of the chips being stashed, and it writes each keystroke straight
	// through to that chip, so closing it loses nothing at all.
	a.pasteEdit = pasteEditor{}
	// AND THE KEYSTROKE FOLD WATCHING FOR A DROPPED PATH (dropkeys.go). It holds a
	// RANGE in the box and the box it is watching by POINTER — and every
	// recipient's words are drawn through the same editor, so the pointer cannot
	// tell that the sentence under the fold has just been replaced. Left open, a
	// drop typed character by character in the conversation would be converted
	// inside whatever page was opened before enter was pressed.
	a.drop.open = false
	a.keepComposer(a.composerOwner, a.liveComposer())
	a.composerOwner = to
	kept := a.composers[to]
	// The live box is now the only copy that matters. Leaving the stash behind
	// would be a second answer to "what is this recipient holding", and the two
	// would drift the moment a key was pressed.
	delete(a.composers, to)
	a.putComposer(kept)
	// AND A SENTENCE A CROSSING HANDED BACK IS OFFERED HERE, on the way in, which
	// is the one moment it cannot overwrite anything ([app.offerRecovered]).
	a.offerRecovered()
	// The typed overlays follow the draft, and the draft has just been replaced
	// (input.go): a command list left open over somebody else's sentence is a
	// list whose enter would commit a completion of words that are no longer
	// there.
	a.closeLists()
	a.touch()
}

// mainComposer is the CONVERSATION's box, wherever it is living right now — in
// the editor when no page is open, and in the stash while one is.
//
// EVERY DOOR THAT SPEAKS FOR MAIN ASKS THIS AND NEVER THE EDITOR. The draft file,
// the sentence written on the way out and the tray a refused message hands back
// are all facts about the conversation, and reading them off the screen while a
// task's page is open is how a steering line ends up in the conversation's own
// crash file.
func (a *app) mainComposer() composerState { return a.composerAt(mainRecipient) }

// composerAt is any recipient's composer, wherever it is living — in the editor
// when that recipient has the box, and in the stash otherwise.
func (a *app) composerAt(who recipient) composerState {
	if who == a.composerOwner {
		return a.liveComposer()
	}
	return a.composers[who]
}

// atComposer changes one recipient's composer wherever it is living, and is the
// writing half of [app.composerAt].
//
// EVERYTHING THAT ACTS ON A RECIPIENT THAT MAY NOT BE IN FRONT GOES THROUGH IT:
// the tray a refused message hands back, a crossing that failed while the person
// was reading another page, the draft file's own reading of the conversation.
// Reading those off the screen is how one recipient's words end up in another's
// box, which is the whole of what recipient.go exists to prevent.
func (a *app) atComposer(who recipient, change func(*composerState)) {
	state := a.composerAt(who)
	change(&state)
	if who == a.composerOwner {
		a.putComposer(state)
		return
	}
	a.keepComposer(who, state)
}

// atMainComposer is that, said about the conversation.
func (a *app) atMainComposer(change func(*composerState)) {
	a.atComposer(mainRecipient, change)
}

// mainDraftText is the conversation's unsent sentence, and nothing else's.
func (a *app) mainDraftText() string {
	main := a.mainComposer()
	return main.box.String()
}

// composersAside is every PAGE's composer, for a conversation being put down —
// the stash plus the live box when a page is the one holding it.
//
// MAIN IS DELIBERATELY NOT IN IT. The conversation's own sentence travels as
// [aside.draft], which is that string plus everything still parked behind a turn
// (quitarm.go's [app.leavingDraft]), and having it in two places on one aside
// would be two answers to what the person was typing.
func (a *app) composersAside() map[recipient]composerState {
	out := map[recipient]composerState{}
	for who, state := range a.everyComposer() {
		if who == mainRecipient {
			continue
		}
		out[who] = state
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// restoreComposers hands a kept conversation its pages' drafts back, with the
// box itself on MAIN: the room this conversation had open is reopened by
// [app.restoreAside] a moment later, through the same door a rail click uses, and
// that door is what lays the room's own words back out.
func (a *app) restoreComposers(kept map[recipient]composerState) {
	a.composerOwner = mainRecipient
	if len(kept) == 0 {
		a.composers = nil
		return
	}
	a.composers = map[recipient]composerState{}
	for who, state := range kept {
		if who == mainRecipient {
			// A stash that named main would overwrite the sentence the person is
			// carrying. It cannot happen through [app.composersAside]; it is
			// refused here because this door is what makes that true.
			continue
		}
		a.composers[who] = state
	}
}

// forgetComposers drops every page's draft, and is what a conversation being
// REPLACED owes: a task id means something only inside the graph that minted it,
// so a stash carried into the next conversation would be a sentence addressed to
// a node this one has never heard of (detach.go's [app.clearConversation] states
// the same law about the rail, the folds and the lane generations).
func (a *app) forgetComposers() {
	a.composers = nil
	a.composerOwner = mainRecipient
	a.sends = nil
	// AND WHAT WAS BEING CARRIED FOR SOMEBODY ELSE IS PUT DOWN WITH THE FILE IT
	// CAME OUT OF (draftkeep.go's [app.readKeptElsewhere]). It is still on that
	// window's own record; the conversation arriving is about to be written to a
	// different name, and carrying it there would be copying one conversation's
	// unsent lines into another conversation's file.
	a.keptElsewhere, a.keptFrom = nil, ""
}
