package pair

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// L4-R2: Revoking and rejoining with a current code replaces the removed device key.
func TestL4FixRemovedComputerRejoins(t *testing.T) {
	r := newChatRig(t)
	ctx := t.Context()
	ui := newScreen()
	offering := r.offer(ctx, ui)
	if _, err := Join(ctx, r.route, Joining{Home: r.homeB, Label: "desk-b"}, ui.nextCode(t).Shown(), newScreen()); err != nil {
		t.Fatal(err)
	}
	if end := within(t, offering); end.err != nil {
		t.Fatal(end.err)
	}
	old, err := identity.Device(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	a, err := identity.Device(r.homeA)
	if err != nil {
		t.Fatal(err)
	}
	dir := directory.NewMemory(time.Now)
	if err := syncsetup.PutOwnDevice(ctx, dir.For(a.ID()), r.a, a, "laptop-a"); err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.PutOwnDevice(ctx, dir.For(old.ID()), r.a, old, "desk-b"); err != nil {
		t.Fatal(err)
	}
	if err := dir.For(a.ID()).Revoke(ctx, old.ID()); err != nil {
		t.Fatal(err)
	}
	ui = newScreen()
	offering = r.offer(ctx, ui)
	if _, err := Join(ctx, r.route, Joining{Home: r.homeB, Label: "desk-b", Replace: true}, ui.nextCode(t).Shown(), newScreen()); err != nil {
		t.Fatal(err)
	}
	if end := within(t, offering); end.err != nil {
		t.Fatal(end.err)
	}
	fresh, err := identity.Device(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID() == old.ID() {
		t.Fatal("rejoin retained the revoked key")
	}
	if err := fresh.Cert.Verify(r.a.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.PutOwnDevice(ctx, dir.For(fresh.ID()), r.a, fresh, "desk-b"); err != nil {
		t.Fatal(err)
	}
	list, err := dir.For(fresh.ID()).List(ctx)
	if err != nil || list.Devices[fresh.ID()].Revoked || list.Devices[fresh.ID()].Name == "" || !list.Devices[old.ID()].Revoked {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}
