package tui3

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE SWITCHER — alt+tab for the conversations this process already holds ──
//
// THE ASSUMPTION BEING REMOVED IS THAT THE WAY BETWEEN CONVERSATIONS IS A PAGE.
// The keeper has held up to eight of them alive since the conversations wave
// (keeper.go), and the only gesture between them was `tab`, which goes to ONE —
// the last — and says nothing about the other six. Reaching a third meant
// leaving the chat for home, reading a list and coming back, which is a screen
// transition for a gesture a person fires fifty times a day.
//
// So: a card of the open conversations, over whatever you are looking at, with
// the surface behind it DIMMED rather than covered.
//
// IT IS CALLED `hop` HERE AND `the switcher` TO A PERSON. This package already
// spends the word `switcher` on home's own reading (switcher.go), which is a
// different thing — a whole page, ranked, grouped, with standing orders and a
// ledger in it — and two `switcher`s in one package would be two things nobody
// can tell apart in a stack trace.
//
// ── WHY IT IS BUILT ONLY FROM MEMORY ────────────────────────────────────────
//
// EVERY FIELD ON EVERY ROW COMES FROM THIS PROCESS'S OWN STATE: the keeper's
// map, the sidecar each detach left, and three predicates asked of the agent
// pointers this process is already holding. Nothing here opens a file, scans a
// world, reads a presence heartbeat or crosses a wire.
//
// That is not thrift, it is the feature. Home's reading is correct and costs a
// world scan on a three-second beat; a gesture fired between two sentences must
// cost nothing at all, and over `--host` a reading that touched the disk would
// be a round trip to another machine before the card could be drawn. A switcher
// that took a quarter of a second to appear is a switcher people stop using.
//
// ── AND THE ROWS ARE FROZEN THE MOMENT IT OPENS ─────────────────────────────
//
// A conversation that finishes a turn while the card is up stirs the surface
// (keeper.go's [behindWatch.stir]), and a list that re-ranked on that stir would
// move the row under the cursor between the keystroke that aimed at it and the
// `enter` that took it. So [hopCard.rows] is a snapshot: what is drawn is what
// was true when the card opened, and the only thing that moves is the cursor.
//
// ── THE CARD HAS NO BORDER, AND THE DEPTH IS THE WHOLE OF WHAT SAYS `LAYER` ──
//
// This surface draws no boxes (home.go's preview card states the same law), so
// the card is not framed: the body behind it is repainted at the FAINTEST stop
// of the depth ladder ([composerFade], depthfade.go) and the card's own rows are
// left at full ink. The contrast between the two is the layer. That is the same
// move SCREEN 2e's composer layer makes over a place, one mechanism rather than
// two, and it is why nothing underneath has a word to say about being under one.
//
// The rows themselves are home's rows — the same glyph door, the same bold
// subject, the same dim tail dropped in the same order (switcher.go's
// [switcherLine]) — because a person who has read home once should not have to
// learn a second list.

// hopOpenKey is the key that opens the switcher, and it is `ctrl+k` for four
// reasons stated in the order they were weighed:
//
//  1. IT ARRIVES IN EVERY TERMINAL. `ctrl+k` has a legacy encoding (0x0B), so it
//     needs no protocol negotiation, no kitty keyboard flag and no cooperation
//     from a multiplexer in between. That is the first question asked of any
//     chord on this surface, because a capability that cannot work is absent
//     rather than broken (bargein.go states the law).
//  2. NOTHING TAKES IT FIRST. No window manager claims it, and no common
//     emulator binds it — unlike `ctrl+tab`, which WezTerm and Windows Terminal
//     both spend on their own tabs by default, and unlike `alt+tab`, which the
//     window manager takes on Windows and on most Linux desktops.
//  3. IT ALREADY MEANS THIS. `ctrl+k` / `cmd+k` is "jump to a conversation" in
//     Slack, and the switcher in VS Code, Linear and Notion. A person who has
//     used any of them has already learned this key.
//  4. IT IS FREE HERE. The one other place this surface binds it is the harness
//     design's approval row (roomapproval.go), which is a modal that owns the
//     whole keyboard while it is up and is read before this claim — so the two
//     never contend for one keystroke on one screen, exactly as `ctrl+.` means
//     two things on two screens (chords.go's [chordMapAlias]).
const hopOpenKey = "ctrl+k"

