package serve

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// ── a software authenticator ────────────────────────────────────────────────
//
// softKey is one ES256 passkey held by a test "authenticator". It builds the
// exact bytes a browser hands a relying party (clientDataJSON, an attestation
// object with fmt "none", an assertion signed over authData || SHA-256 of
// clientDataJSON), so the server's real go-webauthn verification runs on
// them. The flags it sets are the test's to choose, which is how a missing
// user verification is proven to be refused.

const (
	flagUP = 0x01
	flagUV = 0x04
	flagBE = 0x08
	flagBS = 0x10
	flagAT = 0x40
)

type softKey struct {
	priv       *ecdsa.PrivateKey
	id         []byte
	userHandle []byte
	count      uint32
	flags      byte
}

func pkB64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

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
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &softKey{priv: priv, id: id, flags: flagUP | flagUV | flagBE | flagBS}
}

func (k *softKey) cose(t *testing.T) []byte {
	t.Helper()
	x, y := make([]byte, 32), make([]byte, 32)
	k.priv.PublicKey.X.FillBytes(x)
	k.priv.PublicKey.Y.FillBytes(y)
	out, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func clientData(kind, challenge, origin string) []byte {
	out, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return out
}

func authData(rpID string, flags byte, count uint32) []byte {
	hash := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, hash[:]...)
	out = append(out, flags)
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], count)
	return append(out, c[:]...)
}

// create answers a navigator.credentials.create() for options as the
// browser would at origin.
func (k *softKey) create(t *testing.T, options json.RawMessage, origin string) json.RawMessage {
	t.Helper()
	var opt struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			AuthenticatorSelection struct {
				ResidentKey             string `json:"residentKey"`
				UserVerification        string `json:"userVerification"`
				AuthenticatorAttachment string `json:"authenticatorAttachment"`
			} `json:"authenticatorSelection"`
			Attestation string `json:"attestation"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opt); err != nil {
		t.Fatalf("creation options %s: %v", options, err)
	}
	k.userHandle = unb64(t, opt.PublicKey.User.ID)
	ad := authData(opt.PublicKey.RP.ID, k.flags|flagAT, k.count)
	ad = append(ad, make([]byte, 16)...) // AAGUID: none
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(k.id)))
	ad = append(ad, l[:]...)
	ad = append(ad, k.id...)
	ad = append(ad, k.cose(t)...)
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(map[string]any{
		"id": pkB64(k.id), "rawId": pkB64(k.id), "type": "public-key", "authenticatorAttachment": "platform",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    pkB64(clientData("webauthn.create", opt.PublicKey.Challenge, origin)),
			"attestationObject": pkB64(att), "transports": []string{"internal", "hybrid"},
		},
	})
	return out
}

// get answers a navigator.credentials.get() for options at origin.
func (k *softKey) get(t *testing.T, options json.RawMessage, origin string) json.RawMessage {
	t.Helper()
	var opt struct {
		PublicKey struct {
			Challenge        string `json:"challenge"`
			RPID             string `json:"rpId"`
			UserVerification string `json:"userVerification"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opt); err != nil {
		t.Fatalf("request options %s: %v", options, err)
	}
	k.count++
	ad := authData(opt.PublicKey.RPID, k.flags, k.count)
	cd := clientData("webauthn.get", opt.PublicKey.Challenge, origin)
	sum := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(map[string]any{
		"id": pkB64(k.id), "rawId": pkB64(k.id), "type": "public-key", "clientExtensionResults": map[string]any{},
		"response": map[string]any{"clientDataJSON": pkB64(cd), "authenticatorData": pkB64(ad), "signature": pkB64(sig), "userHandle": pkB64(k.userHandle)},
	})
	return out
}

// ── the engine's side of a flow ─────────────────────────────────────────────

const pkOrigin = "https://org.test"

type pkFlow struct {
	id, url  string
	verifier string
}

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
	w := o.do("POST", "/v1/auth/passkey/start", token, body, "")
	var out api.PasskeyStarted
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			o.t.Fatal(err)
		}
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

// page posts to the ceremony page's JSON as the browser would from origin.
func (o *pwOrg) page(flow, step, origin string, body any) *httptest.ResponseRecorder {
	o.t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/passkey/"+flow+"/"+step, strings.NewReader(string(data)))
	r.RemoteAddr = "127.0.0.1:4100"
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_1) Safari")
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	return w
}

func (o *pwOrg) options(flow string, body any) json.RawMessage {
	o.t.Helper()
	w := o.page(flow, "options", pkOrigin, body)
	if w.Code != 200 {
		o.t.Fatalf("options = %d %s", w.Code, w.Body)
	}
	return w.Body.Bytes()
}

