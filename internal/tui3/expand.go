package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// THE PHONE'S TOOL DETAIL.
//
// A tool row at tierPhone is one compact line and hangs nothing under itself
// (toolview.go). Its expansion is this: the SAME gesture — a tap on the row,
// enter on the selected one — opens the call over the whole frame instead.
//
//	 edit                                esc close
//	 internal/session/loop.go
//	 ───────────────────────────────────────────────
//	 @@ -1,4 +1,4 @@
//	   // argsLimit bounds Event.Args.
//	 -const argsLimit = 400
//	 +const argsLimit = 8192
//	 ───────────────────────────────────────────────
//	 esc close · ↑↓ scroll
//
// The reason it is a sheet and not an inline block is arithmetic. An expansion
// under a row is drawn at the row's width minus the rail's stem, which at
// forty-four columns is forty-two cells for a unified diff, inside a page that
// also holds the conversation, the strip and the draft. Nothing in that block
// is readable and none of it can be scrolled to on its own. Over the whole
// frame the same rows get every column and every line the terminal has, and
// they get a scroll position of their own.
//
// It is the SETTINGS PANEL'S SHAPE and not a second kind of overlay
// (settings.go): one function builds every row of the frame and says what each
// of them answers to the pointer, because a frame drawing one geometry while
// the pointer resolves another is how a tap lands on the wrong line. It is
// modal for the keyboard and for the pointer for the same reason that one is:
// there is nothing else on the screen to send a key to.
//
// EVERY TAP TARGET IS A WHOLE ROW. There is no close button to aim at, no
// hover state carrying anything, and no gap that falls through to the body
// underneath — the head and the foot both close, and the "… N more lines" row
// lifts the cap exactly as it does inline.

// expand is the sheet's whole state. The zero value is closed, which is what
// it is almost always, and closed it costs the frame nothing.
type expand struct {
	open bool
	// entry indexes the deck the BODY was drawing when the sheet opened
	// (render.go's [app.bodyDeck]) — the room's page while one is open, the
	// conversation otherwise. It is re-resolved every frame rather than
	// snapshot, because a running call's output is still arriving and a sheet
	// showing the payload as it stood at the tap would stop at "running".
	entry int
	// offset is the first body row on screen. It is the sheet's own scroll and
	// not the conversation's: the transcript under it has not moved.
	offset int
}

// phoneFrame reports whether the body region is being laid out at tierPhone.
//
// It asks [app.bodyWidth] rather than the terminal's width because that is the
// number the rows were actually built at (task.go), and a door that opened on
// one answer while the rows were drawn at the other would be a tap that opened
// a sheet over an expansion that is also on screen.
func (a *app) phoneFrame() bool { return layoutTier(a.bodyWidth()) == tierPhone }

// expandShowing reports whether the sheet is on the frame RIGHT NOW, which is
// the question every router below asks — the frame, the keys, the wheel, the
// tap.
//
// It is the open flag AND the width, because a terminal that is dragged wider
// while the sheet is up has stopped being the tier this sheet exists for: the
// call is expanded inline from that moment, exactly as it would have been had
// it been opened there, and the state stays so that narrowing again brings the
// sheet back rather than closing the call. One derived answer rather than a
// flag somebody has to remember to clear on resize.
func (a *app) expandShowing() bool { return a.expand.open && a.phoneFrame() }

// openExpand raises the sheet over one call. It is what [app.openTool] does at
// tierPhone, and it is reached by the same two gestures for the same reason:
// "show me this call" is one act, and which shape the answer takes is a
// question about the frame's width rather than about the person's intent.
//
// The entry's own open flag is set as well as the sheet's index. It is what a
// WIDER frame would have used — resize the terminal with the sheet up, close
// it, and the call is expanded inline where it always was — and it is the
// weak check [app.expandEntry] resolves against.
func (a *app) openExpand(i int) {
	es := a.bodyDeck().entries
	if i < 0 || i >= len(es) || es[i].kind != entryTool {
		return
	}
	es[i].open = true
	a.expand = expand{open: true, entry: i}
	if a.room != nil {
		a.room.dirty = true
	} else {
		a.sel = i
	}
	a.touch()
}

