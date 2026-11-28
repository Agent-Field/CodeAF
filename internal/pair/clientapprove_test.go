package pair

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/fabricclient"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

type recordedReq struct {
	Method string
	Path   string
}

type clientPairRig struct {
	t        *testing.T
	clock    *directorytest.FakeClock
	svc      *relayserve.Service
	srv      *httptest.Server
	home     string
	id       identity.Identity
	dev      identity.Dev
	execPub  *[32]byte
	execPriv *[32]byte
	dir      *directory.HTTP
	fc       *fabricclient.Client
	approver Approver

	mu       sync.Mutex
	recorded []recordedReq
}

func newClientPairRig(t *testing.T) *clientPairRig {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	clock := directorytest.NewFakeClock()
	svc := relayserve.New(relayserve.Config{
		Store: t.TempDir(),
		Now:   clock.Now,
		After: clock.After,
	})
	r := &clientPairRig{
		t:     t,
		clock: clock,
		svc:   svc,
		home:  t.TempDir(),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.recorded = append(r.recorded, recordedReq{Method: req.Method, Path: req.URL.Path})
		r.mu.Unlock()
		svc.Handler.ServeHTTP(w, req)
	}))
	t.Cleanup(func() {
		srv.Close()
		_ = svc.Close()
	})
	r.srv = srv

	id, err := identity.Ensure(r.home)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(r.home)
	if err != nil {
		t.Fatal(err)
	}
	execPub, execPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	r.id = id
	r.dev = dev
	r.execPub = execPub
	r.execPriv = execPriv

	signer := testSigner{id: id, dev: dev}
	r.dir = directory.NewHTTP(srv.URL, reqsign.SignFor(signer, clock.Now), srv.Client())
	sealed, err := directory.SealName(directory.MetadataKey(id.CellKey()), "desk")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1, Name: sealed, AddedBy: id.ID()}); err != nil {
		t.Fatal(err)
	}

	r.fc = fabricclient.New(srv.URL, signer, srv.Client())
	r.fc.Now = clock.Now

	r.approver = Approver{
		Requests: directory.NewLinkHTTP(srv.URL, srv.Client()),
		Dir:      r.dir,
		Fabric:   r.fc,
		Identity: id,
		SyncURL:  srv.URL + "/",
		Now:      clock.Now,
		Card: func(context.Context) (clientproto.Card, error) {
			return r.signedCard(), nil
		},
	}
	return r
}

func (r *clientPairRig) signedCard() clientproto.Card {
	c := clientproto.Card{
		V:      1,
		Writer: r.dev.ID(),
		Cert:   r.dev.Cert,
		BoxPub: append([]byte(nil), r.execPub[:]...),
		Name:   "desk",
		At:     r.clock.Now().UnixMilli(),
	}
	c.Sig = r.dev.Sign(c.SignText())
	return c
}

func (r *clientPairRig) resetRecorded() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = nil
}

func (r *clientPairRig) writeRequests() []recordedReq {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []recordedReq
	for _, req := range r.recorded {
		if req.Method != http.MethodGet {
			out = append(out, req)
		}
	}
	return out
}

type phoneJoiner struct {
	signPub  ed25519.PublicKey
	signPriv ed25519.PrivateKey
	boxPub   *[32]byte
	boxPriv  *[32]byte
	linkKey  []byte
	device   string
	check    string
	bind     string
	ref      LinkRef
}

