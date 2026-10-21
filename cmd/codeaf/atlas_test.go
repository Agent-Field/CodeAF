package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/atlas"
)

// AN UNKNOWN NAME REFUSES, naming what was asked and what the registry holds
// instead (atlas.NoMap), and the refusal is the error the exit carries —
// `codeaf atlas nope` leaves non-zero with it on stderr. Opening a real map
// needs a terminal, so the door it waits behind is the pty run in the verify
// steps; what a unit test can hold is the registry's answer.
func TestAtlasUnknownNameRefusesWithTheRegistryLine(t *testing.T) {
	err := runAtlas([]string{"nope"})
	if err == nil {
		t.Fatal("an unknown name opened nothing and refused nothing")
	}
	if !strings.Contains(err.Error(), atlas.NoMap("nope")) {
		t.Fatalf("the refusal is not the registry's sentence: %v", err)
	}
	if !strings.Contains(err.Error(), `no map "nope"`) {
		t.Fatalf("the refusal does not name what was asked: %v", err)
	}
	for _, mp := range atlas.Maps {
		if !strings.Contains(err.Error(), mp.Name) {
			t.Fatalf("the refusal does not name %q: %v", mp.Name, err)
		}
	}
}

// TOO MANY WORDS IS A USAGE LINE, not a map name strung together.
func TestAtlasTooManyWordsIsUsage(t *testing.T) {
	err := runAtlas([]string{"pairing", "pairing"})
	if err == nil || !strings.Contains(err.Error(), "usage: codeaf atlas [map]") {
		t.Fatalf("two names gave %v, want the usage line", err)
	}
}
