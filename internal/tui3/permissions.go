package tui3

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
)

// THE PERMISSIONS PANEL: /permissions.
//
// The consent card (consent.go) is where a person says "always" about one call.
// It is a good place to say yes and a terrible place to remember having said it:
// the card scrolls away, and what it wrote lives in two settings rows a person
// has to go looking for. This panel is the other half of that sentence — WHAT
// HAVE I ALREADY ANSWERED, and let me take one back — which is a LIST, the shape
// this surface already knows how to draw:
//
//	  what runs without asking
//	  $ git status*
//	  ◇ read                                                          every call
//
// Four decisions, and three of them are borrowed rather than invented:
//
//   - IT IS THE OVERLAY GRAMMAR (palette.go), exactly as /connect and /harness
//     are: a short list under the draft, ↑↓, enter, esc. A second list with its
//     own manners would be a second thing to learn for a question of the same
//     shape.
//   - THE HEADING IS THE PANEL'S ONE SENTENCE and there is never a second one.
//     With nothing banked it draws the heading and NOTHING under it — not "no
//     exceptions", not "0 rules", not a paragraph explaining what could be here.
//     An empty list is already the whole answer, and a surface that narrates its
//     own emptiness is a surface that made a person read a sentence to learn
//     nothing.
//   - TWO GLYPHS AND A NAME. A '$' where the answer is about one shell command
//     and a hollow mark where it is about a whole tool, because those two are not
//     the same size of promise and the difference is the first thing a person
//     scanning this list needs. The dim tail carries the one fact the heading did
//     not already say — the breadth of a tool row, or the answer of a row that is
//     not an allow — and nothing at all when the heading said it.
//   - ENTER ASKS ONCE AND DROPS ON THE SECOND PRESS, which is what /connect's
//     disconnect does and for the same reason: what is on this list is what a
//     person deliberately banked, and one keystroke is not enough of a decision
//     to throw it away.
//
// THE HEADING IS A TITLE AND NOT A CLAIM ABOUT EVERY ROW. Nearly everything that
// lands here is an allow — the card never writes a deny (consent.go) — so the
// panel is named after what it nearly always contains, and the rare hand-written
// deny or prompt says so in its own tail rather than being filed under a word
// that is wrong for it.
//
// THE ROWS ARE THE PERSON'S OWN AND NOT THE POLICY IN FORCE, and that is the one
// surprising consequence worth stating here: inside a repository that answers
// tools.approval its row replaces the person's wholesale at launch
// (config/projectconfig.go, law 2), so dropping a line here changes what this
// person carries everywhere and changes nothing inside that repository. The
// panel offers what a person can take back, which is exactly what they put there.

// permRowsMax is how many LINES the panel takes at most, the heading among them
// — the same ceiling every bottom-anchored list on this surface has.
const permRowsMax = 10

// permHeading is the panel's one sentence, drawn whether or not anything is
// under it.
const permHeading = "what runs without asking"

// The dim tails a row can carry. An allow on a shell command carries NONE: the
// heading already said what it is, and a column of rows all saying "allowed"
// would be the heading repeated once per line.
const (
	permDenyWord   = "refused"
	permPromptWord = "asks every time"
	permToolWord   = "every call"
	permArmWord    = "enter again to drop it"
)

// The two marks. The shell one needs no screen-reader twin because it already is
// one — '$' is ASCII and reads as a command line in every tier this surface has.
const (
	glyphPermTool      = "◇"
	glyphPermToolASCII = "o"
	glyphPermBash      = "$"
)

// The sentences this panel says in the transcript rather than in the list: a
// settings row somebody wrote that does not read back, and the receipt for a
// drop. A broken row is reported and never redrawn as an empty list, because
// "there is nothing here" and "I cannot read what is here" are different facts.
const (
	badBashRowWord = "the shell command rules do not read back · "
	badToolRowWord = "the tool exceptions do not read back · "
	droppedWord    = "dropped · "
	// nextSessionWord is the narrower truth, appended when the running gate
	// could not be told (see [app.dropPermission]).
	nextSessionWord = " · from the next session"
)

