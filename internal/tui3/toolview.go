package tui3

import (
	"encoding/json"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// THE TOOL CLUSTER (docs/CHAT-V3.md D11).
//
// A turn's calls are one object on the screen, not a stream of them: a rail
// down the left, one row per call, the elbow closing the run.
//
//	├─▶ read internal/session/session.go   · 189 lines
//	├─▶ edit internal/session/loop.go      +3 −1
//	│ internal/session/loop.go
//	│ @@ -1,4 +1,4 @@
//	│   // argsLimit bounds Event.Args.
//	│ -const argsLimit = 400
//	│ +const argsLimit = 8192
//	╰─▶ bash go test ./internal/session    exit 1 ✗
//
// The grammar is four parts and every one of them is a decision:
//
//   - the RAIL says these rows are one thing. `╰─▶` is the last call, `├─▶`
//     every call above it, `│ ` an expanded call's detail; a terminal that
//     cannot draw them gets `+-> ` and `| `, which are the same widths.
//   - the NAME is chrome, so it is the muted accent. The TARGET — the path,
//     the command, the pattern — is what the person is actually reading, so it
//     leads: primary ink, and never dimmed as a whole. What QUALIFIES it — a
//     `cd` prefix, a line range, the directory a pattern is searched in —
//     recedes to dim within it (see the parameter hierarchy, below).
//   - the STAT trails, dim, and is derived from the payload in toolstat.go.
//   - the MARK: the call's state at the line's right end, and its elapsed time
//     once there is one. NO SUCCESS GLYPH, EVER. A column of ✓ is a column that
//     must be read to learn nothing; a quiet line is a success.
//
// At most [toolWindow] calls stay on screen; the rest fold into one line that
// says how many, and ctrl+o (or a click on that line) unfolds them. One call
// opens inline — click it, or select it with ↑/↓ and press enter — and shows a
// tool-shaped expansion under the rail rather than in a pane somewhere else.
// A TASK'S PAGE KEEPS AS MANY AS ITS VIEW IS TALL instead of three, and there a
// scroll up at the top of the page opens the fold too (render.go's
// [deck.toolTail], room.go's [app.roomScroll]).

// toolDetail is a call's payload as the surface holds it: the two display
// fields internal/session sends (session.go's Event.Args and Event.Output),
// unparsed. Everything derived from them — the stat, the diff, the previews —
// is computed in toolstat.go at render time, so this struct never has to be
// invalidated when a colour or a cap changes.
type toolDetail struct {
	// Args is the call's arguments, the compacted JSON the model sent.
	Args string
	// Output is what the call returned, capped by session for display.
	Output string
}

// ── THE STATE MACHINE (this wave) ──
//
// Every wide row is the same two things: a SENTENCE that starts at the rail, and
// a RIGHT COLUMN that ends at the frame. The sentence says what the call is
// pointed at; the column says how it is going and what it cost.
//
//	├─▶ edit internal/session/loop.go                                        ◌
//	│ pending                                 …and here is the change it will make
//	│ @@ -1,4 +1,4 @@
//	│ -const argsLimit = 400
//	│ +const argsLimit = 8192
//	├─▶ bash rm -rf build                                                    ?
//	├─▶ bash go test ./internal/tui3                                      ⠋ 4s
//	├─▶ read internal/session/loop.go                          189 lines · 0.4s
//	╰─▶ bash go build ./…                                     ✗ exit 1 · 1.2s
//
// The states, and what the column says in each:
//
//   - QUEUED — the model asked, nothing ran. An empty circle, dim, alone.
//   - CONSENT — waiting on YOU, the whole row violet, and a "?" in the column.
//   - RUNNING — the spinner, and its own age beside it: ⠋ 4s. A spinner is a
//     claim that something is turning and says nothing else, so a two-minute
//     `go test` carrying only a spinner is indistinguishable from a two-second
//     one; the clock is the difference. NOTHING ELSE ANIMATES ON THIS SURFACE.
//   - DONE — what the call came to, then how long it took: 189 lines · 0.4s.
//     NO SUCCESS GLYPH, EVER: a column of ✓ is a column that must be read to
//     learn nothing, and a quiet line is a success.
//   - FAILED — the ✗ leads the column, and the row is already open.
//
// The spinner used to cover all four of the first states, and that was the
// defect: a mutating call spun while the RESPONSE was still streaming and
// nothing had started, and a call parked on a consent question spun exactly
// like one doing work.
//
// THE COLUMN DROPS WHOLE SEGMENTS RATHER THAN CLIPPING CHARACTERS, in a stated
// order — the size first, then the duration, and the mark is never given up (see
// [app.toolTail]). Half a figure is worse than no figure: "12.4 K" is a number a
// person has to distrust, and a column cut two cells short of its spinner is a
// row that claims nothing is happening. Below the floor for the mark alone, the
// column is absent entirely, which is the emptiness law.
//
// The figures are the call's OWN — [elapsedWord] is begin to end of the tool,
// never the turn's span — and one too fast to have a duration draws none.

// clusterRows lays out one contiguous run of tool entries — d.entries[from:to],
// all from one turn — and appends it to out. This is where the fold lives,
// because folding is a property of the RUN and not of any call in it, and it is
// where the elbow is chosen for the same reason.
//
// The deck is carried in rather than read off the app because a task's page is
// drawn by this function too, from its own list and its own fold state
// (render.go's [deck], room.go): one cluster renderer, two lists.
//
// THE WINDOW IS THE DECK'S AND NOT A CONSTANT'S. The conversation keeps
// [toolWindow]; a room keeps a tail sized to its view ([deck.window]), so that a
// fold never starves the screen — only the overflow folds. One renderer, two
// lists, and the fold's sentence names the gesture each list answers.
func (a *app) clusterRows(d deck, out []row, from, to, width int) []row {
	turn := d.entries[from].turn
	start := from
	if window := d.window(); to-from > window && !d.unfolded[turn] {
		start = to - window
		word := foldWord(start-from, d.toolTail > 0)
		fold := a.pal.dim(a.pal.toolGlyph() + word)
		if a.hoveringFold(turn) {
			fold = a.pal.accent(a.pal.toolGlyph()) + a.pal.dim(word)
		}
		out = append(out, row{text: fold, entry: -1, hit: hitFold, turn: turn})
	}
	for i := start; i < to; i++ {
		out = append(out, a.toolRows(d, i, i == to-1, width)...)
	}
	return out
}

// foldWord is the fold line's sentence. It names the key that opens it, because
// a surface that hides something without saying how to see it has hidden it —
// and on a task's page, where scrolling up at the top opens it too, it names
// the scroll first, because that is the gesture a person reading history is
// already making (room.go's [app.roomScroll]).
func foldWord(n int, scrolls bool) string {
	opens := foldKeyWord
	if scrolls {
		opens = foldScrollWord
	}
	if n == 1 {
		return "1 earlier tool call" + opens
	}
	return strconv.Itoa(n) + " earlier tool calls" + opens
}

// The two endings of the fold's sentence: the conversation's, and the room's.
// They are constants because the manual quotes them and a test pins each.
const (
	foldKeyWord    = " · ctrl+o"
	foldScrollWord = " · scroll up or ctrl+o"
)

// toolRows is one call: its line, plus its expansion when it is open.
//
// It returns rows rather than strings — unlike every other entry — because the
// "… N more lines" foot of a capped expansion is a DIFFERENT click target from
// the line it hangs under: one lifts the cap, the other closes the call.
func (a *app) toolRows(d deck, i int, last bool, width int) []row {
	e := &d.entries[i]
	hit := hitTool
	if replayInert(e) {
		hit = hitNone
	}
	// A CALL THAT IS STILL ARRIVING ANSWERS NO POINTER. There is nothing to open:
	// the call has not been announced, so no payload exists to expand and no
	// result is coming — which is [replayInert]'s rule arrived at from the other
	// direction, a row that brightened under the pointer promising an answer it
	// does not have. It becomes an ordinary row the moment the announcement
	// brings the payload with it.
	//
	// IT DOES HANG THE FILE IT IS WRITING, THOUGH, and that block is the one
	// thing on this surface drawn from an unfinished call's arguments. It is not
	// parsed — session's [session.PartialString] scans the streamed text for one
	// field and app.go keeps the answer (see [entry.formed]) — and it is not a
	// claim about what the call will do: it is the last lines of a file that is
	// visibly being typed, which is the whole of what a person watching a long
	// write wants and none of what a preview promises.
	if e.status == toolForming {
		out := []row{{text: a.toolLine(e, i, last, width), entry: i, hit: hitNone}}
		stem := a.pal.railCont()
		phone := layoutTier(width) == tierPhone
		for _, line := range a.formingRows(e, width-ansi.StringWidth(stem), previewCap(phone)) {
			out = append(out, row{text: a.pal.dim(stem) + line, entry: i, hit: hitNone})
		}
		return out
	}
	out := []row{{text: a.toolLine(e, i, last, width), entry: i, hit: hit}}
	stem := a.pal.railCont()
	room := width - ansi.StringWidth(stem)

	// THE LIVE PREVIEW. A call that has not finished shows what it is about to
	// do — the diff an edit will apply, the content a write will lay down — with
	// no click and no waiting, because the moment that answer is worth anything
	// is the moment BEFORE it happens. It is drawn from the arguments, which are
	// the whole of what has arrived; nothing here waits for a result.
	//
	// AT tierPhone THIS IS THE ONLY BLOCK A ROW EVER HANGS, open or not. The
	// expansion goes over the whole frame instead (expand.go): forty-four
	// columns cannot carry a rail, a stem and a unified diff at once, and a
	// detail block squeezed into what is left is a thing a person scrolls PAST
	// rather than reads. The preview stays at every tier because it is the
	// change shown BEFORE it lands — the one block nobody asked for and
	// everybody wants — but at tierPhone it is bounded HARDER, because twelve
	// rows nobody asked for is most of a phone frame ([previewPhoneWindow]).
	phone := layoutTier(width) == tierPhone
	if !e.open || phone {
		// THE PICTURE IS THE OTHER BLOCK NOBODY ASKS FOR, and it hangs at the
		// far end of the same argument. The live preview shows a change BEFORE
		// it lands because that is the moment it is worth something; a picture
		// shows AFTER, because that is the only moment it exists — and in both
		// cases the row on its own cannot say the thing the person wants. It
		// takes the tier's cap for the tier's reason (imagepreview.go's
		// [app.pictureThumb]).
		if picture, drawn := a.pictureThumb(e, room, previewCap(phone)); drawn {
			for _, line := range picture {
				out = append(out, row{text: a.pal.dim(stem) + line, entry: i, hit: hitTool})
			}
			return out
		}
		head, body, more := a.previewBody(e, room, previewCap(phone))
		if head == "" {
			return out
		}
		out = append(out, row{text: a.pal.dim(stem) + head, entry: i, hit: hitTool})
		for _, line := range body {
			out = append(out, row{text: a.pal.dim(stem) + line, entry: i, hit: hitTool})
		}
		if more > 0 {
			out = append(out, a.moreRow(i, stem, more))
		}
		return out
	}

	body, more := a.detailBody(e, room)
	for _, line := range body {
		out = append(out, row{text: a.pal.dim(stem) + line, entry: i, hit: hitTool})
	}
	if more > 0 {
		out = append(out, a.moreRow(i, stem, more))
	}
	return out
}

// moreRow is the clickable foot of anything this file capped.
func (a *app) moreRow(i int, stem string, more int) row {
	return row{
		text:  a.pal.dim(stem + glyphMore + " " + strconv.Itoa(more) + " more lines"),
		entry: i,
		hit:   hitMore,
	}
}

// toolLine is the line itself: a sentence that starts at the rail, and a right
// column that ends at the frame.
//
// THE WIDTH IT IS GIVEN IS THE WIDTH IT TAKES, exactly, and that is the one law
// this function has. The pieces are measured as PLAIN text and painted
// afterwards, because a width measured through escape sequences is a width
// measured wrong — and nothing here is reserved for machinery that is not on
// the row. A line laid out two cells wider than the column it is drawn in is
// cut back by [app.railJoin] with an ellipsis, and the two cells it loses are
// the two the right column lives in: the defect that produced `0…` where a
// spinner should have been. So the indent every tool row is drawn with is
// subtracted HERE, from the frame's width, before anything is laid out in it
// (render.go's INDENT LAW is what applies it).
//
// When the row is too narrow for everything, the right column gives up whole
// segments in a stated order ([app.toolTail]) and the target is truncated last:
// the target is the substance, and a figure nobody has room for is a number
// about a line nobody can read. The target keeps [toolTargetFloor] cells
// whatever else is on the row.
func (a *app) toolLine(e *entry, i int, last bool, width int) string {
	// A CALL STILL ARRIVING IS ITS OWN SENTENCE, at every tier: what is on the
	// line is how much of the instruction has landed, not what the call did.
	if e.status == toolForming {
		return a.formingLine(e, i, last, width)
	}
	// THE PHONE HAS ITS OWN ROW, and it is a different sentence rather than this
	// one squeezed (see [app.toolLinePhone]). Every other tier reaches this line
	// unchanged, which is the whole contract of [layoutTier].
	if layoutTier(width) == tierPhone {
		return a.toolLinePhone(e, i, last, width)
	}
	// THE INDENT LAW COSTS THIS ROW TWO CELLS. Every tool row is drawn two
	// columns right of the frame's edge (workfold.go's [workIndent], applied by
	// render.go's pass), so the width this line may lay itself out to is the
	// frame's less that — and a line that spent the whole frame was cut back by
	// [app.railJoin] with an ellipsis over the two cells the right column lives
	// in. It is subtracted AFTER the tier has been chosen, because the tier is a
	// fact about the FRAME: a row that picked its tier from its own indent would
	// take the phone's shape two cells early.
	width -= workIndentCols(width)
	name, fallback := toolWords(e.tool, e.text)
	target := toolTarget(e.tool, e.detail.Args, e.text)
	if target == "" {
		target = fallback
	}
	rail := a.pal.rail(last)
	// MEASURED, NOT ASSUMED. [railWidth] is what the rail costs and every glyph
	// tier is drawn to it, but the row that has to add up is this one, so the
	// figure it adds up is the one it is about to draw.
	railCells := ansi.StringWidth(rail)
	nameWidth := ansi.StringWidth(name)

	// room is everything the sentence and the column have between them: the
	// frame, less the rail, the name, and the single space after the name.
	room := width - railCells - nameWidth - 1
	tail, tailWidth := a.toolTail(e, room-toolGap-toolTargetFloor)
	// And the target takes everything the column did not, which on a wide frame
	// is everything: no reservation is held back for machinery that is not on
	// this row.
	targetRoom := room
	if tailWidth > 0 {
		targetRoom -= toolGap + tailWidth
	}
	target, targetWidth := toolFit(e.tool, target, targetRoom)

	// A selected line takes the accent on its rail — no band, no marker
	// column, nothing that changes the width. Selection is a brightness here,
	// which is what a one-line row can carry honestly, and hover is the same
	// brightness for the same reason (hover.go). A call waiting on a person
	// takes the question hue instead, over the whole row.
	painted := a.pal.dim(rail)
	switch {
	case e.status == toolConsent:
		painted = a.pal.askBold(rail)
	case a.selected(i), a.hoveringEntry(i):
		painted = a.pal.accent(rail)
	}
	line := painted + a.paintName(e, name)
	used := railCells + nameWidth
	if target != "" {
		line += " " + a.paintTarget(e, target)
		used += 1 + targetWidth
	}
	if tailWidth == 0 {
		return line
	}
	// The column is flush against the frame's right edge, and the row adds up to
	// exactly the width it was given.
	pad := width - used - tailWidth
	if pad < 1 {
		pad = 1
	}
	return line + strings.Repeat(" ", pad) + tail
}

// toolGap is the space between the sentence and the right column. One cell,
// because a figure run straight into a path reads as part of the path.
const toolGap = 1

// toolTargetFloor is how many cells the target keeps before the row is allowed
// to spend any on the right column.
//
// Seven is the last few characters of a name and the ellipsis that says the rest
// was cut — the least that is still a FRAGMENT of a path rather than a stub of
// one. Both tiers read it, so the wide row and the phone row give the same thing
// up at the same moment, and it is the floor both of them have always had: the
// wide row spelled it as eight cells of a slot that included the target's
// leading space, and the phone row restated that as a constant of its own.
const toolTargetFloor = 7

// ── THE RIGHT COLUMN ────────────────────────────────────────────────────────
//
//	├─▶ web_fetch https://apnews.com/article/rates-…-c6e1f27a5c9b4e     ⠋ 4s
//	├─▶ read internal/session/loop.go                       189 lines · 0.4s
//	╰─▶ bash go test ./internal/tui3                       ✗ exit 1 · 1m02s
//
// The mark, then what the call came to, then how long it took — and a row that
// cannot hold all three sheds the size first, the duration next, and the mark
// never ([app.toolTail]).

// tailSeg is one segment of the column: the painted text, and the cells it takes.
// The two travel together because a width measured through escape sequences is a
// width measured wrong, and because [app.mark] is the one thing on this row
// whose glyph the palette chooses.
type tailSeg struct {
	text  string
	width int
}

// toolTail is the right column, painted, and the cells it took.
//
// IT DROPS WHOLE SEGMENTS RATHER THAN CLIPPING CHARACTERS, and the order is
// stated: the size goes first, then the duration, and the MARK IS NEVER GIVEN
// UP while there is room for it at all. Half a figure is worse than no figure —
// "12.4 K" is a number a person has to distrust — and the mark is the state of
// the call, which is the one thing on the row that cannot be inferred from
// anything else. Below the room for the mark alone the column is absent
// entirely, which is the emptiness law: nothing, rather than a bare ellipsis.
//
// A FAILURE'S ✗ LEADS THE COLUMN rather than trailing the sentence. It used to
// sit against the target, which put the loudest glyph on the surface at a
// different column on every row; a state belongs where every other state is,
// and a person scanning a cluster for the one that broke is scanning one column.
func (a *app) toolTail(e *entry, budget int) (string, int) {
	mark := a.mark(e)
	clockPlain, clockPainted := a.toolClock(e)
	return a.tailOf(e, tailSeg{text: mark, width: ansi.StringWidth(mark)}, clockPlain, clockPainted, budget)
}

// tailOf is the column itself, given what leads it and the clock the row tells
// time by. The two are handed in rather than read here so the drop order below
// is stated once and the tier decides nothing about it.
func (a *app) tailOf(e *entry, head tailSeg, clockPlain, clockPainted string, budget int) (string, int) {
	statPlain, statPainted := a.toolStat(e)
	// What the person answered when this call was asked about (consent.go). It
	// rides the size segment because it is the same kind of fact — dim, trailing,
	// about the call rather than in it — and because a row that was approved
	// must still read as one row.
	if e.decision != "" {
		statPlain, statPainted = joinFact(statPlain, statPainted, e.decision, a.pal.dim(e.decision))
	}
	// And what the person DID to the call while it ran: a foreground command
	// sent to the background with ctrl+g says which job it became
	// (background.go). It rides the same segment for the same reason, and it is
	// last, because it is the most recent thing to have happened to the row.
	if e.bg != "" {
		statPlain, statPainted = joinFact(statPlain, statPainted, e.bg, a.pal.dim(e.bg))
	}

	// THE DROP ORDER, WRITTEN DOWN ONCE: what the call came to, then how long it
	// took. The column sheds from the front of this list.
	figures := make([]tailSeg, 0, 2)
	if statPlain != "" {
		figures = append(figures, tailSeg{text: statPainted, width: ansi.StringWidth(statPlain)})
	}
	if clockPlain != "" {
		figures = append(figures, tailSeg{text: clockPainted, width: ansi.StringWidth(clockPlain)})
	}
	for from := 0; from <= len(figures); from++ {
		text, cells := a.joinTail(head, figures[from:])
		if cells <= budget {
			return text, cells
		}
	}
	if head.width > 0 && head.width <= budget {
		return head.text, head.width
	}
	return "", 0
}

// joinTail lays the column out: the mark, then the figures.
//
// The mark is followed by a SPACE and the figures by a DOT, and that is not
// decoration — the mark is the call's state and the figures are quantities about
// it, so "⠋ 4s" is a spinner with a clock beside it while "189 lines · 0.4s" is
// two numbers in a list. A dot after the mark would read as a third number.
func (a *app) joinTail(mark tailSeg, figures []tailSeg) (string, int) {
	text, cells, drawn := mark.text, mark.width, 0
	for _, seg := range figures {
		if seg.width == 0 {
			continue
		}
		switch {
		case cells == 0:
			text, cells = seg.text, seg.width
		case drawn == 0:
			text, cells = text+" "+seg.text, cells+1+seg.width
		default:
			text, cells = text+a.pal.dim(" · ")+seg.text, cells+3+seg.width
		}
		drawn++
	}
	return text, cells
}

// joinFact appends one fact to another with the surface's own separator, in
// both forms at once — a plain string to measure and a painted one to draw. It
// is not homestanding.go's [joinDot], which joins one string to another; this
// one keeps the measured copy and the drawn copy in step, which is the whole
// reason the right column can add up.
func joinFact(plain, painted, addPlain, addPainted string) (string, string) {
	if plain == "" {
		return addPlain, addPainted
	}
	return plain + " · " + addPlain, painted + " · " + addPainted
}

// toolClock is the row's ONE figure of time, plain and painted: a finished
// call's own duration, or a running one's age with whatever it says about the
// bound it runs under.
//
// The two are mutually exclusive by construction — [elapsedWord] answers only
// for a call that has reported finishing and [app.countClock] only for one that
// is running — and only one of them is ever on a row, because a finished call's
// duration and a running call's age are the same figure said a different way.
func (a *app) toolClock(e *entry) (plain, painted string) {
	if word := elapsedWord(e); word != "" {
		return word, a.pal.dim(word)
	}
	return a.countClock(e)
}

// ── THE TARGET, CUT WHERE IT CAN AFFORD TO BE ───────────────────────────────

// elideFloor is the narrowest a middle cut is worth making. Under it the two
// fragments left either side of the ellipsis are shorter than the ellipsis is
// worth, and one readable end says more than two unreadable ones.
const elideFloor = 12

// toolFit cuts a target to the cells the row can give it — and it cuts a PATH
// or a URL in the MIDDLE.
//
// A URL's two ends are the two a person reads: the host says whose page this is,
// the tail says which page, and the query string between them is what a reader
// skips. An end cut keeps the half nobody wanted — three fetches of one news
// site cut at the end are three identical rows — so the target keeps both ends
// and spends one cell on the ellipsis between them.
//
// A COMMAND IS CUT AT THE END, and so is a pattern, because both are read left
// to right and their first words are what they do. A middle cut on `go test
// ./internal/…/tui3` would hide the verb and keep the argument.
func toolFit(tool, target string, width int) (string, int) {
	switch tool {
	case "read", "edit", "write", "ls", "web_fetch":
		// The qualifier keeps its place: it is the line range the path was read
		// with, and a cut that took it would leave a path claiming it was read
		// whole (the parameter hierarchy, below, is what paints the two).
		head, rest, found := strings.Cut(target, " ")
		if !found {
			return elideMiddle(target, width)
		}
		if room := width - 1 - ansi.StringWidth(rest); room >= elideFloor {
			cut, cells := elideMiddle(head, room)
			return cut + " " + rest, cells + 1 + ansi.StringWidth(rest)
		}
		return fitWidth(target, width)
	}
	return fitWidth(target, width)
}

// elideMiddle keeps both ends of a string, with one cell of ellipsis between
// them, and answers with what it drew and what that measured.
//
// THE HEAD GROWS TO THE AUTHORITY WHERE THERE IS ONE. An even split through
// "https://www.reuters.com/world/…" can land inside the host, which is the one
// part of a URL a person identifies it by, so the scheme and the host are kept
// whole whenever they fit with something left over for the tail.
func elideMiddle(s string, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	measured := ansi.StringWidth(s)
	if measured <= width {
		return s, measured
	}
	if width < elideFloor {
		return fitWidth(s, width)
	}
	keep := width - 1
	head := (keep + 1) / 2
	if at := urlAuthority(s); at > head && at <= keep-elideFloor/3 {
		head = at
	}
	cut := ansi.Truncate(s, head, "") + glyphMore + ansi.Cut(s, measured-(keep-head), measured)
	return cut, ansi.StringWidth(cut)
}

// urlAuthority is how many cells of a URL are its scheme and its host — the
// part before the path begins — or zero for anything that is not one. A path
// has no authority and gets the even split, which is right: every segment of a
// path is the same kind of thing.
func urlAuthority(s string) int {
	at := strings.Index(s, "://")
	if at < 0 {
		return 0
	}
	rest := s[at+3:]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		return ansi.StringWidth(s[:at+3+slash])
	}
	return ansi.StringWidth(s)
}

