package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE RAIL: WHERE A PERSON READS THE TRAFFIC ─────────────────────────────
//
// While the manager is the conversation in front, the right of the body is the
// team's Traffic as threads, the one that moved last at the top, under a
// header that says what it is and how to put it away (teamthread.go draws
// the threads):
//
//	Traffic                          hide alt+l
//	◆ manager → @web @parser  do             2m
//	  take the scope model and the lexer
//	├ @parser  ✓ the lexer is in             1m
//	└ @web  asking: may I run the migration  now   (the needs-you amber)
//
// THE RIGHT COLUMN IS TRAFFIC'S FIRST. The task column and the rail are both
// right-hand panels, and two of them side by side left the conversation ninety
// columns on a wide frame and folded the rail to nothing at 110. So while the
// manager is in front and the rail stands, the task column folds to its own
// edge (task.go's [app.railStowed]), with its whisper of what the work is doing
// still in it, and a press on that edge or ctrl+g brings the tasks back by
// putting the Traffic away. It is one column with two things to show, the way
// an editor's side panel is.
//
// PUT AWAY, OR ON A FRAME TOO NARROW FOR IT, the rail is an edge: the word
// `Traffic` down the right, with a quiet count of what arrived since the person
// last had it in front of them. On a wide frame a press on the edge brings the
// column back; on a narrow one it lays the Traffic over the lower part of the
// body as a card with its own border, `Close esc` in its foot, and the top of
// the conversation still in view above it. [trafficKey] does the same from
// the keyboard, and `esc` closes the card.
//
// EVERY HANDLE IS A DOOR to its member, and a message's words are a door that
// lays them out in full; a word about the whole team (`everyone`) is not, and
// does not light. Under the pointer the hint line says what a press does, and
// over a message its whole text.
//
// THE FRAME DRAWS THE CACHE AND NOTHING ELSE, and the column's rows are kept
// between frames ([trafficCache]) until the entries, the width, the pointer or
// the minute move.

const (
	// The column takes three tenths of the frame left after the task column's
	// edge, between trafficColsMin and trafficColsMax, and never leaves the
	// conversation under trafficBodyFloor. At 110 columns that is 32 of 108.
	trafficColsMin   = 28
	trafficColsMax   = 44
	trafficBodyFloor = 56
	// trafficGripCols is the edge's width, the task column's own
	// ([railGripCols]).
	trafficGripCols = 2
	// trafficHandleCap is the most cells one address takes on a row.
	trafficHandleCap = 10
	// trafficKey shows or hides the Traffic, and teamManagerKey goes to the
	// team's manager. Both were free in keys.md and in every key map here.
	trafficKey     = "alt+l"
	teamManagerKey = "alt+m"
	// trafficCardShare is how much of the body's height the narrow frame's card
	// takes, in hundredths.
	trafficCardShare = 60
	// trafficWord is the rail's name, in its header, on its edge and on the card.
	trafficWord = "Traffic"
)

// The rail's three shapes on the last frame.
const (
	trafficNone = iota
	trafficColumn
	trafficEdge
	trafficCard
)

// trafficDrawn is where the last frame put the rail, for the pointer. Columns
// are the frame's; rows count from the body's top.
type trafficDrawn struct {
	mode   int
	x0, x1 int
	y0, y1 int
	// doors is, per body row, the row's doors in the frame's columns
	// (teamthread.go), and hints what the hint line says with the pointer on
	// the row's header words (the hide word's row).
	doors [][]trafficDoor
	hints []string
	// hide is the header's `hide` word, on row hideY, tab its `Tasks 2` word
	// on the same row, and close the card's `Close esc`, on row closeY.
	hide   hudSpan
	tab    hudSpan
	hideY  int
	close  hudSpan
	closeY int
}

// trafficCacheKey is everything the column's rows are drawn from.
type trafficCacheKey struct {
	team, last, seen    string
	rows, width, height int
	hot, hotDoor        int
	open, tasks         int
	hideHot, tabHot     bool
	ascii               bool
	minute              int64
}

