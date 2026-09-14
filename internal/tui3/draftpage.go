package tui3

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE PAGE OVER THE KILL RING: /drafts, the box's lost sentences as a list.
//
// ↑ walks what you SENT (recall.go) and visits the ring in front of it, dim;
// this page is the same ring as its own screen — the door for the person who
// knows it was more than a step or two back, or who wants the whole ten to
// choose from rather than walk for the one. What it lists is written by
// [app.noteKilled] at every whole-box clear (draftring.go says what lands and
// what never does), and the two readers share one rule: the list is newest
// first and a restored line LEAVES the ring, because a line back in the box
// is no longer lost.
//
// ENTER RESTORES OVER WHAT IS THERE, AND IT NEVER THROWS AWAY. A box with
// words in it when the page opens is a draft too, so restoring pushes it onto
// the ring before the chosen line takes the box — the act of recovering one
// sentence cannot cost another. That is the ring's own law (push's comment
// says why an empty box records nothing) applied to the one clear this page
// performs itself.

// draftHeading is the page's one heading line: what is listed, and what enter
// and d do, because this list's two keys are the half of it nobody can guess
// (the hint line says them too; the heading is where a person reading the
// page itself finds them).
const draftHeading = "drafts you cleared · enter puts one back · d lets one go"

// draftEmptyWord is THE ONE LINE the page draws with nothing in the ring. The
// emptiness law is about facts unknown at draw time; an empty ring is a KNOWN
// fact — the clear a person is looking for never happened — and a page that
// answered /drafts with blank space would read as a page that failed to open
// rather than as a page saying so.
const draftEmptyWord = "no cleared draft is waiting"

// draftPageRows is the most of the ring the page ever draws under the heading:
// the ring's own cap, so the page never scrolls to reach something the walk
// could reach first.
const draftPageRows = draftRingSize

// draftPanel is the page's whole state. The zero value is closed.
type draftPanel struct {
	open    bool
	entries []draftEntry
	cursor  int
	top     int
	// owner maps each screen line back to the entry that drew it, written at
	// layout for the pointer — the same bargain the registry panels make
	// (connectpanel.go, permissions.go). The heading answers to no entry.
	owner []int
}

func (p *draftPanel) close() { *p = draftPanel{} }

func (p *draftPanel) start(entries []draftEntry) {
	*p = draftPanel{open: true, entries: entries}
}

// adopt takes a re-read of the same ring under the cursor, after a drop
// shortened it. The cursor holds its PLACE rather than its entry, because the
// entry it was on is the one that just went away and the next thing a person
// wants is whatever moved up into its position.
func (p *draftPanel) adopt(entries []draftEntry) {
	p.entries = entries
	p.cursor = moveCursor(p.cursor, 0, len(p.entries))
	p.follow(draftPageRows)
}

func (p *draftPanel) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.entries))
	p.follow(draftPageRows)
}

func (p *draftPanel) follow(height int) {
	p.top = listTop(p.cursor, p.top, len(p.entries), height)
}

// label is one entry's own half of its row: the unsent mark and the words as
// they stood. Both are DIM, because they were never sent — the same ink the
// ↑ walk gives them (app.draftInk), so the two doors onto the ring read as
// one thing.
func (p *draftPanel) label(index int, pal palette) string {
	if index < 0 || index >= len(p.entries) {
		return ""
	}
	return pal.dim(pal.glyph(tokens.GDraftUnsent) + " " + string(p.entries[index].text))
}

// note is the dim tail, and it is the AGE and nothing else: the ring's
// entries are otherwise identical in kind, so the one fact a row can carry
// that another cannot is how long it has been sitting here.
func (p *draftPanel) note(index int) string {
	if index < 0 || index >= len(p.entries) {
		return ""
	}
	return ago(p.entries[index].at)
}

// height is how many lines the page wants: the heading, plus the entries.
// With nothing in the ring it wants the heading and the one dim line, which
// is the emptiness law drawn — never a blank page.
func (p *draftPanel) height(width int) int {
	if !p.open {
		return 0
	}
	if len(p.entries) == 0 {
		return 2
	}
	return 1 + overlayWindow(width, p.top, len(p.entries), draftPageRows, p.note)
}

