package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// ── ONE FACT, TWO PACKAGES ──────────────────────────────────────────────────
//
// THE CONSTRAINT. `internal/session` keeps the newest answered questions in its
// book so that `c change` and `u undo` have something to render a revision
// against ([session.askSettledKept]). The surface is what OFFERS those keys, and
// it offers them on the receipts it is drawing ([questionRecordsKept]). So the
// engine's cap may never be smaller than the surface's count: a receipt on
// screen wearing `c change` whose entry the engine had already dropped is a key
// drawn as pressable that refuses.
//
// AND IT IS A LAW RATHER THAN A COMMENT BECAUSE THE TWO NUMBERS CANNOT BE MADE
// ONE. `internal/session` cannot import `internal/tui3` — the arrow points the
// other way — so the figure cannot be interpolated the way ONE SOURCE OF TRUTH
// asks for (CLAUDE.md). What can be done is this: read the other number out of
// the tree and fail when the relationship breaks, so the day somebody raises the
// surface's count the build says which other file has to move. The reason is
// lane A's (#914): the constant's own doc states the constraint, names this
// figure, and says which half of its own value is the rule and which half is a
// guess.
func TestTheEngineKeepsAtLeastAsManyReceiptsAsTheSurfaceDraws(t *testing.T) {
	kept := constInt(t, "../session", "askSettledKept")
	if kept < questionRecordsKept {
		t.Fatalf("session.askSettledKept is %d and this surface draws %d receipts, each offering [c] change and [u] undo — raise the engine's cap or lower questionRecordsKept, in the same commit",
			kept, questionRecordsKept)
	}
}

// constInt reads one integer constant out of a package's source.
//
// IT FAILS WHEN IT CANNOT FIND ONE, which is the whole of its value: a reading
// that quietly answered zero would turn this law into a green that ran nothing,
// which is the shape of #576.
func constInt(t *testing.T, dir, name string) int {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, id := range value.Names {
						if id.Name != name || i >= len(value.Values) {
							continue
						}
						lit, ok := value.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.INT {
							t.Fatalf("%s in %s is not a plain integer any more; this law reads the literal", name, dir)
						}
						n, err := strconv.Atoi(lit.Value)
						if err != nil {
							t.Fatalf("%s in %s reads %q: %v", name, dir, lit.Value, err)
						}
						return n
					}
				}
			}
		}
	}
	t.Fatalf("%s is not declared in %s any more — this law is guarding nothing; move it or delete it", name, dir)
	return 0
}
