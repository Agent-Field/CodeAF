package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

func TestDeclaredCommandsAloneAreTheCheckerContract(t *testing.T) {
	declared := []string{"./verify proof", "./verify record"}
	checks, problem := declaredCheckList(declared)
	if problem != "" {
		t.Fatalf("declaration refused: %s", problem)
	}
	if !reflect.DeepEqual(checks, declared) {
		t.Fatalf("contract = %q, want every declaration %q", checks, declared)
	}
	door := auditDoorFor(declaringNode(checks...), standingOn(""))
	if !reflect.DeepEqual(door.checks, declared) {
		t.Fatalf("checker door = %q, want every declaration %q", door.checks, declared)
	}
	for _, command := range declared {
		if refusal, ok := doorRefusal(command, door); !ok {
			t.Fatalf("declared command %q lacks audit approval: %s", command, refusal)
		}
	}
}

func TestNoDeclarationLeavesAReadingOnlyCheckerDoor(t *testing.T) {
	door := auditDoorFor(declaringNode(), standingOn(""))
	if len(door.checks) != 0 {
		t.Fatalf("reading contract has commands: %q", door.checks)
	}
	if refusal, ok := doorRefusal("./verify proof", door); ok {
		t.Fatalf("reading contract admitted an undeclared command: %s", refusal)
	}
}

func TestSessionContainsNoTrajectoryToContractInference(t *testing.T) {
	packages, err := parser.ParseDir(token.NewFileSet(), ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"InvocableChecks":   true,
		"commandSegments":   true,
		"exitBearingRunner": true,
	}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if ok && forbidden[function.Name.Name] {
					t.Errorf("trajectory inference function remains: %s", function.Name.Name)
				}
			}
		}
	}
}
