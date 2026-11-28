package fabricclient_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/fabricclient"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

type testDevice struct {
	id  identity.Identity
	dev identity.Dev
}

func (d testDevice) IdentityKey() ed25519.PublicKey { return d.id.PublicKey() }
func (d testDevice) Cert() identity.Cert            { return d.dev.Cert }
func (d testDevice) Sign(msg []byte) []byte         { return d.dev.Sign(msg) }

type testRig struct {
	clock *directorytest.FakeClock
	svc   *relayserve.Service
	srv   *httptest.Server
	id    identity.Identity
	full  testDevice
	fc    *fabricclient.Client
	dir   *directory.HTTP
}

func newTestRig(t *testing.T) *testRig {
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
	srv := httptest.NewServer(svc.Handler)
	t.Cleanup(func() {
		srv.Close()
		_ = svc.Close()
	})

	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	full := testDevice{id: id, dev: dev}
	dir := directory.NewHTTP(srv.URL, reqsign.SignFor(full, clock.Now), srv.Client())
	sealed, err := directory.SealName(directory.MetadataKey(id.CellKey()), "desk")
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1, Name: sealed, AddedBy: id.ID()}); err != nil {
		t.Fatal(err)
	}

	fc := fabricclient.New(srv.URL, full, srv.Client())
	fc.Now = clock.Now
	return &testRig{
		clock: clock,
		svc:   svc,
		srv:   srv,
		id:    id,
		full:  full,
		fc:    fc,
		dir:   dir,
	}
}

func newPhone(t *testing.T, id identity.Identity, now time.Time) (ed25519.PublicKey, [32]byte, identity.Cert, testDevice) {
	t.Helper()
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	cert := identity.IssueCert(id, seed.Public(), now)
	dev, err := seed.Join(cert)
	if err != nil {
		t.Fatal(err)
	}
	var boxPub [32]byte
	copy(boxPub[:], bytes.Repeat([]byte{0x07}, 32))
	return seed.Public(), boxPub, cert, testDevice{id: id, dev: dev}
}

func signEnrollment(id identity.Identity, signPub ed25519.PublicKey, boxPub [32]byte, methods, reads []string, ver uint64) clientproto.Enrollment {
	e := clientproto.Enrollment{
		V:       1,
		Device:  clientproto.DeviceIDOf(signPub),
		SignPub: append([]byte(nil), signPub...),
		BoxPub:  append([]byte(nil), boxPub[:]...),
		Methods: methods,
		Reads:   reads,
		Ver:     ver,
	}
	if e.Methods == nil {
		e.Methods = []string{}
	}
	if e.Reads == nil {
		e.Reads = []string{}
	}
	e.Sig = id.Sign(e.SignText())
	return e
}

func TestFabricClientEnrollmentAndIdempotentRegistration(t *testing.T) {
	r := newTestRig(t)
	ctx := context.Background()

	// Empty list before any phone is enrolled.
	rows, err := r.fc.ListEnrollments(ctx)
	if err != nil {
		t.Fatalf("ListEnrollments empty: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListEnrollments = %+v, want empty", rows)
	}

	signPub, boxPub, cert, phoneDev := newPhone(t, r.id, r.clock.Now())
	e1 := signEnrollment(r.id, signPub, boxPub, clientproto.PhoneMethods(), clientproto.PhoneReads(), 1)

	// 1. PutEnrollment works before the client is registered and is idempotent on identical re-send.
	if err := r.fc.PutEnrollment(ctx, e1); err != nil {
		t.Fatalf("PutEnrollment v1: %v", err)
	}
	if err := r.fc.PutEnrollment(ctx, e1); err != nil {
		t.Fatalf("PutEnrollment v1 identical retry: %v", err)
	}

	// ListEnrollments returns the stored enrollment.
	rows, err = r.fc.ListEnrollments(ctx)
	if err != nil {
		t.Fatalf("ListEnrollments after v1: %v", err)
	}
	if len(rows) != 1 || rows[0].Device != e1.Device || !rows[0].Enroll.Same(e1) {
		t.Fatalf("ListEnrollments got %+v, want %+v", rows, e1)
	}

	// 2. RegisterClient registers the phone, and a retry after a crash returns ErrExists for the caller to treat as idempotent.
	sealedName, err := directory.SealName(directory.MetadataKey(r.id.CellKey()), "pixel")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.fc.RegisterClient(ctx, cert, e1.Methods, sealedName); err != nil {
		t.Fatalf("RegisterClient first call: %v", err)
	}
	err = r.fc.RegisterClient(ctx, cert, e1.Methods, sealedName)
	if !errors.Is(err, fabricclient.ErrExists) {
		t.Fatalf("RegisterClient second call err = %v, want errors.Is(..., ErrExists)", err)
	}
	assertReadableError(t, err)

	// Upgrading enrollment to v2 succeeds; resending v1 or a different statement at v2 fails with ErrStale.
	e2 := signEnrollment(r.id, signPub, boxPub, []string{clientproto.MethodSend}, clientproto.PhoneReads(), 2)
	if err := r.fc.PutEnrollment(ctx, e2); err != nil {
		t.Fatalf("PutEnrollment v2: %v", err)
	}
	if err := r.fc.PutEnrollment(ctx, e1); !errors.Is(err, fabricclient.ErrStale) {
		t.Fatalf("PutEnrollment stale v1 err = %v, want errors.Is(..., ErrStale)", err)
	} else {
		assertReadableError(t, err)
	}
	e2Diff := signEnrollment(r.id, signPub, boxPub, []string{clientproto.MethodCreate}, clientproto.PhoneReads(), 2)
	if err := r.fc.PutEnrollment(ctx, e2Diff); !errors.Is(err, fabricclient.ErrStale) {
		t.Fatalf("PutEnrollment conflicting v2 err = %v, want errors.Is(..., ErrStale)", err)
	}

	// A scoped client device cannot call full-device fabric routes.
	phoneClient := fabricclient.New(r.srv.URL, phoneDev, r.srv.Client())
	phoneClient.Now = r.clock.Now
	if _, err := phoneClient.ListEnrollments(ctx); !errors.Is(err, fabricclient.ErrUnauthorized) {
		t.Fatalf("phone ListEnrollments err = %v, want ErrUnauthorized", err)
	} else {
		assertReadableError(t, err)
	}
}

