package serve

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

const tokensDevicesPerPerson = tokens.DevicesPerPerson

// deviceSignIn is a password sign-in over TLS from remote presenting device,
// from a client that can keep a new one.
func deviceSignIn(o *pwOrg, u, p, remote, device string) *httptest.ResponseRecorder {
	return wantSignIn(o, u, p, remote, device, true)
}

func wantSignIn(o *pwOrg, u, p, remote, device string, want bool) *httptest.ResponseRecorder {
	data, _ := json.Marshal(api.PasswordSignIn{Username: u, Password: p, Device: device, WantDevice: want})
	r := httptest.NewRequest("POST", "/v1/auth/password", bytes.NewReader(data))
	r.RemoteAddr = remote
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	return w
}

// joinWithDevice signs username up through link and answers the device token
// the sign-up handed back.
func joinWithDevice(o *pwOrg, link, username, password string) (session, device string) {
	o.t.Helper()
	out, w := o.access(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: username, Password: password, WantDevice: true})
	if w.Code != 200 || out.State != "joined" || out.AccessToken == "" || out.DeviceToken == "" {
		o.t.Fatalf("sign up %s = %d %s", username, w.Code, w.Body)
	}
	return out.AccessToken, out.DeviceToken
}

// guess is one stranger's wrong guess at abir, from the i'th of forty
// addresses.
func guess(o *pwOrg, i int) {
	tlsSignIn(o, "abir", "attacker wrong guess", fmt.Sprintf("10.9.%d.%d:1", i%40, 1))
}

// drain is a burst of guesses that leaves abir's ceiling dry now.
func drain(o *pwOrg) {
	for i := 0; i < 5; i++ {
		guess(o, i)
	}
}

// restart is a fresh server on the same files: nothing in memory survives.
func restart(o *pwOrg) {
	o.h.Close()
	o.h = New(factory.Seam{Load: func() (factory.Snapshot, error) { return factory.Snapshot{}, nil }}, o.cfg)
	o.t.Cleanup(o.h.Close)
}

// M1: a stranger draining abir's username ceiling from forty addresses, an
// attack that began before abir ever joined, never keeps abir's own machine
// out, from any address. A machine abir never used waits; the operator's
// reset link lets abir in at once and trusts that machine from then on.
func TestAStrangerCannotKeepTheOwnerOut(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	invite := o.invite(operator)
	s := 0
	for ; s < 1800; s += 5 { // half an hour of guessing at a name nobody has yet
		guess(o, s/5)
		o.advance(5 * time.Second)
	}
	// An invitation is never throttled, and hands abir's machine its token.
	_, device := joinWithDevice(o, invite, "abir", pwAbir)
	locked, tries := 0, 0
	for ; s < 6*3600; s += 5 {
		guess(o, s/5)
		if s%60 == 0 {
			tries++
			if w := deviceSignIn(o, "abir", pwAbir, fmt.Sprintf("198.51.100.%d:1", tries%250+1), device); w.Code != 200 {
				locked++
			}
		}
		o.advance(5 * time.Second)
	}
	if locked != 0 {
		t.Fatalf("abir's own machine, from new addresses, refused %d/%d over 5.5h", locked, tries)
	}
	drain(o)
	if w := deviceSignIn(o, "abir", pwAbir, "203.0.113.50:1", ""); w.Code != 429 {
		t.Fatalf("a new machine during the attack = %d %s", w.Code, w.Body)
	}
	drain(o)
	if w := deviceSignIn(o, "abir", pwAbir, "203.0.113.51:1", "not a device token at all"); w.Code != 429 {
		t.Fatalf("a made-up device token during the attack = %d %s", w.Code, w.Body)
	}
	// The fallback: the operator's reset link, never throttled.
	w := o.do("POST", "/v1/access/links", operator, api.CreateAccessLink{Kind: "reset", Person: "abir"}, "")
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &made) != nil {
		t.Fatalf("reset link = %d %s", w.Code, w.Body)
	}
	drain(o)
	out, _ := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "reset", Password: "a fresh passphrase here", WantDevice: true})
	if out.State != "joined" || out.AccessToken == "" || out.DeviceToken == "" {
		t.Fatalf("reset during the attack = %+v", out)
	}
	drain(o)
	if w := deviceSignIn(o, "abir", "a fresh passphrase here", "203.0.113.52:1", out.DeviceToken); w.Code != 200 {
		t.Fatalf("the new machine after its reset = %d %s", w.Code, w.Body)
	}
	// The reset changed the password: the old machine's token is no longer
	// trusted.
	drain(o)
	if w := deviceSignIn(o, "abir", "a fresh passphrase here", "203.0.113.53:1", device); w.Code != 429 {
		t.Fatalf("a token from before the reset = %d %s", w.Code, w.Body)
	}
}

