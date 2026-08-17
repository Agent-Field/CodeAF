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
//	◌ edit internal/session/loop.go          queued: the model asked, nothing ran
//	│ pending                                 …and here is the change it will make
//	│ @@ -1,4 +1,4 @@
//	│ -const argsLimit = 400
//	│ +const argsLimit = 8192
//	? bash rm -rf build                      waiting on YOU, the whole row violet
//	⠋ bash go test ./…                       running: the spinner, and only here
//	  read internal/session/loop.go   · 189 lines   0.4s   done, quietly
//	✗ bash go build ./…             exit 1        1.2s   failed, already open
//
// The spinner used to cover all four of the first states, and that was the
// defect: a mutating call spun while the RESPONSE was still streaming and
// nothing had started, and a call parked on a consent question spun exactly
// like one doing work. A spinner is a claim that something is turning, so it
// now means that and nothing else — and the two states it used to cover got
// the marks they always deserved: an empty circle for work not begun, and the
// question hue for work waiting on a person.
//
// The elapsed time trails a finished call, dim, at the line's right end — the
// column the spinner just vacated. It is the call's OWN duration (begin to end)
// and not the turn's, and a call too fast to have one does not draw one: see
// [elapsedWord].

// clusterRows lays out one contiguous run of tool entries — d.entries[from:to],
// all from one turn — and appends it to out. This is where the fold lives,
// because folding is a property of the RUN and not of any call in it, and it is
// where the elbow is chosen for the same reason.
//
// The deck is carried in rather than read off the app because a task's page is
// drawn by this function too, from its own list and its own fold state
// (render.go's [deck], room.go): one cluster renderer, two lists.
func (a *app) clusterRows(d deck, out []row, from, to, width int) []row {
	turn := d.entries[from].turn
	start := from
	if to-from > toolWindow && !d.unfolded[turn] {
		start = to - toolWindow
		fold := a.pal.dim(a.pal.toolGlyph() + foldWord(start-from))
		if a.hoveringFold(turn) {
			fold = a.pal.accent(a.pal.toolGlyph()) + a.pal.dim(foldWord(start-from))
		}
		out = append(out, row{text: fold, entry: -1, hit: hitFold, turn: turn})
	}
	for i := start; i < to; i++ {
		out = append(out, a.toolRows(d, i, i == to-1, width)...)
	}
	return out
}

