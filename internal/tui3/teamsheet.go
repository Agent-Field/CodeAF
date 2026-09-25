package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TEAM SETTINGS CARD, AND THE CLOSE AND DELETE CARDS ─────────────────
//
// One card over whatever the frame is showing, opened from three doors that
// mean the same thing: `Team settings…` in the strip's switcher, `e` on the
// wall, and `Settings` in the teams page's header. It was a popover on the wall
// alone; it is a card of its own now because a team's settings are a fact
// about the team and not about the wall.
//
//	╭─ Team settings ─────────────────────────────────────────╮
//	│                                                          │
//	│  Name      harbor▏                                       │
//	│  Colour    ◉ ● ● ● ●                                     │
//	│  ──────────────────────────────────────────────────────  │
//	│  questions go to the manager   on · from Settings        │
//	│  daily cap                     $5 a day          reset   │
//	│  team depth                    3 levels · from harbor    │
//	│  sub-team share                50% · from Settings       │
//	│  ──────────────────────────────────────────────────────  │
//	│   Close team…                                   Done ⏎   │
//	│                                                          │
//	╰──────────────────────────────────────────────────────────╯
//
// OVERRIDES ONLY. A value the team inherits is drawn dim with where it comes
// from ([teamstore.Origin.Words]: `from Settings`, `from harbor`); a value the
// team sets is drawn in ink with `reset` beside it. Changing a dim value makes
// it an override; `reset` makes it inherit again. The writes are
// [teamstore.File.SetSettings] through [app.teamEdit], so they reach the
// session's own file, over --host included.
//
// THERE IS NO `wake` ROW. The ruling names one, and the store on this build has
// no such field (internal/teams' teamsettings.go keeps four); a row that wrote
// nothing would be a control that lies, so it waits for the store.
//
// THE SAME CARD CLOSES AND DELETES (teamclose.go says what each does), in two
// more modes, because each is a question about one team asked where the team
// is: the close card from `Close…`, the delete card only on a closed team.

// The card's modes.
type teamSheetMode int

const (
	teamSheetSettings teamSheetMode = iota + 1
	teamSheetClose
	teamSheetDelete
)

// The card's rows and buttons, as the keyboard and the pointer name them.
const (
	tsName = iota
	tsColour
	tsQuestions
	tsWake
	tsCap
	tsDepth
	tsShare
	tsCloseTeam
	tsDone
	tsWrapUp
	tsCloseNow
	tsCancel
	tsDelete
	tsKeep
	// `Inside: harbor ▾` and the move it asks for (teammove.go): its
	// consequence line's `Move` and `Cancel`, and the Undo after it.
	tsInside
	tsMoveYes
	tsMoveNo
	tsMoveUndo
	// The reset words sit on their rows; a reset is its row's code plus this.
	tsReset = 100
	// A colour swatch is its choice plus this.
	tsSwatch = 200
)

// teamSheet is the card's whole state.
type teamSheet struct {
	on   bool
	mode teamSheetMode
	team string
	// cursor is the row or button the keyboard is on, hot the one under the
	// pointer (-1 none).
	cursor, hot int
	// name is the name being edited, choices and choice the colours offered.
	name    editor
	choices []teamHueSpec
	choice  int
	// editing is the value row whose box is open, and box the box.
	editing int
	box     editor
	err     string
	// rect and hits are where the last frame drew it, in frame cells.
	rect wallRect
	hits []wallHit
}

