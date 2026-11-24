package syncsetup

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// fakeDirectRoadHost stands in for the pion implementation: it records what the
// sync package asked of it and lets a test open a stream into the door without
// any network, which is the one thing the sync package decides.
type fakeDirectRoadHost struct {
	stun []string

	mu       sync.Mutex
	ctx      context.Context
	accept   func(ctx context.Context, stream io.ReadWriteCloser)
	closed   chan struct{}
	closeOne sync.Once
}

func (h *fakeDirectRoadHost) Offered(context.Context, livedoor.Peer, []byte, livedoor.Signaller) livedoor.DirectSession {
	return nil
}

func (h *fakeDirectRoadHost) SetAcceptor(ctx context.Context, accept func(ctx context.Context, stream io.ReadWriteCloser)) {
	h.mu.Lock()
	h.ctx, h.accept = ctx, accept
	h.mu.Unlock()
}

func (h *fakeDirectRoadHost) Close() { h.closeOne.Do(func() { close(h.closed) }) }

func (h *fakeDirectRoadHost) acceptor() (context.Context, func(context.Context, io.ReadWriteCloser)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ctx, h.accept
}

// connect hands the door the far end of a fresh pipe, the way the road hands it
// the stream of an opened data channel, and returns the viewer's end.
func (h *fakeDirectRoadHost) connect(t *testing.T) io.ReadWriteCloser {
	t.Helper()
	ctx, accept := h.acceptor()
	if accept == nil {
		t.Fatal("the door never installed an acceptor on the direct road's host")
	}
	viewer, host := net.Pipe()
	guard.Go("syncsetup.test.direct.accept", func() { accept(ctx, host) })
	t.Cleanup(func() { _ = viewer.Close() })
	return viewer
}

// fakeDirectRoad installs a counting stand-in for the direct road and restores
// whatever the program had installed when the test ends.
type fakeDirectRoad struct {
	mu    sync.Mutex
	hosts []*fakeDirectRoadHost
}

func installFakeDirectRoad(t *testing.T) *fakeDirectRoad {
	t.Helper()
	previous, _ := InstalledDirectRoad()
	t.Cleanup(func() { SetDirectRoad(previous) })
	f := &fakeDirectRoad{}
	SetDirectRoad(DirectRoad{
		NewHost: func(stun []string) DirectRoadHost {
			h := &fakeDirectRoadHost{stun: slices.Clone(stun), closed: make(chan struct{})}
			f.mu.Lock()
			f.hosts = append(f.hosts, h)
			f.mu.Unlock()
			return h
		},
		Offer: func(context.Context, []string, livedoor.Signaller, <-chan []byte) (io.ReadWriteCloser, error) {
			return nil, errors.New("the test's direct road makes no offers")
		},
	})
	return f
}

func (f *fakeDirectRoad) built() []*fakeDirectRoadHost {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.hosts)
}

// V36 / V38: a computer builds no direct road, and so can make no STUN request,
// unless remote control is on, the road is not pinned to the relay, and the
// program installed one; when it does, the door takes its host half and serves
// every live tunnel through the road-aware handler.
func TestDirectIsAbsentWhenRemoteControlIsOffOrTheRoadIsRelay(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		install bool
		want    bool
	}{
		{"remote control off", map[string]string{"CODEAF_REMOTE_CONTROL": "off"}, true, false},
		{"road pinned to the relay", map[string]string{"CODEAF_REMOTE_CONTROL": "on", "CODEAF_REMOTE_ROAD": "relay"}, true, false},
		{"nothing installed", map[string]string{"CODEAF_REMOTE_CONTROL": "on"}, false, false},
		{"on, default road", map[string]string{"CODEAF_REMOTE_CONTROL": "on"}, true, true},
		{"on, road auto", map[string]string{"CODEAF_REMOTE_CONTROL": "on", "CODEAF_REMOTE_ROAD": "auto"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			r := newLiveRig(t)
			road := installFakeDirectRoad(t)
			if !tc.install {
				SetDirectRoad(DirectRoad{})
			}
			cfg, err := r.a.filesDoorConfig()
			if err != nil {
				t.Fatal(err)
			}
			hosts := road.built()
			if !tc.want {
				if len(hosts) != 0 || cfg.Direct != nil || cfg.LiveRoad != nil {
					t.Fatalf("a direct road was built (%d hosts, Direct set=%v, LiveRoad set=%v)", len(hosts), cfg.Direct != nil, cfg.LiveRoad != nil)
				}
				if config.RemoteControlOn() != (cfg.Live != nil) {
					t.Fatalf("Live set=%v with remote control %v: the relay road must be unchanged", cfg.Live != nil, config.RemoteControlOn())
				}
				return
			}
			if len(hosts) != 1 || cfg.Direct == nil || cfg.LiveRoad == nil || cfg.Live != nil {
				t.Fatalf("hosts=%d Direct=%v LiveRoad=%v Live=%v; want one host and the road-aware handler only", len(hosts), cfg.Direct != nil, cfg.LiveRoad != nil, cfg.Live != nil)
			}
			if want := config.RemoteSTUN(); !slices.Equal(hosts[0].stun, want) || len(want) != 2 {
				t.Fatalf("the host was given STUN %v, want the configured default %v", hosts[0].stun, want)
			}
		})
	}

	t.Run("STUN off reaches the host as an empty list", func(t *testing.T) {
		t.Setenv("CODEAF_REMOTE_CONTROL", "on")
		t.Setenv("CODEAF_REMOTE_STUN", "off")
		r := newLiveRig(t)
		road := installFakeDirectRoad(t)
		if _, err := r.a.filesDoorConfig(); err != nil {
			t.Fatal(err)
		}
		hosts := road.built()
		if len(hosts) != 1 || hosts[0].stun == nil || len(hosts[0].stun) != 0 {
			t.Fatalf("with STUN off the host was given %#v, want an empty non-nil list", hosts)
		}
	})
}

