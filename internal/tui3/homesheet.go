package tui3

// THE SHEET: A ROW'S CARD, OVER THE WHOLE FRAME.
//
// The card is home's right column and the right column goes at eighty
// ([homeCardMin]). Under sixty there is no second column and there is not
// going to be one — thirty cells beside thirty cells is two truncated columns —
// so the card takes the FRAME instead, opened by the same gesture that would
// have previewed it on a wide one: enter on the row, or a tap.
//
//	 ‹ back                            esc
//	 fix the nil-map crash
//	 aforge-v2 · ~/src/aforge-v2
//	 ─────────────────────────────────────
//	 open in another window
//	 bash wants to run: git push --force
//
//	  1 allow once
//	  2 always
//	  3 deny
//
//	 ◆ 2 things since you left
//	 ▸ more
//	 ─────────────────────────────────────
//	  ‹ back    open      more
//
// IT IS THE SAME CONTENT AND NOT A SECOND CARD. Every row under the place line
// is a band from the registry (homebands.go), drawn by the same function that
// draws it on a wide frame, at the full width — [bandClauses] already lays a
// band out for whatever width it is given, so a band written once draws on both.
// What this tier changes is three things and only three:
//
//   - THE ORDER. The wide card reads top-down as a description; the sheet is
//     read by somebody standing up, so what they can ACT on comes first —
//     answer, then state, then news, then the work, then what is next, then
//     where it left off, then what it produced. Everything that is
//     bookkeeping — the repository, the keys, the spend — is behind one
//     `▸ more` at the foot ([homeSheetMore]).
//
//   - THE ANSWERS ARE BANDS. The chips a wide card packs onto one row become
//     one full-width row each, in the consent sheet's own shape (consent.go):
//     a digit, a label, and the WHOLE ROW as the target. Three answers across
//     forty-four columns is fourteen cells each before the gaps, and a phone
//     tier reaches down to twenty columns where that is four cells — a target
//     that misses.
//
//   - IT SCROLLS AND IT DROPS. ↑↓ and PgUp/PgDn move the body, and a frame too
//     short for the bands drops them FROM THE BOTTOM, never the title and never
//     the answer band — which is why the answer is drawn second and not seventh
//     ([homeBands] does the dropping, and it keeps what is nearest the title).
//
// AND AN ERRAND IS ONE KIND OF SHEET rather than a shape of its own. `ask here`
// already had a narrow form — the pane takes the frame while it holds the
// keyboard ([app.homeStacked]) — and that IS this sheet with an exchange in it:
// one mechanism, one way out, one bar at the foot.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// The sentences the sheet says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// homeSheetBackWord is the sheet's top row and the bar's first target. It
	// is the same two cells and the same word in both places, because they are
	// one door: the row a thumb reaches at the top of the screen, and the target
	// it reaches at the bottom.
	homeSheetBackWord = "‹ back"
	// homeSheetBackASCII is that word where the arrow cannot be drawn.
	homeSheetBackASCII = "< back"
	// homeSheetMoreWord is the fold at the foot holding the bookkeeping bands.
	homeSheetMoreWord = "more"
	// homeSheetOpenWord is the bar's middle target on a card: the door the row
	// has always offered.
	homeSheetOpenWord = "open"
	// homeSheetSendWord is that target on an errand, where there is nothing to
	// open and enter sends what is in the box instead.
	homeSheetSendWord = "send"
)

// homeSheetOrder is the reading order of the bands at this tier, by band name.
// A band not named here keeps its registry position after the named ones, so a
// lane adding a band gets a sensible place without editing this line.
var homeSheetOrder = []string{
	"answer", "state", "news", "work", "nextup", "leftoff", "deliverables",
}

// homeSheetMore is what goes behind the fold: the bands that say how things
// stand rather than what to do about them. They are still one gesture away —
// `m`, or a tap on the fold line — and nothing is dropped.
var homeSheetMore = []string{"repo", "keys", "spend", "projectfacts"}

// homeSheetMoreKey is the fold key the `▸ more` line is remembered under. It is
// not a registered band, so it takes a name of its own in the same map
// ([app.bandFolded] keys on band and subject together).
const homeSheetMoreKey = "phonemore"

