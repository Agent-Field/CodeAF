package directorytest

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// PairingRig is one fresh relay as link pairing sees it: the open side a new
// device calls, and the signed Client of an already-paired device. Both share
// one request store and the clock, so a case can wait out a request's life.
type PairingRig struct {
	Clock    Clock
	Devices  Devices
	Requests directory.Requests
}

// PairingFactory builds a fresh PairingRig for one test.
type PairingFactory func(t *testing.T) PairingRig

// RunPairing runs every link-pairing case (docs/ux-pairing-contract.md) against
// a fresh rig each. The Go store, its HTTP wire and the Worker all answer to it.
func RunPairing(t *testing.T, factory PairingFactory) {
	for name, fn := range pairingCases {
		t.Run(name, func(t *testing.T) { fn(t, factory(t)) })
	}
}

var pairingCases = map[string]func(*testing.T, PairingRig){
	"CreateAnswersCodeCheckAndExpiry": createAnswers,
	"CreateRefusesMalformed":          createRefusesMalformed,
	"CreateKeepsUnknownPlatformOther": createPlatformOther,
	"GetShowsPendingRecord":           getShowsPending,
	"GetUnknownIsGone":                getUnknownIsGone,
	"GetIsCaseInsensitive":            getIsCaseInsensitive,
	"ApproveWritesDeviceAndGrant":     approveWritesDevice,
	"ApproveIsIdempotent":             approveIsIdempotent,
	"ApproveOtherBodyIsDecided":       approveOtherBodyIsDecided,
	"ApproveRefusesForeignCert":       approveRefusesForeignCert,
	"ApproveRefusesBigGrant":          approveRefusesBigGrant,
	"DenyThenApproveIsDecided":        denyThenApproveIsDecided,
	"DenyIsIdempotent":                denyIsIdempotent,
	"ApproveUnknownIsGone":            approveUnknownIsGone,
	"RevokedApproverIsRefused":        revokedApproverIsRefused,
	"RequestExpiresAfterTenMinutes":   requestExpires,
	"DecidedRequestIsKeptTwoMinutes":  decidedIsKeptTwoMinutes,
	"WaitWakesOnDecision":             waitWakes,
	"WaitEndsWhilePending":            waitEnds,
	"ThreePendingPerNetwork":          threePending,
	"TenCreatesPerHour":               tenPerHour,
	"PutDeviceIgnoresRelayFields":     putDeviceIgnoresRelayFields,
}

// joiner is a new device's keys and the request it makes.
type joiner struct {
	pub  []byte
	req  directory.NewRequest
	cert string
	id   string
}

func newJoiner(t *testing.T) joiner {
	t.Helper()
	pub, box, name := random(t, 32), random(t, 32), random(t, 40+8)
	cert := identity.Cert{V: 1, Device: hex.EncodeToString(pub), Made: 1, Sig: "00"}
	raw, err := json.Marshal(cert)
	must(t, err)
	return joiner{
		pub: pub, id: cert.DeviceID(), cert: b64(raw),
		req: directory.NewRequest{Pubkey: b64(pub), X25519: b64(box), NameSealed: b64(name), Platform: "linux"},
	}
}

