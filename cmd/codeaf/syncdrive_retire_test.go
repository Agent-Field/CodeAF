package main

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// C7/R2 gives a reopened chat one maintenance loop. The displaced window must
// finish its old loop before another drive can acquire and publish its copy.
func TestC7R2ReopeningChatStopsPreviousDrive(t *testing.T) {
	celltest.Available(t)
	t.Setenv(home.EnvVar, t.TempDir())
	service := relayserve.New(relayserve.Config{Store: t.TempDir()})
	defer service.Close()
	server := httptest.NewServer(service.Handler)
	defer server.Close()
	t.Setenv(syncsetup.URLVar, server.URL)
	_, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		t.Fatalf("open: %v %v", ok, err)
	}
	c := cell.Cell{ID: "01J0000000000000000000000A", Root: t.TempDir()}
	engine := cellstore.Engine{DataRoot: t.TempDir()}
	started, stopped := make(chan struct{}), make(chan struct{})
	var once sync.Once
	old, err := s.Drive(t.Context(), engine, c, syncsetup.DriveOptions{Sleep: func(ctx context.Context, _ time.Duration) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close(context.Background())
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("previous drive did not begin maintenance")
	}
	_, err = s.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "sealed-head"})
	if err != nil {
		t.Fatal(err)
	}
	if err = old.CheckLease(t.Context()); err != nil {
		t.Fatal(err)
	}
	if old.Gate() == nil {
		t.Fatal("fixture did not displace the old drive")
	}
	kept := &liveDrive{drive: old, attempted: true}
	book := &driveBook{drive: map[string]*liveDrive{c.ID: kept}}
	defer book.closeAll()
	next := book.driveOf(c, engine, func(error) {})
	if next == kept {
		t.Fatal("reopened chat retained its displaced drive")
	}
	select {
	case <-stopped:
	default:
		t.Error("reopened chat left the previous drive running")
	}
	if next != nil {
		_ = next.Gate()
	}
}
