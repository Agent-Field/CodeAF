// Package lawcheck holds the reusable checkers behind the structural laws
// about persisted objects (docs/ARCHITECTURE.md §13). Each checker takes bytes
// or source and returns findings; an empty result means the law holds.
package lawcheck

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
)

var windowsDrive = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// NoAbsolutePaths returns every string value in the JSON document that looks
// like an absolute path or contains the user's home directory (L1).
func NoAbsolutePaths(doc []byte) []string {
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return []string{"not JSON: " + err.Error()}
	}
	home, _ := os.UserHomeDir()
	var bad []string
	walkStrings(v, func(s string) {
		if isAbsolute(s) || inHome(s, home) {
			bad = append(bad, s)
		}
	})
	return bad
}

func isAbsolute(s string) bool {
	return strings.HasPrefix(s, "/") || windowsDrive.MatchString(s)
}

func inHome(s, home string) bool {
	return len(home) > 1 && strings.Contains(s, home)
}

func walkStrings(v any, visit func(string)) {
	switch x := v.(type) {
	case string:
		visit(x)
	case []any:
		for _, e := range x {
			walkStrings(e, visit)
		}
	case map[string]any:
		for _, e := range x {
			walkStrings(e, visit)
		}
	}
}

// VersionFirst reports whether the JSON document begins with the version
// field, so a reader can branch on it before reading anything else (L11).
func VersionFirst(doc []byte) bool {
	return bytes.HasPrefix(doc, []byte(`{"V":`))
}

// ConstructsType returns the position of every place in src that builds a
// value of the named type: a composite literal, new(T), or a var declaration
// (which yields a zero value). Declaring the type itself is not construction.
func ConstructsType(filename string, src any, typeName string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		if e := constructedType(n); e != nil && namesType(e, typeName) {
			hits = append(hits, fset.Position(n.Pos()).String())
		}
		return true
	})
	return hits, nil
}

// constructedType is the type expression a node instantiates, or nil.
func constructedType(n ast.Node) ast.Expr {
	switch x := n.(type) {
	case *ast.CompositeLit:
		return x.Type
	case *ast.ValueSpec:
		return x.Type
	case *ast.CallExpr:
		return newArg(x)
	}
	return nil
}

func newArg(c *ast.CallExpr) ast.Expr {
	if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "new" && len(c.Args) == 1 {
		return c.Args[0]
	}
	return nil
}

func namesType(e ast.Expr, name string) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == name
	case *ast.SelectorExpr:
		return x.Sel.Name == name
	case *ast.StarExpr:
		return namesType(x.X, name)
	}
	return false
}
