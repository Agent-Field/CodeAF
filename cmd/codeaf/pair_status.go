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
	s, err := joinStanding(ctx, d.joining(replace).Home, route)
	if err != nil {
		return err
	}
	return s.refusal()
}

// pairStanding is what the directory says about a computer that is about to
// join another's chats, read once before any code or link is used.
type pairStanding struct {
	// known is false when there was nothing to ask: no identity, or a solo one.
	known bool
	// revoked is this computer's own device having been removed.
	revoked bool
	// rotated is this computer's identity having been replaced by a rotation, so
	// joining may be following it rather than leaving it.
	rotated bool
	// others names the other devices still in this computer's identity.
	others []string
}

// joinStanding reads this computer's standing in its own directory. A computer
// whose device was removed reads as revoked; any other refusal is the error.
func joinStanding(ctx context.Context, dir string, route pair.Mailbox) (pairStanding, error) {
	if _, err := identity.Load(dir); errors.Is(err, identity.ErrNone) {
		return pairStanding{}, nil
	} else if err != nil {
		return pairStanding{}, err
	}
	if identity.Solo(dir) {
		return pairStanding{}, nil
	}
	client, id, err := homeDirectory(dir, route)
	if err != nil {
		return pairStanding{}, err
	}
	listing, err := client.List(ctx)
	if errors.Is(err, directory.ErrRevoked) {
		return pairStanding{known: true, revoked: true}, nil
	}
	if err != nil {
		return pairStanding{}, err
	}
	dev, err := identity.Device(dir)
	if err != nil {
		return pairStanding{}, err
	}
	s := pairStanding{known: true, revoked: listing.Devices[dev.ID()].Revoked, rotated: listing.Identity.Rotation != nil}
	if s.revoked {
		return s, nil
	}
	for key, record := range listing.Devices {
		if key != dev.ID() && !record.Revoked {
			s.others = append(s.others, openName(directory.MetadataKey(id.CellKey()), record.Name))
		}
	}
	sort.Strings(s.others)
	return s, nil
}

// refusal is the answer for a computer whose chats already sync with others:
// joining another's chats would set them aside, so no code or link is used, and
// no replacement is offered either. A removed computer may pair back, and one
// whose identity was rotated may follow it, so neither is refused here.
func (s pairStanding) refusal() error {
	if !s.known || s.revoked || s.rotated || len(s.others) == 0 {
		return nil
	}
	return pairedWithError{names: s.others}
}

// alone is a computer that no other device still holds the identity of. Its
// chats are its own, so the join treats it as solo and refuses its own work
// before a code is used.
func (s pairStanding) alone() bool {
	return s.known && !s.revoked && !s.rotated && len(s.others) == 0
}

// pairedWithError is the already-paired refusal. The terminal sentence points at
// `codeaf devices`; the app words it itself (appPairFailure).
type pairedWithError struct{ names []string }

func (e pairedWithError) Error() string {
	return "this computer is already paired with " + strings.Join(e.names, " and ") + " — codeaf devices lists them"
}
