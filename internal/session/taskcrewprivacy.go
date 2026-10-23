package session

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// A saved or already-routed task may predate the current free-route setting.
// Explicit pins retain consent to their named model; automatic seats do not.
func (c crewSeatCompleter) freeRouteBlocked(current string) bool {
	for _, pick := range c.run.crew.current().Crew {
		if pick.Send == current && !pick.Pinned && (pick.Kind == crewroute.Free || crewroute.IsFree(pick.Send)) {
			return !config.CrewFreeRoutesAt(c.agent.config.ProfileDir)
		}
	}
	return false
}
