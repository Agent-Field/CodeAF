package tui3

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TEAMS PAGE: THE TEAM-LEVEL VIEW ─────────────────────────────────────
//
// The third of the three surfaces (DESIGN.md section 8.4, ruling c-b): the
// strip is what is open in this window, the wall is the same open set drawn
// big, and this page is the team as a team. Every member is on it whether this
// window has it open or not, with what it is doing and when it last moved; the
// packets waiting on the person are cards on it; and the selected team's
// manager is not summarised here, it IS here, as the real conversation
// (teamspagehost.go), so a person can run most of their work from this one
// page and step away.
//
//	home  teams  tasks  spend  settings
//	────────────────────────────────────────────────────────────────────────
//	 All teams   + Manager │ ◆ harbor   $1.20 of $5 today  Settings  Close…  Open ▦
//	 ● harbor          ⠿   │ @web running now · @api idle 2h · @docs not open 3d
//	   ● parser      ? 1   │
//	 ● orbit               │ ◆ conflict · raised by @web                waiting on you
//	                       │ which shape does the signup form send?
//	 + New team            │  JSON   @api changes the handler     recommended
//	 ✦ Organize            │
//	                       │ (the manager's own conversation, composer and all)
//	 ▸ Closed · 2          │
//
// THE LEFT RAIL IS THE TREE (teamspagedraw.go). One row per open team,
// indented by level, each with its colour dot and at most one mark, and a mark
// only when something is happening: `⠿` dim while a member works, and the
// needs-you amber `? N` while N things wait on the person. Idle draws nothing.
// Above the teams is `All teams`: with no root team it carries `+ Manager` (the
// optional global manager, which makes the root); with one it is the root and
// selects like any team. Below them `+ New team` and `✦ Organize`, and at the
// foot, folded, `Closed · N`.
//
// EVERYTHING HERE IS READ OFF THE LOOP, THROUGH THE SEAM, WITH A STAMP
// (teamseam.go). The page's clock turns only while the page is up AND some
// team has a manager, because nothing else writes packets or spends a team's
// money; each turn asks the seam for the packets and the spend with the stamps
// it holds, and a quiet turn is answered `same` and draws nothing new. The
// frame reads memory and nothing else (framedisk_law_test.go).

// teamsAllRow is the rail's `All teams` row while there is no root team behind
// it: a row the page draws over the top level with nothing stored.
const teamsAllRow = "\x00all"

// The rail's width: a fifth of the frame between its floor and ceiling, and
// none at all under teamsRailFloor, where the page is the pane alone (the rail
// is still walked with its keys).
const (
	teamsRailMin   = 22
	teamsRailMax   = 30
	teamsRailFloor = 72
)

// teamsInboxWhole is how many cards the inbox draws whole; past it each card is
// one line, still pressable.
const teamsInboxWhole = 3

// The page's words, quoted in the manual exactly as spelled here.
const (
	teamsExplainWord   = "A team is a set of conversations you run together; give it a manager and you talk to the manager, which hands out the work and asks you only what it cannot decide."
	teamsNoManagerWord = "a manager takes your messages to the team and asks you only what it cannot decide"
	teamsOrganizeWord  = "Organize my conversations"
	teamsNewTeamWord   = "New team"
	teamsHostedWord    = "the inbox and the spend are not available over this connection"
	teamsFocusKeys     = "alt+↑↓"
)

