package pair

// A new device joining by link: it asks, shows a link, and waits for a device
// that is already in to say yes. No code is typed on the new device and nothing
// secret crosses the relay: the answer is sealed to a key made here.

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// LinkUI is what a person sees on the new device.
type LinkUI interface {
	// Invited shows the link, the typed form and the check number, once.
	Invited(Invite)
}

// Fleet is what a device sees of the identity it just joined.
type Fleet struct{ Devices, Workspaces int }

// LinkJoining is this computer as the device that asks to join.
type LinkJoining struct {
	Joining
	// Platform is runtime.GOOS; the directory shows it as an icon.
	Platform string
	// Host names the address that did not answer, for the sentence about it.
	Host string
	// Via is the sync address to tell the approving device when it is not the default.
	Via string
	// Pull reads the identity's devices and workspaces as the new device.
	Pull func(ctx context.Context) (Fleet, error)
}

// LinkJoined says how a join by link ended.
type LinkJoined struct {
	Joined
	Fleet Fleet
}

// retryPause is how long a poll waits after the relay was unreachable.
const retryPause = 2 * time.Second

// JoinByLink asks to join, waits to be approved, and installs what it is sent.
// It ends with ErrLinkExpired when nobody answers within the request's life.
func JoinByLink(ctx context.Context, reqs directory.Requests, j LinkJoining, ui LinkUI) (LinkJoined, error) {
	if err := j.free(); err != nil {
		return LinkJoined{}, err
	}
	me, err := newAsker()
	if err != nil {
		return LinkJoined{}, err
	}
	opened, ref, err := me.ask(ctx, reqs, j)
	if err != nil {
		return LinkJoined{}, phraseLink(err, j.Host)
	}
	ui.Invited(Invite{Ref: ref, Check: opened.Check, ExpiresIn: time.Duration(opened.ExpiresAt-opened.RequestedAt) * time.Millisecond, Via: j.Via})
	grant, err := me.hear(ctx, reqs, opened)
	if err != nil {
		return LinkJoined{}, phraseLink(err, j.Host)
	}
	return me.install(ctx, j, grant)
}

// free refuses a join that would overwrite chats, before any approval is spent.
func (j LinkJoining) free() error {
	if _, err := identity.Load(j.Home); err == nil && !j.Replace {
		return ErrDifferentChats
	}
	return nil
}

// asker is the new device's keys while it waits: its own device secret, the box
// the answer is sealed to, and the secret in the link.
type asker struct {
	seed    identity.Seed
	boxPub  *[32]byte
	boxPriv *[32]byte
	linkKey []byte
}

func newAsker() (*asker, error) {
	seed, err := identity.NewSeed()
	if err != nil {
		return nil, err
	}
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	key, err := NewLinkKey()
	return &asker{seed: seed, boxPub: pub, boxPriv: priv, linkKey: key}, err
}

func (a *asker) ask(ctx context.Context, reqs directory.Requests, j LinkJoining) (directory.Opened, LinkRef, error) {
	sealed, err := SealDeviceName(a.linkKey, j.Label)
	if err != nil {
		return directory.Opened{}, LinkRef{}, err
	}
	opened, err := reqs.CreateRequest(ctx, directory.NewRequest{
		Pubkey: b64u.EncodeToString(a.seed.Public()), X25519: b64u.EncodeToString(a.boxPub[:]),
		NameSealed: sealed, Platform: j.Platform,
	})
	return opened, LinkRef{Code: opened.Code, Key: a.linkKey}, err
}

// hear waits for the decision and opens the grant it carries.
func (a *asker) hear(ctx context.Context, reqs directory.Requests, o directory.Opened) (LinkGrant, error) {
	ctx, stop := context.WithTimeout(ctx, directory.RequestTTL+directory.DecidedKeep)
	defer stop()
	r, err := awaitDecision(ctx, reqs, o.Code)
	switch {
	case err != nil:
		return LinkGrant{}, err
	case r.State != directory.RequestApproved || r.Grant == nil:
		return LinkGrant{}, ErrLinkDeclined
	}
	return openLinkGrant(*r.Grant, a.boxPub, a.boxPriv)
}

// awaitDecision holds one long poll after another until the request is decided.
func awaitDecision(ctx context.Context, reqs directory.Requests, code string) (directory.Request, error) {
	for {
		r, err := reqs.GetRequest(ctx, code, directory.MaxWait)
		if err == nil {
			return r, nil
		}
		if pause, again := pollAgain(err); again {
			if !sleepFor(ctx, pause) {
				return directory.Request{}, ErrLinkExpired
			}
			continue
		}
		return directory.Request{}, err
	}
}

// pollAgain says whether a failed poll is worth another try, and after how long.
func pollAgain(err error) (time.Duration, bool) {
	switch {
	case errors.Is(err, directory.ErrStillPending):
		return 0, true
	case errors.Is(err, directory.ErrRateLimited):
		return max(wireauth.After(err), retryPause), true
	case errors.Is(err, directory.ErrUnreachable):
		return retryPause, true
	}
	return 0, false
}

func sleepFor(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// install makes the identity and the certified device this computer's own, then
// reads what the identity holds.
func (a *asker) install(ctx context.Context, j LinkJoining, g LinkGrant) (LinkJoined, error) {
	dev, err := a.seed.Join(g.Cert)
	if err != nil {
		return LinkJoined{}, ErrBadGrant
	}
	joined, err := adopt(j.Joining, g.Grant)
	if err != nil {
		return LinkJoined{}, err
	}
	if err := identity.Install(j.Home, g.Identity, dev); err != nil {
		return LinkJoined{}, err
	}
	fleet, err := j.Pull(ctx)
	if err != nil {
		return LinkJoined{Joined: joined}, ErrPairedNotRead
	}
	return LinkJoined{Joined: joined, Fleet: fleet}, nil
}

// phraseLink turns what the directory said into the one sentence for it.
func phraseLink(err error, host string) error {
	switch {
	case errors.Is(err, directory.ErrRequestGone):
		return ErrLinkExpired
	case errors.Is(err, directory.ErrRateLimited):
		return TooManyFor(wireauth.After(err))
	case errors.Is(err, directory.ErrFull):
		return ErrRelayBusy
	case errors.Is(err, directory.ErrTooOld):
		return ErrRelayTooOld
	case errors.Is(err, directory.ErrUnreachable):
		return CannotReachHost(host)
	}
	return err
}
