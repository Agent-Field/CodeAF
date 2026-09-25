package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── MOVE INTO…: PUTTING A TEAM INSIDE ANOTHER (rulings c-12, c-13) ──────────
//
// A team is moved by choosing where it goes, never by indenting it: `m` on a
// team row of the teams page's rail (or on the teams picked with `space`), and
// `Inside: harbor ▾` on the team's card, open the same picker:
//
//	╭─ Move api into ───────────────────────────────╮
//	│  Filter   ha▏                                  │
//	│  ──────────────────────────────────────────── │
//	│    Top level                                  │
//	│    ● harbor                                   │
//	│      ● dock        (dim: past the depth limit)│
//	│  ──────────────────────────────────────────── │
//	│  dock is 2 levels deep · limit 2 · Settings   │
//	╰───────────────────────────────────────────────╯
//
// EVERY TEAM IS A ROW, AND A ROW THAT CANNOT TAKE THE MOVE IS DIMMED WITH ITS
// REASON, not hidden: the team itself, a team under it, a closed team, one too
// deep for the depth limit, the place it already is. Hiding them would leave a
// person looking for harbor and not finding it; a dim row with its reason in
// the foot says why at the moment they look. The reasons are the store's
// ([teamstore.File.MoveCheck]); the words are here.
//
// TYPING FILTERS, THE TREE STAYS A TREE. The picker has one box and every
// printable key goes into it, so the arrows walk and nothing else does; a
// filtered row keeps its indent, so `api` under two teams called api is still
// told apart by where it stands.
//
// A MOVE THAT CHANGES WHO DECIDES ASKS FIRST, ONE LINE ([app.teamMoveAsk]).
// When the move would give the teams' conversations another manager to report
// to, put their spend in another capped pool, or send their conflicts to
// another manager, one line says so with `Move` and `Cancel`; every other move
// is made at once. Either way the move is offered back with `Undo` for the
// same six seconds a close is, because a move made by a slip of the pointer is
// still a slip.
//
// THE WRITE IS AN ORDINARY EDIT ([app.teamEdit]), made twice and written off
// the loop through the teams seam, so it reaches the session's own file over
// --host too. Nothing here reads a disk or a wire, and the frame draws from
// what the window holds.

// Where a move was asked from, which is where its question and its Undo are
// drawn: the teams page's pane, or the team's card.
const (
	teamMoveFromPage = iota
	teamMoveFromCard
)

// teamMoveTop is the picker's `Top level` row, and a drop on the empty rail
// under the tree.
const teamMoveTop = "\x00top"

// teamMove is the picker, a move waiting on the person's word, and the last
// move for Undo.
type teamMove struct {
	on bool
	// ids are the teams being moved, from is where it was asked.
	ids  []string
	from int
	// filter is the box; cursor the row the keyboard is on and hot the one
	// under the pointer (-1 none), both by the row's parent id, so a filter
	// that moved the rows keeps them on the same team.
	filter      editor
	cursor, hot string
	// top is the first row drawn when the rows are more than the card holds.
	top int
	// rect and hits are where the last frame drew it, in frame cells.
	rect wallRect
	hits []wallHit
	// pend is a move whose consequence line is up; undo the last move made.
	pend teamMovePend
	undo teamMoveUndo
}

// teamMovePend is a move waiting on `Move` or `Cancel`.
type teamMovePend struct {
	ids    []string
	parent string
	words  string
	from   int
}

// teamMoveUndo is the last move, while Undo is offered: every moved team's
// parent before it, and every carried conversation's home team before it.
type teamMoveUndo struct {
	back  map[string]string
	homes map[string]string
	word  string
	from  int
	at    time.Time
}

// teamMoveRow is one row of the picker: a parent (teamMoveTop for the top
// level), how deep it is drawn, and whether it can take the move and why not.
type teamMoveRow struct {
	parent string
	depth  int
	name   string
	t      team
	ok     bool
	why    string
}

// ── WHAT A MOVE IS CALLED ───────────────────────────────────────────────────