// teamsPage is the page's whole state, on the app (place_teams.go's handle
// holds none).
type teamsPage struct {
	// sel is the team the pane is about: a team id, [teamsAllRow], or "" for
	// none (no teams at all).
	sel string
	// closedOpen says the `Closed · N` fold is open.
	closedOpen bool
	// focus says the keyboard is on the page's buttons rather than on the
	// manager's composer. With no manager in the pane there is no composer and
	// the page always has the keyboard ([app.teamsHasKeys]).
	focus bool
	// cur is the target the keyboard is on and hot the one under the pointer,
	// each named by what it does rather than by where it was drawn, so a
	// frame that moved it keeps it lit.
	cur, hot teamsRef
	// targets is every pressable thing the last frame drew, in frame cells.
	targets []teamsTarget
	// expand is an inbox card beyond the first three that a press unfolded.
	expand string
	// railW is the rail's columns on the last sync, its separator included.
	railW int
	// host is the manager key the pane hosts, "" when it hosts none; forwarding
	// says a key or a pointer event is being handed to that conversation
	// (teamspagehost.go).
	host       string
	forwarding bool
	// traffic says the hosted manager's Traffic rail is out. It is folded by
	// default here, because this page has a rail of its own on the left.
	traffic bool
	// reading says a read is out, so a beat that comes round before it is
	// answered does not start a second.
	reading bool
	// The readings the frame draws from, each with the stamp the next read
	// asks the seam to answer `same` to.
	packets      []teamstore.Packet
	packetsStamp string
	packetsKnown bool
	spend        map[string]teamstore.Spend
	spendStamp   map[string]string
	defaults     teamstore.Defaults
	defaultsOK   bool
	world        map[string]session.SessionRow
	worldKnown   bool
	history      map[string][]teamstore.Packet
	// answering is the packet whose `Your own answer…` box is open, and answer
	// the box.
	answering string
	answer    editor
	// msg is the page's one line of news, said on its note.
	msg string
	// opening is the manager being brought in front, so the pane can say so
	// for the beat it takes.
	opening string
	// top is the hosted pane's header rows, kept between frames.
	top teamsTopCache
	// undo is the last close, while Undo is offered (teamclose.go).
	undo teamsUndo
}

// teamsTarget is one pressable thing on the page, in frame cells.
type teamsTarget struct {
	x0, x1, y int
	act       teamsAct
	// id is the team it acts on; arg and opt are the member key, the packet
	// id and option, or the session row's transcript and answer key.
	id, arg, opt string
	// hint is what the hint line says with the pointer or the cursor on it,
	// its key included.
	hint string
	// pane says the target is in the pane rather than on the rail. ↑ and ↓
	// walk within one of the two, and ← and → cross between them.
	pane bool
	// line is the body line the target is on with the pane unscrolled: the
	// router's name for a row ([place.stops]), which a scroll does not move.
	line int
}

// teamsRef names a target by what it does.
type teamsRef struct {
	act          teamsAct
	id, arg, opt string
}

// ref is the target's name.
func (t teamsTarget) ref() teamsRef { return teamsRef{act: t.act, id: t.id, arg: t.arg, opt: t.opt} }

// teamsAct is what a target does.
type teamsAct int

const (
	teamsActNone teamsAct = iota
	teamsActSelect
	teamsActRootManager
	teamsActNewTeam
	teamsActOrganize
	teamsActClosedFold
	teamsActManager
	teamsActSettings
	teamsActClose
	teamsActWall
	teamsActMember
	teamsActOption
	teamsActOwnAnswer
	teamsActPrompt
	teamsActReopen
	teamsActReopenParent
	teamsActDelete
	teamsActTraffic
	teamsActUndo
)

// ── THE TREE ────────────────────────────────────────────────────────────────

// teamsRailRow is one row of the rail's model.
type teamsRailRow struct {
	kind  int
	id    string
	depth int
}

const (
	railRowAll = iota
	railRowTeam
	railRowBlank
	railRowNew
	railRowOrganize
	railRowClosed
	railRowClosedTeam
)

// teamsRoot is the root team, false when there is none. Memory only.
func (a *app) teamsRoot() (team, bool) {
	for _, t := range a.wall.teams {
		if t.Root {
			return t, true
		}
	}
	return team{}, false
}

// teamsOpenTree is every open team that is not the root, in tree order: each
// top-level team (or each team directly under the root) and then its open
// sub-teams, depth first, in stored order, with its depth from 0.
func (a *app) teamsOpenTree() []teamsRailRow {
	root, hasRoot := a.teamsRoot()
	var out []teamsRailRow
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, t := range a.wall.teams {
			if t.Root || t.Closed() || t.Parent != parent {
				continue
			}
			out = append(out, teamsRailRow{kind: railRowTeam, id: t.ID, depth: depth})
			if depth < 10 {
				walk(t.ID, depth+1)
			}
		}
	}
	top := ""
	if hasRoot {
		top = root.ID
	}
	walk(top, 0)
	return out
}

// teamsClosed is every closed team, newest close first.
func (a *app) teamsClosed() []team {
	return a.teamTree().ClosedTeams()
}

// teamsRailRows is the rail, top to bottom. Memory only.
func (a *app) teamsRailRows() []teamsRailRow {
	rows := []teamsRailRow{{kind: railRowAll}}
	rows = append(rows, a.teamsOpenTree()...)
	rows = append(rows, teamsRailRow{kind: railRowBlank}, teamsRailRow{kind: railRowNew}, teamsRailRow{kind: railRowOrganize})
	if closed := a.teamsClosed(); len(closed) > 0 {
		rows = append(rows, teamsRailRow{kind: railRowBlank}, teamsRailRow{kind: railRowClosed})
		if a.tp.closedOpen {
			for _, t := range closed {
				rows = append(rows, teamsRailRow{kind: railRowClosedTeam, id: t.ID})
			}
		}
	}
	return rows
}

