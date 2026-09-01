package blocks

import (
	"strings"
	"testing"
)

func TestHeaderGrammar(t *testing.T) {
	h := Header{
		Glyph: "◐",
		Title: "fix",
		Desc:  "rewriting the executor harness",
		Badges: []Badge{
			{Text: "?2", Hue: HueAttention},
		},
		Meta: []string{"K3 ▄ $8.65", "4m"},
	}
	got := h.Render(80, Plain)
	want := "◐ fix: rewriting the executor harness [?2] · K3 ▄ $8.65 · 4m"
	if got != want {
		t.Fatalf("header grammar broke:\n got %q\nwant %q", got, want)
	}
}

// seedStyler is a Styler with an identity door, recording what it was asked
// for so the test can tell WHICH door the header went through.
type seedStyler struct {
	seeds  []uint64
	hues   []Hue
	states []State
}

func (s *seedStyler) Paint(text string, state State, hue Hue) string {
	s.hues = append(s.hues, hue)
	s.states = append(s.states, state)
	return text
}

func (s *seedStyler) PaintIdentity(text string, seed uint64, state State) string {
	s.seeds = append(s.seeds, seed)
	s.states = append(s.states, state)
	return text
}

// The identity glyph goes THROUGH the one header grammar (8.1.5). Before
// [Header.GlyphSeed] existed a renderer holding a task id had to pre-paint the
// glyph and hand the grammar a finished string, because HueIdentity without a
// seed can only resolve to the wheel's first entry — an ad-hoc header by
// another name.
func TestHeaderGlyphResolvesItsIdentitySeed(t *testing.T) {
	sty := &seedStyler{}
	h := Header{Glyph: "◐", GlyphHue: HueIdentity, GlyphSeed: Seed("wisp-parity"), Title: "wisp"}
	if got := h.Render(40, sty); got != "◐ wisp" {
		t.Fatalf("the seed changed the printable row: %q", got)
	}
	if len(sty.seeds) != 1 || sty.seeds[0] != Seed("wisp-parity") {
		t.Fatalf("the glyph did not go through the identity door: seeds %v", sty.seeds)
	}
	// The seed reaches the wheel; the title beside it does not. Identity is
	// the glyph's, never the prose's (5.16: accent hues never colorize
	// running text).
	for _, hue := range sty.hues {
		if hue == HueIdentity {
			t.Fatal("a non-glyph cell was painted as an identity")
		}
	}
}

// The new field is inert at its zero value: same door, same bytes, same
// arguments as before it existed.
func TestHeaderWithoutASeedIsUnchanged(t *testing.T) {
	seeded := &seedStyler{}
	Header{Glyph: "◐", GlyphHue: HueIdentity, Title: "wisp"}.Render(40, seeded)
	if len(seeded.seeds) != 0 {
		t.Fatalf("a zero seed reached the identity wheel: %v", seeded.seeds)
	}
	if len(seeded.hues) == 0 || seeded.hues[0] != HueIdentity {
		t.Fatalf("a zero seed stopped painting the glyph on the hue axis: %v", seeded.hues)
	}
	// And a Styler with no identity door keeps working when a seed IS set:
	// the seam is optional on the token layer's side.
	h := Header{Glyph: "◐", GlyphHue: HueIdentity, GlyphSeed: Seed("wisp"), Title: "wisp"}
	if got := h.Render(40, Plain); got != "◐ wisp" {
		t.Fatalf("a plain Styler could not render a seeded header: %q", got)
	}
}

func TestSeedIsStableAndEmptyMeansNoIdentity(t *testing.T) {
	if Seed("") != 0 {
		t.Fatal("an empty task id produced an identity")
	}
	if Seed("aforge") != Seed("aforge") {
		t.Fatal("the seed is not stable for one id")
	}
	if Seed("aforge") == Seed("aforge2") {
		t.Fatal("two task ids share one seed")
	}
}

