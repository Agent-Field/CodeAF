package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// ── A RUN THAT IS RECORDING MAKES ITS OWN CALLS ─────────────────────────────
//
// THE MEASURED FAILURE (issue #1022). `CODEAF_DEBUG=1 codeaf` wrote a folder
// holding `run.json` and not one request body, four launches running: the header
// is written by the process at the door and the calls were made by this
// workspace's session host, which was never told to record. `codeaf chat
// --debug` wrote them all, because the host rule read the FLAG while the record
// has three doors and the flag is one of them.
//
// The rule below is the switch and not the flag, and this is the law that keeps
// it so: a shape test, because there is no way to reach the wiring through the
// door without opening a terminal, and because a flag re-read here is exactly
// how it was wrong.

// TestTheHostRoadIsDecidedByTheRecordsSwitchAndNotOneOfItsDoors reads the chat
// door's own source: the `debug` field of the host choice it builds must be the
// switch ([trace.Enabled]), never the flag variable it parsed a moment earlier.
func TestTheHostRoadIsDecidedByTheRecordsSwitchAndNotOneOfItsDoors(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "chatv3.go", nil, 0)
	if err != nil {
		t.Fatalf("read chatv3.go: %v", err)
	}

	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if name, _ := literal.Type.(*ast.Ident); name == nil || name.Name != "v3HostChoice" {
			return true
		}
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, _ := pair.Key.(*ast.Ident); key == nil || key.Name != "debug" {
				continue
			}
			found = true
			var written strings.Builder
			if err := printer.Fprint(&written, fset, pair.Value); err != nil {
				t.Fatalf("print the debug field: %v", err)
			}
			if got := written.String(); got != "trace.Enabled()" {
				t.Fatalf(
					"the chat door decides the host road from %s at %s; it must read the record's own switch, "+
						"trace.Enabled(), so the environment pin keeps the calls in this process exactly as --debug does",
					got, fset.Position(pair.Value.Pos()),
				)
			}
		}
		return true
	})
	if !found {
		t.Fatal("chatv3.go builds no v3HostChoice with a debug field; this law has stopped watching the chat door")
	}
}

// AND THE RULE ITSELF REFUSES THE HOST ROAD WHILE THE RECORD IS ON, whichever
// door turned it on — the other half of the same promise, asserted on the rule
// rather than on the source.
func TestARecordingLaunchKeepsItsCallsInThisProcess(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	answeringHost(t, workspace)
	if !v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("a host was answering and an ordinary launch did not take it, so this test proves nothing")
	}
	if v3TakeHostRoad(workspace, v3HostChoice{debug: true}) {
		t.Fatal("a recording launch handed its calls to a host that was never told to record")
	}
}
