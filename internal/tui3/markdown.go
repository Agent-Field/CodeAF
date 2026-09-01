package tui3

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/prose"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Markdown on this surface is internal/tui2/prose, unchanged.
//
// A model answers in markdown whether or not anyone asked it to, and there is
// exactly one renderer in this tree that turns that into rows without handing a
// second colour authority a seat on the screen — goldmark's parser walked into
// internal/tui2/tokens. Adapting it here is four lines and one cached Styler;
// re-deriving it would be a second heading ladder, a second table fitter and a
// second answer to what a 16-colour terminal may draw, all drifting from the
// first the week after they were written.
//
// What this file owns is therefore only the two things prose leaves to a
// caller: which Styler paints, and which measure the prose wraps to.
//
// "Which Styler paints" is doing more work than it looks like. The Styler is the
// colour authority, so stating the BODY INK on it ([newMarkdownStyler]) is not a
// second authority arriving — it is this surface answering the one question the
// authority cannot answer for it, which white a reply's paragraphs are read in.
// Everything else about the row — the heading ladder, the code ramp, the raised
// plane under an inline span, what a 16-colour terminal may draw — stays prose's
// and tokens'.
//
// CODE FENCES STAY ON THE TOKENS RAMP, and this is the whole of D11's "else"
// branch. Decision 11 asks for a pastel chroma style (catppuccin-mocha) on
// fenced code IF it can be had without editing internal/tui2 — and it cannot:
// prose.Options carries a width, a measure, a Styler and one semantic callback,
// with no theme parameter and no seam for one. Behind it, prose/code.go BUILDS
// its chroma style out of the tokens ramp and maps every chroma token onto a
// tokens.CodeSlot, so there is no style to swap from this side even in principle
// — the theme is the token layer. Adopting mocha here would mean adding a whole
// second ramp to prose, and would hand the surface a second colour authority —
// exactly what prose's own package comment rejects glamour for.
//
// The cost is small and the floor is already right: the tokens ramp is muted by
// construction, so a fence renders quiet next to styles.go's pastels rather
// than clashing with them. If the theme is wanted later it is one option on
// prose.Options and one line here, and it belongs to whoever owns tui2.
//
// THE BODY INK IS NOT A COUNTER-EXAMPLE to any of that. Stating one token's
// value on the Styler is naming a colour to the authority that already owns the
// answer; shipping a rival ramp is standing a second authority beside it. The
// first is one value and one law to hold it (styles.go, THE GLARE LAW); the
// second is two hundred token relationships maintained twice.

var (
	stylerOnce sync.Once
	styler     *tokens.Styler
)

// markdownStyler is the surface's painter, built once per process.
//
// Once, because a Styler is immutable and every escape sequence in it was
// computed at package initialization by the palette table: rebuilding one per
// render would redo that work on every frame of a stream and produce the same
// bytes each time. Lazily, because construction reads the environment, and a
// package-level var would fix the profile at init — before a test or a caller
// that sets NO_COLOR for a subprocess has had a word.
func markdownStyler() *tokens.Styler {
	stylerOnce.Do(func() { styler = newMarkdownStyler(os.Getenv) })
	return styler
}

// newMarkdownStyler is that construction as a PURE FUNCTION of the environment,
// which is the same shape [newThemedPalette] already has and for the same
// reason: the two decisions below are both read off variables, and a test that
// wants to see what a 256-colour terminal gets has to be able to say so rather
// than race the once.
//
// The profile comes from [tokens.DetectProfile], the same door cmd/aforge opens
// for the v2 surface, so both surfaces answer "what can this terminal say" from
// one decision table rather than from two guesses. The glyph tier stays
// [tokens.Plain]: that axis is opt-out-able elsewhere in the tree by a flag this
// surface does not have yet, and the plain tier is a designed floor, not a
// degradation. Focus is normal — this surface has one pane, so there is nothing
// for a dimmed one to recede behind.
//
// ── AND THE BODY WEARS THIS SURFACE'S OWN INK ──
//
// THERE IS ONE BODY WHITE ON THIS SCREEN AND IT IS THE PALETTE'S. Left alone,
// prose paints a reply at [tokens.TextPrimary], which is a brighter white than
// anything styles.go authors, while every row this package draws around it wears
// [hueInk] — two whites on one screen, and the louder of them on the thing a
// person reads most, at a contrast the palette spent a whole wave coming down
// from (styles.go, THE GLARE LAW). So the Styler is handed the ink before it is
// handed to prose, and the reply's paragraphs, headings and inline code spans
// all come back in it.
//
// The ladder it comes from is asked the way [detectPalette] asks: [rampFor] with
// [themeAuto] and the same environment. It has to be the same question — a
// person on a white terminal reading a reply in a dark-terminal white would be
// the two-whites defect wearing its other face — and asking it here rather than
// caching a ramp keeps ONE answer to "which ladder is this terminal on".
//
// Nothing below the 256 rung takes the override at all, and that is
// [tokens.Styler.WithBodyInk]'s decision rather than one made here: the sixteen
// are the user's own theme, and an authored hex has no honest form there.
func newMarkdownStyler(env func(string) string) *tokens.Styler {
	return tokens.NewStyler(tokens.DetectProfile(env), tokens.FocusNormal).
		WithBodyInk(rampFor(themeAuto, env).ink.tokenColor())
}

