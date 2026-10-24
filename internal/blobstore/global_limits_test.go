package blobstore_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// TestHostedGlobalBrakesReachTheStoreClient proves C20: operator refusals
// reach the client's existing storage and retry handling without losing time.
func TestHostedGlobalBrakesReachTheStoreClient(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
		want   error
		wait   time.Duration
	}{
		{"relay_storage_full", 507, blobstore.ErrFull, 0},
		{"relay_admission_full", 429, wireauth.ErrRateLimited, 42 * time.Second},
	} {
		t.Run(tc.code, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.wait > 0 {
					w.Header().Set("Retry-After", "42")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"err":"` + tc.code + `"}`))
			}))
			defer srv.Close()
			client := blobstore.NewHTTP(srv.URL, fakeSign("alice"), srv.Client())
			_, err := client.Has(context.Background(), nil)
			if !errors.Is(err, tc.want) || wireauth.After(err) != tc.wait {
				t.Fatalf("Has = %v after %v", err, wireauth.After(err))
			}
		})
	}
}
