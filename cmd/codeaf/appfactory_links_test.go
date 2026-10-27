package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clipboard"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// ── a scripted team factory, a fake clipboard and a log of what was asked ───

const (
	linkTestSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	linkTestURL    = "codeaf://factory/join?url=http%3A%2F%2Ffactory.example#token=" + linkTestSecret
	linkTestBearer = "bearer-op-fixture-0001"
)

type fakeCopier struct {
	mu    sync.Mutex
	avail bool
	err   error
	calls []string
	hook  func()
}

func (c *fakeCopier) Available() bool { return c.avail }
func (c *fakeCopier) Copy(_ context.Context, text string) error {
	c.mu.Lock()
	c.calls = append(c.calls, text)
	hook, err := c.hook, c.err
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}
func (c *fakeCopier) set(err error) { c.mu.Lock(); c.err = err; c.mu.Unlock() }
func (c *fakeCopier) count() int    { c.mu.Lock(); defer c.mu.Unlock(); return len(c.calls) }

type linkFactory struct {
	t     *testing.T
	stub  *stubFactory
	clock *factoryClock
	hub   *appFactoryHub
	b     *appBackend
	id    string
	clip  *fakeCopier

	mu     sync.Mutex
	me     api.Me
	rows   []api.AccessLink
	post   func(http.ResponseWriter, *http.Request)
	del    func(http.ResponseWriter, *http.Request)
	listH  func(http.ResponseWriter, *http.Request)
	bodies []string
	seen   []string // everything the engine returned, to be searched for a secret
}

func newLinkFactory(t *testing.T) *linkFactory {
	t.Helper()
	f := &linkFactory{t: t, clock: &factoryClock{at: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}, clip: &fakeCopier{avail: true}}
	f.me = api.Me{Mode: api.FactoryTeam, Operator: true, GitHubSignedIn: true, SessionID: "session_op1",
		GitHubSignInAvailable: true, InviteAvailable: true, Profile: api.Profile{Handle: "alice", Scopes: []string{}}}
	f.post = f.mint
	f.del = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	f.stub = startStub(t, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		me, rows, post, del, list := f.me, append([]api.AccessLink(nil), f.rows...), f.post, f.del, f.listH
		f.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(me)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/access/links":
			if list != nil {
				list(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"links": rows})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/access/links":
			buf := new(strings.Builder)
			var raw json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&raw)
			buf.Write(raw)
			f.mu.Lock()
			f.bodies = append(f.bodies, buf.String())
			f.mu.Unlock()
			post(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/access/revoke-sessions":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/access/links/"):
			del(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	f.hub = testFactoryHub(t)
	f.hub.now = f.clock.now
	b, conn := attachStub(t, f.hub, f.stub, linkTestBearer)
	f.b, f.id = b, conn.ID
	b.useClipboard(f.clip)
	return f
}

// mint is the factory's honest answer to a create.
func (f *linkFactory) mint(w http.ResponseWriter, r *http.Request) {
	row := api.AccessLink{ID: "tok_aabbccdd", CreatedAt: f.clock.now(), ExpiresAt: f.clock.now().Add(tokens.HumanLinkTTL),
		CreatedBy: "alice", MaxUses: 1, Remaining: 1, Status: "open", Counted: boolPtr(true)}
	f.mu.Lock()
	// An honest factory answers with what it was asked for.
	var asked struct {
		ExpiresAt *time.Time `json:"expires_at"`
		MaxUses   int        `json:"max_uses"`
		Login     string     `json:"login"`
		Name      string     `json:"name"`
	}
	_ = json.Unmarshal([]byte(f.bodies[len(f.bodies)-1]), &asked)
	if asked.ExpiresAt != nil {
		row.ExpiresAt = *asked.ExpiresAt
	}
	if asked.MaxUses > 0 {
		row.MaxUses, row.Remaining = asked.MaxUses, asked.MaxUses
	}
	row.Login, row.Name = asked.Login, asked.Name
	f.rows = append(f.rows, row)
	f.mu.Unlock()
	row.URL = linkTestURL
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"factory": api.Factory{Name: "x"}, "link": row})
}

func boolPtr(b bool) *bool { return &b }

func (f *linkFactory) requests() []string {
	f.stub.mu.Lock()
	defer f.stub.mu.Unlock()
	return append([]string(nil), f.stub.requests...)
}

// count is how many requests began with method and path prefix.
func (f *linkFactory) count(method, prefix string) int {
	n := 0
	for _, r := range f.requests() {
		if strings.HasPrefix(r, method+" "+prefix) {
			n++
		}
	}
	return n
}
func (f *linkFactory) posts() int   { return f.count("POST", "/v1/access/links") }
func (f *linkFactory) deletes() int { return f.count("DELETE", "/v1/access/links") }

func (f *linkFactory) record(v any, err error) {
	if v != nil {
		raw, _ := json.Marshal(v)
		f.seen = append(f.seen, string(raw))
	}
	if err != nil {
		f.seen = append(f.seen, err.Error())
	}
}

func (f *linkFactory) create(p appwire.FactoryLinkCreateParams) (appwire.FactoryLinkMade, error) {
	p.Factory = f.id
	out, err := f.b.FactoryLinkCreate(context.Background(), p)
	f.record(out, err)
	return out, err
}

func (f *linkFactory) list() (appwire.FactoryLinkList, error) {
	out, err := f.b.FactoryLinkList(context.Background(), appwire.FactoryLinkParams{Factory: f.id})
	f.record(out, err)
	return out, err
}

func (f *linkFactory) copyAgain(ref string) (appwire.FactoryLinkMade, error) {
	out, err := f.b.FactoryLinkCopy(context.Background(), appwire.FactoryLinkCopyParams{Factory: f.id, Ref: ref})
	f.record(out, err)
	return out, err
}

func (f *linkFactory) revoke(id string) (appwire.FactoryLinkRevoked, error) {
	out, err := f.b.FactoryLinkRevoke(context.Background(), appwire.FactoryLinkRevokeParams{Factory: f.id, ID: id})
	f.record(out, err)
	return out, err
}

func (f *linkFactory) noSecretAnywhere() {
	f.t.Helper()
	for _, s := range f.seen {
		for _, bad := range []string{linkTestSecret, "codeaf://", "#token", linkTestBearer} {
			if strings.Contains(s, bad) {
				f.t.Fatalf("the engine returned %q: %.300s", bad, s)
			}
		}
	}
}

func linkWantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", code)
	}
	if got := wireCodeOf(t, err); got != code {
		t.Fatalf("code %s (%v), want %s", got, err, code)
	}
}

// ── create: the copy decides what is said ──────────────────────────────────

