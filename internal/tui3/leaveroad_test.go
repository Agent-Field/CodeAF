package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestTheSurfaceNamesNoSignalOfItsOwn(t *testing.T) {
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("read the surface source: %v", err)
	}

	checkSignalName := func(identifier *ast.Ident, receiver string) {
		switch identifier.Name {
		case "SIGINT", "SIGTERM", "SIGHUP":
			t.Errorf("%s names %s; the shared leaving road owns the signal set", fset.Position(identifier.Pos()), identifier.Name)
		case "Interrupt":
			if receiver == "os" {
				t.Errorf("%s names os.Interrupt; the shared leaving road owns the signal set", fset.Position(identifier.Pos()))
			}
		}
	}

	var forward *ast.FuncDecl
	for _, parsed := range packages {
		for _, file := range parsed.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.FuncDecl:
					if typed.Name.Name == "forwardSignals" {
						forward = typed
					}
				case *ast.Ident:
					checkSignalName(typed, "")
				case *ast.SelectorExpr:
					receiver, _ := typed.X.(*ast.Ident)
					if receiver != nil && receiver.Name == "os" && typed.Sel.Name == "Interrupt" {
						// The identifier arm cannot see what selected a name, and
						// Interrupt is a signal only when it is selected from os.
						checkSignalName(typed.Sel, receiver.Name)
					}
				case *ast.CallExpr:
					selector, ok := typed.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					receiver, _ := selector.X.(*ast.Ident)
					if receiver == nil {
						return true
					}
					if receiver.Name == "signal" && (selector.Sel.Name == "Notify" || selector.Sel.Name == "NotifyContext") {
						t.Errorf("%s calls signal.%s; the shared leaving road owns notification", fset.Position(typed.Pos()), selector.Sel.Name)
					}
				}
				return true
			})
		}
	}
	if forward == nil {
		t.Fatal("forwardSignals is gone; the surface no longer exposes its leaving wrapper")
	}
	delegates := 0
	ast.Inspect(forward.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "On" {
			return true
		}
		receiver, _ := selector.X.(*ast.Ident)
		if receiver != nil && receiver.Name == "leave" {
			delegates++
		}
		return true
	})
	if delegates != 1 {
		t.Fatalf("forwardSignals calls leave.On %d times, want exactly once", delegates)
	}
}
