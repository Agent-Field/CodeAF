package main

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/resident"
)

// requeueWords is the ↻ line for a claim that went back on the queue with a
// record behind it: how much was picked up, and what stopped the attempt that
// banked it.
//
// TWO FACTS, TWO SOURCES, NEITHER OF THEM RE-WORDED HERE. The count is the
// release payload's own — the same number the next claim's resume seed is built
// from — and the why is the release's own reason with its turn-count clause
// taken off by the package that wrote it ([resident.ReleaseWhy]). Composing the
// why here from anything else is how a stream ends up saying one thing on the
// ⏳ line and a different thing one line under it.
func requeueWords(recorded int, reason string) string {
	picked := fmt.Sprintf("picked up again from %s", plural(recorded, "recorded turn"))
	why := resident.ReleaseWhy(firstLine(reason))
	if why == "" {
		return picked
	}
	return picked + " — " + why
}
