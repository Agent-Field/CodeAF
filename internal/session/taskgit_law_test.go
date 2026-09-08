package session

// THE ONE LIST OF GIT VERBS HAS ONE DOOR. Both a task and a session carrying
// its own work choose their register before entering [refusedTaskGit]; a second
// call site could choose only one posture, or grow a second list that drifts.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// C7 — refusedTaskGit has exactly one non-test call site in this package. The
// scanner is deliberately counted by syntax rather than text so its declaration
// and comments cannot make a broken law pass.
func TestTheRefusedGitListHasExactlyOneCallSite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/session: %v", err)
	}
	fset := token.NewFileSet()
	var sites []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			function, ok := call.Fun.(*ast.Ident)
			if !ok || function.Name != "refusedTaskGit" {
				return true
			}
			sites = append(sites, fmt.Sprintf("%s:%d", name, fset.Position(call.Pos()).Line))
			return true
		})
	}
	sort.Strings(sites)
	if len(sites) != 1 {
		t.Fatalf("refusedTaskGit has %d non-test call sites (%s), want exactly one. The task and session postures must enter one scanner so their refused git list cannot drift.",
			len(sites), strings.Join(sites, ", "))
	}
}