// homeSheet is the sheet's whole state. The zero value is closed, which costs
// the frame nothing.
//
// THE SUBJECT IS THE CURSOR'S ROW AND IS NEVER SNAPSHOT, which is what makes a
// resize free: a sheet open at fifty-five columns is the row under the cursor,
// and at ninety that same row is what the right pane draws — the same card,
// arrived at by the same cursor. Nothing has to be carried across the
// breakpoint because nothing was ever held here.
type homeSheet struct {
	open bool
	// top is the first band row on screen. It is the sheet's own scroll and not
	// the list's: the column under it has not moved.
	top int
}

// homeSheetShowing reports whether the sheet is on the frame RIGHT NOW, which
// is what every router below asks.
//
// It is the open flag AND the tier AND — for an errand — the keyboard, for
// [app.expandShowing]'s reason: a terminal dragged wider while the sheet is up
// has stopped being the tier this sheet exists for, and the card is beside the
// list from that moment exactly as it would have been had it been opened there.
func (a *app) homeSheetShowing() bool {
	if !a.home.sheet.open || !a.homePhone() {
		return false
	}
	if ex, ok := a.homeSheetErrand(); ok {
		// AN ERRAND'S SHEET IS ITS FOCUS. `tab` and `esc` hand the keyboard back
		// to the list from inside the pane (homeexchange.go), and that IS the way
		// out of this sheet — one gesture, not two flags to keep in step.
		return ex.focused
	}
	return true
}

// homeSheetErrand is the exchange the sheet is about, when it is about one.
func (a *app) homeSheetErrand() (*homeExchange, bool) {
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeExchangeRow || line.ex == nil {
		return nil, false
	}
	return line.ex, true
}

// openHomeSheet raises the sheet over the row under the cursor.
func (a *app) openHomeSheet() {
	if ex, ok := a.homeSheetErrand(); ok {
		ex.focused = true
	}
	a.home.sheet = homeSheet{open: true}
	a.touch()
}

// closeHomeSheet puts it away and leaves the cursor exactly where it was, which
// is the whole of what `‹ back` promises.
func (a *app) closeHomeSheet() {
	if ex, ok := a.homeSheetErrand(); ok {
		ex.focused = false
	}
	a.home.sheet = homeSheet{}
	a.touch()
}

// homeSheetSubject is what the sheet is ABOUT, as the band registry sees it.
//
// It is [app.homeSubject] plus the one row that surface has never had to answer
// for: a `since you left` line, whose subject is the thing the news is about —
// the conversation it landed in, or the project whose inbox it was left in.
func (a *app) homeSheetSubject() (bandSubject, bool) {
	line, ok := a.home.focusedLine()
	if ok && line.kind == homePhoneNews {
		note := line.note
		if note == nil {
			return bandSubject{}, false
		}
		if note.hasRow {
			return bandSubject{
				kind: bandKindSession, row: note.row, project: note.project,
				dir: strings.TrimSpace(note.row.ProjectDir), world: a.home.world,
			}, true
		}
		return bandSubject{
			kind: bandKindProject, project: note.project,
			dir: homeProjectPath(note.proj), world: a.home.world,
		}, true
	}
	return a.homeSubject()
}

// ── the frame ───────────────────────────────────────────────────────────────

// homeSheetHitKind is what one row of the sheet answers to a tap.
type homeSheetHitKind uint8

const (
	homeSheetHitNone homeSheetHitKind = iota
	// homeSheetHitBack is the top row, which is the way out.
	homeSheetHitBack
	// homeSheetHitOpen is the title, which is the door the row offers.
	homeSheetHitOpen
	// homeSheetHitAnswer is one answer band; key is the digit it gives.
	homeSheetHitAnswer
	// homeSheetHitTask is a row of the work band; index is which task.
	homeSheetHitTask
	// homeSheetHitMore is the `▸ more` fold at the foot.
	homeSheetHitMore
)

// homeSheetHit is one screen row's answer to the pointer.
type homeSheetHit struct {
	kind  homeSheetHitKind
	key   string
	index int
}