// ── THE PHONE ROW (tierPhone) ───────────────────────────────────────────────
//
//	├─▶ ◌ edit  loop.go                        the change is queued
//	├─▶ ⠋ bash  go test ./…              12s    …and this one is turning
//	├─▶   edit  loop.go       +12 −4     1.2s   done, and quiet about it
//	╰─▶ ✗ bash  go build ./…  exit 1     1.2s   failed, and loud about it
//
// One line, never two, at forty-four columns. Three things had to move for that
// and each of them is a decision:
//
//   - THE STATE GLYPH LEADS. On a wide frame the mark sits at the right end,
//     where there is always room for it; on a phone the right end is exactly
//     where the row runs out, and a state that competed with the target for the
//     last cells would be a state that disappears on the rows that have the
//     most to say. A fixed cell at the left is a COLUMN — the one thing a
//     narrow screen reads well — and it costs the target two cells, flat.
//     The machine behind it is unchanged: [app.mark], the same ◌ → ? → spinner
//     → (nothing) / ✗ every other tier draws, and a success is still silent.
//   - THE TARGET SHEDS ITS QUALIFIER AND KEEPS ITS TAIL ([phoneTarget]). The
//     parameter hierarchy already said which half of a target is substance and
//     which is context; at this width context is not dimmed, it is dropped, and
//     a path collapses to the basename that distinguishes it.
//   - THE CLOCK IS ONE FIGURE ([app.phoneClock]). A finished call's duration or
//     a running one's age, never a bound stated beside an age — that is
//     arithmetic, and arithmetic is the first thing forty-four columns give up.
//   - AND IT SHEDS WHAT IT CANNOT HOLD IN THE WIDE ROW'S ORDER. The size goes
//     first and the duration after it, and the state is never given up at all —
//     the same order [app.toolTail] applies to the wide row's right column, off
//     the same floor ([toolTargetFloor]). This tier reaches the moment sooner
//     and never differently, which is what stops a person who has read one
//     shape from having to learn a second.
//
// The rail stays. It costs four cells and it is what says these rows are one
// object rather than four unrelated lines in a column of prose, which is worth
// more on a narrow screen than on a wide one, not less.