func TestHeaderFlattensNewlines(t *testing.T) {
	h := Header{Title: "a\ntitle", Desc: "with\ta\r\ntab", Meta: []string{"one\ntwo"}}
	got := h.Render(80, Plain)
	if strings.ContainsAny(got, "\n\r\t") {
		t.Fatalf("header smuggled a row break: %q", got)
	}
	if got != "a title: with a tab · one two" {
		t.Fatalf("flattened header %q", got)
	}
}

// The degradation order is fixed: meta from the right, then the hint, then
// non-sticky badges, then the description, then the title.
func TestHeaderDegradesInOrder(t *testing.T) {
	h := Header{
		Glyph:  "✓",
		Title:  "compile",
		Desc:   "turning the plan into a graph",
		Badges: []Badge{{Text: "boost"}},
		Meta:   []string{"12s", "$0.04", "K3"},
		Hint:   "▸ 12 lines",
	}
	prev := ""
	for width := 90; width >= 1; width-- {
		got := h.Render(width, Plain)
		if w := Width(got); w > width {
			t.Fatalf("width %d: header is %d cells (%q)", width, w, got)
		}
		if strings.Contains(got, "\n") {
			t.Fatalf("width %d: header became two rows", width)
		}
		prev = got
	}
	if prev == "" {
		t.Fatal("a one-cell header rendered nothing at all")
	}

	// Meta goes before the description.
	tight := h.Render(46, Plain)
	if strings.Contains(tight, "K3") {
		t.Fatalf("meta survived past the description: %q", tight)
	}
	if !strings.Contains(tight, "compile") {
		t.Fatalf("the title was dropped before the meta: %q", tight)
	}
}

// The truncation law's badge is sticky: a cut turn still says it was cut on a
// narrow terminal, after everything else has been given up.
func TestTruncationBadgeIsSticky(t *testing.T) {
	h := Header{
		Glyph:  "✓",
		Title:  "aforge",
		Desc:   "a long description that will certainly not fit",
		Badges: []Badge{{Text: "droppable"}},
		Meta:   []string{"12s"},
		End:    EndTruncatedByCap,
	}
	for _, width := range []int{80, 60, 40, 30} {
		got := h.Render(width, Plain)
		if !strings.Contains(got, "cut off") {
			t.Fatalf("width %d lost the truncation badge: %q", width, got)
		}
	}
	if got := h.Render(40, Plain); strings.Contains(got, "droppable") {
		t.Fatalf("an ordinary badge outlived the width budget: %q", got)
	}
}

// The separator is measured in CELLS, not bytes: a header that exactly fits
// must still render whole.
func TestHeaderMeasuresSeparatorsInCells(t *testing.T) {
	h := Header{Glyph: "◐", Title: "aforge", Desc: "a·b", Meta: []string{"·1", "·2"}}
	full := h.Render(120, Plain)
	exact := Width(full)
	if got := h.Render(exact, Plain); got != full {
		t.Fatalf("a header that fits in %d cells was degraded:\n got %q\nwant %q", exact, got, full)
	}
	if got := h.Render(exact-1, Plain); got == full {
		t.Fatalf("a header one cell too wide was not degraded: %q", got)
	}
}

func TestHeaderAtWidthOne(t *testing.T) {
	h := Header{Glyph: "◐", Title: "aforge", Desc: "x", Meta: []string{"1s"}}
	got := h.Render(1, Plain)
	if Width(got) > 1 {
		t.Fatalf("width-1 header is %d cells: %q", Width(got), got)
	}
	if h.Render(0, Plain) != "" || h.Render(-3, Plain) != "" {
		t.Fatal("a non-positive width rendered something")
	}
}

func TestDiscloseAndCountBadge(t *testing.T) {
	if got := Disclose(false, 12, "line", "lines"); got != "▸ 12 lines" {
		t.Fatalf("collapsed hint %q", got)
	}
	if got := Disclose(false, 1, "line", "lines"); got != "▸ 1 line" {
		t.Fatalf("singular hint %q", got)
	}
	if got := Disclose(true, 12, "line", "lines"); got != "▾" {
		t.Fatalf("expanded hint %q", got)
	}
	if got := CountBadge("?", 2, HueAttention); got.Text != "?2" || got.Hue != HueAttention {
		t.Fatalf("count badge %+v", got)
	}
	if got := CountBadge("?", 0, HueAttention); got.Text != "" {
		t.Fatal("a zero count produced a badge")
	}
}

