package blocks

import (
	"strings"
	"testing"
)

func TestHeaderGrammar(t *testing.T) {
	h := Header{
		Glyph: "◐",
		Title: "swe",
		Desc:  "rewriting the executor harness",
		Badges: []Badge{
			{Text: "?2", Hue: HueAttention},
		},
		Meta: []string{"K3 ▄ $8.65", "4m"},
	}
	got := h.Render(80, Plain)
	want := "◐ swe: rewriting the executor harness [?2] · K3 ▄ $8.65 · 4m"
	if got != want {
		t.Fatalf("header grammar broke:\n got %q\nwant %q", got, want)
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

func TestExpandHintAndCountBadge(t *testing.T) {
	if got := ExpandHint(false, 12); got != "▸ 12 lines" {
		t.Fatalf("collapsed hint %q", got)
	}
	if got := ExpandHint(false, 1); got != "▸ 1 line" {
		t.Fatalf("singular hint %q", got)
	}
	if got := ExpandHint(true, 12); got != "▾" {
		t.Fatalf("expanded hint %q", got)
	}
	if got := CountBadge("?", 2, HueAttention); got.Text != "?2" || got.Hue != HueAttention {
		t.Fatalf("count badge %+v", got)
	}
	if got := CountBadge("?", 0, HueAttention); got.Text != "" {
		t.Fatal("a zero count produced a badge")
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