// homeSheetFrame is the whole screen while the sheet is up, in [app.homeFrame]'s
// own contract: exactly height rows, what each answers to the pointer, and the
// caret. The hit map it returns is the LIST's (-1 everywhere, since the list is
// not on screen); the sheet's own map is recorded on the view, which is where
// [app.homeSheetPress] reads it.
func (a *app) homeSheetFrame(width, height int) ([]string, []int, int, int) {
	if _, ok := a.homeSheetErrand(); ok {
		return a.homeErrandSheet(width, height)
	}
	pal := a.pal
	var lines []string
	var hits []homeSheetHit
	add := func(text string, hit homeSheetHit) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	add(a.homeSheetHead(width, pal), homeSheetHit{kind: homeSheetHitBack})
	subject, ok := a.homeSheetSubject()
	if !ok {
		a.closeHomeSheet()
		return a.homePhoneFrame(width, height)
	}
	for _, row := range a.homeSheetTitle(subject, width, pal) {
		add(row, homeSheetHit{kind: homeSheetHitOpen})
	}
	add(pal.dim(rule(width)), homeSheetHit{})

	const foot = 2 // the rule and the bar
	room := height - len(lines) - foot
	if room < 1 {
		room = 1
	}
	body, marks := a.homeSheetBody(subject, width, room)
	a.home.sheet.top = clampTop(a.home.sheet.top, len(body), room)
	for i := 0; i < room; i++ {
		at := a.home.sheet.top + i
		if at >= len(body) {
			// Padding, and it answers to NOTHING. A gap that fell through to the
			// list underneath would be a tap that moved a cursor nobody can see
			// (expand.go states the same law about its own padding).
			add("", homeSheetHit{})
			continue
		}
		add(body[at], marks[at])
	}
	add(pal.dim(rule(width)), homeSheetHit{})
	add(a.homeBar(width, a.homeSheetBar(subject), pal), homeSheetHit{})
	a.home.barRow = len(lines) - 1

	if len(lines) > height && height > 1 {
		cut := len(lines) - (height - 1)
		lines = append(lines[:1], lines[cut:]...)
		hits = append(hits[:1], hits[cut:]...)
		a.home.barRow = len(lines) - 1
	}
	a.home.sheetHits = hits
	a.home.pane = homeNoHits(len(lines))
	return lines, homeNoHits(len(lines)), 0, 0
}

// homeNoHits is a hit map that answers for nothing: the sheet covers the list,
// so no screen row of it belongs to a row of the column underneath.
func homeNoHits(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = -1
	}
	return out
}

// homeSheetHead is the top row: the way out on the left, the key that does the
// same on the right. THE WHOLE ROW IS THE TARGET — there is no button to aim
// at, which is expand.go's law about its own head and the reason a thumb can
// use either end of it.
func (a *app) homeSheetHead(width int, pal palette) string {
	word := homeSheetBackWord
	if pal.ascii {
		word = homeSheetBackASCII
	}
	left := " " + pal.dim(word)
	right := "esc"
	gap := width - 1 - ansi.StringWidth(word) - len(right) - 1
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + pal.dim(right)
}

// homeSheetTitle is the two lines nothing may displace — what this is, and
// where it is — which is [app.homeDetail]'s own division of labour said again
// at this width.
func (a *app) homeSheetTitle(subject bandSubject, width int, pal palette) []string {
	pad := width - 1
	var name, place string
	switch subject.kind {
	case bandKindSession:
		name, place = homeName(subject.row), subject.project
		if dir := strings.TrimSpace(subject.row.ProjectDir); dir != "" && dir != place {
			place += " · " + dir
		}
	case bandKindItem:
		name, place = strings.TrimSpace(subject.item.Item.Words), subject.project
		if subject.dir != "" && subject.dir != place {
			place = joinDot(place, subject.dir)
		}
	case bandKindProject:
		name, place = subject.project, subject.dir
	}
	out := []string{" " + pal.bold(pal.ink(fit(name, pad)))}
	if strings.TrimSpace(place) != "" {
		out = append(out, " "+pal.dim(a.pathLink(subject.dir, fitLeft(place, pad))))
	}
	return out
}