// hopAlias and hopBackAlias are the muscle memory, bound ONLY where the terminal
// says it can spell them ([app.ctrlDigits], the same reply `ctrl+1`…`ctrl+7`
// hang off in chords.go).
//
// `ctrl+tab` has no legacy encoding: on a terminal that has not taken the kitty
// keyboard protocol's disambiguation flag it arrives as a bare `tab` and means
// whatever `tab` means there. That is why it cannot be the way in and can only
// ever be a second name for one — and why it is never advertised where it would
// not be delivered.
const (
	hopAlias     = chordCtrlWord + "tab"
	hopBackAlias = chordCtrlWord + "shift+tab"
)

// hopHereWord marks the conversation you are standing in, which is drawn LAST so
// the ring has a visible seam: the list reads "these are the others, and here is
// where you are", rather than being a circle with no beginning.
const hopHereWord = "you are here"

// The four things a row can say about what changed since you last looked. They
// are sentences rather than figures because the question a person is asking when
// they open this card is "does anything want me", and `0` is not an answer to it.
const (
	hopAskingWord  = "asking you something"
	hopLandedWord  = "it finished while you were away"
	hopNothingWord = "nothing new"
)

// hopRow is one open conversation as the card draws it. Every field is a string
// the card prints, decided once here, so the paint never re-derives a fact.
type hopRow struct {
	// file is the transcript, and it is the ADDRESS: committing a row hands it
	// to [app.bringForward], which is the same door home's `enter` uses.
	file    string
	title   string
	project string
	note    string
	age     string
	here    bool
	needs   bool
	moving  bool
}

// hopCard is the whole of the switcher's state. The zero value is closed.
type hopCard struct {
	open bool
	// at is the cursor, an index into rows. It opens on ZERO, which is the most
	// recently open conversation behind this one — the same place `tab` goes —
	// so the commonest journey is `ctrl+k enter` and the second commonest is one
	// more `ctrl+k` before the `enter`.
	at int
	// rows are frozen at open. See the header: a stir must never renumber a list
	// somebody is aiming at.
	rows []hopRow
}

// hopShowing is the one predicate the frame asks.
func (a *app) hopShowing() bool { return a.hop.open && len(a.hop.rows) > 0 }

// hopAvailable reports whether the key would DO anything if it were pressed
// right now, which is both the guard and the advertisement's condition — the two
// may not come apart (render.go's [app.hintWord] states the law).
func (a *app) hopAvailable() bool {
	if len(a.behind) == 0 {
		// One conversation is not a ring. The key is neither bound nor named,
		// which is the emptiness law said about a keystroke.
		return false
	}
	// THE TWO LAYERS THAT HAVE ALREADY CLAIMED THE KEYBOARD ARE ASKED ABOUT
	// HERE, because this claim is read ABOVE the place router and so does not
	// pass through either of their own arbitration. The composer layer is a
	// decision with four answers on screen (composerlayer.go) and copy mode is a
	// frozen viewport (copymode.go); a card that opened over either would be a
	// card drawn over a gesture somebody is in the middle of.
	return !a.composer.open && !a.copy.on
}

// hopOpen builds the reading and raises the card.
func (a *app) hopOpen() {
	rows := a.hopReading()
	if len(rows) < 2 {
		// Nowhere to go. The guard above has already refused this, and this is
		// the same refusal said where the rows are actually counted.
		return
	}
	a.hop = hopCard{open: true, rows: rows}
	a.touch()
}

// hopClose puts it away and leaves the person exactly where they were.
func (a *app) hopClose() {
	if !a.hop.open {
		return
	}
	a.hop = hopCard{}
	a.touch()
}

// hopReading is the card's whole reading: the conversations in the keeper,
// most recently in front first, and then the one on screen.
//
// IT WALKS THE PREVIOUS-STACK AND NEVER THE MAP. Go's map order is random, and a
// switcher whose rows moved between two presses of the same key would be
// unusable; [app.prev] is the order the keeper already keeps and the order `tab`
// already walks (keeper.go's [app.rememberOpen]), so the card and the key agree
// about what "the last one" means by construction.
func (a *app) hopReading() []hopRow {
	now := a.now()
	rows := make([]hopRow, 0, len(a.behind)+1)
	for at := len(a.prev) - 1; at >= 0; at-- {
		held := a.behind[a.prev[at]]
		if held == nil {
			// A key the keeper no longer has. Stepped over rather than cleaned
			// up, exactly as [app.lastBehind] steps over it.
			continue
		}
		rows = append(rows, a.hopKept(held, now))
	}
	rows = append(rows, a.hopFront(now))
	return rows
}

