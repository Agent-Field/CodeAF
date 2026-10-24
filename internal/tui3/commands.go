package tui3

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"runtime"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/slashcat"
	"github.com/Agent-Field/codeaf/internal/trace"
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
// The table (internal/slashcat) is the ONE place a command is written down. /help renders the
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
	// door names the alternate send road this command owns. THE SEND-DOOR SET
	// LIVES ON THE COMMAND ROWS so dispatch, aliases, chips and tags cannot grow
	// separate lists that disagree about which words act inside a sentence.
	door sendDoor
	// alias are the other words that reach this same row. They are NOT rows of
	// their own: the list shows the canonical name, filtering an alias surfaces
	// that canonical row, and running one runs that canonical command — with the
	// aliases printed dimly beside it, so somebody who typed the word they knew
	// can see both what ran and what this surface calls it.
	alias []string
}

// commands is the shared catalogue as THIS surface draws it. The rows are not
// written here: internal/slashcat holds the one table, because the desktop app is
// served the same rows over the wire and a table that lived in this package could
// not be imported without dragging a terminal into it. This file adapts them —
// the chord of a place appended where a row names one, the send door turned into
// this package's own type — and nothing else. Declaration order is retained by
// /help; the interactive menu sorts it by name regardless of the placement notes
// in internal/slashcat, which are about that menu's eight-row viewport.
var commands = adaptCommands(slashcat.Rows())

// placePages is the page each [slashcat.Row.Place] word names. The init check
// below refuses a row whose word is missing, so a place cannot silently lose its
// chord.
var placePages = map[string]page{"teams": pageTeams, "spend": pageSpend, "memory": pageMemory}

func adaptCommands(list []slashcat.Row) []command {
	out := make([]command, 0, len(list))
	for _, r := range list {
		c := command{name: r.Name, args: r.Args, desc: r.Desc, alias: r.Alias}
		if r.Door == slashcat.DoorTask {
			c.door = sendDoorTask
		}
		if r.Place != "" {
			pg, ok := placePages[r.Place]
			if !ok {
				panic("tui3: the command table names place " + r.Place + " and this surface has no such page")
			}
			c.desc += " · " + placeChord(pg)
		}
		out = append(out, c)
	}
	return out
}

