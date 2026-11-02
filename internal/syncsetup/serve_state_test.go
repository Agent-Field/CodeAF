package syncsetup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// R4.1 exercises the serving lifetime over the existing authenticated fake
// relay, including a key saved by another computer after serving began.
func TestR41ServeHomeStateReceivesProviderKeysAtStartAndOnInterval(t *testing.T) {
	for _, atStart := range []bool{true, false} {
		t.Run(map[bool]string{true: "startup", false: "interval"}[atStart], func(t *testing.T) {
			server := relay(t)
			var cluster atomic.Int64
			var directoryRequests atomic.Int64
			next := server.Config.Handler
			server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/cluster/") {
					cluster.Add(1)
				}
				if strings.HasPrefix(r.URL.Path, "/v1/dir/") {
					directoryRequests.Add(1)
				}
				next.ServeHTTP(w, r)
			})
			aHome := machine(t, server.URL)
			t.Setenv(config.ProfileDirEnv, "")
			a, ok, err := Open(aHome)
			if err != nil || !ok {
				t.Fatalf("open source: %v", err)
			}
			bHome := t.TempDir()
			mustNil(t, identity.Save(bHome, a.Identity))
			if atStart {
				t.Setenv(IntervalVar, "3600000")
				mustNil(t, config.WriteAPIKey(aHome, fakeKeyOne))
				mustNil(t, a.SyncState(t.Context()))
			} else {
				t.Setenv(IntervalVar, "25")
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			errors := make(chan error, 16)
			go func() {
				defer close(done)
				RunHomeState(ctx, bHome, func(err error) { errors <- err })
			}()
			t.Cleanup(func() { cancel(); <-done })
			if !atStart {
				waitServeState(t, func() bool { return directoryRequests.Load() > 0 })
				mustNil(t, config.WriteAPIKey(aHome, fakeKeyTwo))
				// B owns stateMu during that first request. A's SyncState cannot
				// publish this key until B's startup round releases it, so only
				// a later interval round can receive the key on B.
				mustNil(t, a.SyncState(t.Context()))
			}
			want := fakeKeyTwo
			if atStart {
				want = fakeKeyOne
			}
			waitServeState(t, func() bool { return config.PersistedAPIKey(bHome) == want })
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("R4.3: serving home-state goroutine did not join")
			}
			select {
			case err := <-errors:
				t.Fatalf("home-state round: %v", err)
			default:
			}
			if cluster.Load() != 0 {
				t.Fatal("R4.3: serve started a cluster member")
			}
		})
	}
}

func TestR42ServeHomeStateIneligibleHomesStaySilent(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, "")
	for _, reason := range []string{"no identity", "cells unavailable", "quiet"} {
		t.Run(reason, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected request", http.StatusInternalServerError)
			}))
			defer server.Close()
			home := machine(t, server.URL)
			switch reason {
			case "no identity":
				mustNil(t, os.RemoveAll(home))
				mustNil(t, os.MkdirAll(home, 0o700))
			case "quiet":
				// A solo home is made in a fresh folder, because codeaf now refuses to
				// create an identity in a home this process saw and that was then
				// deleted (internal/home Witness), which is what removing the paired
				// machine's folder and reusing its path would look like.
				home = filepath.Join(t.TempDir(), "solo")
				_, err := identity.EnsureSolo(home)
				mustNil(t, err)
			case "cells unavailable":
				t.Cleanup(cell.UseAvailabilityInTests(false))
			}
			before, err := os.ReadDir(home)
			mustNil(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			RunHomeState(ctx, home, func(err error) { t.Errorf("ineligible home reported: %v", err) })
			if ctx.Err() != nil {
				t.Fatal("ineligible home kept its loop alive")
			}
			if requests.Load() != 0 {
				t.Fatal("ineligible home contacted the relay")
			}
			entries, err := os.ReadDir(home)
			mustNil(t, err)
			if len(entries) != len(before) {
				t.Fatal("ineligible home created state")
			}
			for i, entry := range entries {
				if entry.Name() != before[i].Name() {
					t.Errorf("ineligible home created %s", entry.Name())
				}
			}
		})
	}
}

