package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// Contract F4 gives a pairing request deadline the same plain sentence as status.
func TestPairDoorExplainsARequestDeadline(t *testing.T) {
	celltest.RequireEngine(t)
	for _, kind := range []string{"transport", "parent", "approve"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(200 * time.Millisecond):
				}
			}))
			defer server.Close()
			t.Setenv(syncsetup.URLVar, server.URL)
			var out bytes.Buffer
			door := newPairDoor(strings.NewReader(""), &out)
			client := &http.Client{Timeout: 20 * time.Millisecond}
			door.requests = func(route pair.Mailbox) directory.Requests {
				return directory.NewLinkHTTP(route.URL, client)
			}
			ctx := t.Context()
			var args []string
			if kind == "parent" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
				client.Timeout = time.Second
			}
			if kind == "approve" {
				key, err := pair.NewLinkKey()
				if err != nil {
					t.Fatal(err)
				}
				ref := pair.LinkRef{Code: "k7m2q9xd", Key: key}
				args = []string{"approve", ref.Token()}
				door.approver = func(route pair.Mailbox) (pair.Approver, error) {
					return pair.Approver{Requests: door.requests(route)}, nil
				}
			}
			err := door.run(ctx, args)
			want := "the sync service at " + server.URL + " did not answer in time — try again"
			if err == nil || err.Error() != want {
				t.Fatalf("pairing deadline=%v; want %q", err, want)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("pairing lost the deadline cause: %v", err)
			}
			if strings.Contains(out.String(), context.DeadlineExceeded.Error()) {
				t.Fatalf("raw deadline on screen: %s", out.String())
			}
		})
	}
}
