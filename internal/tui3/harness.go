package tui3

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE HARNESS PANEL: /harness, and the chip that appears while one runs.
//
// A sub-harness is built in conversation and approved on a card there
// (internal/session's tools_harness.go), which answers "may this be kept". What
// the conversation cannot answer is the question people ask a day later — WHAT
// HAVE I GOT, and what did it do last time — because the card that said so has
// scrolled away. That question is a LIST, which is a thing this surface already
// knows how to draw:
//
//	  ◆ triage-flake       v3 · 6 runs · last ok, 2h ago
//	  ◆ review-diff        v1 · never run
//
// Three decisions, and all three are borrowed rather than invented:
//
//   - IT IS THE OVERLAY GRAMMAR (palette.go), exactly as /connect is: a short
//     list under the draft, ↑↓, enter, esc. A second list with its own manners
//     would be a second thing to learn for a question of the same shape.
//   - ENTER PRINTS THE CARD INTO THE CONVERSATION rather than opening a second
//     overlay. The card is prose — numbered steps, the bounds under them — and
//     the transcript is where prose lives on this surface, where it can be
//     scrolled, copied and read beside the work it is about. An overlay would
//     be a worse text viewer than the one already on screen.
//   - THE RUNNING ONE IS A CHIP ON THE STRIP (taskstrip.go), not a bar of its
//     own. The strip is already the row that says WHAT IS ALIVE; a harness run
//     is alive for minutes at a time and belongs on it, in front of the tasks,
//     with the panel as its door.
//
// THE PANEL READS THE DISK AND THE AGENT SEPARATELY, and that is not an
// accident. What is REGISTERED is a directory, which any window may have
// written since this one booted, so the list is resolved on the keystroke that
// opens it. What is RUNNING is this session's own business, so it is polled off
// the agent while the frame is drawn.

// harnessRowsMax is how many LINES the panel takes at most — the same ceiling
// every bottom-anchored list on this surface has.
const harnessRowsMax = 10

// The sentences this surface says when it cannot show a list.
const (
	harnessUnavailableWord = "harnesses are unavailable here"
	noHarnessWord          = "no harnesses are registered yet — build one in the conversation"
)

// harnessRow is one registered harness as the panel draws it: what the entry
// says, plus what its history says, resolved once when the list opens.
type harnessRow struct {
	harness subharness.Harness
	// runs is how many traces are on disk and last is the newest, if any.
	runs int
	last subharness.Run
	// hasLast distinguishes "never run" from "ran and the trace is unreadable",
	// which is the emptiness law again: one of those is a fact worth drawing and
	// the other is a shrug.
	hasLast bool
}

// harnessPanel is the overlay's whole state. The zero value is closed.
type harnessPanel struct {
	open   bool
	rows   []harnessRow
	cursor int
	top    int
	// owner maps each screen line back to the row that drew it, written at
	// layout for the pointer — the same bargain the connections panel makes
	// (connectpanel.go).
	owner []int
}

func (p *harnessPanel) close() { *p = harnessPanel{} }

func (p *harnessPanel) start(rows []harnessRow) {
	*p = harnessPanel{open: true, rows: rows}
}

func (p *harnessPanel) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.rows))
	p.follow(harnessRowsMax)
}

func (p *harnessPanel) follow(height int) {
	p.top = listTop(p.cursor, p.top, len(p.rows), height)
}

// at resolves one row.
func (p *harnessPanel) at(index int) (harnessRow, bool) {
	if index < 0 || index >= len(p.rows) {
		return harnessRow{}, false
	}
	return p.rows[index], true
}

// choice is the harness under the cursor.
func (p *harnessPanel) choice() (harnessRow, bool) {
	if !p.open {
		return harnessRow{}, false
	}
	return p.at(p.cursor)
}

// label is the row's own half: the identity mark, the name, and the version.
// The version rides on the LEFT with the name because it is part of what this
// thing is — "triage-flake v3" is the sentence somebody says out loud — while
// the history on the right is what it has done.
func (p *harnessPanel) label(index int, pal palette) string {
	row, ok := p.at(index)
	if !ok {
		return ""
	}
	mark := glyphHarness
	if pal.linear {
		mark = glyphHarnessASCII
	}
	return fmt.Sprintf("%s %s v%d", pal.muted(mark), row.harness.Name, row.harness.Version)
}

// note is the dim tail: what this harness is for, and what its history says.
//
// The description comes first because it is what a person is scanning for, and
// the history is appended only when there IS one — a row that said "never run"
// beside every other row's timing would be a column of apologies.
func (p *harnessPanel) note(index int) string {
	row, ok := p.at(index)
	if !ok {
		return ""
	}
	parts := []string{}
	if note := strings.TrimSpace(row.harness.Description); note != "" {
		parts = append(parts, note)
	}
	switch {
	case row.runs == 0:
		parts = append(parts, "never run")
	case row.hasLast:
		parts = append(parts, fmt.Sprintf("%s · last %s %s",
			countedRuns(row.runs), row.last.Status, harnessAgo(row.last.Started)))
	default:
		parts = append(parts, countedRuns(row.runs))
	}
	return strings.Join(parts, " · ")
}

