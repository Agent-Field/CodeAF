package relayserve

import "testing"

// C15 requires help to describe the relay without starting it or failing.
func TestRelayHelpSucceeds(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		if err := Main([]string{arg}); err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
	}
}
