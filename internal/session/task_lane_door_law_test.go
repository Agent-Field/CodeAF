package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// theLaneDoor is the pair of functions that move a lane, and the only place in
// this package where either count of one may move. Both names are here rather
// than in the assertion below so that the failure can say what the door is.
var theLaneDoor = map[string]string{
	"takeLaneLocked": "take",
	"giveLaneLocked": "give",
}

// A run worker has no graph-local count. Its gate writes only the process
// account, so these are the two additional doors that account for that road.
var runLaneDoor = map[string]string{
	"Started":  "take",
	"Returned": "give",
}

// A LANE IS TAKEN AND HANDED BACK THROUGH ONE DOOR, AND THE DOOR KEEPS BOTH
// COUNTS IN STEP.
//
// A node has two counts: its graph's, which task.parallel reads, and the
// process's ([TaskLanes]), which the machine reading uses. The node doors move
// both together. A run worker has no graph count, but still moves that same
// process account through its own admission gate. Every other write remains
// a breach of the one process-wide account.
//
// It reads the package's own source, so it is on the pull-request gate
// (scripts/laws.sh) and decides in well under a second.
func TestEveryLaneMovesThroughTheOneDoor(t *testing.T) {
	files := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package: %v", err)
	}
	// counter is every function that moves a graph's own count of running
	// lanes, and account every function that writes the process's.
	counter, account := map[string]bool{}, map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			where := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.IncDecStmt:
					if isField(node.X, "running") {
						counter[where] = true
					}
				case *ast.AssignStmt:
					for index, lhs := range node.Lhs {
						// A COUNT MOVES BY ARITHMETIC, and `a.running = true`
						// elsewhere in this package is a different field on a
						// different type: what is watched here is a number.
						if !isField(lhs, "running") {
							continue
						}
						if node.Tok != token.ASSIGN || (index < len(node.Rhs) && isNumber(node.Rhs[index])) {
							counter[where] = true
						}
					}
				case *ast.CallExpr:
					if call, ok := node.Fun.(*ast.SelectorExpr); ok && isField(call.X, "lanes") {
						if call.Sel.Name == "take" || call.Sel.Name == "give" {
							account[where] = true
						}
					}
				}
				return true
			})
		}
	}
	for where := range counter {
		if _, isDoor := theLaneDoor[where]; !isDoor {
			t.Errorf("%s moves a graph's count of running lanes itself. Every lane goes through takeLaneLocked or giveLaneLocked, "+
				"which are the only two places that can tell the process's account as well (task_pressure.go, #907).", where)
		}
	}
	for where := range account {
		_, nodeDoor := theLaneDoor[where]
		_, runDoor := runLaneDoor[where]
		if !nodeDoor && !runDoor {
			t.Errorf("%s writes the process's lane account directly, outside the door that keeps it in step with the graph's own count", where)
		}
	}
	// AND THE DOOR ITSELF DOES BOTH HALVES, which is the whole of what makes it
	// a door: a take that forgot the account would pass every check above.
	for door := range theLaneDoor {
		if !counter[door] {
			t.Errorf("%s no longer moves the graph's own count of running lanes", door)
		}
		if !account[door] {
			t.Errorf("%s no longer tells the process's lane account, so the two counts can drift apart", door)
		}
	}
	for door := range runLaneDoor {
		if !account[door] {
			t.Errorf("%s no longer tells the process's lane account for a run worker", door)
		}
		if counter[door] {
			t.Errorf("%s moves a graph count for a worker outside that graph", door)
		}
	}
}

// isField says whether an expression is a selector onto the named field, which
// is how both halves of the count are spelled at every site.
func isField(expr ast.Expr, name string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == name
}

// isNumber says whether an expression is a literal number, which is how a
// count is set outright rather than stepped.
func isNumber(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.INT
}