// closeExpand puts the sheet away and takes the expansion with it: a call
// closed here is closed at every width, which is the same rule
// [app.openTool] keeps about a lifted cap.
func (a *app) closeExpand() {
	if !a.expand.open {
		return
	}
	if es := a.bodyDeck().entries; a.expand.entry >= 0 && a.expand.entry < len(es) {
		es[a.expand.entry].open = false
		es[a.expand.entry].full = false
	}
	a.expand = expand{}
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}

// expandEntry is the call the sheet is about, or false when there is no longer
// one — the deck was rebuilt shorter, or the row it named is not a tool call
// any more. A sheet with nothing behind it draws nothing and the frame falls
// back to the conversation, which is the safe way round: an overlay that
// insisted on staying up over a call that had gone would be a trap.
func (a *app) expandEntry() (*entry, bool) {
	if !a.expandShowing() {
		return nil, false
	}
	es := a.bodyDeck().entries
	if a.expand.entry < 0 || a.expand.entry >= len(es) || es[a.expand.entry].kind != entryTool {
		return nil, false
	}
	return &es[a.expand.entry], true
}

// ── the frame ───────────────────────────────────────────────────────────────

// expandHit is what one row of the sheet answers to a tap.
type expandHit uint8

const (
	expandHitNone  expandHit = iota
	expandHitClose           // the head and the foot: both are the way out
	expandHitOriginal
	expandHitMore // the "… N more lines" row: lift the cap
)

// expandFoot is what the sheet spends on its own foot: the rule, and the line
// that names the keys.
const expandFoot = 2

// expandFrame is the whole screen while the sheet is up: exactly the rows the
// terminal has, what each of them answers to the pointer, and where the caret
// sits. It is one function for [app.sheetFrame]'s reason — the frame draws
// these rows and the pointer resolves against them, and two answers to "which
// row is the foot" is a tap that closes a sheet somebody meant to scroll.
func (a *app) expandFrame(width, height int) ([]string, []expandHit, int, int) {
	e, ok := a.expandEntry()
	if !ok {
		return nil, nil, 0, 0
	}
	pal := a.pal
	lines := make([]string, 0, height)
	hits := make([]expandHit, 0, height)
	add := func(text string, hit expandHit) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	name, fallback := toolWords(e.tool, e.text)
	add(expandTitle(width, name, pal), expandHitClose)
	// THE TARGET WHOLE, WRAPPED RATHER THAN FITTED. The row that opened this
	// sheet showed a basename; "elided from what" is the first question the
	// sheet exists to answer, so the path is given every line it needs instead
	// of being cut a second time.
	//
	// bash is the exception and it is not an omission: its target IS the
	// command, and the command is the first block of the body below — whole,
	// wrapped and highlighted (shellx.go). Printing it here as well would be
	// the sheet answering one question twice.
	if e.tool != "bash" {
		target := toolTarget(e.tool, e.detail.Args, e.text)
		if target == "" {
			target = fallback
		}
		for _, line := range wrap(target, width-2) {
			add(" "+pal.ink(line), expandHitClose)
		}
	}
	add(pal.dim(rule(width)), expandHitNone)

	room := height - len(lines) - expandFoot
	if room < 1 {
		room = 1
	}

	// The body is the SAME rows the inline expansion draws, from the same
	// per-tool table (toolstat.go's windows, toolview.go's [app.detailBody]).
	// A sheet with a renderer of its own would be a second rendering of the
	// same payload, and a second rendering diverges.
	body, more := a.detailBody(e, width-1)
	kinds := make([]expandHit, len(body))
	if picturesAFile(e.tool) {
		for i, line := range body {
			if pictureOriginalRow(line) {
				kinds[i] = expandHitOriginal
			}
		}
	}
	if more > 0 {
		body = append(body, pal.dim(glyphMore+" "+itoa(more)+" more lines"))
		kinds = append(kinds, expandHitMore)
	}
	a.expand.offset = clampTop(a.expand.offset, len(body), room)
	for i := 0; i < room; i++ {
		at := a.expand.offset + i
		if at >= len(body) {
			// Padding, and it answers to NOTHING. A gap that fell through to
			// whatever is drawn under an overlay is a tap that did something
			// the person could not see coming.
			add("", expandHitNone)
			continue
		}
		add(" "+body[at], kinds[at])
	}

	add(pal.dim(rule(width)), expandHitNone)
	add(" "+pal.dim(fit(expandKeys(len(body) > room), width-2)), expandHitClose)

	// A terminal too short for the whole sheet keeps its head and its foot:
	// what this is, and how to leave (the same cut [app.sheetFrame] makes).
	if len(lines) > height && height > 1 {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
	}
	return lines, hits, 0, 0
}

