package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// The suite signs dozens of people in; at OWASP's 46 MiB each it would spend
// the box's memory for nothing the tests prove. One test below hashes at the
// real parameters.
func init() { currentArgon = argonParams{memory: 64, time: 1, threads: 1} }

// THE PASSWORDS EVERY TEST USES. leakPassword is the one the leak test looks
// for in every sink; nothing else in this file spells it.
const (
	pwOperator = "violet otter campsite drizzle"
	pwAbir     = "a long passphrase for my friend"
)

// pwOrg is an organization server with no GitHub at all, served from a temp
// folder with a real store, record and token files, the way `codeaf factory
// serve` runs one.
type pwOrg struct {
	t    *testing.T
	h    *Handler
	st   *store.Store
	cfg  Config
	now  time.Time
	mu   sync.Mutex
	root string
}

func newPWOrg(t *testing.T) *pwOrg {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := &pwOrg{t: t, st: st, now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), root: st.Root()}
	clock := func() time.Time { o.mu.Lock(); defer o.mu.Unlock(); return o.now }
	history, err := record.Open(st, record.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.Seam{Load: func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err }, SetRepos: st.SetRepos}
	o.cfg = Config{Name: "Acme", Version: "test", Address: "https://org.test", Mode: api.FactoryTeam, AccessToken: "host-secret",
		Store: st, Record: history, TokensPath: filepath.Join(st.Root(), "tokens.json"), PullWait: time.Millisecond, Now: clock}
	o.h = New(seam, o.cfg)
	t.Cleanup(o.h.Close)
	return o
}

func (o *pwOrg) advance(d time.Duration) {
	o.mu.Lock()
	o.now = o.now.Add(d)
	o.mu.Unlock()
}

func (o *pwOrg) clock() time.Time { o.mu.Lock(); defer o.mu.Unlock(); return o.now }

// do sends one request from remote (loopback when empty) and answers it.
func (o *pwOrg) do(method, path, token string, body any, remote string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			reader = strings.NewReader(b)
		default:
			data, _ := json.Marshal(body)
			reader = bytes.NewReader(data)
		}
	}
	r := httptest.NewRequest(method, path, reader)
	if remote == "" {
		remote = "127.0.0.1:4000"
	}
	r.RemoteAddr = remote
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	return w
}

func (o *pwOrg) operatorLink() string {
	o.t.Helper()
	tok, err := tokens.RotateOperator(o.cfg.TokensPath, "", o.clock(), false)
	if err != nil {
		o.t.Fatal(err)
	}
	return accessLinkURL(o.cfg.Address, tok)
}

func (o *pwOrg) access(body api.FactoryAccessRequest) (api.FactoryAccess, *httptest.ResponseRecorder) {
	o.t.Helper()
	w := o.do("POST", "/v1/factory-access", "", body, "")
	var out api.FactoryAccess
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			o.t.Fatal(err)
		}
	}
	return out, w
}

func (o *pwOrg) signUp(link, username, password string) string {
	o.t.Helper()
	out, w := o.access(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: username, Password: password})
	if w.Code != 200 || out.State != "joined" || out.AccessToken == "" {
		o.t.Fatalf("sign up %s = %d %s", username, w.Code, w.Body)
	}
	return out.AccessToken
}

func (o *pwOrg) invite(operator string) string {
	o.t.Helper()
	w := o.do("POST", "/v1/access/links", operator, api.CreateAccessLink{}, "")
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &made) != nil || made.Link.URL == "" {
		o.t.Fatalf("invite = %d %s", w.Code, w.Body)
	}
	return made.Link.URL
}

func (o *pwOrg) signIn(username, password, remote string) *httptest.ResponseRecorder {
	return o.do("POST", "/v1/auth/password", "", api.PasswordSignIn{Username: username, Password: password}, remote)
}

func (o *pwOrg) token(username, password string) string {
	o.t.Helper()
	w := o.signIn(username, password, "")
	var signed api.PasswordSignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil || signed.AccessToken == "" {
		o.t.Fatalf("sign in %s = %d %s", username, w.Code, w.Body)
	}
	return signed.AccessToken
}

func (o *pwOrg) me(token string) (api.Me, int) {
	w := o.do("GET", "/v1/me", token, nil, "")
	var me api.Me
	_ = json.Unmarshal(w.Body.Bytes(), &me)
	return me, w.Code
}

// ── hashing ──

