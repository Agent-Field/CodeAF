package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/client"
)

func TestPublishCheckBoundedFailure(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusConflict, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "refused", status) }))
			defer server.Close()
			err := client.Open(server.URL, "").Worker("credential").CheckPublish(context.Background(), "lease")
			if err == nil {
				t.Fatal("unconfirmed publication authorized")
			}
			if status == http.StatusServiceUnavailable {
				if calls != 3 || !strings.Contains(err.Error(), "could not confirm Stop") || !strings.Contains(err.Error(), "Approve again") {
					t.Fatalf("calls=%d error=%v; want three attempts and honest retry guidance", calls, err)
				}
			} else if calls != 1 {
				t.Fatalf("refusal retried %d times", calls)
			}
		})
	}
}

func TestStopChoiceReadSupportsOlderFactoryRoutes(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "old route", status) }))
			defer srv.Close()
			on, err := client.Open(srv.URL, "").Worker("credential").StopAfterStep(context.Background(), "lease")
			if on || err != nil {
				t.Fatalf("old factory stopped healthy work: on=%v err=%v", on, err)
			}
		})
	}
}
