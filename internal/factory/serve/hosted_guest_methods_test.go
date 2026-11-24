package serve

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"
)

// P2-5
// Every remote method has exactly one class, and only the listed methods can reach the engine from a guest.
func TestP2_5_HostedGuestMethodTableLaw(t *testing.T) {
	fset := token.NewFileSet()
	paths, err := filepath.Glob("../../remote/*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("finding the wire sources: %v", err)
	}
	var sources []*ast.File
	var methods []string
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		var declarations []ast.Decl
		for _, decl := range source.Decls {
			group, ok := decl.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			declarations = append(declarations, group)
			for _, spec := range group.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if strings.HasPrefix(name.Name, "Method") {
						methods = append(methods, name.Name)
					}
				}
			}
		}
		source.Decls, source.Imports = declarations, nil
		sources = append(sources, source)
	}
	if len(methods) == 0 {
		t.Fatal("no wire method constants found")
	}
	// Resolving just constant declarations handles aliases and constant expressions without loading the engine.
	// An unrelated constant can depend on an import, so report unresolved values only for wire methods.
	conf := types.Config{Error: func(error) {}}
	pkg, _ := conf.Check("remote", fset, sources, nil)

	expected := map[string]string{}
	for class, names := range map[string]string{
		"view":   "Ping Detach Transcript EarlierHistory Title Usage ContextTokens Model SpendRail Effort ResolvedEffort ReasoningFor Autonomy ResolvedApproval AttachedSkills Held.Questions Question.Watch Task.Watch Task.Room Task.Pending Title.Watch Design.Watch CheckWork Render.List Render.Get App.Tasks App.Task App.QuickTasks App.ProgramActions PlanTasks PlanSpend PlanTaskPage RewindPoints",
		"steer":  "Steer Interrupt",
		"answer": "ResolveQuestion",
		"run":    "Submit SubmitBash SubmitImage SubmitFiles FollowUp UnqueueFollowUp ReplaceQuestion Typing Take",
	} {
		for _, name := range strings.Fields(names) {
			expected[name] = class
		}
	}
	seen := map[string]bool{}
	for _, method := range methods {
		c, ok := pkg.Scope().Lookup(method).(*types.Const)
		if !ok || c.Val().Kind() != constant.String {
			t.Errorf("could not resolve wire constant %s", method)
			continue
		}
		wire := constant.StringVal(c.Val())
		seen[wire] = true
		want := expected[wire]
		if want == "" {
			want = "owner"
		}
		// Go rejects duplicate constant keys in the table, even through an alias.
		if got := hostedGuestMethods[wire]; got != want {
			t.Errorf("%s (%q) class = %q, want %q", method, wire, got, want)
		}
	}
	for wire := range hostedGuestMethods {
		if !seen[wire] {
			t.Errorf("%q has a class but no wire constant", wire)
		}
	}
	for wire := range expected {
		if !seen[wire] {
			t.Errorf("the guest contract names %q without a wire constant", wire)
		}
	}
}
