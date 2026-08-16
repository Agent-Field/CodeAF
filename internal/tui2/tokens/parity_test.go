package tokens

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The parity golden (12.7 F.7): one sample line per surface, rendered in BOTH
// tiers, with the printable widths asserted equal under both rulers and the
// bytes recorded so the shapes are reviewable.
//
// This is the gate the whole default-on decision rests on. 12.7's governing
// invariant is that the tier changes which glyph is drawn in a cell and never
// how many cells a line occupies — so a user who turns the tier off does not
// get a broken surface or a lesser one, they get 5.17 exactly as specified,
// and the cost of guessing wrong about their font is one keystroke and zero
// layout damage. That claim is only worth making because this test measures it.
//
// The golden records the codepoints beside each line on purpose: a private-use
// character is invisible in a diff, and a golden nobody can read in review is a
// golden that rots.

var updateParity = flag.Bool("update-glyph-parity", false, "rewrite testdata/glyph_parity.golden")

const parityGolden = "testdata/glyph_parity.golden"

// paritySurfaces renders one sample line for each surface that carries a glyph,
// spelled the way that surface spells it. The tokens package cannot import the
// rail, the place line or the composer — the dependency runs the other way —
// so the lines are composed here from the same API those packages hold, which
// is the part under test anyway.
func paritySurfaces(s *Styler) []struct{ name, line string } {
	const width = 72

	card := blocks.Header{
		Glyph:    s.Glyph(GWorking),
		GlyphHue: blocks.HueAlive,
		State:    blocks.StateLive,
		Title:    "navigate",
		Desc:     "read src/navigate.rs",
		Badges:   []blocks.Badge{blocks.CountBadge(s.Glyph(GNeedsHuman), 2, blocks.HueAttention)},
		Meta:     []string{"12s", "$0.04"},
	}

	// The place line of 5.19, with the git segment. The folder and the branch
	// are ASCII and identity slots, so they come through the explicit door —
	// which is exactly the shape the placeline package will use.
	sep := " " + s.Glyph(GSeparator) + " "
	place := "~/a/v2" + sep + s.Glyph(GHome) + " /tmp/wisp-parity" +
		sep + s.Glyph(GGitBranch) + " main" + sep + s.Glyph(GFolder) + " src/navigate.rs"

	// The status line of 5.17: "K3 ▄ $8.65", with the model mark in front.
	status := s.Glyph(GModel) + " K3 " + Gauge(0.5) + " " + s.Glyph(GSpend) + "8.65"

	return []struct{ name, line string }{
		{"card line 1", card.Render(width, s)},
		{"place line", s.Paint(place, blocks.StateChrome, blocks.HueNone)},
		{"status line", s.Paint(status, blocks.StateChrome, blocks.HueNone)},
		{"composer prompt (chat)", s.PaintGlyph(GPromptChat, blocks.StateSettled, blocks.HueNone) +
			" what changed in navigate.rs"},
		{"composer prompt (steer)", s.PaintGlyph(GPromptSteer, blocks.StateLive, blocks.HueAlive) +
			" keep the branch"},
		{"fold hint", s.Paint(s.GlyphSet().UpgradeChrome(blocks.Disclose(false, 12, "line", "lines")),
			blocks.StateChrome, blocks.HueNone)},
		{"fold hint (expanded)", s.Paint(blocks.Disclose(true, 0, "line", "lines"), blocks.StateChrome, blocks.HueNone)},
		{"cut row", blocks.CutRule(blocks.EndTruncatedByCap, 44, s)},
		{"step dots", s.Paint(strings.Join([]string{
			s.Glyph(GStepDone), s.Glyph(GStepDone), s.Glyph(GStepRunning),
			s.Glyph(GStepBlocked), s.Glyph(GStepPending),
		}, "")+"  3/5", blocks.StateChrome, blocks.HueNone)},
		{"queue and boost", s.Paint(strings.Repeat(s.Glyph(GQueuePill), 3)+" "+
			s.Glyph(GBoosted)+" boosted"+sep+s.Glyph(GMissing), blocks.StateChrome, blocks.HueNone)},
		{"scope breadcrumb", s.Paint(s.Glyph(GScopeUp)+" tasks"+sep+s.Glyph(GTruncated),
			blocks.StateChrome, blocks.HueNone)},
		// The geometry slots that a reader most often expects to have changed:
		// the prose marks and the static overflow ellipsis. This row exists to
		// record a NON-change — both tiers must be byte-identical here — which
		// is the half of the tier's contract the other rows cannot show.
		{"prose and static overflow", s.Paint(s.Glyph(GProseQuote)+" "+s.Glyph(GProseBullet)+
			" a list item that ran out of column"+s.Glyph(GEllipsis)+"  "+
			s.Glyph(GCodeGutter)+" fmt.Println", blocks.StateChrome, blocks.HueNone)},
		{"settle row", s.Paint(s.Glyph(GSettled)+" done"+sep+s.Glyph(GDiffAdd)+"42"+
			sep+s.Glyph(GDiffDel)+"7"+sep+s.Glyph(GFailed)+" one check", blocks.StateSettled, blocks.HueNone)},
	}
}

