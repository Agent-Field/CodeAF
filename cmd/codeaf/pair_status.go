package main

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// standingRoute is where the terminal reads this computer's own set: where it
// pairs, unless --via names another relay. That relay is the other computer's
// and has never heard of this computer's set, so the set is read where this
// computer syncs.
func (d pairDoor) standingRoute(home, relayFlag string) func() (pair.Mailbox, error) {
	return func() (pair.Mailbox, error) {
		if strings.TrimSpace(relayFlag) != "" {
			if own := syncsetup.Resolve(home); own.On() && own.URL != "" {
				return pair.Mailbox{URL: own.URL}, nil
			}
		}
		return d.mailbox(relayFlag)
	}
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
	// others names the other devices still in this computer's identity; a name
	// that cannot be read is empty, for each door to word.
	others []string
}

// joinStanding reads this computer's standing in its own directory. A computer
// whose device was removed reads as revoked, and one whose replaced identity the
// relay has deleted as rotated; any other refusal is the error. The directory's
// route is only worked out when there is something to ask, so a computer with
// no identity, or a solo one, contacts nothing.
func joinStanding(ctx context.Context, dir string, route func() (pair.Mailbox, error)) (pairStanding, error) {
	if _, err := identity.Load(dir); errors.Is(err, identity.ErrNone) {
		return pairStanding{}, nil
	} else if err != nil {
		return pairStanding{}, err
	}
	if identity.Solo(dir) {
		return pairStanding{}, nil
	}
	where, err := route()
	if err != nil {
		return pairStanding{}, err
	}
	client, id, err := homeDirectory(dir, where)
	if err != nil {
		return pairStanding{}, err
	}
	listing, err := client.List(ctx)
	switch {
	case errors.Is(err, directory.ErrRevoked):
		return pairStanding{known: true, revoked: true}, nil
	case errors.Is(err, wireauth.ErrGone):
		return pairStanding{known: true, rotated: true}, nil
	case err != nil:
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
	key := directory.MetadataKey(id.CellKey())
	for id, record := range listing.Devices {
		if id != dev.ID() && !record.Revoked {
			name, err := directory.OpenName(key, record.Name)
			if err != nil || strings.TrimSpace(name) == "" {
				s.others = append(s.others, "")
				continue
			}
			s.others = append(s.others, devname.Shown(name))
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
	return "this computer is already paired with " + e.list("a device") + " — codeaf devices lists them"
}

// list names the other computers, calling one whose name cannot be read unnamed.
func (e pairedWithError) list(unnamed string) string {
	names := make([]string, len(e.names))
	for i, name := range e.names {
		if name == "" {
			name = unnamed
		}
		names[i] = name
	}
	return strings.Join(names, " and ")
}
