package pair

// A device that is already in, answering a new device's request. The same calls
// serve the terminal's `codeaf pair approve` and the approve screen of the app:
// Look shows who is asking, then Approve or Deny answers once.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/fabricclient"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// Fabric is the signed fabric client Approver uses when enrolling and registering a phone.
type Fabric interface {
	ListEnrollments(ctx context.Context) ([]fabricclient.EnrollmentRow, error)
	PutEnrollment(ctx context.Context, e clientproto.Enrollment) error
	RegisterClient(ctx context.Context, cert identity.Cert, methods []string, sealedName string) error
}

// Approver is this computer as a device that may let another one in.
type Approver struct {
	Requests directory.Requests // the open side, to read a request
	Dir      directory.Client   // this device's signed directory
	Fabric   Fabric             // this device's signed client-fabric routes for phone enrollment
	Identity identity.Identity
	// SyncURL and Replaces go to the new device in its grant, as in a pairing by code.
	SyncURL  string
	Replaces []string
	Now      func() time.Time
	// Card supplies this computer's signed executor card for a phone grant (design 3.3, 3.8a).
	Card func(context.Context) (clientproto.Card, error)
	// BeforeApprove publishes the approving computer before the grant becomes
	// visible, so Paired. cannot precede its device record on the joiner's list.
	BeforeApprove func(context.Context) error
	// AfterApprove refreshes local account information only after the grant succeeds.
	AfterApprove func(context.Context)
}

// Asking is one device waiting to be let in, as the approving person sees it.
type Asking struct {
	Ref         LinkRef
	Name        string // "a new device" when the link carried no key to open it with
	Platform    string
	Check       string
	Client      bool
	RequestedAt time.Time
	ExpiresAt   time.Time

	pubkey []byte
	box    [32]byte
}

// unnamed is what a device is called when its name cannot be opened.
const unnamed = "a new device"

