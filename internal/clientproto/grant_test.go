package clientproto

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// grantFixture is a phone, an owner identity, one computer card and a grant for the phone's request.
type grantFixture struct {
	link      []byte
	phoneSign ed25519.PublicKey
	device    string
	code      string
	grant     ClientGrant
}

func newGrantFixture(t *testing.T) grantFixture {
	t.Helper()
	id := testIdentity(t, 1)
	_, card := testCard(t, id, 0x31, "desk", 1700000000500)
	phoneSign := ed25519.NewKeyFromSeed(seed(2)).Public().(ed25519.PublicKey)
	cert := identity.IssueCert(id, phoneSign, time.UnixMilli(1700000000000))
	f := grantFixture{link: bytes.Repeat([]byte{0x05}, ClientLinkKeyBytes), phoneSign: phoneSign, device: cert.DeviceID(), code: "k7m2q9xd4a"}
	f.grant = ClientGrant{V: GrantVersion, Device: f.device, Code: f.code, Cert: cert, IdentityPub: id.PublicKey(),
		Relay: "https://relay.example/fabric", Executors: []Card{card}}
	return f
}

func (f grantFixture) seal(t *testing.T, g ClientGrant, link []byte) []byte {
	t.Helper()
	nonce, err := NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealGrant(g, nonce, link)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

// P12: a grant opens under the link key of its own request, says what the approver put in it, and passes the phone's check.
func TestGrantOpensUnderItsLinkKeyAndPassesThePhonesCheck(t *testing.T) {
	f := newGrantFixture(t)
	got, err := OpenGrant(f.seal(t, f.grant, f.link), f.link)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Check(f.device, f.code, f.phoneSign); err != nil {
		t.Fatalf("a good grant must pass: %v", err)
	}
	if got.Relay != f.grant.Relay || len(got.VerifiedExecutors()) != 1 || got.VerifiedExecutors()[0].Writer != f.grant.Executors[0].Writer {
		t.Fatalf("the grant lost what was put in it: %+v", got)
	}
}

// P12: a relay that does not hold the link key cannot make a grant: not by sealing one under another key,
// and not by changing any byte of a real one (the relay stores and serves the grant it was given).
func TestGrantCannotBeForgedOrChangedWithoutTheLinkKey(t *testing.T) {
	f := newGrantFixture(t)
	sealed := f.seal(t, f.grant, f.link)
	evil := f.grant
	evil.Relay = "https://evil.example"
	forged := f.seal(t, evil, bytes.Repeat([]byte{0x66}, ClientLinkKeyBytes))
	if _, err := OpenGrant(forged, f.link); err == nil {
		t.Fatal("a grant sealed under another key must not open")
	}
	for _, i := range []int{0, NonceSize, len(sealed) / 2, len(sealed) - 1} {
		c := append([]byte(nil), sealed...)
		c[i] ^= 1
		if _, err := OpenGrant(c, f.link); err == nil {
			t.Errorf("a grant with byte %d changed must not open", i)
		}
	}
	if _, err := OpenGrant(sealed[:NonceSize+10], f.link); err == nil {
		t.Fatal("a truncated grant must not open")
	}
	if _, err := OpenGrant(sealed, f.link[:8]); err == nil {
		t.Fatal("a short link key must not open anything")
	}
}

// P12: a grant that opened but is not for this request or this phone is refused: the relay can replay a grant it
// really saw, but only under a link key it does not have, so the in-seal names are the second line.
func TestGrantMustAnswerThePhonesOwnRequest(t *testing.T) {
	f := newGrantFixture(t)
	other := ed25519.NewKeyFromSeed(seed(7)).Public().(ed25519.PublicKey)
	cases := map[string]func(*ClientGrant){
		"another phone's device id":       func(g *ClientGrant) { g.Device = DeviceIDOf(other) },
		"another request's code":          func(g *ClientGrant) { g.Code = "zzzzzzzzzz" },
		"a certificate for another phone": func(g *ClientGrant) { g.Cert = identity.IssueCert(testIdentity(t, 1), other, time.UnixMilli(1)) },
		"a certificate from another identity": func(g *ClientGrant) {
			g.Cert = identity.IssueCert(testIdentity(t, 9), f.phoneSign, time.UnixMilli(1))
		},
		"no relay": func(g *ClientGrant) { g.Relay = "" },
		"no cards": func(g *ClientGrant) { g.Executors = nil },
		"only a card from another identity": func(g *ClientGrant) {
			_, c := testCard(t, testIdentity(t, 9), 0x31, "stranger", 1)
			g.Executors = []Card{c}
		},
		"a version other than two": func(g *ClientGrant) { g.V = 1 },
		"a short identity key":     func(g *ClientGrant) { g.IdentityPub = g.IdentityPub[:31] },
	}
	for name, change := range cases {
		g := f.grant
		change(&g)
		if g.Check(f.device, f.code, f.phoneSign) == nil {
			t.Errorf("a grant with %s must be refused", name)
		}
	}
	g := f.grant
	if g.Check(DeviceIDOf(other), f.code, other) == nil {
		t.Error("a grant opened by another phone must be refused")
	}
}

// P12: a card in the grant that does not verify is dropped, not trusted; the grant stands while one verifies.
func TestGrantKeepsOnlyCardsThatVerify(t *testing.T) {
	f := newGrantFixture(t)
	bad := f.grant.Executors[0]
	bad.Name = "tampered"
	g := f.grant
	g.Executors = []Card{bad, f.grant.Executors[0]}
	if err := g.Check(f.device, f.code, f.phoneSign); err != nil {
		t.Fatal(err)
	}
	if len(g.VerifiedExecutors()) != 1 || g.VerifiedExecutors()[0].Name != "desk" {
		t.Fatalf("only the card that verifies may be kept: %+v", g.VerifiedExecutors())
	}
	g.Executors = []Card{bad}
	if g.Check(f.device, f.code, f.phoneSign) == nil {
		t.Fatal("a grant whose every card fails must be refused")
	}
	g.Executors = make([]Card, MaxGrantExecutors+1)
	for i := range g.Executors {
		g.Executors[i] = f.grant.Executors[0]
	}
	if g.Check(f.device, f.code, f.phoneSign) == nil {
		t.Fatal("a grant with more cards than the limit must be refused")
	}
}

// P3: the grant carries no seed, no dedup secret, no cell key and no private key of any kind: decode it and read its keys.
func TestGrantCarriesNoSecretOfTheIdentity(t *testing.T) {
	f := newGrantFixture(t)
	id := testIdentity(t, 1)
	raw, err := json.Marshal(f.grant)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	want := []string{"cert", "code", "device", "executors", "identity_pub", "relay", "v"}
	if len(keys) != len(want) {
		t.Fatalf("the grant has keys %v, want exactly %v", keys, want)
	}
	for _, k := range want {
		if _, ok := keys[k]; !ok {
			t.Errorf("the grant lacks %q", k)
		}
	}
	// testIdentity makes every secret from one repeated byte, so the seed is known without an accessor.
	secrets := [][]byte{id.CellKey(), id.DedupSecret(), seed(1)}
	for _, secret := range secrets {
		for _, spelling := range []string{hex.EncodeToString(secret), base64.StdEncoding.EncodeToString(secret), base64.RawURLEncoding.EncodeToString(secret)} {
			if strings.Contains(string(raw), spelling) {
				t.Fatalf("the grant contains a secret of the identity (%s)", spelling)
			}
		}
	}
}

// The grant must fit the relay's own bound for a stored grant, which is a different package's number.
func TestGrantBoundEqualsTheRelaysJoinGrantBound(t *testing.T) {
	if MaxGrantBytes != clusterstate.MaxJoinGrant {
		t.Fatalf("MaxGrantBytes %d != clusterstate.MaxJoinGrant %d", MaxGrantBytes, clusterstate.MaxJoinGrant)
	}
	f := newGrantFixture(t)
	if sealed := f.seal(t, f.grant, f.link); len(sealed) > MaxGrantBytes/2 {
		t.Fatalf("a grant with one card takes %d bytes of %d: room for %d cards is gone", len(sealed), MaxGrantBytes, MaxGrantExecutors)
	}
	g := f.grant
	g.Executors = make([]Card, MaxGrantExecutors)
	for i := range g.Executors {
		g.Executors[i] = f.grant.Executors[0]
	}
	nonce, _ := NewNonce()
	if sealed, err := SealGrant(g, nonce, f.link); err != nil || len(sealed) > MaxGrantBytes {
		t.Fatalf("the most cards a grant may carry must fit the bound: %d bytes, %v", len(sealed), err)
	}
}
