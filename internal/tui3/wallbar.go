package tui3

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── THE WALL'S CONTROLS: BUTTONS, TEAMS, CARDS AND POPOVERS ─────────────────
//
// Every act the wall answers a key for has a button, and every button names
// its key: ` Filter / `, ` New team s `. A person who clicks learns the key on
// the way, and a person who never clicks loses nothing, because the keys do
// what they did.
//
// This is still the painting half (wallview.go): pure functions from a
// [wallView] to rows and [wallHit]s. The hover arrives in v.hover and is only
// ever drawn here, never resolved.
//
// A CONTROL UNDER THE POINTER WEARS THE CURSOR GROUND (styles.go), the step the
// tab strip's own doors wear. A control inside something already on a ground
// takes the ladder's next step up, so the two never read as one.
//
// COLOUR BELONGS TO TEAMS AND TO STATE, AND TO NOTHING ELSE. A team's colour
// (teamhue.go) is drawn only as its dot: on its segment, on the tiles it
// holds, in its popovers. Borders and grounds stay with state.

// wallButton is one pressable label and the key that does the same thing.
type wallButton struct {
	act   wallAct
	label string
	key   string // "" for an act no single key does
}

// wallButtonW is the cells a button takes: a cell of padding either side, so
// the hover ground reads as a button and not as a highlighted word.
func wallButtonW(b wallButton) int {
	w := 2 + ansi.StringWidth(b.label)
	if b.key != "" {
		w += 1 + ansi.StringWidth(b.key)
	}
	return w
}

// wallButtonPaint draws one button: the label in ink, the key dim.
func wallButtonPaint(pal palette, b wallButton, hot bool) string {
	s := " " + pal.ink(b.label)
	if b.key != "" {
		s += " " + pal.dim(b.key)
	}
	s += " "
	if hot {
		return pal.cursor(s, 0)
	}
	return s
}

// wallLay draws buttons left to right from column x on row y, gap blank cells
// apart, and says where each landed.
func wallLay(pal palette, bs []wallButton, hover wallHitRef, x, y, gap int) (string, int, []wallHit) {
	var b strings.Builder
	hits := make([]wallHit, 0, len(bs))
	w := 0
	for i, btn := range bs {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", gap))
			w += gap
		}
		bw := wallButtonW(btn)
		hot := hover.kind == wallHitAction && hover.arg == int(btn.act)
		b.WriteString(wallButtonPaint(pal, btn, hot))
		hits = append(hits, wallHit{x0: x + w, y0: y, x1: x + w + bw, y1: y + 1, kind: wallHitAction, arg: int(btn.act)})
		w += bw
	}
	return b.String(), w, hits
}

// wallBarWidth is the cells a row of buttons takes, gap blank cells between.
func wallBarWidth(bs []wallButton, gap int) int {
	w := 0
	for i, b := range bs {
		if i > 0 {
			w += gap
		}
		w += wallButtonW(b)
	}
	return w
}

// wallKeys is the spelling of the marks the controls use, in the palette's
// tier.
type wallKeys struct {
	back, enter, pick, shuffle, minus, more, menu, caret, rule string
	boxOff, boxOn, boxSome                                     string
}

func wallKeysFor(ascii bool) wallKeys {
	if ascii {
		return wallKeys{back: "<", enter: "enter", pick: "space", shuffle: "~", minus: "-", more: "...", menu: "~", caret: "v",
			rule: "-", boxOff: "[ ]", boxOn: "[x]", boxSome: "[-]"}
	}
	return wallKeys{back: "‹", enter: "↵", pick: "␣", shuffle: "↻", minus: "−", more: "…", menu: "⋯", caret: "▾",
		rule: "─", boxOff: "☐", boxOn: "☑", boxSome: "▣"}
}

// wallPart is one piece of a row drawn on a ground: hot pieces are a control
// under the pointer and take the ladder's next step over it.
type wallPart struct {
	s   string
	hot bool
}

// wallCompose draws parts on ground, the hot ones on the mark step.
func wallCompose(pal palette, parts []wallPart, ground func(string) string) string {
	var b, run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(ground(run.String()))
			run.Reset()
		}
	}
	for _, p := range parts {
		if p.hot {
			flush()
			b.WriteString(pal.background(p.s, 0, pal.ramp.mark))
			continue
		}
		run.WriteString(p.s)
	}
	flush()
	return b.String()
}

// wallTeamMark is team i's dot in its colour, or, where there is no colour
// to draw, its initial, dim. Either way it is one cell.
func wallTeamMark(pal palette, v wallView, i int, glyph string) string {
	if i >= 0 && i < len(v.teams) {
		if ink := pal.teamInk(v.teams[i].hue); ink != nil {
			return ink(glyph)
		}
	}
	initial := "?"
	if i >= 0 && i < len(v.teams) {
		if r := []rune(v.teams[i].name); len(r) > 0 {
			initial = strings.ToLower(string(r[0]))
			if ansi.StringWidth(initial) != 1 {
				initial = "?"
			}
		}
	}
	return pal.dim(initial)
}

func wallPlural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// ── THE TOOLBAR ─────────────────────────────────────────────────────────────

// wallBar is the toolbar on row y of a frame width wide, ending a cell from
// its right edge.
func wallBar(pal palette, v wallView, width, y int) (string, []wallHit) {
	return wallBarIn(pal, v, width, wallMargin, y)
}