// teamsAny reports whether there is any team at all, closed ones included.
func (a *app) teamsAny() bool {
	for _, t := range a.wall.teams {
		if !t.Root {
			return true
		}
	}
	_, root := a.teamsRoot()
	return root
}

// teamsSelected is the team the pane is about, false for the `All teams` row
// with no root and for nothing selected.
func (a *app) teamsSelected() (team, bool) {
	if a.tp.sel == "" || a.tp.sel == teamsAllRow {
		return team{}, false
	}
	return a.teamByID(a.tp.sel)
}

// teamsSettle keeps the selection on something that exists: a team that went
// is replaced by the team of the conversation in front, else the first open
// team, else the root, else the `All teams` row, else nothing.
func (a *app) teamsSettle() {
	if a.tp.sel == teamsAllRow {
		if _, root := a.teamsRoot(); !root && a.teamsAny() {
			return
		}
		a.tp.sel = ""
	}
	if t, ok := a.teamByID(a.tp.sel); ok {
		if !t.Closed() || a.tp.closedOpen {
			return
		}
	}
	a.tp.sel = ""
	if t, ok := a.teamOfFront(); ok && !t.Closed() && !t.Root {
		a.tp.sel = t.ID
		return
	}
	if tree := a.teamsOpenTree(); len(tree) > 0 {
		a.tp.sel = tree[0].id
		return
	}
	if root, ok := a.teamsRoot(); ok {
		a.tp.sel = root.ID
		return
	}
	if a.teamsAny() {
		a.tp.sel = teamsAllRow
	}
}

// teamsManaged reports whether any open team has a manager, which is when the
// page's clock turns: nothing else writes packets or spends a team's money.
func (a *app) teamsManaged() bool {
	for _, t := range a.wall.teams {
		if t.Manager != "" && !t.Closed() {
			return true
		}
	}
	return false
}

// teamsSubtree reports whether id is sel or under it.
func (a *app) teamsSubtree(sel, id string) bool {
	if id == sel {
		return true
	}
	for _, t := range a.teamAncestors(id) {
		if t.ID == sel {
			return true
		}
	}
	return false
}

// ── WHAT A TEAM IS DOING ────────────────────────────────────────────────────

// teamsMemberState is one member as the page draws it: a word, whether it is
// the needs-you amber, and when it last moved ("" unknown).
type teamsMemberState struct {
	word   string
	asking bool
	open   bool
	at     time.Time
}

// teamsMember reads one member's state from memory: this window's own signal
// for a conversation it holds, and the world reading for one it does not.
func (a *app) teamsMember(m teamMember) teamsMemberState {
	row, known := a.tp.world[filepath.Clean(m.File)]
	st := teamsMemberState{}
	if known {
		st.at = row.At
	}
	if a.trafficHeld(m.Key) {
		st.open = true
		switch a.tabSignalFor(m.Key, m.Key == a.frontTabKey()) {
		case tabNeedsPerson:
			st.word, st.asking = "asking", true
		case tabWorking:
			st.word = "running"
		default:
			st.word = "idle"
		}
		if st.word == "running" {
			st.at = a.now()
		}
		return st
	}
	switch {
	case known && row.NeedsPerson():
		st.word, st.asking, st.open = "asking", true, true
	case known && row.Live && row.Presence.State != "" && string(row.Presence.State) != "idle":
		st.word, st.open = "running", true
	case known && row.Open:
		st.word, st.open = "open elsewhere", true
	default:
		st.word = "not open"
	}
	return st
}

// teamsNeeds is how many things in team id wait on the person: packets raised
// from it (or under it) that wait on the person, and members asking.
func (a *app) teamsNeeds(t team) int {
	n := 0
	for _, p := range a.tp.packets {
		if p.Team == teamstore.Person && p.Waiting() && a.teamsSubtree(t.ID, p.Origin) {
			n++
		}
	}
	for _, m := range t.Members {
		if a.teamsMember(m).asking {
			n++
		}
	}
	return n
}

