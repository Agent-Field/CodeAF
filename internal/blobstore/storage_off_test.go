package blobstore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// StorageOff is the store wire of a relay that keeps the device list and no chats. What a computer is told is what the hosted
// relay tells an account it has not approved, because the computer already knows what to do with that.
func TestStorageOffAnswersAsTheHostedRelayDoes(t *testing.T) {
	store := NewMemory()
	real := Handler(func(*http.Request, []byte) (string, string, error) { return "id", "device", nil }, func(string) (Store, error) { return store, nil })
	srv := httptest.NewServer(StorageOff(real))
	defer srv.Close()
	c := NewHTTP(srv.URL, func(*http.Request, []byte) {}, srv.Client())

	counts, err := c.Stats(t.Context())
	if err != nil || counts.Storage != "off" {
		t.Fatalf("a relay with its storage off must say so: %+v %v", counts, err)
	}
	if _, err := c.PutFrame(t.Context(), []byte("a frame")); !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("an upload must be refused as storage off, not as anything else: %v", err)
	}
	if errors.Is(wireauth.ErrStorageOff, ErrFull) {
		t.Fatal("storage off is not the relay being full")
	}
	if ops := store.Log(); len(ops) != 0 {
		t.Fatalf("nothing may reach the store: %v", ops)
	}
	// Reads still reach the real handler, which is empty: nothing is stored, so nothing is held.
	have, err := c.Has(t.Context(), []string{strings.Repeat("a", 64)})
	if err != nil || len(have) != 1 || have[0] {
		t.Fatalf("a read must reach the store and find it empty: %v %v", have, err)
	}
}

// The identity rotation of a computer asks the relay to carry its storage permission to the successor. A relay with its storage
// off holds none, so it says yes and changes nothing, and `codeaf identity rotate` is not stopped by a relay that reports `off`.
func TestStorageOffCarriesNothingOverForARotation(t *testing.T) {
	real := Handler(func(*http.Request, []byte) (string, string, error) { return "id", "device", nil }, func(string) (Store, error) { return NewMemory(), nil })
	h := StorageOff(real)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, pathHandoff, strings.NewReader(`{}`)))
	if r.Code != http.StatusOK {
		t.Fatalf("the handoff of a relay with nothing to hand over: %d %s", r.Code, r.Body.String())
	}
}
