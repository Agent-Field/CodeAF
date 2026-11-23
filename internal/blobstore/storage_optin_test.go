package blobstore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStorageOptInWireAndNoWriteRequest(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == pathStats {
			_, _ = w.Write([]byte(`{"storage":"off"}`))
			return
		}
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"err":"storage_off"}`))
	}))
	defer srv.Close()
	c := NewHTTP(srv.URL, func(*http.Request, []byte) {}, srv.Client())
	if _, err := c.PutFrame(t.Context(), nil); !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("wire refusal: %v", err)
	}
	c.CanStore = func() bool { return false }
	if _, err := c.PutFrame(context.Background(), nil); !errors.Is(err, wireauth.ErrStorageOff) || calls != 1 {
		t.Fatalf("local gate: calls=%d err=%v", calls, err)
	}
	counts, err := c.Stats(t.Context())
	if err != nil || counts.Storage != "off" || calls != 2 {
		t.Fatalf("stats while off: %+v %v", counts, err)
	}
	if errors.Is(wireauth.ErrStorageOff, ErrFull) || errors.Is(wireauth.ErrStorageOff, wireauth.ErrRelayFull) {
		t.Fatal("off must remain distinct from full")
	}
}

func TestStorageOptInSelfHostedStatsOmitStorage(t *testing.T) {
	h := Handler(func(*http.Request, []byte) (string, string, error) { return "id", "device", nil }, func(string) (Store, error) { return NewMemory(), nil })
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", pathStats, nil))
	var body map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, present := body["storage"]; present {
		t.Fatal("self-hosted stats sent storage state")
	}
}
