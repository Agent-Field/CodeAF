package store_test

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// TestHeadInterruptKindMatchesHeadPackage pins 12.8.10's two halves together.
// internal/head/interrupt.go declares its own HeadInterruptKind because the
// store's kind list was closed to that lane; now that this lane has opened it,
// store.CommandHeadInterrupt must stay byte-identical to head.HeadInterruptKind
// or the door (RequestInterrupt) and the arm (ApplyInterrupt) would be talking
// about two different commands. This is a cross-package test on purpose: it
// lives outside both lanes' files and fails loudly if either spelling drifts.
func TestHeadInterruptKindMatchesHeadPackage(t *testing.T) {
	if store.CommandHeadInterrupt != head.HeadInterruptKind {
		t.Fatalf("store.CommandHeadInterrupt = %q, head.HeadInterruptKind = %q; the two halves of 12.8.10 must agree",
			store.CommandHeadInterrupt, head.HeadInterruptKind)
	}
}