// permKind is which of the two settings rows a line came from.
type permKind int

const (
	permBash permKind = iota
	permTool
)

// permRule is one banked answer as the panel draws it, and as it drops it.
//
// at is the line's INDEX in the shell rules row and is what a drop removes. It
// is an index and not a match because that row's order is the author's priority
// statement (config/approvalmemory.go) — two lines may carry the same glob, and
// dropping "the one that says git status*" would then be dropping whichever one
// a loop reached first.
type permRule struct {
	kind   permKind
	name   string
	action string
	at     int
}

// mark is the row's glyph.
func (r permRule) mark(pal palette) string {
	if r.kind == permBash {
		return glyphPermBash
	}
	if pal.linear {
		return glyphPermToolASCII
	}
	return glyphPermTool
}

// permPanel is the overlay's whole state. The zero value is closed.
type permPanel struct {
	open   bool
	rows   []permRule
	cursor int
	top    int
	// armed says a second enter on the CURSOR's row drops it. It is a flag and
	// not an index because moving the cursor is what disarms it: the row a
	// person can see asking is always the row they are on.
	armed bool
	// owner maps each screen line back to the row that drew it, written at
	// layout for the pointer — the same bargain the other two panels make
	// (connectpanel.go, harnesspanel.go). The heading answers to no row.
	owner []int
}

func (p *permPanel) close() { *p = permPanel{} }

func (p *permPanel) start(rows []permRule) {
	*p = permPanel{open: true, rows: rows}
}

// adopt takes a re-read of the same list under the cursor, after a drop changed
// it. The cursor holds its PLACE rather than its row, because the row it was on
// is the one that just went away and the next thing a person wants to look at is
// whatever moved up into its position.
func (p *permPanel) adopt(rows []permRule) {
	p.rows, p.armed = rows, false
	p.cursor = moveCursor(p.cursor, 0, len(p.rows))
	p.follow(permRowsMax - 1)
}

func (p *permPanel) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.rows))
	// A walk is not a decision. Leaving a row armed while the cursor moved off
	// it would leave the panel holding a question about a row nobody is looking
	// at any more.
	p.armed = false
	p.follow(permRowsMax - 1)
}

func (p *permPanel) follow(height int) {
	p.top = listTop(p.cursor, p.top, len(p.rows), height)
}

// at resolves one row.
func (p *permPanel) at(index int) (permRule, bool) {
	if index < 0 || index >= len(p.rows) {
		return permRule{}, false
	}
	return p.rows[index], true
}

// label is the row's own half: the mark and the thing it answers for.
func (p *permPanel) label(index int, pal palette) string {
	row, ok := p.at(index)
	if !ok {
		return ""
	}
	return pal.muted(row.mark(pal)) + " " + row.name
}

// note is the dim tail, and it is empty for the row this panel is mostly made
// of. See the constants above for why.
func (p *permPanel) note(index int) string {
	row, ok := p.at(index)
	if !ok {
		return ""
	}
	if p.armed && index == p.cursor {
		return permArmWord
	}
	switch approval.Action(row.action) {
	case approval.ActionDeny:
		return permDenyWord
	case approval.ActionPrompt:
		return permPromptWord
	}
	if row.kind == permTool {
		return permToolWord
	}
	return ""
}

// height is how many lines the overlay wants: the heading, plus whatever the
// rows take. An open panel with no rows wants exactly ONE, which is the
// emptiness law drawn — the heading, and nothing under it.
func (p *permPanel) height(width int) int {
	if !p.open {
		return 0
	}
	return 1 + overlayWindow(width, p.top, len(p.rows), permRowsMax-1, p.note)
}

