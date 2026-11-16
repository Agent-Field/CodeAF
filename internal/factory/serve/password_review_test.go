package serve

// ── THE SECURITY REVIEW'S FINDINGS, EACH HELD SHUT ──────────────────────────
//
// One test per finding of the independent review of #2646, named for the
// behaviour it holds. The ones that go through HTTP began as the reviewer's
// probes and failed against the reviewed commit; they now assert the fixed
// behaviour.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// tlsSignIn is a sign-in over https from remote, as a server behind its own
// certificate sees one.
func tlsSignIn(o *pwOrg, u, p, remote string) *httptest.ResponseRecorder {
	data, _ := json.Marshal(api.PasswordSignIn{Username: u, Password: p})
	r := httptest.NewRequest("POST", "/v1/auth/password", bytes.NewReader(data))
	r.RemoteAddr = remote
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	return w
}

func withOWASPArgon(t *testing.T) {
	saved := currentArgon
	currentArgon = owaspArgon
	t.Cleanup(func() { currentArgon = saved })
}

// H1: a sign-in verified with the old password just before a change makes no
// session that outlives the change.
func TestNoSessionMadeWithTheOldPasswordOutlivesAChange(t *testing.T) {
	withOWASPArgon(t)
	o := newPWOrg(t)
	owner := o.signUp(o.operatorLink(), "santosh", pwOperator)
	var mu sync.Mutex
	var stolen []string
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				w := o.signIn("santosh", pwOperator, "")
				var s api.PasswordSignedIn
				if w.Code == 200 && json.Unmarshal(w.Body.Bytes(), &s) == nil {
					mu.Lock()
					stolen = append(stolen, s.AccessToken)
					mu.Unlock()
				}
			}
		}()
	}
	time.Sleep(300 * time.Millisecond)
	w := o.do("POST", "/v1/me/password", owner, api.PasswordChange{Current: pwOperator, New: "a brand new passphrase"}, "")
	close(stop)
	wg.Wait()
	alive := 0
	for _, tok := range stolen {
		if _, code := o.me(tok); code == 200 {
			alive++
		}
	}
	if w.Code != 200 || alive != 0 {
		t.Fatalf("change=%d; old-password sessions minted=%d, alive after the change=%d", w.Code, len(stolen), alive)
	}
	if _, code := o.me(owner); code != 200 {
		t.Fatalf("the changing session itself = %d", code)
	}
}

// H1, without timing: a session can only be made at the account's current
// generation, and one made at an older one is refused on its first use.
func TestASessionIsTiedToThePasswordItWasMadeWith(t *testing.T) {
	o := newPWOrg(t)
	owner := o.signUp(o.operatorLink(), "santosh", pwOperator)
	if w := o.do("POST", "/v1/me/password", owner, api.PasswordChange{Current: pwOperator, New: "a brand new passphrase"}, ""); w.Code != 200 {
		t.Fatalf("change = %d %s", w.Code, w.Body)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/auth/password", nil)
	if _, _, err := o.h.mintSession(w, r, api.Profile{Handle: "santosh", Kind: tokens.KindPassword}, 1); err != errPasswordMoved {
		t.Fatalf("mint at the old generation = %v", err)
	}
	// A session row left at an old generation is refused, not used.
	o.h.sessions.mu.Lock()
	for key, s := range o.h.sessions.ids {
		s.credGen = 1
		o.h.sessions.ids[key] = s
	}
	o.h.sessions.mu.Unlock()
	if _, code := o.me(owner); code != 401 {
		t.Fatalf("a session at the old generation = %d", code)
	}
	acct, _, _ := o.h.passwordAccount("santosh")
	if acct.Gen != 2 || !acct.Changed.Equal(o.clock()) {
		t.Fatalf("account after one change = %+v", acct)
	}
}

// H2: a link made for one GitHub login cannot be redeemed with a password.
func TestALinkForAGitHubLoginTakesNoPassword(t *testing.T) {
	o := newPWOrg(t)
	op := o.signUp(o.operatorLink(), "santosh", pwOperator)
	w := o.do("POST", "/v1/access/links", op, api.CreateAccessLink{Login: "octocat"}, "")
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &made) != nil {
		t.Fatalf("invite = %d %s", w.Code, w.Body)
	}
	check, _ := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "check"})
	for _, m := range check.Factory.SignIn {
		if m == signInPassword {
			t.Fatalf("a GitHub link offers a password: %v", check.Factory.SignIn)
		}
	}
	for _, name := range []string{"octocat", "someone"} {
		out, rw := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "sign_up", Username: name, Password: "attacker chosen passphrase"})
		if rw.Code != 200 || out.State == "joined" || out.AccessToken != "" || out.Reason != reasonGitHubLink {
			t.Fatalf("sign_up as %s through a GitHub link = %d %+v", name, rw.Code, out)
		}
		if _, ok, _ := o.h.passwordAccount(name); ok {
			t.Fatalf("%s has an account", name)
		}
	}
}

