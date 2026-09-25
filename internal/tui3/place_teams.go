package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TEAMS PLACE ─────────────────────────────────────────────────────────
//
// The handle the registry files (pages.go's [place]), and the keys, the
// pointer and the acts of the page teamspage.go describes. The second place on
// the bar, right after home: `home  teams  tasks  spend  settings`.
//
// TWO SHAPES, ONE PLACE. With a manager in the pane the page hosts that
// conversation whole (teamspagehost.go): the frame is the conversation's own,
// laid out beside the rail, and the composer has the keyboard; `alt+↑↓` puts it
// on the page's buttons and `esc` gives it back. With no manager in the pane
// (a team without one, a closed team, the `All teams` row, no teams at all)
// the page is drawn in the shared place frame and has the keyboard itself.

// placeTeams is this place's handle (pages.go's [place] states the contract
// and why the handle holds no state of its own).
type placeTeams struct{ placeBase }

func init() { registerPlace(placeTeams{}) }

func (placeTeams) id() page     { return pageTeams }
func (placeTeams) word() string { return "teams" }

// counted is true: what is in here is a pile of things, and a packet waiting
// on the person is news about this place.
func (placeTeams) counted() bool { return true }

// open loads the teams at the door (the one read made on the loop, and it does
// not block, teamseam.go), settles the selection, brings the selected team's
// manager in front, and arms the router's beat, which the page answers.
func (placeTeams) open(a *app) tea.Cmd {
	a.teamsEnsure()
	a.tp.focus, a.tp.cur, a.tp.hot = false, teamsRef{}, teamsRef{}
	a.tp.answering, a.tp.msg = "", ""
	a.tp.top = teamsTopCache{}
	a.teamsSettle()
	// The keyboard starts on the selected team's row, where `↓` walks from.
	// A managed team's page opens with the keyboard on the manager's composer.
	a.tp.focus, a.tp.cur = true, teamsRef{act: teamsActSelect, id: a.tp.sel}
	if t, ok := a.teamsSelected(); ok && t.Manager != "" && !t.Closed() {
		a.tp.focus = false
	}
	return tea.Batch(a.armPlaceClock(), a.teamsRead(true), a.teamsBringManager())
}

// tick is the router's beat: the counts are the router's own, and the page
// reads the store on it only while an open team has a manager.
func (placeTeams) tick(a *app, now time.Time) (bool, tea.Cmd) {
	if !a.teamsManaged() {
		return true, nil
	}
	return true, a.teamsRead(true)
}

// close drops what the page drew; the selection and the
// fold are kept for the next visit, as a place's views are.
func (placeTeams) close(a *app) {
	a.tp.focus, a.tp.host, a.tp.targets = false, "", nil
	a.tp.answering = ""
	a.tp.top = teamsTopCache{}
	// A drag, the members card and a move waiting on its line belong to the
	// page and go with it; a move made keeps its Undo for the next visit.
	a.tdrag, a.tcrew = teamDrag{}, teamCrew{}
	a.tmove.pend = teamMovePend{}
}

func (placeTeams) body(a *app, width, room int) []placeRow { return a.teamsBody(width, room) }

// ownFrame is the hosted shape: the manager's own conversation beside the
// rail (teamspagehost.go). Every other shape is the shared frame's.
func (placeTeams) ownFrame(a *app, width, height int) ([]string, []placeHit, int, int, bool) {
	return a.teamsHostFrame()
}

// remote is the one line over --host against an engine without the teams
// doors: this window's own file is not the session's, and the page does not
// pretend it is.
func (placeTeams) remote(a *app) string {
	if a.teamsOff() {
		return teamHostedWord
	}
	return ""
}

// stops is every body line with a target on it, top first: the router's
// names for rows, which a scrolled pane does not move.
func (placeTeams) stops(a *app) []int {
	out := make([]int, 0, len(a.tp.targets))
	for _, t := range a.tp.targets {
		out = append(out, t.line)
	}
	sort.Ints(out)
	return out
}

// cursorAt is the body line the keyboard is on, the first stop when it is on
// none, so `↑` from the first target reaches the bar.
func (placeTeams) cursorAt(a *app) int {
	if t, ok := a.teamsCursorTarget(); ok {
		return t.line
	}
	return 0
}