func TestPasswordHashIsArgon2idPHCAtOWASPParameters(t *testing.T) {
	if owaspArgon != (argonParams{memory: 47104, time: 1, threads: 1}) {
		t.Fatalf("the production parameters moved: %+v", owaspArgon)
	}
	saved := currentArgon
	currentArgon = owaspArgon
	phc, err := hashPassword(pwOperator)
	currentArgon = saved
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=47104,t=1,p=1$") || strings.Contains(phc, pwOperator) {
		t.Fatalf("hash = %q", phc)
	}
	p, salt, key, err := decodePHC(phc)
	if err != nil || p != owaspArgon || len(salt) != 16 || len(key) != 32 {
		t.Fatalf("decoded %+v %d %d %v", p, len(salt), len(key), err)
	}
	ok, weaker, err := verifyPassword(pwOperator, phc)
	if !ok || weaker || err != nil {
		t.Fatalf("verify = %v %v %v", ok, weaker, err)
	}
	if ok, _, _ := verifyPassword(pwOperator+"!", phc); ok {
		t.Fatal("a different password verified")
	}
	// Two hashes of one password differ: the salt is per password.
	again, _ := hashPassword(pwOperator)
	if again == phc {
		t.Fatal("no salt")
	}
}

func TestPHCDecodeRefusesHostileStrings(t *testing.T) {
	good, _ := hashPassword(pwOperator)
	for _, bad := range []string{"", "$argon2i$v=19$m=64,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5", "$argon2id$v=18$m=64,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=4294967295,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5", "$argon2id$v=19$m=64,t=999,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=64,t=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5", "$argon2id$v=19$m=64,t=1,p=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5",
		strings.Replace(good, "$argon2id$", "$argon2id$$", 1), good + "$"} {
		if _, _, _, err := decodePHC(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func FuzzDecodePHC(f *testing.F) {
	good, _ := hashPassword(pwOperator)
	f.Add(good)
	f.Add("$argon2id$v=19$m=64,t=1,p=1$$")
	f.Fuzz(func(t *testing.T, s string) {
		p, salt, key, err := decodePHC(s)
		if err != nil {
			return
		}
		// Whatever decodes re-encodes to a string that decodes the same.
		p2, salt2, key2, err := decodePHC(encodePHC(p, salt, key))
		if err != nil || p2 != p || !bytes.Equal(salt, salt2) || !bytes.Equal(key, key2) {
			t.Fatalf("round trip of %q failed", s)
		}
	})
}

// ── policy ──

func TestPasswordPolicyIsNISTs(t *testing.T) {
	cases := map[string]string{
		"elevenchars":                                 reasonPasswordShort,
		"twelve chars":                                "",
		strings.Repeat("xy", 128):                     "",
		strings.Repeat("xy", 128) + "z":               reasonPasswordLong,
		"qwertyuiopasdfghjkl":                         reasonPasswordCommon,
		"P.a.s.s.w.o.r.d-1234":                        reasonPasswordCommon,
		"correct horse battery staple":                reasonPasswordCommon,
		"Correct-Horse Battery Staple!":               reasonPasswordCommon,
		"the quick brown fox jumps over the lazy dog": reasonPasswordCommon,
		"my name is A.B.I.R, ok":                      reasonPasswordName,
		"QwertyuiopASDFGHJKL":                         reasonPasswordCommon,
		"my name is abir, ok":                         reasonPasswordName,
		"日本語のパスフレーズです、長い":                             "",
		"spaces are fine here":                        "",
		"no composition rules apply to this one":      "",
	}
	for pw, want := range cases {
		if got := passwordRefusal(normalPassword(pw), "abir"); got != want {
			t.Errorf("%q = %q, want %q", pw, got, want)
		}
	}
	// NFKC: a fullwidth passphrase and its ASCII spelling are one password.
	if normalPassword("ｃｏｒｒｅｃｔ　ｈｏｒｓｅ") != "correct horse" {
		t.Fatalf("NFKC = %q", normalPassword("ｃｏｒｒｅｃｔ　ｈｏｒｓｅ"))
	}
	if !commonPassword("ＱＷＥＲＴＹＵＩＯＰＡＳＤＦＧＨＪＫＬ") && !commonPassword(normalPassword("ＱＷＥＲＴＹＵＩＯＰＡＳＤＦＧＨＪＫＬ")) {
		t.Fatal("the list is not consulted after normalizing")
	}
	// The list is the large one, and a two-letter username is not a rule.
	commonOnce.Do(loadCommon)
	if len(commonAt) < 400000 {
		t.Fatalf("common list has %d entries", len(commonAt))
	}
	if passwordRefusal(normalPassword("absolutely fine passphrase"), "ab") != "" {
		t.Fatal("a two-letter username refuses everything")
	}
	for _, u := range []string{"a", "-abc", "abc-", "Ab C", strings.Repeat("a", 40), "ab_c"} {
		if cleanUsername(u) != "" {
			t.Errorf("username %q accepted", u)
		}
	}
	if cleanUsername("  Abir-K ") != "abir-k" || cleanUsername("ab") != "ab" {
		t.Fatal("a username was refused")
	}
}

// ── sign-up and its refusals, over the wire ──

func TestSignUpThroughTheOperatorLinkMakesTheOperator(t *testing.T) {
	o := newPWOrg(t)
	link := o.operatorLink()
	check, w := o.access(api.FactoryAccessRequest{Link: link, Verb: "check"})
	if w.Code != 200 || check.State != "ready" || strings.Join(check.Factory.SignIn, ",") != "password" {
		t.Fatalf("check = %d %s", w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control = %q", w.Header().Get("Cache-Control"))
	}
	token := o.signUp(link, "Santosh", pwOperator)
	me, code := o.me(token)
	if code != 200 || me.Handle != "santosh" || !me.Operator || me.SignInKind != "password" || !me.GitHubSignedIn || me.Kind != "password" {
		t.Fatalf("me = %d %+v", code, me)
	}
	// The claim is spent: the link opens as used, and a second sign-up on it
	// makes no account.
	again, _ := o.access(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: "mallory", Password: pwAbir})
	if again.State == "joined" || again.AccessToken != "" {
		t.Fatalf("a spent claim made an account: %+v", again)
	}
	if _, ok, _ := o.h.passwordAccount("mallory"); ok {
		t.Fatal("mallory has an account")
	}
	// The record says who signed up and who claimed.
	events, err := o.cfg.Record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind+":"+e.Verb)
	}
	if !strings.Contains(strings.Join(kinds, " "), "operator_claimed") || !strings.Contains(strings.Join(kinds, " "), "account_changed:signed up") {
		t.Fatalf("record = %v", kinds)
	}
}

func TestSignUpRefusalsNameTheRuleAndSpendNothing(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	link := o.invite(operator)
	for _, c := range []struct{ user, pw, reason string }{
		{"-x", pwAbir, reasonUsernameInvalid},
		{"santosh", pwAbir, reasonUsernameTaken},
		{"SANTOSH", pwAbir, reasonUsernameTaken},
		{"abir", "too short", reasonPasswordShort},
		{"abir", "qwertyuiopasdfghjkl", reasonPasswordCommon},
		{"abir", "abir is my password", reasonPasswordName},
		{"abir", strings.Repeat("é", 129), reasonPasswordLong},
	} {
		out, w := o.access(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: c.user, Password: c.pw})
		if w.Code != 200 || out.State != "ready" || out.Reason != c.reason || out.Message != refusalWords[c.reason] || out.AccessToken != "" {
			t.Errorf("%s/%d chars = %d %s; want %s", c.user, len(c.pw), w.Code, w.Body, c.reason)
		}
	}
	// None of them used the one place the link has.
	o.signUp(link, "abir", pwAbir)
}

