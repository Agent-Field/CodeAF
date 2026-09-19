package guard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A LOCK HANDED OUT AS A CLOSER IS A LOCK THE OTHER LAW CANNOT SEE.
//
// [TestEveryLockInTheGuardedTreeUnlocksFromADefer] asks that a Lock be followed
// by its deferred Unlock in the same function. A helper that opens a handle
// under a lock and returns `func()` to release both walks straight past that
// question: the Lock and the Unlock are in different functions on purpose, and
// whether the lock is ever released is decided by every CALLER, on every path
// out of it.
//
// It was decided wrongly once, and the cost was a freeze with no stack trace. A
// reader of a run's stores took the plan's lock, found no store to read, and
// its callers answered "no stores" by returning before the line that deferred
// the closer. Nothing released the lock. Every later read of the plan, the
// run's own driver and the conversation's close then waited on it for good: an
// engine an hour old that would not end when asked to.
//
// SO THE LAW IS STATED BY PROPERTY, AND IT HAS TWO HALVES. A function that
// returns a closure which releases a lock it did not take is A LOCK HANDED OUT.
//
//   - THE HELPER never answers "nothing" while locked: a return whose first
//     result is nil comes directly after the unlock. A caller told there is
//     nothing to read owes nothing.
//   - THE CALLER writes no way out before the closer is in hand. Between the
//     call and the first statement that defers or calls the closer, the only
//     return allowed is inside `if <first result> == nil`, which the first half
//     makes safe. Any other test there (a length, an error, a second result) is
//     a return the helper never promised to have unlocked for.
//
// Some callers release early on purpose, before a model is called, and the law
// lets them: what it forbids is a path out that never mentions the closer.

// closerPackages are the trees the scan reads. It is every package that hands a
// store or a plan out under a lock, and the surfaces that call them.
var closerPackages = []string{
	"cmd/codeaf",
	"internal/enginehost",
	"internal/plandb",
	"internal/remote",
	"internal/run",
	"internal/session",
	"internal/tui3",
}

// closerScanFloor is the same guard the lock scan carries: a scan that stops
// finding the helpers it was written about has broken rather than passed.
const closerScanFloor = 2

// TestEveryLockHandedOutAsACloserIsDeferredAtOnce is that law.
func TestEveryLockHandedOutAsACloserIsDeferredAtOnce(t *testing.T) {
	root := moduleRoot(t)
	var loose []string
	helpers := 0
	for _, pkg := range closerPackages {
		files := parsePackage(t, filepath.Join(root, pkg))
		// The helpers are found first and per package, because the walk has no
		// types: a name is matched against the helpers its own package declares,
		// which is where every one of these is called from.
		handing := map[string]int{}
		for _, parsed := range files {
			for name, at := range closerHelpers(parsed.file) {
				handing[name] = at
			}
		}
		helpers += len(handing)
		for _, parsed := range files {
			relative, err := filepath.Rel(root, parsed.path)
			if err != nil {
				t.Fatal(err)
			}
			where := func(pos token.Pos) string {
				return fmt.Sprintf("%s:%d", filepath.ToSlash(relative), parsed.fset.Position(pos).Line)
			}
			for _, pos := range lockedNothings(parsed.file, handing) {
				loose = append(loose, where(pos)+": a function that hands out a lock answers nil here, and the statement before it is not the unlock")
			}
			for _, site := range looseClosers(parsed.file, handing) {
				loose = append(loose, fmt.Sprintf("%s: %s hands out a lock, and a return is written here before `%s` is deferred or called",
					where(site.pos), site.helper, site.closer))
			}
		}
	}
	if helpers < closerScanFloor {
		t.Fatalf("the scan found only %d functions that hand a lock out across %v: it is looking in the wrong place",
			helpers, closerPackages)
	}
	sort.Strings(loose)
	if len(loose) > 0 {
		t.Fatalf("a way out that never releases the lock freezes every later reader, with no stack trace.\n"+
			"Defer the closer directly under the call, or test the first result against nil and\n"+
			"nothing else before the closer is in hand; a helper with nothing to hand out unlocks,\n"+
			"then returns nil and a closer that does nothing:\n  %s",
			strings.Join(loose, "\n  "))
	}
}

type parsedFile struct {
	path string
	fset *token.FileSet
	file *ast.File
}