// hopKept is one row for a conversation this process holds but is not drawing.
func (a *app) hopKept(held *kept, now time.Time) hopRow {
	agent := held.conv.Agent
	running := runningTasks(agent)
	row := hopRow{
		file:    held.conv.SessionFile,
		title:   hopTitle(agent, held.side),
		project: hopProject(held.conv.Place, held.conv.Workspace),
		needs:   needsPerson(agent),
		moving:  running > 0,
	}
	if held.side != nil {
		row.age = sinceAt(held.side.since, now)
	}
	row.note = hopNote(row.needs, running, held.watch.landedSince())
	return row
}

// hopFront is the conversation on screen. It is a row like any other — same
// title, same project, same age — because a ring with a hole in it is a ring a
// person has to count their way around.
func (a *app) hopFront(now time.Time) hopRow {
	return hopRow{
		file:    a.file,
		// THE SURFACE'S OWN SPELLING FOR THE ONE ON SCREEN. It is what the status
		// line is showing this instant ([app.sessionName]), and a card that named
		// the conversation you are sitting in differently from the line at the
		// foot of the frame would be two names for one thing on one screen.
		title:   hopFrontName(a.sessionName()),
		project: hopProject(a.place, a.workspace),
		note:    hopHereWord,
		age:     sinceAt(a.frontAt, now),
		here:    true,
	}
}

// hopTitle is the conversation's name: the agent's own, then the one the surface
// was using when it was left, then the word for a conversation that has not been
// called anything yet.
//
// THE AGENT IS ASKED FIRST because a title goes on being groomed while nobody is
// watching, and a name copied into the sidecar would be the one it had at the
// moment somebody walked away from it.
//
// AND THE FILE NAME IS NEVER REACHED. The resume picker's ladder ends at the
// transcript's own name, which is right for a page somebody went to in order to
// choose between sessions; names.go states the rule this obeys instead — a
// session file is called `20260816-150405_a1b2c3`, and a card that offered that
// to a person as the name of their conversation would be worse than saying
// plainly that it has no name yet.
func hopTitle(agent Agent, side *aside) string {
	title := ""
	if agent != nil {
		title = strings.TrimSpace(agent.Title())
	}
	if title == "" && side != nil {
		title = strings.TrimSpace(side.title)
	}
	if title == "" {
		return hopNewWord
	}
	return readableName(title)
}

// hopNewWord is a conversation nothing has been said in yet. It is the phrase
// the entry line already uses for the same fact (app.go's `new conversation ·
// <place>`), because one thing has one name.
const hopNewWord = "new conversation"

// hopFrontName is that spelling with the empty case answered.
func hopFrontName(name string) string {
	if strings.TrimSpace(name) == "" {
		return hopNewWord
	}
	return name
}

// hopProject is the word at the right margin: the name the door gave this
// conversation's workspace, or the folder's own name where it gave none.
func hopProject(place, workspace string) string {
	if place = strings.TrimSpace(place); place != "" {
		return place
	}
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		return filepath.Base(workspace)
	}
	return ""
}

// hopNote is what changed since you last looked, and the order is worst news
// first — which is the order home's own rows are ranked in (switcher.go's
// [switcherRank]) and the order a person needs them in.
//
// IT NEVER GUESSES AT A QUESTION'S WORDS. Home can say "asks: …" because it
// reads the presence file the conversation wrote, which carries the question's
// text; this card is asking a live agent pointer a yes-or-no question and has
// nothing but the yes. Naming the tool it wants would mean a door onto the
// engine that does not exist, and inventing a sentence for it would be worse
// than the short true one.
func hopNote(needs bool, running, landed int) string {
	switch {
	case needs:
		return hopAskingWord
	case running > 0:
		return itoa(running) + " " + plural("task", running) + " running"
	case landed > 0:
		return hopLandedWord
	}
	return hopNothingWord
}

