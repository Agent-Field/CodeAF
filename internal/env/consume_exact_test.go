package env

import (
	"os"
	"testing"
)

func TestConsumeExactNeverAcceptsAnInheritedLegacyArrangement(t *testing.T) {
	const canonical = "CODEAF_TEST_ARRANGEMENT"
	alias := Legacy(canonical)
	for _, current := range []string{"", "v1:current-parent"} {
		t.Run(current, func(t *testing.T) {
			t.Setenv(canonical, current)
			t.Setenv(alias, "v1:inherited-parent")
			if got := ConsumeExact(canonical); got != current {
				t.Fatal("legacy arrangement replaced explicit launcher provenance")
			}
			if _, ok := os.LookupEnv(canonical); ok {
				t.Fatal("canonical arrangement inherited by child")
			}
			if _, ok := os.LookupEnv(alias); ok {
				t.Fatal("legacy arrangement inherited by child")
			}
			if got := ConsumeExact(canonical); got != "" {
				t.Fatal("arrangement reused after consumption")
			}
		})
	}
}