// foldWord is the fold line's sentence. It names the key that opens it, because
// a surface that hides something without saying how to see it has hidden it.
func foldWord(n int) string {
	if n == 1 {
		return "1 earlier tool call · ctrl+o"
	}
	return strconv.Itoa(n) + " earlier tool calls · ctrl+o"
}

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
	// A CALL THAT IS STILL ARRIVING HANGS NOTHING, AND ANSWERS NO POINTER. The
	// preview under a row is drawn from the arguments, and the arguments of a
	// forming call are half a JSON object this surface has deliberately not
	// kept — so there is nothing to show under it and nothing to open, which is
	// [replayInert]'s rule arrived at from the other direction: a row that
	// brightened under the pointer would be promising an answer it does not
	// have. It becomes an ordinary row the moment the announcement brings the
	// payload with it.
	if e.status == toolForming {
		return []row{{text: a.toolLine(e, i, last, width), entry: i, hit: hitNone}}
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
	// everybody wants — and it is already bounded by [previewWindow].
	if !e.open || layoutTier(width) == tierPhone {
		head, body, more := a.previewBody(e, room)
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

// toolLine is the line itself.
//
// The pieces are measured as PLAIN text and painted afterwards, which is the
// only way the widths land: a width measured through escape sequences is a
// width measured wrong. When the row is too narrow for everything, the stat
// goes first and the target is truncated last — the target is the substance,
// and a stat nobody has room for is a number about a line nobody can read.
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
	name, fallback := toolWords(e.tool, e.text)
	target := toolTarget(e.tool, e.detail.Args, e.text)
	if target == "" {
		target = fallback
	}
	statPlain, statPainted := a.toolStat(e)
	// The elapsed time rides the stat slot on its way to the right end. It is
	// the last thing the row gives up when the terminal is narrow, because it is
	// the only thing on the line that is not about WHAT the call did.
	elapsed := elapsedWord(e)
	// THE COUNT-UP, which is the same figure one state earlier: while the call
	// runs, its age sits BESIDE the spinner rather than in place of it. The two
	// say different things and a row needs both — the spinner is the claim that
	// something is turning, the clock is how long it has been turning, and a
	// two-minute `go test` with only a spinner on it is indistinguishable from a
	// two-second one. It is measured plain and drawn painted, because the last
	// seconds of a bounded call take a hue of their own (the countdown, below).
	counting, countingInk := a.countClock(e)
	// What the person answered when this call was asked about (consent.go). It
	// rides the stat slot because it is the same kind of fact — dim, trailing,
	// about the call rather than in it — and because a row that was approved
	// must still read as one row.
	if e.decision != "" {
		if statPlain == "" {
			statPlain, statPainted = e.decision, a.pal.dim(e.decision)
		} else {
			statPlain += " · " + e.decision
			statPainted += a.pal.dim(" · " + e.decision)
		}
	}
	mark := a.mark(e)

	rail := a.pal.rail(last)
	railWidth := ansi.StringWidth(rail)
	nameWidth := ansi.StringWidth(name)

	// The right edge holds the spinner, or the elapsed time that replaces it
	// when the call is done; the ✗ is appended to the text instead, so a failure
	// reads as part of the sentence rather than as a column.
	reserve := 1
	if e.status == toolFailed {
		reserve = 2
	}
	switch {
	case elapsed != "":
		reserve = ansi.StringWidth(elapsed) + 1
	case counting != "":
		// The clock, the space, and the spinner cell it stands beside.
		reserve = ansi.StringWidth(counting) + 2
	}
	room := width - railWidth - nameWidth - reserve
	if statWidth := ansi.StringWidth(statPlain) + 2; room-statWidth < 8 {
		statPlain, statPainted = "", ""
	} else {
		room -= statWidth
	}
	target = fit(target, room-1)

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
	used := railWidth + nameWidth
	if target != "" {
		line += " " + a.paintTarget(e, target)
		used += 1 + ansi.StringWidth(target)
	}
	if statPlain != "" {
		line += "  " + statPainted
		used += 2 + ansi.StringWidth(statPlain)
	}
	if e.status == toolFailed {
		line += " " + mark
		used += 2
		mark = ""
	}
	// tail is the unpainted width of whatever ends the line: one cell for a
	// glyph, the whole word for an elapsed time, both for a call still counting.
	tail := 1
	switch {
	case elapsed != "":
		mark, tail = a.pal.dim(elapsed), ansi.StringWidth(elapsed)
	case counting != "" && mark != "":
		mark, tail = countingInk+" "+mark, ansi.StringWidth(counting)+2
	}
	if mark == "" {
		return line
	}
	// The spinner — or what replaced it, or what now stands beside it — sits at
	// the line's right end.
	if pad := width - used - tail; pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line + mark
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
//
// The rail stays. It costs four cells and it is what says these rows are one
// object rather than four unrelated lines in a column of prose, which is worth
// more on a narrow screen than on a wide one, not less.

// phoneGutterWidth is the state column: the mark, and the space after it.
const phoneGutterWidth = 2

// phoneStatFloor is how many cells the target must keep before the row is
// allowed to spend any on a stat. It is the wide row's own floor, restated so
// the two tiers drop the same thing at the same moment.
const phoneStatFloor = 8

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
	railWidth := ansi.StringWidth(rail)
	gutter, gutterWidth := a.phoneGutter(e)
	nameWidth := ansi.StringWidth(name)

	reserve := 0
	if clockPlain != "" {
		reserve = ansi.StringWidth(clockPlain) + 1
	}
	room := width - railWidth - gutterWidth - nameWidth - reserve
	if statWidth := ansi.StringWidth(statPlain) + 2; statPlain == "" || room-statWidth < phoneStatFloor {
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
	used := railWidth + gutterWidth + nameWidth
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
		// The budgeted command fragment: the directory it runs in is context,
		// and the row keeps the work. [fit] does the rest at the call site.
		if _, command, found := cutCDPrefix(target); found {
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
	if e.status != toolRunning || e.began.IsZero() || a.state != stateWorking {
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
		if a.state != stateWorking {
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
	if e.status != toolRunning || e.began.IsZero() || a.state != stateWorking {
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
// when it set one, the session's default when it did not, the session's cap
// above that — the same law internal/session's wrapper applies to the wire
// args, restated here from the call's original args so the row agrees with the
// clock the command actually dies on.
func toolLimit(e *entry) time.Duration {
	if e.tool != "bash" {
		return 0
	}
	var args struct {
		Timeout    float64 `json:"timeout"`
		Background bool    `json:"background"`
	}
	seconds := float64(session.DefaultBashTimeoutSeconds)
	if raw := strings.TrimSpace(e.detail.Args); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err == nil {
			if args.Background {
				return 0
			}
			if args.Timeout > 0 {
				seconds = math.Min(args.Timeout, session.MaxBashTimeoutSeconds)
			}
		}
	}
	return time.Duration(seconds * float64(time.Second))
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
// ABOUT stays primary ink (or, for a search, the accent — a pattern is a
// question, not a place), and what merely qualifies it recedes to dim. The
// parse is deliberately conservative and shape-based — only the `cd X && `
// prefix, only a trailing range or flag after a space — because a target this
// file guessed wrong about is a line that says the wrong thing is important.

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
		// The cd prefix keeps its own rule ahead of the lexer, and the two do not
		// disagree: the lexer would paint `cd` as a command and `/tmp` as a path,
		// which is true, and the parameter hierarchy says that whole clause is
		// CONTEXT rather than substance. Context recedes; the work is what the
		// eye should land on.
		if context, command, found := cutCDPrefix(target); found {
			return a.pal.dim(context) + a.pal.shell(command)
		}
		return a.pal.shell(target)

	case "grep", "find":
		// The pattern leads and the place follows it. Accent rather than ink
		// because a pattern is the one target on this surface that is not a
		// thing that exists — it is what the call is looking for.
		pattern, where, found := strings.Cut(target, " ")
		if !found {
			return a.pal.accent(target)
		}
		return a.pal.accent(pattern) + a.pal.dim(" "+where)

	case "read", "edit", "write":
		path, rest, found := strings.Cut(target, " ")
		if !found {
			return a.pal.ink(target)
		}
		return a.pal.ink(path) + a.pal.dim(" "+rest)
	}
	return a.pal.ink(target)
}

// cutCDPrefix splits the one bash shape worth splitting: a command that changes
// directory before doing the thing it is about. Everything else — a pipeline, a
// chain of two real commands, a cd with no `&&` — is left whole, because the
// only prefix that is reliably CONTEXT rather than work is this one.
func cutCDPrefix(command string) (context, rest string, found bool) {
	if !strings.HasPrefix(command, "cd ") {
		return "", command, false
	}
	at := strings.Index(command, " && ")
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

// previewBody is what a call that has NOT finished shows under its row: the
// header, the rows, and how many were dropped.
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
func (a *app) previewBody(e *entry, width int) (head string, body []string, more int) {
	if !e.status.live() || width < 8 {
		return "", nil, 0
	}
	switch e.tool {
	case "edit":
		body = a.diffRows(e, width)
	case "write":
		body = a.plainRows(argString(argsOf(e.detail.Args), "content"), width)
	default:
		return "", nil, 0
	}
	if len(body) == 0 {
		return "", nil, 0
	}
	if !e.full && len(body) > previewWindow {
		more, body = len(body)-previewWindow, body[:previewWindow]
	}
	return a.previewHead(e), body, more
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
		if head, body, more := a.previewBody(e, width); head != "" {
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
		content := argString(argsOf(e.detail.Args), "content")
		return a.cap(e, a.plainRows(content, width), writeWindow)
	case "read":
		return a.cap(e, a.plainRows(resultText(e.detail.Output), width), readWindow)
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
		out = append(out, a.pal.dim(fit(path, width)))
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
