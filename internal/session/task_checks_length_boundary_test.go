package session

import (
	"strings"
	"testing"
)

func TestTheDeclaredAndRunnableDoorsShareTheLengthBoundary(t *testing.T) {
	atLimit := "printf " + strings.Repeat("x", 4096-len("printf "))
	overLimit := atLimit + "x"

	declared, refusal := declaredCheckList([]string{atLimit})
	if refusal != "" || len(declared) != 1 || declared[0] != atLimit {
		t.Fatalf("4096-byte check: declared = %q, refusal = %q", declared, refusal)
	}
	runnable := runnableChecks([]string{atLimit}, taskCopy{})
	if len(runnable) != 1 || runnable[0] != atLimit {
		t.Fatalf("4096-byte check absent from runnable door: %q", runnable)
	}
	if declared, refusal = declaredCheckList([]string{overLimit}); declared != nil || refusal != "Invalid arguments: checks must each be ONE rerunnable command: each check may be at most 4096 bytes" {
		t.Fatalf("4097-byte check: declared = %q, refusal = %q", declared, refusal)
	}
}