func (placeTeams) rowID(a *app) string {
	if !a.tp.focus {
		return ""
	}
	r := a.tp.cur
	return itoa(int(r.act)) + ":" + r.id + ":" + r.arg + ":" + r.opt
}

// note is the page's one line of news, and otherwise the one fact under the
// rows that a person may not see anywhere else: that the manager has the
// keyboard or that the page does.
func (placeTeams) note(a *app, width int) []string {
	if a.tp.msg != "" {
		return []string{" " + a.pal.dim(noteFit(a.tp.msg, width-2))}
	}
	return nil
}

// hint is what the pointer or the cursor is on, with its key, and otherwise
// the page's keys.
func (placeTeams) hint(a *app) string {
	if a.tmove.on {
		return a.teamMoveHint()
	}
	if a.tsheet.on {
		return a.teamSheetHint()
	}
	if words := a.teamDragHint(); words != "" {
		return words
	}
	if a.tcrew.on {
		return a.teamCrewHint()
	}
	if words := a.teamsTargetHint(); words != "" {
		return words
	}
	if n := len(a.teamsPickedIDs()); n > 0 {
		return itoa(n) + " picked · m moves them into… · space picks · esc clears"
	}
	if a.tp.answering != "" {
		return "enter decide · esc put it away"
	}
	if !a.teamsAny() {
		return "o organize · n new team · " + homeDoorWord
	}
	return "↑↓ walk · enter open · m move into… · space pick · p members · s settings · c close · n new team · o organize · " + homeDoorWord
}

// changed is how many packets wait on the person, which is the count a person
// wants beside `teams` on the bar.
func (placeTeams) changed(a *app, since time.Time) int {
	n := 0
	for _, p := range a.tp.packets {
		if p.Waiting() && p.Team == teamstore.Person && p.At.After(since) {
			n++
		}
	}
	return n
}

// summary is what is behind the place, for home's typed drop-up.
func (placeTeams) summary(a *app) string {
	n := 0
	for _, t := range a.wall.teams {
		if !t.Root && !t.Closed() {
			n++
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return "1 team"
	}
	return itoa(n) + " teams"
}

// owns is the `Your own answer…` box, which has the whole keyboard while it is
// open, as every box inside a place does.
func (placeTeams) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.tp.answering == "" {
		return nil, false
	}
	return a.teamsAnswerKey(msg), true
}

// key is the page's own keys, the router's classes read first (placekeys.go).
func (placeTeams) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	cmd, _ := a.teamsKey(msg)
	return cmd
}

// wheel walks the page's buttons exactly as the arrows do, a row a notch.
func (placeTeams) wheel(a *app, delta int) (tea.Cmd, bool) {
	step := tea.KeyPressMsg{Code: tea.KeyDown}
	if delta < 0 {
		step, delta = tea.KeyPressMsg{Code: tea.KeyUp}, -delta
	}
	for i := 0; i < delta; i++ {
		a.teamsKey(step)
	}
	a.tp.top = teamsTopCache{}
	a.touch()
	return nil, true
}

func (placeTeams) enter(a *app) tea.Cmd {
	if t, ok := a.teamsCursorTarget(); ok {
		return a.teamsDo(t)
	}
	return nil
}

// ── THE KEYBOARD ────────────────────────────────────────────────────────────

// teamsHasKeys reports whether the page, rather than a hosted composer, has
// the keyboard.
func (a *app) teamsHasKeys() bool { return a.tp.host == "" || a.tp.focus }

// teamsCursorIndex is the index of the target the cursor is on, -1 for none.
func (a *app) teamsCursorIndex() int {
	if !a.tp.focus && a.tp.host != "" {
		return -1
	}
	for i, t := range a.tp.targets {
		if t.ref() == a.tp.cur {
			return i
		}
	}
	return -1
}

// teamsCursorTarget is the target the cursor is on.
func (a *app) teamsCursorTarget() (teamsTarget, bool) {
	if i := a.teamsCursorIndex(); i >= 0 {
		return a.tp.targets[i], true
	}
	return teamsTarget{}, false
}

