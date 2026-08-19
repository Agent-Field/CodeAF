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
	// WHAT HAS ALREADY BEEN ANSWERED, and the way to take one back
	// (permissions.go). It BELONGS beside /settings and /connect — those two are
	// "what may this thing do" and "what may it reach", and this is "what has it
	// already been told it may do without asking" — and it sits down here
	// instead for the reason /harness does, stated one row below: [menuRows]
	// shows eight rows at once, position in this table is a claim about
	// frequency, and putting this at the top of the list would have pushed
	// /compact into a scroll to make room for a panel a person opens when
	// something has surprised them. /perms is here because it is what fingers
	// type; the row shows the whole word.
	{name: "permissions", desc: "what runs without asking · drop one with d", alias: []string{"perms"}},
	// THE SHAPES OF WORK THIS CONVERSATION HAS SAVED (harnesspanel.go). It
	// belongs topically beside /connect — one is what this surface may reach,
	// the other is what it has learned to do — and it sits here instead for a
	// reason about the LIST rather than about the command: [menuRows] shows
	// eight rows at once, and a new row put in the middle would have pushed
	// /compact, which people reach for daily, into a scroll to make room for one
	// they will open occasionally. Position in this table is a claim about
	// frequency; this is the honest one.
	// The other words are here because a sub-harness and a harness are the same
	// thing under two names (docs/SUBHARNESS.md), and a person who learned the
	// longer one should not have to find out which half of it this build chose.
	// Every one of them opens the same panel bare, and the same picker with a
	// space after it (harnesspick.go).
	{name: "harness", desc: "the shapes of work you have saved · a space picks one to run",
		alias: []string{"harnesses", "subharness", "sub"}},
	// WHAT IT KNOWS ABOUT YOU, and the two ways to change it. They sit beside
	// /harness because they answer the neighbouring question — one is what this
	// conversation has learned to DO, these are what it has been told about YOU
	// — and they are three rows rather than one because a person arrives with
	// one of three errands: seeing the list, adding to it, or dropping something
	// off it.
	//
	// /memories has the bare form and the narrowed one, the way /model does: the
	// bare row is the one nearly everybody wants, and a single row carrying
	// [query] would make it unreachable from the list ([app.runMenu] puts a row
	// that TAKES something into the draft instead of running it).
	{name: "memory", desc: "inspect and change what is remembered"},
	{name: "memory", args: "<query>", desc: "print only the memories matching a word"},
	{name: "memories", desc: "print what is remembered about you"},
	{name: "memories", args: "<query>", desc: "…only the ones matching a word"},
	{name: "remember", args: "<text>", desc: "keep one thing across conversations"},
	{name: "forget", args: "<query>", desc: "drop what is remembered about something"},
	// AND WHAT AFORGE WORKS WITH, beside what it knows about you. The four models
	// it uses on your behalf, answered as one word (crew.go). Two rows for one
	// command, the way /model and /export have two: the bare form is the listing
	// nearly everybody wants, and a single row carrying <preset> would make it
	// unreachable from this list — [app.runMenu] puts a row that TAKES something
	// into the draft instead of running it.
	//
	// It sits here, under the memory rows, because those three are "what does it
	// know" and this is "what does it think WITH", and because position in this
	// table is a claim about frequency: a person sets their crew once and then
	// occasionally regrets it, which is exactly where /memories sits too.
	{name: "crew", desc: "the four models aforge works with · frugal, balanced or max"},
	{name: "crew", args: "<preset>", desc: "…set it to one of the three"},
	{name: "task", args: "<brief>", desc: "start work you can walk away from"},
	{name: "task", args: "solo <brief>", desc: "…with one worker and no planner"},
	{name: "task", args: "adaptive <brief>", desc: "…with a planner and parallel parts"},
	// THE TWO QUESTIONS THE STATUS LINE IS ALREADY ANSWERING, asked out loud. The
	// line at the bottom of the frame drops whatever does not fit and a phone-width
	// frame keeps two of eleven facts (statusdeck.go), so on any surface these are
	// the commands that say the rest of it — and on a surface with the mouse turned
	// off they are the only way to the sheet at all.
	//
	// They sit HERE, under /harness and above the copy pair, for the reason
	// /harness sits where it does: position in this table is a claim about
	// frequency, [menuRows] shows eight rows at once, and a person reads their bill
	// occasionally while they compact and rewind daily. Neither may push /compact
	// into a scroll.
	//
	// /status is the wider word and goes first, because the spend is one of the
	// lines it prints: somebody who wanted the money and typed the general word
	// still gets their answer, while the reverse is not true.
	{name: "status", desc: "everything the status line knows, one fact per line", alias: []string{"info", "context"}},
	{name: "cost", desc: "what this conversation has spent, and on what", alias: []string{"usage", "tokens", "spend"}},
	// THE THREE DOORS ONTO GETTING TEXT OUT, and they sit beside /help because
	// that is where a person goes with the question they answer. The keys behind
	// the first two are the least discoverable on the surface — nothing on the
	// screen says either exists — and "why can I not copy this" is the first
	// question this surface gets asked. /copy is the keyboard's way, /select the
	// mouse's (copymode.go).
	//
	// /export is the third and it is a different KIND of answer: those two hand
	// over what is on the screen, and this one writes the whole conversation to a
	// file somebody can send (export.go). It is last of the three because it is
	// the one a person reaches for once, at the end.
	{name: "copy", desc: "read the conversation back and copy from it · ctrl+b"},
	{name: "select", desc: "drag to select with your mouse · ctrl+s"},
	// TWO ROWS FOR ONE COMMAND, the way /model has two. A single row carrying
	// <path> would make the bare form — which is the one nearly everybody wants —
	// unreachable from the list: [app.runMenu] writes a row that TAKES something
	// into the draft instead of running it, so choosing it would put "/export "
	// in the box and wait for a path nobody had in mind.
	{name: "export", desc: "write this conversation to a file", alias: []string{"save"}},
	{name: "export", args: "<path>", desc: "…and write it there · tab completes the path"},
	// AND THE FOURTH DOOR, which is the other direction: those three take
	// something out of THIS conversation, and this one finds what any of them
	// has already made — a picture, an export, a document — from a list of
	// everything, whatever directory it was made in (deliverables.go). It sits
	// beside them because that is the errand a person is on when they reach for
	// it, and after them because it is the one you type when the making is
	// already done.
	//
	// No argument form and no alias. A deliverable is picked from rows a person
	// recognizes by title, and a title a model wrote is not a thing anybody
	// types back correctly.
	{name: "files", desc: "what has been made for you · open, reveal or copy one"},
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
	// at is the rune index of the '/' this list is filtering under. It used to
	// be implicit — the list only ever opened on a draft whose first character
	// was a slash, so the answer was always zero — and it is written down now
	// that a slash anywhere in a sentence opens it (see [menu.sync]).
	at int
	// hits are indexes into commands, in rank order.
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

