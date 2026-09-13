package config

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// THE SHIPPED ROUTING ROW, PINNED WHERE IT IS SPELLED TWICE.
//
// The word on disk is this package's ([DefaultRouting]) and the word on the
// wire is the transport's ([provider.DefaultRouting]), deliberately separate so
// a settings key's vocabulary cannot change because a package renamed a
// constant. Two spellings of one fact drift, so this holds them together: a
// profile nobody has written is in force as `simple`, nobody has chosen
// anything there, and the transport ships the same answer.
func TestAnUnwrittenProfileShipsSimpleRouting(t *testing.T) {
	dir := t.TempDir()
	if got := RoutingAt(dir); got != RoutingSimple {
		t.Fatalf("an unwritten profile routes as %q, want %q", got, RoutingSimple)
	}
	if got := RoutingChoiceAt(dir); got != "" {
		t.Fatalf("an unwritten profile reads as the choice %q, want nobody having chosen", got)
	}
	if DefaultRouting != RoutingSimple {
		t.Fatalf("the shipped row is %q, want %q", DefaultRouting, RoutingSimple)
	}
	if string(provider.DefaultRouting) != DefaultRouting {
		t.Fatalf("the wire ships %q and the settings row ships %q; one fact, two spellings, and they have drifted",
			provider.DefaultRouting, DefaultRouting)
	}
	// AND THE DEFAULT LEADS THE CYCLE, which is what every other choice row in
	// the sheet does: the row a person walks starts from what they already have.
	if len(RoutingModes) == 0 || RoutingModes[0] != DefaultRouting {
		t.Fatalf("the routing cycle is %v, want the shipped row first", RoutingModes)
	}
}

// AND A WORD ALREADY IN HAND RESOLVES THROUGH THE SAME DOOR. A surface holding
// the row — the picker's `auto` sentence is the one that matters — must not
// decide for itself what an empty or unknown word means.
func TestARoutingWordInHandResolvesToTheShippedRow(t *testing.T) {
	for _, word := range []string{"", "   ", "sideways", "LATENCY?"} {
		if got := RoutingWord(word); got != DefaultRouting {
			t.Fatalf("%q is in force as %q, want the shipped %q", word, got, DefaultRouting)
		}
	}
	if got := RoutingWord(" Latency "); got != RoutingLatency {
		t.Fatalf("a written word in force reads %q, want %q", got, RoutingLatency)
	}
}
