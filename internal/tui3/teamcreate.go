package tui3

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Creation selects conversations without opening them. Its draft belongs to
// the dialog, so cancel cannot change the current conversation or its composer.
type teamCreateSheet struct {
	on, loading        bool
	generation         int
	parent             string
	name, filter       editor
	field, cursor, top int
	choices            []teamHueSpec
	choice             int
	rows, shown        []chatTab
	selected           map[string]chatTab
	message            string
	hits               []wallHit
	rect               wallRect
}

func (a *app) teamCreateOpen(parent string) tea.Cmd {
	if parent != "" {
		if ok, why := a.teamsCanNest(parent); !ok {
			a.tp.msg = why
			a.touch()
			return nil
		}
	}
	a.teamsEnsure()
	gen := a.tcreate.generation + 1
	a.tcreate = teamCreateSheet{on: true, loading: true, generation: gen, parent: parent, selected: map[string]chatTab{}, choices: teamHueChoices(a.teamHues(""), teamReservedHues(a.pal), wallSwatchCount)}
	a.touch()
	return a.conversationCatalogRead(func(rows []conversationCandidate, known bool) tea.Cmd {
		if !a.tcreate.on || a.tcreate.generation != gen {
			return nil
		}
		a.tcreate.loading = false
		if !known {
			a.tcreate.message = "Conversations are still loading; close and try again"
		}
		for _, row := range rows {
			a.tcreate.rows = append(a.tcreate.rows, row.tab)
		}
		a.touch()
		return nil
	})
}

func (a *app) teamCreateShut() {
	a.tcreate = teamCreateSheet{generation: a.tcreate.generation + 1}
	a.touch()
}

func (a *app) teamCreateRows() []chatTab {
	var rows []chatTab
	filter := strings.ToLower(a.tcreate.filter.String())
	for _, row := range a.tcreate.rows {
		if !a.conversationDeleted(row.file) && strings.Contains(strings.ToLower(row.word+" "+row.where), filter) {
			rows = append(rows, row)
		}
	}
	return rows
}

func (a *app) teamCreateToggle(i int) {
	if i < 0 || i >= len(a.tcreate.shown) {
		return
	}
	tab := a.tcreate.shown[i]
	if a.conversationDeleted(tab.file) {
		return
	}
	if _, ok := a.tcreate.selected[tab.key]; ok {
		delete(a.tcreate.selected, tab.key)
	} else {
		a.tcreate.selected[tab.key] = tab
	}
	a.tcreate.cursor, a.tcreate.field = i, 2
	a.touch()
}

func (a *app) teamCreateSave() tea.Cmd {
	s := &a.tcreate
	if !s.on || s.rect.w() == 0 {
		return nil
	}
	name := strings.TrimSpace(s.name.String())
	if name == "" {
		s.message = "Give the team a name"
		s.field = 0
		a.touch()
		return nil
	}
	if s.parent != "" {
		if ok, why := a.teamsCanNest(s.parent); !ok {
			s.message = why
			a.touch()
			return nil
		}
	}
	// Creating a team must not silently replace another team's membership.
	if teamNamed(a.wall.teams, name) >= 0 {
		s.message = "A team already uses this name"
		a.touch()
		return nil
	}
	var tabs []chatTab
	for _, tab := range s.rows {
		if _, ok := s.selected[tab.key]; ok {
			if a.conversationDeleted(tab.file) {
				s.message = "A selected conversation was deleted; choose again"
				delete(s.selected, tab.key)
				a.touch()
				return nil
			}
			tabs = append(tabs, tab)
		}
	}
	hue := nextTeamHue(a.teamHues(""), teamReservedHues(a.pal))
	if s.choice >= 0 && s.choice < len(s.choices) {
		hue = s.choices[s.choice]
	}
	id, err := a.teamCreateIn(name, tabs, hue, s.parent)
	if err != nil {
		s.message = err.Error()
		a.touch()
		return nil
	}
	a.teamCreateShut()
	return a.teamCreatedShow(id, err)
}

