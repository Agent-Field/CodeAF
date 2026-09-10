package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// iconvocab_test.go holds the words this surface's tests read the shared
// vocabulary with, and the one law that is about THIS package's names.
//
// THE PRODUCT-WIDE LAW MOVED. The sweep that refuses a mark spelled as a
// literal used to live here and covered internal/tui3 alone, which left the
// older window and the resident free to spell their own — and they did.
// internal/iconlaw is where it lives now, walking every surface package on one
// list; docs/design/icons/DESIGN.md is the law's own page.

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