// teamSheetOpen puts the card up on team id in mode.
func (a *app) teamSheetOpen(id string, mode teamSheetMode) tea.Cmd {
	a.teamsEnsure()
	t, ok := a.teamByID(id)
	if !ok {
		return nil
	}
	s := teamSheet{on: true, mode: mode, team: id, hot: -1, editing: -1}
	switch mode {
	case teamSheetSettings:
		s.cursor = tsName
		s.name.setText(t.Name)
		s.choices = append([]teamHueSpec{t.HueSpec()}, teamHueChoices(a.teamHues(id), teamReservedHues(a.pal), wallSwatchCount-1)...)
	case teamSheetClose:
		s.cursor = tsCloseNow
		if _, managed := a.teamsRunning(id); managed && a.teamsCanWrapUp() {
			s.cursor = tsWrapUp
		}
	case teamSheetDelete:
		s.cursor = tsKeep
	}
	a.tsheet = s
	a.touch()
	if !a.tp.defaultsOK {
		return a.teamsRead(false)
	}
	return nil
}

// teamSheetShut puts the card away, keeping a name typed into it.
func (a *app) teamSheetShut() {
	a.teamSheetRename()
	a.tsheet = teamSheet{}
	a.touch()
}

// teamSheetRename keeps the name typed into the card, when it changed.
func (a *app) teamSheetRename() {
	s := &a.tsheet
	if s.mode != teamSheetSettings {
		return
	}
	if t, ok := a.teamByID(s.team); ok {
		if name := strings.TrimSpace(s.name.String()); name != "" && name != t.Name {
			if err := a.teamRename(s.team, name); err != nil {
				a.note(err.Error())
			}
		}
	}
}

// ── WHAT EACH VALUE ROW SAYS ────────────────────────────────────────────────

// teamSheetRow is one value row as drawn: its label, the value, where it came
// from, and whether the team sets it.
type teamSheetRow struct {
	code         int
	label, value string
	from         string
	own          bool
}

// teamSheetRows is the four overrides of team t, in the ruling's order.
func (a *app) teamSheetRows(t team) []teamSheetRow {
	e := a.teamTree().Effective(t.ID, a.tp.defaults)
	known := a.tp.defaultsOK
	row := func(code int, label, value string, o teamstore.Origin) teamSheetRow {
		r := teamSheetRow{code: code, label: label, value: value, own: !o.Inherited(), from: o.Words()}
		if !known && o.Kind == teamstore.OriginSettings {
			r.value = ""
			r.from = "from Settings"
		}
		return r
	}
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	cap := "no cap"
	if e.CapUSDDay > 0 {
		cap = teamsMoney(e.CapUSDDay) + " a day"
	}
	depth := itoa(e.DepthLimit) + " levels"
	if e.DepthLimit == 1 {
		depth = "1 level"
	}
	share := strconv.Itoa(int(e.SubShare*100+0.5)) + "%"
	return []teamSheetRow{
		row(tsQuestions, "questions go to the manager", onOff(e.QuestionsUp), e.QuestionsUpFrom),
		row(tsWake, "team messages wake", onOff(e.Wake), e.WakeFrom),
		row(tsCap, "daily cap", cap, e.CapFrom),
		row(tsDepth, "team depth", depth, e.DepthFrom),
		row(tsShare, "sub-team share", share, e.SubShareFrom),
	}
}

// ── DRAWING IT ──────────────────────────────────────────────────────────────