// M1: trust is kept on disk, so a restart in the middle of an attack does not
// turn abir's machine into a stranger.
func TestAKnownDeviceIsAdmittedAcrossARestart(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	_, device := joinWithDevice(o, o.invite(operator), "abir", pwAbir)
	restart(o)
	locked, tries := 0, 0
	for s := 0; s < 3*3600; s += 5 {
		guess(o, s/5)
		if s >= 1800 && s%60 == 0 {
			tries++
			if w := deviceSignIn(o, "abir", pwAbir, "192.0.2.7:1", device); w.Code != 200 {
				locked++
			}
		}
		if s == 5400 {
			restart(o)
		}
		o.advance(5 * time.Second)
	}
	if locked != 0 {
		t.Fatalf("abir's machine refused %d/%d across two restarts", locked, tries)
	}
}

// M1: a password change ends every device token of the old password; the
// changing machine is handed a new one.
func TestAPasswordChangeRevokesEveryDeviceToken(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	session, first := joinWithDevice(o, o.invite(operator), "abir", pwAbir)
	w := deviceSignIn(o, "abir", pwAbir, "192.0.2.8:1", "")
	var signed api.PasswordSignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil || signed.DeviceToken == "" {
		t.Fatalf("second machine = %d %s", w.Code, w.Body)
	}
	second := signed.DeviceToken
	if o.h.trustedDevice("abir", first) == "" || o.h.trustedDevice("abir", second) == "" {
		t.Fatal("the tokens were not trusted before the change")
	}
	if o.h.trustedDevice("santosh", first) != "" {
		t.Fatal("abir's token opened santosh's name")
	}
	w = o.do("POST", "/v1/me/password", session, api.PasswordChange{Current: pwAbir, New: "a brand new passphrase", WantDevice: true}, "")
	var changed api.PasswordChanged
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &changed) != nil || changed.DeviceToken == "" {
		t.Fatalf("change = %d %s", w.Code, w.Body)
	}
	if o.h.trustedDevice("abir", first) != "" || o.h.trustedDevice("abir", second) != "" {
		t.Fatal("a device token of the old password is still trusted")
	}
	if o.h.trustedDevice("abir", changed.DeviceToken) == "" {
		t.Fatal("the changing machine's new token is not trusted")
	}
	// Through the door: during an attack the old token reads as a new machine.
	for i := 0; i < userFailuresAll+20; i++ {
		guess(o, i)
	}
	drain(o)
	if w := deviceSignIn(o, "abir", "a brand new passphrase", "203.0.113.60:1", second); w.Code != 429 {
		t.Fatalf("a revoked token during the attack = %d %s", w.Code, w.Body)
	}
	if w := deviceSignIn(o, "abir", "a brand new passphrase", "203.0.113.61:1", changed.DeviceToken); w.Code != 200 {
		t.Fatalf("the new token during the attack = %d %s", w.Code, w.Body)
	}
}