func TestPasswordDoorsAreJSONOnlyAndHTTPSOnly(t *testing.T) {
	o := newPWOrg(t)
	link := o.operatorLink()
	body, _ := json.Marshal(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: "santosh", Password: pwOperator})
	// Plain HTTP from another machine: refused before anything is read.
	if w := o.do("POST", "/v1/factory-access", "", string(body), "192.0.2.7:5000"); w.Code != 403 || !strings.Contains(w.Body.String(), passwordHTTPSWords) {
		t.Fatalf("plain http = %d %s", w.Code, w.Body)
	}
	if w := o.signIn("santosh", pwOperator, "192.0.2.7:5000"); w.Code != 403 {
		t.Fatalf("plain http sign-in = %d %s", w.Code, w.Body)
	}
	// A form post is not taken.
	r := httptest.NewRequest("POST", "/v1/auth/password", strings.NewReader("username=santosh&password=x"))
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form = %d", w.Code)
	}
	// An unknown field, and a body over 8 KiB, are refused without an echo.
	if w := o.do("POST", "/v1/auth/password", "", `{"username":"santosh","password":"`+pwOperator+`","admin":true}`, ""); w.Code != 422 || strings.Contains(w.Body.String(), pwOperator) {
		t.Fatalf("unknown field = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/auth/password", "", `{"username":"santosh","password":"`+strings.Repeat("x", 9000)+`"}`, ""); w.Code != 413 {
		t.Fatalf("big body = %d", w.Code)
	}
	if w := o.do("POST", "/v1/factory-access", "", `{"link":"`+link+`","verb":"sign_up","username":"santosh","password":"x","extra":1}`, ""); w.Code != 422 {
		t.Fatalf("unknown access field = %d", w.Code)
	}
	// A verb that is not a password door takes no password.
	if w := o.do("POST", "/v1/factory-access", "", api.FactoryAccessRequest{Link: link, Verb: "check", Password: pwOperator}, ""); w.Code != 422 {
		t.Fatalf("check with a password = %d", w.Code)
	}
	// Through a trusted proxy that says https, from anywhere, it is taken.
	o.h.config.TrustedProxies = trustedLoopback(t)
	req := httptest.NewRequest("POST", "/v1/factory-access", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "198.51.100.4")
	req.Header.Set("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	o.h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"joined"`) {
		t.Fatalf("via proxy = %d %s", w.Code, w.Body)
	}
}

// ── sign in again ──