func (r *clientPairRig) openPhoneRequest(t *testing.T, name string) phoneJoiner {
	t.Helper()
	signPub, signPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	boxPub, boxPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	linkKey, err := NewLinkKey()
	if err != nil {
		t.Fatal(err)
	}
	mac := clientproto.BindMAC(linkKey, signPub, boxPub[:])
	bind := b64u.EncodeToString(clientproto.Bind(mac))
	check := clientproto.CheckDigits(mac)
	sealedName, err := SealDeviceName(linkKey, name)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := r.approver.Requests.CreateRequest(context.Background(), directory.NewRequest{
		Pubkey:     b64u.EncodeToString(signPub),
		X25519:     b64u.EncodeToString(boxPub[:]),
		NameSealed: sealedName,
		Platform:   "ios",
		Class:      clientproto.ClientLinkClass,
		Bind:       bind,
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := ReadLink(opened.Code + "." + b64u.EncodeToString(linkKey))
	if err != nil {
		t.Fatalf("ReadLink phone token: %v", err)
	}
	return phoneJoiner{
		signPub:  signPub,
		signPriv: signPriv,
		boxPub:   boxPub,
		boxPriv:  boxPriv,
		linkKey:  linkKey,
		device:   clientproto.DeviceIDOf(signPub),
		check:    check,
		bind:     bind,
		ref:      ref,
	}
}

// TestP7ReadLinkTenCharClientTokens verifies that ReadLink reads a 10-character
// phone token with its 22-character key, refuses a keyless 10-character code with
// ErrLinkKeyless, and keeps short-link URLs restricted to 8-character computer codes (contract P7).
func TestP7ReadLinkTenCharClientTokens(t *testing.T) {
	key, err := NewLinkKey()
	if err != nil {
		t.Fatal(err)
	}
	keyText := b64u.EncodeToString(key)
	if len(keyText) != 22 {
		t.Fatalf("encoded link key len = %d, want 22", len(keyText))
	}

	// 10-character phone token with key succeeds.
	for _, token := range []string{
		"k7m2q9xd4a." + keyText,
		"K7M2Q9XD4A." + keyText,
		"  k7m2q9xd4a." + keyText + "  ",
	} {
		ref, err := ReadLink(token)
		if err != nil {
			t.Fatalf("ReadLink(%q) unexpected error: %v", token, err)
		}
		if ref.Code != "k7m2q9xd4a" || !bytes.Equal(ref.Key, key) {
			t.Fatalf("ReadLink(%q) = %+v", token, ref)
		}
		if !IsLinkText(token) {
			t.Fatalf("IsLinkText(%q) = false, want true", token)
		}
	}

	// 10-character code without a key fails with ErrLinkKeyless and the exact design 9.2 sentence.
	for _, keyless := range []string{"k7m2q9xd4a", "K7M2Q9XD4A", "k7m2q9xd4a."} {
		_, err := ReadLink(keyless)
		if !errors.Is(err, ErrLinkKeyless) {
			t.Fatalf("ReadLink(%q) = %v, want ErrLinkKeyless", keyless, err)
		}
		if err.Error() != "that link has no key; copy the whole link from the phone" {
			t.Fatalf("ErrLinkKeyless text = %q", err.Error())
		}
	}

	// Short-link URL forms stay 8 characters only; 10-character codes in URLs are refused.
	for _, badURL := range []string{
		"https://codeaf.agentfield.ai/p/k7m2q9xd4a#" + keyText,
		"codeaf://pair?code=k7m2q9xd4a#" + keyText,
	} {
		if _, err := ReadLink(badURL); !errors.Is(err, ErrLinkShape) {
			t.Fatalf("ReadLink(%q) = %v, want ErrLinkShape", badURL, err)
		}
	}
}

// TestP2ClientCheckAndBindRefusalsWriteNothing verifies that a wrong --check
// number or a mismatched bind enrolls, registers and grants nothing, asserting
// directly on the relay's recorded HTTP requests (contract P2).
func TestP2ClientCheckAndBindRefusalsWriteNothing(t *testing.T) {
	r := newClientPairRig(t)
	ctx := context.Background()
	phone := r.openPhoneRequest(t, "iphone")

	as, err := r.approver.Look(ctx, phone.ref)
	if err != nil {
		t.Fatalf("Look valid phone request: %v", err)
	}
	if !as.Client || as.Name != "iphone" || as.Check != phone.check {
		t.Fatalf("Look returned %+v, want Client=true Name=iphone Check=%s", as, phone.check)
	}

	// 1. A wrong typed --check number refuses before step 1 and sends zero write requests to the relay.
	r.resetRecorded()
	wrongCheck := "0000"
	if phone.check == wrongCheck {
		wrongCheck = "0001"
	}
	err = r.approver.ApproveClientChecked(ctx, as, wrongCheck)
	if !errors.Is(err, ErrCheckNumberMismatch) || err.Error() != "that check number does not match. Nothing was approved." {
		t.Fatalf("ApproveClientChecked wrong check err = %v", err)
	}
	if writes := r.writeRequests(); len(writes) != 0 {
		t.Fatalf("relay received write requests on wrong check: %+v", writes)
	}

	// 2. A mismatched link key / bind in Look refuses with ErrBindMismatch and sends zero write requests.
	otherKey, err := NewLinkKey()
	if err != nil {
		t.Fatal(err)
	}
	r.resetRecorded()
	badRef := LinkRef{Code: phone.ref.Code, Key: otherKey}
	if _, err := r.approver.Look(ctx, badRef); !errors.Is(err, ErrBindMismatch) ||
		err.Error() != "that request does not match its link. Nothing was approved." {
		t.Fatalf("Look with wrong link key err = %v, want ErrBindMismatch", err)
	}
	if writes := r.writeRequests(); len(writes) != 0 {
		t.Fatalf("relay received write requests on mismatched bind: %+v", writes)
	}

	// Assert on the relay's state that nothing was enrolled, registered or granted.
	enrolls, err := r.fc.ListEnrollments(ctx)
	if err != nil || len(enrolls) != 0 {
		t.Fatalf("enrollments after refusals = %+v (%v), want empty", enrolls, err)
	}
	listing, err := r.dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := listing.Devices[phone.device]; ok {
		t.Fatalf("phone %s was registered in directory after refusal", phone.device)
	}
	reqState, err := r.approver.Requests.GetRequest(ctx, phone.ref.Code, 0)
	if err != nil || reqState.State != directory.RequestPending || reqState.Grant != nil {
		t.Fatalf("request state after refusals = %+v (%v), want pending", reqState, err)
	}

	// 3. With the matching locally computed check number, ApproveClientChecked succeeds.
	if err := r.approver.ApproveClientChecked(ctx, as, phone.check); err != nil {
		t.Fatalf("ApproveClientChecked matching check: %v", err)
	}
}

// TestP3ClientGrantContainsNoIdentitySecrets decodes the sealed ClientGrant with
// the link key and asserts the exact JSON key set and the absence of any
// identity or computer secret in hex or base64 (contract P3).
func TestP3ClientGrantContainsNoIdentitySecrets(t *testing.T) {
	r := newClientPairRig(t)
	ctx := context.Background()
	phone := r.openPhoneRequest(t, "pixel")

	as, err := r.approver.Look(ctx, phone.ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.approver.Approve(ctx, as); err != nil {
		t.Fatal(err)
	}

	decided, err := r.approver.Requests.GetRequest(ctx, phone.ref.Code, 0)
	if err != nil || decided.State != directory.RequestApproved || decided.Grant == nil {
		t.Fatalf("decided request = %+v (%v)", decided, err)
	}
	sealed, err := b64u.DecodeString(*decided.Grant)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) < clientproto.NonceSize+secretbox.Overhead {
		t.Fatalf("sealed grant too short: %d", len(sealed))
	}
	var nonce [clientproto.NonceSize]byte
	copy(nonce[:], sealed[:clientproto.NonceSize])
	grantKey := clientproto.GrantKey(phone.linkKey)
	plain, ok := secretbox.Open(nil, sealed[clientproto.NonceSize:], &nonce, &grantKey)
	if !ok {
		t.Fatal("secretbox.Open failed under GrantKey(linkKey)")
	}

	// Assert exact key set: v, device, code, cert, identity_pub, relay, executors.
	var obj map[string]any
	if err := json.Unmarshal(plain, &obj); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"v", "device", "code", "cert", "identity_pub", "relay", "executors"}
	if len(obj) != len(wantKeys) {
		t.Fatalf("grant keys = %v, want %v", obj, wantKeys)
	}
	for _, k := range wantKeys {
		if _, exists := obj[k]; !exists {
			t.Fatalf("grant JSON missing key %q: %s", k, plain)
		}
	}

	// Collect every secret of the identity and the approving computer and verify none appear in hex or base64.
	idDoc, err := r.id.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var rawID struct {
		SigningSeed string `json:"signing_seed"`
		DedupSecret string `json:"dedup_secret"`
		CellKey     string `json:"cell_key"`
	}
	if err := json.Unmarshal(idDoc, &rawID); err != nil {
		t.Fatal(err)
	}
	seedBytes, err := hex.DecodeString(rawID.SigningSeed)
	if err != nil {
		t.Fatal(err)
	}
	secrets := [][]byte{
		seedBytes,
		r.id.DedupSecret(),
		r.id.CellKey(),
		directory.MetadataKey(r.id.CellKey()),
		r.execPriv[:],
	}
	plainText := string(plain)
	plainLower := strings.ToLower(plainText)
	for i, sec := range secrets {
		if len(sec) == 0 {
			t.Fatalf("empty secret at index %d", i)
		}
		encodings := []string{
			strings.ToLower(hex.EncodeToString(sec)),
			base64.StdEncoding.EncodeToString(sec),
			base64.RawStdEncoding.EncodeToString(sec),
			base64.URLEncoding.EncodeToString(sec),
			base64.RawURLEncoding.EncodeToString(sec),
		}
		for _, enc := range encodings {
			if strings.Contains(plainText, enc) || strings.Contains(plainLower, strings.ToLower(enc)) {
				t.Fatalf("grant JSON leaked secret %d encoding %q in %s", i, enc, plainText)
			}
			if strings.Contains(*decided.Grant, enc) {
				t.Fatalf("wire grant leaked secret %d encoding %q", i, enc)
			}
		}
	}
}

// TestP6ClientAndFullGrantSeparation verifies that a client approval never sends
// a full grant, a normal computer approval never sends Class: "client", and a
// relay that downgrades or upgrades the request class is refused (contract P6).
func TestP6ClientAndFullGrantSeparation(t *testing.T) {
	r := newClientPairRig(t)
	ctx := context.Background()

	// Direction 1: a phone request cannot be approved through the full-device approval builder.
	phone := r.openPhoneRequest(t, "phone")
	phoneAs, err := r.approver.Look(ctx, phone.ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.approver.approval(phoneAs); !errors.Is(err, ErrBindMismatch) {
		t.Fatalf("full approval builder on phone Asking = %v, want ErrBindMismatch", err)
	}
	forcedFull := phoneAs
	forcedFull.Client = false
	if err := r.approver.Approve(ctx, forcedFull); !errors.Is(err, ErrBindMismatch) {
		t.Fatalf("Approve(Client=false) on 10-char code = %v, want ErrBindMismatch", err)
	}

	// Direction 2: a normal computer request gets the full grant (Class == "") and is refused by ApproveClient.
	compSeed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	compBoxPub, compBoxPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	compLinkKey, err := NewLinkKey()
	if err != nil {
		t.Fatal(err)
	}
	compName, err := SealDeviceName(compLinkKey, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	compOpened, err := r.approver.Requests.CreateRequest(ctx, directory.NewRequest{
		Pubkey:     b64u.EncodeToString(compSeed.Public()),
		X25519:     b64u.EncodeToString(compBoxPub[:]),
		NameSealed: compName,
		Platform:   "linux",
	})
	if err != nil {
		t.Fatal(err)
	}
	compRef, err := ReadLink(compOpened.Code + "." + b64u.EncodeToString(compLinkKey))
	if err != nil {
		t.Fatal(err)
	}
	compAs, err := r.approver.Look(ctx, compRef)
	if err != nil {
		t.Fatal(err)
	}
	if compAs.Client {
		t.Fatal("computer Asking unexpectedly marked Client=true")
	}
	if err := r.approver.ApproveClient(ctx, compAs); !errors.Is(err, ErrBindMismatch) {
		t.Fatalf("ApproveClient on computer Asking = %v, want ErrBindMismatch", err)
	}
	forcedClient := compAs
	forcedClient.Client = true
	if err := r.approver.ApproveClient(ctx, forcedClient); !errors.Is(err, ErrBindMismatch) {
		t.Fatalf("ApproveClient on 8-char code = %v, want ErrBindMismatch", err)
	}

	// Normal Approve on compAs still issues the full identity grant with Class == "".
	body, err := r.approver.approval(compAs)
	if err != nil {
		t.Fatal(err)
	}
	if body.Class != "" {
		t.Fatalf("computer approval Class = %q, want empty", body.Class)
	}
	if err := r.approver.Approve(ctx, compAs); err != nil {
		t.Fatalf("Approve computer request: %v", err)
	}
	compDecided, err := r.approver.Requests.GetRequest(ctx, compRef.Code, 0)
	if err != nil || compDecided.State != directory.RequestApproved || compDecided.Grant == nil {
		t.Fatalf("decided computer request = %+v (%v)", compDecided, err)
	}
	fullGrant, err := openLinkGrant(*compDecided.Grant, compBoxPub, compBoxPriv)
	if err != nil || fullGrant.Identity.ID() != r.id.ID() {
		t.Fatalf("opened computer full grant = %+v (%v)", fullGrant, err)
	}

	// Class downgrade (10-char code with empty class) and class upgrade (8-char code with class "client") are both refused by Look.
	downgraded := directory.Request{
		Code:       phone.ref.Code,
		Device:     phone.device,
		Pubkey:     b64u.EncodeToString(phone.signPub),
		X25519:     b64u.EncodeToString(phone.boxPub[:]),
		NameSealed: compName,
		Platform:   "ios",
		Class:      "",
		Check:      directory.CheckOf(phone.signPub),
		State:      directory.RequestPending,
	}
	if _, err := askingOf(phone.ref, downgraded); !errors.Is(err, ErrBindMismatch) {
		t.Fatalf("downgraded 10-char request gave %v, want ErrBindMismatch", err)
	}
	compMAC := clientproto.BindMAC(compLinkKey, compSeed.Public(), compBoxPub[:])
	upgraded := directory.Request{
		Code:       compRef.Code,
		Device:     clientproto.DeviceIDOf(compSeed.Public()),
		Pubkey:     b64u.EncodeToString(compSeed.Public()),
		X25519:     b64u.EncodeToString(compBoxPub[:]),
		NameSealed: compName,
		Platform:   "linux",
		Class:      clientproto.ClientLinkClass,
		Bind:       b64u.EncodeToString(clientproto.Bind(compMAC)),
		Check:      clientproto.CheckDigits(compMAC),
		State:      directory.RequestPending,
	}
	if _, err := askingOf(compRef, upgraded); !errors.Is(err, ErrBindMismatch) {
		t.Fatalf("upgraded 8-char request gave %v, want ErrBindMismatch", err)
	}
}

// lyingRequests simulates a malicious relay that intercepts a client-class link
// request, substitutes its own keypair, and returns matching check digits while
// not knowing the token's link key.
type lyingRequests struct {
	inner        directory.Requests
	evilSignPub  ed25519.PublicKey
	evilBoxPub   *[32]byte
	overrideBind string
	overrideCode string
	overrideCls  *string
}

func (l *lyingRequests) CreateRequest(ctx context.Context, in directory.NewRequest) (directory.Opened, error) {
	return l.inner.CreateRequest(ctx, in)
}

func (l *lyingRequests) GetRequest(ctx context.Context, code string, wait time.Duration) (directory.Request, error) {
	r, err := l.inner.GetRequest(ctx, code, wait)
	if err != nil {
		return r, err
	}
	if len(l.evilSignPub) > 0 {
		r.Pubkey = b64u.EncodeToString(l.evilSignPub)
		r.Device = clientproto.DeviceIDOf(l.evilSignPub)
		r.Check = directory.CheckOf(l.evilSignPub)
	}
	if l.evilBoxPub != nil {
		r.X25519 = b64u.EncodeToString(l.evilBoxPub[:])
	}
	if l.overrideBind != "" {
		r.Bind = l.overrideBind
	}
	if l.overrideCode != "" {
		r.Code = l.overrideCode
	}
	if l.overrideCls != nil {
		r.Class = *l.overrideCls
	}
	return r, nil
}

// TestP9LyingRelayKeySubstitutionRefused tests against a stub relay that rewrites
// the phone's keys (and returns matching check digits) or downgrades the class:
// the approver recomputes the MAC locally, refuses with ErrBindMismatch, and
// writes nothing to the relay (contract P9).
func TestP9LyingRelayKeySubstitutionRefused(t *testing.T) {
	r := newClientPairRig(t)
	ctx := context.Background()
	phone := r.openPhoneRequest(t, "iphone")

	evilSignPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	evilBoxPub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	guessedKey := bytes.Repeat([]byte{0x99}, clientproto.ClientLinkKeyBytes)
	forgedMAC := clientproto.BindMAC(guessedKey, evilSignPub, evilBoxPub[:])
	forgedBind := b64u.EncodeToString(clientproto.Bind(forgedMAC))

	emptyClass := ""
	cases := []struct {
		name string
		stub *lyingRequests
	}{
		{
			name: "relay substitutes sign key and keeps original bind",
			stub: &lyingRequests{inner: r.approver.Requests, evilSignPub: evilSignPub},
		},
		{
			name: "relay substitutes box key and keeps original bind",
			stub: &lyingRequests{inner: r.approver.Requests, evilBoxPub: evilBoxPub},
		},
		{
			name: "relay substitutes both keys and forges bind without link key",
			stub: &lyingRequests{
				inner:        r.approver.Requests,
				evilSignPub:  evilSignPub,
				evilBoxPub:   evilBoxPub,
				overrideBind: forgedBind,
			},
		},
		{
			name: "relay downgrades class to empty on 10-char request",
			stub: &lyingRequests{
				inner:       r.approver.Requests,
				overrideCls: &emptyClass,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.resetRecorded()
			approver := r.approver
			approver.Requests = tc.stub
			_, err := approver.Look(ctx, phone.ref)
			if !errors.Is(err, ErrBindMismatch) {
				t.Fatalf("Look err = %v, want ErrBindMismatch", err)
			}
			if err.Error() != "that request does not match its link. Nothing was approved." {
				t.Fatalf("unexpected error text: %q", err.Error())
			}
			if writes := r.writeRequests(); len(writes) != 0 {
				t.Fatalf("relay received writes after key substitution: %+v", writes)
			}
		})
	}

	// Verify also that when keys and bind are honest but the relay lies about r.Check,
	// Look ignores r.Check and computes the four digits locally from the MAC.
	honestWithBogusRelayCheck := &lyingRequests{inner: r.approver.Requests}
	approver := r.approver
	approver.Requests = honestWithBogusRelayCheck
	as, err := approver.Look(ctx, phone.ref)
	if err != nil {
		t.Fatalf("Look honest request: %v", err)
	}
	if as.Check != phone.check {
		t.Fatalf("as.Check = %q, want locally computed %q", as.Check, phone.check)
	}
}

// stepFailFabric wraps a Fabric and Dir to inject failures at a chosen step of ApproveClient.
type stepFailFabric struct {
	inner           Fabric
	failPutEnroll   bool
	failRegister    bool
	putEnrollCalls  int
	registerCalls   int
	listEnrollCalls int
}

func (f *stepFailFabric) ListEnrollments(ctx context.Context) ([]fabricclient.EnrollmentRow, error) {
	f.listEnrollCalls++
	return f.inner.ListEnrollments(ctx)
}

func (f *stepFailFabric) PutEnrollment(ctx context.Context, e clientproto.Enrollment) error {
	f.putEnrollCalls++
	if f.failPutEnroll {
		return fabricclient.ErrUnavailable
	}
	return f.inner.PutEnrollment(ctx, e)
}

func (f *stepFailFabric) RegisterClient(ctx context.Context, cert identity.Cert, methods []string, sealedName string) error {
	f.registerCalls++
	if f.failRegister {
		return fabricclient.ErrUnavailable
	}
	return f.inner.RegisterClient(ctx, cert, methods, sealedName)
}

func (f *stepFailFabric) Base() string {
	if b, ok := f.inner.(interface{ Base() string }); ok {
		return b.Base()
	}
	return ""
}

type stepFailDir struct {
	directory.Client
	failApprove       bool
	failAfterDecision bool
	approveCalls      int
}

func (d *stepFailDir) ApproveRequest(ctx context.Context, code string, a directory.Approval) error {
	d.approveCalls++
	if d.failApprove {
		return directory.ErrUnreachable
	}
	err := d.Client.ApproveRequest(ctx, code, a)
	if err == nil && d.failAfterDecision {
		return directory.ErrUnreachable
	}
	return err
}

// TestP10ClientApprovalCrashRecoveryAndIdempotency stops the approval after each
// of the three steps by failing the next call, runs it again, and verifies it
// completes with no duplicate row and without bumping the enrollment ver (contract P10).
func TestP10ClientApprovalCrashRecoveryAndIdempotency(t *testing.T) {
	r := newClientPairRig(t)
	ctx := context.Background()
	phone := r.openPhoneRequest(t, "pixel")

	as, err := r.approver.Look(ctx, phone.ref)
	if err != nil {
		t.Fatal(err)
	}

	ff := &stepFailFabric{inner: r.fc}
	fd := &stepFailDir{Client: r.dir}
	approver := r.approver
	approver.Fabric = ff
	approver.Dir = fd

	// 1. Stop after step 1 (PutEnrollment succeeds, step 2 RegisterClient fails).
	ff.failRegister = true
	if err := approver.Approve(ctx, as); !errors.Is(err, fabricclient.ErrUnavailable) {
		t.Fatalf("stop after step 1 err = %v, want ErrUnavailable", err)
	}
	rows, err := r.fc.ListEnrollments(ctx)
	if err != nil || len(rows) != 1 || rows[0].Enroll.Ver != 1 {
		t.Fatalf("after step 1 enrollments = %+v (%v), want 1 row at ver=1", rows, err)
	}
	listing, err := r.dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, registered := listing.Devices[phone.device]; registered {
		t.Fatal("phone should not be registered when step 2 failed")
	}

	// 2. Retry and stop after step 2 (PutEnrollment and RegisterClient succeed, step 3 ApproveRequest fails).
	ff.failRegister = false
	fd.failApprove = true
	if err := approver.Approve(ctx, as); !errors.Is(err, directory.ErrUnreachable) {
		t.Fatalf("stop after step 2 err = %v, want ErrUnreachable", err)
	}
	rows, err = r.fc.ListEnrollments(ctx)
	if err != nil || len(rows) != 1 || rows[0].Enroll.Ver != 1 {
		t.Fatalf("after step 2 retry enrollments = %+v (%v), want ver=1 (not bumped)", rows, err)
	}
	listing, err = r.dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dev, ok := listing.Devices[phone.device]; !ok || dev.Kind != clientproto.ClientLinkClass || dev.Revoked {
		t.Fatalf("after step 2 device row = %+v (ok=%v)", dev, ok)
	}
	pendingReq, err := r.approver.Requests.GetRequest(ctx, phone.ref.Code, 0)
	if err != nil || pendingReq.State != directory.RequestPending {
		t.Fatalf("after step 2 request state = %+v (%v), want pending", pendingReq, err)
	}

	// 3. Retry and fail after step 3 already committed on the relay (simulating a lost reply on step 3).
	fd.failApprove = false
	fd.failAfterDecision = true
	if err := approver.Approve(ctx, as); !errors.Is(err, directory.ErrUnreachable) {
		t.Fatalf("stop after step 3 commit err = %v, want ErrUnreachable", err)
	}

	// 4. Final retry completes cleanly with no error, no duplicate row, and enrollment ver still 1.
	fd.failAfterDecision = false
	if err := approver.Approve(ctx, as); err != nil {
		t.Fatalf("final retry Approve: %v", err)
	}

	rows, err = r.fc.ListEnrollments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Device != phone.device || rows[0].Enroll.Ver != 1 {
		t.Fatalf("final enrollments = %+v, want 1 row for %s at ver=1", rows, phone.device)
	}
	listing, err = r.dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clientCount := 0
	for id, dev := range listing.Devices {
		if id == phone.device {
			clientCount++
			if dev.Kind != clientproto.ClientLinkClass || dev.Revoked {
				t.Fatalf("final phone directory row = %+v", dev)
			}
		}
	}
	if clientCount != 1 {
		t.Fatalf("clientCount = %d, want 1", clientCount)
	}
	decidedReq, err := r.approver.Requests.GetRequest(ctx, phone.ref.Code, 0)
	if err != nil || decidedReq.State != directory.RequestApproved || decidedReq.Grant == nil {
		t.Fatalf("final decided request = %+v (%v)", decidedReq, err)
	}
}

// TestP12ClientGrantAuthenticatedByLinkKey verifies that the sealed grant opens
// only under the link key and satisfies the phone-side grant.Check validation (contract P12).
func TestP12ClientGrantAuthenticatedByLinkKey(t *testing.T) {
	r := newClientPairRig(t)
	ctx := context.Background()
	phone := r.openPhoneRequest(t, "iphone")

	as, err := r.approver.Look(ctx, phone.ref)
	if err != nil {
		t.Fatal(err)
	}

	// An approver without a Card callback refuses before writing anything.
	noCardApprover := r.approver
	noCardApprover.Card = nil
	if err := noCardApprover.Approve(ctx, as); !errors.Is(err, ErrNoExecutorCard) {
		t.Fatalf("Approve without Card = %v, want ErrNoExecutorCard", err)
	}

	if err := r.approver.Approve(ctx, as); err != nil {
		t.Fatalf("Approve phone: %v", err)
	}
	decided, err := r.approver.Requests.GetRequest(ctx, phone.ref.Code, 0)
	if err != nil || decided.State != directory.RequestApproved || decided.Grant == nil {
		t.Fatalf("decided request = %+v (%v)", decided, err)
	}
	sealed, err := b64u.DecodeString(*decided.Grant)
	if err != nil {
		t.Fatal(err)
	}

	// Wrong link key refuses to open.
	wrongKey, err := NewLinkKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientproto.OpenGrant(sealed, wrongKey); !errors.Is(err, clientproto.ErrGrant) {
		t.Fatalf("OpenGrant with wrong key = %v, want ErrGrant", err)
	}

	// Tampered byte refuses to open.
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := clientproto.OpenGrant(tampered, phone.linkKey); !errors.Is(err, clientproto.ErrGrant) {
		t.Fatalf("OpenGrant with tampered byte = %v, want ErrGrant", err)
	}

	// Opens under the true link key and passes phone-side grant.Check.
	grant, err := clientproto.OpenGrant(sealed, phone.linkKey)
	if err != nil {
		t.Fatalf("OpenGrant with link key: %v", err)
	}
	if err := grant.Check(phone.device, phone.ref.Code, phone.signPub); err != nil {
		t.Fatalf("grant.Check valid: %v", err)
	}
	if grant.Relay != r.srv.URL {
		t.Fatalf("grant.Relay = %q, want trailing slash trimmed %q", grant.Relay, r.srv.URL)
	}
	if len(grant.Executors) != 1 || grant.Executors[0].Writer != r.dev.ID() {
		t.Fatalf("grant.Executors = %+v, want approving computer's card", grant.Executors)
	}
	if name, err := directory.OpenName(directory.MetadataKey(r.id.CellKey()), mustListingDevice(t, r, phone.device).Name); err != nil || name != "iphone" {
		t.Fatalf("registered client name = %q (%v), want iphone", name, err)
	}

	// Phone-side check refuses a grant bound to a different code or a different phone.
	if err := grant.Check(phone.device, "zzzzzzzzzz", phone.signPub); !errors.Is(err, clientproto.ErrGrant) {
		t.Fatalf("grant.Check wrong code = %v, want ErrGrant", err)
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := grant.Check(clientproto.DeviceIDOf(otherPub), phone.ref.Code, otherPub); !errors.Is(err, clientproto.ErrGrant) {
		t.Fatalf("grant.Check wrong phone = %v, want ErrGrant", err)
	}
}

func mustListingDevice(t *testing.T, r *clientPairRig, id string) directory.Device {
	t.Helper()
	l, err := r.dir.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d, ok := l.Devices[id]
	if !ok {
		t.Fatalf("device %s not found in listing", id)
	}
	return d
}

// TestSetupLinkAndParseSetupLink verifies round-trip agreement across paths,
// ports, IPv6 hosts and percent-encoded characters, and verifies every refusal
// required by design 3.8b.
func TestSetupLinkAndParseSetupLink(t *testing.T) {
	valid := []struct {
		in   string
		want string
	}{
		{"https://codeaf.agentfield.ai/fabric", "https://codeaf.agentfield.ai/fabric"},
		{"https://codeaf.agentfield.ai/fabric/", "https://codeaf.agentfield.ai/fabric"},
		{"http://127.0.0.1:8787", "http://127.0.0.1:8787"},
		{"http://127.0.0.1:8787/", "http://127.0.0.1:8787"},
		{"http://[::1]:8787/fabric", "http://[::1]:8787/fabric"},
		{"https://[2001:db8::1]:8443/sub/path/", "https://[2001:db8::1]:8443/sub/path"},
		{"https://sync.example.com/team%20space/a+b~c", "https://sync.example.com/team%20space/a+b~c"},
		{"https://sync.example.com/path(1)/sub=2&more", "https://sync.example.com/path(1)/sub=2&more"},
	}
	for _, tc := range valid {
		link := SetupLink(tc.in)
		if !strings.HasPrefix(link, "workaf://setup?v=1&sync=") {
			t.Fatalf("SetupLink(%q) = %q, missing prefix", tc.in, link)
		}
		got, err := ParseSetupLink(link)
		if err != nil {
			t.Fatalf("ParseSetupLink(SetupLink(%q)) [%s] unexpected error: %v", tc.in, link, err)
		}
		if got != tc.want {
			t.Fatalf("ParseSetupLink(SetupLink(%q)) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Trailing slash inside a raw setup link is also trimmed by ParseSetupLink.
	got, err := ParseSetupLink("workaf://setup?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric%2F")
	if err != nil || got != "https://codeaf.agentfield.ai/fabric" {
		t.Fatalf("ParseSetupLink trailing slash = %q (%v)", got, err)
	}

	refused := []struct {
		name string
		link string
	}{
		{"another scheme for sync (ftp)", "workaf://setup?v=1&sync=ftp%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
		{"another scheme for sync (ws)", "workaf://setup?v=1&sync=ws%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
		{"relative sync URL", "workaf://setup?v=1&sync=%2Ffabric"},
		{"sync without host", "workaf://setup?v=1&sync=https%3A%2F%2F"},
		{"sync with port only", "workaf://setup?v=1&sync=https%3A%2F%2F%3A8787"},
		{"user info in sync", "workaf://setup?v=1&sync=https%3A%2F%2Fuser%3Apass%40codeaf.agentfield.ai%2Ffabric"},
		{"username only in sync", "workaf://setup?v=1&sync=https%3A%2F%2Fuser%40codeaf.agentfield.ai%2Ffabric"},
		{"empty user info in sync", "workaf://setup?v=1&sync=https%3A%2F%2F%40codeaf.agentfield.ai%2Ffabric"},
		{"query in sync", "workaf://setup?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric%3Fx%3D1"},
		{"empty query in sync", "workaf://setup?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric%3F"},
		{"fragment in sync", "workaf://setup?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric%23frag"},
		{"empty fragment in sync", "workaf://setup?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric%23"},
		{"outer fragment", "workaf://setup?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric#frag"},
		{"missing v", "workaf://setup?sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
		{"v=2", "workaf://setup?v=2&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
		{"v empty", "workaf://setup?v=&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
		{"extra key", "workaf://setup?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric&via=other"},
		{"empty sync", "workaf://setup?v=1&sync="},
		{"missing sync", "workaf://setup?v=1"},
		{"duplicate v", "workaf://setup?v=1&v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
		{"duplicate sync", "workaf://setup?v=1&sync=https%3A%2F%2Fa.example.com&sync=https%3A%2F%2Fb.example.com"},
		{"duplicate v with two parts", "workaf://setup?v=1&v=1"},
		{"duplicate sync with two parts", "workaf://setup?sync=https%3A%2F%2Fa.example.com&sync=https%3A%2F%2Fb.example.com"},
		{"wrong outer scheme", "codeaf://setup?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
		{"wrong outer host", "workaf://pair?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
		{"outer path present", "workaf://setup/more?v=1&sync=https%3A%2F%2Fcodeaf.agentfield.ai%2Ffabric"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSetupLink(tc.link)
			if !errors.Is(err, ErrSetupLinkShape) {
				t.Fatalf("ParseSetupLink(%q) = %q, err = %v; want ErrSetupLinkShape", tc.link, got, err)
			}
			if err.Error() != "that is not a codeaf setup link" {
				t.Fatalf("unexpected error text: %q", err.Error())
			}
		})
	}
}

// TestP13ApprovingAPhoneNeverRunsTheAfterApproveHook proves that approving a phone does
// not run the hook that refreshes what this computer knows about the account it shares
// with another computer. A phone is not another computer, and on a computer that has
// never been paired the hook would be its first request to the shared storage.
func TestP13ApprovingAPhoneNeverRunsTheAfterApproveHook(t *testing.T) {
	r := newClientPairRig(t)
	ctx := context.Background()
	var ran int
	r.approver.AfterApprove = func(context.Context) { ran++ }

	phone := r.openPhoneRequest(t, "phone")
	as, err := r.approver.Look(ctx, phone.ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.approver.ApproveClient(ctx, as); err != nil {
		t.Fatalf("ApproveClient: %v", err)
	}
	if ran != 0 {
		t.Fatalf("AfterApprove ran %d times when approving a phone, want 0", ran)
	}
}
