package session

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"testing"
)

// THE STANDING GATE ON THE TWO FACTS A PARKED PARENT READS.
//
// A node that has handed work out — to divided parts, to forked hands — asks two
// questions of the instant it wakes in: is anything still outstanding, and is
// anything owed to the model that no request has carried. The delivery that
// answers them writes THREE things in a fixed order (the queue, the fact, the
// wake: [Agent.deliverTaskNote]), and the two facts live under two different
// locks. So a reader that asks the two questions separately can catch a delivery
// half-written, whichever order it asks them in — and it costs a turn either way:
// a model turn on an empty request one way round, a report left unread on the
// queue the other.
//
// It cost one measured turn already, and it was not found by review: the read
// site and the write site are two thousand lines apart and each is correct on
// its own page. So the pairing is held here instead, where a new reader of the
// pair has to say in words how it reads it.

// taskNewsPairReaders is every function in this package that asks BOTH of those
// questions, and how each one asks them.
//
// AN ENTRY IS A CLAIM ABOUT SAFETY, not a permission slip. "It reads them
// separately and a tear costs nothing" is a legitimate answer and one entry
// gives it — but it has to be written down and it has to be true.
// THE KEY IS THE RECEIVER-QUALIFIED NAME ([functionName], complexity_test.go),
// because a bare verb is not an address: two types in this package may perfectly
// well both answer `count`, and an exemption written for one of them would sit
// silently over the other.
var taskNewsPairReaders = map[string]string{
	"Agent.taskNewsStanding": "THE LOCKED READ ITSELF. Both facts are taken under the handover, which is " +
		"the lock a delivery makes its own two writes under ([Agent.handOverTaskNews]), so no " +
		"caller of this can see half a delivery. Every decision that costs a turn goes through it.",
	"childRun.count": "the no-progress switch ([childRun.count]) reads the two one at a time, and " +
		"deliberately: this is the event drain, and evaluating `childrenOutstanding` eagerly would " +
		"walk the graph on every event to answer a question the earlier cases usually settle. A " +
		"tear there is harmless — BOTH branches suspend the counter and neither ends anything — so " +
		"the worst it can do is count one step against a node whose last part just landed, which " +
		"the report count resets on the next event anyway. The PARK decision, which is the one " +
		"that buys a model turn, is taken through [Agent.taskNewsStanding] in [childRun.foldParts].",
}

// TestEveryReaderOfAParkedParentsPairSaysHowItReadsIt fails when a function
// starts asking both questions without an entry above, and when an entry names a
// function that no longer asks them.
func TestEveryReaderOfAParkedParentsPairSaysHowItReadsIt(t *testing.T) {
	found := map[string]bool{}
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			owed, working := readsTaskNewsPair(function.Body, intoClosures)
			if !owed || !working {
				continue
			}
			name := functionName(function)
			found[name] = true
			if _, known := taskNewsPairReaders[name]; known {
				continue
			}
			t.Errorf("%s:%d: %s asks both of a parked parent's questions and "+
				"tasknews_law_test.go's taskNewsPairReaders does not say how it reads them.\n"+
				"A delivery writes the queue, then the fact, then the wake, and never in any other "+
				"order ([Agent.deliverTaskNote]); two separate reads can land inside that and see "+
				"half of it. Read the pair through [Agent.taskNewsStanding], or add the entry "+
				"saying why a tear costs this reader nothing.",
				filepath.Base(path), fset.Position(function.Pos()).Line, name)
		}
	})
	var gone []string
	for reader := range taskNewsPairReaders {
		if !found[reader] {
			gone = append(gone, reader)
		}
	}
	sort.Strings(gone)
	for _, reader := range gone {
		t.Errorf("taskNewsPairReaders names %q and nothing there asks both questions any more — remove the entry", reader)
	}
}

// TestNothingReadsTheParkedParentsPairInOneBreath is the exact shape the defect
// had, refused by name: `owed, working := a.taskNewsOwed(), a.childrenOutstanding()`.
//
// It is a claim the test above cannot make, because that one is about functions
// and this is about a statement. Two calls side by side in one assignment READ AS
// ONE READING and are not one — which is precisely why the inversion survived
// review in two files at once. There is one way to take both facts together and
// it is [Agent.taskNewsStanding].
func TestNothingReadsTheParkedParentsPairInOneBreath(t *testing.T) {
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		ast.Inspect(file, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, value := range assignment.Rhs {
				if owed, working := readsTaskNewsPair(value, stayingHere); owed && working {
					t.Errorf("%s:%d: this takes \"is anything outstanding\" and \"is anything owed\" "+
						"as two calls in one statement, which is the shape that cost a divided parent "+
						"a whole model turn on an empty request. Take both from "+
						"[Agent.taskNewsStanding], which reads them under the lock the delivery "+
						"writes them under.",
						filepath.Base(path), fset.Position(assignment.Pos()).Line)
				}
			}
			return true
		})
	})
}

// TestTheWakeIsOnlyPostedInsideTheHandover keeps the writing side honest. The
// read is only atomic because the write is: a road that counted its news outside
// [Agent.handOverTaskNews] would put the fact and the wake back on either side of
// a window nothing holds shut.
func TestTheWakeIsOnlyPostedInsideTheHandover(t *testing.T) {
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || function.Name.Name == "handOverTaskNews" {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if !callsMethod(node, "postTaskNews") {
					return true
				}
				t.Errorf("%s:%d: %s posts a parked parent's wake on its own.\n"+
					"The wake and the fact it follows are one step ([Agent.handOverTaskNews]), "+
					"because [Agent.taskNewsStanding] reads them as one fact. Hand the news over "+
					"there instead, with the fact this road settles.",
					filepath.Base(path), fset.Position(node.Pos()).Line, function.Name.Name)
				return true
			})
		}
	})
}

// intoClosures and stayingHere are the two readings of "what does this ask",
// and both are needed. A function OWNS what the closures written inside it ask —
// a drain written as `drain := func…` is still its enclosing function's own
// reading — so the entry above is answered for the whole declaration. A STATEMENT
// does not: `drain := func…` asks nothing itself, and reading it as though it did
// would put every closure's two questions on the line that names it.
const (
	intoClosures = true
	stayingHere  = false
)

// readsTaskNewsPair answers which of the two questions this piece of syntax asks.
func readsTaskNewsPair(node ast.Node, inside bool) (owed, working bool) {
	ast.Inspect(node, func(inner ast.Node) bool {
		if _, literal := inner.(*ast.FuncLit); literal && !inside {
			return false
		}
		switch {
		case callsMethod(inner, "taskNewsOwed"):
			owed = true
		case callsMethod(inner, "childrenOutstanding"):
			working = true
		}
		return true
	})
	return owed, working
}

// callsMethod reports whether this node is a call of the named method on
// something — `child.taskNewsOwed()`, `a.childrenOutstanding()`.
func callsMethod(node ast.Node, name string) bool {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == name
}
