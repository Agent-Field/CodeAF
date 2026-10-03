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
// Settings belongs to the Teams page. The five rows use the same override and
// reset mechanics; inherited parent values name their source, while profile
// defaults need no repeated provenance. Movement remains in the Teams picker.
// Writes reach the session's own store, including over --host.
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
	tsDeleteTeam
	tsDone
	tsWrapUp
	tsCloseNow
	tsCancel
	tsDelete
	tsKeep
	// Retired movement hit codes remain stable so stale frames cannot become
	// a different settings action after the Inside control is removed.
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
	on                     bool
	mode                   teamSheetMode
	affected               []string
	detailsTop, detailsMax int
	team                   string
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
	s := teamSheet{on: true, mode: mode, team: id, hot: -1, editing: -1, affected: a.teamsAffectedIDs(id)}
	switch mode {
	case teamSheetSettings:
		s.cursor = tsName
		s.name.setText(t.Name)
		s.choices = append([]teamHueSpec{t.HueSpec()}, teamHueChoices(a.teamHues(id), teamReservedHues(a.pal), wallSwatchCount-1)...)
	case teamSheetClose:
		s.cursor = tsCancel
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

// teamSheetRows lists the five overrides in their displayed order.
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
	inner := min(width-8, 64)
	if inner < 10 || height < 8 {
		return wallCard{}
	}
	switch s.mode {
	case teamSheetSettings:
		title, lines = "Team settings", a.teamSheetSettingsLines(t, inner)
	case teamSheetClose:
		title, lines = "Disband "+t.Name, a.teamSheetCloseLines(t, inner)
	case teamSheetDelete:
		title, lines = "Delete "+t.Name, a.teamSheetDeleteLines(t, inner)
	}
	hints := []teamFooterHint{teamHint("up/down", "move"), teamHint("enter", "choose"), teamHint("esc", "done")}
	if s.editing >= 0 {
		hints = []teamFooterHint{teamHint("enter", "save"), teamHint("esc", "cancel")}
	}
	if s.mode != teamSheetSettings {
		hints = []teamFooterHint{teamHint("up/down", "choose"), teamHint("enter", "choose"), teamHint("esc", "cancel")}
		choices := 1
		if s.mode == teamSheetClose {
			choices = 2
		}
		footer := teamFooter(a.pal, inner, hints...)
		budget := max(height-7-choices-len(footer), 1)
		content := lines[:len(lines)-choices]
		if len(content) > budget {
			hints = append([]teamFooterHint{teamHint("pgup/pgdown", "details")}, hints...)
			footer = teamFooter(a.pal, inner, hints...)
			budget = max(height-7-choices-len(footer), 1)
		}
		s.detailsMax = max(len(content)-budget, 0)
		s.detailsTop = min(max(s.detailsTop, 0), s.detailsMax)
		if s.detailsMax > 0 {
			ending := append([]wallCardLine(nil), lines[len(lines)-choices:]...)
			lines = append([]wallCardLine{}, content[s.detailsTop:min(s.detailsTop+budget, len(content))]...)
			lines = append(lines, ending...)
		}
	}
	lines = append(lines, teamFooter(a.pal, inner, hints...)...)
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
			text = pal.dim(teamsPad(r.label, valueX)) + pal.ink(value)
		default:
			words := value
			if r.from != "" && r.from != "from Settings" {
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
			rs := pal.dim(reset)
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
	closeWord := "Disband team" + a.linearMark("…", "...")
	var hits []wallHit
	row := ""
	if !t.Root {
		b, hit, _ := a.teamSheetButton(closeWord, "", tsCloseTeam, 0, false)
		row, hits = b, append(hits, hit)
		b, hit, _ = a.teamSheetButton("Delete team…", "", tsDeleteTeam, ansi.StringWidth(row)+1, false)
		row += " " + b
		hits = append(hits, hit)
	}
	doneX := inner + 2 - a.teamSheetButtonW("Done", "")
	done, doneHit, _ := a.teamSheetButton("Done", "", tsDone, doneX, false)
	row = teamsPad(row, doneX) + done
	lines = append(lines, wallCardLine{s: row, hits: append(hits, doneHit), bleed: true})
	return lines
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
	var lines []wallCardLine
	for _, word := range wrap("Disband these teams: "+a.teamsAffectedNames(t.ID)+". Memberships end, current work finishes, and every conversation survives. Team history is kept read-only.", inner) {
		lines = append(lines, wallCardLine{s: pal.ink(word)})
	}
	lines = append(lines, wallCardLine{})
	for _, choice := range []struct {
		word string
		code int
	}{{"Cancel", tsCancel}, {"Disband", tsCloseNow}} {
		button, hit, _ := a.teamSheetButton(choice.word, "", choice.code, 0, choice.code == tsCloseNow)
		lines = append(lines, wallCardLine{s: button, hits: []wallHit{hit}, bleed: true})
	}
	return lines
}

// teamSheetDeleteLines is the delete card: what goes, what stays, and the two
// answers, the one that cannot be undone in the failure red.
func (a *app) teamSheetDeleteLines(t team, inner int) []wallCardLine {
	pal := a.pal
	var lines []wallCardLine
	say := "Permanently delete these teams: " + a.teamsAffectedNames(t.ID) + "? Active teams are disbanded first. Their records, interactions and decisions are deleted. Conversations and their current work survive. This cannot be undone."
	for _, l := range wrap(say, inner) {
		lines = append(lines, wallCardLine{s: pal.ink(l)})
	}
	lines = append(lines, wallCardLine{})
	keepW := a.teamSheetButtonW("Keep", "")
	delW := a.teamSheetButtonW("Delete", "")
	x := inner + 2 - keepW - 1 - delW
	keep, kh, _ := a.teamSheetButton("Keep", "", tsKeep, x, false)
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
		stops = append(stops, tsQuestions, tsWake, tsCap, tsDepth, tsShare)
		if ok && !t.Root {
			stops = append(stops, tsCloseTeam, tsDeleteTeam)
		}
		return append(stops, tsDone)
	case teamSheetClose:
		return []int{tsCancel, tsCloseNow}
	case teamSheetDelete:
		return []int{tsKeep, tsDelete}
	}
	return nil
}

