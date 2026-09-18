package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeclaredCommandsAloneAreTheCheckerContract(t *testing.T) {
	declared := []string{"printf proof", "cat record"}
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
	if refusal, ok := doorRefusal("printf proof", door); ok {
		t.Fatalf("reading contract admitted an undeclared command: %s", refusal)
	}
}

func TestSessionContainsNoTrajectoryToContractInference(t *testing.T) {
	path := filepath.Join("task_checks.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"InvocableChecks":   true,
		"commandSegments":   true,
		"exitBearingRunner": true,
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && forbidden[function.Name.Name] {
			t.Errorf("trajectory inference function remains: %s", function.Name.Name)
		}
	}
}