// M-new: the session making a change is never signed out by it, however
// busy it is while the change lands.
func TestAChangeNeverSignsOutItsOwnSession(t *testing.T) {
	lost := 0
	for trial := 0; trial < 20; trial++ {
		o := newPWOrg(t)
		owner := o.signUp(o.operatorLink(), "santosh", pwOperator)
		stop := make(chan struct{})
		var refused sync.Map
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					if _, code := o.me(owner); code != 200 {
						refused.Store(code, true)
					}
				}
			}()
		}
		time.Sleep(20 * time.Millisecond)
		w := o.do("POST", "/v1/me/password", owner, api.PasswordChange{Current: pwOperator, New: "a brand new passphrase"}, "")
		close(stop)
		wg.Wait()
		_, code := o.me(owner)
		if w.Code != 200 {
			t.Fatalf("trial %d: change = %d %s", trial, w.Code, w.Body)
		}
		refused.Range(func(k, _ any) bool { t.Errorf("trial %d: a poll was answered %v", trial, k); return true })
		if code != 200 {
			lost++
		}
	}
	if lost != 0 {
		t.Fatalf("the changer's own session was signed out in %d/20 trials", lost)
	}
}

// L-a: a right password gives its tries back even when it is settled later
// than it was admitted, as it always is on a real clock.
func TestARightPasswordSettledLaterGivesItsTriesBack(t *testing.T) {
	g := newPasswordGuard()
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	admitted := 0
	// Sixty different people from one office, all right, in twelve seconds.
	for i := 0; i < 60; i++ {
		now := t0.Add(time.Duration(i) * 200 * time.Millisecond)
		a, ok := g.admit(fmt.Sprintf("person%02d", i), "203.0.113.5", "", now, true)
		if !ok {
			continue
		}
		admitted++
		a.succeeded(now.Add(80 * time.Millisecond))
	}
	if admitted != 60 {
		t.Fatalf("one office's sixty people admitted %d", admitted)
	}
	// An abandoned try is back to the full burst.
	a, _ := g.admit("x", "203.0.113.9", "", t0, true)
	a.abandon(t0.Add(80 * time.Millisecond))
	if got := g.addrs.keys["203.0.113.9"].tokens(t0.Add(80 * time.Millisecond)); got < addrFailures-1e-9 {
		t.Fatalf("address tries after an abandoned try = %.3f, want %d", got, addrFailures)
	}
	// A refund never lifts a key above its burst.
	for i := 0; i < 5; i++ {
		a, _ := g.admit("y", "203.0.113.10", "", t0, true)
		a.succeeded(t0.Add(time.Hour))
	}
	if got := g.addrs.keys["203.0.113.10"].tokens(t0.Add(time.Hour)); got > addrFailures {
		t.Fatalf("address tries after refunds = %.3f, above %d", got, addrFailures)
	}
	// A wrong password keeps its try spent.
	a, _ = g.admit("z", "203.0.113.11", "", t0, true)
	a.failed(t0.Add(80 * time.Millisecond))
	if got := g.addrs.keys["203.0.113.11"].tokens(t0.Add(80 * time.Millisecond)); got > addrFailures-1+0.01 {
		t.Fatalf("address tries after a wrong password = %.3f", got)
	}
}

// L-b: a full throttle refuses rather than forgets a key backing off, and
// says so in the log once a minute, not once a try.
func TestAFullThrottleRefusesAndSaysSoOnceAMinute(t *testing.T) {
	g := newPasswordGuard()
	var lines []string
	g.logf = func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	g.pairs.most = 1
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for i := 0; i < userFailures; i++ {
		a, ok := g.admit("u", "a", "", now, true)
		if !ok {
			t.Fatalf("try %d refused", i)
		}
		a.failed(now)
	}
	// Still backing off throughout, as a key a stranger keeps hitting is.
	g.pairs.keys["u\x00a"].until = now.Add(time.Hour)
	for s := 0; s < 125; s += 5 {
		if _, ok := g.admit("v", "b", "", now.Add(time.Duration(s)*time.Second), true); ok {
			t.Fatal("a full set of keys backing off made room")
		}
	}
	if len(lines) != 3 {
		t.Fatalf("log lines over 2 minutes = %d: %q", len(lines), lines)
	}
}