func TestASignInRefusalSaysTheSameThingAndCostsTheSame(t *testing.T) {
	o := newPWOrg(t)
	o.signUp(o.operatorLink(), "santosh", pwOperator)
	dummyHash()
	answers := map[string]string{}
	for name, attempt := range map[string][2]string{"unknown": {"nobody", pwOperator}, "wrong": {"santosh", pwAbir}, "invalid": {"-bad-", pwOperator}} {
		before := argonRuns.Load()
		w := o.signIn(attempt[0], attempt[1], "")
		if argonRuns.Load()-before != 1 {
			t.Errorf("%s ran argon2 %d times, want 1", name, argonRuns.Load()-before)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		delete(body, "request_id")
		got, _ := json.Marshal(body)
		answers[name] = w.Result().Status + " " + string(got)
		if w.Code != 401 || !strings.Contains(w.Body.String(), signInRefusedWords) {
			t.Errorf("%s = %d %s", name, w.Code, w.Body)
		}
	}
	if answers["unknown"] != answers["wrong"] || answers["wrong"] != answers["invalid"] {
		t.Fatalf("the refusals differ:\n%v", answers)
	}
	// The right one signs in, and a removed person reads like a stranger.
	o.token("santosh", pwOperator)
	if err := tokens.RemoveAccount(o.cfg.TokensPath, "santosh"); err != nil {
		t.Fatal(err)
	}
	if w := o.signIn("santosh", pwOperator, ""); w.Code != 401 {
		t.Fatalf("removed = %d", w.Code)
	}
}

func TestSignInIsThrottledByNameAndAddressAndRecovers(t *testing.T) {
	o := newPWOrg(t)
	o.signUp(o.operatorLink(), "santosh", pwOperator)
	for i := 0; i < userFailures; i++ {
		if w := o.signIn("santosh", pwAbir, "127.0.0.1:1"); w.Code != 401 {
			t.Fatalf("try %d = %d", i, w.Code)
		}
	}
	// From that address the next try is refused, even with the right password.
	w := o.signIn("santosh", pwOperator, "127.0.0.1:1")
	if w.Code != 429 || !strings.Contains(w.Body.String(), tooManyTriesWords) {
		t.Fatalf("eleventh = %d %s", w.Code, w.Body)
	}
	// From another address the owner is in: one address's guesses hold up
	// that address, not the name.
	if w := o.signIn("santosh", pwOperator, "127.0.0.2:1"); w.Code != 200 {
		t.Fatalf("another address = %d %s", w.Code, w.Body)
	}
	// Another name from the same address is not held up by santosh's.
	if w := o.signIn("abir", pwAbir, "127.0.0.1:1"); w.Code != 401 {
		t.Fatalf("other name = %d", w.Code)
	}
	// A summary, not a flood, is written down.
	events, _ := o.cfg.Record.Since(context.Background(), time.Time{})
	refused := 0
	for _, e := range events {
		if e.Verb == "refused sign-ins" {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("refused-sign-in rows = %d, want 1", refused)
	}
	// It decays: fifteen quiet minutes later that address may try again.
	o.advance(throttleWindow)
	if w := o.signIn("santosh", pwOperator, "127.0.0.1:1"); w.Code != 200 {
		t.Fatalf("after the window = %d %s", w.Code, w.Body)
	}

	// By address: fifty failures over many names hold that address only.
	for i := 0; i < addrFailures; i++ {
		o.signIn("name"+string(rune('a'+i%26))+string(rune('a'+i/26)), pwAbir, "127.0.0.9:1")
	}
	if w := o.signIn("santosh", pwOperator, "127.0.0.9:1"); w.Code != 429 {
		t.Fatalf("held address = %d", w.Code)
	}
	if w := o.signIn("santosh", pwOperator, "127.0.0.10:1"); w.Code != 200 {
		t.Fatalf("other address = %d %s", w.Code, w.Body)
	}
}

// A NAME HAS A LOOSER CEILING OF ITS OWN, which an address the person signed
// in from before does not count against.
func TestANameHasACeilingThatItsOwnAddressesSkip(t *testing.T) {
	o := newPWOrg(t)
	o.signUp(o.operatorLink(), "santosh", pwOperator)
	if w := o.signIn("santosh", pwOperator, "127.0.0.5:1"); w.Code != 200 {
		t.Fatalf("first sign-in = %d", w.Code)
	}
	for i := 0; i < userFailuresAll; i++ {
		o.signIn("santosh", pwAbir, "127.1."+itoa(i/200)+"."+itoa(i%200+1)+":1")
	}
	if w := o.signIn("santosh", pwOperator, "127.0.0.6:1"); w.Code != 429 {
		t.Fatalf("a new address past the ceiling = %d", w.Code)
	}
	if w := o.signIn("santosh", pwOperator, "127.0.0.5:1"); w.Code != 200 {
		t.Fatalf("the owner's own address = %d %s", w.Code, w.Body)
	}
}

func TestTheThrottleBacksOffExponentiallyAndForgets(t *testing.T) {
	th := newThrottle(10, true)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	try := func(at time.Time) bool {
		_, ok := th.reserve("u", at)
		if ok {
			th.failed("u", at)
		}
		return ok
	}
	for i := 0; i < 10; i++ {
		if !try(now) {
			t.Fatalf("try %d refused", i)
		}
	}
	if _, ok := th.reserve("u", now); ok {
		t.Fatal("open after ten")
	}
	first := th.keys["u"].until.Sub(now)
	// Wait out the bucket, fail once more: the wait doubles.
	now = now.Add(2 * time.Minute)
	if !try(now) {
		t.Fatal("not open after the refill")
	}
	second := th.keys["u"].until.Sub(now)
	if second != 2*first {
		t.Fatalf("backoff %v then %v", first, second)
	}
	// Never past the most, and forgiven after a quiet spell.
	for i := 0; i < 20; i++ {
		now = th.keys["u"].until.Add(time.Minute)
		try(now)
	}
	if th.keys["u"].until.Sub(now) > backoffMost {
		t.Fatalf("waited %v", th.keys["u"].until.Sub(now))
	}
	now = now.Add(throttleWindow + backoffForgiven)
	if !try(now) {
		t.Fatal("still locked after a quiet spell")
	}
	// Bounded: a full set forgets keys that are not backing off.
	small := newThrottle(10, false)
	small.most = 3
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		if _, ok := small.reserve(k, now); !ok {
			t.Fatalf("%s refused", k)
		}
	}
	if len(small.keys) > 3 {
		t.Fatalf("set grew to %d", len(small.keys))
	}
}

// ── one link, one account ──

func TestTwoSignUpsRacingForOneLinkMakeOneAccount(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	link := o.invite(operator)
	var wg sync.WaitGroup
	results := make([]api.FactoryAccess, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := o.do("POST", "/v1/factory-access", "", api.FactoryAccessRequest{Link: link, Verb: "sign_up",
				Username: "racer" + string(rune('a'+i)), Password: pwAbir}, "")
			_ = json.Unmarshal(w.Body.Bytes(), &results[i])
		}(i)
	}
	wg.Wait()
	joined := 0
	for _, r := range results {
		if r.State == "joined" {
			joined++
		}
	}
	accounts, _ := tokens.LoadAccounts(o.cfg.TokensPath)
	if joined != 1 || len(accounts.Accounts) != 2 {
		t.Fatalf("joined %d, accounts %d", joined, len(accounts.Accounts))
	}
}

