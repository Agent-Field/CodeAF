package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── DRAWING THE TEAMS PAGE (teamspage.go says what it is) ──────────────────
//
// Every function here reads memory and nothing else, and records where each
// pressable thing landed ([teamsTarget]) so the pointer and the keyboard act on
// what was drawn. A target is lit by name ([teamsRef]): the pointer's ground
// when it is under the pointer, the cursor's when the keyboard is on it.

// teamsRailCols is the rail's columns at width, its separator column
// included, and 0 under [teamsRailFloor].
func teamsRailCols(width int) int {
	if width < teamsRailFloor {
		return 0
	}
	return min(max(width/5, teamsRailMin), teamsRailMax)
}

// teamsDraw collects the targets of one frame as it is drawn.
type teamsDraw struct {
	a       *app
	targets []teamsTarget
}

// lit reports whether the target named r wears a ground, and which.
func (d *teamsDraw) lit(r teamsRef) (hot, cur bool) {
	tp := &d.a.tp
	cur = tp.focus && tp.cur == r
	hot = tp.hot == r
	return hot, cur
}

// button paints one word button: the word with a cell of air either side, on
// the pointer's ground or the cursor's, and records it at x0 on row y.
func (d *teamsDraw) button(word string, t teamsTarget, ink func(string) string) (string, int) {
	pal := d.a.pal
	chip := " " + word + " "
	w := ansi.StringWidth(chip)
	t.x1 = t.x0 + w
	d.targets = append(d.targets, t)
	hot, cur := d.lit(t.ref())
	switch {
	case cur:
		return pal.selected(pal.bold(pal.ink(chip)), w), w
	case hot:
		return pal.cursor(pal.ink(chip), w), w
	}
	return ink(chip), w
}

// row paints a whole-row target of width cells: its text, grounded when lit.
func (d *teamsDraw) row(text string, width int, t teamsTarget, selected bool) string {
	pal := d.a.pal
	t.x1 = t.x0 + width
	d.targets = append(d.targets, t)
	text = fit(text, width)
	text += strings.Repeat(" ", max(width-ansi.StringWidth(text), 0))
	hot, cur := d.lit(t.ref())
	switch {
	case cur:
		return pal.cursor(text, width)
	case selected:
		return pal.selected(text, width)
	case hot:
		return pal.cursor(text, width)
	}
	return text
}

// shift moves the targets recorded from index from by dx and dy.
func (d *teamsDraw) shift(from, dx, dy int) {
	for i := from; i < len(d.targets); i++ {
		d.targets[i].x0 += dx
		d.targets[i].x1 += dx
		d.targets[i].y += dy
	}
}

// pad is s padded or cut to exactly width cells.
func teamsPad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = fit(s, width)
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

// ── THE RAIL ────────────────────────────────────────────────────────────────