func TestFabricClientTypedErrorsAndNoRawCodes(t *testing.T) {
	r := newTestRig(t)
	ctx := context.Background()

	// Enrolling an existing full device returns ErrExists.
	fullPub, err := hex.DecodeString(r.full.dev.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	var boxPub [32]byte
	fullEnroll := signEnrollment(r.id, fullPub, boxPub, clientproto.PhoneMethods(), clientproto.PhoneReads(), 1)
	err = r.fc.PutEnrollment(ctx, fullEnroll)
	if !errors.Is(err, fabricclient.ErrExists) {
		t.Fatalf("PutEnrollment full device err = %v, want ErrExists", err)
	}
	assertReadableError(t, err)

	// Malformed enrollment signature returns ErrBadRequest without leaking "bad_request".
	signPub, boxPub, _, _ := newPhone(t, r.id, r.clock.Now())
	bad := signEnrollment(r.id, signPub, boxPub, clientproto.PhoneMethods(), clientproto.PhoneReads(), 1)
	bad.Sig[0] ^= 0xff
	err = r.fc.PutEnrollment(ctx, bad)
	if !errors.Is(err, fabricclient.ErrBadRequest) {
		t.Fatalf("PutEnrollment bad sig err = %v, want ErrBadRequest", err)
	}
	assertReadableError(t, err)

	// Exceeding MaxEnrollments (16) returns ErrFull on the 17th phone.
	for i := 0; i < clientproto.MaxEnrollments; i++ {
		pub, box, _, _ := newPhone(t, r.id, r.clock.Now())
		e := signEnrollment(r.id, pub, box, clientproto.PhoneMethods(), clientproto.PhoneReads(), 1)
		if err := r.fc.PutEnrollment(ctx, e); err != nil {
			t.Fatalf("PutEnrollment %d: %v", i, err)
		}
	}
	pub17, box17, _, _ := newPhone(t, r.id, r.clock.Now())
	e17 := signEnrollment(r.id, pub17, box17, clientproto.PhoneMethods(), clientproto.PhoneReads(), 1)
	err = r.fc.PutEnrollment(ctx, e17)
	if !errors.Is(err, fabricclient.ErrFull) {
		t.Fatalf("PutEnrollment 17th err = %v, want ErrFull", err)
	}
	assertReadableError(t, err)
}

func assertReadableError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	for _, forbidden := range []string{
		"bad_request", "not_found", "not_enrolled", "already_decided",
		"too_large", "too_big", "rate_limited", "relay_storage_full",
		"relay", "auditor", "verdict", "verified", "refuted",
	} {
		if strings.Contains(strings.ToLower(msg), forbidden) {
			t.Fatalf("error %q contains forbidden code or word %q", msg, forbidden)
		}
	}
}