// styler is the painter THIS surface hands prose, and it is the seam where the
// two halves of the readability wave meet.
//
// [markdownStyler] above carries the ink the palette was CONSTRUCTED with, which
// is the ink an assumed ground was authored for. adaptive.go re-derives that ink
// the moment a terminal says what colour it actually is, and a styler that kept
// the startup value would put the two-whites defect straight back — the whole
// surface repainted against the measured ground while the one thing a person
// reads most stayed on the ladder nobody measured.
//
// So the door is a METHOD rather than a package function: a surface with a
// measured ground reads its own styler, and every surface without one — which is
// most of them, and every test that never answers — reads the process-wide one
// and pays nothing. The rebuild happens once per measurement, in
// [app.repaintPalette], because a Styler is a value worth caching per pane and
// not per frame ([tokens.Styler]) and a streaming reply asks for one thirty
// times a second.
func (a *app) styler() *tokens.Styler {
	if a.mdStyler != nil {
		return a.mdStyler
	}
	return markdownStyler()
}

// renderMarkdown renders model-written markdown into screen rows, one string
// per row, each at most width printable cells, with the package's typographic
// hierarchy applied: headings promoted by tier (accent, h1 bold), fenced code
// chroma-highlighted, tables fitted (truncate, never wrap), bold/italic, and
// hanging list indents.
//
// It caches nothing: a caller redraws on turn settle and a few times a second
// while a reply streams, and a cache keyed on text that is still growing is a
// cache that is wrong at exactly the moments anyone is looking at it. The one
// thing held across calls is the Styler, which is a value and not a result.
//
// Untrusted text needs no laundering here — prose.Render is itself the
// chokepoint, running every byte through internal/sanitize with the token
// layer's ANSI-16 remap and then stripping the SGR that survives, so a reply
// cannot paint itself a heading. Sanitizing first would only mean doing it
// twice.
func renderMarkdown(text string, width int) []string {
	return renderMarkdownWith(markdownStyler(), text, width)
}

func (a *app) renderMarkdown(text string, width int) []string {
	return renderMarkdownWithCode(a.styler(), text, width, a.plainCodePath)
}

// renderMarkdownWith is [renderMarkdown] against a stated Styler. It is the
// whole body, split off because the profile is detected from the environment
// exactly once per process: a test that wants to see what a truecolor terminal
// gets cannot ask for one afterwards, and a test that raced the detection to
// set NO_COLOR would be a test whose result depended on which test ran first.
func renderMarkdownWith(st *tokens.Styler, text string, width int) []string {
	return renderMarkdownWithCode(st, text, width, nil)
}

// renderMarkdownWithCode carries the one piece of workspace knowledge prose
// needs: whether an inline code span will become a path link after rendering.
func renderMarkdownWithCode(st *tokens.Styler, text string, width int, plainCodeSpan func(string) bool) []string {
	if width < 1 {
		width = 1
	}
	if layoutTier(width) == tierPhone {
		return phoneMarkdown(st, text, width, plainCodeSpan)
	}
	return proseRowsWithCode(st, text, width, plainCodeSpan)
}

// proseRows is the unconditional path: prose renders the whole document, at the
// package's one measure. Every tier above the phone reaches it and nothing else.
func proseRows(st *tokens.Styler, text string, width int) []string {
	return proseRowsWithCode(st, text, width, nil)
}

func proseRowsWithCode(st *tokens.Styler, text string, width int, plainCodeSpan func(string) bool) []string {
	return prose.Render(text, prose.Options{
		Width: width,
		// The hard ceiling is the pane; the reading length is prose's own
		// constant, clamped to the ceiling by prose. A 200-column window is a
		// wide window, not a wide sentence — and naming the constant rather
		// than a number of our own keeps one definition of a measure in the
		// tree.
		Measure:       prose.DefaultMeasure,
		Styler:        st,
		PlainCodeSpan: plainCodeSpan,
	})
}

// ── THE PHONE TIER ──────────────────────────────────────────────────────────
//
// A fenced block TRUNCATES (prose/code.go says so, and says why: indentation is
// how source is read, and a caller that can scroll should do the cropping). At
// forty-four columns there is no caller that can scroll sideways, so the law is
// right everywhere except here, where its premise is false — the reader has no
// horizontal scroll and the cut takes the half of the line that carried the
// meaning. Same for a table: a four-column table fitted into forty-four cells
// is four ellipses.
//
// So the phone tier, and ONLY the phone tier ([layoutTier] — this file never
// compares a width itself), wraps instead of cutting. Everything above it
// reaches [proseRows] and renders byte-identically to before.
//
// THE WIDTH THIS SEES IS THE BODY'S, NOT THE FRAME'S, and the two agree where it
// matters: the rail is the only thing that takes columns off the frame, and it
// takes none under railSlimFloor (task.go, 100) — so a body under 60 cells can
// only have come from a frame under 60 cells. Reading the tier off the width the
// text is actually laid out in is also the honest question here: what wraps is
// this column, not the window around it.
//
// THE EDIT IS AT THIS CALL SITE AND NOWHERE ELSE. internal/tui2/prose is a
// second surface's renderer with its own suite; a wrap mode added there would be
// this slice legislating for tui2. What this file does instead is hand prose a
// document whose code lines already fit — the wrap is ours, the rendering,
// tinting and ground stay prose's — and mark the rows it split.

