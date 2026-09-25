package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── A TEAM'S MEMBERS: ACTIVE ONES ON THE HEADER, EVERYONE ON A CARD ─────────
//
// The pane's header is one line (owner feedback 2026-09-24, which replaced two
// wrapped rows of every member as prose):
//
//	● harbor  ◆ Manager   @news ⠿ working   @review ? asking   +4 idle     $0.42 today   Settings  Close…  Open ▦
//
// ONLY A MEMBER WITH SOMETHING HAPPENING IS A CHIP: working, asking (in the
// needs-you amber) or failed, each a door to its conversation with its title in
// the hint. Everyone else is one quiet word, `+4 idle`, or `6 members` when
// nobody is doing anything: the emptiness law, which says an idle thing draws
// nothing, applied to people. Whether a conversation is open in this window is
// a fact about the window, not the team, so the page never says `not open`;
// the members card's `Open` or `Resume` carries it.
//
// NARROW, THE LINE GIVES UP ITS PARTS IN ORDER OF WHAT IT CAN LIVE WITHOUT: the
// idle word first (the card is still `p`), then the spend, then the chips from
// the last, then the manager's chip, then the action buttons from the right.
// The team's name is never dropped.
//
// THE MEMBERS CARD (`+4 idle`, `6 members`, or `p`) is every member, one row
// each: handle, title, state, when it last moved, `also in test` for a
// conversation that reports to another team's manager, and `Open` or `Resume`.
// Its rows are also where a member is dragged from to ADD it to another team
// on the rail (teamdrag.go), so the card hangs over the pane and never over the
// rail it drops onto.

// teamCrewLetter is the page's letter for the members card.
const teamCrewLetter = "p"

// teamsCrewRow is one member as the header and the card draw it.
type teamsCrewRow struct {
	key, handle, title string
	// word is the state in one word: working, asking, failed or idle.
	word    string
	asking  bool
	manager bool
	held    bool
	at      time.Time
	// also is the team whose manager a shared member reports to, "" for this
	// team's own.
	also string
	file string
}

// active reports whether the member has something happening, which is what
// earns it a chip on the header.
func (r teamsCrewRow) active() bool { return r.word != "idle" }

// name is the member as the page spells it: `@handle`, else its title.
func (r teamsCrewRow) name() string {
	if r.handle != "" {
		return "@" + r.handle
	}
	if r.title != "" {
		return r.title
	}
	return "member"
}

// teamsCrewMembers is who the header and the card list for team t: its members,
// or, on the root with a manager, the managers of the top-level teams, which
// are the global manager's members (DESIGN.md 8.10), never their chats.
func (a *app) teamsCrewMembers(t team) []teamMember {
	if t.Root && t.Manager != "" {
		tops := a.teamTree().TopManagers()
		out := make([]teamMember, 0, len(tops)+1)
		if m, ok := t.Member(t.Manager); ok {
			out = append(out, m)
		}
		return append(out, tops...)
	}
	return t.Members
}

// teamsCrew is every member of team t, the manager first. Memory only.
func (a *app) teamsCrew(t team) []teamsCrewRow {
	tree := a.teamTree()
	members := a.teamsCrewMembers(t)
	out := make([]teamsCrewRow, 0, len(members))
	for pass := 0; pass < 2; pass++ {
		for _, m := range members {
			if (m.Key == t.Manager) != (pass == 0) {
				continue
			}
			st := a.teamsMember(m)
			r := teamsCrewRow{key: m.Key, handle: m.Handle, title: strings.TrimSpace(m.Word), word: "idle",
				manager: m.Key == t.Manager, held: a.trafficHeld(m.Key), at: st.at, file: m.File}
			switch {
			case st.asking:
				r.word, r.asking = "asking", true
			case st.word == "running":
				r.word = "working"
			case a.teamsFailed(t, m):
				r.word = "failed"
			}
			if home, ok := tree.Home(m.Key); ok && home.Team != t.ID && home.Via != t.ID && !r.manager {
				r.also = a.teamNameOf(home.Team)
			}
			if r.title == "" && r.handle == "" {
				continue
			}
			out = append(out, r)
		}
	}
	return out
}

