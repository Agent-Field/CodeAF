package tui3

import (
	"github.com/charmbracelet/x/ansi"
)

// ── THE SEAM INSIDE A TASK'S ROOM ────────────────────────────────────────────
//
// A room's box talks to a node, and the line over it is the box seam with the
// node's facts on it — the same four cells as a conversation's and a draft's
// (boxseam.go), with the way out where the name would be:
//
//	─ room · esc/←← main · task glm-5.2 (z-ai) · ⠿ high · ◇ on its own ──── ↑↓ history ─
//	 › steer the node
//
// Until this file the room's seam said `room · esc/←← main` and nothing else;
// the node's model and machine were on the STATUS ROW, as the identity
// cluster, and its rung was on the panel or nowhere. So the one box that
// looked least like the others was the one a person opens to correct running
// work. The cells move up here and the status row keeps the node's name alone
// — the row is numbers and aliveness (foot.go), and the model is who answers.
//
// THE THREE CELLS ARE THE NODE'S, NEVER THE CONVERSATION'S. What the seam
// says over a box is what that box talks to, and while a room is open that is
// a task: the model is the one it published (room.go's [app.roomModelWord],
// lead word and all), the rung is the one set on it or the floor it inherited
// (taskeffort.go), and the gate is a task's own posture — see below. Two of
// them are doors on the node, exactly as the panel's rows are: the model opens
// the node's picker and the rung walks its wheel, each only while the node can
// still be moved ([app.roomModelMovable], [app.taskRungMovable]).
//
// ── `◇ on its own` IS A READING, NOT A WHEEL ───────────────────────────────
//
// A node runs every tool without asking; the floor under dangerous shapes is
// a refusal it can read rather than a question (internal/session's task_run.go
// builds every worker with the allow policy and consent off). That is not a
// posture a person picks from the wheel — there is no stop on it a task can
// be walked to — so the cell says what the composer layer already promises
// when the work leaves: `it will run on its own`. It is drawn dim, is not a
// door, and `alt+a` inside a room says so ([approvalRoomWord]) rather than
// moving a gate that is not on the screen. The conversation's own gate is on
// its own seam, one esc away.
//
// AND THE STATUS ROW'S `YOLO` BADGE IS OFF INSIDE A ROOM, for the reason it is
// off under the conversation's chip: the seam carries the gate's word, and
// the badge was the conversation's fact drawn where the work it governs is
// not running.

// approvalTaskWord is the gate cell inside a room: the composer layer's own
// promise about a task, in the two words the chip has room for.
const approvalTaskWord = "on its own"

// approvalRoomWord is what `alt+a` answers inside a room.
const approvalRoomWord = "approvals · a task runs on its own — its tools do not ask · what this conversation runs without asking is on the conversation's own seam · esc"

// effortScopeRoom names the room's rung in [effortMoved]'s namespace, beside
// the conversation's and the draft's.
const effortScopeRoom = "\x00room"

// roomSeamIdentity is [app.seamIdentity] for a room: the same pieces, the
// same ladder, and the way out where the name goes.
func (a *app) roomSeamIdentity(room int, tier seamTier) (string, hudSpan, hudSpan, hudSpan) {
	pieces := a.roomSeamPieces()
	return seamLay(&pieces, room, tier)
}

// roomSeamPieces is the room's cluster as the seam's layout takes it. There
// is no host — the way out is the whole of the name — and no branch: a node
// works on its own tree and the page says so where it matters.
func (a *app) roomSeamPieces() seamPieces {
	name := roomLegendWord
	if a.recalling() {
		name = roomLegendRecallWord
	}
	pieces := seamPieces{name: name, model: a.roomModelWord()}
	if pieces.model != "" {
		pieces.rung = a.roomEffortChip()
		pieces.gate = a.roomGateChip()
		// THE RIDER IS WHOLE OR NOTHING: the node's machine is one sighting and
		// has no shorter spelling worth a cell.
		pieces.rider = a.roomLaneRider()
		rider := pieces.rider
		pieces.fit = func(room int) string {
			if rider != "" && ansi.StringWidth(rider) <= room {
				return rider
			}
			return ""
		}
	}
	return pieces
}

// roomEffortChip is the node's thinking cell: the rung set on it, else the
// floor it inherited — the conversation's own resolved rung, which is what
// the engine hands a worker as its default (task_run.go) — spelled by the
// same function the conversation's cell uses. "" where this window has no door
// onto a task's rung, on the absence law.
func (a *app) roomEffortChip() string {
	node := a.roomNode()
	if node == nil {
		return ""
	}
	if _, ok := a.taskEffortDoors(); !ok {
		return ""
	}
	word := a.taskRung(node.id).String()
	if word == "" {
		word = a.effortWord()
	}
	return a.effortChip(word)
}

// roomGateChip is the node's gate cell, and "" where there is no node to say
// it of — an orchestration's room, or a page whose node this window never
// received.
func (a *app) roomGateChip() string {
	if a.roomNode() == nil || a.room.orch != nil {
		return ""
	}
	mark := glyphPermTool
	if a.pal.ascii || a.pal.linear {
		mark = glyphPermToolASCII
	}
	return mark + " " + approvalTaskWord
}

// roomSeamDoors keeps only the spans that are doors on this node: the model's
// while it can be retargeted, the rung's while it can be moved, and never the
// gate's.
func (a *app) roomSeamDoors(model, rung hudSpan) (hudSpan, hudSpan, hudSpan) {
	if !a.roomModelMovable() {
		model = hudSpan{}
	}
	if !a.taskRungMovable(a.roomNode()) || a.roomIsGuest() {
		rung = hudSpan{}
	}
	return model, rung, hudSpan{}
}

// roomEffortFlashing is the room's rung wearing its last change — set by
// [app.cycleNodeEffort] when the node it moved is the one whose page is open.
func (a *app) roomEffortFlashing() bool { return a.effortFlashingIn(effortScopeRoom) }
