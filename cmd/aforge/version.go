package main

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/buildinfo"
)

// runVersion answers the one question an installer, a doctor, or a packaging
// script asks before it trusts the binary on the PATH: is aforge here, and
// which one is it. It reads nothing, writes nothing, and needs no API key —
// probing for the binary must never be a configuration problem.
func runVersion() error {
	fmt.Println(versionString())
	return nil
}

func versionString() string {
	return "aforge " + resolvedVersion()
}

// resolvedVersion has one source with presence and session metadata, so the
// command can never name a different build from the one those files name.
func resolvedVersion() string {
	return buildinfo.String()
}