// L-c: a password of punctuation, or of a few characters repeated, is as
// guessable as any on the list.
func TestPunctuationAndRepeatedPasswordsAreRefused(t *testing.T) {
	for _, pw := range []string{"!@#$%^&*()_+", "!!!!!!!!!!!!", "............", "qwertyuiop[]", "aaaaaaaaaaaa",
		"abababababab", "1q2w3e4r5t6y", ")(*&^%$#@!~+", "1234567890-="} {
		if got := passwordRefusal(normalPassword(pw), "santosh"); got != reasonPasswordCommon {
			t.Errorf("%q = %q, want %q", pw, got, reasonPasswordCommon)
		}
	}
	for _, pw := range []string{pwOperator, pwAbir, "#Gx!9q&vT2*m", "~kettle.ROBIN.42~"} {
		if got := passwordRefusal(normalPassword(pw), "santosh"); got != "" {
			t.Errorf("%q = %q, want accepted", pw, got)
		}
	}
}

// L-d: when every hashing slot is busy a request waits a moment for one
// before it is told to try again.
func TestAHashWaitsForASlotThenGivesUp(t *testing.T) {
	for i := 0; i < hashSlotsMost; i++ {
		hashSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for len(hashSlots) > 0 {
			<-hashSlots
		}
	})
	// A slot freed while it waits is taken.
	go func() { time.Sleep(50 * time.Millisecond); <-hashSlots }()
	ran := false
	if err := withHashSlot(func() { ran = true }); err != nil || !ran {
		t.Fatalf("a slot freed meanwhile = %v, ran %v", err, ran)
	}
	// None freed: busy after the wait, not at once.
	hashSlots <- struct{}{}
	start := time.Now()
	if err := withHashSlot(func() { t.Fatal("ran without a slot") }); err != errHashBusy {
		t.Fatalf("all busy = %v", err)
	}
	if waited := time.Since(start); waited < hashSlotWait-10*time.Millisecond {
		t.Fatalf("gave up after %v, want about %v", waited, hashSlotWait)
	}
}

// A password person is signed in, but not with GitHub: /v1/me and the
// sign-in answer say github_signed_in false and sign_in_kind "password".
func TestAPasswordPersonIsNotSaidToHaveSignedInWithGitHub(t *testing.T) {
	o := newPWOrg(t)
	session := o.signUp(o.operatorLink(), "santosh", pwOperator)
	w := o.do("GET", "/v1/me", session, nil, "")
	signedIn := tlsSignIn(o, "santosh", pwOperator, "192.0.2.9:1")
	for name, body := range map[string]string{"/v1/me": w.Body.String(), "sign-in": signedIn.Body.String()} {
		var raw map[string]any
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			t.Fatalf("%s: %v %s", name, err, body)
		}
		if me, ok := raw["me"].(map[string]any); ok {
			raw = me
		}
		if raw["github_signed_in"] != false || raw["sign_in_kind"] != "password" {
			t.Errorf("%s: github_signed_in = %v, sign_in_kind = %v", name, raw["github_signed_in"], raw["sign_in_kind"])
		}
	}
}

