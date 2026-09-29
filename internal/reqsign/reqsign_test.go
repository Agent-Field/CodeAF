package reqsign

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

var t0 = time.UnixMilli(1759049990000)

// signer adapts a real identity and device to Signer.
type signer struct {
	id  identity.Identity
	dev identity.Dev
}

func (s signer) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s signer) Cert() identity.Cert            { return s.dev.Cert }
func (s signer) Sign(msg []byte) []byte         { return s.dev.Sign(msg) }

func newSigner(t *testing.T) signer {
	t.Helper()
	home := t.TempDir()
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	id, err := identity.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	return signer{id, dev}
}

func signed(t *testing.T, s Signer, url string, body string, at time.Time) *http.Request {
	t.Helper()
	r := httptest.NewRequest("PUT", url, strings.NewReader(body))
	Sign(r, []byte(body), s, at)
	return r
}

func TestSignVerifyRoundTrip(t *testing.T) {
	s := newSigner(t)
	r := signed(t, s, "/v1/blob?k=1", "hello", t0)
	id, dev, err := Verify(r, []byte("hello"), t0)
	if err != nil || id != s.id.ID() || dev != s.dev.ID() || id != IDOf(s.id.PublicKey()) {
		t.Fatalf("got %q %q %v", id, dev, err)
	}
}

func TestSignForAuthenticateAtRoundTrip(t *testing.T) {
	s := newSigner(t)
	now := func() time.Time { return t0 }
	r := httptest.NewRequest("POST", "/v1/dir", strings.NewReader("x"))
	SignFor(s, now)(r, []byte("x"))
	id, dev, err := AuthenticateAt(now)(r, []byte("x"))
	if err != nil || id != s.id.ID() || dev != s.dev.ID() {
		t.Fatalf("got %q %q %v", id, dev, err)
	}
}

func TestVerifyRefusesSkew(t *testing.T) {
	s := newSigner(t)
	r := signed(t, s, "/p", "b", t0)
	for _, off := range []time.Duration{6 * time.Minute, -6 * time.Minute} {
		_, _, err := Verify(r, []byte("b"), t0.Add(off))
		if !errors.Is(err, wireauth.ErrSkew) || !errors.Is(err, ErrSkew) || errors.Is(err, wireauth.ErrUnauthorized) {
			t.Fatalf("offset %v: %v", off, err)
		}
	}
	if _, _, err := Verify(r, []byte("b"), t0.Add(Skew)); err != nil {
		t.Fatalf("the edge of the window is allowed: %v", err)
	}
}

func TestVerifyRefusesForeignCert(t *testing.T) {
	s, other := newSigner(t), newSigner(t)
	r := signed(t, s, "/p", "b", t0)
	r.Header.Set(HeaderCert, signedBy(t, other).Header.Get(HeaderCert))
	expectRefused(t, r, "b", ErrBadCert)
}

func signedBy(t *testing.T, s Signer) *http.Request { return signed(t, s, "/p", "b", t0) }

func TestVerifyRefusesWrongDeviceKey(t *testing.T) {
	s := newSigner(t)
	// A key that is not the cert's device key signs, under a real cert.
	_, stranger, _ := ed25519.GenerateKey(nil)
	r := httptest.NewRequest("PUT", "/p", nil)
	Sign(r, nil, forged{s, stranger}, t0)
	expectRefused(t, r, "", ErrBadSignature)
}

// forged presents a real cert but signs with a key that is not the cert's.
type forged struct {
	Signer
	key ed25519.PrivateKey
}

func (f forged) Sign(msg []byte) []byte { return ed25519.Sign(f.key, msg) }

func TestVerifyRefusesTamperedBody(t *testing.T) {
	r := signed(t, newSigner(t), "/p", "body", t0)
	expectRefused(t, r, "bodz", ErrBadSignature)
}

func TestVerifyRefusesTamperedPathOrQuery(t *testing.T) {
	r := signed(t, newSigner(t), "/v1/a?k=1", "b", t0)
	for _, target := range []string{"/v1/b?k=1", "/v1/a?k=2", "/v1/a"} {
		moved := r.Clone(r.Context())
		moved.URL.Path, moved.URL.RawQuery, _ = strings.Cut(target, "?")
		expectRefused(t, moved, "b", ErrBadSignature)
	}
	moved := r.Clone(r.Context())
	moved.Method = "DELETE"
	expectRefused(t, moved, "b", ErrBadSignature)
}

func TestVerifyRefusesMissingHeaders(t *testing.T) {
	for _, h := range []string{HeaderIdentity, HeaderCert, HeaderTime, HeaderSig} {
		r := signed(t, newSigner(t), "/p", "b", t0)
		r.Header.Del(h)
		if _, _, err := Verify(r, []byte("b"), t0); !errors.Is(err, wireauth.ErrUnauthorized) {
			t.Fatalf("without %s: %v", h, err)
		}
	}
	expectRefused(t, httptest.NewRequest("GET", "/p", nil), "", ErrUnsigned)
}

func TestVerifyRefusesGarbageHeaders(t *testing.T) {
	for _, h := range []string{HeaderIdentity, HeaderCert, HeaderTime, HeaderSig} {
		r := signed(t, newSigner(t), "/p", "b", t0)
		r.Header.Set(h, "!!not-valid!!")
		if _, _, err := Verify(r, []byte("b"), t0); !errors.Is(err, wireauth.ErrUnauthorized) {
			t.Fatalf("garbage %s: %v", h, err)
		}
	}
}

func expectRefused(t *testing.T, r *http.Request, body string, want error) {
	t.Helper()
	_, _, err := Verify(r, []byte(body), t0)
	if !errors.Is(err, want) || !errors.Is(err, wireauth.ErrUnauthorized) {
		t.Fatalf("want %v (unauthorized), got %v", want, err)
	}
}
