package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The render core is two passes and one cache.
//
// PASS 1 — an entry renders ITSELF into rows and remembers them. It re-renders
// when its own text changes or the width does, and at no other time; a frame
// that touches a settled paragraph is a frame that wrapped text nobody
// re-typed.
//
// PASS 2 — [app.layout] joins those rows into the screen list, and it is the
// ONLY place a blank row is ever emitted. That is the whole fix for the gaps
// this surface used to grow: every entry politely left a line above itself, two
// polite entries left two, and nobody owned the result. Entries no longer get a
// vote.
//
// The frame joins [app.window]'s slice of that list and nothing else — no
// transcript rebuild, no re-wrap per keystroke.

// hitKind is what a visible row answers to a click.
type hitKind uint8

const (
	hitNone hitKind = iota
	hitTool         // a tool call: click expands that call inline
	hitFold         // the "N earlier tool calls" line: click expands the turn
	hitMore         // the "… N more lines" foot of a capped expansion: click lifts the cap
)

// row is one visible screen row and what it points at. It is the single
// mapping from screen geometry to the conversation: the frame joins row.text,
// the mouse hit-tests row.entry, and ↑/↓ walk the same list. Two sources of
// truth for "which entry is this row" is how a click lands on the wrong call.
type row struct {
	text  string
	entry int // index into app.entries; -1 for a blank or the fold line
	hit   hitKind
	turn  int // the turn a fold line folds
}

// toolWindow is how many of a turn's tool calls stay on screen. Three is the
// number a person can hold without reading: the call that is running and the
// two it followed.
const toolWindow = 3

// visible returns the row list, rebuilding it only when something changed.
//
// The dirty flag is the whole of the repaint discipline. A streamed delta
// mutates the entry's text but does NOT set it — the frame clock does, once per
// [frameInterval] — so a hundred deltas in a second cost one hundred string
// appends and thirty layouts, not a hundred layouts.
func (a *app) visible(width int) []row {
	if a.rows == nil || a.rowsWidth != width || a.dirty {
		a.rows = a.layout(width)
		a.rowsWidth = width
		a.dirty = false
		a.builds++
	}
	return a.rows
}

// layout is THE SPACING LAW (docs/CHAT-V3.md D11), and it is a law because it
// is enforced in exactly one place. Every blank row on this surface is emitted
// by the four rules below and by nothing else — no entry appends one, no
// renderer leaves one behind (see [trimBlanks]):
//
//   - ONE blank before a tool cluster that directly follows text. A cluster
//     that answers the person's own message gets none: the calls ARE the reply
//     starting, and a gap there would read as a pause that did not happen.
//   - ZERO between the lines of a cluster — a cluster is one thing.
//   - ONE blank after a cluster, before the text that follows it.
//   - ONE blank before each user message: the turn boundary, the only
//     structural silence this surface has.
//
// Two rules can ask for the same gap — a cluster ending a turn, then the next
// user message — and a gap asked for twice is still one gap, which is why each
// block asks once, before it draws. Nothing is ever emitted at the top of the
// transcript.
func (a *app) layout(width int) []row {
	out := make([]row, 0, len(a.entries)+8)
	// wasCluster says the block that just drew was a tool cluster, and is the
	// whole of the state this pass carries.
	wasCluster := false
	gap := func() {
		if len(out) > 0 {
			out = append(out, row{entry: -1})
		}
	}
	for i := 0; i < len(a.entries); i++ {
		e := &a.entries[i]

		// A run of tool entries from one turn is a cluster, and a cluster is
		// laid out as a unit: it is the thing that folds.
		if e.kind == entryTool {
			end := i + 1
			for end < len(a.entries) &&
				a.entries[end].kind == entryTool &&
				a.entries[end].turn == e.turn {
				end++
			}
			if !wasCluster && !a.opensTurn(i) {
				gap()
			}
			out = a.clusterRows(out, i, end, width)
			wasCluster = true
			i = end - 1
			continue
		}

		rows := a.entryRows(i, width)
		if len(rows) == 0 {
			continue
		}
		if wasCluster || e.kind == entryUser {
			gap()
		}
		for _, text := range rows {
			out = append(out, row{text: text, entry: i})
		}
		wasCluster = false
	}
	if line, ok := a.ellipsis(); ok {
		if wasCluster {
			gap()
		}
		out = append(out, row{text: line, entry: -1})
	}
	// THE POINTER, LAST. Hover is a property of the screen and not of the
	// conversation, so it is applied to finished rows in one pass here rather
	// than threaded through six renderers (hover.go).
	for i := range out {
		if a.isHot(out[i]) {
			out[i].text = a.hoverRow(out[i].text, width)
		}
	}
	return out
}