// runningTasks is how many nodes are turning in one conversation, asked of
// whatever agent this is.
//
// IT IS [app.behindTasks]'S OWN QUESTION ASKED OF ONE AGENT, and that function
// now asks it through this one — the assertion, the status string and the walk
// were about to exist twice, which is how the two answers drift.
func runningTasks(agent Agent) int {
	door, ok := agent.(interface {
		TaskIndex() []session.TaskIndexEntry
	})
	if !ok {
		return 0
	}
	running := 0
	for _, entry := range door.TaskIndex() {
		if entry.Status == string(session.TaskRunning) {
			running++
		}
	}
	return running
}

// ── the keyboard, while the card is up ──────────────────────────────────────

// hopKey is the switcher's whole claim on the keyboard: the key that OPENS it,
// and — while it is open — every key there is.
//
// IT TAKES EVERYTHING BUT `ctrl+c`, for the composer layer's reason
// (composerlayer.go's [app.composerLayerKey]): the card is a choice with the
// answers on screen, and a key that fell through it would be a key acting on a
// conversation the person is looking past. `ctrl+c` is excepted as it is at every
// modal on this surface — leaving is never modal — and it puts the card away on
// its way through.
func (a *app) hopKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if !a.hop.open {
		if !a.hopOpens(key) || !a.hopAvailable() {
			return nil, false
		}
		a.hopOpen()
		return nil, true
	}
	if key == "ctrl+c" {
		// Put away, and the key goes on to mean what it always means.
		a.hopClose()
		return nil, false
	}
	switch {
	case a.hopOpens(key), key == "down", key == "tab", key == "ctrl+n":
		a.hopWalk(1)
		return nil, true
	case key == "up", key == "shift+tab", key == "ctrl+p", key == hopBackAlias && a.ctrlDigits():
		a.hopWalk(-1)
		return nil, true
	case key == "enter":
		return a.hopTake(), true
	case key == "esc":
		a.hopClose()
		return nil, true
	}
	// A DIGIT TAKES ITS ROW OUTRIGHT. Eight is the cap (keeper.go's [convCap]),
	// so every row this card can hold has a digit, and the digit is drawn on it.
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		if at := int(key[0] - '1'); at < len(a.hop.rows) {
			a.hop.at = at
			return a.hopTake(), true
		}
		return nil, true
	}
	// ANYTHING ELSE PUTS IT AWAY AND IS SWALLOWED. A person who reached for a
	// key that means nothing here has stopped switching; the card goes, and the
	// keystroke is not also delivered to the conversation underneath, because a
	// letter that arrived in a draft on the way out of an overlay is a letter
	// nobody typed on purpose.
	a.hopClose()
	return nil, true
}

// hopOpens reports whether this key is a way in — the binding, or the alias on a
// terminal that answered the keyboard query.
func (a *app) hopOpens(key string) bool {
	return key == hopOpenKey || (key == hopAlias && a.ctrlDigits())
}

// hopWalk moves the cursor, wrapping at both ends. It wraps because this is a
// ring: a person who overshoots the row they wanted must not have to walk back
// through seven conversations to reach it.
func (a *app) hopWalk(by int) {
	n := len(a.hop.rows)
	if n == 0 {
		return
	}
	a.hop.at = ((a.hop.at+by)%n + n) % n
	a.touch()
}

// hopTake goes to the row under the cursor.
//
// THE ROW YOU ARE ALREADY ON IS NOT A SWITCH. Committing `you are here` closes
// the card and does nothing else — [app.bringForward] answers the same way for
// the same reason, and doing it here as well means the card never depends on
// that agreement holding.
func (a *app) hopTake() tea.Cmd {
	if a.hop.at < 0 || a.hop.at >= len(a.hop.rows) {
		a.hopClose()
		return nil
	}
	row := a.hop.rows[a.hop.at]
	a.hopClose()
	if row.here {
		return nil
	}
	cmd, ok := a.bringForward(row.file)
	if !ok {
		// The conversation went away between the card opening and this key —
		// another window took it over (takeover.go), or it was closed. The card
		// is already down; saying so is better than a keystroke that did nothing.
		a.note(hopGoneWord)
		return nil
	}
	return cmd
}

// hopGoneWord is what the switcher says about a row that stopped existing while
// it was on screen. It names no path: the person pressed a row, and the row is
// what they are being told about.
const hopGoneWord = "that conversation is no longer open"

// ── what the card looks like ────────────────────────────────────────────────