// expandTitle is the head: the tool's own name on the left, the way out on the
// right. The whole row closes, so the words are a label rather than a target
// somebody has to hit.
func expandTitle(width int, name string, pal palette) string {
	left := " " + pal.bold(pal.ink(name))
	right := "esc close"
	gap := width - 1 - ansi.StringWidth(name) - len(right) - 1
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + pal.dim(right)
}

// expandKeys is the foot's sentence. It names the cap only when there is one to
// lift: a key printed for something that is not on screen is a key that has to
// be read to learn nothing.
func expandKeys(scrolls bool) string {
	if scrolls {
		return "esc close · ↑↓ scroll · tap … for the rest"
	}
	return "esc close · ↑↓ scroll"
}

// clampTop bounds a scroll position to a list of a length inside a window of a
// height. It is [listTop] without a cursor to follow: this sheet is read, not
// walked, so the offset is the only thing that moves.
func clampTop(top, count, height int) int {
	if bottom := count - height; top > bottom {
		top = bottom
	}
	if top < 0 {
		return 0
	}
	return top
}

// ── the gestures ────────────────────────────────────────────────────────────

// expandKey routes one keypress while the sheet is up. Everything that is not
// a way out or a way down is dropped rather than passed under, because there is
// nothing under it to pass to (input.go's key order).
func (a *app) expandKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "alt+o", "o":
		return a.openPictureAt(a.expand.entry, 0)
	case "esc", "q", "left":
		a.closeExpand()
	case "enter":
		// The key that opened it closes it, which is what enter means on every
		// other expandable thing this surface draws.
		a.closeExpand()
	case "up", "k":
		a.expandScroll(-1)
	case "down", "j":
		a.expandScroll(1)
	case "pgup", "ctrl+b":
		a.expandScroll(-a.expandPage())
	case "pgdown", "ctrl+f", " ", "space":
		a.expandScroll(a.expandPage())
	case "home", "g":
		a.expand.offset = 0
		a.touch()
	case "end", "G":
		// A very large offset is clamped by [expandFrame] on the way out, which
		// is the one place that knows how long the body is.
		a.expand.offset = 1 << 20
		a.touch()
	case "ctrl+o":
		a.showAll(a.expand.entry)
	}
	return nil
}

// expandPage is a screenful of the sheet, one row shy so a page turn keeps a
// line of context — the same courtesy [app.page] pays the conversation.
func (a *app) expandPage() int {
	_, height := a.size()
	if page := height - expandFoot - 3; page > 1 {
		return page
	}
	return 1
}

func (a *app) expandScroll(delta int) {
	a.expand.offset += delta
	if a.expand.offset < 0 {
		a.expand.offset = 0
	}
	a.touch()
}

// expandPress resolves a tap on the sheet. Every row was laid out with what it
// answers to, so this is a lookup rather than a second geometry.
func (a *app) expandPress(y int) tea.Cmd {
	width, height := a.size()
	_, hits, _, _ := a.expandFrame(width, height)
	if y < 0 || y >= len(hits) {
		return nil
	}
	switch hits[y] {
	case expandHitOriginal:
		return a.openPictureAt(a.expand.entry, 0)
	case expandHitClose:
		a.closeExpand()
	case expandHitMore:
		a.showAll(a.expand.entry)
	}
	return nil
}
