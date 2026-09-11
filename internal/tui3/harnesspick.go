package tui3

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE HARNESS PICKER: /harness with a space after it, and every saved shape of
// work is a row you can type at.
//
// The panel next door (harnesspanel.go) answers "what have I got". This answers
// the other half of the same errand — I KNOW WHICH ONE I WANT, RUN THAT ONE —
// and until it existed there was no way to say it. A harness reached you by
// being detected: you described the work, a matcher scored your sentence, and a
// card asked whether you meant it. That is the right road for somebody who does
// not know the registry has the thing they are describing, and it is a maze for
// somebody who does. Cues are not even saved to disk, so after a restart the
// only sentence that reliably raises the card is one that NAMES the harness.
//
// So: a picker, and the choice sits in a chip while you write the request.
//
//	/harness            the panel, as it always was — browse the cards
//	/harness <query>    the picker: filter, ↑↓, enter, and the name is a chip
//	<the request>       enter runs THAT harness on exactly what you typed
//
// Four decisions, and three of them are borrowed rather than invented:
//
//   - IT IS SYNCED OFF THE DRAFT, not modal like the model picker. The query is
//     the text after "/harness " in the box a person is already typing in — the
//     command list's own arrangement (commands.go), because this is the same
//     gesture continued rather than a second thing that opened over it.
//   - IT IS THE OVERLAY GRAMMAR (palette.go): a short list under the draft, ↑↓,
//     enter, esc, and the scoring is the model picker's own [tokenScore].
//   - THE CHOICE IS A CHIP IN THE TRAY (attach.go), not text in the draft. A
//     harness name written into the sentence would be a word the harness then
//     has to read past — the request is what the run is asked to do, and the
//     name is not part of it. The tray is where this surface already keeps
//     "what the next message carries besides its words".
//   - THE WORDS THAT REACH IT ARE THE /harness ROW'S OWN (commands.go), read off
//     that row rather than listed again here. `/subharness` and `/sub` used to be
//     among them and are not any more: a subharness is now a typed program with
//     an input schema and an intake card (subharness.go), which is a different
//     thing from a saved shape of work, and one word for two things is a word
//     nobody could rely on.

// harnessPickRows is how many LINES the picker takes at most, the ceiling every
// bottom-anchored list on this surface has.
const harnessPickRows = 10

// harnessBrowseWord is the last row, always: the door back to the cards. It is
// a row rather than a hint because the panel is a real answer to "I do not
// recognize any of these names" — and a person who opened this list by typing
// is a person whose hands are on the keys, not on the slash they would have to
// retype.
const harnessBrowseWord = "Browse the registry"

// harnessPickHint is the tray's dim tail beside a chosen harness: what to do
// next, said where the thing that is waiting is drawn.
const harnessPickHint = "— type the request · enter runs it"

// harnessChipCap is how much of a name the chip may spend. It is the strip's
// own cap plus a little, because this chip is alone on its row rather than one
// of six on a shared one (taskstrip.go's [stripTitleCap]).
const harnessChipCap = 24

// The chip's two marks. The glyph is a COG rather than the registry's diamond:
// the diamond means "a thing this session is carrying" wherever it appears
// (styles.go), and what is in the tray is not a thing being carried — it is a
// tool that has been picked up and is about to be used.
const (
	glyphHarnessChip      = "⚙"
	glyphHarnessChipASCII = "*"
	// glyphChipDrop is the way off. It is the same ✕ every other stoppable
	// thing on this surface is closed by (stop.go).
	// It is the vocabulary's cross (tokens.GFailed) spent as a DISMISSAL rather
	// than as a finding — the same byte doing a second job, told apart by where
	// it sits, exactly as the shared table's own `$` is.
	glyphChipDrop = tokens.GlyphFailed
)

// harnessPickRow is one harness as this list ranks and draws it: what the page
// says, and when it was last anything.
type harnessPickRow struct {
	name string
	desc string
	// when is the row's RECENCY, and it is one stamp with two sources: the last
	// run's start when there has been one, and otherwise the moment the page was
	// written. A harness saved this morning and never run is a latest harness,
	// which is the whole word this list is sorted by.
	when time.Time
	// ran distinguishes those two, because they are drawn with different words
	// and the emptiness law forbids inventing a timing for a thing that has
	// never happened.
	ran bool
	// last is that run as every list in this product spells it
	// ([subharness.LastRunLine]). The zero value is a page nobody has run, and it
	// draws nothing.
	last subharness.LastRun
}