// homeSheetBody is every band the subject has, in this tier's order, with the
// bookkeeping behind one fold — and what each row answers to a tap.
func (a *app) homeSheetBody(subject bandSubject, width, room int) ([]string, []homeSheetHit) {
	// The sheet is this card in the phone tier, so it empties the same door
	// registry the desktop card writes (carddoors.go). There is no hover on
	// glass, so nothing here reads it back — but a registry left standing from a
	// wide frame would be a tap resolved against rows this sheet never drew.
	a.resetCardDoors()
	pal := a.pal
	ctx := bandContext{subject: subject, width: width - 1, now: a.home.world.Read, pal: pal}
	top, more := homeSheetBands(subject.kind)

	var bands [][]string
	var marks [][]homeSheetHit
	push := func(rows []string, hits []homeSheetHit) {
		if len(rows) == 0 {
			return
		}
		bands = append(bands, rows)
		marks = append(marks, hits)
	}
	for _, band := range top {
		rows, hits := a.homeSheetBand(band, ctx)
		push(rows, hits)
	}
	// THE FOLD IS DRAWN ONLY WHEN THERE IS SOMETHING BEHIND IT. A `▸ more` over
	// nothing is a control that does nothing, which is the same rule the quiet
	// tail and every list-shaped band already keep ([app.bandFold]).
	var rest [][]string
	for _, band := range more {
		if rows := band.draw(a, ctx); len(rows) > 0 {
			rest = append(rest, rows)
		}
	}
	if len(rest) > 0 {
		folded := a.bandFolded(homeSheetMoreKey, subject)
		mark := bandFoldMark(pal, folded)
		push([]string{pal.dim(fit(mark+" "+homeSheetMoreWord, width-1))},
			[]homeSheetHit{{kind: homeSheetHitMore}})
		a.noteBandFoldLine(homeSheetMoreKey, subject, homeSheetMoreWord)
		if !folded {
			for _, rows := range rest {
				push(rows, nil)
			}
		}
	}

	// THE BLANK BETWEEN TWO BANDS IS THE CARD'S OWN SEPARATOR ([homeBands]),
	// and the hits are assembled beside the rows in the same walk so a row and
	// what it answers to can never fall out of step.
	var out []string
	var hits []homeSheetHit
	for i, band := range bands {
		if len(out) > 0 {
			out = append(out, "")
			hits = append(hits, homeSheetHit{})
		}
		for j, row := range band {
			out = append(out, " "+row)
			hit := homeSheetHit{}
			if i < len(marks) && j < len(marks[i]) {
				hit = marks[i][j]
			}
			hits = append(hits, hit)
		}
	}
	return out, hits
}

// homeSheetBand draws one band and says what its rows answer to.
//
// TWO BANDS ARE NOT SIMPLY DRAWN. The answers become full-width bands (this
// file's header says why), and the work band's rows become doors onto the
// tasks they name. Everything else is the registry's own drawing, untouched.
func (a *app) homeSheetBand(band homeBand, ctx bandContext) ([]string, []homeSheetHit) {
	switch band.name {
	case "answer":
		return a.homeAnswerBands(ctx)
	case "work":
		rows := band.draw(a, ctx)
		return rows, homeSheetTaskHits(rows, ctx)
	}
	return band.draw(a, ctx), nil
}

// homeSheetBands splits the registry into what the sheet draws and what its
// fold holds, in this tier's own order.
func homeSheetBands(kind bandKind) (top, more []homeBand) {
	at := func(name string) int {
		for i, word := range homeSheetOrder {
			if word == name {
				return i
			}
		}
		return len(homeSheetOrder)
	}
	behind := func(name string) bool {
		for _, word := range homeSheetMore {
			if word == name {
				return true
			}
		}
		return false
	}
	for _, band := range homeBandsFor(kind) {
		if behind(band.name) {
			more = append(more, band)
			continue
		}
		top = append(top, band)
	}
	// A STABLE SORT, so bands the order does not name keep the registry's own
	// sequence among themselves — which is what lets a lane add a band without
	// editing [homeSheetOrder] and still get a sensible place.
	stableSortBands(top, at)
	return top, more
}

