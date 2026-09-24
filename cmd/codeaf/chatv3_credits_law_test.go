package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Both local surface roads must own a reader and an implicit-choice signal;
// the two remote roads must leave this machine's account absent.
func TestOnlyLocalSurfaceRoadsWireCreditReading(t *testing.T) {
	for _, tc := range []struct {
		file, function string
		want           []string
		forbid         []string
	}{
		{"chatv3.go", "openChatV3", []string{"ReadCredits", "ImplicitTalk"}, nil},
		{"chatv3_local.go", "localDoors", []string{"ReadCredits"}, nil},
		{"chatv3_local.go", "openChatV3Local", []string{"localDoors", "ImplicitTalk"}, nil},
		{"chatv3_host.go", "hostOptions", nil, []string{"ReadCredits", "ImplicitTalk", "localDoors"}},
		{"chatv3_host.go", "openChatV3Host", nil, []string{"localDoors", "ReadCredits", "ImplicitTalk"}},
		{"chatv3_at.go", "openChatV3At", nil, []string{"localDoors", "ReadCredits", "ImplicitTalk"}},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), tc.file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != tc.function {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.Ident:
					seen[n.Name] = true
				case *ast.SelectorExpr:
					seen[n.Sel.Name] = true
				}
				return true
			})
		}
		for _, field := range tc.want {
			if !seen[field] {
				t.Errorf("%s %s does not wire %s", tc.file, tc.function, field)
			}
		}
		for _, field := range tc.forbid {
			if seen[field] {
				t.Errorf("%s %s wires local %s onto a remote surface", tc.file, tc.function, field)
			}
		}
	}
}