// sync opens, narrows or closes the list from the draft and the caret. It is
// called after every edit, and it is the ONLY thing that opens this overlay:
// there is no key for it, because the key is "/".
//
// ── A SLASH ANYWHERE, NOT ONLY AT THE HEAD OF THE LINE ──
//
// This used to ask one question of the whole line — does it start with "/" and
// hold no space — which meant the list could only ever be reached by starting a
// message with it. A person half a sentence in who wanted to know what commands
// exist had to throw the sentence away to find out.
//
// So it asks the same question the "@" list asks instead ([slashToken]): the
// caret is standing in a word, and that word begins with a slash. The dampers
// that keep a PATH from dragging this open on every keystroke are three, and all
// three are here rather than spread around the surface:
//
//   - A '/' with a non-space in front of it opens nothing. That is the token
//     rule, and it is what makes "/Users/santosh" one candidate rather than two.
//   - A filter that matches NOTHING closes the list. The word being matched is
//     the whole run to the next space — "Users/santosh", "tmp/aforge" — so a
//     path drops out within a couple of keystrokes and stays out; a backspace
//     back into a word that does match brings it straight back.
//   - A space closes it, because the word the caret is in stops being the slash
//     word — which is the rule that has always closed this list, restated.
//
// And esc seals the token outright ([menu.dismiss]), for the person who meant
// the word and does not want to be asked again about it.
func (m *menu) sync(e *editor) {
	at, query, ok := slashToken(e.value, e.cursor)
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
	was := m.open && m.at == at
	m.open, m.at = true, at
	m.rank(strings.ToLower(query))
	if len(m.hits) == 0 {
		// NOTHING MATCHED, so there is nothing to be offered. It drew no rows in
		// this state before ([menu.height] returns none), and being closed as
		// well is what stops a path from holding an invisible overlay open
		// underneath a sentence — and what lets esc, ↑ and ↓ mean what they
		// ordinarily mean again.
		m.open = false
		return
	}
	if !was {
		m.cursor, m.top = 0, 0
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
	e := &a.input
	// The token's start is CLAMPED to the draft as it stands. The lists follow
	// edits and not caret moves, so there are gestures — a history recall, a
	// draft restored under an open list — that can leave this index pointing
	// past the end of a draft that has since got shorter, and an index into a
	// slice is not a thing to be optimistic about.
	at := min(max(a.menu.at, 0), len(e.value))
	end := tokenEnd(e.value, at)
	word := "/" + chosen.name
	if at != 0 || strings.TrimSpace(string(e.value[end:])) != "" {
		head := append([]rune(nil), e.value[:at]...)
		tail := append([]rune(nil), e.value[end:]...)
		e.value = append(append(head, []rune(word)...), tail...)
		e.cursor = at + len([]rune(word))
		a.menu.dismiss(at)
		return a.edited()
	}
	a.menu.close()
	if chosen.args != "" {
		e.setText(word + " ")
		return a.edited()
	}
	e.reset()
	// It goes into the recall list exactly as if it had been typed out and
	// entered, because from the person's side it was: the list is a shortcut
	// for typing, not a second door with different rules (see [app.enter]).
	a.remember(word)
	a.dropDraft()
	return a.slash(word)
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
		"ctrl+b         copy mode · ↑↓ move · v marks · a takes the block · y yanks",
		"ctrl+s         drag to select with your mouse · any key ends it",
		"ctrl+q         ask this after the current turn instead of into it",
		"ctrl+e         open the model's thinking, streaming or finished",
		"ctrl+t         the task roster · ↑↓ move · →← fold · enter opens · esc leaves",
		"ctrl+l         back to the latest · the chip above the box says so too",
		"→ ←            over an empty box: into a running task, and back out",
		"← ←            home · the conversation, at the live edge",
		"ctrl+w         delete the word behind the caret · ctrl+u the line",
		"ctrl+,         settings",
		"d              in /permissions: drop the line under the cursor · press it twice",
		"ctrl+r ctrl+y  in /files: reveal the folder it is in · copy it somewhere",
	)
	if file != "" {
		lines = append(lines, "session · "+file)
	}
	return strings.Join(lines, "\n")
}