// teamSheetKey is a key while the card is up: it has the keyboard, as every
// card does.
func (a *app) teamSheetKey(msg tea.KeyPressMsg) tea.Cmd {
	if a.tsheet.mode != teamSheetSettings {
		switch msg.String() {
		case "pgup":
			a.tsheet.detailsTop = max(a.tsheet.detailsTop-5, 0)
			a.touch()
			return nil
		case "pgdown":
			a.tsheet.detailsTop = min(a.tsheet.detailsTop+5, a.tsheet.detailsMax)
			a.touch()
			return nil
		}
	}
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
	expected := append([]string(nil), s.affected...)
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
	case tsMoveYes:
		s.cursor = tsInside
		return a.teamMoveConfirm()
	case tsMoveNo:
		a.teamMoveCancel()
	case tsMoveUndo:
		s.cursor = tsInside
		return a.teamMoveUndo()
	case tsDeleteTeam:
		a.teamSheetShut()
		return a.teamSheetOpen(id, teamSheetDelete)
	case tsCloseTeam:
		a.teamSheetShut()
		return a.teamsCloseAsk(id)
	case tsDone:
		a.teamSheetShut()
	case tsWrapUp:
		a.tsheet = teamSheet{}
		return a.teamsWrapUp(id)
	case tsCloseNow:
		if a.teamSheetCard(a.width, a.height).w == 0 {
			s.err = "Resize the terminal to read the confirmation"
			a.touch()
			return nil
		}
		a.tsheet = teamSheet{}
		return a.teamsCloseNow(id, "", expected)
	case tsCancel, tsKeep:
		a.tsheet = teamSheet{}
		a.touch()
	case tsDelete:
		if a.teamSheetCard(a.width, a.height).w == 0 {
			s.err = "Resize the terminal to read the confirmation"
			a.touch()
			return nil
		}
		a.tsheet = teamSheet{}
		return a.teamsDelete(id, expected)
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
		return "PgUp/PgDn details · ↑↓ choose · enter · esc cancel"
	case teamSheetDelete:
		return "PgUp/PgDn details · enter · esc keep it"
	}
	if s.editing >= 0 {
		return "enter keep it as this team's own · esc cancel"
	}
	switch s.cursor {
	case tsName:
		return "type to rename · ↓ next · esc done"
	case tsColour:
		return "←→ colour · esc done"
	case tsQuestions, tsWake, tsCap, tsDepth, tsShare:
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

// Every affected name is shown before a recursive lifecycle action is accepted.
func (a *app) teamsAffectedNames(id string) string {
	var names []string
	if t, ok := a.teamByID(id); ok {
		names = append(names, t.Name)
	}
	for _, t := range a.teamTree().Descendants(id) {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func (a *app) teamsAffectedIDs(id string) []string {
	ids := []string{id}
	for _, t := range a.teamTree().Descendants(id) {
		ids = append(ids, t.ID)
	}
	return ids
}
