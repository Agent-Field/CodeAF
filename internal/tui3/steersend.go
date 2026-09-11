package tui3

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// SENDING A CORRECTION IS A CROSSING, AND A CROSSING IS NOT A KEYSTROKE.
//
// Enter in a room used to CALL the engine from inside the update loop
// (room.go's [app.steer], as it was): the surface asked the far machine to take
// the words and sat in [app.Update] until it answered. On this process's own
// engine that is microseconds. Over an ssh pipe to a machine that is busy, or
// paused, or gone, it is up to the ordinary call deadline — TEN SECONDS — and
// for that whole time the terminal draws nothing, esc does not leave the room,
// ctrl+c does not reach the door and the arrows do not move: the one moment a
// person most wants to be able to walk away is the moment the surface stops
// answering the keyboard. That is the defect this file ends.
//
// So the send LEAVES THE EVENT LOOP. Enter draws the person's words at once,
// hands the crossing to a command, and comes straight back to the keyboard; the
// engine's answer arrives later as an ordinary message ([steerSentMsg]) and
// settles the row it belongs to. Nothing else about the act changes — the same
// door, the same receipt, the same sentences.
//
// ── THE FOUR THINGS THE ROW SAYS, AND THREE OF THEM ARE THE ENGINE'S ────────
//
//	sending      the words have left this surface and nothing has answered.
//	             One of the two things this surface says by itself.
//	the receipt  the engine took them, in the ENGINE'S sentence: `delivered`,
//	             `it was waiting on its pieces — your line wakes it`, or one of
//	             the two keepings — `held on the task's record …` — which is the
//	             engine saying nobody has READ them yet and the landing may not
//	             publish over them (internal/session's [session.SteerReceipt]).
//	             RECEIVED AND APPLIED ARE THAT VALUE'S OWN DISTINCTION and this
//	             surface neither adds to it nor collapses it: `delivered` means
//	             a reader has the words, `held on the task's record` means the
//	             record has them and nobody has read them yet.
//	nobody
//	answered     the crossing was asked for and got no answer, twice where the
//	             engine keeps message names. The row KEEPS THE WORDS and stays
//	             offering to ask again; it is not a fade and not a spinner,
//	             because nothing is happening.
//	nothing      once a receipt is old. The words stay; the clause goes
//	             (steerelbow.go's fade).
//
// WHAT THIS SURFACE MAY NEVER DRAW IS A STATE OF ITS OWN INVENTION. "sent" over
// a send nothing has answered would be exactly the lie the receipt exists to
// prevent, one layer further out.
//
// ── AND A SEND HAS A NAME, SO A LOST ANSWER IS NOT A LOST SENTENCE ─────────
//
// The failure that has no good answer without one: the words cross, the engine
// takes them, and the ANSWER is lost — the link died, the deadline ran out. The
// surface then holds a sentence it cannot say arrived and cannot say did not,
// and both moves are wrong. Send it again and the worker is corrected twice;
// drop it and the person's words are gone with a spinner still on the row.
//
// So every send carries an identity ([session.SteerSource]): a scope minted
// once per life of this surface, and a number counting this surface's sends.
// An engine that keeps them answers a repeat with the receipt already on its
// record and delivers nothing ([session.SteerReceipt.Again]), which is what
// makes asking again safe — including after a reconnect, after the engine
// restarted, and after the task the words were for has finished, because the
// identity is written on the node's record beside the direction and read back
// with it (internal/session's task_store.go).
//
// AND AN UNCERTAIN SEND IS KEPT AS A SEND, NEVER DEMOTED TO TEXT. It stays in
// [steerOutbox.unsure] with the identity it already had, so `r` on the question
// it raises is A RETRY OF THAT SEND and not a new one. Handing the words back
// to the box instead would throw the name away, and the next enter would mint a
// new one — which is the double-delivery this whole mechanism exists to stop.
//
// TWO INTENTIONAL SENDS OF ONE SENTENCE ARE TWO SENDS. The identity is the
// EVENT and never the words — "try it again" typed twice means something the
// first one did not — so each press of enter takes the next number and lands as
// its own direction, uncertain sends waiting beside it or not.
//
// AND AN ENGINE THAT DOES NOT KEEP NAMES IS NEVER ASKED TWICE BY THIS SURFACE.
// The capability is asserted before anything is sent
// ([session.Agent.SteerRepeatKnown], answered for the far machine by
// internal/remote's client): where it is absent the automatic second ask does
// not happen at all, and the question the row raises says in as many words that
// asking again may arrive twice. A person choosing that with the sentence in
// front of them is a decision; a surface doing it quietly is a defect.
//
// ── WHO THE SEND BELONGS TO, AND WHY IT IS NOT A NUMBER ────────────────────
//
// A task id is a number inside ONE conversation's graph. A window holds several
// conversations (the keeper), and switching between them replaces the engine
// under the surface while the numbers start again from 1 — so a send queued
// behind another, released after a switch, would once have gone to whatever
// task 7 means over there. Two things make that impossible here:
//
//   - THE ENGINE IS CAPTURED AT THE KEYPRESS ([steerSend.door]) and never
//     resolved again. A queued send crosses to the engine it was addressed to,
//     whatever is on screen when it goes.
//   - EVERY MAP IS KEYED BY OWNER AND TASK ([steerAddress]), never by the task
//     number alone, so a queue, an answer and a kept draft all belong to the
//     conversation they were made in and are found again when it comes back.
//   - AND THE CONVERSATION'S NAME CROSSES WITH THE SEND
//     ([session.SteerSource.Conversation]), because holding the door is not
//     enough over a wire: `/resume` and `/new` swap the conversation behind the
//     SAME remote handle (cmd/aforge's chatv3_host.go returns the same agent),
//     so a captured pointer would still deliver to the replacement. The engine
//     compares the name against the conversation actually open, under the lock
//     the swap happens beneath, and refuses rather than re-aiming
//     ([session.ErrNotThatConversation]).
//
// ── ONE SEND AT A TIME PER NODE ────────────────────────────────────────────
//
// Sends to one node go one after another, in the order they were typed. It is
// not a throughput decision — it is the engine's own ordering law: a task
// refuses a message older than one it already holds from the same life
// (internal/session's [taskAssignment.hear]), so two sends racing on one link
// could arrive reversed and the second one typed could refuse the first. The
// queue is per address, so two rooms and two conversations never wait on each
// other.

// steerSendingWord is the clause a correction wears while it is crossing and
// nothing has answered. It is a word rather than a bare spinner for
// [steerPendingWord]'s reason.
const steerSendingWord = "sending"

// steerLostWord is the clause an uncertain send keeps. It states the two facts
// a person needs and refuses to guess between them: nobody knows whether the
// words arrived, and they have not been thrown away.
const steerLostWord = "no answer — it is not known whether this arrived"

// steerElsewhereWord is the clause a send wears when the engine refused it
// because that conversation is no longer the one open there. Nothing was
// delivered, and the words are still waiting on this page for the conversation
// they were written for.
const steerElsewhereWord = "not sent — that conversation is not open"

// steerElsewhereAway is what the conversation says about the same refusal when
// the person is on another page.
const steerElsewhereAway = " was not corrected — that conversation is not open, and your words are kept on its page"

// steerElsewhereAsk is that refusal's own question over the composer, in place
// of [steerLostAsk]: nobody failed to answer here, the engine said no.
const steerElsewhereAsk = " was not corrected — "

// The question an uncertain send raises over the composer (room.go draws it in
// the guard's own row). Two spellings, because what `r` costs depends on the
// engine at the other end.
const (
	steerLostAsk   = " got no answer — "
	steerLostRetry = " ask again (nothing is sent twice) · "
	steerLostRisk  = " send it again (it may arrive twice) · "
	steerLostLeave = " leave it here"
	// The two lines the CONVERSATION says about a send whose page the person has
	// walked away from. They are two sentences because they are two different
	// facts, and telling them apart is the whole of this file: one is the task
	// declining the words, and the other is nobody knowing.
	steerKeptForPage = " would not take your correction — it is kept on its page"
	steerUnsureAway  = " gave no answer — your correction is still on its page, unresolved"
)