// ── change, reset ──

func TestChangingAPasswordEndsEveryOtherSession(t *testing.T) {
	o := newPWOrg(t)
	here := o.signUp(o.operatorLink(), "santosh", pwOperator)
	there := o.token("santosh", pwOperator)
	if w := o.do("POST", "/v1/me/password", here, api.PasswordChange{Current: pwAbir, New: "a brand new passphrase"}, ""); w.Code != 403 {
		t.Fatalf("wrong current = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/me/password", here, api.PasswordChange{Current: pwOperator, New: "short"}, ""); w.Code != 422 || !strings.Contains(w.Body.String(), refusalWords[reasonPasswordShort]) {
		t.Fatalf("short new = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/me/password", here, api.PasswordChange{Current: pwOperator, New: "a brand new passphrase"}, ""); w.Code != 204 {
		t.Fatalf("change = %d %s", w.Code, w.Body)
	}
	if _, code := o.me(here); code != 200 {
		t.Fatalf("this session = %d", code)
	}
	if _, code := o.me(there); code != 401 {
		t.Fatalf("the other session = %d", code)
	}
	if w := o.signIn("santosh", pwOperator, ""); w.Code != 401 {
		t.Fatal("the old password still works")
	}
	o.token("santosh", "a brand new passphrase")
}

func TestAnOperatorResetLinkIsOneUseOneHourAndEndsSessions(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	abir := o.signUp(o.invite(operator), "abir", pwAbir)
	// Only the operator, only for a password person.
	if w := o.do("POST", "/v1/access/links", abir, api.CreateAccessLink{Kind: "reset", Person: "santosh"}, ""); w.Code != 403 {
		t.Fatalf("member made a reset link: %d", w.Code)
	}
	if w := o.do("POST", "/v1/access/links", operator, api.CreateAccessLink{Kind: "reset", Person: "ghost"}, ""); w.Code != 422 {
		t.Fatalf("reset for nobody = %d", w.Code)
	}
	w := o.do("POST", "/v1/access/links", operator, api.CreateAccessLink{Kind: "reset", Person: "abir"}, "")
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &made) != nil || made.Link.Kind != "reset" || made.Link.Login != "abir" ||
		!made.Link.ExpiresAt.Equal(o.clock().Add(time.Hour)) || !strings.Contains(made.Link.URL, "reset=true") {
		t.Fatalf("reset link = %d %s", w.Code, w.Body)
	}
	// Its secret is no bearer.
	_, frag, _ := strings.Cut(made.Link.URL, "#token=")
	if _, code := o.me(frag); code != 401 {
		t.Fatalf("a reset secret read /v1/me: %d", code)
	}
	check, _ := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "check"})
	if check.State != "ready" || check.Kind != "reset" {
		t.Fatalf("check = %+v", check)
	}
	if out, _ := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "reset", Password: "abir again and again"}); out.Reason != reasonPasswordName {
		t.Fatalf("a refused reset = %+v", out)
	}
	out, _ := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "reset", Password: "a fresh passphrase here"})
	if out.State != "joined" || out.AccessToken == "" {
		t.Fatalf("reset = %+v", out)
	}
	if _, code := o.me(abir); code != 401 {
		t.Fatalf("the old session lived through a reset: %d", code)
	}
	if me, code := o.me(out.AccessToken); code != 200 || me.Handle != "abir" {
		t.Fatalf("the new session = %d", code)
	}
	if again, _ := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "reset", Password: "another one entirely"}); again.State != "used_up" {
		t.Fatalf("second use = %+v", again)
	}
	o.token("abir", "a fresh passphrase here")
	// A reset link left an hour is over.
	w = o.do("POST", "/v1/access/links", operator, api.CreateAccessLink{Kind: "reset", Person: "abir"}, "")
	_ = json.Unmarshal(w.Body.Bytes(), &made)
	o.advance(time.Hour)
	if late, _ := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "reset", Password: "too late for this one"}); late.State != "expired" {
		t.Fatalf("an hour later = %+v", late)
	}
	events, _ := o.cfg.Record.Since(context.Background(), time.Time{})
	verbs := ""
	for _, e := range events {
		verbs += e.Verb + ";"
	}
	for _, want := range []string{"made password reset link", "reset password", "signed in"} {
		if !strings.Contains(verbs, want) {
			t.Errorf("record has no %q: %s", want, verbs)
		}
	}
}

