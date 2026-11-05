package pair

// Joining a device's chats: type the code the other device shows, compare three
// words with its screen, and this computer has the same chats.
//
// NOTHING IS WRITTEN UNTIL THE OTHER DEVICE HAS SAID YES. The grant arrives in
// memory, is checked whole, and only then does anything touch the disk.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
	// Platform disambiguates computers with the same name during approval.
	Platform string
	// Replace lets a computer that already has different chats give them up.
	Replace bool
	// Revoked says this computer's own device was removed. Rejoining the same
	// identity then mints a new device key without replacing local state.
	Revoked bool
	// Alone says no other device that is still in holds this computer's
	// identity, which the caller learned from the directory. Such an identity
	// is solo in all but its marker (showing a code once ends the marker), so
	// its own work refuses before a code is used, as a solo one's does.
	Alone bool
	// ProfileDir names the preferences being joined, when outside Home.
	ProfileDir string
	// TeamsPaired supplies the live quiet gate after adopting the new identity.
	TeamsPaired func() bool
	// SaveSyncURL is told the relay the other device syncs through, when it is
	// not the default. Nil ignores it.
	SaveSyncURL func(url string) error
	// After runs after each durable local merge step. An error interrupts the
	// merge; startup or retry completes its recorded switch.
	After func(step string) error
}

// Joined is how a pairing ended on the joining device.
type Joined struct {
	// TeamsAside names the preserved team file after explicit replacement.
	TeamsAside string
	// With is an existing computer in the newly joined set of devices.
	With string
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
	// A solo identity cannot be the grant's identity, so known local work can
	// refuse before the approver spends an approval. One that no other device
	// still holds is refused the same way: nobody else could be offering it.
	if !j.Replace {
		if _, err := identity.Load(j.Home); err == nil && (identity.Solo(j.Home) || j.Alone) {
			if can, err := identity.Joinable(j.Home); err != nil || !can {
				return Joined{}, differentChats(j.Home)
			}
		}
	}
	if err := checkRelay(ctx, route); err != nil {
		return Joined{}, err
	}
	grant, intro, err := joinRun(ctx, route, j, code, ui)
	if err != nil {
		return Joined{}, err
	}
	return adoptWithDevice(j, grant, nil, func() error {
		// A person who stopped this side before it decided has not paired.
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := intro.took(); errors.Is(err, pairbox.ErrGone) {
			// The other device stopped waiting and deleted its mailbox, so it
			// does not say they are paired; nor, with nothing written, does this.
			return ErrDidNotFinish
		}
		// Any other failure may still have reached the mailbox. Keeping the
		// grant then leaves the other device telling a person to look here,
		// which is true, and never the reverse.
		return nil
	})
}

// joinRun is the exchange: it answers the grant and the introduction it came
// on, whose word that the grant is kept is still unsaid, and touches no file.
func joinRun(ctx context.Context, route Mailbox, j Joining, code JoinCode, ui JoinUI) (Grant, *introduction, error) {
	key, err := pairbox.NewKey()
	if err != nil {
		return Grant{}, nil, err
	}
	ctx, stop := context.WithTimeout(ctx, CodeValidFor+ConfirmWithin)
	defer stop()
	link := &boxLink{ctx: ctx, box: route.Box, plate: code.Plate, mine: pairbox.SideB, theirs: pairbox.SideA, key: key,
		gone:     map[stage]error{awaitAnswer: ErrStopped, awaitVerdict: ErrCodeDidNotWork},
		sendGone: NothingWaitingUnder(code.Plate)}
	offer, err := joinOffer(j.Label, j.Platform)
	if err != nil {
		return Grant{}, nil, err
	}
	intro, err := begin(link, chatScheme(code.Plate), code.Digits, offer)
	if err != nil {
		return Grant{}, nil, phraseJoin(err, route)
	}
	ui.Waiting(intro.Words)
	said, err := intro.verdict()
	if err != nil {
		return Grant{}, nil, phraseJoin(err, route)
	}
	grant, err := hearGrant(said)
	if errors.Is(err, ErrRefused) {
		// A no is read and kept like any answer, so the other device ends its
		// own no at once instead of waiting for a word that would never come.
		_ = intro.took()
	}
	return grant, intro, err
}

// phraseJoin gives the joining device's failures their sentences: the code that
// did not work is the same fact to a person whether the far end hung up or the
// key did not match.
func phraseJoin(err error, route Mailbox) error {
	if errors.Is(err, ErrWrongCode) {
		return ErrCodeDidNotWork
	}
	return phraseBox(err, route.URL)
}

// joinOffer is what this device sends in message 3: its name, then random bytes
// that make the message differ from any other session's.
func joinOffer(label string, platform ...string) ([]byte, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	name := readableLabel(label)
	if len(platform) != 0 && platform[0] != "" {
		name = ComputerLabel(name, platform[0])
	}
	return append([]byte(name), nonce...), nil
}

// adopt takes the grant into this home. Submitted chats require explicit
// replacement; a startup-only home retains its state through the durable switch.
func adopt(j Joining, grant Grant) (Joined, error) {
	return adoptWithDevice(j, grant, nil, nil)
}