// isHot reports whether the pointer is on this row. The linear tier has no
// pointer at all (Options.Linear), so it has no hot row.
func (a *app) isHot(r row) bool {
	if a.linear {
		return false
	}
	switch a.hot.kind {
	case hoverEntry:
		return r.entry >= 0 && r.entry == a.hot.entry
	case hoverFold:
		return r.hit == hitFold && r.turn == a.hot.turn
	}
	return false
}

// opensTurn reports whether the entry at i is the first thing its turn drew.
// A cluster that opens a turn follows the person's own message and takes no
// blank of its own — the user message already brought one.
func (a *app) opensTurn(i int) bool {
	for at := i - 1; at >= 0; at-- {
		if a.entries[at].turn != a.entries[i].turn {
			return true
		}
		if a.entries[at].kind != entryUser {
			return false
		}
	}
	return true
}

// entryRows is the per-entry cache for everything that is not a tool call.
//
// Tool lines never come through here — [app.clusterRows] draws them, because
// their marker depends on their position in the cluster — and they are
// deliberately NOT cached: one of them is animating, all of them are one line
// plus a bounded expansion, and a cache with an animation in it is a cache that
// has to be invalidated thirty times a second, which is not a cache but a bug
// with a field.
func (a *app) entryRows(i, width int) []string {
	e := &a.entries[i]
	if e.built && e.width == width && !e.stale {
		return e.rows
	}
	e.rows = a.renderEntry(i, e, width)
	e.width, e.built, e.stale = width, true, false
	return e.rows
}

// renderEntry paints one block. Nothing here appends a blank row — see
// [app.layout].
//
// The index is carried in for one reason: hover is a fact about a POSITION in
// the conversation, and the only block that draws its own hover state — the
// thinking block, whose marker brightens — is also the only one whose rows are
// cached (hover.go marks it stale in exchange).
func (a *app) renderEntry(i int, e *entry, width int) []string {
	switch e.kind {
	case entryUser:
		// THE PERSON'S OWN WORDS, IN THE PERSON'S OWN HUE — the glyph and the
		// whole body in the accent, and every continuation line aligned under
		// the TEXT rather than under the glyph. The glyph marks the turn; the
		// column belongs to the sentence.
		//
		// The body was bold ink until this wave, and bold was the wrong marker
		// for one reason: MARKDOWN OWNS WEIGHT. An assistant answer with a bold
		// lead-in renders exactly like a person's message, and the two things a
		// reader must never confuse were separated by an attribute either of
		// them could wear. Hue is the one channel identity can hold alone —
		// nothing the model writes is ever painted in the accent — so identity
		// takes hue and markdown keeps weight, and neither can impersonate the
		// other. On a sixteen-colour terminal the accent degrades to bold
		// (styles.go's tier table), which is the old rendering and the right
		// one there: with no hue at all, weight is the only marker left.
		body := wrap(e.text, width-2)
		out := make([]string, 0, len(body))
		for i, line := range body {
			lead := "  "
			if i == 0 {
				lead = a.pal.accent(a.pal.youGlyph())
			}
			out = append(out, lead+a.pal.accent(line))
		}
		return out

	case entryAssistant:
		return a.assistantRows(e, width)

	case entryThinking:
		return a.thoughtRows(e, width, a.hoveringEntry(i))

	case entryDivider:
		return []string{a.divider(e.text, width)}

	case entryNote:
		body := wrap(e.text, width-2)
		out := make([]string, 0, len(body))
		for i, line := range body {
			lead := "· "
			if i > 0 {
				lead = "  "
			}
			out = append(out, a.pal.dim(lead+line))
		}
		return out
	}
	return nil
}