const (
	// mdContMark is the continuation marker, and the two cells it occupies ARE
	// the hanging indent: a first row opens on two spaces, a wrapped one on this.
	//
	// It is the same arrow styles.go spends on the tool fold, and the reservation
	// there ("the fold line's marker, and only the fold line's") is about the
	// TRANSCRIPT's vocabulary — a row that opens a disclosure. This mark never
	// stands in that column: it is dim, it sits in a margin whose next cell is
	// always the code gutter, and it is inert. One arrow means "there is more of
	// this line" in both places, which is the reading that makes them one word
	// rather than two.
	mdContMark = "↳ "
	// mdContLead is what an UNwrapped row opens on: the marker's width in
	// spaces, so every row of a block shares one left edge and the marker is the
	// only thing that varies down the margin.
	mdContLead = "  "
	// mdCodeFloor is the narrowest code column worth wrapping into. Under it the
	// margin and the gutter cost more than they buy, and the block falls back to
	// prose's own rendering — a cut line at eight cells and a wrapped one at
	// eight cells are the same unreadable, and only one of them is new code.
	mdCodeFloor = 8
)

// phoneMarkdown renders a document for a phone-width column: prose blocks
// through prose, fenced code wrapped, tables stacked.
//
// It is a PURE FUNCTION of (text, width) — no cache, no carried state, nothing
// read from the app — which is what makes a resize a re-wrap and a re-wrap
// idempotent, and what lets a streaming reply call it on a growing prefix every
// frame without an earlier row ever changing under the reader's eye.
func phoneMarkdown(st *tokens.Styler, text string, width int, plainCodeSpan func(string) bool) []string {
	var out []string
	for _, seg := range mdSegments(text) {
		var rows []string
		switch seg.kind {
		case mdSegFence:
			rows = phoneCodeRows(st, seg, width)
		case mdSegTable:
			rows = phoneTableRows(st, seg, width, plainCodeSpan)
		default:
			rows = proseRowsWithCode(st, seg.text, width, plainCodeSpan)
		}
		if len(rows) == 0 {
			continue
		}
		// One blank row between blocks, which is the gap prose puts between two
		// of its own — a document does not learn a wider rhythm because this
		// file rendered part of it.
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, rows...)
	}
	return out
}

// The three kinds of block this file tells apart. Everything that is not a
// top-level fence or a top-level table is prose, and prose is whatever goldmark
// says it is — this scanner never tries to parse markdown, only to find the two
// shapes that need taking out of it.
type mdSegKind int

const (
	mdSegProse mdSegKind = iota
	mdSegFence
	mdSegTable
)

type mdSegment struct {
	kind mdSegKind
	// text is the source, for prose and for a fence's BODY (no delimiters).
	text string
	// info is a fence's language, kept because it is what chroma lexes by.
	info string
	// table is the header row followed by the body rows, cells already split.
	table [][]string
	// src is a table's own source lines, delimiter row and all. It is kept
	// because mdtable.go hands a table back to prose ALONE to find out where
	// prose drew it inside the document, and the only text that renders to the
	// same rows is the text the document had (see [app.openableTables]).
	src string
}

// mdSegments splits a document into the runs [phoneMarkdown] renders three ways.
//
// A fence and a table are recognized ONLY AT COLUMN ZERO, and that is the whole
// of the nesting story. A fence inside a list item or a blockquote must be
// indented to sit inside it, so an unindented fence is a top-level fence by
// construction — and one that IS indented stays in its prose segment and renders
// exactly as it does today, cut rather than wrapped. That is a real gap and it
// is the deliberate one: pulling a fence out of a list item would mean this
// file re-deriving the list's marker column, its numbering and its gutter, which
// is goldmark's job and prose's, done worse and in a second place.
func mdSegments(text string) []mdSegment {
	lines := strings.Split(text, "\n")
	var (
		out   []mdSegment
		block []string
	)
	flush := func() {
		if len(block) > 0 {
			out = append(out, mdSegment{kind: mdSegProse, text: strings.Join(block, "\n")})
			block = nil
		}
	}
	for i := 0; i < len(lines); {
		if delim, info, ok := mdFenceOpen(lines[i]); ok {
			end := i + 1
			for end < len(lines) && !mdFenceClose(lines[end], delim) {
				end++
			}
			flush()
			out = append(out, mdSegment{
				kind: mdSegFence,
				info: info,
				// A fence with no closing line is a fence that is still being
				// typed — a stream renders one on every frame — and it holds
				// everything to the end, which is what goldmark does with it too.
				text: strings.Join(lines[i+1:end], "\n"),
			})
			i = end + 1
			continue
		}
		if table, end, ok := mdTableAt(lines, i); ok {
			flush()
			out = append(out, mdSegment{
				kind:  mdSegTable,
				table: table,
				src:   strings.Join(lines[i:end], "\n"),
			})
			i = end
			continue
		}
		block = append(block, lines[i])
		i++
	}
	flush()
	return out
}

// mdFenceOpen reads an opening fence at column zero, returning its delimiter run
// and its info string.
func mdFenceOpen(line string) (delim, info string, ok bool) {
	var mark byte
	switch {
	case strings.HasPrefix(line, "```"):
		mark = '`'
	case strings.HasPrefix(line, "~~~"):
		mark = '~'
	default:
		return "", "", false
	}
	n := 0
	for n < len(line) && line[n] == mark {
		n++
	}
	info = strings.TrimSpace(line[n:])
	// A backtick in a backtick fence's info string is not an info string —
	// CommonMark says so, and the line is prose.
	if mark == '`' && strings.ContainsRune(info, '`') {
		return "", "", false
	}
	// The language is the first word; the rest is metadata no lexer reads.
	if cut := strings.IndexAny(info, " \t"); cut >= 0 {
		info = info[:cut]
	}
	return line[:n], info, true
}

// mdFenceClose reports whether line closes a fence opened by delim: the same
// character, at least as many of them, and nothing else on the row.
func mdFenceClose(line, delim string) bool {
	trimmed := strings.TrimRight(line, " \t")
	if len(trimmed) < len(delim) {
		return false
	}
	mark := delim[0]
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != mark {
			return false
		}
	}
	return true
}