func parsePackage(t *testing.T, dir string) []parsedFile {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []parsedFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		out = append(out, parsedFile{path: path, fset: fset, file: file})
	}
	return out
}

// closerHelpers is every function in one file that returns a closure which
// unlocks, with the index of that closure among its results. The closure is
// read where it is written: a `func() { … x.Unlock() … }` inside a return.
func closerHelpers(file *ast.File) map[string]int {
	found := map[string]int{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Type.Results == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			// A closure's own returns are not this function's.
			if _, nested := node.(*ast.FuncLit); nested {
				return false
			}
			ret, ok := node.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for at, result := range ret.Results {
				lit, ok := result.(*ast.FuncLit)
				if ok && releasesALock(lit) {
					found[fn.Name.Name] = at
				}
			}
			return true
		})
	}
	return found
}

// releasesALock reports whether a closure releases a lock IT DID NOT TAKE: an
// Unlock on some receiver with no Lock on that receiver inside the same
// closure. That is what "returned while holding" looks like from the closure's
// side, and it is what separates a lock handed out from the far commoner
// closure that takes a lock, does its work and gives it back before it returns
// (an unsubscribe, a restore).
func releasesALock(lit *ast.FuncLit) bool {
	taken, released := map[string]bool{}, map[string]bool{}
	ast.Inspect(lit.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		receiver, method, ok := selector(call)
		if !ok {
			return true
		}
		switch method {
		case "Lock", "RLock":
			taken[receiver] = true
		case "Unlock", "RUnlock":
			released[receiver] = true
		}
		return true
	})
	for receiver := range released {
		if !taken[receiver] {
			return true
		}
	}
	return false
}

type looseCloser struct {
	pos    token.Pos
	helper string
	closer string
}

// lockedNothings is the helper's half: inside a function that hands out a lock,
// every return whose first result is nil, unless the statement directly before
// it in the same block is an unlock. A return before the lock is taken is left
// alone, because nothing is held there yet.
func lockedNothings(file *ast.File, handing map[string]int) []token.Pos {
	var out []token.Pos
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if _, hands := handing[fn.Name.Name]; !hands {
			continue
		}
		locked := token.NoPos
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && locked == token.NoPos {
				if _, method, ok := selector(call); ok && (method == "Lock" || method == "RLock") {
					locked = call.Pos()
				}
			}
			return true
		})
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if _, nested := node.(*ast.FuncLit); nested {
				return false
			}
			block, ok := node.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, statement := range block.List {
				ret, ok := unlabel(statement).(*ast.ReturnStmt)
				if !ok || len(ret.Results) == 0 || !isNil(ret.Results[0]) {
					continue
				}
				if locked == token.NoPos || ret.Pos() < locked {
					continue
				}
				if i > 0 && isUnlock(block.List[i-1]) {
					continue
				}
				out = append(out, ret.Pos())
			}
			return true
		})
	}
	return out
}

func isNil(expression ast.Expr) bool {
	ident, ok := expression.(*ast.Ident)
	return ok && ident.Name == "nil"
}