// checkCommands is THE TABLE CHECK, run at init over [commands] and by the tests
// over tables of their own. The law itself — an alias may never shadow a name or
// another alias — is internal/slashcat's [slashcat.Check], so the terminal and
// the wire cannot disagree about it.
func checkCommands(list []command) error {
	rows := make([]slashcat.Row, len(list))
	for i, c := range list {
		rows[i] = slashcat.Row{Name: c.name, Alias: c.alias}
	}
	return slashcat.Check(rows)
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
//
// AND IT IS WHERE A ROW'S CHORD IS SPELLED FOR THIS KEYBOARD. Two of these
// descriptions carry a place's own chord ([placeChord]), baked in at init where
// no terminal has been detected yet — so on a Mac the list said `/spend … alt+3`
// while the map two keystrokes away said `opt+1…opt+8`. The substitution has to
// happen HERE rather than at either paint, because `⌘` is one cell where `cmd+`
// is four and [menu.fit] counts the lines this string will take before
// [menu.rows] draws it: measuring one spelling and drawing the other is a list
// that pushes the status line off the frame.
func (c command) note(chords chordSpelling) string {
	if c.name == "pair" && c.args == "" {
		if reason := cell.DisabledReason(); reason != "" {
			c.desc += " · " + reason
		}
	}
	if tail := c.aliasNote(); tail != "" {
		return chords.say(c.desc + " · " + tail)
	}
	return chords.say(c.desc)
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
func (c command) menuNote(width int, chords chordSpelling) string {
	note := c.note(chords)
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
// "res" selects /resume rather than the /new that carries "reset".
const aliasRung = 1_000

// bareFor says whether this row is the argless form of exactly the word that
// was typed — by its name or by any alias. It is [menu.rank]'s tiebreak: a
// finished word is a command a person is about to run, and the row that runs
// it is the one that takes nothing more.
func (c command) bareFor(needle string) bool {
	if c.args != "" {
		return false
	}
	if c.name == needle {
		return true
	}
	for _, al := range c.alias {
		if al == needle {
			return true
		}
	}
	return false
}

// matchAt is where needle was found in this command's words and whether it was
// found at all — the name first, then the aliases a rung below it. Lower is
// better: this is a plain offset, the one ranking the command list has always
// kept, and not the fuzzy matcher the pickers rank with (internal/fuzzy).
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

// menuRows is the preferred minimum viewport, further limited by the actual
// terminal space. Taller frames use the available room above the seam. Hidden
// commands remain reachable by scrolling, and the conversation list counts
// the rows remaining below its viewport.
const menuRows = 8

const commandNoMatchWord = "no commands match"

// menu is the command list's whole state. The zero value is closed.
type menu struct {
	open bool
	// at is the rune index of the '/' this list is filtering under. It used to
	// be implicit — the list only ever opened on a draft whose first character
	// was a slash, so the answer was always zero — and it is written down now
	// that a slash anywhere in a sentence opens it (see [menu.sync]).
	at    int
	query string
	// hits are indexes into commands, in alphabetical order.
	hits   []int
	score  []int
	cursor int
	top    int
	// sealed says a token has been ANSWERED and the list must stay out of it,
	// and sealAt is which one — the rune index its '/' sits at. Two gestures set
	// it, and they are the same gesture from the list's side: esc, which is a
	// person saying they meant the word rather than the list, and a row chosen
	// mid-sentence, whose answer the list would otherwise reopen on top of.
	//
	// It is the completion's `done` under another name (files.go) and it is kept
	// as a POSITION rather than as text because a command is a word a person
	// keeps typing after: "/task" dismissed and then continued into "/tasks of
	// the day" is still the same token, and the list may not come back for it.
	sealed bool
	sealAt int
}

// sync follows the slash token under the caret after edits and caret movement.
// The entire token must be a command word, so moving within a path cannot open
// the list. Unknown command words keep an empty list rather than revealing
// unrelated search results on home. A space or path punctuation leaves command
// mode, and Esc seals a literal token until the caret leaves it.
func (m *menu) sync(e *editor) {
	if strings.HasPrefix(strings.TrimSpace(e.String()), "!") {
		m.close()
		return
	}
	at, query, ok := slashToken(e.value, e.cursor)
	if ok {
		// Inspect the whole token, including text after the caret, so moving
		// within an absolute path cannot reopen the command list.
		for _, r := range e.value[at+1 : tokenEnd(e.value, at)] {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '?') {
				ok = false
				break
			}
		}
	}
	if !ok {
		m.close()
		return
	}
	if m.sealed && m.sealAt == at {
		// Answered already. The list stays down without forgetting why, so that
		// the next keystroke inside this same word does not reopen it.
		m.open = false
		return
	}
	was := m.open && m.at == at && m.query == query
	m.open, m.at, m.query = true, at, query
	m.rank(strings.ToLower(query))
	if !was {
		m.cursor, m.top = m.best(), 0
	}
}

// close is the full reset, the seal included: the draft this list was filtering
// has been sent, or an overlay took the box, and there is no word left to stay
// out of.
func (m *menu) close() { *m = menu{} }

// dismiss is close plus the memory of which token was dismissed. See [menu.sync]
// for what the seal buys and [app.dismissLists] for who presses it.
func (m *menu) dismiss(at int) {
	m.close()
	m.sealed, m.sealAt = true, at
}

// rank filters names and aliases by substring, then orders canonical rows
// alphabetically. Scores choose the initial selection without rearranging the
// list. Argument variants keep their table order beside their command.
func (m *menu) rank(needle string) {
	if cap(m.score) < len(commands) {
		m.score = make([]int, len(commands))
	}
	m.hits = m.hits[:0]
	for i, c := range commands {
		if !commandOnPlatform(c) {
			continue
		}
		at, ok := c.matchAt(needle)
		if !ok {
			continue
		}
		m.score[i] = at
		m.hits = append(m.hits, i)
	}
	sort.SliceStable(m.hits, func(a, b int) bool {
		left, right := commands[m.hits[a]], commands[m.hits[b]]
		if left.name != right.name {
			return left.name < right.name
		}
		// A fully typed command still chooses its bare form. Argument variants
		// otherwise retain their order within that command's alphabetic group.
		return left.bareFor(needle) && !right.bareFor(needle)
	})
	m.cursor = moveCursor(m.cursor, 0, len(m.hits))
	m.follow(menuRows)
}

// best selects a name or prefix match without moving it out of alphabetic order.
// An alias remains discoverable without stealing Enter from a matching name.
func (m *menu) best() int {
	best := 0
	for i, hit := range m.hits {
		if m.score[hit] < m.score[m.hits[best]] {
			best = i
		}
	}
	return best
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
// THE ROOM IS HANDED IN AS A NUMBER and the list stays pure. What is above this
// line ranks and filters commands and has never known how tall a terminal is;
// what the frame knows is how many rows are left once the status line, the box
// and a row of conversation have taken theirs ([app.overlayHeight] does that
// arithmetic once, for every list). So the frame passes the figure and this
// decides what to do with it, rather than either of them guessing at the other.
func (m *menu) height(width, room int, chords chordSpelling) int {
	if !m.open {
		return 0
	}
	if len(m.hits) == 0 {
		return 1
	}
	// The ceiling is in LINES, so at [tierPhone] the list holds four commands
	// with what they do written under them instead of eight rows that all say
	// "/settings   open the settings pa…" (palette.go).
	ceiling := menuRows
	if room > ceiling {
		ceiling = room
	}
	shown, lines := m.fit(width, ceiling, chords)
	if m.top+shown >= len(m.hits) || ceiling <= 1 {
		return lines
	}
	// SOMETHING IS HIDDEN, SO ONE LINE OF THE CEILING IS THE FOLD'S. It is
	// counted here rather than added on top, because the frame subtracts this
	// figure from the conversation before the rows are drawn: a list that came
	// back a line longer than it promised would push the status line off.
	_, lines = m.fit(width, ceiling-1, chords)
	return lines + 1
}

// fit is how many ROWS and how many LINES this list draws from [menu.top] inside
// a ceiling of screen lines. Two numbers rather than one because the fold has to
// know how many commands were left over, and at [tierPhone] a row is two lines —
// so a count of lines cannot answer that on its own.
func (m *menu) fit(width, ceiling int, chords chordSpelling) (rows, lines int) {
	for at := m.top; at < len(m.hits) && lines < ceiling; at++ {
		take := overlayItemLines(width, commands[m.hits[at]].note(chords))
		if lines+take > ceiling {
			break
		}
		lines += take
		rows++
	}
	return rows, lines
}

func (m *menu) rows(width, n int, pal palette, hover int, chords chordSpelling) []string {
	if n <= 0 {
		return nil
	}
	if len(m.hits) == 0 {
		return []string{pal.dim(fit("  "+commandNoMatchWord, width))}
	}
	m.follow(overlayItems(n, width))
	// The fold's line is taken off the room BEFORE the rows are laid into it, so
	// what is said about the remainder is true of the rows actually drawn. A fold
	// appended after the fact would be counting a row that is on the screen.
	room := n
	if shown, _ := m.fit(width, n, chords); m.top+shown < len(m.hits) && n > 1 {
		room = n - 1
		m.follow(overlayItems(room, width))
	}
	fill := newOverlayFill(width, room, pal, hover)
	past := m.top
	for at := m.top; at < len(m.hits) && fill.room(); at++ {
		c := commands[m.hits[at]]
		if !fill.add(at, c.typed(), c.menuNote(width, chords), at == m.cursor, false) {
			break
		}
		past = at + 1
	}
	lines, _ := fill.done()
	if hidden := len(m.hits) - past; hidden > 0 && room < n {
		// The two cells in front of it are [overlayLead]'s own, so the count hangs
		// under the commands rather than out in the margin beside them.
		lines = append(lines, pal.dim(fit("  "+foldLine(hidden, ""), width)))
	}
	return lines
}

// runMenu is enter while the command list is up: the row under the cursor wins.
//
// WHAT IT DOES WITH THE ROW DEPENDS ON WHERE THE TOKEN IS, and the rule is the
// submit rule read backwards. [app.enter] sends a draft to [app.slash] when its
// FIRST character is a slash and never otherwise, so:
//
//   - The token opens the draft and is all of it. This is a command being
//     chosen, and it behaves exactly as it always has — the row runs, or, if it
//     TAKES something, "/model " goes into the box with the caret after it,
//     because "/model <slug>" with no slug is not a command anybody meant.
//   - Anything else — a slash word inside a sentence, or a command whose
//     argument is already typed — is a MENTION. The token is replaced with the
//     command's word, the caret parks after it, and NOTHING RUNS. A list that
//     ran a command from the middle of a sentence would be running something
//     the same line submitted by hand would not.
//
// Either way the list stops offering: a mention seals its own token, so the
// answer this just wrote does not have the list reopen on top of it.
func (a *app) runMenu() tea.Cmd {
	chosen, ok := a.menu.choice()
	if !ok {
		return nil
	}
	word, ran := chooseCommand(&a.input, &a.menu, chosen)
	if !ran {
		return a.edited()
	}
	// It goes into the recall list exactly as if it had been typed out and
	// entered, because from the person's side it was: the list is a shortcut
	// for typing, not a second door with different rules (see [app.enter]).
	//
	// THIS HALF IS THE CHAT BOX'S OWN and is not in [chooseCommand] with the
	// rest. Home's composer runs the same rows through the same helper and has
	// no recall list and no draft to drop — ↑ there walks the drop-up rather
	// than a history — so the two lines below would be two no-ops and a lie
	// about what that surface keeps (homeslash.go's [app.homeRunCommand]).
	a.remember(word)
	a.dropDraft()
	return a.slash(word)
}

// chooseCommand is the mechanics of [app.runMenu] over WHATEVER box and
// WHATEVER list are handed to it: the token is rewritten with the chosen
// command's word, the list is put away, and the answer says whether what is
// left is a command to run. It is one function because it is one gesture — home
// offers the same rows over its own box (homeslash.go), and a second copy of
// this surgery is a second answer to what choosing a row does.
//
// It returns the word that was written and whether the caller should RUN it.
// False is a token rewritten inside a sentence, or a command left in the box
// waiting for the words it takes; true is a box that now holds the command and
// nothing else, and has been emptied ready for the dispatch. What happens after
// the dispatch is the caller's, because the two surfaces keep different things.
func chooseCommand(e *editor, m *menu, chosen command) (string, bool) {
	// The token's start is CLAMPED to the draft as it stands. The lists follow
	// edits and not caret moves, so there are gestures — a history recall, a
	// draft restored under an open list — that can leave this index pointing
	// past the end of a draft that has since got shorter, and an index into a
	// slice is not a thing to be optimistic about.
	at := min(max(m.at, 0), len(e.value))
	end := tokenEnd(e.value, at)
	// THE NAME AND NEVER [command.typed]. The row on the list reads
	// "/model <slug>" because that is what the command wants said to it, but the
	// placeholder is a thing to READ and never a thing to leave in somebody's
	// box — a draft holding a literal "<slug>" is a command nobody meant and one
	// the dispatcher would refuse.
	word := "/" + chosen.name
	if at != 0 || strings.TrimSpace(string(e.value[end:])) != "" {
		head := append([]rune(nil), e.value[:at]...)
		tail := append([]rune(nil), e.value[end:]...)
		e.value = append(append(head, []rune(word)...), tail...)
		e.cursor = at + len([]rune(word))
		m.dismiss(at)
		return word, false
	}
	m.close()
	if chosen.args != "" {
		e.setText(word + " ")
		return word, false
	}
	e.reset()
	return word, true
}

// helpText renders the same table the list draws, plus the two keys that have
// no slash and the session file. One source, two renderings.
// helpKeyRow is one hand-written row of the key sheet: the chord, then the
// sentence at [helpKeyColumn]. A chord wider than the column keeps one space, so
// a Mac's spelling can never run into the words beside it.
func helpKeyRow(key, note string) string {
	gap := helpKeyColumn - ansi.StringWidth(key)
	if gap < 1 {
		gap = 1
	}
	return key + strings.Repeat(" ", gap) + note
}

// helpKeyColumn is where the sentence starts on every hand-written row of the
// key sheet below — five for `@path` plus its ten spaces, six for `ctrl+c` plus
// its nine. It is named because ONE of those rows is spelled for the terminal
// rather than typed out (chords.go), and a padded literal cannot be padded twice.
//
// IT IS NOT THE COMMAND ROWS' COLUMN, which is measured off the longest command
// on the list and sits further right. The two blocks have always been two
// columns; this constant states the second one rather than changing it.
const helpKeyColumn = 15

func helpText(file string, chords chordSpelling) string {
	width := 0
	for _, c := range commands {
		if !commandOnPlatform(c) {
			continue
		}
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
		if !commandOnPlatform(c) {
			continue
		}
		// The aliases are printed here as they are on the row, and nothing is cut:
		// /help is prose in the transcript, which wraps, rather than a list drawn
		// into a fixed block (see [command.menuNote]).
		lines = append(lines, c.typed()+strings.Repeat(" ", width-len(c.typed())+2)+c.note(chords))
	}
	lines = append(lines,
		// THE KEY THAT GETS A PERSON HERE IS THE FIRST KEY ON THE SHEET. `?` over
		// an empty box is what opened this list for anybody who did not already
		// know six characters of it, and a sheet that did not name it would be a
		// door with no sign on it (the block at the foot of this file argues the
		// binding).
		helpKeyRow(helpAskKey, helpAskWord),
		"@path          complete a file · a picture attaches",
		// THE DOOR IS NAMED HERE BECAUSE ONE KEY CARRIES TWO MEANINGS
		// (leaving.go): at rest it leaves, mid-turn it stops the model, and a
		// person whose ctrl+c "only interrupted" looks here before anywhere else.
		"ctrl+c         quits everything · mid-turn it interrupts instead, like esc",
		// tab is the seventeenth rung of the key router (input.go) and does
		// nothing at all when this terminal holds one conversation — which is
		// why the line says what it needs rather than promising it always works.
		"tab            go back to the last conversation, with an empty box",
		// ── THE SEVEN PLACES, WHICH THIS SHEET USED TO NAME NO WAY INTO ───────
		//
		// This block lists every chord a person can press, and until this wave it
		// held not one of `alt+1`…`alt+7`, `alt+.` or `tab`'s meaning on a place —
		// so somebody who typed /help from a cold start finished it without
		// learning that the places exist. The map (`alt+.`) is the surface's own
		// chord list and was reachable only from inside a place you already had to
		// know how to open, which is a help sheet behind the thing it explains.
		//
		// The three rows are spelled through [chordSpelling.say] like the
		// `alt+enter` row above them, so a Mac reads `opt+1…opt+8` and a Linux box
		// reads what is authored here — one substitution, one door (chords.go).
		//
		// THE DIGITS ARE NAMED ONE BY ONE, AND NOT AS THE BAR'S ORDER. This row
		// said "in the tab bar's own order" over a list in [placeOrder]'s order,
		// and the two have differed since the bar was regrouped: memory is fifth
		// on the bar and `alt+8` ([placeWordList] says why each word carries its
		// number).
		helpKeyRow(chords.say(chordJumpWords), "go to a place · "+placeWordList()),
		helpKeyRow(chords.say(placeMapKey), "on a place: what else is here · every key that place has, drawn"),
		"               on a place, tab is the next place · esc back",
		// THE CHORD IS SPELLED FOR THIS TERMINAL AND THEN PADDED, in that order,
		// because [helpKeyRow] pads to a fixed column and a literal padded to one
		// spelling would put this row's sentence out of the column every other row
		// on the sheet sits in. `alt+` and `opt+` are the same four cells, so this
		// row is safe either way today; `cmd+` against `⌘` is not, and the order
		// is the sheet's rule rather than this row's luck (chords.go).
		helpKeyRow(chords.say("alt+enter"), "open a line · enter sends"),
		// SPELL IT OUT (spellout.go), on this sheet because it is about the
		// sentence in the box rather than about the screen, and nothing else names
		// it until a draft happens to look like something to build.
		// AND THE SCOPE IS ON THE ROW, because this chord is on this sheet TWICE.
		// `ctrl+r` is the spell-it-out chord over a draft and the reveal key inside
		// /files (deliverables.go's [filesRevealKey]), thirty-six rows apart, and
		// neither row said the other existed — so the sheet a person opens to learn
		// the keys contradicted itself and gave no way to tell which reading was
		// theirs. The two do not collide in the code, and now they do not collide
		// on the page either: each says where it acts, in the grammar the scoped
		// rows at the foot of this list already use.
		helpKeyRow(spellOutKey, "over a draft: spell it out · what it means · enter adds it to yours"),
		"ctrl+o         expand this turn's tool calls · click one to open it · in a task, scroll up does too",
		"ctrl+b         copy mode · ↑↓ move · v marks · a takes the block · y yanks",
		"ctrl+s         drag to select with your mouse · any key ends it",
		"enter          mid-answer: stops the current reply and steers these words in",
		// AND THE THIRD THING TO DO WITH A SENTENCE TYPED OVER A RUNNING ANSWER
		// (steer.go). It sits directly under the line about the other two because
		// the three are one decision — wait, go in, or stop the answer — and the
		// one that waits is now the secondary choice a person may not guess.
		// AND THIS ROW IS SPELLED FOR THE TERMINAL READING IT, like the `alt+`
		// rows above and below. It used to be a literal `cmd+enter` on every
		// platform, so the sheet on a Linux box named a modifier that keyboard does
		// not have — while the keystroke itself arrives there as `super+enter`
		// (steer.go binds both names). The substitution is chords.go's one door.
		helpKeyRow(chords.say(parkKey), "mid-answer: waits above the box · → sends a waiting one"),
		// THE QUEUE KEY (followup.go), pressed many times a session.
		// AND THE CAVEAT IS ON THE ROW for the chord's own limit: on a terminal that
		// cannot tell ctrl+enter from a plain enter, the key arrives as a newline
		// and queues nothing.
		helpKeyRow(chords.say("ctrl+enter"), "mid-answer: queue the draft to run after the current turn · needs a terminal that can send it"),
		// THE ROW READS IN THE ROUTER'S ORDER. The empty-box key asks the running
		// turn's window first ([app.toggleLatestWorkfold]) and falls through to
		// thinking ([app.toggleLatestThought]); the row was rewritten when the key was.
		"ctrl+e         open the running turn's compact steps · the newest worked chip · or the thinking",
		// THE NEW TAB AND THE ROSTER, IN THAT ORDER AND ON TWO ROWS. They used to be
		// one key: ctrl+t handed the roster the keyboard, and the tab strip's `+` had
		// no chord at all. The strip is drawn as tabs, so the key every browser opens
		// a tab with is the one people press at it — and the roster keeps the letter
		// under the other modifier (task.go's [railHoldChord]), which is the smallest
		// move a hand has to make and the modifier its own widen chord already uses.
		helpKeyRow(newChatChord, "a new chat start page · your draft stays put · esc back"),
		// AND THE OTHER DIRECTION, ON THE ROW UNDER IT. The two chords are one
		// gesture, so they are read together here as they are in input.go, and the
		// row says what the key does NOT do — because "close" is the word people
		// fear on a conversation that has an hour of work in it.
		// AND THE ROW MAY NOT SPELL THE SWITCHER'S OWN CHORD (escword_test.go finds
		// the switcher's row by that prefix, and a second row carrying it is a second
		// answer to the question that test asks). The card is named by what it is
		// instead.
		helpKeyRow(closeTabChord, "close this tab · select the last open chat · keep your draft"),
		// THE COLUMN CHORD AND THE MANAGER CHORD (teamrail.go). The first works in
		// every chat; the second needs a team with a manager, and the rows say so
		// rather than leaving a person to find out by pressing them elsewhere.
		helpKeyRow(chords.say(trafficKey), "show or hide the right column · Tasks or Traffic"),
		helpKeyRow(modelCycleKey, "next quick switch chat model · choose the rotation in Settings → Models"),
		helpKeyRow(chords.say(teamManagerKey), "in a team with a manager: go to the manager"),
		helpKeyRow(reopenTabChord, "reopen the last closed tab · when the terminal sends this distinct chord"),
		helpKeyRow(chords.say(railHoldChord), "the task roster · ↑↓ move · ←→ tasks/traffic · enter opens · esc back"),
		"ctrl+.         every task this project has run · /history · type to filter",
		"ctrl+g         close the roster's column, or bring it back · remembered",
		"ctrl+l         back to the latest · the chip above the box says so too",
		// THE SWITCHER (hop.go). It is named here on every terminal because the
		// binding reaches every terminal; the `ctrl+tab` alias is not on this
		// list, for the reason the manual states — a line that named it would be
		// naming a chord half the terminals reading this cannot send.
		// The line is TRUE IN BOTH MODES of ui.quick_switch on purpose: this list
		// has no reach into the profile, and a clause that named one mode would be
		// wrong in the other. The card's own head and the manual say the rest.
		// IT IS BUILT RATHER THAN TYPED OUT, because the chord wears a modifier
		// with two keycaps and the padding has to be measured after the spelling
		// is chosen ([helpKeyColumn] says why a padded literal cannot be padded
		// twice).
		helpKeyRow(chords.say(hopOpenKey), "choose a conversation · enter open · esc cancel"),
		"               → reaches every other one on this machine · ctrl+w closes one",
		"→ ←            over an empty box: into a running task, and back out",
		// THE WORD "home" USED TO BE HERE AND IS NOW SPENT. This gesture leaves a
		// task room for the conversation; /home is a screen of every project on
		// the machine, and one word meaning two places on the same list is a
		// person pressing ← ← to find out where they end up.
		"← ←            out of a task room · the conversation, at the live edge",
		"space space    over an empty box: home · /home · esc back",
		// THE WORD KILL IS NAMED BY THE KEYS THAT STILL REACH THE BOX. ctrl+w was
		// on this row until it became the close-tab chord above, and a sheet that
		// went on offering it would be teaching a keystroke that shuts the window
		// you are typing in.
		helpKeyRow(chords.say("alt+backspace"), "delete the word behind the caret · ctrl+u the line · ctrl+k the rest of it"),
		"ctrl+,         open settings",
		"d              in /permissions: drop the line under the cursor · press it twice",
		// THE AUTOMATIONS PLACE'S LETTERS, which work while → has drawn the row's
		// keys (verbstrip.go); `s` stops a run in flight and is drawn only then.
		"→ r p e d o    in /automations: show the row's keys · run it now · pause it · edit it · delete it · open where it was asked",
		// THE TEAMS PAGE'S LETTERS, each the button of the same word on the
		// selected team, and the chord that puts the keyboard on those buttons
		// while the manager's conversation has the box (teamspagehost.go).
		"s c w n o M m p d u  in /teams: settings · disband · wall · new team · organize · manager · move · members · delete · undo",
		helpKeyRow(chords.say("alt+↑↓"), "in /teams: onto the page's buttons while the manager has the box · esc back"),
		"ctrl+r ctrl+y  in /files: reveal the folder it is in · copy it somewhere",
	)
	if file != "" {
		lines = append(lines, "session · "+file)
	}
	return strings.Join(lines, "\n")
}

// ── WHAT THE WORDS AFTER /model MEAN ────────────────────────────────────────
//
// /model has always taken one thing — a slug — and switched to it. It now takes
// three, and they are told apart by SHAPE rather than by a flag, because a
// person types what they want and not what kind of thing it is:
//
//	/model deepseek/deepseek-v4-flash    a name: switch, exactly as before
//	/model @cloudflare                   a machine: pin the lane, stay on the model
//	/model auto                          give the lane choice back
//	/model deep seek                     words: open the list with them typed in
//
// THE SLUG CASE IS UNCHANGED AND IS THE FALL-THROUGH, which is the same
// arrangement the filter box has: anything this grammar does not recognise is
// still the thing it always was.

// modelIntent is what one /model argument asks for.
type modelIntent uint8

const (
	// modelSwitch is a slug, taken as typed.
	modelSwitch modelIntent = iota
	// modelPinLane is `@name`.
	modelPinLane
	// modelAutoLane is `auto`.
	modelAutoLane
	// modelQuery is a query for the picker.
	modelQuery
)

// modelArg reads the words after /model. The second return is the lane for a
// pin and the query for a filter, and is the argument itself for a switch.
func modelArg(rest string) (modelIntent, string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return modelQuery, ""
	}
	fields := strings.Fields(rest)
	if len(fields) == 1 {
		switch {
		case strings.EqualFold(fields[0], config.LaneAuto):
			return modelAutoLane, ""
		case strings.HasPrefix(fields[0], "@") && len(fields[0]) > 1:
			return modelPinLane, strings.TrimPrefix(fields[0], "@")
		}
	}
	// A QUERY IS ANYTHING WITH A SPACE IN IT: a slug has no spaces, so two words
	// were never a name, and the only sensible thing to do with two words is hand
	// them to the box that searches names by their pieces ([picker.rank]).
	//
	// A SINGLE WORD IS A SLUG, whatever it looks like. It used to be checked
	// against the filter box's own query language first — so `/model fast` and
	// `/model <1s` opened a narrowed list — and that language is gone: the box
	// searches names only, and a word this command took as a question would be
	// a word it refused to take as the name of a model.
	if len(fields) > 1 {
		return modelQuery, rest
	}
	return modelSwitch, rest
}

// runDebugCommand is /debug: keep the full record of THIS conversation from
// here on.
//
// IT IS ONE-WAY, and that is the whole design. A person types it because
// something has already gone wrong, and a switch that could be turned off again
// would only ever produce half a record — the half after the thing they were
// trying to catch. The other two doors mean the same thing (--debug on the
// command line, CODEAF_DEBUG in a shell), and this one exists for the case
// neither of them can serve: the conversation is already open, and the turn
// worth recording is the next one.
//
// IT TURNS THE RECORD ON FOR THIS RUN AND NO OTHER. One process can hold
// several conversations, and a process-wide flip from a command typed inside
// one of them would write another person's prompts and replies into a folder
// they never asked for. The pin and the flag are the process-wide doors,
// because those were handed to the process on purpose.
//
// It ANSWERS every way round. Turning it on says where the record goes, because
// a command that recorded something and did not say where would leave a person
// hunting a folder; typing it twice says it is already on and where; and where
// the whole process is already recording it says that instead, because "it is
// on for everything this codeaf is doing" is a different fact from "it is on
// for you", and a person reading a folder later needs to know which.
func (a *app) runDebugCommand() {
	_, message := trace.KeepConversation(a.ctx)
	a.note(message)
}

// ── `?` — THE KEY A PERSON PRESSES WHEN THEY ARE LOST ───────────────────────
//
// Until this wave `?` was bound to nothing at all. It was an ALIAS of /help —
// `/?`, six characters and a slash you already had to know about — so the
// shortest honest route to the key sheet was a command a person could only find
// by opening the command list, which is itself a door the greeting names in
// four words at the bottom of the screen. Every program with a key sheet in it
// has answered this key since curses existed, and the first thing a developer
// does when a full-screen program stops making sense is press it.
//
// WHAT IT OPENS, AND WHY THAT IS TWO THINGS. `?` means one sentence — SHOW ME
// THE KEYS FOR WHERE I AM STANDING — and this surface has two answers to it
// because it has two screens:
//
//   - In a conversation it runs /help, which is the key sheet: every command,
//     every chord, in the transcript where it can be scrolled and searched.
//   - On a place it draws THE MAP (`alt+.`), which is that place's own key list
//     drawn in the cells the foot was already using ([placeMapWords], SCREEN
//     3b). Printing the sheet from a place would mean leaving the room a person
//     is standing in to answer a question about it, and the map is the answer
//     the surface already has: the chords, and — since row 14 of this wave —
//     only the ones this place really has.
//
// It is one gesture with one meaning and two renderings, exactly as [helpText]
// is one table with two ([app.slash]'s "One source, two renderings").
//
// AND THE ONE THING THAT WOULD MAKE IT WORSE THAN NOTHING: eating a `?` a
// person is typing. The guard is the offer key's guard word for word (keys.go)
// and it is structural rather than clever — THE BOX MUST BE EMPTY. A question
// mark is nearly always the LAST character of a sentence and never the first,
// so a draft with anything in it keeps the key and it types; and every overlay,
// list, card and modal on this surface is read above this rung, so a `?` typed
// into a filter box, a folder picker or a consent question never reaches here
// at all.
const helpAskKey = "?"

// helpAsk is `?` on the conversation's road: the key sheet, over an empty box.
//
// It answers false for every other key and for a box with something in it, so
// the rung it sits on in [app.key] costs one string comparison.
func (a *app) helpAsk(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != helpAskKey || !a.input.empty() {
		return nil, false
	}
	// THE COMMAND AND NOT A SECOND PRINTING OF THE SHEET. /help is what this
	// key means, so it goes through the dispatcher the typed word goes through
	// and gains whatever that command gains next.
	return a.slash("/help"), true
}

// helpAskWord is the key sheet's own row for the key, and it is the sheet's
// first key row because it is the one a lost person presses before they have
// read any of the others.
const helpAskWord = "the keys · on a place it draws that place's own map"

// ── THE WORD THIS SURFACE DOES NOT HAVE ─────────────────────────────────────

// unknownCommandWord is what a slash word nobody here recognises is answered
// with, and it is A SENTENCE ABOUT THE WORLD.
//
// It read `unknown command: /nosuchthing · try /help` — a compiler's noun and a
// colon, in a lane where every other refusal on this surface is written the way
// a person would say it: `there is no manual page named xyzzy` (manualcmd.go),
// `that conversation is not on this machine any more`. The half that mattered
// was already right — it says what to do — so what changes is the register and
// where it points.
//
// AND IT POINTS AT THE LIST RATHER THAN AT A SECOND COMMAND. `/help` is six
// characters somebody who has just mistyped a command has to type correctly;
// `/` is one keystroke, it is the door the greeting already advertises, and
// since row 8 of this wave the list it opens fills the frame and says how many
// rows it is holding back. `?` opens the sheet itself, in one key ([helpAskKey]).
func unknownCommandWord(name string) string {
	return unknownCommandLead + " /" + name + " · " + unknownCommandDoorWord
}

// unknownCommandLead is that sentence's opening, named so a test can assert the
// refusal without pasting the whole of it — and so that a test asserting a
// refusal did NOT happen cannot go on passing after the wording moves, which is
// exactly what eight of them did while this line said "unknown command".
const unknownCommandLead = "there is no command called"

// unknownCommandDoorWord is that sentence's second clause, named because the
// refusal for a DROPPED path is written from the same two halves (dropkeys.go)
// and a door spelled twice is a door that gets moved once.
const unknownCommandDoorWord = "/ lists them"

// commandOnPlatform keeps unsupported move-only rows out of help and completion.
// A fully typed command still reaches its one-sentence refusal.
func commandOnPlatform(c command) bool {
	return runtime.GOOS != "windows" || (c.name != "pair" && c.name != "devices" && c.name != "atlas")
}
