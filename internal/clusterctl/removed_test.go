package clusterctl_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	ctl "github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestStatusPreservesRemovedDeviceRefusal(t *testing.T) {
	for _, code := range []string{"revoked", "unauthorized", "skew"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"err":"` + code + `"}`))
			}))
			defer server.Close()
			client := ctl.Client{Base: server.URL, Sign: func(*http.Request, []byte) {}}
			_, err := client.Status(context.Background())
			want := ctl.ErrUnavailable
			if code == "revoked" {
				want = directory.ErrRevoked
			}
			if !errors.Is(err, want) {
				t.Fatalf("status refusal = %v, want %v", err, want)
			}
		})
	}
}
