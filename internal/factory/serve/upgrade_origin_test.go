package serve

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// THE VALIDATION CONTRACT (a browser page from another site must not be able to
// open a person's chat connection with the credential their browser holds):
//
//  1. An upgrade whose Origin is another site is refused with 403 and one plain
//     sentence, before any credential is read and before the chats' host is asked.
//  2. An upgrade whose Origin is this server's own (the host the request was sent
//     to, or the host of the server's public address) goes through.
//  3. An upgrade with no Origin goes through: the command line, the desktop
//     app's engine and every other program that is not a browser send none.
//  4. A request that is not an upgrade is not touched by this check.

func dialWire(t *testing.T, srv *httptest.Server, id string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return websocket.Dial(ctx, wireURL(srv, id), &websocket.DialOptions{HTTPHeader: header})
}

// A hostile page's connection is refused with the sentence, whether or not it
// carries a credential, and the chats' host hears nothing of it (contract 1).
func TestACrossSiteUpgradeIsRefusedBeforeAnythingElseIsAsked(t *testing.T) {
	rg := newHostedRig(t)
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	id := "hs_" + strings.Repeat("f", 24)
	rg.host.adopt(id, "santosh")
	before := rg.host.count()
	for _, tc := range []struct {
		name   string
		header http.Header
	}{
		{"with the owner's credential", http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}, "Origin": {"https://evil.example"}}},
		{"with the owner's sign-in cookie", http.Header{"Cookie": {accessCookieName + "=whatever"}, "Origin": {"https://evil.example"}}},
		{"with no credential", http.Header{"Origin": {"https://evil.example"}}},
		{"from a sibling name of the same site", http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}, "Origin": {"https://app.localhost"}}},
		{"from the same host on another port", http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}, "Origin": {"http://127.0.0.1:1"}}},
		{"with an opaque origin", http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}, "Origin": {"null"}}},
	} {
		_, resp, err := dialWire(t, srv, id, tc.header)
		if err == nil || resp == nil {
			t.Fatalf("%s: the upgrade was accepted (err %v)", tc.name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || errorOf(t, body) != crossSiteUpgradeWords {
			t.Errorf("%s: %d %s", tc.name, resp.StatusCode, body)
		}
	}
	if after := rg.host.count(); after != before {
		t.Errorf("a refused upgrade reached the chats' host: %d requests", after-before)
	}
}

// A guest's connection (the frame-aware path) and every other route share the
// one check, because it runs before routing (contract 1).
func TestACrossSiteUpgradeIsRefusedForAGuestToo(t *testing.T) {
	rg := newHostedRig(t)
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "edit")
	_, resp, err := dialWire(t, srv, chat, http.Header{"Authorization": {"Bearer " + rg.tokens["abir"]}, "Origin": {"https://evil.example"}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a guest's cross-site upgrade: %v %v", resp, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/about", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden || errorOf(t, body) != crossSiteUpgradeWords {
		t.Errorf("an upgrade of another route from another site: %d %s", res.StatusCode, body)
	}
}

// An upgrade from this server's own origin works and still never carries the
// Origin to the chats' host, and so does one with no Origin at all (contracts 2 and 3).
func TestAnUpgradeFromTheServersOwnOriginOrWithNoneGoesThrough(t *testing.T) {
	rg := newHostedRig(t)
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	id := "hs_" + strings.Repeat("9", 24)
	rg.host.adopt(id, "santosh")
	for name, origin := range map[string]string{"its own origin": srv.URL, "no origin": ""} {
		header := http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}}
		if origin != "" {
			header.Set("Origin", origin)
		}
		c, _, err := dialWire(t, srv, id, header)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err := c.Write(ctx, websocket.MessageBinary, []byte("ping")); err != nil {
			t.Fatal(err)
		}
		if _, got, err := c.Read(ctx); err != nil || string(got) != "ping" {
			t.Fatalf("%s: %q %v", name, got, err)
		}
		c.Close(websocket.StatusNormalClosure, "")
		cancel()
	}
	for _, seen := range rg.host.requests() {
		if values := seen.header.Values("Origin"); len(values) != 0 {
			t.Errorf("%s %s carried an Origin to the chats' host: %v", seen.method, seen.path, values)
		}
	}
}

