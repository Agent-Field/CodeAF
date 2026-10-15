package tui3

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// placeMoneyInk is the one semantic door onto money's ink, so a palette move
// cannot leave one reading behind with the old meaning.
//
// IT IS [palette.money] AND NO LONGER [palette.add]. The design's own preamble
// spends green on money — "green = money" — and this file was reaching for the
// tick's olive instead, which said that finishing and paying are one event.
// styles.go's [hueMoney] is the mint the design names, held at this table's own
// lightness so it sits inside the conversation's signal band.
func placeMoneyInk(pal palette) func(string) string { return pal.money }

// needsCheckWord is the name of the `unread` group of `needs you`
// (homepanel_needs.go), spelled once here because the group line, the fold and
// the manual all quote it.
//
// THE WORD IS THE WHOLE EXPLANATION. The difference between a row that has
// stopped a conversation and a landing nobody has looked at used to be spelled
// under every landing — `landed unchecked · enter to look`, the same sub-line on
// nine rows (owner, 2026-09-11) — then once, as a clause at the group line's
// right margin (`finished, nobody has checked it`). The owner cut the clause on
// 2026-09-15: `unread` says it in one word, the way a mailbox does, and the
// group line carries neither a clause nor a count. The rows under it are one
// line each.
const needsCheckWord = "unread"

// ── THE FIVE-LEVEL SCALE (SCREEN 2a) ────────────────────────────────────────
//
// A terminal has no font sizes, so a place's hierarchy is five levels built
// from brightness, weight, case and air — and each is spelled ONCE, here, so a
// place cannot light a heading or bold a fact without saying so in this file:
//
//	edge      [placeLead] — one cell in, for every heading and every mark
//	page      the tab bar alone, bold, and nowhere in a body
//	section   [placeHeading] — lowercase, muted, one blank row above it
//	subject   [placeSubject] — the reading ink, bold inside the band
//	note      [placeFactInk] — dim, lifted to the reading ink inside the band
//	margin    the same ink as a note, flushed right
//
// and one ground for both hands, [placeBand]. SECTIONS ARE MUTED AND NOT DIM,
// which is where this departs from the screen's own mock: DESIGN-LANGUAGE's
// accent budget says headings, band labels and wordmarks wear muted, and one
// heading ink across the bar is the law this scale exists for. The accent is
// spent on the live thing and nothing here.

// placeHeading is a section heading on a place.
func placeHeading(text string, pal palette) string { return placeHeadingInk(pal)(text) }

// placeHeadingInk is THE ONE HEADING INK, for every place's section words and
// every one of home's panel headings (homecell.go's [homeCellHead]). Moving
// headings between muted and dim is this line and nothing else, so home and
// the places can never be moved apart by a change that remembered one of them.
func placeHeadingInk(pal palette) func(string) string { return pal.muted }

// placeSubject is the thing a row is about. COLOUR IS STROKE, NEVER FILL: the
// row's glyph carries its state and the words beside it keep the ordinary ink
// (docs/DESIGN-LANGUAGE.md), so a finished task and a running one are told
// apart by their marks rather than by a second ink on their titles.
func placeSubject(text string, lit bool, pal palette) string { return placeSubjectInk(lit, pal)(text) }

// placeSubjectInk is [placeSubject] as an ink, for the painters that are handed
// one.
func placeSubjectInk(lit bool, pal palette) func(string) string {
	if lit {
		return func(s string) string { return pal.bold(pal.ink(s)) }
	}
	return pal.ink
}

// placeFactInk is the ink of what is true about a row and of its margin: dim,
// and the reading ink inside the band — dim grey on a raised ground is grey on
// grey, and the facts are the half of the row a person stopped on it to read.
func placeFactInk(lit bool, pal palette) func(string) string {
	if lit {
		return pal.ink
	}
	return pal.dim
}

