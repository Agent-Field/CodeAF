// Package iconlaw is THE ICON LAW, and it is a package of nothing but the law.
//
// One vocabulary draws every mark a person sees in this product — the task
// states, the action families in a step gutter, the file kinds on a chip, the
// chrome — and it lives in internal/tui2/tokens with three spellings per slot:
// the Font Awesome 4 icon a patched font draws, the geometric floor every
// terminal draws, and the one ASCII character a screen reader can name.
// docs/design/icons/DESIGN.md is the law's own page; what follows is the part a
// build can fail on.
//
// IT LIVES IN ITS OWN PACKAGE BECAUSE IT IS NOT ONE SURFACE'S LAW. It landed as
// internal/tui3's own test and covered internal/tui3 only, which left the older
// window and the resident free to spell marks for themselves — and they did:
// v1 drew its own dotted circle for work waiting on a sibling, its own cross
// for failure, and its own flag for work waiting on a person, which is the
// character the vocabulary keeps for the OTHER kind of waiting. One list of
// packages here is how a fourth surface joins the law by being added to it,
// rather than by somebody remembering to copy a test.
package iconlaw

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
)

// surfaces are the packages held to the table: every package that draws marks
// on a screen a person looks at. A package joins this list the day its sweep
// lands, and never leaves it.
//
// internal/tui2 is deliberately NOT here. It holds the vocabulary itself, where
// the private-use codepoints are the table rather than a spelling of it, and its
// remaining component packages are swept by tokens' own
// TestConsumersDrawFromTheTable, whose list grows the same way this one does.
//
// internal/head and internal/resident are here even though the sweep found
// almost nothing in them, which is the point of a gate rather than an audit:
// the resident writes the lines a person reads on the board and in a brief, and
// nothing but this test stands between it and its own private spelling of the
// flag.
var surfaces = []string{
	"../tui",      // v1, and the visual north star
	"../tui3",     // v3, the live chat
	"../head",     // the resident's head
	"../resident", // the resident itself
}

// ownedRunes are the marks a surface may not spell for itself, with the slot
// each one belongs to. They are the SHAPES — the vocabulary gives up its claim
// on its ASCII slots ("?", "=", "+", "$", "/") because those are characters a
// line carries for a hundred honest reasons, and a gate that failed on them
// would be a gate people learn to ignore.
//
// The four at the end are RETIRED: they were a surface's own spelling for a
// state the vocabulary already had a mark for, and a build that finds one has
// found a surface drawing outside the table again.
var ownedRunes = map[rune]string{
	'○': "tokens.GQueued",
	'◐': "tokens.GWorking",
	'✓': "tokens.GSettled",
	'✕': "tokens.GFailed",
	'■': "tokens.GStopped",
	'⚑': "tokens.GWaitsOn",
	'▤': "tokens.GActionRead / tokens.GFileDocument",
	'◎': "tokens.GActionTest",
	'↗': "tokens.GActionBrowse",
	'⇄': "tokens.GActionTransfer",
	'⇉': "tokens.GActionCoordinate",
	'◷': "tokens.GActionWait",
	'▪': "tokens.GActionWork",
	'⌕': "tokens.GSearch",
	'✎': "tokens.GWrite",
	'⌾': "tokens.GFileImage",
	'♪': "tokens.GFileAudio",
	'◌': "tokens.GWaitsOn (retired: v1's own dotted circle for work waiting on a sibling)",
	'⊘': "tokens.GStopped (retired: a surface's own stop mark)",
	'✗': "tokens.GFailed (retired: a surface's own cross)",
	'⏸': "tokens.GPaused (retired, and BANNED by tokens.BannedGlyphs)",
}

// exemptions are the (file, rune) pairs allowed to spell a mark, with the
// reason each one is out. A reason is required: an exemption without one is how
// a list like this stops meaning anything. There is exactly one, and it is not
// a mark at all.
var exemptions = map[string]string{
	"internal/resident/bank.go⚑": "resident.NoteMark is a PROTOCOL BYTE, not a cell on a screen: " +
		"a job-board note is written with it and read back by prefix (cmd/aforge/chat.go strips it " +
		"before anybody sees the line). Routing it through the tier would make the bytes in the " +
		"journal depend on the terminal that wrote them, which is a data-format bug wearing a " +
		"design law's clothes",
}

// TestNoSurfaceSpellsAnIconItself is the law, enforced rather than remembered.
//
// A MARK SPELLED AS A LITERAL DRAWS THE PLAIN FLOOR FOREVER. It cannot know
// which repertoire the terminal is on, so the day somebody writes `✓` into a row
// is the day that row stops upgrading with the rest of the product — which is
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
	for _, surface := range surfaces {
		if _, err := os.Stat(surface); err != nil {
			t.Fatalf("%s is on the law's list and is not there: %v", surface, err)
		}
		fset := token.NewFileSet()
		err := filepath.WalkDir(surface, func(path string, d os.DirEntry, err error) error {
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
				text, ok := literalText(lit)
				if !ok {
					return true
				}
				for _, r := range text {
					if r >= 0xE000 && r <= 0xF8FF {
						t.Errorf("%s: the literal %s spells %U, a private-use codepoint. Every icon "+
							"is a binding in internal/tui2/tokens with its Font Awesome name pinned "+
							"against the release; ask for the SLOT through this surface's own door",
							fset.Position(lit.Pos()), lit.Value, r)
						continue
					}
					slot, owned := ownedRunes[r]
					if !owned {
						continue
					}
					if why, out := exemptions[repoPath(path)+string(r)]; out {
						t.Logf("%s: %U stands, and here is why: %s", repoPath(path), r, why)
						continue
					}
					t.Errorf("%s: the literal %s spells %U, which is %s. Ask for the slot through "+
						"this surface's own glyph door — palette.glyph or app.icon in internal/tui3, "+
						"Model.icon in internal/tui — so the line gets this terminal's repertoire "+
						"(docs/design/icons/DESIGN.md)",
						fset.Position(lit.Pos()), lit.Value, r, slot)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", surface, err)
		}
	}
}

// TestEveryExemptionIsStillReal keeps the carve-out list honest from the other
// side. An exemption that no longer names a file, or names a file that no longer
// spells the rune, is a line nobody will ever delete on purpose — and a stale
// carve-out is how the next real violation gets waved through.
func TestEveryExemptionIsStillReal(t *testing.T) {
	for key, why := range exemptions {
		r, size := utf8.DecodeLastRuneInString(key)
		path := key[:len(key)-size]
		body, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Errorf("the exemption for %s (%s) names a file that is not there: %v", path, why, err)
			continue
		}
		if !strings.ContainsRune(string(body), r) {
			t.Errorf("%s no longer spells %U: delete its exemption rather than leaving a "+
				"carve-out standing over nothing", path, r)
		}
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
	if !utf8.ValidString(s) {
		return "", false
	}
	return s, true
}

// repoPath spells a walked path the way the exemption table spells it — from
// the repository root — so a carve-out reads as the file a person would open.
func repoPath(path string) string {
	return "internal/" + strings.TrimPrefix(filepath.ToSlash(path), "../")
}
