package syncsetup

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/relayserve"
)

// A closed bootstrap retains real independent stores and enrollment. Restoring
// it leaves each directory empty while its recovered member wins leadership
// through the ordinary transfer and fresh majority-read receipt.
func personalLeaderDirectories(t *testing.T) func(*testing.T) (*memberRig, *Sync) {
	t.Helper()
	r := newMemberRig(t)
	p := r.home(nil)
	personalLeader(t, r, p)
	p.stopMember()
	mustNil(t, r.svc.Close())
	r.svc = nil
	storeImage, homeImage := t.TempDir(), t.TempDir()
	mustNil(t, os.CopyFS(storeImage, os.DirFS(r.storage)))
	mustNil(t, os.CopyFS(homeImage, os.DirFS(p.Home)))
	start := r.clock.Now()
	home, id, dev := p.Home, p.Identity, p.Device
	return func(t *testing.T) (*memberRig, *Sync) {
		t.Helper()
		for _, s := range r.homes {
			s.stopMember()
		}
		if r.svc != nil {
			mustNil(t, r.svc.Close())
			r.svc = nil
		}
		for _, image := range [][2]string{{storeImage, r.storage}, {homeImage, home}} {
			mustNil(t, os.RemoveAll(image[1]))
			mustNil(t, os.CopyFS(image[1], os.DirFS(image[0])))
		}
		r.clock = directorytest.NewFakeClock()
		r.clock.Advance(start.Sub(r.clock.Now()))
		r.svc = relayserve.New(relayserve.Config{Store: r.storage, Now: r.clock.Now, After: r.clock.After})
		p := buildAt(home, r.server.URL, id, dev, DefaultInterval, r.clock.Now)
		r.homes = []*Sync{p}
		t.Cleanup(p.stopMember)
		personalLeader(t, r, p)
		return r, p
	}
}

// R6/R25 require every conformance case to start empty, including after the
// previous identity retired, and still use a recovered actual personal leader.
func TestR6R25PersonalLeaderDirectoryIsolation(t *testing.T) {
	fresh := personalLeaderDirectories(t)
	t.Run("retired", func(t *testing.T) {
		_, p := fresh(t)
		_, err := p.Dir.Create(t.Context(), "01J0000000000000000000000A", directory.CellInit{Head: head})
		mustNil(t, err)
		mustNil(t, p.Dir.PutDevice(t.Context(), p.Device.ID(), directory.Device{V: 1, Name: "c2VhbGVk"}))
		mustNil(t, p.Dir.SetVault(t.Context(), "", "saved-vault"))
		_, err = p.Dir.Rotate(t.Context(), directory.RotationReq{Op: directory.OpFreeze})
		mustNil(t, err)
		_, err = p.Dir.Rotate(t.Context(), directory.RotationReq{Op: directory.OpRetire})
		mustNil(t, err)
	})
	t.Run("empty", func(t *testing.T) {
		_, p := fresh(t)
		listing, err := p.Dir.List(t.Context())
		mustNil(t, err)
		if len(listing.Cells) != 0 || len(listing.Devices) != 0 || listing.Identity.Vault != "" || listing.Identity.Rotation != nil {
			t.Fatalf("recovered directory retained the previous case: %+v", listing)
		}
	})
}
