package client_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/client"
)

// A runner slot opens a client for every pull, so clients opened one after
// another must reuse the connection the last one left idle. A client of its
// own pool kept each idle connection open until the transport's idle timeout,
// and a machine with many slots held hundreds of sockets to its own factory.
func TestClientsOpenedInTurnShareOneConnection(t *testing.T) {
	var mu sync.Mutex
	open := map[net.Conn]bool{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ConnState = func(c net.Conn, s http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		if s == http.StateClosed || s == http.StateHijacked {
			delete(open, c)
		} else {
			open[c] = true
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	for range 20 {
		if _, _, err := client.Open(server.URL, "").Worker("slot").Pull(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(open) > 1 {
		t.Fatalf("twenty pulls in turn left %d connections open", len(open))
	}
}
