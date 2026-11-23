package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every completion in the knowledge package must be the compiler's one billed
// provider call. Adding another client or removing its billing arm fails here.
func memoryFunnelViolations(root string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(root, "internal", "knowledge", "*.go"))
	if err != nil {
		return nil, err
	}
	var violations []string
	calls := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var armed token.Pos
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if assignment, ok := n.(*ast.AssignStmt); ok && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
					lhs, lhsOK := assignment.Lhs[0].(*ast.Ident)
					call, callOK := assignment.Rhs[0].(*ast.CallExpr)
					if lhsOK && lhs.Name == "ctx" && callOK {
						if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "memoryBilling" && len(call.Args) == 1 {
							if arg, ok := call.Args[0].(*ast.Ident); ok && arg.Name == "ctx" {
								armed = assignment.Pos()
							}
						}
					}
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if strings.HasPrefix(selector.Sel.Name, "Complete") {
					calls++
					contextOK := false
					if len(call.Args) > 0 {
						if arg, ok := call.Args[0].(*ast.Ident); ok {
							contextOK = arg.Name == "ctx"
						}
					}
					if !contextOK {
						violations = append(violations, path+": completion discards the billed context")
					}
					if filepath.Base(path) != "compiler_provider.go" || fn.Name.Name != "call" || armed == 0 || armed > call.Pos() {
						violations = append(violations, path+": completion bypasses the memory billing funnel")
					}
				}
				return true
			})
		}
	}
	if calls != 1 {
		violations = append(violations, "knowledge must have exactly one billed completion door")
	}
	return violations, nil
}

func checkMemoryFunnel(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..")
	violations, err := memoryFunnelViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatal(strings.Join(violations, "\n"))
	}
	// The application's ledger binding is part of the law, not just a hook that
	// a future caller can leave unconnected.
	source, err := os.ReadFile(filepath.Join(root, "internal", "session", "knowledge_usage.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"knowledge.SetUsageRecorder(", "RecordUsage(", "a.bank(", "ledger: true"} {
		if !bytes.Contains(source, []byte(required)) {
			t.Fatalf("memory ledger binding lost %s", required)
		}
	}
	// Plant a real bypass in a copy: the law must reject its own failure mode.
	copyRoot := t.TempDir()
	dir := filepath.Join(copyRoot, "internal", "knowledge")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(root, "internal", "knowledge", "compiler_provider.go"))
	if err != nil {
		t.Fatal(err)
	}
	planted := bytes.Replace(original, []byte("ctx = memoryBilling(ctx)"), []byte("memoryBilling(ctx)"), 1)
	if bytes.Equal(original, planted) {
		t.Fatal("billing mutation did not land")
	}
	if err := os.WriteFile(filepath.Join(dir, "compiler_provider.go"), planted, 0600); err != nil {
		t.Fatal(err)
	}
	violations, err = memoryFunnelViolations(copyRoot)
	if err != nil || len(violations) == 0 {
		t.Fatalf("law accepted an unbilled compiler: %v %v", violations, err)
	}
}
