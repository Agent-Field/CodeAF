package blobstore_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{"relay_storage_full", 507, blobstore.ErrRelayFull, 0},
		{"full", 507, blobstore.ErrFull, 0},
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
			if tc.status == 507 && (!errors.Is(err, blobstore.ErrFull) || errors.Is(err, blobstore.ErrRelayFull) != (tc.code == "relay_storage_full")) {
				t.Fatalf("Has = %v, want a full store with the correct ceiling", err)
			}
		})
	}
}

func TestFullWireAnswersKeepTheCeilingsDistinct(t *testing.T) {
	for _, tc := range []struct {
		err  error
		body string
	}{
		{blobstore.ErrFull, `{"err":"full"}`},
		{blobstore.Capped(blobstore.ErrFull, 5<<30), `{"err":"full","limit_bytes":5368709120}`},
		{blobstore.ErrRelayFull, `{"err":"relay_storage_full"}`},
	} {
		t.Run(tc.body, func(t *testing.T) {
			g := newRig(t)
			g.memory("alice").FailAfter(0, tc.err)
			req, err := http.NewRequest(http.MethodGet, g.srv.URL+"/v1/store/objects/"+strings.Repeat("a", 64), nil)
			if err != nil {
				t.Fatal(err)
			}
			fakeSign("alice")(req, nil)
			resp, err := g.srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 507 || string(body) != tc.body {
				t.Fatalf("wire = %d %s, want 507 %s", resp.StatusCode, body, tc.body)
			}
		})
	}
}
