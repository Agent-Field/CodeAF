package directory_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
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
			if tc.code == "relay_storage_full" {
				if !errors.Is(err, wireauth.ErrRelayFull) {
					t.Fatalf("List = %v, want shared sentinel as well as directory.ErrFull", err)
				}
				r, ok := cellsync.RefusalOf(err)
				if !ok || r.Say(err) != chatlist.RelayStorageFull {
					t.Fatalf("shared directory refusal = %+v, %v; want shared-capacity sentence", r, ok)
				}
				assertDirectoryFullWire(t, err, 507, `{"err":"relay_storage_full"}`)
			}
		})
	}
}

func TestDirectoryFullWireKeepsSharedCapacityDistinct(t *testing.T) {
	assertDirectoryFullWire(t, directory.ErrFull, 503, `{"err":"full"}`)
	assertDirectoryFullWire(t, blobstore.ErrRelayFull, 507, `{"err":"relay_storage_full"}`)
	assertDirectoryFullWire(t, wireauth.ErrRelayFull, 507, `{"err":"relay_storage_full"}`)
}

func assertDirectoryFullWire(t *testing.T, refusal error, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(directory.Handler(fakeAuth, func(string) (directory.Directory, error) {
		return failing{refusal}, nil
	}))
	defer srv.Close()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/dir/cells/"+cellX+"/acquire", nil)
	if err != nil {
		t.Fatal(err)
	}
	fakeSign("alice", devA)(req, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != status || string(got) != body+"\n" {
		t.Fatalf("wire = %d %s (%v), want %d %s", resp.StatusCode, got, err, status, body)
	}
}