func stableSortBands(bands []homeBand, at func(string) int) {
	for i := 1; i < len(bands); i++ {
		for j := i; j > 0 && at(bands[j].name) < at(bands[j-1].name); j-- {
			bands[j], bands[j-1] = bands[j-1], bands[j]
		}
	}
}

// homeSheetTaskHits resolves name rows in the same parent-first order they were
// drawn. Matching a title independently loses identity when siblings share a
// name, and can mistake an outcome sentence for a task. The whole rendered
// block advances the cursor, so continuation rows can never become another door.
func homeSheetTaskHits(rows []string, ctx bandContext) []homeSheetHit {
	row := ctx.subject.row
	hits := make([]homeSheetHit, len(rows))
	indices := make(map[string]int, len(row.Tasks.Rows))
	for i, entry := range row.Tasks.Rows {
		if _, found := indices[homeTaskKey(entry)]; !found {
			indices[homeTaskKey(entry)] = i
		}
	}
	next := 0
	for _, family := range homeWorkFamilies(row) {
		for _, node := range family {
			expected := homeWorkNodeRows(node, row, ctx.width, ctx.now, ctx.pal)
			if len(expected) == 0 {
				continue
			}
			name := ansi.Strip(expected[0])
			for at := next; at < len(rows); at++ {
				if ansi.Strip(rows[at]) != name {
					continue
				}
				hits[at] = homeSheetHit{kind: homeSheetHitTask, index: indices[homeTaskKey(node.entry)]}
				next = at + len(expected)
				break
			}
		}
	}
	return hits
}

// ── the answers, as bands ───────────────────────────────────────────────────

// homeAnswerBands is homeband_answer.go's chips laid out one to a row.
//
// IT KEEPS THAT BAND'S FOUR LAWS WHOLE and re-states none of them: it draws
// what the session OFFERED ([session.PresenceQuestion.Options]), it never
// repeats the question, it says `answered · …` once a key has landed, and it
// draws nothing at all where this window has no way to leave an answer. What
// changes is the SHAPE, and the shape is consent.go's — a digit, a label, and
// the whole row as the target.
func (a *app) homeAnswerBands(ctx bandContext) ([]string, []homeSheetHit) {
	row, pal := ctx.subject.row, ctx.pal
	question, ok := answerable(row, ctx.now)
	if !ok {
		return nil, nil
	}
	if sent, ok := a.answerSent(row, question); ok {
		if ctx.now.Sub(sent.at) < answerHoldFor {
			return []string{pal.dim(fit(answerWaitingWord, ctx.width))}, nil
		}
		return nil, nil
	}
	if a.leaveAnswer == nil && !a.answeringHere(row) {
		return nil, nil
	}
	chips := answerChips(question)
	if len(chips) == 0 {
		return nil, nil
	}
	rows := make([]string, 0, len(chips))
	hits := make([]homeSheetHit, 0, len(chips))
	for _, chip := range chips {
		rows = append(rows, a.homeAnswerBand(chip, ctx.width, pal))
		hits = append(hits, homeSheetHit{kind: homeSheetHitAnswer, key: chip.key})
	}
	return rows, hits
}

// homeAnswerBand is one answer as a row: the key it also answers to, then the
// word, in the question's own hue.
//
// IT IS consent.go's BAND at one remove and deliberately not its function.
// [app.questionBandRow] asks whether the pointer is over the block it belongs to,
// and there is no block here — only a card in a sheet. What is shared is the
// thing that matters: a key on this surface is bold and violet wherever it is
// offered, the row is the target at every width this tier has, and the one cell
// of margin is the same one ([questionBandPad]).
func (a *app) homeAnswerBand(chip answerChip, width int, pal palette) string {
	word := chip.label
	if room := width - len(questionBandPad) - ansi.StringWidth(chip.key) - 1; ansi.StringWidth(word) > room {
		word = fit(word, room)
	}
	// The phone's chip is the wide tier's chip, said in the same amber
	// ([app.answerChipLines] holds the reasoning).
	return pal.warn(questionBandPad) + pal.warnBold(chip.key) + pal.warn(" "+word)
}

// ── the errand's sheet ──────────────────────────────────────────────────────

