package credits

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBalanceUsesAccountAndKeyCapWithoutSendingACompletion(t *testing.T) {
	for _, tc := range []struct {
		name, credits, key string
		known, low         bool
	}{
		{"account only", `{"data":{"total_credits":1,"total_usage":0.8}}`, `{"data":{"limit_remaining":null}}`, true, true},
		{"cap only", `forbidden`, `{"data":{"limit_remaining":0.1}}`, true, true},
		{"smaller cap", `{"data":{"total_credits":2,"total_usage":0}}`, `{"data":{"limit_remaining":0.4}}`, true, true},
		{"smaller account", `{"data":{"total_credits":0.5,"total_usage":0}}`, `{"data":{"limit_remaining":9}}`, true, true},
		{"healthy", `{"data":{"total_credits":2,"total_usage":0}}`, `{"data":{"limit_remaining":null}}`, true, false},
		{"unknown", `forbidden`, `{"data":{"limit_remaining":null}}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer private-test-key" || r.Header.Get("Accept") != "application/json" || r.ContentLength > 0 {
					t.Errorf("balance request carried the wrong method, headers or body: %+v", r)
				}
				switch r.URL.Path {
				case "/api/v1/key":
					fmt.Fprint(w, tc.key)
				case "/api/v1/credits":
					if tc.credits == "forbidden" {
						w.WriteHeader(http.StatusForbidden)
					} else {
						fmt.Fprint(w, tc.credits)
					}
				default:
					t.Errorf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()
			got, err := Read(context.Background(), server.Client(), server.URL+"/api/v1", "private-test-key")
			if err != nil || got.Known != tc.known || got.Low != tc.low || calls != 2 {
				t.Fatalf("Read = %+v, %v; calls %d, want known=%v low=%v and two GETs", got, err, calls, tc.known, tc.low)
			}
		})
	}
}

func TestFailedBalanceReadDoesNotBecomeUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/key") {
			fmt.Fprint(w, `{"data":{"limit_remaining":null}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	if _, err := Read(context.Background(), server.Client(), server.URL+"/api/v1", "key"); err == nil {
		t.Fatal("a failed balance read became an unknown account")
	}
}

func TestCreditsFallbackAndFailedReads(t *testing.T) {
	t.Run("old key route only after a 404", func(t *testing.T) {
		var paths []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			switch r.URL.Path {
			case "/api/v1/key":
				w.WriteHeader(http.StatusNotFound)
			case "/api/v1/auth/key":
				fmt.Fprint(w, `{"data":{"limit_remaining":0.2}}`)
			case "/api/v1/credits":
				w.WriteHeader(http.StatusForbidden)
			}
		}))
		defer server.Close()
		reading, err := Read(context.Background(), server.Client(), server.URL+"/api/v1", "key")
		if err != nil || !reading.Known || !reading.Low || strings.Join(paths, ",") != "/api/v1/key,/api/v1/auth/key,/api/v1/credits" {
			t.Fatalf("fallback = %+v, %v; paths %v", reading, err, paths)
		}
	})
	for _, failure := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", http.StatusUnauthorized, ""},
		{"server failure", http.StatusInternalServerError, ""},
		{"malformed JSON", http.StatusOK, `not JSON`},
	} {
		t.Run(failure.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/key") {
					fmt.Fprint(w, `{"data":{"limit_remaining":null}}`)
					return
				}
				w.WriteHeader(failure.status)
				fmt.Fprint(w, failure.body)
			}))
			defer server.Close()
			if _, err := Read(context.Background(), server.Client(), server.URL+"/api/v1", "key"); err == nil {
				t.Fatal("failed read became a balance")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx, nil, "http://127.0.0.1:1/api/v1", "key"); err == nil {
		t.Fatal("canceled read did not fail")
	}
}

func TestTriggerCoalescesAndDebouncesRefusalsWithAFakeClock(t *testing.T) {
	now := time.Unix(100, 0)
	trigger := NewTrigger(func() time.Time { return now })
	if !trigger.Begin(Launch) || trigger.Begin(KeyChanged) {
		t.Fatal("two balance reads began together")
	}
	trigger.End()
	if trigger.Begin(Refusal) {
		t.Fatal("a 402 immediately repeated the balance read")
	}
	now = now.Add(30 * time.Second)
	if !trigger.Begin(Refusal) {
		t.Fatal("a 402 after quiet period was ignored")
	}
	trigger.End()
	if !trigger.Begin(KeyChanged) {
		t.Fatal("a changed key was debounced")
	}
}

// AN EXPIRED KEY IS A READING, NOT A FAILURE. The service's 401 that says the
// key has expired is the one fact the account can still give, so it comes back
// as a known, expired reading — never low, because there is no balance under
// it — on either of the two routes. A 401 that says nothing about expiry stays
// a failed read (the table above holds that).
func TestAnExpiredKeyIsAKnownReadingOnEitherRoute(t *testing.T) {
	const refusal = `{"error":{"message":"API key expired.","code":401}}`
	for _, where := range []string{"/api/v1/key", "/api/v1/credits"} {
		t.Run(where, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == where {
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(w, refusal)
					return
				}
				fmt.Fprint(w, `{"data":{"limit_remaining":50,"total_credits":0,"total_usage":0}}`)
			}))
			defer server.Close()
			reading, err := Read(context.Background(), server.Client(), server.URL+"/api/v1", "key")
			if err != nil || !reading.Known || !reading.Expired || reading.Low {
				t.Fatalf("an expired key read as %+v, %v; want known and expired, not low", reading, err)
			}
		})
	}
}