// teamsWorking reports whether a member of t is running.
func (a *app) teamsWorking(t team) bool {
	for _, m := range t.Members {
		if a.trafficHeld(m.Key) && a.tabSignalFor(m.Key, m.Key == a.frontTabKey()) == tabWorking {
			return true
		}
	}
	return false
}

// teamsInbox is the packets the pane's inbox shows for the selection, oldest
// first so the newest is last: those waiting on the person that were raised in
// it or under it (every one of them on the root and the `All teams` row), and
// those waiting on its own manager.
func (a *app) teamsInbox() []teamstore.Packet {
	sel := a.tp.sel
	var out []teamstore.Packet
	root, hasRoot := a.teamsRoot()
	all := sel == teamsAllRow || hasRoot && sel == root.ID
	for _, p := range a.tp.packets {
		if !p.Waiting() {
			continue
		}
		switch {
		case p.Team == teamstore.Person && (all || a.teamsSubtree(sel, p.Origin)):
			out = append(out, p)
		case p.Team == sel && sel != "":
			out = append(out, p)
		}
	}
	return out
}

// teamsPrompts is every member of the selection whose conversation is stopped
// on a question the person can answer from here: the session's own offer, read
// off the world, never this window's own conversation in front (its card is in
// the pane already).
func (a *app) teamsPrompts(t team) []session.SessionRow {
	var out []session.SessionRow
	now := time.Now()
	for _, m := range t.Members {
		row, ok := a.tp.world[filepath.Clean(m.File)]
		if !ok || a.answeringHere(row) {
			continue
		}
		if _, ok := answerable(row, now); ok {
			out = append(out, row)
		}
	}
	return out
}

// ── THE SPEND AND THE CAP ───────────────────────────────────────────────────

// teamsPool is whose spend sits beside team t's cap, and the effective
// settings: the team itself with no cap or its own, the ancestor whose cap it
// inherits, and the top of the chain when the cap is the profile's default. A
// cap is a pool (DESIGN.md section 8.2), so the figure beside it is always the
// pool owner's.
func (a *app) teamsPool(t team) (string, teamstore.Effective) {
	e := a.teamTree().Effective(t.ID, a.tp.defaults)
	if e.CapUSDDay <= 0 {
		return t.ID, e
	}
	switch e.CapFrom.Kind {
	case teamstore.OriginTeam, teamstore.OriginAncestor:
		return e.CapFrom.Team, e
	}
	top := t.ID
	for _, up := range a.teamAncestors(t.ID) {
		if !up.Closed() {
			top = up.ID
		}
	}
	return top, e
}

// ── THE READ ────────────────────────────────────────────────────────────────

// teamsGot is what one read found, folded on the loop.
type teamsGot struct {
	packets      []teamstore.Packet
	packetsStamp string
	packetsSame  bool
	packetsErr   error
	defaults     teamstore.Defaults
	defaultsOK   bool
	spend        map[string]teamstore.Spend
	spendStamp   map[string]string
	world        map[string]session.SessionRow
	worldKnown   bool
	worldAsked   bool
	history      map[string][]teamstore.Packet
}

// THE PAGE HAS NO CLOCK OF ITS OWN. It answers the router's beat, which every
// place but home answers ([placeTeams.tick]), and on that beat it reads the
// store only while an open team has a manager: nothing else raises a packet
// or spends a team's money, so a page of plain teams reads nothing at all.
// Each read is stamped, so a quiet store answers `same` and the frame before
// it stands.

