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
	closed := false
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
		case *ast.DeferStmt:
			ast.Inspect(typed.Call, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Close" {
					return true
				}
				receiver, _ := selector.X.(*ast.Ident)
				if receiver != nil && receiver.Name == "agent" {
					closed = true
				}
				return true
			})
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
	if !closed {
		t.Fatal("runChatV3Once does not defer agent.Close")
	}
}
