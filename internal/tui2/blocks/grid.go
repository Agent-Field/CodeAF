package blocks

// THE GRID (§20), as numbers rather than as a habit each surface picks up on its
// own.
//
//	col:  0 1 2 3 4 5 6 7 8 …
//	      [G ][content at E0............................  (dim receipt)]
//	      [  ][└ ][child at E1.......................................]
//	      [  ][  ][□ ][grandchild at E2..............................]
//
// One geometry for every surface, and the reason it lives HERE is the reason
// [BodyIndent] does: blocks is the leaf every renderer already depends on, so a
// constant declared once is a constant every consumer inherits. A surface that
// spells its own `2` is a surface that can drift, and §20 was written because
// four of them had.
//
// The authority above this file is tokens.LensIndent, which states the same
// number for the same reason; blocks cannot import tokens (the edge runs
// tokens → blocks so blocks stays a leaf), so — exactly as [AccentEdge] and
// [CutMark] do — the number lives here as well as there and a drift is a failed
// test rather than two grids for one product.
const (
	// Gutter is the marker column: cols 0–1, holding the block's mark (a state
	// glyph, a voice glyph, `›`, `●`, or nothing) and one space. CHROME ONLY —
	// selection rails and accent edges may share it, content never enters it.
	Gutter = 2

	// ContentEdge is col 2: where every top-level title, sentence and row name
	// on every surface hangs. The eye learns it once. It is the same number as
	// [Gutter] because the gutter is exactly what precedes it, and they are
	// named separately because they are different facts — one is a width, the
	// other is a position, and a surface that conflated them is why the board
	// drew its names at col 4.
	ContentEdge = 2

	// IndentStep is 2: the cells one level of descent costs. Depth n hangs at
	// [ContentEdge] + n*IndentStep, and the two cells before a child's edge are
	// its own marker cells — its elbow (`└`), its glyph, or its `│` gutter.
	// Never three spaces, never one.
	IndentStep = 2

	// SectionAbove is the blank rows a section word takes ABOVE itself: ONE, and
	// none below it.
	//
	// A section word belongs to what FOLLOWS it, and padding is how a row says
	// so. Air above and none below binds the word to its list; air on both sides
	// leaves it floating between two groups with nothing saying which one it
	// names, which is what three of the four home headings were doing. §20's own
	// rhythm is "exactly one blank line between blocks", and this is that same
	// blank read from the side that owns it: the section is the block, and its
	// word is the first row of it rather than a block of its own.
	SectionAbove = 1
)

// Depth is the content edge of depth n: 2 + 2n. Depth 0 is [ContentEdge].
//
// It exists so a renderer walking a tree says what it means rather than
// open-coding the arithmetic, which is how a `4*depth` ladder gets into a file
// that meant to say `2*depth`.
func Depth(n int) int {
	if n < 0 {
		n = 0
	}
	return ContentEdge + n*IndentStep
}

// MarkerCol is where the marker for depth-n content sits: the two cells
// immediately before its edge. Depth 0's marker is the gutter itself.
func MarkerCol(n int) int {
	if n < 0 {
		n = 0
	}
	return n * IndentStep
}