// steerKeptNowhere is what ANY of these refusals says when the conversation
// those words belong to is not in this window at all — closed here, or taken
// over by another window. There is no page to keep them on and no box to put
// them in, so the sentence itself is said here rather than lost.
const steerUnsureClosed = " gave no answer — delivery is unknown; reopen its conversation to ask again"

const steerKeptNowhere = " was not corrected, and its conversation is not open here · these words are only in this line: "

// taskSteerIdentityDoor is the engine door that can be asked TWICE for one
// send, and it is TWO METHODS ON PURPOSE.
//
// A CAPABILITY IS ASSERTED, NEVER ASSUMED (this house's law), and here the
// assumption would be dangerous rather than merely wrong: an engine that takes
// an identity and does not keep it looks exactly like one that does, right up
// until a lost answer is asked again and the worker reads one correction twice.
// So the door answers for itself, before anything is sent. This process's own
// engine says yes because it IS the record; a client says what the machine at
// the other end said in its welcome (internal/remote).
type taskSteerIdentityDoor interface {
	SteerTaskFrom(id uint64, text string, from session.SteerSource) (session.SteerReceipt, error)
	SteerRepeatKnown() bool
}

// steerDoor is ONE ENGINE'S EAR, taken at the instant enter was pressed and
// carried with the send.
//
// IT IS A VALUE AND NOT A LOOKUP, and that is the whole of the wrong-engine
// law above: a send released from a queue after the window moved to another
// conversation must reach the machine it was typed at, not whatever is
// answering now.
type steerDoor struct {
	// identity is the door that takes the send's name with it, when this engine
	// has one; plain is the door every engine has.
	identity taskSteerIdentityDoor
	plain    taskSteerDoor
	// repeat says this engine recognises one send arriving twice. It is read
	// ONCE, here, and never again: after the link dies there is nobody to ask.
	repeat bool
}

// live reports whether there is any ear at all.
func (d steerDoor) live() bool { return d.identity != nil || d.plain != nil }

// steerDoorNow is the engine under this surface right now, as one value to
// carry. It is the ONLY place a send's door is resolved.
//
// TWO PAGES HAVE NO EAR AT ALL, and both refusals are here rather than at the
// keypress so that nothing downstream has to remember them:
//
//   - A GUEST PAGE. Its rows come from another conversation's journal
//     (recipient.go's [guestRecipient]), and its `task 7` is not the `task 7`
//     this window's engine knows. A door taken here would deliver a correction to
//     a stranger's node with the same number, which is this wave's own defect one
//     page further out. It is a READ-ONLY DEFENCE and not the navigation lane's
//     hook: it refuses whether or not that lane has landed.
//   - A CONVERSATION THIS SURFACE CANNOT WRITE A SEND DOWN FOR. A send has to be
//     on disk before it crosses ([app.keepSendFirst]), and a window with no
//     transcript to be named after, or no record to be written into, has nowhere
//     to put one — draftkeep.go writes main and nothing else under a window's own
//     name, and a save with no path succeeds by doing nothing. Crossing on the
//     strength of that would be a correction this surface could never ask about
//     again and could never prove it had made. The words stay in the box and the
//     room says why.
func (a *app) steerDoorNow() steerDoor {
	if a.composerOwner.guest != "" || !a.steerDurable() {
		return steerDoor{}
	}
	var door steerDoor
	if identity, ok := a.agent.(taskSteerIdentityDoor); ok {
		door.identity = identity
		door.repeat = identity.SteerRepeatKnown()
	}
	if plain, ok := a.taskSteerDoors(); ok {
		door.plain = plain
	}
	return door
}

// steerDurable reports whether a send made here could be WRITTEN DOWN before it
// crossed: a conversation this build can name, and a record path to name it in.
// Both, because either one missing turns [app.keepSendFirst] into a save that
// succeeds by doing nothing (draftkeep.go's [draftSave.commit]).
func (a *app) steerDurable() bool {
	return namedConversation(a.draftKeepOwner()) && draftKeepPath(a.draftFile) != ""
}

// canSteerTask reports whether this surface has any door onto a node's ear at
// all. It is the question the room's enter asks before it takes a person's
// sentence away from them.
func (a *app) canSteerTask() bool { return a.steerDoorNow().live() }

// steerRefusal is WHY this page cannot take a correction, in the words that are
// true of it. Three doors are closed by [app.steerDoorNow] and they are closed
// for three different reasons; one sentence for all of them would tell two
// people out of three something that is not so about their own conversation.
func (a *app) steerRefusal() string {
	switch {
	case a.composerOwner.guest != "":
		return steerGuestWord
	case !a.steerDurable():
		return steerUnnamedWord
	}
	return roomUnavailableRefusal.line()
}

const (
	// steerGuestWord is a page of another conversation, open here to be read.
	steerGuestWord = "this page belongs to another conversation · it can be read here, and corrected from the conversation it is in"
	// steerUnnamedWord is a conversation with no transcript of its own: there is
	// nowhere to write a correction down, and one that cannot be written down is
	// one this surface could never ask about again.
	steerUnnamedWord = "this conversation has no transcript of its own yet, so a correction cannot be kept — and one that cannot be kept is not sent"
)

// steerAddress is WHO A SEND IS FOR: one task, inside one conversation.
//
// THE OWNER IS A WHOLE IDENTITY AND NOT A PATH. A transcript path is not unique
// by itself: the same `/srv/app/.aforge/…jsonl` names different work on two
// machines, so the machine and the workspace are part of it (host.go's own
// reading of what a hosted conversation is). A conversation with no transcript
// has no such identity and no sender either ([app.steerDoorNow]), so no address
// is ever built for one.
type steerAddress struct {
	owner string
	task  uint64
}

// steerOwner is this conversation's identity for the maps below, and IT IS THE
// DRAFTS LANE'S OWN IDENTITY (draftkeep.go's [app.draftKeepOwner]) rather than a
// second spelling of the same idea.
//
// THEY HAVE TO BE ONE STRING. A send is written into the recipient's own durable
// record, whose slots are stamped with that identity; two namings that agree
// almost always would make the seam silently do nothing on the day they differed
// — an unclean workspace path, a conversation with no transcript — and a send
// that is written down under a name nobody reads back is a send that was never
// written down at all.
func (a *app) steerOwner() string { return a.draftKeepOwner() }

// steerOwnerOf turns a conversation PATH — which is what a record on disk
// carries — into this surface's owner identity, and refuses any path that is
// not the conversation open here. It is the one place the two namings meet.
func (a *app) steerOwnerOf(conversation string) (string, bool) {
	conversation = strings.TrimSpace(conversation)
	file := strings.TrimSpace(a.file)
	if conversation == "" {
		return a.steerOwner(), file == ""
	}
	if file == "" || filepath.Clean(conversation) != filepath.Clean(file) {
		return "", false
	}
	return a.steerOwner(), true
}

// A CONVERSATION WITH NO TRANSCRIPT USED TO BE GIVEN A NAME OF ITS OWN HERE, and
// it is not any more. That name lived in this process and nowhere else, so a send
// made under it could be neither written down nor recognised after a restart —
// and the drafts lane already answers the same question in a way that survives
// one (draftkeep.go's [draftWindowOwnerOf]). Such a conversation cannot steer at
// all now ([app.steerDoorNow] says why), which is the honest end of that road.

// steerKeep is EVERYTHING THE BOX WAS HOLDING when a send was made, kept so a
// send that fails can give the person back what they actually had.
//
// TEXT ALONE IS NOT THE DRAFT. A sentence restored without its caret puts the
// cursor somewhere the person did not leave it; restored without its compact
// paste chips it sends `[paste 1 · 42 lines]` as though those were the words
// (pastechip.go). The tray of attachments is deliberately NOT in here: a room's
// enter does not spend it — the tray belongs to the conversation (room.go says
// so at the keypress) — so a send has nothing of it to give back.
type steerKeep struct {
	// box is the editor BY VALUE with its own copy of the runes: the live editor
	// reuses its slice on every keystroke ([editor.setText] appends into
	// value[:0]), so a snapshot sharing it would be rewritten by the next
	// character typed.
	box    editor
	pastes []pasteChip
}