// wallBarIn is the toolbar along the bottom: the way back on the left, the
// acts on the wall as a whole on the right, ending inset cells from the right
// edge so its last button lines up with the grid. It never wraps: on a narrow
// frame the least needed buttons leave first, and a button is drawn whole or
// not at all.
//
// BETWEEN THE TWO IS THE STATUS LINE. While the pointer rests on any control,
// the toolbar says in one dim line what that control does and the key that
// does the same, as a desktop program's status bar does: a glyph like a
// segment's dot is learnt by pointing at it once, and nothing is drawn over
// the thing pointed at.
func wallBarIn(pal palette, v wallView, width, inset, y int) (string, []wallHit) {
	k := wallKeysFor(pal.ascii)
	hint := wallHint(v, pal.ascii)
	if v.naming {
		n := wallMarked(v)
		word := " Naming a team for " + strconv.Itoa(n) + " " + wallPlural(n, "conversation")
		if hint != "" {
			word = " " + hint
		}
		return pal.dim(ansi.Truncate(word, width, "")), nil
	}
	const gap = 2
	left := []wallButton{{act: wallActBack, label: k.back + " Back", key: "esc"}}
	acts := []wallButton{
		{act: wallActFilter, label: "Filter", key: "/"},
		{act: wallActNewTeam, label: "New team", key: "s"},
	}
	cols := []wallButton{{act: wallActColsLess, label: k.minus}, {act: wallActColsMore, label: "+"}}
	const colsWord = "Columns"
	colsW := len(colsWord) + wallBarWidth(cols, 0)
	help := []wallButton{{act: wallActHelp, label: "Help", key: "?"}}

	// A narrow frame gives up the columns first, then the acts from the right,
	// and Help last of all: it is the one button that says what the rest do.
	// The keys stay whatever is drawn.
	showCols, showActs, showHelp := true, len(acts), true
	rightW := func() int {
		w := 0
		part := func(pw int) {
			if w > 0 {
				w += gap
			}
			w += pw
		}
		if showActs > 0 {
			part(wallBarWidth(acts[:showActs], gap))
		}
		if showCols {
			part(colsW)
		}
		if showHelp {
			part(wallBarWidth(help, gap))
		}
		return w
	}
	fits := func() bool {
		return 1+wallBarWidth(left, gap)+gap+rightW()+max(inset-1, 0) <= width
	}
	for !fits() {
		switch {
		case showCols:
			showCols = false
		case showActs > 0:
			showActs--
		case showHelp:
			showHelp = false
		default:
			return "", nil
		}
	}
	s, lw, hits := wallLay(pal, left, v.hover, 1, y, gap)
	row := " " + s
	rw := rightW()
	// The last button's ground sits in the inset, as the title's last pill's
	// does, so its word ends where the grid does.
	at := width - max(inset-1, 0) - rw
	used := 1 + lw
	// The status line: three cells after the way back, and at least three
	// before the first button on the right, cut with an ellipsis before it
	// would crowd them.
	const hintGap = 3
	if room := at - used - 2*hintGap; hint != "" && room >= 8 {
		if ansi.StringWidth(hint) > room {
			hint = ansi.Truncate(hint, room, k.more)
		}
		row += strings.Repeat(" ", hintGap) + pal.dim(hint)
		used += hintGap + ansi.StringWidth(hint)
	}
	row += strings.Repeat(" ", max(at-used, 0))
	first := true
	sep := func() {
		if !first {
			row += strings.Repeat(" ", gap)
			at += gap
		}
		first = false
	}
	if showActs > 0 {
		sep()
		rs, w, rh := wallLay(pal, acts[:showActs], v.hover, at, y, gap)
		row += rs
		hits = append(hits, rh...)
		at += w
	}
	if showCols {
		sep()
		row += pal.dim(colsWord)
		cs, w, ch := wallLay(pal, cols, v.hover, at+len(colsWord), y, 0)
		row += cs
		hits = append(hits, ch...)
		at += len(colsWord) + w
	}
	if showHelp {
		sep()
		hs, w, hh := wallLay(pal, help, v.hover, at, y, gap)
		row += hs
		hits = append(hits, hh...)
		at += w
	}
	return row, hits
}

// wallMembersWord is a team's size in words: `1 member`, `3 members`.
func wallMembersWord(n int) string {
	if n == 1 {
		return "1 member"
	}
	return strconv.Itoa(n) + " members"
}

// wallHint is what the control under the pointer does, and its key after a
// dot when it has one; "" when the pointer is on no control. It is the
// person's words, the same ones the buttons use.
func wallHint(v wallView, ascii bool) string {
	h := v.hover
	sep, arrows := "·", "← →"
	if ascii {
		sep, arrows = "-", "left right"
	}
	name := func(i int) string {
		if i >= 0 && i < len(v.tiles) {
			return v.tiles[i].name
		}
		return "this conversation"
	}
	team := func(id string) string {
		if i := v.teamRow(id); i >= 0 {
			return v.teams[i].name
		}
		return "this team"
	}
	keyed := func(s, key string) string { return s + " " + sep + " " + key }
	marked := wallMarked(v)
	switch h.kind {
	case wallHitTile:
		if h.arg < 0 || h.arg >= len(v.tiles) {
			return ""
		}
		switch {
		case marked > 0 && v.tiles[h.arg].marked:
			return keyed("Click to take out of the selection", "space")
		case marked > 0:
			return keyed("Click to add to the selection", "space")
		case h.arg == min(max(v.focus, 0), len(v.tiles)-1):
			return keyed("Click to open "+name(h.arg), "enter")
		}
		return "Click to focus, click again to open"
	case wallHitSelect:
		if h.arg >= 0 && h.arg < len(v.tiles) && v.tiles[h.arg].marked {
			return keyed("Take out of the selection", "space")
		}
		return keyed("Select for a team", "space")
	case wallHitTeams:
		return keyed("Add this conversation to teams", "m")
	case wallHitOpen:
		if h.arg >= 0 && h.arg < len(v.tiles) && v.tiles[h.arg].signal == tabNeedsPerson {
			return keyed("Open the conversation to answer it", "enter")
		}
		return keyed("Open conversation", "enter")
	case wallHitClose:
		return keyed("Close this view; the work keeps running", "x")
	case wallHitChip:
		// tab steps through the teams rather than naming one, so no key is
		// offered for a single segment.
		if h.id == "" {
			return "Show every conversation open in this window"
		}
		// The team's segment counts what is open here, and the hint says what
		// the team is beside it, so the two numbers are never read as one.
		if i := v.teamRow(h.id); i >= 0 {
			row := v.teams[i]
			return row.name + " " + sep + " " + strconv.Itoa(row.count) + " open here " + sep + " " + wallMembersWord(row.members)
		}
		return "Show only the conversations in " + team(h.id)
	case wallHitChipMenu:
		return "Rename, recolour or delete " + team(h.id)
	case wallHitAddTeam:
		return keyed(wallNewTeamHint(marked), "s")
	case wallHitMini:
		return "Go to " + name(h.arg)
	case wallHitSwatch:
		return keyed("Use this colour", arrows)
	case wallHitPopRow:
		switch h.arg {
		case wallPopNew:
			return "Make a new team with this conversation in it"
		case wallPopManager:
			if v.popManager == wallManagerRemove {
				return "Make this an ordinary member again; it keeps its history"
			}
			return "Make this conversation the shown team's manager; the one before stays a member"
		case wallPopDelete:
			return "Delete this team; its conversations stay open"
		case wallPopConfirm:
			return "Delete the team now"
		case wallPopKeep:
			return keyed("Keep the team", "esc")
		case wallPopDone:
			return keyed("Keep the name and close", "enter")
		}
		return keyed("Put in or take out of "+team(h.id), "space")
	case wallHitAction:
		switch wallAct(h.arg) {
		case wallActBack:
			switch {
			case v.filter != "":
				return keyed("Clear the filter", "esc")
			case marked > 0:
				return keyed("Clear the selection", "esc")
			}
			return keyed("Back to the conversation", "esc")
		case wallActFilter:
			return keyed("Filter conversations by name", "/")
		case wallActNewTeam:
			return keyed(wallNewTeamHint(marked), "s")
		case wallActNext:
			return keyed("Go to the next conversation waiting on you", "n")
		case wallActResume:
			// Short, because the toolbar gives the hint what its buttons leave:
			// the title beside the button already says which team.
			return keyed("Resume the "+strconv.Itoa(v.away)+" not open here", "r")
		case wallActColsLess:
			return keyed("Fewer columns", "-")
		case wallActColsMore:
			return keyed("More columns", "+")
		case wallActMakeTeam:
			return keyed("Make a team of the selection", "s")
		case wallActAddTo:
			return "Add the selection to teams"
		case wallActCloseViews:
			return "Close these views; the work keeps running"
		case wallActClear:
			return keyed("Clear the selection", "esc")
		case wallActFilterClear:
			return keyed("Clear the filter", "esc")
		case wallActSave:
			return keyed("Make the team", "enter")
		case wallActCancel:
			return keyed("Put the card away", "esc")
		case wallActShuffle:
			return keyed("Suggest another name and colour", "ctrl+r")
		case wallActHelp:
			return keyed("What you can do here", "?")
		case wallActOrganize:
			return keyed("Suggest teams for your conversations", "o")
		case wallActOrgUndo:
			return keyed("Put the teams back as they were", "u")
		case wallActOrgApply:
			return keyed("Make the ticked teams", "enter")
		case wallActOrgCancel:
			return keyed("Close without changing anything", "esc")
		}
	case wallHitOrgRow:
		if h.arg >= 0 && h.arg < len(v.org.props) {
			p := v.org.props[h.arg]
			switch {
			case p.reason != "":
				return keyed(p.reason, "space")
			case p.team != "":
				return keyed("Add these to "+p.name, "space")
			case p.folder:
				return keyed("These conversations share a folder", "space")
			}
		}
		return keyed("Tick or untick this suggestion", "space")
	case wallHitHelp:
		if rows := wallHelpRows(ascii); h.arg >= 0 && h.arg < len(rows) {
			return rows[h.arg].hint
		}
	}
	if v.doorHot {
		return keyed("Close Conversations", "alt+v")
	}
	return ""
}

