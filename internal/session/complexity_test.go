package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// THE STANDING GATE ON HOW MANY ENDINGS ONE ROAD MAY HAVE.
//
// The four defects filed off the family audit (#229/#237, #255, #256, #258) were
// all the same miss: a function with too many endings for anybody to hold in
// their head, so one ending forgot what the others do. Nothing in a compiler
// notices that, and a review does not either — every ending reads correctly on
// its own screen, and what is wrong is only that there are nine of them.
//
// So the shape is held here, and it is held THE WAY `SIZE-BUDGET` HOLDS BYTES:
// one number that a change may not push up. A binary that grows needs the budget
// raised in the same commit as the thing that spent it, deliberately, by
// somebody who can say what it bought; a function that grows past [theCeiling]
// needs the same. The difference is only that bytes are one number for the whole
// program and this is one number per road.
//
// ── THE BARGAIN, IN FULL ──
//
//   - A NEW FUNCTION MAY NOT BE WRITTEN OVER THE CEILING. There is no road to
//     adding a row: [complexityDebt] below is the debt that existed when the
//     ratchet was fitted, and it is closed.
//   - A ROW MAY NOT GO UP. That is the ratchet, and it is the whole of what this
//     buys — the debt above cannot grow while it is being paid off.
//   - A ROW THAT IS PAID OFF IS DELETED. A function that has come down to the
//     ceiling has no debt to name, and an entry that outlived its function is a
//     lie that rots exactly as a stale exception ledger anywhere else does.
//   - AND A ROW THAT FELL IS LOWERED IN THE SAME CHANGE, which is the half that
//     makes the ratchet turn rather than merely hold. A row left at the old
//     number is slack: it would let the very next change put back everything the
//     one before it took out, with the gate green throughout. So the row IS the
//     measured number, and the failure hands the author the figure to write.
//
// ── AND IT IS A LEDGER, WHICH IS A DIFFERENT THING FROM A LOCK ──
//
// Nothing here stops somebody editing [complexityDebt]. Nothing stops somebody
// raising `SIZE-BUDGET` either, or adding a line to `.github/known-red.txt`, and
// the answer is the same for all three: the edit is a LINE IN THE DIFF that a
// reviewer reads and the author has to justify in words. What the gate buys is
// that the growth cannot happen QUIETLY — which is exactly how it happened, one
// ending at a time, over the runs that produced #229, #255, #256 and #258.
//
// The count is gocyclo's, because it is the number everybody quotes: one, plus
// every `if`, `for`, `range`, named `case`, named comm clause, `&&` and `||` —
// a `default` decides nothing and gocyclo does not count one. It is computed
// here over `go/ast` rather than shelled out to the tool, so the gate is `go
// test` and the repository gains no dependency to run it: anybody who wants a
// second opinion can run gocyclo over the same files and read the same numbers.

// theCeiling is how many decisions one function may hold. Fifteen is the number
// the repository's own quality bar already states (CLAUDE.md: no function over
// about fifteen branches), written down here so the bar and the gate cannot
// drift apart.
const theCeiling = 15

// complexityDebt is every function in `task_*.go` that was already over
// [theCeiling] when this gate was fitted, the number it measured at, and one
// line saying what the debt IS — because a number with no account of itself is
// a number nobody can pay off.
//
// Measured on 2026-09-01, after #260's extractions. `runTaskChild` was the worst
// of them at 55 and is not here: it is [childRun] now (task_child_run.go), and
// every one of its phases is under the ceiling.
var complexityDebt = map[string]int{
	"decodeTasks":           28,
	"TaskGraph.rehydrate":   22,
	"TaskGraph.runFrontier": 22,
	"Agent.workTaskNode":    21,
	"declaredInvalidations": 21,
	"taskNote":              19,
	"auditDoor.admitsFile":  16,
	"copyOriginal":          16,
}