// steerComposerNow snapshots the box as it stands.
func (a *app) steerComposerNow() steerKeep {
	return steerKeep{
		box: editor{
			value:       append([]rune(nil), a.input.value...),
			cursor:      a.input.cursor,
			demotedTags: append([]segment(nil), a.input.demotedTags...),
		},
		pastes: append([]pasteChip(nil), a.pastes...),
	}
}

// steerSend is one correction on its way to one node, and everything needed to
// ask for it again.
type steerSend struct {
	// key is this surface's own handle on the send, and it is what pairs the
	// answer with the row. It is not the engine's direction id: there is no
	// engine id until the engine answers, which is the whole of the interval
	// this type exists for.
	key uint64
	// at is who it was addressed to, taken at the keypress and never resolved
	// again. A person walks out of a room, into another one, into another
	// CONVERSATION, while a send is crossing.
	at steerAddress
	// gen is the PAGE it was typed into (room.go's [taskRoom.gen]). A page that
	// has been closed and reopened is a different page — its rows were built
	// again from the journal — so a receipt for a send typed into the old one
	// paints nothing.
	gen int
	// line is the sentence as the person typed it, compact paste tags and all;
	// words is the same sentence as the worker reads it, with pastes unfolded.
	line  string
	words string
	// from is the send's identity. IT IS NEVER RE-MINTED: a retry of this send is
	// this send. A send that could not be given one never leaves
	// ([app.keepSendFirst]), so anything crossing has it.
	from session.SteerSource
	// keep is the composer as it was at the keypress.
	keep steerKeep
	// door is the engine this send is addressed to.
	door steerDoor
	// elbow is the row drawing it. It is held as a POINTER rather than as an
	// index because the page under it is rebuilt on every journal refresh
	// (roomrefresh.go) — the entry moves, the block does not.
	elbow *steerElbow
	// asked counts the crossings made for this one send, retries included.
	asked int
	// lost says at least one crossing of this send ended with nobody answering,
	// so it can never again be described as certainly undelivered — a later
	// refusal only tells us about the later crossing.
	lost bool
	// gone says the conversation this send belongs to was CLOSED FOR REAL while
	// the crossing was in the air ([app.forgetSteerOwner] marks it). The answer
	// still arrives and still matches — an unmatched answer releases nothing and
	// would strand the address — but there is no page until the conversation
	// reopens. Its uncertain send stays on disk and in the owner-scoped outbox;
	// [app.steerKeptGone] never treats silence as refusal.
	gone bool
	// remembered says this sentence is already in the recall list. One send is
	// one line the person typed, however many times it is asked about.
	remembered bool
	// kept is the record write this send is waiting on before it may cross
	// ([app.keepSendFirst]). Its path and its number are what an arriving
	// [draftKeptMsg] is matched against; whether the send itself survived is asked
	// of the record by name ([draftKeptSend]). It is the zero value for a send
	// that has crossed and for a restored one, which is already on disk.
	kept draftSave
}

// steerOutbox is every correction this surface has sent into a node's page and
// not yet heard the engine's answer to, and the numbering that names them.
type steerOutbox struct {
	// scope names this life of this surface, minted once at the first send.
	// SIXTEEN BYTES AND NO FALLBACK, for [session.SteerSource]'s reason: a scope
	// drawn from the clock or the pid would be a guarantee this file states and
	// does not have.
	//
	// IT NAMES NEW SENDS ONLY. A send restored from a previous life keeps the
	// scope it was made under ([app.restoreSteerSnapshots]); replacing it would
	// make a message this engine already holds unrecognisable and deliver it
	// twice — which is precisely the failure the identity exists to prevent.
	scope string
	// denied says the machine refused those bytes. Nothing is sent from then on:
	// a correction with no name can be written down nowhere and asked about never,
	// so it is refused with the words kept rather than delivered on a promise this
	// surface cannot make ([app.keepSendFirst]).
	denied bool
	// seq counts this surface's sends, from 1, across every conversation. It is
	// not per node or per conversation because it does not have to be: a number
	// is only ever compared with another from the same scope on the same node's
	// record.
	seq uint64
	// key counts the rows, and is this surface's own handle on each send.
	key uint64
	// flight is the send crossing right now for each address, and queue is what
	// is waiting behind it in the order it was typed.
	flight map[steerAddress]uint64
	queue  map[steerAddress][]*steerSend
	// flying is every send that has left this surface and not been answered, by
	// its own key — what an arriving answer is paired through.
	flying map[uint64]*steerSend
	// unsure is every send NOBODY ANSWERED, held with the identity it already
	// has so that asking again is a retry of it rather than a new correction.
	//
	// IT IS A LIST PER ADDRESS AND IN THE ORDER THEY WERE TYPED. A link that is
	// down stays down: the second correction typed into it is as uncertain as the
	// first, and a single slot would drop the older one — leaving a row on the
	// page nothing could ever resolve. Asking again asks about all of them, in
	// order, because the engine refuses an older message behind a newer one from
	// the same life (internal/session's [taskAssignment.hear]).
	unsure map[steerAddress][]*steerSend
	// waiting is every send that has been written into its recipient's composer
	// and is waiting for that record to reach the disk before it crosses, by its
	// own key ([app.sendSteer]).
	//
	// THE COMPOSER THAT REFUSED SENDS GO BACK TO IS NOT HERE ANY MORE. It was a
	// map on this struct — one per address, offered to whoever built that page —
	// and it is now the recipient's own draft (recipient.go's [app.recoverOwned]),
	// which is the same fact kept in the one place that survives a restart.
	waiting map[uint64]*steerSend
}

// ready builds the outbox's maps on the first send. The zero value of the
// struct is an outbox nobody has sent through, which is what a surface that
// never opens a room has for its whole life.
func (o *steerOutbox) ready() {
	if o.flight == nil {
		o.flight = map[steerAddress]uint64{}
		o.queue = map[steerAddress][]*steerSend{}
		o.flying = map[uint64]*steerSend{}
		o.unsure = map[steerAddress][]*steerSend{}
		o.waiting = map[uint64]*steerSend{}
	}
}

// unresolved writes one send down as unanswered, keeping the order they were
// typed in and never twice.
func (o *steerOutbox) unresolved(send *steerSend) {
	for _, held := range o.unsure[send.at] {
		if held.key == send.key {
			return
		}
	}
	o.unsure[send.at] = append(o.unsure[send.at], send)
}

// resolved takes one send off that list — it was answered, or refused, and
// either way nobody is waiting to hear about it any more. The others at that
// address are left exactly where they are.
func (o *steerOutbox) resolved(send *steerSend) {
	held := o.unsure[send.at]
	kept := held[:0]
	for _, one := range held {
		if one.key == send.key {
			continue
		}
		kept = append(kept, one)
	}
	if len(kept) == 0 {
		delete(o.unsure, send.at)
		return
	}
	o.unsure[send.at] = kept
}

// mint is the next NEW send's identity, or the zero value on a machine that
// would not give this surface the bytes to name one.
//
// conversation is the session file this send was written for, and it travels
// with it so the engine can refuse it after /resume or /new have put a
// different conversation behind the same handle
// ([session.ErrNotThatConversation]). Empty is this surface making no claim,
// which is what a conversation with no file yet honestly has.
func (o *steerOutbox) mint(at time.Time, conversation string) session.SteerSource {
	if o.denied {
		return session.SteerSource{}
	}
	if o.scope == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			o.denied = true
			return session.SteerSource{}
		}
		o.scope = hex.EncodeToString(raw[:])
	}
	o.seq++
	return session.SteerSource{Scope: o.scope, Seq: o.seq, At: at, Conversation: conversation}
}

