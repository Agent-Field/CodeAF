package serve

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve/passkeytest"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// softKey wraps passkeytest.Key with the test's own failure.
type softKey struct{ *passkeytest.Key }

func pkB64(b []byte) string { return passkeytest.B64(b) }

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64url %q: %v", s, err)
	}
	return b
}

func newSoftKey(t *testing.T) *softKey {
	t.Helper()
	k, err := passkeytest.New()
	if err != nil {
		t.Fatal(err)
	}
	return &softKey{k}
}

func (k *softKey) create(t *testing.T, options json.RawMessage, origin string) json.RawMessage {
	t.Helper()
	out, err := k.Create(options, origin)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (k *softKey) get(t *testing.T, options json.RawMessage, origin string) json.RawMessage {
	t.Helper()
	out, err := k.Get(options, origin)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const (
	flagUV = passkeytest.FlagUV
	flagBE = passkeytest.FlagBE
	flagBS = passkeytest.FlagBS
)

// ── the engine's side of a flow ─────────────────────────────────────────────

const pkOrigin = "https://org.test"

type pkFlow struct {
	id, url  string
	verifier string
}

// pkBrowser is one browser: the passkey-page cookie its host set, if any.
type pkBrowser struct{ cookies sync.Map }

// theBrowser is the person's own browser, which every helper uses unless a
// test names another.
var theBrowser = &pkBrowser{}

// returnKeys is the loopback key each started flow's "engine" holds.
var returnKeys sync.Map

func newVerifier() (verifier, challenge string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	sum := sha256.Sum256(b)
	return pkB64(b), pkB64(sum[:])
}

func (o *pwOrg) pkStart(token string, body api.PasskeyStart) (pkFlow, *httptest.ResponseRecorder) {
	o.t.Helper()
	verifier, challenge := newVerifier()
	if body.CodeChallenge == "" {
		body.CodeChallenge = challenge
	}
	if body.ReturnPort == 0 && body.ReturnPath == "" {
		path, _ := newVerifier()
		key, _ := newVerifier()
		body.ReturnPort, body.ReturnPath, body.ReturnKey = 49152, path, key
	}
	w := o.do("POST", "/v1/auth/passkey/start", token, body, "")
	var out api.PasskeyStarted
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			o.t.Fatal(err)
		}
		returnKeys.Store(out.Flow, body.ReturnKey)
	}
	return pkFlow{id: out.Flow, url: out.URL, verifier: verifier}, w
}

func (o *pwOrg) mustStart(token string, body api.PasskeyStart) pkFlow {
	o.t.Helper()
	f, w := o.pkStart(token, body)
	if w.Code != http.StatusCreated || f.id == "" {
		o.t.Fatalf("start %+v = %d %s", body, w.Code, w.Body)
	}
	return f
}

// page posts to the ceremony page's JSON as the person's browser would from
// origin.
func (o *pwOrg) page(flow, step, origin string, body any) *httptest.ResponseRecorder {
	o.t.Helper()
	return o.pageIn(theBrowser, flow, step, origin, body)
}

// pageIn is a page post from browser b, which keeps the cookie it is given.
func (o *pwOrg) pageIn(b *pkBrowser, flow, step, origin string, body any) *httptest.ResponseRecorder {
	o.t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/passkey/"+flow+"/"+step, strings.NewReader(string(data)))
	r.RemoteAddr = "127.0.0.1:4100"
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_1) Safari")
	return o.browse(b, flow, r)
}

// browse sends r with b's passkey cookie, and keeps any cookie answered,
// as a browser keeps a __Host- cookie: only Secure, Path=/, no Domain.
func (o *pwOrg) browse(b *pkBrowser, flow string, r *http.Request) *httptest.ResponseRecorder {
	if b != nil {
		if c, ok := b.cookies.Load(passkeyCookie); ok {
			r.AddCookie(c.(*http.Cookie))
		}
	}
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name != passkeyCookie || b == nil {
			continue
		}
		if !strings.HasPrefix(c.Name, "__Host-") || c.Path != "/" || c.Domain != "" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
			o.t.Fatalf("the browser cookie = %+v", c)
		}
		b.cookies.Store(passkeyCookie, &http.Cookie{Name: c.Name, Value: c.Value})
	}
	return w
}

// handOver is browser b's navigation to a finished flow's hand-over: the
// loopback url with the code, or "" when the server sent it to the page.
func (o *pwOrg) handOver(b *pkBrowser, flow string) string {
	o.t.Helper()
	r := httptest.NewRequest("GET", "/passkey/"+flow+"/go", nil)
	r.RemoteAddr = "127.0.0.1:4100"
	w := o.browse(b, flow, r)
	if w.Code != http.StatusSeeOther {
		o.t.Fatalf("hand-over = %d %s", w.Code, w.Body)
	}
	loc := w.Header().Get("Location")
	if loc == "/passkey/"+flow {
		return ""
	}
	return loc
}

func (o *pwOrg) options(flow string, body any) json.RawMessage {
	o.t.Helper()
	w := o.page(flow, "options", pkOrigin, body)
	if w.Code != 200 {
		o.t.Fatalf("options = %d %s", w.Code, w.Body)
	}
	return w.Body.Bytes()
}

// loopbackCodes is the code each finished flow sent "the browser" to the
// engine's loopback with, as the engine's listener would have taken it.
var loopbackCodes sync.Map

// finish hands the page's credential back from the person's browser, which
// then follows the hand-over as the page does; the code it lands on the
// loopback with is kept for poll, as the engine's listener would take it.
func (o *pwOrg) finish(flow string, cred json.RawMessage, name string) *httptest.ResponseRecorder {
	o.t.Helper()
	w := o.page(flow, "finish", pkOrigin, map[string]any{"credential": cred, "name": name})
	var done struct {
		Next     string `json:"next"`
		Redirect string `json:"redirect"`
	}
	if w.Code == 200 && json.Unmarshal(w.Body.Bytes(), &done) == nil && done.Next != "" {
		if done.Redirect != "" || strings.Contains(w.Body.String(), "?code=") || strings.Contains(w.Body.String(), `"code"`) {
			o.t.Fatalf("a finish answer carries the code: %s", w.Body)
		}
		if done.Next != "/passkey/"+flow+"/go" {
			o.t.Fatalf("next = %q", done.Next)
		}
		loc := o.handOver(theBrowser, flow)
		u, err := url.Parse(loc)
		if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:49152" {
			o.t.Fatalf("hand-over location = %q", loc)
		}
		code := u.Query().Get("code")
		if k, ok := returnKeys.Load(flow); ok && !loopbackCodeIsOurs(k.(string), code) {
			o.t.Fatalf("the code does not carry the engine's mark: %q", code)
		}
		loopbackCodes.Store(flow, code)
	}
	return w
}

// loopbackCodeIsOurs is the engine listener's own check of a code.
func loopbackCodeIsOurs(key, code string) bool {
	k, _ := base64.RawURLEncoding.DecodeString(key)
	c, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil || len(c) != 32 {
		return false
	}
	mac := hmac.New(sha256.New, k)
	mac.Write(c[:16])
	return hmac.Equal(mac.Sum(nil)[:16], c[16:])
}

// poll is the engine's poll with its verifier and the loopback code.
func (o *pwOrg) poll(f pkFlow) api.PasskeyPolled {
	o.t.Helper()
	code, _ := loopbackCodes.Load(f.id)
	c, _ := code.(string)
	return o.pollWith(f.id, f.verifier, c)
}

func (o *pwOrg) pollWith(flow, verifier, code string) api.PasskeyPolled {
	o.t.Helper()
	w := o.do("POST", "/v1/auth/passkey/poll", "", api.PasskeyPoll{Flow: flow, Verifier: verifier, Code: code}, "")
	var out api.PasskeyPolled
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		o.t.Fatalf("poll = %d %s", w.Code, w.Body)
	}
	return out
}

// joinByPasskey is a whole join: start, choose a username, create, poll.
func (o *pwOrg) joinByPasskey(link, username string, key *softKey) string {
	o.t.Helper()
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: link})
	opt := o.options(f.id, map[string]string{"username": username})
	if w := o.finish(f.id, key.create(o.t, opt, pkOrigin), ""); w.Code != 200 {
		o.t.Fatalf("finish join = %d %s", w.Code, w.Body)
	}
	got := o.poll(f)
	if got.State != api.PasskeyDone || got.AccessToken == "" || got.Person == nil || got.Person.Handle != username {
		o.t.Fatalf("poll join = %+v", got)
	}
	return got.AccessToken
}