// V35: the door counts direct streams (filesdoor) and the router counts direct
// tunnels (livedoor); they are one limit written twice, so they must agree.
func TestDirectCapIsTheSameNumberOnBothSidesOfTheDoor(t *testing.T) {
	if filesdoor.MaxDirect != livedoor.MaxDirect {
		t.Fatalf("filesdoor admits %d direct streams and livedoor serves %d direct tunnels; the two caps are one number", filesdoor.MaxDirect, livedoor.MaxDirect)
	}
}

// V34: a device removed through the real directory path loses its direct view
// with the word `removed`, the view said it was on the direct road, and the
// same device is refused a new direct stream afterwards.
func TestRemovingADeviceClosesItsDirectViewThroughTheRealDirectoryPath(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	r := newLiveRig(t)
	liveDoorRosterFor = 20 * time.Millisecond
	road := installFakeDirectRoad(t)

	stub := &stubLiveAgent{title: "Direct Revoke Chat"}
	hostHeldChat(t, r, r.cell.ID, stub)
	r.a.ensureFilesDoor()
	waitFor(t, "the door to take its direct host half", func() bool {
		hosts := road.built()
		if len(hosts) == 0 {
			return false
		}
		_, accept := hosts[0].acceptor()
		return accept != nil
	})
	host := road.built()[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun, err := filesdoor.DialDirect(ctx, host.connect(t), r.b.Identity, r.b.Device, r.a.Device.ID())
	if err != nil {
		t.Fatalf("DialDirect: %v", err)
	}
	mux := livedoor.NewMux(tun, livedoor.Viewer, livedoor.MuxConfig{})
	t.Cleanup(func() { _ = mux.Close() })
	ack, err := mux.WaitHelloAck(ctx)
	if err != nil {
		t.Fatalf("WaitHelloAck: %v", err)
	}
	if ack.Road != livedoor.RoadDirect || ack.Direct {
		t.Fatalf("the host's ack on a direct stream = %+v, want road %q and no further offer", ack, livedoor.RoadDirect)
	}

	opened := make(chan *livedoor.Channel, 4)
	viewer, err := remote.Roam(nameA, remote.Hello{Surface: nameB, Join: true}, remote.Roaming{
		Dial: func() (io.ReadWriteCloser, error) {
			ch, openErr := mux.Open(ctx, r.cell.ID)
			if openErr != nil {
				return nil, openErr
			}
			opened <- ch
			return ch, nil
		},
	})
	if err != nil {
		t.Fatalf("a direct view did not open: %v", err)
	}
	t.Cleanup(func() { _ = viewer.Close() })
	ch := <-opened

	mustNil(t, r.a.Dir.Revoke(context.Background(), r.b.Device.ID()))
	select {
	case <-ch.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a removed device's direct view stayed open")
	}
	if got := ch.Reason(); got != livedoor.ReasonRemoved {
		t.Fatalf("the direct view ended with %q, want %q", got, livedoor.ReasonRemoved)
	}

	_, err = filesdoor.DialDirect(ctx, host.connect(t), r.b.Identity, r.b.Device, r.a.Device.ID())
	var refused *filesdoor.RefusedError
	if !errors.As(err, &refused) || strings.TrimSpace(refused.Reason) != filesdoor.RemovedLine {
		t.Fatalf("a removed device's new direct stream gave %v, want a refusal that says %q", err, filesdoor.RemovedLine)
	}
}

// V37 (lifetime): the host half is closed when the door's context ends, so a
// door that stops takes every pending attempt with it.
func TestDirectHostEndsWithTheDoor(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	r := newLiveRig(t)
	road := installFakeDirectRoad(t)
	r.a.ensureFilesDoor()
	waitFor(t, "the door to take its direct host half", func() bool {
		hosts := road.built()
		if len(hosts) == 0 {
			return false
		}
		_, accept := hosts[0].acceptor()
		return accept != nil
	})
	host := road.built()[0]
	stopFilesDoor(r.a.Home)
	select {
	case <-host.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the direct host half outlived the files door that owned it")
	}
}