// note is the dim tail: what it is for, and what the last run did.
//
// THE LAST RUN IS SPELLED WHERE BOTH DOORS CAN REACH IT and never here: this
// list, the panel behind it and `/subharness` all say the same fact about the
// same program, and three spellings of it is three lists that behave alike until
// the day one of them is edited. `never run` is gone with the same move — the
// emptiness law: a program nobody has run says what it is for and stops.
func (r harnessPickRow) note() string {
	parts := make([]string, 0, 2)
	if desc := strings.TrimSpace(r.desc); desc != "" {
		parts = append(parts, desc)
	}
	if line := subharness.LastRunLine(r.last, time.Now()); line != "" {
		parts = append(parts, line)
	}
	return strings.Join(parts, " · ")
}

// harnessPick is the picker's whole state. The zero value is closed.
type harnessPick struct {
	open bool
	// rows are the registry as it was when the list opened, in recency order.
	// It is resolved on the keystroke and not held from boot, for the panel's
	// reason: another window may have saved a harness a minute ago.
	rows []harnessPickRow
	// score is per-row scratch, reused across keystrokes.
	score []int
	// hits are indexes into rows, in rank order. The browse row is not in here
	// — it is drawn after them and never filtered away, because a list that
	// filtered its own way out would be a door that closes as you walk at it.
	hits []int
	// query is what has been typed after the command, folded once.
	query string

	cursor int
	top    int
	// owner maps each screen line back to the row that drew it, written at
	// layout for the pointer — the bargain every panel on this surface makes.
	owner []int
}

func (p *harnessPick) close() { *p = harnessPick{} }

// count is how many rows the list has, the browse row included: it is a row
// like any other as far as the cursor is concerned.
func (p *harnessPick) count() int { return len(p.hits) + 1 }

// browsing reports whether the cursor is on the last row — the one that opens
// the panel instead of choosing anything.
func (p *harnessPick) browsing() bool { return p.cursor == len(p.hits) }

// at resolves a row index to the harness it draws, and false for the browse
// row.
func (p *harnessPick) at(index int) (harnessPickRow, bool) {
	if index < 0 || index >= len(p.hits) {
		return harnessPickRow{}, false
	}
	return p.rows[p.hits[index]], true
}

// choice is the harness under the cursor, and false when the cursor is on the
// browse row or the filter matched nothing.
func (p *harnessPick) choice() (harnessPickRow, bool) { return p.at(p.cursor) }

// start opens the list over rows already in recency order.
func (p *harnessPick) start(rows []harnessPickRow, query string) {
	*p = harnessPick{open: true, rows: rows}
	p.score = make([]int, len(rows))
	p.rank(query)
}

// rank narrows the list to the query: case-insensitive, EVERY TOKEN MUST MATCH,
// and each token matches in one of the model picker's three tiers — prefix,
// then substring, then subsequence ([tokenScore]). It is that scoring and not a
// second one, because "which of these names did I mean" is the same question in
// both lists and two answers to it would be two lists that behave alike until
// the day they do not.
//
// WHAT IS SCORED IS THE NAME AND THE SENTENCE UNDER IT. The cue list a designer
// wrote would be the third thing worth matching, and it is not on the page: a
// harness page has nowhere to put its cues, so the registry on disk carries name
// and description and nothing else. Scoring what is there is the honest half.
//
// TIES KEEP RECENCY, which is what makes the empty query — the list as it opens
// — read as "the latest harnesses" rather than as an alphabet.
func (p *harnessPick) rank(query string) {
	p.query = query
	tokens := strings.Fields(strings.ToLower(query))
	p.hits = p.hits[:0]
	for i, row := range p.rows {
		if len(tokens) == 0 {
			p.hits = append(p.hits, i)
			continue
		}
		hay := strings.ToLower(row.name + " " + row.desc)
		total, matched := 0, true
		for _, token := range tokens {
			score, hit := tokenScore(hay, token)
			if !hit {
				matched = false
				break
			}
			total += score
		}
		if !matched {
			continue
		}
		p.score[i] = total
		p.hits = append(p.hits, i)
	}
	if len(tokens) > 0 {
		sort.SliceStable(p.hits, func(a, b int) bool { return p.score[p.hits[a]] < p.score[p.hits[b]] })
	}
	// A changed query is a changed list, and a cursor left at row nine of the
	// old one points at nothing anybody chose (palette.go says it first).
	p.cursor, p.top = 0, 0
}