// A CONFIRMED COPY: one POST, no DELETE, the URL only ever on the copier's
// input, and a result with a row and an outcome and nothing else.
func TestLinkCreateCopiedSendsOnePostAndKeepsTheSecretOffTheWire(t *testing.T) {
	f := newLinkFactory(t)
	out, err := f.create(appwire.FactoryLinkCreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != appwire.LinkCopied || out.Ref != "" || out.HoldsUntil != nil || out.Entry.ID != "tok_aabbccdd" || out.Entry.Status != "open" {
		t.Fatalf("out = %+v", out)
	}
	if f.posts() != 1 || f.deletes() != 0 || f.clip.count() != 1 || f.clip.calls[0] != linkTestURL {
		t.Fatalf("posts %d deletes %d copies %v", f.posts(), f.deletes(), f.clip.calls)
	}
	// Nothing was typed, so the factory's own defaults apply.
	if f.bodies[0] != `{}` {
		t.Fatalf("the create carried %s, want the factory's defaults", f.bodies[0])
	}
	if out.Entry.MaxUses != 1 || out.Entry.ExpiresAt == nil || out.Entry.ExpiresAt.Sub(f.clock.now()) != tokens.HumanLinkTTL {
		t.Fatalf("entry = %+v", out.Entry)
	}
	if redact.Secrets(linkTestSecret) == linkTestSecret || redact.Secrets(linkTestURL) == linkTestURL {
		t.Fatal("the secret was not registered with the redactors")
	}
	f.noSecretAnywhere()
	if f.b.factoryHub().linkSt != nil && len(f.b.factoryHub().linkSt.held) != 0 {
		t.Fatal("a copied invitation is held")
	}
}

// TYPED CHOICES ARE SENT, AND ONLY THOSE.
func TestLinkCreateSendsExactlyTheTypedChoices(t *testing.T) {
	f := newLinkFactory(t)
	exp := f.clock.now().Add(48 * time.Hour)
	three := 3
	if _, err := f.create(appwire.FactoryLinkCreateParams{Name: "  Contractors  ", MaxUses: &three, ExpiresAt: &exp}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(f.bodies[0]), &got)
	if len(got) != 3 || got["name"] != "Contractors" || got["max_uses"] != float64(3) || got["expires_at"] != exp.UTC().Format(time.RFC3339) {
		t.Fatalf("body = %s", f.bodies[0])
	}
	one := 1
	if _, err := f.create(appwire.FactoryLinkCreateParams{Login: "@Priya", MaxUses: &one}); err != nil {
		t.Fatal(err)
	}
	if f.bodies[1] != `{"max_uses":1,"login":"priya"}` {
		t.Fatalf("body = %s", f.bodies[1])
	}
}

func TestLinkCreateBoundsAreRefusedBeforeAnyRequest(t *testing.T) {
	f := newLinkFactory(t)
	zero, big, two := 0, appwire.LinkMaxUses+1, 2
	past, far := f.clock.now().Add(-time.Minute), f.clock.now().Add(tokens.HumanLinkTTL+time.Hour)
	for name, p := range map[string]appwire.FactoryLinkCreateParams{
		"zero uses": {MaxUses: &zero}, "too many": {MaxUses: &big}, "login and two": {Login: "priya", MaxUses: &two},
		"bad login": {Login: "not a login"}, "long name": {Name: strings.Repeat("x", appwire.LinkNameMax+1)},
		"newline name": {Name: "a\nb"}, "past": {ExpiresAt: &past}, "too far": {ExpiresAt: &far},
	} {
		if _, err := f.create(p); err == nil || wireCodeOf(t, err) != appwire.CodeBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	// "Exactly seven days from now" is allowed.
	edge := f.clock.now().Add(tokens.HumanLinkTTL)
	if _, err := f.create(appwire.FactoryLinkCreateParams{ExpiresAt: &edge}); err != nil {
		t.Errorf("seven days: %v", err)
	}
	if f.posts() != 1 {
		t.Fatalf("%d POSTs, want only the allowed one", f.posts())
	}
}

// A FAILED OR UNCONFIRMED COPY AFTER A CONFIRMED MINT: ONE POST, NO DELETE, NO
// SECOND MINT; the same invitation is held and the retry copies it.
func TestLinkCopyFailureHoldsAndRetryMintsNothing(t *testing.T) {
	for name, c := range map[string]struct {
		err     error
		outcome string
	}{
		"helper failed":      {clipboard.ErrFailed, appwire.LinkNotCopied},
		"helper unavailable": {clipboard.ErrUnavailable, appwire.LinkNotCopied},
		"helper unconfirmed": {clipboard.ErrUnconfirmed, appwire.LinkUnconfirmed},
		"unknown error":      {errors.New("exec: boom " + linkTestSecret), appwire.LinkUnconfirmed},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLinkFactory(t)
			f.clip.set(c.err)
			out, err := f.create(appwire.FactoryLinkCreateParams{})
			if err != nil {
				t.Fatalf("a made invitation is not an error: %v", err)
			}
			if out.Outcome != c.outcome || !appwire.CheckLinkRef(out.Ref) || out.HoldsUntil == nil || out.Entry.ID != "tok_aabbccdd" {
				t.Fatalf("out = %+v", out)
			}
			if want := f.clock.now().Add(appwire.LinkHoldFor); !out.HoldsUntil.Equal(want) {
				t.Fatalf("holds until %v, want %v", out.HoldsUntil, want)
			}
			if f.posts() != 1 || f.deletes() != 0 {
				t.Fatalf("posts %d deletes %d", f.posts(), f.deletes())
			}
			// Still failing: the same ref is kept and nothing else happens.
			again, err := f.copyAgain(out.Ref)
			if err != nil || again.Outcome == appwire.LinkCopied || again.Ref != out.Ref {
				t.Fatalf("again = %+v %v", again, err)
			}
			// The helper recovers: the SAME invitation is copied, with no mint.
			f.clip.set(nil)
			done, err := f.copyAgain(out.Ref)
			if err != nil || done.Outcome != appwire.LinkCopied || done.Ref != "" || done.HoldsUntil != nil {
				t.Fatalf("done = %+v %v", done, err)
			}
			if f.posts() != 1 || f.deletes() != 0 {
				t.Fatalf("a retry wrote: posts %d deletes %d", f.posts(), f.deletes())
			}
			for _, call := range f.clip.calls {
				if call != linkTestURL {
					t.Fatalf("a different text was copied: %q", call)
				}
			}
			// Copied once: nothing is held any more.
			_, err = f.copyAgain(out.Ref)
			linkWantCode(t, err, appwire.CodeNotFound)
			f.noSecretAnywhere()
		})
	}
}

// THE HOLD NEVER OUTLIVES THE INVITATION.
func TestLinkHoldEndsWithTheInvitationsOwnExpiry(t *testing.T) {
	f := newLinkFactory(t)
	f.clip.set(clipboard.ErrFailed)
	exp := f.clock.now().Add(4 * time.Minute)
	out, err := f.create(appwire.FactoryLinkCreateParams{ExpiresAt: &exp})
	if err != nil || out.HoldsUntil == nil || !out.HoldsUntil.Equal(exp.UTC()) {
		t.Fatalf("out = %+v %v", out, err)
	}
	f.clock.advance(4 * time.Minute)
	f.clip.set(nil)
	_, err = f.copyAgain(out.Ref)
	linkWantCode(t, err, appwire.CodeNotFound)
	if f.clip.count() != 1 {
		t.Fatalf("an expired hold was copied: %v", f.clip.calls)
	}
}

func TestLinkHoldEndsAfterTenMinutes(t *testing.T) {
	f := newLinkFactory(t)
	f.clip.set(clipboard.ErrFailed)
	out, _ := f.create(appwire.FactoryLinkCreateParams{})
	f.clock.advance(appwire.LinkHoldFor - time.Second)
	f.clip.set(nil)
	if again, err := f.copyAgain(out.Ref); err != nil || again.Outcome != appwire.LinkCopied {
		t.Fatalf("just inside the hold: %+v %v", again, err)
	}
	f.clip.set(clipboard.ErrFailed)
	out, _ = f.create(appwire.FactoryLinkCreateParams{})
	f.clock.advance(appwire.LinkHoldFor)
	_, err := f.copyAgain(out.Ref)
	linkWantCode(t, err, appwire.CodeNotFound)
	if st := f.hub.linkSt; st != nil {
		f.hub.mu.Lock()
		n := len(st.held)
		f.hub.mu.Unlock()
		if n != 0 {
			t.Fatalf("%d expired copies still held", n)
		}
	}
}