// assistantRows is the markdown swap.
//
// While a turn streams, the live tail is PLAIN wrapped text: markdown of a
// half-written sentence costs a parse per frame and re-flows under the
// reader's eye. Every [markdownThrottle] the settled PREFIX — everything up to
// the last newline — is promoted to rendered rows and remembered as promoted,
// so the formatting catches up without the tail flickering between two
// renderings. On EventTurnDone the whole block is rendered at once.
func (a *app) assistantRows(e *entry, width int) []string {
	if e.settled {
		return trimBlanks(renderMarkdown(e.text, width))
	}
	var out []string
	if e.mdCut > 0 {
		out = append(out, renderMarkdown(e.text[:e.mdCut], width)...)
	}
	out = append(out, wrap(e.text[e.mdCut:], width)...)
	return trimBlanks(out)
}

// stillWorking is how long a turn has to be silent before the indicator says
// so out loud.
//
// THE DEFECT THIS FIXES: the session's loop retries a failed request silently,
// with a backoff — a rate limit, a 529, a connection reset — and it says nothing
// to the surface while it does, because a retry that succeeds is not news. From
// the outside that is indistinguishable from a hang: three dots, pulsing, for
// forty seconds. The dots are the only thing on screen and they claim exactly
// as much at second one as at second forty.
//
// Ten seconds is chosen against the thing being waited on rather than against a
// person's patience: a first token from a large model on a cold cache can take
// six or seven, so below ten this would fire on ordinary turns and mean nothing.
// Past it, silence is either a retry or a very long tool-free think, and "still
// working" is true of both — which is why it says that and not "retrying". The
// surface does not know that it is retrying. It knows the stream has said
// nothing for ten seconds, and that is exactly what it claims.
const stillWorking = 10 * time.Second

// stillWorkingWord is the suffix.
const stillWorkingWord = " · still working"

// pulse is the ellipsis frame — or the still one, in the linear tier, where an
// animation is a word repeated forever.
func (a *app) pulse() string {
	if a.linear {
		return ellipsisFrames[len(ellipsisFrames)-1]
	}
	return ellipsisFrames[(a.paints/pulseStep)%len(ellipsisFrames)]
}

// ellipsis is the sign of life while a turn runs and nothing else on screen is
// moving. It is suppressed while text is actively streaming, and suppressed
// while any call is spinning: the text and the spinner each already answer "is
// this alive?", and two answers to one question is one too many.
func (a *app) ellipsis() (string, bool) {
	if a.state != stateWorking || a.running() {
		return "", false
	}
	if a.live >= 0 && a.live < len(a.entries) && a.entries[a.live].text != "" && !a.quiet() {
		return "", false
	}
	line := a.pal.accent("  " + a.pulse())
	if a.silentFor() >= stillWorking {
		line += a.pal.dim(stillWorkingWord)
	}
	return line, true
}

// silentFor is how long the stream has said nothing. Zero when nothing has ever
// arrived, which is a turn that has not started rather than one that has stopped.
func (a *app) silentFor() time.Duration {
	if a.lastDelta.IsZero() {
		return 0
	}
	return time.Since(a.lastDelta)
}

// divider is the compaction mark: a rule with the fact in it, because a
// conversation that silently lost its middle is a conversation the person
// cannot reason about.
func (a *app) divider(hint string, width int) string {
	label := " ⚭ " + hint + " "
	rest := width - ansi.StringWidth(label) - 2
	if rest < 0 {
		return a.pal.dim(ansi.Truncate("──"+label, width, glyphMore))
	}
	left := rest / 2
	return a.pal.dim(strings.Repeat("─", left+2) + label + strings.Repeat("─", rest-left))
}

