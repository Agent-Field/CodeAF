package provider

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Regression for #1896: every provider client that carries the configured key
// must refuse a redirect away from the exact origin it was configured for,
// instead of following Go's default policy, which forwards Authorization to a
// subdomain of the same hostname or to another port of it. The completion must
// fail naming the redirect target, and the target must never see the key.
//
// Deterministic: no DNS, no model. Every *.test name dials loopback through
// the transport the client is handed, exactly the way NewClient's callers hand
// one in; the redirect policy under test is the product's.
func TestIssueReproRedirectForwardsKey(t *testing.T) {
	for _, target := range []string{"evil.api.repro.test", "api.repro.test", "other-vendor.test"} {
		t.Run(target, func(t *testing.T) {
			seen := make(chan string, 4)
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Host + " Authorization=" + fmt.Sprintf("%q", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"gen-1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}))
			defer second.Close()
			_, secondPort, _ := net.SplitHostPort(strings.TrimPrefix(second.URL, "http://"))
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://"+target+":"+secondPort+r.URL.Path, http.StatusTemporaryRedirect)
			}))
			defer first.Close()
			_, firstPort, _ := net.SplitHostPort(strings.TrimPrefix(first.URL, "http://"))
			transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				_, port, _ := net.SplitHostPort(address)
				return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
			}}
			client, err := NewClient(Config{APIKey: "sk-or-REPRO-KEY", BaseURL: "http://api.repro.test:" + firstPort + "/api/v1",
				Model: "repro/model", HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CompleteWithMessages(context.Background(), userMessages("hello"))
			// The redirect left the configured origin, so the completion must
			// fail and the error must say where it was going.
			if err == nil {
				t.Fatalf("completion followed the redirect to %s; want a refusal", target)
			}
			if !strings.Contains(err.Error(), target) {
				t.Fatalf("refusal %v names no redirect target %q", err, target)
			}
			// And the target must never have been asked for anything, keyed or not.
			select {
			case got := <-seen:
				t.Fatalf("redirect target %s received %s", target, got)
			default:
			}
		})
	}
	// Same-origin redirects keep working: a 307 to the same host and port at a
	// different path is followed end to end.
	t.Run("same-origin-307", func(t *testing.T) {
		var completions atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/landed" {
				completions.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"gen-1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
				return
			}
			http.Redirect(w, r, "http://"+r.Host+"/landed", http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
		transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, _ := net.SplitHostPort(address)
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		}}
		client, err := NewClient(Config{APIKey: "sk-or-REPRO-KEY", BaseURL: "http://api.repro.test:" + port + "/api/v1",
			Model: "repro/model", HTTPClient: &http.Client{Transport: transport}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.CompleteWithMessages(context.Background(), userMessages("hello"))
		if err != nil {
			t.Fatalf("same-origin 307 refused: %v", err)
		}
		if completions.Load() != 1 {
			t.Fatalf("same-origin redirect not followed: /landed served %d times, want 1", completions.Load())
		}
	})
}