// A HELD COPY BELONGS TO THE CALLER, THE FACTORY AND THE MOMENT.
func TestLinkHoldIsClearedOrRefusedWhenItsScopeChanges(t *testing.T) {
	held := func(t *testing.T) (*linkFactory, appwire.FactoryLinkMade) {
		f := newLinkFactory(t)
		f.clip.set(clipboard.ErrFailed)
		out, err := f.create(appwire.FactoryLinkCreateParams{})
		if err != nil || out.Ref == "" {
			t.Fatalf("%+v %v", out, err)
		}
		f.clip.set(nil)
		return f, out
	}
	n := func(f *linkFactory) int { f.hub.mu.Lock(); defer f.hub.mu.Unlock(); return len(f.hub.linkSt.held) }
	t.Run("wrong ref", func(t *testing.T) {
		f, out := held(t)
		other := []byte(out.Ref)
		other[0] = 'A'
		if out.Ref[0] == 'A' {
			other[0] = 'B'
		}
		_, err := f.copyAgain(string(other))
		linkWantCode(t, err, appwire.CodeNotFound)
		if n(f) != 1 || f.clip.count() != 1 {
			t.Fatal("a wrong ref disturbed the hold or copied")
		}
	})
	t.Run("a malformed ref", func(t *testing.T) {
		f, _ := held(t)
		_, err := f.copyAgain("../etc/passwd")
		linkWantCode(t, err, appwire.CodeBadParams)
	})
	t.Run("another factory", func(t *testing.T) {
		f, out := held(t)
		_, err := f.b.FactoryLinkCopy(context.Background(), appwire.FactoryLinkCopyParams{Factory: "factory_other", Ref: out.Ref})
		if err == nil || f.clip.count() != 1 {
			t.Fatalf("%v %v", err, f.clip.calls)
		}
	})
	t.Run("the caller is replaced", func(t *testing.T) {
		f, out := held(t)
		f.hub.hold(f.id, "a-different-bearer", 0)
		if n(f) != 0 {
			t.Fatal("a new caller inherited the copy")
		}
		_, err := f.copyAgain(out.Ref)
		linkWantCode(t, err, appwire.CodeNotFound)
		if f.clip.count() != 1 {
			t.Fatal("copied for a replaced caller")
		}
	})
	t.Run("signed out", func(t *testing.T) {
		f, out := held(t)
		f.hub.dropSession(f.id, linkTestBearer)
		if n(f) != 0 {
			t.Fatal("still held after the sign-in ended")
		}
		_, err := f.copyAgain(out.Ref)
		linkWantCode(t, err, appwire.CodeSignedOut)
	})
	t.Run("detached", func(t *testing.T) {
		f, out := held(t)
		if err := f.b.FactoryDetach(context.Background(), f.id); err != nil {
			t.Fatal(err)
		}
		if n(f) != 0 {
			t.Fatal("still held after detach")
		}
		_, err := f.copyAgain(out.Ref)
		if err == nil || f.clip.count() != 1 {
			t.Fatalf("%v %v", err, f.clip.calls)
		}
	})
	t.Run("engine closed", func(t *testing.T) {
		f, _ := held(t)
		f.hub.forgetAll()
		if n(f) != 0 {
			t.Fatal("still held after close")
		}
	})
	t.Run("everyone signed out", func(t *testing.T) {
		f, out := held(t)
		if _, err := f.b.FactoryRevokeSessions(context.Background(), appwire.FactoryAdminParams{Factory: f.id}); err != nil {
			t.Fatal(err)
		}
		if n(f) != 0 {
			t.Fatal("still held after revoke-all")
		}
		// The operator's own sign-in ended with the rest, so nothing is held to
		// ask with; no copy happens either way.
		_, err := f.copyAgain(out.Ref)
		linkWantCode(t, err, appwire.CodeSignedOut)
		if f.clip.count() != 1 {
			t.Fatal("copied after everyone was signed out")
		}
	})
	t.Run("revoked", func(t *testing.T) {
		f, out := held(t)
		if _, err := f.revoke(out.Entry.ID); err != nil {
			t.Fatal(err)
		}
		if n(f) != 0 {
			t.Fatal("still held after the revoke")
		}
		_, err := f.copyAgain(out.Ref)
		linkWantCode(t, err, appwire.CodeNotFound)
	})
	t.Run("ended some other way", func(t *testing.T) {
		f, out := held(t)
		f.mu.Lock()
		f.rows[0].Status = "revoked"
		f.mu.Unlock()
		_, err := f.copyAgain(out.Ref)
		linkWantCode(t, err, appwire.CodeNotFound)
		if n(f) != 0 || f.clip.count() != 1 || f.posts() != 1 {
			t.Fatal("a dead invitation was copied or kept")
		}
	})
	t.Run("no longer the operator", func(t *testing.T) {
		f, out := held(t)
		f.mu.Lock()
		f.me.Operator = false
		f.mu.Unlock()
		_, err := f.copyAgain(out.Ref)
		linkWantCode(t, err, appwire.CodeForbidden)
		if f.clip.count() != 1 {
			t.Fatal("copied for someone who is not the operator")
		}
	})
	t.Run("a new invitation replaces the held one", func(t *testing.T) {
		f, first := held(t)
		f.clip.set(clipboard.ErrFailed)
		second, err := f.create(appwire.FactoryLinkCreateParams{})
		if err != nil || second.Ref == first.Ref || n(f) != 1 {
			t.Fatalf("%+v %v held %d", second, err, n(f))
		}
		_, err = f.copyAgain(first.Ref)
		linkWantCode(t, err, appwire.CodeNotFound)
	})
	t.Run("a copied one drops the old hold", func(t *testing.T) {
		f, first := held(t)
		if _, err := f.create(appwire.FactoryLinkCreateParams{}); err != nil {
			t.Fatal(err)
		}
		_, err := f.copyAgain(first.Ref)
		linkWantCode(t, err, appwire.CodeNotFound)
	})
}

// THE HOLD IS BOUNDED.
func TestLinkHoldIsCappedAndDropsTheSoonestToExpire(t *testing.T) {
	h := testFactoryHub(t)
	now := time.Now()
	for i := 0; i < appwire.LinkHoldMax+9; i++ {
		h.keep(fmt.Sprintf("f%02d", i), &heldLink{ref: fmt.Sprint(i), secret: []byte("s"), until: now.Add(time.Duration(i+1) * time.Minute)})
	}
	if len(h.linkSt.held) != appwire.LinkHoldMax {
		t.Fatalf("%d held, want %d", len(h.linkSt.held), appwire.LinkHoldMax)
	}
	if h.linkSt.held["f00"] != nil || h.linkSt.held["f40"] == nil {
		t.Fatal("the cap dropped the wrong copies")
	}
}

// A HELD COPY CANNOT BE PRINTED.
func TestHeldLinkNeverPrints(t *testing.T) {
	l := &heldLink{ref: "r", secret: []byte(linkTestURL), entry: appwire.FactoryLink{ID: "tok_x"}}
	raw, _ := json.Marshal(map[string]any{"l": l, "v": *l})
	var logged strings.Builder
	slog.New(slog.NewTextHandler(&logged, nil)).Info("x", "l", l, "v", *l)
	text, _ := l.MarshalText()
	for _, s := range []string{
		fmt.Sprintf("%v %+v %#v %s %q %x %d", l, l, l, l, l, l, l), fmt.Sprintf("%v %+v %#v", *l, *l, *l),
		fmt.Sprint(map[string]*heldLink{"a": l}), fmt.Sprint([]heldLink{*l}), string(raw), logged.String(), string(text),
	} {
		if strings.Contains(s, linkTestSecret) || strings.Contains(s, "codeaf://") || strings.Contains(s, "tok_x") {
			t.Fatalf("a held invitation printed itself: %s", s)
		}
	}
}

// ── create: what the factory answered ──────────────────────────────────────

func abort(w http.ResponseWriter, _ *http.Request) { panic(http.ErrAbortHandler) }

func respond(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}

// refuse is a refusal the factory made BEFORE its first change: it carries the
// phase marker (api.PhaseHeader). respond's bare status does not.
func refuse(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}

func goodRow(url string, mutate func(*api.AccessLink)) string {
	row := api.AccessLink{ID: "tok_aabbccdd", Status: "open", MaxUses: 1, Remaining: 1, URL: url, CreatedBy: "alice"}
	if mutate != nil {
		mutate(&row)
	}
	raw, _ := json.Marshal(map[string]any{"link": row})
	return string(raw)
}

