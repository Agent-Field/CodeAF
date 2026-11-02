package taskdoor_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// R7-17 enforces PERF.md at the measurement's source boundary: time is diagnostic.
func TestTheTaskCopyDeltaMeasurementNeverGatesOnWallClockSpeed(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "copy_test.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		branch, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		clock, fatal := false, false
		inspectClock := func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if method, ok := call.Fun.(*ast.SelectorExpr); ok {
					if owner, ok := method.X.(*ast.Ident); ok && owner.Name == "time" && method.Sel.Name == "Since" {
						clock = true
					}
				}
			}
			return true
		}
		if branch.Init != nil {
			ast.Inspect(branch.Init, inspectClock)
		}
		ast.Inspect(branch.Cond, inspectClock)
		ast.Inspect(branch.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if method, ok := call.Fun.(*ast.SelectorExpr); ok && (method.Sel.Name == "Fatal" || method.Sel.Name == "Fatalf" || method.Sel.Name == "Error" || method.Sel.Name == "Errorf") {
					fatal = true
				}
			}
			return true
		})
		if clock && fatal {
			t.Error("task-copy measurement fails on elapsed wall-clock time")
		}
		return true
	})
}
