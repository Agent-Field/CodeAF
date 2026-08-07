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

// lockedPackages are the trees this scan holds to the law. They are the ones
// that take a mutex on a path a guard is watching: once a panic is absorbed
// rather than fatal, a critical section that unlocks only on the success path
// no longer crashes the process — it wedges it, and a wedge has no stack trace
// and no line in the log.
var lockedPackages = []string{
	"cmd/aforge",
	"internal/exec",
	"internal/provider",
	"internal/resident",
	"internal/store",
	"internal/tui",
}

// lockAllowlist names critical sections that may unlock without a defer, keyed
// by file:line and carrying the reason. It is empty, and that is the finding:
// every one of the thirty-three sites this scan first reported converted
// cleanly, because Go's open-coded defers make the scoped form free and a
// critical section that will not fit one is a function that wants splitting.
//
// An entry here has to argue that the section is both correct as written and
// unreachable from anything that recovers. "It cannot panic" is not that
// argument — it is a claim the next edit breaks silently. Line numbers move, so
// an entry added here is re-checked whenever its file is touched.
var lockAllowlist = map[string]string{}

// lockScanFloor guards against a scan that quietly stops finding anything —
// a moved package, a bad root, a walk that returns early. The territory holds
// well over a hundred lock sites; a run that sees fewer than this has broken
// rather than passed.
const lockScanFloor = 60

// TestEveryLockInTheGuardedTreeUnlocksFromADefer is the standing check behind
// "a fault must not become a freeze". A Lock is followed immediately by the
// matching deferred Unlock, or it is named in lockAllowlist with a reason.
func TestEveryLockInTheGuardedTreeUnlocksFromADefer(t *testing.T) {
	root := moduleRoot(t)
	var loose []string
	scanned := 0

	for _, pkg := range lockedPackages {
		err := filepath.WalkDir(filepath.Join(root, pkg), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			for _, site := range scanLocks(file) {
				scanned++
				if site.deferred {
					continue
				}
				key := fmt.Sprintf("%s:%d", relative, fset.Position(site.pos).Line)
				if _, allowed := lockAllowlist[key]; allowed {
					continue
				}
				loose = append(loose, fmt.Sprintf("%s: %s.%s()", key, site.receiver, site.method))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", pkg, err)
		}
	}

	if scanned < lockScanFloor {
		t.Fatalf("the scan found only %d lock sites across %v — it is looking in the wrong place",
			scanned, lockedPackages)
	}
	sort.Strings(loose)
	if len(loose) > 0 {
		t.Fatalf("these critical sections turn an absorbed panic into a deadlock.\n"+
			"Put `defer x.Unlock()` directly under the Lock, splitting the function if the\n"+
			"section is not the rest of it:\n  %s", strings.Join(loose, "\n  "))
	}
}

// TestLockAllowlistIsStillReal keeps the escape hatch honest: an exemption
// whose site has moved or been fixed is one the next Lock on that line inherits
// for free.
func TestLockAllowlistIsStillReal(t *testing.T) {
	root := moduleRoot(t)
	for key, reason := range lockAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Fatalf("allowlisted %s carries no reason", key)
		}
		path, line, found := strings.Cut(key, ":")
		if !found {
			t.Fatalf("allowlist key %q is not file:line", key)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(path)), nil, 0)
		if err != nil {
			t.Fatalf("allowlisted %s: %v", path, err)
		}
		still := false
		for _, site := range scanLocks(file) {
			if !site.deferred && fmt.Sprint(fset.Position(site.pos).Line) == line {
				still = true
			}
		}
		if !still {
			t.Fatalf("allowlisted %s no longer holds a non-deferred lock — drop the entry", key)
		}
	}
}

// lockSite is one Lock or RLock call and whether the statement directly under
// it is the matching deferred unlock.
type lockSite struct {
	receiver string
	method   string
	pos      token.Pos
	deferred bool
}

