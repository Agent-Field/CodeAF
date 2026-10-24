package identity

import "testing"

// TestC30ExactStateNamesOutrankWildcards proves C30.4: classifying a startup
// file is deterministic and cannot depend on randomized Go map iteration.
func TestC30ExactStateNamesOutrankWildcards(t *testing.T) {
	table := rules{"*": unknown, "sweep.stamp": never}
	for i := 0; i < 100; i++ {
		if used, err := table.of("sweep.stamp")("unused"); used || err != nil {
			t.Fatal("wildcard hid the exact startup file")
		}
	}
}