// steerSentMsg is one crossing's answer, on its way back into the update loop.
// It carries every field the answer has to be MATCHED on, not merely the ones
// the drawing needs: a stale answer that released somebody else's queue would
// be one lost send and one sent twice.
type steerSentMsg struct {
	key     uint64
	at      steerAddress
	gen     int
	receipt session.SteerReceipt
	err     error
	// unanswered records a lost first answer even if the automatic retry refused.
	unanswered bool
	// asked is how many crossings this send has now cost, retries included.
	asked int
}

// sendSteer takes one typed correction, writes it down against the recipient it
// was typed at, and hands its crossing to a command ONCE THAT WRITE HAS LANDED.
//
// IT IS THE ONLY DOOR THAT MINTS, so the numbering law above is stated once: a
// new press of enter is a new send with a new number, whatever is unresolved
// beside it.
//
// ── NOTHING CROSSES BEFORE IT IS ON DISK ────────────────────────────────────
//
// A correction that reached the engine and was never written down is the one
// failure this whole file cannot recover from: the window dies, and the next one
// has no name to ask about it under, so the person is left with a worker that may
// or may not have been corrected and no way to find out. So the snapshot goes
// into the recipient's composer and the record is written FIRST, and the crossing
// is started by the answer to that write ([app.sendsKeptAt]).
//
// AND THE KEYBOARD DOES NOT WAIT FOR THE DISK. The write is a command like the
// crossing is: the row is already drawn saying `sending`, the box is already
// empty, and the person can keep typing, walk out of the room or leave the
// conversation while it happens. A queued send is written down at the keypress
// too — it enters the queue immediately, in the order it was typed, and its turn
// to cross simply cannot come before its own words are safe.
func (a *app) sendSteer(room *taskRoom, line, words string, elbow *steerElbow, keep steerKeep) tea.Cmd {
	if room == nil || elbow == nil {
		return nil
	}
	door := a.steerDoorNow()
	if !door.live() {
		return nil
	}
	a.outbox.ready()
	a.outbox.key++
	send := &steerSend{
		key: a.outbox.key,
		at:  steerAddress{owner: a.steerOwner(), task: room.id},
		gen: room.gen, line: line, words: words,
		from: a.outbox.mint(a.now(), a.file), keep: keep, door: door, elbow: elbow,
	}
	return a.keepSendFirst(send)
}

// keepSendFirst writes one send into its recipient's composer and hands the
// record's own write to a command. The send waits for that write's answer.
//
// A SEND WITH NO NAME IS REFUSED RATHER THAN SENT ANYWAY. The only way to reach
// here without one is the machine refusing the bytes to mint it
// ([steerOutbox.mint] — a failing crypto source, and nothing else), and a
// nameless correction can be written down nowhere and asked about never. Sending
// it would buy one delivery at the price of the guarantee the rest of this file
// is: the words stay the person's, and the room says so.
func (a *app) keepSendFirst(send *steerSend) tea.Cmd {
	snap := send.snapshot(draftSendCrossing)
	back := func(word string) tea.Cmd {
		if a.standingIn(send) {
			a.withdrawRoomSteer(a.room, send.elbow)
		}
		if !a.recoverOwned(send.at.owner, taskRecipient(send.at.task), snap) {
			// No composer of its own to give them back to, so the words are said here
			// whole rather than dropped ([app.restoreSteerDraft] states the same law).
			word = steerKeptNowhere + strings.TrimSpace(send.line)
		}
		a.note(taskIDWord(send.at.task) + word)
		return a.armDraftKeep()
	}
	if send.from.Scope == "" {
		return back(steerNamelessWord)
	}
	if !a.keepSendOwned(send.at.owner, taskRecipient(send.at.task), snap) {
		// The conversation it was typed in is not one this window is holding, which
		// cannot happen at a keypress and is not a thing to guess about: the words
		// stay where they are and nothing crosses.
		return back(steerUnwrittenWord)
	}
	send.kept = a.draftSaveOf(a.mainDraftText())
	if send.kept.keepPath == "" {
		// Nowhere to write it. [app.steerDurable] refuses this at the door, and this
		// is the same refusal one layer in: a send left waiting on a write that will
		// never happen is a correction that never goes and never comes back either.
		return back(steerUnwrittenWord)
	}
	a.outbox.waiting[send.key] = send
	return send.kept.command()
}

// steerNamelessWord is what the conversation says about a correction this
// machine could not give a durable name to. It says nothing was sent and where
// the words are, exactly as the disk's own refusal does.
const steerNamelessWord = " was not corrected — this correction could not be given a name to ask about later, and your words are back on its page"

// sendsKeptAt is the answer to one record's write, read as "which sends may
// cross now" (draftkeep.go's [draftKeptMsg]).
//
// THE PROOF IS THE MESSAGE'S OWN NAME AND NOT THE NUMBER OF THE WRITE. A save
// that landed says which messages are in the record it put on disk
// ([draftKeptSend]), so a save that CLEARED the record — a box emptied, a
// conversation closed, a reunion pruning what it did not take — cannot release a
// send whose words it did not carry. Each waiting send is asked about
// separately, and there are exactly three answers:
//
//   - its name is in the record on disk, at or after its own save: it crosses;
//   - its own save was superseded and its name is not there yet: it goes on
//     waiting, and the newer save's answer settles it either way;
//   - anything else: nothing of it reached the disk, so it does not cross and the
//     words go back to the person.
func (a *app) sendsKeptAt(path string, rev uint64, err error) tea.Cmd {
	if len(a.outbox.waiting) == 0 || path == "" {
		return nil
	}
	superseded := errors.Is(err, errDraftSuperseded)
	// THEY GO IN THE ORDER THEY WERE TYPED, which a map is not. One write can
	// answer for several sends — a second correction typed while the first was
	// still being written is carried by the same whole record — and releasing them
	// in map order would put the second one in flight and queue the first behind
	// it, which is the reversal the queue exists to prevent.
	var ready, unwritten []*steerSend
	for key, send := range a.outbox.waiting {
		if send.kept.keepPath != path || send.kept.rev > rev {
			continue
		}
		switch {
		case draftKeptSend(path, send.kept.rev, draftSendName(send.from.Scope, send.from.Seq)):
			delete(a.outbox.waiting, key)
			ready = append(ready, send)
		case superseded:
			// The save that would have carried it is still on its way. Its own answer
			// releases this one or hands the words back; the debounce armed by the
			// same keypress is the backstop if that answer never arrives.
		default:
			delete(a.outbox.waiting, key)
			unwritten = append(unwritten, send)
		}
	}
	byKey := func(sends []*steerSend) {
		sort.Slice(sends, func(i, j int) bool { return sends[i].key < sends[j].key })
	}
	byKey(ready)
	byKey(unwritten)
	var cmds []tea.Cmd
	for _, send := range ready {
		cmds = append(cmds, a.crossSteer(send))
	}
	for _, send := range unwritten {
		cmds = append(cmds, a.sendUnwritten(send))
	}
	return tea.Batch(cmds...)
}

// sendUnwritten is a send whose words never reached the disk: it does not cross,
// its row comes off the page, and the sentence goes back to the recipient that
// typed it with the reason said out loud.
//
// IT IS THE SAME ENDING AS AN ENGINE'S REFUSAL and is spelled the same way — the
// words are the person's again and nothing was delivered anywhere — with the
// disk's own sentence instead of the engine's.
func (a *app) sendUnwritten(send *steerSend) tea.Cmd {
	a.outbox.resolved(send)
	if a.standingIn(send) {
		a.withdrawRoomSteer(a.room, send.elbow)
	}
	word := steerUnwrittenWord
	if !a.recoverOwned(send.at.owner, taskRecipient(send.at.task), send.snapshot(draftSendRefused)) {
		// No composer of its own left to give them back to ([app.restoreSteerDraft]).
		word = steerKeptNowhere + strings.TrimSpace(send.line)
	}
	// The disk's own sentence has already been said once by the write's answer
	// ([app.draftKept]); this one is about the correction that did not go.
	a.note(taskIDWord(send.at.task) + word)
	// AND THE WORDS ARE ASKED FOR AGAIN. The write that failed was carrying them
	// as a crossing; they are a draft now, and the next attempt is the ordinary
	// debounce rather than a retry of a send that never left.
	return a.armDraftKeep()
}

