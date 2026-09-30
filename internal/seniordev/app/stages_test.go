//go:build !windows

package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// THE HELLO NAMES THE RUN'S STAGES, ALL OF THEM AND ONLY THEM. codeaf draws
// the whole track of a run from its hello before the run has walked it, so a
// stage the run can emit and the hello did not name is a stop on no track,
// and a name the run never emits is a stop nobody reaches.
//
// The stages are read out of this package's sources with go/parser, as the
// first argument of every `.stage(…)` and `.emitStage(…)` call, so a stage
// added anywhere is held to the list the day it is written.
func TestTheHelloNamesEveryStageTheRunCanEmit(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	emitted := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "stage" && selector.Sel.Name != "emitStage") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			stage, err := strconv.Unquote(literal.Value)
			if err == nil {
				emitted[stage] = fset.Position(literal.Pos()).String()
			}
			return true
		})
	}
	if len(emitted) < 10 {
		t.Fatalf("only %d stages were read out of the sources; the reader has stopped working", len(emitted))
	}
	named := map[string]bool{}
	for _, stage := range Stages {
		if named[stage] {
			t.Errorf("the hello names %q twice", stage)
		}
		named[stage] = true
	}
	var unnamed, unreached []string
	for stage, where := range emitted {
		if !named[stage] {
			unnamed = append(unnamed, stage+" ("+where+")")
		}
	}
	for _, stage := range Stages {
		if _, ok := emitted[stage]; !ok {
			unreached = append(unreached, stage)
		}
	}
	sort.Strings(unnamed)
	sort.Strings(unreached)
	if len(unnamed) > 0 {
		t.Errorf("the run emits stages its hello does not name: %v", unnamed)
	}
	if len(unreached) > 0 {
		t.Errorf("the hello names stages the run never emits: %v", unreached)
	}
}
