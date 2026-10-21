package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fileWriters are the os calls that put bytes on a path.
var fileWriters = map[string]bool{"WriteFile": true, "Rename": true, "CreateTemp": true, "Create": true, "OpenFile": true}

// TestTheProfileFileIsWrittenOnlyThroughEditProfile is the law that keeps a
// writer from putting a copy it loaded earlier back over what another writer
// stored since. editProfile reads the file under the lock and hands the reading
// to the writer's decision, so it is the only function allowed to name the
// profile file and write; and the file replacement it uses is called from
// nowhere else.
func TestTheProfileFileIsWrittenOnlyThroughEditProfile(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(here), "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				checkProfileWriter(t, fn)
			}
		}
	}
}

func checkProfileWriter(t *testing.T, fn *ast.FuncDecl) {
	t.Helper()
	name := fn.Name.Name
	namesProfile, writes, replaces := false, false, false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Ident:
			namesProfile = namesProfile || n.Name == "BudgetConfigPath"
		case *ast.BasicLit:
			namesProfile = namesProfile || n.Kind == token.STRING && strings.Contains(n.Value, "config.json")
		case *ast.CallExpr:
			writes = writes || isOSWrite(n)
			replaces = replaces || calls(n, "replaceProfileFile")
		}
		return true
	})
	if replaces && name != "editProfile" {
		t.Errorf("%s calls replaceProfileFile; only editProfile may replace the profile file", name)
	}
	if namesProfile && writes && name != "editProfile" {
		t.Errorf("%s writes the profile file itself; change it with editProfile so it reads under the lock", name)
	}
}

func isOSWrite(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	return isIdent && pkg.Name == "os" && fileWriters[sel.Sel.Name]
}

func calls(call *ast.CallExpr, name string) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == name
}