// teamMoveSubject is what is being moved, as a sentence names it: the team's
// name, or `3 teams`.
func (a *app) teamMoveSubject(ids []string) string {
	roots := a.teamTree().MoveRoots(ids)
	if len(roots) == 1 {
		if t, ok := a.teamByID(roots[0]); ok {
			return t.Name
		}
	}
	return itoa(len(roots)) + " teams"
}

// teamNameOf is team id's name as a sentence says it: `All teams` for the root,
// "" for none.
func (a *app) teamNameOf(id string) string {
	t, ok := a.teamByID(id)
	if !ok {
		return ""
	}
	if t.Root {
		return teamstore.RootName
	}
	return t.Name
}

// teamMoveWhy is a block as the hint line says it.
//
//	harbor is 3 levels deep · limit 3 · Settings
func (a *app) teamMoveWhy(b teamstore.MoveBlock, ids []string, target string) string {
	subject := a.teamMoveSubject(ids)
	dot := " " + a.teamsDot() + " "
	switch b.Kind {
	case teamstore.MoveBlockSelf:
		return "a team cannot go inside itself"
	case teamstore.MoveBlockInside:
		return a.teamNameOf(target) + " is inside " + b.Name
	case teamstore.MoveBlockClosed:
		return b.Name + " is closed"
	case teamstore.MoveBlockHere:
		if b.Name == "" || target == teamMoveTop {
			return subject + " is at the top level already"
		}
		return subject + " is in " + b.Name + " already"
	case teamstore.MoveBlockRoot:
		return teamstore.RootName + " holds every team and does not move"
	case teamstore.MoveBlockDepth:
		levels := func(n int) string {
			if n == 1 {
				return "1 level"
			}
			return itoa(n) + " levels"
		}
		from := "Settings"
		switch b.LimitFrom.Kind {
		case teamstore.OriginTeam, teamstore.OriginAncestor:
			from = "set on " + b.LimitFrom.Name
		}
		words := b.Name + " is " + levels(b.Depth) + " deep" + dot + "limit " + itoa(b.Limit) + dot + from
		if b.Need > 1 {
			words = subject + " takes " + levels(b.Need) + dot + words
		}
		return words
	}
	return "that team is gone"
}

// teamMoveParent is the parent a row or a drop names, as the store takes it:
// "" for the top level.
func teamMoveParent(parent string) string {
	if parent == teamMoveTop {
		return ""
	}
	return parent
}

// teamMoveCheck is whether ids may go into parent (a team id or
// [teamMoveTop]) and, when not, the reason in words. Frame-safe: memory only.
func (a *app) teamMoveCheck(ids []string, parent string) (bool, string) {
	b, ok := a.teamTree().MoveCheck(ids, teamMoveParent(parent), a.tp.defaults)
	if ok {
		return true, ""
	}
	return false, a.teamMoveWhy(b, ids, parent)
}

// teamMoveDoing is the sentence for a move that can be made: `Move api into
// harbor`, `Move api to the top level`.
func (a *app) teamMoveDoing(ids []string, parent string) string {
	subject := a.teamMoveSubject(ids)
	if parent == teamMoveTop || parent == "" {
		return "Move " + subject + " to the top level"
	}
	return "Move " + subject + " into " + a.teamNameOf(parent)
}

// ── THE PICKER ──────────────────────────────────────────────────────────────

// teamMoveOpen puts the picker up for ids, asked from where.
func (a *app) teamMoveOpen(ids []string, from int) tea.Cmd {
	a.teamsEnsure()
	if a.teamsOff() {
		a.tp.msg = teamHostedWord
		a.touch()
		return nil
	}
	var keep []string
	for _, id := range ids {
		if t, ok := a.teamByID(id); ok && !t.Root && !t.Closed() {
			keep = append(keep, id)
		}
	}
	if len(keep) == 0 {
		return nil
	}
	a.tmove.on, a.tmove.ids, a.tmove.from = true, keep, from
	a.tmove.filter.reset()
	a.tmove.hot, a.tmove.top = "", 0
	a.tmove.pend = teamMovePend{}
	// The keyboard starts on the first row the teams can go to.
	a.tmove.cursor = ""
	for _, r := range a.teamMoveRows() {
		if r.ok {
			a.tmove.cursor = r.parent
			break
		}
	}
	a.touch()
	if !a.tp.defaultsOK {
		return a.teamsRead(false)
	}
	return nil
}