// wallNewTeamHint says what + New team will hold: the selection, or with
// none the focused conversation, as [app.wallStartNaming] decides.
func wallNewTeamHint(marked int) string {
	if marked > 0 {
		return "Make a team of the " + strconv.Itoa(marked) + " selected"
	}
	return "Make a team, starting with the focused conversation"
}

// wallEmptyRow is the whisper an empty wall draws, with the way back beside it
// as a button, centred.
func wallEmptyRow(pal palette, v wallView, width, y int) (string, []wallHit) {
	btn := wallButton{act: wallActBack, label: wallKeysFor(pal.ascii).back + " Back", key: "esc"}
	ww, bw := ansi.StringWidth(wallEmptyWord), wallButtonW(btn)
	if ww+3+bw > width {
		word := ansi.Truncate(wallEmptyWord, width, "")
		return strings.Repeat(" ", (width-ansi.StringWidth(word))/2) + pal.dim(word), nil
	}
	left := (width - ww - 3 - bw) / 2
	s, _, hits := wallLay(pal, []wallButton{btn}, v.hover, left+ww+3, y, 1)
	return strings.Repeat(" ", left) + pal.dim(wallEmptyWord) + "   " + s, hits
}

// wallNoneRow is what a narrowed grid with nothing in it says, centred on
// row y, beside the one button that widens it again: a filter matching
// nothing offers Clear, a team holding no open conversation offers All.
//
//	No conversations match "xyz"   Clear esc
func wallNoneRow(pal palette, v wallView, width, y int) (string, []wallHit) {
	name := "this team"
	if i := v.teamRow(v.team); i >= 0 {
		name = v.teams[i].name
	}
	word := "No open conversations in " + name
	btn := wallButton{act: wallActFilterClear, label: "Clear", key: "esc"}
	kind, arg := wallHitAction, int(wallActFilterClear)
	if v.filter != "" {
		word = "No conversations match \"" + v.filter + "\""
	} else {
		btn = wallButton{label: "Show all"}
		kind, arg = wallHitChip, 0
	}
	const gap = 3
	bw := wallButtonW(btn)
	if room := width - 2 - gap - bw; ansi.StringWidth(word) > room {
		if room < 8 {
			return "", nil
		}
		word = ansi.Truncate(word, room, wallKeysFor(pal.ascii).more)
	}
	ww := ansi.StringWidth(word)
	x := (width - ww - gap - bw) / 2
	hot := v.hover == wallHitRef{kind: kind, arg: arg}
	row := strings.Repeat(" ", x) + pal.muted(word) + strings.Repeat(" ", gap) + wallButtonPaint(pal, btn, hot)
	at := x + ww + gap
	return row, []wallHit{{x0: at, y0: y, x1: at + bw, y1: y + 1, kind: kind, arg: arg}}
}

// ── THE TEAMS ROW ───────────────────────────────────────────────────────────

// wallChipCap is the widest a team's name is drawn on its segment.
const wallChipCap = teamNameCells

// wallTeamsRow is the second row of the head: the Teams control, one
// segmented row with a segment for All, one per team, and + New team last;
// the one shown sits on the selected ground. The minimap stands at the right
// end, inset cells from the edge, when there are more conversations than the
// screen shows. While a filter narrows the grid the row is the filter instead,
// so what narrowed it and the way to undo it are both on screen.
//
//	Teams   All 6 │ ● port 3 │ ● infra 2 │ + New team                ▣▣ ▣▣ ▪▪
//
// EVERY SEGMENT IS PADDED ALIKE, one cell either side of its words, and parted
// from the next by one rule, so the row reads as one control.
//
// A SEGMENT IS A DOOR TO ITS TEAM. Its dot opens the team's settings, and so
// does the ⋯ the pointer brings up where the count was, the way a sidebar
// trades a count for its menu under the pointer: nothing beside it moves.
func wallTeamsRow(pal palette, g wallGlyphs, v wallView, width, height, inset, c, first, last, y int) (string, []wallHit) {
	var b strings.Builder
	var hits []wallHit
	x := 1
	b.WriteString(" ")
	put := func(s string, w int) {
		b.WriteString(s)
		x += w
	}
	// limit is the last cell the left part may reach; Organize's piece, when
	// it is drawn, keeps its own cells at the right.
	limit := width
	fitsAt := func(w int) bool { return x+w <= limit }
	k := wallKeysFor(pal.ascii)
	var org wallOrgPiece
	button := func(btn wallButton) {
		if !fitsAt(1 + wallButtonW(btn)) {
			return
		}
		put(" ", 1)
		s, w, h := wallLay(pal, []wallButton{btn}, v.hover, x, y, 1)
		put(s, w)
		hits = append(hits, h...)
	}

	switch {
	case v.naming && !wallNameCardFits(width, height):
		// The card has no room on this frame; the prompt is drawn here instead,
		// with the same two buttons.
		lead := pal.muted("New team "+g.gt+" ") + pal.ink(v.name) + pal.ink(g.cursor)
		if v.asking {
			lead += pal.dim(wallNamingWord(pal.ascii))
		}
		put(lead, ansi.StringWidth(lead))
		count := "   " + pal.dim(strconv.Itoa(wallMarked(v))+" picked") + "  "
		put(count, ansi.StringWidth(count))
		button(wallButton{act: wallActCancel, label: "Cancel", key: "esc"})
		button(wallButton{act: wallActSave, label: "Create", key: k.enter})
		return ansi.Truncate(b.String(), width, ""), wallHitsWithin(hits, width)
	case v.filtering || v.filter != "":
		put(pal.dim("Filter  "), 8)
		w := 2 + ansi.StringWidth(v.filter)
		paint := pal.dim("/ ") + pal.ink(v.filter)
		if v.filtering {
			paint += pal.ink(g.cursor)
			w += ansi.StringWidth(g.cursor)
		}
		if fitsAt(w) {
			hits = append(hits, wallHit{x0: x, y0: y, x1: x + w, y1: y + 1, kind: wallHitAction, arg: int(wallActFilter)})
		}
		put(paint, w)
		put("  ", 2)
		if n := len(v.tiles); v.filter != "" {
			word := "1 match"
			if n != 1 {
				word = strconv.Itoa(n) + " matches"
			}
			if fitsAt(len(word) + 2) {
				put(pal.dim(word)+" ", len(word)+1)
			}
		}
		button(wallButton{act: wallActFilterClear, label: "Clear", key: "esc"})
	default:
		// ORGANIZE STANDS AT THE RIGHT END, and yields to the teams: the row is
		// laid with its cells kept, and laid again without it when that would
		// leave a team or + New team off the row.
		org = wallOrganizeButton(pal, g, v, y)
		if org.w > 0 {
			limit = width - inset - org.w - 2
			if !wallTeamsSegments(pal, g, v, k, y, &b, &x, &hits, fitsAt) {
				b.Reset()
				b.WriteString(" ")
				x, hits, limit, org = 1, hits[:0], width, wallOrgPiece{}
				wallTeamsSegments(pal, g, v, k, y, &b, &x, &hits, fitsAt)
			}
		} else {
			wallTeamsSegments(pal, g, v, k, y, &b, &x, &hits, fitsAt)
		}
	}

	left := b.String()
	lw := x
	return wallTeamsRight(pal, g, v, left, lw, hits, org, width, inset, c, first, last, y)
}

