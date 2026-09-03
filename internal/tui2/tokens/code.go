package tokens

// The code ramp (5.13, 5.16) and the two prose glyphs.
//
// A fenced code block is the one place on this surface where colour does NOT
// mean what the five-word vocabulary says it means: a keyword is not "needs a
// human" and a string literal is not "money". Syntax is a second, narrower
// language, and letting it borrow amber and green would make the loudest words
// in the palette say two different things on the same screen.
//
// So the ramp below is its OWN small vocabulary — six pastels and the body
// tier — declared here rather than in the renderer for the reason every other
// colour is declared here: it has to be readable at a glance, gated by the
// contrast test, and degraded in ONE place. It is deliberately NOT part of the
// [Token] inventory: these values are never composed with [Hue] or [State],
// never resolved by [ResolveToken], and never legal as a foreground for
// anything but source text, so giving them token ordinals would widen the
// composition law to cover a vocabulary that does not compose.
//
// # Degradation (10.1.2, honest degradation)
//
// The ramp exists at [TrueColor] and is approximated at [ANSI256]. Below that
// it does not exist at all: [CodeHighlighting] answers false and the renderer
// draws every line of the block at the ordinary primary tier. That is the same
// ruling [Profile.IdentityDistinct] makes about the identity wheel and for the
// same reason — six pastels collapsed onto the classic sixteen would put a
// tool's red on a Go keyword, and a lying colour is worse than no colour
// (5.20). Unhighlighted code is still perfectly legible code.
//
// The pastels are pitched against the [Sheet] ground a code block is painted
// on, not against the [Ground], because that is where they are actually drawn.
// code_test.go walks every slot against ground, sheet and band at both focus
// states, so this ramp signs the same contract the palette does.

// CodeSlot names one class of source token. Six pastels plus the body tier is
// the whole vocabulary: the ramp answers "what KIND of word is this", not "what
// does this identifier mean", so a finer palette would be a palette nobody can
// read at prose size.
type CodeSlot uint8

const (
	// CodeText is everything a lexer has no opinion about — punctuation,
	// operators, identifiers, whitespace. It is the primary text tier by
	// value, so unhighlighted code and the uncoloured parts of highlighted
	// code are the same grey.
	CodeText CodeSlot = iota
	CodeKeyword
	CodeString
	CodeNumber
	// CodeComment is the only slot in the ramp that RECEDES. It signs
	// [ClassChrome], because a comment is prose a reader skips past on the way
	// to the code, and 5.13 spends its contrast budget on what is being read.
	CodeComment
	CodeFunction
	CodeType
	codeSlotCount
)

// The ramp, as literal hex, in slot order. These are the only hand-authored
// colours this file adds, and they are pitched one step off the semantic hues
// on purpose: close enough to belong to the same palette, far enough that a
// keyword is never mistaken for a state.
const (
	CodeTextHex     = "#E6E6F0" // the primary text tier, restated so the ramp reads whole
	CodeKeywordHex  = "#C9BDE5" // indigo
	CodeStringHex   = "#ADE0BE" // leaf
	CodeNumberHex   = "#E8CFA4" // sand
	CodeCommentHex  = "#838A9E" // slate — the one receding slot
	CodeFunctionHex = "#A8D6EA" // sky
	CodeTypeHex     = "#E8B4CE" // orchid
)

// Prose glyphs (5.17). Both are bytes the vocabulary already ships elsewhere,
// named again here because a slot is a MEANING and not a byte: a renderer that
// reached for [GlyphSeparator] to draw a list bullet would be one rename away
// from drawing telemetry dots down the left margin. glyph_test.go pins each of
// these equal to the glyph it shares a byte with, so the duplication can never
// become a drift.
const (
	// GlyphProseBullet is the mark on an unordered list item. It is the middle
	// dot rather than U+2022 because the middle dot is already measured and
	// already shipping in this tree, and because 5.13 asks structure to be the
	// quietest thing on the row: the INDENT is what says "list", the mark only
	// says "item".
	GlyphProseBullet = "·"
	// GlyphProseQuote is the gutter bar down the left of a blockquote —
	// structure without boxes (5.21), one column wide.
	//
	// IT IS THE HAIRLINE AND NO LONGER THE BOX RULE. It was `│` — the same byte
	// the spawn tree's trunk ([GlyphTreeVert]) and every vertical rule in the
	// product are drawn from — and a blockquote sat at the left margin of the
	// chat feed while the margin column's own divider ran down the right of the
	// very same rows, so on a screen with no other vertical rules two `│`
	// columns meaning unrelated things read as one broken frame. The fence
	// beside it already owned the right mark for "this block is set apart", so
	// the quote takes [GlyphCodeGutter]'s byte instead: an eighth of a cell,
	// which is a margin and cannot be mistaken for a frame.
	GlyphProseQuote = "▏"
	// GlyphCodeGutter is the hairline down the left of a fenced code block. It
	// is one eighth of a cell of ink: enough to bound the block on the side the
	// eye returns to, and far too little to read as a border. There is no line
	// number beside it — a number column is a second thing to read in a region
	// the reader opened in order to read something else.
	GlyphCodeGutter = "▏"
)

