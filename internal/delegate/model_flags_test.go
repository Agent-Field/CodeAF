package delegate

import (
	"strings"
	"testing"
)

func TestAuxiliaryModelFlagsMustBeDeclaredByTheCommand(t *testing.T) {
	program := testProgram(nil)
	program.AuxiliaryModelFlags = []string{"variant"}
	if err := program.Validate(); err != nil {
		t.Fatalf("a declared model flag was refused: %v", err)
	}
	for _, flag := range []string{"missing", ""} {
		program.AuxiliaryModelFlags = []string{flag}
		if err := program.Validate(); err == nil || !strings.Contains(err.Error(), "auxiliary model flag") {
			t.Fatalf("undeclared auxiliary flag %q was not refused: %v", flag, err)
		}
	}
}
