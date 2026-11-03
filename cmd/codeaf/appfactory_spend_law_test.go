package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The inventory is exact in both directions: a new caller needs a ledger
// owner or an explicit debt entry; a removed caller must leave the debt list.
// Like the provider funnel and session purpose laws, this parses production
// sources rather than relying on which branches a fixture happens to run.
func TestFactoryModelSendsHaveLedgerOwners(t *testing.T) {
	root := filepath.Join("..", "..")
	calls, err := modelSpendCallers(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/model_spend_owners.tsv")
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, row := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		key, owner, ok := strings.Cut(row, "\t")
		if !ok || !(strings.HasPrefix(owner, "owner: ") || strings.HasPrefix(owner, "exception: ")) {
			t.Fatalf("invalid ledger owner: %s", row)
		}
		if _, exists := entries[key]; exists {
			t.Fatalf("duplicate ledger owner: %s", key)
		}
		entries[key] = owner
	}
	for _, violation := range modelSpendViolations(calls, entries) {
		t.Error(violation)
	}
	if len(calls) < 50 {
		t.Fatalf("only %d send callers found; source walk is incomplete", len(calls))
	}
	// Plant a production helper in a copy. A new unowned send must be rejected.
	dir := t.TempDir()
	source := `package fixture
 func unowned(c Client) { c.CompleteWithMessages(ctx, nil) }
 `
	if err := os.WriteFile(filepath.Join(dir, "unowned.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	mutant, err := modelSpendCallers(dir)
	if err != nil {
		t.Fatal(err)
	}
	for key, at := range calls {
		mutant[key] = at
	}
	violations := modelSpendViolations(mutant, entries)
	found := false
	for _, v := range violations {
		if strings.Contains(v, "unowned.go:unowned:CompleteWithMessages") {
			found = true
			t.Log("mutation rejected: " + v)
		}
	}
	if !found {
		t.Fatal("ledger law accepted an unowned production helper")
	}
	// Removing an exception without retiring its entry must also fail.
	for key := range entries {
		if !strings.HasPrefix(entries[key], "exception: ") {
			continue
		}
		delete(mutant, key)
		stale := false
		for _, v := range modelSpendViolations(mutant, entries) {
			if v == "stale ledger entry: "+key {
				stale = true
			}
		}
		if !stale {
			t.Fatalf("ledger law accepted stale exception %s", key)
		}
		break
	}
}

func modelSpendViolations(calls, entries map[string]string) []string {
	var out []string
	for key, at := range calls {
		if entries[key] == "" {
			out = append(out, fmt.Sprintf("unowned model send: %s (%s)", key, at))
		}
	}
	for key := range entries {
		if calls[key] == "" {
			out = append(out, "stale ledger entry: "+key)
		}
	}
	sort.Strings(out)
	return out
}

func modelSpendCallers(root string) (map[string]string, error) {
	calls := map[string]string{}
	verbs := map[string]bool{"CompleteWithMessages": true, "StreamComplete": true, "ExecuteToolCallLoop": true, "ParseDocument": true, "GenerateImage": true, "GenerateMusic": true, "GenerateVideo": true, "Speak": true, "Transcribe": true}
	// The provider funnel's request functions are counted as well, including
	// media requests and lane probes that do not call a completion method.
	requests := map[string]bool{"send": true, "sendShaped": true, "sendRecovered": true, "sendRepaired": true, "resend": true, "do": true, "doEndpoint": true, "postJSON": true, "newHTTPRequest": true, "newHTTPRequestAt": true}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "node_modules" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil {
				recv := fn.Recv.List[0].Type
				if p, ok := recv.(*ast.StarExpr); ok {
					recv = p.X
				}
				if id, ok := recv.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				verb := selector.Sel.Name
				if !verbs[verb] && !(strings.HasPrefix(rel, "internal/provider/") && requests[verb]) {
					return true
				}
				key := rel + ":" + name + ":" + verb
				calls[key] = fmt.Sprintf("%s:%d", rel, set.Position(call.Pos()).Line)
				return true
			})
		}
		return nil
	})
	return calls, err
}
