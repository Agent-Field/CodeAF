package syncsetup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

const head = "1111111111111111111111111111111111111111111111111111111111111111"

// relay is the two real handlers behind the real request verification, in
// process: one directory and one store per identity, as the relay program does.
func relay(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	dirs, stores := map[string]directory.Directory{}, map[string]blobstore.Store{}
	openDir := func(id string) (directory.Directory, error) {
		mu.Lock()
		defer mu.Unlock()
		if dirs[id] == nil {
			dirs[id] = directory.NewMemory(time.Now)
		}
		return dirs[id], nil
	}
	openStore := func(id string) (blobstore.Store, error) {
		mu.Lock()
		defer mu.Unlock()
		if stores[id] == nil {
			stores[id] = blobstore.NewMemory()
		}
		return stores[id], nil
	}
	auth := reqsign.AuthenticateAt(time.Now)
	mux := http.NewServeMux()
	mux.Handle("/v1/dir/", directory.Handler(auth, openDir))
	mux.Handle("/v1/store/", blobstore.Handler(auth, openStore))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// machine is a home with an identity, pointed at url.
func machine(t *testing.T, url string) string {
	t.Helper()
	home := t.TempDir()
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv(URLVar, url)
	t.Setenv(IntervalVar, "")
	return home
}

func TestOpenOffWithoutURL(t *testing.T) {
	t.Setenv(URLVar, "")
	home := filepath.Join(t.TempDir(), "home")
	s, ok, err := Open(home)
	if s != nil || ok || err != nil {
		t.Fatalf("Open = %v, %v, %v; want nothing", s, ok, err)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open with no relay touched the home: %v", err)
	}
}

func TestSyncOffWithoutIdentity(t *testing.T) {
	t.Setenv(URLVar, "http://relay.example:8787")
	home := t.TempDir()
	_, ok, err := Open(home)
	if ok || !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("Open = ok %v, err %v; want ErrNoIdentity", ok, err)
	}
	if _, statErr := os.Stat(filepath.Join(home, identity.File)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("Open made an identity nobody else knows")
	}
}

func TestOpenRefusesBadSettings(t *testing.T) {
	for name, set := range map[string]map[string]string{
		"no scheme":        {URLVar: "relay.example:8787"},
		"wrong scheme":     {URLVar: "ftp://relay.example"},
		"no host":          {URLVar: "http://"},
		"interval text":    {URLVar: "http://r:1", IntervalVar: "soon"},
		"interval is zero": {URLVar: "http://r:1", IntervalVar: "0"},
	} {
		t.Run(name, func(t *testing.T) {
			home := machine(t, "")
			for k, v := range set {
				t.Setenv(k, v)
			}
			_, ok, err := Open(home)
			if ok || err == nil || strings.Contains(err.Error(), "\n") {
				t.Fatalf("Open = ok %v, err %v; want one sentence", ok, err)
			}
		})
	}
}

func TestOpenReadsInterval(t *testing.T) {
	home := machine(t, "http://r:1")
	if s, _, _ := Open(home); s.Interval != DefaultInterval {
		t.Fatalf("default interval = %v", s.Interval)
	}
	t.Setenv(IntervalVar, "60000")
	if s, _, _ := Open(home); s.Interval != time.Minute {
		t.Fatalf("interval = %v", s.Interval)
	}
}

// The ledger name has one source, cellstore.LedgerName, fed the one normalized
// relay address: two spellings of one relay share a ledger, two relays do not.
func TestLedgerNameIsPerRelayAndIdentity(t *testing.T) {
	home := machine(t, "http://r:1/")
	a, _, _ := Open(home)
	t.Setenv(URLVar, "http://r:1")
	b, _, _ := Open(home)
	t.Setenv(URLVar, "http://other:1")
	c, _, _ := Open(home)
	if a.Ledger != b.Ledger || a.Ledger == c.Ledger {
		t.Fatalf("ledger names %q %q %q are not per relay", a.Ledger, b.Ledger, c.Ledger)
	}
	if want := cellstore.LedgerName(a.Relay, a.Identity.ID()); a.Ledger != want || a.Relay != "http://r:1" {
		t.Fatalf("ledger %q for relay %q, want %q", a.Ledger, a.Relay, want)
	}
}

// TestOpenBuildsBoundClients is the whole seam: the clients Open builds sign as
// this device, the relay verifies them, and what the counters say matches what
// the relay counted.
func TestOpenBuildsBoundClients(t *testing.T) {
	ctx := context.Background()
	srv := relay(t)
	home := machine(t, srv.URL)
	s, ok, err := Open(home)
	if err != nil || !ok {
		t.Fatalf("Open = %v, %v", ok, err)
	}

	key := directory.MetadataKey(s.Identity.CellKey())
	title, err := directory.SealTitle(key, "Port the picker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dir.Create(ctx, "01J0000000000000000000000A", directory.CellInit{Head: head, Class: "chat", Title: title}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	rows, err := s.Rows(ctx)
	if err != nil || len(rows) != 1 || rows[0].Title != "Port the picker" {
		t.Fatalf("Rows = %+v, %v", rows, err)
	}

	rid := strings.Repeat("ab", 32)
	frame, err := blobstore.Encode(s.Identity.CellKeyID(), []blobstore.Object{{RID: rid, Bytes: []byte("AGEO\x01sealed")}})
	if err != nil {
		t.Fatal(err)
	}
	sc := s.scope("probe")
	if _, err := sc.Store.PutFrame(ctx, frame); err != nil {
		t.Fatalf("PutFrame: %v", err)
	}
	if have, err := sc.Store.Has(ctx, []string{rid}); err != nil || !have[0] {
		t.Fatalf("Has = %v, %v", have, err)
	}
	if _, err := sc.Store.Get(ctx, rid); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := sc.Store.Get(ctx, strings.Repeat("cd", 32)); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("Get of a missing object = %v", err)
	}

	theirs, err := blobstore.NewHTTP(srv.URL, reqsign.SignFor(deviceSigner{s.Identity, s.Device}, time.Now), nil).Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sc.Settle()
	lines, err := cellstats.Read(home, "probe")
	if err != nil {
		t.Fatal(err)
	}
	l := cellstats.Total(lines)
	got := blobstore.Counts{Puts: l.Puts, Gets: l.Gets, Has: l.Has, BytesIn: l.BytesUp, BytesOut: l.BytesDown}
	if got != theirs {
		t.Fatalf("scope stats %+v, relay says %+v", got, theirs)
	}
}