// placeLead is THE ONE LEFT EDGE: the cell every place's body starts one in
// from, where the pulse, the composer and the hint start and where home hangs
// its headings. A row's mark stands on it and the row's words start two cells
// after the mark; a heading starts on it. Before it the bodies started at
// columns 0, 1 and 2 depending on the place, so walking the bar the body
// stepped sideways (PLACES-AUDIT.md finding 12).
const placeLead = " "

// placeBand is the ground under the row the cursor or the pointer is on. They
// are ONE step (THE GROUND LADDER): a place has nothing open, so nothing on it
// wears the selected step.
func placeBand(text string, width int, pal palette) string { return pal.cursor(text, width) }

// ── a place with nothing in it ──────────────────────────────────────────────

// placeBlank is what one place says while it holds nothing: the heading its
// list will stand under, and the one line under that heading.
type placeBlank struct {
	// heading is "" where the place's own word is the heading, which is every
	// place today; a place whose page wants a longer heading than its tab word
	// names it here.
	heading string
	whisper string
}

// placeWhisper is THE COPY OF RECORD for an empty place, one sentence each, and
// the manual quotes it from here.
//
// NO WHISPER CARRIES AN ELLIPSIS, not even a quoted one — home's rule for its
// panels (homegrid.go's [homeWhisper]), for home's reason: a whisper wraps
// rather than being cut ([placeWhisperLines]), so a `…` on one of these lines
// could only be read as the screen having run out of room. Standing's example
// was `"every morning, …"` until it took home's `"every morning at 9"`.
//
// A WHISPER NAMES WHAT ARRIVES AND THE ONE THING THAT PUTS IT THERE — the rule
// home's panels already keep (homegrid.go's [homeWhisper], DESIGN.md §4). It
// never says the place is empty: `no tasks yet`, `nothing learned yet` and
// `nothing spent yet` were each a paragraph ending in that sentence, which is
// the emptiness law inverted into words (docs/DESIGN-LANGUAGE.md, "presence
// over labels"). Settings is absent because it is never empty.
var placeWhisper = map[page]placeBlank{
	pageTasks:       {whisper: "work you send off with /task lands here, and its record stays"},
	pageSpend:       {whisper: "every chat and task is priced here as it runs"},
	pageAutomations: {whisper: `reminders, scheduled work and watches · "remind me at 6" or "every morning at 9"`},
	pageMemory:      {whisper: "what it has learned about you and this machine · /remember adds a line"},
}

// placeWhisperLead is where the whisper hangs: the place's own lead, then the
// gutter home hangs a panel's whisper in (homecell.go's [homeCellLeadBlank]), so
// the two stand in one column by construction.
var placeWhisperLead = placeLead + homeCellLeadBlank

// placeWhisperLines is an empty place's rows: the heading in the muted tier
// every heading on this surface wears, and the whisper dim under it.
//
// THE WHISPER WRAPS; IT IS NEVER CUT. It takes the dim lines it needs from the
// one wrapper home's panels use ([homeWhisperLines]), handed the width to the
// right of the place's lead. It used to give up its example after the middle
// dot and then take an ellipsis where there was no clause left to give, so at
// forty-four columns tasks read `work you send off with /task lands here, and…`
// — the half a person needed was the half that went (DESIGN.md §4).
func placeWhisperLines(id page, width int, pal palette) []string {
	blank, ok := placeWhisper[id]
	if !ok || width < len(placeWhisperLead)+1 {
		return nil
	}
	heading := blank.heading
	if heading == "" {
		heading = id.word()
	}
	lines := []string{placeLead + placeHeading(fit(heading, width-len(placeLead)), pal)}
	for _, words := range homeWhisperLines(blank.whisper, width-len(placeLead)) {
		lines = append(lines, placeWhisperLead+pal.dim(words))
	}
	return lines
}