// ── one namespace ──

func TestAGitHubLoginThatIsAPasswordUsernameIsRefused(t *testing.T) {
	o := newPWOrg(t)
	o.signUp(o.operatorLink(), "santosh", pwOperator)
	g := &fakeGitHub{t: t, login: "santosh"}
	o.h.config.GitHubClientID, o.h.config.GitHubHTTPClient = "client", g.client()
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Add(o.cfg.TokensPath, row); err != nil {
		t.Fatal(err)
	}
	device := startIdentity(t, o.h, row.Token, nil)
	o.advance(5 * time.Second)
	w := pollIdentity(o.h, device, row.Token, nil)
	if w.Code != 403 || !strings.Contains(w.Body.String(), githubNameHeldWords) {
		t.Fatalf("github as santosh = %d %s", w.Code, w.Body)
	}
	// And the other way: a GitHub person's login cannot be chosen as a username.
	g.login = "mia"
	device = startIdentity(t, o.h, row.Token, nil)
	o.advance(5 * time.Second)
	if w := pollIdentity(o.h, device, row.Token, nil); w.Code != 200 {
		t.Fatalf("github as mia = %d %s", w.Code, w.Body)
	}
	operator := o.token("santosh", pwOperator)
	out, _ := o.access(api.FactoryAccessRequest{Link: o.invite(operator), Verb: "sign_up", Username: "mia", Password: pwAbir})
	if out.Reason != reasonUsernameTaken {
		t.Fatalf("mia as a username = %+v", out)
	}
	// /v1/about and /v1/me say both ways in, password first.
	var about struct {
		SignIn []string `json:"sign_in"`
	}
	_ = json.Unmarshal(o.do("GET", "/v1/about", "", nil, "").Body.Bytes(), &about)
	if strings.Join(about.SignIn, ",") != "password,github" {
		t.Fatalf("about = %v", about.SignIn)
	}
	var people api.People
	_ = json.Unmarshal(o.do("GET", "/v1/people", operator, nil, "").Body.Bytes(), &people)
	kinds := map[string]string{}
	for _, p := range people.People {
		kinds[p.Handle] = p.Kind
	}
	if kinds["santosh"] != "password" || kinds["mia"] != "github" {
		t.Fatalf("people kinds = %v", kinds)
	}
}

// ── sessions ──

func TestAPasswordSessionIdlesOutAtThirtyDaysAndEndsAtNinety(t *testing.T) {
	o := newPWOrg(t)
	token := o.signUp(o.operatorLink(), "santosh", pwOperator)
	for i := 0; i < 4; i++ {
		o.advance(20 * 24 * time.Hour)
		if _, code := o.me(token); code != 200 {
			t.Fatalf("used every 20 days, day %d = %d", (i+1)*20, code)
		}
	}
	o.advance(11 * 24 * time.Hour) // day 91
	if _, code := o.me(token); code != 401 {
		t.Fatalf("day 91 = %d", code)
	}
	idle := o.token("santosh", pwOperator)
	o.advance(31 * 24 * time.Hour)
	if _, code := o.me(idle); code != 401 {
		t.Fatalf("31 idle days = %d", code)
	}
}

// ── two password people, no GitHub, over a real listener ──