// wallTeamsSegments lays the Teams control from cell *x: the label, All, a
// segment per team and + New team, then a moment's note of a team just made.
// It reports whether every team and + New team fit.
func wallTeamsSegments(pal palette, g wallGlyphs, v wallView, k wallKeys, y int, b *strings.Builder, x *int, hits *[]wallHit, fitsAt func(int) bool) bool {
	put := func(s string, w int) {
		b.WriteString(s)
		*x += w
	}
	whole := true
	{
		const label = "Teams   "
		put(pal.dim(label[:len(label)-1]), len(label)-1)
		sep := pal.dim("│")
		if pal.ascii {
			sep = pal.dim("|")
		}
		addW := 2 + len("+ New team")
		first := true
		// segment draws one segment and reports whether it fit, keeping room
		// for the + New team segment after it when keep is set.
		segment := func(at int, id, name, count string, on, keep bool) bool {
			dotted := at >= 0
			nw, cw := ansi.StringWidth(name), ansi.StringWidth(count)
			w := 1 + nw + 1 + cw + 1
			if dotted {
				w += 2 // the dot and its blank
			}
			need := w + 1
			if keep {
				need += 1 + addW
			}
			if !fitsAt(need) {
				return false
			}
			if !first {
				put(sep, 1)
			}
			first = false
			hot := (v.hover.kind == wallHitChip || v.hover.kind == wallHitChipMenu) && v.hover.id == id
			menuHot := v.hover.kind == wallHitChipMenu && v.hover.id == id
			ground := func(s string) string { return s }
			switch {
			case on:
				ground = func(s string) string { return pal.selected(s, 0) }
			case hot:
				ground = func(s string) string { return pal.cursor(s, 0) }
			}
			nameInk := pal.muted
			if on || hot {
				nameInk = pal.ink
			}
			var parts []wallPart
			x0, end := *x, *x+w
			if dotted {
				// The dot, with the pad before it, is the settings door.
				parts = append(parts, wallPart{s: " " + wallTeamMark(pal, v, at, "●"), hot: menuHot})
				*hits = append(*hits, wallHit{x0: x0, y0: y, x1: x0 + 2, y1: y + 1, kind: wallHitChipMenu, id: id})
				x0 += 2
			}
			parts = append(parts, wallPart{s: " " + nameInk(name) + " "})
			tail := end - 1 - cw // the count's first cell
			if dotted && hot {
				menu := k.menu
				if mw := ansi.StringWidth(menu); mw > cw {
					menu = ansi.Truncate(menu, cw, "")
				}
				*hits = append(*hits, wallHit{x0: x0, y0: y, x1: tail, y1: y + 1, kind: wallHitChip, id: id})
				parts = append(parts, wallPart{s: wallFit(pal.ink(menu), cw) + " ", hot: menuHot})
				*hits = append(*hits, wallHit{x0: tail, y0: y, x1: end, y1: y + 1, kind: wallHitChipMenu, id: id})
			} else {
				parts = append(parts, wallPart{s: pal.dim(count) + " "})
				*hits = append(*hits, wallHit{x0: x0, y0: y, x1: end, y1: y + 1, kind: wallHitChip, id: id})
			}
			put(wallCompose(pal, parts, ground), w)
			return true
		}
		segment(-1, "", "All", strconv.Itoa(v.total), v.team == "", true)
		for i, t := range v.teams {
			name := t.name
			if ansi.StringWidth(name) > wallChipCap {
				name = ansi.Truncate(name, wallChipCap, g.more)
			}
			if !segment(i, t.id, name, strconv.Itoa(t.count), t.id == v.team, true) {
				whole = false
				break
			}
		}
		// + New team is the control's last segment, drawn as an action: muted
		// until the pointer lights it.
		add := " + New team "
		if !fitsAt(1 + len(add)) {
			whole = false
		} else {
			if !first {
				put(sep, 1)
			}
			s := pal.muted(add)
			if v.hover.kind == wallHitAddTeam {
				s = pal.cursor(pal.ink(add), 0)
			}
			*hits = append(*hits, wallHit{x0: *x, y0: y, x1: *x + len(add), y1: y + 1, kind: wallHitAddTeam})
			put(s, len(add))
		}
		// A team just made says so for a moment, in the row it now sits in.
		if v.made != "" && !v.madeAt.IsZero() && v.now.Sub(v.madeAt) < wallMadeFor {
			word := "  Made " + v.made + " " + g.sep + " " + strconv.Itoa(v.madeN)
			if fitsAt(ansi.StringWidth(word)) {
				put(pal.dim(word), ansi.StringWidth(word))
			}
		}
	}
	return whole
}

// wallTeamsRight ends the Teams row: left, lw cells wide, then Organize's
// piece and the minimap at the right end, inset cells from the edge, when there
// are more conversations than the screen shows. A minimap that does not fit
// whole says the rows on screen in words instead, and neither is drawn when
// that does not fit either.
func wallTeamsRight(pal palette, g wallGlyphs, v wallView, left string, lw int, hits []wallHit, org wallOrgPiece, width, inset, c, first, last, y int) (string, []wallHit) {
	orgW := 0
	if org.w > 0 {
		orgW = org.w + 2
	}
	var mini string
	var miniW int
	var miniCells []int
	// The minimap says where the screen sits among the conversations, which
	// is news only when they do not all fit.
	if last-first < len(v.tiles) {
		mm, cells := wallMinimap(pal, g, v, c, first, last, width-lw-2-inset-orgW)
		mw := ansi.StringWidth(mm)
		if mm != "" && lw+2+mw+inset+orgW <= width && cells[len(cells)-1] >= 0 {
			mini, miniW, miniCells = mm, mw, cells
		} else {
			// A minimap missing its last cells would say the wall is shorter
			// than it is; the rows on screen are said in words instead.
			dash := "–"
			if pal.ascii {
				dash = "-"
			}
			word := strconv.Itoa(first+1) + dash + strconv.Itoa(last) + " of " + strconv.Itoa(len(v.tiles))
			if ww := ansi.StringWidth(word); lw+2+ww+inset+orgW <= width {
				mini, miniW = pal.dim(word), ww
			}
		}
	}
	if miniW == 0 && org.w == 0 {
		return ansi.Truncate(left, width, ""), wallHitsWithin(hits, width)
	}
	rightW := org.w + miniW
	if org.w > 0 && miniW > 0 {
		rightW += 2
	}
	at := width - inset - rightW
	row := left + strings.Repeat(" ", max(at-lw, 0))
	if org.w > 0 {
		row += org.s
		for _, h := range org.hits {
			h.x0, h.x1 = h.x0+at, h.x1+at
			hits = append(hits, h)
		}
		at += org.w
		if miniW > 0 {
			row += "  "
			at += 2
		}
	}
	if miniW > 0 {
		row += mini
		for i, cx := range miniCells {
			if cx >= 0 {
				hits = append(hits, wallHit{x0: at + cx, y0: y, x1: at + cx + 1, y1: y + 1, kind: wallHitMini, arg: i})
			}
		}
	}
	return row + strings.Repeat(" ", inset), wallHitsWithin(hits, width)
}

