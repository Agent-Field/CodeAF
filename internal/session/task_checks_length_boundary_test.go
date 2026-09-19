package session

import (
	"strings"
	"testing"
)

func TestTheDeclaredAndRunnableDoorsShareTheLengthBoundary(t *testing.T) {
	atLimit := "printf " + strings.Repeat("x", 1000-len("printf "))
	overLimit := atLimit + "x"

	declared, refusal := declaredCheckList([]string{atLimit})
	if refusal != "" || len(declared) != 1 || declared[0] != atLimit {
		t.Fatalf("1000-byte check: declared = %q, refusal = %q", declared, refusal)
	}
	runnable := runnableChecks([]string{atLimit}, taskCopy{})
	if len(runnable) != 1 || runnable[0] != atLimit {
		t.Fatalf("1000-byte check absent from runnable door: %q", runnable)
	}
	if declared, refusal = declaredCheckList([]string{overLimit}); declared != nil || refusal != "Invalid arguments: checks must each be ONE rerunnable command: this check is 1001 bytes and a check may be at most 1000" {
		t.Fatalf("1001-byte check: declared = %q, refusal = %q", declared, refusal)
	}

	// BOTH DOORS MEASURE THE SAME TEXT. commandLike admits the trimmed command,
	// so a check padded to the limit with spaces around it is inside the limit,
	// and a refusal that counted the padding would name a length the door never
	// counted.
	padded := "  " + atLimit + "  "
	if declared, refusal = declaredCheckList([]string{padded}); refusal != "" || len(declared) != 1 || declared[0] != atLimit {
		t.Fatalf("padded 1000-byte check: declared = %q, refusal = %q", declared, refusal)
	}
	if got := checkShapeRefusal(padded + " "); strings.Contains(got, "this check is") {
		t.Fatalf("a check inside the limit was refused for its length: %q", got)
	}
}