// mdTableAt reads a GFM table starting at lines[i]: a header row, a delimiter
// row of matching width, and every row under it until a blank line or a line
// with no pipe in it. It returns the header and body rows and the index after
// the table.
func mdTableAt(lines []string, i int) (rows [][]string, end int, ok bool) {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") || lines[i] != strings.TrimLeft(lines[i], " \t") {
		return nil, 0, false
	}
	head := mdCells(lines[i])
	if len(head) == 0 || !mdDelimiterRow(lines[i+1], len(head)) {
		return nil, 0, false
	}
	rows = [][]string{head}
	j := i + 2
	for ; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" || !strings.Contains(lines[j], "|") {
			break
		}
		rows = append(rows, mdCells(lines[j]))
	}
	return rows, j, true
}

// mdDelimiterRow reports whether line is a GFM alignment row of exactly n cells.
func mdDelimiterRow(line string, n int) bool {
	cells := mdCells(line)
	if len(cells) != n {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimPrefix(cell, ":")
		cell = strings.TrimSuffix(cell, ":")
		if cell == "" || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

// mdCells splits one table row into its cells, honouring the one escape GFM has
// inside a table: `\|` is a pipe and not a wall.
func mdCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var (
		cells []string
		cur   strings.Builder
	)
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

// phoneTableRows stacks a table: one `key: value` line per cell, one blank row
// between records.
//
// A table read on a phone is a table read one row at a time, and the column
// header is the only thing that says what a cell MEANS — which is why the header
// travels with the cell rather than standing once at the top where a reader who
// has scrolled past it cannot see it. Empty cells are dropped: a row that spends
// a line saying a field is blank is a row that costs a tenth of the screen to
// say nothing.
//
// Each line goes back through prose, so a cell keeps its bold, its code spans
// and its links, and wraps at the same measure as the paragraph above it.
func phoneTableRows(st *tokens.Styler, seg mdSegment, width int, plainCodeSpan func(string) bool) []string {
	if len(seg.table) == 0 {
		return nil
	}
	head := seg.table[0]
	records := seg.table[1:]
	if len(records) == 0 {
		// A header with no rows under it is a list of column names, and the
		// honest rendering of that is the names.
		records, head = [][]string{head}, nil
	}
	var out []string
	for _, record := range records {
		var rows []string
		for j, cell := range record {
			if head != nil && j >= len(head) {
				// GFM drops the cells past the header's width, and a stacked
				// rendering that kept them would be showing a reader a value
				// with no name — worse than the table it came from.
				break
			}
			if strings.TrimSpace(cell) == "" {
				continue
			}
			key := ""
			if j < len(head) {
				key = head[j]
			}
			rows = append(rows, proseRowsWithCode(st, mdKeyed(key, cell), width, plainCodeSpan)...)
		}
		if len(rows) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, rows...)
	}
	return out
}

// mdKeyed writes one stacked cell as markdown source. The key is emphasized so
// the eye can run down the labels — unless the header already carries markup of
// its own, in which case adding more would be this file editing the author's
// text rather than laying it out.
func mdKeyed(key, value string) string {
	key = strings.TrimSpace(key)
	switch {
	case key == "":
		// With no key in front of it a cell OPENS the line, where a leading
		// `-` or `#` it never meant as markup would be read as one. The
		// backslash is markdown's own escape and renders as nothing.
		if v := strings.TrimSpace(value); v != "" && strings.IndexByte("#-*+>=|", v[0]) >= 0 {
			return "\\" + v
		}
		return value
	case strings.ContainsAny(key, "*_`[]<>\\"):
		return key + ": " + value
	default:
		return "**" + key + "**: " + value
	}
}

// phoneCodeRows renders one fenced block wrapped rather than cut.
//
// The block is rendered by PROSE, at two cells less than the column, and the two
// cells this keeps are the margin the continuation marker stands in. That split
// is the point: chroma's lexing, the gutter, the raised ground and the width
// ceiling all stay in the one file that owns them, and what this adds is a
// document whose lines already fit and a mark down the left edge saying which of
// them the reader was not given by the model.
//
// The marker lives OUTSIDE the code plane deliberately. A `↳` set inside the
// block would be a character on the same ground, in the same ramp, as the source
// around it — indistinguishable from something the code actually said. It is
// also what copy mode takes with it, and a margin cell is chrome a paste can
// drop, exactly as the gutter beside it already is.
func phoneCodeRows(st *tokens.Styler, seg mdSegment, width int) []string {
	body := strings.ReplaceAll(seg.text, "\t", "    ")
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		return nil
	}
	inner := width - len(mdContLead)
	// prose's fenced block spends three cells of its width on the gutter, the
	// cell of padding after it and the cell of ground before the right edge
	// (prose/code.go). What is left is what a source line may occupy.
	content := inner - 3
	if content < mdCodeFloor {
		// Too narrow for a wrap to be an improvement: prose renders it whole,
		// which is what every other tier gets.
		return proseRows(st, mdFenceSource(seg.info, body), width)
	}

	type chunk struct {
		text string
		cont bool
	}
	var chunks []chunk
	for _, line := range strings.Split(body, "\n") {
		for k, part := range wrapCodeLine(line, content) {
			chunks = append(chunks, chunk{text: part, cont: k > 0})
		}
	}
	if len(chunks) == 0 {
		return nil
	}

	var src strings.Builder
	src.Grow(len(body) + 32)
	for i, c := range chunks {
		if i > 0 {
			src.WriteByte('\n')
		}
		src.WriteString(c.text)
	}
	rows := prose.Render(mdFenceSource(seg.info, src.String()), prose.Options{
		Width: inner,
		// A figure fills its column: prose's reading measure is for sentences,
		// and a code line that stopped at 88 cells inside a 44-cell frame would
		// have stopped for no reason anyone could see.
		Measure: inner,
		Styler:  st,
	})

	out := make([]string, len(rows))
	for i, row := range rows {
		lead := mdContLead
		// One row per source line is prose's contract for a fence, and the
		// markers are keyed on it. If it ever stops holding, the block still
		// renders — it just renders with a plain margin and no claim about
		// which line was split, which is the safe half of the feature.
		if len(rows) == len(chunks) && chunks[i].cont {
			lead = mdInk(st, mdContMark)
		}
		out[i] = lead + row
	}
	return out
}