func (o *pwOrg) finish(flow string, cred json.RawMessage, name string) *httptest.ResponseRecorder {
	return o.page(flow, "finish", pkOrigin, map[string]any{"credential": cred, "name": name})
}

func (o *pwOrg) poll(f pkFlow) api.PasskeyPolled {
	o.t.Helper()
	w := o.do("POST", "/v1/auth/passkey/poll", "", api.PasskeyPoll{Flow: f.id, Verifier: f.verifier}, "")
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
	if pk.Name != "Mac passkey" || !pk.BackupEligible || pk.PublicKey == "" || pk.ID != pkB64(key.id) {
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
	key.flags &^= flagUV
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
		// The wrong verifier ended the flow: the right one now finds nothing.
		if got := o.poll(f); got.State != api.PasskeyExpired || got.AccessToken != "" {
			t.Fatalf("poll after a wrong verifier = %+v", got)
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
		stranger.userHandle = make([]byte, 32)
		if _, w := o.signInByPasskey(stranger); w.Code != http.StatusBadRequest {
			t.Fatalf("an unknown passkey = %d %s", w.Code, w.Body)
		}
	})
	t.Run("credential owned by someone else", func(t *testing.T) {
		operator := o.token2(key)
		abir := newSoftKey(t)
		abir.id = key.id // the same credential id as santosh's
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
			w := o.do("POST", "/v1/auth/passkey/poll", "", api.PasskeyPoll{Flow: f.id, Verifier: f.verifier}, "")
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
	add := o.mustStart(here, api.PasskeyStart{Purpose: api.PasskeyAdd})
	if w := o.finish(add.id, second.create(t, o.options(add.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	_ = o.poll(add)

	f := o.mustStart("", api.PasskeyStart{Purpose: api.PasskeySignIn})
	if w := o.finish(f.id, first.get(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("finish = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/me/passkeys/remove", here, api.PasskeyRemove{ID: pkB64(second.id)}, ""); w.Code != http.StatusNoContent {
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
	w := o.do("POST", "/v1/auth/passkey/poll", "", api.PasskeyPoll{Flow: f.id, Verifier: f.verifier}, "")
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
	// The same person signing in with a password is a password sign-in.
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
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, r := range both {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := o.finish(r.f.id, r.cred, ""); w.Code == 200 {
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
	if len(excluded.PublicKey.Exclude) != 1 || excluded.PublicKey.Exclude[0].ID != pkB64(first.id) {
		t.Fatalf("the person's own passkey is not excluded: %s", opt)
	}
	second := newSoftKey(t)
	second.flags &^= flagBE | flagBS // a security key: not synced
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
	if w := o.do("POST", "/v1/me/passkeys/remove", here, api.PasskeyRemove{ID: pkB64(first.id)}, ""); w.Code != http.StatusNoContent {
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
	if w := o.do("POST", "/v1/me/passkeys/remove", here, api.PasskeyRemove{ID: pkB64(second.id)}, ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "last_way_in") {
		t.Fatalf("remove the last = %d %s", w.Code, w.Body)
	}
	// With a password set, it can.
	if w := o.do("POST", "/v1/me/password", here, api.PasswordChange{Current: "", New: pwOperator}, ""); w.Code/100 != 2 {
		t.Fatalf("set a first password = %d %s", w.Code, w.Body)
	}
	if w := o.do("POST", "/v1/me/passkeys/remove", here, api.PasskeyRemove{ID: pkB64(second.id)}, ""); w.Code != http.StatusNoContent {
		t.Fatalf("remove with a password = %d %s", w.Code, w.Body)
	}
	o.token("santosh", pwOperator)
}

func TestAPasswordPersonAddsAPasskeyAndAGitHubSessionCannot(t *testing.T) {
	o := newPWOrg(t)
	operator := o.signUp(o.operatorLink(), "santosh", pwOperator)
	key := newSoftKey(t)
	f := o.mustStart(operator, api.PasskeyStart{Purpose: api.PasskeyAdd})
	if w := o.finish(f.id, key.create(t, o.options(f.id, map[string]string{}), pkOrigin), ""); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
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
		if _, w := o.pkStart("", api.PasskeyStart{Purpose: api.PasskeySignIn}); w.Code != http.StatusCreated {
			t.Fatalf("start %d = %d", i, w.Code)
		}
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
	priv, _ := key.priv.Bytes()
	if strings.Contains(string(body), pkB64(priv)) {
		t.Fatal("a private key reached accounts.json")
	}
}