// homeErrandSheet is the same sheet with an exchange in it: the pane that
// already exists ([app.exchangePane]), the box it is talked to through, and the
// same bar at the foot.
//
// THE PANE'S ROWS ARE RECORDED WHERE THE POINTER LOOKS FOR THEM
// ([app.homePane] reads [homeView.pane]), so a press inside it resolves through
// [app.exchangePress] exactly as it does on a wide frame — one mechanism, one
// hit map, and the sheet is only where it was drawn.
func (a *app) homeErrandSheet(width, height int) ([]string, []int, int, int) {
	ex, _ := a.homeSheetErrand()
	pal := a.pal
	var lines []string
	var panes []int
	var hits []homeSheetHit
	add := func(text string, pane int, hit homeSheetHit) {
		lines = append(lines, text)
		panes = append(panes, pane)
		hits = append(hits, hit)
	}

	add(a.homeSheetHead(width, pal), -1, homeSheetHit{kind: homeSheetHitBack})
	add(pal.dim(rule(width)), -1, homeSheetHit{})

	const foot = 3 // the rule, the box, the bar
	room := height - len(lines) - foot
	if room < 1 {
		room = 1
	}
	pane := a.exchangePane(ex, width, room, pal)
	for i := 0; i < room; i++ {
		text := ""
		if i < len(pane) {
			text = pane[i]
		}
		add(text, i, homeSheetHit{})
	}

	add(pal.dim(rule(width)), -1, homeSheetHit{})
	text := ex.box.String()
	add(" "+pal.muted("› ")+pal.ink(fit(text, width-4)), -1, homeSheetHit{})
	caretX, caretY := 3+ansi.StringWidth(text), len(lines)-1
	if caretX > width-1 {
		caretX = width - 1
	}
	add(a.homeBar(width, a.homeErrandBar(ex), pal), -1, homeSheetHit{})
	a.home.barRow = len(lines) - 1

	if len(lines) > height && height > 1 {
		cut := len(lines) - (height - 1)
		lines = append(lines[:1], lines[cut:]...)
		panes = append(panes[:1], panes[cut:]...)
		hits = append(hits[:1], hits[cut:]...)
		a.home.barRow = len(lines) - 1
		caretY -= cut - 1
	}
	a.home.pane = panes
	a.home.sheetHits = hits
	return lines, homeNoHits(len(lines)), caretX, caretY
}

// ── the bar ─────────────────────────────────────────────────────────────────

// homeSheetBar is the three targets over a card: the way back, the door the row
// offers, and everything the fold is holding.
func (a *app) homeSheetBar(subject bandSubject) []homeBarTarget {
	back := homeSheetBackWord
	if a.pal.ascii {
		back = homeSheetBackASCII
	}
	return []homeBarTarget{
		{word: back, do: func(a *app) tea.Cmd { a.closeHomeSheet(); return nil }},
		{word: homeSheetOpenWord, do: func(a *app) tea.Cmd { return a.homeSheetOpen() }},
		{word: homeSheetMoreWord, do: func(a *app) tea.Cmd {
			a.toggleSheetMore(subject)
			return nil
		}},
	}
}

// homeErrandBar is the same three over an errand, where the middle target sends
// what is in the box — there is nothing to open, because the errand is already
// the thing on screen.
//
// AND `more` IS THE ONE THING THERE IS MORE OF: an errand that has answered can
// become an ordinary conversation, folder and all ([homeContinueWord]), and the
// target is drawn only while that offer is on the pane. A target for an act
// that is not on offer is a target that teaches a person the bar is unreliable.
func (a *app) homeErrandBar(ex *homeExchange) []homeBarTarget {
	back := homeSheetBackWord
	if a.pal.ascii {
		back = homeSheetBackASCII
	}
	bar := []homeBarTarget{
		{word: back, do: func(a *app) tea.Cmd { a.closeHomeSheet(); return nil }},
		{word: homeSheetSendWord, do: func(a *app) tea.Cmd { return a.exchangeEnter(ex) }},
	}
	if ex.offering() {
		bar = append(bar, homeBarTarget{
			word: homeSheetMoreWord,
			do:   func(a *app) tea.Cmd { return a.promoteExchange(ex) },
		})
	}
	return bar
}