func (p *permPanel) draw(width, n int, pal palette, hover int) []string {
	if n <= 0 || !p.open {
		return nil
	}
	fill := newOverlayFill(width, n, pal, hover)
	// THE HEADING IS A [overlayFill.plain] LINE, which is what makes it
	// unpressable without anything downstream having to know it is a heading:
	// plain records the line as belonging to row -1, and [app.permPanelPress]
	// already swallows -1.
	fill.plain(pal.dim(fit("  "+permHeading, width)))
	p.follow(overlayItems(n-1, width))
	for at := p.top; at < len(p.rows) && fill.room(); at++ {
		if !fill.add(at, p.label(at, pal), p.note(at), at == p.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED (palette.go's
	// [overlayFill.done] says why). The heading is what makes the promise
	// breakable here: [permPanel.height] counted it against the top the last
	// frame left behind, and the scroll above may have moved that top since.
	for len(lines) < n {
		lines = append(lines, "")
		owner = append(owner, -1)
	}
	p.owner = owner
	return lines
}

// ── the app's side ──────────────────────────────────────────────────────────

// openPermissions is /permissions.
//
// The rows are read HERE and not held from boot, on the terms /connect and
// /harness read theirs: a card answered in another window five minutes ago wrote
// a line this list has to know about, and asking costs one small file read.
func (a *app) openPermissions() {
	if a.hosted() {
		a.note(a.remoteProfileWord("permissions"))
		return
	}
	a.closeLists()
	a.dismissWelcome()
	rows, trouble := a.permissionRules()
	a.permPanel.start(rows)
	if trouble != "" {
		// It goes in the TRANSCRIPT and not in the list. A row somebody wrote
		// that does not read back is a thing to go and fix in /settings, which
		// is prose; the list stays the list.
		a.note(trouble)
	}
	a.touch()
}

// permissionRules resolves the two settings rows into the list, and reports
// whatever could not be read.
//
// THE SHELL RULES KEEP THE ORDER THEY WERE WRITTEN IN and the tool exceptions
// are sorted, and that difference is not a slip: the shell row's order is the
// author's priority statement — first match wins — so reordering it here would
// be the panel telling a person something untrue about which line answers first.
// The tool row is a map with no order of its own, so it gets the one order that
// is the same on every read.
func (a *app) permissionRules() ([]permRule, string) {
	var (
		rows    []permRule
		trouble []string
	)
	if rules, err := config.ParseBashApprovals(config.BashApprovalsAt(a.profileDir)); err != nil {
		trouble = append(trouble, badBashRowWord+err.Error())
	} else {
		for at, rule := range rules {
			rows = append(rows, permRule{kind: permBash, name: rule.Match, action: rule.Action, at: at})
		}
	}
	if tools, err := config.ParseToolApprovals(config.ToolApprovalsAt(a.profileDir)); err != nil {
		trouble = append(trouble, badToolRowWord+err.Error())
	} else {
		names := make([]string, 0, len(tools))
		for tool := range tools {
			names = append(names, tool)
		}
		sort.Strings(names)
		for _, tool := range names {
			rows = append(rows, permRule{kind: permTool, name: tool, action: tools[tool]})
		}
	}
	return rows, strings.Join(trouble, "\n")
}

// permPanelKey routes one keypress while the panel owns the keyboard.
func (a *app) permPanelKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.permPanel
	switch msg.String() {
	case "esc":
		// ESC UNDOES ONE THING AT A TIME, nearest first: the question standing
		// on a row, and only then the panel. A dismiss key that closed the whole
		// overlay while a person could still see something smaller to dismiss
		// would be throwing away something they can see (connectpanel.go).
		if p.armed {
			p.armed = false
			break
		}
		p.close()
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.move(-(permRowsMax - 1))
	case "pgdown":
		p.move(permRowsMax - 1)
	// enter is the list grammar's act and d is the word for it. The letter is
	// free here in a way it is not on a filterable list: nothing on this panel
	// is typed into, so a bare key can mean what it says.
	case "enter", "d":
		a.permissionAct(p.cursor)
	}
	a.touch()
	return nil
}

// permPanelPress resolves a click on one of the panel's rows.
func (a *app) permPanelPress(y int) tea.Cmd {
	p := &a.permPanel
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		// A press anywhere else closes it, which is what pressing outside a
		// modal list means everywhere on this surface.
		p.close()
		a.touch()
		return nil
	}
	at := -1
	if mark.index >= 0 && mark.index < len(p.owner) {
		at = p.owner[mark.index]
	}
	if at < 0 {
		// The heading, or a blank under the last row: a line belonging to no
		// rule. It is swallowed rather than acted on — pressing a word must not
		// act on whichever row it happened to be nearest.
		return nil
	}
	// The pointer moves the cursor before it acts, so a mis-aimed press arms the
	// question on the row a person can see armed.
	if at != p.cursor {
		p.cursor, p.armed = at, false
	}
	a.permissionAct(at)
	a.touch()
	return nil
}

// permissionAct is enter, d, and the click that means the same thing: ask once,
// drop on the second press.
func (a *app) permissionAct(at int) {
	p := &a.permPanel
	row, ok := p.at(at)
	if !ok {
		return
	}
	if !p.armed {
		p.cursor, p.armed = at, true
		return
	}
	p.armed = false
	if err := a.dropPermission(row); err != nil {
		// A REFUSAL IS SAID OUT LOUD, unlike the card's failed write
		// (consent.go). That one is dropped because nobody asked for it — the
		// answer had already been given and the write was a bonus. This one IS
		// the errand: a person pressed a key to take something back, and silence
		// after it would be the surface pretending to have done something.
		a.note(err.Error())
		return
	}
	rows, _ := a.permissionRules()
	p.adopt(rows)
	receipt := droppedWord + row.name
	if !a.approvalsReloaded() {
		receipt += nextSessionWord
	}
	a.note(receipt)
}

// dropPermission writes the row back without this line.
//
// It goes through the settings registry rather than through config's writers
// directly, so a row the environment has pinned refuses here exactly as it
// refuses in the settings sheet ([config.Setting.Apply]) — one door, one answer.
func (a *app) dropPermission(row permRule) error {
	key, text := config.KeyToolApprovals, ""
	if row.kind == permBash {
		rules, err := config.ParseBashApprovals(config.BashApprovalsAt(a.profileDir))
		if err != nil {
			return err
		}
		if row.at < 0 || row.at >= len(rules) {
			return fmt.Errorf("%q is no longer on the list", row.name)
		}
		key = config.KeyBashApprovals
		// The three-index slice is what keeps this from writing over the line
		// after the one being dropped.
		text = config.FormatBashApprovals(append(rules[:row.at:row.at], rules[row.at+1:]...))
	} else {
		text = dropToolApproval(config.ToolApprovalsAt(a.profileDir), row.name)
	}
	entry, ok := a.registry().Row(key)
	if !ok {
		return fmt.Errorf("%q cannot be changed here", key)
	}
	return entry.Apply(text)
}

// dropToolApproval removes one tool's entry and leaves the others in the order
// they were written. It is config's own replacePair with the middle taken out,
// and it lives here because it is a thing this panel does to a row rather than a
// thing the row does to itself.
func dropToolApproval(raw, tool string) string {
	entries := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if name, _, found := strings.Cut(entry, ":"); found && strings.TrimSpace(name) == tool {
			continue
		}
		out = append(out, entry)
	}
	return strings.Join(out, ", ")
}

// approvalsReloaded tells the RUNNING gate that the person's approval rows have
// changed, and reports whether it heard.
//
// A NIL SEAM IS NOT A FAILURE, it is a narrower truth. The line is out of the
// person's config either way, so the next session is right; what a surface
// without the seam cannot promise is that the conversation already in flight
// stops honouring the line it just deleted. So the receipt says "from the next
// session" instead of claiming an effect nothing here delivered — the same
// honesty the consent card practises when its own write fails and the row says
// "allowed" rather than "saved" (consent.go).
func (a *app) approvalsReloaded() bool {
	if a.applyApprovals == nil {
		return false
	}
	return a.applyApprovals() == nil
}