func (p *harnessPick) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, p.count())
	p.follow(harnessPickRows)
}

func (p *harnessPick) follow(height int) {
	p.top = listTop(p.cursor, p.top, p.count(), height)
}

// label is a row's own half: the mark and the name. The browse row carries an
// arrow instead, because it is a door and not a thing.
func (p *harnessPick) label(index int, pal palette) string {
	row, ok := p.at(index)
	if !ok {
		if pal.ascii || pal.linear {
			return harnessBrowseWord + " ->"
		}
		return harnessBrowseWord + " →"
	}
	mark := glyphHarness
	if pal.linear {
		mark = glyphHarnessASCII
	}
	return pal.muted(mark) + " " + row.name
}

// note is the row's dim tail, and nothing for the browse row: the arrow on its
// label already says what it does.
func (p *harnessPick) note(index int) string {
	row, ok := p.at(index)
	if !ok {
		return ""
	}
	return row.note()
}

// height is how many lines the overlay wants.
func (p *harnessPick) height(width int) int {
	if !p.open {
		return 0
	}
	return overlayWindow(width, p.top, p.count(), harnessPickRows, p.note)
}

func (p *harnessPick) draw(width, n int, pal palette, hover int) []string {
	if n <= 0 || !p.open {
		return nil
	}
	p.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := p.top; at < p.count() && fill.room(); at++ {
		if !fill.add(at, p.label(at, pal), p.note(at), at == p.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	p.owner = owner
	return lines
}

// ── the app's side: opening it off the draft ────────────────────────────────

// harnessPickWords are the commands that open this list. They are the /harness
// row's own name and aliases (commands.go) rather than a second list beside it,
// so a word added to the table reaches the picker without anybody remembering
// to add it here twice.
func harnessPickWords() []string {
	words := []string{"harness"}
	for _, c := range commands {
		if c.name == "harness" {
			words = append(words, c.alias...)
		}
	}
	return words
}

// harnessPickQuery reads the draft as this list reads it: the text after
// "/harness ", and false for every line that is not one of these commands with
// a space after it.
//
// THE SPACE IS THE DOOR. A bare "/harness" is the command list's row and opens
// the panel on enter, which is the arrangement every other command with an
// argument keeps: the bare word is one thing and the word with something after
// it is another (see [app.runMenu]).
func harnessPickQuery(line string) (string, bool) {
	if !strings.HasPrefix(line, "/") || strings.Contains(line, "\n") {
		return "", false
	}
	word, rest, spaced := strings.Cut(line[1:], " ")
	if !spaced {
		return "", false
	}
	word = strings.ToLower(word)
	for _, other := range harnessPickWords() {
		if word == other {
			return rest, true
		}
	}
	return "", false
}

// syncHarnessPick opens, narrows or closes the picker from what is in the
// draft, and reports whether it is up. It is called from [app.syncLists] after
// the command list has had its say, because the two can never be open together:
// the command list closes on the space that opens this one.
func (a *app) syncHarnessPick() bool {
	query, ok := harnessPickQuery(a.input.String())
	if !ok {
		a.harnPick.close()
		return false
	}
	// A SURFACE WITH NO REGISTRY HAS NO LIST, and says so in the one sentence
	// /harness has always said rather than opening an empty overlay
	// (harnesspanel.go). It says it once, when the picker would have opened.
	if a.harn == nil {
		return false
	}
	if !a.harnPick.open {
		a.harnPick.start(a.harnessPickList(), query)
		return true
	}
	if query != a.harnPick.query {
		a.harnPick.rank(query)
	}
	return true
}

// harnessPickList resolves the registry into rows, newest first.
//
// It reads the HEAD of every name and the NEWEST of its runs, which is exactly
// what the panel reads ([app.harnessRows]) — and it is a second walk rather
// than a shared one because the two lists want different halves of it: the
// panel wants the version and the run count, and this wants one timestamp to
// sort by.
func (a *app) harnessPickList() []harnessPickRow {
	if a.harn == nil {
		return nil
	}
	names, err := a.harn.Names()
	if err != nil {
		return nil
	}
	rows := make([]harnessPickRow, 0, len(names))
	for _, name := range names {
		harness, err := a.harn.Load(name, 0)
		if err != nil {
			continue
		}
		row := harnessPickRow{name: harness.Id.Name, desc: harness.Id.Desc}
		if paths, err := a.harn.Runs(name); err == nil && len(paths) > 0 {
			// Runs come back oldest first — the stamp is the filename — so the
			// newest is the last one.
			if trace, err := a.harn.LoadRun(paths[len(paths)-1]); err == nil {
				row.when, row.ran = trace.Started, true
				row.last = subharness.TraceRun(trace)
			}
		}
		if !row.ran {
			row.when = harnessWritten(a.harn, name)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].when.Equal(rows[j].when) {
			return rows[i].when.After(rows[j].when)
		}
		// Nothing to compare by is the store's own answer: name order, which is
		// what Names() already handed over.
		return rows[i].name < rows[j].name
	})
	return rows
}

// harnessWritten is when a harness that has never run last had a page written
// for it, or the zero time when the disk will not say.
//
// It stats the harness's own DIRECTORY rather than a version page, and that is
// deliberate: the page filename is the store's law (`v3.json`, and the head is
// the highest one present), and a second spelling of it out here would be a
// second source of truth that drifts the day the store changes its mind. A page
// is written by linking it into this directory, so the directory's own mtime
// moves with it.
func harnessWritten(store *subharness.Store, name string) time.Time {
	info, err := os.Stat(filepath.Join(store.Dir(), name))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// ── the keys ────────────────────────────────────────────────────────────────

// harnessPickKey routes one keypress while the picker is up. It takes only the
// keys that MOVE and COMMIT the list: everything else falls through to the
// editor, because the person is still typing the query into their own draft
// (commands.go states the law for both typed overlays).
func (a *app) harnessPickKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up", "ctrl+p":
		a.harnPick.move(-1)
		a.touch()
		return nil, true
	case "down", "ctrl+n":
		a.harnPick.move(1)
		a.touch()
		return nil, true
	case "pgup":
		a.harnPick.move(-harnessPickRows)
		a.touch()
		return nil, true
	case "pgdown":
		a.harnPick.move(harnessPickRows)
		a.touch()
		return nil, true
	case "esc":
		// The list closes and the draft is left exactly as it was typed. A
		// dismiss key that also took the sentence away would be a key nobody
		// presses twice.
		a.harnPick.close()
		a.touch()
		return nil, true
	case "enter":
		return a.harnessPicked(), true
	}
	return nil, false
}

