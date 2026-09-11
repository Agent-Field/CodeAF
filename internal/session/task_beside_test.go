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
// reading — and that name must be ended IN THE FILE THAT STARTED IT, or the
// goroutine behind it has no join.
//
// THE FILE IS THE KEY, NOT THE BARE NAME. A reading and its join are two halves
// of one owner and live together; keying on the name alone let a field called
// `reading` on a type that never ends it pass because some other type's
// `reading` was ended somewhere else in the package. The same trap is closed a
// second way: a name that holds a reading may be declared by ONE type, so two
// owners cannot share a spelling and cover for each other.
func TestEveryReadingBesideTheWorkHasAJoin(t *testing.T) {
	_, files := packageSources(t)
	type join struct{ path, name string }
	started := map[join]bool{}
	ended := map[join]bool{}
	holders := map[string]string{}
	for path, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.TypeSpec:
				structure, ok := node.Type.(*ast.StructType)
				if !ok {
					return true
				}
				for _, field := range structure.Fields.List {
					if star, ok := field.Type.(*ast.StarExpr); !ok || calledName(star.X) != "besideWork" {
						continue
					}
					for _, name := range field.Names {
						if owner, taken := holders[name.Name]; taken && owner != node.Name.Name {
							t.Errorf("both %s and %s hold a reading beside the work under the name %q: "+
								"one spelling, one owner, or a join written for one counts for the other",
								owner, node.Name.Name, name.Name)
						}
						holders[name.Name] = node.Name.Name
					}
				}
			case *ast.AssignStmt:
				for index, right := range node.Rhs {
					call, ok := right.(*ast.CallExpr)
					if !ok || calledName(call.Fun) != "beside" || index >= len(node.Lhs) {
						continue
					}
					started[join{path, heldAs(node.Lhs[index])}] = true
				}
			case *ast.CallExpr:
				if selector, ok := node.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "end" {
					ended[join{path, heldAs(selector.X)}] = true
				}
			}
			return true
		})
	}
	if len(started) == 0 {
		t.Fatal("no reading beside the work was found, so this law is reading the wrong source")
	}
	for reading := range started {
		if reading.name == "" || !ended[reading] {
			t.Errorf("%s starts a reading beside the work as %q and nothing in that file ever ends it: "+
				"a reading with no join outlives the node it was read for (task_beside.go)", reading.path, reading.name)
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

// WHAT IS DONE UNDER THE HANDOVER LOCK IS BOUNDED (agent.go's law above
// [Agent.handOverTaskNews]). The parked runner takes that lock on every pass, so
// a holder may do local work whose end this process decides, and may not wait on
// a model, on the network or on a person — no matter what that would make
// atomic.
//
// IT IS A LIST AND NOT A SEARCH, because a call graph built from names alone
// cannot tell this package's `Run` from another type's and answers a chain for
// anything. So every call a holder makes under the hold is written down here
// with the reason it is bounded, and adding one is a line in this law: the
// person adding it has to say what ends it. A name that waits on the world
// cannot be given such a reason, which is the whole of the law.
func TestWhatIsDoneUnderTheHandoverLockIsBounded(t *testing.T) {
	bounded := map[string]string{
		"len":                 "a builtin",
		"settled":             "the delivery's own first write, handed in by the caller: the law above holds every caller of handOverTaskNews to a mark and nothing more",
		"Lock":                "the lock itself",
		"Unlock":              "the lock itself",
		"childrenOutstanding": "a read of the graph under its own lock",
		"taskNewsOwed":        "a read of one queue under its own lock",
		"postTaskNews":        "one write to the steering queue",
		"admissible":          "a read of the weighed division in hand",
		"drop":                "a field written on the weighed division in hand",
		"Err":                 "a read of the reading's own context",
		"speaker":             "a read of the room under its own lock",
		"stopWork":            "the run's cancel, which returns at once and is waited for elsewhere",
		"graph":               "a read of the agent's own field",
		"children":            "a read of the graph under its own lock",
		"admitDivision":       "the claim, the family freeze and the admits: local work and git on the node's own copy, no model and no person",
		"withReport":          "string joining",
		"afterParts":          "a read of the drawing in hand",
		"enqueueNote":         "one write to the steering queue",
		"briefNote":           "a value built in memory",
	}
	_, files := packageSources(t)
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || !holdsTheHandover(function) {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := calledName(call.Fun)
				if _, known := bounded[name]; name != "" && !known {
					t.Errorf("%s holds the handover lock and calls %s, which this law does not know to be "+
						"bounded: say here what ends it, or move it out of the hold — the parked runner takes "+
						"this lock on every pass (agent.go, above handOverTaskNews)", function.Name.Name, name)
				}
				return true
			})
		}
	}
}

// holdsTheHandover answers whether a function takes `handover` itself. Every
// holder does it the same way, at the top of its own body, which is what makes
// the law above readable off the source.
func holdsTheHandover(function *ast.FuncDecl) bool {
	held := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Lock" &&
			calledName(selector.X) == "handover" {
			held = true
		}
		return true
	})
	return held
}
