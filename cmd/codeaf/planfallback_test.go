package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// A PLANNER'S RETURN IS TESTED FOR A GRAPH, NEVER FOR A FAULT.
//
// plan.Build reports the faults of every pass it ran alongside the graph it
// drew, and those two answer different questions. A surface that asked "did
// anything go wrong" and fell back to one worker threw away a sound six-node
// plan because one node's instruction came back cut — the whole goal then ran
// as a single oversized worker. The question the fall-back is for is whether a
// plan exists at all, so the statement right after every build asks exactly
// that, and this test is what keeps the two from being confused again.
func TestEveryPlanBuildIsGuardedOnTheGraphAndNotTheError(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	guarded := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			block, ok := node.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for index, statement := range block.List {
				assign, ok := statement.(*ast.AssignStmt)
				if !ok || !buildsAPlan(assign) || index+1 >= len(block.List) {
					continue
				}
				guarded++
				// The graph's own name, whatever the site calls it, tested
				// against nil and nothing else.
				graph, _ := assign.Lhs[0].(*ast.Ident)
				where := fileSet.Position(statement.Pos())
				if graph == nil {
					t.Errorf("%s: a plan build does not name its graph", where)
					continue
				}
				guard, ok := block.List[index+1].(*ast.IfStmt)
				if !ok || !isNilTest(guard.Cond, graph.Name) {
					t.Errorf("%s: the statement after plan.Build must be `if %s == nil`, so the "+
						"fall-back fires only when no plan was drawn; a build that reports a "+
						"fault still hands back the graph it drew", where, graph.Name)
				}
			}
			return true
		})
	}
	if guarded == 0 {
		t.Fatal("no plan.Build call was found to check; this test has stopped watching anything")
	}
}

// buildsAPlan reports that this assignment is the one that draws a plan.
func buildsAPlan(assign *ast.AssignStmt) bool {
	if len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
		return false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Build" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "plan"
}

// isNilTest reports that the condition is exactly `name == nil`.
func isNilTest(cond ast.Expr, name string) bool {
	binary, ok := cond.(*ast.BinaryExpr)
	if !ok || binary.Op != token.EQL {
		return false
	}
	left, leftOK := binary.X.(*ast.Ident)
	right, rightOK := binary.Y.(*ast.Ident)
	return leftOK && rightOK && left.Name == name && right.Name == "nil"
}
