package session

// NO READING OUTLIVES THE NODE THAT STARTED IT, AS TESTS (task_beside.go).
//
// Two halves. The runtime half pins what [besideWork.end] promises: the reading
// is cancelled and waited for, safely, however many roads call it. The structural
// half pins that every reading anybody starts HAS a join: a [beside] call whose
// result is never ended is a goroutine that runs past the node it was read for,
// and the next person to add a reading beside the work is the one this stops.
//
// And the rule the whole wave rests on, read off the source rather than trusted:
// the constructor of a task's worker makes no model call. It made one, for
// memory, and every worker a node built waited on it.

import (
	"context"
	"go/ast"
	"testing"
)

func TestAReadingBesideTheWorkIsCancelledAndWaitedFor(t *testing.T) {
	started := make(chan struct{})
	finished := false
	reading := beside(context.Background(), func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		finished = true
	})
	<-started
	reading.end()
	// Read without a lock on purpose: end() returning is the join, so the write
	// above happened before this read or the race detector says otherwise.
	if !finished {
		t.Fatal("end returned before the reading let go")
	}
	reading.end()
	var nobody *besideWork
	nobody.end()
}

// EVERY READING STARTED BESIDE THE WORK IS ENDED SOMEWHERE. The value [beside]
// answers is always kept under a name — a field on the thing that owns the
// reading — and that name must be ended in this package, or the goroutine behind
// it has no join.
func TestEveryReadingBesideTheWorkHasAJoin(t *testing.T) {
	_, files := packageSources(t)
	started := map[string]string{}
	ended := map[string]bool{}
	for path, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				for index, right := range node.Rhs {
					call, ok := right.(*ast.CallExpr)
					if !ok || calledName(call.Fun) != "beside" || index >= len(node.Lhs) {
						continue
					}
					started[heldAs(node.Lhs[index])] = path
				}
			case *ast.CallExpr:
				if selector, ok := node.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "end" {
					ended[heldAs(selector.X)] = true
				}
			}
			return true
		})
	}
	if len(started) == 0 {
		t.Fatal("no reading beside the work was found, so this law is reading the wrong source")
	}
	for name, path := range started {
		if name == "" || !ended[name] {
			t.Errorf("%s starts a reading beside the work as %q and nothing ever ends it: "+
				"a reading with no join outlives the node it was read for (task_beside.go)", path, name)
		}
	}
}

// THE WORKER'S CONSTRUCTOR ASKS NO MODEL ANYTHING. Whatever a worker is handed
// that needs a model — its memories, a division of its work — is read beside it
// and handed over when it lands, never waited for before the worker exists.
func TestATaskWorkersConstructorAsksNoModel(t *testing.T) {
	asks := map[string]bool{
		"memoryBlock": true, "routedMemory": true, "refreshMemory": true,
		"callRole": true, "callRoleChecked": true, "reviewDivision": true, "weighDivision": true,
	}
	_, files := packageSources(t)
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || function.Name.Name != "newTaskAgentOn" {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok && asks[calledName(call.Fun)] {
					t.Errorf("newTaskAgentOn calls %s: the worker's constructor must not wait on a model", calledName(call.Fun))
				}
				return true
			})
			return
		}
	}
	t.Fatal("newTaskAgentOn was not found, so this law is reading the wrong source")
}

// heldAs is the last name an expression is reached by: `reader` for m.reader,
// `reading` for sizing.reading or a local called reading.
func heldAs(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		return expression.Sel.Name
	}
	return ""
}
