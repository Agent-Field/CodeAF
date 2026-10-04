package main

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/atlas"
)

// runAtlas opens the architecture map: the two-computer work — pairing,
// devices, cells, sync and the furrow engine — drawn as boxes and arrows a
// person can drag, open and step through (internal/atlas). It reads nothing
// and changes nothing; it is a picture of the code, and internal/atlas holds
// the only version of it.
func runAtlas(args []string) error {
	flags := commandFlags("atlas")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: codeaf atlas")
	}
	// It draws one screen and waits on a person, like the chat surface — the
	// same scheduler and heap bargain, from the same dispatch seam.
	tuneForTheSurface()
	return atlas.Run()
}
