package main

import (
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/livedoor"
)

// THE LIVE DOOR REACHES AN ENGINE HOST THROUGH THE SAME TWO CALLS EVERY OTHER
// DOOR USES. The live door is started from the sync package, and the engine-host
// package imports the sync package, so the live door cannot import the engine
// host itself. This is the one place the two meet: the program hands the resolver
// the engine host's own inspect and dial, so a chat held by a daemon is found by
// exactly the rule that decides which daemon holds a workspace, including the
// pid an older host does not state and the socket path rules. Nothing else here
// can start a host; both calls only ask or connect.
func init() {
	livedoor.SetDaemonDoors(livedoor.DaemonDoors{
		Inspect: func(workspace string) (livedoor.Holder, error) {
			held, err := enginehost.Inspect(workspace)
			if err != nil {
				return livedoor.Holder{}, err
			}
			return livedoor.Holder{Self: held.Self, Answered: held.Answered}, nil
		},
		Dial: enginehost.Dial,
	})
}
