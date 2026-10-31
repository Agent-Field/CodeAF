package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func unlistedRepairChat(t *testing.T, wrap func(http.Handler) http.Handler) (*syncsetup.Sync, cell.Cell, localChatInventory) {
	t.Helper()
	cfg, _ := doorChat(t)
	c, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(c.Root, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	cfg.Place = session.Place{Dir: c.Root, Workspace: work, Owned: true}
	cfg.Workspace, cfg.SessionFile = work, cfg.Place.Transcript()
	cfg.Model, cfg.APIKey, cfg.BaseURL = "stub/chat", "stub", "http://127.0.0.1:1"
	var reject atomic.Bool
	reject.Store(true)
	handler := relayserveForMove(t)
	if wrap != nil {
		handler = wrap(handler)
	}
	srv := publicationRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reject.Load() && r.Method == http.MethodPost && r.URL.Path == "/v1/dir/cells/"+c.ID {
			w.WriteHeader(503)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	publicationPair(t, srv.URL)
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		t.Fatalf("open sync: %v %v", ok, err)
	}
	a, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	events, err := a.SubmitBash(t.Context(), "! printf repair-fixture")
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			a.Close()
			t.Fatal(event.Text)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	e := cellstore.EngineFor(work)
	e.Workspace = work
	e.Identity = cellstore.Identity{Device: s.Device.Cert.Device}
	if _, err := e.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := cellstore.Await(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	d, err := s.Drive(t.Context(), e, c, syncsetup.DriveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = d.Close(ctx)
	if !s.PublicationExpected(c) {
		t.Fatal("fixture has no paired publication intent")
	}
	reject.Store(false)
	return s, c, localChatInventory{foldersByID: map[string][]string{c.ID: {c.Root}}, inUseByID: map[string]bool{}}
}

// SPEC-3 item 3: an archive during remote creation also puts away the new record.
func TestFixRound3DoctorArchivesChatPutAwayDuringRepair(t *testing.T) {
	for _, lostReply := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "lost-reply"}[lostReply], func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var loseReply atomic.Bool
			var repeats atomic.Int32
			loseReply.Store(lostReply)
			s, c, inv := unlistedRepairChat(t, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/dir/cells/") && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/v1/dir/cells/"), "/") {
						once.Do(func() { close(entered) })
						select {
						case <-release:
						case <-r.Context().Done():
							return
						}
						if loseReply.Swap(false) {
							// Discard the committed reply and replay the identical request before
							// returning. This forces ErrExists without depending on which of the
							// background publisher and Close made the first network attempt.
							raw, err := io.ReadAll(r.Body)
							if err != nil {
								t.Error(err)
								w.WriteHeader(503)
								return
							}
							r.Body = io.NopCloser(bytes.NewReader(raw))
							saved := httptest.NewRecorder()
							next.ServeHTTP(saved, r)
							if saved.Code != http.StatusOK && saved.Code != http.StatusCreated {
								t.Errorf("lost-reply fixture did not create: %d %s", saved.Code, saved.Body.String())
							}
							r.Body = io.NopCloser(bytes.NewReader(raw))
							repeated := httptest.NewRecorder()
							next.ServeHTTP(repeated, r)
							if repeated.Code != http.StatusConflict {
								t.Errorf("repeated create did not return exists: %d %s", repeated.Code, repeated.Body.String())
							}
							repeats.Add(1)
							for key, values := range repeated.Header() {
								w.Header()[key] = values
							}
							w.WriteHeader(repeated.Code)
							_, _ = w.Write(repeated.Body.Bytes())
							return
						}
					}
					next.ServeHTTP(w, r)
				})
			})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			type result struct{ issues, fixes []string }
			done := make(chan result, 1)
			go func() {
				issues, fixes := doctorUnlistedChats(s, inv, directory.Listing{}, true)
				done <- result{issues, fixes}
			}()
			select {
			case <-entered:
			case <-time.After(driveCloseWithin):
				t.Fatal("repair never created record")
			}
			if err := session.SetArchived(c.Root, true); err != nil {
				t.Fatal(err)
			}
			unblock()
			select {
			case got := <-done:
				if len(got.issues) != 0 || len(got.fixes) != 0 {
					t.Fatalf("archived repair: %+v", got)
				}
			case <-time.After(driveCloseWithin):
				t.Fatal("repair did not finish")
			}
			if lostReply && repeats.Load() != 1 {
				t.Fatal("lost-reply fixture never returned a repeated-create conflict")
			}
			view, err := s.Dir.Cell(t.Context(), c.ID)
			if err != nil || !view.Cell.Archived {
				t.Fatalf("remote chat revived: %+v %v", view, err)
			}
		})
	}
}

