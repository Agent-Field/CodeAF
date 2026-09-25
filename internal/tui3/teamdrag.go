package tui3

import (
	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── DRAG AND DROP IN THE TEAMS RAIL: THE SHORTCUT (rulings c-12, c-13) ──────
//
// `Move into…` is the main road; a drag is the shortcut for a person whose
// hand is already on the pointer. Two things can be dragged:
//
//   - A TEAM ROW of the rail, onto another team row (`Drop to move api into
//     harbor`) or onto the empty rail under the tree, which is the top level.
//     Teams picked with `space` travel together when the one dragged is among
//     them. The move then goes exactly the way the picker's does
//     ([app.teamMoveAsk]): at once with Undo, or after the one consequence line.
//   - A MEMBER, from the members card, onto a team row. That ADDS the
//     conversation to the team (`Add @security to harbor`) and never takes it
//     out of the one it was dragged from: removing a conversation from a team is
//     always its own explicit act, so a drag cannot lose one.
//
// A DRAG STARTS ONLY AFTER TWO CELLS OF HELD MOVEMENT. A press that lets go
// where it landed, or a cell away, is a click and does what a click does (a
// team row selects its team, a member opens), because a hand on a mouse is
// never still and a one-cell wobble must not turn a click into a move.
//
// ONLY A TARGET THAT CAN TAKE THE DROP IS GROUNDED. Every other row under the
// pointer stays as it is and the hint line says why it will not take it, in
// the picker's own words. `esc` drops the drag and nothing happens.
//
// Like everything on the page it is memory: the rows and their targets are the
// last frame's ([teamsTarget]), and the write is an ordinary edit.

// teamDragCells is how far a held press must move before it is a drag.
const teamDragCells = 2

// teamDrag is one press on something that can be dragged, and the drag it
// became.
type teamDrag struct {
	// press says a press is held on a draggable thing; on says it moved far
	// enough to be a drag.
	press, on bool
	// member says a member is dragged (key, from team id) rather than team id.
	member bool
	id     string
	key    string
	// x0 and y0 are where the press landed.
	x0, y0 int
	// over is what a release here would drop on: a team id, [teamMoveTop], or
	// "" for nothing; ok says it takes the drop and why says why it does not.
	over string
	ok   bool
	why  string
	// click is what the press does when it lets go without a drag, for a
	// target whose press waits for the release.
	click   teamsTarget
	clicked bool
}

// teamDragPress holds a press on team row id, or on member key of team from.
func (a *app) teamDragPress(x, y int, member bool, id, key string, click teamsTarget, deferred bool) {
	a.tdrag = teamDrag{press: true, member: member, id: id, key: key, x0: x, y0: y, click: click, clicked: deferred}
}

// teamDragging reports whether a drag is under way.
func (a *app) teamDragging() bool { return a.tdrag.on }

// teamDragIDs is the teams a team drag moves: every team picked with `space`
// when the dragged one is among them, else the dragged one alone.
func (a *app) teamDragIDs() []string {
	if a.tp.picked[a.tdrag.id] {
		return a.teamsPickedIDs()
	}
	return []string{a.tdrag.id}
}

// teamDragMotion is the pointer moving while a press is held. It reports
// whether the drag took the motion.
func (a *app) teamDragMotion(x, y int, held bool) bool {
	d := &a.tdrag
	if !d.press {
		return false
	}
	if !held {
		// The release was never heard (a terminal that drops it): the press is
		// over, and nothing is dropped.
		*d = teamDrag{}
		a.touch()
		return false
	}
	if !d.on {
		if abs(x-d.x0) < teamDragCells && abs(y-d.y0) < teamDragCells {
			return true
		}
		d.on = true
		a.tp.hot = teamsRef{}
	}
	over, ok, why := a.teamDropAt(x, y)
	if over != d.over || ok != d.ok || why != d.why {
		d.over, d.ok, d.why = over, ok, why
		a.tp.top = teamsTopCache{}
		a.touch()
	}
	return true
}

// teamDropAt is what a release at x, y would drop on: a team row, the All
// teams row or the empty rail under the tree (the top level), each with
// whether it takes this drag and why not.
func (a *app) teamDropAt(x, y int) (string, bool, string) {
	d := a.tdrag
	lastTree := -1
	for _, t := range a.tp.targets {
		if t.pane || t.act != teamsActSelect {
			continue
		}
		if tt, ok := a.teamByID(t.id); ok && tt.Closed() {
			continue
		}
		lastTree = max(lastTree, t.y)
	}
	for _, t := range a.tp.targets {
		if t.pane || y != t.y || x < t.x0 || x >= t.x1 {
			continue
		}
		switch {
		case t.act == teamsActSelect && t.id != teamsAllRow:
			tt, ok := a.teamByID(t.id)
			if !ok {
				return "", false, ""
			}
			if tt.Root {
				return a.teamDropJudge(teamMoveTop)
			}
			return a.teamDropJudge(tt.ID)
		case t.act == teamsActSelect || t.act == teamsActRootManager:
			return a.teamDropJudge(teamMoveTop)
		}
	}
	// THE EMPTY RAIL UNDER THE TREE IS THE TOP LEVEL, for a team; a member has
	// no top level to go to.
	if !d.member && a.tp.railW > 0 && x < a.tp.railW-1 && lastTree >= 0 && y > lastTree {
		return a.teamDropJudge(teamMoveTop)
	}
	return "", false, ""
}

// teamDropJudge is whether the drag can drop on target, and why not.
func (a *app) teamDropJudge(target string) (string, bool, string) {
	d := a.tdrag
	if !d.member {
		ok, why := a.teamMoveCheck(a.teamDragIDs(), target)
		return target, ok, why
	}
	name := a.teamDragMemberName()
	if target == teamMoveTop {
		return target, false, "drop " + name + " on a team to add it"
	}
	t, ok := a.teamByID(target)
	switch {
	case !ok:
		return "", false, ""
	case t.Closed():
		return target, false, t.Name + " is closed"
	case t.Holds(d.key):
		return target, false, name + " is in " + t.Name + " already"
	}
	return target, true, ""
}

// teamDragMember is the member being dragged, as its team keeps it.
func (a *app) teamDragMember() (teamMember, bool) {
	if t, ok := a.teamByID(a.tdrag.id); ok {
		return t.Member(a.tdrag.key)
	}
	return teamMember{}, false
}

// teamDragMemberName is the dragged member as a sentence says it: `@handle`,
// else its title.
func (a *app) teamDragMemberName() string {
	m, ok := a.teamDragMember()
	switch {
	case !ok:
		return "the conversation"
	case m.Handle != "":
		return "@" + m.Handle
	case m.Word != "":
		return m.Word
	}
	return "the conversation"
}

// teamDragRelease is the press let go: a drag drops, a press that never moved
// far enough does what its click does.
func (a *app) teamDragRelease() (tea.Cmd, bool) {
	d := a.tdrag
	if !d.press {
		return nil, false
	}
	a.tdrag = teamDrag{}
	a.tp.top = teamsTopCache{}
	a.touch()
	if !d.on {
		if d.clicked {
			return a.teamsDo(d.click), true
		}
		return nil, true
	}
	if d.over == "" || !d.ok {
		if d.why != "" {
			a.tp.msg = d.why
		}
		return nil, true
	}
	if d.member {
		return a.teamDropMember(d), true
	}
	ids := []string{d.id}
	if a.tp.picked[d.id] {
		ids = a.teamsPickedIDs()
		a.tp.picked = nil
	}
	return a.teamMoveAsk(ids, d.over, teamMoveFromPage), true
}

// teamDropMember adds the dragged member to the team it was dropped on, with
// what its own team kept of it, which is enough to open it again there.
func (a *app) teamDropMember(d teamDrag) tea.Cmd {
	src, ok := a.teamByID(d.id)
	if !ok {
		return nil
	}
	m, ok := src.Member(d.key)
	if !ok {
		return nil
	}
	target, _ := a.teamByID(d.over)
	add := teamstore.Member{Key: m.Key, File: m.File, Where: m.Where, Word: m.Word}
	name := a.teamDragMemberName()
	if err := a.teamEdit(func(f *teamstore.File) error { return f.AddMember(d.over, add) }); err != nil {
		a.tp.msg = name + " was not added: " + err.Error()
	} else {
		a.tp.msg = name + " is in " + target.Name + " too"
	}
	a.touch()
	return nil
}

// teamDragCancel drops the drag, and nothing happens.
func (a *app) teamDragCancel() {
	a.tdrag = teamDrag{}
	a.tp.top = teamsTopCache{}
	a.touch()
}

// teamDragHint is what the hint line says while a drag is under way, "" with
// none.
//
//	Drop to move api into harbor · esc cancels
func (a *app) teamDragHint() string {
	d := a.tdrag
	if !d.on {
		return ""
	}
	tail := hintSegment + "esc cancels"
	switch {
	case d.over == "":
		if d.member {
			return "Drag " + a.teamDragMemberName() + " onto a team to add it" + tail
		}
		return "Drag onto a team, or below the teams for the top level" + tail
	case !d.ok:
		return d.why + tail
	case d.member:
		return "Add " + a.teamDragMemberName() + " to " + a.teamNameOf(d.over) + tail
	}
	return "Drop to " + lowerFirst(a.teamMoveDoing(a.teamDragIDs(), d.over)) + tail
}

// lowerFirst is s with its first letter lowered: `Move api` inside a sentence.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]|0x20) + s[1:]
}

// teamDropLit reports whether team id's rail row is the drop the pointer is
// over and takes it, which is the only row grounded during a drag.
func (a *app) teamDropLit(id string) bool {
	d := a.tdrag
	return d.on && d.ok && d.over == id
}