// wallHitsWithin keeps the hits that end inside the row.
func wallHitsWithin(hits []wallHit, width int) []wallHit {
	kept := hits[:0]
	for _, h := range hits {
		if h.x1 <= width {
			kept = append(kept, h)
		}
	}
	return kept
}

// ── A TILE'S ACTION ROW AND ITS TEAMS ───────────────────────────────────────

// wallTileAct is one button on a tile's action row, x cells from the tile's
// left edge.
type wallTileAct struct {
	kind wallHitKind
	btn  wallButton
	x    int
}

// wallActGap is the run of border between two buttons on the action row, so
// the row still reads as the tile's border with words set into it.
const wallActGap = 2

// wallTileActs is the buttons tile t's action row carries at width w: Open,
// or Answer on a tile waiting on a person, then Select, Teams and Close.
// They are laid from the border's second cell, two rule cells apart, and a
// narrow tile loses them from the right, keeping its first; a tile too narrow
// for even that has no row. The answer depends on the tile's width and its
// state, never on the pointer, so the cells are the same whether the row is
// drawn or not.
func wallTileActs(ascii bool, t wallTile, w int) []wallTileAct {
	k := wallKeysFor(ascii)
	first := wallButton{label: "Open", key: k.enter}
	if t.signal == tabNeedsPerson {
		first = wallAnswerButton(ascii)
	}
	all := []wallTileAct{
		{kind: wallHitOpen, btn: first},
		{kind: wallHitSelect, btn: wallButton{label: "Select", key: k.pick}},
		{kind: wallHitTeams, btn: wallButton{label: "Teams", key: "m"}},
		{kind: wallHitClose, btn: wallButton{label: "Close", key: "x"}},
	}
	// The row keeps "╰─" before the first button and at least one rule cell
	// and the corner after the last.
	x := 2
	var out []wallTileAct
	for j, act := range all {
		if j > 0 {
			x += wallActGap
		}
		bw := wallButtonW(act.btn)
		if x+bw+2 > w {
			break
		}
		act.x = x
		out = append(out, act)
		x += bw
	}
	return out
}

// wallActRow is the bottom border as the action row: each button a verb in
// ink and its key dim, in the toolbar's button style, parted by runs of the
// border, and the button under the pointer on the next ground up from the
// tile's. y is the frame row it is drawn on.
func wallActRow(pal palette, v wallView, i int, look wallTileLook, acts []wallTileAct, w, y int) string {
	box, border := look.box, look.border
	parts := []wallPart{{s: border(box.bl + box.h)}}
	at := 2
	for j, act := range acts {
		if j > 0 {
			parts = append(parts, wallPart{s: border(strings.Repeat(box.h, act.x-at))})
		}
		parts = append(parts, wallPart{s: wallButtonPaint(pal, act.btn, false), hot: wallRowHot(v, act.kind, i, y)})
		at = act.x + wallButtonW(act.btn)
	}
	parts = append(parts, wallPart{s: border(strings.Repeat(box.h, max(w-at-1, 0)) + box.br)})
	return wallFit(wallCompose(pal, parts, look.ground), w)
}

// wallTileDotsMax is the most dots a tile's border carries before the rest
// are a count.
const wallTileDotsMax = 3

// wallTileDots is the teams a tile is in, as the dots on its border: up to
// three in their colours, then +N, then a team. It is "" for a tile in no
// team, which keeps no cells at all.
func wallTileDots(pal palette, v wallView, t wallTile) (string, int) {
	if len(t.teams) == 0 {
		return "", 0
	}
	var b strings.Builder
	w := 0
	for i, id := range t.teams {
		if i == wallTileDotsMax {
			more := "+" + strconv.Itoa(len(t.teams)-wallTileDotsMax)
			b.WriteString(pal.dim(more))
			w += len(more)
			break
		}
		b.WriteString(wallTeamMark(pal, v, v.teamRow(id), "●"))
		w++
	}
	b.WriteString(" ")
	return b.String(), w + 1
}

// wallTileHits is where tile i's targets landed, its top-left corner at x0,y0.
// The first hit is always the one on the corner. A button is a target only
// while it is drawn; hidden, its cells are the tile's body, and moving onto
// them is hovering the tile, which draws it.
func wallTileHits(pal palette, v wallView, t wallTile, i int, focused bool, x0, y0, w, h int) []wallHit {
	look := wallLookFor(pal, v, t, i, focused)
	var hits []wallHit
	// row lays one border row's targets: the tile, cut around each control.
	row := func(y int, ctls []wallHit) {
		at := x0
		for _, c := range ctls {
			if c.x0 > at {
				hits = append(hits, wallHit{x0: at, y0: y, x1: c.x0, y1: y + 1, kind: wallHitTile, arg: i})
			}
			c.y0, c.y1, c.arg = y, y+1, i
			hits = append(hits, c)
			at = c.x1
		}
		if at < x0+w {
			hits = append(hits, wallHit{x0: at, y0: y, x1: x0 + w, y1: y + 1, kind: wallHitTile, arg: i})
		}
	}
	var top []wallHit
	if look.boxOn && wallSelFits(w) && w >= 5 {
		top = append(top, wallHit{x0: x0 + 2, x1: x0 + 2 + wallSelW, kind: wallHitSelect})
	}
	row(y0, top)
	if h <= 1 {
		return hits
	}
	body := func(ya, yb, xa, xb int) {
		if yb > ya && xb > xa {
			hits = append(hits, wallHit{x0: xa, y0: ya, x1: xb, y1: yb, kind: wallHitTile, arg: i})
		}
	}
	// A waiting tile's Answer is the tile's open, cut out of the body around
	// it so the two never share a cell.
	last := y0 + h - 1
	if dx, dy, bw, ok := wallAnswerAt(pal.ascii, t, w, h); ok && y0+dy < last {
		ay := y0 + dy
		body(y0+1, ay, x0, x0+w)
		body(ay, ay+1, x0, x0+dx)
		hits = append(hits, wallHit{x0: x0 + dx, y0: ay, x1: x0 + dx + bw, y1: ay + 1, kind: wallHitOpen, arg: i})
		body(ay, ay+1, x0+dx+bw, x0+w)
		body(ay+1, last, x0, x0+w)
	} else {
		body(y0+1, last, x0, x0+w)
	}
	var bottom []wallHit
	if look.rowOn {
		for _, act := range wallTileActs(pal.ascii, t, w) {
			bottom = append(bottom, wallHit{x0: x0 + act.x, x1: x0 + act.x + wallButtonW(act.btn), kind: act.kind})
		}
	}
	row(last, bottom)
	return hits
}

// ── CARDS ───────────────────────────────────────────────────────────────────

