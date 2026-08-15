package tui3

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// THE COMMAND LIST: type "/" and what you can type appears.
//
// It is the palette gesture again (palette.go) with two differences, and both
// of them come from the same fact — the slash is typed into the DRAFT, not into
// a filter box of its own:
//
//   - it is not modal. The person keeps typing into the box they were already
//     typing in, the list narrows under it, and only ↑/↓/enter/esc are taken.
//   - it opens and closes by itself. A leading "/" opens it; a space closes it,
//     because a line with an argument in it is a line being written rather than
//     a command being chosen.
//
// The table below is the ONE place a command is written down. /help renders the
// same rows (see [helpText]), so a command that exists in one and not the other
// is not possible.

// command is one slash command: what to type, what it takes, and one line.
type command struct {
	name string
	args string
	desc string
}

// commands is the list, in the order a person meets them.
var commands = []command{
	{name: "model", desc: "pick a model from the list"},
	{name: "model", args: "<slug>", desc: "switch the model"},
	{name: "new", desc: "close this session and start a fresh one"},
	{name: "compact", desc: "summarize the conversation now"},
	{name: "help", desc: "this list"},
	{name: "quit", desc: "leave"},
}

// typed is the command as it is written: "/model <slug>".
func (c command) typed() string {
	if c.args == "" {
		return "/" + c.name
	}
	return "/" + c.name + " " + c.args
}

// menuRows is how many command rows fit at once. The list is short enough that
// this is a ceiling nobody reaches; it exists so the overlay has one.
const menuRows = 8

// menu is the command list's whole state. The zero value is closed.
type menu struct {
	open bool
	// hits are indexes into commands, in rank order.
	hits   []int
	score  []int
	cursor int
	top    int
}

// sync opens, narrows or closes the list from what is in the draft. It is
// called after every edit, and it is the ONLY thing that opens this overlay:
// there is no key for it, because the key is "/".
func (m *menu) sync(line string) {
	if !strings.HasPrefix(line, "/") || strings.ContainsAny(line, " \n") {
		m.close()
		return
	}
	needle := strings.ToLower(line[1:])
	was := m.open
	m.open = true
	m.rank(needle)
	if !was {
		m.cursor, m.top = 0, 0
	}
}

func (m *menu) close() { *m = menu{} }

// rank filters by SUBSTRING over the command's name, prefix first — the same
// rule the model picker uses, for the same reason: the score is the offset the
// match was found at, so "od" finds /model and "m" puts it first.
func (m *menu) rank(needle string) {
	if cap(m.score) < len(commands) {
		m.score = make([]int, len(commands))
	}
	m.hits = m.hits[:0]
	for i, c := range commands {
		if needle == "" {
			m.hits = append(m.hits, i)
			continue
		}
		at := strings.Index(c.name, needle)
		if at < 0 {
			continue
		}
		m.score[i] = at
		m.hits = append(m.hits, i)
	}
	if needle != "" {
		sort.SliceStable(m.hits, func(a, b int) bool { return m.score[m.hits[a]] < m.score[m.hits[b]] })
	}
	m.cursor = moveCursor(m.cursor, 0, len(m.hits))
	m.follow(menuRows)
}

func (m *menu) move(delta int) {
	m.cursor = moveCursor(m.cursor, delta, len(m.hits))
	m.follow(menuRows)
}

func (m *menu) follow(height int) { m.top = listTop(m.cursor, m.top, len(m.hits), height) }

// choice is the command under the cursor, and false when the filter matched
// nothing — enter on an empty list is the typed line's, not the list's.
func (m *menu) choice() (command, bool) {
	if !m.open || m.cursor < 0 || m.cursor >= len(m.hits) {
		return command{}, false
	}
	return commands[m.hits[m.cursor]], true
}

// height is how many rows the list wants. A filter that matches nothing wants
// NONE: the draft under it is a perfectly good "/nonsense" that enter will
// answer, and an overlay saying "no match" over a line that is about to get a
// better answer is two answers to one question.
func (m *menu) height() int {
	switch {
	case !m.open:
		return 0
	case len(m.hits) < menuRows:
		return len(m.hits)
	default:
		return menuRows
	}
}

func (m *menu) rows(width, n int, pal palette) []string {
	if n <= 0 || len(m.hits) == 0 {
		return nil
	}
	m.follow(n)
	out := make([]string, 0, n)
	for at := m.top; at < len(m.hits) && len(out) < n; at++ {
		c := commands[m.hits[at]]
		out = append(out, overlayRow(c.typed(), c.desc, at == m.cursor, false, width, pal))
	}
	return out
}

// runMenu is enter while the command list is up: the row under the cursor wins.
//
// A row that TAKES something is written into the draft instead of run — "/model
// <slug>" with no slug is not a command anybody meant, and putting "/model "
// in the box with the caret after it is the surface finishing the person's
// sentence rather than guessing at it.
func (a *app) runMenu() tea.Cmd {
	chosen, ok := a.menu.choice()
	if !ok {
		return nil
	}
	a.menu.close()
	if chosen.args != "" {
		a.input.setText("/" + chosen.name + " ")
		return a.edited()
	}
	a.input.reset()
	// It goes into the recall list exactly as if it had been typed out and
	// entered, because from the person's side it was: the list is a shortcut
	// for typing, not a second door with different rules (see [app.enter]).
	a.remember("/" + chosen.name)
	a.dropDraft()
	return a.slash("/" + chosen.name)
}

// helpText renders the same table the list draws, plus the two keys that have
// no slash and the session file. One source, two renderings.
func helpText(file string) string {
	width := 0
	for _, c := range commands {
		if n := len(c.typed()); n > width {
			width = n
		}
	}
	lines := make([]string, 0, len(commands)+4)
	for _, c := range commands {
		lines = append(lines, c.typed()+strings.Repeat(" ", width-len(c.typed())+2)+c.desc)
	}
	lines = append(lines,
		"@path          complete a file from this directory",
		"alt+enter      open a line · enter sends",
		"ctrl+o         expand this turn's tool calls · click one to open it",
		"ctrl+q         ask this after the current turn instead of into it",
		"ctrl+e         open the model's thinking, when it showed any",
	)
	if file != "" {
		lines = append(lines, "session · "+file)
	}
	return strings.Join(lines, "\n")
}
