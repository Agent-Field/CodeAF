package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/livedoor"
)

// TestTheProgramWiresTheLiveDoorToTheEngineHost pins the road contract V1 needs
// when the chat is held by a daemon. Another computer can open such a chat only
// if the resolver was handed the engine host's own inspect and dial, and the
// live door package cannot import the engine host to do that itself. Without
// the call in livedoor_doors.go the resolver answers "nothing is holding" for
// every chat a daemon holds, and no other test in the program would notice.
func TestTheProgramWiresTheLiveDoorToTheEngineHost(t *testing.T) {
	if !livedoor.DaemonDoorsInstalled() {
		t.Fatal("the program started without handing the live door the engine host's inspect and dial")
	}
}
