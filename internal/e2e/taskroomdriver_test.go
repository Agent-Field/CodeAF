package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// The live subtest can land done and fit both conversations on its first
// frame. This gate also covers the driver when your call pushes its own row
// below the fold, without paying for a second model run to choose that shape.
func TestTaskRoomDriverWalksToTheUntitledConversation(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(moduleRoot(t), "internal", "e2e", "tui_e2e_test.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var body *ast.BlockStmt
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Name.Name == "testTaskRoomKeepsSpace" {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatal("task-room driver is missing")
	}
	var untitled, named, record, pane token.Pos
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		word := ""
		for _, arg := range call.Args {
			if say, ok := arg.(*ast.CallExpr); ok && hostCallName(say) == "say" && len(say.Args) == 2 {
				word, _ = hostString(say.Args[1])
			}
		}
		if word == "tasksUnusedConversationSelectedWord" {
			if hostCallName(call) != "walkTo" || len(call.Args) != 3 {
				t.Error("titleless row must be reached by walking, not awaited on the first frame")
			} else if key, _ := hostString(call.Args[2]); key != "Down" {
				t.Error("titleless row must be reached with Down")
			} else {
				untitled = call.Pos()
			}
		}
		if word == "tasksUntitledWord" {
			if hostCallName(call) != "strings.Contains" || !untitled.IsValid() {
				t.Error("titleless word must be checked after walking to the unused conversation")
			} else {
				named = call.Pos()
			}
		}
		if word == "tasksEnterInsideWord" && hostCallName(call) == "walkTo" && untitled.IsValid() && !record.IsValid() {
			record = call.Pos()
		}
		if word == "tasksPaneOpenWord" && !pane.IsValid() {
			pane = call.Pos()
		}
		return true
	})
	if !untitled.IsValid() || !named.IsValid() || !record.IsValid() || !pane.IsValid() || !(untitled < named && named < record && record < pane) {
		t.Fatal("driver must reach the untitled conversation, then return to the record before reading its pane")
	}
}