// steerUnwrittenWord is what the conversation says about a correction that was
// not sent because it could not be written down first. It names the task, says
// nothing was sent, and says where the words are.
const steerUnwrittenWord = " was not corrected — the draft could not be saved first, and your words are back on its page"

// crossSteer sends one correction, or writes it down behind the one already
// crossing to that address.
func (a *app) crossSteer(send *steerSend) tea.Cmd {
	if send == nil {
		return nil
	}
	a.outbox.ready()
	if _, crossing := a.outbox.flight[send.at]; crossing {
		a.outbox.queue[send.at] = append(a.outbox.queue[send.at], send)
		return nil
	}
	a.outbox.flight[send.at] = send.key
	a.outbox.flying[send.key] = send
	send.asked++
	return steerCrossing(send)
}

// steerCrossing is the command that actually calls the engine, off the event
// loop.
//
// EVERYTHING IT NEEDS IS CAPTURED BEFORE IT RETURNS. The closure runs on
// another goroutine and touches nothing of the app: the door, the words and the
// identity were taken at the keypress and travel into it by value.
func steerCrossing(send *steerSend) tea.Cmd {
	door, from := send.door, send.from
	task, words := send.at.task, send.words
	key, at, gen, asked := send.key, send.at, send.gen, send.asked
	return func() tea.Msg {
		unanswered := false
		answer := func(receipt session.SteerReceipt, err error, tries int) tea.Msg {
			return steerSentMsg{key: key, at: at, gen: gen, receipt: receipt, err: err, asked: tries, unanswered: unanswered}
		}
		cross := func() (session.SteerReceipt, error) {
			if door.identity != nil {
				return door.identity.SteerTaskFrom(task, words, from)
			}
			if door.plain != nil {
				return door.plain.SteerTask(task, words)
			}
			return session.SteerReceipt{}, errors.New(roomUnavailableRefusal.line())
		}
		receipt, err := cross()
		unanswered = errors.Is(err, session.ErrSendUnanswered)
		// ONE AUTOMATIC REPEAT, AND ONLY FOR THE ONE ERROR THAT MEANS NOBODY
		// ANSWERED. A refusal is an answer — the node is done, or has nobody in it
		// — and asking again would send the same words at the same closed door.
		// An engine that does not keep names is not asked twice by this surface at
		// all; the person is asked instead.
		if err != nil && door.repeat && from.Scope != "" && errors.Is(err, session.ErrSendUnanswered) {
			receipt, err = cross()
			return answer(receipt, err, asked+1)
		}
		return answer(receipt, err, asked)
	}
}

// steerSent is the engine's answer arriving: the row settles, the send becomes
// one nobody answered, or the words come back to the person.
//
// A STALE OR DUPLICATE ANSWER RELEASES NOTHING. It is matched on the send's own
// key AND on the address it was made for, and a send this surface is no longer
// holding — the conversation was replaced, the answer arrived twice — leaves
// every queue exactly as it found it. Releasing on an unmatched answer would
// send the next correction early and leave the one in flight unanswerable.
func (a *app) steerSent(msg steerSentMsg) tea.Cmd {
	send := a.outbox.flying[msg.key]
	if send == nil || send.at != msg.at || send.gen != msg.gen {
		return nil
	}
	delete(a.outbox.flying, msg.key)
	send.asked = msg.asked
	send.lost = send.lost || msg.unanswered
	var next tea.Cmd
	if a.outbox.flight[msg.at] == msg.key {
		next = a.releaseSteer(msg.at)
	}
	if msg.err != nil {
		return tea.Batch(next, a.steerFailed(send, msg))
	}
	// THE SEND IS RESOLVED, so nothing is holding it any more. An answer to a
	// send that was uncertain — the retry landed — is what takes it off that
	// list, and the receipt below is the same receipt it would have had.
	a.outbox.resolved(send)
	// AND ITS RECORD LETS GO OF IT TOO, in the conversation that made it and not
	// in whichever one is in front ([app.settleSend]). The delete is written by the
	// ordinary debounce rather than at once: losing it to a crash means the send is
	// offered again after a restart, which an engine that keeps names answers with
	// the receipt it already has — where losing the WRITE that made it would have
	// cost the person the correction itself.
	a.settleSend(send)
	a.rememberSteer(send)
	// THE ROW SETTLES INTO THE ENGINE'S OWN SENTENCE. The block is written
	// through the pointer the send has held all along, so a page rebuilt by a
	// journal refresh in the meantime settles the same block. A send restored
	// from a previous life and answered before anybody opened its page has no
	// block at all, and that is a row nobody is looking at rather than a fault.
	if send.elbow != nil {
		send.elbow.consumed = true
		send.elbow.stalled = false
		send.elbow.landed = a.now()
		send.elbow.landing = ""
		send.elbow.receipt = steerReceiptWords(msg.receipt)
	}
	if !a.standingIn(send) {
		// The page it belongs to is not the one on screen. The block is settled all
		// the same — it is the record of what happened, and the person will read it
		// when they walk back in — and nothing on the frame is touched, because the
		// page under their eyes is somebody else's work.
		return next
	}
	a.roomTouched()
	// The two wakeups the clause's fade needs and no ticker (steerelbow.go).
	return tea.Batch(next, fadeTicks())
}

// settleSend takes one message off the durable record of the conversation that
// made it, and holdSend writes one back down under a state that has changed.
//
// THEY NAME THE OWNER AND NEVER THE SURFACE. An answer arrives long after the
// keypress and the window may be showing another conversation entirely by then;
// the composer this send belongs to is that conversation's, wherever it is
// (recipient.go's [app.atOwnedComposer]).
//
// AND A CONVERSATION THIS WINDOW NO LONGER HOLDS IS NOT PROOF OF ANYTHING. Its
// record keeps the send exactly as it was: closed for good is what
// [app.forgetSteerOwner] is for, and it is called by the close and by nothing
// else.
func (a *app) settleSend(send *steerSend) {
	if send == nil || send.from.Scope == "" {
		return
	}
	a.dropSendOwned(send.at.owner, taskRecipient(send.at.task), send.from.Scope, send.from.Seq)
}

func (a *app) holdSend(send *steerSend, state string) {
	if send == nil || send.from.Scope == "" {
		return
	}
	a.keepSendOwned(send.at.owner, taskRecipient(send.at.task), send.snapshot(state))
}

// releaseSteer sends the next correction queued for one address, if there is
// one.
func (a *app) releaseSteer(at steerAddress) tea.Cmd {
	delete(a.outbox.flight, at)
	waiting := a.outbox.queue[at]
	if len(waiting) == 0 {
		delete(a.outbox.queue, at)
		return nil
	}
	next := waiting[0]
	if len(waiting) == 1 {
		delete(a.outbox.queue, at)
	} else {
		a.outbox.queue[at] = waiting[1:]
	}
	return a.crossSteer(next)
}

// standingIn reports whether the page this send was typed into is the page on
// screen right now — the same conversation, the same node, the same opening of
// its page.
func (a *app) standingIn(send *steerSend) bool {
	if a.room == nil || send == nil {
		return false
	}
	return a.room.gen == send.gen && a.room.id == send.at.task && a.steerOwner() == send.at.owner
}

// rememberSteer puts a correction into the recall list, and it is called at the
// SETTLEMENT rather than at the keypress.
//
// A REFUSED SEND IS NOT REMEMBERED, because its words are back in the box: the
// list would then hold a sentence that went nowhere, beside the very sentence
// the person is still holding (roomrecall.go's law, and its own test). Every
// other ending leaves the words out of the box — delivered, held, or kept as a
// send nobody answered — and those are lines that were typed and sent.
func (a *app) rememberSteer(send *steerSend) {
	if send == nil || send.remembered || strings.TrimSpace(send.line) == "" {
		return
	}
	send.remembered = true
	a.remember(send.line)
}