// hopHeadRoom is the head row plus the blank under it, and hopMinBody is the
// shortest body the card will draw itself into: the head, one row, and a row of
// air above and below. Anything tighter and the card would be a list with no
// room to say what it is, drawn over a conversation it has hidden.
const (
	hopHeadRoom = 2
	hopMinBody  = 5
	// hopInset is the air on each side of the card. One cell, which is this
	// surface's smallest step and all the separation a card needs when
	// everything around it is three tiers darker.
	hopInset = 1
)

// hopCardLines is the card itself — the head row, a blank, and one line per
// conversation — laid out to `width` and capped to `height` rows.
//
// A CARD THAT DOES NOT FIT DROPS ROWS FROM THE BOTTOM AND NEVER THE HEAD. The
// head is what says how many there are and which keys move; a list that ate it
// to show one more row would be a list a person cannot get out of.
func (a *app) hopCardLines(width, height int, pal palette) []string {
	if !a.hopShowing() || width < 1 || height < hopHeadRoom+1 {
		return nil
	}
	lines := []string{a.hopHead(width, pal), ""}
	for at, row := range a.hop.rows {
		if len(lines) >= height {
			break
		}
		lines = append(lines, hopLine(row, at, at == a.hop.at, width, pal))
	}
	return lines
}

// hopHead is the one line above the list: how many conversations are open on the
// left, and the keys that move on the right.
//
// THE KEYS ARE DROPPED FROM THE RIGHT AS THE FRAME TIGHTENS, in the order a
// person can most afford to lose them: the digits go first (every row they name
// is also reachable with the arrows), then `esc`, then the walk — and the count
// alone survives, because a card with no head is the one shape this refuses.
func (a *app) hopHead(width int, pal palette) string {
	left := itoa(len(a.hop.rows)) + " open"
	clauses := append([]string(nil), hopClauses...)
	for {
		right := strings.Join(clauses, " · ")
		if ansi.StringWidth(left)+2+ansi.StringWidth(right) <= width {
			pad := width - ansi.StringWidth(left) - ansi.StringWidth(right)
			return pal.dim(left) + strings.Repeat(" ", max(1, pad)) + pal.dim(right)
		}
		if len(clauses) == 0 {
			break
		}
		clauses = clauses[:len(clauses)-1]
	}
	return pal.dim(fit(left, width))
}

// hopLine is one conversation, drawn the way home draws one (switcher.go's
// [switcherLine]): the state glyph, the subject in the body ink, and a dim tail
// of what changed, which project it is, and how long ago you left it.
//
// THE TAIL IS DROPPED IN HOME'S OWN ORDER as the frame tightens — the note
// first, then the project, then the age — so the two lists narrow the same way.
func hopLine(row hopRow, at int, sel bool, width int, pal palette) string {
	glyph, glyphInk := tokens.GlyphQueued, pal.dim
	switch {
	case row.needs:
		glyph, glyphInk = tokens.GlyphNeedsHuman, pal.warn
	case row.moving:
		glyph, glyphInk = tokens.GlyphWorking, pal.accent
	}
	// THE DIGIT IS DRAWN BECAUSE THE DIGIT IS BOUND. A row a person can take with
	// `3` and is never told about is a key that does nothing until somebody
	// guesses, which is the clause SCREEN 3a forbids.
	lead := pal.dim(itoa(at+1)) + " " + glyphInk(glyph) + " "
	parts := []string{row.note, row.project, row.age}
	for hopTailWidth(parts)+ansi.StringWidth(lead)+8 > width {
		if parts[0] != "" {
			parts[0] = ""
			continue
		}
		if parts[1] != "" {
			parts[1] = ""
			continue
		}
		if parts[2] != "" {
			parts[2] = ""
			continue
		}
		break
	}
	tail := hopTailWidth(parts)
	room := max(0, width-ansi.StringWidth(lead)-tail)
	// THE SUBJECT GOES BOLD ON THE ROW THE KEYBOARD IS ON and the tail steps up
	// with it, for switcher.go's reason exactly: dim grey on a raised ground is
	// grey on grey.
	name, tailInk := pal.ink(fit(row.title, room)), pal.dim
	if sel {
		name, tailInk = pal.bold(name), pal.ink
	}
	line := lead + name
	if pad := width - ansi.StringWidth(line) - tail; pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	for _, part := range parts {
		if part != "" {
			line += " " + tailInk(part)
		}
	}
	line = fit(line, width)
	if !sel {
		return line
	}
	return pal.cursor(line, width)
}