// wallCard is a rounded card floated over the grid: its rows, its top-left
// cell, its width, and its own targets in frame cells.
type wallCard struct {
	rows []string
	x, y int
	w    int
	hits []wallHit
	// over is how far a scrolled card's lines can scroll, zero for a card
	// that fits, and top the first of them it shows.
	over int
	top  int
}

// wallCardLine is one row inside a card: painted words, and the targets on
// them in cells from the row's first inner cell. A rule line is drawn as the
// card's own divider.
type wallCardLine struct {
	s    string
	hits []wallHit
	rule bool
	// bleed gives the line one cell of the padding either side, for a row of
	// buttons: a button's ground sits outside its word, so its word lines up
	// with the text above and its ground with nothing.
	bleed bool
}

// wallCardBuild draws a card w wide at x,y: a title on the top border, then
// the lines, each padded padX cells inside the border and padY blank rows
// above and below them, cut to fit.
func wallCardBuild(pal palette, title string, lines []wallCardLine, x, y, w, padX, padY int) wallCard {
	box := wallBoxLight
	if pal.ascii {
		box = wallBoxLightASCII
	}
	border := pal.muted
	inner := w - 2 - 2*padX
	card := wallCard{x: x, y: y, w: w}
	top := border(box.tl + strings.Repeat(box.h, w-2) + box.tr)
	if title != "" {
		t := " " + title + " "
		top = border(box.tl+box.h) + pal.ink(t) + border(strings.Repeat(box.h, max(w-3-ansi.StringWidth(t), 0))+box.tr)
	}
	card.rows = append(card.rows, top)
	pad := strings.Repeat(" ", padX)
	blank := border(box.v) + strings.Repeat(" ", max(w-2, 0)) + border(box.v)
	for range padY {
		card.rows = append(card.rows, blank)
	}
	for _, ln := range lines {
		k := len(card.rows) - 1
		if ln.rule {
			card.rows = append(card.rows, border(box.v)+pad+pal.dim(strings.Repeat(box.h, inner))+pad+border(box.v))
			continue
		}
		lw, lpad := inner, padX
		if ln.bleed && padX > 0 {
			lw, lpad = inner+2, padX-1
		}
		side := strings.Repeat(" ", lpad)
		card.rows = append(card.rows, border(box.v)+side+wallFit(ln.s, lw)+side+border(box.v))
		for _, h := range ln.hits {
			if h.x1 > lw {
				continue
			}
			h.x0 += x + 1 + lpad
			h.x1 += x + 1 + lpad
			h.y0, h.y1 = y+1+k, y+2+k
			card.hits = append(card.hits, h)
		}
	}
	for range padY {
		card.rows = append(card.rows, blank)
	}
	card.rows = append(card.rows, border(box.bl+strings.Repeat(box.h, w-2)+box.br))
	return card
}

// A card's padding: two cells inside its border either side and one blank row
// above and below, the same for every card and popover, so each reads as the
// same kind of thing. The tray is a floating toolbar and keeps the toolbar's
// single row.
const (
	wallCardPadX = 2
	wallCardPadY = 1
)

// wallTray is the selection tray: while any conversation is picked, a floating
// toolbar docked on the foot's rule says how many, and holds everything that
// can be done to them, the most used first.
func wallTray(pal palette, g wallGlyphs, v wallView, width, height int) wallCard {
	n := wallMarked(v)
	if n == 0 || height < 8 {
		return wallCard{}
	}
	k := wallKeysFor(pal.ascii)
	word := strconv.Itoa(n) + " selected"
	lead := pal.accent(g.marked) + " " + pal.ink(word) + "  "
	leadW := ansi.StringWidth(g.marked) + 1 + len(word) + 2
	bs := []wallButton{
		{act: wallActMakeTeam, label: "Make team", key: "s"},
		{act: wallActAddTo, label: "Add to" + k.more + " " + k.caret},
		{act: wallActCloseViews, label: "Close views"},
		{act: wallActClear, label: "Clear", key: "esc"},
	}
	room := width - 2 - 2 - 2*wallCardPadX
	// Close views leaves first, then Add to: Make team and Clear are the two a
	// selection cannot do without.
	for _, act := range []wallAct{wallActCloseViews, wallActAddTo} {
		if leadW+wallBarWidth(bs, 1) <= room {
			break
		}
		for i, b := range bs {
			if b.act == act {
				bs = append(bs[:i], bs[i+1:]...)
				break
			}
		}
	}
	if leadW+wallBarWidth(bs, 1) > room {
		return wallCard{}
	}
	s, bw, hits := wallLay(pal, bs, v.hover, leadW, 0, 1)
	w := leadW + bw + 2 + 2*wallCardPadX
	x := (width - w) / 2
	// Its bottom border lies on the foot's rule, so the tray reads as risen
	// out of the foot and never covers the toolbar.
	y := height - wallFootRows - 1
	return wallCardBuild(pal, "", []wallCardLine{{s: lead + s, hits: hits}}, x, y, w, wallCardPadX, 0)
}

// wallNamingWord is what the card says while a name is asked for, with the
// two cells that part it from the field.
func wallNamingWord(ascii bool) string {
	if ascii {
		return "  naming..."
	}
	return "  naming…"
}

// wallNameCardRows is the new-team card's height: two borders, the padding
// above and below, and four lines.
const wallNameCardRows = 2 + 2*wallCardPadY + 4

// wallNameCardFits reports whether the new-team card has room on a frame.
func wallNameCardFits(width, height int) bool {
	return width >= 44 && height >= wallChromeRows+wallNameCardRows+2
}

// wallSwatches is a row of colour choices, the one taken drawn ringed, and a
// target on each. It says how wide it is.
func wallSwatches(pal palette, v wallView, choices []teamHueSpec, choice, x int) (string, int, []wallHit) {
	var b strings.Builder
	var hits []wallHit
	w := 0
	for j, c := range choices {
		if j > 0 {
			b.WriteString(" ")
			w++
		}
		glyph := "●"
		if j == choice {
			glyph = "◉"
		}
		ink := pal.teamInk(c)
		switch {
		case ink != nil:
			glyph = ink(glyph)
		case j == choice:
			glyph = pal.ink("@")
		default:
			glyph = pal.dim("o")
		}
		if v.hover.kind == wallHitSwatch && v.hover.arg == j {
			glyph = pal.background(glyph, 0, pal.ramp.mark)
		}
		b.WriteString(glyph)
		hits = append(hits, wallHit{x0: x + w, y0: 0, x1: x + w + 1, y1: 1, kind: wallHitSwatch, arg: j})
		w++
	}
	return b.String(), w, hits
}

