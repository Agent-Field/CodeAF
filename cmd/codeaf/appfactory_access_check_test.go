package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

const checkSecret = "invite-secret-check-post"

// checkFactory is a factory whose /v1/about and /v1/factory-access are scripted,
// and which keeps every request it was sent: method, URI and body.
type checkFactory struct {
	srv         *httptest.Server
	mu          sync.Mutex
	about       string
	aboutStatus int
	accessPOST  int // status of POST /v1/factory-access; 0 answers normally
	log         []string
	bodies      []string
}

func newCheckFactory(t *testing.T, about string) *checkFactory {
	t.Helper()
	f := &checkFactory{about: about, aboutStatus: 200}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.log = append(f.log, r.Method+" "+r.URL.RequestURI())
		f.bodies = append(f.bodies, string(raw))
		about, aboutStatus, post := f.about, f.aboutStatus, f.accessPOST
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/about":
			w.WriteHeader(aboutStatus)
			io.WriteString(w, about)
		case r.URL.Path == "/v1/factory-access" && r.Method == http.MethodPost:
			if post != 0 {
				http.Error(w, `{"error":"no"}`, post)
				return
			}
			var body struct{ Verb string }
			_ = json.Unmarshal(raw, &body)
			state := "ready"
			if body.Verb == "sign_in" {
				http.SetCookie(w, &http.Cookie{Name: appFactoryJoinCookie, Value: "join-cookie", Path: "/v1/factory-access"})
				state = "waiting"
			} else if _, err := r.Cookie(appFactoryJoinCookie); err == nil && body.Verb == "check" {
				state = "joined"
			}
			io.WriteString(w, `{"state":"`+state+`","factory":{"name":"Stub","repos":null}}`)
		case r.URL.Path == "/v1/factory-access":
			io.WriteString(w, `{"state":"ready","factory":{"name":"Stub","repos":null}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *checkFactory) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *checkFactory) count(prefix string) int {
	n := 0
	for _, line := range f.requests() {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

// neverInURL fails when any request's method line or URI carried the link; only
// the JSON body of a POST may.
func (f *checkFactory) neverInURL(t *testing.T) {
	t.Helper()
	for _, line := range f.requests() {
		if strings.Contains(line, checkSecret) || strings.Contains(line, "link=") || strings.Contains(line, "codeaf%3A") {
			t.Fatalf("the link rode in a URL: %s", line)
		}
	}
}

func checkBackend(t *testing.T) (*appBackend, *appFactoryHub, *factoryClock) {
	t.Helper()
	hub := testFactoryHub(t)
	clock := &factoryClock{at: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	hub.now = clock.now
	b := newFakeBackend(newAppRecorder(), nil)
	b.factoryOnce.Do(func() { b.factories = hub })
	t.Cleanup(func() { b.Close() })
	return b, hub, clock
}

func (f *checkFactory) link() string {
	return "codeaf://factory/join?url=" + f.srv.URL + "#token=" + checkSecret
}

const advertises = `{"factory":{"name":"Stub"},"name":"Stub","contract":1,"access_check_post":true}`

func TestAccessCheckUsesPostWhenAdvertisedAndNeverStartsSignIn(t *testing.T) {
	f := newCheckFactory(t, advertises)
	b, _, _ := checkBackend(t)
	ctx := context.Background()

	got, err := b.FactoryAccessCheck(ctx, "w1", f.link())
	if err != nil || got.State != "ready" {
		t.Fatalf("check = %+v %v", got, err)
	}
	f.neverInURL(t)
	if f.count("GET /v1/factory-access") != 0 {
		t.Fatalf("a GET check was sent: %v", f.requests())
	}
	if f.count("POST /v1/factory-access") != 1 {
		t.Fatalf("requests = %v", f.requests())
	}
	// THE FIRST CHECK STARTS NOTHING: its body names check, not sign_in.
	for _, body := range f.bodies {
		if strings.Contains(body, "sign_in") || strings.Contains(body, "cancel") {
			t.Fatalf("a check started or ended a flow: %s", body)
		}
	}
	if !strings.Contains(strings.Join(f.bodies, "\n"), `"verb":"check"`) {
		t.Fatalf("no check verb sent: %q", f.bodies)
	}
}

func TestAccessPollsGoByJSONAndCarryOnlyTheirOwnCookie(t *testing.T) {
	f := newCheckFactory(t, advertises)
	b, _, _ := checkBackend(t)
	ctx := context.Background()

	if st, err := b.FactoryAccessSignIn(ctx, "w1", f.link()); err != nil || st.State != "waiting" {
		t.Fatalf("sign_in = %+v %v", st, err)
	}
	// Another window has no cookie, so its check is not joined.
	if st, err := b.FactoryAccessCheck(ctx, "w2", f.link()); err != nil || st.State != "ready" {
		t.Fatalf("other caller = %+v %v", st, err)
	}
	if st, err := b.FactoryAccessCheck(ctx, "w1", f.link()); err != nil || st.State != "joined" {
		t.Fatalf("owner poll = %+v %v", st, err)
	}
	f.neverInURL(t)
	if f.count("GET /v1/factory-access") != 0 {
		t.Fatalf("a poll fell back to GET: %v", f.requests())
	}
	if f.count("POST /v1/factory-access") != 3 {
		t.Fatalf("requests = %v", f.requests())
	}
}

func TestAccessCapabilityIsCachedForAFlowLifeAndBounded(t *testing.T) {
	f := newCheckFactory(t, advertises)
	b, hub, clock := checkBackend(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := b.FactoryAccessCheck(ctx, "w1", f.link()); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count("GET /v1/about"); n != 1 {
		t.Fatalf("about asked %d times for three checks", n)
	}
	clock.advance(appFactoryFlowLife + time.Second)
	if _, err := b.FactoryAccessCheck(ctx, "w1", f.link()); err != nil {
		t.Fatal(err)
	}
	if n := f.count("GET /v1/about"); n != 2 {
		t.Fatalf("an expired capability was reused (%d about calls)", n)
	}
	hub.mu.Lock()
	// Fill the table with other origins; a new origin is then answered but not kept.
	hub.checkCaps = map[string]appFactoryCheckCap{}
	for i := 0; i < appwire.MaxFactoryFlows; i++ {
		hub.checkCaps["http://other-"+strconv.Itoa(i)+".test"] = appFactoryCheckCap{expires: clock.now().Add(time.Hour)}
	}
	hub.mu.Unlock()
	if _, err := b.FactoryAccessCheck(ctx, "w1", f.link()); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	n := len(hub.checkCaps)
	hub.mu.Unlock()
	if n > appwire.MaxFactoryFlows {
		t.Fatalf("%d capabilities cached, cap %d", n, appwire.MaxFactoryFlows)
	}
}

// LEGACY: an older factory with a valid about and no flag is still asked by GET.
// That puts the link in the URL, the same risk those factories always carried;
// it is explicit here so it cannot become the silent default.
func TestAccessLegacyFactoryWithValidAboutKeepsDocumentedGet(t *testing.T) {
	for name, about := range map[string]string{
		"flag absent": `{"name":"Stub","contract":1}`,
		"flag false":  `{"name":"Stub","contract":1,"access_check_post":false}`,
		"flag null":   `{"name":"Stub","contract":1,"access_check_post":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newCheckFactory(t, about)
			b, _, _ := checkBackend(t)
			got, err := b.FactoryAccessCheck(context.Background(), "w1", f.link())
			if err != nil || got.State != "ready" {
				t.Fatalf("legacy check = %+v %v", got, err)
			}
			if f.count("GET /v1/factory-access?link=") != 1 || f.count("POST /v1/factory-access") != 0 {
				t.Fatalf("requests = %v", f.requests())
			}
		})
	}
}

