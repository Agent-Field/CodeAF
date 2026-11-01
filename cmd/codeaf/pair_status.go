package main

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// pairingStatus distinguishes a live pairing from local chats before showing a
// fresh invitation. Replacement is a remedy only after this device was removed.
func (d pairDoor) pairingStatus(ctx context.Context, route pair.Mailbox, replace bool) error {
	j := d.joining(replace)
	if _, err := identity.Load(j.Home); errors.Is(err, identity.ErrNone) {
		return nil
	} else if err != nil {
		return err
	}
	if identity.Solo(j.Home) {
		return nil
	}
	client, id, err := homeDirectory(j.Home, route)
	if err != nil {
		return err
	}
	listing, err := client.List(ctx)
	if errors.Is(err, directory.ErrRevoked) {
		return nil
	}
	if err != nil {
		return err
	}
	dev, err := identity.Device(j.Home)
	if err != nil {
		return err
	}
	if listing.Devices[dev.ID()].Revoked {
		return nil
	}
	var names []string
	for key, record := range listing.Devices {
		if key != dev.ID() && !record.Revoked {
			names = append(names, openName(directory.MetadataKey(id.CellKey()), record.Name))
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return errors.New("this computer is already paired with " + strings.Join(names, " and ") + " — codeaf devices lists them")
}
