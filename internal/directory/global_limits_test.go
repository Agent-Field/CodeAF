package directory_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// TestHostedGlobalBrakesReachTheDirectoryClient proves C20: a first request
// receives the operator's storage refusal or admission wait through the wire.
func TestHostedGlobalBrakesReachTheDirectoryClient(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
		want   error
		wait   time.Duration
	}{
		{"relay_storage_full", 507, directory.ErrFull, 0},
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
			client := directory.NewHTTP(srv.URL, fakeSign("alice", devA), srv.Client())
			_, err := client.List(context.Background())
			if !errors.Is(err, tc.want) || wireauth.After(err) != tc.wait {
				t.Fatalf("List = %v after %v", err, wireauth.After(err))
			}
		})
	}
}
