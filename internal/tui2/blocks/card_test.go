package blocks

import (
	"strings"
	"testing"
	"time"
)

// sampleCard is the card the anatomy tests read: every optional part present,
// so a row that goes missing is a failure and not a shrug.
func sampleCard() *CardBlock {
	c := NewCard("delivery-1", "rivers haiku")
	c.Glyph = "✓"
	c.Tone = ToneDelivered
	c.Receipt = "4 workers · 2m · $0.14"
	c.Body = []string{"three haiku in 5-7-5, one blank line between them"}
	c.ArtifactGlyph = "▸"
	c.Artifacts = []string{"/home/state/workspace/task-8/rivers.txt"}
	c.Verbs = []string{"ctrl+r open", "r rerun"}
	return c
}

// The anatomy, read off one render: a blank, the title line, the body, the
// artifact, the verbs, a blank — every content row behind the accent edge.
func TestCardAnatomy(t *testing.T) {
	rows := sampleCard().Rows(60)
	if len(rows) != 6 {
		t.Fatalf("a full card drew %d rows:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if rows[0] != "" || rows[len(rows)-1] != "" {
		t.Fatalf("a card must open and close on a blank row:\n%q\n%q", rows[0], rows[len(rows)-1])
	}
	for i, row := range rows[1 : len(rows)-1] {
		if !strings.HasPrefix(row, AccentEdge+" ") {
			t.Fatalf("content row %d does not carry the accent edge: %q", i, row)
		}
	}
	want := []string{"✓ rivers haiku", "4 workers · 2m · $0.14", "three haiku", "rivers.txt", "ctrl+r open · r rerun"}
	joined := strings.Join(rows, "\n")
	for _, part := range want {
		if !strings.Contains(joined, part) {
			t.Fatalf("the card lost %q:\n%s", part, joined)
		}
	}
}

// The card's own left edges: the glyph hangs in the card's gutter and the body
// starts where the title's words do. This is 5.13's rhythm repeated inside the
// card, and it is the shape defect #9 was about.
func TestCardBodyLinesUpUnderTheTitle(t *testing.T) {
	rows := sampleCard().Rows(60)
	title, body := rows[1], rows[2]
	titleAt := columnOf(t, title, "rivers haiku")
	bodyAt := columnOf(t, body, "three")
	if titleAt != bodyAt {
		t.Fatalf("the title starts at column %d and the body at %d — two left edges in one card",
			titleAt, bodyAt)
	}
	if want := edgeWidth + BodyIndent; bodyAt != want {
		t.Fatalf("the card's words start at column %d, want %d (edge + %d)", bodyAt, want, BodyIndent)
	}
	if glyph := columnOf(t, title, "✓"); glyph != edgeWidth {
		t.Fatalf("the glyph sits at column %d, want the card's gutter at %d", glyph, edgeWidth)
	}
}

// The receipt is right-aligned against the card's inner edge, not floated after
// the title — FOR AS LONG AS THAT EDGE IS STILL NEAR THE TITLE.
//
// §20 permits the shared right column as one of three placements and permits it
// conditionally: "where every row has one AND THE COLUMN IS CLOSE". A card whose
// title is three words wide on a 140-cell terminal was throwing its money most
// of a screen away from the thing it is the money FOR, which §20 names as the
// defect the user has flagged twice. Past [receiptGulfMax] cells the figure
// comes home in parentheses; inside it the column is still a column.
//
// Either way the receipt is WHOLE and the reader has the number.
func TestCardReceiptIsRightAlignedWhileTheColumnIsClose(t *testing.T) {
	const receipt = "4 workers · 2m · $0.14"
	for _, width := range []int{48, 60, 100, 140} {
		row := sampleCard().Rows(width)[1]
		if !strings.Contains(row, receipt) {
			t.Fatalf("width %d: the card lost its receipt: %q", width, row)
		}
		// The inline form: attached to the title, no gulf at all. An ungrounded
		// card does not pad its rows out, so this row is legitimately short.
		if strings.Contains(row, receiptOpen+receipt+receiptClose) {
			continue
		}
		// The column form reaches the card's own right edge, and the edge is
		// close enough to the title to still BE a column.
		if w := Width(row); w != width {
			t.Fatalf("width %d: the title line is %d cells, want the full row", width, w)
		}
		if !strings.HasSuffix(row, "$0.14") {
			t.Fatalf("width %d: the receipt is neither flush right nor inline: %q", width, row)
		}
		gulf := Width(row) - edgeWidth - Width(strings.TrimRight(row[:strings.Index(row, receipt)], " "))
		if gulf-Width(receipt) > receiptGulfMax {
			t.Fatalf("width %d: the column sits %d cells from its subject, past the %d cap: %q",
				width, gulf-Width(receipt), receiptGulfMax, row)
		}
	}
}

// Half a receipt is a lie about a number, so a row with no room for the whole
// of it draws none of it — and never a mangled one.
func TestCardDropsAReceiptItCannotDrawWhole(t *testing.T) {
	const receipt = "4 workers · 2m · $0.14"
	dropped, seen := false, false
	for width := 20; width <= 140; width++ {
		row := sampleCard().Rows(width)[1]
		switch {
		case strings.Contains(row, receipt):
			seen = true
		case strings.Contains(row, "$0.1") || strings.Contains(row, "workers"):
			t.Fatalf("width %d: a partial receipt: %q", width, row)
		default:
			if seen {
				t.Fatalf("width %d: the receipt vanished on a WIDER terminal: %q", width, row)
			}
			dropped = true
		}
	}
	if !dropped || !seen {
		t.Fatalf("nothing was tested: dropped=%v drawn=%v across 20→140", dropped, seen)
	}
}

// Width discipline: every row of every shape of card fits exactly, at every
// width from one cell to a wide terminal.
func TestCardWidthSweep(t *testing.T) {
	cards := map[string]func() *CardBlock{
		"full": sampleCard,
		"bare": func() *CardBlock { return NewCard("bare", "a delivery with nothing else") },
		"failed": func() *CardBlock {
			c := sampleCard()
			c.Glyph, c.Tone = "✕", ToneFailed
			c.SetEnd(EndInterrupted)
			return c
		},
		"long words": func() *CardBlock {
			c := sampleCard()
			c.Body = []string{strings.Repeat("supercalifragilistic", 6)}
			c.Verbs = []string{"ctrl+shift+alt+r rerun this whole delivery from the top"}
			return c
		},
		"wide runes": func() *CardBlock {
			c := sampleCard()
			c.Title, c.Body = "日本語のタイトル", []string{"日本語の本文がここにあります"}
			return c
		},
	}
	for name, build := range cards {
		t.Run(name, func(t *testing.T) {
			card := build()
			for width := 1; width <= 140; width++ {
				rows := card.Rows(width)
				if len(rows) == 0 {
					t.Fatalf("width %d: the card rendered nothing", width)
				}
				for i, row := range rows {
					if w := Width(row); w > width {
						t.Fatalf("width %d: row %d is %d cells: %q", width, i, w, row)
					}
					if strings.Contains(row, "\n") {
						t.Fatalf("width %d: row %d smuggled a newline: %q", width, i, row)
					}
				}
			}
		})
	}
}

// A card too narrow to hold its edge and a cell of content gives up the edge,
// not the content.
func TestCardDropsTheEdgeBeforeTheContent(t *testing.T) {
	c := NewCard("narrow", "delivered")
	c.Glyph = "✓"
	for width := 1; width <= 2; width++ {
		row := c.Rows(width)[1]
		if strings.HasPrefix(row, AccentEdge) {
			t.Fatalf("width %d: the edge survived at the content's expense: %q", width, row)
		}
		if row == "" {
			t.Fatalf("width %d: the card drew an empty title line", width)
		}
	}
	if row := c.Rows(3)[1]; !strings.HasPrefix(row, AccentEdge) {
		t.Fatalf("width 3: the edge should be back: %q", row)
	}
}

// The tone is the whole colour axis, and the zero value claims nothing (8.2.20).
func TestCardToneHues(t *testing.T) {
	cases := map[CardTone]Hue{
		ToneNeutral:   HueNone,
		ToneDelivered: HueMoney,
		ToneFailed:    HueBroken,
	}
	for tone, want := range cases {
		if got := tone.Hue(); got != want {
			t.Fatalf("tone %d resolved to hue %d, want %d", tone, got, want)
		}
	}
	if NewCard("c", "t").Tone != ToneNeutral {
		t.Fatal("a fresh card must claim nothing about its outcome")
	}
}

// A failed card carries coral on its glyph AND its edge — the two cells a
// reader sees before reading a word.
func TestFailedCardPaintsGlyphAndEdgeBroken(t *testing.T) {
	c := sampleCard()
	c.Glyph, c.Tone = "✕", ToneFailed
	c.Styler = hueSpy{}
	row := c.Rows(60)[1]
	if !strings.HasPrefix(row, "<broken:"+AccentEdge+">") {
		t.Fatalf("the edge is not coral: %q", row)
	}
	if !strings.Contains(row, "<broken:✕>") {
		t.Fatalf("the glyph is not coral: %q", row)
	}
	if strings.Contains(row, "<broken:rivers haiku>") {
		t.Fatal("the title took the failure hue; only the glyph and the edge do")
	}
}

// The truncation law applies inside the treatment: a delivery that stopped
// short says so under its body, behind its own edge (12.5).
func TestCardCarriesTheCutRule(t *testing.T) {
	c := sampleCard()
	c.SetEnd(EndTruncatedByCap)
	if c.Version() == 0 {
		t.Fatal("SetEnd did not bump the version")
	}
	rows := c.Rows(60)
	var rule string
	for _, row := range rows {
		if strings.Contains(row, CutMark) {
			rule = row
		}
	}
	if rule == "" {
		t.Fatalf("a cut delivery drew no cut rule:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rule, AccentEdge+" ") {
		t.Fatalf("the cut rule escaped the card's edge: %q", rule)
	}
}

// Paths are cut from the middle: the filename is what a reader was looking for.
func TestCardArtifactKeepsTheFilename(t *testing.T) {
	c := sampleCard()
	c.Body, c.Verbs, c.Receipt = nil, nil, ""
	for width := 24; width <= 60; width++ {
		row := c.Rows(width)[2]
		if !strings.Contains(row, "rivers.txt") {
			t.Fatalf("width %d: the artifact row lost its filename: %q", width, row)
		}
	}
}

// A verb is drawn whole or not at all: a key nobody can press is worse than a
// verb nobody was offered (5.22).
func TestCardKeepsVerbsWhole(t *testing.T) {
	c := NewCard("verbs", "delivered")
	c.Verbs = []string{"ctrl+r open", "r rerun", "d discard"}
	drew := 0
	for width := 8; width <= 60; width++ {
		// A blank, the title line, the verb row when it fits, a blank.
		rows := c.Rows(width)
		if len(rows) != 4 {
			continue // no verb row at this width, which is a legal answer
		}
		row := strings.TrimLeft(strings.TrimPrefix(rows[2], AccentEdge), " ")
		// Whatever was drawn must be a whole prefix of the verb list, joined
		// the one way verbRow joins them.
		ok := false
		for n := len(c.Verbs); n > 0; n-- {
			if row == strings.Join(c.Verbs[:n], sepDot) {
				ok, drew = true, drew+1
				break
			}
		}
		if !ok {
			t.Fatalf("width %d: the verb row is not a whole prefix of the verbs: %q", width, row)
		}
	}
	if drew == 0 {
		t.Fatal("no width drew a verb row, so nothing was tested")
	}
}

// columnOf is where a substring starts in CELLS, which is the only unit a
// layout claim can be made in — a byte offset counts the accent edge as three.
func columnOf(t *testing.T, row, want string) int {
	t.Helper()
	at := strings.Index(row, want)
	if at < 0 {
		t.Fatalf("%q is not in %q", want, row)
	}
	return Width(row[:at])
}

// A grounded card is a rectangle: every row runs the full width, or the ground
// behind it draws torn.
func TestGroundedCardRowsAreRectangular(t *testing.T) {
	c := sampleCard()
	c.Styler = groundSpy{}
	for _, width := range []int{40, 60, 100} {
		rows := c.Rows(width)
		for i, row := range rows[1 : len(rows)-1] {
			if w := Width(row); w != width {
				t.Fatalf("width %d: grounded row %d is %d cells: %q", width, i, w, row)
			}
			if !strings.Contains(row, groundSeq) {
				t.Fatalf("width %d: row %d never reached the ground door: %q", width, i, row)
			}
		}
	}
}

// "Pre-rendered" has to mean it: a body line that fits is the caller's bytes,
// not this package's opinion of them. A table row re-wrapped here is soup.
func TestCardPassesFittingBodyLinesThrough(t *testing.T) {
	const row = "name       │ state   │ cost"
	c := NewCard("table", "delivered")
	c.Body = []string{row, "\x1b[38;5;180mhighlighted\x1b[39m code"}
	rows := c.Rows(60)
	if got := strings.TrimPrefix(rows[2], AccentEdge+" "+strings.Repeat(" ", BodyIndent)); got != row {
		t.Fatalf("a fitting table row was rewritten:\n got %q\nwant %q", got, row)
	}
	if !strings.Contains(rows[3], "\x1b[38;5;180mhighlighted\x1b[39m code") {
		t.Fatalf("a painted line lost its escapes: %q", rows[3])
	}
}

// A Styler with no ground door changes nothing: the card is its edge, and the
// rows are the plain rows.
func TestCardWithoutAGroundDrawsNoPadding(t *testing.T) {
	rows := sampleCard().Rows(80)
	for i, row := range rows {
		if strings.HasSuffix(row, " ") && row != "" {
			t.Fatalf("row %d trails whitespace with no ground to fill: %q", i, row)
		}
	}
}

// The cache is per (width, version), and Mutate is the door that moves both.
func TestCardMutateBumpsTheVersion(t *testing.T) {
	c := sampleCard()
	before := c.Rows(60)[1]
	if c.Version() != 0 {
		t.Fatalf("a fresh card is at version %d", c.Version())
	}
	c.Mutate(func(c *CardBlock) { c.Title = "renamed" })
	if c.Version() != 1 {
		t.Fatalf("version %d after Mutate", c.Version())
	}
	if after := c.Rows(60)[1]; after == before {
		t.Fatalf("the card re-rendered the stale row: %q", after)
	}
	if !c.IsFinalized() {
		t.Fatal("a card is finalized from birth")
	}
}

// A card is a Block like any other: it survives the transcript's strict check,
// which re-renders every cached block and catches committed bytes that moved
// without a version behind them (8.1.1).
func TestCardLivesInATranscript(t *testing.T) {
	at := time.Unix(0, 0)
	tr := New(60, 12)
	tr.Append(NewText("turn", Header{Glyph: "✓", Title: "aforge"}))
	card := sampleCard()
	tr.Append(card)
	first := tr.Frame(at).String()
	if !strings.Contains(first, AccentEdge) {
		t.Fatalf("the card never reached the frame:\n%s", first)
	}
	if again := tr.Frame(at); again.String() != first || !again.Unchanged() {
		t.Fatalf("a settled frame moved:\n%s", again.String())
	}
	card.Mutate(func(c *CardBlock) { c.Receipt = "4 workers · 3m · $0.19" })
	if after := tr.Frame(at).String(); !strings.Contains(after, "$0.19") {
		t.Fatalf("the transcript kept the stale card:\n%s", after)
	}
}

// hueSpy marks which hue each span was painted with, so a colour law can be
// asserted without a token layer.
type hueSpy struct{}

func (hueSpy) Paint(text string, _ State, hue Hue) string {
	if hue == HueBroken {
		return "<broken:" + text + ">"
	}
	return text
}

// groundSpy is the smallest [GroundStyler]. It writes a REAL background
// sequence rather than a marker word, because the contract a card leans on is
// that painting adds no printable cells — a spy that widened its spans would
// pass a rectangle test the token layer would then fail.
type groundSpy struct{}

const groundSeq = "\x1b[48;5;236m"

func (groundSpy) Paint(text string, _ State, _ Hue) string { return text }

func (groundSpy) PaintGround(text string, _ State, _ Hue) string {
	if text == "" {
		return ""
	}
	return groundSeq + text + "\x1b[49m"
}
