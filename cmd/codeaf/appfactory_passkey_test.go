package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/serve/passkeytest"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// localhostCert is a self-signed certificate for the DNS name localhost,
// written to dir as cert.pem and key.pem, and the pool that trusts it. A
// passkey needs a DNS name and a secure context; localhost is the one name a
// test can serve both on.
func localhostCert(t *testing.T, dir string) (tls.Certificate, *x509.CertPool, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return pair, pool, certFile, keyFile
}

// passkeyOrg is a real organization server over TLS at https://localhost,
// the shape on which a server offers passkeys.
type passkeyOrg struct {
	url, tokensPath string
	pool            *x509.CertPool
	clock           *factoryClock
}

func startPasskeyOrg(t *testing.T) *passkeyOrg {
	t.Helper()
	o := &passkeyOrg{clock: &factoryClock{at: time.Now().UTC()}}
	pair, pool, _, _ := localhostCert(t, t.TempDir())
	o.pool = pool
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: o.clock.now})
	if err != nil {
		t.Fatal(err)
	}
	o.tokensPath = filepath.Join(st.Root(), "tokens.json")
	var mu sync.Mutex
	var h *serve.Handler
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		handler := h
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	srv.StartTLS()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	o.url = "https://localhost:" + port
	seam := factory.Seam{Load: func() (factory.Snapshot, error) { return factory.Snapshot{}, nil }}
	mu.Lock()
	h = serve.New(seam, serve.Config{Version: "test", Name: "Acme", ServedBy: "factory", Address: o.url, AccessToken: testHostToken,
		Mode: api.FactoryTeam, Now: o.clock.now, Store: st, Record: rec, Root: st.Root(), TokensPath: o.tokensPath})
	mu.Unlock()
	t.Cleanup(func() { srv.Close(); h.Close() })
	return o
}

func (o *passkeyOrg) client() *http.Client {
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: o.pool}}, Timeout: 10 * time.Second}
}

// browser plays the person's browser on the ceremony page: it asks the page
// for its options, lets key answer them, and posts the answer back, from the
// page's own origin, keeping the cookie the page binds it with. A finished
// ceremony is followed through the server's hand-over to the engine's
// loopback, as the page does, which is what lets the engine's poll finish.
func (o *passkeyOrg) browser(t *testing.T, pageURL string, key *passkeytest.Key, ask map[string]string) {
	t.Helper()
	b := o.newBrowser()
	opt := b.post(t, pageURL, "options", ask)
	var answer struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(opt, &answer)
	var cred json.RawMessage
	var err error
	if answer.Kind == "create" {
		cred, err = key.Create(opt, o.url)
	} else {
		cred, err = key.Get(opt, o.url)
	}
	if err != nil {
		t.Fatal(err)
	}
	var done struct {
		Next string `json:"next"`
	}
	if err := json.Unmarshal(b.post(t, pageURL, "finish", map[string]any{"credential": cred}), &done); err != nil || done.Next == "" {
		t.Fatalf("a finished ceremony names no hand-over: %+v %v", done, err)
	}
	resp, err := b.client.Get(o.url + done.Next)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Request.URL.Host == "" || !strings.HasPrefix(resp.Request.URL.String(), "http://127.0.0.1:") {
		t.Fatalf("the hand-over ended at %s with %d", resp.Request.URL.Redacted(), resp.StatusCode)
	}
}

// pageBrowser is one browser: a client with its own cookies.
type pageBrowser struct {
	client *http.Client
	origin string
}

func (o *passkeyOrg) newBrowser() *pageBrowser {
	jar, _ := cookiejar.New(nil)
	c := o.client()
	c.Jar = jar
	return &pageBrowser{client: c, origin: o.url}
}

// post posts one step of a ceremony page from the page's own origin.
func (b *pageBrowser) post(t *testing.T, pageURL, step string, body any) []byte {
	t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", pageURL+"/"+step, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", b.origin)
	resp, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("page %s = %d %s", step, resp.StatusCode, out)
	}
	return out
}

func (o *passkeyOrg) operatorLink(t *testing.T) string {
	t.Helper()
	tok, err := tokens.RotateOperator(o.tokensPath, "", o.clock.now(), false)
	if err != nil {
		t.Fatal(err)
	}
	return "codeaf://factory/join?" + url.Values{"url": {o.url}, "operator": {"true"}}.Encode() + "#token=" + tok.Token
}

