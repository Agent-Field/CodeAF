package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// THE ROW'S WORD IS ONE VOCABULARY, AND THIS IS THE LAW THAT KEEPS IT ONE.
//
// [PlanTaskRow.StateWord] is the mapping from the store's words to the ones a
// person reads, and the digest and the `tasks` listing say it. The side list
// draws its own word through internal/tui3's planStateWord, which this package
// cannot call. So this reads that function out of the tree and holds the two to
// the same table: every store word it maps, and the stopped row, must read the
// same word here. The day planStateWord simply returns row.StateWord() there is
// nothing left to compare, and the law says so and passes.
func TestTheRowsWordIsTheRailsWord(t *testing.T) {
	path := filepath.Join(completerLawRoot(t), "internal", "tui3", "taskplan.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse the side list's word: %v", err)
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if candidate, ok := decl.(*ast.FuncDecl); ok && candidate.Name.Name == "planStateWord" {
			fn = candidate
		}
	}
	if fn == nil {
		t.Fatal("internal/tui3/taskplan.go has no planStateWord; point this law at the side list's word")
	}
	delegates := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "StateWord" {
				delegates = true
			}
		}
		return true
	})
	if delegates {
		t.Log("the side list says row.StateWord() itself: one mapping, nothing to compare")
		return
	}

	literal := func(expr ast.Expr) (string, bool) {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(lit.Value)
		return value, err == nil
	}
	returned := func(body []ast.Stmt) (string, bool) {
		for _, stmt := range body {
			if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
				return literal(ret.Results[0])
			}
		}
		return "", false
	}

	compared := 0
	for _, stmt := range fn.Body.List {
		switch stmt := stmt.(type) {
		case *ast.IfStmt:
			// `if row.Stopped { return "stopped" }`
			selector, ok := stmt.Cond.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Stopped" {
				continue
			}
			want, ok := returned(stmt.Body.List)
			if !ok {
				continue
			}
			if got := (PlanTaskRow{Stopped: true, Status: "cancelled"}).StateWord(); got != want {
				t.Errorf("a stopped row: the side list says %q and the row says %q", want, got)
			}
			compared++
		case *ast.SwitchStmt:
			for _, clause := range stmt.Body.List {
				cc := clause.(*ast.CaseClause)
				want, ok := returned(cc.Body)
				if !ok {
					continue
				}
				for _, expr := range cc.List {
					status, ok := literal(expr)
					if !ok {
						continue
					}
					if got := (PlanTaskRow{Status: status}).StateWord(); got != want {
						t.Errorf("store word %q: the side list says %q and the row says %q", status, want, got)
					}
					compared++
				}
			}
		}
	}
	// A word the side list has never heard of draws nothing there, and must
	// draw nothing here.
	if got := (PlanTaskRow{Status: "a-word-nobody-wrote"}).StateWord(); got != "" {
		t.Errorf("an unknown store word reads %q, want nothing", got)
	}
	if compared < 8 {
		t.Fatalf("the law compared %d words out of the side list's planStateWord; its shape moved, so this law is reading nothing", compared)
	}
}