func (o *pwOrg) signInByPasskey(key *softKey) (api.PasskeyPolled, *httptest.ResponseRecorder) {
	o.t.Helper()
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	opt := o.options(f.id, map[string]string{})
	w := o.finish(f.id, key.get(o.t, opt, pkOrigin), "")
	if w.Code != 200 {
		return api.PasskeyPolled{}, w
	}
	return o.poll(f), w
}

// confirm is a step-up for one action: a confirm ceremony with one of the
// person's own passkeys, polled with the code, which lets session do that
// action once.
func (o *pwOrg) confirm(session string, key *softKey, action api.StepUpAction) {
	o.t.Helper()
	f := o.mustStart(session, api.PasskeyStart{Purpose: api.PasskeyConfirm, Action: &action, Device: "santosh-mac"})
	if w := o.finish(f.id, key.get(o.t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		o.t.Fatalf("confirm finish = %d %s", w.Code, w.Body)
	}
	if got := o.poll(f); got.State != api.PasskeyDone || got.AccessToken != "" {
		o.t.Fatalf("confirm poll = %+v", got)
	}
}

// pwConfirm is a step-up for one action with the current password.
func (o *pwOrg) pwConfirm(session, password string, action api.StepUpAction) {
	o.t.Helper()
	if w := o.do("POST", "/v1/me/password/confirm", session, api.PasswordConfirm{Password: password, Action: action}, ""); w.Code != http.StatusNoContent {
		o.t.Fatalf("password confirm = %d %s", w.Code, w.Body)
	}
}

// addPasskey confirms with key, then adds next, for session.
func (o *pwOrg) addPasskey(session string, key, next *softKey) {
	o.t.Helper()
	o.confirm(session, key, toAdd)
	f := o.mustStart(session, api.PasskeyStart{Purpose: api.PasskeyAdd})
	if w := o.finish(f.id, next.create(o.t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		o.t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	if got := o.poll(f); got.State != api.PasskeyDone {
		o.t.Fatalf("add poll = %+v", got)
	}
}

var (
	toAdd         = api.StepUpAction{Kind: api.StepUpAddPasskey}
	toSetPassword = api.StepUpAction{Kind: api.StepUpSetPassword}
)

func toRemove(k *softKey) api.StepUpAction {
	return api.StepUpAction{Kind: api.StepUpRemovePasskey, Passkey: pkB64(k.ID)}
}

func (o *pwOrg) accountOf(handle string) tokens.Account {
	o.t.Helper()
	accounts, err := tokens.LoadAccounts(o.cfg.TokensPath)
	if err != nil {
		o.t.Fatal(err)
	}
	acct, _ := accounts.Find(handle)
	return acct
}

// ── the tests ───────────────────────────────────────────────────────────────

func TestPasskeysAreOfferedFirstOnlyOverHTTPSWithADNSName(t *testing.T) {
	for _, tc := range []struct {
		address string
		want    string
	}{
		{"https://org.test", `["passkey","password"]`},
		{"https://org.test:8443", `["passkey","password"]`},
		{"https://localhost:8443", `["passkey","password"]`},
		{"http://org.test", `["password"]`},
		{"https://10.0.0.5", `["password"]`},
		{"https://[::1]:8443", `["password"]`},
		{"http://localhost:8080", `["password"]`},
	} {
		o := newPWOrg(t)
		o.cfg.Address = tc.address
		o.h = New(o.h.seam, o.cfg)
		t.Cleanup(o.h.Close)
		w := o.do("GET", "/v1/about", "", nil, "")
		var about struct {
			SignIn []string `json:"sign_in"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &about)
		got, _ := json.Marshal(about.SignIn)
		if string(got) != tc.want {
			t.Errorf("%s: sign_in = %s, want %s", tc.address, got, tc.want)
		}
		if tc.want == `["password"]` {
			// Absent, not broken: the routes are not there.
			if _, w := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeySignIn}); w.Code != http.StatusNotFound {
				t.Errorf("%s: start = %d, want 404", tc.address, w.Code)
			}
		}
	}
}

func TestJoinByPasskeySignOutAndSignInAgain(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	operator := o.joinByPasskey(o.operatorLink(), "santosh", key)
	me, code := o.me(operator)
	if code != 200 || me.Handle != "santosh" || !me.Operator {
		t.Fatalf("me = %d %+v", code, me)
	}
	acct := o.accountOf("santosh")
	if acct.Hash != "" || len(acct.Passkeys) != 1 || acct.UserID == "" {
		t.Fatalf("account = %+v", acct)
	}
	pk := acct.Passkeys[0]
	if pk.Name != "Mac passkey" || !pk.BackupEligible || pk.PublicKey == "" || pk.ID != pkB64(key.ID) {
		t.Fatalf("stored passkey = %+v", pk)
	}
	// The user handle is opaque: never the username.
	if strings.Contains(string(unb64(t, acct.UserID)), "santosh") || len(unb64(t, acct.UserID)) != 32 {
		t.Fatal("the WebAuthn user handle carries the username")
	}
	// A password cannot sign a passkey person in.
	if w := o.signIn("santosh", "", ""); w.Code == 200 {
		t.Fatal("an empty password signed a passkey person in")
	}
	got, w := o.signInByPasskey(key)
	if w.Code != 200 || got.State != api.PasskeyDone || got.AccessToken == "" || got.Me == nil || got.Me.Handle != "santosh" {
		t.Fatalf("sign in = %d %+v %s", w.Code, got, w.Body)
	}
	if again := o.accountOf("santosh").Passkeys[0]; again.SignCount != 1 || again.LastUsed.IsZero() {
		t.Fatalf("use was not recorded: %+v", again)
	}
}

func TestTheCeremonyRequiresUserVerificationAndResidentKeysAndLeavesAttachmentOpen(t *testing.T) {
	o := newPWOrg(t)
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: o.operatorLink()})
	opt := o.options(f.id, map[string]string{"username": "santosh"})
	var shape struct {
		PublicKey struct {
			RP struct {
				ID string `json:"id"`
			} `json:"rp"`
			AuthenticatorSelection map[string]any `json:"authenticatorSelection"`
			Attestation            string         `json:"attestation"`
			Challenge              string         `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(opt, &shape); err != nil {
		t.Fatal(err)
	}
	sel := shape.PublicKey.AuthenticatorSelection
	if shape.PublicKey.RP.ID != "org.test" || sel["residentKey"] != "required" || sel["userVerification"] != "required" {
		t.Fatalf("options = %s", opt)
	}
	if _, set := sel["authenticatorAttachment"]; set {
		t.Fatalf("authenticatorAttachment is set; it must stay open: %s", opt)
	}
	if shape.PublicKey.Attestation != "none" || len(unb64(t, shape.PublicKey.Challenge)) != 32 {
		t.Fatalf("attestation or challenge = %s", opt)
	}
	// An authenticator that did not verify the person is refused.
	key := newSoftKey(t)
	key.Flags &^= flagUV
	if w := o.finish(f.id, key.create(t, opt, pkOrigin), ""); w.Code != http.StatusBadRequest {
		t.Fatalf("finish without UV = %d %s", w.Code, w.Body)
	}
	if acct := o.accountOf("santosh"); acct.Handle != "" {
		t.Fatal("an unverified passkey made an account")
	}
}

func TestCeremonyRefusals(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", key)

	t.Run("wrong origin", func(t *testing.T) {
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		if w := o.page(f.id, "options", "https://evil.test", map[string]string{}); w.Code != http.StatusForbidden {
			t.Fatalf("options from another origin = %d", w.Code)
		}
		if w := o.page(f.id, "options", "", map[string]string{}); w.Code != http.StatusForbidden {
			t.Fatalf("options with no origin = %d", w.Code)
		}
		// A response signed for another origin is refused by the library.
		opt := o.options(f.id, map[string]string{})
		if w := o.finish(f.id, key.get(t, opt, "https://evil.test"), ""); w.Code != http.StatusBadRequest {
			t.Fatalf("finish for another origin = %d %s", w.Code, w.Body)
		}
	})
	t.Run("replayed challenge", func(t *testing.T) {
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		opt := o.options(f.id, map[string]string{})
		cred := key.get(t, opt, pkOrigin)
		if w := o.finish(f.id, cred, ""); w.Code != 200 {
			t.Fatalf("first finish = %d %s", w.Code, w.Body)
		}
		if w := o.finish(f.id, cred, ""); w.Code == 200 {
			t.Fatal("the same response was accepted twice")
		}
		// And on a second flow, the old response meets a new challenge.
		g := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		o.options(g.id, map[string]string{})
		if w := o.finish(g.id, cred, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("replay on a new flow = %d %s", w.Code, w.Body)
		}
	})
	t.Run("expired flow", func(t *testing.T) {
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		opt := o.options(f.id, map[string]string{})
		o.advance(passkeyFlowLife + 1)
		if w := o.finish(f.id, key.get(t, opt, pkOrigin), ""); w.Code != http.StatusGone {
			t.Fatalf("finish after the flow ended = %d", w.Code)
		}
		if got := o.poll(f); got.State != api.PasskeyExpired {
			t.Fatalf("poll after the flow ended = %+v", got)
		}
	})
	t.Run("expired challenge", func(t *testing.T) {
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		opt := o.options(f.id, map[string]string{})
		o.advance(passkeyChallengeLife + 1)
		if w := o.finish(f.id, key.get(t, opt, pkOrigin), ""); w.Code != http.StatusConflict {
			t.Fatalf("finish after the challenge ended = %d", w.Code)
		}
	})
	t.Run("missing and wrong verifier", func(t *testing.T) {
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		opt := o.options(f.id, map[string]string{})
		if w := o.finish(f.id, key.get(t, opt, pkOrigin), ""); w.Code != 200 {
			t.Fatalf("finish = %d", w.Code)
		}
		if w := o.do("POST", "/v1/auth/passkey/poll", "", map[string]string{"flow": f.id}, ""); !strings.Contains(w.Body.String(), `"refused"`) || strings.Contains(w.Body.String(), "access_token") {
			t.Fatalf("poll with no verifier = %d %s", w.Code, w.Body)
		}
		// L2: one wrong verifier is counted, not let end the engine's flow;
		// the right one still gets the session.
		if got := o.poll(f); got.State != api.PasskeyDone || got.AccessToken == "" {
			t.Fatalf("poll after one wrong verifier = %+v", got)
		}
		// passkeyWrongMost wrong ones do end it.
		h := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		_ = o.finish(h.id, key.get(t, o.options(h.id, map[string]string{}), pkOrigin), "")
		for range passkeyWrongMost {
			guess, _ := newVerifier()
			if got := o.pollWith(h.id, guess, ""); got.State != api.PasskeyRefused || got.AccessToken != "" {
				t.Fatalf("a guessed verifier = %+v", got)
			}
		}
		if got := o.poll(h); got.State != api.PasskeyExpired || got.AccessToken != "" {
			t.Fatalf("poll after %d wrong verifiers = %+v", passkeyWrongMost, got)
		}
		g := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		opt = o.options(g.id, map[string]string{})
		_ = o.finish(g.id, key.get(t, opt, pkOrigin), "")
		other, _ := newVerifier()
		got := o.poll(pkFlow{id: g.id, verifier: other})
		if got.State != api.PasskeyRefused || got.AccessToken != "" {
			t.Fatalf("poll with a wrong verifier = %+v", got)
		}
	})
	t.Run("unknown passkey", func(t *testing.T) {
		stranger := newSoftKey(t)
		stranger.UserHandle = make([]byte, 32)
		if _, w := o.signInByPasskey(stranger); w.Code != http.StatusBadRequest {
			t.Fatalf("an unknown passkey = %d %s", w.Code, w.Body)
		}
	})
	t.Run("credential owned by someone else", func(t *testing.T) {
		operator := o.token2(key)
		abir := newSoftKey(t)
		abir.ID = key.ID // the same credential id as santosh's
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: o.invite(operator)})
		opt := o.options(f.id, map[string]string{"username": "abir"})
		if w := o.finish(f.id, abir.create(t, opt, pkOrigin), ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "passkey_taken") {
			t.Fatalf("a credential id held by another person = %d %s", w.Code, w.Body)
		}
		if acct := o.accountOf("abir"); acct.Handle != "" {
			t.Fatal("the refused join made an account")
		}
	})
	t.Run("a username already taken is refused before a passkey is made", func(t *testing.T) {
		operator := o.token2(key)
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: o.invite(operator)})
		if w := o.page(f.id, "options", pkOrigin, map[string]string{"username": "santosh"}); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), reasonUsernameTaken) {
			t.Fatalf("options for a taken name = %d %s", w.Code, w.Body)
		}
	})
	t.Run("a stolen url cannot mint a session", func(t *testing.T) {
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		opt := o.options(f.id, map[string]string{})
		_ = o.finish(f.id, key.get(t, opt, pkOrigin), "")
		// The page itself never carries the session.
		r := httptest.NewRequest("GET", "/passkey/"+f.id, nil)
		r.RemoteAddr = "127.0.0.1:4100"
		w := httptest.NewRecorder()
		o.h.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), "session_") || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("the page carried a session")
		}
	})
}

