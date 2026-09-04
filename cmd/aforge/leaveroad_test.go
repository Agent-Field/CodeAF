package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestTheHeadlessChatDoorLeavesThroughClose(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "chatv3.go", nil, 0)
	if err != nil {
		t.Fatalf("read chatv3.go: %v", err)
	}

	var door *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "runChatV3Once" {
			door = function
			break
		}
	}
	if door == nil {
		t.Fatal("runChatV3Once is gone; this test has stopped watching the headless chat door")
	}

	var leaving, opening token.Pos
	ast.Inspect(door.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CallExpr:
			switch called := typed.Fun.(type) {
			case *ast.Ident:
				if called.Name == "openV3Agent" {
					opening = typed.Pos()
				}
			case *ast.SelectorExpr:
				receiver, _ := called.X.(*ast.Ident)
				if receiver != nil && receiver.Name == "leave" && called.Sel.Name == "On" {
					leaving = typed.Pos()
				}
			}
		}
		return true
	})

	if !leaving.IsValid() {
		t.Fatal("runChatV3Once does not use the shared leaving road")
	}
	if !opening.IsValid() {
		t.Fatal("runChatV3Once no longer opens its session through openV3Agent")
	}
	if leaving >= opening {
		t.Fatalf("the leaving road is installed at %s, after session opening at %s", fset.Position(leaving), fset.Position(opening))
	}
}

// TestEveryHeadlessChatDoorTakesTheOneLeavingRoad is the assertion the test
// above used to make and could not: `defer agent.Close()` was already in
// runChatV3Once before any of this, so asserting it passed against unchanged
// code and proved nothing about the road.
//
// WHAT IS ACTUALLY OWED IS THAT EVERY HEADLESS CHAT DOOR REACHES leave.On, AND
// THAT NONE OF THEM SPELLS THE SIGNAL SET ITSELF. There are two doors and they
// are deliberately the same shape — runHostOnce's own comment says it is
// runChatV3Once with a remote agent — so a road installed in one and not the
// other is the shape this whole change exists to end. `--host --once` was
// exactly that: a hardcoded context.Background() and no road at all.
func TestEveryHeadlessChatDoorTakesTheOneLeavingRoad(t *testing.T) {
	doors := map[string]string{
		"runChatV3Once": "chatv3.go",
		"runHostOnce":   "chatv3_host.go",
	}
	for name, file := range doors {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		var door *ast.FuncDecl
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Name.Name == name {
				door = function
				break
			}
		}
		if door == nil {
			t.Fatalf("%s is gone from %s; this test has stopped watching a headless chat door", name, file)
		}

		// A context nothing can cancel is the tell: the turn cannot be cut short,
		// so the road would have nothing to pull even if it were installed. A
		// Background that is being WRAPPED into a cancellable one is the
		// opposite, and is how both doors get their context, so those are
		// gathered first and forgiven.
		wrapped := map[token.Pos]bool{}
		ast.Inspect(door.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, _ := selector.X.(*ast.Ident)
			if receiver == nil || receiver.Name != "context" {
				return true
			}
			switch selector.Sel.Name {
			case "WithCancel", "WithTimeout", "WithDeadline":
				for _, argument := range call.Args {
					wrapped[argument.Pos()] = true
				}
			}
			return true
		})

		road := false
		background := token.NoPos
		ast.Inspect(door.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, _ := selector.X.(*ast.Ident)
			if receiver == nil {
				return true
			}
			switch {
			case receiver.Name == "leave" && selector.Sel.Name == "On":
				road = true
			case receiver.Name == "context" && selector.Sel.Name == "Background" && !wrapped[call.Pos()]:
				background = call.Pos()
			}
			return true
		})

		if !road {
			t.Errorf("%s does not take the shared leaving road, so a signal orphans whatever it is holding", name)
		}
		if background.IsValid() {
			t.Errorf("%s submits under an uncancellable context at %s, so the leaving road has nothing to pull",
				name, fset.Position(background))
		}
	}
}