// mdFenceSource writes a fence back out as markdown, with a delimiter longer
// than any backtick run inside it — source that contains ``` is source, not a
// closing fence, and a block that ended early would take the rest of the reply
// with it.
func mdFenceSource(info, body string) string {
	// A backtick in a backtick fence's info string is what makes the line stop
	// being a fence — a tilde fence is allowed one, and this writes every fence
	// back out with backticks.
	info = strings.ReplaceAll(info, "`", "")
	run, longest := 0, 0
	for i := 0; i < len(body); i++ {
		if body[i] == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + info + "\n" + body + "\n" + fence
}

// mdInk paints the continuation marker at the chrome tier. A nil Styler — the
// headless default prose itself accepts — draws the mark unpainted, which is the
// same degradation a NO_COLOR profile makes: the arrow is a SHAPE, and the shape
// is what carries the meaning.
func mdInk(st *tokens.Styler, s string) string {
	if st == nil {
		return s
	}
	return st.PaintToken(s, tokens.TextTertiary)
}

// wrapCodeLine breaks one source line into rows of at most width cells.
//
// It breaks at a space when there is one in the back half of the row — an
// argument list and a shell pipeline both read better broken between words — and
// MID-TOKEN when there is not. A forty-cell URL in a thirty-cell column has no
// break in it, and the choice there is between a wrapped token and a lost one.
func wrapCodeLine(line string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for ansi.StringWidth(line) > width {
		head := ansi.Cut(line, 0, width)
		if head == "" {
			// A grapheme wider than the whole column: take it whole rather than
			// advance by nothing, which is a loop that never ends. prose's own
			// ceiling trims the overhang.
			head = string([]rune(line)[:1])
		}
		rest := line[len(head):]
		if at := strings.LastIndexByte(head, ' '); at > 0 && ansi.StringWidth(head[:at]) >= (width+1)/2 {
			// The run of spaces at the break is alignment, and alignment for a
			// column the reader is no longer in is dead cells.
			head, rest = head[:at], strings.TrimLeft(line[at:], " ")
		}
		out = append(out, head)
		line = rest
	}
	return append(out, line)
}

// ── INLINE TASK LINKS ───────────────────────────────────────────────────────
//
// A MODEL THAT SAYS "task 7" IS NAMING A DOOR, AND THE DOOR WAS NOT THERE.
// Everywhere else on this surface a running node can be pressed — the strip's
// chips, the roster's rows, a spawn card, a landed card — and the one place the
// work is named most often is the reply that talks about it, where the name was
// nine dead cells of prose. A person reading "I split this into task 7 and task
// 8" had to go and find those two rows in a column to look at either.
//
// So a reference becomes a link: accent, underlined, and pressing it opens that
// node's room through the same door the chips use (room.go's [app.openRoomFor]).
//
// ── THE GRAMMAR IS SMALL ON PURPOSE ──
//
// A number in prose is almost never a task, so the only shapes recognized are
// the ones that name one out loud — an anchoring "task"/"tasks", a space, an
// optional "id", an optional "#", and the digits:
//
//	task 7 · task #7 · tasks id 7 · task id #7
//
// A bare "7" is not one, a bare "#7" is not one, "taskbar 7" is not one, and
// "task 7.1" is not one (a version, a date and a range all read that way). What
// this refuses costs a reader nothing — the words are still there — and what a
// looser grammar would cost is a paragraph pockmarked with underlined numbers.
//
// AN UNKNOWN ID IS PLAIN TEXT. The reference is resolved against the nodes this
// surface has actually seen ([app.tasks]) and a miss is left exactly as the
// model wrote it. A link that opens nothing is worse than no link: it is the
// surface claiming a door it does not have.
//
// ── IT IS A PASS OVER RENDERED ROWS ──
//
// The pass runs on the ROWS prose already produced, not on the markdown behind
// them, and that is what keeps it correct at every width: the wrap has already
// happened, so a reference split across two rows is simply not one, and the
// phone tier's re-wrapped fences and stacked tables need no special case. It is
// a pure function of (row, task index) — no state, nothing cached — so a resize
// re-derives it and a re-render produces the same bytes.
//
// CODE IS NOT PROSE AND GETS NO LINKS. Two guards cover the code that can name
// a task: a fenced block carries [tokens.GlyphCodeGutter], while a non-path
// inline span carries prose's raised plane. Path spans deliberately drop that
// plane under ONE VISIBLE MARK PER TOKEN, but the linker's path grammar cannot
// recognize their whitespace-bearing task reference anyway. At profiles with
// no plane, prose restores backticks and [masked] reads those instead.

// taskLink is one drawn reference: the columns it occupies on its row, and the
// node behind them.
type taskLink struct {
	span  hudSpan
	id    uint64
	title string
	// ord is this reference's place among the ones its BLOCK drew, counted across
	// every row the block wrapped over. It is written by the layout that numbered
	// them (render.go's [app.deckRows]) and read by the pointer, which holds a
	// hover as (block, ordinal): a paragraph re-wraps when the frame is dragged,
	// so a hover stored as "the second link on screen row nine" would follow the
	// wrap instead of following the words.
	ord int
}

// linkTasks is the pass bound to this surface's own task index. It is called
// once per rendered row of model prose (render.go's [app.deckRows]).
//
// hot is which of THIS ROW's references the pointer is on, and -1 for none.
func (a *app) linkTasks(text string, hot int) (string, []taskLink) {
	if text == "" || len(a.tasks) == 0 {
		return text, nil
	}
	return linkifyTasks(text, a.pal, func(id uint64) (string, bool) {
		node := a.tasks[id]
		if node == nil {
			return "", false
		}
		return node.title, true
	}, hot)
}

// linkifyTasks is that pass, whole: one painted row in, the same row with its
// resolved references inked and their columns recorded out.
//
// It returns the row UNTOUCHED whenever it has nothing to say, which is almost
// every row — the cheap check is first, and it is a substring scan for the one
// word every shape in the grammar has to carry.
func linkifyTasks(text string, pal palette, look func(uint64) (string, bool), hot int) (string, []taskLink) {
	if !hasTaskWord(text) {
		return text, nil
	}
	flat, ground := flatten(text)
	if strings.Contains(flat, tokens.GlyphCodeGutter) {
		// A fenced line. The whole row is source, and source that says "task 7"
		// is saying it to a compiler.
		return text, nil
	}
	refs := taskRefs(flat)
	kept := refs[:0]
	for _, ref := range refs {
		if grounded(ground, ref.from, ref.to) || masked(flat, ref.from) {
			continue
		}
		title, ok := look(ref.id)
		if !ok {
			continue
		}
		ref.title = title
		kept = append(kept, ref)
	}
	if len(kept) == 0 {
		return text, nil
	}
	return paintLinks(text, flat, kept, pal, hot)
}

// ── the grammar ─────────────────────────────────────────────────────────────

// taskRef is one recognized reference, in PLAIN byte offsets into its row.
type taskRef struct {
	from, to int
	id       uint64
	title    string
}

// taskWord is the anchor every shape in the grammar opens on.
const taskWord = "task"

// hasTaskWord is the cheap reject, and it is run on the PAINTED row rather than
// on a stripped copy of it: no escape sequence this surface writes contains a
// letter of "task", so a painted row that has none of them has no reference in
// it and costs one scan instead of one allocation.
func hasTaskWord(text string) bool { return indexFold(text, taskWord, 0) >= 0 }

// taskRefs finds every reference in one row of plain text, in order and
// non-overlapping.
func taskRefs(s string) []taskRef {
	var out []taskRef
	for at := 0; at+len(taskWord) <= len(s); {
		i := indexFold(s, taskWord, at)
		if i < 0 {
			break
		}
		// The LEFT boundary: "taskbar" and "subtask" are words, not references.
		// A reference may open a row or follow punctuation — "(task 7)" is one.
		if i > 0 && wordByte(s[i-1]) {
			at = i + len(taskWord)
			continue
		}
		head := i + len(taskWord)
		if head < len(s) && (s[head] == 's' || s[head] == 'S') {
			head++
		}
		if end, id, ok := taskRefTail(s, head); ok {
			out = append(out, taskRef{from: i, to: end, id: id})
			at = end
			continue
		}
		at = i + len(taskWord)
	}
	return out
}

// taskRefTail reads what must follow the anchor: whitespace, an optional "id",
// an optional "#", and the digits. It is the whole of the grammar's tail.
func taskRefTail(s string, at int) (end int, id uint64, ok bool) {
	at, spaced := skipBlank(s, at)
	if !spaced {
		// No gap after the anchor, so the anchor was the head of a longer word:
		// "tasked", "taskbar". The right boundary is enforced here rather than
		// beside the left one because "tasks" is also a legal anchor and the two
		// questions resolve at the same byte.
		return 0, 0, false
	}
	if at+1 < len(s) && (s[at] == 'i' || s[at] == 'I') && (s[at+1] == 'd' || s[at+1] == 'D') {
		if next, gap := skipBlank(s, at+2); gap {
			at = next
		}
	}
	if at < len(s) && s[at] == '#' {
		at++
	}
	start := at
	for at < len(s) && s[at] >= '0' && s[at] <= '9' {
		at++
	}
	// No digits, or more of them than any session will ever mint. The ceiling is
	// what keeps [strconv.ParseUint] off a hundred-digit number somebody pasted.
	if at == start || at-start > 9 {
		return 0, 0, false
	}
	if at < len(s) {
		switch c := s[at]; {
		case wordByte(c):
			// "task 7a" is not a reference to task 7.
			return 0, 0, false
		case (c == '.' || c == '-' || c == '/' || c == ':') && at+1 < len(s) &&
			s[at+1] >= '0' && s[at+1] <= '9':
			// A version, a date, a range, a clock. All of them read as a number
			// followed by another number, and none of them is a task.
			return 0, 0, false
		}
	}
	n, err := strconv.ParseUint(s[start:at], 10, 64)
	if err != nil || n == 0 {
		// Node ids are minted from one, so a "task 0" is prose about nothing.
		return 0, 0, false
	}
	return at, n, true
}

// masked reports whether the reference at this offset is inside something that
// is not prose: a URL, or a backticked span on a terminal with no raised plane
// to draw one on (prose/inline.go's codeSpan says when the backticks come back).
//
// The URL test is a test of the reference's own FIELD, and the grammar is what
// makes that enough: an anchor must be followed by whitespace, so the field an
// anchor sits in ENDS at the anchor — and a field that ends in "task" and has a
// scheme in it is an address, not a sentence.
func masked(flat string, at int) bool {
	from := strings.LastIndexAny(flat[:at], " \t") + 1
	to := at
	for to < len(flat) && flat[to] != ' ' && flat[to] != '\t' {
		to++
	}
	if strings.Contains(flat[from:to], "://") {
		return true
	}
	return strings.Count(flat[:at], "`")%2 == 1
}

// grounded reports whether any cell of a reference was drawn on the raised
// plane, which now means non-path inline code. A linkable path is the one code
// span that spends its visible mark on an underline instead.
func grounded(ground []bool, from, to int) bool {
	for i := from; i < to && i < len(ground); i++ {
		if ground[i] {
			return true
		}
	}
	return false
}

// skipBlank steps over one run of spaces or tabs and reports whether there was
// one. A reference is written with a gap in it and never without.
func skipBlank(s string, at int) (int, bool) {
	start := at
	for at < len(s) && (s[at] == ' ' || s[at] == '\t') {
		at++
	}
	return at, at > start
}

// wordByte reports whether a byte can be part of a word, which is what both
// boundary tests ask. A byte above ASCII is a boundary: this grammar is written
// in ASCII and a rune beside it is punctuation, an emoji or another language,
// none of which continues "task".
func wordByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
		return true
	}
	return false
}