// Look reads a pending request and checks it agrees with itself: a computer
// request must match its key's check digits, and a phone request must match the
// locally recomputed key-binding MAC under the token's link key.
func (a Approver) Look(ctx context.Context, ref LinkRef) (Asking, error) {
	code := directory.NormalizeCode(ref.Code)
	if len(code) == clientproto.ClientLinkCodeLen && len(ref.Key) == 0 {
		return Asking{}, ErrLinkKeyless
	}
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
	code := directory.NormalizeCode(ref.Code)
	if r.Code != "" && directory.NormalizeCode(r.Code) != code {
		if r.Class != "" || len(code) == clientproto.ClientLinkCodeLen {
			return Asking{}, ErrBindMismatch
		}
		return Asking{}, ErrCheckMismatch
	}
	isClientCode := len(code) == clientproto.ClientLinkCodeLen && clientCodeShape.MatchString(code)
	isComputerCode := codeShape.MatchString(code)
	if r.Class != "" || isClientCode {
		return clientAskingOf(ref, r, isClientCode)
	}
	if !isComputerCode || r.Bind != "" {
		return Asking{}, ErrBindMismatch
	}
	pub, err := b64u.DecodeString(r.Pubkey)
	boxed, err2 := b64u.DecodeString(r.X25519)
	if err != nil || len(pub) != ed25519.PublicKeySize || err2 != nil || len(boxed) != 32 ||
		directory.CheckOf(pub) != r.Check ||
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

// clientAskingOf validates a client-class request against the token's link key.
// The sync service's r.Check is never used or compared; both bind and the four
// digits are computed locally from the link key and the request's two public keys.
func clientAskingOf(ref LinkRef, r directory.Request, isClientCode bool) (Asking, error) {
	if r.Class != clientproto.ClientLinkClass || !isClientCode {
		return Asking{}, ErrBindMismatch
	}
	if len(ref.Key) == 0 {
		return Asking{}, ErrLinkKeyless
	}
	if len(ref.Key) != clientproto.ClientLinkKeyBytes {
		return Asking{}, ErrBindMismatch
	}
	pub, err1 := b64u.DecodeString(r.Pubkey)
	boxed, err2 := b64u.DecodeString(r.X25519)
	gotBind, err3 := b64u.DecodeString(r.Bind)
	if err1 != nil || len(pub) != ed25519.PublicKeySize ||
		err2 != nil || len(boxed) != 32 ||
		err3 != nil || len(gotBind) != clientproto.ClientLinkBindLen || b64u.EncodeToString(gotBind) != r.Bind ||
		r.Device != clientproto.DeviceIDOf(pub) {
		return Asking{}, ErrBindMismatch
	}
	mac := clientproto.BindMAC(ref.Key, pub, boxed)
	wantBind := clientproto.Bind(mac)
	if subtle.ConstantTimeCompare(gotBind, wantBind) != 1 {
		return Asking{}, ErrBindMismatch
	}
	as := Asking{
		Ref:         ref,
		Name:        nameFor(ref, r),
		Platform:    r.Platform,
		Check:       clientproto.CheckDigits(mac),
		Client:      true,
		RequestedAt: time.UnixMilli(r.RequestedAt),
		ExpiresAt:   time.UnixMilli(r.ExpiresAt),
		pubkey:      pub,
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

// Approve lets the asking device in. A phone request runs ApproveClient; a
// computer request runs the full-identity grant flow.
func (a Approver) Approve(ctx context.Context, as Asking) error {
	if as.Client {
		return a.ApproveClient(ctx, as)
	}
	body, err := a.approval(as)
	if err != nil {
		return err
	}
	if a.BeforeApprove != nil {
		if err := a.BeforeApprove(ctx); err != nil {
			return err
		}
	}
	if err := a.Dir.ApproveRequest(ctx, as.Ref.Code, body); err != nil {
		return phraseDecision(err)
	}
	if a.AfterApprove != nil {
		a.AfterApprove(ctx)
	}
	return nil
}

// ApproveClientChecked compares typedCheck against the locally computed check
// digits from the link key and the phone's keys, refusing before step 1 on any
// mismatch so nothing is enrolled, registered or granted.
func (a Approver) ApproveClientChecked(ctx context.Context, as Asking, typedCheck string) error {
	if err := validateClientAsking(as); err != nil {
		return err
	}
	mac := clientproto.BindMAC(as.Ref.Key, as.pubkey, as.box[:])
	localCheck := clientproto.CheckDigits(mac)
	typed := strings.TrimSpace(typedCheck)
	if subtle.ConstantTimeCompare([]byte(typed), []byte(localCheck)) != 1 ||
		(as.Check != "" && subtle.ConstantTimeCompare([]byte(as.Check), []byte(localCheck)) != 1) {
		return ErrCheckNumberMismatch
	}
	return a.ApproveClient(ctx, as)
}

// ApproveClient admits a phone in three idempotent steps: store the identity-signed
// enrollment, register the client row in the directory, and decide the link request
// with a ClientGrant sealed under the link key.
func (a Approver) ApproveClient(ctx context.Context, as Asking) error {
	if err := validateClientAsking(as); err != nil {
		return err
	}
	if a.Card == nil {
		return ErrNoExecutorCard
	}
	if a.Fabric == nil || a.Dir == nil {
		return ErrNoFabricClient
	}
	card, err := a.Card(ctx)
	if err != nil {
		return phraseDecision(err)
	}
	if err := card.Verify(a.Identity.PublicKey()); err != nil {
		return ErrNoExecutorCard
	}
	relayAddr := a.clientRelayAddress()
	if relayAddr == "" {
		return ErrNoSync
	}
	if a.BeforeApprove != nil {
		if err := a.BeforeApprove(ctx); err != nil {
			return phraseDecision(err)
		}
	}

	// Step 1: store the identity-signed enrollment at the next version for this
	// device (or the current version when retrying the identical enrollment).
	device := clientproto.DeviceIDOf(as.pubkey)
	ver, err := a.nextEnrollmentVer(ctx, device, as.pubkey, as.box[:])
	if err != nil {
		return phraseDecision(err)
	}
	enroll := clientproto.Enrollment{
		V:       1,
		Device:  device,
		SignPub: append([]byte(nil), as.pubkey...),
		BoxPub:  append([]byte(nil), as.box[:]...),
		Methods: clientproto.PhoneMethods(),
		Reads:   clientproto.PhoneReads(),
		Ver:     ver,
	}
	enroll.Sig = a.Identity.Sign(enroll.SignText())
	if err := a.Fabric.PutEnrollment(ctx, enroll); err != nil {
		return phraseDecision(err)
	}

	// Step 2: register the client device record. A 409 exists on retry is
	// idempotent when the existing row is this same non-revoked client.
	cert := identity.IssueCert(a.Identity, as.pubkey, a.now())
	sealedName, err := directory.SealName(directory.MetadataKey(a.Identity.CellKey()), as.Name)
	if err != nil {
		return err
	}
	if err := a.Fabric.RegisterClient(ctx, cert, clientproto.PhoneMethods(), sealedName); err != nil {
		if !errors.Is(err, fabricclient.ErrExists) || !a.isExistingClient(ctx, device) {
			return phraseDecision(err)
		}
	}

	// Step 3: decide the link request with a ClientGrant sealed under the link key.
	code := directory.NormalizeCode(as.Ref.Code)
	grant := clientproto.ClientGrant{
		V:           clientproto.GrantVersion,
		Device:      device,
		Code:        code,
		Cert:        cert,
		IdentityPub: append([]byte(nil), a.Identity.PublicKey()...),
		Relay:       relayAddr,
		Executors:   []clientproto.Card{card},
	}
	nonce, err := clientproto.GrantNonce(rand.Reader)
	if err != nil {
		return err
	}
	sealedGrant, err := clientproto.SealGrant(grant, nonce, as.Ref.Key)
	if err != nil {
		return err
	}
	approval := directory.Approval{
		Class: clientproto.ClientLinkClass,
		Grant: b64u.EncodeToString(sealedGrant),
	}
	if err := a.Dir.ApproveRequest(ctx, code, approval); err != nil {
		if !errors.Is(err, directory.ErrAlreadyDecided) || !a.alreadyApprovedClient(ctx, code, device, as.pubkey, as.Ref.Key) {
			return phraseDecision(err)
		}
	}
	if a.AfterApprove != nil {
		a.AfterApprove(ctx)
	}
	return nil
}

func validateClientAsking(as Asking) error {
	code := directory.NormalizeCode(as.Ref.Code)
	if !as.Client || !clientCodeShape.MatchString(code) {
		return ErrBindMismatch
	}
	if len(as.Ref.Key) == 0 {
		return ErrLinkKeyless
	}
	if len(as.Ref.Key) != clientproto.ClientLinkKeyBytes || len(as.pubkey) != ed25519.PublicKeySize {
		return ErrBindMismatch
	}
	mac := clientproto.BindMAC(as.Ref.Key, as.pubkey, as.box[:])
	localCheck := clientproto.CheckDigits(mac)
	if as.Check != "" && subtle.ConstantTimeCompare([]byte(as.Check), []byte(localCheck)) != 1 {
		return ErrBindMismatch
	}
	return nil
}

func (a Approver) clientRelayAddress() string {
	if addr := strings.TrimRight(strings.TrimSpace(a.SyncURL), "/"); addr != "" {
		return addr
	}
	if b, ok := a.Fabric.(interface{ Base() string }); ok {
		return strings.TrimRight(strings.TrimSpace(b.Base()), "/")
	}
	return ""
}

func (a Approver) nextEnrollmentVer(ctx context.Context, device string, signPub, boxPub []byte) (uint64, error) {
	rows, err := a.Fabric.ListEnrollments(ctx)
	if err != nil {
		return 0, err
	}
	var highest uint64
	var stored *clientproto.Enrollment
	for i := range rows {
		row := rows[i]
		if row.Device == device || row.Enroll.Device == device {
			if row.Enroll.Ver >= highest {
				highest = row.Enroll.Ver
				e := row.Enroll
				stored = &e
			}
		}
	}
	if stored == nil || highest == 0 {
		return 1, nil
	}
	candidate := clientproto.Enrollment{
		V:       1,
		Device:  device,
		SignPub: append([]byte(nil), signPub...),
		BoxPub:  append([]byte(nil), boxPub...),
		Methods: clientproto.PhoneMethods(),
		Reads:   clientproto.PhoneReads(),
		Ver:     highest,
	}
	candidate.Sig = a.Identity.Sign(candidate.SignText())
	if candidate.Same(*stored) {
		return highest, nil
	}
	return highest + 1, nil
}

func (a Approver) isExistingClient(ctx context.Context, device string) bool {
	if a.Dir == nil {
		return false
	}
	listing, err := a.Dir.List(ctx)
	if err != nil {
		return false
	}
	// A 409 exists with no row to show for it proves nothing, so an unlisted device is
	// NOT this client: only a listed, live client row makes a retry safe to continue.
	d, ok := listing.Devices[device]
	return ok && d.Kind == clientproto.ClientLinkClass && !d.Revoked
}

func (a Approver) alreadyApprovedClient(ctx context.Context, code, device string, signPub ed25519.PublicKey, linkKey []byte) bool {
	if a.Requests == nil {
		return false
	}
	r, err := a.Requests.GetRequest(ctx, code, 0)
	if err != nil || r.State != directory.RequestApproved || r.Grant == nil {
		return false
	}
	raw, err := b64u.DecodeString(*r.Grant)
	if err != nil {
		return false
	}
	g, err := clientproto.OpenGrant(raw, linkKey)
	if err != nil || g.Check(device, code, signPub) != nil {
		return false
	}
	return bytes.Equal(g.IdentityPub, a.Identity.PublicKey())
}

// Deny turns the asking device down.
func (a Approver) Deny(ctx context.Context, as Asking) error {
	return phraseDecision(a.Dir.DenyRequest(ctx, as.Ref.Code))
}

func (a Approver) approval(as Asking) (directory.Approval, error) {
	if as.Client || !codeShape.MatchString(directory.NormalizeCode(as.Ref.Code)) {
		return directory.Approval{}, ErrBindMismatch
	}
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
	case errors.Is(err, directory.ErrAlreadyDecided), errors.Is(err, fabricclient.ErrAlreadyDecided):
		return ErrLinkDecided
	case errors.Is(err, directory.ErrNotEnrolled):
		return fabricclient.ErrNotEnrolled
	case errors.Is(err, directory.ErrBadRequest):
		return fabricclient.ErrBadRequest
	case errors.Is(err, directory.ErrTooBig):
		return fabricclient.ErrTooLarge
	case errors.Is(err, directory.ErrFull):
		return ErrRelayBusy
	case errors.Is(err, directory.ErrTooOld):
		return ErrRelayTooOld
	}
	return err
}