// token2 signs santosh in by passkey and answers the session.
func (o *pwOrg) token2(key *softKey) string {
	o.t.Helper()
	got, w := o.signInByPasskey(key)
	if w.Code != 200 || got.AccessToken == "" {
		o.t.Fatalf("sign in = %d %s", w.Code, w.Body)
	}
	return got.AccessToken
}

func TestPollAnswersTheSessionExactlyOnce(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", key)
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if got := o.poll(f); got.State != api.PasskeyPending {
		t.Fatalf("poll before the ceremony = %+v", got)
	}
	opt := o.options(f.id, map[string]string{})
	_ = o.finish(f.id, key.get(t, opt, pkOrigin), "")
	var tokens atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := loopbackCodes.Load(f.id)
			w := o.do("POST", "/v1/auth/passkey/poll", "", api.PasskeyPoll{Flow: f.id, Verifier: f.verifier, Code: code.(string)}, "")
			var out api.PasskeyPolled
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			if out.AccessToken != "" {
				tokens.Add(1)
			}
		}()
	}
	wg.Wait()
	if tokens.Load() != 1 {
		t.Fatalf("%d polls answered a session, want exactly 1", tokens.Load())
	}
}

// A way in that changes between the ceremony and the engine's poll signs
// nobody in: the session would be born at a generation already ended.
func TestAWayInChangedBeforeThePollSignsNobodyIn(t *testing.T) {
	o := newPWOrg(t)
	first, second := newSoftKey(t), newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", first)
	here := o.token2(first)
	o.confirm(here, first, toAdd)
	add := o.mustStart(here, api.PasskeyStart{Purpose: api.PasskeyAdd})
	if w := o.finish(add.id, second.create(t, o.options(add.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	_ = o.poll(add)

	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if w := o.finish(f.id, first.get(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("finish = %d %s", w.Code, w.Body)
	}
	o.confirm(here, second, toRemove(second))
	if w := o.do("POST", "/v1/me/passkeys/remove", here, api.PasskeyRemove{ID: pkB64(second.ID)}, ""); w.Code != http.StatusNoContent {
		t.Fatalf("remove = %d %s", w.Code, w.Body)
	}
	if got := o.poll(f); got.State != api.PasskeyRefused || got.AccessToken != "" {
		t.Fatalf("poll after a way in changed = %+v", got)
	}
}

// A link made for one GitHub login is that person's: it neither offers nor
// takes a passkey, as it neither offers nor takes a password.
func TestALinkForAGitHubLoginTakesNoPasskey(t *testing.T) {
	o := newPWOrg(t)
	op := o.joinByPasskey(o.operatorLink(), "santosh", newSoftKey(t))
	w := o.do("POST", "/v1/access/links", op, api.CreateAccessLink{Login: "octocat"}, "")
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &made) != nil {
		t.Fatalf("invite = %d %s", w.Code, w.Body)
	}
	check, _ := o.access(api.FactoryAccessRequest{Link: made.Link.URL, Verb: "check"})
	if slices.Contains(check.Factory.SignIn, signInPasskey) {
		t.Fatalf("a GitHub link offers a passkey: %v", check.Factory.SignIn)
	}
	_, challenge := newVerifier()
	_, rw := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: made.Link.URL, CodeChallenge: challenge})
	if rw.Code != http.StatusConflict || !strings.Contains(rw.Body.String(), reasonGitHubLink) {
		t.Fatalf("a passkey join through a GitHub link = %d %s", rw.Code, rw.Body)
	}
}

// A passkey sign-in says so: the poll's me and /v1/me both answer
// sign_in_kind "passkey" and github_signed_in false (X1 P4). A password
// sign-in by the same person still says "password".
func TestAPasskeySignInIsReportedAsAPasskeyNotGitHub(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	joined := o.joinByPasskey(o.operatorLink(), "santosh", key)
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if w := o.finish(f.id, key.get(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("finish = %d %s", w.Code, w.Body)
	}
	code, _ := loopbackCodes.Load(f.id)
	w := o.do("POST", "/v1/auth/passkey/poll", "", api.PasskeyPoll{Flow: f.id, Verifier: f.verifier, Code: code.(string)}, "")
	var polled struct {
		AccessToken string         `json:"access_token"`
		Me          map[string]any `json:"me"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &polled) != nil || polled.AccessToken == "" {
		t.Fatalf("poll = %d %s", w.Code, w.Body)
	}
	if polled.Me["sign_in_kind"] != "passkey" || polled.Me["github_signed_in"] != false {
		t.Fatalf("poll me: sign_in_kind = %v, github_signed_in = %v", polled.Me["sign_in_kind"], polled.Me["github_signed_in"])
	}
	for name, token := range map[string]string{"sign-in": polled.AccessToken, "join": joined} {
		me := o.do("GET", "/v1/me", token, nil, "")
		var raw map[string]any
		if me.Code != 200 || json.Unmarshal(me.Body.Bytes(), &raw) != nil {
			t.Fatalf("%s /v1/me = %d %s", name, me.Code, me.Body)
		}
		if raw["sign_in_kind"] != "passkey" || raw["github_signed_in"] != false {
			t.Errorf("%s /v1/me: sign_in_kind = %v, github_signed_in = %v", name, raw["sign_in_kind"], raw["github_signed_in"])
		}
	}
	// The kind survives a server restart: it is kept with the session.
	restart(o)
	me := o.do("GET", "/v1/me", polled.AccessToken, nil, "")
	if !strings.Contains(me.Body.String(), `"sign_in_kind":"passkey"`) {
		t.Fatalf("after a restart /v1/me = %d %s", me.Code, me.Body)
	}
	// The same person signing in with a password is a password sign-in. A
	// restart forgot the session's proof, so it confirms first.
	o.confirm(polled.AccessToken, key, toSetPassword)
	if w := o.do("POST", "/v1/me/password", polled.AccessToken, api.PasswordChange{Current: "", New: pwOperator}, ""); w.Code/100 != 2 {
		t.Fatalf("set a first password = %d %s", w.Code, w.Body)
	}
	me = o.do("GET", "/v1/me", o.token("santosh", pwOperator), nil, "")
	if !strings.Contains(me.Body.String(), `"sign_in_kind":"password"`) || strings.Contains(me.Body.String(), `"github_signed_in":true`) {
		t.Fatalf("password /v1/me = %s", me.Body)
	}
}

// A passkey person's password change pays E9's per-session bucket (F1):
// wrong "current" passwords from one session run dry after userFailures,
// from any address, while another session of the same person is not held up.
func TestAPasskeySessionsPasswordTriesPayThePerSessionBucket(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", key)
	guessing, other := o.token2(key), o.token2(key)
	o.confirm(guessing, key, toSetPassword)
	o.confirm(other, key, toSetPassword)
	for i := range userFailures {
		remote := fmt.Sprintf("198.51.100.%d:1", i+1)
		if w := tlsChange(o, guessing, "not empty", pwOperator, remote); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "current_wrong") {
			t.Fatalf("try %d = %d %s", i, w.Code, w.Body)
		}
	}
	if w := tlsChange(o, guessing, "", pwOperator, "198.51.100.200:1"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("a dry session's try = %d %s", w.Code, w.Body)
	}
	if w := tlsChange(o, other, "", pwOperator, "198.51.100.201:1"); w.Code/100 != 2 {
		t.Fatalf("another session = %d %s", w.Code, w.Body)
	}
}

func TestTwoPasskeyJoinsRacingForOneLinkMakeOneAccount(t *testing.T) {
	o := newPWOrg(t)
	operator := o.joinByPasskey(o.operatorLink(), "santosh", newSoftKey(t))
	link := o.invite(operator)
	type ready struct {
		f    pkFlow
		cred json.RawMessage
	}
	var both []ready
	for _, name := range []string{"abir", "carol"} {
		f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: link})
		opt := o.options(f.id, map[string]string{"username": name})
		both = append(both, ready{f, newSoftKey(t).create(t, opt, pkOrigin)})
	}
	// Both ceremonies verify; the link is spent at the code-bearing poll,
	// where the two race.
	for _, r := range both {
		if w := o.finish(r.f.id, r.cred, ""); w.Code != 200 {
			t.Fatalf("finish = %d %s", w.Code, w.Body)
		}
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, r := range both {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := o.poll(r.f); got.AccessToken != "" {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d joins won one link, want 1", wins.Load())
	}
	accounts, _ := tokens.LoadAccounts(o.cfg.TokensPath)
	if len(accounts.Accounts) != 2 {
		t.Fatalf("accounts = %d, want the operator and one person", len(accounts.Accounts))
	}
}

func TestAddAPasskeyThenRemoveItEndsOtherSessions(t *testing.T) {
	o := newPWOrg(t)
	first := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", first)
	here := o.token2(first)
	elsewhere := o.token2(first)
	o.confirm(here, first, toAdd)

	// Add a second passkey, named, while signed in.
	f := o.mustStart(here, api.PasskeyStart{Purpose: api.PasskeyAdd})
	opt := o.options(f.id, map[string]string{})
	var excluded struct {
		PublicKey struct {
			Exclude []struct {
				ID string `json:"id"`
			} `json:"excludeCredentials"`
		} `json:"publicKey"`
	}
	_ = json.Unmarshal(opt, &excluded)
	if len(excluded.PublicKey.Exclude) != 1 || excluded.PublicKey.Exclude[0].ID != pkB64(first.ID) {
		t.Fatalf("the person's own passkey is not excluded: %s", opt)
	}
	second := newSoftKey(t)
	second.Flags &^= flagBE | flagBS // a security key: not synced
	if w := o.finish(f.id, second.create(t, opt, pkOrigin), "YubiKey"); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	if got := o.poll(f); got.State != api.PasskeyDone || got.AccessToken != "" {
		t.Fatalf("an add's poll = %+v (it must carry no session)", got)
	}
	w := o.do("GET", "/v1/me/passkeys", here, nil, "")
	var list api.PasskeyList
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || len(list.Passkeys) != 2 || list.Password || list.Passkeys[1].Name != "YubiKey" || list.Passkeys[1].Synced {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	// Sign in with the second, then remove the first: every other session ends.
	o.token2(second)
	o.confirm(here, second, toRemove(first))
	if w := o.do("POST", "/v1/me/passkeys/remove", here, api.PasskeyRemove{ID: pkB64(first.ID)}, ""); w.Code != http.StatusNoContent {
		t.Fatalf("remove = %d %s", w.Code, w.Body)
	}
	if _, code := o.me(elsewhere); code == 200 {
		t.Fatal("a session outlived the removal of a passkey")
	}
	if _, code := o.me(here); code != 200 {
		t.Fatal("the session that removed the passkey ended")
	}
	if _, w := o.signInByPasskey(first); w.Code != http.StatusBadRequest {
		t.Fatalf("a removed passkey still signs in: %d", w.Code)
	}
	// The last way in cannot be removed.
	if w := o.do("POST", "/v1/me/passkeys/remove", here, api.PasskeyRemove{ID: pkB64(second.ID)}, ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "last_way_in") {
		t.Fatalf("remove the last = %d %s", w.Code, w.Body)
	}
	// With a password set, it can.
	o.confirm(here, second, toSetPassword)
	if w := o.do("POST", "/v1/me/password", here, api.PasswordChange{Current: "", New: pwOperator}, ""); w.Code/100 != 2 {
		t.Fatalf("set a first password = %d %s", w.Code, w.Body)
	}
	o.pwConfirm(here, pwOperator, toRemove(second))
	if w := o.do("POST", "/v1/me/passkeys/remove", here, api.PasskeyRemove{ID: pkB64(second.ID)}, ""); w.Code != http.StatusNoContent {
		t.Fatalf("remove with a password = %d %s", w.Code, w.Body)
	}
	o.token("santosh", pwOperator)
}

func TestAPasswordPersonAddsAPasskeyAndAGitHubSessionCannot(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	key := newSoftKey(t)
	o.pwConfirm(operator, pwOperator, toAdd)
	f := o.mustStart(operator, api.PasskeyStart{Purpose: api.PasskeyAdd})
	if w := o.finish(f.id, key.create(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	// Nothing is added until the engine's poll shows the loopback code.
	if len(o.accountOf("santosh").Passkeys) != 0 {
		t.Fatal("a passkey was added before the code-bearing poll")
	}
	if got := o.poll(f); got.State != api.PasskeyDone {
		t.Fatalf("add poll = %+v", got)
	}
	if got, w := o.signInByPasskey(key); w.Code != 200 || got.Me == nil || got.Me.Handle != "santosh" {
		t.Fatalf("passkey sign-in of a password person = %d %+v", w.Code, got)
	}
	// Adding needs a person's session; the host's own token is not one.
	if _, w := o.pkStart("host-secret", api.PasskeyStart{Purpose: api.PasskeyAdd}); w.Code != http.StatusNotFound {
		t.Fatalf("add with the host token = %d", w.Code)
	}
	if _, w := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeyAdd}); w.Code != http.StatusNotFound {
		t.Fatalf("add signed out = %d", w.Code)
	}
}

func TestAResetByPasskeyReplacesEveryWayInAndEndsSessions(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	abirSession := o.signUp(o.invite(operator), "abir", pwAbir)
	old := newSoftKey(t)
	o.pwConfirm(abirSession, pwAbir, toAdd)
	f := o.mustStart(abirSession, api.PasskeyStart{Purpose: api.PasskeyAdd})
	_ = o.finish(f.id, old.create(t, o.options(f.id, map[string]string{}), pkOrigin), "")

	w := o.do("POST", "/v1/access/links", operator, api.CreateAccessLink{Kind: "reset", Person: "abir"}, "")
	var made struct {
		Link api.AccessLink `json:"link"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &made) != nil {
		t.Fatalf("reset link = %d %s", w.Code, w.Body)
	}
	fresh := newSoftKey(t)
	g := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: made.Link.URL})
	if w := o.finish(g.id, fresh.create(t, o.options(g.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("reset finish = %d %s", w.Code, w.Body)
	}
	got := o.poll(g)
	if got.State != api.PasskeyDone || got.Person == nil || got.Person.Handle != "abir" {
		t.Fatalf("reset poll = %+v", got)
	}
	if _, code := o.me(abirSession); code == 200 {
		t.Fatal("a session outlived the reset")
	}
	if w := o.signIn("abir", pwAbir, ""); w.Code == 200 {
		t.Fatal("the old password outlived the reset")
	}
	if _, w := o.signInByPasskey(old); w.Code != http.StatusBadRequest {
		t.Fatal("the old passkey outlived the reset")
	}
	if _, w := o.signInByPasskey(fresh); w.Code != 200 {
		t.Fatal("the new passkey does not sign in")
	}
	// The link is used up.
	if _, w := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: made.Link.URL}); w.Code != http.StatusConflict {
		t.Fatalf("a used reset link = %d", w.Code)
	}
}

