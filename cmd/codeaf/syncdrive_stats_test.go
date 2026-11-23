package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestChatSyncStatsUsesSignedRelayClientAndStaysQuietWhenOff(t *testing.T) {
	celltest.Available(t)
	t.Setenv(home.EnvVar, t.TempDir())
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/store/stats" {
			t.Errorf("stats request = %s %s", r.Method, r.URL.Path)
		}
		if _, _, err := reqsign.Verify(r, nil, time.Now()); err != nil {
			t.Errorf("unsigned stats request: %v", err)
		}
		_, _ = w.Write([]byte(`{"stored_bytes":123,"limit_bytes":5368709120}`))
	}))
	defer srv.Close()
	t.Setenv(syncsetup.URLVar, srv.URL)
	if _, err := identity.EnsureSolo(home.Dir()); err != nil {
		t.Fatal(err)
	}
	if got, err := chatSyncStats(context.Background()); err != nil || got.StoredBytes != nil || calls.Load() != 0 {
		t.Fatalf("solo stats = %+v, %v; calls=%d", got, err, calls.Load())
	}
	if err := identity.EndSolo(home.Dir()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := chatSyncStats(ctx)
	if err != nil || got.StoredBytes == nil || *got.StoredBytes != 123 || got.LimitBytes == nil || *got.LimitBytes != 5<<30 || calls.Load() != 1 {
		t.Fatalf("paired stats = %+v, %v; calls=%d", got, err, calls.Load())
	}
	t.Setenv(syncsetup.URLVar, "off")
	if got, err := chatSyncStats(ctx); err != nil || got.StoredBytes != nil || calls.Load() != 1 {
		t.Fatalf("disabled stats = %+v, %v; calls=%d", got, err, calls.Load())
	}
}