func hopTailWidth(parts []string) int {
	n := 0
	for _, part := range parts {
		if part != "" {
			n += 1 + ansi.StringWidth(part)
		}
	}
	return n
}

// hopOver is the whole of how the card meets the surface underneath: the body's
// own rows are repainted at the faintest stop of the depth ladder, and the
// card's lines are written over the middle of them.
//
// THE ROWS ARE REPLACED RATHER THAN THE FRAME BEING REBUILT, which is what makes
// the card cost nothing: the body was going to be laid out anyway, the frame is
// the same height it was before the key was pressed, and no place, room or
// transcript has a single line about being underneath one.
//
// It is one function over `[]string` because both surfaces that call it — the
// conversation (view.go) and the places (pages.go) — hold their body as rows of
// text with marks beside them, and the marks are the caller's business: a card
// row carries no hit, because a click on it is a click on the card.
func (a *app) hopOver(body []string, width int, pal palette) []string {
	// THE CARD IS INSET BY A CELL ON EACH SIDE, and that one cell is the whole of
	// what makes it read as an object rather than as a band of text that happens
	// to be brighter. It matters most at the right margin, where the roster's
	// column stands: a head row laid out to the full body width put `esc back`
	// hard against the `│`, which reads as the card having grown into the rail.
	room := width - 2*hopInset
	card := a.hopCardLines(room, len(body)-2, pal)
	if len(card) == 0 || len(body) < hopMinBody {
		// Too short to lay a card into. The body is still faded — the card is up,
		// and a surface that dimmed nothing would be a surface where the keys the
		// card owns are being pressed at a page that looks live.
		return hopFadeAll(body, pal)
	}
	out := hopFadeAll(body, pal)
	// CENTRED, AND NUDGED UP BY A THIRD. Dead centre puts the head row below the
	// middle of the frame on a tall window, which reads as low; a third of the
	// way down is where a person's eye already is on a page of prose.
	top := (len(out) - len(card)) / 3
	if top < 1 {
		top = 1
	}
	if top+len(card) > len(out) {
		top = len(out) - len(card)
	}
	pad := strings.Repeat(" ", hopInset)
	for i, line := range card {
		out[top+i] = pad + line
	}
	return out
}

// hopFadeAll repaints every row of a body at the faintest stop of the depth
// ladder. It is [composerFade] over a whole body, and it goes through that same
// function rather than reaching for [palette.fade] itself, so the two layers on
// this surface can never end up at two different depths.
func hopFadeAll(body []string, pal palette) []string {
	out := make([]string, len(body))
	for i, line := range body {
		out[i] = composerFade(line, pal)
	}
	return out
}

// hopMaybe lays the card over a body that is already a list of lines, and gives
// the lines straight back when the card is down. It is the shape the roster's
// full-width branch needs (view.go) and the shape a place's body needs
// (pages.go), which is why it is one function rather than two `if`s.
func (a *app) hopMaybe(body []string, width int) []string {
	if !a.hopShowing() {
		return body
	}
	return a.hopOver(body, width, a.pal)
}

// hopFadeRail dims the roster's column while the card is up.
//
// IT IS DONE ROW BY ROW WHERE THE COLUMN IS JOINED, because the rail is not part
// of the body: it is a second column drawn beside it, and a card that dimmed the
// conversation and left the roster at full ink would say the roster was still
// live — which is exactly what it is not while the switcher holds the keyboard.
func (a *app) hopFadeRail(line string) string {
	if !a.hopShowing() {
		return line
	}
	return composerFade(line, a.pal)
}

// hopMapWords names the switcher on a place's map — the one line on a place
// whose job is to say what the keys are (pages.go's [app.placeHintSaid] states
// why it is that line and not the resting foot).
const hopMapWords = hopOpenKey + " switch conversation"

// hopClauses are the four keys the card owns, in the order a person meets them,
// and they are a list rather than a sentence because the head row DROPS them
// from the right as the frame tightens ([app.hopHead]).
var hopClauses = []string{"tab down", "shift+tab up", "enter go", "esc back"}

// hopFootWords is the foot of the frame while the card is up: the head row's
// clauses again, from the same list, so a person reading the bottom of the
// screen and a person reading the top of the card are told the same four things.
var hopFootWords = strings.Join(hopClauses, " · ")
