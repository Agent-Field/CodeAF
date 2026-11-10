package shell

import (
	"runtime"
	"strings"
	"testing"
)

// Contract 6: process advice is present in the real Windows shell footer only.
func TestContract6WindowsProcessFacts(t *testing.T) {
	if runtime.GOOS != "windows" {
		if fact := PromptFact(); fact != "" {
			t.Fatalf("Unix prompt changed: %q", fact)
		}
		return
	}
	if _, err := Bash(); err != nil {
		t.Skip("Git Bash is unavailable")
	}
	fact := PromptFact()
	for _, want := range []string{"python3", "Microsoft Store", "python", "py", "npm/npx/yarn", ".cmd", "jobs", "&", "kill", "native Windows"} {
		if !strings.Contains(fact, want) {
			t.Errorf("shell footer missing %q: %s", want, fact)
		}
	}
}
