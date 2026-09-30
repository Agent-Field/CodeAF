package relayserve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// watchCap is the cap the in-process relay is built with, small enough that a
// case can fill it; the default of a thousand is proved against a live relay.
const watchCap = 3

// household is one identity and the devices made under it as cases name them.
type household struct {
	t  *testing.T
	id identity.Identity
	mu sync.Mutex
	by map[string]device
}

func newHousehold(t *testing.T) *household {
	id, _ := newIdentity(t)
	return &household{t: t, id: id, by: map[string]device{}}
}

func (h *household) device(name string) device {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d, ok := h.by[name]; ok {
		return d
	}
	d := newDevice(h.t, h.id, h.t.TempDir())
	h.by[name] = d
	return d
}

func (r *rig) signer(d device) wireauth.Sign { return reqsign.SignFor(d, r.clock.Now) }

func (r *rig) watchRig(t *testing.T) directorytest.WatchRig {
	home := newHousehold(t)
	return directorytest.WatchRig{
		Rig: directorytest.Rig{
			Clock:   r.clock,
			Devices: func(name string) directory.Client { return r.dir(home.device(name)) },
			ID:      func(name string) string { return home.device(name).dev.ID() },
		},
		Base: r.srv.URL,
		Sign: func(name string) wireauth.Sign { return r.signer(home.device(name)) },
		Stranger: func() wireauth.Sign {
			_, d := newIdentity(t)
			return r.signer(d)
		},
		Cap: watchCap,
	}
}

func TestWatchConformance(t *testing.T) {
	directorytest.RunWatch(t, func(t *testing.T) directorytest.WatchRig {
		r := &rig{t: t, store: t.TempDir(), clock: directorytest.NewFakeClock(), watch: watchCap}
		r.start()
		return r.watchRig(t)
	})
}

// firstVersion dials the watch socket as d and returns the version it opens with.
func firstVersion(t *testing.T, r *rig, d device) uint64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	conn, resp, err := directorytest.DialWatch(ctx, r.srv.URL, r.signer(d))
	if err != nil {
		t.Fatalf("watch dial: %v (%v)", err, statusOf(resp))
	}
	defer conn.CloseNow()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("first frame: %v", err)
	}
	var frame struct{ V uint64 }
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("first frame %q is not a version: %v", data, err)
	}
	return frame.V
}

func statusOf(resp *http.Response) string {
	if resp == nil {
		return "no response"
	}
	return resp.Status
}

// The version is durable: a relay that restarts over the same store opens a new
// socket at the number the old one last reported, never back at zero.
func TestWatchVersionSurvivesRestart(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.dir(a).Publish(bg, cell, directory.Publish{Fence: 1, OldHead: head1, Head: head2, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	last := firstVersion(t, r, a)
	if last < 2 {
		t.Fatalf("two visible writes left the version at %d", last)
	}

	r.restart()

	if got := firstVersion(t, r, a); got != last {
		t.Fatalf("first frame after a restart is %d, want %d", got, last)
	}
}