// placeWindowStep is SCREEN 3d'S FOUR KEYS, and it is one function because
// there is one answer.
//
// Time is two dimensions — which window, and how coarse — so it gets two arrow
// axes rather than three letters: `shift+←→` pages the window by its own length
// and `shift+↑↓` zooms the grain. Every place that has a window answers exactly
// these four and no others, and a place that wrote its own switch would be a
// fourth chance for one of them to disagree about what `shift+↑` means. The
// arithmetic itself is [session.UsageWindow]'s, which is the one window
// vocabulary this surface has.
//
// A KEY THAT IS NOT ONE OF THE FOUR ANSWERS THE WINDOW IT WAS GIVEN, so a caller
// can compare and learn whether anything actually moved — which is what lets a
// place refuse to redraw for a keystroke that changed nothing.
func placeWindowStep(win session.UsageWindow, key string) session.UsageWindow {
	switch key {
	case "shift+left":
		return win.Step(-1)
	case "shift+right":
		return win.Step(1)
	case "shift+up":
		return win.Coarser()
	case "shift+down":
		return win.Finer()
	}
	return win
}

// The window control as screen 3d draws it: the label BETWEEN THE ARROWS, which
// is the control and the reading at once. A person reads the span they are
// looking at and the keys that move it in one glance, on the row that reports
// it, rather than learning a chord from a foot note somewhere else.
//
// A WINDOW WITH NO SPAN DRAWS NOTHING — not the arrows, not an empty pair of
// them. [session.UsageWindow.Label] answers "" for a window nobody has chosen
// yet, and arrows around nothing would be a control over no reading.
const (
	placeWindowBack = "shift+← "
	placeWindowOn   = " →"
)

// placeWindowWords is the control as PLAIN TEXT, which is what a caller measures
// against the frame it has. It is separate from the painted form because a
// string with escape sequences in it cannot be clipped or counted safely, and a
// header deciding whether the control fits has to do both.
func placeWindowWords(win session.UsageWindow) string {
	label := win.Label()
	if label == "" {
		return ""
	}
	return placeWindowBack + label + placeWindowOn
}

// placeWindowRow is that same control, painted: the arrows dim because they are
// the instruction, the label in the reading tier because it is the fact.
func placeWindowRow(win session.UsageWindow, pal palette) string {
	label := win.Label()
	if label == "" {
		return ""
	}
	return pal.dim(placeWindowBack) + pal.ink(label) + pal.dim(placeWindowOn)
}

// placeGrainWords is THE SECOND AXIS, NAMED — `shift+↑ coarser` and its inverse
// — and it names every direction that would actually move.
//
// A window already on days cannot get finer and a window on months cannot get
// coarser, so the clause is one key at either end of the ladder and two in the
// middle. That is the same law the verb strip keeps: a key drawn is a key bound,
// and a key that would do nothing is not offered.
func placeGrainWords(win session.UsageWindow) string {
	switch win.Normalized().Grain {
	case session.GrainMonth:
		return placeFinerWords
	case session.GrainWeek:
		return placeCoarserWords + " · " + placeFinerWords
	default:
		return placeCoarserWords
	}
}

const (
	placeCoarserWords = "shift+↑ coarser"
	placeFinerWords   = "shift+↓ finer"
	// placeHeadGap is the least air between what a place is called and the
	// control at the other end of its line. Two cells would technically fit and
	// would read as one run-on row; four is a gap a person's eye reads as a gap,
	// which is screen 2a's own device — air where there is no fifth brightness
	// tier to spend.
	placeHeadGap = 4
)

