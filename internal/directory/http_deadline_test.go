package directory_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// Contract F4 retains request deadlines so each pairing door can explain the cause.
func TestHTTPRetainsTheRequestDeadlineCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	c := directory.NewLinkHTTP(server.URL, server.Client())
	_, err := c.CreateRequest(ctx, directory.NewRequest{})
	if !errors.Is(err, directory.ErrUnreachable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request cause lost: %v", err)
	}
}
