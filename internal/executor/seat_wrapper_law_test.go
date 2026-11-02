package executor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A type that embeds Seat inherits Around and In but not the optional seat
// methods, and CheckWork is asked by type assertion ([CheckWork]). A wrapper
// that does not forward it hides the ownership gate beneath, so a displaced
// chat admits user work (#2027). Every Seat-embedding type here forwards it.
func TestEverySeatWrapperForwardsCheckWork(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var wrappers, forwarding []string
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			wrappers = append(wrappers, seatEmbedders(decl)...)
			if name, ok := checkWorkReceiver(decl); ok {
				forwarding = append(forwarding, name)
			}
		}
	}
	if len(wrappers) == 0 {
		t.Fatal("found no type that embeds Seat; the law reads the wrong files")
	}
	for _, name := range wrappers {
		if !slices.Contains(forwarding, name) {
			t.Errorf("%s embeds Seat but has no CheckWork method, so it hides the ownership gate beneath", name)
		}
	}
}

// seatEmbedders names the struct types decl declares with an embedded Seat.
func seatEmbedders(decl ast.Decl) []string {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.TYPE {
		return nil
	}
	var names []string
	for _, spec := range gen.Specs {
		ts := spec.(*ast.TypeSpec)
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}
		for _, field := range st.Fields.List {
			if id, ok := field.Type.(*ast.Ident); ok && len(field.Names) == 0 && id.Name == "Seat" {
				names = append(names, ts.Name.Name)
			}
		}
	}
	return names
}

// checkWorkReceiver names the receiver type of a CheckWork method.
func checkWorkReceiver(decl ast.Decl) (string, bool) {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Recv == nil || fn.Name.Name != "CheckWork" {
		return "", false
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	id, ok := typ.(*ast.Ident)
	return id.Name, ok
}