// indexFold is [strings.Index] for an ASCII-lowercase needle, case-insensitive,
// without the allocation a lowered copy of every row would cost.
func indexFold(s, needle string, from int) int {
	for i := from; i+len(needle) <= len(s); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// ── the painted row, taken apart and put back together ──────────────────────

// flatten strips one painted row to its plain bytes and reports, byte for byte,
// whether it was drawn on a background.
//
// It does its own stripping rather than calling [ansi.Strip] for one reason: the
// two answers have to be indexed the same way, and the only guarantee of that is
// that one walk produced both.
func flatten(text string) (string, []bool) {
	var (
		out    strings.Builder
		ground []bool
		on     bool
	)
	out.Grow(len(text))
	for i := 0; i < len(text); {
		if n := escLen(text, i); n > 0 {
			on = sgrGround(text[i:i+n], on)
			i += n
			continue
		}
		out.WriteByte(text[i])
		ground = append(ground, on)
		i++
	}
	return out.String(), ground
}

// paintLinks writes the row back out with every reference inked, and returns the
// columns each one landed in.
//
// THE INK REPLACES WHAT WAS UNDER IT. A reference is one control and it is drawn
// as one, so whatever prose painted inside those bytes — a bold word, a heading
// tier — is dropped for the span and the plain phrase is repainted. What is
// AROUND it is restored exactly: the foreground in force at the cut and the
// underline flag, re-emitted after the link closes, because this surface's paint
// closes with SGR 39 rather than a full reset and a link that swallowed the
// paragraph's colour would be the loudest bug on the screen.
// hot is which of refs the pointer is on, and -1 for none. That one is inked a
// step brighter and nothing else about the row changes — see [taskLinkHotInk].
func paintLinks(text, flat string, refs []taskRef, pal palette, hot int) (string, []taskLink) {
	var (
		out   strings.Builder
		links []taskLink
		fg    string
		under bool
		at    int // the PLAIN offset the walk has reached
		next  int // the reference being looked for
	)
	out.Grow(len(text) + 32*len(refs))
	for i := 0; i < len(text); {
		if next < len(refs) && at == refs[next].from {
			ref := refs[next]
			inked := taskLinkInk(pal, flat[ref.from:ref.to])
			if next == hot {
				inked = taskLinkHotInk(pal, flat[ref.from:ref.to])
			}
			restore := ""
			if inked != flat[ref.from:ref.to] {
				// Only a row that was actually painted needs its paint put back;
				// on a terminal that draws no SGR at all the link is a click
				// target and nothing else, and the row stays byte-identical.
				if fg != "" {
					restore = fg
				} else {
					restore = "\x1b[39m"
				}
				if under {
					restore += "\x1b[4m"
				}
			}
			links = append(links, taskLink{
				span:  hudSpan{from: ansi.StringWidth(flat[:ref.from]), to: ansi.StringWidth(flat[:ref.to])},
				id:    ref.id,
				title: ref.title,
			})
			next++
			// A REFERENCE ALREADY WEARING THIS INK IS LEFT ALONE, and that is the
			// whole of the idempotence: a second pass over a row this one wrote
			// finds its own bytes at the cut and copies them rather than opening a
			// link inside a link. Nothing on this surface feeds a painted row back
			// in today — [app.deckRows] always starts from what prose returned —
			// and a pass that only held together because nobody did would be a
			// trap for whoever eventually does.
			if want := inked + restore; strings.HasPrefix(text[i:], want) {
				out.WriteString(want)
				i += len(want)
				at = ref.to
				continue
			}
			out.WriteString(inked)
			out.WriteString(restore)
			// Step the walk over the span's bytes, keeping the escape state up to
			// date: what was opened inside a link still has to be closed outside
			// it, and a run of prose beginning mid-reference is the row's own.
			for i < len(text) && at < ref.to {
				if n := escLen(text, i); n > 0 {
					fg, under = sgrInk(text[i:i+n], fg, under)
					i += n
					continue
				}
				at++
				i++
			}
			continue
		}
		if n := escLen(text, i); n > 0 {
			fg, under = sgrInk(text[i:i+n], fg, under)
			out.WriteString(text[i : i+n])
			i += n
			continue
		}
		out.WriteByte(text[i])
		at++
		i++
	}
	return out.String(), links
}

// taskLinkInk is what a resolved reference wears: the accent, underlined.
//
// UNDERLINE IS THE WORD THIS TREE ALREADY USES FOR "A PLACE YOU COULD GO" —
// styles.go spends it on a path inside a highlighted command, and prose spends
// it on a markdown link's label (prose/inline.go) — so a task reference wearing
// it needs no explaining to anybody who has read one screen of this surface.
//
// THE ACCENT IS A BORROWED HUE AND IT IS WORTH SAYING SO. render.go's identity
// law reserves it for the person's own words, on the grounds that hue is the one
// channel that cannot be impersonated by markdown. A link is not the model's
// voice — it is a control this surface put into the model's sentence — and the
// underline is what says which of the two a run of accent is. The distinction
// holds because a person's message is accent WHOLE and behind its own glyph: a
// nine-cell underlined run mid-paragraph reads as a button, not as a turn.
func taskLinkInk(pal palette, s string) string { return pal.underline(pal.accent(s)) }

// taskLinkHotInk is the same reference with the pointer on it: the underline
// stays and the hue takes one step up, accent to ink.
//
// IT IS A BRIGHTENING AND NOT A BACKGROUND BAND, which is the model segment's
// own decision for the model segment's own reason (topbar.go's
// [app.topBarPaint]): a reference is two or three words inside somebody's
// sentence, not a row of a list, and a highlighted rectangle in the middle of a
// paragraph would be the one boxed thing on a surface with no boxes. It is also
// the only step that survives every terminal this surface draws on — a background
// is dropped below the 256-colour rung, and the hue is not.
//
// The reference is repainted rather than wrapped, because these hues are raw SGR
// with an explicit reset and a colour inside a colour ends at the inner one's
// reset ([paintLinks] restores what was around it either way).
func taskLinkHotInk(pal palette, s string) string { return pal.underline(pal.ink(s)) }

// escLen is the length of the escape sequence at s[i], or zero where there is
// none. Two forms are recognized, because two forms are written:
//
//	CSI   ESC [ … final          every colour this surface paints
//	OSC   ESC ] … BEL | ESC \    the hyperlink around a path (pathlink.go)
//
// AN OSC HAS TO BE SWALLOWED WHOLE OR IT IS NOT SWALLOWED AT ALL. Its payload
// is ordinary printable bytes — `8;;file:///Users/x/main.go` — so a walk that
// stopped after `ESC ]` would count twenty-six cells of URI as text, and every
// caller of [flatten] indexes a row by those counts. Task references would land
// on the wrong columns and [paintLinks] would splice an SGR into the middle of a
// URI. Anything else still returns the two bytes it opens with rather than
// swallowing the row.
func escLen(s string, i int) int {
	if s[i] != 0x1b || i+1 >= len(s) {
		return 0
	}
	switch s[i+1] {
	case '[':
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j - i + 1
			}
		}
	case ']':
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j - i + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j - i + 2
			}
		}
	default:
		return 2
	}
	return len(s) - i
}