// steerFailed is a send that did not go, or that nobody answered for. They are
// two different endings and the difference is the whole of this function.
func (a *app) steerFailed(send *steerSend, msg steerSentMsg) tea.Cmd {
	// ── ITS CONVERSATION WAS CLOSED WHILE THE CROSSING WAS IN THE AIR ──
	//
	// The closed page cannot receive an answer. Keep uncertainty under its
	// original owner and name, and describe the missing page without inventing
	// a refusal. A definite refusal can still give the person's words back.
	if send.gone {
		return a.steerKeptGone(send, msg.err)
	}
	// ── NOBODY ANSWERED ──
	//
	// The words may be on the node's record already, so they are NOT handed back
	// to the box: the box would mint a new name for them on the next enter and
	// the correction could land twice. The send is kept as a send, with the
	// identity it already has, and the row keeps the person's words with an
	// honest clause on it. `r` on the question below is a retry OF THIS SEND.
	if errors.Is(msg.err, session.ErrSendUnanswered) {
		send.lost = true
		a.outbox.unresolved(send)
		// AND THE RECORD SAYS SO: this send is now one nobody answered, which is a
		// different fact from the crossing it was written down as, and the state is
		// what stops a restart offering these words back as a plain draft
		// (recipient.go's [draftSendUnanswered]).
		a.holdSend(send, draftSendUnanswered)
		a.rememberSteer(send)
		if send.elbow != nil {
			send.elbow.consumed = false
			send.elbow.stalled = true
			send.elbow.landing = steerLostWord
		}
		if a.standingIn(send) {
			// THE QUESTION IS ABOUT THE OLDEST OF THEM. Asking again asks about
			// every unresolved send at that address, in the order they were typed,
			// so the line names the one the retry starts with.
			a.raiseLostGuard(a.unsureSteer())
			a.roomTouched()
			return nil
		}
		// A PERSON WHO WALKED AWAY IS STILL TOLD. The row is waiting for them on
		// that page, and a correction whose outcome nobody knows is not a thing to
		// find out about by accident.
		a.note(taskIDWord(send.at.task) + steerUnsureAway)
		return nil
	}
	// ── THAT CONVERSATION IS NOT OPEN THERE ANY MORE ──
	//
	// The engine refused before it looked at the task number, so this correction
	// was NOT delivered to some other conversation's task 7 ([session.ErrNotThatConversation]).
	// The send is kept exactly as it stands, under the name it already has and at
	// the address it was made for: the words are not handed to whatever composer
	// is on screen — that belongs to another conversation — and nothing is sent
	// again by itself. Going back to that conversation and opening the page
	// offers the retry ([app.adoptUnsentSteer]), and it lands once.
	if errors.Is(msg.err, session.ErrNotThatConversation) {
		a.outbox.unresolved(send)
		// IT IS A DEFINITE NON-DELIVERY THAT KEEPS ITS NAME. Nothing was written on
		// any node, so this is not the unknown the word above means — and it is not
		// a draft either, because the retry that reaches the right conversation has
		// to be THIS send. A send an earlier crossing already left uncertain stays
		// uncertain: this refusal is about the crossing just made.
		if send.lost {
			a.holdSend(send, draftSendUnanswered)
		} else {
			a.holdSend(send, draftSendUndelivered)
		}
		a.rememberSteer(send)
		if send.elbow != nil {
			send.elbow.consumed = false
			send.elbow.stalled = true
			// A send that was already uncertain stays uncertain: this refusal is about
			// the crossing just made, and says nothing about the one nobody answered.
			send.elbow.landing = steerElsewhereWord
			if send.lost {
				send.elbow.landing = steerLostWord
			}
		}
		if a.standingIn(send) {
			a.raiseLostGuard(a.unsureSteer())
			a.roomTouched()
			return nil
		}
		word := steerElsewhereAway
		if send.lost {
			word = steerUnsureAway
		}
		a.note(taskIDWord(send.at.task) + word)
		return nil
	}
	// ── A REFUSAL DOES NOT SETTLE A SEND THAT WAS ALREADY UNCERTAIN ──
	//
	// This refusal is about the crossing just made. An earlier crossing of the
	// same send got no answer and may be on the task's record, so handing these
	// words back to the box would let the next enter mint a NEW name for them and
	// deliver them a second time. It stays a send, under the name it has, and the
	// row carries the engine's own sentence.
	if send.lost {
		a.outbox.unresolved(send)
		a.holdSend(send, draftSendUnanswered)
		a.rememberSteer(send)
		if send.elbow != nil {
			send.elbow.consumed = false
			send.elbow.stalled = true
			send.elbow.landing = steerLostWord
		}
		if a.standingIn(send) {
			a.raiseLostGuard(a.unsureSteer())
			a.roomTouched()
			return nil
		}
		a.note(taskIDWord(send.at.task) + steerUnsureAway)
		return nil
	}
	// ── THE ENGINE REFUSED ──
	//
	// It read the call and said no, so nothing was written anywhere and the words
	// are the person's again. THE ROW COMES OFF FIRST: a block left on the page
	// for words that are back in the box would be the surface drawing the same
	// sentence twice and claiming one of them is on its way.
	a.outbox.resolved(send)
	a.settleSend(send)
	if a.standingIn(send) {
		a.withdrawRoomSteer(a.room, send.elbow)
	}
	a.restoreSteerDraft(send)
	if !a.standingIn(send) {
		// The engine's own sentence about a page the person has walked out of goes
		// in the conversation's own lane, naming the task: a refusal drawn into
		// whichever room is open would be about the wrong work.
		a.note(taskIDWord(send.at.task) + steerKeptForPage)
		return nil
	}
	// The engine's own sentence, kept, and the keys that are honest for it —
	// room.go's guard states the whole law and the reason revive is withheld
	// from a node that is still running with nobody inside it.
	if errors.Is(msg.err, session.ErrNobodyToRead) {
		a.raiseBusyGuard(send.line, msg.err.Error())
		return nil
	}
	a.raiseGuard(send.line, msg.err.Error())
	return nil
}

// steerKeptGone handles an answer after its conversation was closed. Closing
// a page cannot settle a crossing: an unknown outcome keeps its original name
// and payload, and only a definite refusal may give words back to a composer.
func (a *app) steerKeptGone(send *steerSend, err error) tea.Cmd {
	if send.lost || errors.Is(err, session.ErrSendUnanswered) {
		send.lost = true
		a.outbox.unresolved(send)
		a.holdSend(send, draftSendUnanswered)
		a.rememberSteer(send)
		if a.steerOwner() == send.at.owner && a.room != nil && a.room.id == send.at.task {
			a.adoptOneSteer(a.room, send)
			a.raiseLostGuard(send)
		} else {
			a.note(taskIDWord(send.at.task) + steerUnsureClosed)
		}
		return nil
	}
	a.outbox.resolved(send)
	a.rememberSteer(send)
	if !a.recoverOwned(send.at.owner, taskRecipient(send.at.task), send.snapshot(draftSendRefused)) {
		a.note(taskIDWord(send.at.task) + steerKeptNowhere + strings.TrimSpace(send.line))
	}
	return nil
}

// retrySteer asks again for the send nobody answered, UNDER THE NAME IT
// ALREADY HAS. That is what makes it a retry: an engine that keeps names
// answers it with the receipt already on its record, and the correction lands
// exactly once however many times this is pressed.
// IT ASKS ABOUT EVERY UNRESOLVED SEND AT THAT ADDRESS, oldest first. Two
// corrections typed into a link that was already down are both unanswered, and
// asking about one of them would leave the other on the page with nothing that
// could ever resolve it. They go one at a time — the second waits behind the
// first through the ordinary queue — because a task refuses an older message
// admitted behind a newer one from the same life.
func (a *app) retrySteer(at steerAddress) tea.Cmd {
	held := a.outbox.unsure[at]
	if len(held) == 0 {
		return nil
	}
	delete(a.outbox.unsure, at)
	cmds := make([]tea.Cmd, 0, len(held)+1)
	for _, send := range held {
		// THE ROW GOES BACK TO CROSSING. It is the same row and the same words:
		// this is one send being asked about again, not a second correction.
		if send.elbow != nil {
			send.elbow.stalled = false
			send.elbow.consumed = false
			send.elbow.landing = steerSendingWord
		}
		cmds = append(cmds, a.crossSteer(send))
	}
	// A RETRY MAY FIND THE PAGE MOVED ON, and it still goes: the words were
	// typed for that node and the answer will simply paint nothing. Where the
	// page IS still open, the rows it settles are the rows already on it.
	if a.room != nil && a.room.gen == held[0].gen {
		a.roomTouched()
	}
	return tea.Batch(append(cmds, fadeTicks())...)
}

