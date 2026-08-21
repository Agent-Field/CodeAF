package prose

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	gtext "github.com/yuin/goldmark/text"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Fenced code.
//
// chroma does the lexing and OUR style does the colour. The two are joined the
// only way that keeps both honest: [codeStyle] is a real chroma style built
// from the ramp in internal/tui2/tokens, chroma's own inheritance decides which
// of the six pastels a lexer token inherits, and the answer is then read back
// as a [tokens.CodeSlot] so the token layer — not chroma's terminal formatter —
// decides how that pastel degrades. chroma never writes a byte to the screen.
//
// That indirection is worth one map: chroma knows roughly two hundred token
// types and how they relate, and re-deriving that relationship as a switch here
// would be a second, worse copy of a table someone else maintains.

// codeStyle is the ramp expressed as a chroma style. Only the seven colours the
// ramp declares appear on the right-hand side — there is no eighth colour in
// this file, and [slotFor] would not know what to do with one.
var codeStyle = chroma.MustNewStyle("aforge", chroma.StyleEntries{
	chroma.Background: tokens.CodeTextHex,
	chroma.Text:       tokens.CodeTextHex,
	chroma.Error:      tokens.CodeTextHex,
	chroma.Other:      tokens.CodeTextHex,

	chroma.Keyword:     tokens.CodeKeywordHex,
	chroma.KeywordType: tokens.CodeTypeHex,

	chroma.Name:                 tokens.CodeTextHex,
	chroma.NameBuiltin:          tokens.CodeTypeHex,
	chroma.NameBuiltinPseudo:    tokens.CodeKeywordHex,
	chroma.NameClass:            tokens.CodeTypeHex,
	chroma.NameException:        tokens.CodeTypeHex,
	chroma.NameFunction:         tokens.CodeFunctionHex,
	chroma.NameFunctionMagic:    tokens.CodeFunctionHex,
	chroma.NameDecorator:        tokens.CodeFunctionHex,
	chroma.NameTag:              tokens.CodeKeywordHex,
	chroma.NameAttribute:        tokens.CodeFunctionHex,
	chroma.NameConstant:         tokens.CodeNumberHex,
	chroma.NameNamespace:        tokens.CodeTypeHex,
	chroma.NameEntity:           tokens.CodeTypeHex,
	chroma.NameLabel:            tokens.CodeFunctionHex,
	chroma.NameVariableMagic:    tokens.CodeKeywordHex,
	chroma.NameVariableGlobal:   tokens.CodeTypeHex,
	chroma.NameOther:            tokens.CodeTextHex,
	chroma.NameProperty:         tokens.CodeTextHex,
	chroma.NameVariable:         tokens.CodeTextHex,
	chroma.NameVariableClass:    tokens.CodeTextHex,
	chroma.NameVariableInstance: tokens.CodeTextHex,

	chroma.Literal:               tokens.CodeStringHex,
	chroma.LiteralString:         tokens.CodeStringHex,
	chroma.LiteralStringEscape:   tokens.CodeNumberHex,
	chroma.LiteralStringInterpol: tokens.CodeTextHex,
	chroma.LiteralNumber:         tokens.CodeNumberHex,
	chroma.LiteralDate:           tokens.CodeNumberHex,

	chroma.Operator:        tokens.CodeTextHex,
	chroma.OperatorWord:    tokens.CodeKeywordHex,
	chroma.Punctuation:     tokens.CodeTextHex,
	chroma.Comment:         tokens.CodeCommentHex,
	chroma.CommentPreproc:  tokens.CodeKeywordHex,
	chroma.Generic:         tokens.CodeTextHex,
	chroma.GenericHeading:  tokens.CodeKeywordHex,
	chroma.GenericDeleted:  tokens.CodeStringHex,
	chroma.GenericInserted: tokens.CodeStringHex,
})

// slotByColour inverts [codeStyle]: the six pastels plus the body tier, keyed
// by the value chroma resolved. It is total over what codeStyle can produce,
// and anything outside that set is the body tier — the safe answer, since the
// body tier is what an unhighlighted block draws anyway.
var slotByColour = buildSlotIndex()

func buildSlotIndex() map[chroma.Colour]tokens.CodeSlot {
	m := make(map[chroma.Colour]tokens.CodeSlot, len(tokens.CodeSlots()))
	for _, pair := range []struct {
		hex  string
		slot tokens.CodeSlot
	}{
		{tokens.CodeTextHex, tokens.CodeText},
		{tokens.CodeKeywordHex, tokens.CodeKeyword},
		{tokens.CodeStringHex, tokens.CodeString},
		{tokens.CodeNumberHex, tokens.CodeNumber},
		{tokens.CodeCommentHex, tokens.CodeComment},
		{tokens.CodeFunctionHex, tokens.CodeFunction},
		{tokens.CodeTypeHex, tokens.CodeType},
	} {
		m[chroma.MustParseColour(pair.hex)] = pair.slot
	}
	return m
}