// AN AMBIGUOUS CREATE: Uncertain, ONE POST, NO DELETE, NO COPY, NO RETRY; the
// next create is refused without a request until the list has been read; after
// it the create is allowed and says an earlier one may be live.
func TestLinkAmbiguousCreateNeverCopiesDeletesOrRetries(t *testing.T) {
	for name, post := range map[string]func(http.ResponseWriter, *http.Request){
		"connection dropped": abort,
		"500":                respond(500, `{"error":"event write failed"}`),
		"502":                respond(502, ``),
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://elsewhere.example/", http.StatusFound)
		},
		"200 not 201":       respond(200, goodRow(linkTestURL, nil)),
		"204":               respond(204, ``),
		"garbage body":      respond(201, `not json`),
		"empty body":        respond(201, ``),
		"array body":        respond(201, `[]`),
		"no link":           respond(201, `{"factory":{}}`),
		"missing id":        respond(201, goodRow(linkTestURL, func(l *api.AccessLink) { l.ID = "" })),
		"unsafe id":         respond(201, goodRow(linkTestURL, func(l *api.AccessLink) { l.ID = "../x" })),
		"operator row":      respond(201, goodRow(linkTestURL, func(l *api.AccessLink) { l.Operator = true })),
		"not open":          respond(201, goodRow(linkTestURL, func(l *api.AccessLink) { l.Status = "revoked" })),
		"unknown status":    respond(201, goodRow(linkTestURL, func(l *api.AccessLink) { l.Status = "pending" })),
		"no url":            respond(201, goodRow("", nil)),
		"wrong scheme":      respond(201, goodRow("https://factory.example/join#token=abc", nil)),
		"wrong door":        respond(201, goodRow("codeaf://factory/other?url=http%3A%2F%2Ff.example#token=abc", nil)),
		"no token":          respond(201, goodRow("codeaf://factory/join?url=http%3A%2F%2Ff.example", nil)),
		"empty token":       respond(201, goodRow("codeaf://factory/join?url=http%3A%2F%2Ff.example#token=", nil)),
		"bad origin":        respond(201, goodRow("codeaf://factory/join?url=ftp%3A%2F%2Ff.example#token=abc", nil)),
		"origin with login": respond(201, goodRow("codeaf://factory/join?url=http%3A%2F%2Fu%3Ap%40f.example#token=abc", nil)),
		"url with newline":  respond(201, goodRow("codeaf://factory/join?url=http%3A%2F%2Ff.example#token=a\nb", nil)),
		"url too long":      respond(201, goodRow(linkTestURL+strings.Repeat("a", clipboard.MaxText), nil)),
		"truncated body": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "5000")
			w.WriteHeader(201)
			fmt.Fprint(w, `{"link":{"id":"tok_`)
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		},
		"oversize body": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(201)
			fmt.Fprint(w, goodRow(linkTestURL, nil))
			_, _ = w.Write([]byte(strings.Repeat(" ", appFactorySmall+10)))
		},
		"timeout": func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newLinkFactory(t)
			f.post = post
			ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
			defer cancel()
			out, err := f.b.FactoryLinkCreate(ctx, appwire.FactoryLinkCreateParams{Factory: f.id})
			f.record(out, err)
			linkWantCode(t, err, appwire.CodeUncertain)
			if f.posts() != 1 || f.deletes() != 0 || f.clip.count() != 0 {
				t.Fatalf("posts %d deletes %d copies %d", f.posts(), f.deletes(), f.clip.count())
			}
			if out.Ref != "" || out.Entry.ID != "" {
				t.Fatalf("an uncertain create answered %+v", out)
			}
			// No claim that the invitation is missing, and no instruction to retry.
			low := strings.ToLower(err.Error())
			for _, bad := range []string{"was not made", "nothing was made", "try again", "retry"} {
				if strings.Contains(low, bad) {
					t.Fatalf("the words claim too much: %q", err)
				}
			}
			// Another explicit create is refused with no request until the list is read.
			_, err = f.create(appwire.FactoryLinkCreateParams{})
			linkWantCode(t, err, appwire.CodeConflict)
			if f.posts() != 1 {
				t.Fatal("a refused create was sent")
			}
			// A FAILED list does not clear it.
			f.mu.Lock()
			f.listH = respond(500, ``)
			f.mu.Unlock()
			if _, err = f.list(); err == nil {
				t.Fatal("a failed list succeeded")
			}
			_, err = f.create(appwire.FactoryLinkCreateParams{})
			linkWantCode(t, err, appwire.CodeConflict)
			// A successful list clears it, and says an earlier create may be live.
			f.mu.Lock()
			f.listH, f.post = nil, f.mint
			f.mu.Unlock()
			l, err := f.list()
			if err != nil || !l.EarlierUnconfirmed {
				t.Fatalf("list = %+v %v", l, err)
			}
			made, err := f.create(appwire.FactoryLinkCreateParams{})
			if err != nil || !made.EarlierUnconfirmed || made.Outcome != appwire.LinkCopied || f.posts() != 2 {
				t.Fatalf("made = %+v %v posts %d", made, err, f.posts())
			}
			// Once the unconfirmed one could no longer be live, the warning ends.
			f.clock.advance(tokens.HumanLinkTTL + 2*time.Minute)
			l, _ = f.list()
			if l.EarlierUnconfirmed {
				t.Fatal("the warning outlived the longest an invitation can live")
			}
			f.noSecretAnywhere()
		})
	}
}

// A DEFINITE REFUSAL: one POST, no hold, fixed words, no list owed.
func TestLinkDefiniteRefusalsMapToFixedWords(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		code   string
	}{
		"401":            {401, `{"error":"bad session /v1/me/sessions/session_zzz"}`, appwire.CodeSignedOut},
		"401 expired":    {401, `{"reason":"github_expired","error":"x"}`, appwire.CodeGitHubExpired},
		"403":            {403, `{"error":"only the operator can do this"}`, appwire.CodeForbidden},
		"422":            {422, `{"error":"login must be a GitHub handle: SECRET-PROSE"}`, appwire.CodeBadParams},
		"400":            {400, `{"error":"SECRET-PROSE"}`, appwire.CodeBadParams},
		"501":            {501, `{"error":"this factory does not provide that action"}`, appwire.CodeConflict},
		"429":            {429, `{"error":"SECRET-PROSE"}`, appwire.CodeBusy},
		"404":            {404, `SECRET-PROSE`, appwire.CodeNotFound},
		"409":            {409, `{"error":"SECRET-PROSE"}`, appwire.CodeEngine},
		"401 with token": {401, `{"error":"` + linkTestBearer + `"}`, appwire.CodeSignedOut},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLinkFactory(t)
			f.post = refuse(c.status, c.body)
			_, err := f.create(appwire.FactoryLinkCreateParams{})
			linkWantCode(t, err, c.code)
			if strings.Contains(err.Error(), "SECRET-PROSE") || strings.Contains(err.Error(), "session_zzz") || strings.Contains(err.Error(), linkTestBearer) {
				t.Fatalf("the factory's prose was passed on: %v", err)
			}
			if f.posts() != 1 || f.deletes() != 0 || f.clip.count() != 0 {
				t.Fatalf("posts %d deletes %d copies %d", f.posts(), f.deletes(), f.clip.count())
			}
			// Nothing is owed: a refusal made nothing, so the next create is sent.
			f.post = f.mint
			if _, err := f.create(appwire.FactoryLinkCreateParams{}); err != nil || f.posts() != 2 {
				t.Fatalf("after a refusal: %v posts %d", err, f.posts())
			}
		})
	}
}

// A CONNECTION THAT NEVER OPENED PROVES NOTHING WAS SENT, so nothing is owed.
func TestLinkUnreachableIsNotAmbiguous(t *testing.T) {
	f := newLinkFactory(t)
	f.stub.srv.Close()
	_, err := f.create(appwire.FactoryLinkCreateParams{})
	if err == nil {
		t.Fatal("no error")
	}
	if f.hub.mustList(f.id) {
		t.Fatal("an unreachable factory left a list owed")
	}
}

// ── who may ────────────────────────────────────────────────────────────────

func TestLinkGatesFailClosedBeforeAnyWrite(t *testing.T) {
	type edit func(*linkFactory)
	for name, c := range map[string]struct {
		edit edit
		code string
	}{
		"not the operator":   {func(f *linkFactory) { f.me.Operator = false }, appwire.CodeForbidden},
		"solo factory":       {func(f *linkFactory) { f.me.Mode = api.FactorySolo }, appwire.CodeConflict},
		"no mode":            {func(f *linkFactory) { f.me.Mode = "" }, appwire.CodeConflict},
		"older factory":      {func(f *linkFactory) { f.me.GitHubSignInAvailable, f.me.InviteAvailable = false, false }, appwire.CodeConflict},
		"no invite config":   {func(f *linkFactory) { f.me.InviteAvailable = false }, appwire.CodeConflict},
		"no github client":   {func(f *linkFactory) { f.me.GitHubSignInAvailable = false }, appwire.CodeConflict},
		"no clipboard":       {func(f *linkFactory) { f.clip.avail = false }, appwire.CodeConflict},
		"a role is no proof": {func(f *linkFactory) { f.me.Operator, f.me.Handle, f.me.SessionID = false, "alice", "tok_operator" }, appwire.CodeForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLinkFactory(t)
			c.edit(f)
			_, err := f.create(appwire.FactoryLinkCreateParams{})
			linkWantCode(t, err, c.code)
			if f.posts() != 0 || f.clip.count() != 0 {
				t.Fatalf("posts %d copies %d", f.posts(), f.clip.count())
			}
		})
	}
}