// unsureSteer is the OLDEST send nobody answered for the page on screen, or
// nil. It is the one a question is raised about, and asking about it asks about
// everything behind it.
func (a *app) unsureSteer() *steerSend {
	if a.room == nil {
		return nil
	}
	held := a.outbox.unsure[steerAddress{owner: a.steerOwner(), task: a.room.id}]
	if len(held) == 0 {
		return nil
	}
	return held[0]
}

// restoreSteerDraft gives a REFUSED send's composer back to the person, on the
// page it was typed into and never on the page in front of them.
//
// THE RECIPIENT OWNS THE DRAFT, so this is one call into recipient.go: the whole
// composer — the words as they spelled them, the caret where they left it, and
// the compact paste chips the text stands on — goes back to THAT node of THAT
// conversation, whether its page is open, held in the keeper, or written down.
// A draft they have typed into since is never overwritten; the sentence waits
// beside it as a [draftSendRefused] one and is offered the moment that box is
// empty again ([app.offerRecovered]).
func (a *app) restoreSteerDraft(send *steerSend) {
	if send == nil || len(send.keep.box.value) == 0 {
		return
	}
	if a.recoverOwned(send.at.owner, taskRecipient(send.at.task), send.snapshot(draftSendRefused)) {
		return
	}
	// THAT CONVERSATION IS NOT IN THIS WINDOW — closed here, or taken over by
	// another one — so it has no composer of its own to give the words back to and
	// no page they could appear under. Dropping them quietly would be losing the
	// sentence, so it is said here, whole, where the person can take it back.
	a.note(taskIDWord(send.at.task) + steerKeptNowhere + strings.TrimSpace(send.line))
}

// forgetSteerOwner cancels unsent work for a closed conversation. Crossings
// and unanswered sends keep their identity because closing is not a receipt.
func (a *app) forgetSteerOwner(owner string) {
	if owner == "" {
		// A conversation this build could never name held no sends either
		// ([app.steerDurable]), and an empty owner would match nothing but could
		// only ever match by accident.
		return
	}
	// An unanswered crossing remains unanswered after a close. Its record and
	// its name survive, but nothing retries until its own page is reopened.
	for at, held := range a.outbox.unsure {
		if at.owner == owner {
			for _, send := range held {
				send.gone = true
			}
		}
	}
	for at := range a.outbox.queue {
		if at.owner == owner {
			for _, send := range a.outbox.queue[at] {
				if send.lost {
					send.gone = true
					a.outbox.unresolved(send)
				}
			}
			delete(a.outbox.queue, at)
		}
	}
	// AND A SEND STILL WAITING ON ITS RECORD NEVER CROSSES. Its write may land
	// after this, and handing it to the transport then would be a correction sent
	// into a conversation this window has been told is closed.
	for key, send := range a.outbox.waiting {
		if send != nil && send.at.owner == owner {
			delete(a.outbox.waiting, key)
		}
	}
	// A crossing cannot be recalled. Its answer must still match, and its
	// durable snapshot survives the close until the outcome can be learned.
	for _, send := range a.outbox.flying {
		if send != nil && send.at.owner == owner {
			send.gone = true
		}
	}
}

// ── SURVIVING THE PROCESS ───────────────────────────────────────────────────
//
// A send nobody answered is exactly the thing a person loses a correction to
// when a window is closed and reopened, so it has to be able to be written down
// — and it has to come back WITH ITS NAME. A restored send that took a fresh
// name would be a new correction to an engine that may already hold the old
// one, which is the double-delivery this whole mechanism exists to prevent.
//
// THIS LANE OWNS THE VALUE AND NOT THE FILE. There is no second store here:
// [outboxSnapshot] is a plain value, and the recipient's own durable state
// (draftkeep.go, the drafts lane) writes it down beside that recipient's unsent
// line and hands it back on the way in. A send and a half-typed line are the
// same fact about the same reader — words that have not landed — and two stores
// would be two answers to it.
//
// THE CONVERSATION IS NOT IN THE SNAPSHOT because the slot it is written into
// already names one, and it is put back only under the conversation it was
// written for (draftkeep.go's own law). It is handed back to this lane with that
// name, and it becomes the send's expected conversation again
// ([session.SteerSource.Conversation]) — so a restored retry is refused by an
// engine that has moved on, exactly as a live one is.

// [outboxSnapshot] AND THE FOUR STATE WORDS LIVE IN recipient.go, which is the
// lane that writes them down. This one owns the value and its transitions and
// never the file.

