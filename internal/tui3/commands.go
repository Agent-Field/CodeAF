package tui3

import (
	"fmt"
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
//
// THE OTHER WORDS FOR A COMMAND ARE IN THE TABLE TOO, on the row they belong to
// ([command.alias]). People arrive here from other tools with a vocabulary
// already in their fingers — /clear, /exit, /q, /? — and a surface that answers
// "unknown command" to those is a surface asking somebody to unlearn something
// before it will talk to them. So the words are accepted, and the dispatch
// resolves every one of them through this table ([canonicalCommand]) rather than
// growing a second list of synonyms next to the switch that runs them.

// command is one slash command: what to type, what it takes, and one line.
type command struct {
	name string
	args string
	desc string
	// alias are the other words that reach this same row. They are NOT rows of
	// their own: the list shows the canonical name, filtering an alias surfaces
	// that canonical row, and running one runs that canonical command — with the
	// aliases printed dimly beside it, so somebody who typed the word they knew
	// can see both what ran and what this surface calls it.
	alias []string
}

// commands is the list, in the order a person meets them.
var commands = []command{
	// The picker's OTHER door is named here rather than on a line of its own,
	// because it is the same door: press the model's name in the status line
	// (render.go's [app.identityParts]). It is worth saying because a surface
	// with the mouse turned off (config's ui.mouse) does not have it, and this
	// row is then the only one there is.
	{name: "model", desc: "pick a model · or press its name in the status line"},
	{name: "model", args: "<slug>", desc: "switch the model"},
	{name: "image", args: "<path>", desc: "attach a picture · tab completes the path"},
	// /set and /config were already answered by the dispatch before aliases
	// existed, and /connections and /sessions with them. They are written here
	// now because the table is the one place: a word the surface accepts and the
	// table does not mention is exactly the drift this file exists to prevent.
	{name: "settings", desc: "open the settings panel · ctrl+,", alias: []string{"set", "config"}},
	// It sits under /settings because it is the other half of the same errand:
	// one is what this surface may do, the other is what it may reach.
	{name: "connect", desc: "your connected accounts · connect another", alias: []string{"connections"}},
	// THE VOCABULARY OF THE FRESH START IS BORROWED AND NOT INVENTED. /clear is
	// what a terminal person's fingers type, /reset is what a chat person's do,
	// and both of them mean the thing this surface calls /new — so all three land
	// on it rather than on "unknown command: /clear".
	{name: "new", desc: "close this session and start a fresh one", alias: []string{"clear", "clean", "reset"}},
	{name: "resume", desc: "open an earlier conversation", alias: []string{"sessions"}},
	{name: "compact", desc: "summarize the conversation now"},
	// It sits AFTER /compact and before /help because those two are the pair a
	// person reads together when a conversation has gone wrong: compacting is
	// what you do when the turn was right and too long, rewinding is what you do
	// when the turn was wrong. Putting it last, beside /quit, would file "take
	// back a message" under leaving.
	{name: "rewind", desc: "take back a message · esc esc", alias: []string{"undo", "back"}},
	{name: "help", desc: "this list", alias: []string{"?"}},
	{name: "quit", desc: "leave", alias: []string{"exit", "q"}},
}

// checkCommands is THE TABLE CHECK, run at init over [commands] and by the tests
// over tables of their own.
//
// AN ALIAS MAY NEVER SHADOW A CANONICAL NAME OR ANOTHER ALIAS. A word that meant
// two things would resolve to whichever row the loop reached first, which makes
// the answer to "what does /clear do" a fact about the table's ORDER — and this
// table is ordered for reading, so it would be an accident waiting on the next
// person who moves a row. A canonical name may repeat, because /model and
// /model <slug> are two forms of one command rather than two commands.
func checkCommands(list []command) error {
	names := make(map[string]bool, len(list))
	for _, c := range list {
		names[c.name] = true
	}
	owner := make(map[string]string, len(list))
	for _, c := range list {
		for _, word := range c.alias {
			switch {
			case word == "":
				return fmt.Errorf("/%s has an empty alias", c.name)
			case names[word]:
				return fmt.Errorf("/%s is an alias of /%s and a command in its own right", word, c.name)
			case owner[word] != "":
				return fmt.Errorf("/%s is an alias of both /%s and /%s", word, owner[word], c.name)
			}
			owner[word] = c.name
		}
	}
	return nil
}

// It fails at startup and not at the first keystroke: a broken table is a
// programming mistake in this file, and the loudest place to say so is before
// anything has been drawn.
func init() {
	if err := checkCommands(commands); err != nil {
		panic("tui3: the command table is broken: " + err.Error())
	}
}

// aliasNote is the dim tail that names the other words for this command, or ""
// when there are none: "also /clear /clean /reset".
func (c command) aliasNote() string {
	if len(c.alias) == 0 {
		return ""
	}
	words := make([]string, 0, len(c.alias))
	for _, word := range c.alias {
		words = append(words, "/"+word)
	}
	return "also " + strings.Join(words, " ")
}

// note is the whole right-hand side of a row: what the command does, and then
// what else it answers to. It is ONE function because the list and /help both
// draw it, and because the height that reserves the rows and the fill that draws
// them have to be counting the same string (see [menu.height]).
func (c command) note() string {
	if tail := c.aliasNote(); tail != "" {
		return c.desc + " · " + tail
	}
	return c.desc
}

// menuNote is [command.note] cut to what is left of a row after the command's
// own name — and it is THE ONE PLACE ON THIS SURFACE WHERE THE TAIL GIVES WAY
// FIRST rather than the label. Everywhere else a truncated label is still
// recognizable and the tail carries the numbers being compared ([overlayRow]),
// but a command name is a thing a person has to type back EXACTLY: "/set…" is
// not a command, while a sentence about it that stops early still reads. The
// alias tail is what pushed this over — "· also /clear /clean /reset" is longer
// than most of these rows' widths to spare — and without the cut a narrow frame
// drew a row wider than the frame.
//
// At [tierPhone] nothing is cut here: the tail has a line of its own there and
// fits itself to it (see [overlayLines]).
func (c command) menuNote(width int) string {
	note := c.note()
	if phoneList(width) {
		return note
	}
	return fit(note, width-2-len(c.typed())-1)
}

// canonicalCommand is the name a typed word RUNS as: an alias resolves to the
// row that owns it, and everything else — including a word nobody defined — is
// returned folded to lower case for the dispatch to answer as it always has.
//
// The aliases are searched and the names are not, which is only safe because
// [checkCommands] has already proved no alias can shadow a name.
func canonicalCommand(word string) string {
	word = strings.ToLower(word)
	for _, c := range commands {
		for _, other := range c.alias {
			if other == word {
				return c.name
			}
		}
	}
	return word
}

// aliasRung is the wall between a name match and an alias match, far above any
// offset a name a dozen characters long can reach. A row found by its own name
// always outranks a row found by a word it merely also answers to, so typing
// "res" puts /resume above the /new that carries "reset".
const aliasRung = 1_000

// matchAt is where needle was found in this command's words and whether it was
// found at all — the name first, then the aliases a rung below it. Lower is
// better, the same way the model picker's tiers are (palette.go's [tokenScore]).
func (c command) matchAt(needle string) (int, bool) {
	if needle == "" {
		return 0, true
	}
	if at := strings.Index(c.name, needle); at >= 0 {
		return at, true
	}
	best, found := 0, false
	for _, word := range c.alias {
		at := strings.Index(word, needle)
		if at < 0 || (found && at >= best) {
			continue
		}
		best, found = at, true
	}
	if !found {
		return 0, false
	}
	return aliasRung + best, true
}

// typed is the command as it is written: "/model <slug>".
func (c command) typed() string {
	if c.args == "" {
		return "/" + c.name
	}
	return "/" + c.name + " " + c.args
}

// menuRows is how many command rows fit at once. The table has grown past it,
// so it is now a real ceiling and the list scrolls under the cursor — which is
// the trade taken on purpose: eight rows of commands over the conversation is
// already half a short terminal, and a list that grew with the table would take
// the screen every time a command was added. The ALIASES cost nothing here,
// because an alias is a word on a row and never a row of its own.
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
//
// AN ALIAS MATCHES AND THE CANONICAL ROW IS WHAT APPEARS. Typing "clea" narrows
// the list to /new, not to a /clear row that does not exist: there is one row
// per command, and the alias is a way of reaching it (see [command.matchAt]).
func (m *menu) rank(needle string) {
	if cap(m.score) < len(commands) {
		m.score = make([]int, len(commands))
	}
	m.hits = m.hits[:0]
	for i, c := range commands {
		at, ok := c.matchAt(needle)
		if !ok {
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
func (m *menu) height(width int) int {
	if !m.open {
		return 0
	}
	// The ceiling is in LINES, so at [tierPhone] the list holds four commands
	// with what they do written under them instead of eight rows that all say
	// "/settings   open the settings pa…" (palette.go).
	return overlayWindow(width, m.top, len(m.hits), menuRows, func(at int) string {
		return commands[m.hits[at]].note()
	})
}

func (m *menu) rows(width, n int, pal palette, hover int) []string {
	if n <= 0 || len(m.hits) == 0 {
		return nil
	}
	m.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := m.top; at < len(m.hits) && fill.room(); at++ {
		c := commands[m.hits[at]]
		if !fill.add(at, c.typed(), c.menuNote(width), at == m.cursor, false) {
			break
		}
	}
	lines, _ := fill.done()
	return lines
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
	lines := make([]string, 0, len(commands)+6)
	// The product names itself once, at the top of the one place it explains
	// itself. Everywhere else on this surface it is simply the thing you are
	// already in (styles.go's [product]).
	lines = append(lines, product, "")
	for _, c := range commands {
		// The aliases are printed here as they are on the row, and nothing is cut:
		// /help is prose in the transcript, which wraps, rather than a list drawn
		// into a fixed block (see [command.menuNote]).
		lines = append(lines, c.typed()+strings.Repeat(" ", width-len(c.typed())+2)+c.note())
	}
	lines = append(lines,
		"@path          complete a file · a picture attaches",
		"alt+enter      open a line · enter sends",
		"ctrl+o         expand this turn's tool calls · click one to open it",
		"ctrl+b         copy mode · ↑↓ move · v marks · y yanks · esc leaves",
		"ctrl+q         ask this after the current turn instead of into it",
		"ctrl+e         open the model's thinking, streaming or finished",
		"ctrl+t         the task roster · ↑↓ move · →← fold · enter opens · esc leaves",
		"ctrl+l         back to the latest · the chip above the box says so too",
		"→ ←            over an empty box: into a running task, and back out",
		"← ←            home · the conversation, at the live edge",
		"ctrl+w         delete the word behind the caret · ctrl+u the line",
		"ctrl+,         settings",
	)
	if file != "" {
		lines = append(lines, "session · "+file)
	}
	return strings.Join(lines, "\n")
}