// teamMoveShut puts the picker away.
func (a *app) teamMoveShut() {
	a.tmove.on, a.tmove.hits, a.tmove.rect = false, nil, wallRect{}
	a.touch()
}

// teamMoveRows is the picker's rows: `Top level`, then every open team but the
// root in tree order, each with whether it can take the move; with a filter,
// only the rows whose name holds it. Frame-safe: memory only.
func (a *app) teamMoveRows() []teamMoveRow {
	ids := a.tmove.ids
	want := strings.ToLower(strings.TrimSpace(a.tmove.filter.String()))
	var out []teamMoveRow
	add := func(parent, name string, depth int, t team) {
		if want != "" && !strings.Contains(strings.ToLower(name), want) {
			return
		}
		ok, why := a.teamMoveCheck(ids, parent)
		out = append(out, teamMoveRow{parent: parent, depth: depth, name: name, t: t, ok: ok, why: why})
	}
	add(teamMoveTop, "Top level", 0, team{})
	for _, r := range a.teamsOpenTree() {
		t, _ := a.teamByID(r.id)
		add(t.ID, t.Name, r.depth+1, t)
	}
	return out
}

// teamMoveKey is a key while the picker is up: it has the whole keyboard.
func (a *app) teamMoveKey(msg tea.KeyPressMsg) tea.Cmd {
	rows := a.teamMoveRows()
	at := -1
	for i, r := range rows {
		if r.parent == a.tmove.cursor {
			at = i
		}
	}
	a.touch()
	switch msg.String() {
	case "esc":
		a.teamMoveShut()
		return nil
	case "up":
		if len(rows) > 0 {
			a.tmove.cursor = rows[max(at-1, 0)].parent
		}
		return nil
	case "down":
		if len(rows) > 0 {
			a.tmove.cursor = rows[min(at+1, len(rows)-1)].parent
		}
		return nil
	case "enter":
		if at >= 0 {
			return a.teamMoveChoose(rows[at])
		}
		return nil
	case "backspace":
		a.tmove.filter.deleteBackward()
	default:
		text := msg.Key().Text
		if text == "" {
			return nil
		}
		a.tmove.filter.insert(text)
	}
	// The filter moved the rows: the cursor goes to the first that can take
	// the move, or the first at all.
	a.tmove.top = 0
	rows = a.teamMoveRows()
	a.tmove.cursor = ""
	for _, r := range rows {
		if r.ok {
			a.tmove.cursor = r.parent
			break
		}
	}
	if a.tmove.cursor == "" && len(rows) > 0 {
		a.tmove.cursor = rows[0].parent
	}
	return nil
}

// teamMoveChoose is a row chosen: a row that cannot take the move says why and
// stays up; one that can closes the picker and asks, or moves.
func (a *app) teamMoveChoose(r teamMoveRow) tea.Cmd {
	a.tmove.cursor = r.parent
	if !r.ok {
		a.touch()
		return nil
	}
	ids, from := a.tmove.ids, a.tmove.from
	a.teamMoveShut()
	return a.teamMoveAsk(ids, r.parent, from)
}

// teamMoveHitAt is the picker's row under the pointer.
func (a *app) teamMoveHitAt(x, y int) (wallHit, bool) {
	for _, hit := range a.tmove.hits {
		if x >= hit.x0 && x < hit.x1 && y >= hit.y0 && y < hit.y1 {
			return hit, true
		}
	}
	return wallHit{}, false
}

// teamMovePress is a left press while the picker is up: a row is chosen, the
// card between rows does nothing, and off the card the picker goes away.
func (a *app) teamMovePress(x, y int) tea.Cmd {
	if hit, ok := a.teamMoveHitAt(x, y); ok {
		for _, r := range a.teamMoveRows() {
			if r.parent == hit.id {
				return a.teamMoveChoose(r)
			}
		}
		return nil
	}
	if !a.tmove.rect.holds(x, y) {
		a.teamMoveShut()
	}
	return nil
}