// Only an upgrade is asked: an ordinary request that carries another site's
// Origin is answered as it always was (contract 4), and the same credential
// on the same route from a page of ours is not changed either.
func TestARequestThatIsNotAnUpgradeIsNotAskedAboutItsOrigin(t *testing.T) {
	rg := newHostedRig(t)
	w := rg.send("santosh", http.MethodGet, "/v1/hosted/sessions", nil, map[string]string{"Origin": "https://evil.example"})
	if w.Code != http.StatusOK {
		t.Fatalf("a plain request from another site's page = %d %s", w.Code, w.Body)
	}
}

// The decision itself, over the shapes an Origin can take (contracts 1 to 3),
// including a proxy that does not pass the Host header through.
func TestWhichOriginsMayUpgrade(t *testing.T) {
	cases := []struct {
		name, host, address string
		origins             []string
		upgrade             bool
		refuse              bool
	}{
		{"no origin", "org.example.com", "https://org.example.com", nil, true, false},
		{"the same host", "org.example.com", "https://org.example.com", []string{"https://org.example.com"}, true, false},
		{"the same host in capitals", "ORG.example.com", "https://org.example.com", []string{"https://org.EXAMPLE.com"}, true, false},
		{"the same host with its default port spelled", "org.example.com:443", "https://org.example.com", []string{"https://org.example.com"}, true, false},
		{"plain http on the same host", "127.0.0.1:8787", "http://127.0.0.1:8787", []string{"http://127.0.0.1:8787"}, true, false},
		{"a proxy that rewrites Host to its backend", "codeaf:8787", "https://org.example.com", []string{"https://org.example.com"}, true, false},
		{"another site", "org.example.com", "https://org.example.com", []string{"https://evil.example"}, true, true},
		{"a sibling name of the same site", "org.example.com", "https://org.example.com", []string{"https://blog.example.com"}, true, true},
		{"a name that merely ends with ours", "org.example.com", "https://org.example.com", []string{"https://evilorg.example.com"}, true, true},
		{"a name that merely starts with ours", "org.example.com", "https://org.example.com", []string{"https://org.example.com.evil.example"}, true, true},
		{"our host on another port", "org.example.com", "https://org.example.com", []string{"https://org.example.com:8443"}, true, true},
		{"an opaque origin", "org.example.com", "https://org.example.com", []string{"null"}, true, true},
		{"an origin with a path", "org.example.com", "https://org.example.com", []string{"https://org.example.com/x"}, true, true},
		{"an origin with credentials", "org.example.com", "https://org.example.com", []string{"https://a@org.example.com"}, true, true},
		{"a file page", "org.example.com", "https://org.example.com", []string{"file://"}, true, true},
		{"an empty origin", "org.example.com", "https://org.example.com", []string{""}, true, true},
		{"two origins", "org.example.com", "https://org.example.com", []string{"https://org.example.com", "https://org.example.com"}, true, true},
		{"another site on a request that is not an upgrade", "org.example.com", "https://org.example.com", []string{"https://evil.example"}, false, false},
	}
	for _, tc := range cases {
		h := &Handler{config: Config{Address: tc.address}}
		r := httptest.NewRequest(http.MethodGet, "/v1/hosted/sessions/hs_x/wire", nil)
		r.Host = tc.host
		if tc.upgrade {
			r.Header.Set("Connection", "Upgrade")
			r.Header.Set("Upgrade", "websocket")
		}
		for _, origin := range tc.origins {
			r.Header.Add("Origin", origin)
		}
		if got := h.crossSiteUpgrade(r); got != tc.refuse {
			t.Errorf("%s: refused = %v, want %v", tc.name, got, tc.refuse)
		}
	}
}
