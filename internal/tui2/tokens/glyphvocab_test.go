package tokens

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The vocabulary gates: one table, one meaning per glyph, one cell per glyph in
// every tier, and no consumer spelling a mark the table already owns.
//
// glyph_test.go measures the 5.17 floor and glyphset_test.go measures the
// tier's own data. This file measures the two claims that only make sense
// across BOTH at once — that a slot costs the same in either tier, and that no
// two slots say the same thing — plus the one claim that lives outside this
// package entirely: that the packages holding the table's authority actually
// draw from it.

// TestEveryBindingMeasuresOneCellInBothTiers is the width sweep over the WHOLE
// table rather than over each tier's own walk.
//
// GlyphsIn already measures each side separately, and TestTierWidthParity
// measures composed SAMPLE LINES. Neither states the per-slot claim the tier's
// governing invariant actually makes: flipping the tier must not move a column,
// which is a fact about one slot at a time. A binding whose two sides disagreed
// by a cell would pass both existing gates as long as no sample line happened
// to carry it.
func TestEveryBindingMeasuresOneCellInBothTiers(t *testing.T) {
	for _, b := range Vocabulary() {
		for _, side := range []struct {
			tier  GlyphSet
			glyph string
		}{{Plain, Plain.Glyph(b.ID)}, {NerdFont, NerdFont.Glyph(b.ID)}} {
			if side.glyph == "" {
				t.Errorf("%s draws nothing under %s", b.Name, side.tier)
				continue
			}
			if n := utf8.RuneCountInString(side.glyph); n != 1 {
				t.Errorf("%s under %s is %d runes, must be 1", b.Name, side.tier, n)
				continue
			}
			if w := ansi.StringWidth(side.glyph); w != 1 {
				t.Errorf("%s under %s (%q): grapheme width %d, must be 1", b.Name, side.tier, side.glyph, w)
			}
			if w := ansi.StringWidthWc(side.glyph); w != 1 {
				t.Errorf("%s under %s (%q): wcwidth %d, must be 1", b.Name, side.tier, side.glyph, w)
			}
		}
		plain, nf := Plain.Glyph(b.ID), NerdFont.Glyph(b.ID)
		if a, c := ansi.StringWidth(plain), ansi.StringWidth(nf); a != c {
			t.Errorf("%s: %d cells plain, %d cells nerdfont — the tier moved this slot's column", b.Name, a, c)
		}
		if a, c := ansi.StringWidthWc(plain), ansi.StringWidthWc(nf); a != c {
			t.Errorf("%s: wcwidth %d plain, %d nerdfont", b.Name, a, c)
		}
		if b.Geometry && nf != plain {
			t.Errorf("%s is geometry and must resolve to its plain glyph under every tier", b.Name)
		}
	}
}

