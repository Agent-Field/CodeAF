package providerhttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOriginIncludesSchemeAndEffectivePort(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"https://api.test/a", "https://API.test:443/b", true},
		{"http://api.test", "http://api.test:80", true},
		{"https://api.test", "http://api.test:443", false},
		{"https://api.test:443", "https://api.test:444", false},
		{"https://api.test", "https://child.api.test", false},
	} {
		a, _ := url.Parse(tc.a)
		b, _ := url.Parse(tc.b)
		if got := SameOrigin(a, b); got != tc.want {
			t.Errorf("origins %s / %s = %v", tc.a, tc.b, got)
		}
	}
}
func TestProviderRedirectStopsBeforeForeignTargetAndRedactsSecrets(t *testing.T) {
	for _, targetHost := range []string{"api.test", "child.api.test", "unrelated.test"} {
		t.Run(targetHost, func(t *testing.T) {
			hits := 0
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; fmt.Fprint(w, "wrong") }))
			defer target.Close()
			tu, _ := url.Parse(target.URL)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://mock-user:mock-password@"+targetHost+":"+tu.Port()+"/mock-secret-path?key=mock-query-secret#mock-fragment")
				w.WriteHeader(307)
			}))
			defer origin.Close()
			ou, _ := url.Parse(origin.URL)
			transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				_, port, _ := net.SplitHostPort(address)
				return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
			}}
			defer transport.CloseIdleConnections()
			caller := &http.Client{Transport: transport, Timeout: time.Second}
			req, _ := http.NewRequest("POST", "http://api.test:"+ou.Port()+"/ask", strings.NewReader("test"))
			req.Header.Set("Authorization", "Bearer mock-provider-key")
			_, err := Do(Clone(caller), req)
			if err == nil || !strings.Contains(err.Error(), targetHost+":"+tu.Port()) {
				t.Fatalf("missing refusal host: %v", err)
			}
			for _, secret := range []string{"mock-user", "mock-password", "mock-secret-path", "mock-query-secret", "mock-fragment", "mock-provider-key"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error disclosed %s", secret)
				}
			}
			if hits != 0 {
				t.Fatalf("foreign target received %d requests", hits)
			}
			if caller.CheckRedirect != nil || caller.Transport != transport || caller.Timeout != time.Second {
				t.Fatal("caller client mutated")
			}
		})
	}
}
func TestProviderRedirectSameOriginPreservesBearerAndMethod(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			w.Header().Set("Location", "/finish")
			w.WriteHeader(307)
			return
		}
		hits++
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer mock-key" {
			t.Errorf("redirect request %s auth %q", r.Method, r.Header.Get("Authorization"))
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/start", strings.NewReader("body"))
	req.Header.Set("Authorization", "Bearer mock-key")
	resp, err := Do(Clone(nil), req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits != 1 {
		t.Fatal(hits)
	}
}
func TestProviderRedirectLoopIsBounded(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Location", "/loop")
		w.WriteHeader(307)
	}))
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/loop", nil)
	_, err := Do(Clone(nil), req)
	if err == nil || !strings.Contains(err.Error(), "redirect limit") || hits != 10 {
		t.Fatalf("hits=%d err=%v", hits, err)
	}
}
func TestCallerRedirectHookCannotEscapeOrigin(t *testing.T) {
	source, _ := url.Parse("https://api.test/one")
	target, _ := url.Parse("https://api.test/two")
	client := Clone(&http.Client{CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		r.URL, _ = url.Parse("https://foreign.test/private?secret=mock-secret")
		return nil
	}})
	if err := client.CheckRedirect(&http.Request{URL: target}, []*http.Request{{URL: source}}); err == nil {
		t.Fatal("caller redirected outside origin")
	}
}

type failTransport struct{ err error }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }
func TestSafeRequestErrorsPreserveCancellationAndNetworkCategories(t *testing.T) {
	for _, tc := range []struct {
		cause error
		want  string
	}{
		{context.Canceled, "request canceled"}, {context.DeadlineExceeded, "context deadline exceeded"},
		{&net.DNSError{Err: "failed at https://mock-user:mock-password@api.test/?key=mock-query-secret", Name: "secret.test"}, "DNS lookup failed"},
	} {
		req, _ := http.NewRequest("GET", "http://mock-user:mock-password@api.test/?key=mock-query-secret", nil)
		_, err := Do(&http.Client{Transport: failTransport{tc.cause}}, req)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.cause, err)
		}
		for _, secret := range []string{"mock-user", "mock-password", "mock-query-secret"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe error %v", err)
			}
		}
		if !errors.Is(err, tc.cause) {
			t.Fatal("original error identity lost")
		}
	}
}

func TestCallerCanRefuseSameOriginWithoutMutation(t *testing.T) {
	called := 0
	caller := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { called++; return http.ErrUseLastResponse }}
	u, _ := url.Parse("https://api.test/first")
	v, _ := url.Parse("https://api.test/second")
	if err := Clone(caller).CheckRedirect(&http.Request{URL: v}, []*http.Request{{URL: u}}); err != http.ErrUseLastResponse || called != 1 {
		t.Fatalf("stricter caller rule lost: %v calls=%d", err, called)
	}
}