// steerSnapshots is everything one recipient of one conversation has unsettled,
// oldest send first — the crossing ones as well as the ones nobody answered,
// because a window that goes away mid-crossing leaves an outcome nobody learned
// and that is the same uncertainty.
//
// IT IS ASKED BY CONVERSATION PATH, because that is all a record on disk knows
// (draftkeep.go's slots), and it answers for THIS surface's owner identity only
// — a path that is not the conversation open here names work this window cannot
// speak for, and it gets nothing rather than somebody else's sends.
//
// ── WHERE THE KEEPING HAPPENS ──
//
// Reading a snapshot is not keeping one, and these are the four places one is:
//
//	[app.keepSendFirst]   writes it BEFORE the crossing is handed to the command,
//	                      and the crossing waits for that write to land.
//	[app.settleSend]      drops it when the engine answers.
//	[app.holdSend]        writes the state that has changed, per branch of
//	                      [app.steerFailed].
//	[app.restoreSentDrafts] reads them back when the window opens, and
//	                      [app.adoptUnsentSteer] draws them when the page does.
func (a *app) steerSnapshots(conversation string, task uint64) []outboxSnapshot {
	owner, ok := a.steerOwnerOf(conversation)
	if !ok {
		return nil
	}
	at := steerAddress{owner: owner, task: task}
	var out []outboxSnapshot
	for _, send := range a.outbox.unsure[at] {
		out = append(out, send.snapshot(draftSendUnanswered))
	}
	for _, send := range a.outbox.flying {
		if send.at == at {
			out = append(out, send.snapshot(draftSendCrossing))
		}
	}
	for _, send := range a.outbox.queue[at] {
		out = append(out, send.snapshot(draftSendCrossing))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// snapshot is this send as something else can write down — and it is the WHOLE
// composer it was made from, not the sentence: the caret where the person left
// it and the documents behind the compact tags travel with the words, because
// what a refused send gives back has to be what they actually had ([steerKeep]).
func (s *steerSend) snapshot(state string) outboxSnapshot {
	return outboxSnapshot{
		scope: s.from.Scope, seq: s.from.Seq, state: state, at: s.from.At,
		line: s.line, words: s.words, caret: s.keep.box.cursor,
		pastes: append([]pasteChip(nil), s.keep.pastes...),
	}
}

// restoreSteerSnapshots takes them back, WITH THE IDENTITIES THEY WERE MADE
// UNDER and never with new ones — a restored send given a fresh name would be a
// second correction to an engine that may already hold the first.
//
// A RESTORED SEND HAS NO PAGE AND NO ENGINE YET. It is given both when its room
// is opened ([app.adoptUnsentSteer]), which is also the moment the person can
// see it and answer for it — a retry fired at a conversation nobody is looking
// at would be this surface acting on somebody's behalf without them.
//
// AND AN UNKNOWN OWNER IS NOT RESTORED. A record names a conversation by path;
// if that is not the conversation this surface has open, the sends in it belong
// to a window that is not this one and are left where they are — restoring them
// here would put another conversation's corrections on this one's pages, which
// is the misdelivery the whole address exists to prevent.
func (a *app) restoreSteerSnapshots(conversation string, task uint64, kept []outboxSnapshot) {
	if len(kept) == 0 {
		return
	}
	owner, ok := a.steerOwnerOf(conversation)
	if !ok || strings.TrimSpace(conversation) == "" {
		// A send with no conversation on it cannot be bound to an engine, and this
		// surface will not invent one for it.
		return
	}
	a.outbox.ready()
	at := steerAddress{owner: owner, task: task}
	for _, one := range kept {
		if strings.TrimSpace(one.words) == "" || one.scope == "" || one.seq == 0 {
			// A send with no name cannot be asked about again without risking a second
			// delivery, so it is not restored as a send. The recipient's own kept line
			// is where words with no name belong.
			continue
		}
		if !sendKept(one.state) {
			// A REFUSED SEND IS NOT A SEND ANY MORE. Those words are the person's
			// again, and the composer hands them back on the way in
			// ([app.offerRecovered]); making a row out of them here would offer to
			// deliver a second time something the engine has already said no to.
			continue
		}
		if a.steerKnown(one.scope, one.seq) {
			// THIS ROOM IS OPENED MANY TIMES AND THE RECORD IS READ EVERY TIME. A send
			// this surface is already holding under that name is that same send, and a
			// second row for it would be a second correction on the next retry.
			continue
		}
		a.outbox.key++
		a.outbox.unresolved(&steerSend{
			key: a.outbox.key, at: at, gen: -1,
			line: one.line, words: one.words,
			// THE CONVERSATION COMES BACK ON THE IDENTITY, so the restored retry is
			// checked against the engine exactly as the original crossing was.
			from: session.SteerSource{
				Scope: one.scope, Seq: one.seq, At: one.at,
				Conversation: strings.TrimSpace(conversation),
			},
			keep: steerKeep{
				box:    editor{value: []rune(one.line), cursor: max(0, min(one.caret, len([]rune(one.line))))},
				pastes: append([]pasteChip(nil), one.pastes...),
			},
			// It was crossing or it was unanswered, and either way nobody learned its
			// outcome — which is what [steerSend.lost] means.
			lost: true,
		})
	}
	// AND THIS LIFE'S OWN SCOPE IS NOT TOUCHED. A restored send keeps the name it
	// was made under; new sends keep taking this life's, which is the safer of the
	// two — a fresh scope has no direction on any node's record, so a new send can
	// never be read as arriving behind a higher number this window cannot see.
}

// steerKnown reports whether this surface is already holding a send under that
// name — crossing, queued, waiting on its record or unanswered. It is what makes
// reading the record on every open idempotent.
func (a *app) steerKnown(scope string, seq uint64) bool {
	same := func(send *steerSend) bool {
		return send != nil && send.from.Scope == scope && send.from.Seq == seq
	}
	for _, held := range a.outbox.unsure {
		for _, send := range held {
			if same(send) {
				return true
			}
		}
	}
	for _, send := range a.outbox.flying {
		if same(send) {
			return true
		}
	}
	for _, held := range a.outbox.queue {
		for _, send := range held {
			if same(send) {
				return true
			}
		}
	}
	for _, send := range a.outbox.waiting {
		if same(send) {
			return true
		}
	}
	return false
}

// restoreSentDrafts gives this process's unsettled sends back to the outbox from
// the composers the record was just read into — the other half of
// [app.keepSendFirst], and the reason a correction survives the window closing.
//
// IT READS THE COMPOSERS AND NOT THE FILE. draftkeep.go has already put every
// recipient's snapshots where they belong ([app.restoreComposers]); this walks
// the conversation in front, hands each node's kept sends to
// [app.restoreSteerSnapshots] under this conversation's own path, and touches
// nothing that is not this conversation's.
//
// GUESTS AND RUNS ARE PASSED OVER. A read-only guest has no sender at all
// ([app.steerDoorNow]), and a run is not a node this surface can correct.
func (a *app) restoreSentDrafts() {
	file := strings.TrimSpace(a.file)
	if file == "" {
		// A conversation with no transcript cannot bind a send to an engine, and
		// this surface will not invent a name for one ([app.steerOwnerOf]).
		return
	}
	for who, state := range a.everyComposer() {
		if who.task == 0 || who.guest != "" || who.run != "" || len(state.sends) == 0 {
			continue
		}
		a.restoreSteerSnapshots(file, who.task, state.sends)
	}
}

// adoptUnsentSteer gives an unresolved send THIS OPENING of its page, and draws
// it back onto it as the row it is.
//
// IT RUNS ON EVERY OPEN AND NOT ONLY AFTER A RESTORE. A page is rebuilt from
// the journal every time it is entered, so a send whose outcome is still
// unknown would otherwise be visible only in the opening it was typed in — and
// a person who pressed esc and came back would find their correction gone from
// a page that may or may not have it. A send already drawn on THIS opening is
// left exactly as it is.
func (a *app) adoptUnsentSteer(room *taskRoom) {
	if room == nil {
		return
	}
	held := a.outbox.unsure[steerAddress{owner: a.steerOwner(), task: room.id}]
	if len(held) == 0 {
		return
	}
	for _, send := range held {
		a.adoptOneSteer(room, send)
	}
	// AND THE QUESTION COMES UP WITH IT. A row that says nobody answered and
	// offers nothing is a dead end: the retry is the recovery action, and it has
	// to be reachable from the page the words are on. esc puts the question down
	// and leaves everything exactly as it is.
	if a.room == room {
		a.raiseLostGuard(held[0])
	}
}

// adoptOneSteer draws one unresolved send onto the page in front of it.
func (a *app) adoptOneSteer(room *taskRoom, send *steerSend) {
	if send == nil || send.at.owner != a.steerOwner() || send.at.task != room.id || send.gen == room.gen {
		return
	}
	send.gone = false
	a.holdSend(send, draftSendUnanswered)
	send.gen = room.gen
	send.door = a.steerDoorNow()
	send.elbow = &steerElbow{
		words: send.line, at: send.from.At, stalled: true, landing: steerLostWord,
	}
	room.said(entry{kind: entrySteer, turn: room.turn, steer: send.elbow})
	// A HOSTED PAGE REBUILDS ITSELF FROM THE JOURNAL, so the row has to be held
	// in hand the way a live send's is (roomrefresh.go) or the next refresh drops
	// it. And if the correction DID arrive before the window closed, the journal
	// is what says so: the echo retires against the real line, which is the
	// honest ending for a send whose outcome was unknown.
	if a.farRoomRecord != nil && len(room.entries) > 0 {
		room.keepSteerEcho(send.words, room.entries[len(room.entries)-1])
	}
	a.roomTouched()
}

// withdrawRoomSteer takes one correction off a node's page.
//
// It is [app.withdrawSteer]'s rule applied to a room's own list, and the rule is
// echo.go's: TRUNCATED WHEN IT IS LAST AND EMPTIED OTHERWISE, because removing
// an entry from the middle moves every index after it and the fold, the
// selection and the row cache are all held by index.
//
// AND THE PENDING ECHO GOES WITH IT. A hosted page keeps a sent correction in
// hand until the journal catches up (roomrefresh.go); an echo left behind for
// words that never arrived would put the sentence back on the page at the next
// refresh, under a clause nothing will ever settle.
func (a *app) withdrawRoomSteer(room *taskRoom, elbow *steerElbow) {
	if room == nil || elbow == nil {
		return
	}
	for at := len(room.entries) - 1; at >= 0; at-- {
		e := &room.entries[at]
		if e.kind != entrySteer || e.steer != elbow {
			continue
		}
		if at == len(room.entries)-1 {
			room.entries = room.entries[:at]
		} else {
			e.steer = nil
			e.stale = true
		}
		break
	}
	kept := room.pendingSteers[:0]
	for _, echo := range room.pendingSteers {
		if echo.entry.steer == elbow {
			continue
		}
		kept = append(kept, echo)
	}
	room.pendingSteers = kept
	a.roomTouched()
}
