package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── a stub factory: a scripted /v1/me and a scripted repository switch ─────
//
// Nothing here is a model, a worker or a live service: httptest only. The stub
// logs every request so a test can say exactly what was sent, and how often.

type repoStub struct {
	t   *testing.T
	srv *httptest.Server
	mu  sync.Mutex
	log []string // "METHOD escaped-path"
	// the last PUT's bearer and body
	auths, bodies []string
	me            api.Me
	meStatus      int
	meBody        string
	// put answers the switch; the default is a plain 200.
	put func(w http.ResponseWriter, r *http.Request)
	// onMe and onPut run at the door, before the answer.
	onMe, onPut func()
}

func newRepoStub(t *testing.T, me api.Me) *repoStub {
	t.Helper()
	s := &repoStub{t: t, me: me, meStatus: http.StatusOK}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.log = append(s.log, r.Method+" "+r.URL.EscapedPath())
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			s.auths = append(s.auths, r.Header.Get("Authorization"))
			s.bodies = append(s.bodies, string(body))
		}
		me, status, meBody, onMe, onPut, put := s.me, s.meStatus, s.meBody, s.onMe, s.onPut, s.put
		s.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/me":
			if onMe != nil {
				onMe()
			}
			if status != http.StatusOK {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"/v1/me/sessions/session_secret_id leaked"}`)
				return
			}
			if meBody != "" {
				_, _ = io.WriteString(w, meBody)
				return
			}
			_ = json.NewEncoder(w).Encode(me)
		case r.Method == http.MethodPut:
			if onPut != nil {
				onPut()
			}
			if put != nil {
				put(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"factory":{}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *repoStub) puts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.log {
		if strings.HasPrefix(l, "PUT ") {
			n++
		}
	}
	return n
}

func (s *repoStub) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

func repoMarked(status int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"/v1/repos/octo/hello is a path the factory echoes"}`)
	}
}