// THE UNIT IS THE CALLER'S, because the unit is a fact about what is folded and
// not about folding: a transcript hides LINES, a subtree hides PARTS, a batch
// hides CALLS, and §14's vocabulary is a product law rather than this package's
// guess. Both spellings are taken so a singular is never assembled by trimming
// an `s` off a word that might not have one.
func TestDiscloseSpellsTheCallersOwnUnit(t *testing.T) {
	for _, want := range []struct {
		n           int
		unit, units string
		shut        string
	}{
		{3, "part", "parts", "▸ 3 parts"},
		{1, "part", "parts", "▸ 1 part"},
		{2, "call", "calls", "▸ 2 calls"},
		{1, "call", "calls", "▸ 1 call"},
	} {
		if got := Disclose(false, want.n, want.unit, want.units); got != want.shut {
			t.Errorf("Disclose(false, %d, %q, %q) = %q, want %q",
				want.n, want.unit, want.units, got, want.shut)
		}
	}
	// NO VERB, EVER — §15's delete test applied to a door. `more`, `expand` and
	// `view` are all things the chevron already says.
	for _, n := range []int{0, 1, 12} {
		for _, open := range []bool{false, true} {
			door := Disclose(open, n, "line", "lines")
			for _, verb := range []string{"view", "more", "expand", "show", "open", "⏎"} {
				if strings.Contains(door, verb) {
					t.Errorf("the door %q carries the verb %q", door, verb)
				}
			}
		}
	}
	// An OPEN door counts nothing: the rows are on screen, and a count beside
	// them would be the surface narrating what the reader is looking at.
	if got := Disclose(true, 99, "part", "parts"); got != ExpandedMark {
		t.Errorf("an open door says %q, want the bare witness %q", got, ExpandedMark)
	}
	// A shut door with nothing behind it is the bare mark too — never `▸ 0
	// lines`, which is an affordance onto an empty room (5.20 rule 3).
	if got := Disclose(false, 0, "line", "lines"); got != CollapsedMark {
		t.Errorf("an empty door says %q, want %q", got, CollapsedMark)
	}
}

// A SECTION'S DOOR KEEPS ITS COUNT WHEN IT OPENS, and that is the one way it
// differs from [Disclose]. A tail's count is a fact about the fold — how much is
// hidden — so it goes when the fold does. A section's count is a fact about the
// BAND: `history (18)` is as true with the rows showing as without them.
func TestDiscloseSectionNamesTheBandAndCountsIt(t *testing.T) {
	if got := DiscloseSection(false, "history", 18); got != "▸ history (18)" {
		t.Errorf("a shut section reads %q", got)
	}
	if got := DiscloseSection(true, "history", 18); got != "▾ history (18)" {
		t.Errorf("an open section reads %q", got)
	}
	// A band nobody counted is a band with a name and no parenthetical, never
	// `history (0)`.
	if got := DiscloseSection(false, "history", 0); got != "▸ history" {
		t.Errorf("an uncounted section reads %q", got)
	}
	if got := DiscloseSection(true, "", 4); got != ExpandedMark {
		t.Errorf("a nameless section reads %q, want the bare witness", got)
	}
}