// toggleSheetMore is `→` (and `m`, the sheet's modal synonym) and the fold
// line together: it opens the bookkeeping fold AND every list-shaped band on
// the card, and closes the lot again.
//
// ONE GESTURE FOR ONE INTENT. `→` on a wide card means "show me everything this
// row has" ([app.setAllBandFolds]); a phone that made it mean "show me the
// three bands I moved behind a fold" would be the same key meaning two things at
// two widths.
func (a *app) toggleSheetMore(subject bandSubject) {
	if a.home.bandOpen == nil {
		a.home.bandOpen = map[string]bool{}
	}
	key := bandFoldKey(homeSheetMoreKey, subject)
	open := !a.home.bandOpen[key]
	a.home.bandOpen[key] = open
	for _, band := range homeBandsFor(subject.kind) {
		a.home.bandOpen[bandFoldKey(band.name, subject)] = open
	}
	a.touch()
}

// homeSheetOpen is the door the sheet's title offers: whatever enter meant on
// the row before the sheet was over it.
func (a *app) homeSheetOpen() tea.Cmd {
	line, ok := a.home.focusedLine()
	if !ok {
		return nil
	}
	a.closeHomeSheet()
	switch line.kind {
	case homeItem:
		return a.homeItemEnter(line)
	case homePhoneNews:
		if line.note == nil || !line.note.hasRow {
			return nil
		}
		return a.homeOpenRow(line.note.row, line.dir)
	case homeSession:
		// NOT [app.homeEnter], which at this tier is the key that OPENS THIS
		// SHEET ([app.homePhoneEnter]) — asking it again from inside the sheet
		// would raise the sheet a person is standing in. The session half of it
		// is one function below, and it is the one the door needs.
		return a.homeOpenRow(line.row, line.dir)
	}
	return nil
}

// homeOpenRow is [app.homeEnter]'s session half, reached from a row that is not
// itself a conversation — a `since you left` line standing for one.
//
// IT GOES THROUGH THE ONE DOOR ([app.homeOpenLine], home.go) rather than
// spelling the checks again. It used to have its own, older shape: it refused
// another project's row with `elsewhere · <path>` and opened this one's through
// the same-workspace seam, both of which stopped being true when enter learnt to
// open any project on the screen.
func (a *app) homeOpenRow(row session.SessionRow, dir string) tea.Cmd {
	if row.Transcript == a.file {
		a.closeHome()
		return nil
	}
	return a.homeOpenLine(homeLine{kind: homeSession, dir: dir, row: row, project: a.home.projectNameOf(dir)})
}

// ── the gestures ────────────────────────────────────────────────────────────

