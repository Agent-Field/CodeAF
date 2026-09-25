package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// EVERY CLIENT THIS PACKAGE BUILDS CARRIES THE ROUTE GATE. The gate is set in
// [Config.clientConfig] and nowhere else, so the law is that every
// provider.NewClient in this package is handed a Config that function built —
// directly, or through [Config.documentConfig] — in the same function. A
// client built from anything else would be a road that reaches a route its
// crew was routed around, which is how a run summary on a second road asked a
// refused model twice a task.
func TestEveryProviderClientIsBuiltThroughTheGatedConfig(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			builds, gated := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "provider" && sel.Sel.Name == "NewClient" {
					builds = true
				}
				if sel.Sel.Name == "clientConfig" || sel.Sel.Name == "documentConfig" {
					gated = true
				}
				return true
			})
			if builds && !gated {
				t.Errorf("%s: %s builds a provider client from a Config clientConfig did not make", name, fn.Name.Name)
			}
		}
	}
	routed := Config{RouteCrew: func(config.CrewAsk) (crewroute.Decision, error) { return crewroute.Decision{}, nil }}
	if routed.clientConfig("vendor/model", 0).RouteGate == nil {
		t.Error("a conversation with crews builds its clients without the route gate")
	}
	if routed.documentConfig(0).RouteGate == nil {
		t.Error("the document road builds its client without the route gate")
	}
}
