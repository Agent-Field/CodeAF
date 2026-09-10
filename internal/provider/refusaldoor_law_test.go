package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ── THE LAW: A REFUSAL BECOMES A LEDGER FACT IN ONE PLACE ───────────────────
//
// The defect this law guards against is not a wrong line, it is a SECOND line:
// the pacing note lived in retry.go's status loop and the strike lived in
// velocity.go, and a 429 delivered inside an open 200 stream reached neither
// (client.go's stream branch, three Io Net refusals in ninety-three seconds on
// 2026-09-10). Two writers of one ledger will always come to know different
// halves of the same sentence, so there is exactly one — [Client.refuseLane] —
// and this test fails the build the day a second appears.
//
// IT NAMES THE WRITERS RATHER THAN THE CALLERS, because callers are meant to
// multiply: every transport, every recovery, every fork that reads a refusal
// should be able to reach the door. What may not multiply is the set of places
// that WRITE, and those are these two.
func TestOneDoorWritesEveryRefusalIntoTheLedger(t *testing.T) {
	// ledgerWriters are the calls that turn a refusal into something the next
	// encode reads: the pacing note, and the serving set's own refusal.
	ledgerWriters := map[string]string{
		"notePacedProvider": "refuseLane",
		"refuseServing":     "refuseLane",
	}

	callers := map[string][]string{}
	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				called := selector.Sel.Name
				if _, watched := ledgerWriters[called]; !watched {
					return true
				}
				callers[called] = append(callers[called], function.Name.Name)
				return true
			})
		}
	}

	for writer, door := range ledgerWriters {
		got := callers[writer]
		sort.Strings(got)
		if len(got) != 1 || got[0] != door {
			t.Fatalf("%s is called from %v, want only %s — every refusal becomes a "+
				"ledger fact at the one door (velocity.go's [Client.refuseLane])", writer, got, door)
		}
	}
}
