package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/teams"
	"strings"
	"time"
)

// factoryRoomOwned requires local origin, not a server-supplied id or a crew name.
// A legacy local room can corroborate that origin through its actual conversations.
func factoryRoomOwned(f *teams.File, it factory.Item, room string) bool {
	t, ok := f.Team(room)
	if !ok || t.Root || t.Closed() || factoryIsParent(f, room) {
		return false
	}
	if t.FactoryManaged {
		return true
	}
	if it.Stream == nil {
		return false
	}
	s := *it.Stream
	s.Room = ""
	it.Stream = &s
	c, proved := legacyRoomClaimFor(f, it)
	return proved && c.Room == room
}

// factoryEnsureRoom recreates the logical job room on a new worker machine.
// An ordinary local crew with the same id is never adopted. A collision leaves
// this round teamless; it never clears or replaces the factory's logical id.
func factoryEnsureRoom(profile string, it factory.Item, room string) (string, error) {
	room = strings.TrimSpace(room)
	if !factoryrun.ValidRoom(room) {
		return "", nil
	}
	owned := ""
	err := teams.Update(profile, func(f *teams.File) error {
		if _, exists := f.Team(room); exists {
			if factoryRoomOwned(f, it, room) {
				for i := range f.Teams {
					if f.Teams[i].ID == room {
						f.Teams[i].FactoryManaged = true
					}
				}
				owned = room
			}
			return nil
		}
		now := time.Now()
		parent := factoryParentTeam(f, now)
		f.Teams = append(f.Teams, teams.Team{ID: room, Name: talkTeamName(it), Parent: parent, Made: now, FactoryManaged: true})
		owned = room
		return nil
	})
	return owned, err
}