// THE STATUS LINE, and it is the LAST row of the frame.
//
//	porting the parser · openai/gpt-4.1-mini · $0.14 · 12% ctx · working
//
// Five segments, in the order a person asks for them: WHICH conversation this
// is, WHAT is answering it, what it has SPENT, what it is CARRYING, and what it
// is DOING. Everything is dim — the surface talking about itself is never the
// subject — except the last segment, which is the only thing on the line that
// changes without being asked and is therefore the only thing painted:
//
//	working      accent   the model has the turn
//	waiting      violet   IT HAS THE TURN AND IT IS YOURS (consent.go)
//	idle         dim      nothing is happening
//	interrupted  soft red the last turn was stopped by hand
//
// The line OPENS WITH THE PRODUCT'S NAME, and the name is "openaf" — the one
// word this surface calls itself, wherever it speaks (styles.go's [product]).
// It was dropped for a wave on the argument that a person who has opened the
// thing knows what they have opened, and that argument was right about the
// reader and wrong about the screenshot: this line is what a terminal
// photograph, a bug report and a shared pane carry, and a status bar that names
// everything except the program is the one fact none of them can recover. It is
// also the first segment to go when the frame is narrow — see below — so it
// costs a wide terminal nine cells and a narrow one nothing.
//
// The workspace is still gone from it: the session's own name for the
// conversation says more about which window this is than its directory's base
// name does, and the directory is what the shell prompt behind it already says.
// The name falls back to the place when the session has not named itself yet
// (title.go), so the segment is never empty.
//
// The hints ride the right end. They were a row of their own until this wave;
// two keys is not a row.
const statusHints = "/help · ctrl+o"

func (a *app) status(width int) string {
	sep := a.pal.dim(" · ")
	name := a.title
	if name == "" {
		name = a.place
	}
	word, painted := a.stateWord()

	parts := []string{product, name, a.model, dollars(a.cost)}
	// The context segment is the one part of the line that can be painted
	// without being the state word, so it is assembled and painted separately
	// and spliced back in below.
	context, crowded := a.contextSegment()
	if context != "" {
		parts = append(parts, context)
	}
	if warm := a.warmSegment(); warm != "" {
		parts = append(parts, warm)
	}
	// The product name is the FIRST thing dropped on a narrow frame — before
	// the hints, and long before the cost. It is the segment a reader least
	// needs and a screenshot most does, and those are two different frames.
	// The +3 is the two cells the hints are held off by plus the one that makes
	// the gap below non-zero: the two tests have to agree, or the name survives
	// by exactly the width that costs the hints.
	if statusWidth(parts, word)+ansi.StringWidth(statusHints)+3 > width {
		parts = parts[1:]
	}

	plain := strings.Join(parts, " · ")
	line := ""
	for i, part := range parts {
		if i > 0 {
			line += sep
		}
		// Everything is dim except a context segment that has got close to
		// compaction. It is painted in place rather than moved to the end
		// because WHERE it is is how a person finds it; the colour is only how
		// they notice it.
		if crowded && part == context {
			line += a.pal.accent(part)
			continue
		}
		line += a.pal.dim(part)
	}
	plain += " · " + word
	line += sep + painted

	// The hints are the first thing to go: they are a reminder, and a reminder
	// that crowds out the cost is not one. They are dropped whole rather than
	// truncated — "/help · ctr" is not a key anybody can press.
	if gap := width - ansi.StringWidth(plain) - ansi.StringWidth(statusHints) - 2; gap >= 1 {
		line += strings.Repeat(" ", gap) + a.pal.dim(statusHints)
		plain += strings.Repeat(" ", gap) + statusHints
	}
	if ansi.StringWidth(plain) > width {
		line = ansi.Truncate(line, width, "")
	}
	return line
}

