package trace

import (
	"fmt"
	"testing"
)

// SecretPassing has Secret's floor and a bound of its own.
func TestPassingSecretsAreBoundedAndHaveAFloor(t *testing.T) {
	SecretPassing("short")
	if got := string(ScrubRegistered([]byte("a short word"))); got != "a short word" {
		t.Fatalf("a five-byte value was scrubbed: %q", got)
	}
	for i := 0; i < 3*passingMost; i++ {
		SecretPassing(fmt.Sprintf("trace-passing-secret-%03d", i))
	}
	secrets.mutex.Lock()
	n := len(secrets.passing)
	secrets.mutex.Unlock()
	if n > passingMost {
		t.Fatalf("%d passing secrets kept", n)
	}
	last := fmt.Sprintf("trace-passing-secret-%03d", 3*passingMost-1)
	if got := string(ScrubRegistered([]byte(last))); got == last {
		t.Fatal("the latest passing secret is not scrubbed")
	}
}