// teamsCursorHome puts the cursor on the selected team's rail row, or on the
// first target when that is not drawn.
func (a *app) teamsCursorHome() {
	for _, t := range a.tp.targets {
		if t.act == teamsActSelect && t.id == a.tp.sel {
			a.tp.cur = t.ref()
			return
		}
	}
	if len(a.tp.targets) > 0 {
		a.tp.cur = a.tp.targets[0].ref()
	}
}

// teamsWalk moves the cursor to the nearest target on the row above (dy -1)
// or below (dy 1), keeping as close to its column as it can, or along its row
// for dx. It reports whether it moved.
func (a *app) teamsWalk(dx, dy int) bool {
	at := a.teamsCursorIndex()
	if at < 0 {
		a.teamsCursorHome()
		return true
	}
	here := a.tp.targets[at]
	order := make([]int, len(a.tp.targets))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		ti, tj := a.tp.targets[order[i]], a.tp.targets[order[j]]
		if ti.y != tj.y {
			return ti.y < tj.y
		}
		return ti.x0 < tj.x0
	})
	// ↑ AND ↓ STAY ON THEIR SIDE: the rail walks the rail and the pane walks
	// the pane, so a walk down the teams is never pulled into a card beside
	// them. ← and → walk along a row, and past its end cross to the nearest
	// target on the other side.
	best, bestScore := -1, 1<<30
	for _, i := range order {
		t := a.tp.targets[i]
		if i == at {
			continue
		}
		var score int
		switch {
		case dy < 0 && t.y < here.y && t.pane == here.pane:
			score = (here.y-t.y)*1000 + abs(t.x0-here.x0)
		case dy > 0 && t.y > here.y && t.pane == here.pane:
			score = (t.y-here.y)*1000 + abs(t.x0-here.x0)
		case dx < 0 && t.y == here.y && t.x0 < here.x0:
			score = here.x0 - t.x0
		case dx > 0 && t.y == here.y && t.x0 > here.x0:
			score = t.x0 - here.x0
		case dx < 0 && here.pane && !t.pane, dx > 0 && !here.pane && t.pane:
			score = 100000 + abs(t.y-here.y)*1000 + abs(t.x0-here.x0)
		default:
			continue
		}
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return false
	}
	a.tp.cur = a.tp.targets[best].ref()
	return true
}

// teamsLetters are the page's bare-letter keys, each the button of the same
// word on the selected team.
//
// `m` IS MOVE INTO… (ruling c-12), so starting a manager moved to `M`: the
// ruling names the letter, and a manager is started once per team while a
// team is moved whenever the tree is reshaped.
var teamsLetters = map[string]teamsAct{
	"s": teamsActSettings, "c": teamsActClose, "w": teamsActWall, "n": teamsActNewTeam,
	"o": teamsActOrganize, "M": teamsActManager, "r": teamsActReopen, "d": teamsActDelete,
}

// teamsMoveLetter is `Move into…`, on the selected team or the picked ones.
const teamsMoveLetter = "m"