// placeWindowFits is what one head row HAS ROOM FOR: the arrows around the
// label, and the grain clause beside them.
//
// ONE PREDICATE ANSWERS THE PAINT AND THE KEYS, on every place that has a time
// window, and that is what keeps the surface honest: a capability that cannot
// work is absent rather than broken, so on a frame too narrow for the label the
// arrows are not drawn AND the keys do nothing — and the same, separately, for
// the zoom. A control bound but invisible is the exact defect the verb strip
// exists to end.
//
// AT [tierPhone] THERE IS NO WIDTH TO SHARE. A list wraps its tail onto a line
// of its own there rather than cutting both halves in half, and the control is
// the half a phone can most afford to lose.
func placeWindowFits(width int, head string, win session.UsageWindow) (arrows, grain bool) {
	words := placeWindowWords(win)
	if words == "" || phoneList(width) {
		return false, false
	}
	used := len(placeLead) + ansi.StringWidth(head) + ansi.StringWidth(words) + placeHeadGap
	if width < used {
		return false, false
	}
	return true, width >= used+ansi.StringWidth(placeGrainWords(win))+placeHeadGap
}

// placeHeadRow is THE ONE HEAD ROW every place with a time window draws: what
// the place is on the left, and on the right the window exactly as SCREEN 3d
// draws it — `shift+← aug 12 – aug 25 →`, the control and the reading at once,
// with the grain clause beside it where the line has room.
//
// IT IS ONE FUNCTION BECAUSE THERE IS ONE CONTROL. Standing drew this pair, the
// spend place drew a legend of its own that named the keys and not the span, and
// the tasks place drew neither while binding all four keys — three answers to one
// question, and the third was the exact defect this file's law is written
// against. What differs between the three places is the sentence on the LEFT,
// which is the only thing any of them knows that the others do not.
// head is the left field as it is MEASURED and painted as it is DRAWN. The two
// are separate arguments because a painted string cannot be measured — an escape
// sequence takes no cells and every one of them would be counted — and a head
// row that measured its own colours would put the control off the right edge.
// An empty `painted` means "paint it dim", which is what a place name wants.
func placeHeadRow(width int, head, painted string, win session.UsageWindow, pal palette) string {
	if painted == "" {
		painted = placeHeading(head, pal)
	}
	arrows, grain := placeWindowFits(width, head, win)
	if !arrows {
		// A HEAD WITH NO ROOM FOR ITS CONTROL KEEPS ITS OWN INK where it fits
		// whole; only a head too long for the row is cut, and cut dim.
		if ansi.StringWidth(head) <= width-len(placeLead) {
			return placeLead + painted
		}
		return placeLead + pal.dim(fit(head, width-len(placeLead)))
	}
	right := placeWindowRow(win, pal)
	plainRight := placeWindowWords(win)
	if grain {
		right += pal.dim("  " + placeGrainWords(win))
		plainRight += "  " + placeGrainWords(win)
	}
	gap := width - len(placeLead) - ansi.StringWidth(head) - ansi.StringWidth(plainRight)
	if gap < 1 {
		gap = 1
	}
	return placeLead + painted + strings.Repeat(" ", gap) + right
}

// foldWords is THE ONE SENTENCE a fold says, and the reason it lives here
// rather than beside either of its callers is that home spelled it twice for a
// wave: the retired flat list said `▸ 4 more, quiet since sep 1` while the phone and the project tails said `▸ …7 more, quiet since 3h`
// ([homeQuietWord]) — a leading ellipsis on one and not the other, and a
// calendar date against an elapsed span, for one idea.
//
// A SHUT FOLD SAYS HOW MANY IT HIDES; AN OPEN ONE SAYS THE WAY BACK. The count
// is the same number both ways — what a fold stands over is counted at the cap
// and never at what is drawn — but `12 more` over a list already showing all twelve is a sentence that is
// not true, so an open fold is `12 fewer`, which is what pressing it does.
//
// THE MARK IS THE CALLER'S. Home draws `>` and `v` in an ASCII palette and the
// places draw `▸` from the token table, so a speller that owned the glyph would
// have to know which of them was asking.
func foldWords(open bool, n int, clause string) string {
	if open {
		return groupedInt(n) + " fewer"
	}
	line := groupedInt(n) + " more"
	if clause = strings.TrimSpace(strings.TrimPrefix(clause, ",")); clause != "" {
		line += ", " + clause
	}
	return line
}