// teamSheetOver lays the card over a finished frame and writes down where its
// targets landed. With the card down the frame comes back as it was given.
func (a *app) teamSheetOver(frame string) string {
	if !a.tsheet.on {
		return frame
	}
	width, height := a.width, a.height
	card := a.teamSheetCard(width, height)
	a.tsheet.hits = card.hits
	if len(card.rows) == 0 {
		a.tsheet.rect = wallRect{}
		return frame
	}
	a.tsheet.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
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

// teamSheetLit reports whether code wears a ground: the keyboard's or the
// pointer's.
func (a *app) teamSheetLit(code int) bool {
	return a.tsheet.cursor == code || a.tsheet.hot == code
}

// teamSheetButton paints one button of a card line at x: its word with a cell
// either side, its key dim after it, on the cursor's ground when lit. The
// default button takes the accent, the one on the card.
func (a *app) teamSheetButton(label, key string, code, x int, accent bool) (string, wallHit, int) {
	pal := a.pal
	ink := pal.ink
	if accent {
		ink = pal.accent
	}
	s := " " + ink(label)
	w := 2 + ansi.StringWidth(label)
	if key != "" {
		s += " " + pal.dim(key)
		w += 1 + ansi.StringWidth(key)
	}
	s += " "
	if a.teamSheetLit(code) {
		s = pal.cursor(s, 0)
	}
	return s, wallHit{x0: x, x1: x + w, y1: 1, kind: wallHitPopRow, arg: code}, w
}

// teamSheetCard is the card in its mode, centred, a third of the way down.
func (a *app) teamSheetCard(width, height int) wallCard {
	s := &a.tsheet
	t, ok := a.teamByID(s.team)
	if !ok {
		return wallCard{}
	}
	var title string
	var lines []wallCardLine
	inner := min(max(width-12, 40), 64)
	switch s.mode {
	case teamSheetSettings:
		title, lines = "Team settings", a.teamSheetSettingsLines(t, inner)
	case teamSheetClose:
		title, lines = "Close "+t.Name, a.teamSheetCloseLines(t, inner)
	case teamSheetDelete:
		title, lines = "Delete "+t.Name, a.teamSheetDeleteLines(t, inner)
	}
	w := inner + 2 + 2*wallCardPadX
	h := len(lines) + 2 + 2*wallCardPadY
	if w > width-2 || h > height-1 {
		return wallCard{}
	}
	x := a.teamsCardX(width, w)
	y := max((height-h)/3, 1)
	return wallCardBuild(a.pal, title, lines, x, y, w, wallCardPadX, wallCardPadY)
}

// teamsCardX is where a card w wide stands on a frame width wide: centred over
// the teams page's pane while that page stands and the pane can hold it, so
// the rail beside it stays readable (the card is about the team the rail has
// selected, and covering the rail hid which one); centred on the frame
// everywhere else.
func (a *app) teamsCardX(width, w int) int {
	if a.at(pageTeams) {
		rail := teamsRailCols(width)
		if pane := width - rail; rail > 0 && w <= pane-2 {
			return rail + (pane-w)/2
		}
	}
	return (width - w) / 2
}

// teamSheetSettingsLines is the settings card's lines.
func (a *app) teamSheetSettingsLines(t team, inner int) []wallCardLine {
	pal := a.pal
	s := &a.tsheet
	var lines []wallCardLine
	const labelW = 10
	name := s.name.String()
	field := pal.ink(name)
	if s.cursor == tsName {
		field += pal.ink(a.linearMark("▏", "|"))
	}
	nameLine := pal.dim(teamsPad("Name", labelW)) + field
	if s.cursor == tsName || s.hot == tsName {
		nameLine = pal.cursor(teamsPad(nameLine, inner), inner)
	}
	lines = append(lines, wallCardLine{s: nameLine, hits: []wallHit{{x0: 0, x1: inner, y1: 1, kind: wallHitPopRow, arg: tsName}}})
	sws, _, swh := wallSwatches(pal, wallView{}, s.choices, s.choice, labelW)
	for i := range swh {
		swh[i].kind, swh[i].arg = wallHitPopRow, tsSwatch+swh[i].arg
	}
	colour := pal.dim(teamsPad("Colour", labelW)) + sws
	if s.cursor == tsColour {
		colour += "  " + pal.dim("←→")
	}
	lines = append(lines, wallCardLine{s: colour, hits: swh})
	lines = append(lines, a.teamSheetInsideLines(t, inner, labelW)...)
	lines = append(lines, wallCardLine{rule: true})
	const valueX = 31
	for _, r := range a.teamSheetRows(t) {
		value := r.value
		var text string
		switch {
		case s.editing == r.code:
			box, _, _ := draftBlock(&s.box, pal, inner-valueX-2, 1, teamSheetBoxHint(r.code), "")
			text = pal.ink(teamsPad(r.label, valueX)) + strings.Join(box, "")
		case r.own:
			text = pal.ink(teamsPad(r.label, valueX)) + pal.ink(value)
		default:
			words := value
			if r.from != "" {
				if words != "" {
					words += " " + a.teamsDot() + " "
				}
				words += r.from
			}
			text = pal.dim(teamsPad(r.label, valueX)) + pal.dim(words)
		}
		ln := wallCardLine{hits: []wallHit{{x0: 0, x1: inner - 8, y1: 1, kind: wallHitPopRow, arg: r.code}}}
		if r.own && s.editing != r.code {
			reset := "reset"
			rs := pal.muted(reset)
			if s.hot == r.code+tsReset {
				rs = pal.cursor(pal.ink(reset), 0)
			}
			text = teamsPad(text, inner-ansi.StringWidth(reset)) + rs
			ln.hits = append(ln.hits, wallHit{x0: inner - ansi.StringWidth(reset), x1: inner, y1: 1, kind: wallHitPopRow, arg: r.code + tsReset})
		}
		if s.cursor == r.code && s.editing != r.code {
			text = pal.cursor(teamsPad(text, inner), inner)
		}
		ln.s = text
		lines = append(lines, ln)
	}
	if s.err != "" {
		lines = append(lines, wallCardLine{s: pal.bad(fit(s.err, inner))})
	}
	lines = append(lines, wallCardLine{rule: true})
	closeWord := "Close team" + a.linearMark("…", "...")
	var hits []wallHit
	row := ""
	if !t.Root {
		b, hit, _ := a.teamSheetButton(closeWord, "", tsCloseTeam, 0, false)
		row, hits = b, append(hits, hit)
	}
	enter := a.linearMark("⏎", "enter")
	doneX := inner + 2 - a.teamSheetButtonW("Done", enter)
	done, doneHit, _ := a.teamSheetButton("Done", enter, tsDone, doneX, false)
	row = teamsPad(row, doneX) + done
	lines = append(lines, wallCardLine{s: row, hits: append(hits, doneHit), bleed: true})
	return lines
}

// teamSheetInsideLines is `Inside: harbor ▾`, the team's place in the tree,
// which opens the move picker; under it, while one is asked from here, the
// move's consequence line with `Move` and `Cancel`, or the move just made with
// `Undo`. The root has no place to move to and draws none.
func (a *app) teamSheetInsideLines(t team, inner, labelW int) []wallCardLine {
	if t.Root {
		return nil
	}
	pal := a.pal
	s := &a.tsheet
	where := "Top level"
	if p, ok := a.teamByID(t.Parent); ok && !p.Root {
		where = p.Name
	}
	value := pal.ink(where + " " + a.linearMark("▾", "v"))
	line := pal.dim(teamsPad("Inside", labelW)) + value
	if s.cursor == tsInside || s.hot == tsInside {
		line = pal.cursor(teamsPad(line, inner), inner)
	}
	out := []wallCardLine{{s: line, hits: []wallHit{{x0: 0, x1: inner, y1: 1, kind: wallHitPopRow, arg: tsInside}}}}
	if p := a.tmove.pend; len(p.ids) > 0 && p.from == teamMoveFromCard {
		for _, l := range wrap(p.words, max(inner-labelW, 12)) {
			out = append(out, wallCardLine{s: strings.Repeat(" ", labelW) + pal.ink(l)})
		}
		// The line bleeds a cell to the left (wallCardLine), so a button's cell
		// of air stands where the value's first letter's left neighbour is, and
		// its word lines up with the value above it.
		yes, hy, wy := a.teamSheetButton("Move", "", tsMoveYes, labelW, true)
		no, hn, _ := a.teamSheetButton("Cancel", "esc", tsMoveNo, labelW+wy+1, false)
		out = append(out, wallCardLine{s: strings.Repeat(" ", labelW) + yes + " " + no, hits: []wallHit{hy, hn}, bleed: true})
	} else if a.teamMoveUndoing() && a.tmove.undo.from == teamMoveFromCard {
		said := pal.dim(a.tmove.undo.word) + " "
		x := labelW + 1 + ansi.StringWidth(a.tmove.undo.word) + 1
		undo, hu, _ := a.teamSheetButton("Undo", "u", tsMoveUndo, x, false)
		out = append(out, wallCardLine{s: strings.Repeat(" ", labelW+1) + said + undo, hits: []wallHit{hu}, bleed: true})
	}
	return out
}

// teamSheetButtonW is a button's width as [app.teamSheetButton] draws it.
func (a *app) teamSheetButtonW(label, key string) int {
	w := 2 + ansi.StringWidth(label)
	if key != "" {
		w += 1 + ansi.StringWidth(key)
	}
	return w
}

// teamSheetBoxHint is the dim sentence in a value row's box.
func teamSheetBoxHint(code int) string {
	switch code {
	case tsCap:
		return "dollars a day, 0 no cap"
	case tsDepth:
		return "levels, 1 to 10"
	case tsShare:
		return "percent, 1 to 100"
	}
	return ""
}

// teamSheetCloseLines is the close card: what is running, and the three ways
// on, the default in the accent.
func (a *app) teamSheetCloseLines(t team, inner int) []wallCardLine {
	pal := a.pal
	names, managed := a.teamsRunning(t.ID)
	var lines []wallCardLine
	for _, l := range wrap(teamsCloseWords(names)+".", inner) {
		lines = append(lines, wallCardLine{s: pal.ink(l)})
	}
	lines = append(lines, wallCardLine{})
	type choice struct {
		code        int
		label, key  string
		consequence string
	}
	var cs []choice
	switch {
	case t.Manager != "" && a.teamsCanWrapUp():
		cs = append(cs, choice{tsWrapUp, "Wrap up first", "", "the manager asks everyone to finish and commit, then brings you a closing report"})
	case t.Manager != "":
		// The engine behind this window has no wrap-up door: said, not hidden.
		for _, l := range wrap(teamsNoWrapUpWord, inner) {
			lines = append(lines, wallCardLine{s: pal.dim(l)})
		}
		lines = append(lines, wallCardLine{})
	}
	cs = append(cs,
		choice{tsCloseNow, "Close now", "", "stops every turn and closes the tabs; Undo for a few seconds"},
		choice{tsCancel, "Cancel", "esc", ""})
	labelW := 0
	for _, c := range cs {
		labelW = max(labelW, a.teamSheetButtonW(c.label, c.key))
	}
	for _, c := range cs {
		accent := c.code == tsWrapUp && managed
		b, hit, w := a.teamSheetButton(c.label, c.key, c.code, 0, accent)
		text := b + strings.Repeat(" ", max(labelW-w, 0))
		// The consequence wraps under itself rather than being cut: it is the
		// one thing that says what the button does.
		said := wrap(c.consequence, max(inner-labelW-2, 8))
		if len(said) > 0 {
			text += "  " + pal.dim(said[0])
		}
		lines = append(lines, wallCardLine{s: text, hits: []wallHit{hit}, bleed: true})
		for _, l := range said[min(1, len(said)):] {
			lines = append(lines, wallCardLine{s: strings.Repeat(" ", labelW+2) + pal.dim(l)})
		}
	}
	return lines
}

// teamSheetDeleteLines is the delete card: what goes, what stays, and the two
// answers, the one that cannot be undone in the failure red.
func (a *app) teamSheetDeleteLines(t team, inner int) []wallCardLine {
	pal := a.pal
	var lines []wallCardLine
	say := "Forget " + t.Name + "? Its grouping, its Traffic and its packets go. Its conversations stay in your history."
	for _, l := range wrap(say, inner) {
		lines = append(lines, wallCardLine{s: pal.ink(l)})
	}
	lines = append(lines, wallCardLine{})
	keepW := a.teamSheetButtonW("Keep", "esc")
	delW := a.teamSheetButtonW("Delete", "")
	x := inner + 2 - keepW - 1 - delW
	keep, kh, _ := a.teamSheetButton("Keep", "esc", tsKeep, x, false)
	del := " " + pal.bad("Delete") + " "
	if a.teamSheetLit(tsDelete) {
		del = pal.cursor(del, 0)
	}
	dh := wallHit{x0: x + keepW + 1, x1: x + keepW + 1 + delW, y1: 1, kind: wallHitPopRow, arg: tsDelete}
	lines = append(lines, wallCardLine{s: strings.Repeat(" ", max(x, 0)) + keep + " " + del, hits: []wallHit{kh, dh}, bleed: true})
	return lines
}

// ── THE KEYS AND THE POINTER ────────────────────────────────────────────────

// teamSheetStops is the rows and buttons the keyboard walks, in order.
func (a *app) teamSheetStops() []int {
	s := &a.tsheet
	switch s.mode {
	case teamSheetSettings:
		stops := []int{tsName, tsColour}
		t, ok := a.teamByID(s.team)
		if ok && !t.Root {
			stops = append(stops, tsInside)
			if p := a.tmove.pend; len(p.ids) > 0 && p.from == teamMoveFromCard {
				stops = append(stops, tsMoveYes, tsMoveNo)
			} else if a.teamMoveUndoing() && a.tmove.undo.from == teamMoveFromCard {
				stops = append(stops, tsMoveUndo)
			}
		}
		stops = append(stops, tsQuestions, tsWake, tsCap, tsDepth, tsShare)
		if ok && !t.Root {
			stops = append(stops, tsCloseTeam)
		}
		return append(stops, tsDone)
	case teamSheetClose:
		var stops []int
		if t, ok := a.teamByID(s.team); ok && t.Manager != "" && a.teamsCanWrapUp() {
			stops = append(stops, tsWrapUp)
		}
		return append(stops, tsCloseNow, tsCancel)
	case teamSheetDelete:
		return []int{tsKeep, tsDelete}
	}
	return nil
}

// teamSheetKey is a key while the card is up: it has the keyboard, as every
// card does.
func (a *app) teamSheetKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &a.tsheet
	key := msg.String()
	a.touch()
	if s.editing >= 0 {
		switch key {
		case "esc":
			s.editing, s.err = -1, ""
		case "enter":
			a.teamSheetSave(s.editing, s.box.String())
		case "backspace":
			s.box.deleteBackward()
		default:
			if text := msg.Key().Text; text != "" {
				s.box.insert(text)
			}
		}
		return nil
	}
	stops := a.teamSheetStops()
	at := 0
	for i, c := range stops {
		if c == s.cursor {
			at = i
		}
	}
	switch key {
	case "esc":
		// A move waiting on its line is answered first.
		if p := a.tmove.pend; len(p.ids) > 0 && p.from == teamMoveFromCard {
			a.teamMoveCancel()
			return nil
		}
		if s.mode == teamSheetSettings {
			a.teamSheetShut()
		} else {
			a.tsheet = teamSheet{}
		}
		return nil
	case "up", "shift+tab":
		s.cursor = stops[max(at-1, 0)]
		return nil
	case "down", "tab":
		s.cursor = stops[min(at+1, len(stops)-1)]
		return nil
	case "left", "right":
		if s.cursor == tsColour && len(s.choices) > 0 {
			step := 1
			if key == "left" {
				step = len(s.choices) - 1
			}
			return a.teamSheetRecolor((s.choice + step) % len(s.choices))
		}
		if s.cursor == tsName {
			if key == "left" {
				s.name.left()
			} else {
				s.name.right()
			}
		}
		return nil
	case "enter":
		return a.teamSheetDo(s.cursor)
	}
	if s.mode == teamSheetSettings && s.cursor == tsName {
		switch key {
		case "backspace":
			s.name.deleteBackward()
		default:
			if text := msg.Key().Text; text != "" {
				s.name.insert(text)
			}
		}
		return nil
	}
	if s.mode == teamSheetSettings && key == "u" && a.teamMoveUndoing() && a.tmove.undo.from == teamMoveFromCard {
		return a.teamSheetDo(tsMoveUndo)
	}
	if s.mode == teamSheetSettings && (key == "r" || key == "delete") && s.cursor >= tsQuestions && s.cursor <= tsShare {
		return a.teamSheetDo(s.cursor + tsReset)
	}
	if key == "space" {
		return a.teamSheetDo(s.cursor)
	}
	return nil
}

