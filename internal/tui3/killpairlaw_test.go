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
)

// THE TWO LINE KILLS ARE ONE GESTURE, AND EVERY BOX THAT HAS ONE HAS BOTH.
//
// `ctrl+u` kills to the start of the line and `ctrl+k` kills to the end of it.
// That is one thing a hand learned in a shell twenty years ago, not two, and a
// box that answers half of it is a box where the person's hand is wrong about
// where they are — which is exactly the defect editkeys.go's header was written
// about, one modifier over.
//
// IT IS A LAW BECAUSE THE KEY MAPS ARE HAND-ROLLED PER BOX and a hand-rolled map
// is a map that forgets. When `ctrl+k` came back to the draft (hop.go's
// [hopOpenKey]) it was added to the composer, to [listNavigate] — which is every
// typed list on the surface — and to the settings value editor, and it was
// MISSED on home's own box, on the errand pane, on the tasks and rewind filters
// and on the settings filter. Home is where a launch lands, so the first box
// anybody tried the new chord in was one of the five that did not have it. The
// surface has no shared kill vocabulary to add it to, so this is the thing that
// notices instead: it reads the package with go/ast and fails a key switch that
// answers one of the pair without the other.
//
// THE TWO EXCEPTIONS ARE NAMED RATHER THAN QUIETLY PASSED, because a law whose
// reach a reader cannot predict lies by omission. Both are forms that keep their
// text in a plain string with no caret in it — [setup] on the first-run screen
// and the onboarding panel's limit row — where `ctrl+u` clears the field and
// kill-to-the-end has nothing it could mean: the caret is always already at the
// end. A box that grows an [editor] grows off this list in the same change.
var killPairExempt = map[string]string{
	"firstrun.go":   "the first-run setup keeps its text in a plain string with no caret",
	"onboarding.go": "the onboarding limit row is a string field, not an editor",
}

func TestEveryBoxWithALineKillHasBothHalvesOfIt(t *testing.T) {
	const (
		toStart = "ctrl+u"
		toEnd   = "ctrl+k"
	)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if why, ok := killPairExempt[name]; ok {
			// AND THE EXEMPTION IS PROVEN RATHER THAN TRUSTED. A file named here
			// that has grown the other half no longer needs the excuse, and a
			// stale exemption is how a list like this rots into a place bugs hide.
			src, readErr := os.ReadFile(name)
			if readErr != nil {
				t.Fatalf("reading %s: %v", name, readErr)
			}
			if strings.Contains(string(src), `"`+toEnd+`"`) {
				t.Errorf("%s binds %s and is still excused from this law (%q) — delete its line", name, toEnd, why)
			}
			continue
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok || sw.Body == nil {
				return true
			}
			keys := switchKeyLiterals(sw)
			if !keys[toStart] {
				return true
			}
			checked++
			if !keys[toEnd] {
				t.Errorf("%s:%d answers %s and not %s — the line kills are one gesture and this box has half of it",
					name, fset.Position(sw.Pos()).Line, toStart, toEnd)
			}
			return true
		})
	}
	// AND THE WALK ITSELF IS ASSERTED. A law that silently matched nothing —
	// a renamed chord, a switch shape it stopped recognising — reports green
	// forever, which is the one failure a structural test must not have.
	if checked < 5 {
		t.Fatalf("the law found only %d key switches with a line kill in them; it has stopped seeing the shape it walks", checked)
	}
}

// switchKeyLiterals is every string constant a switch's cases compare against,
// which is exactly the chord spellings of a key switch and nothing else.
func switchKeyLiterals(sw *ast.SwitchStmt) map[string]bool {
	found := map[string]bool{}
	for _, stmt := range sw.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expr := range clause.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			if value, err := strconv.Unquote(lit.Value); err == nil {
				found[value] = true
			}
		}
	}
	return found
}