// SPEC-3 item 8: a fresh lookup finding a record must not print the missing sentence.
func TestFixRound8DoctorAlreadyPresentIsSilent(t *testing.T) {
	s, c, inv := unlistedRepairChat(t, nil)
	created, err := s.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: strings.Repeat("f", 64), Class: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	before := created.Cell
	issues, fixes := doctorUnlistedChats(s, inv, directory.Listing{}, true)
	if len(issues) != 0 || len(fixes) != 0 {
		t.Fatalf("already present was reported missing: %v %v", issues, fixes)
	}
	after, err := s.Dir.Cell(t.Context(), c.ID)
	if err != nil || after.Cell.Head != before.Head || after.Cell.Lease != before.Lease {
		t.Fatalf("existing record changed: %+v %v", after, err)
	}
}

// SPEC-3 item 7: an empty cleanup has no timer allocations, output or relay work.
func TestFixRound7OnceEmptyCleanupReturnsImmediately(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	var requests atomic.Int32
	srv := publicationRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	t.Setenv(syncsetup.URLVar, srv.URL)
	out, errout := capturePublicationOutput(t)
	b := &driveBook{}
	allocs := testing.AllocsPerRun(100, func() { b.closeAllWith(func(*liveDrive, error) { t.Fatal("empty cleanup reported a failure") }) })
	if allocs != 0 {
		t.Fatalf("empty cleanup created closing work: %g allocations", allocs)
	}
	stdout, _ := os.ReadFile(out.Name())
	stderr, _ := os.ReadFile(errout.Name())
	if len(stdout) != 0 || len(stderr) != 0 || requests.Load() != 0 {
		t.Fatalf("empty cleanup: stdout=%q stderr=%q requests=%d", stdout, stderr, requests.Load())
	}
}

// SPEC-3 item 7: a drive starting after entry is drained before --once returns.
func TestFixRound7OnceClosesDriveStartedAfterEntry(t *testing.T) {
	cfg, c := publicationChat(t)
	next := relayserveForMove(t)
	relay := publicationRelay(t, next)
	t.Setenv(syncsetup.URLVar, relay.URL)
	target, err := url.Parse(cfg.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	target.Path = ""
	target.RawPath = ""
	proxy := httputil.NewSingleHostReverseProxy(target)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	model := publicationRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	cfg.BaseURL = model.URL + "/api/v1"
	capturePublicationOutput(t)
	if syncsetup.MayTalk(home.Dir()) {
		t.Fatal("fixture paired at entry")
	}
	done := make(chan error, 1)
	go func() { done <- runChatV3Once(t.Context(), cfg, cfg.Workspace, "PROBE-ECHO late-pair", "", false) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("returned before model: %v", err)
	case <-time.After(driveCloseWithin):
		t.Fatal("no model call")
	}
	b := publicationPair(t, relay.URL)
	if err := identity.EndSolo(home.Dir()); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(driveCloseWithin):
		t.Fatal("late drive did not close")
	}
	if len(syncDrives.drive) != 0 {
		t.Fatal("late drive left for unrelated cleanup")
	}
	view, err := b.Dir.Cell(t.Context(), c.ID)
	if err != nil || view.Cell.Head == "" || view.Cell.Lease.Expires > view.Now {
		t.Fatalf("late drive not released: %+v %v", view, err)
	}
}

