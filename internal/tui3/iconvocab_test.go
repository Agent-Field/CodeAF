package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// iconvocab_test.go holds THE ICON LAW and the words the tests read it with.
//
// One vocabulary draws every mark a person sees on this surface — the task
// states, the action families in the step gutter, the chrome — and it lives in
// internal/tui2/tokens with three spellings per slot: the Font Awesome 4 icon a
// patched font draws, the geometric floor every terminal draws, and the one
// ASCII character a screen reader can name. docs/design/icons/DESIGN.md is the
// law's own page; what follows is the part a build can fail on.

// The task-state marks as the PLAIN tier spells them, which is the tier a test
// palette draws in ([newPalette] leaves [palette.icons] at [tokens.Plain]).
// They are the vocabulary's own constants under this package's older names, so
// a test says `glyphDone` and means the one slot every surface draws.
const (
	glyphQueued  = tokens.GlyphQueued
	glyphRunning = tokens.GlyphWorking
	glyphDone    = tokens.GlyphSettled
	glyphBad     = tokens.GlyphFailed
	glyphStopped = tokens.GlyphStopped
	glyphAsk     = tokens.GlyphNeedsHuman
	glyphPaused  = tokens.GlyphPaused
	glyphWaitsOn = tokens.GlyphWaitsOn
)

// And the same eight as the screen-reader tier spells them.
var (
	glyphQueuedASCII  = tokens.ASCII.Glyph(tokens.GQueued)
	glyphRunningASCII = tokens.ASCII.Glyph(tokens.GWorking)
	glyphDoneASCII    = tokens.ASCII.Glyph(tokens.GSettled)
	glyphBadASCII     = tokens.ASCII.Glyph(tokens.GFailed)
	glyphStoppedASCII = tokens.ASCII.Glyph(tokens.GStopped)
	glyphPausedASCII  = tokens.ASCII.Glyph(tokens.GPaused)
)

// palOf is a test palette at a glyph floor, for the drawing functions that are
// handed a palette and nothing else.
func palOf(ascii bool) palette { return newPalette(tokens.TrueColor, ascii) }

// iconLawRunes are the marks this surface may not spell for itself, with the
// slot each one belongs to. They are the SHAPES — the vocabulary gives up its
// claim on its ASCII slots ("?", "=", "+", "$", "/") because those are
// characters a line carries for a hundred honest reasons, and a gate that
// failed on them would be a gate people learn to ignore.
//
// The four at the end are RETIRED: they were this surface's own spellings for
// states the vocabulary already had a mark for, and a build that finds one has
// found a surface drawing outside the table again.
var iconLawRunes = map[rune]string{
	'○': "tokens.GQueued",
	'◐': "tokens.GWorking",
	'✓': "tokens.GSettled",
	'✕': "tokens.GFailed",
	'■': "tokens.GStopped",
	'⚑': "tokens.GWaitsOn",
	'▤': "tokens.GActionRead",
	'◎': "tokens.GActionTest",
	'↗': "tokens.GActionBrowse",
	'⇄': "tokens.GActionTransfer",
	'⇉': "tokens.GActionCoordinate",
	'◷': "tokens.GActionWait",
	'▪': "tokens.GActionWork",
	'⌕': "tokens.GSearch",
	'✎': "tokens.GWrite",
	'◌': "tokens.GQueued (retired: the surface's own queued circle)",
	'⊘': "tokens.GStopped (retired: the surface's own stop mark)",
	'✗': "tokens.GFailed (retired: the surface's own cross)",
	'⏸': "tokens.GPaused (retired, and BANNED by tokens.BannedGlyphs)",
}

// TestNoSurfaceSpellsAnIconItself is the law, enforced rather than remembered.
//
// A MARK SPELLED AS A LITERAL DRAWS THE PLAIN FLOOR FOREVER. It cannot know
// which repertoire the terminal is on, so the day somebody writes `✓` into a row
// is the day that row stops upgrading with the rest of the surface — which is
// exactly how a person with a patched font came to see proper icons beside their
// tool calls and bare geometric shapes beside their tasks. The private-use range
// is refused for the other half of the same reason: an icon spelled here is one
// the width gate never measured and the pinned Nerd Fonts release never
// verified.
//
// It reads string and character literals only. A comment may draw all the marks
// it likes — several do, because a table in prose is how this codebase explains
// itself.
func TestNoSurfaceSpellsAnIconItself(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
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
			text, ok := iconLiteralText(lit)
			if !ok {
				return true
			}
			for _, r := range text {
				if r >= 0xE000 && r <= 0xF8FF {
					t.Errorf("%s: the literal %s spells %U, a private-use codepoint. Every icon "+
						"is a binding in internal/tui2/tokens with its Font Awesome name pinned "+
						"against the release; ask for the SLOT through palette.glyph or app.icon",
						fset.Position(lit.Pos()), lit.Value, r)
					continue
				}
				if slot, owned := iconLawRunes[r]; owned {
					t.Errorf("%s: the literal %s spells %U, which is %s. Ask for the slot through "+
						"palette.glyph or app.icon so the line gets this terminal's repertoire "+
						"(docs/design/icons/DESIGN.md)",
						fset.Position(lit.Pos()), lit.Value, r, slot)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/tui3: %v", err)
	}
}

// TestTheStateMarksAreDeclaredOnceAndComeFromTheTable is the other half: the
// names, not the bytes. A surface that reintroduced `glyphStopped = "⊘"` as a
// constant somewhere would pass the literal sweep the day somebody spelled it
// with an escape, so the DECLARATIONS are pinned too — every one of the retired
// names must stay retired, and no file may declare a mark of its own.
func TestTheStateMarksAreDeclaredOnceAndComeFromTheTable(t *testing.T) {
	retired := map[string]bool{
		"glyphQueued": true, "glyphQueuedASCII": true,
		"glyphRunning": true, "glyphRunningASCII": true,
		"glyphDone": true, "glyphDoneASCII": true,
		"glyphBad": true, "glyphBadASCII": true,
		"glyphStopped": true, "glyphStoppedASCII": true,
		"glyphAsk": true, "glyphPaused": true, "glyphPausedASCII": true,
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, name := range spec.Names {
				if retired[name.Name] {
					t.Errorf("%s: %s is back. The task states are slots of the shared "+
						"vocabulary now, and tasktier.go's tierSlot is where a reading picks one",
						fset.Position(name.Pos()), name.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/tui3: %v", err)
	}
}

// iconLiteralText unquotes a literal, reporting false for one it cannot read —
// a malformed literal is the compiler's business, not this test's.
func iconLiteralText(lit *ast.BasicLit) (string, bool) {
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
	if !utf8.ValidString(s) {
		return "", false
	}
	return s, true
}
