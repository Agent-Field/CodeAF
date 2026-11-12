package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

func startupFinished(l *liveDrive) {
	if ready := l.pending(); ready != nil {
		<-ready
	}
}

// Startup goes through the signed directory wire, then catches up turns sealed
// locally during the refusal. Advancing the injected clock is the only retry trigger.
func TestLiveDriveRefusedStartupRetriesAndUploads(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
		after  int
		wait   time.Duration
		line   string
	}{
		{"relay_storage_full", 507, 0, cellsync.MaxHalt, chatlist.RelayStorageFull},
		{"too_many_identities", 429, 120, 2 * time.Minute, chatlist.TooManyNewIn(2 * time.Minute)},
		{"too_many_identities", 429, 10800, cellsync.MaxHalt, chatlist.TooManyNewIn(3 * time.Hour)},
	} {
		t.Run(tc.code+strconv.Itoa(tc.after), func(t *testing.T) {
			_, c := spokenDoorChatWithPairing(t, true)
			if _, err := identity.Ensure(home.Dir()); err != nil {
				t.Fatal(err)
			}
			svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
			t.Cleanup(func() { _ = svc.Close() })
			var calls, startups, frames atomic.Int32
			published := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/v1/dir/devices/") && startups.Add(1) <= 2 {
					if tc.after > 0 {
						w.Header().Set("Retry-After", strconv.Itoa(tc.after))
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"err":"` + tc.code + `"}`))
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/v1/store/frames" {
					frames.Add(1)
				}
				svc.Handler.ServeHTTP(w, r)
				if r.Method == http.MethodPost && (r.URL.Path == "/v1/dir/cells/"+c.ID || strings.HasSuffix(r.URL.Path, "/publish")) {
					select {
					case published <- struct{}{}:
					default:
					}
				}
			}))
			t.Cleanup(srv.Close)
			t.Setenv(syncsetup.URLVar, srv.URL)
			t.Setenv(syncsetup.IntervalVar, "50")
			clock := directorytest.NewFakeClock()
			var said []string
			engine := cellstore.EngineFor("")
			l := &liveDrive{
				start: func() (*syncsetup.Drive, error) {
					return startDrive(c, engine, func(err error) {
						if err != nil {
							t.Errorf("startup report: %v", err)
						}
					})
				},
				now: clock.Now, after: clock.After,
				say: func(line string) { said = append(said, line) },
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), driveCloseWithin)
				defer cancel()
				if err := l.Close(ctx); err != nil {
					t.Errorf("close: %v", err)
				}
			})
			l.try()
			startupFinished(l)
			if len(said) != 1 || said[0] != tc.line {
				t.Fatalf("startup said %q, want %q once", said, tc.line)
			}
			// One startup attempt costs what writing this device's own record
			// costs: a read of the directory, so waiting messages and an
			// unchanged record survive (PutOwnDevice), and the refused write.
			// THE LAW IS NO RELAY CALL BEYOND ONE ATTEMPT PER WAIT, so measure
			// one attempt here rather than spell its request count.
			perStartup := calls.Load()
			if perStartup < 1 || perStartup > 2 {
				t.Fatalf("one refused startup made %d relay calls, want the device read and write at most", perStartup)
			}
			var sealed cellstore.Sealed
			for attempt := int32(1); attempt <= 2; attempt++ {
				clock.Advance(tc.wait - time.Nanosecond)
				for n := 0; n < 3; n++ {
					var err error
					sealed, err = l.Store(engine).Seal(t.Context(), c, cellstore.TurnInfo{})
					if err != nil {
						t.Fatal(err)
					}
					l.Idle()
					if err := l.Gate(); err != nil {
						t.Fatal(err)
					}
				}
				if got := calls.Load(); got != attempt*perStartup {
					t.Fatalf("relay calls before wait = %d, want %d (%d per startup attempt)", got, attempt*perStartup, perStartup)
				}
				clock.Advance(time.Nanosecond)
				startupFinished(l)
				if len(said) != 1 {
					t.Fatalf("refusal repeated: %q", said)
				}
			}
			if l.started() == nil {
				t.Fatal("startup did not retry automatically")
			}
			select {
			case <-published:
			case <-time.After(10 * time.Second):
				spoken, known := session.ConversationIn((session.Place{Dir: c.Root}).Transcript(), sealed.Receipt.Transcript.End)
				t.Fatalf("pending turn did not upload after startup recovered: calls=%d frames=%d receipt=%+v spoken=%v known=%v", calls.Load(), frames.Load(), sealed.Receipt.Transcript, spoken, known)
			}
			s, ok, err := syncsetup.Open(home.Dir())
			if err != nil || !ok {
				t.Fatalf("Open = %v, %v", ok, err)
			}
			view, err := s.Dir.Cell(t.Context(), c.ID)
			if err != nil || view.Cell.Head != sealed.Turn.ID || frames.Load() == 0 {
				t.Fatalf("uploaded head = %q (%v), frames=%d; want %q", view.Cell.Head, err, frames.Load(), sealed.Turn.ID)
			}
		})
	}
}

func TestLiveDriveStartupRefusalWaitsAndStopsOnClose(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		wait time.Duration
	}{
		{"identity full", blobstore.Capped(blobstore.ErrFull, 5<<30), cellsync.MaxHalt},
		{"rate limited", wireauth.Wait(wireauth.ErrRateLimited, http.Header{"Retry-After": {"120"}}), 2 * time.Minute},
		{"rate limited capped", wireauth.Wait(wireauth.ErrRateLimited, http.Header{"Retry-After": {"7200"}}), cellsync.MaxHalt},
		{"too many without wait", wireauth.ErrTooManyIdentities, 2 * cellsync.DefaultInterval},
		{"revoked", wireauth.ErrRevoked, 0},
		{"gone", wireauth.ErrGone, 0},
		{"rotated", wireauth.ErrRotated, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			t.Setenv(syncsetup.URLVar, "http://127.0.0.1:1")
			if _, err := identity.Ensure(home.Dir()); err != nil {
				t.Fatal(err)
			}
			clock := directorytest.NewFakeClock()
			var attempts atomic.Int32
			var said []string
			l := &liveDrive{
				start: func() (*syncsetup.Drive, error) { attempts.Add(1); return nil, tc.err },
				now:   clock.Now, after: clock.After, say: func(s string) { said = append(said, s) },
			}
			l.try()
			startupFinished(l)
			if tc.wait > 0 {
				clock.Advance(tc.wait - time.Nanosecond)
				for n := 0; n < 10; n++ {
					l.try()
					startupFinished(l)
				}
				if attempts.Load() != 1 {
					t.Fatal("startup retried before its wait")
				}
				clock.Advance(time.Nanosecond)
				startupFinished(l)
				if attempts.Load() != 2 {
					t.Fatal("startup did not retry after its wait")
				}
			} else {
				clock.Advance(2 * cellsync.MaxHalt)
				l.try()
				startupFinished(l)
				if attempts.Load() != 1 {
					t.Fatal("person-only refusal retried")
				}
			}
			if err := l.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			before := attempts.Load()
			clock.Advance(2 * cellsync.MaxHalt)
			l.try()
			startupFinished(l)
			if attempts.Load() != before {
				t.Fatal("closed startup retried")
			}
			row, _ := cellsync.RefusalOf(tc.err)
			want := 1
			if row.Quiet > 0 && tc.wait < row.Quiet {
				want = 0
			}
			if len(said) != want || want == 1 && said[0] != row.Say(tc.err) {
				t.Fatalf("said %q, want %d notices for %q", said, want, row.Say(tc.err))
			}
		})
	}
}

func TestLiveDriveStartupRateLimitSaysOnceAfterQuiet(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(syncsetup.URLVar, "http://127.0.0.1:1")
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatal(err)
	}
	clock := directorytest.NewFakeClock()
	var said []string
	l := &liveDrive{
		start: func() (*syncsetup.Drive, error) {
			return nil, wireauth.Wait(wireauth.ErrRateLimited, http.Header{"Retry-After": {"120"}})
		},
		now: clock.Now, after: clock.After, say: func(s string) { said = append(said, s) },
	}
	t.Cleanup(func() { _ = l.Close(context.Background()) })
	l.try()
	startupFinished(l)
	clock.Advance(2 * time.Minute)
	startupFinished(l)
	if len(said) != 0 {
		t.Fatalf("rate limit spoke during its quiet period: %q", said)
	}
	clock.Advance(2 * time.Minute)
	startupFinished(l)
	clock.Advance(4 * time.Minute)
	startupFinished(l)
	if len(said) != 1 || said[0] != chatlist.SlowDown {
		t.Fatalf("said %q, want the slow-down sentence once", said)
	}
}
