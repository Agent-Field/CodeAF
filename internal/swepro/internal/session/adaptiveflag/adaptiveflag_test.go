package adaptiveflag

import (
	"os"
	"testing"
)

// adaptive-flag.ts ships no .test.ts of its own; these subtests encode the
// validation contract stated in its doc comment.

func TestAdaptiveCutsEnabled(t *testing.T) {
	t.Run("defaults ON when the variable is unset", func(t *testing.T) {
		os.Unsetenv(envVar)
		if !AdaptiveCutsEnabled() {
			t.Fatal("want enabled")
		}
	})

	t.Run("exactly \"0\" is the kill switch", func(t *testing.T) {
		t.Setenv(envVar, "0")
		if AdaptiveCutsEnabled() {
			t.Fatal("want disabled")
		}
	})

	t.Run("any other value leaves the chain ON", func(t *testing.T) {
		for _, v := range []string{"1", "", "00", "0 ", " 0", "0.0", "-0", "false", "off", "no", "true", "2"} {
			t.Setenv(envVar, v)
			if !AdaptiveCutsEnabled() {
				t.Fatalf("value %q: want enabled", v)
			}
		}
	})

	t.Run("the env var is read live on every call", func(t *testing.T) {
		t.Setenv(envVar, "1")
		if !AdaptiveCutsEnabled() {
			t.Fatal("want enabled")
		}
		os.Setenv(envVar, "0")
		if AdaptiveCutsEnabled() {
			t.Fatal("want disabled after mid-process flip")
		}
		os.Setenv(envVar, "1")
		if !AdaptiveCutsEnabled() {
			t.Fatal("want enabled after flipping back")
		}
	})
}