func isUnlock(statement ast.Stmt) bool {
	expression, ok := unlabel(statement).(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expression.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	_, method, ok := selector(call)
	return ok && (method == "Unlock" || method == "RUnlock")
}

// looseClosers is the caller's half: every call to one of those helpers that is
// followed, before any statement mentions the closer, by a return that is not
// inside `if <first result> == nil`.
func looseClosers(file *ast.File, handing map[string]int) []looseCloser {
	var out []looseCloser
	ast.Inspect(file, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, statement := range block.List {
			assign, ok := unlabel(statement).(*ast.AssignStmt)
			if !ok || len(assign.Rhs) != 1 {
				continue
			}
			call, ok := assign.Rhs[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			name := calledName(call)
			at, hands := handing[name]
			if !hands || at >= len(assign.Lhs) {
				continue
			}
			closer, ok := assign.Lhs[at].(*ast.Ident)
			if !ok || closer.Name == "_" {
				out = append(out, looseCloser{pos: assign.Pos(), helper: name, closer: "the closer"})
				continue
			}
			first := ""
			if ident, ok := assign.Lhs[0].(*ast.Ident); ok && at != 0 {
				first = ident.Name
			}
			for _, next := range block.List[i+1:] {
				if mentions(next, closer.Name) {
					break
				}
				if pos, leaves := returnsOtherThanOnNil(next, first); leaves {
					out = append(out, looseCloser{pos: pos, helper: name, closer: closer.Name})
					break
				}
			}
		}
		return true
	})
	return out
}

func mentions(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		if ident, ok := inner.(*ast.Ident); ok && ident.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// returnsOtherThanOnNil finds a return in one statement, passing over the one
// shape the helper's half makes safe: `if <first> == nil { … }`.
func returnsOtherThanOnNil(statement ast.Stmt, first string) (token.Pos, bool) {
	if branch, ok := unlabel(statement).(*ast.IfStmt); ok && branch.Init == nil && branch.Else == nil && first != "" {
		if test, ok := branch.Cond.(*ast.BinaryExpr); ok && test.Op == token.EQL {
			if left, ok := test.X.(*ast.Ident); ok && left.Name == first && isNil(test.Y) {
				return token.NoPos, false
			}
		}
	}
	pos, found := token.NoPos, false
	ast.Inspect(statement, func(inner ast.Node) bool {
		if _, nested := inner.(*ast.FuncLit); nested {
			return false
		}
		if ret, ok := inner.(*ast.ReturnStmt); ok && !found {
			pos, found = ret.Pos(), true
		}
		return !found
	})
	return pos, found
}

func calledName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// TestTheCloserLawSeesTheLeakItWasWrittenFor keeps the scan honest the way the
// floor does, from the other side: the shapes below are the leak as it shipped
// for a day, and the shapes the tree uses instead. A scan that stops telling
// them apart has stopped being a law.
func TestTheCloserLawSeesTheLeakItWasWrittenFor(t *testing.T) {
	const helper = `package p
func open() ([]int, *int, func()) {
	mu.Lock()
	if broken {
		%s
	}
	return stores, plan, func() { mu.Unlock() }
}
`
	const caller = `package p
func open() (*int, func()) { mu.Lock(); return store, func() { mu.Unlock() } }
func read() int {
	store, done := open()
	%s
	return *store
}
`
	for _, tc := range []struct {
		name    string
		source  string
		nothing int
		loose   int
	}{
		{"a helper that answers nil while locked", fmt.Sprintf(helper, "return nil, nil, func() {}"), 1, 0},
		{"a helper that unlocks and then answers nil", fmt.Sprintf(helper, "mu.Unlock()\n\t\treturn nil, nil, func() {}"), 0, 0},
		{"a caller that leaves on a length before the closer", fmt.Sprintf(caller, "if len(list) == 0 {\n\t\treturn 0\n\t}\n\tdefer done()"), 0, 1},
		{"a caller that leaves on an error before the closer", fmt.Sprintf(caller, "if err != nil {\n\t\treturn 0\n\t}\n\tdefer done()"), 0, 1},
		{"a caller that never mentions the closer", fmt.Sprintf(caller, "return 1"), 0, 1},
		{"a caller that defers at once", fmt.Sprintf(caller, "defer done()\n\tif len(list) == 0 {\n\t\treturn 0\n\t}"), 0, 0},
		{"a caller that leaves only when the first result is nil", fmt.Sprintf(caller, "if store == nil {\n\t\treturn 0\n\t}\n\tdefer done()"), 0, 0},
		{"a caller that releases by hand on the way out", fmt.Sprintf(caller, "if failed {\n\t\tdone()\n\t\treturn 0\n\t}\n\tdone()"), 0, 0},
	} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "p.go", tc.source, 0)
		if err != nil {
			t.Fatalf("%s: the fixture does not parse: %v\n%s", tc.name, err, tc.source)
		}
		handing := closerHelpers(file)
		if len(handing) != 1 {
			t.Fatalf("%s: the scan found %d functions handing out a lock, want 1", tc.name, len(handing))
		}
		if got := len(lockedNothings(file, handing)); got != tc.nothing {
			t.Errorf("%s: %d locked answers of nothing, want %d", tc.name, got, tc.nothing)
		}
		if got := len(looseClosers(file, handing)); got != tc.loose {
			t.Errorf("%s: %d ways out before the closer, want %d", tc.name, got, tc.loose)
		}
	}
}