// TestTierWidthParity is the assertion; TestParityGolden below is the record.
// Widths are measured with colour ON, so what is compared is the printable
// width of the bytes a terminal actually receives.
func TestTierWidthParity(t *testing.T) {
	plain := paritySurfaces(NewStylerIn(TrueColor, FocusNormal, Plain))
	nf := paritySurfaces(NewStylerIn(TrueColor, FocusNormal, NerdFont))
	if len(plain) != len(nf) {
		t.Fatal("the two tiers rendered a different number of surfaces")
	}
	for i := range plain {
		if plain[i].name != nf[i].name {
			t.Fatalf("surface %d is %q in one tier and %q in the other", i, plain[i].name, nf[i].name)
		}
		if a, b := ansi.StringWidth(plain[i].line), ansi.StringWidth(nf[i].line); a != b {
			t.Errorf("%s: grapheme width %d plain, %d nerdfont — the tier moved a column",
				plain[i].name, a, b)
		}
		if a, b := ansi.StringWidthWc(plain[i].line), ansi.StringWidthWc(nf[i].line); a != b {
			t.Errorf("%s: wcwidth %d plain, %d nerdfont — the tier moved a column",
				plain[i].name, a, b)
		}
		if plain[i].line == "" {
			t.Errorf("%s rendered nothing", plain[i].name)
		}
	}
}

// TestParityGolden records both tiers' bytes. Run with -update-glyph-parity to
// rewrite it, and READ the diff: this file is where a codepoint that drifted,
// or a surface that started spelling itself differently, becomes visible.
func TestParityGolden(t *testing.T) {
	var b strings.Builder
	b.WriteString("# 12.7 F.7 — one sample line per surface, both tiers, widths asserted equal.\n")
	b.WriteString("# Rendered at NoColor so the shapes are the subject; TestTierWidthParity\n")
	b.WriteString("# measures the coloured bytes. Marks list the non-ASCII codepoints in order,\n")
	b.WriteString("# because a private-use character is invisible in a diff.\n")

	plain := paritySurfaces(NewStylerIn(NoColor, FocusNormal, Plain))
	nf := paritySurfaces(NewStylerIn(NoColor, FocusNormal, NerdFont))
	for i := range plain {
		fmt.Fprintf(&b, "\n--- %s ---\n", plain[i].name)
		for _, row := range []struct {
			tier string
			line string
		}{{"plain   ", plain[i].line}, {"nerdfont", nf[i].line}} {
			fmt.Fprintf(&b, "%s %q\n", row.tier, row.line)
			fmt.Fprintf(&b, "%s width %d/%d  marks %s\n", strings.Repeat(" ", len(row.tier)),
				ansi.StringWidth(row.line), ansi.StringWidthWc(row.line), marksOf(row.line))
		}
	}
	got := b.String()

	if *updateParity {
		if err := os.WriteFile(parityGolden, []byte(got), 0o644); err != nil {
			t.Fatalf("update %s: %v", parityGolden, err)
		}
		return
	}
	want, err := os.ReadFile(parityGolden)
	if err != nil {
		t.Fatalf("read %s (run go test -update-glyph-parity): %v", parityGolden, err)
	}
	if string(want) != got {
		t.Errorf("%s is stale.\n--- recorded ---\n%s\n--- rendered ---\n%s", parityGolden, want, got)
	}
}

// marksOf names every non-ASCII rune in a line, in order, so the golden can be
// reviewed by someone whose font draws private use as a box.
func marksOf(line string) string {
	var out []string
	for _, r := range line {
		if r < 0x80 {
			continue
		}
		out = append(out, fmt.Sprintf("%U", r))
	}
	if len(out) == 0 {
		return "(none)"
	}
	return strings.Join(out, " ")
}