// teamSheetDo is one row or button of the card, pressed.
func (a *app) teamSheetDo(code int) tea.Cmd {
	s := &a.tsheet
	id := s.team
	s.err = ""
	switch {
	case code >= tsSwatch:
		return a.teamSheetRecolor(code - tsSwatch)
	case code >= tsReset:
		a.teamSheetReset(code - tsReset)
		return nil
	}
	switch code {
	case tsName:
		// enter on the name keeps it and moves on, as a form's field does.
		a.teamSheetRename()
		s.cursor = tsColour
	case tsColour:
		s.cursor = code
	case tsQuestions:
		s.cursor = code
		t, ok := a.teamByID(id)
		if !ok {
			return nil
		}
		next := !a.teamTree().Effective(t.ID, a.tp.defaults).QuestionsUp
		a.teamSheetWrite(func(set *teamstore.Settings) { set.QuestionsUp = &next })
	case tsWake:
		s.cursor = code
		t, ok := a.teamByID(id)
		if !ok {
			return nil
		}
		next := !a.teamTree().Effective(t.ID, a.tp.defaults).Wake
		a.teamSheetWrite(func(set *teamstore.Settings) { set.Wake = &next })
	case tsCap, tsDepth, tsShare:
		s.cursor, s.editing = code, code
		s.box.reset()
	case tsInside:
		s.cursor = code
		return a.teamMoveOpen([]string{id}, teamMoveFromCard)
	case tsMoveYes:
		s.cursor = tsInside
		return a.teamMoveConfirm()
	case tsMoveNo:
		a.teamMoveCancel()
	case tsMoveUndo:
		s.cursor = tsInside
		return a.teamMoveUndo()
	case tsCloseTeam:
		a.teamSheetShut()
		return a.teamsCloseAsk(id)
	case tsDone:
		a.teamSheetShut()
	case tsWrapUp:
		a.tsheet = teamSheet{}
		return a.teamsWrapUp(id)
	case tsCloseNow:
		a.tsheet = teamSheet{}
		return a.teamsCloseNow(id, "")
	case tsCancel, tsKeep:
		a.tsheet = teamSheet{}
		a.touch()
	case tsDelete:
		a.tsheet = teamSheet{}
		return a.teamsDelete(id)
	}
	return nil
}

