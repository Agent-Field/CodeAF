package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// ── THE LAW: ONLY A WINDOW ON ANOTHER MACHINE SAYS IT IS A WINDOW THERE ─────
//
// [remote.Hello.Window] makes the far machine hold automations presence for a
// connection, and presence is a COUNT: the far clock runs while it is above
// zero, and a window asks it whether anybody else is still open before it lets
// a run in progress stop. So the field is said exactly once per window, on the
// one connection that lives as long as the window does:
//
//   - the ssh door and the relay door say it on their BOOT connection
//     (openChatV3Host, openChatV3At), and only there — a conversation opened
//     beside the first is the same window on another pipe, and saying it again
//     would count one window once per tab ([besideHello]);
//   - the local engine road says it NOWHERE. That window is a process on this
//     very machine and holds its own presence (chatv3_local.go's
//     keepAutomationsWindow), so the engine holding one too would count it
//     twice — and a window asking on its way out whether anybody else is still
//     open would find itself, and let a run stop without a word.
//
// The reading is the source, because the mistake this refuses — one more
// `Window: true` in a hello literal — compiles, runs, and fails only as a clock
// that keeps going with nobody in front of it.
func TestOnlyTheDoorsToAnotherMachineSayTheirWindowIsAWindowThere(t *testing.T) {
	for _, tc := range []struct {
		file, function string
		says           int
	}{
		{"chatv3_host.go", "openChatV3Host", 1},
		{"chatv3_host.go", "launchHello", 0},
		{"chatv3_host.go", "besideHello", 0},
		// The relay door dials twice inside one function: the boot connection,
		// which says it, and the closure that opens a conversation beside it,
		// which does not.
		{"chatv3_at.go", "openChatV3At", 1},
		{"chatv3_local.go", "", 0},
		{"chatv3_beside.go", "", 0},
		{"chatv3_taskowner.go", "", 0},
		{"team_resume.go", "", 0},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), tc.file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		said, found := 0, tc.function == ""
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || (tc.function != "" && fn.Name.Name != tc.function) {
				continue
			}
			found = true
			said += windowSaid(fn.Body)
		}
		if !found {
			t.Errorf("%s has no function %s; this law names the door that says Window — move the law with it", tc.file, tc.function)
			continue
		}
		where := tc.file
		if tc.function != "" {
			where += " " + tc.function
		}
		if said != tc.says {
			t.Errorf("%s says Hello.Window %d times, want %d: a window is counted on the far machine once, on its boot connection, and only when that machine is another one", where, said, tc.says)
		}
	}
}

// windowSaid counts the places a body sets a hello's Window: a `Window:` key in
// a composite literal, or an assignment to a `.Window` selector.
func windowSaid(body *ast.BlockStmt) int {
	said := 0
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.KeyValueExpr:
			if key, ok := n.Key.(*ast.Ident); ok && key.Name == "Window" {
				said++
			}
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Window" {
					said++
				}
			}
		}
		return true
	})
	return said
}
