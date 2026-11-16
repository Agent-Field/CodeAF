package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// passwordOrg is a real organization server with no GitHub configured at all,
// on a real loopback listener, the way `codeaf factory serve` runs one.
type passwordOrg struct {
	url, tokensPath string
	clock           *factoryClock
}

func startPasswordOrg(t *testing.T) *passwordOrg {
	t.Helper()
	// The engine reads its own clock when it judges a made link, so the server
	// keeps real time too.
	o := &passwordOrg{clock: &factoryClock{at: time.Now().UTC()}}
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		handler := h
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	seam := factory.Seam{Load: func() (factory.Snapshot, error) { return factory.Snapshot{}, nil }}
	mu.Lock()
	h = serve.New(seam, serve.Config{Version: "test", Name: "Acme", ServedBy: "factory", Address: srv.URL, AccessToken: testHostToken,
		Mode: api.FactoryTeam, Now: o.clock.now, Store: st, Record: rec, Root: st.Root(), TokensPath: o.tokensPath})
	mu.Unlock()
	t.Cleanup(func() { srv.Close(); h.Close() })
	o.url = srv.URL
	return o
}

func (o *passwordOrg) operatorLink(t *testing.T) string {
	t.Helper()
	tok, err := tokens.RotateOperator(o.tokensPath, "", o.clock.now(), false)
	if err != nil {
		t.Fatal(err)
	}
	return "codeaf://factory/join?" + url.Values{"url": {o.url}, "operator": {"true"}}.Encode() + "#token=" + tok.Token
}

