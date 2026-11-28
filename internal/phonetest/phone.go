// Package phonetest is a test-only stand-in for the codeaf personal phone app.
// It holds only a phone's own keys and grant, speaks the client wire over HTTP
// signed with codeaf-req-v1, and checks every relay and computer reply against
// the design contract so end-to-end tests can pair, read cards and snapshot
// slots, queue calls, and read outcomes without a TypeScript runtime.
package phonetest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

const (
	// DefaultName is the display name sealed into a link request when Config.Name is empty.
	DefaultName = "phone"
	// DefaultPlatform is the platform string sent in link requests when Config.Platform is empty.
	DefaultPlatform = "test"
	// ExpiryAllowance is how long past a call's not_after the phone waits before deriving
	// an expired outcome from its own clock when no computer outcome has arrived.
	ExpiryAllowance = clientproto.WatchingDampen
	// maxLinkNameBytes bounds a device name before it is sealed under the link key.
	maxLinkNameBytes = clientproto.MaxClientMethods + clientproto.MaxOutcomes
	// decimalBase formats integers in base ten on the wire.
	decimalBase = clientproto.MaxComputerReads
)

var (
	b64u            = base64.RawURLEncoding
	clientCodeShape = regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{` + strconv.Itoa(clientproto.ClientLinkCodeLen) + `}$`)
)

// Sentinel errors returned when the phone refuses a relay or computer answer.
var (
	ErrNotPaired          = errors.New("phonetest: phone is not paired")
	ErrSetupLink          = pair.ErrSetupLinkShape
	ErrLinkMismatch       = errors.New("phonetest: link request does not match phone keys or class")
	ErrLinkDeclined       = pair.ErrLinkDeclined
	ErrLinkExpired        = pair.ErrLinkExpired
	ErrGrant              = clientproto.ErrGrant
	ErrGrantRelayMismatch = fmt.Errorf("%w: grant relay does not match setup link", clientproto.ErrGrant)
	ErrCard               = clientproto.ErrCard
	ErrCardWriterMismatch = fmt.Errorf("%w: card writer does not match requested computer", clientproto.ErrCard)
	ErrCardReplay         = fmt.Errorf("%w: card timestamp is not newer than the last accepted card", clientproto.ErrCard)
	ErrSlot               = clientproto.ErrSlot
	ErrSlotWriterMismatch = fmt.Errorf("%w: slot writer does not match requested computer", clientproto.ErrSlot)
	ErrSlotReplay         = fmt.Errorf("%w: slot timestamp is not newer than the last accepted slot", clientproto.ErrSlot)
	ErrOutcome            = clientproto.ErrOutcome
	ErrRevoked            = wireauth.ErrRevoked
	ErrUnauthorized       = wireauth.ErrUnauthorized
	ErrNotFound           = directory.ErrNotFound
	ErrMethodDenied       = errors.New("phonetest: method denied")
	ErrSequence           = errors.New("phonetest: queue sequence mismatch")
	ErrFull               = wireauth.ErrRelayFull
)

// Config configures a stand-in Phone for tests.
type Config struct {
	// Name is the phone's display name sealed under the link key during pairing.
	Name string
	// Platform is the platform label sent in the link request; empty defaults to "test".
	Platform string
	// Rand is the entropy source for keys, nonces, and client_id values; nil uses crypto/rand.Reader.
	Rand io.Reader
	// HTTP is the HTTP client used for relay requests; nil uses http.DefaultClient.
	HTTP *http.Client
	// Now is the phone's clock; nil uses time.Now.
	Now func() time.Time
}

// Link holds the state of an open client-class pairing request started by a phone.
type Link struct {
	// Token is `<code>.<base64url key>`, what the phone shows and the owner pastes.
	Token string
	// Code is the client-class request code assigned by the relay.
	Code string
	// CheckDigits is the four-digit check number computed locally from the bind MAC, never read from the relay.
	CheckDigits string
	// Key is the raw 16-byte link key that never travels to the relay.
	Key []byte
	// SyncAddress is the normalized relay base URL the request was started against.
	SyncAddress string
	// FromSetup reports whether the link was started from a workaf://setup?v=1&sync=... link (design 3.8b).
	FromSetup bool
}

// Computer is one entry returned by GET /v1/clients/{device}/computers.
// THE PHONE NEVER TRUSTS THIS LIST FOR KEYS OR NAMES: only a verified Card
// under the identity public key can supply a computer's executor key or name.
type Computer struct {
	Device  string `json:"device"`
	Online  bool   `json:"online"`
	Serving bool   `json:"serving"`
	Name    string `json:"name,omitempty"`
	At      int64  `json:"at,omitempty"`
}

// CreateParams is the exact parameter object for agents.create from a phone (design 3.5).
type CreateParams struct {
	ClientID string `json:"client_id"`
}

// SendParams is the exact parameter object for agents.send from a phone (design 3.5).
type SendParams struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	ClientID string `json:"client_id"`
}

// AnswerParams is the exact parameter object for asks.answer from a phone (design 3.5).
type AnswerParams struct {
	Agent  string `json:"agent"`
	ID     string `json:"id"`
	Option string `json:"option"`
	Text   string `json:"text"`
}

// StopParams is the exact parameter object for agents.stop from a phone (design 3.5).
type StopParams struct {
	ID string `json:"id"`
}

// CallOptions configures optional overrides when queueing a call from the phone.
type CallOptions struct {
	// ClientID sets the idempotency key for agents.create or agents.send; empty draws 16 random bytes in hex.
	ClientID string
	// Lifetime overrides the call's not_after - at duration (for example, to test expiry).
	Lifetime time.Duration
	// FaceID sets the biometric confirmation flag on the inner call plaintext.
	FaceID bool
	// Number overrides the queue entry number when positive; zero uses the phone's monotonic counter.
	Number int64
}

// CallRef identifies a call the phone appended to its relay queue.
type CallRef struct {
	Number   int64  `json:"number"`
	ClientID string `json:"client_id,omitempty"`
	At       int64  `json:"at"`
	NotAfter int64  `json:"not_after"`
	Target   string `json:"target,omitempty"`
	Method   string `json:"method,omitempty"`
}

// Phone is a stand-in personal phone client for tests.
// A PHONE NEVER HOLDS THE IDENTITY PRIVATE KEY, AN IDENTITY SEED, A DEDUP SECRET,
// OR A CELL KEY (contract P3, L2). It holds only its own Ed25519 signing key, its
// own X25519 box key, and, after pairing, its verified ClientGrant and executor cards.
type Phone struct {
	mu       sync.Mutex
	name     string
	platform string
	rand     io.Reader
	http     *http.Client
	now      func() time.Time

	signPub  ed25519.PublicKey
	signPriv ed25519.PrivateKey
	boxPub   [ed25519.PublicKeySize]byte
	boxPriv  [ed25519.PublicKeySize]byte
	device   string

	paired             bool
	grant              clientproto.ClientGrant
	relay              string
	cards              map[string]clientproto.Card
	cardSlotAt         map[string]int64
	slotAt             map[string]int64
	lastOutcomesSealed map[string][]byte
	nextNumber         int64
}

// New creates a new unpaired Phone with its own Ed25519 signing key and X25519 box key.
func New(cfg Config) (*Phone, error) {
	r := cfg.Rand
	if r == nil {
		r = rand.Reader
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = DefaultName
	}
	platform := strings.TrimSpace(cfg.Platform)
	if platform == "" {
		platform = DefaultPlatform
	}
	signPub, signPriv, err := ed25519.GenerateKey(r)
	if err != nil {
		return nil, err
	}
	boxPub, boxPriv, err := box.GenerateKey(r)
	if err != nil {
		return nil, err
	}
	p := &Phone{
		name:               name,
		platform:           platform,
		rand:               r,
		http:               hc,
		now:                now,
		signPub:            signPub,
		signPriv:           signPriv,
		boxPub:             *boxPub,
		boxPriv:            *boxPriv,
		device:             clientproto.DeviceIDOf(signPub),
		cards:              map[string]clientproto.Card{},
		cardSlotAt:         map[string]int64{},
		slotAt:             map[string]int64{},
		lastOutcomesSealed: map[string][]byte{},
		nextNumber:         1,
	}
	return p, nil
}

// DeviceID returns the phone's `dev_<32hex>` identifier derived from its signing key.
func (p *Phone) DeviceID() string { return p.device }

// ID is an alias for DeviceID.
func (p *Phone) ID() string { return p.device }

// Name returns the phone's display name.
func (p *Phone) Name() string { return p.name }

// SignPublicKey returns a copy of the phone's Ed25519 public key.
func (p *Phone) SignPublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), p.signPub...)
}

// BoxPublicKey returns the phone's X25519 public key.
func (p *Phone) BoxPublicKey() [ed25519.PublicKeySize]byte { return p.boxPub }

// Paired reports whether the phone has accepted a valid ClientGrant.
func (p *Phone) Paired() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paired
}

// Grant returns the phone's accepted ClientGrant when paired.
func (p *Phone) Grant() (clientproto.ClientGrant, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.paired {
		return clientproto.ClientGrant{}, false
	}
	return p.grant, true
}

// KnownCard returns the highest-timestamp verified Card the phone currently holds for computer.
func (p *Phone) KnownCard(computer string) (clientproto.Card, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.cards[computer]
	return c, ok
}

// ParseSetupLink parses a `workaf://setup?v=1&sync=<percent-encoded address>` setup link
// according to design 3.8b and returns the normalized sync address.
func ParseSetupLink(raw string) (string, error) {
	return pair.ParseSetupLink(raw)
}

// StartLink draws a 16-byte (22-char base64url) link key and creates a client-class link
// request (`POST /v1/link/requests`) against syncAddress. If syncAddress is a
// `workaf://setup?v=1&sync=...` setup link, StartLink parses it with design 3.8b's rules
// and marks the returned Link as coming from a setup link.
func (p *Phone) StartLink(ctx context.Context, syncAddress string) (Link, error) {
	trimmed := strings.TrimSpace(syncAddress)
	fromSetup := false
	if strings.HasPrefix(strings.ToLower(trimmed), "workaf://") || strings.Contains(trimmed, "://setup?") {
		parsed, err := pair.ParseSetupLink(trimmed)
		if err != nil {
			return Link{}, err
		}
		trimmed = parsed
		fromSetup = true
	}
	base, err := normalizeSyncAddress(trimmed)
	if err != nil {
		return Link{}, err
	}

	key := make([]byte, clientproto.ClientLinkKeyBytes)
	if _, err := io.ReadFull(p.rand, key); err != nil {
		return Link{}, err
	}
	mac := clientproto.BindMAC(key, p.signPub, p.boxPub[:])
	bind := b64u.EncodeToString(clientproto.Bind(mac))
	check := clientproto.CheckDigits(mac)

	sealedName, err := sealDeviceName(p.rand, key, p.name)
	if err != nil {
		return Link{}, err
	}
	reqBody := directory.NewRequest{
		Pubkey:     b64u.EncodeToString(p.signPub),
		X25519:     b64u.EncodeToString(p.boxPub[:]),
		NameSealed: sealedName,
		Platform:   p.platform,
		Class:      clientproto.ClientLinkClass,
		Bind:       bind,
	}
	var opened directory.Opened
	if err := p.doOpenJSON(ctx, base, http.MethodPost, directory.LinkPath+"/requests", reqBody, http.StatusCreated, &opened); err != nil {
		return Link{}, err
	}
	code := directory.NormalizeCode(opened.Code)
	if !clientCodeShape.MatchString(code) || opened.Device != p.device ||
		(opened.Class != "" && opened.Class != clientproto.ClientLinkClass) ||
		opened.ExpiresAt <= opened.RequestedAt {
		return Link{}, ErrLinkMismatch
	}
	return Link{
		Token:       code + "." + b64u.EncodeToString(key),
		Code:        code,
		CheckDigits: check,
		Key:         key,
		SyncAddress: base,
		FromSetup:   fromSetup,
	}, nil
}

// StartLinkFromSetup parses a `workaf://setup?v=1&sync=...` link and starts a client-class
// link request at its sync address, requiring the resulting grant to match that relay (design 3.8b).
func (p *Phone) StartLinkFromSetup(ctx context.Context, setupLink string) (Link, error) {
	syncAddr, err := pair.ParseSetupLink(setupLink)
	if err != nil {
		return Link{}, err
	}
	link, err := p.StartLink(ctx, syncAddr)
	if err != nil {
		return Link{}, err
	}
	link.FromSetup = true
	return link, nil
}

// AwaitGrant polls `GET /v1/link/requests/{code}?wait=<ms>` until the request is approved,
// denied, or expired, opens the grant under the link key with clientproto.OpenGrant, and
// checks it with grant.Check and design 3.8b's relay rule. Any refusal leaves the phone
// with no paired state (contract P12).
func (p *Phone) AwaitGrant(ctx context.Context, link Link) (clientproto.ClientGrant, error) {
	code := directory.NormalizeCode(link.Code)
	if !clientCodeShape.MatchString(code) || len(link.Key) != clientproto.ClientLinkKeyBytes {
		return clientproto.ClientGrant{}, clientproto.ErrGrant
	}
	base, err := normalizeSyncAddress(link.SyncAddress)
	if err != nil {
		return clientproto.ClientGrant{}, err
	}
	wantMAC := clientproto.BindMAC(link.Key, p.signPub, p.boxPub[:])
	wantBind := clientproto.Bind(wantMAC)
	wantPub := b64u.EncodeToString(p.signPub)
	wantBox := b64u.EncodeToString(p.boxPub[:])

	waitMs := directory.MaxWait.Milliseconds()
	if deadline, ok := ctx.Deadline(); ok {
		if rem := time.Until(deadline).Milliseconds(); rem > 0 && rem < waitMs {
			waitMs = max(rem, 1)
		}
	}
	path := directory.LinkPath + "/requests/" + url.PathEscape(code) + "?wait=" + strconv.FormatInt(waitMs, decimalBase)

	for {
		if err := ctx.Err(); err != nil {
			return clientproto.ClientGrant{}, err
		}
		var rec directory.Request
		status, err := p.getLinkRequest(ctx, base, path, &rec)
		if err != nil {
			return clientproto.ClientGrant{}, err
		}
		if status == http.StatusNoContent {
			runtime.Gosched()
			continue
		}
		gotBind, bindErr := b64u.DecodeString(rec.Bind)
		if directory.NormalizeCode(rec.Code) != code || rec.Device != p.device ||
			rec.Pubkey != wantPub || rec.X25519 != wantBox ||
			rec.Class != clientproto.ClientLinkClass || bindErr != nil ||
			len(gotBind) != clientproto.ClientLinkBindLen || b64u.EncodeToString(gotBind) != rec.Bind ||
			subtle.ConstantTimeCompare(gotBind, wantBind) != 1 {
			return clientproto.ClientGrant{}, ErrLinkMismatch
		}
		switch rec.State {
		case directory.RequestPending:
			if rec.ExpiresAt > 0 && p.now().UnixMilli() >= rec.ExpiresAt {
				return clientproto.ClientGrant{}, ErrLinkExpired
			}
			runtime.Gosched()
			continue
		case directory.RequestDenied:
			return clientproto.ClientGrant{}, ErrLinkDeclined
		case directory.RequestApproved:
			if rec.Grant == nil || *rec.Grant == "" {
				return clientproto.ClientGrant{}, clientproto.ErrGrant
			}
			return p.openAndAdoptGrant(link, base, code, *rec.Grant)
		default:
			return clientproto.ClientGrant{}, clientproto.ErrGrant
		}
	}
}

func (p *Phone) openAndAdoptGrant(link Link, base, code, rawGrantB64 string) (clientproto.ClientGrant, error) {
	sealed, err := b64u.DecodeString(rawGrantB64)
	if err != nil || b64u.EncodeToString(sealed) != rawGrantB64 {
		return clientproto.ClientGrant{}, clientproto.ErrGrant
	}
	grant, err := clientproto.OpenGrant(sealed, link.Key)
	if err != nil {
		return clientproto.ClientGrant{}, err
	}
	if err := grant.Check(p.device, code, p.signPub); err != nil {
		return clientproto.ClientGrant{}, err
	}
	grantBase, err := normalizeSyncAddress(grant.Relay)
	if err != nil || (base != "" && grantBase != base) {
		return clientproto.ClientGrant{}, ErrGrantRelayMismatch
	}
	verified := grant.VerifiedExecutors()
	if len(verified) == 0 {
		return clientproto.ClientGrant{}, clientproto.ErrGrant
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.paired = true
	p.grant = grant
	p.relay = base
	for _, card := range verified {
		if prev, ok := p.cards[card.Writer]; !ok || card.At > prev.At {
			p.cards[card.Writer] = card
		}
	}
	return grant, nil
}

// Computers calls `GET /v1/clients/{device}/computers` and returns the computers
// reported by the relay. The phone never trusts this list for keys or display names;
// only a verified Card decides those (contract S11, L3, L8).
func (p *Phone) Computers(ctx context.Context) ([]Computer, error) {
	var reply struct {
		Now       int64 `json:"now"`
		Computers []struct {
			Device  string `json:"device"`
			Online  *bool  `json:"online,omitempty"`
			Serving bool   `json:"serving"`
			Name    string `json:"name,omitempty"`
			At      int64  `json:"at"`
		} `json:"computers"`
	}
	if err := p.doSignedJSON(ctx, http.MethodGet, p.clientPath()+"/computers", nil, http.StatusOK, &reply); err != nil {
		return nil, err
	}
	out := make([]Computer, 0, len(reply.Computers))
	for _, item := range reply.Computers {
		online := item.Serving
		if item.Online != nil {
			online = *item.Online
		}
		out = append(out, Computer{
			Device:  item.Device,
			Online:  online,
			Serving: item.Serving,
			Name:    item.Name,
			At:      item.At,
		})
	}
	return out, nil
}

// Card reads the `card.<computer>` slot, opens it with the phone's box key, verifies
// its certificate and device signature under the grant's identity public key, and
// refuses a card whose writer is not computer, whose name is invalid, or whose
// timestamp is not newer than the last card slot accepted for computer (contract L3, L6).
func (p *Phone) Card(ctx context.Context, computer string) (clientproto.Card, error) {
	p.mu.Lock()
	if !p.paired {
		p.mu.Unlock()
		return clientproto.Card{}, ErrNotPaired
	}
	identityPub := append(ed25519.PublicKey(nil), p.grant.IdentityPub...)
	lastSlotAt := p.cardSlotAt[computer]
	prevCard, hadCard := p.cards[computer]
	p.mu.Unlock()

	slotName := "card." + computer
	var slot struct {
		Sealed  []byte `json:"sealed"`
		Writer  string `json:"writer"`
		At      int64  `json:"at"`
		Version uint64 `json:"version"`
	}
	if err := p.doSignedJSON(ctx, http.MethodGet, p.clientPath()+"/slots/"+url.PathEscape(slotName), nil, http.StatusOK, &slot); err != nil {
		return clientproto.Card{}, err
	}
	if len(slot.Sealed) > clientproto.MaxSlotBytes {
		return clientproto.Card{}, clientproto.ErrCard
	}
	if slot.Writer != computer {
		return clientproto.Card{}, ErrCardWriterMismatch
	}
	card, err := clientproto.OpenCard(slot.Sealed, &p.boxPub, &p.boxPriv)
	if err != nil {
		return clientproto.Card{}, err
	}
	if card.Writer != computer {
		return clientproto.Card{}, ErrCardWriterMismatch
	}
	if err := card.Verify(identityPub); err != nil {
		return clientproto.Card{}, err
	}
	if card.At <= lastSlotAt || (hadCard && card.At < prevCard.At) {
		return clientproto.Card{}, ErrCardReplay
	}

	p.mu.Lock()
	if card.At <= p.cardSlotAt[computer] || (p.cards[computer].At > card.At) {
		p.mu.Unlock()
		return clientproto.Card{}, ErrCardReplay
	}
	p.cardSlotAt[computer] = card.At
	p.cards[computer] = card
	p.mu.Unlock()
	return card, nil
}

func (p *Phone) ensureCard(ctx context.Context, computer string) (clientproto.Card, error) {
	p.mu.Lock()
	if !p.paired {
		p.mu.Unlock()
		return clientproto.Card{}, ErrNotPaired
	}
	card, ok := p.cards[computer]
	p.mu.Unlock()
	if ok {
		return card, nil
	}
	return p.Card(ctx, computer)
}

// Read reads one snapshot slot (`agents.entries`, `asks.list`, `agents.list`, and so on)
// for computer, opens the authenticated NaCl box under that computer's verified card
// `box_pub`, and checks it with clientproto.SlotPlain.Check against a per-(computer, slot)
// monotonic `lastAt` watermark so any replayed older slot or wrong writer is refused.
func (p *Phone) Read(ctx context.Context, computer, read string) (clientproto.SlotPlain, error) {
	plain, _, err := p.readBoxSlot(ctx, computer, read, false)
	return plain, err
}

func (p *Phone) readBoxSlot(ctx context.Context, computer, read string, allowUnchangedPoll bool) (clientproto.SlotPlain, []byte, error) {
	card, err := p.ensureCard(ctx, computer)
	if err != nil {
		return clientproto.SlotPlain{}, nil, err
	}
	if len(card.BoxPub) != ed25519.PublicKeySize {
		return clientproto.SlotPlain{}, nil, clientproto.ErrCard
	}
	slotName := read
	if !strings.HasSuffix(slotName, "."+computer) {
		slotName = read + "." + computer
	}
	var slot struct {
		Sealed  []byte `json:"sealed"`
		Writer  string `json:"writer"`
		At      int64  `json:"at"`
		Version uint64 `json:"version"`
	}
	if err := p.doSignedJSON(ctx, http.MethodGet, p.clientPath()+"/slots/"+url.PathEscape(slotName), nil, http.StatusOK, &slot); err != nil {
		return clientproto.SlotPlain{}, nil, err
	}
	if len(slot.Sealed) > clientproto.MaxSlotBytes {
		return clientproto.SlotPlain{}, nil, clientproto.ErrSlot
	}
	if slot.Writer != computer {
		return clientproto.SlotPlain{}, nil, ErrSlotWriterMismatch
	}
	var execPub [ed25519.PublicKeySize]byte
	copy(execPub[:], card.BoxPub)
	plain, err := clientproto.OpenSlot(slot.Sealed, &execPub, &p.boxPriv)
	if err != nil {
		return clientproto.SlotPlain{}, nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	lastAt := p.slotAt[slotName]
	if allowUnchangedPoll && plain.At == lastAt && lastAt > 0 && bytes.Equal(slot.Sealed, p.lastOutcomesSealed[slotName]) {
		if err := plain.Check(slotName, p.device, computer, lastAt-1); err != nil {
			return clientproto.SlotPlain{}, nil, err
		}
		return plain, slot.Sealed, nil
	}
	if plain.Executor != computer {
		return clientproto.SlotPlain{}, nil, ErrSlotWriterMismatch
	}
	if plain.At <= lastAt {
		if err := plain.Check(slotName, p.device, computer, lastAt); err != nil {
			return clientproto.SlotPlain{}, nil, ErrSlotReplay
		}
	}
	if err := plain.Check(slotName, p.device, computer, lastAt); err != nil {
		return clientproto.SlotPlain{}, nil, err
	}
	p.slotAt[slotName] = plain.At
	if allowUnchangedPoll {
		p.lastOutcomesSealed[slotName] = append([]byte(nil), slot.Sealed...)
	}
	return plain, slot.Sealed, nil
}

// Outcomes reads the `outcomes.<computer>` slot under that computer's verified card
// and decodes its payload with clientproto.DecodeOutcomes.
func (p *Phone) Outcomes(ctx context.Context, computer string) ([]clientproto.Outcome, error) {
	plain, _, err := p.readBoxSlot(ctx, computer, "outcomes", true)
	if err != nil {
		return nil, err
	}
	return clientproto.DecodeOutcomes(plain.Payload)
}

// Call builds a clientproto.CallPlain for computer, seals it anonymously to that
// computer's verified card `box_pub` with clientproto.SealCall, and appends it to
// `POST /v1/clients/{device}/queue` signed with `codeaf-req-v1`.
func (p *Phone) Call(ctx context.Context, computer string, method string, params any, opts CallOptions) (CallRef, error) {
	card, err := p.ensureCard(ctx, computer)
	if err != nil {
		return CallRef{}, err
	}
	if len(card.BoxPub) != ed25519.PublicKeySize {
		return CallRef{}, clientproto.ErrCard
	}
	rawParams, clientID, err := p.prepareCallParams(method, params, opts.ClientID)
	if err != nil {
		return CallRef{}, err
	}

	p.mu.Lock()
	number := opts.Number
	if number <= 0 {
		number = p.nextNumber
	}
	at := p.now().UnixMilli()
	p.mu.Unlock()

	lifetime := clientproto.CallLifetime
	if method == clientproto.MethodCreate {
		lifetime = clientproto.CreateLifetime
	}
	if opts.Lifetime != 0 {
		lifetime = opts.Lifetime
	}
	notAfter := at + lifetime.Milliseconds()

	plain := clientproto.CallPlain{
		V:        clientproto.CallVersion,
		Client:   p.device,
		Number:   number,
		At:       at,
		NotAfter: notAfter,
		Target:   computer,
		Method:   method,
		Params:   rawParams,
		FaceID:   opts.FaceID,
	}
	var execPub [ed25519.PublicKeySize]byte
	copy(execPub[:], card.BoxPub)
	sealed, err := clientproto.SealCall(plain, &execPub, p.rand)
	if err != nil {
		return CallRef{}, err
	}
	outer := struct {
		Number int64  `json:"number"`
		At     int64  `json:"at"`
		Method string `json:"method"`
		Target string `json:"target"`
		Sealed []byte `json:"sealed"`
	}{
		Number: number,
		At:     at,
		Method: method,
		Target: computer,
		Sealed: sealed,
	}
	if err := p.doSignedJSON(ctx, http.MethodPost, p.clientPath()+"/queue", outer, http.StatusNoContent, nil); err != nil {
		return CallRef{}, err
	}

	p.mu.Lock()
	if number >= p.nextNumber {
		p.nextNumber = number + 1
	}
	p.mu.Unlock()

	return CallRef{
		Number:   number,
		ClientID: clientID,
		At:       at,
		NotAfter: notAfter,
		Target:   computer,
		Method:   method,
	}, nil
}

// Create queues an `agents.create` call targeting computer with a fresh `{client_id}` (design 3.5).
func (p *Phone) Create(ctx context.Context, computer string) (CallRef, error) {
	return p.Call(ctx, computer, clientproto.MethodCreate, CreateParams{}, CallOptions{})
}

// Send queues an `agents.send` call targeting computer for chat with `{id, text, client_id}` (design 3.5).
func (p *Phone) Send(ctx context.Context, computer, chat, text string) (CallRef, error) {
	return p.Call(ctx, computer, clientproto.MethodSend, SendParams{ID: chat, Text: text}, CallOptions{})
}

// WaitOutcome polls Outcomes (invoking step between tries when non-nil) until an
// outcome for ref.Number appears or the phone's own clock passes ref.NotAfter plus
// ExpiryAllowance, with any outcome published by the computer winning when both exist.
func (p *Phone) WaitOutcome(ctx context.Context, computer string, ref CallRef, step func()) (clientproto.Outcome, error) {
	for {
		outcomes, err := p.Outcomes(ctx, computer)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return clientproto.Outcome{}, err
		}
		if err == nil {
			for i := len(outcomes) - 1; i >= 0; i-- {
				if outcomes[i].Number == ref.Number {
					return outcomes[i], nil
				}
			}
		}
		if ref.NotAfter > 0 && p.now().UnixMilli() > ref.NotAfter+ExpiryAllowance.Milliseconds() {
			return clientproto.Outcome{
				Number: ref.Number,
				Status: clientproto.OutcomeExpired,
				Reason: clientproto.ReasonExpired,
			}, nil
		}
		if err := ctx.Err(); err != nil {
			return clientproto.Outcome{}, err
		}
		if step != nil {
			step()
		} else {
			runtime.Gosched()
		}
		if err := ctx.Err(); err != nil {
			return clientproto.Outcome{}, err
		}
	}
}

// Revoked probes the relay as this phone and reports whether the phone has been
// revoked (contract V1).
func (p *Phone) Revoked(ctx context.Context) bool {
	_, err := p.Computers(ctx)
	return errors.Is(err, ErrRevoked)
}

func (p *Phone) drawClientID() (string, error) {
	var b [clientproto.ClientLinkKeyBytes]byte
	if _, err := io.ReadFull(p.rand, b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (p *Phone) prepareCallParams(method string, params any, optClientID string) (json.RawMessage, string, error) {
	clientID := strings.TrimSpace(optClientID)
	switch v := params.(type) {
	case nil:
		if method == clientproto.MethodCreate {
			if clientID == "" {
				var err error
				clientID, err = p.drawClientID()
				if err != nil {
					return nil, "", err
				}
			}
			raw, err := json.Marshal(CreateParams{ClientID: clientID})
			return raw, clientID, err
		}
		return json.RawMessage(`{}`), clientID, nil
	case CreateParams:
		if v.ClientID == "" {
			v.ClientID = clientID
		}
		if v.ClientID == "" {
			var err error
			v.ClientID, err = p.drawClientID()
			if err != nil {
				return nil, "", err
			}
		}
		raw, err := json.Marshal(v)
		return raw, v.ClientID, err
	case *CreateParams:
		var cp CreateParams
		if v != nil {
			cp = *v
		}
		return p.prepareCallParams(method, cp, clientID)
	case SendParams:
		if v.ClientID == "" {
			v.ClientID = clientID
		}
		if v.ClientID == "" {
			var err error
			v.ClientID, err = p.drawClientID()
			if err != nil {
				return nil, "", err
			}
		}
		raw, err := json.Marshal(v)
		return raw, v.ClientID, err
	case *SendParams:
		var sp SendParams
		if v != nil {
			sp = *v
		}
		return p.prepareCallParams(method, sp, clientID)
	case json.RawMessage:
		return v, extractClientID(v, clientID), nil
	case []byte:
		return json.RawMessage(v), extractClientID(v, clientID), nil
	default:
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, "", err
		}
		return raw, extractClientID(raw, clientID), nil
	}
}

func extractClientID(raw []byte, fallback string) string {
	var obj struct {
		ClientID string `json:"client_id"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.ClientID != "" {
		return obj.ClientID
	}
	return fallback
}

type phoneSigner struct {
	identityPub ed25519.PublicKey
	cert        identity.Cert
	signPriv    ed25519.PrivateKey
}

func (s phoneSigner) IdentityKey() ed25519.PublicKey { return s.identityPub }
func (s phoneSigner) Cert() identity.Cert            { return s.cert }
func (s phoneSigner) Sign(msg []byte) []byte         { return ed25519.Sign(s.signPriv, msg) }

func (p *Phone) signer() (phoneSigner, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.paired {
		return phoneSigner{}, "", ErrNotPaired
	}
	return phoneSigner{
		identityPub: append(ed25519.PublicKey(nil), p.grant.IdentityPub...),
		cert:        p.grant.Cert,
		signPriv:    p.signPriv,
	}, p.relay, nil
}

func (p *Phone) clientPath() string {
	return "/v1/clients/" + url.PathEscape(p.device)
}

func (p *Phone) doOpenJSON(ctx context.Context, base, method, route string, in any, wantStatus int, out any) error {
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, wireauth.Endpoint(base, route), bytes.NewReader(body))
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, clientproto.MaxFabricBody))
	if err != nil {
		return err
	}
	if resp.StatusCode != wantStatus {
		return decodeRelayError(resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

func (p *Phone) getLinkRequest(ctx context.Context, base, route string, out *directory.Request) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wireauth.Endpoint(base, route), nil)
	if err != nil {
		return 0, err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return http.StatusNoContent, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, clientproto.MaxFabricBody))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return resp.StatusCode, ErrLinkExpired
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, decodeRelayError(resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return resp.StatusCode, err
	}
	return http.StatusOK, nil
}

func (p *Phone) doSignedJSON(ctx context.Context, method, route string, in any, wantStatus int, out any) error {
	s, base, err := p.signer()
	if err != nil {
		return err
	}
	var body []byte
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, wireauth.Endpoint(base, route), bytes.NewReader(body))
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	reqsign.Sign(req, body, s, p.now())
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, clientproto.MaxFabricBody))
	if err != nil {
		return err
	}
	if resp.StatusCode != wantStatus {
		return decodeRelayError(resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

func decodeRelayError(status int, raw []byte) error {
	var body struct {
		Err   string `json:"err"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	code := body.Err
	if code == "" {
		code = body.Error
	}
	switch {
	case status == http.StatusUnauthorized && code == "revoked":
		return ErrRevoked
	case status == http.StatusUnauthorized && code == "skew":
		return wireauth.ErrSkew
	case status == http.StatusUnauthorized:
		return ErrUnauthorized
	case status == http.StatusForbidden && code == "method_denied":
		return ErrMethodDenied
	case status == http.StatusNotFound:
		return ErrNotFound
	case status == http.StatusConflict && code == "sequence":
		return ErrSequence
	case status == http.StatusTooManyRequests && code == "full", status == http.StatusInsufficientStorage:
		return ErrFull
	case status == http.StatusTooManyRequests:
		return wireauth.ErrRateLimited
	case status == http.StatusGone && code == "rotated":
		return wireauth.ErrRotated
	case status == http.StatusGone:
		return wireauth.ErrGone
	default:
		if code != "" {
			return fmt.Errorf("phonetest: relay HTTP %d (%s)", status, code)
		}
		return fmt.Errorf("phonetest: relay HTTP %d", status)
	}
}

func normalizeSyncAddress(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.Contains(trimmed, "?") || strings.Contains(trimmed, "#") {
		return "", ErrSetupLink
	}
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Host == "" || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", ErrSetupLink
	}
	if _, rest, ok := strings.Cut(trimmed, "://"); !ok || strings.Contains(authorityPart(rest), "@") {
		return "", ErrSetupLink
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func authorityPart(afterScheme string) string {
	if i := strings.IndexByte(afterScheme, '/'); i >= 0 {
		return afterScheme[:i]
	}
	return afterScheme
}

// sealDeviceName seals the phone's display name under the link key using r for the
// secretbox nonce so tests with a seeded reader stay deterministic while matching
// pair.OpenDeviceName byte for byte.
func sealDeviceName(r io.Reader, linkKey []byte, name string) (string, error) {
	var nonce [clientproto.NonceSize]byte
	if _, err := io.ReadFull(r, nonce[:]); err != nil {
		return "", err
	}
	key := sha256.Sum256(append([]byte("codeaf/link-name/v1\n"), linkKey...))
	plain := cutUTF8Bytes(cleanDeviceName(name), maxLinkNameBytes)
	return b64u.EncodeToString(secretbox.Seal(nonce[:], []byte(plain), &nonce, &key)), nil
}

func cleanDeviceName(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func cutUTF8Bytes(s string, limit int) string {
	for len(s) > limit {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}
