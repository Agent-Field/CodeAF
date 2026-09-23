package builtin

import "testing"

// Every program this build carries is one that can run: its definition
// validates, and no two share a name.
func TestEveryCarriedProgramIsWellDefined(t *testing.T) {
	seen := map[string]bool{}
	for _, program := range All() {
		if err := program.Validate(); err != nil {
			t.Errorf("%v", err)
		}
		if seen[program.Name] {
			t.Errorf("two programs are called %s", program.Name)
		}
		seen[program.Name] = true
	}
}
