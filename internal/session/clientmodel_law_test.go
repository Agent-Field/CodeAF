package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A MODEL OVERRIDE MAY APPEAR ONLY BESIDE THE CLIENT-ACCOUNT DOOR. WithModel
// changes a request slug without changing its address or bearer, so using it
// anywhere else can send a role's model to the conversation's service.
func TestOnlyTheClientDoorCanOverrideAModel(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	doors := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files := token.NewFileSet()
		file, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		aliases := make(map[string]bool)
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil || path != "github.com/Agent-Field/agentfield/sdk/go/ai" {
				continue
			}
			alias := "ai"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			aliases[alias] = true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, named := selector.X.(*ast.Ident)
			if !named || selector.Sel.Name != "WithModel" || !aliases[pkg.Name] {
				return true
			}
			if name == "clientdoor.go" {
				doors++
			} else {
				t.Errorf("%s:%d calls ai.WithModel outside clientdoor.go", name, files.Position(call.Pos()).Line)
			}
			return true
		})
	}
	if doors != 1 {
		t.Errorf("clientdoor.go contains %d ai.WithModel calls, want the one model-override door", doors)
	}
}

// THE SESSION CLIENT MAY BE READ ONLY THROUGH CLIENTDOOR.GO. A raw completer
// can be handed to another package and pinned to a model from another account;
// named doors expose the narrower operation, and the one completer that may
// leave resolves every model option back through the account pool.
func TestOnlyTheClientDoorReadsTheSessionClient(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files := token.NewFileSet()
		file, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "client" {
				return true
			}
			if name == "clientdoor.go" {
				reads++
			} else {
				t.Errorf("%s:%d reads a session client outside clientdoor.go", name, files.Position(selector.Pos()).Line)
			}
			return true
		})
	}
	if reads == 0 {
		t.Error("clientdoor.go no longer reads the session client; move or remove this law with the field")
	}
}

// Every production Agent is born through agent.go's account-aware doors.
// newAgent remains the scripted-completer seam for tests, but calling it from
// another production file recreates the unmanaged child that bypassed model
// account resolution.
func TestEveryProductionAgentGetsTheAccountPool(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files := token.NewFileSet()
		file, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			function, ok := call.Fun.(*ast.Ident)
			if !ok || function.Name != "newAgent" {
				return true
			}
			calls++
			if name != "agent.go" {
				t.Errorf("%s:%d builds an unmanaged Agent outside agent.go", name, files.Position(call.Pos()).Line)
			}
			return true
		})
	}
	if calls != 2 {
		t.Errorf("production contains %d newAgent calls, want only the public and child doors in agent.go", calls)
	}
}
