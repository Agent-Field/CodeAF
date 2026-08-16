package tui3

import (
	"os"
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
// CODE FENCES STAY ON THE TOKENS RAMP, and this is the whole of D11's "else"
// branch. Decision 11 asks for a pastel chroma style (catppuccin-mocha) on
// fenced code IF it can be had without editing internal/tui2 — and it cannot:
// prose.Options is Width, Measure and *tokens.Styler, with no theme parameter
// and no seam for one. Behind it, prose/code.go BUILDS its chroma style out of
// the tokens ramp and maps every chroma token onto a tokens.CodeSlot, so there
// is no style to swap from this side even in principle — the theme is the token
// layer. Adopting mocha here would mean adding an option to prose, which is the
// one edit this slice is not allowed to make and would in any case hand the
// surface a second colour authority — exactly what prose's own package comment
// rejects glamour for.
//
// The cost is small and the floor is already right: the tokens ramp is muted by
// construction, so a fence renders quiet next to styles.go's pastels rather
// than clashing with them. If the theme is wanted later it is one option on
// prose.Options and one line here, and it belongs to whoever owns tui2.

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
//
// The profile comes from [tokens.DetectProfile], the same door cmd/aforge opens
// for the v2 surface, so both surfaces answer "what can this terminal say" from
// one decision table rather than from two guesses. The glyph tier stays
// [tokens.Plain]: that axis is opt-out-able elsewhere in the tree by a flag this
// surface does not have yet, and the plain tier is a designed floor, not a
// degradation. Focus is normal — this surface has one pane, so there is nothing
// for a dimmed one to recede behind.
func markdownStyler() *tokens.Styler {
	stylerOnce.Do(func() {
		styler = tokens.NewStyler(tokens.DetectProfile(os.Getenv), tokens.FocusNormal)
	})
	return styler
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

// renderMarkdownWith is [renderMarkdown] against a stated Styler. It is the
// whole body, split off because the profile is detected from the environment
// exactly once per process: a test that wants to see what a truecolor terminal
// gets cannot ask for one afterwards, and a test that raced the detection to
// set NO_COLOR would be a test whose result depended on which test ran first.
func renderMarkdownWith(st *tokens.Styler, text string, width int) []string {
	if width < 1 {
		width = 1
	}
	if layoutTier(width) == tierPhone {
		return phoneMarkdown(st, text, width)
	}
	return proseRows(st, text, width)
}

// proseRows is the unconditional path: prose renders the whole document, at the
// package's one measure. Every tier above the phone reaches it and nothing else.
func proseRows(st *tokens.Styler, text string, width int) []string {
	return prose.Render(text, prose.Options{
		Width: width,
		// The hard ceiling is the pane; the reading length is prose's own
		// constant, clamped to the ceiling by prose. A 200-column window is a
		// wide window, not a wide sentence — and naming the constant rather
		// than a number of our own keeps one definition of a measure in the
		// tree.
		Measure: prose.DefaultMeasure,
		Styler:  st,
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
func phoneMarkdown(st *tokens.Styler, text string, width int) []string {
	var out []string
	for _, seg := range mdSegments(text) {
		var rows []string
		switch seg.kind {
		case mdSegFence:
			rows = phoneCodeRows(st, seg, width)
		case mdSegTable:
			rows = phoneTableRows(st, seg, width)
		default:
			rows = proseRows(st, seg.text, width)
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
			out = append(out, mdSegment{kind: mdSegTable, table: table})
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
func phoneTableRows(st *tokens.Styler, seg mdSegment, width int) []string {
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
			rows = append(rows, proseRows(st, mdKeyed(key, cell), width)...)
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