// TestOneGlyphOneMeaning is 12's first clause enforced: a glyph means exactly
// one thing product-wide.
//
// Three plain glyphs deliberately serve two slots each — ○ is queued and step
// pending, ◐ is working and step running, ⚑ is waits-on and step blocked —
// because those pairs ARE one meaning read at two scales, which is why the
// whole-cell upgrade can rewrite them without knowing which slot it is looking
// at. Every such pair is named below, so a FOURTH one has to be argued for in
// this test rather than discovered on a screen. The tier side is held to the
// same rule from the other direction: two slots may share an icon only where
// they already share the plain glyph, or the tier would collapse a distinction
// the floor draws.
func TestOneGlyphOneMeaning(t *testing.T) {
	deliberate := map[string]string{
		GlyphStepPending: "queued, at two scales",
		GlyphStepRunning: "working, at two scales",
		GlyphStepBlocked: "blocked on something else, at two scales",
		GlyphProseBullet: "a middle dot: the telemetry separator's byte, a different slot",
		GlyphProseQuote:  "an eighth block: the code fence's gutter, and the blockquote's — both are a margin beside a block set apart",
		GlyphShell:       "a dollar: the spend mark's byte, told apart by what follows it",
		GlyphActionCreate: "a plus: the diffstat's byte, and the step gutter's mark for a thing " +
			"that was not there — one is bound to a number, the other stands alone in a gutter",
		GlyphFileDocument: "a page of text, at two scales: a call that read one, and one on an " +
			"attachment chip — and the tier draws BOTH as nf-fa-file_text, so nothing the floor " +
			"draws apart is collapsed",
	}
	plainOwner := map[string]string{}
	for _, b := range Vocabulary() {
		if prior, dup := plainOwner[b.Plain]; dup {
			if _, ok := deliberate[b.Plain]; !ok {
				t.Errorf("%q says both %q and %q, and 12 allows a glyph exactly one meaning; "+
					"if the two really are one meaning at two scales, say so in this test's table",
					b.Plain, prior, b.Name)
			}
			continue
		}
		plainOwner[b.Plain] = b.Name
	}

	nfOwner := map[string]string{}
	for _, b := range Vocabulary() {
		if b.NerdFont == "" {
			continue
		}
		prior, dup := nfOwner[b.NerdFont]
		if !dup {
			nfOwner[b.NerdFont] = b.Name
			continue
		}
		// Sharing an icon is only honest where the floor already shares a glyph.
		var priorPlain string
		for _, other := range Vocabulary() {
			if other.Name == prior {
				priorPlain = other.Plain
			}
		}
		if priorPlain != b.Plain {
			t.Errorf("%s and %s draw the same icon (%s) but different plain glyphs (%q vs %q): "+
				"the tier would collapse a distinction the floor draws",
				prior, b.Name, b.NFName, priorPlain, b.Plain)
		}
	}
}

// TestBlocksTwinsAreOneMark extends the seam TestCutMarkIsOneMark opened.
//
// blocks cannot import this package — the edge runs tokens → blocks so blocks
// stays a leaf — so a mark blocks has to draw itself is spelled twice: once as
// the vocabulary's authority here, once as a byte there. tokens is the only
// package that can hold both spellings and fail when they part. The cut mark
// was the first; the accent edge and the body indent are the two that followed,
// and the arrangement is deliberate in blocks/card.go for the same reason.
func TestBlocksTwinsAreOneMark(t *testing.T) {
	if GlyphAccentRail != blocks.AccentEdge {
		t.Errorf("two vocabularies for one accent rail: tokens %q, blocks %q",
			GlyphAccentRail, blocks.AccentEdge)
	}
	if GlyphTreeDash != blocks.RuleMark {
		t.Errorf("two vocabularies for one rule stroke: tokens %q, blocks %q",
			GlyphTreeDash, blocks.RuleMark)
	}
	if LensIndent != blocks.BodyIndent {
		t.Errorf("two numbers for one left edge (5.13): tokens.LensIndent %d, blocks.BodyIndent %d",
			LensIndent, blocks.BodyIndent)
	}
}

// -- the source scan ---------------------------------------------------------

// scannedPackages are the consumer packages held to the table by
// TestConsumersDrawFromTheTable. It is a list rather than "every package under
// internal/tui2" on purpose: the sweep that moves the remaining surfaces onto
// slots is a later wave, and a gate that fails for work nobody has done yet is
// a gate people learn to ignore. A package joins this list the day its sweep
// lands, and never leaves it.
var scannedPackages = []string{
	"../modelui",
	// Not yet, and each for the same reason — the sweep is a later wave, and the
	// remaining sites are few since the v2 surface's packages were removed on
	// 2026-08-31: blocks (a leaf, so it needs twin constants rather than an
	// import), prose, and internal/tui2 itself (placeholder.go, dialog.go).
	// reltime was scanned during the audit and is already clean, so it can join
	// the day somebody wants it pinned.
}

// literalExemptions are the files allowed to spell a vocabulary byte, with the
// reason each one is out. A reason is required: an exemption without one is how
// a list like this stops meaning anything.
var literalExemptions = map[string]string{
	"../golden/diff.go": "the invisible-character legend of a TEST diff — `·` there is a SPACE, " +
		"not the telemetry separator, and the line that draws it also names it. Developer " +
		"output, never a rendered surface, so the vocabulary does not reach it",
}