// teamsKey is a key while the page has the keyboard. It reports whether it
// took it.
func (a *app) teamsKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch key {
	case "up", "k":
		if !a.tp.focus {
			a.tp.focus = true
			a.teamsCursorHome()
		} else {
			// The first target's `↑` is the router's: it reaches the bar
			// ([app.barReach]) before this key is asked.
			a.teamsWalk(0, -1)
		}
		a.touch()
		return nil, true
	case "down", "j":
		if !a.tp.focus {
			a.tp.focus = true
			a.teamsCursorHome()
		} else {
			a.teamsWalk(0, 1)
		}
		a.touch()
		return nil, true
	case "left", "h":
		a.tp.focus = true
		if !a.teamsWalk(-1, 0) {
			a.teamsCursorHome()
		}
		a.touch()
		return nil, true
	case "right", "l":
		a.tp.focus = true
		a.teamsWalk(1, 0)
		a.touch()
		return nil, true
	case "enter", "space":
		t, ok := a.teamsCursorTarget()
		if !ok {
			a.tp.focus = true
			a.teamsCursorHome()
			a.touch()
			return nil, true
		}
		// SPACE PICKS A TEAM ON THE RAIL, as it picks a tile on the wall, so
		// several can be moved with one `Move into…`.
		if key == "space" && t.act == teamsActSelect && !t.pane {
			if u, ok := a.teamByID(t.id); ok && !u.Root && !u.Closed() {
				a.teamsPick(t.id)
				return nil, true
			}
		}
		return a.teamsDo(t), true
	case teamsMoveLetter:
		ids := a.teamsMoveIDs()
		if len(ids) == 0 {
			return nil, true
		}
		return a.teamMoveOpen(ids, teamMoveFromPage), true
	case teamCrewLetter:
		if t, ok := a.teamsSelected(); ok && !t.Closed() {
			return a.teamCrewOpen(t.ID), true
		}
		return nil, true
	case "esc":
		if len(a.tp.picked) > 0 {
			a.tp.picked = nil
			a.tp.top = teamsTopCache{}
			a.touch()
			return nil, true
		}
		if a.tp.host != "" {
			a.tp.focus = false
			a.touch()
			return nil, true
		}
		a.leavePlace()
		return nil, true
	}
	if act, ok := teamsLetters[key]; ok {
		return a.teamsLetter(act), true
	}
	return nil, false
}

// teamsLetter is one of the bare letters: the act on the selected team, when
// the page offers it there.
func (a *app) teamsLetter(act teamsAct) tea.Cmd {
	for _, t := range a.tp.targets {
		if t.act != act {
			continue
		}
		switch act {
		case teamsActNewTeam, teamsActOrganize, teamsActRootManager:
			return a.teamsDo(t)
		}
		if t.id == a.tp.sel {
			return a.teamsDo(t)
		}
	}
	if act == teamsActManager {
		for _, t := range a.tp.targets {
			if t.act == teamsActRootManager {
				return a.teamsDo(t)
			}
		}
	}
	return nil
}

// teamsAnswerKey is a key in the `Your own answer…` box.
func (a *app) teamsAnswerKey(msg tea.KeyPressMsg) tea.Cmd {
	box := &a.tp.answer
	switch msg.String() {
	case "esc":
		a.tp.answering = ""
		box.reset()
	case "enter":
		words := strings.TrimSpace(box.String())
		id := a.tp.answering
		if words == "" {
			return nil
		}
		a.tp.answering = ""
		box.reset()
		a.touch()
		return a.teamsDecide(id, "", words)
	case "backspace":
		box.deleteBackward()
	case "left":
		box.left()
	case "right":
		box.right()
	default:
		if text := msg.Key().Text; text != "" {
			box.insert(text)
		}
	}
	a.touch()
	return nil
}

// ── THE POINTER ─────────────────────────────────────────────────────────────

// teamsTargetAt is the target under the pointer on the last frame.
func (a *app) teamsTargetAt(x, y int) (teamsTarget, bool) {
	for _, t := range a.tp.targets {
		if y == t.y && x >= t.x0 && x < t.x1 {
			return t, true
		}
	}
	return teamsTarget{}, false
}

// teamsTargetHint is what the hint line says over the target under the pointer,
// or under the cursor while the page has the keyboard.
func (a *app) teamsTargetHint() string {
	if a.tp.hot != (teamsRef{}) {
		for _, t := range a.tp.targets {
			if t.ref() == a.tp.hot {
				return t.hint
			}
		}
	}
	if t, ok := a.teamsCursorTarget(); ok {
		return t.hint
	}
	return ""
}

// teamsHover lights the target under the pointer, repainting only when that
// moved.
func (a *app) teamsHover(x, y int) {
	t, _ := a.teamsTargetAt(x, y)
	if r := t.ref(); r != a.tp.hot {
		a.tp.hot = r
		a.tp.top = teamsTopCache{}
		a.touch()
	}
}

// teamsPress is a left press on the page: a target does what it says, and the
// keyboard stays where it was.
func (a *app) teamsPress(x, y int) (tea.Cmd, bool) {
	t, ok := a.teamsTargetAt(x, y)
	if !ok {
		return nil, false
	}
	return a.teamsDo(t), true
}
