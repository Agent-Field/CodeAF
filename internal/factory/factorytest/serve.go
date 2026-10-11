// Package factorytest gives surface tests the same HTTP boundary as the desk.
package factorytest

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// ServeInProcess returns a client Seam backed by seam on loopback. Cleanup
// belongs to the calling test so no listener survives a failed assertion.
func ServeInProcess(t testing.TB, seam factory.Seam) factory.Seam {
	t.Helper()
	h := serve.New(seam, serve.Config{
		Version: "test",
		// Approval needs a receipt before the door is called. This fixture
		// supplies that boundary without writing a person's factory record.
		RecordApproval: func(context.Context, int, bool) (string, error) {
			return "evt_00000000", nil
		},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		h.Close()
	})
	return client.Open(srv.URL, "").Seam()
}