// homeSheetKeyFirst routes a key while the sheet is up, and reports whether it
// took it. It stands down for an ERRAND's sheet, which is held by the exchange's
// own keys (homeexchange.go's [app.exchangeKey]) — one pane, one keyboard.
func (a *app) homeSheetKeyFirst(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.homePhone() {
		return nil, false
	}
	if !a.homeSheetShowing() {
		// A SHEET THE EXCHANGE HANDED BACK IS CLOSED. `tab` and `esc` inside the
		// pane clear its focus, and a flag left standing would re-raise the sheet
		// over the next row the cursor walked onto.
		a.home.sheet = homeSheet{}
		return nil, false
	}
	if _, ok := a.homeSheetErrand(); ok {
		return nil, false
	}
	defer a.touch()
	a.home.say("", "")
	switch msg.String() {
	case "esc", "left", "q":
		a.closeHomeSheet()
		return nil, true
	case "enter":
		return a.homeSheetOpen(), true
	case "up", "k":
		a.homeSheetScroll(-1)
		return nil, true
	case "down", "j":
		a.homeSheetScroll(1)
		return nil, true
	case "pgup", "ctrl+b":
		a.homeSheetScroll(-a.homeSheetPage())
		return nil, true
	case "pgdown", "ctrl+f":
		a.homeSheetScroll(a.homeSheetPage())
		return nil, true
	case "home", "g":
		a.home.sheet.top = 0
		return nil, true
	case "end", "G":
		a.home.sheet.top = 1 << 20
		return nil, true
	case "m", "right":
		// THE SHEET MAY KEEP ITS BARE LETTERS. It is modal and has no box, so
		// a letter here cannot be the start of anybody's sentence — but the
		// wide card's legend teaches `→` now (homeband_keys.go), and a key a
		// legend teaches works at every width. Both spellings, one meaning.
		if subject, ok := a.homeSheetSubject(); ok {
			a.toggleSheetMore(subject)
		}
		return nil, true
	case "p", "s", "ctrl+e", "ctrl+x":
		// THE ITEM'S OWN KEYS, working here exactly as they do on the row
		// (homestanding.go's [app.homeItemWrite]). The sheet is the card, and
		// the card names `ctrl+e pause` and `ctrl+x stop`; the bare pair stays
		// as a synonym for the same modal reason as `m` above.
		if line, ok := a.home.focusedLine(); ok && line.kind == homeItem {
			if msg.String() == "p" || msg.String() == "ctrl+e" {
				return a.homeItemWrite(line, standing.StatusPaused), true
			}
			return a.homeItemWrite(line, standing.StatusRetired), true
		}
		return nil, true
	case "ctrl+v":
		// AND THE ITEM'S RUNG, at this width too, on the rule above: a key the
		// card's legend names works wherever the card is drawn (homeeffort.go).
		// An item's rung is the only one this chord moves anywhere now, so there
		// is nothing else it could mean here.
		if line, ok := a.home.focusedLine(); ok && line.kind == homeItem {
			return a.cycleItemEffort(line.item), true
		}
		return nil, true
	}
	// A DIGIT ANSWERS THE QUESTION THE SHEET IS SHOWING, which is the same rule
	// the wide card keeps and the same function behind it (homeband_answer.go).
	if cmd, took := a.answerKey(msg.String()); took {
		return cmd, true
	}
	return nil, true
}

// homeSheetPage is a screenful of the sheet, one row shy so a page turn keeps a
// line of context — the courtesy [app.expandPage] pays its own sheet.
func (a *app) homeSheetPage() int {
	_, height := a.size()
	if page := height - 7; page > 1 {
		return page
	}
	return 1
}

func (a *app) homeSheetScroll(delta int) {
	a.home.sheet.top += delta
	if a.home.sheet.top < 0 {
		a.home.sheet.top = 0
	}
	a.touch()
}

// homeSheetPress resolves a tap on the sheet. Every row was laid out with what
// it answers to, so this is a lookup rather than a second geometry.
func (a *app) homeSheetPress(x, y int) tea.Cmd {
	width, height := a.size()
	a.homeSheetFrame(width, height)
	if ex, ok := a.homeSheetErrand(); ok {
		if cmd, took := a.homeBarPress(x, y, a.homeErrandBar(ex)); took {
			return cmd
		}
		if row, column, ok := a.homePane(x, y); ok {
			return a.exchangePress(column, row)
		}
		if y >= 0 && y < len(a.home.sheetHits) && a.home.sheetHits[y].kind == homeSheetHitBack {
			a.closeHomeSheet()
		}
		return nil
	}
	subject, ok := a.homeSheetSubject()
	if !ok {
		return nil
	}
	if cmd, took := a.homeBarPress(x, y, a.homeSheetBar(subject)); took {
		return cmd
	}
	if y < 0 || y >= len(a.home.sheetHits) {
		return nil
	}
	a.home.say("", "")
	hit := a.home.sheetHits[y]
	switch hit.kind {
	case homeSheetHitBack:
		a.closeHomeSheet()
	case homeSheetHitOpen:
		return a.homeSheetOpen()
	case homeSheetHitAnswer:
		if cmd, took := a.answerKey(hit.key); took {
			a.touch()
			return cmd
		}
	case homeSheetHitMore:
		a.toggleSheetMore(subject)
	case homeSheetHitTask:
		// THE TASK'S OWN RECORD, which is what enter on that row opens on the
		// task page (taskview.go's [app.taskSheetInside]). A tap on a task here
		// is a tap on the same task there, and it opens the same card.
		if hit.index >= 0 && hit.index < len(subject.row.Tasks.Rows) {
			return a.openTaskRecord(&subject.row.Tasks.Rows[hit.index])
		}
	}
	a.touch()
	return nil
}