// A PROBE THAT IS NOT A VALID ABOUT NEVER LEADS TO THE SECRET BEING SENT.
func TestAccessUnknownOrMalformedCapabilityForwardsNoSecret(t *testing.T) {
	for name, tc := range map[string]struct {
		body   string
		status int
	}{
		"server error":      {`{"error":"boom"}`, 500},
		"refused":           {`{"error":"no"}`, 403},
		"not json":          {`<html>proxy</html>`, 200},
		"no contract":       {`{"name":"Stub","access_check_post":true}`, 200},
		"flag is a string":  {`{"name":"Stub","contract":1,"access_check_post":"true"}`, 200},
		"flag is a number":  {`{"name":"Stub","contract":1,"access_check_post":1}`, 200},
		"flag is an object": {`{"name":"Stub","contract":1,"access_check_post":{}}`, 200},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCheckFactory(t, tc.body)
			f.aboutStatus = tc.status
			b, _, _ := checkBackend(t)
			_, err := b.FactoryAccessCheck(context.Background(), "w1", f.link())
			if err == nil {
				t.Fatal("an unreadable capability was accepted")
			}
			if strings.Contains(err.Error(), checkSecret) {
				t.Fatalf("error carries the secret: %v", err)
			}
			if got := f.requests(); len(got) != 1 || got[0] != "GET /v1/about" {
				t.Fatalf("requests after a bad probe = %v", got)
			}
			if strings.Contains(strings.Join(f.bodies, ""), checkSecret) {
				t.Fatal("the secret was forwarded")
			}
		})
	}
}

func TestAccessUnreachableProbeIsUnreachableNotAFallback(t *testing.T) {
	f := newCheckFactory(t, advertises)
	link := f.link()
	f.srv.Close()
	b, _, _ := checkBackend(t)
	got, err := b.FactoryAccessCheck(context.Background(), "w1", link)
	if err != nil || got.State != "unreachable" {
		t.Fatalf("down factory = %+v %v", got, err)
	}
}

// AN ADVERTISED POST THAT FAILS IS A FAILURE, NEVER A GET.
func TestAccessAdvertisedPostFailureDoesNotDowngradeToGet(t *testing.T) {
	for _, status := range []int{400, 404, 405, 422, 500, 502} {
		f := newCheckFactory(t, advertises)
		f.accessPOST = status
		b, _, _ := checkBackend(t)
		_, err := b.FactoryAccessCheck(context.Background(), "w1", f.link())
		if err == nil {
			t.Fatalf("status %d: a failed check passed", status)
		}
		if strings.Contains(err.Error(), checkSecret) {
			t.Fatalf("status %d: error carries the secret: %v", status, err)
		}
		if n := f.count("GET /v1/factory-access"); n != 0 {
			t.Fatalf("status %d: downgraded to GET: %v", status, f.requests())
		}
		f.neverInURL(t)
	}
}