// whyTheDebtIsStillThere is the sentence each row above owes. It is a map of its
// own rather than a struct field so that the numbers read as a ledger — the
// thing a change edits — and the reasons read as prose.
var whyTheDebtIsStillThere = map[string]string{
	"decodeTasks": "the checkpoint's own validator: every field of every node record is " +
		"refused by name, and the refusals are the point (task_store.go). It is a table " +
		"waiting to be written as one.",
	"TaskGraph.rehydrate": "the other half of the same road — one restored record turned back " +
		"into a live node, field by field, with a default per absent field.",
	"TaskGraph.runFrontier": "the scheduler. Every reason a runnable node may not start yet is " +
		"one arm of it, and they are read in an order that is itself the policy.",
	"Agent.workTaskNode": "the node's whole life, and #260 took its world, its handoff " +
		"contract, its unfinished settlements and its landing out of it. What is left is the " +
		"one-worker-or-two loop and the gate's three verdicts, which is its own issue.",
	"declaredInvalidations": "a worker's claim read for the beliefs it says it moved " +
		"(task_claims.go): several shapes of sentence, each with its own reading.",
	"taskNote": "one landing turned into the sentence its parent reads, and every " +
		"kind of ending spells that sentence differently — including, since #277, the one " +
		"whose work was saved nowhere and has no branch to offer.",
	"auditDoor.admitsFile": "one path weighed against what a checker may open, which is a " +
		"question with that many separate answers.",
	"copyOriginal": "the original of a file fetched for a check, from whichever of the " +
		"several places it may still exist in (task_audit.go).",
}

// TestNoRoadInTheTaskEngineHasMoreEndingsThanItsLedgerRow is the ratchet.
func TestNoRoadInTheTaskEngineHasMoreEndingsThanItsLedgerRow(t *testing.T) {
	measured := measureTaskComplexity(t)
	for _, complaint := range judgeComplexity(measured, complexityDebt) {
		t.Error(complaint)
	}
	// AND EVERY ROW OWES ITS SENTENCE. A number with no reason beside it is a
	// number the next person can only raise.
	for name := range complexityDebt {
		if strings.TrimSpace(whyTheDebtIsStillThere[name]) == "" {
			t.Errorf("complexityDebt carries %q and whyTheDebtIsStillThere does not say what that debt is", name)
		}
	}
	for name := range whyTheDebtIsStillThere {
		if _, owed := complexityDebt[name]; !owed {
			t.Errorf("whyTheDebtIsStillThere explains %q, which is not in complexityDebt — remove it", name)
		}
	}
}

// measuredFunction is one function's name, the file it lives in and what it
// measured, which is everything the judge below needs.
type measuredFunction struct {
	file string
	at   int
}

// judgeComplexity is the whole rule, kept apart from the walk so that it can be
// put to a table of numbers in a test rather than only to this package's own
// source. It answers one complaint per thing wrong, in a stable order.
func judgeComplexity(measured map[string]measuredFunction, ledger map[string]int) []string {
	var complaints []string
	var names []string
	for name := range measured {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		found := measured[name]
		row, ledgered := ledger[name]
		switch {
		case found.at <= theCeiling:
			// Under the ceiling and unledgered is the ordinary case and says nothing.
			// Under the ceiling and LEDGERED is a debt that has been paid, and the
			// row goes.
			if ledgered {
				complaints = append(complaints, name+" ("+found.file+") is down to "+
					strconv.Itoa(found.at)+", at or under the ceiling of "+strconv.Itoa(theCeiling)+
					" — delete its row from complexityDebt and its reason from "+
					"whyTheDebtIsStillThere. The debt is paid.")
			}
		case !ledgered:
			complaints = append(complaints, name+" ("+found.file+") holds "+strconv.Itoa(found.at)+
				" decisions and the ceiling is "+strconv.Itoa(theCeiling)+".\n"+
				"There is no road to adding a row to complexityDebt: that ledger is the debt "+
				"this gate was fitted around and it only shrinks. Split the function along the "+
				"phases its own comments already draw — one job each, the prose moving with the "+
				"code it explains.")
		case found.at > row:
			complaints = append(complaints, name+" ("+found.file+") was "+strconv.Itoa(row)+
				" and is now "+strconv.Itoa(found.at)+".\n"+
				"complexityDebt is a ratchet: a road that is already too long may not get "+
				"longer while it is being paid off. Put the new ending in a function of its "+
				"own, or take an old one out first.")
		case found.at < row:
			complaints = append(complaints, name+" ("+found.file+") is down from "+
				strconv.Itoa(row)+" to "+strconv.Itoa(found.at)+" — write "+strconv.Itoa(found.at)+
				" into its complexityDebt row.\n"+
				"This is not a complaint about the change; it is how the ratchet turns. A row "+
				"left at the old number is room the next change may spend without anything "+
				"noticing, which would put back exactly what this one took out.")
		}
	}
	var gone []string
	for name := range ledger {
		if _, alive := measured[name]; !alive {
			gone = append(gone, name)
		}
	}
	sort.Strings(gone)
	for _, name := range gone {
		complaints = append(complaints, "complexityDebt names "+name+
			" and no such function is in task_*.go any more — remove the row.")
	}
	return complaints
}

