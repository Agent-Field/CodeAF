package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// TestTheProgramInstallsTheDirectRoad pins the seam the direct road needs: the
// sync package cannot import the peer-to-peer implementation without putting it
// into every package that links the door, so the program hands it over at start.
// Without the call in directroad_install.go no computer ever offers or answers a
// direct connection, remote control still works over the relay, and no other
// test in the program would notice.
func TestTheProgramInstallsTheDirectRoad(t *testing.T) {
	if !syncsetup.DirectRoadInstalled() {
		t.Fatal("the program started without handing the sync package the direct road")
	}
}