// N1: only a client that asks is given a device token, and sign-ins on
// machines that keep nothing never push the owner's own device out: past
// DevicesPerPerson live ones a sign-in still succeeds but makes none, and a
// device in use is marked used at every sign-in.
func TestSignInsThatKeepNothingNeverEvictTheOwnersDevice(t *testing.T) {
	o := newPWOrg(t)
	g := o.h.guard()
	var lines []string
	g.logf = func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	if out, _ := o.access(api.FactoryAccessRequest{Link: o.invite(operator), Verb: "sign_up", Username: "mia", Password: pwAbir}); out.State != "joined" || out.DeviceToken != "" {
		t.Fatalf("a sign-up that did not ask = %+v", out)
	}
	_, device := joinWithDevice(o, o.invite(operator), "abir", pwAbir)
	signedIn := func(w *httptest.ResponseRecorder) api.PasswordSignedIn {
		t.Helper()
		var s api.PasswordSignedIn
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil {
			t.Fatalf("sign-in = %d %s", w.Code, w.Body)
		}
		return s
	}
	// Old clients and machines that keep nothing ask for none.
	for i := 0; i < 40; i++ {
		if s := signedIn(wantSignIn(o, "abir", pwAbir, fmt.Sprintf("198.51.100.%d:1", i+1), "", false)); s.DeviceToken != "" {
			t.Fatalf("sign-in %d that did not ask was given a device", i)
		}
		o.advance(time.Minute)
	}
	// The owner's device, used now and then.
	o.advance(48 * time.Hour)
	signedIn(deviceSignIn(o, "abir", pwAbir, "192.0.2.7:1", device))
	// Clients that do ask, many more than are kept, the owner's device the
	// least recently used of them all.
	minted := 0
	for i := 0; i < 3*tokensDevicesPerPerson; i++ {
		o.advance(time.Minute)
		if s := signedIn(wantSignIn(o, "abir", pwAbir, fmt.Sprintf("203.0.113.%d:1", i+1), "", true)); s.DeviceToken != "" {
			minted++
		}
	}
	if minted != tokensDevicesPerPerson-1 {
		t.Fatalf("devices made = %d, want %d (the owner's is the other)", minted, tokensDevicesPerPerson-1)
	}
	full := 0
	for _, l := range lines {
		if strings.Contains(l, "already holds") {
			full++
		}
	}
	if full != 1 {
		t.Fatalf("a full set of devices was said %d times: %q", full, lines)
	}
	// During a drain the owner's device is still admitted.
	for i := 0; i < userFailuresAll+20; i++ {
		guess(o, i)
	}
	if w := deviceSignIn(o, "abir", pwAbir, "192.0.2.8:1", device); w.Code != 200 {
		t.Fatalf("the owner's device after many sign-ins elsewhere, during a drain = %d %s", w.Code, w.Body)
	}
	// Used is the latest sign-in, not a day-old one.
	doc, _ := o.h.loadAccounts()
	for _, d := range doc.Devices {
		if d.ID == o.h.trustedDevice("abir", device) && !d.Used.Equal(o.clock()) {
			t.Fatalf("device used %v, last sign-in at %v", d.Used, o.clock())
		}
	}
}

// N2: a signed-in person changes their password while a stranger drains the
// username's ceiling: the session already proves who they are.
func TestAChangeIsNotHeldUpByAStrangersGuesses(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	session, _ := joinWithDevice(o, o.invite(operator), "abir", pwAbir)
	for i := 0; i < userFailuresAll+20; i++ {
		guess(o, i)
	}
	if w := tlsSignIn(o, "abir", pwAbir, "203.0.113.70:1"); w.Code != 429 {
		t.Fatalf("the ceiling is not dry: %d", w.Code)
	}
	if w := o.do("POST", "/v1/me/password", session, api.PasswordChange{Current: pwAbir, New: "a brand new passphrase"}, ""); w.Code != 200 {
		t.Fatalf("a signed-in change during a drain = %d %s", w.Code, w.Body)
	}
	// Still throttled per session: ten wrong current passwords, then 429.
	for i := 0; i < userFailures; i++ {
		if w := o.do("POST", "/v1/me/password", session, api.PasswordChange{Current: "not the current one at all", New: "yet another passphrase"}, ""); w.Code != 403 {
			t.Fatalf("wrong current %d = %d", i, w.Code)
		}
	}
	if w := o.do("POST", "/v1/me/password", session, api.PasswordChange{Current: "a brand new passphrase", New: "yet another passphrase"}, ""); w.Code != 429 {
		t.Fatalf("an eleventh change = %d", w.Code)
	}
}

// N3: a thief guessing with a stolen device token from elsewhere backs off
// their own (device, address) pair, never the owner's machine.
func TestAStolenDeviceTokenCannotLockOutTheOwnersMachine(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	_, device := joinWithDevice(o, o.invite(operator), "abir", pwAbir)
	for i := 0; i < 3*userFailures; i++ {
		deviceSignIn(o, "abir", "the thief's wrong guess", "10.66.0.1:1", device)
	}
	if w := deviceSignIn(o, "abir", pwAbir, "10.66.0.1:1", device); w.Code != 429 {
		t.Fatalf("the thief's own pair = %d", w.Code)
	}
	if w := deviceSignIn(o, "abir", pwAbir, "192.0.2.7:1", device); w.Code != 200 {
		t.Fatalf("the owner's machine with the same token = %d %s", w.Code, w.Body)
	}
}