func TestStartsAreThrottledByAddressOnEntry(t *testing.T) {
	o := newPWOrg(t)
	for i := range passkeyStarts {
		f, w := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
		if w.Code != http.StatusCreated {
			t.Fatalf("start %d = %d", i, w.Code)
		}
		// Cancelled at once, so the address's cap on waiting flows is not
		// what stops it: the start is spent all the same.
		o.do("POST", "/v1/auth/passkey/cancel", "", api.PasskeyPoll{Flow: f.id, Verifier: f.verifier}, "")
	}
	if _, w := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeySignIn}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("start past the limit = %d", w.Code)
	}
	o.advance(throttleWindow)
	if _, w := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeySignIn}); w.Code != http.StatusCreated {
		t.Fatalf("start after the window = %d", w.Code)
	}
}

func TestPasskeyDoorsAreStrictJSONAndHTTPS(t *testing.T) {
	o := newPWOrg(t)
	_, challenge := newVerifier()
	if w := o.do("POST", "/v1/auth/passkey/start", "", `{"purpose":"sign_in","code_challenge":"`+challenge+`","extra":1}`, ""); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field = %d", w.Code)
	}
	if w := o.do("POST", "/v1/auth/passkey/start", "", api.PasskeyStart{Purpose: api.PasskeySignIn, CodeChallenge: "short"}, ""); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a short challenge = %d", w.Code)
	}
	if _, w := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeySignIn}); w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("start is cacheable")
	}
	// From another machine over plain http, refused.
	if w := o.do("POST", "/v1/auth/passkey/start", "", api.PasskeyStart{Purpose: api.PasskeySignIn, CodeChallenge: challenge}, "203.0.113.9:5000"); w.Code != http.StatusForbidden {
		t.Fatalf("plain http from afar = %d", w.Code)
	}
}