// THE ENGINE'S HALF, END TO END: santosh claims the organization with a
// passkey, adds a second, signs out, signs back in with the second, removes
// the first, and is refused removing the last. No line the engine writes
// carries the verifier or a session.
func TestAPasskeyPersonThroughTheEngine(t *testing.T) {
	o := startPasskeyOrg(t)
	hub := testFactoryHub(t)
	hub.client = o.client()
	santosh := newFactoryWire(t, hub)
	if !strings.Contains(santosh.hello, appwire.FeatureFactoryPasskey) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPasskey, santosh.hello)
	}
	claim := o.operatorLink(t)
	var access appwire.FactoryAccess
	santosh.must("factory.access_check", map[string]any{"link": claim, "caller": "w1"}, &access)
	if strings.Join(access.Factory.SignIn, ",") != "passkey,password" {
		t.Fatalf("check sign_in = %v", access.Factory.SignIn)
	}

	var started appwire.FactoryPasskeyStarted
	santosh.must("factory.passkey_start", map[string]any{"link": claim, "caller": "w1", "purpose": "join", "username": "santosh", "name": "Santosh"}, &started)
	if !strings.HasPrefix(started.URL, o.url+"/passkey/") || started.Flow == "" {
		t.Fatalf("started = %+v", started)
	}
	var polled appwire.FactoryPasskeyPolled
	santosh.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "pending" {
		t.Fatalf("poll before = %+v", polled)
	}
	// Another caller cannot poll this flow.
	santosh.mustFail("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w2"}, appwire.CodeNotFound)
	first, _ := passkeytest.New()
	o.browser(t, started.URL, first, map[string]string{})
	santosh.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "done" || polled.Factory == "" {
		t.Fatalf("poll after = %+v", polled)
	}
	id := polled.Factory
	var me appwire.FactoryMe
	santosh.must("factory.me", map[string]any{"factory": id}, &me)
	if me.Handle != "santosh" || !me.Operator || me.SignInKind != "passkey" || me.GitHubSignedIn || !me.CanSignOut {
		t.Fatalf("me = %+v", me)
	}
	var list appwire.FactoryList
	santosh.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 1 || list.Factories[0].State != appwire.FactoryOK || strings.Join(list.Factories[0].SignIn, ",") != "passkey,password" {
		t.Fatalf("list = %+v", list)
	}

	// A sign-in is no step-up: an add first needs a confirm.
	santosh.mustFail("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "add"}, appwire.CodeStepUp)
	santosh.must("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "confirm"}, &started)
	o.browser(t, started.URL, first, map[string]string{})
	santosh.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "done" || polled.Factory != id {
		t.Fatalf("confirm poll = %+v", polled)
	}
	// Add a second passkey.
	santosh.must("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "add"}, &started)
	second, _ := passkeytest.New()
	second.Flags &^= passkeytest.FlagBE | passkeytest.FlagBS
	o.browser(t, started.URL, second, map[string]string{})
	santosh.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "done" || polled.Factory != id {
		t.Fatalf("add poll = %+v", polled)
	}
	var keys appwire.FactoryPasskeyList
	santosh.must("factory.passkey_list", map[string]any{"factory": id}, &keys)
	if len(keys.Passkeys) != 2 || keys.Password || keys.Passkeys[1].Synced {
		t.Fatalf("passkeys = %+v", keys)
	}

	// Sign out, and back in with the second.
	santosh.must("factory.sign_out", map[string]any{"factory": id}, nil)
	santosh.must("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "sign_in"}, &started)
	o.browser(t, started.URL, second, map[string]string{})
	santosh.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "done" || polled.Factory != id {
		t.Fatalf("sign-in poll = %+v", polled)
	}
	santosh.must("factory.me", map[string]any{"factory": id}, &me)
	if me.Handle != "santosh" {
		t.Fatalf("me after sign-in = %+v", me)
	}
	// A done flow is answered once.
	santosh.mustFail("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, appwire.CodeNotFound)

	// Signed in again, the session must confirm before removing.
	santosh.mustFail("factory.passkey_remove", map[string]any{"factory": id, "id": keys.Passkeys[0].ID}, appwire.CodeStepUp)
	santosh.must("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "confirm"}, &started)
	o.browser(t, started.URL, second, map[string]string{})
	santosh.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	santosh.must("factory.passkey_remove", map[string]any{"factory": id, "id": keys.Passkeys[0].ID}, nil)
	if msg := santosh.mustFail("factory.passkey_remove", map[string]any{"factory": id, "id": keys.Passkeys[1].ID}, appwire.CodeConflict); !strings.Contains(msg, "only way to sign in") {
		t.Fatalf("last way in = %q", msg)
	}

	santosh.mu.Lock()
	defer santosh.mu.Unlock()
	for _, line := range santosh.lines {
		if strings.Contains(line, "access_token") || strings.Contains(line, "verifier") || strings.Contains(line, "code_challenge") {
			t.Errorf("a line carries a secret: %.200s", line)
		}
	}
}

// A start whose page is on another origin is refused before a client can
// open it, and the params are checked by shape first.
func TestPasskeyStartIsCheckedBeforeAnythingIsOpened(t *testing.T) {
	if passkeyURLOn("https://evil.test/passkey/x", "https://org.test") || passkeyURLOn("http://org.test/passkey/x", "http://org.test") ||
		passkeyURLOn("https://org.test/elsewhere", "https://org.test") || !passkeyURLOn("https://org.test/passkey/abc", "https://org.test") {
		t.Fatal("passkeyURLOn judged a url wrongly")
	}
	w := newFactoryWire(t, testFactoryHub(t))
	for _, params := range []map[string]any{
		{"caller": "c", "purpose": "join"},
		{"caller": "c", "purpose": "sign_in", "link": "codeaf://factory/join"},
		{"caller": "c", "purpose": "add", "factory": "f", "username": "x"},
		{"caller": "c", "purpose": "steal", "factory": "f"},
		{"caller": "c", "purpose": "sign_in", "factory": "f", "verifier": "x"},
		{"caller": "c", "purpose": "confirm", "factory": "f", "username": "x"},
		{"caller": "c", "purpose": "confirm", "link": "codeaf://factory/join"},
	} {
		w.mustFail("factory.passkey_start", params, appwire.CodeBadParams)
	}
	for _, params := range []map[string]any{
		{"factory": "f"},
		{"factory": "f", "password": "long enough password", "echo": true},
	} {
		w.mustFail("factory.password_confirm", params, appwire.CodeBadParams)
	}
}

// THE STEP-UP AND THE LOOPBACK THROUGH THE ENGINE: a sign-in finished in a
// browser that never reaches this computer polls pending, then elsewhere, and
// never signs anyone in; a stale session's remove is step_up until the person
// confirms with a passkey.
func TestAPasskeyCeremonyEndsOnThisComputerAndRemovingNeedsAProof(t *testing.T) {
	o := startPasskeyOrg(t)
	hub := testFactoryHub(t)
	hub.client = o.client()
	engineClock := &factoryClock{at: time.Now().UTC()}
	hub.now = engineClock.now
	w := newFactoryWire(t, hub)
	claim := o.operatorLink(t)

	var started appwire.FactoryPasskeyStarted
	var polled appwire.FactoryPasskeyPolled
	w.must("factory.passkey_start", map[string]any{"link": claim, "caller": "w1", "purpose": "join", "username": "santosh", "name": "Santosh"}, &started)
	first, _ := passkeytest.New()
	o.browser(t, started.URL, first, map[string]string{})
	w.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "done" {
		t.Fatalf("join poll = %+v", polled)
	}
	id := polled.Factory
	w.must("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "confirm"}, &started)
	o.browser(t, started.URL, first, map[string]string{})
	w.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	w.must("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "add"}, &started)
	second, _ := passkeytest.New()
	o.browser(t, started.URL, second, map[string]string{})
	w.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "done" {
		t.Fatalf("add poll = %+v", polled)
	}

	// Elsewhere: the ceremony finishes in a browser that does not follow the
	// redirect here.
	w.must("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "sign_in"}, &started)
	elsewhere := o.newBrowser()
	opt := elsewhere.post(t, started.URL, "options", map[string]string{})
	cred, err := first.Get(opt, o.url)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere.post(t, started.URL, "finish", map[string]any{"credential": cred})
	w.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "pending" {
		t.Fatalf("poll inside the grace = %+v", polled)
	}
	engineClock.advance(passkeyElsewhereAfter)
	polled = appwire.FactoryPasskeyPolled{}
	w.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != appwire.FactoryPasskeyElsewhere || polled.Factory != "" {
		t.Fatalf("poll after the grace = %+v", polled)
	}
	w.must("factory.passkey_cancel", map[string]any{"flow": started.Flow, "caller": "w1"}, nil)

	// The session is older than the step-up window: removing is step_up.
	o.clock.advance(10 * time.Minute)
	var keys appwire.FactoryPasskeyList
	w.must("factory.passkey_list", map[string]any{"factory": id}, &keys)
	if len(keys.Passkeys) != 2 {
		t.Fatalf("passkeys = %+v", keys)
	}
	w.mustFail("factory.passkey_remove", map[string]any{"factory": id, "id": keys.Passkeys[0].ID}, appwire.CodeStepUp)
	w.mustFail("factory.password_confirm", map[string]any{"factory": id, "password": "not-a-password-here"}, appwire.CodeNotFound)
	w.must("factory.passkey_start", map[string]any{"factory": id, "caller": "w1", "purpose": "confirm"}, &started)
	o.browser(t, started.URL, second, map[string]string{})
	w.must("factory.passkey_poll", map[string]any{"flow": started.Flow, "caller": "w1"}, &polled)
	if polled.State != "done" || polled.Factory != id {
		t.Fatalf("confirm poll = %+v", polled)
	}
	w.must("factory.passkey_remove", map[string]any{"factory": id, "id": keys.Passkeys[0].ID}, nil)
}