// teamSheetRecolor takes colour j at once.
func (a *app) teamSheetRecolor(j int) tea.Cmd {
	s := &a.tsheet
	if j < 0 || j >= len(s.choices) {
		return nil
	}
	s.choice, s.cursor = j, tsColour
	if err := a.teamRecolor(s.team, s.choices[j]); err != nil {
		a.note("the colour is kept for this window, but " + err.Error())
	}
	return nil
}

// teamSheetSave reads one value row's box and writes it as the team's own
// override. A value outside its band is said on the card and nothing changes.
func (a *app) teamSheetSave(code int, raw string) {
	s := &a.tsheet
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "$"), "%"))
	if raw == "" {
		s.editing = -1
		return
	}
	switch code {
	case tsCap:
		if strings.EqualFold(raw, "no cap") {
			raw = "0"
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 {
			s.err = "a cap is dollars a day, 0 for no cap"
			return
		}
		a.teamSheetWrite(func(set *teamstore.Settings) { set.CapUSDDay = &v })
	case tsDepth:
		v, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(raw, " levels"), " level"))
		if err != nil || v < 1 || v > 10 {
			s.err = "a depth is 1 to 10 levels"
			return
		}
		a.teamSheetWrite(func(set *teamstore.Settings) { set.DepthLimit = &v })
	case tsShare:
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 100 {
			s.err = "a share is 1 to 100 percent"
			return
		}
		f := float64(v) / 100
		a.teamSheetWrite(func(set *teamstore.Settings) { set.SubShare = &f })
	}
	if s.err == "" {
		s.editing = -1
	}
}