// Both creation routes land on the new overview; no selected chat is resumed.
func (a *app) teamCreatedShow(id string, err error) tea.Cmd {
	if made, ok := a.teamByID(id); ok {
		a.wall.madeSaid = a.teamWriteWatch(err)
		a.wall.made, a.wall.madeN, a.wall.madeAt = made.Name, len(made.Members), a.now()
	}
	a.closeWall()
	return tea.Batch(a.showPage(pageTeams), a.teamsSelect(id))
}

func (a *app) teamCreateKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &a.tcreate
	a.touch()
	switch msg.String() {
	case "esc":
		a.teamCreateShut()
	case "tab":
		s.field = (s.field + 1) % 4
	case "shift+tab":
		s.field = (s.field + 3) % 4
	case "enter":
		return a.teamCreateSave()
	case "up":
		s.field = 2
		s.cursor = max(s.cursor-1, 0)
	case "down":
		s.field = 2
		s.cursor = min(s.cursor+1, max(len(a.teamCreateRows())-1, 0))
	case "left", "right":
		if s.field == 1 && len(s.choices) > 0 {
			by := 1
			if msg.String() == "left" {
				by = len(s.choices) - 1
			}
			s.choice = (s.choice + by) % len(s.choices)
		}
	case "space":
		if s.field == 2 {
			a.teamCreateToggle(s.cursor)
		} else if s.field == 0 {
			s.name.insert(" ")
		}
	case "backspace":
		if s.field == 0 {
			s.name.deleteBackward()
		} else if s.field == 2 {
			s.filter.deleteBackward()
			s.cursor, s.top = 0, 0
		}
	default:
		if text := msg.Key().Text; text != "" {
			if s.field == 0 {
				s.name.insert(text)
			} else if s.field == 2 {
				s.filter.insert(text)
				s.cursor, s.top = 0, 0
			}
		}
	}
	return nil
}

const (
	teamCreateName = iota
	teamCreateFilter
	teamCreateCancel
	teamCreateSubmit
)

func (a *app) teamCreateMouse(msg tea.Msg, m tea.Mouse) tea.Cmd {
	s := &a.tcreate
	switch msg.(type) {
	case tea.MouseWheelMsg:
		s.field = 2
		s.cursor = min(max(s.cursor+placeWheelDelta(m.Button), 0), max(len(s.shown)-1, 0))
		a.touch()
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		for _, h := range s.hits {
			if m.X >= h.x0 && m.X < h.x1 && m.Y >= h.y0 && m.Y < h.y1 {
				switch h.kind {
				case wallHitSelect:
					a.teamCreateToggle(h.arg)
				case wallHitSwatch:
					s.field, s.choice = 1, h.arg
					a.touch()
				case wallHitAction:
					switch h.arg {
					case teamCreateName:
						s.field = 0
						a.touch()
					case teamCreateFilter:
						s.field = 2
						a.touch()
					case teamCreateCancel:
						a.teamCreateShut()
					case teamCreateSubmit:
						return a.teamCreateSave()
					}
				}
				return nil
			}
		}
		if s.rect.w() > 0 && !s.rect.holds(m.X, m.Y) {
			a.teamCreateShut()
		}
	}
	return nil
}