// foldLine is [foldWords] wearing the shut mark, for the folds that are only
// ever shut — the command list's, home's quiet tail. A fold a place draws
// over its own rows is a door both ways instead ([foldDoor]).
func foldLine(n int, clause string) string {
	return tokens.GlyphCollapsed + " " + foldWords(false, n, clause)
}

// foldDoor is a fold line that is a door both ways — `▸ 11 more` shut and
// `▾ 11 fewer` open — for the lists that draw their first few rows and the rest
// on `enter` or a click (the spend place's subjects, a search's tail, a memory
// shelf). hidden is how many rows the shut fold keeps back.
func foldDoor(open bool, hidden int, clause string) string {
	mark := tokens.GlyphCollapsed
	if open {
		mark = tokens.GlyphExpanded
	}
	return mark + " " + foldWords(open, hidden, clause)
}

// foldEnterWord is the foot's `enter` clause while the cursor is on a
// [foldDoor].
func foldEnterWord(open bool) string {
	if open {
		return "enter folds them"
	}
	return "enter shows the rest"
}

// foldSpellings is the fold line at EVERY LENGTH IT WILL GIVE WAY THROUGH,
// widest first, so a caller with a measured row can walk down it and take the
// first rung that fits.
//
// THE MARK IS ON EVERY RUNG AND THE WORDS ARE WHAT GO. `▸` is the part that
// carries the meaning — it says a list has more behind it and that the line is a
// door — and `more` only says it again in letters. A ladder that dropped the
// glyph first would leave `+3`, which cannot be told from a count, a badge or a
// door; the narrow place bar spelled it exactly that way for a wave while the
// command menu one file over said `▸ 3 more` about the same idea.
func foldSpellings(n int, clause string) []string {
	return []string{
		foldLine(n, clause),
		foldLine(n, ""),
		tokens.GlyphCollapsed + " " + groupedInt(n),
	}
}

// quietFoldClause is the one spelling of HOW LONG the rows behind a fold have
// been quiet, and it is [sinceAt] — the same ladder every row's own age is
// drawn with — because a fold and the rows it stands over sit on the same list
// and get compared. A fold that said `quiet since sep 1` beside rows reading
// `3h` and `4d` was asking a person to convert between two units to find out
// whether those meant the same day; and past thirty days [sinceAt] reaches for a
// calendar date by itself, which is exactly when the elapsed form stops being
// readable.
//
// AN AGE OF `now` IS NO CLAUSE AT ALL. "quiet since now" is a contradiction, and
// the emptiness law would rather the sentence stopped after the count.
func quietFoldClause(at, now time.Time) string {
	age := sinceAt(at, now)
	if age == "" || age == "now" {
		return ""
	}
	return "quiet since " + age
}

// appendPlaceSection gives consecutive blocks exactly one breath without
// growing a second blank when two callers describe the same boundary.
func appendPlaceSection(rows []string, heading string) []string {
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) > 0 {
		rows = append(rows, "")
	}
	return append(rows, heading)
}

// groupedInt is the one thousands spelling for reading-layer counts.
func groupedInt(n int) string { return groupDigits(strconv.Itoa(n)) }

// groupDigits marks the thousands in a run of digits, and it is THE ONE PLACE
// that mark is put in.
//
// [groupedInt] spells counts with it and [dollars] spells money with it, and
// before they shared this they did not agree: the spend place drew `128,400
// calls` and `$4210.55` on one row, the count grouped and the money not, which
// is a row that has been laid out by two people. It takes the digits rather
// than the number because money has already been rounded to its two places by
// the time it gets here, and rounding a figure twice is how the halfpenny goes
// missing.
func groupDigits(digits string) string {
	for at := len(digits) - 3; at > 0; at -= 3 {
		digits = digits[:at] + "," + digits[at:]
	}
	return digits
}
