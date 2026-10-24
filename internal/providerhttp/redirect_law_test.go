package providerhttp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every provider-key client construction names a redirect policy explicitly.
// Caller-supplied clients additionally pass through Clone at their admission door.
func TestLawProviderClientsDeclareRedirectPolicy(t *testing.T) {
	for _, dir := range []string{"../provider", "../config", "../catalog", "../codexauth", "../router"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			fset := token.NewFileSet()
			tree, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(tree, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				typ, ok := literal.Type.(*ast.SelectorExpr)
				if !ok || typ.Sel.Name != "Client" {
					return true
				}
				pkg, ok := typ.X.(*ast.Ident)
				if !ok || pkg.Name != "http" {
					return true
				}
				declared := false
				for _, elt := range literal.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if ok && key.Name == "CheckRedirect" {
						declared = sharedRedirectRule(kv.Value)
					}
				}
				if !declared {
					t.Errorf("%s constructs a provider-key client without an explicit redirect policy", fset.Position(literal.Pos()))
				}
				return true
			})
		}
	}
}

// Settings probes and credit lookups must not bypass the shared request door.
func TestLawProviderCredentialLookupsUseGuardedRequestDoor(t *testing.T) {
	for _, path := range []string{"../credits/credits.go", "../router/catalog.go", "../tui3/addprovider.go"} {
		fset := token.NewFileSet()
		tree, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(tree, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Do" {
				return true
			}
			owner, ok := sel.X.(*ast.Ident)
			if ok && (owner.Name == "providerhttp" || owner.Name == "localProbeClient") {
				return true
			}
			t.Errorf("%s bypasses the shared provider credential request door", fset.Position(call.Pos()))
			return true
		})
	}
}

func sharedRedirectRule(expr ast.Expr) bool {
	if selector, ok := expr.(*ast.SelectorExpr); ok {
		pkg, ok := selector.X.(*ast.Ident)
		return ok && pkg.Name == "providerhttp" && selector.Sel.Name == "CheckRedirect"
	}
	// The connectivity HEAD is deliberately credential-free and refuses every
	// redirect. This exact one-return function is the only stricter exception.
	fn, ok := expr.(*ast.FuncLit)
	if !ok || len(fn.Body.List) != 1 {
		return false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	selector, ok := ret.Results[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "http" && selector.Sel.Name == "ErrUseLastResponse"
}

func TestRedirectLawRejectsNilAndPermissivePolicies(t *testing.T) {
	for _, source := range []string{"nil", "func(*http.Request, []*http.Request) error {return nil}", "unrelated.CheckRedirect"} {
		expr, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatal(err)
		}
		if sharedRedirectRule(expr) {
			t.Fatalf("law accepted unsafe policy %s", source)
		}
	}
}