// phoneGutterWidth is the state column: the mark, and the space after it.
const phoneGutterWidth = 2

func (a *app) toolLinePhone(e *entry, i int, last bool, width int) string {
	name, fallback := toolWords(e.tool, e.text)
	target := toolTarget(e.tool, e.detail.Args, e.text)
	if target == "" {
		target = fallback
	}
	target = phoneTarget(e.tool, target)
	statPlain, statPainted := a.toolStat(e)
	// What the person answered when this call was asked about (consent.go). It
	// REPLACES the stat here rather than trailing it behind a dot: both are
	// facts about the call rather than in it, and this row has one slot.
	if e.decision != "" {
		statPlain, statPainted = e.decision, a.pal.dim(e.decision)
	}
	clockPlain, clockPainted := a.phoneClock(e)

	rail := a.pal.rail(last)
	railCells := ansi.StringWidth(rail)
	gutter, gutterWidth := a.phoneGutter(e)
	nameWidth := ansi.StringWidth(name)

	reserve := 0
	if clockPlain != "" {
		reserve = ansi.StringWidth(clockPlain) + 1
	}
	room := width - railCells - gutterWidth - nameWidth - reserve
	// THE WIDE ROW'S DROP ORDER, at this tier's own moment: the size goes before
	// the duration and the state goes last of all — here it is never given up at
	// all, because it is a gutter at the left rather than a segment of a column
	// (see [app.toolTail], which sheds the same two figures in the same order).
	if statWidth := ansi.StringWidth(statPlain) + 2; statPlain == "" || room-statWidth < toolTargetFloor+1 {
		statPlain, statPainted = "", ""
	} else {
		room -= statWidth
	}
	target = fit(target, room-1)

	// Selection and hover are a brightness on the rail, exactly as they are on
	// the wide row: a phone has no columns to spend on a marker either.
	painted := a.pal.dim(rail)
	switch {
	case e.status == toolConsent:
		painted = a.pal.askBold(rail)
	case a.selected(i), a.hoveringEntry(i):
		painted = a.pal.accent(rail)
	}
	line := painted + gutter + a.paintName(e, name)
	used := railCells + gutterWidth + nameWidth
	if target != "" {
		line += " " + a.paintTarget(e, target)
		used += 1 + ansi.StringWidth(target)
	}
	if statPlain != "" {
		line += "  " + statPainted
		used += 2 + ansi.StringWidth(statPlain)
	}
	if clockPlain == "" {
		return line
	}
	// The clock rides the right end. A row with nothing left to give it still
	// gets one space rather than none, because a duration run straight into a
	// path reads as part of the path.
	pad := width - used - ansi.StringWidth(clockPlain)
	if pad < 1 {
		pad = 1
	}
	return line + strings.Repeat(" ", pad) + clockPainted
}