// contextSegment is what the conversation is CARRYING, and it reports whether
// that has got close enough to compaction to be painted.
//
//	12.4k/128k · 10%      the ordinary reading
//	842/128k              under one percent: the figure without a percentage
//	                      (empty)  nobody has said what the window is
//
// It leads with the tokens rather than the percentage because the two answer
// different questions and only one of them is answerable without the other. "How
// much am I carrying" is a fact about the conversation; "how much of the window
// is that" is a fact about the model, and it changes under a person's feet when
// they switch models without a single word being added. Both are on the line, in
// that order.
//
// The percentage is dropped entirely below 1% rather than shown as "0%" or "1%".
// A meter that reads 1% for the first twenty turns of a session is not a meter —
// it is what the byte-counting estimator this replaced actually did, and the
// figure it parked at was the only thing anybody ever read off it.
func (a *app) contextSegment() (string, bool) {
	if a.ctxTokens <= 0 || a.ctxWindow <= 0 {
		return "", false
	}
	segment := tokenWord(a.ctxTokens) + "/" + tokenWord(a.ctxWindow)
	if pct, ok := a.ctxPercent(); ok && pct >= 1 {
		segment += " · " + itoa(pct) + "%"
	}
	return segment, a.ctxCrowded()
}

// warmSegment is the session's cached share of everything it has sent — "⟲ 62%"
// — and empty until there is one.
//
// It is a share rather than a count because a count of cached tokens says
// nothing on its own: 40k cached is excellent against 50k sent and a rounding
// error against 4M. The glyph is the same one the per-turn savings note opens
// with, so the running total and the line that explains one turn of it are
// visibly the same subject.
//
// Rounding is toward the honest side: 0% is shown when the share is real but
// tiny, because "there is a cache and it is barely hitting" is a different fact
// from the empty segment's "there is no cache accounting here at all".
func (a *app) warmSegment() string {
	share, ok := session.Usage{Input: a.inputTokens, CacheRead: a.cacheRead}.CachedShare()
	if !ok {
		return ""
	}
	return "⟲ " + itoa(int(share*100)) + "%"
}

// statusWidth is what the line's own facts measure, unpainted — the segments
// and the state word that always follows them.
func statusWidth(parts []string, word string) int {
	return ansi.StringWidth(strings.Join(parts, " · ")) + 3 + ansi.StringWidth(word)
}

// stateWord is the last segment, plain and painted.
//
// A pending question OUTRANKS the run state, and says so in words as well as in
// colour: the turn is technically still working — the tool call is parked
// inside the batch — but what is true about it that a person can act on is that
// it is waiting for them. "your call" rather than "your answer" because it is
// shorter and because it is what it is.
func (a *app) stateWord() (string, string) {
	// COPY OUTRANKS EVERYTHING, because it is the only state on this line that is
	// about the KEYBOARD rather than about the turn. While the viewport is frozen
	// the keys do something else entirely (copymode.go), and a status line that
	// said "idle" would be describing the session correctly and the screen
	// wrongly. The turn underneath keeps running; the row it would have claimed
	// is back the moment esc is pressed.
	if a.copy.on {
		word := a.copyWord()
		return word, a.pal.accent(word)
	}
	if a.asking() {
		return waitingWord, a.pal.askBold(waitingWord)
	}
	word := a.state.String()
	switch a.state {
	case stateWorking:
		return word, a.pal.accent(word)
	case stateInterrupted:
		return word, a.pal.bad(word)
	default:
		return word, a.pal.dim(word)
	}
}

// waitingWord is the state a person has to answer.
const waitingWord = "waiting · your call"

// wrap breaks a block of plain text to width, keeping its own newlines. The
// text is unstyled at this point: styling after wrapping is what keeps every
// width measurement honest.
func wrap(text string, width int) []string {
	if width < 4 {
		width = 4
	}
	text = strings.ReplaceAll(text, "\t", "    ")
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(para, width, ""), "\n")...)
	}
	return out
}

// fit truncates to a printable width, or returns nothing at all when there is
// no room — a one-cell ellipsis in a one-cell gap says less than a space.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, glyphMore)
}

// trimBlanks drops leading and trailing empty rows from a block. It is the
// other half of the spacing law: a block that ends in a newline must not hand
// the layout a gap it did not ask for, because the layout would keep it.
func trimBlanks(rows []string) []string {
	for len(rows) > 0 && strings.TrimSpace(rows[0]) == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