// trafficCache is the column's rows as last drawn.
type trafficCache struct {
	key   trafficCacheKey
	out   []string
	doors [][]trafficDoor
	hints []string
	hide  hudSpan
	tab   hudSpan
}

// ── WHERE IT STANDS ─────────────────────────────────────────────────────────

// trafficColsFor is the column the rail takes from room columns, 0 when it
// cannot stand there.
func trafficColsFor(room int) int {
	cols := min(max(room*3/10, trafficColsMin), trafficColsMax)
	if room-cols < trafficBodyFloor {
		return 0
	}
	return cols
}

// trafficOn reports whether the frame has a rail in any shape: the manager in
// front, on a window that can read the engine's teams. Frame-safe, allocation
// free: it is asked wherever [app.bodyWidth] is.
func (a *app) trafficOn() bool {
	if a.teamsOff() {
		return false
	}
	_, ok := a.teamFrontManaged()
	return ok
}

// WITH THE MANAGER IN FRONT THE RIGHT COLUMN IS THE TRAFFIC (ruled
// 2026-09-24). The task column is not drawn and not reserved, whatever the
// saved ctrl+g answer says, so no folded task edge stands beside the traffic
// and no folded traffic edge beside an empty task column. Only when the manager
// has live tasks of its own does the header offer them, `Traffic · Tasks 2`,
// and choosing Tasks lays the task list in the same column
// ([app.trafficTasksShowing]); the task column's own code draws it, at this
// column's width ([app.railColumns]).

// trafficTasksWord is the header's word for the manager's own tasks, and
// trafficTabSep what stands between it and `Traffic`.
const (
	trafficTasksWord = "Tasks"
	trafficTabSep    = " · "
)

// trafficTasksHead is the column's header while it shows the manager's tasks:
// `Traffic · Tasks 2` with Tasks the current word, and the key back at the
// right. The whole line is the way back to the traffic.
func (a *app) trafficTasksHead(width int) string {
	word := trafficTasksWord + " " + itoa(a.trafficTaskCount())
	head := a.pal.dim(trafficWord) + a.pal.dim(trafficTabSep) + a.pal.bold(a.pal.ink(word))
	used := len(trafficWord) + ansi.StringWidth(trafficTabSep) + ansi.StringWidth(word)
	back := railStowKey + " traffic"
	if gap := width - used - ansi.StringWidth(back); gap >= 2 {
		head += strings.Repeat(" ", gap) + a.pal.dim(back)
	}
	return fit(head, width)
}

// trafficFits reports whether this frame is wide enough for the column.
func (a *app) trafficFits() bool {
	width, _ := a.size()
	return trafficColsFor(width) > 0
}

// trafficTaskCount is how many of the manager's own tasks are live: asking,
// running, admitted or parked. 0 without the manager in front.
func (a *app) trafficTaskCount() int {
	if !a.trafficOn() || len(a.taskOrder) == 0 {
		return 0
	}
	members := a.railMembers()
	n := 0
	for g := railAttention; g < railDone; g++ {
		n += len(members[g])
	}
	return n
}

// trafficTasksShowing reports whether the column shows the manager's tasks
// instead of the traffic: the person chose Tasks, there are live ones, and the
// column is up on a frame wide enough for it.
func (a *app) trafficTasksShowing() bool {
	return a.traffic.tasks && !a.traffic.hidden && a.trafficOn() && a.trafficFits() && a.trafficTaskCount() > 0
}

// trafficTasksShow swaps the column between the traffic and the manager's
// tasks, and brings the column back if it was put away. With no live tasks it
// is the traffic, and there is nothing to swap to.
func (a *app) trafficTasksShow(on bool) {
	a.traffic.tasks = on && a.trafficTaskCount() > 0
	if a.traffic.hidden && a.trafficFits() {
		a.traffic.hidden = false
	}
	if !a.traffic.tasks {
		a.railHold = false
	}
	a.dropHover()
	a.touch()
}

