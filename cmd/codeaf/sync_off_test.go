package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// TestC10SyncOffCannotBeBypassedByPairVia proves C10 at the pairing route door,
// even for a home whose identity was already shared.
func TestC10SyncOffCannotBeBypassedByPairVia(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	if _, err := identity.EnsureSolo(home.Dir()); err != nil {
		t.Fatal(err)
	}
	if err := identity.EndSolo(home.Dir()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, "off")
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	for _, via := range []string{"", srv.URL} {
		if _, err := pairMailbox(via); !errors.Is(err, pair.ErrSyncOff) {
			t.Fatalf("pair via %q with sync off: %v", via, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("off pairing made %d requests", hits.Load())
	}
}
