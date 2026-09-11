package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ONE ACCOUNT OF AN ANSWER'S CASE, AND ONE RENDERER FOR WHAT IT DREW.
//
// An answer's evidence is drawn in four places — the pane beside a wide panel's
// list, the fold under a narrow panel's pointer, the page's right pane and the
// page's one column — and before questionevidence.go each place that drew the
// labelled case spelled it itself: the page wrote `then ·` and `would switch if`
// on its own and the panel wrote its one-line case on its own, and the two
// disagreed about whether a model's `If you…` was joined onto `would switch if`
// twice. These laws hold the seam shut:
//
//   - the words that label a case line are READ in questionevidence.go and
//     nowhere else (their declarations stand where the page's other words are
//     spelled), so a new drawing of the case has to go through [questionCase];
//   - the six block renderers are called by [app.questionBlockRows] and by each
//     other in questionblocks.go, and from no other file, so a new drawing of
//     evidence cannot grow a diagram painter of its own.

// questionCaseWords are the labels of an answer's case.
var questionCaseWords = []string{"questionThenWord", "questionWhyWord", "questionWouldSwitchWord", "questionConfidenceLabel"}

// questionBlockPainters are the renderers behind [app.questionBlockRows].
var questionBlockPainters = []string{
	"questionDiagramLines", "questionTableLines", "questionDiffLines",
	"questionImageLines", "questionLayoutLines",
}

func TestAnAnswersCaseIsSpelledInOnePlace(t *testing.T) {
	uses := questionIdentUses(t, append(append([]string{}, questionCaseWords...), questionBlockPainters...))
	for _, word := range questionCaseWords {
		for _, file := range uses[word] {
			if file != "questionevidence.go" {
				t.Errorf("%s reads %s: an answer's case is drawn through questionCase in questionevidence.go "+
					"and nowhere else, so every drawing of it agrees", file, word)
			}
		}
	}
	for _, painter := range questionBlockPainters {
		for _, file := range uses[painter] {
			if file != "questionblocks.go" {
				t.Errorf("%s calls %s: every block is drawn through questionBlockRows, "+
					"so a diagram looks the same wherever a question shows one", file, painter)
			}
		}
	}
}

// questionIdentUses maps each named identifier to the files (by base name) that
// USE it — a declaration of the name is not a use.
func questionIdentUses(t *testing.T, names []string) map[string][]string {
	t.Helper()
	want := map[string]bool{}
	for _, name := range names {
		want[name] = true
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		declared := map[*ast.Ident]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.ValueSpec:
				for _, name := range d.Names {
					declared[name] = true
				}
			case *ast.FuncDecl:
				declared[d.Name] = true
			}
			return true
		})
		seen := map[string]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || !want[id.Name] || declared[id] || seen[id.Name] {
				return true
			}
			seen[id.Name] = true
			out[id.Name] = append(out[id.Name], filepath.Base(path))
			return true
		})
	}
	return out
}
