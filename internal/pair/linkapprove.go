package pair

// A device that is already in, answering a new device's request. The same calls
// serve the terminal's `codeaf pair approve` and the approve screen of the app:
// Look shows who is asking, then Approve or Deny answers once.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// Approver is this computer as a device that may let another one in.
type Approver struct {
	Requests directory.Requests // the open side, to read a request
	Dir      directory.Client   // this device's signed directory
	Identity identity.Identity
	// SyncURL and Replaces go to the new device in its grant, as in a pairing by code.
	SyncURL  string
	Replaces []string
	Now      func() time.Time
}

// Asking is one device waiting to be let in, as the approving person sees it.
type Asking struct {
	Ref         LinkRef
	Name        string // "a new device" when the link carried no key to open it with
	Platform    string
	Check       string
	RequestedAt time.Time
	ExpiresAt   time.Time

	pubkey []byte
	box    [32]byte
}

// unnamed is what a device is called when its name cannot be opened.
const unnamed = "a new device"

// Look reads a pending request and checks it agrees with itself: the check
// number must be the one its key gives, so a swapped key shows.
func (a Approver) Look(ctx context.Context, ref LinkRef) (Asking, error) {
	r, err := a.Requests.GetRequest(ctx, ref.Code, 0)
	if err != nil {
		return Asking{}, phraseDecision(err)
	}
	if r.State != directory.RequestPending {
		return Asking{}, ErrLinkDecided
	}
	return askingOf(ref, r)
}

func askingOf(ref LinkRef, r directory.Request) (Asking, error) {
	pub, err := b64u.DecodeString(r.Pubkey)
	boxed, err2 := b64u.DecodeString(r.X25519)
	if err != nil || err2 != nil || len(boxed) != 32 || directory.CheckOf(pub) != r.Check ||
		(identity.Cert{Device: hex.EncodeToString(pub)}).DeviceID() != r.Device {
		return Asking{}, ErrCheckMismatch
	}
	as := Asking{
		Ref: ref, Name: nameFor(ref, r), Platform: r.Platform, Check: r.Check, pubkey: pub,
		RequestedAt: time.UnixMilli(r.RequestedAt), ExpiresAt: time.UnixMilli(r.ExpiresAt),
	}
	copy(as.box[:], boxed)
	return as, nil
}

func nameFor(ref LinkRef, r directory.Request) string {
	if name, err := OpenDeviceName(ref.Key, r.NameSealed); err == nil && name != "" {
		return name
	}
	return unnamed
}

// Approve lets the asking device in. The first answer wins; saying the same
// thing twice is fine.
func (a Approver) Approve(ctx context.Context, as Asking) error {
	body, err := a.approval(as)
	if err != nil {
		return err
	}
	return phraseDecision(a.Dir.ApproveRequest(ctx, as.Ref.Code, body))
}

// Deny turns the asking device down.
func (a Approver) Deny(ctx context.Context, as Asking) error {
	return phraseDecision(a.Dir.DenyRequest(ctx, as.Ref.Code))
}

func (a Approver) approval(as Asking) (directory.Approval, error) {
	sealed, err := directory.SealName(directory.MetadataKey(a.Identity.CellKey()), as.Name)
	if err != nil {
		return directory.Approval{}, err
	}
	cert := identity.IssueCert(a.Identity, as.pubkey, a.now())
	rawCert, err := json.Marshal(cert)
	if err != nil {
		return directory.Approval{}, err
	}
	grant, err := LinkGrant{Grant{Identity: a.Identity, SyncURL: a.SyncURL, Replaces: a.Replaces}, cert}.seal(&as.box)
	return directory.Approval{
		Device: directory.Device{V: 1, Name: sealed, AddedBy: a.Identity.ID(), Platform: as.Platform,
			Caps: directory.Caps{OS: as.Platform, Cow: "none"}},
		Cert: b64u.EncodeToString(rawCert), Grant: grant,
	}, err
}

func (a Approver) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// phraseDecision turns what the directory said about a request into its sentence.
func phraseDecision(err error) error {
	switch {
	case errors.Is(err, directory.ErrRequestGone):
		return ErrLinkGone
	case errors.Is(err, directory.ErrAlreadyDecided):
		return ErrLinkDecided
	}
	return err
}