// SPEC-3 item 6: older repairs finish before a publication send can consume time.
func TestFixRound6DoctorRepairsOlderHoldsBeforePublication(t *testing.T) {
	var oldReleased, createdBeforeOld atomic.Bool
	var target string
	s, c, _ := unlistedRepairChat(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/v1/dir/cells/"+target && !oldReleased.Load() {
				createdBeforeOld.Store(true)
			}
			next.ServeHTTP(w, r)
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release") {
				oldReleased.Store(true)
			}
		})
	})
	target = c.ID
	oldID, err := cell.NewID(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dir.Create(t.Context(), oldID, directory.CellInit{Head: strings.Repeat("e", 64), Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	_, fixes := inspectDoctorSync(true)
	if !oldReleased.Load() {
		t.Fatalf("older repair did not release its hold: %v", fixes)
	}
	if createdBeforeOld.Load() {
		t.Fatal("publication repair ran before older repairs")
	}
	if !strings.Contains(strings.Join(fixes, "\n"), "fixed: sent chat "+c.ID) {
		t.Fatalf("publication repair did not run: %v", fixes)
	}
}

// SPEC-3 item 9: the real one-message door keeps marker failure off stdout and stderr.
func TestFixRound9OnceMarkerFailureIsSilent(t *testing.T) {
	cfg, c := publicationChat(t)
	next := relayserveForMove(t)
	relay := publicationRelay(t, next)
	b := publicationPair(t, relay.URL)
	parent := home.Join("v3", "sync", "publication")
	if err := os.MkdirAll(filepath.Dir(parent), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	out, errout := capturePublicationOutput(t)
	oldLog := log.Writer()
	log.SetOutput(errout)
	defer log.SetOutput(oldLog)
	if err := runChatV3Once(t.Context(), cfg, cfg.Workspace, "PROBE-ECHO marker-failure", "", false); err != nil {
		t.Fatal(err)
	}
	stdout, _ := os.ReadFile(out.Name())
	stderr, _ := os.ReadFile(errout.Name())
	if string(stdout) != "STUB-REPLY: PROBE-ECHO marker-failure\n" {
		t.Fatalf("reply=%q", stdout)
	}
	if len(stderr) != 0 {
		t.Fatalf("marker failure reached standard error: %s", stderr)
	}
	view, err := b.Dir.Cell(t.Context(), c.ID)
	if err != nil || view.Cell.Head == "" || view.Cell.Lease.Expires > view.Now {
		t.Fatalf("marker failure stopped publication: %+v %v", view, err)
	}
}

// publicationCreateRace installs unfamiliar history after the final lookup.
type publicationCreateRace struct {
	directory.Client
	dir       string
	archive   bool
	once      sync.Once
	injectErr error
}

func (r *publicationCreateRace) Create(ctx context.Context, id string, in directory.CellInit) (directory.CellView, error) {
	r.once.Do(func() {
		if r.archive {
			r.injectErr = session.SetArchived(r.dir, true)
			if r.injectErr != nil {
				return
			}
		}
		_, r.injectErr = r.Client.Create(ctx, id, directory.CellInit{Head: strings.Repeat("f", 64), Class: "chat"})
	})
	if r.injectErr != nil {
		return directory.CellView{}, r.injectErr
	}
	return r.Client.Create(ctx, id, in)
}

// SPEC-3 items 3/8: a create race is silent and an archive cannot touch unfamiliar history.
func TestFixRound8DoctorAlreadyPresentAtCreateIsSilent(t *testing.T) {
	for _, archive := range []bool{false, true} {
		t.Run(map[bool]string{false: "present", true: "archived-during-create"}[archive], func(t *testing.T) {
			s, c, inv := unlistedRepairChat(t, nil)
			s.Dir = &publicationCreateRace{Client: s.Dir, dir: c.Root, archive: archive}
			issues, fixes := doctorUnlistedChats(s, inv, directory.Listing{}, true)
			if len(issues) != 0 || len(fixes) != 0 {
				t.Fatalf("already present: %v %v", issues, fixes)
			}
			view, err := s.Dir.Cell(t.Context(), c.ID)
			if err != nil || view.Cell.Head != strings.Repeat("f", 64) || view.Cell.Archived || view.Cell.Lease.Device != s.Device.ID() {
				t.Fatalf("unfamiliar record changed: %+v %v", view, err)
			}
		})
	}
}
