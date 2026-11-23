package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A valid shell command can expand sixfold in JSON (HTML escaping). Count
// bounds alone must not let codeaf write a file its next launch cannot read.
func TestMetadataBudgetRejectsBeforeOverwritingTheReadableStore(t *testing.T) {
	h := newHarness(t)
	accepted := 0
	rejected := false
	for n := 0; n < maxServices; n++ {
		spec := webSpec(fmt.Sprintf("web-%d", n), h.dir)
		spec.Command = fmt.Sprintf("printf '%%s' '%s' # %d", strings.Repeat("<", 4000), n)
		_, err := h.s.Keep(fmt.Sprintf("agent-%d", n/maxPerOwner), spec, PolicyNever, Provenance{})
		if err != nil {
			if !errors.Is(err, ErrNotSaved) {
				t.Fatalf("metadata budget refusal must be actionable: %v", err)
			}
			rejected = true
		} else {
			accepted++
		}
		image, readErr := readStore(h.dir)
		if readErr != nil {
			t.Fatalf("an accepted mutation wrote an unreadable store after %d services: %v", accepted, readErr)
		}
		if len(image.Services) != accepted || h.rt.starts != accepted {
			t.Fatalf("refused mutation must conserve prior records and start no process: records=%d starts=%d accepted=%d", len(image.Services), h.rt.starts, accepted)
		}
		if rejected {
			break
		}
	}
	if !rejected || accepted == 0 {
		t.Fatalf("fixture did not exercise metadata admission: accepted=%d rejected=%t", accepted, rejected)
	}
}
