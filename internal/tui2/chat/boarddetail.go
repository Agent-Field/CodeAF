package chat

import (
	"image"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The charter and service detail pages.
//
// A ROW ON THIS PAGE OPENS A FULL PAGE, NOT A FOLD (notebook-split.md §3). The
// reason is that neither of these things has a room: a job has a conversation
// and a record and its door is [App.jumpTo], but a charter's whole history is
// five judgments and a service's is ten lines of log, and there is no transcript
// to take a reader to. A fold would have been the other option and it is the
// wrong one — the body is six to twenty lines of prose and receipts, which is a
// page's worth of reading pushed into a list that a reader is scanning.
//
// THE GRAMMAR IS THE NOTEBOOK'S, so the two pages that opened out of two
// different lists read as one product: trail · title with its receipt · body ·
// verbs. Only the body varies by kind.
//
// ESC GOES BACK TO THE ROW IT LEFT. The list's cursor is remembered on the way
// in and restored on the way out, which is the whole of "scrolled to the row it
// left": the list has no scroll of its own (the cursor derives the window), so
// restoring the cursor restores the view exactly.

// boardDetailLogLines and boardDetailLogBytes are the service log tail's bounds,
// named here as the SURFACE's ask. The engine's own defaults are the same
// numbers (command.ServiceLogTailLines / Bytes) and the two are pinned equal by
// a test, for the reason every twin in this tree is: a surface that asked for
// twelve and got ten would be a card silently disagreeing with its own read.
const (
	boardDetailLogLines = command.ServiceLogTailLines
	boardDetailLogBytes = command.ServiceLogTailBytes
)

// boardFirings is how many judgments the charter page shows. Five, because
// notebook-split.md §3 says five and because a firing history is a SAMPLE — it
// answers "has this been behaving", not "what has it ever done".
const boardFirings = 5

// The verb registry ids, fixed across all three lanes of this wave
// (notebook-split.md §4). They are spelled here as constants rather than inline
// so a rename is one edit and a typo is a build error — a chip whose id did not
// match a registry row would be a door that opened nothing, which 5.22 rule 5
// refuses outright.
const (
	charterPauseVerb     = "charter.pause"
	charterCadenceVerb   = "charter.cadence"
	charterProbationVerb = "charter.probation"
	charterRetireVerb    = "charter.retire"

	serviceStopVerb        = "service.stop"
	serviceRestartVerb     = "service.restart"
	serviceAutoRestartVerb = "service.autorestart"
)

// boardDetail is the open detail page, or the zero value for none.
type boardDetail struct {
	// target is which kind of page, reusing the click law's own vocabulary so
	// there is one enum for "what does this row open" and "what is open".
	target boardTarget
	// id is the charter's or the service's id — never drawn (5.14).
	id string
	// scroll is how far down the body the reader has come. A DETAIL PAGE
	// SCROLLS even though the list does not: the list is a set of doors and the
	// cursor is how you move through it, while this is a document.
	scroll int
	// log is the service log tail as it was read when the page opened, and
	// logID is the service it was read for. It is not re-read per frame: a log
	// tail is file I/O, and file I/O inside a paint is how a surface starts
	// stuttering on a busy disk.
	log   []string
	logID string
}

// open reports whether a detail page is up.
func (d boardDetail) open() bool {
	return d.target == boardOpensCharter || d.target == boardOpensService
}

// boardHit is a click target inside one line — the verb chips, and the trail's
// own back door. It is computed when the lines are built rather than when they
// are painted, so the pointer and the paint agree by construction.
type boardHit struct {
	id       string
	from, to int
}

// boardBack is the id the trail line's own back door carries. It is not a
// registry row: going back is this page's own navigation, the same way esc is,
// and the registry names doors that act on the WORLD rather than on a viewport.
const boardBack = "\x00board.back"

// openBoardDetail opens a detail page over the list.
func (a *App) openBoardDetail(target boardTarget, id string, cursor int) {
	if id == "" {
		return
	}
	a.board.cursorWas = cursor
	a.board.detail = boardDetail{target: target, id: id}
	a.board.hover = 0
	if target == boardOpensService {
		a.board.detail.log, a.board.detail.logID = a.boardServiceLog(id), id
	}
	a.refresh()
}

// closeBoardDetail puts the list back with the reader on the row they left.
func (a *App) closeBoardDetail() {
	a.board.detail = boardDetail{}
	a.boardSelect(a.board.cursorWas)
	a.refresh()
}

// boardServiceLog is the live log tail, through the commander that owns the
// process. A window with no commander — a visitor, a test — shows no log, which
// is the honest answer for a surface that is not the one running the thing.
func (a *App) boardServiceLog(id string) []string {
	service, ok := a.boardServiceAt(id)
	if !ok || strings.TrimSpace(service.LogPath) == "" {
		return nil
	}
	logs, ok := a.commander.(ServiceLogs)
	if !ok {
		return nil
	}
	return logs.ServiceLogTail(service.LogPath, boardDetailLogLines, boardDetailLogBytes)
}

// boardDetailKey is the detail page's keyboard: esc back, and the scroll
// vocabulary the notebook page uses, so two documents in this window do not
// scroll by two grammars.
func (a *App) boardDetailKey(key string) (tea.Cmd, bool) {
	switch key {
	case "esc":
		a.closeBoardDetail()
		return nil, true
	case "j", "down":
		a.boardDetailScroll(1)
	case "k", "up":
		a.boardDetailScroll(-1)
	case "pgdown":
		a.boardDetailScroll(notebookScroll)
	case "pgup":
		a.boardDetailScroll(-notebookScroll)
	case "home", "g":
		a.board.detail.scroll = 0
		a.shell.Invalidate()
	case "end", "G":
		a.boardDetailScroll(len(a.boardDetailLines()))
	default:
		return nil, false
	}
	return nil, true
}

// boardDetailScroll moves the body, clamped against the lines it actually has.
func (a *App) boardDetailScroll(delta int) {
	lines := a.boardDetailLines()
	max := len(lines) - a.board.height
	if max < 0 {
		max = 0
	}
	scroll := a.board.detail.scroll + delta
	if scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	a.board.detail.scroll = scroll
	a.shell.Invalidate()
}

// boardDetailPoint is the pointer on a detail page: the wheel scrolls the
// document, a click on the trail goes back, and a click on a verb chip fires it
// at the item this page is about ([App.runBoardVerb]).
func (a *App) boardDetailPoint(msg tea.MouseMsg, local image.Point) tea.Cmd {
	switch event := msg.(type) {
	case tea.MouseWheelMsg:
		switch event.Button {
		case tea.MouseWheelUp:
			a.boardDetailScroll(-notebookScroll)
		case tea.MouseWheelDown:
			a.boardDetailScroll(notebookScroll)
		}
		return nil
	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft {
			return nil
		}
		id, ok := a.boardDetailHit(local.X, local.Y)
		if !ok {
			return nil
		}
		if id == boardBack {
			a.closeBoardDetail()
			return nil
		}
		return a.runBoardVerb(id)
	}
	return nil
}

// VerbInvoker is the commander's one door from a drawn verb to the thing it
// does (internal/command/verbs.go).
//
// It takes a TARGET, and that is the whole reason this page cannot fire its
// chips through [App.runFooterVerb] the way the footer's own words do: a footer
// verb acts on the window, so the id is the whole instruction, while `pause`
// acts on ONE charter and an id with no subject would be a verb pointed at
// nothing. The subject is the detail page's own id — which is the only sense in
// which these chips differ from every other chip in the product.
type VerbInvoker interface {
	InvokeVerb(id, target, words string) (store.Command, error)
}

// runBoardVerb fires one chip at the item whose page is open.
//
// A verb that CARRIES AN ARGUMENT does not fire (command.ErrSteerVerb, the
// one-mouth law): "make it Tuesdays instead" is a thing to say, not a form to
// fill, so the surface says what to say and the person says it. Seeding the
// composer with [registry.Entry.SteerFor] is the finished form of that and it
// belongs to the composer's own file — see the wiring note in this lane's
// report; until it lands, the sentence is offered rather than typed.
//
// A window with no engine behind it falls back to the registry executor every
// other surface uses, so a visitor window draws the same chips and simply
// cannot fire the ones that need a journal.
func (a *App) runBoardVerb(id string) tea.Cmd {
	return a.runItemVerb(id, a.board.detail.id, a.boardDetailName())
}

// boardDetailName is what the open page is ABOUT, in the words a person would
// use for it — the seed a steering verb needs, and never the id.
func (a *App) boardDetailName() string {
	switch a.board.detail.target {
	case boardOpensCharter:
		if charter, ok := a.boardCharterAt(a.board.detail.id); ok {
			return charter.Invariant
		}
	case boardOpensService:
		if service, ok := a.boardServiceAt(a.board.detail.id); ok {
			return service.Name
		}
	}
	return ""
}

// boardDetailHit resolves a pane-local cell to whatever door is drawn on it.
func (a *App) boardDetailHit(x, y int) (string, bool) {
	lines := a.boardDetailLines()
	index := a.board.detail.scroll + y
	if y < 0 || index < 0 || index >= len(lines) {
		return "", false
	}
	for _, hit := range lines[index].hits {
		if x >= hit.from && x < hit.to {
			return hit.id, true
		}
	}
	return "", false
}

// -- the bodies --------------------------------------------------------------

// boardDetailLines is the open detail page as drawable rows.
func (a *App) boardDetailLines() []boardLine {
	switch a.board.detail.target {
	case boardOpensCharter:
		return a.charterDetailLines()
	case boardOpensService:
		return a.serviceDetailLines()
	}
	return nil
}

// charterDetailLines is one standing charter, full page.
//
// The body is notebook-split.md §3's list for this kind and nothing beside it:
// the invariant, the rails that bound every firing, where it stands on the
// tenure ladder, and the last few judgments with what followed each. The rails
// and the ladder are written as WORD-THEN-VALUE on one line each — the craft
// page's `ceilings $0.50 · 10m` idiom — because a heading over a single line of
// values is a heading doing a label's job for one row (§15).
func (a *App) charterDetailLines() []boardLine {
	charter, ok := a.boardCharterAt(a.board.detail.id)
	if !ok {
		return a.detailGone(boardWatchingWord)
	}
	glyph, tier := boardCharterGlyph(charter)
	lines := []boardLine{
		a.detailTrail(boardWatchingWord, charter.Invariant),
		{kind: boardBlank},
		{kind: boardEntry, indent: boardNameCol, mark: glyph, markTier: tier,
			spans: []boardSpan{{text: charter.Invariant, tier: tokens.TextPrimary}}},
	}
	if spans := charter.receipt(); len(spans) > 0 {
		lines = append(lines, boardLine{kind: boardMeta, indent: boardMetaCol, spans: spans})
	}

	body := make([]boardLine, 0, 2)
	// A word with nothing after it is a heading over an empty row, which is
	// exactly the label §15 says to delete. A charter with no rails on record
	// simply does not have a rails line.
	if rails := charterRails(charter); rails != "" {
		body = append(body, boardLine{kind: boardMeta, indent: boardNameCol,
			spans: detailPair("rails", rails)})
	}
	if tenure := charterTenure(charter); tenure != "" {
		body = append(body, boardLine{kind: boardMeta, indent: boardNameCol,
			spans: detailPair("tenure", tenure)})
	}
	if len(body) > 0 {
		lines = append(lines, boardLine{kind: boardBlank})
		lines = append(lines, body...)
	}

	if firings := a.charterFirings(charter.ID); len(firings) > 0 {
		lines = append(lines, boardLine{kind: boardBlank})
		for _, firing := range firings {
			lines = append(lines, firing)
		}
	}

	lines = append(lines, boardLine{kind: boardBlank})
	lines = append(lines, a.detailVerbs(
		charterPauseVerb, charterCadenceVerb, charterProbationVerb, charterRetireVerb))
	return lines
}

// charterRails is the rails line: what one firing may spend, how many may happen
// in a day, and when the whole thing expires. `never` is not written — a charter
// with no expiry simply does not mention one (§16's EMPTINESS).
func charterRails(c boardCharter) string {
	rails := c.Rails()
	parts := make([]string, 0, 3)
	if rails.PerFiringBudgetUSD > 0 {
		parts = append(parts, tokens.Money(rails.PerFiringBudgetUSD)+" a firing")
	}
	if rails.MaxFiringsPerDay > 0 {
		parts = append(parts, strconv.Itoa(rails.MaxFiringsPerDay)+" a day")
	}
	if rails.ExpiresAt != nil && !rails.ExpiresAt.IsZero() {
		if rails.ExpiresAt.After(c.now) {
			parts = append(parts, "expires in "+detailElapsed(rails.ExpiresAt.Sub(c.now)))
		} else {
			parts = append(parts, "expired")
		}
	}
	return strings.Join(parts, boardSeparator)
}

// charterTenure is where this charter stands on the ladder, HONESTLY.
//
// There is no denominator, and that is deliberate. The promotion threshold is
// the resident's to decide at review time (store/tenure.go takes it as an
// argument and raises it by one for every refusal), so this surface genuinely
// does not know what "3 of 5" would mean — and §3's no-fractions law says a
// promise of a denominator nobody can keep is worse than none. What it knows is
// the autonomy word, the green run behind it, and how many times it has been
// demoted, and it says exactly those.
func charterTenure(c boardCharter) string {
	parts := []string{string(c.Autonomy)}
	if c.GreenFirings == 1 {
		parts = append(parts, "1 green firing")
	} else if c.GreenFirings > 1 {
		parts = append(parts, strconv.Itoa(c.GreenFirings)+" green firings")
	}
	if c.Demotions == 1 {
		parts = append(parts, "1 demotion")
	} else if c.Demotions > 1 {
		parts = append(parts, strconv.Itoa(c.Demotions)+" demotions")
	}
	return strings.Join(parts, boardSeparator)
}

// charterFirings is the last few judgments as rows: what the sentinel decided,
// the sentence it wrote, and what followed.
//
// The glyph is the JUDGMENT and the dim words after it are the OUTCOME, which is
// the distinction store.SentinelJudgment draws in its own doc ("a yes that led
// nowhere" is a real and readable state). An error wears the broken glyph,
// because a sentinel that could not answer is not a sentinel that answered no.
func (a *App) charterFirings(id string) []boardLine {
	reader, ok := a.backend.(Firings)
	if !ok {
		return nil
	}
	judgments, err := reader.RecentSentinelJudgments(id, boardFirings)
	if err != nil || len(judgments) == 0 {
		return nil
	}
	out := make([]boardLine, 0, len(judgments))
	for _, judgment := range judgments {
		glyph, tier := tokens.GlyphQueued, tokens.TextTertiary
		switch {
		case judgment.Error != "":
			glyph, tier = tokens.GlyphFailed, tokens.Coral
		case judgment.Yes:
			glyph, tier = tokens.GlyphSettled, tokens.Green
		}
		words := strings.TrimSpace(judgment.Line)
		if words == "" {
			words = strings.TrimSpace(judgment.Error)
		}
		if words == "" {
			// A judgment with nothing written on it still happened, and the
			// glyph is the whole of what it has to say.
			words = "—"
		}
		spans := []boardSpan{
			{text: glyph, tier: tier},
			{text: " ", tier: tokens.TextTertiary},
			{text: words, tier: tokens.TextSecondary},
		}
		if outcome := strings.TrimSpace(judgment.Outcome); outcome != "" {
			spans = append(spans,
				boardSpan{text: boardSeparator, tier: tokens.TextTertiary},
				boardSpan{text: outcome, tier: tokens.TextTertiary})
		}
		out = append(out, boardLine{kind: boardTwig, indent: boardNameCol, spans: spans})
	}
	return out
}

// serviceDetailLines is one promoted process, full page.
//
// The body is notebook-split.md §3's list for this kind: the command, where it
// runs, how it is probed, what happens when it dies, and the live log tail. The
// log is drawn behind a `│` gutter at the dim tier, which is §5's own treatment
// for machine output — a log rendered as prose would be the loudest thing on a
// page whose subject is a process.
func (a *App) serviceDetailLines() []boardLine {
	service, ok := a.boardServiceAt(a.board.detail.id)
	if !ok {
		return a.detailGone(boardServicesWord)
	}
	glyph, tier := boardServiceGlyph(service)
	lines := []boardLine{
		a.detailTrail(boardServicesWord, service.Name),
		{kind: boardBlank},
		{kind: boardEntry, indent: boardNameCol, mark: glyph, markTier: tier,
			spans: []boardSpan{{text: service.Name, tier: tokens.TextPrimary}}},
	}
	if spans := service.receipt(); len(spans) > 0 {
		lines = append(lines, boardLine{kind: boardMeta, indent: boardMetaCol, spans: spans})
	}
	if command, where := strings.TrimSpace(service.Command), serviceWhere(service); command != "" || where != "" {
		lines = append(lines, boardLine{kind: boardBlank})
	}
	if command := strings.TrimSpace(service.Command); command != "" {
		lines = append(lines, boardLine{kind: boardMeta, indent: boardNameCol,
			spans: []boardSpan{{text: command, tier: tokens.TextSecondary}}})
	}
	if where := serviceWhere(service); where != "" {
		lines = append(lines, boardLine{kind: boardMeta, indent: boardNameCol,
			spans: []boardSpan{{text: where, tier: tokens.TextTertiary}}})
	}

	lines = append(lines, boardLine{kind: boardBlank})
	if tail := a.board.detail.log; len(tail) > 0 {
		for _, row := range tail {
			lines = append(lines, boardLine{kind: boardTwig, indent: boardNameCol,
				spans: []boardSpan{
					{text: tokens.GlyphTreeVert + " ", tier: tokens.TextTertiary},
					{text: row, tier: tokens.TextTertiary},
				}})
		}
	} else {
		lines = append(lines, boardLine{kind: boardNote, indent: boardNameCol,
			spans: []boardSpan{{text: "nothing in the log yet", tier: tokens.TextTertiary}}})
	}

	lines = append(lines, boardLine{kind: boardBlank})
	lines = append(lines, a.detailVerbs(serviceStopVerb, serviceRestartVerb, serviceAutoRestartVerb))
	return lines
}

// serviceWhere is the one dim line under the command: where it runs, how it is
// probed, and what happens when it dies.
func serviceWhere(s boardService) string {
	parts := make([]string, 0, 3)
	if dir := strings.TrimSpace(s.Dir); dir != "" {
		parts = append(parts, dir)
	}
	if health := strings.TrimSpace(s.Health.String()); health != "" {
		parts = append(parts, health)
	}
	if s.AutoRestart {
		parts = append(parts, "restarts itself")
	} else {
		parts = append(parts, "stays down")
	}
	return strings.Join(parts, boardSeparator)
}

// -- the shared furniture ----------------------------------------------------

// detailTrail is the page's own trail line: `work ‹ watching ‹ <name>`.
//
// The FOOTER is another lane's file and this is not a breadcrumb in the bar; it
// is the page saying where it sits, which is what notebook-split.md §3 asks
// every detail page to draw. The ancestors are the door back — the whole run up
// to the last `‹` is one click target, because a reader points at "where I came
// from" and not at a chevron — and the current location middle-cuts last (§16's
// ONE ELLIPSIS GRAMMAR).
func (a *App) detailTrail(band, name string) boardLine {
	up := tokens.GlyphScopeUp
	head := "work " + up + " " + band
	spans := []boardSpan{
		{text: head, tier: tokens.TextSecondary},
		{text: " " + up + " ", tier: tokens.TextTertiary},
		{text: name, tier: tokens.TextPrimary},
	}
	return boardLine{
		kind: boardMeta, indent: boardNameCol, spans: spans,
		hits: []boardHit{{id: boardBack, from: boardNameCol, to: boardNameCol + blocks.Width(head)}},
	}
}

// detailGone is what a detail page says when the thing it was about has left the
// store between the click that opened it and the frame that draws it. An empty
// page and a deleted subject must not look alike (12.10).
func (a *App) detailGone(band string) []boardLine {
	return []boardLine{
		a.detailTrail(band, "—"),
		{kind: boardBlank},
		{kind: boardNote, indent: boardNameCol, spans: []boardSpan{
			{text: "this is gone now", tier: tokens.TextTertiary}}},
	}
}

// detailPair is a word-then-value line: `rails $0.50 a firing · 6 a day`. The
// word is chrome and the value is the reading, which is the only reason the two
// tiers differ.
func detailPair(word, value string) []boardSpan {
	return []boardSpan{
		{text: word + " ", tier: tokens.TextTertiary},
		{text: value, tier: tokens.TextSecondary},
	}
}

// detailVerbs is the page's verb row, drawn to §16's VERB·KEY CHIP law through
// the one place that law lives: [registry.ChipOn], the same ladder the footer,
// the palette and the `?` sheet are fed by.
//
// A verb whose registry row has not landed yet still draws, wearing the house
// word for its id and no key. That is deliberate rather than defensive: the
// registry entries for these seven ids belong to another lane of this same wave,
// and a page that drew four verbs today and seven tomorrow would be a page whose
// anatomy depended on merge order. The id is what the chip carries either way,
// so the door is correct from the moment the row exists.
func (a *App) detailVerbs(ids ...string) boardLine {
	spans := make([]boardSpan, 0, len(ids)*4)
	hits := make([]boardHit, 0, len(ids))
	x := boardNameCol
	for _, id := range ids {
		chip := boardVerbChip(id)
		if chip.Empty() {
			continue
		}
		if len(spans) > 0 {
			spans = append(spans, boardSpan{text: boardSeparator, tier: tokens.TextTertiary})
			x += blocks.Width(boardSeparator)
		}
		from := x
		// Verb first at the brighter tier, the key after it one tier down, and
		// the WHOLE chip — the gap included — is the one target, because a
		// reader points at the words and not at the key.
		spans = append(spans, boardSpan{text: chip.Verb, tier: tokens.TextSecondary})
		x += blocks.Width(chip.Verb)
		if chip.Key != "" {
			spans = append(spans, boardSpan{
				text: registry.ChipGap + chip.Key, tier: tokens.TextTertiary})
			x += blocks.Width(registry.ChipGap + chip.Key)
		}
		hits = append(hits, boardHit{id: id, from: from, to: x})
	}
	return boardLine{kind: boardMeta, indent: boardNameCol, spans: spans, hits: hits}
}

// boardVerbChip is one verb's chip: the registry's spelling when the row exists,
// and the house word when it does not yet.
func boardVerbChip(id string) registry.Chip {
	if entry, ok := registry.ByID(id); ok {
		return registry.ChipOn(entry, registry.SurfaceComposerFirst)
	}
	return registry.ChipFor(boardVerbWord(id), "")
}

// boardVerbWord is the fallback word for a verb id, and it is the word the
// registry row is expected to carry. All lowercase, because chrome words are
// (§16's CASE).
func boardVerbWord(id string) string {
	switch id {
	case charterPauseVerb:
		return "pause"
	case charterCadenceVerb:
		return "cadence"
	case charterProbationVerb:
		return "probation"
	case charterRetireVerb:
		return "retire"
	case serviceStopVerb:
		return "stop"
	case serviceRestartVerb:
		return "restart"
	case serviceAutoRestartVerb:
		return "auto-restart"
	}
	return ""
}

// detailElapsed is the one duration reading this page uses, kept here so the
// expiry and the uptime cannot drift onto two formatters.
func detailElapsed(d time.Duration) string { return reltime.Elapsed(d) }

// renderBoardDetail paints the open detail page.
func (a *App) renderBoardDetail(width, height int) string {
	lines := a.boardDetailLines()
	top := a.board.detail.scroll
	if max := len(lines) - height; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	out := make([]string, 0, height)
	for i := top; i < len(lines) && len(out) < height; i++ {
		out = append(out, a.boardRow(lines[i], width, false, false, ""))
	}
	return strings.Join(out, "\n")
}