// sgrGround folds one escape sequence into "is there a background under this".
func sgrGround(seq string, on bool) bool {
	params, ok := sgrParams(seq)
	if !ok {
		return on
	}
	for i := 0; i < len(params); i++ {
		switch param := params[i]; {
		case param == "", param == "0", param == "49":
			on = false
		case param == "48":
			on = true
			i += sgrColorSpan(params, i)
		case param == "38":
			i += sgrColorSpan(params, i)
		case len(param) == 2 && param[0] == '4' && param[1] >= '0' && param[1] <= '7':
			on = true
		case len(param) == 3 && param[0] == '1' && param[1] == '0' && param[2] <= '7':
			on = true
		}
	}
	return on
}

// sgrInk folds one escape sequence into the two things a link has to put back:
// the foreground in force, and whether an underline was already open.
func sgrInk(seq, fg string, under bool) (string, bool) {
	params, ok := sgrParams(seq)
	if !ok {
		return fg, under
	}
	for i := 0; i < len(params); i++ {
		switch param := params[i]; {
		case param == "", param == "0":
			fg, under = "", false
		case param == "39":
			fg = ""
		case param == "38":
			fg = seq
			i += sgrColorSpan(params, i)
		case param == "48":
			// A background's own index is not a foreground, which is the whole
			// reason the extended forms are stepped over rather than read one
			// parameter at a time: "48;5;38" says nothing about the ink.
			i += sgrColorSpan(params, i)
		case param == "4":
			under = true
		case param == "24":
			under = false
		case len(param) == 2 && param[0] == '3' && param[1] >= '0' && param[1] <= '7',
			len(param) == 2 && param[0] == '9' && param[1] >= '0' && param[1] <= '7':
			fg = seq
		}
	}
	return fg, under
}

// sgrColorSpan is how many parameters an extended colour introducer takes with
// it: two for the 256 form, four for the truecolor one, none for anything else.
func sgrColorSpan(params []string, at int) int {
	if at+1 >= len(params) {
		return 0
	}
	switch params[at+1] {
	case "5":
		return 2
	case "2":
		return 4
	}
	return 0
}

// sgrParams splits one SGR sequence into its parameters, and reports false for a
// CSI that is not one — a sequence ending in anything but "m" says nothing about
// colour. A bare "\x1b[m" is the reset, which is one empty parameter.
func sgrParams(seq string) ([]string, bool) {
	if len(seq) < 3 || seq[1] != '[' || seq[len(seq)-1] != 'm' {
		return nil, false
	}
	return strings.Split(seq[2:len(seq)-1], ";"), true
}