// THE ACCEPTANCE: an organization with no GitHub configured at all. santosh
// claims it through the operator link, makes an invitation, abir joins with a
// username and a password, santosh shares a workstream with abir, both read
// it; then both sign out and sign back in with the password alone.
func TestTwoPasswordPeopleShareAWorkstreamOverTheWire(t *testing.T) {
	o := newPWOrg(t)
	srv := httptest.NewServer(o.h)
	t.Cleanup(srv.Close)
	call := func(token, method, path string, body any, out any) int {
		t.Helper()
		var reader io.Reader
		if body != nil {
			data, _ := json.Marshal(body)
			reader = bytes.NewReader(data)
		}
		req, _ := http.NewRequest(method, srv.URL+path, reader)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if out != nil && res.StatusCode < 300 {
			if err := json.NewDecoder(res.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode
	}
	var claim api.FactoryAccess
	if code := call("", "POST", "/v1/factory-access", api.FactoryAccessRequest{Link: o.operatorLink(), Verb: "sign_up", Username: "santosh", Password: pwOperator, Name: "Santosh"}, &claim); code != 200 || claim.State != "joined" {
		t.Fatalf("claim = %d %+v", code, claim)
	}
	santosh := claim.AccessToken
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	if code := call(santosh, "POST", "/v1/access/links", api.CreateAccessLink{}, &made); code != 201 {
		t.Fatalf("invite = %d", code)
	}
	var joined api.FactoryAccess
	if code := call("", "POST", "/v1/factory-access", api.FactoryAccessRequest{Link: made.Link.URL, Verb: "sign_up", Username: "abir", Password: pwAbir}, &joined); code != 200 || joined.State != "joined" {
		t.Fatalf("abir joins = %d %+v", code, joined)
	}
	abir := joined.AccessToken

	var ws api.Workstream
	if code := call(santosh, "POST", "/v1/workstreams", api.WorkstreamNew{Name: "Webhook reliability"}, &ws); code != 201 {
		t.Fatalf("create = %d", code)
	}
	path := "/v1/workstreams/" + ws.ID
	if code := call(abir, "GET", path, nil, nil); code != 404 && code != 403 {
		t.Fatalf("abir before the share = %d", code)
	}
	view := []factory.Share{{Kind: "person", ID: "abir", Role: "view"}}
	if code := call(santosh, "PUT", path+"/shares", api.WorkstreamSharesPut{Shares: &view, Rev: &ws.Rev}, nil); code != 200 {
		t.Fatalf("share = %d", code)
	}
	read := func(who, token string) {
		t.Helper()
		var seen api.Workstream
		if code := call(token, "GET", path, nil, &seen); code != 200 || seen.Name != "Webhook reliability" {
			t.Fatalf("%s reads = %d %+v", who, code, seen)
		}
	}
	read("santosh", santosh)
	read("abir", abir)
	if code := call(abir, "PATCH", path, map[string]any{"purpose": "mine"}, nil); code != 403 && code != 428 {
		t.Fatalf("abir's PATCH with view = %d", code)
	}

	// Both sign out, on the server, and come back with the password alone.
	for who, token := range map[string]string{"santosh": santosh, "abir": abir} {
		var me api.Me
		call(token, "GET", "/v1/me", nil, &me)
		if code := call(token, "DELETE", "/v1/me/sessions/"+me.SessionID, nil, nil); code != 200 {
			t.Fatalf("%s signs out = %d", who, code)
		}
		if code := call(token, "GET", "/v1/me", nil, nil); code != 401 {
			t.Fatalf("%s after sign-out = %d", who, code)
		}
	}
	for who, pw := range map[string]string{"santosh": pwOperator, "abir": pwAbir} {
		var signed api.PasswordSignedIn
		if code := call("", "POST", "/v1/auth/password", api.PasswordSignIn{Username: who, Password: pw}, &signed); code != 200 || signed.Person.Handle != who {
			t.Fatalf("%s signs in again = %d", who, code)
		}
		read(who, signed.AccessToken)
	}
}

// ── the leak test ──

// A KNOWN PASSWORD IS PUSHED THROUGH EVERY DOOR: sign-up (refused and
// accepted), sign-in (wrong and right), change (wrong current and right),
// reset; then every file the server wrote, every record row, every answer and
// every log line is read for it.
func TestAPasswordReachesNoSinkOnTheServer(t *testing.T) {
	const leak = "zebra-LEAK-7f3c-passphrase"
	var logs bytes.Buffer
	saved := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(saved) })
	o := newPWOrg(t)
	var answers bytes.Buffer
	keep := func(w *httptest.ResponseRecorder) {
		for k, v := range w.Header() {
			answers.WriteString(k + ": " + strings.Join(v, ",") + "\n")
		}
		answers.Write(w.Body.Bytes())
	}
	link := o.operatorLink()
	_, w := o.access(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: "-bad", Password: leak})
	keep(w)
	_, w = o.access(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: "santosh", Password: leak + " contains santosh"})
	keep(w)
	out, w := o.access(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: "santosh", Password: leak})
	keep(w)
	operator := out.AccessToken
	keep(o.signIn("santosh", leak+"x", ""))
	keep(o.signIn("nobody", leak, ""))
	keep(o.signIn("santosh", leak, ""))
	keep(o.do("POST", "/v1/auth/password", "", `{"username":"santosh","password":"`+leak+`","x":1}`, ""))
	keep(o.do("POST", "/v1/auth/password", "", `{"username":"santosh","password":"`+leak+`"`, ""))
	keep(o.do("POST", "/v1/me/password", operator, api.PasswordChange{Current: leak + "no", New: leak + "2"}, ""))
	keep(o.do("POST", "/v1/me/password", operator, api.PasswordChange{Current: leak, New: leak + "2"}, ""))
	rw := o.do("POST", "/v1/access/links", operator, api.CreateAccessLink{Kind: "reset", Person: "santosh"}, "")
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	_ = json.Unmarshal(rw.Body.Bytes(), &made)
	_, w = o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "reset", Password: leak + "3"})
	keep(w)
	keep(o.do("GET", "/v1/activity?limit=200", o.token("santosh", leak+"3"), nil, ""))

	for _, sink := range []struct{ name, text string }{{"answers", answers.String()}, {"log", logs.String()}} {
		if strings.Contains(sink.text, leak) {
			t.Errorf("the password reached the %s", sink.name)
		}
	}
	events, _ := o.cfg.Record.Since(context.Background(), time.Time{})
	raw, _ := json.Marshal(events)
	if strings.Contains(string(raw), leak) || len(events) == 0 {
		t.Errorf("the record (%d rows) carries the password", len(events))
	}
	_ = filepath.WalkDir(o.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, _ := os.ReadFile(path)
		if bytes.Contains(data, []byte(leak)) {
			t.Errorf("the password is on disk in %s", path)
		}
		return nil
	})
}