// A LEGITIMATE OPERATOR KEY IS NOT A GITHUB SIGN-IN AND NEEDS NONE.
func TestLinkOperatorKeyWithoutAVerifiedProfileMayManageInvitations(t *testing.T) {
	f := newLinkFactory(t)
	f.me.GitHubSignedIn, f.me.SessionID, f.me.Handle = false, "", "host-key"
	if out, err := f.create(appwire.FactoryLinkCreateParams{}); err != nil || out.Outcome != appwire.LinkCopied {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := f.list(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.revoke("tok_aabbccdd"); err != nil {
		t.Fatal(err)
	}
}

// LIST AND REVOKE ARE THE SAME GATE, AND REVOKE NEEDS NO GITHUB CLIENT.
func TestLinkListAndRevokeGates(t *testing.T) {
	f := newLinkFactory(t)
	f.rows = []api.AccessLink{{ID: "tok_aabbccdd", Status: "open", MaxUses: 1}}
	f.me.GitHubSignInAvailable, f.me.InviteAvailable = false, false
	if _, err := f.list(); err != nil {
		t.Fatalf("a list needs no invitation config: %v", err)
	}
	if _, err := f.revoke("tok_aabbccdd"); err != nil {
		t.Fatalf("revoking must work on a factory that lost its GitHub client: %v", err)
	}
	f.me.Operator = false
	_, err := f.list()
	linkWantCode(t, err, appwire.CodeForbidden)
	_, err = f.revoke("tok_aabbccdd")
	linkWantCode(t, err, appwire.CodeForbidden)
	f.me.Operator, f.me.Mode = true, api.FactorySolo
	_, err = f.list()
	linkWantCode(t, err, appwire.CodeConflict)
	_, err = f.revoke("tok_aabbccdd")
	linkWantCode(t, err, appwire.CodeConflict)
	if f.deletes() != 1 {
		t.Fatalf("%d DELETEs, want the one allowed", f.deletes())
	}
}

func TestLinkRefusesTheMachinesOwnFactoryAndAMissingCredential(t *testing.T) {
	f := newLinkFactory(t)
	f.hub.mu.Lock()
	f.hub.rows[0].Local = true
	f.hub.mu.Unlock()
	before := len(f.requests())
	_, err := f.create(appwire.FactoryLinkCreateParams{})
	linkWantCode(t, err, appwire.CodeConflict)
	_, err = f.list()
	linkWantCode(t, err, appwire.CodeConflict)
	_, err = f.revoke("tok_aabbccdd")
	linkWantCode(t, err, appwire.CodeConflict)

	f.hub.mu.Lock()
	f.hub.rows[0].Local = false
	f.hub.mu.Unlock()
	f.hub.dropSession(f.id, linkTestBearer)
	_, err = f.create(appwire.FactoryLinkCreateParams{})
	linkWantCode(t, err, appwire.CodeSignedOut)
	if len(f.requests()) != before {
		t.Fatalf("a refusal sent %v", f.requests()[before:])
	}
}

// ONE PRESS IS ONE REQUEST.
func TestLinkDoublePressIsBusyAndSendsOnePost(t *testing.T) {
	f := newLinkFactory(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.clip.hook = func() { close(entered); <-release }
	done := make(chan error, 1)
	go func() { _, err := f.create(appwire.FactoryLinkCreateParams{}); done <- err }()
	<-entered
	_, err := f.b.FactoryLinkCreate(context.Background(), appwire.FactoryLinkCreateParams{Factory: f.id})
	linkWantCode(t, err, appwire.CodeBusy)
	_, err = f.b.FactoryLinkRevoke(context.Background(), appwire.FactoryLinkRevokeParams{Factory: f.id, ID: "tok_aabbccdd"})
	linkWantCode(t, err, appwire.CodeBusy)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.posts() != 1 {
		t.Fatalf("%d POSTs", f.posts())
	}
}

// A COPIER THAT NEVER ANSWERS IS BOUNDED.
func TestLinkACopierThatNeverAnswersIsUnconfirmedAndHeld(t *testing.T) {
	f := newLinkFactory(t)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	f.clip.hook = func() { <-block }
	start := time.Now()
	out, err := f.create(appwire.FactoryLinkCreateParams{})
	if err != nil || out.Outcome != appwire.LinkUnconfirmed || out.Ref == "" {
		t.Fatalf("%+v %v", out, err)
	}
	if took := time.Since(start); took > appFactoryCopyBudget+3*time.Second {
		t.Fatalf("took %v", took)
	}
}

func TestLinkAPanickingCopierIsUnconfirmedNotACrash(t *testing.T) {
	f := newLinkFactory(t)
	f.clip.hook = func() { panic("copier bug " + linkTestSecret) }
	out, err := f.create(appwire.FactoryLinkCreateParams{})
	if err != nil || out.Outcome != appwire.LinkUnconfirmed || f.deletes() != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	f.noSecretAnywhere()
}

// ── list ───────────────────────────────────────────────────────────────────

func TestLinkListKeepsOnlyPeoplesInvitationsAndNeverInventsCounts(t *testing.T) {
	f := newLinkFactory(t)
	t0 := f.clock.now()
	f.rows = []api.AccessLink{
		{ID: "tok_operator", Operator: true, CreatedAt: t0},
		{ID: "tok_script", CreatedAt: t0},
		{ID: "tok_unknown", Status: "pending", CreatedAt: t0},
		{ID: "../bad", Status: "open", CreatedAt: t0},
		{ID: "tok_old", Status: "revoked", CreatedAt: t0.Add(-time.Hour), MaxUses: 1, Counted: boolPtr(false), UsedBy: []string{"bob", "bo\nb\x00", ""}},
		{ID: "tok_new", Status: "open", CreatedAt: t0, MaxUses: 5, Used: 2, Remaining: 3, Counted: boolPtr(true), Name: "  Contractors\r\n", ExpiresAt: t0.Add(time.Hour)},
		{ID: "tok_legacy", Status: "open", CreatedAt: t0.Add(-time.Minute), MaxUses: 1, Remaining: 1},
	}
	l, err := f.list()
	if err != nil || l.Factory != f.id {
		t.Fatalf("%+v %v", l, err)
	}
	var ids []string
	byID := map[string]appwire.FactoryLink{}
	for _, e := range l.Items {
		ids = append(ids, e.ID)
		byID[e.ID] = e
	}
	if strings.Join(ids, ",") != "tok_new,tok_legacy,tok_old" {
		t.Fatalf("items = %v (open first, newest first, no operator/script/unknown/unsafe rows)", ids)
	}
	if c := byID["tok_new"].Counted; c == nil || !*c {
		t.Fatal("counted lost")
	}
	if c := byID["tok_old"].Counted; c == nil || *c {
		t.Fatal("legacy counted=false lost")
	}
	if byID["tok_legacy"].Counted != nil {
		t.Fatal("an omitted counted was invented")
	}
	if byID["tok_new"].Name != "Contractors" || len(byID["tok_old"].UsedBy) != 1 && byID["tok_old"].UsedBy[1] != "bob" {
		t.Fatalf("text not cleaned: %+v %+v", byID["tok_new"], byID["tok_old"])
	}
	raw, _ := json.Marshal(l)
	for _, bad := range []string{"tok_operator", "tok_script", `"url"`, `"link"`, `"token"`} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("list carries %q: %s", bad, raw)
		}
	}
	if f.posts() != 0 || f.deletes() != 0 {
		t.Fatal("a list wrote")
	}
}

func TestLinkListIsCappedAndNeverNil(t *testing.T) {
	f := newLinkFactory(t)
	l, _ := f.list()
	if l.Items == nil || len(l.Items) != 0 {
		t.Fatalf("empty list = %#v", l.Items)
	}
	for i := 0; i < appwire.LinkMaxRows+20; i++ {
		f.rows = append(f.rows, api.AccessLink{ID: fmt.Sprintf("tok_%04d", i), Status: "open", CreatedAt: f.clock.now().Add(time.Duration(i) * time.Second)})
	}
	l, err := f.list()
	if err != nil || len(l.Items) != appwire.LinkMaxRows || l.Items[0].ID != fmt.Sprintf("tok_%04d", appwire.LinkMaxRows+19) {
		t.Fatalf("%d items %v", len(l.Items), err)
	}
}

func TestLinkListFailuresAreFixedWords(t *testing.T) {
	for name, c := range map[string]struct {
		h    func(http.ResponseWriter, *http.Request)
		code string
	}{
		"500":     {respond(500, `SECRET-PROSE`), appwire.CodeEngine},
		"garbage": {respond(200, `SECRET-PROSE`), appwire.CodeEngine},
		"403":     {respond(403, `SECRET-PROSE`), appwire.CodeForbidden},
		"dropped": {abort, appwire.CodeUnreachable},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLinkFactory(t)
			f.listH = c.h
			_, err := f.list()
			linkWantCode(t, err, c.code)
			if strings.Contains(err.Error(), "SECRET-PROSE") {
				t.Fatalf("prose passed on: %v", err)
			}
		})
	}
}

// ── revoke ─────────────────────────────────────────────────────────────────

func TestLinkRevokeRereadsTheListAndDeletesExactlyOneHumanRow(t *testing.T) {
	f := newLinkFactory(t)
	f.rows = []api.AccessLink{
		{ID: "tok_operator", Operator: true},
		{ID: "tok_script"},
		{ID: "tok_mine", Status: "open", MaxUses: 1},
	}
	for _, id := range []string{"tok_operator", "tok_script", "tok_absent"} {
		_, err := f.revoke(id)
		linkWantCode(t, err, appwire.CodeNotFound)
	}
	if f.deletes() != 0 {
		t.Fatalf("a DELETE was sent for a row that is not a person's invitation: %v", f.requests())
	}
	before := len(f.requests())
	out, err := f.revoke("tok_mine")
	if err != nil || !out.Revoked || out.ID != "tok_mine" || out.Factory != f.id {
		t.Fatalf("%+v %v", out, err)
	}
	if got := f.requests()[before:]; strings.Join(got, "|") != "GET /v1/me|GET /v1/access/links|DELETE /v1/access/links/tok_mine" {
		t.Fatalf("sent %v", got)
	}
}