// wallNameCard is the card a new team is named in:
//
//	╭─ New team ────────────────────────────────────╮
//	│                                                │
//	│  Name    harbor▌                    ↻ Shuffle  │
//	│  Colour  ◉ ● ● ● ● ●                           │
//	│  3 · the tree walk, ship the port, relay au…   │
//	│                         Cancel esc   Create ↵  │
//	│                                                │
//	╰────────────────────────────────────────────────╯
//
// A name the wall filled in is drawn selected, as a text field draws a
// suggestion: the first key typed replaces it.
func wallNameCard(pal palette, g wallGlyphs, v wallView, width, height int) wallCard {
	if !wallNameCardFits(width, height) {
		return wallCard{}
	}
	k := wallKeysFor(pal.ascii)
	w := min(60, width-4)
	inner := w - 2 - 2*wallCardPadX

	shuffle := wallButton{act: wallActShuffle, label: k.shuffle + " Shuffle", key: "ctrl+r"}
	if inner < 46 {
		shuffle.key = ""
	}
	sw := wallButtonW(shuffle)
	const labelW = 8
	nameRoom := max(inner-labelW-sw-2-ansi.StringWidth(g.cursor), 1)
	name := v.name
	if ansi.StringWidth(name) > nameRoom {
		// The end of a long name is the part being typed.
		name = ansi.TruncateLeft(name, ansi.StringWidth(name)-nameRoom, "")
	}
	field := pal.ink(name)
	if v.nameFresh && name != "" {
		field = pal.selected(pal.ink(name), 0)
	}
	field += pal.ink(g.cursor)
	fieldW := labelW + ansi.StringWidth(name) + ansi.StringWidth(g.cursor)
	// While a better name is asked for, the card says so, quietly, beside the
	// field, and only where it fits whole.
	if v.asking {
		word := wallNamingWord(pal.ascii)
		if ww := ansi.StringWidth(word); inner+1-sw-fieldW >= ww+1 {
			field += pal.dim(word)
			fieldW += ww
		}
	}
	// The Name row and the button row bleed (wallCardLine), so Shuffle and
	// Create end on the text's right edge; the label takes the cell back.
	s, _, sh := wallLay(pal, []wallButton{shuffle}, v.hover, inner+2-sw, 0, 1)
	l1 := wallCardLine{s: " " + pal.dim("Name    ") + field + strings.Repeat(" ", max(inner+1-sw-fieldW, 0)) + s, hits: sh, bleed: true}

	sws, _, swh := wallSwatches(pal, v, v.choices, v.choice, labelW)
	l2 := wallCardLine{s: pal.dim("Colour  ") + sws, hits: swh}

	var names []string
	for _, t := range v.tiles {
		if t.marked {
			names = append(names, t.name)
		}
	}
	who := strconv.Itoa(len(names)) + " " + g.sep + " " + strings.Join(names, ", ")
	if ansi.StringWidth(who) > inner {
		who = ansi.Truncate(who, inner, g.more)
	}
	l3 := wallCardLine{s: pal.dim(who)}

	bs := []wallButton{
		{act: wallActCancel, label: "Cancel", key: "esc"},
		{act: wallActSave, label: "Create", key: k.enter},
	}
	bw := wallBarWidth(bs, 1)
	bstr, _, bh := wallLay(pal, bs, v.hover, inner+2-bw, 0, 1)
	l4 := wallCardLine{s: strings.Repeat(" ", max(inner+2-bw, 0)) + bstr, hits: bh, bleed: true}

	x := (width - w) / 2
	gridH := height - wallChromeRows
	y := wallGridTop + max((gridH-wallNameCardRows)/2, 0)
	return wallCardBuild(pal, "New team", []wallCardLine{l1, l2, l3, l4}, x, y, w, wallCardPadX, wallCardPadY)
}

// ── POPOVERS ────────────────────────────────────────────────────────────────

// wallPopCard is the popover that is up, hung under the control that opened
// it with its left border under the control's first cell, or over the
// control when there is no room below. It stays a margin inside the frame's
// sides, and never reaches the foot's rule or the toolbar under it.
func wallPopCard(pal palette, g wallGlyphs, v wallView, width, height int) wallCard {
	var title string
	var lines []wallCardLine
	var inner int
	switch v.pop.kind {
	case wallPopMembers:
		title, lines, inner = wallMembersLines(pal, g, v)
	case wallPopSettings:
		title, lines, inner = wallSettingsLines(pal, g, v)
	default:
		return wallCard{}
	}
	w := inner + 2 + 2*wallCardPadX
	h := len(lines) + 2 + 2*wallCardPadY
	floor := height - wallFootRows + 1 // the first row a card may not cover
	if w > width-2*wallMargin || h > floor {
		return wallCard{}
	}
	x := min(max(v.pop.x, wallMargin), width-wallMargin-w)
	y := v.pop.y1
	if y+h > floor {
		y = v.pop.y0 - h
	}
	y = min(max(y, 0), floor-h)
	return wallCardBuild(pal, title, lines, x, y, w, wallCardPadX, wallCardPadY)
}

// wallPopRowPaint lays one popover row across the inner width, on the cursor
// ground when the keyboard or the pointer is on it.
func wallPopRowPaint(pal palette, s string, inner int, lit bool) string {
	s = wallFit(s, inner)
	if lit {
		return pal.cursor(s, 0)
	}
	return s
}

// wallMembersLines is the teams popover: every team with a box saying
// whether the targets are in it (partly, when some are and some are not),
// and a way to a new one. It is a menu, so a box pressed is saved at once
// and there is no button to confirm it.
//
//	╭─ Teams ───────────────────╮
//	│                            │
//	│  ☑ ● harbor             3  │
//	│  ☐ ● orbit              5  │
//	│  ────────────────────────  │
//	│  + New team…              │
//	│                            │
//	╰────────────────────────────╯
func wallMembersLines(pal palette, g wallGlyphs, v wallView) (string, []wallCardLine, int) {
	k := wallKeysFor(pal.ascii)
	in := map[string][]string{}
	for _, t := range v.tiles {
		in[t.tab.key] = t.teams
	}
	inner := 24
	for _, t := range v.teams {
		inner = max(inner, ansi.StringWidth(k.boxOff)+3+min(ansi.StringWidth(t.name), wallChipCap)+6)
	}
	// The manager's row says which team and what it replaces, so the card is
	// as wide as that sentence, up to a width a popover can hold.
	if v.popManager != 0 {
		inner = max(inner, min(ansi.StringWidth(v.popManagerWord), 56))
	}
	var lines []wallCardLine
	row := func(code int, id, s string, cursorAt int) {
		lit := v.pop.cursor == cursorAt || v.hover == wallHitRef{kind: wallHitPopRow, arg: code, id: id}
		lines = append(lines, wallCardLine{
			s:    wallPopRowPaint(pal, s, inner, lit),
			hits: []wallHit{{x0: 0, y0: 0, x1: inner, y1: 1, kind: wallHitPopRow, arg: code, id: id}},
		})
	}
	for i, t := range v.teams {
		held := 0
		for _, key := range v.pop.targets {
			for _, id := range in[key] {
				if id == t.id {
					held++
				}
			}
		}
		name := t.name
		box := pal.muted(k.boxOff)
		switch {
		case held > 0 && held == len(v.pop.targets):
			box = pal.ink(k.boxOn)
		case held > 0:
			box = pal.ink(k.boxSome)
		}
		if ansi.StringWidth(name) > wallChipCap {
			name = ansi.Truncate(name, wallChipCap, g.more)
		}
		left := box + " " + wallTeamMark(pal, v, i, "●") + " " + pal.ink(name)
		cs := strconv.Itoa(t.count)
		gap := inner - ansi.StringWidth(left) - len(cs)
		row(wallPopTeam, t.id, left+strings.Repeat(" ", max(gap, 1))+pal.dim(cs), i)
	}
	if len(v.teams) > 0 {
		lines = append(lines, wallCardLine{rule: true})
	}
	row(wallPopNew, "", pal.muted("+ New team"+k.more), len(v.teams))
	if v.popManager != 0 {
		word := v.popManagerWord
		if word == "" {
			word = v.mark + " Make manager"
		}
		if ansi.StringWidth(word) > inner {
			word = ansi.Truncate(word, inner, wallGlyphsFor(pal.ascii).more)
		}
		row(wallPopManager, "", pal.muted(word), len(v.teams)+1)
	}
	return "Teams", lines, inner
}