// teamSheetReset makes one row inherit again.
func (a *app) teamSheetReset(code int) {
	a.teamSheetWrite(func(set *teamstore.Settings) {
		switch code {
		case tsQuestions:
			set.QuestionsUp = nil
		case tsWake:
			set.Wake = nil
		case tsCap:
			set.CapUSDDay = nil
		case tsDepth:
			set.DepthLimit = nil
		case tsShare:
			set.SubShare = nil
		}
	})
}

// teamSheetWrite makes one change to the card's team's overrides, as an
// ordinary edit ([app.teamEdit]).
func (a *app) teamSheetWrite(change func(*teamstore.Settings)) {
	id := a.tsheet.team
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetSettings(id, change) }); err != nil {
		a.tsheet.err = err.Error()
	}
	a.tp.top = teamsTopCache{}
	a.touch()
}

// teamSheetHitAt is the card's target under the pointer on the last frame.
func (a *app) teamSheetHitAt(x, y int) (wallHit, bool) {
	for _, hit := range a.tsheet.hits {
		if x >= hit.x0 && x < hit.x1 && y >= hit.y0 && y < hit.y1 {
			return hit, true
		}
	}
	return wallHit{}, false
}

// teamSheetPress is a left press while the card is up. A press on a target
// takes it; one on the card between targets does nothing; one off the card
// puts it away, keeping a name typed into it.
func (a *app) teamSheetPress(x, y int) tea.Cmd {
	if hit, ok := a.teamSheetHitAt(x, y); ok {
		return a.teamSheetDo(hit.arg)
	}
	if !a.tsheet.rect.holds(x, y) {
		if a.tsheet.mode == teamSheetSettings {
			a.teamSheetShut()
		} else {
			a.tsheet = teamSheet{}
			a.touch()
		}
	}
	return nil
}

