package syncsetup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

func TestConfirmedRemovedFollowsCurrentPairing(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(true))
	r := newMemberRig(t)
	a := r.home(nil)
	b := r.home(&a.Identity)
	mustNil(t, a.putDevice(t.Context(), ""))
	mustNil(t, b.putDevice(t.Context(), ""))
	if ConfirmedRemoved(b.Home) {
		t.Fatal("paired computer reported removed")
	}
	mustNil(t, a.Dir.Revoke(t.Context(), b.Device.ID()))
	if _, err := b.Dir.List(t.Context()); !errors.Is(err, directory.ErrRevoked) {
		t.Fatalf("removal was not learned: %v", err)
	}
	if !ConfirmedRemoved(b.Home) {
		t.Fatal("confirmed removal was lost")
	}
	dev, err := identity.NewDevice(b.Identity)
	mustNil(t, err)
	mustNil(t, identity.Install(b.Home, b.Identity, dev))
	paired := openSync(t, b.Home)
	mustNil(t, paired.putDevice(t.Context(), ""))
	if ConfirmedRemoved(b.Home) {
		t.Fatal("new pairing retained the old removal")
	}
}

func TestConfirmedRemovedReadFailureIsNotRemoval(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(true))
	r := newMemberRig(t)
	s := r.home(nil)
	s.rememberRemoved()
	if !ConfirmedRemoved(s.Home) {
		t.Fatal("fixture removal was not confirmed")
	}
	mustNil(t, os.WriteFile(filepath.Join(s.Home, identity.File), []byte("unreadable identity"), 0600))
	if ConfirmedRemoved(s.Home) {
		t.Fatal("identity read failure counted as removal")
	}
}