// teamsFailed reports whether member m's newest event in team t's Traffic says
// its last turn failed. It reads the Traffic this window already holds and
// allocates nothing.
func (a *app) teamsFailed(t team, m teamMember) bool {
	rows := a.traffic.rows[t.ID]
	for i := len(rows) - 1; i >= 0; i-- {
		e := rows[i]
		if e.Kind != teamstore.KindEvent || (e.Member != m.Key && (m.Handle == "" || e.From != m.Handle)) {
			continue
		}
		return e.State == teamstore.StateFailed
	}
	return false
}

// teamsCrewMark is a state's mark: `⠿` working, `?` asking, `✗` failed.
func (a *app) teamsCrewMark(word string) string {
	switch word {
	case "working":
		return a.linearMark("⠿", "*")
	case "asking":
		return "?"
	case "failed":
		return a.linearMark(a.icon(tokens.GFailed), "x")
	}
	return ""
}

// teamsCrewInk is the ink a state is drawn in: asking in the needs-you amber,
// failed in the failure red, working muted, idle dim.
func (a *app) teamsCrewInk(word string) func(string) string {
	switch word {
	case "asking":
		return a.pal.ask
	case "failed":
		return a.pal.bad
	case "working":
		return a.pal.muted
	}
	return a.pal.dim
}

// teamsCrewHint is what the hint line says over a member: who it is, what it
// is doing, and what a press does.
//
//	@news · weekly news digest · working 3m · click opens
func (a *app) teamsCrewHint(r teamsCrewRow) string {
	words := r.name()
	if r.title != "" && r.handle != "" {
		words += hintSegment + r.title
	}
	state := r.word
	if age := sinceAt(r.at, a.now()); age != "" && r.word != "working" {
		state += " " + age
	}
	words += hintSegment + state
	if r.also != "" {
		words += hintSegment + "reports to " + r.also + "'s manager"
	}
	if r.held {
		return words + hintSegment + "click opens"
	}
	return words + hintSegment + "click resumes it behind, in its own tab"
}

// ── THE HEADER ──────────────────────────────────────────────────────────────

// teamsHeadPiece is one optional piece of the header line, in the order the
// line drops them when it is narrow.
type teamsHeadPiece struct {
	s    string
	w    int
	t    teamsTarget
	btn  bool
	drop int
}

