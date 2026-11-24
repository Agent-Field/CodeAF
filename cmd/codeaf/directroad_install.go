package main

import (
	"context"
	"io"

	"github.com/Agent-Field/codeaf/internal/directroad"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// THE DIRECT ROAD IS HANDED TO THE SYNC PACKAGE HERE AND NOWHERE ELSE. Its
// implementation is a WebRTC data channel, and importing it from syncsetup would
// put a peer-to-peer library into every package that links the door. The program
// is the one place that wants it, so this is where the two meet, the same way the
// live door is handed its engine host in livedoor_doors.go. Remote control being
// off, or CODEAF_REMOTE_ROAD=relay, still leaves this installed and unused:
// syncsetup decides per door, and builds nothing in either case.
func init() {
	syncsetup.SetDirectRoad(syncsetup.DirectRoad{
		NewHost: func(stun []string) syncsetup.DirectRoadHost {
			return directroad.New(directroad.Config{STUN: stun})
		},
		Offer: func(ctx context.Context, stun []string, s livedoor.Signaller, deliver <-chan []byte) (io.ReadWriteCloser, error) {
			return directroad.Offer(ctx, directroad.Config{STUN: stun}, s, deliver)
		},
	})
}
