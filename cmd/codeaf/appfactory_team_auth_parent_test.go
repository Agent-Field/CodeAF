package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestAppFactoryTeamDoesNotRegainHostAccessAfterSignOut(t *testing.T) {
	// Simulate a loopback team service whose no-bearer request resolves as its host.
	b, stub, conn := adminStub(t, meBody("session_loopbackMember", true), func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	if _, err := admin(b, "sign_out", conn.ID); err != nil {
		t.Fatal(err)
	}
	if b.factoryHub().tokenFor(conn.ID) != "" {
		t.Fatal("sign-out retained the bearer")
	}
	list, err := b.FactoryList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Factories) != 1 || list.Factories[0].State != appwire.FactorySignedOut || list.Factories[0].Operator {
		t.Fatal("signed-out team connection regained the host's authority")
	}
	stub.mu.Lock()
	stub.requests = nil
	stub.mu.Unlock()
	_, err = b.FactoryQueue(context.Background(), conn.ID)
	if wireCodeOf(t, err) != appwire.CodeSignedOut {
		t.Fatal("private team read bypassed sign-in")
	}
	if stub.seen() != "" {
		t.Fatal("signed-out team read sent a private request")
	}
}

func TestAppFactoryStaleLocalMarkerDoesNotAuthorizeAReusedListener(t *testing.T) {
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/me" {
			_ = json.NewEncoder(w).Encode(map[string]any{"mode": "solo", "operator": true})
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	hub := testFactoryHub(t)
	hub.loaded = true
	hub.rows = []appFactoryRow{{ID: appLocalID, URL: stub.srv.URL, Name: "Old local service", Mode: api.FactorySolo, Local: true}}
	var queue api.Queue
	if err := hub.read(context.Background(), appLocalID, "/v1/queue", appFactorySmall, &queue); err == nil {
		t.Fatal("old local marker authorized a different listener without the owned service key")
	}
	view := hub.view(context.Background(), hub.rows[0])
	if view.State != appwire.FactoryUnreachable || view.Operator {
		t.Fatal("stale owned-local metadata adopted another authority")
	}
	if stub.seen() != "" {
		t.Fatal("an unowned old listener was contacted")
	}
}