// H3: concurrent guesses are each paid for before they are verified, and one
// name is verified one try at a time.
func TestConcurrentGuessesArePaidForBeforeTheyAreVerified(t *testing.T) {
	withOWASPArgon(t)
	o := newPWOrg(t)
	o.signUp(o.operatorLink(), "santosh", pwOperator)
	var verified, throttled atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 120; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := tlsSignIn(o, "victim", fmt.Sprintf("guess-number-%03d-xx", i), fmt.Sprintf("10.0.%d.%d:1", i/200, i%200+1))
			switch w.Code {
			case 401:
				verified.Add(1)
			case 429:
				throttled.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if verified.Load() > userFailures || verified.Load()+throttled.Load() != 120 {
		t.Fatalf("guesses verified: %d (limit %d), 429s: %d", verified.Load(), userFailures, throttled.Load())
	}
}

// H3, without timing: a try spends its tokens when admitted, a right
// password gives them back, and a name is verified one try at a time.
func TestAGuardAdmitsATryOnlyByPayingForIt(t *testing.T) {
	g := newPasswordGuard()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, ok := g.admit("u", "a", "", now, true)
	if !ok {
		t.Fatal("first try refused")
	}
	if _, ok := g.admit("u", "b", "", now, true); ok {
		t.Fatal("a second try of the name was admitted while the first was verified")
	}
	a.failed(now)
	for i := 1; i < userFailures; i++ {
		a, ok := g.admit("u", "a", "", now, true)
		if !ok {
			t.Fatalf("try %d refused", i)
		}
		a.failed(now)
	}
	if _, ok := g.admit("u", "a", "", now, true); ok {
		t.Fatal("an eleventh try from one address was admitted")
	}
	// Right passwords spend nothing.
	for i := 0; i < 3*userFailures; i++ {
		a, ok := g.admit("v", "c", "", now, true)
		if !ok {
			t.Fatalf("right password %d refused", i)
		}
		a.succeeded(now)
	}
	// An abandoned try (hashing busy) costs nothing and frees the name.
	a, _ = g.admit("w", "d", "", now, true)
	a.abandon(now)
	if _, ok := g.admit("w", "d", "", now, true); !ok {
		t.Fatal("abandon left the name held")
	}
}

// M2: a name the organization already gave somebody (a share, a team, a
// GitHub sign-in long gone) cannot be signed up for by someone else.
func TestANameTheOrganizationAlreadyUsesCannotBeSignedUpFor(t *testing.T) {
	o := newPWOrg(t)
	op := o.signUp(o.operatorLink(), "santosh", pwOperator)
	var ws api.Workstream
	w := o.do("POST", "/v1/workstreams", op, api.WorkstreamNew{Name: "Plans"}, "")
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &ws) != nil {
		t.Fatalf("workstream = %d %s", w.Code, w.Body)
	}
	shares := []factory.Share{{Kind: factory.ShareToPerson, ID: "bob", Role: factory.RoleEdit}}
	if w := o.do("PUT", "/v1/workstreams/"+ws.ID+"/shares", op, api.WorkstreamSharesPut{Shares: &shares, Rev: &ws.Rev}, ""); w.Code != 200 {
		t.Fatalf("share = %d %s", w.Code, w.Body)
	}
	if err := tokens.RememberHandle(o.cfg.TokensPath, "mia"); err != nil {
		t.Fatal(err)
	}
	if err := o.st.ChangeTeams(func(teams *[]store.Team) error {
		*teams = append(*teams, store.Team{ID: "team_x", Name: "X", Members: []string{"carol"}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bob", "mia", "carol"} {
		out, _ := o.access(api.FactoryAccessRequest{Link: o.invite(op), Verb: "sign_up", Username: name, Password: pwAbir})
		if out.State == "joined" || out.Reason != reasonUsernameTaken {
			t.Fatalf("sign up as %s, already named here = %+v", name, out)
		}
	}
	// A name nobody here uses is free.
	o.signUp(o.invite(op), "torvalds", pwAbir)
}

// L1: an IPv6 /64 is one address, and a key that is backing off is never
// forgotten to make room; a full set refuses instead.
func TestTheThrottleCountsAnIPv6BlockAsOneAddressAndForgetsNoBackoff(t *testing.T) {
	if throttleAddr(netip.MustParseAddr("2001:db8:1:2::1")) != throttleAddr(netip.MustParseAddr("2001:db8:1:2:ffff::9")) ||
		throttleAddr(netip.MustParseAddr("2001:db8:1:2::1")) == throttleAddr(netip.MustParseAddr("2001:db8:1:3::1")) ||
		throttleAddr(netip.MustParseAddr("::ffff:192.0.2.1")) != "192.0.2.1" {
		t.Fatal("addresses are not keyed by /64")
	}
	o := newPWOrg(t)
	o.signUp(o.operatorLink(), "santosh", pwOperator)
	for i := 0; i < addrFailures; i++ {
		tlsSignIn(o, fmt.Sprintf("name%02d", i), pwAbir, fmt.Sprintf("[2001:db8:1:2::%x]:1", i+1))
	}
	if w := tlsSignIn(o, "santosh", pwOperator, "[2001:db8:1:2:abcd::1]:1"); w.Code != 429 {
		t.Fatalf("the same /64 after fifty failures = %d", w.Code)
	}
	if w := tlsSignIn(o, "santosh", pwOperator, "[2001:db8:1:3::1]:1"); w.Code != 200 {
		t.Fatalf("another /64 = %d %s", w.Code, w.Body)
	}

	th := newThrottle("test", 1, true)
	th.most = 2
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, k := range []string{"a", "b"} {
		if !th.take(k, now) {
			t.Fatal(k)
		}
		th.failed(k, now)
	}
	if th.take("c", now) {
		t.Fatal("a full set of backing-off keys made room")
	}
	if _, ok := th.keys["a"]; !ok {
		t.Fatal("a backing-off key was forgotten")
	}
}

// L2: a transparent rehash never puts back a password a change replaced.
func TestARehashNeverUndoesAChange(t *testing.T) {
	o := newPWOrg(t)
	o.signUp(o.operatorLink(), "santosh", pwOperator)
	before, _, _ := o.h.freshAccount("santosh")
	if _, err := tokens.SetPassword(o.cfg.TokensPath, "santosh", "$changed", before.Gen, o.clock()); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Rehash(o.cfg.TokensPath, "santosh", before.Hash, "$rehash-of-the-old-one"); err != tokens.ErrStale {
		t.Fatalf("rehash over a change = %v", err)
	}
	after, _, _ := o.h.freshAccount("santosh")
	if after.Hash != "$changed" {
		t.Fatalf("hash = %q", after.Hash)
	}
	// And a second change racing the first loses rather than overwrites.
	if _, err := tokens.SetPassword(o.cfg.TokensPath, "santosh", "$second", before.Gen, o.clock()); err != tokens.ErrStale {
		t.Fatalf("a change at a stale generation = %v", err)
	}
}

// L3: the list is the large one, compared by letters and digits.
func TestCommonPasswordsAreRefusedHoweverTheyAreSpelled(t *testing.T) {
	o := newPWOrg(t)
	for _, pw := range []string{"P.a.s.s.w.o.r.d-1234", "Correct Horse Battery Staple", "q-w-e-r-t-y-1-2-3-4-5-6", "1qaz 2wsx 3edc", "I love you forever"} {
		out, _ := o.access(api.FactoryAccessRequest{Link: o.operatorLink(), Verb: "sign_up", Username: "santosh", Password: pw})
		if out.Reason != reasonPasswordCommon {
			t.Errorf("%q = %+v", pw, out)
		}
	}
}

// L4: a GitHub sign-in and a password sign-up racing for one name leave one
// owner.
func TestAGitHubSignInRacingAPasswordSignUpLeavesOneOwner(t *testing.T) {
	o := newPWOrg(t)
	op := o.signUp(o.operatorLink(), "santosh", pwOperator)
	invite := o.invite(op)
	g := &fakeGitHub{t: t, login: "mia"}
	o.h.config.GitHubClientID, o.h.config.GitHubHTTPClient = "client", g.client()
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Add(o.cfg.TokensPath, row); err != nil {
		t.Fatal(err)
	}
	var signedUp api.FactoryAccess
	afterGitHubNameCheck = func(string) {
		signedUp, _ = o.access(api.FactoryAccessRequest{Link: invite, Verb: "sign_up", Username: "mia", Password: pwAbir})
	}
	t.Cleanup(func() { afterGitHubNameCheck = nil })
	device := startIdentity(t, o.h, row.Token, nil)
	o.advance(5 * time.Second)
	w := pollIdentity(o.h, device, row.Token, nil)
	if signedUp.State == "joined" && w.Code == 200 {
		t.Fatalf("both own mia: password %+v, GitHub %d", signedUp, w.Code)
	}
	if signedUp.State != "joined" || w.Code != 403 {
		t.Fatalf("password %+v, GitHub %d %s", signedUp, w.Code, w.Body)
	}
}

// L5: an account file that cannot be read refuses the request and keeps the
// session; the file is parsed again only when it changes.
func TestAnUnreadableAccountFileKeepsTheSession(t *testing.T) {
	o := newPWOrg(t)
	token := o.signUp(o.operatorLink(), "santosh", pwOperator)
	path := tokens.AccountsPath(o.cfg.TokensPath)
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The sign-up wrote the file (its device token); the first read parses it.
	o.me(token)
	parses := accountsParses.Load()
	for i := 0; i < 5; i++ {
		if _, code := o.me(token); code != 200 {
			t.Fatalf("me = %d", code)
		}
	}
	if accountsParses.Load() != parses {
		t.Fatalf("accounts.json parsed %d times for five requests", accountsParses.Load()-parses)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := o.me(token); code == 200 {
		t.Fatal("an unreadable account file let the session in")
	}
	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := o.me(token); code != 200 {
		t.Fatalf("the session after the file reads again = %d", code)
	}
}