// teamsRead asks the seam, beside the door line, for what the page draws: the
// packets and the spend of the selection's pool (each with its stamp, so a
// quiet file is answered `same`), the defaults, and the world reading that says
// what a member this window does not hold is doing. withWorld is false for a
// read a gesture asked for, which needs only the store.
func (a *app) teamsRead(withWorld bool) tea.Cmd {
	if a.tp.reading || a.teamsOff() {
		return nil
	}
	a.tp.reading = true
	seam := a.teamsSeam()
	packetsSince := a.tp.packetsStamp
	var pools []string
	var closedReports []string
	if t, ok := a.teamsSelected(); ok {
		if t.Closed() {
			closedReports = append(closedReports, t.ID)
		} else {
			owner, _ := a.teamsPool(t)
			pools = append(pools, owner)
			if owner != t.ID {
				pools = append(pools, t.ID)
			}
		}
	}
	spendSince := map[string]string{}
	for _, id := range pools {
		spendSince[id] = a.tp.spendStamp[id]
	}
	worldDoor, placesRoot, hosted := a.world, a.placesRoot(), a.hosted()
	return a.besideLine(func() func(bool) tea.Cmd {
		got := teamsGot{spend: map[string]teamstore.Spend{}, spendStamp: map[string]string{}}
		if seam.delegation() {
			got.packets, got.packetsStamp, got.packetsSame, got.packetsErr = seam.Packets(teamstore.ScopeAll, packetsSince)
			if d, err := seam.Defaults(); err == nil {
				got.defaults, got.defaultsOK = d, true
			}
			for _, id := range pools {
				s, stamp, same, err := seam.Spend(id, "", spendSince[id])
				if err != nil || same {
					continue
				}
				got.spend[id], got.spendStamp[id] = s, stamp
			}
		}
		if seam.History != nil && len(closedReports) > 0 {
			got.history = map[string][]teamstore.Packet{}
			for _, id := range closedReports {
				if list, err := seam.History(id); err == nil {
					got.history[id] = list
				}
			}
		}
		if withWorld {
			got.worldAsked = true
			if world, known := worldSeam(worldDoor, placesRoot, hosted); known {
				got.worldKnown = true
				got.world = map[string]session.SessionRow{}
				for _, p := range world.Projects {
					for _, row := range p.Sessions {
						if strings.TrimSpace(row.Transcript) != "" {
							got.world[filepath.Clean(row.Transcript)] = row
						}
					}
				}
			}
		}
		return func(bool) tea.Cmd { return a.teamsFold(got) }
	})
}

// teamsFold folds one read in. A read that found nothing new leaves the frame
// before it standing: the clock costs a stat and draws nothing.
func (a *app) teamsFold(got teamsGot) tea.Cmd {
	a.tp.reading = false
	changed := false
	if got.packetsErr == nil && !got.packetsSame && got.packetsStamp != "" {
		a.tp.packets, a.tp.packetsStamp, a.tp.packetsKnown = got.packets, got.packetsStamp, true
		changed = true
	}
	if got.defaultsOK && (!a.tp.defaultsOK || got.defaults != a.tp.defaults) {
		a.tp.defaults, a.tp.defaultsOK = got.defaults, true
		changed = true
	}
	if len(got.spend) > 0 {
		if a.tp.spend == nil {
			a.tp.spend, a.tp.spendStamp = map[string]teamstore.Spend{}, map[string]string{}
		}
		for id, s := range got.spend {
			a.tp.spend[id], a.tp.spendStamp[id] = s, got.spendStamp[id]
		}
		changed = true
	}
	if got.worldAsked && got.worldKnown && !teamsSameWorld(a.tp.world, got.world) {
		a.tp.world, a.tp.worldKnown = got.world, true
		changed = true
	}
	if len(got.history) > 0 {
		if a.tp.history == nil {
			a.tp.history = map[string][]teamstore.Packet{}
		}
		for id, list := range got.history {
			a.tp.history[id] = list
		}
		changed = true
	}
	if changed {
		a.tp.top = teamsTopCache{}
		a.touch()
	} else {
		a.ptr.still = a.drawn
	}
	return nil
}

// teamsSameWorld reports whether two readings say the same about every
// conversation the page draws from them: its state, its question and when it
// last moved.
func teamsSameWorld(was, now map[string]session.SessionRow) bool {
	if len(was) != len(now) {
		return false
	}
	for k, r := range now {
		o, ok := was[k]
		if !ok || !o.At.Equal(r.At) || o.Open != r.Open || o.Live != r.Live ||
			o.Presence.State != r.Presence.State || o.Presence.Question.ID != r.Presence.Question.ID ||
			o.Presence.Question.Kind != r.Presence.Question.Kind {
			return false
		}
	}
	return true
}

// wallTeams is the teams the wall, its popovers and the strip's switcher list:
// every open team but the `All teams` root, which is the switcher's own `All`
// row and not a team a conversation is put in. A closed team is on the teams
// page's `Closed` fold and nowhere else. It is the loaded list itself, with
// nothing allocated, while no team is closed and there is no root.
func (a *app) wallTeams() []team {
	hidden := 0
	for _, t := range a.wall.teams {
		if t.Root || t.Closed() {
			hidden++
		}
	}
	if hidden == 0 {
		return a.wall.teams
	}
	out := make([]team, 0, len(a.wall.teams)-hidden)
	for _, t := range a.wall.teams {
		if !t.Root && !t.Closed() {
			out = append(out, t)
		}
	}
	return out
}