func repoBare(status int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"/v1/repos/octo/hello is a path the factory echoes"}`)
	}
}

const repoToken = "tok_held_for_repos_0123456789"

// repoWire is a backend whose registry holds one connection to stub, with the
// credential token held for it (none when token is "").
type repoWire struct {
	*factoryWire
	hub *appFactoryHub
	id  string
}

func newRepoWire(t *testing.T, stub *repoStub, local bool, mode, token string) *repoWire {
	t.Helper()
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)
	hub.resume = nil // nothing may start a local factory behind these tests
	id := appFactoryID(stub.srv.URL)
	if local {
		id = appLocalID
	}
	hub.mu.Lock()
	hub.rows = []appFactoryRow{{ID: id, URL: stub.srv.URL, Name: "Acme", Mode: mode, Local: local}}
	hub.loaded = true
	hub.mu.Unlock()
	if token != "" {
		hub.hold(id, token, 0)
	}
	return &repoWire{factoryWire: w, hub: hub, id: id}
}

func (r *repoWire) set(repo string, on bool) (appwire.FactoryRepoSetDone, error) {
	return r.backend.FactoryRepoSet(context.Background(), appwire.FactoryRepoSetParams{Factory: r.id, Repo: repo, On: &on})
}

func teamOperator() api.Me {
	return api.Me{Mode: api.FactoryTeam, Operator: true, Profile: api.Profile{Handle: "alice"}}
}

func repoWantCode(t *testing.T, err error, code string) *appwire.Error {
	t.Helper()
	if got := wireCodeOf(t, err); got != code {
		t.Fatalf("code %q (%v), want %q", got, err, code)
	}
	var w *appwire.Error
	asWire(err, &w)
	return w
}

// ── the yes path ───────────────────────────────────────────────────────────

func TestRepoSetSendsOnePutForTheHeldOperatorAndAnswersTheReceipt(t *testing.T) {
	for _, on := range []bool{true, false} {
		for _, status := range []int{http.StatusOK, http.StatusNoContent} {
			stub := newRepoStub(t, teamOperator())
			stub.put = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }
			r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
			got, err := r.set("octo/Hello.x_y", on)
			if err != nil {
				t.Fatalf("on=%v status=%d: %v", on, status, err)
			}
			if got != (appwire.FactoryRepoSetDone{Factory: r.id, Repo: "octo/Hello.x_y", On: on}) {
				t.Fatalf("receipt %+v", got)
			}
			want := []string{"GET /v1/me", "PUT /v1/repos/octo/Hello.x_y"}
			if log := stub.requests(); strings.Join(log, "|") != strings.Join(want, "|") {
				t.Fatalf("requests %v, want %v", log, want)
			}
			wantBody := `{"on":false}`
			if on {
				wantBody = `{"on":true}`
			}
			if stub.auths[0] != "Bearer "+repoToken || stub.bodies[0] != wantBody {
				t.Fatalf("PUT sent %q with %q", stub.auths[0], stub.bodies[0])
			}
		}
	}
}

func TestRepoSetOverTheWireAnswersExactlyTheReceiptAndRefusesExtras(t *testing.T) {
	stub := newRepoStub(t, teamOperator())
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	var out map[string]any
	r.must("factory.repo_set", map[string]any{"factory": r.id, "repo": "octo/hello", "on": true}, &out)
	if len(out) != 3 || out["factory"] != r.id || out["repo"] != "octo/hello" || out["on"] != true {
		t.Fatalf("receipt %v", out)
	}
	before := len(stub.requests())
	for _, params := range []map[string]any{
		{"factory": r.id, "repo": "octo/hello"},
		{"factory": r.id, "repo": "octo/hello", "on": true, "path": "/v1/repos"},
		{"factory": r.id, "repo": "octo/hello", "on": true, "verb": "DELETE"},
		{"factory": r.id, "repo": "octo/../x", "on": true},
		{"factory": r.id, "repo": "octo/hello/pipeline", "on": true},
	} {
		r.mustFail("factory.repo_set", params, appwire.CodeBadParams)
	}
	if len(stub.requests()) != before {
		t.Fatalf("a bad request reached the factory: %v", stub.requests())
	}
	r.neverSaid(repoToken)
}

func TestRepoSetServesSoloKeyAndTheOwnedLocalFactory(t *testing.T) {
	solo := api.Me{Mode: api.FactorySolo, Operator: true}
	for name, c := range map[string]struct {
		local bool
		mode  string
	}{"attached solo": {false, api.FactorySolo}, "owned local": {true, api.FactorySolo}, "local with no mode learned": {true, ""}} {
		t.Run(name, func(t *testing.T) {
			stub := newRepoStub(t, solo)
			r := newRepoWire(t, stub, c.local, c.mode, repoToken)
			if _, err := r.set("octo/hello", true); err != nil {
				t.Fatal(err)
			}
			if stub.puts() != 1 || stub.auths[0] != "Bearer "+repoToken {
				t.Fatalf("requests %v auths %v", stub.requests(), stub.auths)
			}
		})
	}
}

// ── who may ────────────────────────────────────────────────────────────────

func TestRepoSetRefusesAMemberBeforeAnyWriteAndAsksAgainEveryCall(t *testing.T) {
	member := teamOperator()
	member.Operator = false
	stub := newRepoStub(t, member)
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	_, err := r.set("octo/hello", true)
	repoWantCode(t, err, appwire.CodeForbidden)
	if stub.puts() != 0 {
		t.Fatalf("a member's request was sent: %v", stub.requests())
	}
	// The role is read fresh each time: promoted, the next call is allowed;
	// demoted again, the call after is refused.
	stub.mu.Lock()
	stub.me.Operator = true
	stub.mu.Unlock()
	if _, err := r.set("octo/hello", true); err != nil {
		t.Fatal(err)
	}
	stub.mu.Lock()
	stub.me.Operator = false
	stub.mu.Unlock()
	_, err = r.set("octo/hello", true)
	repoWantCode(t, err, appwire.CodeForbidden)
	if stub.puts() != 1 {
		t.Fatalf("puts = %d, want 1", stub.puts())
	}
}

func TestRepoSetNeedsAHeldCredentialAndSendsNothingWithout(t *testing.T) {
	for name, c := range map[string]struct {
		local bool
		mode  string
		code  string
	}{
		"team":             {false, api.FactoryTeam, appwire.CodeSignedOut},
		"mode not learned": {false, "", appwire.CodeSignedOut},
		"solo":             {false, api.FactorySolo, appwire.CodeSignedOut},
		"owned local":      {true, api.FactorySolo, appwire.CodeUnreachable},
	} {
		t.Run(name, func(t *testing.T) {
			// The stub answers a no-bearer request as an operator, the way a
			// loopback team service answers as its host.
			stub := newRepoStub(t, teamOperator())
			r := newRepoWire(t, stub, c.local, c.mode, "")
			_, err := r.set("octo/hello", true)
			repoWantCode(t, err, c.code)
			if got := stub.requests(); len(got) != 0 {
				t.Fatalf("a request left with no credential: %v", got)
			}
			// An expired credential is no credential either.
			r.hub.hold(r.id, repoToken, time.Nanosecond)
			time.Sleep(2 * time.Millisecond)
			_, err = r.set("octo/hello", true)
			repoWantCode(t, err, c.code)
			if got := stub.requests(); len(got) != 0 {
				t.Fatalf("a request left with an expired credential: %v", got)
			}
		})
	}
}

func TestRepoSetUsesNoAmbientHostKeyOrAttachRecord(t *testing.T) {
	t.Setenv("CODEAF_FACTORY_TOKEN", "tok_ambient_host_key")
	t.Setenv("CODEAF_HOST_TOKEN", "tok_ambient_host_key")
	stub := newRepoStub(t, teamOperator())
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	if _, err := r.set("octo/hello", true); err != nil {
		t.Fatal(err)
	}
	for _, a := range append(stub.auths, "Bearer "+repoToken) {
		if strings.Contains(a, "ambient") {
			t.Fatalf("an ambient key was sent: %s", a)
		}
	}
}

func TestRepoSetUnknownConnectionIsNotFoundAndNothingIsSent(t *testing.T) {
	stub := newRepoStub(t, teamOperator())
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	on := true
	_, err := r.backend.FactoryRepoSet(context.Background(), appwire.FactoryRepoSetParams{Factory: "factory_nope", Repo: "octo/hello", On: &on})
	repoWantCode(t, err, appwire.CodeNotFound)
	if len(stub.requests()) != 0 {
		t.Fatalf("sent %v", stub.requests())
	}
}

// ── reading who the caller is fails ────────────────────────────────────────

func TestRepoSetReadFailuresSendNoWriteAndLeakNothing(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		code   string
	}{
		"signed out":   {http.StatusUnauthorized, "", appwire.CodeSignedOut},
		"forbidden":    {http.StatusForbidden, "", appwire.CodeEngine},
		"server error": {http.StatusInternalServerError, "", appwire.CodeEngine},
		"unreadable":   {http.StatusOK, "not json", appwire.CodeEngine},
		"redirected":   {http.StatusFound, "", appwire.CodeUnreachable},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newRepoStub(t, teamOperator())
			stub.meStatus, stub.meBody = c.status, c.body
			r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
			_, err := r.set("octo/hello", true)
			w := repoWantCode(t, err, c.code)
			if stub.puts() != 0 {
				t.Fatalf("a write followed a failed read: %v", stub.requests())
			}
			if strings.Contains(w.Message, "session_secret_id") || strings.Contains(w.Message, repoToken) {
				t.Fatalf("the message leaks: %q", w.Message)
			}
		})
	}
}

func TestRepoSetUnreachableFactoryIsUnreachableAndNothingWasSent(t *testing.T) {
	stub := newRepoStub(t, teamOperator())
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	stub.srv.Close()
	_, err := r.set("octo/hello", true)
	repoWantCode(t, err, appwire.CodeUnreachable)
}

// ── refusals ───────────────────────────────────────────────────────────────

func TestRepoSetDefiniteRefusalsNeedTheMarkerAndUseFixedWords(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		code   string
	}{
		"forbidden":    {http.StatusForbidden, appwire.CodeForbidden},
		"not found":    {http.StatusNotFound, appwire.CodeNotFound},
		"bad name":     {http.StatusUnprocessableEntity, appwire.CodeBadParams},
		"bad request":  {http.StatusBadRequest, appwire.CodeBadParams},
		"signed out":   {http.StatusUnauthorized, appwire.CodeSignedOut},
		"busy":         {http.StatusTooManyRequests, appwire.CodeBusy},
		"not provided": {http.StatusNotImplemented, appwire.CodeConflict},
		"conflict":     {http.StatusConflict, appwire.CodeEngine},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newRepoStub(t, teamOperator())
			stub.put = repoMarked(c.status)
			r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
			_, err := r.set("octo/hello", true)
			w := repoWantCode(t, err, c.code)
			if stub.puts() != 1 {
				t.Fatalf("puts = %d", stub.puts())
			}
			if strings.Contains(w.Message, "/v1/") || strings.Contains(w.Message, "octo") {
				t.Fatalf("the factory's own words were passed on: %q", w.Message)
			}
		})
	}
}

// A STATUS WITHOUT THE MARKER MAY FOLLOW A STORED WRITE, so it is uncertain,
// whatever it looks like, and nothing is repeated.
func TestRepoSetUnmarkedOrUnexpectedAnswersAreUncertainAndSentOnce(t *testing.T) {
	for name, put := range map[string]func(http.ResponseWriter, *http.Request){
		"403":        repoBare(http.StatusForbidden),
		"404":        repoBare(http.StatusNotFound),
		"408":        repoBare(http.StatusRequestTimeout),
		"409":        repoBare(http.StatusConflict),
		"422":        repoBare(http.StatusUnprocessableEntity),
		"501":        repoBare(http.StatusNotImplemented),
		"500":        repoBare(http.StatusInternalServerError),
		"marked 500": repoMarked(http.StatusInternalServerError),
		"marked 200 is not so": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
			w.WriteHeader(http.StatusAccepted)
		},
		"202 accepted": repoBare(http.StatusAccepted),
		"206 partial":  repoBare(http.StatusPartialContent),
		"301 redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
		},
		"307 redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
		},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newRepoStub(t, teamOperator())
			stub.put = put
			r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
			got, err := r.set("octo/hello", true)
			w := repoWantCode(t, err, appwire.CodeUncertain)
			if got != (appwire.FactoryRepoSetDone{}) {
				t.Fatalf("a receipt came with uncertainty: %+v", got)
			}
			if stub.puts() != 1 || len(stub.requests()) != 2 {
				t.Fatalf("not sent exactly once: %v", stub.requests())
			}
			if strings.Contains(w.Message, "/v1/") || strings.Contains(w.Message, "octo") {
				t.Fatalf("the message names the repository or the factory's words: %q", w.Message)
			}
		})
	}
}

// THE STORE SUCCEEDED AND THE EVENT FAILED: the stub keeps the list, then
// answers 403 with no marker, as the real factory does when it cannot record
// the event. The engine must not call that a refusal and must not repeat it.
func TestRepoSetStoredThenFailedIsUncertainAndNeverRepeated(t *testing.T) {
	stub := newRepoStub(t, teamOperator())
	watched := map[string]bool{}
	stub.put = func(w http.ResponseWriter, r *http.Request) {
		var body struct{ On bool }
		_ = json.NewDecoder(strings.NewReader(stub.bodies[len(stub.bodies)-1])).Decode(&body)
		watched["octo/hello"] = body.On      // stored …
		repoBare(http.StatusForbidden)(w, r) // … then the event failed
	}
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	_, err := r.set("octo/hello", true)
	repoWantCode(t, err, appwire.CodeUncertain)
	if !watched["octo/hello"] {
		t.Fatal("the stub did not store")
	}
	time.Sleep(50 * time.Millisecond)
	if stub.puts() != 1 {
		t.Fatalf("the write was repeated: %v", stub.requests())
	}
	// A second EXPLICIT call is a second write; it is not blocked by the first.
	stub.put = nil
	if _, err := r.set("octo/hello", true); err != nil || stub.puts() != 2 {
		t.Fatalf("explicit retry: %v after %d puts", err, stub.puts())
	}
}

// ── no reply, and no connection ────────────────────────────────────────────

type switchTransport struct {
	next http.RoundTripper
	mu   sync.Mutex
	puts int
	// put, when set, answers a PUT instead of the network.
	put func() (*http.Response, error)
}

func (s *switchTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPut {
		s.mu.Lock()
		s.puts++
		put := s.put
		s.mu.Unlock()
		if put != nil {
			return put()
		}
	}
	return s.next.RoundTrip(r)
}

func (s *switchTransport) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.puts }

func withTransport(r *repoWire, put func() (*http.Response, error)) *switchTransport {
	st := &switchTransport{next: r.hub.client.Transport, put: put}
	c := *r.hub.client
	c.Transport = st
	r.hub.client = &c
	return st
}

func TestRepoSetConnectionRefusedProvesNothingWasSentAndAnyOtherFailureDoesNot(t *testing.T) {
	stub := newRepoStub(t, teamOperator())
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	tr := withTransport(r, func() (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	})
	_, err := r.set("octo/hello", true)
	w := repoWantCode(t, err, appwire.CodeUnreachable)
	if strings.Contains(w.Message, "127.0.0.1") {
		t.Fatalf("the address leaked: %q", w.Message)
	}
	for name, failure := range map[string]error{
		"reset after sending": &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")},
		"closed early":        io.ErrUnexpectedEOF,
		"timed out":           context.DeadlineExceeded,
	} {
		tr.mu.Lock()
		tr.put = func() (*http.Response, error) { return nil, failure }
		tr.mu.Unlock()
		before := tr.count()
		_, err := r.set("octo/hello", true)
		w := repoWantCode(t, err, appwire.CodeUncertain)
		if tr.count() != before+1 {
			t.Fatalf("%s: %d attempts, want exactly 1", name, tr.count()-before)
		}
		if strings.Contains(w.Message, "127.0.0.1") || strings.Contains(w.Message, "octo") {
			t.Fatalf("%s: leaked %q", name, w.Message)
		}
	}
}

func TestRepoSetTimeoutAfterTheRequestWasReceivedIsUncertain(t *testing.T) {
	stub := newRepoStub(t, teamOperator())
	release := make(chan struct{})
	stub.put = func(http.ResponseWriter, *http.Request) { <-release }
	t.Cleanup(func() { close(release) })
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	on := true
	_, err := r.backend.FactoryRepoSet(ctx, appwire.FactoryRepoSetParams{Factory: r.id, Repo: "octo/hello", On: &on})
	repoWantCode(t, err, appwire.CodeUncertain)
	if stub.puts() != 1 {
		t.Fatalf("puts = %d", stub.puts())
	}
}

// ── the caller changes during a wait ───────────────────────────────────────

func TestRepoSetCallerSwappedWhileWhoIsLoadingSendsNoWrite(t *testing.T) {
	for name, swap := range map[string]func(*repoWire){
		"another key": func(r *repoWire) { r.hub.hold(r.id, "tok_other_caller_credential", 0) },
		"signed out":  func(r *repoWire) { r.hub.mu.Lock(); delete(r.hub.sessions, r.id); r.hub.mu.Unlock() },
	} {
		t.Run(name, func(t *testing.T) {
			stub := newRepoStub(t, teamOperator())
			r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
			stub.mu.Lock()
			stub.onMe = func() { swap(r) }
			stub.mu.Unlock()
			_, err := r.set("octo/hello", true)
			if code := wireCodeOf(t, err); code != appwire.CodeConflict && code != appwire.CodeSignedOut {
				t.Fatalf("code %q: %v", code, err)
			}
			if stub.puts() != 0 {
				t.Fatalf("a write was sent for a caller who had been replaced: %v", stub.requests())
			}
		})
	}
}

func TestRepoSetCallerSwappedDuringTheWriteGetsNoScopedAnswer(t *testing.T) {
	for _, swapTo := range []string{"tok_other_caller_credential", ""} {
		for name, put := range map[string]func(http.ResponseWriter, *http.Request){
			"completed": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
			"unmarked":  repoBare(http.StatusForbidden),
		} {
			t.Run(name+"/"+swapTo, func(t *testing.T) {
				stub := newRepoStub(t, teamOperator())
				stub.put = put
				r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
				stub.mu.Lock()
				stub.onPut = func() {
					if swapTo == "" {
						r.hub.mu.Lock()
						delete(r.hub.sessions, r.id)
						r.hub.mu.Unlock()
						return
					}
					r.hub.hold(r.id, swapTo, 0)
				}
				stub.mu.Unlock()
				got, err := r.set("octo/hello", true)
				w := repoWantCode(t, err, appwire.CodeUncertain)
				if got != (appwire.FactoryRepoSetDone{}) {
					t.Fatalf("a receipt reached the new caller: %+v", got)
				}
				if strings.Contains(w.Message, "octo") || strings.Contains(w.Message, "hello") {
					t.Fatalf("the message names the repository: %q", w.Message)
				}
				if stub.puts() != 1 {
					t.Fatalf("puts = %d", stub.puts())
				}
			})
		}
	}
	// A refusal that PROVES nothing changed is told to the new caller only as a
	// changed sign-in, never as the old caller's refusal.
	stub := newRepoStub(t, teamOperator())
	stub.put = repoMarked(http.StatusForbidden)
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	stub.mu.Lock()
	stub.onPut = func() { r.hub.hold(r.id, "tok_other_caller_credential", 0) }
	stub.mu.Unlock()
	_, err := r.set("octo/hello", true)
	repoWantCode(t, err, appwire.CodeConflict)
}

// ── one write at a time ────────────────────────────────────────────────────

func TestRepoSetASecondPressWhileOneIsInFlightIsBusyAndSendsNothing(t *testing.T) {
	stub := newRepoStub(t, teamOperator())
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	stub.onPut = func() { once.Do(func() { close(reached) }); <-release }
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	first := make(chan error, 1)
	go func() { _, err := r.set("octo/hello", true); first <- err }()
	<-reached
	_, err := r.set("octo/hello", false)
	repoWantCode(t, err, appwire.CodeBusy)
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if stub.puts() != 1 {
		t.Fatalf("puts = %d", stub.puts())
	}
}

// ── the surface ────────────────────────────────────────────────────────────

func TestRepoSetIsAnnouncedAsItsOwnFeature(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	caps, err := b.FactoryCapabilities(context.Background())
	if err != nil || !caps.ReposManage {
		t.Fatalf("capabilities %+v %v", caps, err)
	}
	var _ appwire.FactoryRepoAdmin = b
}

// ── against the real factory handler ───────────────────────────────────────
//
// The same engine code, a real team factory (httptest, a fake GitHub, no
// model, no worker): the operator's switch lands in the store, a member's is
// refused before any request, and the phase marker the real handler sends is
// what separates a refusal from an uncertain outcome.

func TestRepoSetAgainstTheRealFactory(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	var done appwire.FactoryRepoSetDone
	alice.w.must("factory.repo_set", map[string]any{"factory": alice.conn.ID, "repo": "octo/hello", "on": true}, &done)
	if done != (appwire.FactoryRepoSetDone{Factory: alice.conn.ID, Repo: "octo/hello", On: true}) {
		t.Fatalf("receipt %+v", done)
	}
	if repos, _ := f.st.Repos(); len(repos) != 1 || repos[0] != "octo/hello" {
		t.Fatalf("stored %v", repos)
	}
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me|PUT /v1/repos/octo/hello" {
		t.Fatalf("requests %v", got)
	}
	// Off keeps only the watch list's meaning: nothing else is asked of the factory.
	alice.w.must("factory.repo_set", map[string]any{"factory": alice.conn.ID, "repo": "octo/hello", "on": false}, &done)
	if repos, _ := f.st.Repos(); len(repos) != 0 || done.On {
		t.Fatalf("after off: %v %+v", repos, done)
	}
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me|PUT /v1/repos/octo/hello" {
		t.Fatalf("requests %v", got)
	}

	// A member is refused here, in words, before a write is sent.
	bob := f.signIn(t, 2, "bob")
	bob.w.mustFail("factory.repo_set", map[string]any{"factory": bob.conn.ID, "repo": "octo/hello", "on": true}, appwire.CodeForbidden)
	for _, r := range f.since() {
		if strings.HasPrefix(r, "PUT ") {
			t.Fatalf("a member's write was sent: %s", r)
		}
	}
	if repos, _ := f.st.Repos(); len(repos) != 0 {
		t.Fatalf("a member changed the list: %v", repos)
	}
}

func TestRepoSetAgainstTheRealFactoryStoredThenFailedIsUncertainAndSentOnce(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	f.afterStore = errors.New("the event could not be recorded")
	alice.w.mustFail("factory.repo_set", map[string]any{"factory": alice.conn.ID, "repo": "octo/hello", "on": true}, appwire.CodeUncertain)
	if repos, _ := f.st.Repos(); len(repos) != 1 {
		t.Fatalf("the real factory did not store before failing: %v", repos)
	}
	puts := 0
	for _, r := range f.since() {
		if strings.HasPrefix(r, "PUT ") {
			puts++
		}
	}
	if puts != 1 {
		t.Fatalf("puts = %d", puts)
	}
}

func TestRepoSetAgainstTheRealFactoryMarkedRefusalIsDefinite(t *testing.T) {
	f := startAdminFactoryWith(t, true, true)
	alice := f.signIn(t, 1, "alice")
	msg := alice.w.mustFail("factory.repo_set", map[string]any{"factory": alice.conn.ID, "repo": "octo/hello", "on": true}, appwire.CodeConflict)
	if strings.Contains(msg, "octo") {
		t.Fatalf("message %q", msg)
	}
	if repos, _ := f.st.Repos(); len(repos) != 0 {
		t.Fatalf("stored %v", repos)
	}
}

// A TEAM FACTORY ON LOOPBACK ANSWERS A REQUEST WITH NO BEARER AS ITS HOST. The
// switch must never be the way back to that authority.
func TestRepoSetAgainstTheRealLoopbackFactoryNeedsTheHeldCredential(t *testing.T) {
	f := startAdminFactoryAt(t, false)
	w := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": f.URL}, &conn)
	f.since()
	w.mustFail("factory.repo_set", map[string]any{"factory": conn.ID, "repo": "octo/hello", "on": true}, appwire.CodeSignedOut)
	if got := f.since(); len(got) != 0 {
		t.Fatalf("a request left with no credential: %v", got)
	}
	if repos, _ := f.st.Repos(); len(repos) != 0 {
		t.Fatalf("the list changed: %v", repos)
	}

	// Signed in then signed out: the same.
	alice := f.signIn(t, 1, "alice")
	alice.w.must("factory.sign_out", map[string]any{"factory": alice.conn.ID}, nil)
	f.since()
	alice.w.mustFail("factory.repo_set", map[string]any{"factory": alice.conn.ID, "repo": "octo/hello", "on": true}, appwire.CodeSignedOut)
	if got := f.since(); len(got) != 0 {
		t.Fatalf("a signed-out write sent %v", got)
	}
}