// slotIndex is [slotByColour] composed with codeStyle's inheritance, resolved
// once for every token type chroma knows. Building it at initialization means a
// render never walks a style's ancestry and never writes to a shared map — the
// second of which matters, because a TUI renders from one goroutine today and
// has no business being unable to render from two.
var slotIndex = buildSlotTable()

func buildSlotTable() map[chroma.TokenType]tokens.CodeSlot {
	m := make(map[chroma.TokenType]tokens.CodeSlot, len(chroma.StandardTypes))
	for tt := range chroma.StandardTypes {
		if slot, ok := slotByColour[codeStyle.Get(tt).Colour]; ok {
			m[tt] = slot
		}
	}
	return m
}

func slotFor(tt chroma.TokenType) tokens.CodeSlot {
	if slot, ok := slotIndex[tt]; ok {
		return slot
	}
	if slot, ok := slotByColour[codeStyle.Get(tt).Colour]; ok {
		return slot
	}
	return tokens.CodeText
}

// code renders a fenced or indented block: a hairline gutter, one cell of
// padding, the source, and padding out to the block's right edge — all of it on
// the [tokens.Sheet] plane where the profile has one.
//
// Long lines are TRUNCATED, never wrapped. Indentation is how source code is
// read, and a wrapped line puts a continuation at column zero where the eye is
// counting levels; one row per source line keeps the shape and leaves the
// cropping to a caller that can scroll.
func (r *renderer) code(lines *gtext.Segments, lang string) {
	if lines == nil || lines.Len() == 0 {
		return
	}
	var src strings.Builder
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		src.Write(seg.Value(r.src))
	}
	text := strings.TrimRight(src.String(), "\n")
	if text == "" {
		return
	}

	// gutter + one cell of padding on each side.
	inner := atLeastOne(r.figureWidth() - 3)
	gutter := piece{text: tokens.GlyphCodeGutter, st: style{tok: tokens.TextTertiary}}
	ground := style{tok: tokens.TextPrimary, code: true, ground: true}
	space := piece{text: " ", st: ground}

	for _, line := range r.highlight(text, lang, ground) {
		row := make([]piece, 0, len(line)+4)
		row = append(row, gutter, space)
		used := 0
		for _, pc := range line {
			w := cells(pc.text)
			if used+w > inner {
				if cut := truncate(pc.text, inner-used); cut != "" {
					row = append(row, piece{text: cut, st: pc.st})
					used = inner
				}
				break
			}
			row = append(row, pc)
			used += w
		}
		row = append(row, piece{text: spaces(inner - used + 1), st: ground})
		r.emitRaw(row)
	}
}

// highlight splits source into rows of styled pieces. Below 256 colours it does
// no lexing at all: [tokens.CodeHighlighting] says the ramp does not exist
// there, and tokenising in order to throw the answer away would be work spent
// on a screen nobody sees.
func (r *renderer) highlight(src, lang string, ground style) [][]piece {
	return highlightPieces(r.p, src, lang, ground)
}