func random(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	must(t, err)
	return b
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (j joiner) approval(grant string) directory.Approval {
	return directory.Approval{
		Device: directory.Device{V: 1, Name: "bg", AddedBy: "id_x", Platform: "linux", Created: 1, LastSeen: 1},
		Cert:   j.cert, Grant: grant,
	}
}

// ask makes j's request and answers its code.
func ask(t *testing.T, r PairingRig, j joiner) directory.Opened {
	t.Helper()
	o, err := r.Requests.CreateRequest(ctx, j.req)
	must(t, err)
	return o
}

func createAnswers(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	before := r.Clock.Now().UnixMilli()
	o := ask(t, r, j)
	if !regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{8}$`).MatchString(o.Code) {
		t.Fatalf("code %q is not 8 Crockford characters", o.Code)
	}
	if o.Device != j.id || len(o.Check) != 4 || o.Check != directory.CheckOf(j.pub) {
		t.Fatalf("answer = %+v, want device %s and check %s", o, j.id, directory.CheckOf(j.pub))
	}
	if o.ExpiresAt-o.RequestedAt != 600_000 || o.RequestedAt < before {
		t.Fatalf("times = %d..%d, now %d", o.RequestedAt, o.ExpiresAt, before)
	}
}

func createRefusesMalformed(t *testing.T, r PairingRig) {
	short := newJoiner(t)
	short.req.Pubkey = b64(random(t, 31))
	_, err := r.Requests.CreateRequest(ctx, short.req)
	wantErr(t, err, directory.ErrBadRequest)

	long := newJoiner(t)
	long.req.NameSealed = b64(random(t, 24+16+97))
	_, err = r.Requests.CreateRequest(ctx, long.req)
	wantErr(t, err, directory.ErrBadRequest)
}

func createPlatformOther(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	j.req.Platform = "plan9"
	got, err := r.Requests.GetRequest(ctx, ask(t, r, j).Code, 0)
	must(t, err)
	if got.Platform != "other" {
		t.Fatalf("platform = %q, want other", got.Platform)
	}
}

func getShowsPending(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	o := ask(t, r, j)
	got, err := r.Requests.GetRequest(ctx, o.Code, 0)
	must(t, err)
	want := directory.Request{
		Code: o.Code, Device: j.id, Pubkey: j.req.Pubkey, X25519: j.req.X25519, NameSealed: j.req.NameSealed,
		Platform: "linux", Check: o.Check, RequestedAt: o.RequestedAt, ExpiresAt: o.ExpiresAt, State: directory.RequestPending,
	}
	if got != want {
		t.Fatalf("record = %+v\nwant     %+v", got, want)
	}
}

func getUnknownIsGone(t *testing.T, r PairingRig) {
	_, err := r.Requests.GetRequest(ctx, "zzzzzzzz", 0)
	wantErr(t, err, directory.ErrRequestGone)
}

func getIsCaseInsensitive(t *testing.T, r PairingRig) {
	o := ask(t, r, newJoiner(t))
	_, err := r.Requests.GetRequest(ctx, upper(o.Code), 0)
	must(t, err)
}

func upper(s string) string {
	return string([]byte(regexp.MustCompile(`[a-z]`).ReplaceAllFunc([]byte(s), func(b []byte) []byte { return []byte{b[0] - 32} })))
}

func approveWritesDevice(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	o := ask(t, r, j)
	must(t, r.Devices(devA).ApproveRequest(ctx, o.Code, j.approval("Z3JhbnQ")))

	got, err := r.Requests.GetRequest(ctx, o.Code, 0)
	must(t, err)
	if got.State != directory.RequestApproved || got.Grant == nil || *got.Grant != "Z3JhbnQ" {
		t.Fatalf("after approve = %+v", got)
	}
	l, err := r.Devices(devA).List(ctx)
	must(t, err)
	d, ok := l.Devices[j.id]
	if !ok || d.Platform != "linux" || d.Created != l.Now || d.LastSeen != 0 {
		t.Fatalf("device %s = %+v (listed at %d), want platform linux, created by the directory, no last_seen", j.id, d, l.Now)
	}
}

func approveIsIdempotent(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	o := ask(t, r, j)
	must(t, r.Devices(devA).ApproveRequest(ctx, o.Code, j.approval("Z3JhbnQ")))
	must(t, r.Devices(devB).ApproveRequest(ctx, o.Code, j.approval("Z3JhbnQ")))
}

func approveOtherBodyIsDecided(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	o := ask(t, r, j)
	must(t, r.Devices(devA).ApproveRequest(ctx, o.Code, j.approval("Z3JhbnQ")))
	wantErr(t, r.Devices(devB).ApproveRequest(ctx, o.Code, j.approval("b3RoZXI")), directory.ErrAlreadyDecided)
	wantErr(t, r.Devices(devB).DenyRequest(ctx, o.Code), directory.ErrAlreadyDecided)
}

func approveRefusesForeignCert(t *testing.T, r PairingRig) {
	j, other := newJoiner(t), newJoiner(t)
	o := ask(t, r, j)
	wantErr(t, r.Devices(devA).ApproveRequest(ctx, o.Code, other.approval("Z3JhbnQ")), directory.ErrBadRequest)
	wantErr(t, r.Devices(devA).ApproveRequest(ctx, o.Code, directory.Approval{Cert: "!", Grant: "Z3JhbnQ"}), directory.ErrBadRequest)
	got, err := r.Requests.GetRequest(ctx, o.Code, 0)
	must(t, err)
	if got.State != directory.RequestPending {
		t.Fatalf("a refused approve left the request %s", got.State)
	}
}

func approveRefusesBigGrant(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	o := ask(t, r, j)
	big := b64(random(t, directory.MaxGrant+1))
	wantErr(t, r.Devices(devA).ApproveRequest(ctx, o.Code, j.approval(big)), directory.ErrTooBig)
}

func denyThenApproveIsDecided(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	o := ask(t, r, j)
	must(t, r.Devices(devA).DenyRequest(ctx, o.Code))
	got, err := r.Requests.GetRequest(ctx, o.Code, 0)
	must(t, err)
	if got.State != directory.RequestDenied || got.Grant != nil {
		t.Fatalf("after deny = %+v", got)
	}
	wantErr(t, r.Devices(devB).ApproveRequest(ctx, o.Code, j.approval("Z3JhbnQ")), directory.ErrAlreadyDecided)
}

func denyIsIdempotent(t *testing.T, r PairingRig) {
	o := ask(t, r, newJoiner(t))
	must(t, r.Devices(devA).DenyRequest(ctx, o.Code))
	must(t, r.Devices(devB).DenyRequest(ctx, o.Code))
}

func approveUnknownIsGone(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	wantErr(t, r.Devices(devA).ApproveRequest(ctx, "zzzzzzzz", j.approval("Z3JhbnQ")), directory.ErrRequestGone)
	wantErr(t, r.Devices(devA).DenyRequest(ctx, "zzzzzzzz"), directory.ErrRequestGone)
}

func revokedApproverIsRefused(t *testing.T, r PairingRig) {
	must(t, r.Devices(devA).PutDevice(ctx, devA, directory.Device{V: 1}))
	must(t, r.Devices(devB).PutDevice(ctx, devB, directory.Device{V: 1}))
	must(t, r.Devices(devA).Revoke(ctx, devB))
	j := newJoiner(t)
	o := ask(t, r, j)
	wantErr(t, r.Devices(devB).ApproveRequest(ctx, o.Code, j.approval("Z3JhbnQ")), directory.ErrRevoked)
	wantErr(t, r.Devices(devB).DenyRequest(ctx, o.Code), directory.ErrRevoked)
	must(t, r.Devices(devA).ApproveRequest(ctx, o.Code, j.approval("Z3JhbnQ")))
}

func requestExpires(t *testing.T, r PairingRig) {
	o := ask(t, r, newJoiner(t))
	r.Clock.Wait(10*time.Minute + time.Second)
	_, err := r.Requests.GetRequest(ctx, o.Code, 0)
	wantErr(t, err, directory.ErrRequestGone)
	wantErr(t, r.Devices(devA).DenyRequest(ctx, o.Code), directory.ErrRequestGone)
}

func decidedIsKeptTwoMinutes(t *testing.T, r PairingRig) {
	o := ask(t, r, newJoiner(t))
	must(t, r.Devices(devA).DenyRequest(ctx, o.Code))
	r.Clock.Wait(time.Minute + 50*time.Second)
	_, err := r.Requests.GetRequest(ctx, o.Code, 0)
	must(t, err)
	r.Clock.Wait(20 * time.Second)
	_, err = r.Requests.GetRequest(ctx, o.Code, 0)
	wantErr(t, err, directory.ErrRequestGone)
}

func waitWakes(t *testing.T, r PairingRig) {
	j := newJoiner(t)
	o := ask(t, r, j)
	got := make(chan error, 1)
	var rec directory.Request
	go func() {
		var err error
		rec, err = r.Requests.GetRequest(ctx, o.Code, 20*time.Second)
		got <- err
	}()
	time.Sleep(100 * time.Millisecond)
	must(t, r.Devices(devA).ApproveRequest(ctx, o.Code, j.approval("Z3JhbnQ")))
	select {
	case err := <-got:
		must(t, err)
		if rec.State != directory.RequestApproved {
			t.Fatalf("woke with %s", rec.State)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a decision did not wake the waiting read")
	}
}

func waitEnds(t *testing.T, r PairingRig) {
	o := ask(t, r, newJoiner(t))
	_, err := r.Requests.GetRequest(ctx, o.Code, 100*time.Millisecond)
	wantErr(t, err, directory.ErrStillPending)
}

func threePending(t *testing.T, r PairingRig) {
	for range 3 {
		ask(t, r, newJoiner(t))
	}
	_, err := r.Requests.CreateRequest(ctx, newJoiner(t).req)
	wantErr(t, err, wireauth.ErrRateLimited)
}

func tenPerHour(t *testing.T, r PairingRig) {
	for range 10 {
		must(t, r.Devices(devA).DenyRequest(ctx, ask(t, r, newJoiner(t)).Code))
	}
	_, err := r.Requests.CreateRequest(ctx, newJoiner(t).req)
	wantErr(t, err, wireauth.ErrRateLimited)
	if errors.Is(err, wireauth.ErrRateLimited) && wireauth.After(err) <= 0 {
		t.Fatal("a rate limit must say when to retry")
	}
	r.Clock.Wait(time.Hour + time.Second)
	ask(t, r, newJoiner(t))
}

func putDeviceIgnoresRelayFields(t *testing.T, r PairingRig) {
	c := r.Devices(devA)
	must(t, c.PutDevice(ctx, devA, directory.Device{V: 1, Name: "a", Created: 5, LastSeen: 5}))
	first, err := c.List(ctx)
	must(t, err)
	r.Clock.Wait(time.Minute)
	must(t, c.PutDevice(ctx, devA, directory.Device{V: 1, Name: "b", Created: 9, LastSeen: 9, Platform: "darwin"}))
	second, err := c.List(ctx)
	must(t, err)
	got := second.Devices[devA]
	if got.Created != first.Now || got.LastSeen != 0 || got.Name != "b" || got.Platform != "darwin" {
		t.Fatalf("device = %+v, want created %d, last_seen 0, new name and platform", got, first.Now)
	}
}