// harnessPicked is enter on the list, and the click that means the same thing:
// the harness under the cursor becomes the chip, the command comes out of the
// draft, and the box is ready for the request.
//
// The browse row is the one that does something else — it opens the panel,
// which is the card browser this list deliberately did not become.
func (a *app) harnessPicked() tea.Cmd {
	if !a.harnPick.open {
		return nil
	}
	if a.harnPick.browsing() {
		a.harnPick.close()
		a.input.reset()
		a.openHarness()
		return a.edited()
	}
	row, ok := a.harnPick.choice()
	if !ok {
		// Nothing matched what was typed. The list closes and the line is still
		// a line — "/harness nonsense" gets whatever answer it always had.
		a.harnPick.close()
		return nil
	}
	a.harnPick.close()
	// ONE CHIP AT A TIME. Picking a second harness replaces the first, because
	// the chip is the answer to "which one", and two answers to that is not a
	// state this surface can run.
	a.harnChip = row.name
	a.input.reset()
	a.touch()
	return a.edited()
}

// harnessPickPress resolves a click on one of the picker's rows, and reports
// whether it took the press. A click anywhere else falls through untouched:
// this list is not modal, and the conversation under it is still a conversation.
func (a *app) harnessPickPress(y int) (tea.Cmd, bool) {
	if !a.harnPick.open {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		return nil, false
	}
	at := -1
	if mark.index >= 0 && mark.index < len(a.harnPick.owner) {
		at = a.harnPick.owner[mark.index]
	}
	if at < 0 {
		return nil, false
	}
	a.harnPick.cursor = at
	return a.harnessPicked(), true
}