// HighlightLine paints ONE line of source as lang, for a surface composing a
// row this package does not own.
//
// It exists so that chroma keeps living in exactly one place. The record's tool
// rows want a shell command coloured the way a fenced block is coloured (§5:
// "shell commands syntax-highlighted at the dim end of the pastel ramp"), and
// the alternative to this function was a second copy of [codeStyle] — a table
// of two hundred token relationships, maintained twice, drifting once.
//
// What comes back is a PAINTED SPAN and nothing else: no gutter, no ground, no
// wrapping, no truncation, and every newline flattened to a space, because the
// caller is putting this inside a row whose width it is measuring itself.
// Under a profile with no code ramp ([tokens.CodeHighlighting]) it returns the
// scrubbed text unpainted, which is the same degradation a fenced block makes
// and for the same reason.
//
// tier is the span's OWN GREY, and it is load-bearing in two places. It is what
// the whole span falls back to under a profile with no ramp — and it is also
// what every run chroma had nothing to say about is painted at, instead of the
// code body tier. That is the difference between "highlighted" and "promoted":
// a fenced block owns its rectangle and may set the ink inside it, while a span
// inside somebody else's row must keep that row's voice and light up only where
// the lexer actually found something (a keyword, a string, a number). §5 asks
// for shell commands "syntax-highlighted at the dim end of the pastel ramp",
// and the dim end is exactly this: colour where there is meaning, the caller's
// quiet tier everywhere else.
func HighlightLine(s *tokens.Styler, src, lang string, tier tokens.Token) string {
	p := newPainter(s)
	line := strings.ReplaceAll(scrubCode(src), "\n", " ")
	if line == "" {
		return ""
	}
	base := style{tok: tier, code: true}
	if p.plain() || !tokens.CodeHighlighting(p.profile) {
		return p.paint(line, style{tok: tier})
	}
	rows := highlightPieces(p, line, lang, base)
	if len(rows) == 0 {
		return p.paint(line, style{tok: tier})
	}
	var b strings.Builder
	b.Grow(len(line) + 48)
	for _, pc := range rows[0] {
		if pc.st.slot == tokens.CodeText {
			// Nothing the lexer wanted to name: the caller's tier, not the code
			// body tier, so the row keeps the voice it had.
			b.WriteString(p.paint(pc.text, style{tok: tier}))
			continue
		}
		b.WriteString(p.paint(pc.text, pc.st))
	}
	return b.String()
}

// LexerName is chroma's own name for the language a FILE is written in — the
// word [HighlightLine] takes as its lang — or "" when nothing in chroma's
// registry claims that filename.
//
// It exists so a caller holding a PATH rather than a fence's info string can
// still reach the one highlighter in this tree. internal/tui3 draws the body of
// a `write` call and the text a `read` returned, and the only thing either of
// those carries about its language is the file's own name; the alternative to
// this function was that package importing chroma, which is the thing this
// file's opening paragraph exists to prevent.
//
// The empty answer is load-bearing: a caller gets to say "nothing here is
// source" and fall back to whatever it drew before, rather than have chroma's
// fallback lexer paint a log file as if it were code.
//
// IT IS MEMOISED, and it has to be. chroma's own comment on Match says it walks
// every file pattern of every lexer and is not fast, and the callers are drawing
// terminal rows at thirty frames a second. The table is keyed by the name it was
// asked about, so it is bounded by the files one session touched.
func LexerName(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	lexerNames.mu.RLock()
	name, known := lexerNames.byFile[filename]
	lexerNames.mu.RUnlock()
	if known {
		return name
	}
	name = ""
	if lexer := lexers.Match(filename); lexer != nil {
		name = lexer.Config().Name
	}
	lexerNames.mu.Lock()
	if lexerNames.byFile == nil {
		lexerNames.byFile = make(map[string]string, 32)
	}
	lexerNames.byFile[filename] = name
	lexerNames.mu.Unlock()
	return name
}

var lexerNames struct {
	mu     sync.RWMutex
	byFile map[string]string
}

// highlightPieces is the lexing itself, free of a renderer so both the fenced
// block and [HighlightLine] reach it.
func highlightPieces(p painter, src, lang string, ground style) [][]piece {
	plain := strings.Split(scrubCode(src), "\n")
	if !tokens.CodeHighlighting(p.profile) {
		out := make([][]piece, len(plain))
		for i, line := range plain {
			out[i] = []piece{{text: line, st: ground}}
		}
		return out
	}

	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Analyse(src)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, scrubCode(src))
	if err != nil || it == nil {
		out := make([][]piece, len(plain))
		for i, line := range plain {
			out[i] = []piece{{text: line, st: ground}}
		}
		return out
	}

	out := [][]piece{nil}
	for tok := it(); tok != chroma.EOF; tok = it() {
		st := ground
		st.slot = slotFor(tok.Type)
		// A lexer's value can span rows; a row is this package's unit, so the
		// split happens here and not at the ceiling, where a stray newline
		// would already have corrupted the frame.
		parts := strings.Split(tok.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				out = append(out, nil)
			}
			if part == "" {
				continue
			}
			out[len(out)-1] = append(out[len(out)-1], piece{text: part, st: st})
		}
	}
	return out
}

// scrubCode is [scrub] for source text, with the one difference that matters
// inside a fence: newlines survive, because they are the row boundaries, and a
// tab becomes FOUR spaces rather than one. A tab in prose is an accident; a tab
// in code is an indent level, and collapsing it to a single cell would flatten
// the nesting the reader opened the block to see.
func scrubCode(s string) string {
	if !strings.ContainsAny(s, "\t\x00\r") && !hasControl(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 0x20 && c != '\n') || c == 0x7f {
			return true
		}
	}
	return false
}