// phoneGutter is the state cell and the space after it, painted, with the width
// it actually took — measured rather than assumed, because [app.mark] is the
// one thing on this row whose glyph the palette chooses.
func (a *app) phoneGutter(e *entry) (string, int) {
	mark := a.mark(e)
	width := ansi.StringWidth(mark)
	if width >= phoneGutterWidth {
		return mark, width
	}
	return mark + strings.Repeat(" ", phoneGutterWidth-width), phoneGutterWidth
}

// phoneTarget is the target a forty-four column row can carry: its SUBSTANCE
// alone, and — where the substance is a path — the tail of it.
//
// It is the parameter hierarchy taken one step further. On a wide row the
// qualifier recedes to dim and stays on screen; here it is dropped, because a
// dim `cd internal/session && ` at this width is fourteen cells of context in
// front of a command with six left for it.
func phoneTarget(tool, target string) string {
	switch tool {
	case "bash":
		// The budgeted command fragment: earlier clauses are context, and the row
		// keeps the final action. [fit] does the rest at the call site.
		if _, command, found := cutCommandContext(target); found {
			return command
		}
		return target

	case "grep", "find":
		// The pattern is what the call is looking for; where it looked is the
		// qualifier, and a qualifier is the first thing this tier gives up.
		pattern, _, _ := strings.Cut(target, " ")
		return pattern

	case "read", "edit", "write", "ls":
		// The path elided to its tail, and the line range after it dropped: at
		// this width "120-240" is four files' worth of the name it qualifies.
		path, _, _ := strings.Cut(target, " ")
		return pathTail(path)
	}
	return target
}

// pathTail is a path's last segment — the part of it that is not shared with
// every other path under the same roots. A path that is nothing but slashes is
// handed back whole rather than emptied.
//
// It is NOT welcome.go's [baseName], which answers the same question about a
// session file and answers it differently on purpose: an empty path there is a
// session with no name and reads "session", and an empty target here is a call
// with nothing to point at, which must draw nothing rather than a word.
func pathTail(path string) string {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return path
	}
	if at := strings.LastIndex(trimmed, "/"); at >= 0 {
		return trimmed[at+1:]
	}
	return trimmed
}

// phoneClock is the row's ONE figure of time, plain and painted: a finished
// call's own duration, or a running one's age.
//
// It is the narrow answer to [app.countClock], and it differs from it in one
// place — inside a bound's last window the remainder REPLACES the age instead
// of trailing it. "1m 52s · 8s left" is thirteen cells of which four matter,
// and the four that matter are the only ones on this row a person can act on.
// The words and the thresholds are the wide tier's own ([leftWord],
// [countUpWord], timeoutNear, timeoutEdge), so the two can never disagree about
// what eight seconds looks like.
func (a *app) phoneClock(e *entry) (plain, painted string) {
	if word := elapsedWord(e); word != "" {
		return word, a.pal.dim(word)
	}
	// A call that has reported its own finish stops counting HERE, even when
	// what it took is under the floor worth printing: the alternative is a five
	// millisecond call spinning up a climbing clock until its slowest sibling
	// returns, which is the row reading the batch's clock and calling it its own.
	if e.ran > 0 {
		return "", ""
	}
	if e.status != toolRunning || e.began.IsZero() || !e.ended.IsZero() ||
		a.state != stateWorking {
		return "", ""
	}
	if limit := toolLimit(e); limit > 0 {
		if left := limit - a.now().Sub(e.began); left <= timeoutNear {
			word := leftWord(left)
			if left <= timeoutEdge {
				return word, a.pal.bad(word)
			}
			return word, a.pal.warn(word)
		}
	}
	age := countUpWord(a.now().Sub(e.began))
	if age == "" {
		return "", ""
	}
	return age, a.pal.dim(age)
}

// ── THE FORMING ROW (toolForming) ───────────────────────────────────────────
//
//	├─▶ receiving · 1.2 KB                     ◌   nothing named yet
//	├─▶ write · 4.2 KB                         ◌   …the name landed, it is still
//	├─▶ write internal/tui3/app.go · receiving ◌   …and now the call has a face
//
// The row exists from the FIRST FRAGMENT of a call, which is the whole of this
// wave: a `write` whose body is the file takes seconds to arrive, and a surface
// that waited for the announcement drew silence for every one of them.
//
// What it says is what is honestly known, and it gains detail rather than
// changing its mind. Until the wire has named the call there is one fact — how
// much has arrived — and the row is that fact. The moment session can gloss it
// from a field that has CLOSED, the gloss takes the line and "receiving" moves
// to the trailing slot where every other fact ABOUT a call sits.
//
// IT IS DIM, WHOLE, AND IT PULSES. Dim because nothing here is a claim about
// work: the model is writing an instruction, and the surface has not been asked
// to do anything yet. Whole — target included — because the announced row's
// primary ink is what the transition is FOR: the line brightens when the call
// becomes real, which is a state change a person reads without being told. And
// the pulse is the ellipsis's own tick ([pulseStep]), not the spinner's: a
// spinner is a claim that something is turning, and nothing is.
func (a *app) formingLine(e *entry, i int, last bool, width int) string {
	rail := a.pal.rail(last)
	// Selection and hover are a brightness on the rail, exactly as they are on
	// every other tool row (hover.go). A forming row is never the question hue:
	// it cannot be waiting on a person, because nobody has been asked anything.
	painted := a.pal.dim(rail)
	if a.selected(i) || a.hoveringEntry(i) {
		painted = a.pal.accent(rail)
	}
	line, used := painted, ansi.StringWidth(rail)
	phone := layoutTier(width) == tierPhone
	// The indent law's two cells, on [app.toolLine]'s reason and after its tier
	// question for the same one.
	width -= workIndentCols(width)
	// THE MARK KEEPS ITS TIER'S COLUMN — the phone's gutter, everybody else's
	// right end (port/p2's law) — so the announcement that lands on this row
	// changes the ink and the words, and moves nothing.
	if phone {
		gutter, gutterWidth := a.phoneGutter(e)
		line += gutter
		used += gutterWidth
	}
	room := width - used
	if !phone {
		room -= 2 // the mark, and the space in front of it
	}
	if room < 1 {
		return line
	}
	word := fit(formingWord(e, phone), room)
	line += a.pal.dim(word)
	if phone {
		return line
	}
	used += ansi.StringWidth(word)
	if pad := width - used - 1; pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line + a.mark(e)
}