// ── the chip ────────────────────────────────────────────────────────────────

// harnessChipMark is the tray's glyph for a picked harness.
func harnessChipMark(pal palette) string {
	if pal.ascii || pal.linear {
		return glyphHarnessChipASCII
	}
	return glyphHarnessChip
}

// harnessTrayCells is the harness chip's cells on the row above the box: the
// chip itself, which a click takes off, and the dim hint after it saying what
// to do next. Nothing at all when no harness is picked.
//
// The name is ELLIPSIZED rather than wrapped or dropped: a chip is a reminder
// of a choice already made, and half of "release-notes-weekly" still names it.
func (a *app) harnessTrayCells() []string {
	if a.harnChip == "" {
		return nil
	}
	drop := glyphChipDrop
	if a.pal.ascii || a.pal.linear {
		drop = "x"
	}
	chip := harnessChipMark(a.pal) + " " + fit(a.harnChip, harnessChipCap) + " " + drop
	return []string{chip, harnessPickHint}
}

// harnessTrayWidth is what those cells occupy, gaps included — the offset the
// picture chips beside them start at ([app.chipPress]).
func harnessTrayWidth(cells []string) int {
	width := 0
	for _, cell := range cells {
		width += ansi.StringWidth(cell) + len(chipGap)
	}
	return width
}

// dropHarnessChip takes the picked harness off the tray, and reports whether it
// changed anything. It is the ✕, and it is backspace on an empty box once the
// pictures are gone (input.go) — the same gesture as deleting the character
// behind the caret, applied to the last thing left to delete.
func (a *app) dropHarnessChip() bool {
	if a.harnChip == "" {
		return false
	}
	a.harnChip = ""
	a.touch()
	return true
}

// ── running it ──────────────────────────────────────────────────────────────

// harnessRunner is the narrow slice of the session a picked harness needs. It
// is asserted on the agent rather than added to [Agent], on [harnessLive]'s
// terms and for [designAgent]'s reason: a session that cannot run a harness
// should say so, and every scripted agent in this package's own tests is one.
type harnessRunner interface {
	// RunHarnessRequest runs one named harness on one request and streams the
	// turn it becomes (internal/session's harness.go). It bypasses detection
	// entirely: the person chose the name off a list, so there is nothing left
	// to match and nothing to ask.
	RunHarnessRequest(ctx context.Context, name, text, model string) (<-chan session.Event, error)
}

// runPickedHarness is enter with a chip in the tray and a sentence in the box:
// that harness, on exactly those words.
//
// THE CHIP CLEARS ON SUBMIT. A harness is picked for a request, not for a
// conversation — leaving it up would make the next thing typed run the same
// procedure again, which is the one mistake a surface holding a choice for you
// must not make.
//
// It goes through [app.submitting] rather than assembling a turn of its own, so
// the person's line lands in the transcript, the clock starts and the stream is
// pumped exactly as every other turn's is. What differs is one call.
//
// PICTURES IN THE TRAY STAY THERE. A harness run is handed words and nothing
// else (internal/session's Config.RunHarness), so an attachment cannot travel
// with one — and dropping it silently would lose a file somebody went and
// found. It waits in the tray for the next ordinary message, which is the same
// thing a refused submit does with one (attach.go).
func (a *app) runPickedHarness(text string) tea.Cmd {
	name := a.harnChip
	a.harnChip = ""
	runner, ok := a.agent.(harnessRunner)
	if !ok {
		// A capability that cannot work is absent rather than broken: the chip
		// is gone, the sentence is not sent anywhere, and the surface says the
		// same thing /harness says on a surface with no registry.
		a.note(harnessUnavailableWord)
		a.touch()
		return nil
	}
	ctx := a.ctx
	return a.submitting(text, func() (<-chan session.Event, error) {
		// No model is named here. A word chosen in the box travels on the turn's
		// own text for the offer lane to read; the picker chose a harness, not a
		// model, and inventing one would be the surface answering a question
		// nobody asked (internal/session's harnessTurnModel).
		return runner.RunHarnessRequest(ctx, name, text, "")
	})
}