// scanLocks reads statement lists rather than whole functions on purpose. A
// defer anywhere in the function would satisfy a looser rule while protecting
// nothing — `wait` in internal/exec/jobs.go held exactly that shape, a bare
// Lock/Unlock pair in a function whose *second* section deferred, and a
// function-scoped check called it clean.
func scanLocks(root ast.Node) []lockSite {
	var sites []lockSite
	ast.Inspect(root, func(node ast.Node) bool {
		var list []ast.Stmt
		switch typed := node.(type) {
		case *ast.BlockStmt:
			list = typed.List
		case *ast.CaseClause:
			list = typed.Body
		case *ast.CommClause:
			list = typed.Body
		default:
			return true
		}
		for index, statement := range list {
			receiver, method, call, ok := lockCall(unlabel(statement))
			if !ok {
				continue
			}
			site := lockSite{receiver: receiver, method: method, pos: call.Pos()}
			if index+1 < len(list) {
				site.deferred = unlocks(list[index+1], receiver, method)
			}
			sites = append(sites, site)
		}
		return true
	})
	return sites
}

// unlabel reaches through `closed:` and the like, which wrap the statement they
// label rather than sitting beside it.
func unlabel(statement ast.Stmt) ast.Stmt {
	for {
		labeled, ok := statement.(*ast.LabeledStmt)
		if !ok {
			return statement
		}
		statement = labeled.Stmt
	}
}

// lockCall reports a bare `x.Lock()` or `x.RLock()` statement.
func lockCall(statement ast.Stmt) (receiver, method string, call *ast.CallExpr, ok bool) {
	expression, isExpression := statement.(*ast.ExprStmt)
	if !isExpression {
		return "", "", nil, false
	}
	call, isCall := expression.X.(*ast.CallExpr)
	if !isCall {
		return "", "", nil, false
	}
	receiver, method, named := selector(call)
	if !named || (method != "Lock" && method != "RLock") {
		return "", "", nil, false
	}
	return receiver, method, call, true
}

// unlocks reports whether statement is the deferred release of the lock the
// same receiver just took. A read lock released by Unlock is not a match: on a
// sync.RWMutex that is a different operation, and the mismatch is the bug.
func unlocks(statement ast.Stmt, receiver, method string) bool {
	deferred, ok := statement.(*ast.DeferStmt)
	if !ok {
		return false
	}
	target, released, named := selector(deferred.Call)
	if !named || target != receiver {
		return false
	}
	if method == "RLock" {
		return released == "RUnlock"
	}
	return released == "Unlock"
}

// selector renders the receiver of a method call back to source text, which is
// how two sites are told to be the same lock. It is deliberately syntactic: a
// scan that needed type information would need the packages to build, and this
// one has to run on code that does not yet.
func selector(call *ast.CallExpr) (receiver, method string, ok bool) {
	chosen, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return "", "", false
	}
	rendered, renderable := render(chosen.X)
	if !renderable {
		return "", "", false
	}
	return rendered, chosen.Sel.Name, true
}

func render(expression ast.Expr) (string, bool) {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name, true
	case *ast.SelectorExpr:
		inner, ok := render(typed.X)
		if !ok {
			return "", false
		}
		return inner + "." + typed.Sel.Name, true
	case *ast.IndexExpr:
		inner, ok := render(typed.X)
		if !ok {
			return "", false
		}
		key, keyOK := render(typed.Index)
		if !keyOK {
			return "", false
		}
		return inner + "[" + key + "]", true
	case *ast.StarExpr:
		inner, ok := render(typed.X)
		if !ok {
			return "", false
		}
		return "*" + inner, true
	case *ast.ParenExpr:
		return render(typed.X)
	}
	return "", false
}

// moduleRoot walks up to the go.mod. It is this file's own rather than shared
// so the scan does not break when a neighbouring test file is edited.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the guard package")
		}
		dir = parent
	}
}