func TestTheCeremonyPageIsLockedDown(t *testing.T) {
	o := newPWOrg(t)
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	for _, path := range []string{"/passkey/" + f.id, "/passkey/page.js", "/passkey/page.css", "/passkey/nope"} {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:4100"
		w := httptest.NewRecorder()
		o.h.ServeHTTP(w, r)
		h := w.Header()
		csp := h.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("%s: CSP = %q", path, csp)
		}
		if h.Get("Cache-Control") != "no-store" || h.Get("Referrer-Policy") != "no-referrer" || h.Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s: headers = %v", path, h)
		}
		if path == "/passkey/"+f.id {
			body := w.Body.String()
			if w.Code != 200 || !strings.Contains(body, `src="/passkey/page.js"`) || strings.Contains(body, "<script>") || strings.Contains(body, " onclick") {
				t.Errorf("page = %d %s", w.Code, body)
			}
		}
		if path == "/passkey/nope" && w.Code != http.StatusNotFound {
			t.Errorf("an unknown flow's page = %d", w.Code)
		}
	}
	// The page's script holds no url of anywhere else and never edits the
	// server's authenticator selection (so no attachment can be pinned there).
	if strings.Contains(string(passkeyScript), "://") || strings.Contains(string(passkeyScript), "authenticatorSelection") {
		t.Fatal("the ceremony script reaches beyond its own origin or pins an attachment")
	}
}

func TestPasskeysReachNoSecretSink(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	token := o.joinByPasskey(o.operatorLink(), "santosh", key)
	body, err := os.ReadFile(tokens.AccountsPath(o.cfg.TokensPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), token) {
		t.Fatal("a session credential reached accounts.json")
	}
	priv, _ := key.Priv.Bytes()
	if strings.Contains(string(body), pkB64(priv)) {
		t.Fatal("a private key reached accounts.json")
	}
}

// ── the security review's probes (#2652) ────────────────────────────────────

// doTLS is a request over TLS from remote, as from another machine.
func (o *pwOrg) doTLS(method, path, token string, body any, remote string) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.RemoteAddr = remote
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	return w
}

// startFrom starts a sign-in from remote over TLS and answers the flow.
func (o *pwOrg) startFrom(remote string, body api.PasskeyStart) (pkFlow, *httptest.ResponseRecorder) {
	verifier, challenge := newVerifier()
	path, _ := newVerifier()
	key, _ := newVerifier()
	body.CodeChallenge, body.ReturnPort, body.ReturnPath, body.ReturnKey = challenge, 49152, path, key
	w := o.doTLS("POST", "/v1/auth/passkey/start", "", body, remote)
	var out api.PasskeyStarted
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	returnKeys.Store(out.Flow, key)
	return pkFlow{id: out.Flow, url: out.URL, verifier: verifier}, w
}