// teamsRail is the rail, height rows of width cells (the separator not
// included), with its targets on rows counted from its first.
func (a *app) teamsRail(d *teamsDraw, width, height int) []string {
	pal := a.pal
	rows := a.teamsRailRows()
	out := make([]string, 0, height)
	topSaid := false
	paint := func(r teamsRailRow, y int) string {
		switch r.kind {
		case railRowAll:
			return a.teamsRailAll(d, width, y)
		case railRowTeam:
			t, _ := a.teamByID(r.id)
			return a.teamsRailTeam(d, t, r.depth, width, y)
		case railRowNew:
			return a.teamsRailNew(d, width, y, false)
		case railRowNewIn:
			return a.teamsRailNew(d, width, y, true)
		case railRowBlank:
			// DURING A TEAM DRAG THE FIRST BLANK UNDER THE TREE SAYS WHAT IT IS:
			// the top level, where a drop on the empty rail puts the team.
			if a.tdrag.on && !a.tdrag.member && !topSaid {
				topSaid = true
				word := "   " + a.linearMark("↳", "->") + " Top level"
				if a.teamDropLit(teamMoveTop) {
					return a.pal.cursor(teamsPad(a.pal.ink(word), width), width)
				}
				return teamsPad(a.pal.dim(word), width)
			}
		case railRowOrganize:
			return d.row(" "+a.teamsSpark()+" Organize", width, teamsTarget{act: teamsActOrganize, y: y,
				hint: "Suggest teams for your conversations, and close quiet ones" + hintSegment + "o"}, false)
		case railRowClosed:
			fold := a.icon(tokens.GCollapsed)
			if a.tp.closedOpen {
				fold = a.icon(tokens.GExpanded)
			}
			word := " " + fold + " Closed teams " + a.teamsDot() + " " + itoa(len(a.teamsClosed()))
			hint := "Show the closed teams"
			if a.tp.closedOpen {
				hint = "Fold the closed teams away"
			}
			return d.row(word, width, teamsTarget{act: teamsActClosedFold, y: y, hint: hint + hintSegment + "enter"}, false)
		case railRowClosedTeam:
			t, _ := a.teamByID(r.id)
			return d.row("   "+pal.dim(t.Name), width, teamsTarget{act: teamsActSelect, id: t.ID, y: y,
				hint: t.Name + " closed" + hintSegment + "read-only history and permanent Delete"}, a.tp.sel == t.ID)
		}
		return strings.Repeat(" ", width)
	}
	for _, r := range rows {
		out = append(out, paint(r, len(out)))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return out[:min(len(out), height)]
}

// The rail keeps every keyboard stop while showing a bounded window around
// the cursor. Expanding a long Closed category must not hide its later teams.
func (a *app) teamsRailWindow(d *teamsDraw, width, height int) []string {
	mark := len(d.targets)
	kinds := a.teamsRailRows()
	all := a.teamsRail(d, width, len(kinds))
	if height <= 0 {
		return nil
	}
	var head, middle, foot []int
	for i, row := range kinds {
		switch row.kind {
		case railRowAll:
			head = append(head, i)
		case railRowNew, railRowClosed, railRowOrganize:
			foot = append(foot, i)
		case railRowBlank:
		default:
			middle = append(middle, i)
		}
	}
	// Creation takes the only available row on a very short terminal.
	if height == 1 {
		head = []int{foot[0]}
		foot = nil
	}
	for len(head)+len(foot) > height {
		foot = foot[:len(foot)-1]
	}
	room := max(height-len(head)-len(foot), 0)
	off := min(a.tp.railOffset, max(len(middle)-room, 0))
	for _, target := range d.targets[mark:] {
		if target.ref() != a.tp.cur {
			continue
		}
		for i, line := range middle {
			if line != target.y {
				continue
			}
			if i < off {
				off = i
			}
			if room > 0 && i >= off+room {
				off = i - room + 1
			}
		}
	}
	a.tp.railOffset = off
	positions := map[int]int{}
	out := make([]string, height)
	for i := range out {
		out[i] = strings.Repeat(" ", width)
	}
	paint := func(line, y int) { positions[line] = y; out[y] = all[line] }
	for y, line := range head {
		paint(line, y)
	}
	for i := off; i < min(off+room, len(middle)); i++ {
		paint(middle[i], len(head)+i-off)
	}
	for i, line := range foot {
		y := height - len(foot) + i
		if kinds[line].kind == railRowNew {
			y = min(y, len(head)+min(room, len(middle)-off))
		}
		paint(line, y)
	}
	for i := mark; i < len(d.targets); i++ {
		t := &d.targets[i]
		t.line = t.y
		y, visible := positions[t.y]
		t.hidden = !visible
		if visible {
			t.y = y
		} else {
			t.y -= off
		}
	}
	return out
}

// teamsDot is the middle dot in this terminal's glyphs.
func (a *app) teamsDot() string { return a.linearMark("·", "-") }

// teamsSpark is Organize's mark.
func (a *app) teamsSpark() string {
	spark, _ := wallOrgMarks(a.pal)
	return spark
}

// teamsRailAll is the `All teams` row: the root team when there is one, and
// otherwise a row over the top level carrying `+ Manager`, the optional
// global manager.
func (a *app) teamsRailAll(d *teamsDraw, width, y int) string {
	pal := a.pal
	if root, ok := a.teamsRoot(); ok {
		word := " " + pal.bold(pal.ink(teamstore.RootName))
		if root.Manager != "" {
			word += " " + pal.dim(a.teamManagerMark())
		}
		if n := a.teamsNeeds(root); n > 0 {
			word += " " + pal.ask("? "+itoa(n))
		}
		return d.row(word, width, teamsTarget{act: teamsActSelect, id: root.ID, y: y,
			hint: "Every team, and what waits on you from any of them" + hintSegment + "enter"}, a.tp.sel == root.ID)
	}
	word := teamManagerSlotWord
	bw := ansi.StringWidth(word) + 2
	left := width - bw
	selected := a.tp.sel == teamsAllRow
	name := d.row(" "+pal.bold(pal.ink(teamstore.RootName)), left, teamsTarget{act: teamsActSelect, id: teamsAllRow, y: y,
		hint: "Every team, and what waits on you from any of them" + hintSegment + "enter"}, selected)
	if left < 12 {
		return teamsPad(name, width)
	}
	btn, _ := d.button(word, teamsTarget{act: teamsActRootManager, x0: left, y: y,
		hint: "Start a manager over every team: you talk to it, it talks to theirs" + hintSegment + "M"}, pal.muted)
	return name + btn
}

// teamsRailTeam is one team's row: indent, colour dot, name, and at most one
// mark at the right. A team that is doing nothing draws no mark.
func (a *app) teamsRailTeam(d *teamsDraw, t team, depth, width, y int) string {
	pal := a.pal
	mark, markW := "", 0
	if n := a.teamsNeeds(t); n > 0 {
		word := "? " + itoa(n)
		mark, markW = pal.ask(word), ansi.StringWidth(word)
	} else if a.teamsWorking(t) {
		word := a.linearMark("⠿", "*")
		mark, markW = pal.dim(word), ansi.StringWidth(word)
	}
	name := t.Name
	if t.Root {
		name = teamstore.RootName
	}
	lead := " " + strings.Repeat("  ", depth)
	room := width - ansi.StringWidth(lead) - 2 - markW - 2
	if room < 3 {
		room = 3
	}
	if ansi.StringWidth(name) > room {
		name = ansi.Truncate(name, room, a.linearMark("…", "~"))
	}
	// A TEAM PICKED WITH space wears the wall's own picked mark in place of
	// its dot, so the rail says which teams one `Move into…` will move.
	dot := a.tabTeamDot(t)
	if a.tp.picked[t.ID] {
		dot = pal.accent(wallGlyphsFor(pal.ascii).marked)
	}
	text := lead + dot + " " + pal.ink(name)
	if t.Manager != "" {
		text += " " + pal.dim(a.teamManagerMark())
	}
	if mark != "" {
		gap := width - ansi.StringWidth(text) - markW - 1
		text += strings.Repeat(" ", max(gap, 1)) + mark
	}
	hint := t.Name
	if t.Manager != "" {
		hint += hintSegment + "view members and recent interactions"
	} else {
		hint += hintSegment + "no manager yet"
	}
	if !t.Root {
		hint += hintSegment + "m move into…" + hintSegment + "space pick" + hintSegment + "drag to move"
	}
	tg := teamsTarget{act: teamsActSelect, id: t.ID, y: y, hint: hint + hintSegment + "enter"}
	// DURING A DRAG only a row that takes the drop is grounded, and the row
	// being dragged is dim, so the person sees what moves and where it can go.
	if dr := a.tdrag; dr.on {
		tg.x1 = tg.x0 + width
		d.targets = append(d.targets, tg)
		switch {
		case a.teamDropLit(t.ID):
			return pal.cursor(teamsPad(text, width), width)
		case !dr.member && (dr.id == t.ID || a.tp.picked[dr.id] && a.tp.picked[t.ID]):
			return teamsPad(pal.dim(ansi.Strip(text)), width)
		}
		return teamsPad(text, width)
	}
	return d.row(text, width, tg, a.tp.sel == t.ID)
}

// teamsRailNew always creates a top-level team; subteams have their own header door.
func (a *app) teamsRailNew(d *teamsDraw, width, y int, second bool) string {
	return d.row(" + "+teamsNewTeamWord, width, teamsTarget{act: teamsActNewTeam, y: y,
		hint: "Create a top-level team" + hintSegment + "n"}, false)
}

func (a *app) teamsRailNewWords() (string, bool) { return " + " + teamsNewTeamWord, false }

// ── THE PANE'S HEAD: HEADER, MEMBERS, INBOX ─────────────────────────────────

// teamsTop is the pane's head for the selection, width cells wide: the header
// row, the members, and the inbox. Targets are recorded on rows counted from
// its first and columns from the pane's.
func (a *app) teamsTop(d *teamsDraw, width int) []string {
	pal := a.pal
	t, ok := a.teamsSelected()
	// A MOVE WAITING ON THE PERSON, OR A CLOSE OR A MOVE JUST MADE, is said
	// first, over whichever team is shown now (teammove.go, teamclose.go).
	out := a.teamsNoticeRows(d, width, 0)
	if len(out) > 0 {
		// Air between the notice and the team it is not about.
		out = append(out, "")
	}
	if !ok {
		if a.tp.sel == teamsAllRow {
			out = append(out, " "+pal.bold(pal.ink(teamstore.RootName)))
			out = append(out, a.teamsInboxRows(d, width, len(out))...)
		}
		return out
	}
	out = append(out, a.teamsOverviewHeader(d, t, width, len(out)))
	if !t.Closed() && !t.Root {
		for _, control := range []struct {
			word string
			act  teamsAct
		}{{"+ Add member", teamsActAddMember}, {"+ Add subteam", teamsActAddSubteam}} {
			visible := false
			for _, target := range d.targets {
				if target.act == control.act && target.id == t.ID {
					visible = true
				}
			}
			if !visible {
				button, _ := d.button(control.word, teamsTarget{act: control.act, id: t.ID, x0: 1, y: len(out), hint: control.word, pane: true}, pal.muted)
				out = append(out, " "+button)
			}
		}
	}

	if t.Closed() {
		return out
	}
	// THE MEMBERS ARE ON THE HEADER (teamcrew.go): the ones doing something
	// as chips, everyone else one word that opens the members card.
	out = append(out, a.teamsNoManagerRows(d, t, width, len(out))...)
	if !a.teamsCanDelegate() && !a.teamsOff() && a.hosted() {
		out = append(out, "", " "+pal.dim(fit(teamsHostedWord, width-2)))
	}
	out = append(out, a.teamsPromptRows(d, t, width, len(out))...)
	out = append(out, a.teamsInboxRows(d, width, len(out))...)
	return out
}

// teamsSpendWords is today's spend against the cap that applies: `$1.20
// today` with no cap, `$1.20 of $5 today` with the team's own, and the pool
// owner's figure named when the cap is inherited, because a cap is a pool and
// `$1.20 of $5` beside a sub-team would read as a second $5. "" with nothing
// read yet.
func (a *app) teamsSpendWords(t team) string {
	if !a.tp.defaultsOK {
		return ""
	}
	owner, e := a.teamsPool(t)
	s, ok := a.tp.spend[owner]
	if !ok {
		return ""
	}
	if e.CapUSDDay <= 0 {
		// Nothing spent and no cap is nothing to say: an idle team draws no
		// figure.
		if s.USD <= 0 {
			return ""
		}
		return teamsMoney(teamstore.RoundMoney(s.USD)) + " today"
	}
	cap := a.teamsRaisedCap(owner, e.CapUSDDay)
	words := teamsMoney(teamstore.RoundMoney(s.USD)) + " of " + teamsMoney(cap) + " today"
	if owner != t.ID {
		if o, ok := a.teamByID(owner); ok {
			name := o.Name
			if o.Root {
				name = teamstore.RootName
			}
			words += " " + a.teamsDot() + " " + possessiveTeamName(name) + " cap"
		}
	}
	return words
}

// teamsRaisedCap is today's ceiling after a person accepted a raise. The
// decision is deliberately not a team setting, so the header must read the
// same packet the cap gate uses rather than the stored recurring ceiling.
func (a *app) teamsRaisedCap(owner string, cap float64) float64 {
	day := teamstore.Today()
	for _, p := range a.tp.packets {
		if p.Kind != teamstore.PacketCap || p.State != teamstore.PacketDecided || p.Decision != teamstore.OptionRaiseCap || p.Cap == nil {
			continue
		}
		if p.Cap.Team == owner && p.Cap.Day == day && p.Cap.RaiseTo > cap {
			cap = p.Cap.RaiseTo
		}
	}
	return cap
}

func possessiveTeamName(name string) string {
	if strings.HasSuffix(name, "s") {
		return name + "'"
	}
	return name + "'s"
}

// teamsClosedWords is a closed team's dates: when it was made and closed.
func (a *app) teamsClosedWords(t team) string {
	words := "disbanded"
	if !t.ClosedAt.IsZero() {
		words += " " + t.ClosedAt.Local().Format("2 Jan")
	}
	if !t.Made.IsZero() {
		words = "opened " + t.Made.Local().Format("2 Jan") + " " + a.teamsDot() + " " + words
	}
	return words
}

// teamsParentClosed is the closed team above t that keeps it closed, false
// when reopening t alone would work.
func (a *app) teamsParentClosed(t team) (team, bool) {
	for _, up := range a.teamAncestors(t.ID) {
		if up.Closed() {
			return up, true
		}
	}
	return team{}, false
}

// teamsMembersRows is the members, every one whether this window has it open
// or not: its handle, what it is doing, and when it last moved, each a door. A
// press opens a member this window holds, and resumes one it does not behind
// the page, never moving the person's focus.
func (a *app) teamsMembersRows(d *teamsDraw, t team, width, y int) []string {
	pal := a.pal
	var pieces []string
	var targets []teamsTarget
	now := a.now()
	for _, m := range t.Members {
		if m.Key == t.Manager {
			continue
		}
		st := a.teamsMember(m)
		name := m.Word
		if m.Handle != "" {
			name = "@" + m.Handle
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		name = fitConversationTitle(name, 24)
		word := pal.ink(name) + " "
		switch {
		case st.asking:
			word += pal.ask(st.word)
		case st.word == "running" || strings.HasPrefix(st.word, "busy for "):
			word += pal.muted(st.word)
		default:
			word += pal.dim(st.word)
		}
		plain := name + " " + st.word
		if age := sinceAt(st.at, now); age != "" && st.word != "running" && !strings.HasPrefix(st.word, "busy for ") {
			word += " " + pal.dim(age)
			plain += " " + age
		}
		hint := name + " " + st.word
		if st.open && a.trafficHeld(m.Key) {
			hint += hintSegment + "click opens it"
		} else {
			hint += hintSegment + "click resumes it behind, in its own tab"
		}
		pieces = append(pieces, word)
		targets = append(targets, teamsTarget{act: teamsActMember, id: t.ID, arg: m.Key, x1: ansi.StringWidth(plain), hint: hint})
	}
	if len(pieces) == 0 {
		return []string{" " + pal.dim("no members yet")}
	}
	var out []string
	line, x := " ", 1
	sep := " " + a.teamsDot() + " "
	for i, p := range pieces {
		w := targets[i].x1
		if x > 1 && x+ansi.StringWidth(sep)+w > width-1 {
			out = append(out, line)
			line, x = " ", 1
		}
		if x > 1 {
			line += pal.dim(sep)
			x += ansi.StringWidth(sep)
		}
		tg := targets[i]
		tg.x0, tg.x1, tg.y = x, x+w, y+len(out)
		d.targets = append(d.targets, tg)
		hot, cur := d.lit(tg.ref())
		switch {
		case cur:
			p = pal.selected(p, 0)
		case hot:
			p = pal.cursor(p, 0)
		}
		line += p
		x += w
	}
	return append(out, line)
}

// ── THE INBOX ───────────────────────────────────────────────────────────────

// teamsPromptRows is the members' permission prompts, each a card with the
// answers the member's session offered: the person's own gate, answered through
// home's own door ([app.sendAnswer]). A manager never answers these.
func (a *app) teamsPromptRows(d *teamsDraw, t team, width, y int) []string {
	pal := a.pal
	var out []string
	now := time.Now()
	for _, row := range a.teamsPrompts(t) {
		question, _ := answerable(row, now)
		who := "@" + a.teamsHandleOf(t, row.Transcript)
		head := question.Text
		if question.Full != nil && strings.TrimSpace(question.Full.Head) != "" {
			head = question.Full.Head
		}
		out = append(out, "")
		out = append(out, " "+pal.ask("? "+who+" asks")+pal.dim(" "+a.teamsDot()+" ")+pal.ink(fit(switcherFirstLine(head), width-ansi.StringWidth(who)-12)))
		if _, sent := a.answerSent(row, question); sent {
			out = append(out, "   "+pal.dim(answerWaitingWord))
			continue
		}
		line, x := "  ", 2
		for _, chip := range answerChips(question) {
			s, w := d.button(chip.label, teamsTarget{act: teamsActPrompt, id: t.ID, arg: row.Transcript, opt: chip.key,
				x0: x, y: y + len(out), hint: chip.label + " for " + who + hintSegment + "your own gate; a manager never answers it"}, pal.ink)
			if x+w > width {
				break
			}
			line += s + " "
			x += w + 1
		}
		out = append(out, line)
	}
	return out
}

// teamsHandleOf is the handle, or failing that the name, of the member whose
// transcript is file.
func (a *app) teamsHandleOf(t team, file string) string {
	for _, m := range t.Members {
		if strings.TrimSpace(m.File) == strings.TrimSpace(file) {
			if m.Handle != "" {
				return m.Handle
			}
			return m.Word
		}
	}
	return "member"
}

// teamsInboxRows is the packets waiting on the person or on this team's
// manager, one card each, newest last, each beyond the last three folded to
// one line a press unfolds.
func (a *app) teamsInboxRows(d *teamsDraw, width, y int) []string {
	pal := a.pal
	if !a.teamsCanDelegate() {
		return nil
	}
	packets := a.teamsInbox()
	whole := make([]bool, len(packets))
	for i := range packets {
		whole[i] = i >= len(packets)-teamsInboxWhole || a.tp.expand == packets[i].ID
	}
	var out []string
	for i, p := range packets {
		if !whole[i] {
			// One blank line over a run of folded lines, not one each.
			if i == 0 || whole[i-1] {
				out = append(out, "")
			}
			line := " " + pal.muted(a.linearMark("▸", ">")+" "+p.Kind+" "+a.teamsDot()+" ") + pal.ink(p.Question)
			if p.Team == teamstore.Person {
				line = teamsPad(line, width-ansi.StringWidth(teamsFoldWaiting)-1) + pal.ask(teamsFoldWaiting)
			}
			out = append(out, d.row(line, width, teamsTarget{act: teamsActOption, arg: p.ID, opt: "", y: y + len(out),
				hint: "Unfold this card" + hintSegment + "enter"}, false))
			continue
		}
		out = append(out, "")
		out = append(out, a.teamsCard(d, p, width, y+len(out))...)
	}
	return out
}

// A folded decision retains the same waiting label as its expanded card so
// folding cannot hide that the person needs to answer it.
const teamsFoldWaiting = "waiting on you"

// teamsCard is one decision packet as a card:
//
//	? conflict · raised by @web                              waiting on you
//	which shape does the signup form send?
//	  @web   the form posts JSON
//	  @api   the endpoint takes form data
//	 JSON        @api changes the handler; the form stays   recommended
//	 form data   @web rewrites the submit; the handler stays
//	 Your own answer…
//
// The options are word buttons with their consequence dim beside them; the
// recommended one says so; a packet waiting on a manager is dim and says whose,
// and the person may still decide it (authority: the person first).
//
// THE LEAD MARK SAYS WHOSE IT IS, never what a manager is. A card waiting on
// the person opens with the needs-you `?` the rail and the tabs already use;
// one waiting on a manager opens with the manager's mark. Every card used to
// open with the manager's mark, so `◆ question · raised by @boss` read as the
// manager's question.
func (a *app) teamsCard(d *teamsDraw, p teamstore.Packet, width, y int) []string {
	pal := a.pal
	mine := p.Team == teamstore.Person
	var out []string
	kind := p.Kind
	if p.Kind == teamstore.PacketClosing && p.Report != nil && p.Report.Incomplete {
		kind += " " + a.teamsDot() + " wrap-up incomplete"
	}
	lead := a.teamManagerMark()
	if mine {
		lead = "?"
	}
	head := kind
	if by := strings.TrimSpace(p.RaisedBy); by != "" {
		switch by {
		case teamstore.FromManager:
			by = a.teamManagerMark() + " manager"
		case teamstore.Person:
		default:
			by = "@" + by
		}
		if by != teamstore.Person {
			head += " " + a.teamsDot() + " raised by " + by
		}
	}
	if t, ok := a.teamByID(p.Origin); ok && p.Origin != a.tp.sel {
		head += " " + a.teamsDot() + " " + t.Name
	}
	waiting := teamsFoldWaiting
	right := pal.ask(waiting)
	if !mine {
		name := p.Team
		if t, ok := a.teamByID(p.Team); ok {
			name = t.Name
		}
		waiting = "waiting on " + a.teamManagerMark() + " " + name
		right = pal.dim(waiting)
	}
	headInk, leadInk := pal.muted, pal.muted
	if mine {
		headInk, leadInk = pal.ink, pal.ask
	}
	room := width - ansi.StringWidth(waiting) - 3 - ansi.StringWidth(lead) - 1
	line := " " + leadInk(lead) + " " + headInk(fit(head, room))
	line = teamsPad(line, width-ansi.StringWidth(waiting)-1) + right
	out = append(out, line)
	for _, l := range wrap(p.Question, max(width-3, 8)) {
		out = append(out, " "+pal.ink(l))
	}
	for _, party := range p.Parties {
		who := party.Handle
		if who == "" {
			who = "member"
		}
		who = "@" + strings.TrimPrefix(who, "@")
		ctx := strings.Join(strings.Fields(party.Context), " ")
		out = append(out, "   "+pal.muted(teamsPad(who, 10))+pal.dim(fit(ctx, max(width-14, 8))))
	}
	// A CAP PACKET SAYS ITS FIGURES (DESIGN.md 8.8): what the pool spent of
	// its cap today, and which team's pool that is. Its two options,
	// `Raise to $X` and `Stop for today`, are the person's alone; the session
	// reads a raise back from these same figures.
	if c := p.Cap; c != nil {
		pool := c.Team
		if t, ok := a.teamByID(c.Team); ok {
			pool = t.Name
		}
		said := "spent " + teamsMoney(c.SpentUSD) + " of " + teamsMoney(c.CapUSD) + " today " + a.teamsDot() + " " + pool + "'s cap"
		out = append(out, "   "+pal.dim(fit(said, max(width-4, 8))))
	}
	if r := p.Report; r != nil {
		add := func(label, text string) {
			text = strings.TrimSpace(text)
			if text == "" {
				return
			}
			out = append(out, "   "+pal.muted(teamsPad(label, 7))+pal.dim(fit(text, max(width-11, 8))))
		}
		add("done", r.Done)
		add("left", r.Left)
		add("files", strings.Join(r.Files, ", "))
		if r.SpendUSD > 0 {
			add("spent", dollars(r.SpendUSD))
		}
	}
	labelW := 0
	for _, o := range p.Options {
		labelW = max(labelW, ansi.StringWidth(o.Label)+2)
	}
	labelW = min(labelW, max(width/2, 12))
	rec := ""
	if p.Recommendation != nil {
		rec = p.Recommendation.Option
	}
	for _, o := range p.Options {
		word := o.Label
		if p.Kind == teamstore.PacketClosing && (o.ID == teamstore.OptionClose || o.ID == teamstore.OptionCloseNow) {
			word = "Disband"
		}
		if ansi.StringWidth(word) > labelW-2 {
			word = ansi.Truncate(word, labelW-2, a.linearMark("…", "~"))
		}
		hint := word + hintSegment + o.Consequence
		s, w := d.button(word, teamsTarget{act: teamsActOption, arg: p.ID, opt: o.ID, x0: 1, y: y + len(out), hint: hint}, pal.ink)
		rest := strings.Repeat(" ", max(labelW-w, 0)) + "  " + pal.dim(o.Consequence)
		if o.ID == rec {
			// THE RECOMMENDED OPTION IS MARKED ON ITS OWN ROW, and its reason
			// is a line of its own under the options, where it can be read
			// whole at any width.
			rest += "  " + pal.muted(a.linearMark(a.icon(tokens.GSettled), "*")+" recommended")
		}
		out = append(out, fit(" "+s+rest, width))
	}
	if p.Recommendation != nil {
		if reason := strings.TrimSpace(p.Recommendation.Reason); reason != "" {
			for _, l := range wrap("recommended because "+reason, max(width-4, 8)) {
				out = append(out, "   "+pal.dim(l))
			}
		}
	}
	if a.tp.answering == p.ID {
		box, _, _ := draftBlock(&a.tp.answer, pal, width-4, 1, "your own answer, then enter", "")
		for _, l := range box {
			out = append(out, "  "+l)
		}
		out = append(out, "   "+pal.dim("enter decides with your words "+a.teamsDot()+" esc puts it away"))
	} else {
		s, _ := d.button("Your own answer"+a.linearMark("…", "..."), teamsTarget{act: teamsActOwnAnswer, arg: p.ID, x0: 1, y: y + len(out),
			hint: "Decide it in your own words" + hintSegment + "enter"}, pal.muted)
		out = append(out, " "+s)
	}
	return out
}

// ── THE PANE'S BODY WHEN THE MANAGER IS NOT IN IT ───────────────────────────

// teamsPaneRest is what the pane draws under its head when it hosts no
// conversation: `+ Manager` for a team without one, the manager being brought
// in front, a closed team's report, or the `All teams` row's offer.
func (a *app) teamsPaneRest(d *teamsDraw, width, y int) []string {
	pal := a.pal
	t, ok := a.teamsSelected()
	var out []string
	switch {
	case a.teamsOff():
		out = append(out, "", " "+pal.dim(fit(teamHostedWord, width-2)))
	case !ok && a.tp.sel == teamsAllRow:
		out = append(out, "", " "+pal.dim(fit("a manager over every team: you talk to it, and it talks to each team's own", width-2)))
		s, _ := d.button(teamManagerSlotWord, teamsTarget{act: teamsActRootManager, x0: 1, y: y + len(out) + 1,
			hint: "Start the manager of every team" + hintSegment + "M"}, pal.ink)
		out = append(out, "", " "+s)
	case !ok:
	case t.Closed():
		out = append(out, a.teamsClosedRows(d, t, width, y)...)
	default:
		out = append(out, "")
		out = append(out, a.teamsMemberCards(d, t, width, y+len(out))...)
		out = append(out, "")
		out = append(out, a.teamsInteractionTable(d, t, width, y+len(out))...)
	}
	return out
}

// teamsNoManagerRows is a team without a manager's one offer, under its
// members and above anything waiting, so a long inbox never pushes it off the
// pane: `+ Manager` and what a manager is, wrapped beside it.
func (a *app) teamsNoManagerRows(d *teamsDraw, t team, width, y int) []string {
	if (t.Manager != "" && !a.teamsManagerMissing(t)) || t.Closed() || t.Root || a.teamsOff() {
		return nil
	}
	pal := a.pal
	s, w := d.button(teamManagerSlotWord, teamsTarget{act: teamsActManager, id: t.ID, x0: 1, y: y + 1,
		hint: "Start " + t.Name + "'s manager: a conversation that runs the team for you" + hintSegment + "M"}, pal.ink)
	lead := 1 + w + 2
	said := wrap(teamsNoManagerWord, max(width-lead, 8))
	out := []string{""}
	for i, l := range said {
		if i == 0 {
			out = append(out, " "+s+"  "+pal.dim(l))
			continue
		}
		out = append(out, strings.Repeat(" ", lead)+pal.dim(l))
	}
	return out
}

// teamsClosedRows is a closed team's view: its closing report when there is
// one, and its members, each still a door to its conversation.
func (a *app) teamsClosedRows(d *teamsDraw, t team, width, y int) []string {
	pal := a.pal
	var out []string
	var report *teamstore.Packet
	for i, p := range a.tp.history[t.ID] {
		if p.ID == t.Report || (t.Report == "" && p.Kind == teamstore.PacketClosing) {
			report = &a.tp.history[t.ID][i]
		}
	}
	out = append(out, "")
	switch {
	case report != nil && report.Report != nil:
		out = append(out, " "+pal.muted("closing report"))
		r := report.Report
		add := func(label, text string) {
			if text = strings.TrimSpace(text); text != "" {
				for i, l := range wrap(text, max(width-11, 8)) {
					if i == 0 {
						out = append(out, "   "+pal.muted(teamsPad(label, 7))+pal.ink(l))
					} else {
						out = append(out, "          "+pal.ink(l))
					}
				}
			}
		}
		add("done", r.Done)
		add("left", r.Left)
		add("files", strings.Join(r.Files, ", "))
		if r.SpendUSD > 0 {
			add("spent", dollars(r.SpendUSD))
		}
	case !a.teamsCanReadHistory():
		out = append(out, " "+pal.dim(fit("its closing report is kept where the team ran, and is not readable over this connection", width-2)))
	default:
		out = append(out, " "+pal.dim("disbanded without a report"))
	}
	out = append(out, "", " "+pal.muted("members"))
	members := a.teamsMembersRows(d, t, width, y+len(out))
	out = append(out, members...)
	if spend := a.teamsSpendWords(t); spend != "" {
		out = append(out, "", " "+pal.dim(spend))
	}
	out = append(out, "", " "+pal.muted("Decisions"))
	for _, packet := range a.tp.history[t.ID] {
		if packet.Kind == teamstore.PacketClosing {
			continue
		}
		text := packet.Question
		if packet.Decision != "" {
			text += " · " + packet.Decision
		}
		if packet.Reason != "" {
			text += " · " + packet.Reason
		}
		for _, line := range wrap(text, max(width-2, 1)) {
			out = append(out, " "+pal.dim(line))
		}
	}
	out = append(out, "")
	return append(out, a.teamsInteractionTable(d, t, width, y+len(out))...)
}

// teamsEmpty is the page with no teams at all: what a team is, and the two
// ways to make one.
func (a *app) teamsEmpty(d *teamsDraw, width, height int) []string {
	pal := a.pal
	room := min(width-4, 72)
	lead := max((width-room)/2, 2)
	pad := strings.Repeat(" ", lead)
	var out []string
	lines := wrap(teamsExplainWord, room)
	top := max((height-len(lines)-4)/3, 1)
	for range top {
		out = append(out, "")
	}
	for _, l := range lines {
		out = append(out, pad+pal.ink(l))
	}
	out = append(out, "")
	y := len(out)
	s1, w1 := d.button(a.teamsSpark()+" "+teamsOrganizeWord, teamsTarget{act: teamsActOrganize, x0: lead, y: y,
		hint: "Suggest teams; nothing changes until you apply" + hintSegment + "o"}, pal.ink)
	s2, _ := d.button("+ "+teamsNewTeamWord, teamsTarget{act: teamsActNewTeam, x0: lead + w1 + 2, y: y,
		hint: "Make a team of the conversation in front" + hintSegment + "n"}, pal.ink)
	out = append(out, pad+s1+"  "+s2)
	return out
}

// ── THE WHOLE PAGE WHEN IT HOSTS NO CONVERSATION ────────────────────────────

// teamsBody is the page's body inside the shared place frame: the rail and the
// pane side by side, or the explainer alone with no teams. Its targets are
// recorded in frame cells.
func (a *app) teamsBody(width, room int) []placeRow {
	d := &teamsDraw{a: a}
	a.tp.table = teamsTableRect{}
	a.teamsSettle()
	var lines []string
	railW := teamsRailCols(width)
	paneW := width - railW
	var rail []string
	if railW > 0 {
		rail = a.teamsRailWindow(d, railW-1, room)
	} else {
		// A narrow rail has its own window so a long team list cannot take
		// all the space needed by the selected team's overview.
		limit := max(min(room/3, 5), 1)
		lines = append(lines, a.teamsRailWindow(d, width, limit)...)
		lines = append(lines, "")
	}
	top := len(lines)
	mark := len(d.targets)
	pane := a.teamsTop(d, paneW-1)
	if !a.teamsAny() {
		pane = a.teamsEmpty(d, paneW-1, room-top)
	}
	pane = append(pane, a.teamsPaneRest(d, paneW-1, len(pane))...)
	for i := mark; i < len(d.targets); i++ {
		d.targets[i].line = top + d.targets[i].y
	}
	// THE PANE SCROLLS TO KEEP THE CURSOR ON IT: a long inbox moves up
	// under a cursor walking down it, rather than walking it off the frame.
	if vis := room - top; len(pane) > vis && vis > 0 {
		off := 0
		for _, t := range d.targets[mark:] {
			if t.ref() == a.tp.cur {
				if t.y >= vis {
					off = min(t.y-vis+1, len(pane)-vis)
				}
				if t.act == teamsActInteractionUp || t.act == teamsActInteractionDown || t.act == teamsActInteractionToggle || t.act == teamsActInteractionJump {
					off = min(max(a.tp.table.y+a.tp.table.h-vis, 0), len(pane)-vis)
				}
				break
			}
		}
		if off > 0 {
			pane = pane[off:]
			d.shift(mark, 0, -off)
			a.tp.table.y -= off
		}
	}
	d.shift(mark, railW, top)
	a.tp.table.x += railW
	a.tp.table.y += top + placeHeadRows
	for i := mark; i < len(d.targets); i++ {
		d.targets[i].pane = true
	}
	sep := a.pal.dim(a.linearMark("│", "|"))
	for i := 0; i < room-top; i++ {
		left := ""
		if railW > 0 {
			left = strings.Repeat(" ", railW-1)
			if i < len(rail) {
				left = rail[i]
			}
			left += sep
		}
		right := ""
		if i < len(pane) {
			right = pane[i]
		}
		lines = append(lines, left+teamsPad(right, paneW))
	}
	// Offscreen targets remain keyboard stops so long overviews can be walked.
	d.shift(0, 0, placeHeadRows)
	a.tp.targets = d.targets
	// A CURSOR WHOSE BUTTON IS GONE (a team closed, a card decided) comes home
	// to the selected team's row for the next frame, rather than standing on
	// nothing.
	if a.tp.focus && len(d.targets) > 0 && a.teamsCursorIndex() < 0 {
		a.teamsCursorHome()
	}
	rows := make([]placeRow, 0, room)
	for i := 0; i < room; i++ {
		text := ""
		if i < len(lines) {
			text = lines[i]
		}
		rows = append(rows, placeRow{text: text})
	}
	return rows
}