// THE ENGINE'S HALF, END TO END, WITH TWO PASSWORD PEOPLE: santosh claims the
// organization through the operator link, makes an invitation the engine
// copies, abir joins on another machine with it, santosh changes his password,
// signs out and back in with it, and makes abir a reset link that abir uses.
// No answer and no line the engine writes carries a password or a session.
func TestTwoPasswordPeopleThroughTheEngine(t *testing.T) {
	const pwSantosh, pwAbir, pwNew, pwReset = "correct horse battery staple", "a long passphrase for my friend", "another long passphrase", "a reset passphrase, long"
	o := startPasswordOrg(t)
	santosh := newFactoryWire(t, testFactoryHub(t))
	if !strings.Contains(santosh.hello, appwire.FeatureFactoryPassword) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPassword, santosh.hello)
	}
	clip := &fakeCopier{avail: true}
	santosh.backend.useClipboard(clip)

	claim := o.operatorLink(t)
	var access appwire.FactoryAccess
	santosh.must("factory.access_check", map[string]any{"link": claim, "caller": "w1"}, &access)
	if access.State != "ready" || strings.Join(access.Factory.SignIn, ",") != "password" {
		t.Fatalf("check = %+v", access)
	}
	// A refusal names the rule and makes nothing.
	santosh.must("factory.access_sign_up", map[string]any{"link": claim, "caller": "w1", "username": "santosh", "password": "short"}, &access)
	if access.State != "ready" || access.Reason != "password_short" || access.Message == "" {
		t.Fatalf("short = %+v", access)
	}
	santosh.must("factory.access_sign_up", map[string]any{"link": claim, "caller": "w1", "username": "santosh", "password": pwSantosh, "name": "Santosh"}, &access)
	if access.State != "joined" {
		t.Fatalf("claim = %+v", access)
	}
	id := access.Factory.ID
	var list appwire.FactoryList
	santosh.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 1 || list.Factories[0].State != appwire.FactoryOK || strings.Join(list.Factories[0].SignIn, ",") != "password" || !list.Factories[0].Operator {
		t.Fatalf("list = %+v", list)
	}
	var me appwire.FactoryMe
	santosh.must("factory.me", map[string]any{"factory": id}, &me)
	if me.Handle != "santosh" || me.SignInKind != "password" || !me.Operator || !me.InviteAvailable || !me.CanSignOut {
		t.Fatalf("me = %+v", me)
	}

	var made appwire.FactoryLinkMade
	santosh.must("factory.link_create", map[string]any{"factory": id}, &made)
	if made.Outcome != appwire.LinkCopied || clip.count() != 1 {
		t.Fatalf("invite = %+v", made)
	}
	invite := clip.calls[0]

	abir := newFactoryWire(t, testFactoryHub(t))
	abir.must("factory.access_sign_up", map[string]any{"link": invite, "caller": "w9", "username": "abir", "password": pwAbir}, &access)
	if access.State != "joined" {
		t.Fatalf("abir = %+v", access)
	}
	abirID := access.Factory.ID
	var people api.People
	santosh.must("factory.people", map[string]any{"factory": id}, &people)
	kinds := map[string]string{}
	for _, p := range people.People {
		kinds[p.Handle] = p.Kind
	}
	if kinds["santosh"] != "password" || kinds["abir"] != "password" {
		t.Fatalf("people = %v", kinds)
	}

	// Wrong password: the server's one sentence.
	if msg := abir.mustFail("factory.password_sign_in", map[string]any{"factory": abirID, "caller": "w9", "username": "abir", "password": pwSantosh}, appwire.CodeForbidden); msg != "That username and password don't match." {
		t.Fatalf("wrong = %q", msg)
	}

	// Change, sign out, sign back in with the new one.
	santosh.must("factory.password_change", map[string]any{"factory": id, "current": pwSantosh, "new": pwNew}, nil)
	santosh.must("factory.sign_out", map[string]any{"factory": id}, nil)
	santosh.must("factory.list", struct{}{}, &list)
	if list.Factories[0].State != appwire.FactorySignedOut || strings.Join(list.Factories[0].SignIn, ",") != "password" {
		t.Fatalf("after sign-out = %+v", list.Factories[0])
	}
	santosh.mustFail("factory.password_sign_in", map[string]any{"factory": id, "caller": "w1", "username": "santosh", "password": pwSantosh}, appwire.CodeForbidden)
	var signed appwire.FactoryPasswordSignedIn
	santosh.must("factory.password_sign_in", map[string]any{"factory": id, "caller": "w1", "username": "santosh", "password": pwNew}, &signed)
	if signed.State != "signed_in" || signed.Me.Handle != "santosh" || !signed.Me.Operator {
		t.Fatalf("signed in = %+v", signed)
	}

	// The operator's reset link for abir, copied like an invitation.
	santosh.must("factory.link_create", map[string]any{"factory": id, "kind": "reset", "person": "abir"}, &made)
	if made.Outcome != appwire.LinkCopied || made.Entry.Kind != "reset" || made.Entry.Login != "abir" {
		t.Fatalf("reset link = %+v", made)
	}
	reset := clip.calls[len(clip.calls)-1]
	abir.must("factory.access_check", map[string]any{"link": reset, "caller": "w9"}, &access)
	if access.Kind != "reset" || access.State != "ready" {
		t.Fatalf("reset check = %+v", access)
	}
	abir.must("factory.access_reset", map[string]any{"link": reset, "caller": "w9", "password": pwReset}, &access)
	if access.State != "joined" {
		t.Fatalf("reset = %+v", access)
	}
	abir.must("factory.me", map[string]any{"factory": abirID}, &me)
	if me.Handle != "abir" {
		t.Fatalf("abir after reset = %+v", me)
	}

	for _, w := range []*factoryWire{santosh, abir} {
		w.neverSaid(pwSantosh, pwAbir, pwNew, pwReset)
		w.mu.Lock()
		for _, line := range w.lines {
			if strings.Contains(line, "access_token") {
				t.Errorf("a line carries a session: %.200s", line)
			}
		}
		w.mu.Unlock()
	}
	// Every password the engine saw is taken out of anything it writes later.
	for _, pw := range []string{pwSantosh, pwAbir, pwNew, pwReset} {
		if strings.Contains(redact.Secrets("a log line with "+pw+" in it"), pw) {
			t.Errorf("redact does not cover a password it was handed")
		}
	}
}

// A password param is refused by shape before anything is sent, and the
// refusal never quotes it.
func TestPasswordParamsAreCheckedWithoutQuotingThem(t *testing.T) {
	w := newFactoryWire(t, testFactoryHub(t))
	const secret = "never-quote-me-please"
	for _, c := range []struct {
		method string
		params map[string]any
	}{
		{"factory.access_sign_up", map[string]any{"link": "x", "caller": "c", "username": "u", "password": ""}},
		{"factory.access_sign_up", map[string]any{"link": "x", "caller": "c", "username": "u", "password": secret, "extra": secret}},
		{"factory.password_sign_in", map[string]any{"factory": "f", "caller": "c", "username": "u", "password": strings.Repeat(secret, 100)}},
		{"factory.password_change", map[string]any{"factory": "f", "current": 7, "new": secret}},
		{"factory.access_reset", map[string]any{"link": "x", "caller": "c"}},
	} {
		if msg := w.mustFail(c.method, c.params, appwire.CodeBadParams); strings.Contains(msg, secret) {
			t.Errorf("%s quoted the password: %s", c.method, msg)
		}
	}
	w.neverSaid(secret)
}