// teamMoveMotion lights the row under the pointer.
func (a *app) teamMoveMotion(x, y int) {
	hot := ""
	if hit, ok := a.teamMoveHitAt(x, y); ok {
		hot = hit.id
	}
	if hot != a.tmove.hot {
		a.tmove.hot = hot
		a.touch()
	}
}

// teamMoveHint is what the hint line says while the picker is up: the row
// under the pointer, else the cursor's, with what enter does or why it cannot.
func (a *app) teamMoveHint() string {
	r, ok := a.teamMoveFocus()
	if !ok {
		return "type to filter · esc cancel"
	}
	if !r.ok {
		return r.why + hintSegment + "esc cancel"
	}
	return a.teamMoveDoing(a.tmove.ids, r.parent) + hintSegment + "enter" + hintSegment + "esc cancel"
}

// teamMoveFocus is the row the hint speaks for: under the pointer, else the
// cursor's.
func (a *app) teamMoveFocus() (teamMoveRow, bool) {
	rows := a.teamMoveRows()
	for _, want := range []string{a.tmove.hot, a.tmove.cursor} {
		if want == "" {
			continue
		}
		for _, r := range rows {
			if r.parent == want {
				return r, true
			}
		}
	}
	return teamMoveRow{}, false
}

// teamMoveOver lays the picker over a finished frame.
func (a *app) teamMoveOver(frame string) string {
	if !a.tmove.on {
		return frame
	}
	width, height := a.width, a.height
	card := a.teamMoveCard(width, height)
	a.tmove.hits = card.hits
	if len(card.rows) == 0 {
		a.tmove.rect = wallRect{}
		return frame
	}
	a.tmove.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	rows := strings.Split(frame, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	for dy, cr := range card.rows {
		if y := card.y + dy; y >= 0 && y < len(rows) {
			rows[y] = wallSplice(rows[y], cr, card.x, width)
		}
	}
	return strings.Join(rows[:height], "\n")
}

// teamMoveCard is the picker as a card, centred, a third of the way down.
func (a *app) teamMoveCard(width, height int) wallCard {
	pal := a.pal
	inner := min(max(width-12, 30), 52)
	rows := a.teamMoveRows()
	// The rows the card holds: the frame less the border, the padding, the
	// filter, two rules and the foot.
	room := max(height-2-2*wallCardPadY-5, 3)
	at := 0
	for i, r := range rows {
		if r.parent == a.tmove.cursor {
			at = i
		}
	}
	top := min(a.tmove.top, max(len(rows)-room, 0))
	if at < top {
		top = at
	}
	if at >= top+room {
		top = at - room + 1
	}
	a.tmove.top = top
	var lines []wallCardLine
	box := a.tmove.filter.String()
	field := pal.ink(box) + pal.ink(a.linearMark("▏", "|"))
	if box == "" {
		field = pal.ink(a.linearMark("▏", "|")) + pal.dim("type to filter")
	}
	lines = append(lines, wallCardLine{s: pal.dim(teamsPad("Filter", 10)) + field}, wallCardLine{rule: true})
	if len(rows) == 0 {
		lines = append(lines, wallCardLine{s: pal.dim("no team is called that")})
	}
	for _, r := range rows[top:min(top+room, len(rows))] {
		text := strings.Repeat("  ", r.depth)
		if r.parent == teamMoveTop {
			text += r.name
		} else {
			name := r.name
			if ansi.StringWidth(name) > teamNameCells {
				name = ansi.Truncate(name, teamNameCells, a.linearMark("…", "~"))
			}
			text += a.tabTeamDot(r.t) + " " + name
		}
		if r.ok {
			text = pal.ink(text)
		} else {
			text = pal.dim(ansi.Strip(text))
		}
		lit := r.parent == a.tmove.cursor || r.parent == a.tmove.hot
		lines = append(lines, wallCardLine{
			s:    wallPopRowPaint(pal, text, inner, lit),
			hits: []wallHit{{x0: 0, x1: inner, y1: 1, kind: wallHitPopRow, id: r.parent}},
		})
	}
	lines = append(lines, wallCardLine{rule: true})
	foot := "esc cancel"
	if r, ok := a.teamMoveFocus(); ok {
		if r.ok {
			foot = pal.muted(fit(a.teamMoveDoing(a.tmove.ids, r.parent), inner-8)) + pal.dim("  enter")
		} else {
			foot = pal.muted(fit(r.why, inner))
		}
	} else {
		foot = pal.dim(foot)
	}
	lines = append(lines, wallCardLine{s: foot})
	title := "Move " + a.teamMoveSubject(a.tmove.ids) + " into"
	w := inner + 2 + 2*wallCardPadX
	h := len(lines) + 2 + 2*wallCardPadY
	if w > width-2 || h > height {
		return wallCard{}
	}
	x := (width - w) / 2
	y := max((height-h)/3, 1)
	return wallCardBuild(pal, title, lines, x, y, w, wallCardPadX, wallCardPadY)
}

// ── ASKING, MOVING AND TAKING IT BACK ───────────────────────────────────────

// teamMoveAsk moves ids into parent (a team id or [teamMoveTop]): at once
// when nothing a person decides on changes, and otherwise after the one line.
func (a *app) teamMoveAsk(ids []string, parent string, from int) tea.Cmd {
	if ok, why := a.teamMoveCheck(ids, parent); !ok {
		a.tp.msg = why
		a.touch()
		return nil
	}
	words := a.teamMoveEffectWords(ids, parent)
	if words == "" {
		return a.teamMoveApply(ids, parent, from)
	}
	a.tmove.pend = teamMovePend{ids: ids, parent: parent, words: words, from: from}
	// The keyboard goes to the question's first answer, where `enter` moves.
	if from == teamMoveFromPage {
		a.tp.focus, a.tp.cur = true, teamsRef{act: teamsActMoveYes}
	} else {
		a.tsheet.cursor = tsMoveYes
	}
	a.tp.top = teamsTopCache{}
	a.touch()
	return nil
}

// teamMoveEffectWords is the one consequence line for a move, "" when it
// changes nothing a person is asked about.
//
//	api will report to harbor's manager · its $3/day becomes part of harbor's $10 pool
func (a *app) teamMoveEffectWords(ids []string, parent string) string {
	tree := a.teamTree()
	e, err := tree.MoveEffects(ids, teamMoveParent(parent), a.tp.defaults)
	if err != nil || !e.Changes() {
		return ""
	}
	subject := a.teamMoveSubject(ids)
	manager := func(team string) string {
		return a.teamNameOf(team) + "'s manager"
	}
	var parts []string
	if len(e.Reports) > 0 {
		// Said once for the whole move: where the first changed line now runs.
		r := e.Reports[0]
		switch {
		case r.Has:
			parts = append(parts, subject+" will report to "+manager(r.After.Team))
		case r.Had:
			parts = append(parts, subject+" will stop reporting to "+manager(r.Before.Team))
		}
	}
	if len(e.Pools) > 0 {
		p := e.Pools[0]
		its := "its spend"
		if t, ok := a.teamByID(p.Team); ok && t.Settings.CapUSDDay != nil && *t.Settings.CapUSDDay > 0 {
			its = "its " + teamsMoney(*t.Settings.CapUSDDay) + "/day"
		}
		switch {
		case p.After != "" && its == "its spend":
			parts = append(parts, "its spend counts toward "+a.teamNameOf(p.After)+"'s "+teamsMoney(p.AfterCap)+" pool")
		case p.After != "":
			parts = append(parts, its+" becomes part of "+a.teamNameOf(p.After)+"'s "+teamsMoney(p.AfterCap)+" pool")
		default:
			parts = append(parts, "its spend leaves "+a.teamNameOf(p.Before)+"'s "+teamsMoney(p.BeforeCap)+" pool")
		}
	}
	// Conflicts are said only when they go somewhere the report line has not
	// already named: a team that will report to harbor's manager and take its
	// conflicts there too is one fact, not two.
	if len(e.Judges) > 0 {
		j := e.Judges[0]
		named := len(e.Reports) > 0 && e.Reports[0].Has && e.Reports[0].After.Team == j.After
		if !named {
			who := "you"
			if j.After != "" {
				who = manager(j.After)
			}
			parts = append(parts, "conflicts in "+a.teamNameOf(j.Team)+" go to "+who)
		}
	}
	return strings.Join(parts, " "+a.teamsDot()+" ")
}

// teamMoveApply makes the move, as the person's edit, and offers it back.
func (a *app) teamMoveApply(ids []string, parent string, from int) tea.Cmd {
	tree := a.teamTree()
	roots := tree.MoveRoots(ids)
	back := map[string]string{}
	homes := map[string]string{}
	for _, id := range roots {
		t, ok := a.teamByID(id)
		if !ok {
			continue
		}
		back[id] = t.Parent
		for _, u := range append([]team{t}, tree.Descendants(id)...) {
			for _, m := range u.Members {
				if _, seen := homes[m.Key]; seen {
					continue
				}
				homes[m.Key] = ""
				if r, ok := tree.Home(m.Key); ok {
					homes[m.Key] = r.Via
				}
			}
		}
	}
	target := teamMoveParent(parent)
	word := a.teamMoveDone(roots, parent)
	if err := a.teamEdit(func(f *teamstore.File) error { return f.Move(roots, target) }); err != nil {
		a.tp.msg = "not moved: " + err.Error()
		a.touch()
		return nil
	}
	a.tmove.pend = teamMovePend{}
	a.tmove.undo = teamMoveUndo{back: back, homes: homes, word: word, from: from, at: a.now()}
	a.tp.msg = ""
	a.tp.top = teamsTopCache{}
	if a.tp.cur.act == teamsActMoveYes || a.tp.cur.act == teamsActMoveNo {
		a.tp.cur = teamsRef{act: teamsActUndo}
	}
	a.touch()
	return nil
}

// teamMoveDone is what the Undo row says of a move made: `api is in harbor
// now`, `api is at the top level now`.
func (a *app) teamMoveDone(ids []string, parent string) string {
	subject := a.teamMoveSubject(ids)
	if parent == teamMoveTop || parent == "" {
		return subject + " is at the top level now"
	}
	return subject + " is in " + a.teamNameOf(parent) + " now"
}

// teamMoveCancel drops the move waiting on the person.
func (a *app) teamMoveCancel() {
	a.tmove.pend = teamMovePend{}
	if a.tp.cur.act == teamsActMoveYes || a.tp.cur.act == teamsActMoveNo {
		a.teamsCursorHome()
	}
	if a.tsheet.cursor == tsMoveYes || a.tsheet.cursor == tsMoveNo {
		a.tsheet.cursor = tsInside
	}
	a.tp.top = teamsTopCache{}
	a.touch()
}

// teamMoveConfirm is `Move` on the consequence line.
func (a *app) teamMoveConfirm() tea.Cmd {
	p := a.tmove.pend
	if len(p.ids) == 0 {
		return nil
	}
	return a.teamMoveApply(p.ids, p.parent, p.from)
}

// teamMoveUndoing reports whether Undo is offered for the last move.
func (a *app) teamMoveUndoing() bool {
	u := a.tmove.undo
	return len(u.back) > 0 && a.now().Sub(u.at) < teamsUndoFor
}

// teamMoveUndo puts the last move back: every moved team under its parent
// before, and every carried conversation reporting where it did.
func (a *app) teamMoveUndo() tea.Cmd {
	if !a.teamMoveUndoing() {
		return nil
	}
	u := a.tmove.undo
	a.tmove.undo = teamMoveUndo{}
	err := a.teamEdit(func(f *teamstore.File) error {
		for id, parent := range u.back {
			if _, ok := f.Team(id); !ok {
				continue
			}
			if err := f.SetParent(id, parent); err != nil {
				return err
			}
		}
		// A home is a flag the store's tidy may have set on the move; it is put
		// back where it was when that membership can still be one, and a
		// conversation that had none is left to the rule.
		for key, via := range u.homes {
			if via != "" {
				_ = f.SetHome(key, via)
			}
		}
		return nil
	})
	if err != nil {
		a.tp.msg = "not moved back: " + err.Error()
	} else {
		a.tp.msg = "moved back"
	}
	a.tp.top = teamsTopCache{}
	if a.tp.cur.act == teamsActUndo {
		a.teamsCursorHome()
	}
	a.touch()
	return nil
}

// teamMoveSig is everything about a move the teams page's head is drawn from,
// for its cache: "" while nothing is asked or offered.
func (a *app) teamMoveSig() string {
	if p := a.tmove.pend; len(p.ids) > 0 && p.from == teamMoveFromPage {
		return "ask:" + p.parent + ":" + p.words
	}
	if a.teamMoveUndoing() && a.tmove.undo.from == teamMoveFromPage {
		return "undo:" + a.tmove.undo.word
	}
	return ""
}

// teamsMoveRows is the pane's row for a move: the consequence line with `Move`
// and `Cancel` while one waits, or the move just made with `Undo`. The line
// wraps under itself rather than being cut, because it is the thing being
// decided.
func (a *app) teamsMoveRows(d *teamsDraw, width, y int) []string {
	pal := a.pal
	if p := a.tmove.pend; len(p.ids) > 0 && p.from == teamMoveFromPage {
		yes, no := "Move", "Cancel"
		bw := ansi.StringWidth(yes) + ansi.StringWidth(no) + 6
		said := wrap(p.words, max(width-2, 12))
		var out []string
		for i, l := range said {
			line := " " + pal.ink(l)
			if i < len(said)-1 {
				out = append(out, line)
				continue
			}
			x := ansi.StringWidth(line) + 2
			if x+bw > width {
				out = append(out, line)
				line, x = " ", 1
			} else {
				line += "  "
			}
			s1, w1 := d.button(yes, teamsTarget{act: teamsActMoveYes, x0: x, y: y + len(out),
				hint: a.teamMoveDoing(p.ids, p.parent) + hintSegment + "enter"}, pal.accent)
			s2, _ := d.button(no, teamsTarget{act: teamsActMoveNo, x0: x + w1 + 1, y: y + len(out),
				hint: "Leave it where it is" + hintSegment + "esc"}, pal.ink)
			out = append(out, line+s1+" "+s2)
		}
		return out
	}
	if a.teamMoveUndoing() && a.tmove.undo.from == teamMoveFromPage {
		word := " " + pal.dim(a.tmove.undo.word) + "  "
		s, _ := d.button("Undo", teamsTarget{act: teamsActUndo, x0: ansi.StringWidth(word), y: y,
			hint: "Put it back where it was" + hintSegment + "u"}, pal.ink)
		return []string{word + s}
	}
	return nil
}

// teamsCanNest reports whether a new team may be made inside parent, and when
// not, why, in the picker's words. With the defaults not read yet the limit is
// not known, and the answer is yes: the store's own tidy keeps the tree whole.
func (a *app) teamsCanNest(parent string) (bool, string) {
	t, ok := a.teamByID(parent)
	if !ok {
		return false, "that team is gone"
	}
	if t.Closed() {
		return false, t.Name + " is closed"
	}
	tree := a.teamTree()
	e := tree.Effective(parent, a.tp.defaults)
	if e.DepthLimit <= 0 || tree.Depth(parent)+1 <= e.DepthLimit {
		return true, ""
	}
	return false, a.teamMoveWhy(teamstore.MoveBlock{Kind: teamstore.MoveBlockDepth, Team: t.ID, Name: t.Name,
		Depth: tree.Depth(parent), Need: 1, Limit: e.DepthLimit, LimitFrom: e.DepthFrom}, nil, parent)
}

// teamsMoney is a cap as a sentence says it: `$3` for whole dollars, `$2.50`
// otherwise. A cap is a round figure a person chose, and `$3.00/day` reads as
// a bill.
func teamsMoney(usd float64) string {
	if usd >= 1 && usd == float64(int64(usd)) {
		return "$" + itoa(int(usd))
	}
	return dollars(usd)
}
