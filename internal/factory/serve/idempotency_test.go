package serve_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func keyedRequest(h http.Handler, path, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body)).WithContext(context.WithValue(context.Background(), verbContextKey{}, "request"))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestEveryPostSeamVerbReplaysItsFirstResponseWithoutAnotherCall(t *testing.T) {
	for _, tc := range verbCases {
		if tc.method != "POST" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			s, calls := spyVerbs(t, nil)
			s.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "octocat"}, nil }
			h := serve.New(s, recordConfig(t))
			first := keyedRequest(h, tc.path, tc.body, "double-click")
			second := keyedRequest(h, tc.path, "malformed replay body", "double-click")
			if first.Code != tc.status || second.Code != first.Code || second.Body.String() != first.Body.String() || !reflect.DeepEqual(*calls, tc.want) {
				t.Fatalf("responses=%d %s / %d %s; calls=%v", first.Code, first.Body, second.Code, second.Body, *calls)
			}
		})
	}
}

func TestIdempotencySurvivesRestartSerializesServersAndExpiresAt24Hours(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	var calls atomic.Int64
	s := factory.Seam{New: func(string, string) (int, error) { return int(calls.Add(1)), nil }}
	cfg := serve.Config{Store: st, LeaseOptions: lease.Options{Now: func() time.Time { return time.Unix(0, clock.Load()) }}}
	a, b := serve.New(s, cfg), serve.New(s, cfg)
	body := `{"repo":"owner/repo","title":"fix"}`
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h := a
			if i%2 == 0 {
				h = b
			}
			responses <- keyedRequest(h, "/v1/jobs", body, "same-key")
		}(i)
	}
	wg.Wait()
	close(responses)
	var first string
	for r := range responses {
		if r.Code != 201 {
			t.Fatalf("response=%d %s", r.Code, r.Body)
		}
		if first == "" {
			first = r.Body.String()
		}
		if r.Body.String() != first {
			t.Fatal("different replay response")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("created %d jobs", calls.Load())
	}
	clock.Add(int64(24*time.Hour - time.Nanosecond))
	restarted := serve.New(s, cfg)
	if r := keyedRequest(restarted, "/v1/jobs", body, "same-key"); r.Body.String() != first || calls.Load() != 1 {
		t.Fatal("restart lost response before expiry")
	}
	clock.Add(1)
	if r := keyedRequest(restarted, "/v1/jobs", body, "same-key"); r.Code != 201 || calls.Load() != 2 {
		t.Fatalf("key did not expire: %d %s", r.Code, r.Body)
	}
}
