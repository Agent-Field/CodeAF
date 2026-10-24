package exec

import (
	"github.com/Agent-Field/codeaf/internal/executor"
	"runtime"
	"testing"
)

// C18 preserves the PDF helpers' complete output after a clean helper exit.
func TestDocumentCommandRetainsOutputFromInheritedPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	out, err := documentCommand(executor.With(t.Context(), executor.Host), "sh", "-c", "printf first; (sleep 0.4; printf second) & exit 0")
	if err != nil || string(out) != "firstsecond" {
		t.Fatalf("%q: %v", out, err)
	}
}
