package main

import (
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// The sidebar's remembered rung, and the door back to disk.
//
// It is a PAIR — a reader and a writer handed to the surface together — because
// this is one of the two interface settings a person changes with their hands
// rather than by visiting the settings sheet: ctrl+o walks the rail's three
// positions, the way [ and ] nudge the divider. config.Options.SplitPct and
// SaveSplitPct are the same shape for the same reason.
//
// It lives in its own file beside chatv2_linear.go, and for the same stated
// reason: the seam that lets `aforge chat --v2` reach a registry row is a
// different thing from the window's assembly, and keeping them apart means a
// wave editing one does not collide with a wave editing the other.

// resolveRail is where this window's rail opens: AFORGE_RAIL before the
// profile's persisted choice before the built-in default, open.
//
// There is no command-line flag for it and there should not be. A flag is for
// an invocation ("render this run in linear mode"); the sidebar is a standing
// preference, and the whole point of persisting it is that the reader never has
// to say it twice.
func resolveRail() string {
	return config.RailStateAt(profileDirFromEnv())
}

// saveRail records the rung the reader has just collapsed to.
//
// A FAILED WRITE IS DROPPED ON PURPOSE. The rail has already moved on screen by
// the time this runs, and refusing the gesture — or worse, putting a config
// error on a status line — would make an unwritable profile directory into a
// terminal that cannot collapse its own sidebar. The cost of losing it is that
// the next window opens where the last one was told to, which is exactly the
// behaviour of a window that has never been told anything.
func saveRail(state string) {
	_ = config.SaveRailState(profileDirFromEnv(), state)
}

// profileDirFromEnv is the same read chatv2_linear.go makes, named once so the
// two seams cannot come to different conclusions about which profile this
// window belongs to.
func profileDirFromEnv() string {
	return strings.TrimSpace(os.Getenv("AFORGE_PROFILE_DIR"))
}