func TestLinkRevokeOutcomes(t *testing.T) {
	for name, c := range map[string]struct {
		del  func(http.ResponseWriter, *http.Request)
		code string
	}{
		"404 already ended":      {refuse(404, `SECRET-PROSE`), appwire.CodeNotFound},
		"403":                    {refuse(403, ``), appwire.CodeForbidden},
		"401":                    {refuse(401, ``), appwire.CodeSignedOut},
		"404 without the marker": {respond(404, `SECRET-PROSE`), appwire.CodeUncertain},
		"403 without the marker": {respond(403, ``), appwire.CodeUncertain},
		"409 without the marker": {respond(409, ``), appwire.CodeUncertain},
		"500":                    {respond(500, `SECRET-PROSE`), appwire.CodeUncertain},
		"redirect":               {func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://x.example/", 302) }, appwire.CodeUncertain},
		"dropped":                {abort, appwire.CodeUncertain},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLinkFactory(t)
			f.rows = []api.AccessLink{{ID: "tok_mine", Status: "open"}}
			f.del = c.del
			_, err := f.revoke("tok_mine")
			linkWantCode(t, err, c.code)
			if strings.Contains(err.Error(), "SECRET-PROSE") {
				t.Fatalf("prose passed on: %v", err)
			}
			// Sent once, never retried; the person may press again.
			if f.deletes() != 1 {
				t.Fatalf("%d DELETEs", f.deletes())
			}
		})
	}
}

func TestLinkRevokeWithAFailedListSendsNoDelete(t *testing.T) {
	f := newLinkFactory(t)
	f.listH = respond(500, ``)
	_, err := f.revoke("tok_mine")
	if err == nil || f.deletes() != 0 {
		t.Fatalf("%v deletes %d", err, f.deletes())
	}
}

// ── the seam ───────────────────────────────────────────────────────────────

// PRODUCTION DEFAULT: NO CLIPBOARD, SO NO INVITATION IS MADE.
func TestLinkClipboardIsUnavailableUntilARootInstallsOne(t *testing.T) {
	f := newLinkFactory(t)
	f.b.clipboard = nil
	if f.b.copier().Available() {
		t.Fatal("the default clipboard is available")
	}
	caps, _ := f.b.FactoryCapabilities(context.Background())
	if caps.CopyLink || !caps.Links {
		t.Fatalf("caps = %+v", caps)
	}
	_, err := f.create(appwire.FactoryLinkCreateParams{})
	linkWantCode(t, err, appwire.CodeConflict)
	if f.posts() != 0 {
		t.Fatal("an invitation was made with nowhere to put it")
	}
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	if b.clipboard != nil || b.copier().Available() {
		t.Fatal("a fresh backend has a clipboard")
	}
	b.useClipboard(clipboard.NewSystem(false))
	if b.copier().Available() {
		t.Fatal("a system clipboard without the launcher's assertion is available")
	}
	f.b.useClipboard(f.clip)
	if caps, _ = f.b.FactoryCapabilities(context.Background()); !caps.CopyLink {
		t.Fatal("an installed clipboard is not reported")
	}
}

func TestLinkLimitsMatchTheFactorys(t *testing.T) {
	if appwire.LinkMaxUses != tokens.MaxLinkPeople {
		t.Fatalf("max uses %d vs %d", appwire.LinkMaxUses, tokens.MaxLinkPeople)
	}
}

// ── over the wire ──────────────────────────────────────────────────────────

func TestLinkMethodsOverTheWire(t *testing.T) {
	f := newLinkFactory(t)
	hub := testFactoryHub(t)
	hub.now = f.clock.now
	w := newFactoryWire(t, hub)
	w.backend.useClipboard(f.clip)
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": f.stub.srv.URL, "token": linkTestBearer}, &conn)
	if !strings.Contains(w.hello, appwire.FeatureFactoryLinks) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryLinks, w.hello)
	}
	var caps appwire.FactoryCapabilities
	w.must("factory.capabilities", struct{}{}, &caps)
	if !caps.Links || !caps.CopyLink {
		t.Fatalf("caps = %+v", caps)
	}
	var made appwire.FactoryLinkMade
	w.must("factory.link_create", map[string]any{"factory": conn.ID, "name": "Contractors", "max_uses": 3}, &made)
	if made.Outcome != appwire.LinkCopied || made.Entry.ID == "" {
		t.Fatalf("made = %+v", made)
	}
	var list appwire.FactoryLinkList
	w.must("factory.links", map[string]any{"factory": conn.ID}, &list)
	if len(list.Items) != 1 {
		t.Fatalf("list = %+v", list)
	}
	// Anything but the documented params is refused, and nothing is sent.
	before := len(f.requests())
	for _, p := range []map[string]any{
		{"factory": conn.ID, "url": "http://x"}, {"factory": conn.ID, "token": "t"}, {"factory": conn.ID, "path": "/v1/x"},
		{"factory": conn.ID, "body": "{}"}, {"factory": conn.ID, "max_uses": 0}, {"factory": conn.ID, "max_uses": 51},
		{"factory": conn.ID, "login": "a b"}, {"factory": conn.ID, "expires_at": "0001-01-01T00:00:00Z"},
	} {
		w.mustFail("factory.link_create", p, appwire.CodeBadParams)
	}
	for _, id := range []string{"../x", "a/b", "%2e%2e", "tok_a?x=1", "tok a", "", strings.Repeat("a", 65), "tok_é"} {
		w.mustFail("factory.link_revoke", map[string]any{"factory": conn.ID, "id": id}, appwire.CodeBadParams)
	}
	w.mustFail("factory.link_revoke", map[string]any{"factory": conn.ID, "id": "tok_a", "all": true}, appwire.CodeBadParams)
	for _, ref := range []string{"", "short", strings.Repeat("a", 23), "../../../../etc/passwd!!"} {
		w.mustFail("factory.link_copy", map[string]any{"factory": conn.ID, "ref": ref}, appwire.CodeBadParams)
	}
	w.mustFail("factory.links", map[string]any{"factory": conn.ID, "all": true}, appwire.CodeBadParams)
	if got := f.requests(); len(got) != before {
		t.Fatalf("a refused request reached the factory: %v", got[before:])
	}
	w.neverSaid(linkTestSecret, "codeaf://", linkTestBearer, "#token")
	for _, line := range w.lines {
		if strings.Contains(line, `"url"`) && strings.Contains(line, "factory.link") {
			t.Fatalf("a link frame carries a url field: %.200s", line)
		}
	}
}

// ── against the real server ────────────────────────────────────────────────

