package tui3

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"testing"
)

// TestNoTermOfTheWaitClockIsAlsoATermOfTheOtherOne pins the reduction that makes the
// slow paint cadence mean anything.
//
// [app.paint] splits its liveness terms in two: waitLive, the wait on a model, and
// otherLive, everything else that moves on its own. The slow cadence fires on
// `waitLive && !otherLive`. So a term that appears in BOTH is not merely redundant, it is
// DEAD: if w is a disjunct of otherLive then w implies otherLive, so `waitLive &&
// !otherLive` reduces to the same expression with w struck out of waitLive, and
// `waitLive || otherLive` reduces likewise. The code then reads as though the slow
// cadence covers w when it cannot.
//
// That is exactly what shipped in the first draft of this split: a.waiting() was left in
// otherLive, where trunk already had it, and added to waitLive, where it could never
// change either expression. The bug is invisible at a glance and survives any behavioural
// test, because the behaviour is correct; only the reading is wrong. So this test reads
// the source.
func TestNoTermOfTheWaitClockIsAlsoATermOfTheOtherOne(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "app.go", nil, 0)
	if err != nil {
		t.Fatalf("parse app.go: %v", err)
	}

	text := func(n ast.Node) string {
		var b bytes.Buffer
		if err := printer.Fprint(&b, fset, n); err != nil {
			t.Fatalf("print node: %v", err)
		}
		return b.String()
	}

	// disjuncts flattens a chain of `a || b || c` into its leaves.
	var disjuncts func(ast.Expr) []ast.Expr
	disjuncts = func(e ast.Expr) []ast.Expr {
		if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.LOR {
			return append(disjuncts(b.X), disjuncts(b.Y)...)
		}
		if p, ok := e.(*ast.ParenExpr); ok {
			return disjuncts(p.X)
		}
		return []ast.Expr{e}
	}

	found := map[string][]ast.Expr{}
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		id, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		if id.Name == "waitLive" || id.Name == "otherLive" {
			found[id.Name] = disjuncts(as.Rhs[0])
		}
		return true
	})

	for _, name := range []string{"waitLive", "otherLive"} {
		if len(found[name]) == 0 {
			t.Fatalf("no assignment to %s in app.go: this test has lost its subject and must be "+
				"rewritten against whatever replaced the paint clock's two liveness terms, not deleted", name)
		}
	}

	other := map[string]bool{}
	for _, e := range found["otherLive"] {
		other[text(e)] = true
	}
	for _, e := range found["waitLive"] {
		s := text(e)
		if other[s] {
			t.Errorf("%s is a term of BOTH waitLive and otherLive, so it is dead: it cannot change "+
				"`waitLive && !otherLive` (the slow cadence) or `waitLive || otherLive` (whether the clock "+
				"runs at all). Either strike it from waitLive, which changes nothing and makes the code say "+
				"what it does, or strike it from otherLive, which is a real behaviour change on that path and "+
				"needs its own measurement before it ships.", s)
		}
	}
}
