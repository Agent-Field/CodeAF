package directory

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStorageOptInDirectoryWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"err":"storage_off"}`))
	}))
	defer srv.Close()
	_, err := NewHTTP(srv.URL, func(*http.Request, []byte) {}, srv.Client()).List(t.Context())
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("directory refusal: %v", err)
	}
}