func (a *app) teamCreateOver(frame string) string {
	if !a.tcreate.on {
		return frame
	}
	s := &a.tcreate
	s.hits = nil
	s.rect = wallRect{}
	width, height := a.size()
	w := min(width-2, 80)
	inner := w - 4
	if inner < 28 || height < 14 {
		return frame
	}
	title := "New team"
	if s.parent != "" {
		title = "New team in " + a.teamNameOf(s.parent)
	}
	text := func(value string, field int) string {
		if s.field == field {
			value += a.linearMark("▏", "|")
		}
		return fit(value, inner-8)
	}
	action := func(arg int) []wallHit { return []wallHit{{x1: inner, y1: 1, kind: wallHitAction, arg: arg}} }
	lines := []wallCardLine{{s: a.pal.dim("Name    ") + text(s.name.String(), 0), hits: action(teamCreateName)}}
	sw, _, hits := wallSwatches(a.pal, wallView{}, s.choices, s.choice, 8)
	lines = append(lines, wallCardLine{s: a.pal.dim("Colour  ") + sw, hits: hits}, wallCardLine{s: a.pal.dim("Members · optional")}, wallCardLine{s: a.pal.dim("Filter  ") + text(s.filter.String(), 2), hits: action(teamCreateFilter)})
	nameW := max((inner-4)*2/3, 1)
	lines = append(lines, wallCardLine{s: a.pal.dim("    " + teamsPad("Name", nameW) + "  " + teamsPad("Project", max(inner-4-nameW-2, 1)))})
	rows := a.teamCreateRows()
	// Preserve the highlighted conversation when a deletion changes the list.
	if s.cursor >= 0 && s.cursor < len(s.shown) && s.filter.String() == "" {
		key := s.shown[s.cursor].key
		for i, row := range rows {
			if row.key == key {
				s.cursor = i
				break
			}
		}
	}
	s.shown = append([]chatTab(nil), rows...)
	s.cursor = min(max(s.cursor, 0), max(len(rows)-1, 0))
	messageRows := 0
	if s.message != "" {
		messageRows = 1
	}
	room := max(min(height-13-messageRows, 12), 1)
	s.top = min(s.top, max(len(rows)-room, 0))
	if s.cursor < s.top {
		s.top = s.cursor
	}
	if s.cursor >= s.top+room {
		s.top = s.cursor - room + 1
	}
	k := wallKeysFor(a.pal.ascii)
	for i := s.top; i < min(s.top+room, len(rows)); i++ {
		mark := k.boxOff
		if _, ok := s.selected[rows[i].key]; ok {
			mark = k.boxOn
		}
		line := mark + " " + conversationColumns(rows[i], inner-ansi.StringWidth(mark)-1)
		lines = append(lines, wallCardLine{s: wallPopRowPaint(a.pal, line, inner, s.field == 2 && s.cursor == i), hits: []wallHit{{x1: inner, y1: 1, kind: wallHitSelect, arg: i}}})
	}
	if len(rows) == 0 {
		word := "Conversations you add will appear here"
		if s.loading {
			word = "Loading conversations"
		} else if s.filter.String() != "" {
			word = "No matching conversations"
		}
		lines = append(lines, wallCardLine{s: a.pal.dim(fit(word, inner))})
	}
	selection := fmt.Sprintf("%d selected", len(s.selected))
	if len(rows) > room {
		selection += fmt.Sprintf(" · %d to %d of %d", s.top+1, min(s.top+room, len(rows)), len(rows))
	}
	lines = append(lines, wallCardLine{s: a.pal.dim(fit(selection, inner))})
	if s.message != "" {
		lines = append(lines, wallCardLine{s: a.pal.warn(fit(s.message, inner))})
	}
	cancel, create := "Cancel esc", "Create enter"
	at := max(inner-len(cancel)-2-len(create), 0)
	lines = append(lines, wallCardLine{s: strings.Repeat(" ", at) + a.pal.muted(cancel) + "  " + wallPopRowPaint(a.pal, create, len(create), s.field == 3), hits: []wallHit{{x0: at, x1: at + len(cancel), y1: 1, kind: wallHitAction, arg: teamCreateCancel}, {x0: at + len(cancel) + 2, x1: inner, y1: 1, kind: wallHitAction, arg: teamCreateSubmit}}}, wallCardLine{s: a.pal.dim(fit("up/down move · space select · tab next field", inner))})
	h := len(lines) + 2
	x, y := (width-w)/2, max((height-h)/3, 1)
	card := wallCardBuild(a.pal, title, lines, x, y, w, 1, 0)
	s.hits = card.hits
	s.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	return a.confirmCardOver(frame, card)
}