// teamsHeader is the header row: the team's name, its manager, the members
// with something happening, the idle word, the spend, and the three buttons.
//
//	● harbor  ◆ Manager   @news ⠿ working   +4 idle     $0.42 today   Settings  Close…  Open ▦
func (a *app) teamsHeader(d *teamsDraw, t team, width, y int) string {
	pal := a.pal
	name := t.Name
	if t.Root {
		name = teamstore.RootName
	}
	left := " " + a.tabTeamDot(t) + " " + pal.bold(pal.ink(name))
	type btn struct {
		word string
		t    teamsTarget
	}
	var bs []btn
	if t.Closed() {
		bs = append(bs,
			btn{"Reopen", teamsTarget{act: teamsActReopen, id: t.ID, hint: "Reopen " + t.Name + ": its tabs come back and its manager resumes" + hintSegment + "r"}},
			btn{"Delete" + a.linearMark("…", "..."), teamsTarget{act: teamsActDelete, id: t.ID, hint: "Forget this team, its Traffic and its packets; the conversations stay" + hintSegment + "d"}})
		if p, ok := a.teamsParentClosed(t); ok {
			bs = append([]btn{{"Reopen " + p.Name + " too", teamsTarget{act: teamsActReopenParent, id: t.ID,
				hint: t.Name + " sits under " + p.Name + ", which is closed" + hintSegment + "r"}}}, bs[1:]...)
		}
	} else {
		bs = append(bs,
			btn{"Settings", teamsTarget{act: teamsActSettings, id: t.ID, hint: "What this team overrides, and what it inherits" + hintSegment + "s"}})
		if !t.Root {
			bs = append(bs, btn{"Close" + a.linearMark("…", "..."), teamsTarget{act: teamsActClose, id: t.ID, hint: "Close " + t.Name + ": wrap up first, or now" + hintSegment + "c"}})
		}
		bs = append(bs, btn{"Open " + a.linearMark("▦", "#"), teamsTarget{act: teamsActWall, id: t.ID, hint: "The wall, showing " + name + "'s open conversations" + hintSegment + "w"}})
	}
	// THE OPTIONAL PIECES, each with the rank at which a narrow line drops it:
	// the higher the rank, the sooner it goes.
	var pieces []teamsHeadPiece
	const (
		dropIdle  = 1000
		dropSpend = 900
		dropChip  = 800 // minus the chip's place, so the last chip goes first
		dropBoss  = 100
	)
	if !t.Closed() {
		crew := a.teamsCrew(t)
		if t.Manager != "" {
			word := a.teamManagerMark() + " Manager"
			hint := "Talk to " + name + "'s manager"
			for _, r := range crew {
				if r.manager && r.active() {
					word += " " + a.teamsCrewMark(r.word)
					hint += hintSegment + r.word
				}
			}
			if a.teamsHosting() {
				hint += hintSegment + "it is the conversation below"
			} else {
				hint += hintSegment + "click brings it in front"
			}
			pieces = append(pieces, teamsHeadPiece{s: word, w: ansi.StringWidth(word),
				t: teamsTarget{act: teamsActManagerGo, id: t.ID, hint: hint}, btn: true, drop: dropBoss})
		}
		idle, total := 0, 0
		for _, r := range crew {
			if r.manager {
				continue
			}
			total++
			if !r.active() {
				idle++
				continue
			}
			mark := a.teamsCrewMark(r.word)
			plain := r.name() + " " + mark + " " + r.word
			ink := a.teamsCrewInk(r.word)
			s := pal.ink(r.name()) + " " + ink(mark+" "+r.word)
			pieces = append(pieces, teamsHeadPiece{s: s, w: ansi.StringWidth(plain),
				t: teamsTarget{act: teamsActMember, id: t.ID, arg: r.key, hint: a.teamsCrewHint(r)}, drop: dropChip - len(pieces)})
		}
		if total > 0 {
			word := "+" + itoa(idle) + " idle"
			if idle == total {
				word = itoa(total) + " members"
				if total == 1 {
					word = "1 member"
				}
			}
			if idle > 0 {
				pieces = append(pieces, teamsHeadPiece{s: word, w: ansi.StringWidth(word),
					t: teamsTarget{act: teamsActCrew, id: t.ID, hint: "Every member of " + name + ": open one, resume one, or drag one onto another team to add it" + hintSegment + teamCrewLetter}, btn: true, drop: dropIdle})
			}
		}
	}
	spend := a.teamsSpendWords(t)
	if t.Closed() {
		spend = a.teamsClosedWords(t)
	}
	if spend != "" {
		pieces = append(pieces, teamsHeadPiece{s: pal.dim(spend), w: ansi.StringWidth(spend), drop: dropSpend})
	}
	// WHAT FITS. The name and the buttons are the floor; the pieces are added
	// back from the one a narrow line keeps longest.
	bw := 0
	for _, b := range bs {
		bw += ansi.StringWidth(b.word) + 2
	}
	leftW := ansi.StringWidth(left)
	room := width - 1 - leftW - bw
	for room < 0 && len(bs) > 0 {
		// Too narrow for the buttons: they go from the right, the name never.
		last := bs[len(bs)-1]
		bs = bs[:len(bs)-1]
		bw -= ansi.StringWidth(last.word) + 2
		room = width - 1 - leftW - bw
	}
	keep := make([]bool, len(pieces))
	used := 0
	for {
		best := -1
		for i, p := range pieces {
			if !keep[i] && (best < 0 || p.drop < pieces[best].drop) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		w := pieces[best].w + 3
		if pieces[best].btn {
			w++
		}
		if used+w > room {
			break
		}
		keep[best] = true
		used += w
	}
	head := left
	x := leftW
	for i, p := range pieces {
		if !keep[i] {
			continue
		}
		if p.btn {
			// A button's own cell of air is the third cell of the gap.
			head += "  "
			x += 2
			tg := p.t
			tg.x0, tg.y = x, y
			s, w := d.button(p.s, tg, pal.muted)
			head += s
			x += w
			continue
		}
		head += "   "
		x += 3
		if p.t.act != teamsActNone {
			tg := p.t
			tg.x0, tg.x1, tg.y = x, x+p.w, y
			d.targets = append(d.targets, tg)
			s := p.s
			hot, cur := d.lit(tg.ref())
			switch {
			case cur:
				s = pal.selected(s, 0)
			case hot:
				s = pal.cursor(s, 0)
			}
			head += s
		} else {
			head += p.s
		}
		x += p.w
	}
	head = teamsPad(head, max(width-bw, 0))
	x = max(width-bw, 0)
	for _, b := range bs {
		b.t.x0, b.t.y = x, y
		s, w := d.button(b.word, b.t, pal.ink)
		head += s
		x += w
	}
	return head
}

// teamsManagerGo is the header's `◆ Manager`: the manager's conversation in
// front, which on this page is the pane.
func (a *app) teamsManagerGo(id string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Manager == "" {
		return nil
	}
	if t.Manager == a.frontTabKey() {
		// It is in front already: the keyboard goes to its box.
		a.tp.focus = false
		a.tp.top = teamsTopCache{}
		a.touch()
		return nil
	}
	a.tp.sel = id
	return a.teamsBringManager()
}

// ── THE MEMBERS CARD ────────────────────────────────────────────────────────

// teamCrew is the members card's state.
type teamCrew struct {
	on   bool
	team string
	// cursor is the row the keyboard is on; hot the row under the pointer and
	// hotButton whether it is on that row's Open or Resume (-1 none).
	cursor, hot int
	hotButton   bool
	hotClose    bool
	top         int
	rect        wallRect
	hits        []wallHit
}

// The card's targets, as its hits carry them in arg.
const (
	crewHitRow = iota
	crewHitButton
	crewHitClose
	crewHitTag
)

// teamCrewOpen puts the card up for team id.
func (a *app) teamCrewOpen(id string) tea.Cmd {
	if _, ok := a.teamByID(id); !ok {
		return nil
	}
	a.tcrew = teamCrew{on: true, team: id, hot: -1}
	a.touch()
	return nil
}

// teamCrewShut puts the card away.
func (a *app) teamCrewShut() {
	a.tcrew = teamCrew{}
	a.touch()
}

// teamCrewRows is the card's members, the manager first.
func (a *app) teamCrewRows() []teamsCrewRow {
	t, ok := a.teamByID(a.tcrew.team)
	if !ok {
		return nil
	}
	return a.teamsCrew(t)
}

// teamCrewKey is a key while the card is up: it has the keyboard.
func (a *app) teamCrewKey(msg tea.KeyPressMsg) tea.Cmd {
	rows := a.teamCrewRows()
	c := &a.tcrew
	a.touch()
	switch msg.String() {
	case "esc", teamCrewLetter:
		a.teamCrewShut()
	case "up", "k":
		c.cursor = max(c.cursor-1, 0)
	case "down", "j":
		c.cursor = min(c.cursor+1, max(len(rows)-1, 0))
	case "enter", "space":
		if c.cursor >= 0 && c.cursor < len(rows) {
			return a.teamCrewGo(rows[c.cursor])
		}
	}
	return nil
}

// teamCrewGo is `Open` or `Resume` on a member: one this window holds is
// opened and the card goes; one it does not is resumed behind and the card
// stays, so the person sees it change to `Open`.
func (a *app) teamCrewGo(r teamsCrewRow) tea.Cmd {
	id := a.tcrew.team
	if r.held {
		a.teamCrewShut()
	}
	return a.teamsMemberGo(id, r.key)
}

// teamCrewHitAt is the card's target under the pointer.
func (a *app) teamCrewHitAt(x, y int) (wallHit, bool) {
	for _, hit := range a.tcrew.hits {
		if x >= hit.x0 && x < hit.x1 && y >= hit.y0 && y < hit.y1 {
			return hit, true
		}
	}
	return wallHit{}, false
}

// teamCrewMouse is a pointer event while the card is up. A press on a row puts
// the cursor there and holds it for a drag; a press on `Open` or `Resume` does
// it; `Close` and a press off the card put it away. It reports whether the
// card took the event.
func (a *app) teamCrewMouse(msg tea.Msg, m tea.Mouse) (tea.Cmd, bool) {
	c := &a.tcrew
	hit, on := a.teamCrewHitAt(m.X, m.Y)
	switch msg.(type) {
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil, true
		}
		if !on {
			if !c.rect.holds(m.X, m.Y) {
				a.teamCrewShut()
			}
			return nil, true
		}
		rows := a.teamCrewRows()
		switch hit.kind {
		case crewHitClose:
			a.teamCrewShut()
			return nil, true
		case crewHitButton:
			if hit.arg >= 0 && hit.arg < len(rows) {
				c.cursor = hit.arg
				return a.teamCrewGo(rows[hit.arg]), true
			}
		default:
			if hit.arg >= 0 && hit.arg < len(rows) {
				c.cursor = hit.arg
				a.teamDragPress(m.X, m.Y, true, c.team, rows[hit.arg].key, teamsTarget{}, false)
				a.touch()
			}
		}
		return nil, true
	case tea.MouseMotionMsg:
		if a.tdrag.press {
			a.teamDragMotion(m.X, m.Y, m.Button == tea.MouseLeft)
			return nil, true
		}
		hot, button, closing := -1, false, false
		if on {
			switch hit.kind {
			case crewHitClose:
				closing = true
			default:
				hot, button = hit.arg, hit.kind == crewHitButton
			}
		}
		if hot != c.hot || button != c.hotButton || closing != c.hotClose {
			c.hot, c.hotButton, c.hotClose = hot, button, closing
			a.touch()
		}
		return nil, c.rect.holds(m.X, m.Y)
	case tea.MouseReleaseMsg:
		if cmd, took := a.teamDragRelease(); took {
			return cmd, true
		}
		return nil, c.rect.holds(m.X, m.Y)
	}
	return nil, c.rect.holds(m.X, m.Y)
}