// wallPopButton is one button in a popover's last row, as a popover row: a
// press is a wallHitPopRow with its code. danger draws the label in the
// failure red, which is kept for the one act that cannot be undone.
type wallPopButton struct {
	label, key string
	code       int
	danger     bool
}

// wallPopButtons lays buttons one cell apart at the right, primary last, and
// lead at the left when it is given. The row bleeds, so the words line up
// with the text above on both sides.
func wallPopButtons(pal palette, v wallView, inner int, lead *wallPopButton, bs ...wallPopButton) wallCardLine {
	paint := func(b wallPopButton) (string, int) {
		w := 2 + ansi.StringWidth(b.label)
		ink := pal.ink
		if b.danger {
			ink = pal.bad
		}
		s := " " + ink(b.label)
		if b.key != "" {
			s += " " + pal.dim(b.key)
			w += 1 + ansi.StringWidth(b.key)
		}
		s += " "
		if v.hover.kind == wallHitPopRow && v.hover.arg == b.code {
			s = pal.cursor(s, 0)
		}
		return s, w
	}
	ln := wallCardLine{bleed: true}
	inner += 2 // the line bleeds into the padding (wallCardLine)
	var b strings.Builder
	x := 0
	if lead != nil {
		s, w := paint(*lead)
		b.WriteString(s)
		ln.hits = append(ln.hits, wallHit{x0: 0, x1: w, y1: 1, kind: wallHitPopRow, arg: lead.code})
		x = w
	}
	rw := 0
	for i, bt := range bs {
		_, w := paint(bt)
		if i > 0 {
			rw++
		}
		rw += w
	}
	b.WriteString(strings.Repeat(" ", max(inner-x-rw, 1)))
	x = max(inner-rw, x+1)
	var right []wallHit
	for i, bt := range bs {
		if i > 0 {
			b.WriteString(" ")
			x++
		}
		s, w := paint(bt)
		b.WriteString(s)
		right = append(right, wallHit{x0: x, x1: x + w, y1: 1, kind: wallHitPopRow, arg: bt.code})
		x += w
	}
	// The primary's target is listed first of the right-hand ones, so a
	// search of the targets by kind alone meets the primary before the rest.
	for i := len(right) - 1; i >= 0; i-- {
		ln.hits = append(ln.hits, right[i])
	}
	ln.s = b.String()
	return ln
}

// wallSettingsLines is a team's settings: its name, being edited as it is
// typed, its colour among the others it could have, and at the foot its
// deletion on the left and Done on the right. The deletion asks first and
// says the conversations stay open.
//
//	╭─ Team ──────────────────────────╮
//	│                                  │
//	│  Name    port▌                   │
//	│  Colour  ◉ ● ● ● ● ●             │
//	│  ──────────────────────────────  │
//	│  Delete team           Done ↵   │
//	│                                  │
//	╰──────────────────────────────────╯
func wallSettingsLines(pal palette, g wallGlyphs, v wallView) (string, []wallCardLine, int) {
	k := wallKeysFor(pal.ascii)
	name := ""
	if i := v.teamRow(v.pop.team); i >= 0 {
		name = v.teams[i].name
	}
	inner := 30
	const labelW = 8
	field := v.pop.name
	if room := inner - labelW - 1; ansi.StringWidth(field) > room {
		field = ansi.TruncateLeft(field, ansi.StringWidth(field)-room, "")
	}
	l1 := wallCardLine{s: pal.dim("Name    ") + pal.ink(field) + pal.ink(g.cursor)}
	sws, _, swh := wallSwatches(pal, v, v.pop.choices, v.pop.choice, labelW)
	l2 := wallCardLine{s: pal.dim("Colour  ") + sws, hits: swh}
	lines := []wallCardLine{l1, l2, {rule: true}}
	if !v.pop.confirm {
		del := wallPopButton{label: "Delete team", code: wallPopDelete, danger: true}
		lines = append(lines, wallPopButtons(pal, v, inner, &del, wallPopButton{label: "Done", key: k.enter, code: wallPopDone}))
		return "Team settings", lines, inner
	}
	ask := "Delete " + name + "?"
	if ansi.StringWidth(ask) > inner {
		ask = ansi.Truncate(ask, inner, g.more)
	}
	lines = append(lines,
		wallCardLine{s: pal.ink(ask)},
		wallCardLine{s: pal.dim("Its conversations stay open.")},
		wallCardLine{},
		wallPopButtons(pal, v, inner, nil,
			wallPopButton{label: "Keep", key: "esc", code: wallPopKeep},
			wallPopButton{label: "Delete", code: wallPopConfirm, danger: true}))
	return "Team settings", lines, inner
}

// ── LAYING A CARD OVER THE FRAME ────────────────────────────────────────────

// wallSplice lays a card's row over a frame row at column x. The row is padded
// to the frame's width first, so a card can float over a blank row, and the
// card is fenced by resets so no ground leaks in or out of it.
func wallSplice(row, card string, x, width int) string {
	row = wallFit(row, width)
	cw := ansi.StringWidth(card)
	left := ansi.Cut(row, 0, x)
	right := ansi.Cut(row, x+cw, width)
	if !strings.Contains(row, "\x1b") && !strings.Contains(card, "\x1b") {
		return left + card + right
	}
	return left + "\x1b[0m" + card + "\x1b[0m" + right
}

// wallCarve takes the rectangle x0,y0..x1,y1 out of every hit, keeping what is
// left of each around it, so nothing under a card answers the pointer. A
// control the card covers any of is dropped whole: a sliver of a button left
// beside a card is a target on a cell that no longer says what it does.
func wallCarve(hits []wallHit, x0, y0, x1, y1 int) []wallHit {
	out := make([]wallHit, 0, len(hits))
	for _, h := range hits {
		if h.x1 <= x0 || h.x0 >= x1 || h.y1 <= y0 || h.y0 >= y1 {
			out = append(out, h)
			continue
		}
		if h.kind != wallHitTile {
			continue
		}
		if h.y0 < y0 {
			a := h
			a.y1 = y0
			out = append(out, a)
		}
		if h.y1 > y1 {
			a := h
			a.y0 = y1
			out = append(out, a)
		}
		mid := h
		mid.y0, mid.y1 = max(h.y0, y0), min(h.y1, y1)
		if h.x0 < x0 {
			a := mid
			a.x1 = x0
			out = append(out, a)
		}
		if h.x1 > x1 {
			a := mid
			a.x0 = x1
			out = append(out, a)
		}
	}
	return out
}

// wallOverlay lays a card over the frame: its rows spliced in, what it covers
// taken out of the hits, its own targets added.
func wallOverlay(rows []string, hits []wallHit, card wallCard, width int) []wallHit {
	if len(card.rows) == 0 {
		return hits
	}
	for dy, cr := range card.rows {
		if y := card.y + dy; y >= 0 && y < len(rows) {
			rows[y] = wallSplice(rows[y], cr, card.x, width)
		}
	}
	hits = wallCarve(hits, card.x, card.y, card.x+card.w, card.y+len(card.rows))
	return append(hits, card.hits...)
}