// TestAnotherIdentityIsNotServed pins that the signing is real: a relay only
// admits requests whose cert verifies, so a client with a forged identity key
// is refused rather than answered.
func TestAnotherIdentityIsNotServed(t *testing.T) {
	srv := relay(t)
	home := machine(t, srv.URL)
	s, _, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	other, err := identity.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	forged := deviceSigner{other, s.Device}
	c := directory.NewHTTP(srv.URL, reqsign.SignFor(forged, time.Now), http.DefaultClient)
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("a device cert from another identity was accepted")
	}
}

func TestOpenFirstMakesTheFirstIdentity(t *testing.T) {
	t.Setenv(URLVar, "http://relay.example:8787")
	home := t.TempDir()
	s, ok, err := OpenFirst(home)
	if s == nil || !ok || err != nil {
		t.Fatalf("OpenFirst = %v, %v, %v; want a sync", s, ok, err)
	}
	if _, err := identity.Load(home); err != nil {
		t.Fatalf("OpenFirst left no identity: %v", err)
	}
}

func TestOpenFirstWithNoRelayTouchesNothing(t *testing.T) {
	t.Setenv(URLVar, "")
	home := filepath.Join(t.TempDir(), "home")
	if _, ok, err := OpenFirst(home); ok || err != nil {
		t.Fatalf("OpenFirst = ok %v, err %v; want nothing", ok, err)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenFirst with no relay touched the home: %v", err)
	}
}

func TestFirstLaunchIsQuietUntilWoken(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	t.Setenv(URLVar, srv.URL)
	home := t.TempDir()
	s, ok, err := OpenFirst(home)
	if !ok || err != nil {
		t.Fatalf("OpenFirst = %v, %v", ok, err)
	}
	if !Quiet(home) {
		t.Fatal("a machine that just made its identity must be quiet")
	}
	ctx := context.Background()
	if n, err := s.FleetSize(ctx); n != 0 || err != nil {
		t.Fatalf("FleetSize = %d, %v; want 0, nil", n, err)
	}
	if _, err := s.Dir.Cell(ctx, "x"); err == nil || !strings.Contains(err.Error(), ErrQuiet.Error()) {
		t.Fatalf("a direct call = %v; want the quiet refusal", err)
	}
	if hits != 0 {
		t.Fatalf("a quiet machine sent %d requests", hits)
	}
	s.Engage()
	if Quiet(home) {
		t.Fatal("Engage left the machine quiet")
	}
	if _, err := s.FleetSize(ctx); hits == 0 {
		t.Fatalf("an engaged machine sent nothing (err %v)", err)
	}
}

func TestExistingIdentityIsNotQuiet(t *testing.T) {
	t.Setenv(URLVar, "http://relay.example:8787")
	home := t.TempDir()
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenFirst(home); err != nil || Quiet(home) {
		t.Fatalf("OpenFirst on an existing identity: err %v, quiet %v", err, Quiet(home))
	}
}

// The wire, the reads and the watch socket all answer to MayTalk alone.
func TestWireAndReadsFollowMayTalk(t *testing.T) {
	cases := []struct {
		name  string
		setup func(home string)
		talk  bool
	}{
		{"solo", func(h string) { _, _ = identity.EnsureSolo(h) }, false},
		{"paired", func(h string) { _, _ = identity.Ensure(h) }, true},
		{"engaged", func(h string) { _, _ = identity.EnsureSolo(h); _ = identity.EndSolo(h) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits++; http.Error(w, "x", 500) }))
			defer srv.Close()
			t.Setenv(URLVar, srv.URL)
			home := t.TempDir()
			tc.setup(home)
			if MayTalk(home) != tc.talk || Quiet(home) == tc.talk {
				t.Fatalf("MayTalk = %v, want %v", MayTalk(home), tc.talk)
			}
			s, ok, err := Open(home)
			if !ok || err != nil {
				t.Fatalf("Open = %v, %v", ok, err)
			}
			_, _ = s.FleetSize(context.Background())
			_, _ = s.Dir.(directory.Watcher).Watch(context.Background())
			if talked := hits > 0; talked != tc.talk {
				t.Fatalf("requests = %d, want talk %v", hits, tc.talk)
			}
		})
	}
	// no identity: nothing to open at all
	t.Setenv(URLVar, "http://relay.example:8787")
	if _, _, err := Open(t.TempDir()); !errors.Is(err, ErrNoIdentity) || MayTalk(t.TempDir()) {
		t.Fatalf("no identity: err %v", err)
	}
}