// proseMarks are the vocabulary bytes that are also ordinary punctuation. They
// are allowed inside a sentence — a literal carrying at least a few letters —
// because an em dash between two clauses is prose and an em dash alone in a
// cell is [GlyphMissing], and only the surrounding text tells them apart.
var proseMarks = map[rune]bool{'—': true, '–': true, '…': true, '’': true, '“': true, '”': true}

// TestConsumersDrawFromTheTable is the no-inline-glyph-literal gate.
//
// 16's glyph discipline ends "New glyphs need a table entry + parity golden
// update, never an inline literal", and until something reads the source that
// sentence is enforced by review. It is cheap to read the source: parse each
// file, look at string and character literals only (a comment may say `▸` all
// it likes), and fail on any rune the vocabulary already owns.
//
// The gate finds two different bugs with one rule. A surface that spells `⌂`
// itself is not merely duplicating a constant — it is drawing the PLAIN floor
// forever, because a literal cannot know about the repertoire tier, which is
// exactly the bug this test was written after finding in the place line.
func TestConsumersDrawFromTheTable(t *testing.T) {
	owned := map[rune]string{}
	for _, g := range Glyphs() {
		r, _ := utf8.DecodeRuneInString(g.Glyph)
		if r < utf8.RuneSelf {
			// ASCII slots ("?", "$", "/", "+", "~", "=") are characters a
			// consumer types for a hundred honest reasons; the table never had
			// exclusive claim on them, which is the same finding that made them
			// opt-out of the automatic upgrade (12.7 D.3).
			continue
		}
		if _, taken := owned[r]; !taken {
			owned[r] = g.Name
		}
	}

	for _, root := range scannedPackages {
		root := root
		t.Run(strings.TrimPrefix(root, "../"), func(t *testing.T) {
			fset := token.NewFileSet()
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
					return err
				}
				if strings.HasSuffix(path, "_test.go") {
					return nil
				}
				if why, exempt := literalExemptions[filepath.ToSlash(path)]; exempt {
					t.Logf("%s is exempt: %s", path, why)
					return nil
				}
				file, err := parser.ParseFile(fset, path, nil, 0)
				if err != nil {
					t.Errorf("parse %s: %v", path, err)
					return nil
				}
				ast.Inspect(file, func(n ast.Node) bool {
					lit, ok := n.(*ast.BasicLit)
					if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
						return true
					}
					text, ok := literalText(lit)
					if !ok {
						return true
					}
					prose := looksLikeProse(text)
					for _, r := range text {
						name, owns := owned[r]
						if !owns || (prose && proseMarks[r]) {
							continue
						}
						t.Errorf("%s: the literal %s spells %U, which is the vocabulary's %s slot. "+
							"Reach for tokens.Glyph%s, or for the SLOT through a Styler so the line "+
							"gets the repertoire tier (16, 12.7)",
							fset.Position(lit.Pos()), lit.Value, r, name, name)
					}
					return true
				})
				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", root, err)
			}
		})
	}
}

// literalText unquotes a literal, reporting false for one it cannot read — a
// malformed literal is the compiler's business, not this test's.
func literalText(lit *ast.BasicLit) (string, bool) {
	if lit.Kind == token.CHAR {
		r, _, _, err := strconv.UnquoteChar(strings.Trim(lit.Value, "'"), '\'')
		if err != nil {
			return "", false
		}
		return string(r), true
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// looksLikeProse says whether a literal is a sentence rather than a cell. Three
// letters is the whole test, and it is deliberately crude: the marks it gates
// are only ever ambiguous inside running text, and a cell that happens to carry
// three letters and an em dash is a cell that wanted [GlyphMissing] anyway.
func looksLikeProse(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
			if letters >= 3 {
				return true
			}
		}
	}
	return false
}
