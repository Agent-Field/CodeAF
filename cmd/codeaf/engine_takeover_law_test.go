package main

// A LAW ABOUT THE TWO ROADS THAT MEET AN ENGINE ALREADY HOLDING A WORKSPACE:
// [clearStaleEngineHost] in engine.go and the local dial in chatv3_local.go.
//
// An engine may be ended from here only by ASKING it (the host decides, under
// its own lock, whether it may go) and the road may not MIGRATE anything on the
// way to that question. The first is what keeps a turn, a waiting question and
// handed-off work alive when a newer build connects; the second is what keeps a
// connecting client from rewriting state the engine it is about to join is still
// reading — a shared store or layout that changes under a running engine is a
// protocol break, and the wire version is where that has to be said (see the
// engine-update plan).
//
// It is structural because the road is short and the temptation is a line: a
// pid-signal for the host that will not go, a migration called "just to be safe".
// The one signal in the tree is `codeaf engine --stop`, which is a person saying
// so, and it lives in the two doors named below.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestTheTakeoverRoadAsksTheEngineAndMigratesNothing(t *testing.T) {
	// The two doors a person types, and the only callers of the explicit stop.
	personDoors := map[string]bool{"runEngineStop": true, "runEngineStopAll": true}
	forbidden := map[string]string{
		"enginehost.Stop": "the only force is a person's own `codeaf engine --stop`",
		"migrateV3Layout": "a launch may not move a running engine's data",
		"migrateV3Tree":   "a launch may not move a running engine's data",
		"store.Open":      "a launch may not open shared storage before it has a host",
	}
	for _, file := range []string{"engine.go", "chatv3_local.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		enclosing := ""
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch one := node.(type) {
			case *ast.FuncDecl:
				enclosing = one.Name.Name
			case *ast.CallExpr:
				called := calledName(one.Fun)
				why, banned := forbidden[called]
				if !banned {
					return true
				}
				if called == "enginehost.Stop" && personDoors[enclosing] {
					return true
				}
				t.Errorf("%s: %s calls %s — %s", file, enclosing, called, why)
			}
			return true
		})
	}
}

// calledName is the name a call was made under: "pkg.Func" for a selector and
// "Func" for a bare identifier, "" for anything else (a call through a field, a
// variable, an interface — which this law cannot read and does not guess at).
func calledName(fun ast.Expr) string {
	switch one := fun.(type) {
	case *ast.Ident:
		return one.Name
	case *ast.SelectorExpr:
		if pkg, ok := one.X.(*ast.Ident); ok {
			return pkg.Name + "." + one.Sel.Name
		}
	}
	return ""
}