// teamCrewHint is what the hint line says with the card up.
func (a *app) teamCrewHint() string {
	c := a.tcrew
	rows := a.teamCrewRows()
	if c.hotClose {
		return "Close the members" + hintSegment + "esc"
	}
	at := c.cursor
	if c.hot >= 0 {
		at = c.hot
	}
	if at < 0 || at >= len(rows) {
		return "↑↓ walk" + hintSegment + "enter open" + hintSegment + "esc close"
	}
	r := rows[at]
	if c.hot >= 0 && c.hotButton {
		if r.held {
			return "Open " + r.name() + hintSegment + "enter"
		}
		return "Resume " + r.name() + " behind, in its own tab; you stay here" + hintSegment + "enter"
	}
	words := r.name()
	if r.title != "" && r.handle != "" {
		words += hintSegment + r.title
	}
	if r.also != "" {
		words += hintSegment + "reports to " + r.also + "'s manager"
	}
	return words + hintSegment + "drag onto a team on the left to add it there" + hintSegment + "enter opens"
}

// teamCrewOver lays the card over a finished frame, over the pane and clear of
// the rail, under the header.
func (a *app) teamCrewOver(frame string) string {
	if !a.tcrew.on || !a.at(pageTeams) {
		return frame
	}
	width, height := a.width, a.height
	x := 1
	if a.tp.railW > 0 {
		x = a.tp.railW + 1
	}
	y := placeHeadRows
	for _, t := range a.tp.targets {
		if t.act == teamsActSettings || t.act == teamsActCrew {
			y = t.y + 1
			break
		}
	}
	card := a.teamCrewCard(x, y, width-x-1, height-y)
	a.tcrew.hits = card.hits
	if len(card.rows) == 0 {
		a.tcrew.rect = wallRect{}
		return frame
	}
	a.tcrew.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	rows := strings.Split(frame, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	for dy, cr := range card.rows {
		if yy := card.y + dy; yy >= 0 && yy < len(rows) {
			rows[yy] = wallSplice(rows[yy], cr, card.x, width)
		}
	}
	return strings.Join(rows[:height], "\n")
}

// teamCrewCard is the card at x, y, at most w wide and h tall.
//
//	╭─ harbor · 6 members ──────────────────────────────────────────────╮
//	│  ◆ @boss    harbor's manager      working             Open        │
//	│  @review    code review           asking    also in test  Resume  │
//	│                                                         Close esc │
//	╰───────────────────────────────────────────────────────────────────╯
func (a *app) teamCrewCard(x, y, w, h int) wallCard {
	pal := a.pal
	t, ok := a.teamByID(a.tcrew.team)
	if !ok {
		return wallCard{}
	}
	rows := a.teamCrewRows()
	w = min(w, 88)
	if w < 36 || h < 6 {
		return wallCard{}
	}
	const padX = 1
	inner := w - 2 - 2*padX
	room := max(h-4, 1)
	c := &a.tcrew
	c.cursor = min(max(c.cursor, 0), max(len(rows)-1, 0))
	top := min(c.top, max(len(rows)-room, 0))
	if c.cursor < top {
		top = c.cursor
	}
	if c.cursor >= top+room {
		top = c.cursor - room + 1
	}
	c.top = top
	// The columns: handle, state and age, the tag and the button are sized;
	// the title takes what is left.
	const nameW, stateW, ageW, buttonW = 13, 10, 5, 8
	tagW := 0
	for _, r := range rows {
		if r.also != "" {
			tagW = max(tagW, ansi.StringWidth("also in "+r.also)+2)
		}
	}
	tagW = min(tagW, 20)
	titleW := inner - nameW - stateW - ageW - tagW - buttonW
	if titleW < 8 {
		titleW, tagW = max(inner-nameW-stateW-ageW-buttonW, 0), 0
	}
	var lines []wallCardLine
	if len(rows) == 0 {
		lines = append(lines, wallCardLine{s: pal.dim("no members yet")})
	}
	now := a.now()
	for i, r := range rows[top:min(top+room, len(rows))] {
		at := top + i
		name := r.name()
		if r.manager {
			name = a.teamManagerMark() + " " + name
		}
		title := r.title
		if r.manager && title == "" {
			title = t.Name + "'s manager"
		}
		age := ""
		if r.word != "working" {
			age = sinceAt(r.at, now)
		}
		ink := a.teamsCrewInk(r.word)
		text := pal.ink(teamsPad(fitConversationTitle(name, nameW-1), nameW)) +
			pal.muted(teamsPad(fitConversationTitle(title, max(titleW-2, 1)), titleW)) +
			ink(teamsPad(r.word, stateW)) + pal.dim(teamsPad(age, ageW))
		hits := []wallHit{{x0: 0, x1: inner - buttonW, y1: 1, kind: crewHitRow, arg: at}}
		if tagW > 0 {
			tag := ""
			if r.also != "" {
				tag = "also in " + r.also
				hits = append(hits, wallHit{x0: inner - buttonW - tagW, x1: inner - buttonW - 2, y1: 1, kind: crewHitTag, arg: at})
			}
			text += pal.dim(teamsPad(fit(tag, tagW-2), tagW))
		}
		word := "Resume"
		if r.held {
			word = "Open"
		}
		chip := " " + word + " "
		lit := c.hot == at && c.hotButton
		switch {
		case lit:
			chip = pal.cursor(pal.ink(chip), 0)
		default:
			chip = pal.ink(chip)
		}
		text = teamsPad(text, inner-buttonW) + chip
		hits = append(hits, wallHit{x0: inner - buttonW, x1: inner - buttonW + ansi.StringWidth(" "+word+" "), y1: 1, kind: crewHitButton, arg: at})
		if at == c.cursor || (c.hot == at && !c.hotButton) {
			text = pal.cursor(teamsPad(text, inner), inner)
		}
		// A dragged row is dim until it drops, so the person sees what moves.
		if a.tdrag.on && a.tdrag.member && a.tdrag.key == r.key {
			text = pal.dim(ansi.Strip(teamsPad(text, inner)))
		}
		lines = append(lines, wallCardLine{s: text, hits: hits})
	}
	closeWord := " Close " + pal.dim("esc") + " "
	cw := ansi.StringWidth(" Close esc ")
	cs := closeWord
	if c.hotClose {
		cs = pal.cursor(" Close esc ", 0)
	}
	lines = append(lines, wallCardLine{s: strings.Repeat(" ", max(inner-cw, 0)) + cs,
		hits: []wallHit{{x0: inner - cw, x1: inner, y1: 1, kind: crewHitClose, arg: -1}}})
	// THE COUNT IS THE HEADER'S COUNT: the members beside the manager, with
	// the manager named apart, so the header's `◆ Manager  1 member` and this
	// title never disagree about the same team.
	members, managed := 0, false
	for _, r := range rows {
		if r.manager {
			managed = true
			continue
		}
		members++
	}
	count := itoa(members) + " members"
	if members == 1 {
		count = "1 member"
	}
	if managed {
		count = a.teamManagerMark() + " Manager " + a.teamsDot() + " " + count
	}
	title := t.Name + " " + a.teamsDot() + " " + count
	if t.Root {
		title = teamstore.RootName + " " + a.teamsDot() + " the top teams' managers"
	}
	return wallCardBuild(pal, title, lines, x, y, w, padX, 0)
}
