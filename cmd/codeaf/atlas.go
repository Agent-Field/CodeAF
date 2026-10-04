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
//
// `codeaf atlas pairing` opens that map. Bare `codeaf atlas` shows the
// picker — one row per registered map — and enter opens the one the cursor
// is on. A name that is not in the registry refuses with the registry's own
// sentence, which names what was available instead.
func runAtlas(args []string) error {
	flags := commandFlags("atlas")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	switch flags.NArg() {
	case 0:
		mp, err := atlas.Pick()
		if err != nil {
			return err
		}
		if mp == nil {
			// A person who left the picker asked for nothing; that is not a
			// failure, and the shell gets its exit 0.
			return nil
		}
		// It draws one screen and waits on a person, like the chat surface — the
		// same scheduler and heap bargain, from the same dispatch seam.
		tuneForTheSurface()
		return atlas.RunMap(mp)
	case 1:
		mp, ok := atlas.ByName(flags.Arg(0))
		if !ok {
			return fmt.Errorf("%s", atlas.NoMap(flags.Arg(0)))
		}
		tuneForTheSurface()
		return atlas.RunMap(mp)
	default:
		return fmt.Errorf("usage: codeaf atlas [map]")
	}
}
