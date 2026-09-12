package session

import (
	"go/ast"
	"testing"
)

// THE ARMED WORD HAS ONE WRITER OUTSIDE ADMISSION, and it is the door #958
// added.
//
// The word is what the belt and the prompt are BOTH built from
// ([Config.mayDivide] reads it live through [TaskNode.dividing]), so a second
// hand writing it somewhere else is exactly how the two would come to be
// rebuilt from different answers — the disagreement this road refuses to
// create. Three writers are right and no fourth is: admission, which is the one
// door every task comes through; the reload, which re-takes the same reading
// from the same text; and [TaskNode.armAfterAdmission], which arms work whose
// width was read beside its worker and refuses work that already carries a
// word.
func TestTheArmedWordHasOneWriterOutsideAdmission(t *testing.T) {
	allowed := map[string]string{
		"admit":             "admission itself",
		"restoreNode":       "a reload re-taking the reading from the same text",
		"armAfterAdmission": "the one door that arms work read for width beside its worker",
	}
	_, files := packageSources(t)
	for path, file := range files {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			if _, fine := allowed[function.Name.Name]; fine {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				assign, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, target := range assign.Lhs {
					field, ok := target.(*ast.SelectorExpr)
					if !ok || field.Sel.Name != "armed" {
						continue
					}
					spec, ok := field.X.(*ast.SelectorExpr)
					if !ok || spec.Sel.Name != "spec" {
						continue
					}
					t.Errorf("%s in %s writes the arming word, and it has three doors and no fourth: %v",
						function.Name.Name, path, allowed)
				}
				return true
			})
		}
	}
}
