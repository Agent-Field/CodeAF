package syncsetup

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// FleetSize is how many devices this person has paired and not revoked, read
// from the directory. It is the fact the home card that offers another machine
// waits on.
func (s *Sync) FleetSize(ctx context.Context) (int, error) {
	l, err := s.list(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range l.Devices {
		if !d.Revoked {
			n++
		}
	}
	return n, nil
}

// Roster lists the devices that are not revoked, this one first, with the names
// opened, for the row that shows who is online.
func (s *Sync) Roster(ctx context.Context) ([]chatlist.Device, error) {
	l, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	key := directory.MetadataKey(s.Identity.CellKey())
	open := func(sealed string) (string, error) { return directory.OpenName(key, sealed) }
	return chatlist.Devices(l, s.Device.ID(), open), nil
}

// Engage ends the quiet: the person opened the add-machine card, so this
// machine may now ask the sync service about its fleet.
func (s *Sync) Engage() { _ = identity.EndSolo(s.Home) }