// C1: a sign-in started by someone else and run by the person signs the
// starter in to nothing. The attacker holds the flow and its verifier; the
// person runs the genuine page with Touch ID; the code goes to the person's
// own loopback, so every poll the attacker can make answers no session.
func TestASignInStartedBySomeoneElseCannotBePhished(t *testing.T) {
	o := newPWOrg(t)
	victim := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", victim)

	attack := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	w := o.finish(attack.id, victim.get(t, o.options(attack.id, map[string]string{}), pkOrigin), "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"next":"/passkey/`) || strings.Contains(w.Body.String(), "access_token") {
		t.Fatalf("the person's ceremony = %d %s", w.Code, w.Body)
	}
	// The attacker's engine never received the code: polls without it, and
	// with guesses, answer no session; the guesses end the flow.
	if got := o.pollWith(attack.id, attack.verifier, ""); got.State != api.PasskeyElsewhere || got.AccessToken != "" {
		t.Fatalf("attacker's poll without the code = %+v", got)
	}
	// The person's browser, back on the page, is told where it finishes.
	if w := o.do("GET", "/passkey/"+attack.id, "", nil, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Finish on the computer that started this") {
		t.Fatalf("the finished page = %d %.300s", w.Code, w.Body)
	}
	for range passkeyWrongMost {
		guess, _ := newVerifier()
		if got := o.pollWith(attack.id, attack.verifier, guess); got.AccessToken != "" || got.State != api.PasskeyRefused {
			t.Fatalf("attacker's guessed code = %+v", got)
		}
	}
	if got := o.poll(attack); got.AccessToken != "" {
		t.Fatalf("the code after the guesses still answered: %+v", got)
	}

	// The legitimate flow, whose engine got the code, signs in.
	if got, w := o.signInByPasskey(victim); w.Code != 200 || got.AccessToken == "" {
		t.Fatalf("the person's own sign-in = %d %+v", w.Code, got)
	}
}

// C1 for add and join: a ceremony run from a stolen url adds nothing and
// spends nothing, because the account is written only at the poll that
// shows the code, which the engine that started the flow never gets.
func TestAStolenAddOrJoinURLChangesNothing(t *testing.T) {
	o := newPWOrg(t)
	mine := newSoftKey(t)
	session := o.joinByPasskey(o.operatorLink(), "santosh", mine)
	link := o.invite(session)

	o.confirm(session, mine, toAdd)
	add := o.mustStart(session, api.PasskeyStart{Purpose: api.PasskeyAdd})
	thief := newSoftKey(t)
	if w := o.finish(add.id, thief.create(t, o.options(add.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("thief's add ceremony = %d %s", w.Code, w.Body)
	}
	if got := o.pollWith(add.id, add.verifier, ""); got.State != api.PasskeyElsewhere {
		t.Fatalf("the person's engine's poll = %+v", got)
	}
	if n := len(o.accountOf("santosh").Passkeys); n != 1 {
		t.Fatalf("a stolen add url added a passkey: %d", n)
	}

	join := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: link})
	if w := o.finish(join.id, thief.create(t, o.options(join.id, map[string]string{"username": "mallory"}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("thief's join ceremony = %d %s", w.Code, w.Body)
	}
	if _, ok, _ := o.h.passwordAccount("mallory"); ok {
		t.Fatal("a stolen join url made an account")
	}
	// The link is still there for the person it was sent to.
	o.joinByPasskey(link, "abir", newSoftKey(t))
}

// H1 and N2: a stolen session cannot become a permanent takeover, however
// fresh. A sign-in is never a step-up: a session made a minute ago sets no
// first password, adds no passkey and removes none.
func TestAStolenSessionCannotTakeAnAccountOver(t *testing.T) {
	o := newPWOrg(t)
	own, spare := newSoftKey(t), newSoftKey(t)
	mine := o.joinByPasskey(o.operatorLink(), "santosh", own)
	o.addPasskey(mine, own, spare)
	stolen := o.token2(own)
	o.advance(time.Minute)

	stepUp := func(what string, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), api.ReasonStepUp) {
			t.Fatalf("%s without a step-up = %d %s", what, w.Code, w.Body)
		}
	}
	stepUp("a first password", o.do("POST", "/v1/me/password", stolen, api.PasswordChange{Current: "", New: "the attacker's own passphrase"}, ""))
	stepUp("removing the person's passkey", o.do("POST", "/v1/me/passkeys/remove", stolen, api.PasskeyRemove{ID: pkB64(own.ID)}, ""))
	_, w := o.pkStart(stolen, api.PasskeyStart{Purpose: api.PasskeyAdd})
	stepUp("an add", w)
	// A passkey-only person has no password to confirm with.
	if w := o.do("POST", "/v1/me/password/confirm", stolen, api.PasswordConfirm{Password: "anything", Action: toSetPassword}, ""); w.Code != http.StatusNotFound {
		t.Fatalf("password confirm with no password = %d", w.Code)
	}
	if acct := o.accountOf("santosh"); len(acct.Passkeys) != 2 || acct.Hash != "" {
		t.Fatalf("the account changed: %d passkeys, password %v", len(acct.Passkeys), acct.Hash != "")
	}

	// The person, confirming with their own passkey, may.
	o.confirm(stolen, own, toSetPassword)
	if w := o.do("POST", "/v1/me/password", stolen, api.PasswordChange{Current: "", New: pwOperator}, ""); w.Code/100 != 2 {
		t.Fatalf("a first password after confirming = %d %s", w.Code, w.Body)
	}
}

// N1, replayed: the attacker holds a stolen session and gets the person to
// do a step-up on the genuine page, then drives the rest with a plain HTTP
// client and a forged Origin. The step-up is its own flow, bound to the
// person's browser, and its code goes to the person's loopback; the
// attacker's client is refused at every door, and the add it wanted is
// refused for want of a proof.
func TestASplitStepUpCannotBeFinishedByTheAttacker(t *testing.T) {
	o := newPWOrg(t)
	own := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", own)
	stolen := o.token2(own)
	o.advance(passkeyStepUpLife + time.Second)
	attackerBrowser := &pkBrowser{}

	// The old chain: an add started by the stolen session. It is refused
	// outright; there is no step-up inside an add any more.
	if _, w := o.pkStart(stolen, api.PasskeyStart{Purpose: api.PasskeyAdd}); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), api.ReasonStepUp) {
		t.Fatalf("an add by a stolen session = %d %s", w.Code, w.Body)
	}
	// So the attacker starts a confirm for an add and sends the person its
	// page.
	confirm := o.mustStart(stolen, api.PasskeyStart{Purpose: api.PasskeyConfirm, Action: &toAdd, Device: "MacBook"})
	opt := o.options(confirm.id, map[string]string{})
	// The attacker's client, forged Origin and all, cannot ask or answer for
	// the flow the person's browser holds.
	if w := o.pageIn(attackerBrowser, confirm.id, "options", pkOrigin, map[string]string{}); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "other_browser") {
		t.Fatalf("the attacker's options = %d %s", w.Code, w.Body)
	}
	cred := own.get(t, opt, pkOrigin)
	if w := o.pageIn(attackerBrowser, confirm.id, "finish", pkOrigin, map[string]any{"credential": cred}); w.Code != http.StatusForbidden {
		t.Fatalf("the attacker's finish = %d %s", w.Code, w.Body)
	}
	// The person finishes; their browser is handed over to "their" loopback.
	if w := o.finish(confirm.id, cred, ""); w.Code != 200 {
		t.Fatalf("the person's confirm = %d %s", w.Code, w.Body)
	}
	// The attacker cannot fetch the hand-over (no cookie): sent to the page.
	if loc := o.handOver(attackerBrowser, confirm.id); loc != "" {
		t.Fatalf("the attacker's hand-over = %q", loc)
	}
	// Without the code the attacker's poll proves nothing.
	if got := o.pollWith(confirm.id, confirm.verifier, ""); got.State != api.PasskeyElsewhere {
		t.Fatalf("the attacker's poll = %+v", got)
	}
	if _, w := o.pkStart(stolen, api.PasskeyStart{Purpose: api.PasskeyAdd}); w.Code != http.StatusForbidden {
		t.Fatalf("an add after the attacker's confirm = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/me/passkeys/remove", stolen, api.PasskeyRemove{ID: pkB64(own.ID)}, ""); w.Code/100 == 2 {
		t.Fatalf("a removal after the attacker's confirm = %d %s", w.Code, w.Body)
	}
	if n := len(o.accountOf("santosh").Passkeys); n != 1 {
		t.Fatalf("the account's passkeys = %d", n)
	}
}

// The binding: a flow belongs to the first browser that asks it for a
// ceremony; its hand-over answers that browser once.
func TestAFlowIsBoundToOneBrowser(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", key)
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	other := &pkBrowser{}
	opt := o.options(f.id, map[string]string{})
	if w := o.pageIn(other, f.id, "options", pkOrigin, map[string]string{}); w.Code != http.StatusForbidden {
		t.Fatalf("another browser's options = %d", w.Code)
	}
	if w := o.pageIn(nil, f.id, "finish", pkOrigin, map[string]any{"credential": key.get(t, opt, pkOrigin)}); w.Code != http.StatusForbidden {
		t.Fatalf("a finish without the cookie = %d", w.Code)
	}
	if w := o.finish(f.id, key.get(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("the bound browser's finish = %d %s", w.Code, w.Body)
	}
	// A second hand-over by the same browser is sent to the page.
	if loc := o.handOver(theBrowser, f.id); loc != "" {
		t.Fatalf("a second hand-over = %q", loc)
	}
	if got := o.poll(f); got.AccessToken == "" {
		t.Fatalf("the poll with the code = %+v", got)
	}
}

// H1 for a password person: the current password is the step-up.
func TestAPasswordPersonStepsUpWithTheirPassword(t *testing.T) {
	o := newPWOrg(t)
	session := o.signUp(o.operatorLink(), "santosh", pwOperator)
	// Straight after the sign-up: a sign-in is no step-up.
	if _, w := o.pkStart(session, api.PasskeyStart{Purpose: api.PasskeyAdd}); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), api.ReasonStepUp) {
		t.Fatalf("an add without a step-up = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/me/password/confirm", session, api.PasswordConfirm{Password: "not the password", Action: toAdd}, ""); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "current_wrong") {
		t.Fatalf("a wrong password = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/me/password/confirm", session, api.PasswordConfirm{Password: pwOperator, Action: toAdd}, ""); w.Code != http.StatusNoContent {
		t.Fatalf("the right password = %d %s", w.Code, w.Body)
	}
	key := newSoftKey(t)
	f := o.mustStart(session, api.PasskeyStart{Purpose: api.PasskeyAdd})
	if w := o.finish(f.id, key.create(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("add after confirming = %d %s", w.Code, w.Body)
	}
	if got := o.poll(f); got.State != api.PasskeyDone || len(o.accountOf("santosh").Passkeys) != 1 {
		t.Fatalf("add poll = %+v", got)
	}
}

// M1: a flood of sign-in starts from many addresses cannot shut a person
// out. Sign-ins hold only part of the table; a link-bearing join still
// starts; a fresh sign-in makes room by dropping the oldest unopened one;
// and a flow whose page the person opened is never the one dropped.
func TestAFloodOfStartsLeavesRoomForAPerson(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	operator := o.joinByPasskey(o.operatorLink(), "santosh", key)
	link := o.invite(operator)
	// The person's own sign-in, its ceremony asked for by their browser.
	mine := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	o.options(mine.id, map[string]string{})
	refused := 0
	for a := range 80 {
		for range passkeyFlowsPerAddr + 2 {
			if _, w := o.startFrom(fmt.Sprintf("198.18.%d.%d:1", a/250, a%250+1), api.PasskeyStart{Purpose: api.PasskeySignIn}); w.Code == http.StatusTooManyRequests {
				refused++
			}
		}
	}
	if refused == 0 {
		t.Fatal("no address was held to its cap of waiting flows")
	}
	st := o.h.pk()
	st.mu.Lock()
	signIns := 0
	for _, f := range st.flows {
		if f.purpose == api.PasskeySignIn {
			signIns++
		}
	}
	_, kept := st.flows[mine.id]
	st.mu.Unlock()
	if signIns > passkeySignInFlowsMost || !kept {
		t.Fatalf("sign-ins = %d (most %d), the opened flow kept = %v", signIns, passkeySignInFlowsMost, kept)
	}
	o.mustStart("", api.PasskeyStart{Purpose: api.PasskeyJoin, Link: link})
	if _, w := o.startFrom("203.0.113.77:1", api.PasskeyStart{Purpose: api.PasskeySignIn}); w.Code != http.StatusCreated {
		t.Fatalf("a person's fresh sign-in during the flood = %d %s", w.Code, w.Body)
	}
	if w := o.finish(mine.id, key.get(t, o.options(mine.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("the opened flow's ceremony = %d %s", w.Code, w.Body)
	}
	if got := o.poll(mine); got.AccessToken == "" {
		t.Fatalf("the opened flow's poll = %+v", got)
	}
	// An unopened sign-in lives two minutes, and fetching its page does not
	// open it (N4): only a browser asking for a ceremony does.
	lazy := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if w := o.do("GET", "/passkey/"+lazy.id, "", nil, ""); w.Code != 200 {
		t.Fatalf("page = %d", w.Code)
	}
	o.advance(passkeyUnopenedLife)
	if got := o.poll(lazy); got.State != api.PasskeyExpired {
		t.Fatalf("an unopened sign-in after %v = %+v", passkeyUnopenedLife, got)
	}
}

// N4: a flood that opens its own pages (asks each for a ceremony) from 48
// addresses in one /24 keeps at most passkeyOpenedPerPrefix of them; a
// person on another network, page opened, keeps theirs and signs in.
func TestAFloodThatOpensItsPagesIsHeldPerNetwork(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", key)
	mine, w := o.startFrom("203.0.113.77:1", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if w.Code != http.StatusCreated {
		t.Fatalf("the person's start = %d", w.Code)
	}
	o.options(mine.id, map[string]string{})
	flood := &pkBrowser{}
	for a := range 48 {
		for range 3 {
			f, w := o.startFrom(fmt.Sprintf("198.51.100.%d:1", a+1), api.PasskeyStart{Purpose: api.PasskeySignIn})
			if w.Code != http.StatusCreated {
				continue
			}
			if w := o.pageIn(flood, f.id, "options", pkOrigin, map[string]string{}); w.Code != 200 {
				t.Fatalf("the flood's options = %d %s", w.Code, w.Body)
			}
		}
	}
	st := o.h.pk()
	st.mu.Lock()
	opened := 0
	for _, f := range st.flows {
		if f.purpose == api.PasskeySignIn && f.opened && f.prefix == "198.51.100.0/24" {
			opened++
		}
	}
	_, kept := st.flows[mine.id]
	st.mu.Unlock()
	if opened != passkeyOpenedPerPrefix || !kept {
		t.Fatalf("opened sign-ins from the flood's /24 = %d (most %d), the person's kept = %v", opened, passkeyOpenedPerPrefix, kept)
	}
	if w := o.finish(mine.id, key.get(t, o.options(mine.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("the person's ceremony = %d %s", w.Code, w.Body)
	}
	if got := o.poll(mine); got.AccessToken == "" {
		t.Fatalf("the person's poll = %+v", got)
	}
}

// M1: starts from one IPv6 /48 share one budget, however many /64s.
func TestStartsFromOneIPv6SiteShareABudget(t *testing.T) {
	o := newPWOrg(t)
	created := 0
	for i := range passkeyStarts + 5 {
		f, w := o.startFrom(fmt.Sprintf("[2001:db8:1:%x::1]:1", i), api.PasskeyStart{Purpose: api.PasskeySignIn})
		if w.Code == http.StatusCreated {
			created++
			o.doTLS("POST", "/v1/auth/passkey/cancel", "", api.PasskeyPoll{Flow: f.id, Verifier: f.verifier}, "[2001:db8:1::1]:1")
		}
	}
	if created != passkeyStarts {
		t.Fatalf("starts from one /48 = %d, want %d", created, passkeyStarts)
	}
	if _, w := o.startFrom("[2001:db8:2::1]:1", api.PasskeyStart{Purpose: api.PasskeySignIn}); w.Code != http.StatusCreated {
		t.Fatalf("another /48 = %d", w.Code)
	}
}

// L1: an address that spells the default port works; the browser's origin
// never carries it.
func TestADefaultPortInTheAddressStillWorks(t *testing.T) {
	o := newPWOrg(t)
	o.cfg.Address = "https://org.test:443"
	o.h = New(o.h.seam, o.cfg)
	t.Cleanup(o.h.Close)
	if rp, origin, ok := o.h.relyingParty(); !ok || rp != "org.test" || origin != "https://org.test" {
		t.Fatalf("relying party = %q %q %v", rp, origin, ok)
	}
	key := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", key)
	if got, w := o.signInByPasskey(key); w.Code != 200 || got.AccessToken == "" {
		t.Fatalf("sign in = %d %+v", w.Code, got)
	}
}

// C1: a start must name the engine's loopback listener, on 127.0.0.1 only.
func TestAStartNamesTheEnginesLoopback(t *testing.T) {
	o := newPWOrg(t)
	_, challenge := newVerifier()
	path, _ := newVerifier()
	key, _ := newVerifier()
	for name, body := range map[string]api.PasskeyStart{
		"no port":      {Purpose: api.PasskeySignIn, CodeChallenge: challenge, ReturnPath: path, ReturnKey: key},
		"no path":      {Purpose: api.PasskeySignIn, CodeChallenge: challenge, ReturnPort: 49152, ReturnKey: key},
		"a low port":   {Purpose: api.PasskeySignIn, CodeChallenge: challenge, ReturnPort: 80, ReturnPath: path, ReturnKey: key},
		"a short path": {Purpose: api.PasskeySignIn, CodeChallenge: challenge, ReturnPort: 49152, ReturnPath: "abc", ReturnKey: key},
		"no key":       {Purpose: api.PasskeySignIn, CodeChallenge: challenge, ReturnPort: 49152, ReturnPath: path},
	} {
		if w := o.do("POST", "/v1/auth/passkey/start", "", body, ""); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d", name, w.Code)
		}
	}
	// The cancel door is https-only like the others.
	if w := o.do("POST", "/v1/auth/passkey/cancel", "", api.PasskeyPoll{Flow: "x", Verifier: "y"}, "203.0.113.9:5000"); w.Code != http.StatusForbidden {
		t.Fatalf("cancel over plain http from afar = %d", w.Code)
	}
}

// F1, replayed: cookie fixation. The attacker binds a sign-in flow to their
// own browser id and plants it in the person's browser the two ways a
// sibling or on-path host can: a Domain cookie for the parent name, and a
// path cookie on plain HTTP. Neither carries the __Host- name the server
// reads, so the person's browser is not the attacker's: it gets its own id,
// the flow (bound to the attacker) refuses it, and the attacker's own
// browser never holds a ceremony the person ran.
func TestAPlantedCookieDoesNotBindThePersonsBrowser(t *testing.T) {
	o := newPWOrg(t)
	victim := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", victim)
	attacker := &pkBrowser{}
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	opt := o.pageIn(attacker, f.id, "options", pkOrigin, map[string]string{})
	if opt.Code != 200 {
		t.Fatalf("the attacker's own options = %d", opt.Code)
	}
	planted, _ := attacker.cookies.Load(passkeyCookie)
	id := planted.(*http.Cookie).Value
	// What the planted cookies look like on the wire: names without the
	// prefix (a browser refuses __Host- from a sibling or over HTTP).
	person := func(r *http.Request) {
		r.Header.Add("Cookie", "codeaf_passkey="+id+"; codeaf_pkb="+id)
	}
	data, _ := json.Marshal(map[string]string{})
	r := httptest.NewRequest("POST", "/passkey/"+f.id+"/options", strings.NewReader(string(data)))
	r.RemoteAddr = "127.0.0.1:4100"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", pkOrigin)
	person(r)
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "other_browser") {
		t.Fatalf("the person's browser, cookies planted = %d %s", w.Code, w.Body)
	}
	// So the person never runs the attacker's flow; and a flow the person's
	// browser does bind, the attacker's id cannot touch.
	mine := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	o.options(mine.id, map[string]string{})
	if w := o.pageIn(attacker, mine.id, "finish", pkOrigin, map[string]any{"credential": json.RawMessage("{}")}); w.Code != http.StatusForbidden {
		t.Fatalf("the attacker's id on the person's flow = %d", w.Code)
	}
	if w := o.finish(mine.id, victim.get(t, o.options(mine.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("the person's own ceremony = %d %s", w.Code, w.Body)
	}
	if loc := o.handOver(attacker, mine.id); loc != "" {
		t.Fatalf("the attacker's hand-over = %q", loc)
	}
	if got := o.poll(mine); got.AccessToken == "" {
		t.Fatalf("the person's poll = %+v", got)
	}
}

// F3: a confirmation lets one action happen once. A confirm for removing
// one passkey removes no other, adds nothing and sets no password; it is
// spent by the removal; and one older than five minutes lets nothing.
func TestAConfirmationIsForOneActionOnce(t *testing.T) {
	o := newPWOrg(t)
	a, b, c := newSoftKey(t), newSoftKey(t), newSoftKey(t)
	session := o.joinByPasskey(o.operatorLink(), "santosh", a)
	o.addPasskey(session, a, b)
	o.addPasskey(session, a, c)
	// The confirm page names the action and the computer asking.
	f := o.mustStart(session, api.PasskeyStart{Purpose: api.PasskeyConfirm, Action: &api.StepUpAction{Kind: api.StepUpRemovePasskey, Passkey: pkB64(b.ID)}, Device: "santosh-mac"})
	page := o.do("GET", "/passkey/"+f.id, "", nil, "")
	if !strings.Contains(page.Body.String(), "Remove passkey") || !strings.Contains(page.Body.String(), "requested by codeaf on santosh-mac") {
		t.Fatalf("the confirm page = %s", page.Body)
	}
	if w := o.finish(f.id, a.get(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("confirm = %d %s", w.Code, w.Body)
	}
	if got := o.poll(f); got.State != api.PasskeyDone {
		t.Fatalf("confirm poll = %+v", got)
	}
	stepUp := func(what string, w *httptest.ResponseRecorder, want api.StepUpAction) {
		t.Helper()
		var body struct {
			Reason string           `json:"reason"`
			Action api.StepUpAction `json:"action"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusForbidden || body.Reason != api.ReasonStepUp || body.Action != want {
			t.Fatalf("%s = %d %s", what, w.Code, w.Body)
		}
	}
	stepUp("removing another passkey", o.do("POST", "/v1/me/passkeys/remove", session, api.PasskeyRemove{ID: pkB64(c.ID)}, ""), toRemove(c))
	_, w := o.pkStart(session, api.PasskeyStart{Purpose: api.PasskeyAdd})
	stepUp("an add", w, toAdd)
	stepUp("a first password", o.do("POST", "/v1/me/password", session, api.PasswordChange{New: pwOperator}, ""), toSetPassword)
	if w := o.do("POST", "/v1/me/passkeys/remove", session, api.PasskeyRemove{ID: pkB64(b.ID)}, ""); w.Code != http.StatusNoContent {
		t.Fatalf("the confirmed removal = %d %s", w.Code, w.Body)
	}
	// Spent: confirming removal of c, then waiting past the window.
	o.confirm(session, a, toRemove(c))
	o.advance(passkeyStepUpLife)
	stepUp("a removal confirmed too long ago", o.do("POST", "/v1/me/passkeys/remove", session, api.PasskeyRemove{ID: pkB64(c.ID)}, ""), toRemove(c))
}