func TestR42ServeHomeStateRevokedDeviceStopsQuietly(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, "")
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "relay refusal", true: "saved removal"}[known], func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"err":"revoked"}`))
			}))
			defer server.Close()
			home := machine(t, server.URL)
			if known {
				s, _, err := Open(home)
				mustNil(t, err)
				s.rememberRemoved()
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			RunHomeState(ctx, home, func(err error) { t.Errorf("revoked home reported: %v", err) })
			if ctx.Err() != nil {
				t.Fatal("revoked device kept its loop alive")
			}
			if got := requests.Load(); (known && got != 0) || (!known && got != 1) {
				t.Fatalf("removal requests = %d", got)
			}
		})
	}
}

func TestR42ServeHomeStateBecomesQuietDuringRound(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, "")
	started, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
			http.Error(w, "relay retry fixture", http.StatusServiceUnavailable)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	home := machine(t, server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var reports []error
	go func() {
		defer close(done)
		RunHomeState(ctx, home, func(err error) { reports = append(reports, err) })
	}()
	defer func() { cancel(); <-done }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("home-state round did not start")
	}
	// Leave the request alive while removing its identity. The response must
	// end the loop silently even when the relay also reports a failure.
	mustNil(t, os.Remove(filepath.Join(home, identity.File)))
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("quiet home did not stop its in-flight round")
	}
	if len(reports) != 0 {
		t.Fatalf("quiet home reported %v", reports)
	}
}

func TestR41R42ServeHomeStateRetriesDistinctFailuresAndReopensIdentity(t *testing.T) {
	server := relay(t)
	t.Setenv(config.ProfileDirEnv, "")
	other := machine(t, server.URL)
	source, _, err := Open(other)
	mustNil(t, err)
	mustNil(t, config.WriteAPIKey(other, fakeKeyTwo))
	mustNil(t, source.SyncState(t.Context()))
	home := machine(t, server.URL)
	t.Setenv(IntervalVar, "10")
	var status atomic.Int64
	status.Store(http.StatusServiceUnavailable)
	var requests atomic.Int64
	next := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if code := int(status.Load()); code != 0 {
			http.Error(w, "relay retry fixture", code)
			return
		}
		next.ServeHTTP(w, r)
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var reports []string
	go func() {
		defer close(done)
		RunHomeState(ctx, home, func(err error) { reports = append(reports, err.Error()) })
	}()
	t.Cleanup(func() { cancel(); <-done })
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusServiceUnavailable} {
		before := requests.Load()
		status.Store(int64(code))
		waitServeState(t, func() bool { return requests.Load() >= before+3 })
	}
	_, err = identity.Adopt(home, source.Identity, true)
	mustNil(t, err)
	status.Store(0)
	waitServeState(t, func() bool { return config.PersistedAPIKey(home) == fakeKeyTwo })
	// Removing the joined identity while serve remains open ends its worker.
	mustNil(t, os.Remove(filepath.Join(home, identity.File)))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("R4.2: removing the identity did not stop the loop")
	}
	if len(reports) != 2 || reports[0] == reports[1] {
		t.Fatalf("expected each of two distinct failures once, got %v", reports)
	}
}

func waitServeState(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case <-deadline.C:
			t.Fatal("serving state did not arrive")
		case <-tick.C:
		}
	}
}

// Stability: a serve nobody asked to keep state in step behaves exactly as it
// always has. Only the value 1 opts in; unset, empty and anything else do not.
func TestServeKeepsStateOnlyWhenADevBoxAsks(t *testing.T) {
	t.Setenv(env.Legacy(ServeStateVar), "")
	for value, want := range map[string]bool{"": false, "0": false, "true": false, "on": false, " 1 ": true, "1": true} {
		t.Setenv(ServeStateVar, value)
		if got := ServeKeepsState(); got != want {
			t.Errorf("%s=%q keeps state = %v, want %v", ServeStateVar, value, got, want)
		}
	}
}