func countedRuns(n int) string {
	if n == 1 {
		return "1 run"
	}
	return fmt.Sprintf("%d runs", n)
}

// harnessAgo is a coarse "when" — this row is scanned, not audited, and the
// exact timestamp is in the trace.
func harnessAgo(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	since := time.Since(at)
	switch {
	case since < time.Minute:
		return "just now"
	case since < time.Hour:
		return fmt.Sprintf("%dm ago", int(since.Minutes()))
	case since < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(since.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(since.Hours()/24))
}

// height is how many lines the overlay wants.
func (p *harnessPanel) height(width int) int {
	switch {
	case !p.open:
		return 0
	case len(p.rows) == 0:
		return 1
	}
	return overlayWindow(width, p.top, len(p.rows), harnessRowsMax, p.note)
}

func (p *harnessPanel) draw(width, n int, pal palette, hover int) []string {
	if n <= 0 || !p.open {
		return nil
	}
	if len(p.rows) == 0 {
		p.owner = []int{-1}
		return []string{pal.dim(fit("  "+noHarnessWord, width))}
	}
	p.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := p.top; at < len(p.rows) && fill.room(); at++ {
		if !fill.add(at, p.label(at, pal), p.note(at), at == p.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	p.owner = owner
	return lines
}

// ── the app's side ──────────────────────────────────────────────────────────

// openHarness is /harness.
//
// The registry is read HERE and not held from boot, on the terms /connect reads
// its services: a harness registered in another window ten minutes ago is a
// harness this list has to know about, and asking costs a directory walk.
func (a *app) openHarness() {
	if a.harn == nil {
		a.note(harnessUnavailableWord)
		return
	}
	a.closeLists()
	a.dismissWelcome()
	a.harnPanel.start(a.harnessRows())
	a.touch()
}

// harnessRows resolves the registry into rows. A registry that cannot be read
// at all draws the empty list rather than an error: the panel's whole job is to
// say what there is, and "there is nothing I can see" is that answer.
func (a *app) harnessRows() []harnessRow {
	entries, err := a.harn.List()
	if err != nil {
		return nil
	}
	rows := make([]harnessRow, 0, len(entries))
	for _, entry := range entries {
		row := harnessRow{harness: entry}
		if paths, err := a.harn.Runs(entry.Name); err == nil {
			row.runs = len(paths)
			if len(paths) > 0 {
				if run, err := a.harn.LoadRun(paths[0]); err == nil {
					row.last, row.hasLast = run, true
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// harnessPanelKey routes one keypress while the panel owns the keyboard.
func (a *app) harnessPanelKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.harnPanel
	switch msg.String() {
	case "esc":
		p.close()
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.move(-harnessRowsMax)
	case "pgdown":
		p.move(harnessRowsMax)
	case "enter":
		a.harnessAct(p.cursor)
	}
	a.touch()
	return nil
}

// harnessPanelPress resolves a click on one of the panel's rows.
func (a *app) harnessPanelPress(y int) tea.Cmd {
	p := &a.harnPanel
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
		return nil
	}
	if at != p.cursor {
		p.cursor = at
	}
	a.harnessAct(at)
	a.touch()
	return nil
}

// harnessAct is enter, and the click that means the same thing: the card, in
// the conversation, and the panel gets out of the way.
//
// THE RUN HISTORY RIDES WITH IT when there is one. "What is this shape" and
// "what did it do last time" are one question asked twice, and the answer to
// the second is three lines under the answer to the first.
func (a *app) harnessAct(at int) {
	row, ok := a.harnPanel.at(at)
	if !ok {
		return
	}
	a.harnPanel.close()
	card := subharness.Card(row.harness)
	if row.hasLast {
		card += "\n\nlast run\n" + subharness.RunCard(row.last)
	}
	a.note(card)
}

// ── the chip ────────────────────────────────────────────────────────────────

// harnessLive is the narrow slice of the session a running chip needs. It is an
// interface asserted on the agent rather than a method on [Agent] because a
// surface that cannot answer it should simply not draw the chip — and every
// test fake in this package is that surface.
type harnessLive interface {
	RunningHarness() (string, bool)
}

// runningHarness is the name of the harness this session is executing, and
// false when it is executing none.
func (a *app) runningHarness() (string, bool) {
	live, ok := a.agent.(harnessLive)
	if !ok {
		return "", false
	}
	return live.RunningHarness()
}

// harnessChip is the strip's leading cell while a run is in flight: the spinner
// and the harness's name, in the shape every other chip on that row has
// (taskstrip.go). It returns the painted chip and the CELLS it occupies, which
// is the pair the strip's budget is spent in.
func (a *app) harnessChip(name string) (string, int) {
	glyph := a.pal.accent(tokens.Spinner(a.paints / spinnerStep))
	if a.linear {
		glyph = a.pal.accent(glyphRunASCII)
	}
	mark := a.linearMark(glyphHarness, glyphHarnessASCII)
	title := fit(name, stripTitleCap)
	cols := ansi.StringWidth(glyph) + 1 + ansi.StringWidth(mark) + 1 + ansi.StringWidth(title) + stripPadCols
	return stripPad + glyph + " " + a.pal.muted(mark) + " " + a.pal.accent(title) + stripPad, cols
}