// The cut rule is the truncation law's second half: a reader who scrolled past
// the header still sees that the text stops short.
func TestCutRuleMarksTheEnding(t *testing.T) {
	cases := map[EndState]string{
		EndCompleted:      "",
		EndLive:           "",
		EndTruncatedByCap: "output cap",
		EndStreamDropped:  "stream dropped",
		EndInterrupted:    "stopped by you",
	}
	for end, want := range cases {
		got := CutRule(end, 60, Plain)
		if want == "" {
			if got != "" {
				t.Fatalf("%v drew a cut rule: %q", end, got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Fatalf("%v rule %q does not say %q", end, got, want)
		}
		if Width(got) != 60 {
			t.Fatalf("%v rule is %d cells, want 60", end, Width(got))
		}
	}
	for width := 1; width <= 40; width++ {
		if w := Width(CutRule(EndInterrupted, width, Plain)); w > width {
			t.Fatalf("width %d: cut rule is %d cells", width, w)
		}
	}
}

func TestEndStateVocabulary(t *testing.T) {
	if EndCompleted.Cut() || EndLive.Cut() {
		t.Fatal("a clean ending claimed to be cut")
	}
	for _, e := range []EndState{EndTruncatedByCap, EndStreamDropped, EndInterrupted} {
		if !e.Cut() {
			t.Fatalf("%v does not report itself as cut", e)
		}
		if e.String() == "unknown" {
			t.Fatalf("%d has no name", e)
		}
	}
	if EndTruncatedByCap.Hue() != HueBroken || EndInterrupted.Hue() != HueNone {
		t.Fatal("cut hues are wrong: a deliberate stop is not a failure")
	}
}

func TestTruncatePathKeepsTheFilename(t *testing.T) {
	got := TruncatePath("internal/tui2/blocks/transcript.go", 20)
	if !strings.HasSuffix(got, "transcript.go") {
		t.Fatalf("path cut lost the filename: %q", got)
	}
	if Width(got) > 20 {
		t.Fatalf("path %q is %d cells", got, Width(got))
	}
	if got := TruncatePath("short.go", 40); got != "short.go" {
		t.Fatalf("a fitting path was cut: %q", got)
	}
}

func TestPadIsWidthStable(t *testing.T) {
	for _, s := range []string{"", "1s", "12m", "1h02", "1h02m33s"} {
		if got := Width(Pad(s, 6)); got != 6 {
			t.Fatalf("Pad(%q) is %d cells", s, got)
		}
		if got := Width(PadLeft(s, 6)); got != 6 {
			t.Fatalf("PadLeft(%q) is %d cells", s, got)
		}
	}
}

// -- the receipt column --------------------------------------------------------

// §16's FIRST rule: "the right edge is a column". Two rows with the same
// receipt put it at the same x whatever their titles do, and a title long
// enough to reach the edge is CUT rather than allowed to push the receipt off —
// which is the inversion the field exists for, since every other cell on this
// row sheds from the right.
//
// The measured failure: at 88 columns a batch row whose named inputs ran to the
// edge lost its size entirely while the shorter row under it kept one, so a
// reader scanning the column read a ragged list and could not tell an absent
// size from a dropped one.
// The width is one where BOTH rows' receipts are still close to their subjects
// (§20's condition on the right column, pinned by
// [TestAFarReceiptComesHomeToItsSubject]). At 88 the short row's column would be
// fifty-odd cells adrift, which is the gulf §20 forbids and not the column this
// test is about.
func TestHeaderReceiptHoldsTheRightEdgeAcrossRows(t *testing.T) {
	const (
		width  = 44
		column = "1KB"
	)
	rows := []Header{
		{Glyph: "$", Title: "ls -la clips/", Receipt: "1KB", Hint: Disclose(false, 3, "line", "lines")},
		{Glyph: "⌕", Title: "searched 9 · rust async trait · tokio spawn cost · " +
			"pin project macro +6 · async drop rfc · one more query still",
			Receipt: "1KB", Hint: Disclose(false, 3, "line", "lines")},
	}
	for i, h := range rows {
		got := h.Render(width, Plain)
		if w := stringWidth(got); w > width {
			t.Fatalf("row %d is %d cells at width %d: %q", i, w, width, got)
		}
		if !strings.HasSuffix(got, column) {
			t.Fatalf("row %d did not end on the receipt column: %q", i, got)
		}
		if w := stringWidth(got); w != width {
			t.Fatalf("row %d does not reach the right edge: %d of %d cells: %q",
				i, w, width, got)
		}
	}
	// The long title is the one that pays. It is cut; the column is not.
	if long := rows[1].Render(width, Plain); !strings.Contains(long, OverflowMark) {
		t.Fatalf("the long title kept its whole self beside the receipt: %q", long)
	}
	// THE DOOR IS NOT IN THE COLUMN. It sits inline, right after the words it
	// opens, because a receipt is a figure a reader SCANS and a door is
	// something they AIM AT.
	short := rows[0].Render(width, Plain)
	if !strings.Contains(short, "ls -la clips/"+hintLead+CollapsedMark+" 3 lines") {
		t.Fatalf("the fold hint left the flow: %q", short)
	}
}

// A RECEIPT IS DROPPED WHOLE, never cut — [CardBlock.titleLine]'s rule, in the
// grammar this time. "$0.1" is not a smaller truth than "$0.14"; it is a
// different and false one. When the column goes, the fold hint comes back into
// the flow, so the door outlives the telemetry.
func TestHeaderDropsATightReceiptWholeAndKeepsTheDoor(t *testing.T) {
	h := Header{Glyph: "$", Title: "ls -la clips/", Receipt: "1.6KB", Hint: Disclose(false, 3, "line", "lines")}
	for _, width := range []int{1, 6, 12, minHeadRoom + stringWidth("1.6KB") + receiptGap - 1} {
		got := h.Render(width, Plain)
		if stringWidth(got) > width {
			t.Fatalf("width %d drew %d cells: %q", width, stringWidth(got), got)
		}
		for _, fragment := range []string{"1.6KB", "1.6K", "1.6", "1."} {
			if strings.Contains(got, fragment) {
				t.Fatalf("width %d drew a piece of the receipt (%q): %q", width, fragment, got)
			}
		}
	}
	// One cell over the floor it arrives whole, and never half.
	wide := minHeadRoom + stringWidth("1.6KB") + receiptGap
	if got := h.Render(wide, Plain); !strings.HasSuffix(got, "1.6KB") {
		t.Fatalf("the receipt did not arrive whole at its floor width: %q", got)
	}
}

// The zero value changes NOTHING. A header with no receipt takes the path it
// took before the field existed — same degrade order, same hint in the same
// place in the flow, same bytes — which is what keeps every other consumer's
// goldens still.
func TestHeaderWithoutAReceiptIsUnchanged(t *testing.T) {
	h := Header{
		Glyph: "◐", Title: "fix", Desc: "rewriting the executor harness",
		Badges: []Badge{{Text: "?2", Hue: HueAttention}},
		Meta:   []string{"K3 ▄ $8.65", "4m"},
		Hint:   Disclose(false, 12, "line", "lines"),
	}
	want := "◐ fix: rewriting the executor harness [?2] · K3 ▄ $8.65 · 4m  ▸ 12 lines"
	if got := h.Render(80, Plain); got != want {
		t.Fatalf("the flow moved without a receipt:\n got %q\nwant %q", got, want)
	}
	// And the empty string is the same as the field not being written at all,
	// at every width the row can be read at.
	blank := h
	blank.Receipt = ""
	for width := 1; width <= 100; width++ {
		if got, same := h.Render(width, Plain), blank.Render(width, Plain); got != same {
			t.Fatalf("width %d: %q vs %q", width, got, same)
		}
	}
}

// The receipt is CHROME and the hint rides with it: one dim span at the right
// edge, never a cell that outranks the title beside it (§16's dim ramp).
func TestHeaderReceiptIsPaintedAsChrome(t *testing.T) {
	sty := &seedStyler{}
	Header{Glyph: "$", Title: "ls", Receipt: "1KB", Hint: Disclose(false, 3, "line", "lines")}.Render(40, sty)
	for i, state := range sty.states {
		if state == StateLive {
			t.Fatalf("cell %d of a settled row was painted live", i)
		}
	}
	if len(sty.states) == 0 {
		t.Fatal("the row painted nothing")
	}
	if last := sty.states[len(sty.states)-1]; last != StateChrome {
		t.Fatalf("the receipt column was painted %v, want chrome", last)
	}
}

// The receipt is reserved FIRST and everything else narrows into what is left,
// which is the opposite of the header's own meta-first order and deliberately
// so: a column that moves is not a column. What pays is the flow — meta, then
// the door, then the title's own length — and the estimate stays put until it
// cannot fit at all, and then it goes whole.
func TestTheReceiptOutlastsTheFlowAndThenGoesWhole(t *testing.T) {
	h := Header{
		Glyph: "$", Title: "ls -la clips/", Receipt: "1.6KB",
		Meta: []string{"one"}, Hint: Disclose(false, 3, "line", "lines"),
	}
	kept, dropped := 0, 0
	for width := 1; width <= 90; width++ {
		got := h.Render(width, Plain)
		if stringWidth(got) > width {
			t.Fatalf("width %d drew %d cells: %q", width, stringWidth(got), got)
		}
		switch {
		// The inline form §20 falls back to once the column is too far to be a
		// column. It is the whole figure, attached to its subject.
		case strings.HasSuffix(got, receiptOpen+"1.6KB"+receiptClose):
			kept++
		case strings.HasSuffix(got, "1.6KB"):
			kept++
		case strings.Contains(got, "1.6K") || strings.Contains(got, "6KB"):
			t.Fatalf("width %d drew half a receipt: %q", width, got)
		default:
			dropped++
		}
	}
	if kept == 0 || dropped == 0 {
		t.Fatalf("the column never passed through both states: kept=%d dropped=%d", kept, dropped)
	}
	// And the meta cell goes before the receipt does, which is the inversion
	// stated: at the width where meta no longer fits, the estimate is still on
	// the edge.
	tight := minHeadRoom + stringWidth("1.6KB") + receiptGap
	if got := h.Render(tight, Plain); !strings.HasSuffix(got, "1.6KB") || strings.Contains(got, "one") {
		t.Fatalf("the meta cell outlasted the receipt column: %q", got)
	}
}

// §20's condition on the right column, which is the whole reason
// [receiptGulfMax] exists: "a shared right column — ONLY inside dense
// same-shaped lists in a narrow pane (rail, palette), where every row has one
// and THE COLUMN IS CLOSE. A receipt separated from its subject by a gulf of
// empty cells is the defect the user has now flagged twice."
//
// So the same header, at a width where the column would be far, brings the
// figure home in parentheses instead — placement 1 of the three §20 allows,
// never a fourth, and never a dropped number.
func TestAFarReceiptComesHomeToItsSubject(t *testing.T) {
	h := Header{Glyph: "$", Title: "ls", Receipt: "~1.5k tok"}

	// Narrow: the edge is a few cells past the words, so it is still a column.
	near := h.Render(30, Plain)
	if !strings.HasSuffix(near, "~1.5k tok") || strings.Contains(near, receiptOpen) {
		t.Fatalf("a close column stopped being one: %q", near)
	}
	if w := stringWidth(near); w != 30 {
		t.Fatalf("the close form does not reach the right edge: %d of 30: %q", w, near)
	}

	// Wide: the same row would fling the figure a hundred cells from the four
	// bytes it describes. It rides with them instead.
	far := h.Render(140, Plain)
	if !strings.HasSuffix(far, receiptOpen+"~1.5k tok"+receiptClose) {
		t.Fatalf("a far receipt kept its gulf: %q", far)
	}
	if gulf := stringWidth(far) - stringWidth("$ ls"); gulf > receiptGulfMax+receiptInlineCost {
		t.Fatalf("the inline receipt is still %d cells from its subject: %q", gulf, far)
	}

	// The crossing is monotone and the figure is never lost or halved on the
	// way: every width from useless to generous draws the whole receipt or none.
	for width := 1; width <= 200; width++ {
		got := h.Render(width, Plain)
		if stringWidth(got) > width {
			t.Fatalf("width %d drew %d cells: %q", width, stringWidth(got), got)
		}
		if strings.Contains(got, "tok") && !strings.Contains(got, "~1.5k tok") {
			t.Fatalf("width %d drew part of a receipt: %q", width, got)
		}
	}
}
