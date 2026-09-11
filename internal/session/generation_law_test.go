package session

// THE LAW: A GENERATION IS CUT THROUGH ONE DOOR.
//
// A generation is the provider request in flight, cancellable independently of
// the turn around it (steer.go). Four things cut one today — a person's steer, a
// person's word, a routed memory that landed before the first token, and a mark
// that says hand this over — and every one of them has to ask the same question
// first: is there a live attempt to cut at all, and may it be cut. The lock is
// what answers, and the answer is only single if there is one place that reads
// it.
//
// The measured cost of the alternative is in steer.go's own header: a second
// reader writing the same two lines somewhere else is how a build ends up with
// two answers to "is there anything to cut", and how a request that had just
// returned is cancelled after the fact.
//
// So the check is on the SOURCE: a generation's `cancel` may be called in
// exactly one function, [Agent.cutGenerationLocked]; every other road reaches it
// through that or through [Agent.cutGeneration] above it. A lane that adds a
// third cause adds a cause, never a second door.
//
// AND IT FOLLOWS THE VALUE, NOT ITS SPELLING. The first version of this law
// matched the three tokens `x.generation.cancel`, which a local named anything
// at all walks straight past — and `completeWithRetryReasoning` already binds
// one from [Agent.beginGeneration], in the very function the next cause is most
// likely to be written in. So the law asks where a value CAME FROM instead:
// every road that hands out a generation in this package is enumerated below,
// and anything reached by one of them is a generation however it is named.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// theCutDoor is the one function allowed to cancel a generation.
const theCutDoor = "cutGenerationLocked"

// theGenerationType and theGenerationDoor are the two ways a generation is
// SPELLED where it is made: the type itself, and the one constructor that hands
// one out. A local bound from either is a generation.
const (
	theGenerationType = "activeGeneration"
	theGenerationDoor = "beginGeneration"
	// theGenerationField is the third road — the live attempt parked on the
	// agent, which is where every cut that matters starts.
	theGenerationField = "generation"
)

func TestEveryGenerationCutGoesThroughTheOneDoor(t *testing.T) {
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	cutters := map[string]string{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, decl := range file.Decls {
			function, isFunction := decl.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			// Everything inside this function that holds a generation, by the
			// roads above — read BEFORE the body is walked, because a value may be
			// cut on a line above the one that bound it in source order only if
			// somebody wrote it that way, and the law should still see it.
			held := generationsHeldBy(function)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall || !cancelsAGeneration(call.Fun, held) {
					return true
				}
				where := fset.Position(call.Pos())
				cutters[function.Name.Name] = where.String()
				return true
			})
		}
	}
	if len(cutters) == 0 {
		t.Fatal("no generation cut found at all: either the field was renamed or the door was deleted — " +
			"this law is only worth anything while it can see the thing it guards")
	}
	for function, where := range cutters {
		if function == theCutDoor {
			continue
		}
		t.Errorf("%s cancels a generation in %s: every cut goes through %s, "+
			"which is the only reader of a.generation that can tell a live attempt "+
			"from one that returned a microsecond ago (steer.go). Add your cause "+
			"beside errSteerCut and call the door.", function, where, theCutDoor)
	}
}

// generationsHeldBy names every local, parameter and result of one function that
// holds a generation. It is the whole of the law's type reading, and it is
// deliberately GENEROUS: a name it claims wrongly costs one false report with a
// message saying exactly what to do, and a name it misses costs a second door.
func generationsHeldBy(function *ast.FuncDecl) map[string]bool {
	held := map[string]bool{}
	claim := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			if !namesTheGenerationType(field.Type) {
				continue
			}
			for _, name := range field.Names {
				held[name.Name] = true
			}
		}
	}
	claim(function.Recv)
	claim(function.Type.Params)
	claim(function.Type.Results)
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch bound := node.(type) {
		case *ast.AssignStmt:
			if !handsOutAGeneration(bound.Rhs) {
				return true
			}
			for _, target := range bound.Lhs {
				if name, isName := target.(*ast.Ident); isName {
					held[name.Name] = true
				}
			}
		case *ast.ValueSpec:
			if namesTheGenerationType(bound.Type) || handsOutAGeneration(bound.Values) {
				for _, name := range bound.Names {
					held[name.Name] = true
				}
			}
		}
		return true
	})
	return held
}

// namesTheGenerationType recognises `activeGeneration` and `*activeGeneration`.
func namesTheGenerationType(expr ast.Expr) bool {
	switch typed := expr.(type) {
	case *ast.StarExpr:
		return namesTheGenerationType(typed.X)
	case *ast.Ident:
		return typed.Name == theGenerationType
	}
	return false
}

// handsOutAGeneration says whether any of these expressions produces one: the
// constructor, a literal of the type, or the field on the agent.
func handsOutAGeneration(values []ast.Expr) bool {
	found := false
	for _, value := range values {
		ast.Inspect(value, func(node ast.Node) bool {
			switch shape := node.(type) {
			case *ast.CallExpr:
				if selector, isSelector := shape.Fun.(*ast.SelectorExpr); isSelector &&
					selector.Sel.Name == theGenerationDoor {
					found = true
				}
				if name, isName := shape.Fun.(*ast.Ident); isName && name.Name == theGenerationDoor {
					found = true
				}
			case *ast.CompositeLit:
				if namesTheGenerationType(shape.Type) {
					found = true
				}
			case *ast.SelectorExpr:
				if shape.Sel.Name == theGenerationField {
					found = true
				}
			}
			return true
		})
	}
	return found
}

// cancelsAGeneration recognises a cut however the generation is spelled: the
// field reached directly, or any value this function is holding one in.
func cancelsAGeneration(fun ast.Expr, held map[string]bool) bool {
	outer, isSelector := fun.(*ast.SelectorExpr)
	if !isSelector || outer.Sel.Name != "cancel" {
		return false
	}
	switch receiver := outer.X.(type) {
	case *ast.SelectorExpr:
		return receiver.Sel.Name == theGenerationField
	case *ast.Ident:
		return held[receiver.Name]
	}
	return false
}