// trafficHoldsRail reports whether the Traffic has the right-hand column now.
func (a *app) trafficHoldsRail() bool {
	return a.trafficOn() && !a.traffic.hidden && a.trafficFits() && !a.trafficTasksShowing()
}

// trafficWidth is what the rail costs the conversation, in columns: its column
// where it holds the right, the edge where it is away or cannot stand, and
// nothing without the manager in front.
func (a *app) trafficWidth() int {
	if !a.trafficOn() || a.trafficTasksShowing() {
		return 0
	}
	if !a.traffic.hidden {
		width, _ := a.size()
		if cols := trafficColsFor(width); cols > 0 {
			return cols
		}
	}
	return trafficGripCols
}

// trafficOverShowing reports whether the card is laid over the body.
func (a *app) trafficOverShowing() bool {
	return a.traffic.over && a.trafficOn() && !a.trafficFits()
}

// ── ONE ROW ─────────────────────────────────────────────────────────────────

// trafficAddr is one address of an entry as the rail spells it: `@handle`, the
// manager's mark, or the word for everyone, cut to [trafficHandleCap].
func (a *app) trafficAddr(s string) string {
	switch s {
	case teamstore.FromManager:
		return a.teamManagerMark()
	case teamstore.FromSystem:
		return "codeaf"
	case teamstore.ToEveryone:
		return "all"
	case teamstore.ToRoom:
		return "room"
	case "":
		return ""
	}
	word := "@" + s
	if ansi.StringWidth(word) > trafficHandleCap {
		word = ansi.Truncate(word, trafficHandleCap, a.linearMark("…", "~"))
	}
	return word
}

// trafficShown reports whether an entry is drawn at all. The person's own
// words are in the manager's conversation already, where they said them. A
// wake is drawn only as the `working…` of the thread it answers
// (teamthread.go), so one that answers nothing, and a member waking the
// manager, which the manager's own turn already shows, are not drawn.
func trafficShown(e teamstore.Entry) bool {
	if e.Kind == teamstore.KindYou || e.From == teamstore.FromYou {
		return false
	}
	if e.Wake() {
		return e.Answers != "" && e.From == teamstore.FromManager
	}
	return true
}

// trafficAsking reports whether an event is a member waiting on the person,
// the one row on the rail in the needs-you amber.
func trafficAsking(e teamstore.Entry) bool {
	if e.Kind != teamstore.KindEvent {
		return false
	}
	if e.State != "" {
		return e.State == teamstore.StateAsking
	}
	text := strings.ToLower(strings.TrimSpace(e.Text))
	return strings.HasPrefix(text, "ask") || strings.HasPrefix(text, "needs you")
}

// trafficAge is how long ago an entry was written, in the fewest cells.
func (a *app) trafficAge(e teamstore.Entry) string {
	if e.At.IsZero() {
		return ""
	}
	d := a.now().Sub(e.At)
	switch {
	case d < 60e9:
		return "now"
	case d < 3600e9:
		return itoa(int(d/60e9)) + "m"
	case d < 86400e9:
		return itoa(int(d/3600e9)) + "h"
	}
	return itoa(int(d/86400e9)) + "d"
}

// trafficUnseen is how many drawn entries of team t arrived after the newest
// the person had in front of them.
func (a *app) trafficUnseen(t team) int {
	seen := a.traffic.seen[t.ID]
	rows := a.traffic.rows[t.ID]
	n := 0
	for i := len(rows) - 1; i >= 0 && rows[i].ID > seen; i-- {
		if trafficShown(rows[i]) && !rows[i].Wake() {
			n++
		}
	}
	return n
}

// trafficMarkSeen records that the person has team t's newest entry in front of
// them. It is memory, written by the frame that showed it.
func (a *app) trafficMarkSeen(t team) {
	rows := a.traffic.rows[t.ID]
	if len(rows) == 0 {
		return
	}
	if a.traffic.seen == nil {
		a.traffic.seen = map[string]string{}
	}
	a.traffic.seen[t.ID] = rows[len(rows)-1].ID
}
