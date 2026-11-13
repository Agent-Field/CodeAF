package syncsetup

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func TestRemovedHomeStopsTrafficUntilPairedAgain(t *testing.T) {
	var device atomic.Value
	device.Store("")
	var requests, beats atomic.Int64
	h := newTwoHomes(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := base64.RawURLEncoding.DecodeString(r.Header.Get(reqsign.HeaderCert))
			var cert identity.Cert
			_ = json.Unmarshal(raw, &cert)
			if cert.DeviceID() == device.Load().(string) {
				requests.Add(1)
				if strings.HasSuffix(r.URL.Path, "/heartbeat") {
					beats.Add(1)
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	h.a = openSync(t, h.homeA)
	device.Store(h.a.Device.ID())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The accelerated sleeper represents repeated heartbeat intervals. Until
	// the removal is known it keeps an ordinary pace, so heartbeats do not
	// queue up commits ahead of the member's first catch-up; afterwards it
	// runs fast, so the observation below spans many heartbeat intervals.
	var fast atomic.Bool
	options := DriveOptions{Sleep: func(ctx context.Context, _ time.Duration) error {
		wait := 250 * time.Millisecond
		if fast.Load() {
			wait = 10 * time.Millisecond
		}
		select {
		case <-time.After(wait):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	d, err := h.a.Drive(ctx, h.engine, h.cell, options)
	mustNil(t, err)
	appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"user","content":"keep working locally"}`+"\n")
	_, err = d.Store(h.engine).Seal(ctx, h.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	mustNil(t, d.Close(ctx))
	d, err = h.a.Drive(ctx, h.engine, h.cell, options)
	mustNil(t, err)
	defer func() {
		closing, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = d.Close(closing)
	}()
	mustNil(t, h.a.memberRound(ctx))
	watch := h.a.Follow()
	defer watch.Close()
	h.a.Interval = 10 * time.Millisecond
	stopped := make(chan struct{})
	go func() { defer close(stopped); h.a.RunState(ctx) }()
	polling := make(chan struct{})
	go func() {
		defer close(polling)
		for ctx.Err() == nil {
			_, _ = h.a.Rows(ctx)
			_, _ = h.a.FleetSize(ctx)
			_, _ = h.a.Roster(ctx)
			_, _ = h.a.Dir.Presence(ctx)
			select {
			case <-time.After(10 * time.Millisecond):
			case <-ctx.Done():
			}
		}
	}()
	mustNil(t, h.b.putDevice(ctx, ""))
	mustNil(t, h.b.Dir.Revoke(ctx, h.a.Device.ID()))
	if _, err := h.a.Dir.List(ctx); !errors.Is(err, directory.ErrRevoked) {
		t.Fatalf("removal was not learned: %v", err)
	}
	// Let requests already in flight finish and the heartbeat reach its fast
	// pace, then sample many accelerated poll, state and heartbeat intervals
	// while the window remains open.
	fast.Store(true)
	time.Sleep(300 * time.Millisecond)
	before := requests.Load()
	time.Sleep(250 * time.Millisecond)
	if extra := requests.Load() - before; extra != 0 {
		t.Errorf("removed home sent %d further requests (heartbeats=%d)", extra, beats.Load())
	}
	if err := d.Gate(); err != nil {
		t.Errorf("local tool work was disabled: %v", err)
	}
	cancel()
	<-polling
	<-stopped
	reopened := openSync(t, h.homeA)
	before = requests.Load()
	_, _ = reopened.Rows(t.Context())
	_, _ = reopened.ClusterStatus(t.Context())
	if requests.Load() != before {
		t.Error("reopening forgot the known removal")
	}
	dev, err := identity.NewDevice(h.a.Identity)
	mustNil(t, err)
	mustNil(t, identity.Install(h.homeA, h.a.Identity, dev))
	paired := openSync(t, h.homeA)
	mustNil(t, paired.putDevice(t.Context(), ""))
	if _, err := paired.Dir.List(t.Context()); err != nil {
		t.Errorf("pairing did not restore access: %v", err)
	}
}

func TestOpenRemovedHomeAdoptsItsNewlyPairedDevice(t *testing.T) {
	r := newMemberRig(t)
	a := r.home(nil)
	b := r.home(&a.Identity)
	ctx := t.Context()
	mustNil(t, a.putDevice(ctx, ""))
	mustNil(t, b.putDevice(ctx, ""))
	mustNil(t, a.Dir.Revoke(ctx, b.Device.ID()))
	if _, err := b.Dir.List(ctx); !errors.Is(err, directory.ErrRevoked) {
		t.Fatalf("removal was not learned: %v", err)
	}
	dev, err := identity.NewDevice(b.Identity)
	mustNil(t, err)
	mustNil(t, identity.Install(b.Home, b.Identity, dev))
	paired := buildAt(b.Home, b.Relay, b.Identity, dev, b.Interval, b.now)
	mustNil(t, paired.putDevice(ctx, ""))
	if _, err := b.Rows(ctx); err != nil {
		t.Fatalf("open Home did not resume after pairing: %v", err)
	}
	watch := b.Follow()
	defer watch.Close()
	waitFor(t, "the paired Home watch to connect", func() bool { return watch.State().Up })
}
