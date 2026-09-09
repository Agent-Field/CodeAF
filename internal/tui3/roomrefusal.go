package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── A REFUSAL NAMES WHERE THE WORDS CAN GO ──────────────────────────────────
//
// Steering words typed into a FINISHED task's room were refused with four words
// and a way out of the page: `task finished — esc to return`. Every one of them
// was true and the sentence was still the wrong answer, because the words had a
// destination and this surface knew what it was — the main conversation, and the
// node's parent when it has one — and said nothing about either. A person who
// has just been told their sentence went nowhere is asking exactly one question,
// and it is not "how do I leave".
//
// THE LAW: a refusal on the task surfaces is TWO HALVES — what is true, and
// where the thing CAN go — and it is never drawn as only the first. The house
// already writes its best refusals this way (the steer guard's `[r] revive and
// send · [m] send to main · [esc] cancel`, the folder picker, the road-home
// refusals); what was missing was one place holding all of them, so a test can
// enumerate them and a refusal written next year cannot arrive without a door.
//
// [taskRefusals] is that place. A line that is not in it is not a refusal.
//
// ── WHAT IS NOT A REFUSAL, AND WHY IT IS NOT IN THE TABLE ──
//
// [roomGoneWord] and [roomYetWord] are PAGE-STATE lines: they
// answer "why is there nothing under this header" about a page that refused
// nothing and lost nothing (room.go's [app.roomRecordRows] states each one's
// case). Giving them a door would be inventing an action nobody attempted, and
// the table would stop meaning what it says.
//
// ── ESC IS NOT NAMED HERE ──
//
// The legend's left end carries `room · esc/←← main` for the whole of a room's
// life and the focus header carries `esc/← main` above it, so a refusal that
// also spelled esc would be the third row on one screen naming one key — which
// is the defect room.go's own hint slot exists to avoid. What these lines add is
// the half nothing else on the frame says.

// The two halves, and the punctuation that joins them.
const (
	// refusalGap joins what is true to where the words can go. It is the dash
	// this surface already joins a fact to its consequence with.
	refusalGap = " — "
	// refusalMainDoor is the door every task surface always has: the
	// conversation, in the one name this surface has for it ([roomCrumbRoot]).
	refusalMainDoor = "say it to " + roomCrumbRoot
	// refusalParentDoor is the SECOND door, offered only when the node has a
	// parent this surface has actually been told about — the rule the room's own
	// kin line already holds itself to (room.go's [app.roomKinRows]): a parent
	// named as a bare id has told a person nothing.
	refusalParentDoor = ", or open its parent, "
	// refusalOwnerLead opens the ONE door a page read through somebody else's
	// conversation has (taskowner.go's [taskGuest]): the conversation that owns
	// the work. [app.roomDoneRefusal] splices the owner's own name after it when
	// this window has one, and [refusalOwnerPlace] stands in when it does not.
	refusalOwnerLead = "say it in "
	// refusalOwnerPlace is that conversation named by its role, for a guest whose
	// owner nothing has named — the same words the reading page's own refusal
	// already uses (taskowner.go's [roomGuestReadingWord]).
	refusalOwnerPlace = "the conversation that owns it"
)

// refusal is one thing a task surface says when a person's words or keystroke
// cannot go where they were aimed: what is true, and the door.
type refusal struct {
	// what is the fact, in a person's words and never the engine's.
	what string
	// door is where the thing CAN go. It is never empty — a refusal without one
	// is the defect this type exists to make impossible — and [TestEveryRefusal]
	// enumerates the table to say so.
	door string
	// shortWhat is the same fact in fewer cells, for a frame that cannot hold the
	// whole line. It is the fitter's law said about a sentence (rowfit.go): the
	// DOOR is the part that must survive, so it is the fact that degrades.
	shortWhat string
}

// line is the refusal at full length.
func (r refusal) line() string { return r.what + refusalGap + r.door }

// short is the refusal with its fact cut back to the shorter spelling, or the
// full line where there is only one spelling of the fact.
func (r refusal) short() string {
	if r.shortWhat == "" {
		return r.line()
	}
	return r.shortWhat + refusalGap + r.door
}