// trustedLoopback trusts this machine as a proxy.
func trustedLoopback(t *testing.T) []netip.Prefix {
	t.Helper()
	return []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
}

// A PASSWORD PERSON IS JUDGED EXACTLY LIKE A GITHUB PERSON on everything that
// keys off a handle: mia signs in with GitHub (and can see no repository),
// abir with a password, and every answer about a workstream, a private one,
// and the queue is the same for both.
func TestAPasswordPersonIsJudgedLikeAGitHubPerson(t *testing.T) {
	o := newPWOrg(t)
	santosh := o.signUp(o.operatorLink(), "santosh", pwOperator)
	abir := o.signUp(o.invite(santosh), "abir", pwAbir)
	g := &fakeGitHub{t: t, login: "mia", repoStatus: 404}
	o.h.config.GitHubClientID, o.h.config.GitHubHTTPClient = "client", g.client()
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Add(o.cfg.TokensPath, row); err != nil {
		t.Fatal(err)
	}
	mia := signInHandle(t, o.h, g, &o.now, "mia", row.Token)
	var ws, private api.Workstream
	if w := o.do("POST", "/v1/workstreams", santosh, api.WorkstreamNew{Name: "Launch"}, ""); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &ws) != nil {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/workstreams", santosh, api.WorkstreamNew{Name: "Private", Private: true}, ""); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &private) != nil {
		t.Fatalf("private = %d", w.Code)
	}
	view := []factory.Share{{Kind: "person", ID: "abir", Role: "view"}, {Kind: "person", ID: "mia", Role: "view"}}
	if w := o.do("PUT", "/v1/workstreams/"+ws.ID+"/shares", santosh, api.WorkstreamSharesPut{Shares: &view, Rev: &ws.Rev}, ""); w.Code != 200 {
		t.Fatalf("share = %d %s", w.Code, w.Body)
	}
	answer := func(token, method, path string, body any) string {
		w := o.do(method, path, token, body, "")
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		delete(v, "request_id")
		// Each person's own handle is the one thing that should differ.
		raw, _ := json.Marshal(v)
		text := strings.NewReplacer("abir", "PERSON", "mia", "PERSON").Replace(string(raw))
		return w.Result().Status + " " + text
	}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/v1/workstreams/" + ws.ID, nil},
		{"PATCH", "/v1/workstreams/" + ws.ID, map[string]any{"purpose": "mine"}},
		{"GET", "/v1/workstreams/" + private.ID, nil},
		{"DELETE", "/v1/workstreams/" + ws.ID, nil},
		{"GET", "/v1/queue", nil},
	} {
		a, m := answer(abir, c.method, c.path, c.body), answer(mia, c.method, c.path, c.body)
		if c.path == "/v1/workstreams/"+ws.ID && c.method == "GET" && !strings.HasPrefix(a, "200") {
			t.Fatalf("a shared read = %s", a)
		}
		if a != m {
			t.Errorf("%s %s:\n password %s\n github   %s", c.method, c.path, a, m)
		}
	}
}
