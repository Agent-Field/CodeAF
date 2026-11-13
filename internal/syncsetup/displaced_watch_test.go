package syncsetup

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// Contract K2 retires the hold on watch news before any heartbeat can discover displacement.
func TestWatchDisplacementRetiresTheDriveHoldImmediately(t *testing.T) {
	for _, newerLocal := range []bool{false, true} {
		t.Run(map[bool]string{false: "another holder", true: "higher local fence"}[newerLocal], func(t *testing.T) { watchDisplacementRetiresHold(t, newerLocal) })
	}
}

func watchDisplacementRetiresHold(t *testing.T, newerLocal bool) {
	celltest.RequireEngine(t)
	memory := directory.NewMemory(time.Now)
	t.Cleanup(func() { _ = memory.Close() })
	relay := httptest.NewServer(directory.Handler(reqsign.AuthenticateAt(time.Now), func(string) (directory.Directory, error) { return memory, nil }))
	t.Cleanup(relay.Close)
	t.Setenv(URLVar, relay.URL)
	local := t.TempDir()
	t.Setenv(home.EnvVar, local)
	_, err := identity.Ensure(local)
	mustNil(t, err)
	s, ok, err := Open(local)
	mustNil(t, err)
	if !ok {
		t.Fatal("native sync did not open")
	}
	otherHome := t.TempDir()
	_, err = identity.Adopt(otherHome, s.Identity, false)
	mustNil(t, err)
	other, _, err := Open(otherHome)
	mustNil(t, err)
	mustNil(t, other.Rename(t.Context(), "other computer"))
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	mustNil(t, err)
	_, err = s.Dir.Create(t.Context(), c.ID, directory.CellInit{})
	mustNil(t, err)
	changed := make(chan struct{}, 1)
	sleeping := make(chan struct{}, 1)
	d, err := s.Drive(t.Context(), cellstore.Engine{}, c, DriveOptions{Sleep: func(ctx context.Context, _ time.Duration) error {
		select {
		case sleeping <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}, OnRenewalChange: func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}})
	mustNil(t, err)
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	<-sleeping
	if _, _, held := d.RenewalStatus(); !held {
		t.Fatal("the original writer did not hold the chat")
	}
	_, fence, err := s.SendLease(c.ID)
	mustNil(t, err)
	if fence == 0 {
		t.Fatal("the original writer had no hold receipt")
	}
	if newerLocal {
		other = s
	}
	_, err = other.Dir.Acquire(t.Context(), c.ID, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: s.Device.ID(), Fence: fence}})
	mustNil(t, err)
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("watch displacement left the drive held until a heartbeat")
	}
	if _, _, held := d.RenewalStatus(); held {
		t.Error("watch displacement left RenewalStatus held")
	}
	if _, _, err := s.SendLease(c.ID); !os.IsNotExist(err) {
		t.Errorf("watch displacement retained its hold file: %v", err)
	}
	if err := d.Gate(); err == nil {
		t.Error("watch displacement left tools enabled")
	}
}
