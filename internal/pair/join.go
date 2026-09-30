package pair

// Joining a device's chats: type the code the other device shows, compare three
// words with its screen, and this computer has the same chats.
//
// NOTHING IS WRITTEN UNTIL THE OTHER DEVICE HAS SAID YES. The grant arrives in
// memory, is checked whole, and only then does anything touch the disk; a
// computer that already has chats of its own refuses before it writes a byte.

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// JoinUI is what a person sees on the device that types the code.
type JoinUI interface {
	// Waiting shows the three words the other device should be showing, while a
	// person there decides.
	Waiting(words string)
}

// Joining is one device asking to join another's chats.
type Joining struct {
	// Home is the codeaf home the identity is adopted into.
	Home string
	// Label is what this device calls itself on the other device's screen.
	Label string
	// Replace lets a computer that already has different chats give them up.
	Replace bool
	// SaveSyncURL is told the relay the other device syncs through, when it is
	// not the default. Nil ignores it.
	SaveSyncURL func(url string) error
}

// Joined is how a pairing ended on the joining device.
type Joined struct {
	// Already is true when this computer held these chats before, so nothing
	// was changed.
	Already bool
	// Successor is true when this computer was on an identity that the grant's
	// replaced, and followed it: its vault is sealed again under the new keys
	// and nothing it held was given up.
	Successor bool
}

// Join reads the typed code, runs the introduction and adopts the grant.
func Join(ctx context.Context, route Mailbox, j Joining, typed string, ui JoinUI) (Joined, error) {
	code, err := ReadJoinCode(typed)
	if err != nil {
		return Joined{}, err
	}
	if err := checkRelay(ctx, route); err != nil {
		return Joined{}, err
	}
	grant, err := joinRun(ctx, route, j, code, ui)
	if err != nil {
		return Joined{}, err
	}
	return adopt(j, grant)
}

// joinRun is the exchange: it answers the grant, and touches no file.
func joinRun(ctx context.Context, route Mailbox, j Joining, code JoinCode, ui JoinUI) (Grant, error) {
	key, err := pairbox.NewKey()
	if err != nil {
		return Grant{}, err
	}
	ctx, stop := context.WithTimeout(ctx, CodeValidFor+ConfirmWithin)
	defer stop()
	link := &boxLink{ctx: ctx, box: route.Box, plate: code.Plate, mine: pairbox.SideB, theirs: pairbox.SideA, key: key,
		gone:     map[stage]error{awaitAnswer: ErrStopped, awaitVerdict: ErrCodeDidNotWork},
		sendGone: NothingWaitingUnder(code.Plate)}
	offer, err := joinOffer(j.Label)
	if err != nil {
		return Grant{}, err
	}
	intro, err := begin(link, chatScheme(code.Plate), code.Digits, offer)
	if err != nil {
		return Grant{}, phraseJoin(err, route)
	}
	ui.Waiting(intro.Words)
	said, err := intro.verdict()
	if err != nil {
		return Grant{}, phraseJoin(err, route)
	}
	return hearGrant(said)
}

// phraseJoin gives the joining device's failures their sentences: the code that
// did not work is the same fact to a person whether the far end hung up or the
// key did not match.
func phraseJoin(err error, route Mailbox) error {
	if errors.Is(err, ErrWrongCode) {
		return ErrCodeDidNotWork
	}
	return phraseBox(err, route.Host)
}

// joinOffer is what this device sends in message 3: its name, then random bytes
// that make the message differ from any other session's.
func joinOffer(label string) ([]byte, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append([]byte(readableLabel(label)), nonce...), nil
}

// adopt takes the grant into this home, or says why not. A computer that
// already holds these chats changes nothing, and one with chats of its own keeps
// them unless it was told to replace them.
func adopt(j Joining, grant Grant) (Joined, error) {
	held, err := identity.Load(j.Home)
	switch {
	case err == nil && held.ID() == grant.Identity.ID():
		return Joined{Already: true}, nil
	case err == nil && slices.Contains(grant.Replaces, held.ID()):
		return Joined{Successor: true}, follow(j, held, grant)
	}
	if _, err := identity.Adopt(j.Home, grant.Identity, j.Replace); err != nil {
		return Joined{}, adoptFailure(err)
	}
	if grant.SyncURL != "" && j.SaveSyncURL != nil {
		return Joined{}, j.SaveSyncURL(grant.SyncURL)
	}
	return Joined{}, nil
}

// follow moves this computer from an identity that was rotated to the one that
// replaced it. The vault is sealed under the new key first (into a file beside
// the live one), then the identity changes, then the vault is put in place; a
// crash between the last two is finished by the vault's own next read.
func follow(j Joining, held identity.Identity, grant Grant) error {
	if _, err := keys.StageReseal(j.Home, held.CellKey(), grant.Identity.CellKey()); err != nil {
		return err
	}
	if _, err := identity.Adopt(j.Home, grant.Identity, true); err != nil {
		return adoptFailure(err)
	}
	if err := errors.Join(keys.CommitReseal(j.Home), identity.RecordPredecessor(j.Home, held.ID())); err != nil {
		return err
	}
	if grant.SyncURL != "" && j.SaveSyncURL != nil {
		return j.SaveSyncURL(grant.SyncURL)
	}
	return nil
}

func adoptFailure(err error) error {
	if errors.Is(err, identity.ErrDifferent) {
		return ErrDifferentChats
	}
	return err
}