// teamSheetMotion lights the target under the pointer.
func (a *app) teamSheetMotion(x, y int) {
	hot := -1
	if hit, ok := a.teamSheetHitAt(x, y); ok {
		hot = hit.arg
	}
	if hot != a.tsheet.hot {
		a.tsheet.hot = hot
		a.touch()
	}
}

// teamSheetHint is what the hint line says while the card is up.
func (a *app) teamSheetHint() string {
	s := &a.tsheet
	switch s.mode {
	case teamSheetClose:
		return "↑↓ choose · enter · esc cancel"
	case teamSheetDelete:
		return "enter · esc keep it"
	}
	if s.editing >= 0 {
		return "enter keep it as this team's own · esc cancel"
	}
	switch s.cursor {
	case tsName:
		return "type to rename · ↓ next · esc done"
	case tsColour:
		return "←→ colour · esc done"
	case tsQuestions, tsCap, tsDepth, tsShare:
		return "enter change it for this team · r reset to inherit · esc done"
	case tsInside:
		return "enter Move into… another team, or the top level · esc done"
	case tsMoveYes:
		return a.teamMoveDoing(a.tmove.pend.ids, a.tmove.pend.parent) + " · enter · esc cancel"
	case tsMoveNo:
		return "Leave it where it is · enter"
	case tsMoveUndo:
		return "Put it back where it was · u"
	}
	return "↑↓ move · enter · esc done"
}