// THE WHOLE JOURNEY AGAINST THE REAL FACTORY: an invitation is made with the
// factory's own defaults, its URL goes only to the clipboard and redeems at the
// factory, it lists as counted, a member may not, and revoking ends it.
func TestLinkJourneyAgainstTheRealServer(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	bob := f.signIn(t, 2, "bob")
	clip := &fakeCopier{avail: true}
	alice.w.backend.useClipboard(clip)
	bob.w.backend.useClipboard(clip)
	f.since()

	var made appwire.FactoryLinkMade
	alice.w.must("factory.link_create", map[string]any{"factory": alice.conn.ID}, &made)
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me|POST /v1/access/links" {
		t.Fatalf("sent %v", got)
	}
	if made.Outcome != appwire.LinkCopied || made.Ref != "" || made.Entry.MaxUses != 1 || made.Entry.Status != "open" {
		t.Fatalf("made = %+v", made)
	}
	if made.Entry.Counted == nil || !*made.Entry.Counted || made.Entry.ExpiresAt == nil || made.Entry.ExpiresAt.Sub(f.clock.now()) != tokens.HumanLinkTTL {
		t.Fatalf("defaults: %+v", made.Entry)
	}
	if len(clip.calls) != 1 {
		t.Fatalf("copied %d times", len(clip.calls))
	}
	link, err := parseFactoryJoinLink(clip.calls[0])
	if err != nil || link.base != adminFactoryAddress || len(link.secret) < 32 {
		t.Fatalf("the copied text is not a join link to this factory: %v %+v", err, link)
	}
	alice.w.neverSaid(link.secret, clip.calls[0], "codeaf://", "#token")

	// It is a live invitation at the factory.
	var access api.FactoryAccess
	req, _ := http.NewRequest(http.MethodGet, f.URL+"/v1/factory-access?link="+url.QueryEscape(clip.calls[0]), nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&access)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || access.State == "expired" || access.State == "revoked" {
		t.Fatalf("the invitation does not redeem: %d %+v", resp.StatusCode, access)
	}

	var list appwire.FactoryLinkList
	alice.w.must("factory.links", map[string]any{"factory": alice.conn.ID}, &list)
	if len(list.Items) != 1 || list.Items[0].ID != made.Entry.ID || list.EarlierUnconfirmed {
		t.Fatalf("list = %+v", list)
	}
	alice.w.neverSaid(link.secret)

	// A member is refused before anything is sent.
	f.since()
	bob.w.mustFail("factory.link_create", map[string]any{"factory": bob.conn.ID}, appwire.CodeForbidden)
	bob.w.mustFail("factory.links", map[string]any{"factory": bob.conn.ID}, appwire.CodeForbidden)
	bob.w.mustFail("factory.link_revoke", map[string]any{"factory": bob.conn.ID, "id": made.Entry.ID}, appwire.CodeForbidden)
	for _, g := range f.since() {
		if g != "GET /v1/me" {
			t.Fatalf("a member's refusal sent %s", g)
		}
	}

	var gone appwire.FactoryLinkRevoked
	alice.w.must("factory.link_revoke", map[string]any{"factory": alice.conn.ID, "id": made.Entry.ID}, &gone)
	if !gone.Revoked {
		t.Fatalf("gone = %+v", gone)
	}
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me|GET /v1/access/links|DELETE /v1/access/links/"+made.Entry.ID {
		t.Fatalf("sent %v", got)
	}
	alice.w.must("factory.links", map[string]any{"factory": alice.conn.ID}, &list)
	if len(list.Items) != 1 || list.Items[0].Status != "revoked" {
		t.Fatalf("list after revoke = %+v", list)
	}
	// A second revoke is the factory's own "already ended", and is not retried.
	alice.w.mustFail("factory.link_revoke", map[string]any{"factory": alice.conn.ID, "id": made.Entry.ID}, appwire.CodeNotFound)
	alice.w.neverSaid(link.secret, testHostToken)
}

// THE REAL SERVER'S OWN VALIDATION AGREES WITH THE ENGINE'S: what the engine
// lets through, the factory accepts.
func TestLinkTypedChoicesAreAcceptedByTheRealServer(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	alice.w.backend.useClipboard(&fakeCopier{avail: true})
	three := 3
	exp := f.clock.now().Add(36 * time.Hour)
	var made appwire.FactoryLinkMade
	alice.w.must("factory.link_create", map[string]any{"factory": alice.conn.ID, "name": "Contractors", "max_uses": three, "expires_at": exp}, &made)
	if made.Entry.MaxUses != 3 || made.Entry.Name != "Contractors" || made.Entry.ExpiresAt == nil || !made.Entry.ExpiresAt.Equal(exp) {
		t.Fatalf("entry = %+v", made.Entry)
	}
	for _, login := range []string{"@Priya", "dependabot[bot]", "a-b"} {
		alice.w.must("factory.link_create", map[string]any{"factory": alice.conn.ID, "login": login}, &made)
		if made.Entry.Login != strings.ToLower(strings.TrimPrefix(login, "@")) {
			t.Fatalf("%s: %+v", login, made.Entry)
		}
	}
	edge := f.clock.now().Add(tokens.HumanLinkTTL)
	alice.w.must("factory.link_create", map[string]any{"factory": alice.conn.ID, "expires_at": edge, "max_uses": appwire.LinkMaxUses}, &made)
	if made.Entry.MaxUses != appwire.LinkMaxUses {
		t.Fatalf("%+v", made.Entry)
	}
}

// A HOST KEY IS NO GITHUB SIGN-IN. If the factory says it is the operator, it
// manages invitations; the engine does not ask for a profile.
func TestLinkHostKeyOperatorNeedsNoVerifiedProfile(t *testing.T) {
	f := startAdminFactory(t)
	hub := testFactoryHub(t)
	// E5: the host-key contract must not expire when the wall clock passes
	// the invitation made by the factory's fixed clock.
	hub.now = f.clock.now
	w := newFactoryWire(t, hub)
	w.backend.useClipboard(&fakeCopier{avail: true})
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": f.URL, "token": testHostToken}, &conn)
	var me appwire.FactoryMe
	w.must("factory.me", map[string]any{"factory": conn.ID}, &me)
	if me.GitHubSignedIn || !me.GitHubSignInAvailable || !me.InviteAvailable {
		t.Fatalf("me = %+v", me)
	}
	if !me.Operator {
		t.Fatalf("the host key is not an operator in team mode: %+v", me)
	}
	var made appwire.FactoryLinkMade
	w.must("factory.link_create", map[string]any{"factory": conn.ID}, &made)
	if made.Outcome != appwire.LinkCopied {
		t.Fatalf("made = %+v", made)
	}
}

// ── a 4xx does not prove nothing was written ───────────────────────────────

// THE FACTORY STORES AN INVITATION AND THEN RECORDS THE EVENT, and a failure of
// the second step is a 403, 404, 408 or 409. Without the phase marker that is
// uncertain: the list is owed, nothing is copied, deleted or made again.
func TestLinkUnmarkedRefusalsAfterTheCreateAreUncertain(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 408, 409, 422, 429, 501} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newLinkFactory(t)
			f.post = respond(status, `{"error":"SECRET-PROSE"}`)
			out, err := f.create(appwire.FactoryLinkCreateParams{})
			linkWantCode(t, err, appwire.CodeUncertain)
			if strings.Contains(err.Error(), "SECRET-PROSE") || out.Ref != "" || out.Entry.ID != "" {
				t.Fatalf("%v %+v", err, out)
			}
			if f.posts() != 1 || f.deletes() != 0 || f.clip.count() != 0 {
				t.Fatalf("posts %d deletes %d copies %d", f.posts(), f.deletes(), f.clip.count())
			}
			if !f.hub.mustList(f.id) {
				t.Fatal("no list was owed after a refusal that may have followed a write")
			}
			f.post = f.mint
			_, err = f.create(appwire.FactoryLinkCreateParams{})
			linkWantCode(t, err, appwire.CodeConflict)
			if f.posts() != 1 {
				t.Fatal("a second invitation was requested before the list was read")
			}
		})
	}
}

// A marked 2xx, 3xx or 5xx is still not a refusal.
func TestLinkMarkerDoesNotMakeANonRefusalDefinite(t *testing.T) {
	for _, status := range []int{200, 302, 500, 502} {
		f := newLinkFactory(t)
		f.post = refuse(status, ``)
		_, err := f.create(appwire.FactoryLinkCreateParams{})
		linkWantCode(t, err, appwire.CodeUncertain)
	}
}

// ── the invitation must be the one that was asked for ──────────────────────

func TestLinkMadeWithOtherLimitsIsNeverCopiedOrHeld(t *testing.T) {
	two := 2
	for name, c := range map[string]struct {
		ask    appwire.FactoryLinkCreateParams
		mutate func(*api.AccessLink, time.Time)
	}{
		"more people":       {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, _ time.Time) { l.MaxUses = 50 }},
		"fewer people":      {appwire.FactoryLinkCreateParams{MaxUses: &two}, func(l *api.AccessLink, _ time.Time) { l.MaxUses = 1 }},
		"already used":      {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, _ time.Time) { l.Used = 1 }},
		"other login":       {appwire.FactoryLinkCreateParams{Login: "priya"}, func(l *api.AccessLink, _ time.Time) { l.Login = "someone-else" }},
		"login not asked":   {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, _ time.Time) { l.Login = "priya" }},
		"login dropped":     {appwire.FactoryLinkCreateParams{Login: "priya"}, func(l *api.AccessLink, _ time.Time) { l.Login = "" }},
		"other name":        {appwire.FactoryLinkCreateParams{Name: "Ops"}, func(l *api.AccessLink, _ time.Time) { l.Name = "Else" }},
		"no expiry":         {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, _ time.Time) { l.ExpiresAt = time.Time{} }},
		"thirty days":       {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, now time.Time) { l.ExpiresAt = now.Add(30 * 24 * time.Hour) }},
		"already expired":   {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, now time.Time) { l.ExpiresAt = now.Add(-time.Hour) }},
		"not the asked day": {appwire.FactoryLinkCreateParams{ExpiresAt: ptrTime(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))}, func(l *api.AccessLink, now time.Time) { l.ExpiresAt = now.Add(5 * 24 * time.Hour) }},
		"short secret": {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, _ time.Time) {
			l.URL = "codeaf://factory/join?url=http%3A%2F%2Ff.example#token=abc"
		}},
		"upper-case secret": {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, _ time.Time) { l.URL = strings.ToUpper(linkTestURL) }},
		"non-hex secret":    {appwire.FactoryLinkCreateParams{}, func(l *api.AccessLink, _ time.Time) { l.URL = strings.Replace(linkTestURL, "0123", "zzzz", 1) }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLinkFactory(t)
			now := f.clock.now()
			f.post = func(w http.ResponseWriter, r *http.Request) {
				row := api.AccessLink{ID: "tok_aabbccdd", CreatedAt: now, ExpiresAt: now.Add(tokens.HumanLinkTTL), CreatedBy: "alice",
					MaxUses: 1, Remaining: 1, Status: "open", URL: linkTestURL, Name: c.ask.Name, Login: c.ask.Login}
				if c.ask.MaxUses != nil {
					row.MaxUses, row.Remaining = *c.ask.MaxUses, *c.ask.MaxUses
				}
				if c.ask.ExpiresAt != nil {
					row.ExpiresAt = *c.ask.ExpiresAt
				}
				c.mutate(&row, now)
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"link": row})
			}
			out, err := f.create(c.ask)
			linkWantCode(t, err, appwire.CodeUncertain)
			if f.clip.count() != 0 || out.Ref != "" || out.Entry.ID != "" {
				t.Fatalf("copies %d out %+v", f.clip.count(), out)
			}
			if f.posts() != 1 || f.deletes() != 0 || !f.hub.mustList(f.id) {
				t.Fatalf("posts %d deletes %d mustList %v", f.posts(), f.deletes(), f.hub.mustList(f.id))
			}
			f.hub.mu.Lock()
			n := len(f.hub.linkMem().held)
			f.hub.mu.Unlock()
			if n != 0 {
				t.Fatal("a mismatched invitation was held")
			}
			f.noSecretAnywhere()
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// ── the caller is checked after every wait ─────────────────────────────────