// adoptWithDevice is adopt with the device to install, when the grant came with
// one, and agree: what to do the moment this computer has decided to keep the
// grant, which a pairing by code uses to tell the device that sent it.
//
// EVERY REFUSAL COMES BEFORE AGREE, AND AGREE COMES BEFORE ANY WRITE THE GRANT
// CAUSES. So the device that sent the grant hears it was kept exactly when it
// was. An error from agree stops the adoption with nothing of the grant
// written. Outcomes that write nothing (these chats are here already) still
// tell it, and do not wait on its answer.
func adoptWithDevice(j Joining, grant Grant, dev *identity.Dev, agree func() error) (Joined, error) {
	keep := func() error {
		if agree == nil {
			return nil
		}
		return agree()
	}
	recovered, err := j.resumeApproved()
	if err != nil {
		if recovered != nil {
			return *recovered, err
		}
		return Joined{}, err
	}
	if recovered != nil {
		held, err := identity.Load(j.Home)
		if err != nil {
			return *recovered, err
		}
		if held.ID() == grant.Identity.ID() {
			_ = keep()
			return *recovered, nil
		}
	}
	// No-op and refusal paths must not even create a lock file. Recheck the
	// destination under the lock below before making any actual switch.
	held, err := identity.Load(j.Home)
	if err != nil && !errors.Is(err, identity.ErrNone) {
		return Joined{}, err
	}
	if err == nil && !j.Replace && !j.Revoked && held.ID() == grant.Identity.ID() {
		_ = keep()
		return Joined{Already: true}, nil
	}
	if err == nil && !j.Replace && held.ID() != grant.Identity.ID() && !slices.Contains(grant.Replaces, held.ID()) {
		if ok, err := identity.Joinable(j.Home); err != nil || !ok {
			return Joined{}, differentChats(j.Home)
		}
	}
	if err != nil && !j.Replace {
		if refuses, err := identity.AdoptRefuses(j.Home, grant.Identity); err != nil || refuses {
			return Joined{}, legacyRefusal(err)
		}
	}
	unlock, err := keys.LockHome(j.Home)
	if err != nil {
		return Joined{}, err
	}
	defer unlock()
	if _, err := j.resume(); err != nil {
		return Joined{}, err
	}
	held, err = identity.Load(j.Home)
	switch {
	case err == nil && held.ID() == grant.Identity.ID():
		if !j.Replace {
			// A removed device needs a fresh key, while its identity and local
			// state stay in place. Only explicit replacement runs cleanup.
			if j.Revoked {
				if err := keep(); err != nil {
					return Joined{}, err
				}
				return j.merge(grant, dev, held.CellKey())
			}
			_ = keep()
			return Joined{Already: true}, nil
		}
		// Explicit replacement after approval renews this computer's key even
		// when the shared account is unchanged, because its old key may be
		// revoked: the replacing merge below mints a new device.
	case err == nil && slices.Contains(grant.Replaces, held.ID()):
		if err := keep(); err != nil {
			return Joined{}, err
		}
		return Joined{Successor: true}, follow(j, held, grant)
	}
	if err != nil && !errors.Is(err, identity.ErrNone) {
		return Joined{}, err
	}
	if j.Replace {
		if err := keep(); err != nil {
			return Joined{}, err
		}
		return j.merge(grant, dev, nil)
	}
	if err == nil {
		if merge, checkErr := identity.Joinable(j.Home); checkErr != nil || !merge {
			return Joined{}, differentChats(j.Home)
		}
		if err := keep(); err != nil {
			return Joined{}, err
		}
		return j.merge(grant, dev, held.CellKey())
	}
	// No identity here. A vault key from before identities is still chats of
	// its own, which Adopt would refuse only after making it an identity.
	if refuses, err := identity.AdoptRefuses(j.Home, grant.Identity); err != nil || refuses {
		return Joined{}, legacyRefusal(err)
	}
	if err := keep(); err != nil {
		return Joined{}, err
	}
	if _, err := identity.Adopt(j.Home, grant.Identity, false); err != nil {
		return Joined{}, adoptFailure(err)
	}
	if err := markSharing(j.Home, grant.Identity.ID()); err != nil {
		return Joined{}, err
	}
	if grant.SyncURL != "" && j.SaveSyncURL != nil {
		return Joined{}, j.SaveSyncURL(grant.SyncURL)
	}
	return Joined{}, nil
}

// legacyRefusal is what a home holding a vault key from before identities
// answers a grant it will not adopt without replacement: chats of its own, or
// the error that kept the question from being answered.
func legacyRefusal(err error) error {
	if err != nil {
		return err
	}
	return ErrDifferentChats
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

func differentChats(home string) error {
	n, err := identity.ChatCount(home)
	if err != nil {
		return ErrDifferentChats
	}
	if n == 0 {
		return ErrDifferentChats
	}
	count, kept := fmt.Sprintf("%d chats", n), fmt.Sprintf("those %d chats stay on this computer but no longer sync", n)
	if n == 1 {
		count, kept = "1 chat", "that chat stays on this computer but no longer syncs"
	}
	return differentChatError{line: "this computer already has " + count + " of its own, so nothing was changed. To pair it anyway, run this again with --replace: " + kept + "; shared memories and settings are cleared", chats: n}
}

// OwnChats is how many chats of its own a computer that refused to join counted,
// so a screen with no --replace to offer can word the refusal itself. It is 0
// when the refusal had no count: the work was not chats, or could not be read.
func OwnChats(err error) int {
	var e differentChatError
	if errors.As(err, &e) {
		return e.chats
	}
	return 0
}

type differentChatError struct {
	line  string
	chats int
}

func (e differentChatError) Error() string { return e.line }
func (e differentChatError) Unwrap() error { return ErrDifferentChats }