// measureTaskComplexity walks this package's own `task_*.go` sources and answers
// what every function in them measures.
//
// IT IS THE TASK ENGINE AND NOT THE PACKAGE, because that is where the debt was
// found and where the family work keeps landing. A ceiling over the whole of
// `internal/session` is a bigger bargain than #260 struck, and striking it in
// passing would mean a ledger nobody had read written by whoever ran the test
// next.
func measureTaskComplexity(t *testing.T) map[string]measuredFunction {
	t.Helper()
	measured := map[string]measuredFunction{}
	files := 0
	forEachPackageFile(t, func(path string, file *ast.File, _ *token.FileSet) {
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "task_") {
			return
		}
		files++
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			measured[functionName(function)] = measuredFunction{file: base, at: cyclomatic(function.Body)}
		}
	})
	// The floor guards against a walk that quietly stops finding anything — a
	// renamed file, a moved package — which would turn the ratchet into a test
	// that always passes.
	if files < 20 {
		t.Fatalf("only %d task_*.go files were scanned; the engine holds far more, so the walk is broken rather than clean", files)
	}
	return measured
}

// functionName is how a function is named in the ledger: `decodeTasks` for a
// plain one, `Agent.workTaskNode` for a method. The receiver is part of the name
// because two types in this package may perfectly well answer the same verb, and
// a ledger row that could mean either is a row nobody can act on.
func functionName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	return receiverName(function.Recv.List[0].Type) + "." + function.Name.Name
}

// receiverName is the bare type name a method hangs off, with the pointer and
// any type arguments taken back off.
func receiverName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.StarExpr:
		return receiverName(typed.X)
	case *ast.IndexExpr:
		return receiverName(typed.X)
	case *ast.IndexListExpr:
		return receiverName(typed.X)
	case *ast.Ident:
		return typed.Name
	}
	return "?"
}

// cyclomatic counts the decisions in a body exactly as gocyclo counts them: one,
// plus every `if`, `for`, `range`, `case`, comm clause, `&&` and `||`.
//
// IT WALKS INTO FUNCTION LITERALS, and that is deliberate rather than an
// accident of the walk. A closure written inside a function is that function's
// own reading — the whole shape #260 was filed about was a five-hundred-line
// function whose two closures held most of its endings — so a rule that stopped
// at `func(` would have measured that road at a comfortable number and missed
// every one of them.
func cyclomatic(body *ast.BlockStmt) int {
	decisions := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			decisions++
		case *ast.CaseClause:
			// A `default` decides nothing — it is where a value that matched no
			// arm was always going to end up — and gocyclo does not count one.
			if typed.List != nil {
				decisions++
			}
		case *ast.CommClause:
			if typed.Comm != nil {
				decisions++
			}
		case *ast.BinaryExpr:
			if typed.Op == token.LAND || typed.Op == token.LOR {
				decisions++
			}
		}
		return true
	})
	return decisions
}

// ── AND THE JUDGE IS ITSELF JUDGED ──
//
// The three tests below put the rule to a table of numbers rather than to this
// package's source, because a gate that only ever sees a passing package is a
// gate nobody has watched fail.

