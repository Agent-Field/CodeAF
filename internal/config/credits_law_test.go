package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryImplicitModelRungUsesTheBalanceCheck pins the doors that may choose
// a new model. A new path must join the same profile reading instead of making
// a second credit threshold or continuing to use the paid constant directly.
func TestEveryImplicitModelRungUsesTheBalanceCheck(t *testing.T) {
	for _, row := range []struct{ file, function, call string }{
		{"credits.go", "ChatDefaultAt", "useFreeDefaultsAt"},
		{"freecrew.go", "freeTierModelAt", "useFreeDefaultsAt"},
		{"config.go", "load", "ChatDefaultAt"},
		{"seats.go", "TierSeatAt", "freeTierModelAt"},
		{"crewhealth.go", "crewHealthAt", "useFreeDefaultsAt"},
		{"../../internal/tui3/modelservices.go", "reachableModelAfterDisconnect", "ChatDefaultAt"},
		{"../../cmd/codeaf/chatv3.go", "v3TalkModel", "ChatDefaultAt"},
		{"../../cmd/codeaf/chatv3.go", "openV3Launch", "v3TalkModel"},
		{"../../cmd/codeaf/chatv3_process.go", "start", "v3FreshDefault"},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), row.file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != row.function {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch name := call.Fun.(type) {
				case *ast.Ident:
					found = found || name.Name == row.call
				case *ast.SelectorExpr:
					found = found || name.Sel.Name == row.call
				}
				return true
			})
		}
		if !found {
			t.Errorf("%s: %s no longer reaches %s", row.file, row.function, row.call)
		}
	}
}

// Each allowed paid-default read has a separate reason. A new runtime reader
// must join the profile-aware rung or explain its exception here.
func TestNoUnlistedRuntimeReaderUsesThePaidDefault(t *testing.T) {
	allowed := map[string]string{
		"cmd/codeaf/chatv3.go:v3TalkModel":               "distinguishes the implicit rung from a supplied model",
		"cmd/codeaf/chatv3_credits.go:v3FreshDefault":    "recognises the two build defaults before moving a fresh launch",
		"cmd/codeaf/main.go:<package>":                   "prints the compile-time fallback in environment help",
		"cmd/codeaf/competence.go:runCompetenceTo":       "the separate competence command has its own fallback",
		"internal/config/credits.go:ChatDefaultAt":       "owns the paid side of the profile-aware bottom rung",
		"internal/tui3/credits.go:refreshCreditWarnings": "recognises an untouched paid conversation before moving it",
	}
	seen := make(map[string]int)
	root := filepath.Clean("../..")
	for _, subtree := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, subtree), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			aliases := make(map[string]bool)
			for _, imported := range file.Imports {
				if !strings.HasSuffix(strings.Trim(imported.Path.Value, `"`), "/internal/config") {
					continue
				}
				name := "config"
				if imported.Name != nil {
					name = imported.Name.Name
				}
				aliases[name] = true
			}
			check := func(scope string, node ast.Node) {
				ast.Inspect(node, func(n ast.Node) bool {
					key := filepath.ToSlash(rel) + ":" + scope
					if selector, ok := n.(*ast.SelectorExpr); ok {
						pkg, ident := selector.X.(*ast.Ident)
						if ident && aliases[pkg.Name] && selector.Sel.Name == "DefaultModel" {
							seen[key]++
							if allowed[key] == "" {
								t.Errorf("%s: %s reads config.DefaultModel without an allowlist reason", rel, scope)
							}
						}
					}
					if id, ok := n.(*ast.Ident); ok && id.Name == "DefaultModel" && (strings.HasPrefix(filepath.ToSlash(rel), "internal/config/") || aliases["."]) {
						seen[key]++
						if allowed[key] == "" {
							t.Errorf("%s: %s reads DefaultModel without an allowlist reason", rel, scope)
						}
					}
					return true
				})
			}
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					check(d.Name.Name, d.Body)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						if value, ok := spec.(*ast.ValueSpec); ok {
							for _, expr := range value.Values {
								check("<package>", expr)
							}
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for key, reason := range allowed {
		if reason == "" || seen[key] != 1 {
			t.Errorf("%s has %d paid-default reads and reason %q; want exactly one explained read", key, seen[key], reason)
		}
	}
}