// formingWord is the forming row's whole sentence.
//
// The gloss is session's ([formingHint] in its toolhint.go), built from the
// argument fields that have CLOSED — so "write internal/foo.go" appears while
// the body of the file is still arriving, and half a path never appears at all.
// This side adds nothing to it but the state it is in.
func formingWord(e *entry, phone bool) string {
	name, rest := toolWords(e.tool, e.text)
	if phone {
		rest = phoneTarget(e.tool, rest)
	}
	head := strings.TrimSpace(name + " " + rest)
	if !e.ended.IsZero() {
		// The turn ended mid-call: the row keeps whatever the model had said of
		// it and stops claiming anything is still coming (app.go's
		// [app.dropForming]). The size goes with the claim — how much of an
		// instruction that was abandoned had arrived is a number about nothing.
		if head == "" {
			return cancelledWord
		}
		return head + " · " + cancelledWord
	}
	// WHAT THE CALL IS ABOUT ENDS THE COUNTER. Until a hint-bearing field
	// closes, the size is the only thing on the row that changes — it is the
	// difference between a stalled stream and a file arriving — and the moment
	// there is a target to name, the target is what a person is waiting to read
	// and the state trails it instead.
	if rest != "" {
		return head + " · " + receivingWord
	}
	if size := byteWord(e.bytes); size != "" {
		return firstNonEmpty(head, receivingWord) + " · " + size
	}
	return firstNonEmpty(head, receivingWord)
}

// The two words a forming row can end on.
const (
	receivingWord = "receiving"
	cancelledWord = "cancelled"
)

// byteWord is how much of a call has arrived, in the coarsest figure that is
// still true: whole bytes under a kilobyte, one decimal above it.
//
// One decimal is the resolution a person can read off a number that changes ten
// times a second — "1.2 KB" climbing to "1.3 KB" is progress, and the three
// digits under it are a flicker nobody can follow. Nothing is drawn for nothing
// arrived: a "0 B" on a row that exists because bytes are arriving is a figure
// that contradicts the row it is on.
func byteWord(n int) string {
	switch {
	case n <= 0:
		return ""
	case n < 1<<10:
		return itoa(n) + " B"
	case n < 1<<20:
		return tenths(n, 1<<10) + " KB"
	}
	return tenths(n, 1<<20) + " MB"
}

// tenths divides to one decimal place, rounded, without a float.
func tenths(n, unit int) string {
	t := (n*10 + unit/2) / unit
	return itoa(t/10) + "." + itoa(t%10)
}

// formingInk is the forming row's DIM PULSE: the two quietest inks on this
// surface, traded on the ellipsis's own grid so a row that is filling in reads
// as alive without spending the spinner on it.
//
// The linear tier gets the still ink, by its own law (styles.go): an animation
// read aloud is a claim repeated forever.
func (a *app) formingInk(s string) string {
	if a.linear || (a.paints/pulseStep)%2 == 0 {
		return a.pal.dim(s)
	}
	return a.pal.muted(s)
}

// mark is what the right of a tool line says about how the call is going —
// which, on success, is nothing at all.
func (a *app) mark(e *entry) string {
	switch e.status {
	case toolFailed:
		return a.pal.bad(a.pal.badGlyph())
	case toolOK:
		return ""
	case toolForming:
		// STILL ARRIVING. The queue's own circle, pulsing: this is the same
		// object one state earlier, and a second glyph for it would make the
		// announcement look like a different call rather than the same one
		// finishing its sentence.
		if !e.ended.IsZero() {
			// The turn ended around it. The mark a call left unresolved takes,
			// for the reason it takes it there: nothing is coming.
			return a.pal.dim(a.linearMark(glyphIdle, glyphIdleASCII))
		}
		return a.formingInk(a.linearMark(glyphQueued, glyphQueuedASCII))
	case toolQueued:
		// ASKED FOR, NOT STARTED. An empty circle, dim: the row exists because
		// the model has finished asking, and a spinner here would be the surface
		// animating work that has not begun.
		return a.pal.dim(a.linearMark(glyphQueued, glyphQueuedASCII))
	case toolConsent:
		return a.pal.askBold(glyphAsk) // "?" is already the ASCII of itself
	default:
		// A RESOLVED ROW IS OVER WHATEVER THE SESSION IS DOING. A room's lane
		// closing settles the calls that were still in the air by stamping the end
		// and nothing else (room.go's [app.roomResolveUnfinished]), and the page it
		// settles them on sits under a conversation that may well still be working
		// — so the state test below cannot be the only one, or a node that landed
		// ten minutes ago spins for as long as the chat above it is busy.
		if !e.ended.IsZero() || a.state != stateWorking {
			// The turn ended with this call unresolved — interrupted, or the
			// stream closed without a close event. A spinner frozen mid-turn
			// would claim the call is still alive.
			return a.pal.dim(a.linearMark(glyphIdle, glyphIdleASCII))
		}
		// THE ONE ANIMATION ON A TOOL LINE, and linear mode's whole objection to
		// it: a spinner is a claim made thirty times a second, and a surface being
		// read aloud hears that claim thirty times a second. A still `*` makes the
		// same claim once.
		if a.linear {
			return a.pal.muted(glyphRunASCII)
		}
		return a.pal.muted(tokens.Spinner(a.paints / spinnerStep))
	}
}

// linearMark picks between a glyph and its ASCII stand-in (styles.go).
func (a *app) linearMark(glyph, ascii string) string {
	if a.linear {
		return ascii
	}
	return glyph
}

// elapsedFloor is how long a call has to have taken to be worth a number.
//
// A read that returned in four milliseconds took no time a person can act on,
// and "0.0s" trailing every row would be a column that has to be read to learn
// nothing — the same law the missing success glyph is drawn from. The threshold
// is the frame interval times three: below it the call was over before the
// surface could have drawn it running.
const elapsedFloor = 100 * time.Millisecond

