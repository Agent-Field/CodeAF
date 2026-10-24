package relayserve

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// C1 and C10 provide a self-hosted page without a directory lookup, external
// requests or sending the fragment key to any service.
func TestC10SelfHostedPairPageStaysOnChosenService(t *testing.T) {
	svc := New(Config{})
	defer svc.Close()
	srv := httptest.NewServer(svc.Handler)
	defer srv.Close()
	var first string
	for _, code := range []string{"k7m2q9xd", "unknown-code"} {
		res, err := http.Get(srv.URL + "/p/" + code)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("pair page: %s %v", res.Status, err)
		}
		body := string(raw)
		if first != "" && body != first {
			t.Fatal("page content depends on a mailbox lookup")
		}
		first = body
		if !strings.Contains(body, "<!doctype html>") || !strings.Contains(body, " --via ") || !strings.Contains(body, "&via=") || !strings.Contains(body, "location.hash") {
			t.Fatalf("page loses its selected service or fragment: %s", body)
		}
		if regexp.MustCompile(`(?i)fetch\(|XMLHttpRequest|sendBeacon|WebSocket|EventSource|https?://|\s(src|href)=`).MatchString(body) {
			t.Fatal("page makes a request or names an outside address")
		}
		for name, want := range map[string]string{"Referrer-Policy": "no-referrer", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"} {
			if res.Header.Get(name) != want {
				t.Errorf("%s=%q, want %q", name, res.Header.Get(name), want)
			}
		}
		csp := res.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe") {
			t.Fatalf("page policy: %s", csp)
		}
		for tag, directive := range map[string]string{"script": "script-src", "style": "style-src"} {
			inline := regexp.MustCompile(`(?s)<` + tag + `>(.*?)</` + tag + `>`).FindStringSubmatch(body)
			if len(inline) != 2 {
				t.Fatalf("page has no %s", tag)
			}
			sum := sha256.Sum256([]byte(inline[1]))
			if !strings.Contains(csp, directive+" 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
				t.Errorf("%s hash does not match the policy", tag)
			}
		}
	}
}

// C1 limits the browser door to GET of one page, as the hosted Worker does.
func TestC1SelfHostedPairPageRejectsOtherRoutes(t *testing.T) {
	svc := New(Config{})
	defer svc.Close()
	for _, target := range []struct{ method, path string }{{"POST", "/p/a"}, {"GET", "/p/"}, {"GET", "/p/a/b"}} {
		out := httptest.NewRecorder()
		svc.Handler.ServeHTTP(out, httptest.NewRequest(target.method, target.path, nil))
		if out.Code != http.StatusNotFound {
			t.Errorf("%s %s=%d", target.method, target.path, out.Code)
		}
	}
}