// A CHANGED CALLER LEAVES THE INVITATION UNCERTAIN: nothing is deleted or made
// again, the list is owed, no clipboard write, no ref, nothing held.
func TestLinkCreateCallerChangedDuringPostStaysUncertain(t *testing.T) {
	for _, change := range []string{"replaced", "signed_out", "detached", "closed"} {
		t.Run(change, func(t *testing.T) {
			f := newLinkFactory(t)
			f.post = func(w http.ResponseWriter, r *http.Request) {
				switch change {
				case "replaced":
					f.hub.hold(f.id, "replacement-caller-fixture-5ac01e22", 0)
				case "signed_out":
					f.hub.dropSession(f.id, linkTestBearer)
				case "detached":
					_ = f.b.FactoryDetach(context.Background(), f.id)
				case "closed":
					f.hub.forgetAll()
				}
				f.mint(w, r)
			}
			out, err := f.create(appwire.FactoryLinkCreateParams{})
			linkWantCode(t, err, appwire.CodeUncertain)
			if f.clip.count() != 0 || out.Ref != "" || out.Entry.ID != "" {
				t.Fatalf("copies %d out %+v", f.clip.count(), out)
			}
			if f.posts() != 1 || f.deletes() != 0 || !f.hub.mustList(f.id) {
				t.Fatalf("posts %d deletes %d mustList %v", f.posts(), f.deletes(), f.hub.mustList(f.id))
			}
			f.hub.mu.Lock()
			n := len(f.hub.linkMem().held)
			f.hub.mu.Unlock()
			if n != 0 {
				t.Fatal("the earlier caller's secret was kept")
			}
			if !f.hub.earlierUnknown(f.id) {
				t.Fatal("the possibly-live invitation was not remembered")
			}
			f.noSecretAnywhere()
		})
	}
}

// THE CALLER CHANGES WHILE THE CLIPBOARD RUNS: it cannot be undone, but nothing
// of it is kept or reported, and the list is owed.
func TestLinkCreateCallerChangedDuringCopyKeepsAndReportsNothing(t *testing.T) {
	for _, copyErr := range []error{nil, clipboard.ErrFailed} {
		f := newLinkFactory(t)
		f.clip.set(copyErr)
		f.clip.hook = func() { f.hub.hold(f.id, "replacement-caller-fixture-77d0b1c4", 0) }
		out, err := f.create(appwire.FactoryLinkCreateParams{})
		linkWantCode(t, err, appwire.CodeUncertain)
		if out.Ref != "" || out.Entry.ID != "" || f.posts() != 1 || f.deletes() != 0 || !f.hub.mustList(f.id) {
			t.Fatalf("%+v posts %d deletes %d", out, f.posts(), f.deletes())
		}
		f.hub.mu.Lock()
		n := len(f.hub.linkMem().held)
		f.hub.mu.Unlock()
		if n != 0 {
			t.Fatal("the earlier caller's secret was kept")
		}
		f.noSecretAnywhere()
	}
}

// keepFor stores only for the caller that still holds the connection.
func TestKeepForStoresOnlyForTheCurrentCaller(t *testing.T) {
	f := newLinkFactory(t)
	l := &heldLink{ref: "r", secret: []byte("s"), until: f.clock.now().Add(time.Minute)}
	if f.hub.keepFor(f.id, "not-the-holder", l) || l.secret != nil {
		t.Fatal("a copy was stored for a caller that does not hold the connection")
	}
	l = &heldLink{ref: "r", secret: []byte("s"), until: f.clock.now().Add(time.Minute)}
	if !f.hub.keepFor(f.id, linkTestBearer, l) {
		t.Fatal("the current caller's copy was refused")
	}
}

func TestLinkCopyDoesNotCopyAfterCallerChangesDuringList(t *testing.T) {
	for _, change := range []string{"replaced", "signed_out", "detached", "closed", "revoked_all"} {
		t.Run(change, func(t *testing.T) {
			f := newLinkFactory(t)
			f.clip.set(clipboard.ErrFailed)
			first, err := f.create(appwire.FactoryLinkCreateParams{})
			if err != nil || first.Ref == "" {
				t.Fatalf("expected a held invitation: %+v %v", first, err)
			}
			before := f.clip.count()
			f.clip.set(nil)
			f.mu.Lock()
			f.listH = func(w http.ResponseWriter, r *http.Request) {
				switch change {
				case "replaced":
					f.hub.hold(f.id, "replacement-copy-caller-fixture-1b9c0d3e", 0)
				case "signed_out":
					f.hub.dropSession(f.id, linkTestBearer)
				case "detached":
					_ = f.b.FactoryDetach(context.Background(), f.id)
				case "closed":
					f.hub.forgetAll()
				case "revoked_all":
					f.hub.dropHeld(f.id)
				}
				f.mu.Lock()
				rows := append([]api.AccessLink(nil), f.rows...)
				f.mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{"links": rows})
			}
			f.mu.Unlock()
			out, err := f.copyAgain(first.Ref)
			if err == nil {
				t.Fatal("a changed caller was told the invitation was copied")
			}
			if f.clip.count() != before {
				t.Fatal("a stale held secret was copied after the wait")
			}
			if out.Entry.ID != "" || out.Ref != "" || f.posts() != 1 || f.deletes() != 0 {
				t.Fatalf("out %+v posts %d deletes %d", out, f.posts(), f.deletes())
			}
			f.noSecretAnywhere()
		})
	}
}

// THE CALLER CHANGES WHILE A COPY OF A HELD INVITATION RUNS: the answer carries
// no ref and no metadata, and the copy that was held is gone.
func TestLinkCopyCallerChangedDuringCopyReportsNothing(t *testing.T) {
	f := newLinkFactory(t)
	f.clip.set(clipboard.ErrFailed)
	first, err := f.create(appwire.FactoryLinkCreateParams{})
	if err != nil || first.Ref == "" {
		t.Fatal("expected a held invitation")
	}
	f.clip.set(nil)
	f.clip.hook = func() { f.hub.hold(f.id, "replacement-copy-caller-fixture-90aa5e7d", 0) }
	out, err := f.copyAgain(first.Ref)
	if err == nil || out.Entry.ID != "" || out.Ref != "" {
		t.Fatalf("%+v %v", out, err)
	}
	f.noSecretAnywhere()
}

func TestLinkRevokeSendsNoDeleteOnceTheCallerChanged(t *testing.T) {
	f := newLinkFactory(t)
	f.rows = []api.AccessLink{{ID: "tok_mine", Status: "open", MaxUses: 1}}
	f.listH = func(w http.ResponseWriter, r *http.Request) {
		f.hub.hold(f.id, "replacement-revoke-caller-fixture-4e6a28b1", 0)
		_ = json.NewEncoder(w).Encode(map[string]any{"links": f.rows})
	}
	if _, err := f.revoke("tok_mine"); err == nil || f.deletes() != 0 {
		t.Fatalf("%v deletes %d", err, f.deletes())
	}
}