// elapsedWord is a finished call's own duration, or "" when there is none worth
// saying. It is BEGIN to END: the time the tool ran, never the time its
// announcement spent waiting for a response to finish streaming.
//
// The spelling itself is [tookWord] (timestamps.go), because a turn's footer
// says the same kind of thing about a longer span and the two must not be able
// to disagree about what two minutes looks like.
func elapsedWord(e *entry) string {
	// THE CALL'S OWN FIGURE WINS, and it is said as soon as the call itself
	// reports finishing (session.EventToolFinished) rather than when the row
	// closes: the result waits for the rest of the batch, the duration does not,
	// and begin-to-end on a parallel batch is the SLOWEST call's span written on
	// every row in it.
	if e.ran > 0 {
		return tookWord(e.ran)
	}
	if e.status.live() || e.began.IsZero() || e.ended.IsZero() {
		return ""
	}
	return tookWord(e.ended.Sub(e.began))
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

// ── THE COUNT-UP ────────────────────────────────────────────────────────────
//
//	⠿ running · 12s        …and a second later, 13s
//	⠿ running · 1m 4s
//	⠿ running · 12m 30s
//
// A call that is running is a call somebody is WAITING ON, and the only honest
// thing a surface can offer them is how long they have been waiting. The
// spinner says the work is alive and says nothing else — it looks the same at
// two seconds and at twenty minutes — so the row carries the figure beside it.
//
// It STOPS AT COMPLETION. A finished call has [elapsedWord], which is a
// different figure said a different way (one decimal under ten seconds, because
// a duration you can compare wants precision and a duration you are living
// through wants readability), and only one of the two is ever on a row.
//
// NOTHING NEW TICKS FOR IT. The frame clock already redraws while a turn runs —
// it is what turns the spinner (render.go's [app.paint]) — so the count-up is a
// function of the time at paint and costs the surface no wakeup of its own.

// countUpFloor is how old a call has to be before it says so. Under a second
// there is no waiting to report, and "0s" under every call that has just begun
// is a column that has to be read to learn nothing.
const countUpFloor = time.Second

// ── THE COUNTDOWN ───────────────────────────────────────────────────────────
//
//	⠿ bash  go test ./...        1m 12s / 2m 0s     the bound, stated
//	⠿ bash  go test ./...        1m 52s · 8s left   inside ten seconds, warned
//	⠿ bash  go test ./...        1m 56s · 4s left   inside five, in the failure hue
//
// A bounded call is a call that is going to be KILLED at a time the surface
// already knows, and the last ten seconds of it are the only ten seconds in
// which a person can do anything about it — interrupt, or wait deliberately
// rather than hopefully. Up to there the bound is a fact and reads like one, in
// the same dim as the age beside it. Inside them the row stops stating the
// bound and starts counting what is left of it, because "8s left" is the
// sentence and "1m 52s / 2m 0s" is arithmetic the person has to do themselves.
//
// The colour is the escalation and it is two steps, not a gradient: warn while
// the call can still land, [hueBad] under five seconds because by then it very
// likely will not. Only the REMAINDER takes the hue — the age stays dim — so
// the row grows exactly one loud token and nothing else moves.
//
// A call with no timeout gets NONE of this: no remainder, no bound, no colour.
// Nothing is going to happen to it at any particular moment, and chrome that
// implied otherwise would be the surface inventing a deadline.
const (
	// timeoutNear is when a bound stops being background and starts being the
	// thing about the row. Ten seconds is about as long as a person will hold
	// still for something they were told is nearly over.
	timeoutNear = 10 * time.Second
	// timeoutEdge is when it stops being a warning and becomes the outcome.
	timeoutEdge = 5 * time.Second
)

// countUp is a RUNNING call's clock as PLAIN text — its age, and what the row
// says about the bound it runs under. It is "" for a call in any other state:
// the clock belongs to running rows and to nothing else.
//
// A turn that ENDED with this call unresolved stops it too, and for the reason
// the spinner stops there ([app.mark]): the surface no longer knows the call is
// alive, and a number that kept climbing would be claiming it is. The row keeps
// the dim dot it already had.
//
// It is the width half of [app.countClock] — a width measured through escape
// sequences is a width measured wrong (see [app.toolLine]).
func (a *app) countUp(e *entry) string {
	plain, _ := a.countClock(e)
	return plain
}

// countClock is the clock in both forms: the plain text a row measures itself
// by, and the painted text it draws. They are produced together because the
// second is the first with at most one token tinted, and two functions deriving
// that split separately is two chances for the width and the paint to disagree.
func (a *app) countClock(e *entry) (plain, painted string) {
	// A CALL THAT IS OVER DOES NOT COUNT. Its own finish is reported the instant
	// it happens (session.EventToolFinished) while its result waits for the rest
	// of the batch, and a clock still climbing in that gap is timing the batch's
	// slowest call on this call's row.
	if e.ran > 0 {
		return "", ""
	}
	// The resolved row is stopped here too, on [app.mark]'s reason and in the
	// same words: an end stamped on a live row is the lane saying nothing more is
	// coming, and a number climbing under it would be the row insisting otherwise.
	if e.status != toolRunning || e.began.IsZero() || !e.ended.IsZero() ||
		a.state != stateWorking {
		return "", ""
	}
	age := countUpWord(a.now().Sub(e.began))
	limit := toolLimit(e)
	if limit <= 0 {
		// Unbounded: the age alone, or nothing at all in the first second.
		return age, a.pal.dim(age)
	}
	left := limit - a.now().Sub(e.began)
	if left > timeoutNear {
		// The bound stated beside the age — arithmetic the person is not being
		// asked to do yet, because there is nothing to do about it yet.
		if age == "" {
			return "", ""
		}
		word := age + " / " + countUpWord(limit)
		return word, a.pal.dim(word)
	}
	// Inside the window the remainder is said outright, and it is said even in
	// the first second of a call whose bound is that short: a five-second
	// timeout is exactly the case a person most needs the number for.
	remainder := leftWord(left)
	tint := a.pal.warn
	if left <= timeoutEdge {
		tint = a.pal.bad
	}
	if age == "" {
		return remainder, tint(remainder)
	}
	return age + " · " + remainder, a.pal.dim(age+" · ") + tint(remainder)
}

// leftWord is what is left of a bound, in whole seconds, rounded UP so that the
// last second of a call still says "1s left" rather than counting to zero while
// the command is still running. A bound already passed — the harness's own kill
// is a moment behind the clock, and a machine under load can be further — says
// "0s left" rather than a negative number.
func leftWord(left time.Duration) string {
	seconds := int(math.Ceil(left.Seconds()))
	if seconds < 0 {
		seconds = 0
	}
	return itoa(seconds) + "s left"
}

// toolLimit is the timeout the call runs under, or zero when nothing is going
// to interrupt it.
//
// Only bash is bounded on the wire, and only a FOREGROUND bash: the session's
// wrapper starts a background call as a job and returns, and a job runs until
// it is done (internal/session's backgroundBash). The number is the model's own
// when it set one, and the session's ceiling when it did not or when it asked
// for more — the same law internal/session's wrapper applies to the wire args,
// restated here from the call's original args so the row agrees with the clock
// the command is actually bounded by.
func toolLimit(e *entry) time.Duration {
	if e.tool != "bash" {
		return 0
	}
	raw := strings.TrimSpace(e.detail.Args)
	if raw == "" {
		return session.BashCeilingSeconds * time.Second
	}
	var args struct {
		Background bool `json:"background"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err == nil && args.Background {
		return 0
	}
	// THE READ IS THE SESSION'S OWN, never a second copy of the same rules. An
	// explicit null from a weak model — `"timeout": null` — is UNSET on both
	// sides, and so is a zero or a negative: the wrapper writes the default over
	// it, and the row must count down against the same figure. A surface that
	// read the argument for itself drew a bound the engine had not armed, which
	// is the one number on this row a person cannot check.
	return time.Duration(session.BashTimeoutSeconds(json.RawMessage(raw)) * float64(time.Second))
}

// countUpWord spells a duration the way a person says one out loud: seconds
// under a minute, minutes and seconds under an hour, hours and minutes above
// it. The parts are SPACED ("1m 5s", not "1m05s") because this figure is read
// while it moves — it is the one number on the surface that changes under the
// eye — and a padded run of digits reads as one number rather than as two.
func countUpWord(d time.Duration) string {
	if d < countUpFloor {
		return ""
	}
	switch {
	case d < time.Minute:
		return itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m " + itoa(int(d%time.Minute/time.Second)) + "s"
	default:
		return itoa(int(d/time.Hour)) + "h " + itoa(int(d%time.Hour/time.Minute)) + "m"
	}
}

// ── THE PARAMETER HIERARCHY ─────────────────────────────────────────────────
//
// The target used to be one colour, and a target is not one thing:
//
//	bash   cd internal/session && go test ./...   the cd is context, the command
//	                                              is the substance
//	read   internal/session/loop.go 120-240       the path is the substance, the
//	                                              range is a qualifier
//	grep   argsLimit internal/session             the pattern is what is being
//	                                              looked for, the path is where
//
// So each of them is painted in two tiers instead of one: what the call is
// ABOUT stays primary ink, and what merely qualifies it recedes to dim. The
// parse is deliberately conservative and shape-based — only the `cd X && `
// prefix, only a trailing range or flag after a space — because a target this
// file guessed wrong about is a line that says the wrong thing is important.
//
// The two tiers are the WHOLE hierarchy: it is a ladder of reading tiers and
// never a second hue. A search's pattern used to lead in the accent, on the
// argument that a pattern is a question rather than a place — but THE ACCENT
// BUDGET (docs/DESIGN-LANGUAGE.md) is one lit element per screen, marking the
// one live or chosen thing, and a finished tool row scrolling up the
// transcript is dim telemetry rather than that thing. A turn with four greps
// in it spent the budget four times and bought nothing with any of them.

// paintName paints the tool's own name: chrome, so muted — unless the call is
// waiting on a person, in which case the whole row is the question.
func (a *app) paintName(e *entry, name string) string {
	if e.status == toolConsent {
		return a.pal.askBold(name)
	}
	return a.pal.muted(name)
}

// paintTarget paints the already-fitted target in its two tiers.
//
// It takes the FITTED text rather than the whole one so the split and the
// truncation cannot disagree: a target cut at the width is still split by the
// same rule, and a "cd …" prefix that was itself truncated simply stops being
// recognized, which is the safe way round.
func (a *app) paintTarget(e *entry, target string) string {
	if e.status == toolConsent {
		return a.pal.askBold(target)
	}
	switch e.tool {
	case "bash":
		// THE GLOSS IS HIGHLIGHTED TOO (shellx.go). One line, clipped exactly as
		// it was before — the highlighting is applied to the FITTED text, after
		// every width in this function has been measured, because a width
		// measured through an escape sequence is a width measured wrong.
		//
		// A successful chain keeps its context rule ahead of the lexer. Context
		// recedes; the final action is what the eye should land on.
		if context, command, found := cutCommandContext(target); found {
			return a.pal.dim(context) + a.pal.shell(command)
		}
		return a.pal.shell(target)

	case "read", "edit", "write":
		// AND THE TARGET IS THE DOOR ITSELF (pathlink.go). This is the one path
		// on the row that aforge resolved rather than found — it came out of the
		// call's own arguments — so it is exactly the kind of path that may be
		// linked, and the shown text may be an ellipsis or a bare basename
		// without the click losing the file.
		name := argString(argsOf(e.detail.Args), "path")
		path, rest, found := strings.Cut(target, " ")
		if !found {
			return a.pathLink(name, a.pal.ink(target))
		}
		return a.pathLink(name, a.pal.ink(path)) + a.pal.dim(" "+rest)
	}
	// A SEARCH'S PATTERN IS A TARGET LIKE EVERY OTHER TARGET, so it wears the
	// ink the role table gives them all; only where it looked recedes. It is
	// deliberately not the payload rule's `data` either: that hue lifts one
	// datum out of a quiet line the surface says on its OWN account, and a tool
	// row is not that shape — its target is already the loudest thing on it,
	// with the tool's name muted in front and the stat dim behind. Giving a
	// pattern a hue no other tool target wears would be a second way of saying
	// the same thing, which is what the palette is small to prevent.
	//
	// The rule is asked of [targetIsPattern] rather than of a list of tool
	// names, so every search-shaped hand splits the same way and no sibling can
	// arrive painted differently from grep and find.
	if targetIsPattern(e.tool) {
		pattern, where, found := strings.Cut(target, " ")
		if !found {
			return a.pal.ink(target)
		}
		return a.pal.ink(pattern) + a.pal.dim(" "+where)
	}
	return a.pal.ink(target)
}

// cutCommandContext splits a successful chain before its last command. Earlier
// clauses establish the context for the final action, so stacked rows lead with
// the changing action and let repeated setup recede.
func cutCommandContext(command string) (context, rest string, found bool) {
	at := strings.LastIndex(command, " && ")
	if at < 0 {
		return "", command, false
	}
	cut := at + len(" && ")
	return command[:cut], command[cut:], true
}

// ── THE LIVE PREVIEW ────────────────────────────────────────────────────────

// previewWindow caps a preview. It is shorter than the expansion's own cap
// (diffWindow) on purpose: this one is drawn without being asked for, under a
// row nobody clicked, and a thirty-line diff that opened itself in the middle
// of a conversation is a surface taking the screen. A person who wants the rest
// clicks the foot, exactly as they would on an expansion.
const previewWindow = 12

// previewPhoneWindow is the same cap at [tierPhone], and it is a THIRD of it.
//
// The reasoning above scales with the frame. Twelve rows under a row nobody
// clicked is a third of a laptop's body and leaves the conversation it
// interrupted on screen; the same twelve on a phone — where the frame is under
// sixty columns and the body is a dozen-odd rows deep — is the WHOLE view, so a
// person who asked a question and watched an edit start would have their own
// sentence scrolled off by a diff they did not open. Four rows is a hunk's worth
// of evidence: enough to see WHICH change is about to land, bounded so the thing
// it is happening inside of stays visible.
//
// The rest is one tap away on the foot, exactly as it is at every other tier —
// this caps what is shown UNASKED, and it is not consulted at all by the
// full-frame sheet ([app.detailBody] keeps [previewWindow]), because a person
// who opened a call at tierPhone has given the whole frame to the answer.
const previewPhoneWindow = 4

// previewCap is how many rows an unasked-for preview keeps. It takes the tier's
// answer rather than a width so the ONE place that decides which frame this is
// stays [app.toolRows] — the caller that also knows whether the person asked.
func previewCap(phone bool) int {
	if phone {
		return previewPhoneWindow
	}
	return previewWindow
}

// previewBody is what a call that has NOT finished shows under its row: the
// header, the rows, and how many were dropped. cap is the ceiling the caller
// wants — the tier's for a block nobody asked for, [previewWindow] for a call
// somebody opened.
//
// It answers for the two mutating tools and no others, because they are the two
// whose arguments contain the whole change — an edit's replacements, a write's
// content. A bash command is already on its own line in full; a read has
// nothing to preview but the path it is already showing.
//
// The header is the state in one word — `pending` while nothing has started,
// `applying` once execution has — and it is the state that changes under it
// rather than the rows: the diff a person read at `pending` is the same diff
// that lands, and a preview that redrew itself on begin would ask them to read
// it twice. It goes dim in the question hue while a call is waiting on an
// answer, for the same reason the row above it does.
func (a *app) previewBody(e *entry, width, window int) (head string, body []string, more int) {
	if !e.status.live() || width < 8 {
		return "", nil, 0
	}
	switch e.tool {
	case "edit":
		body = a.diffRows(e, width)
	case "write":
		fields := argsOf(e.detail.Args)
		body = a.codeRows(argString(fields, "content"), argString(fields, "path"), width)
	default:
		return "", nil, 0
	}
	if len(body) == 0 {
		return "", nil, 0
	}
	if window <= 0 {
		window = previewWindow
	}
	if !e.full && len(body) > window {
		more, body = len(body)-window, body[:window]
	}
	return a.previewHead(e), body, more
}

// formingRows is the block under a call that is STILL ARRIVING: the last window
// lines of the file it is writing, dim and syntax-coloured, growing downward as
// the model types.
//
// IT IS A TAIL, NOT A TRUNCATION, and that is why it has no header and no
// "… N more lines" foot. Both of those belong to a block that is showing part of
// something whole; this is showing the end of something that is not finished, so
// there is no remainder to offer and nothing a click could lift. The honest
// figure for how much has arrived is already on the row above it, where the
// forming line says `receiving · 12.4 KB`.
//
// It answers for `write` and no other tool ([formingPreviewField] says why).
func (a *app) formingRows(e *entry, width, window int) []string {
	if e.formed == "" || width < 8 || window <= 0 {
		return nil
	}
	// The path comes from session's gloss, which is the only thing about this
	// call that is known before it is whole — the hint closes on the path field
	// well before the body stops arriving (session's toolhint.go).
	rows := a.codeRows(e.formed, toolTarget(e.tool, "", e.text), width)
	if len(rows) > window {
		rows = rows[len(rows)-window:]
	}
	return rows
}

// previewHead is the one word above a preview.
func (a *app) previewHead(e *entry) string {
	switch e.status {
	case toolConsent:
		return a.pal.ask(previewPending)
	case toolRunning:
		return a.pal.dim(previewApplying)
	default:
		// Queued: nothing has started, and the header says exactly that in the
		// hue this surface uses for things that are about to need a person.
		return a.pal.ask(previewPending)
	}
}

// The two words a preview's header can be.
const (
	previewPending  = "pending"
	previewApplying = "applying"
)

// detailBody is what one open call shows, per tool (D11's table), already
// painted and WITHOUT the rail — [app.toolRows] hangs the stem on. It returns
// the rows it kept and how many it dropped; the caller draws the drop as a
// clickable "… N more lines", and a call whose cap has been lifted (e.full)
// drops nothing.
//
// Rows TRUNCATE rather than wrap, for prose's reason about tables: a row that
// stays a row can be counted, and "first 30 lines" has to mean thirty lines on
// screen or it means nothing.
func (a *app) detailBody(e *entry, width int) ([]string, int) {
	if width < 8 {
		width = 8
	}
	if e.status.live() {
		// An unfinished call shows what it CAN: the change it is about to make,
		// where the arguments carry one, and otherwise the one animated line
		// that says the obvious in the same breath the spinner is drawing.
		// THE SHEET IS NOT CAPPED BY THE TIER. This branch answers a call somebody
		// OPENED, and at tierPhone the answer is the whole frame (expand.go), so
		// the phone's tighter ceiling — which exists to stop an unasked-for block
		// from taking that frame — has nothing to protect here.
		if head, body, more := a.previewBody(e, width, previewWindow); head != "" {
			return append([]string{head}, body...), more
		}
		// A COMMAND IS READABLE BEFORE IT FINISHES, and a running one is when a
		// person most wants to read it — that is what they opened the row for. It
		// is drawn here rather than in [app.previewBody] on purpose: this branch
		// answers a row somebody CLICKED, and the preview answers a row nobody
		// did, where a command that unfolded itself under every bash call would
		// be the surface taking the screen.
		if command := a.commandRows(e, width); len(command) > 0 {
			return append(command, a.livePhrase(e)), 0
		}
		return []string{a.livePhrase(e)}, 0
	}

	switch e.tool {
	case "edit":
		return a.cap(e, a.diffRows(e, width), diffWindow)
	case "write":
		// THE CONTENT IS SOURCE AND IS DRAWN AS SOURCE (codeview.go), lexed by the
		// path the same call is writing to. The display copy's own cut, where
		// there was one, is left on the end of the last line rather than trimmed
		// off: it is the only thing on screen that says this is not the whole file.
		fields := argsOf(e.detail.Args)
		content := argString(fields, "content")
		return a.cap(e, a.codeRows(content, argString(fields, "path"), width), writeWindow)
	case "read":
		// A READ'S RESULT IS SOMEBODY ELSE'S SOURCE, and the argument for colouring
		// it is the write's argument arrived at from the other end: a person opens
		// this row to read a file, and a file is the one kind of tool output whose
		// structure a lexer knows.
		return a.cap(e, a.codeRows(
			resultText(e.detail.Output), argString(argsOf(e.detail.Args), "path"), width), readWindow)
	case "bash":
		// THE COMMAND IS NOT CAPPED, AND THE OUTPUT IS.
		//
		// A person clicks a bash row to read the command — that is the one thing
		// on the line that was clipped — so the command is shown whole, every
		// line of it, above the cap rather than inside it. Capping it would mean
		// a forty-line script whose tail was hidden behind a "… N more lines"
		// foot that a click would then answer with forty lines of OUTPUT.
		//
		// The window still governs the output, which is the part that can be a
		// megabyte, and the "… N more" foot still lifts it.
		head, said := a.commandRows(e, width), a.bashRows(e, width)
		if len(head) == 0 {
			return a.cap(e, said, bashWindow)
		}
		if len(said) == 0 {
			// A command that printed nothing has already said everything it has to
			// say; the em dash [app.cap] draws for an empty expansion would be the
			// surface answering a command with a shrug.
			return head, 0
		}
		body, more := a.cap(e, said, bashWindow)
		return append(head, body...), more
	case "generate_image":
		// THE PICTURE IS THE WHOLE ANSWER, and the line under it already says
		// everything this call's result says — where the file is and how big it
		// is. Printing the result underneath as well would be the expansion
		// answering one question twice (imagepreview.go).
		if picture, drawn := a.pictureRows(e, width); drawn {
			return picture, 0
		}
		// AND WHERE NO PICTURE CAN BE DRAWN, THE PATH STILL GOES DOWN WHOLE.
		// This is the terminal that reached sixteen colours, or the file this
		// program cannot decode, and it is exactly the case where a person needs
		// to leave and open the file themselves — so the one thing the fallback
		// must not do is truncate the only string that would let them.
		if rows, more, known := a.pictureWords(e, width); known {
			return rows, more
		}
	case "view_image":
		// A LOOK HAS TWO HALVES and they are both worth the rows: the picture,
		// so a person can see what was looked at, and under it what the looking
		// model said about it — which is the only thing this call actually
		// returned, and the reason it was made.
		if picture, drawn := a.pictureRows(e, width); drawn {
			said := a.plainRows(resultText(e.detail.Output), width)
			if len(said) == 0 {
				return picture, 0
			}
			body, more := a.cap(e, said, listWindow)
			return append(picture, body...), more
		}
		// The undrawable half is replaced by the path and the answer is kept:
		// what the looking model said is this call's actual result, and a person
		// who cannot see the picture needs the file's whole name more than
		// anyone (see the twin above).
		if rows, more, known := a.pictureWords(e, width); known {
			return rows, more
		}
	case "grep", "find", "ls":
		return a.cap(e, a.plainRows(resultText(e.detail.Output), width), listWindow)
	}
	return a.cap(e, a.genericRows(e, width), listWindow)
}

// livePhrase is the line an open, unfinished call carries: what it is doing,
// and — once it has been doing it for a second — for how long.
//
// The count-up REPLACES the pulse rather than trailing it. Both are the same
// claim ("this is still alive") and the clock is the better one: it says the
// thing the ellipsis only implies, and two animations on one line is one
// animation too many. A call with no clock — queued, or waiting on a person —
// keeps the pulse, because nothing has started to count.
//
// It returns PAINTED text: the expansion's own dim wraps the words, and the
// clock at the end of them carries whatever hue the countdown earned.
func (a *app) livePhrase(e *entry) string {
	if _, clock := a.countClock(e); clock != "" {
		return a.pal.dim(liveWord(e)+" · ") + clock
	}
	return a.pal.dim(liveWord(e) + a.pulse())
}

// liveWord is what an unfinished call with nothing to preview says it is doing.
func liveWord(e *entry) string {
	switch e.status {
	case toolQueued:
		return "queued"
	case toolConsent:
		return "waiting for you"
	default:
		return "running"
	}
}

// cap bounds one expansion. The window is the tool's own (D11's table), and a
// call the person has clicked "more" on has no window at all — they asked.
func (a *app) cap(e *entry, rows []string, window int) ([]string, int) {
	if len(rows) == 0 {
		return []string{a.pal.dim("—")}, 0
	}
	if e.full || len(rows) <= window {
		return rows, 0
	}
	return rows[:window], len(rows) - window
}

// diffRows is an edit's expansion: the file it touched, then a unified diff of
// every replacement it sent — computed here from the old/new strings, because
// that pair is the only record of the change that exists (the tool's own result
// is one sentence saying it worked).
func (a *app) diffRows(e *entry, width int) []string {
	fields := argsOf(e.detail.Args)
	pairs := editPairs(e.detail.Args)
	if len(pairs) == 0 {
		return a.genericRows(e, width)
	}
	out := make([]string, 0, 16)
	if path := argString(fields, "path"); path != "" {
		// The file the hunks below belong to, and a door into it — the link is
		// applied to the FITTED text, after the width was measured, which is
		// this file's rule for every escape sequence it writes.
		out = append(out, a.pal.dim(a.pathLink(path, fit(path, width))))
	}
	for _, pair := range pairs {
		ops := diffOps(splitLines(pair.old), splitLines(pair.new))
		for _, h := range hunks(ops) {
			out = append(out, a.pal.dim(fit(h.header(), width)))
			for _, op := range h.ops {
				text := fit(string(op.kind)+expandTabs(op.text), width)
				switch op.kind {
				case '+':
					out = append(out, a.pal.add(text))
				case '-':
					out = append(out, a.pal.del(text))
				default:
					out = append(out, a.pal.dim(text))
				}
			}
		}
	}
	return out
}

// commandRows is the command itself, whole and highlighted, at the head of an
// open bash expansion (shellx.go). It reads the ARGUMENTS rather than the line's
// own target, because the target has already been through [fit] and the whole
// promise of this block is that nothing was cut.
//
// It answers nothing for a call whose payload never arrived — a begin with no
// args, which is what a provider that does not stream tool calls sends — and
// then the expansion is the output alone, exactly as it was.
func (a *app) commandRows(e *entry, width int) []string {
	command := argString(argsOf(e.detail.Args), "command")
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return shellRows(a.pal, command, width)
}

// bashRows is a command's output, with its exit line kept at the foot when
// there was one: a build log's last thirty lines are the interesting ones, and
// the code is what the person opened the row to see.
func (a *app) bashRows(e *entry, width int) []string {
	out := a.plainRows(resultText(e.detail.Output), width)
	if code, failed := bashExit(e.detail.Output); failed {
		out = append(out, a.pal.bad(fit("exit "+itoa(code), width)))
	}
	return out
}

// genericRows is the expansion for a tool this surface has no table row for —
// a workforce tool, a tool added tomorrow. It shows what went in and what came
// back, which is the honest floor.
func (a *app) genericRows(e *entry, width int) []string {
	var out []string
	if args := strings.TrimSpace(e.detail.Args); args != "" {
		out = append(out, a.pal.dim(fit(expandTabs(args), width)))
	}
	return append(out, a.plainRows(resultText(e.detail.Output), width)...)
}

// plainRows is a block of evidence: dim, truncated to width, one row per line.
func (a *app) plainRows(text string, width int) []string {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, a.pal.dim(fit(expandTabs(line), width)))
	}
	return out
}

// resultText is a tool result as evidence: session's display cap marker and
// bare's trailing notice block removed, because both are sentences ABOUT the
// result and the expansion is showing the result.
func resultText(output string) string {
	body, _ := outputBody(output)
	return body
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// toolWords splits a call into the two things a line says: the tool's NAME and
// a gloss of what it was pointed at. Session hints usually lead with the tool's
// own name ("read internal/session/session.go"), so the name is stripped from
// the front of the gloss — a line that printed both would say "read read
// internal/session/session.go". The gloss is the FALLBACK target: the payload
// is asked first (see [toolTarget]).
func toolWords(tool, hint string) (string, string) {
	tool, hint = strings.TrimSpace(tool), strings.TrimSpace(firstLine(hint))
	if tool == "" {
		return hint, ""
	}
	if rest, cut := strings.CutPrefix(hint, tool); cut {
		return tool, strings.TrimSpace(rest)
	}
	return tool, hint
}

// ToolGloss is how a tool's activity is said in ONE plain sentence, with the
// verb said once. It is exported because the non-interactive door in
// cmd/aforge prints the same fact without a terminal, and one rule for one
// sentence is the point.
func ToolGloss(tool, hint string) string {
	name, gloss := toolWords(tool, hint)
	if gloss == "" {
		return name
	}
	return name + " " + gloss
}