func TestTheRatchetRefusesANewRoadOverTheCeiling(t *testing.T) {
	measured := map[string]measuredFunction{
		"Agent.somethingNew": {file: "task_new.go", at: theCeiling + 1},
		"Agent.somethingOld": {file: "task_old.go", at: theCeiling},
	}
	complaints := judgeComplexity(measured, map[string]int{})
	if len(complaints) != 1 {
		t.Fatalf("complaints = %d (%v), want exactly the new road named", len(complaints), complaints)
	}
	if !strings.Contains(complaints[0], "Agent.somethingNew") {
		t.Fatalf("the complaint does not name the road that is over: %q", complaints[0])
	}
	if !strings.Contains(complaints[0], "no road to adding a row") {
		t.Fatalf("the complaint does not say the ledger is closed: %q", complaints[0])
	}
}

func TestTheRatchetRefusesADebtThatGrew(t *testing.T) {
	ledger := map[string]int{"Agent.oldRoad": 20}
	grew := judgeComplexity(map[string]measuredFunction{
		"Agent.oldRoad": {file: "task_old.go", at: 21},
	}, ledger)
	if len(grew) != 1 || !strings.Contains(grew[0], "was 20 and is now 21") {
		t.Fatalf("a debt that grew was not refused: %v", grew)
	}
	// AND STANDING STILL IS FINE, which is what makes the ledger liveable while
	// it is being paid off.
	held := judgeComplexity(map[string]measuredFunction{
		"Agent.oldRoad": {file: "task_old.go", at: 20},
	}, ledger)
	if len(held) != 0 {
		t.Fatalf("a debt that held its number was refused: %v", held)
	}
	// AND A DEBT THAT SHRANK IS TOLD TO WRITE ITS NEW NUMBER DOWN, which is the
	// half that makes the ratchet turn: a row left at the old figure is room the
	// next change may spend with the gate green throughout.
	shrank := judgeComplexity(map[string]measuredFunction{
		"Agent.oldRoad": {file: "task_old.go", at: 17},
	}, ledger)
	if len(shrank) != 1 || !strings.Contains(shrank[0], "down from 20 to 17") {
		t.Fatalf("a debt that shrank was not asked to lower its row: %v", shrank)
	}
}

func TestTheRatchetTakesBackARowThatIsPaidOffOrGone(t *testing.T) {
	ledger := map[string]int{"Agent.oldRoad": 20}
	paid := judgeComplexity(map[string]measuredFunction{
		"Agent.oldRoad": {file: "task_old.go", at: theCeiling},
	}, ledger)
	if len(paid) != 1 || !strings.Contains(paid[0], "The debt is paid") {
		t.Fatalf("a row at the ceiling was not taken back: %v", paid)
	}
	gone := judgeComplexity(map[string]measuredFunction{}, ledger)
	if len(gone) != 1 || !strings.Contains(gone[0], "no such function") {
		t.Fatalf("a row for a function that is gone was not taken back: %v", gone)
	}
}

// TestTheCountIsTheOneEverybodyQuotes pins the counting rule itself against a
// body whose number can be worked out by hand, so that a change to the walk
// cannot quietly redefine what every row in the ledger means.
func TestTheCountIsTheOneEverybodyQuotes(t *testing.T) {
	const source = `package p
func straight() { println(1) }
func branches(a, b bool) {
	if a && b {
		println(1)
	}
	for i := 0; i < 3; i++ {
		switch i {
		case 1:
			println(1)
		case 2, 3:
			println(2)
		default:
			println(3)
		}
	}
}
func closes(events chan int) {
	drain := func() {
		for range events {
			if len(events) > 0 {
				println(1)
			}
		}
	}
	drain()
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", source, 0)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	// straight: nothing but the one.
	// branches: 1 + if + && + for + the two NAMED case clauses = 6. The
	//           `default` is not counted and the loop's own condition carries no
	//           operator.
	// closes:   1 + range + if = 3, and it is 3 only because the walk goes into
	//           the closure.
	want := map[string]int{"straight": 1, "branches": 6, "closes": 3}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if got := cyclomatic(function.Body); got != want[function.Name.Name] {
			t.Errorf("%s measured %d, want %d", function.Name.Name, got, want[function.Name.Name])
		}
	}
}
