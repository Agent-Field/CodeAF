package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// Permission refusals retain the service's definite reason on a live lease.
func TestWriteAccessCheckKeepsDefiniteRefusal(t *testing.T) {
	want := (run.WriteAccessRefusal{}).Error()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": want, "reason": "conflict"})
	}))
	defer server.Close()
	err := client.Open(server.URL, "").Worker("fixture").CheckPublish(context.Background(), "fixture")
	var refusal client.Refusal
	if !errors.As(err, &refusal) || refusal.Status != http.StatusConflict || err.Error() != want {
		t.Fatalf("permission refusal = %v", err)
	}
}

// No request fields are added for a factory that predates permission checks.
func TestWriteAccessCheckSupportsOlderService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.ContentLength > 0 {
			t.Error("permission read changed the request contract")
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	if err := client.Open(server.URL, "").Worker("fixture").CheckPublish(context.Background(), "fixture"); err != nil {
		t.Fatalf("older service=%v", err)
	}
}