// fit is the longest spelling that fits, and the short one cut where neither
// does — the door first, because a refusal cut back to its fact is the sentence
// this whole file exists to stop being drawn.
func (r refusal) fit(width int) string {
	if ansi.StringWidth(r.line()) <= width {
		return r.line()
	}
	if short := r.short(); ansi.StringWidth(short) <= width {
		return short
	}
	return fit(r.short(), width)
}

// The refusals themselves, spelled once each.
var (
	// roomFinishedRefusal is what a landed node's room says — at its foot, and in
	// the box's own placeholder. The parent clause is added by
	// [app.roomFinishedRefusal] when there is a parent to name.
	roomFinishedRefusal = refusal{
		what:      "this task has finished",
		shortWhat: "finished",
		door:      refusalMainDoor,
	}
	// roomUnavailableRefusal is the degraded case: an agent under this surface
	// with no room doors on it at all. It used to open with `room unavailable`,
	// which is the program describing its own wiring to somebody who typed a
	// sentence.
	roomUnavailableRefusal = refusal{
		what: "this session has no task rooms",
		door: refusalMainDoor,
	}
	// roomGuestFinishedRefusal is what a landed task's page says when the page is
	// a READING of another conversation's work (taskowner.go's [taskGuest]). ITS
	// DOOR IS NEVER MAIN: `say it to main` on a guest page would aim the words at
	// THIS window's conversation, whose task of the same number is different work
	// — the wrong-owner delivery the whole guest lane exists to prevent, offered
	// as advice. [app.roomDoneRefusal] puts the owner's own name in the door when
	// this window has one.
	roomGuestFinishedRefusal = refusal{
		what:      "this task has finished",
		shortWhat: "finished",
		door:      refusalOwnerLead + refusalOwnerPlace,
	}
)

// taskRefusals is EVERY refusal the task surfaces can say to a person's words or
// to a key they pressed. It exists so the rule can be checked rather than
// remembered: [TestEveryRefusalOnTheTaskSurfacesNamesADoor] walks it.
//
// The steer guard is not in it because it is not a line — it is a raised
// question drawing three doors as pressable keys (room.go's [app.guardRows]),
// which is this law's own best case rather than an exception to it.
var taskRefusals = []refusal{
	roomFinishedRefusal,
	roomUnavailableRefusal,
	roomGuestFinishedRefusal,
}

// roomFinishedRefusal is [roomFinishedRefusal] with the node's parent named in
// it, when this surface has been told the parent's name.
//
// THE PARENT IS THE OTHER PLACE THE WORDS BELONG. A node that was spawned under
// another node was somebody's idea of a piece of a bigger job, and a correction
// aimed at the piece after it has landed is nearly always a correction to the
// job — so the room offers that door beside the conversation's, by the name the
// roster and the kin line already call it.
//
// IT IS NAMED BY TITLE AND NEVER BY ID, which is the kin line's own rule
// (room.go's [app.roomKinRows]): `open its parent, 7` has told a person nothing,
// and a parent this session has had no update about is left unsaid rather than
// offered as a door onto a name nobody has.
func (a *app) roomFinishedRefusal() refusal {
	out := roomFinishedRefusal
	if title := a.roomParentTitle(); title != "" {
		out.door += refusalParentDoor + title
	}
	return out
}

// roomDoneRefusal is the refusal a landed page's foot draws: the ordinary one,
// with the parent named when there is one — or, on a page read through somebody
// else's conversation, the guest's own, whose door is the OWNER and never main.
//
// THE OWNER IS NAMED WHEN THIS WINDOW KNOWS WHAT TO CALL IT, by the same name
// the trail above the page already uses ([taskGuest.owner]); a conversation
// nothing has named keeps the door by its role, which is the emptiness law said
// about a destination.
func (a *app) roomDoneRefusal() refusal {
	guest := a.roomGuest()
	if guest == nil {
		return a.roomFinishedRefusal()
	}
	out := roomGuestFinishedRefusal
	if owner := strings.TrimSpace(guest.owner); owner != "" {
		out.door = refusalOwnerLead + owner
	}
	return out
}

// roomParentTitle is the name of the open room's parent node, or "" when there
// is no parent, no room, or no name for it.
func (a *app) roomParentTitle() string {
	node := a.roomNode()
	if node == nil || node.parent == "" {
		return ""
	}
	_, byKey := a.railKin()
	up := byKey[node.ParentID()]
	if up == nil || up == node {
		return ""
	}
	return up.title
}
