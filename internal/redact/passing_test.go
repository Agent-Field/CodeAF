package redact

import (
	"fmt"
	"testing"
)

// A PASSING SECRET IS COVERED WHILE IT IS RECENT, and the list it lives in
// does not grow with every password a process is handed; one too short to
// be a secret is never registered, so ordinary words stay readable.
func TestPassingSecretsAreBoundedAndHaveAFloor(t *testing.T) {
	RegisterPassing("short")
	if got := Secrets("a short word"); got != "a short word" {
		t.Fatalf("a five-byte value was redacted: %q", got)
	}
	first := "the-first-passing-secret"
	RegisterPassing(first)
	if got := Secrets("x " + first + " y"); got != "x [redacted] y" {
		t.Fatalf("a passing secret was not covered: %q", got)
	}
	for i := 0; i < PassingMost; i++ {
		RegisterPassing(fmt.Sprintf("later-passing-secret-%03d", i))
	}
	passing.RLock()
	n := len(passing.values)
	passing.RUnlock()
	if n > PassingMost {
		t.Fatalf("%d passing secrets kept", n)
	}
	if got := Secrets("x " + first + " y"); got == "x [redacted] y" {
		t.Fatal("the oldest passing secret is still kept after the bound")
	}
}
