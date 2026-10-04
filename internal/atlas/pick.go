package atlas

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// THE PICKER (pick.go). Bare `codeaf atlas` shows this instead of guessing a
// map: one row per registered map, its name and its one-line description, in
// the registry's order. Enter opens the row the cursor is on; esc and q leave
// without opening anything, and the shell gets its terminal back whole — the
// same road out the map itself takes, on the same alternate-screen bargain.
//
// The picker draws with the same restraint the map does (docs/DESIGN-LANGUAGE.md):
// dim telemetry, no borders, one cursor mark and nothing else.

// pickCursor is the marker on the row the cursor sits on.
const pickCursor = "›"

// PickRow is one picker row, drawn the same by `codeaf atlas` and `/atlas`:
// the cursor mark, the map's name in bold (accent on the cursor row) padded
// to the longest name, then its description dim — the codeaf list shape of a
// name and a dim tail, so the names read as a column of their own.
func PickRow(mp *Map, selected bool) string {
	nameW := 0
	for _, m := range Maps {
		nameW = max(nameW, cw(m.Name))
	}
	mark := " "
	name := lipgloss.NewStyle().Bold(true)
	if selected {
		mark = pickCursor
		name = name.Foreground(lipgloss.Color(colAccent))
	}
	gap := strings.Repeat(" ", nameW-cw(mp.Name)+3)
	return mark + " " + name.Render(mp.Name) + gap + paint(mp.Description, colMuted, false)
}

// Picker is the map list. Like [Model], it is a pointer used as a tea.Model,
// so Update hands the same model back and the tests can drive one directly.
type Picker struct {
	cursor int
	W, H   int
	chosen *Map
	leave  bool
}

// NewPicker is the picker over the registry as it stands.
func NewPicker() *Picker { return &Picker{} }

// Chosen answers the map enter picked, or nil when the person left instead.
func (p *Picker) Chosen() *Map { return p.chosen }

// Left reports whether the picker was dismissed with esc or q — Chosen being
// nil and Left being false together mean the registry is empty, which the
// registry test refuses to let happen.
func (p *Picker) Left() bool { return p.leave }

// Init implements tea.Model.
func (p *Picker) Init() tea.Cmd { return nil }

// View implements tea.Model.
func (p *Picker) View() tea.View {
	return tea.View{Content: p.frame(), AltScreen: true}
}

// Update implements tea.Model. Opening and leaving are the two keys that end
// the program — Bubble Tea stops when a [tea.Quit] comes back, so enter and
// the roads out return it and everything else only moves the cursor.
func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.W, p.H = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch k := msg.String(); {
		case k == "up" || k == "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case k == "down" || k == "j":
			if p.cursor < len(Maps)-1 {
				p.cursor++
			}
		case k == "home" || k == "g":
			p.cursor = 0
		case k == "end" || k == "G":
			p.cursor = max(0, len(Maps)-1)
		case k == "enter" && p.cursor < len(Maps):
			p.chosen = Maps[p.cursor]
			return p, tea.Quit
		case k == "esc" || k == "q" || k == "ctrl+c":
			p.leave = true
			return p, tea.Quit
		}
	}
	return p, nil
}

// frame draws the list: a heading, one row per map, and the hint line that
// says which keys leave.
func (p *Picker) frame() string {
	var b strings.Builder
	b.WriteString("atlas — choose a map\n")
	b.WriteString("\n")
	for at, mp := range Maps {
		b.WriteString(PickRow(mp, at == p.cursor) + "\n")
	}
	b.WriteString("\nenter open · esc/q leave\n")
	return b.String()
}

// Pick raises the picker in this terminal and answers the map enter chose, or
// nil when the person left. It is the bare `codeaf atlas` road.
func Pick() (*Map, error) {
	m, err := tea.NewProgram(NewPicker()).Run()
	if err != nil {
		return nil, fmt.Errorf("atlas: %w", err)
	}
	if picker, ok := m.(*Picker); ok {
		return picker.Chosen(), nil
	}
	return nil, nil
}