// F3: a finished flow's code works for a minute. A code that reached the
// wrong computer's address bar and is pasted back later opens nothing.
func TestALoopbackCodeLivesOneMinute(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", key)
	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if w := o.finish(f.id, key.get(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("finish = %d", w.Code)
	}
	o.advance(passkeyCodeLife)
	if got := o.poll(f); got.State != api.PasskeyExpired || got.AccessToken != "" {
		t.Fatalf("a poll a minute after done = %+v", got)
	}
	// And a hand-over a minute late hands no code.
	g := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if w := o.page(g.id, "finish", pkOrigin, map[string]any{"credential": key.get(t, o.options(g.id, map[string]string{}), pkOrigin)}); w.Code != 200 {
		t.Fatalf("finish = %d", w.Code)
	}
	o.advance(passkeyCodeLife)
	if loc := o.handOver(theBrowser, g.id); loc != "" {
		t.Fatalf("a late hand-over = %q", loc)
	}
}

// F2: a flood that opens 16 sign-ins from each of 24 networks fills every
// sign-in slot; a person on a fresh network still starts, and the room
// is taken from the network holding the most.
func TestAFloodFromManyNetworksStillLetsAPersonStart(t *testing.T) {
	o := newPWOrg(t)
	key := newSoftKey(t)
	o.joinByPasskey(o.operatorLink(), "santosh", key)
	flood := &pkBrowser{}
	for n := range 24 {
		for a := range 2 {
			for range passkeyFlowsPerAddr {
				f, w := o.startFrom(fmt.Sprintf("10.%d.0.%d:1", n+1, a+1), api.PasskeyStart{Purpose: api.PasskeySignIn})
				if w.Code == http.StatusCreated {
					o.pageIn(flood, f.id, "options", pkOrigin, map[string]string{})
				}
			}
		}
	}
	count := func() (int, map[string]int) {
		st := o.h.pk()
		st.mu.Lock()
		defer st.mu.Unlock()
		n, by := 0, map[string]int{}
		for _, f := range st.flows {
			if f.purpose == api.PasskeySignIn {
				n++
				by[f.prefix]++
			}
		}
		return n, by
	}
	if n, _ := count(); n != passkeySignInFlowsMost {
		t.Fatalf("the flood holds %d sign-ins, want the full %d", n, passkeySignInFlowsMost)
	}
	mine, w := o.startFrom("203.0.113.9:1", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if w.Code != http.StatusCreated {
		t.Fatalf("a person on a fresh network = %d %s", w.Code, w.Body)
	}
	if _, by := count(); by["203.0.113.0/24"] != 1 {
		t.Fatalf("the person's flow is not kept: %v", by)
	}
	o.options(mine.id, map[string]string{})
	if w := o.finish(mine.id, key.get(t, o.options(mine.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("the person's ceremony = %d", w.Code)
	}
	if got := o.poll(mine); got.AccessToken == "" {
		t.Fatalf("the person's poll = %+v", got)
	}
}

// F1: a real name the operator owns covers the names under it too, so no
// plain-HTTP host below it can toss cookies at it; a shared wildcard name
// (demos only), an IP address and localhost get the bare header.
func TestHSTSCoversSubdomainsOnlyOfANameTheOperatorOwns(t *testing.T) {
	for host, want := range map[string]string{
		"org.example.com":         hstsValue + "; includeSubDomains",
		"Factory.Acme.dev.":       hstsValue + "; includeSubDomains",
		"203-0-113-9.sslip.io":    hstsValue,
		"acme.203.0.113.9.nip.io": hstsValue,
		"localhost":               hstsValue,
		"203.0.113.9":             hstsValue,
		"intranet":                hstsValue,
	} {
		if got := hstsFor(host); got != want {
			t.Errorf("hstsFor(%q) = %q, want %q", host, got, want)
		}
	}
}