// codeEntry is one row of the ramp, shaped like [entry] in palette.go: base and
// dimmed values, and the SGR sequence for every profile and focus computed once
// at package initialization so a render appends a constant string.
type codeEntry struct {
	name  string
	class Class
	color [focusCount]Color
	sgr   [profileCount][focusCount]string
}

var codeTable = buildCodeTable()

func buildCodeTable() [codeSlotCount]codeEntry {
	var t [codeSlotCount]codeEntry
	set := func(s CodeSlot, name string, class Class, hex string) {
		base := MustHex(hex)
		t[s] = codeEntry{
			name:  name,
			class: class,
			color: [focusCount]Color{base, Mix(base, groundBase, DimTowardGround)},
		}
	}
	set(CodeText, "code.text", ClassBody, CodeTextHex)
	set(CodeKeyword, "code.keyword", ClassBody, CodeKeywordHex)
	set(CodeString, "code.string", ClassBody, CodeStringHex)
	set(CodeNumber, "code.number", ClassBody, CodeNumberHex)
	set(CodeComment, "code.comment", ClassChrome, CodeCommentHex)
	set(CodeFunction, "code.function", ClassBody, CodeFunctionHex)
	set(CodeType, "code.type", ClassBody, CodeTypeHex)

	for i := range t {
		e := &t[i]
		for f := Focus(0); f < focusCount; f++ {
			c := e.color[f]
			// Below 256 colours the ramp does not exist; see the file comment.
			e.sgr[NoColor][f] = ""
			e.sgr[ANSI16][f] = ""
			e.sgr[ANSI256][f] = "\x1b[38;5;" + itoa(int(nearest256(c))) + "m"
			e.sgr[TrueColor][f] = "\x1b[38;2;" +
				itoa(int(c.R)) + ";" + itoa(int(c.G)) + ";" + itoa(int(c.B)) + "m"
		}
	}
	return t
}

func (s CodeSlot) check() CodeSlot {
	if s >= codeSlotCount {
		panic("tokens: invalid CodeSlot")
	}
	return s
}

// String is the slot's stable name ("code.keyword"). Golden tests key on these,
// so they are part of the API.
func (s CodeSlot) String() string { return codeTable[s.check()].name }

// Class is the contrast contract this slot signs.
func (s CodeSlot) Class() Class { return codeTable[s.check()].class }

// Color returns the slot's value in a focus state.
func (s CodeSlot) Color(f Focus) Color { return codeTable[s.check()].color[f.check()] }

// Hex returns the slot's value as "#RRGGBB".
func (s CodeSlot) Hex(f Focus) string { return s.Color(f).Hex() }

// Fg is the precomputed SGR sequence that sets this slot as the foreground. It
// is "" under every profile where [CodeHighlighting] is false, which is what
// makes "ask the profile, then paint" and "just paint" agree: a caller that
// forgets to ask still cannot emit a colour that does not exist.
func (s CodeSlot) Fg(p Profile, f Focus) string {
	return codeTable[s.check()].sgr[p.check()][f.check()]
}

// CodeHighlighting reports whether syntax colour survives this profile. A
// renderer that gets false must draw the whole block at the primary text tier
// rather than pick six of the classic sixteen — see the file comment.
func CodeHighlighting(p Profile) bool { return p >= ANSI256 }

// CodeSlots returns every slot in ramp order, for the contrast walk and for a
// palette-preview screen.
func CodeSlots() []CodeSlot {
	out := make([]CodeSlot, 0, codeSlotCount)
	for s := CodeSlot(0); s < codeSlotCount; s++ {
		out = append(out, s)
	}
	return out
}
