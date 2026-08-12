package placeline

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func styler() *tokens.Styler { return tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal) }

// TestRender_WidthStability sweeps width 1..110 (and a few pathological
// values around it) across every ground shape this package defines, focused
// and not, styled and not. Nothing may panic and no rendered row may exceed
// the width it was given.
func TestRender_WidthStability(t *testing.T) {
	grounds := [][]Segment{
		nil,
		{{Path: "/home/santosh/src/aforge-v2", Kind: SegmentRoot}},
		{
			{Path: "/home/santosh/src/aforge-v2", Kind: SegmentRoot},
			{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
		},
		{
			{Path: "/home/santosh/src/aforge-v2", Kind: SegmentRoot},
			{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
			{Path: "src/navigate.rs", Kind: SegmentRegion},
		},
		{
			{Path: "/home/santosh/src/aforge-v2", Kind: SegmentRoot},
			{Path: "/tmp/wisp-parity-with-a-genuinely-long-workspace-name-that-will-not-fit", Kind: SegmentWorkspace},
			{Path: "deep/nested/region/of/the/tree/navigate.rs", Kind: SegmentRegion},
		},
	}

	for gi, ground := range grounds {
		for _, home := range []string{"", "/home/santosh"} {
			for _, focused := range []bool{false, true} {
				for _, sty := range []*tokens.Styler{nil, styler()} {
					m := New(Options{Styler: sty, Home: home})
					m.SetGround(ground...)
					m.Focus(focused)

					for width := -1; width <= 110; width++ {
						func() {
							defer func() {
								if r := recover(); r != nil {
									t.Fatalf("ground %d width %d panicked: %v", gi, width, r)
								}
							}()
							out := m.Render(width)
							if strings.Contains(out, "\n") {
								t.Fatalf("ground %d width %d produced more than one row: %q", gi, width, out)
							}
							if w := ansi.StringWidth(out); w > width && width > 0 {
								t.Fatalf("ground %d width %d rendered %d cells: %q", gi, width, w, out)
							}
							if width <= 0 && out != "" {
								t.Fatalf("ground %d width %d rendered non-empty: %q", gi, width, out)
							}
						}()
					}
				}
			}
		}
	}
}

// TestFishAbbreviation pins 5.19's rule in words: every segment but the last
// is cut to its first rune, the last segment always survives whole.
func TestFishAbbreviation(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"", ""},
		{"~", "~"},
		{"~/aforge-v2", "~/aforge-v2"},
		{"~/src/aforge-v2", "~/s/aforge-v2"},
		{"/home/santosh/src/aforge-v2", "/h/s/s/aforge-v2"},
		{"nested/deep/leaf", "n/d/leaf"},
	}
	for _, tt := range tests {
		if got := fishAbbreviate(tt.path); got != tt.want {
			t.Errorf("fishAbbreviate(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// TestFishAbbreviationIsRuneSafe proves a non-ASCII directory name
// abbreviates to one whole character, never a truncated UTF-8 byte.
func TestFishAbbreviationIsRuneSafe(t *testing.T) {
	got := fishAbbreviate("/日本語/ディレクトリ/leaf")
	want := "/日/デ/leaf"
	if got != want {
		t.Errorf("fishAbbreviate(unicode) = %q, want %q", got, want)
	}
}

// TestRootAbbreviatedUnlessFocused: the root leg shortens at rest and
// reveals its full form on focus (5.19: "full path on focus"); the
// workspace and region legs never change with focus at all — they were
// already full.
func TestRootAbbreviatedUnlessFocused(t *testing.T) {
	m := New(Options{Home: "/home/santosh"})
	m.SetGround(
		Segment{Path: "/home/santosh/src/aforge-v2", Kind: SegmentRoot},
		Segment{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
	)

	m.Focus(false)
	blurred := m.Render(200)
	if !strings.Contains(blurred, "~/s/aforge-v2") {
		t.Fatalf("blurred root not fish-abbreviated: %q", blurred)
	}
	if strings.Contains(blurred, "~/src/aforge-v2") {
		t.Fatalf("blurred root leaked its full form: %q", blurred)
	}

	m.Focus(true)
	focused := m.Render(200)
	if !strings.Contains(focused, "~/src/aforge-v2") {
		t.Fatalf("focused root did not reveal its full form: %q", focused)
	}

	// The workspace leg is identical either way: never abbreviated.
	if !strings.Contains(blurred, "⌂ /tmp/wisp-parity") || !strings.Contains(focused, "⌂ /tmp/wisp-parity") {
		t.Fatalf("workspace leg changed with focus:\nblurred=%q\nfocused=%q", blurred, focused)
	}
}

// TestWorkspaceGlyph pins the ⌂ marker on a workspace leg and its absence
// everywhere else.
func TestWorkspaceGlyph(t *testing.T) {
	m := New(Options{})
	m.SetGround(
		Segment{Path: "/home/santosh/aforge-v2", Kind: SegmentRoot},
		Segment{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
		Segment{Path: "src/navigate.rs", Kind: SegmentRegion},
	)
	out := m.Render(200)
	if strings.Count(out, "⌂") != 1 {
		t.Fatalf("want exactly one ⌂, got %q", out)
	}
	if !strings.HasSuffix(out, "src/navigate.rs") {
		t.Fatalf("region leg not shown in full at the tail: %q", out)
	}
}

// TestWorkspaceGlyphFollowsTheTier is the regression the glyph audit left
// behind. The workspace mark used to be a local constant, which meant the place
// line was the one surface in the product that stayed PLAIN when a reader
// turned nerd fonts on — every column around it swapped and this one did not.
// It reads the GHome SLOT off the styler now, so the tier reaches it, and the
// two sides are asserted to cost the same so the fit arithmetic cannot drift.
func TestWorkspaceGlyphFollowsTheTier(t *testing.T) {
	ground := []Segment{
		{Path: "/home/santosh/aforge-v2", Kind: SegmentRoot},
		{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
	}

	nf := New(Options{Styler: tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.NerdFont)})
	nf.SetGround(ground...)
	nfOut := nf.Render(200)
	if want := tokens.NerdFont.Glyph(tokens.GHome); !strings.Contains(nfOut, want) {
		t.Fatalf("the nerd-font tier did not reach the place line: %q does not carry %q", nfOut, want)
	}
	if strings.Contains(nfOut, tokens.GlyphHome) {
		t.Fatalf("the nerd-font tier still drew the plain house: %q", nfOut)
	}

	plain := New(Options{Styler: tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.Plain)})
	plain.SetGround(ground...)
	plainOut := plain.Render(200)
	if !strings.Contains(plainOut, tokens.GlyphHome) {
		t.Fatalf("the plain floor lost its house: %q", plainOut)
	}
	if a, b := ansi.StringWidth(plainOut), ansi.StringWidth(nfOut); a != b {
		t.Errorf("the tier moved a column: %d cells plain, %d cells nerdfont", a, b)
	}

	// A nil Styler is a supported degradation everywhere else in this package,
	// and it degrades to the designed floor rather than to nothing.
	bare := New(Options{})
	bare.SetGround(ground...)
	if got := bare.Render(200); !strings.Contains(got, tokens.GlyphHome) {
		t.Errorf("a nil styler drew %q, want the plain floor's house", got)
	}
}

// TestMiddleEllipsisNeverTailTruncates: when the ground's only leg does not
// fit, and there is enough width to show the ellipsis alongside the whole
// filename, it gives way in the middle (the filename survives intact),
// never at the tail.
func TestMiddleEllipsisNeverTailTruncates(t *testing.T) {
	m := New(Options{})
	m.SetGround(Segment{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace})
	// "⌂ /tmp/wisp-parity" is 19 cells and the leg is laid out inside the
	// lens's left edge, so 15 + LensIndent leaves it the same 15 to shrink into.
	out := m.Render(15 + tokens.LensIndent)
	if !strings.HasSuffix(out, "wisp-parity") {
		t.Fatalf("filename did not survive width pressure: %q", out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("no ellipsis mark despite truncation: %q", out)
	}
}

// TestDropsFromTheLeft: under width pressure a multi-leg ground drops its
// broadest (leftmost) context first, keeping the most specific (rightmost)
// leg intact the longest. When there is room for both the surviving leg and
// a static overflow mark, the mark shows; when the surviving leg alone
// already fills the width, the leg's own content is never sacrificed just to
// also fit a decoration — only the ancestry, already less important, went
// quietly.
func TestDropsFromTheLeft(t *testing.T) {
	m := New(Options{Home: "/home/santosh"})
	m.SetGround(
		Segment{Path: "/home/santosh/src/aforge-v2", Kind: SegmentRoot},
		Segment{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
	)
	full := m.Render(200)
	if !strings.Contains(full, "~/s/aforge-v2") {
		t.Fatalf("full render missing root: %q", full)
	}

	// "⌂ /tmp/wisp-parity" is 19 cells; the root plus separator is 16 more. Every
	// width here is stated as content + the lens's left edge, because that edge
	// is spent before a leg is fitted (5.13's rhythm; see Render).
	withMarker := m.Render(23 + tokens.LensIndent) // "… · " (4) + the workspace leg (19).
	if strings.Contains(withMarker, "aforge-v2") {
		t.Fatalf("root should have dropped first: %q", withMarker)
	}
	if !strings.HasSuffix(withMarker, "wisp-parity") {
		t.Fatalf("workspace leg should have survived: %q", withMarker)
	}
	if !strings.HasPrefix(withMarker, lensPad+overflowMark) {
		t.Fatalf("dropped leg with room to spare left no overflow mark: %q", withMarker)
	}

	bare := m.Render(19 + tokens.LensIndent) // exactly the leg, no room for the marker.
	if bare != lensPad+"⌂ /tmp/wisp-parity" {
		t.Fatalf("workspace leg at its exact width should render whole and bare: %q", bare)
	}
}

// TestSetGroundClears: an empty SetGround call clears a previously set
// ground rather than leaving the old one stuck on screen.
func TestSetGroundClears(t *testing.T) {
	m := New(Options{})
	m.SetGround(Segment{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace})
	if m.Render(80) == "" {
		t.Fatal("ground did not render before clearing")
	}
	m.SetGround()
	if out := m.Render(80); out != "" {
		t.Fatalf("cleared ground still rendered: %q", out)
	}
	if got := m.CopyText(); got != "" {
		t.Fatalf("cleared ground still copies: %q", got)
	}
}

// TestCopyText pins 5.19's "click/y copies": the resolved, real path of the
// ground's most specific leg — a region resolved against its workspace, or
// against the root when there is no workspace leg.
func TestCopyText(t *testing.T) {
	tests := []struct {
		name   string
		ground []Segment
		want   string
	}{
		{"empty", nil, ""},
		{"root only", []Segment{{Path: "/home/santosh/aforge-v2", Kind: SegmentRoot}}, "/home/santosh/aforge-v2"},
		{
			"workspace",
			[]Segment{
				{Path: "/home/santosh/aforge-v2", Kind: SegmentRoot},
				{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
			},
			"/tmp/wisp-parity",
		},
		{
			"region under workspace",
			[]Segment{
				{Path: "/home/santosh/aforge-v2", Kind: SegmentRoot},
				{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
				{Path: "src/navigate.rs", Kind: SegmentRegion},
			},
			"/tmp/wisp-parity/src/navigate.rs",
		},
		{
			"region under root (no workspace)",
			[]Segment{
				{Path: "/home/santosh/aforge-v2", Kind: SegmentRoot},
				{Path: "internal/tui2", Kind: SegmentRegion},
			},
			"/home/santosh/aforge-v2/internal/tui2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Options{})
			m.SetGround(tt.ground...)
			// CopyText must not depend on the abbreviated render form: verify
			// it is unaffected by focus.
			m.Focus(false)
			gotBlurred := m.CopyText()
			m.Focus(true)
			gotFocused := m.CopyText()
			if gotBlurred != tt.want || gotFocused != tt.want {
				t.Fatalf("CopyText = %q / %q (blurred/focused), want %q", gotBlurred, gotFocused, tt.want)
			}
		})
	}
}

// TestRenderNeverPanicsWithNilStyler pins the composer-package posture: a
// nil Styler degrades to plain text rather than a nil-pointer panic.
func TestRenderNeverPanicsWithNilStyler(t *testing.T) {
	m := New(Options{})
	m.SetGround(Segment{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace})
	if out := m.Render(80); !strings.Contains(out, "wisp-parity") {
		t.Fatalf("nil-styler render missing content: %q", out)
	}
}

// TestTheLineOpensAtTheLensEdge: the ground is one of the room's surfaces and
// begins where the room does (5.13's spacing rhythm, [tokens.LensIndent]), with
// the indent taken out of the fitting width rather than added to it.
func TestTheLineOpensAtTheLensEdge(t *testing.T) {
	m := New(Options{Home: "/home/santosh"})
	m.SetGround(
		Segment{Path: "/home/santosh/src/aforge-v2", Kind: SegmentRoot},
		Segment{Path: "/tmp/wisp-parity", Kind: SegmentWorkspace},
	)
	for width := tokens.LensIndent + 1; width <= 120; width++ {
		row := m.Render(width)
		if !strings.HasPrefix(row, lensPad) {
			t.Fatalf("at width %d the ground began at column 0: %q", width, row)
		}
		if strings.HasPrefix(row[tokens.LensIndent:], " ") {
			t.Fatalf("at width %d the ground began past the edge: %q", width, row)
		}
		if got := ansi.StringWidth(row); got > width {
			t.Fatalf("at width %d the indented ground ran %d cells: %q", width, got, row)
		}
	}
	// Below the gutter's own width the rhythm gives way to the path: two cells
	// of air are not worth the last two cells of a ground.
	if row := m.Render(tokens.LensIndent); strings.HasPrefix(row, " ") {
		t.Fatalf("the gutter survived a %d-cell terminal: %q", tokens.LensIndent, row)
	}
}
