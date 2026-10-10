package main

import (
	"testing"
	"time"
)

// BE-SEC-06: the desktop bridge listens on loopback only. The sentence is the
// one runDesktopBridge returns before it binds.
//
// Mutation: delete the IsLoopback check in runDesktopBridge. 0.0.0.0:0 then
// binds and this test hits the deadline instead of the sentence. A public
// address fails the sentence check even when the bind itself is refused by
// the operating system.

const desktopListenSentence = "desktop transport must listen on a loopback address"

func TestDesktopBridgeRefusesUnspecifiedAndPublicListeners(t *testing.T) {
	for _, address := range []string{"0.0.0.0:0", "8.8.8.8:53"} {
		t.Run(address, func(t *testing.T) {
			done := make(chan error, 1)
			go func() {
				done <- runDesktopBridge([]string{"--listen", address})
			}()
			select {
			case err := <-done:
				if err == nil || err.Error() != desktopListenSentence {
					t.Fatalf("%s: %v", address, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%s was not refused with %q", address, desktopListenSentence)
			}
		})
	}
}