func (p *draftPanel) draw(width, n int, pal palette, hover int) []string {
	if n <= 0 || !p.open {
		return nil
	}
	fill := newOverlayFill(width, n, pal, hover)
	// THE HEADING IS A [overlayFill.plain] LINE, which is what makes it
	// unpressable without anything downstream having to know it is a heading:
	// plain records the line as belonging to no entry, and [app.draftPagePress]
	// already swallows those (permissions.go made the same bargain first).
	fill.plain(pal.dim(fit("  "+draftHeading, width)))
	if len(p.entries) == 0 {
		fill.plain(pal.dim(fit("  "+draftEmptyWord, width)))
	} else {
		p.follow(overlayItems(n-1, width))
		for at := p.top; at < len(p.entries) && fill.room(); at++ {
			if !fill.add(at, p.label(at, pal), p.note(at), at == p.cursor, false) {
				break
			}
		}
	}
	lines, owner := fill.done()
	// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED (palette.go's
	// [overlayFill.done] says why). The heading is what makes the promise
	// breakable here: [draftPanel.height] counted it against the top the last
	// frame left behind, and the scroll above may have moved that top since.
	for len(lines) < n {
		lines = append(lines, "")
		owner = append(owner, -1)
	}
	p.owner = owner
	return lines
}

// ── the app's side ──────────────────────────────────────────────────────────

// openDrafts is /drafts.
//
// The entries are read HERE and not held from boot, on the terms /permissions
// reads its rows: another clear can have landed since the page was last open,
// and asking costs one slice copy of ten.
func (a *app) openDrafts() {
	a.closeLists()
	a.dismissWelcome()
	a.draftPage.start(a.drafts.list())
	a.touch()
}

// draftRestore is enter, and the click that means the same thing: the entry
// under the cursor takes the box, and leaves the ring — a line back in the
// box is a line no longer lost. What was in the box goes onto the ring first
// (the page's header says why), unless the box holds exactly the words being
// restored, which is what happens when the ring was browsed from the ↑ walk
// and this is the same sentence coming home.
func (a *app) draftRestore(at int) {
	p := &a.draftPage
	if at < 0 || at >= len(p.entries) {
		return
	}
	words := string(p.entries[at].text)
	live := a.input.String()
	a.drafts.drop(at)
	if words != live {
		a.drafts.push([]rune(live))
	}
	p.close()
	a.input.setText(words)
	a.syncLists()
	a.touch()
}

// draftDrop is d: the entry under the cursor is let go for good. No second
// press, because the line is not thrown away the way a permission line is —
// it stays typed history nowhere, but it was also never anything but a clear
// the person already chose once.
func (a *app) draftDrop(at int) {
	p := &a.draftPage
	if at < 0 || at >= len(p.entries) {
		return
	}
	a.drafts.drop(at)
	p.adopt(a.drafts.list())
	a.touch()
}

// draftPageKey routes one keypress while the page owns the keyboard.
func (a *app) draftPageKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.draftPage
	switch msg.String() {
	case "esc":
		p.close()
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.move(-(draftPageRows - 1))
	case "pgdown":
		p.move(draftPageRows - 1)
	// enter is the list grammar's act and d is the word for it. The letter is
	// free here in a way it is not on a filterable list: nothing on this page
	// is typed into, so a bare key can mean what it says (permissions.go's
	// panel set the precedent and the reason).
	case "enter":
		a.draftRestore(p.cursor)
	case "d":
		a.draftDrop(p.cursor)
	}
	a.touch()
	return nil
}

// draftPagePress resolves a click on one of the page's rows.
func (a *app) draftPagePress(y int) tea.Cmd {
	p := &a.draftPage
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
		// The heading, the empty line, or a blank under the last row: a line
		// belonging to no entry. It is swallowed rather than acted on —
		// pressing a word must not act on whichever entry it happened to be
		// nearest.
		return nil
	}
	// The pointer moves the cursor before it acts, so the entry restored is
	// the one the press was aimed at, and the row a person can see under their
	// hand is the row enter would have taken.
	if at != p.cursor {
		p.cursor = at
	}
	a.draftRestore(at)
	return nil
}
