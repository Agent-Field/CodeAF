package syncsetup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type recordedRequest struct {
	Method string
	Path   string
	Query  string
}

type recordingTransport struct {
	mu   sync.Mutex
	next http.RoundTripper
	seen []recordedRequest
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.seen = append(r.seen, recordedRequest{
		Method: req.Method,
		Path:   req.URL.Path,
		Query:  req.URL.RawQuery,
	})
	r.mu.Unlock()
	next := r.next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(req)
}

func (r *recordingTransport) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}

func (r *recordingTransport) snapshot() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.seen)
}

func (r *recordingTransport) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = nil
}

type fakePhoneServer struct {
	srv        *httptest.Server
	recorder   *recordingTransport
	socketRead chan error
}

func newFakePhoneServer(t *testing.T) *fakePhoneServer {
	t.Helper()
	f := &fakePhoneServer{
		recorder:   &recordingTransport{next: http.DefaultTransport},
		socketRead: make(chan error, 4),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/dir/watch" && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			go func() {
				defer c.CloseNow()
				for {
					_, data, err := c.Read(context.Background())
					if err != nil {
						select {
						case f.socketRead <- err:
						default:
						}
						return
					}
					if string(data) == "ping" {
						_ = c.Write(context.Background(), websocket.MessageText, []byte("pong"))
					}
				}
			}()
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/dir/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"now":1000,"devices":{},"cells":{}}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/clients" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"clients":[]}`))
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/queue") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func newSoloPhoneHome(t *testing.T, relayURL string) (string, *Sync, *recordingTransport) {
	t.Helper()
	celltest.Available(t)
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv(URLVar, "")
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	if err := SaveRelayURL(home, relayURL); err != nil {
		t.Fatal(err)
	}
	s, ok, err := Open(home)
	if err != nil || !ok {
		t.Fatalf("Open(%s) = %v, %v", home, ok, err)
	}
	rec := &recordingTransport{next: http.DefaultTransport}
	s.phoneBase = rec
	return home, s, rec
}

const (
	testPhoneID       = "dev_11111111111111111111111111111111"
	testSecondPhoneID = "dev_22222222222222222222222222222222"
	testOtherDeviceID = "dev_33333333333333333333333333333333"
)

// TestP13PhoneWireRecorderSoloWithApprovedPhone verifies contract P13: a solo
// computer with one approved phone lets ONLY the phone runner's directory and
// client-fabric requests through PhoneDirectory and PhoneHTTP, and refuses
// every other route with ErrQuiet before anything reaches the transport.
func TestP13PhoneWireRecorderSoloWithApprovedPhone(t *testing.T) {
	fake := newFakePhoneServer(t)
	home, s, rec := newSoloPhoneHome(t, fake.srv.URL)

	if err := NotePhoneApproved(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	if !identity.Solo(home) {
		t.Fatal("NotePhoneApproved ended solo")
	}
	if !PhonesApproved(home) || !PhoneMayTalk(home) {
		t.Fatalf("PhonesApproved=%v PhoneMayTalk=%v, want both true", PhonesApproved(home), PhoneMayTalk(home))
	}
	if MayTalk(home) || !Quiet(home) {
		t.Fatalf("MayTalk=%v Quiet=%v on solo+phone home, want false and true", MayTalk(home), Quiet(home))
	}

	ctx := context.Background()
	pdir := s.PhoneDirectory()
	phttp := s.PhoneHTTP()
	self := s.Device.ID()

	// 1. Drive PhoneDirectory().List and PutDevice(own id).
	if _, err := pdir.List(ctx); err != nil {
		t.Fatalf("PhoneDirectory.List: %v", err)
	}
	if err := pdir.PutDevice(ctx, self, directory.Device{V: 1}); err != nil {
		t.Fatalf("PhoneDirectory.PutDevice(self): %v", err)
	}

	// 2. Drive the serving watch dial via ServingWatcher.
	sw, ok := pdir.(directory.ServingWatcher)
	if !ok {
		t.Fatal("PhoneDirectory does not implement directory.ServingWatcher")
	}
	stream, err := sw.WatchServing(ctx)
	if err != nil {
		t.Fatalf("WatchServing: %v", err)
	}
	if err := stream.Ping(ctx); err != nil {
		stream.Close()
		t.Fatalf("WatchServing Ping: %v", err)
	}
	if frame, err := stream.Next(ctx); err != nil || !frame.Pong {
		stream.Close()
		t.Fatalf("WatchServing Next = %+v, %v; want pong", frame, err)
	}
	stream.Close()

	// 3. Drive raw requests on PhoneHTTP() for every allowed shape.
	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/dir/list"},
		{http.MethodGet, "/v1/dir/watch?" + clientproto.ServingParam + "=1"},
		{http.MethodPut, "/v1/dir/devices/" + self},
		{http.MethodGet, "/v1/clients"},
		{http.MethodGet, "/v1/clients/" + testPhoneID + "/queue"},
		{http.MethodPost, "/v1/clients/" + testPhoneID + "/queue/1/claim"},
		{http.MethodDelete, "/v1/clients/" + testPhoneID + "/queue/1"},
		{http.MethodPut, "/v1/clients/" + testPhoneID + "/slots/card." + self},
		{http.MethodGet, "/v1/clients/" + testPhoneID + "/slots/card." + self},
		// Removing a phone this computer approved is the runner's own traffic.
		{http.MethodPost, "/v1/dir/devices/" + testPhoneID + "/revoke"},
	}
	for _, tc := range allowed {
		req, err := http.NewRequestWithContext(ctx, tc.method, fake.srv.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := phttp.Do(req)
		if err != nil {
			t.Fatalf("allowed request %s %s failed: %v", tc.method, tc.path, err)
		}
		_ = resp.Body.Close()
	}

	allowedCount := rec.count()
	wantAllowed := 3 + len(allowed)
	if allowedCount != wantAllowed {
		t.Fatalf("recorder saw %d allowed requests, want %d: %+v", allowedCount, wantAllowed, rec.snapshot())
	}

	// 4. Attempt everything MayTalk gates and every disallowed client/directory route.
	disallowed := []struct {
		name   string
		method string
		path   string
	}{
		{"cluster status", http.MethodGet, "/v1/cluster/status"},
		{"personal cluster admit", http.MethodPost, "/v1/dir/personal/admit"},
		{"raft exchange", http.MethodPost, "/v1/raft/exchange"},
		{"raft socket", http.MethodGet, "/v1/raft/socket"},
		{"blobstore has", http.MethodPost, "/v1/store/has"},
		{"blobstore put frame", http.MethodPut, "/v1/store/frames"},
		{"blobstore get object", http.MethodGet, "/v1/store/objects/1111111111111111111111111111111111111111111111111111111111111111"},
		{"blobstore stats", http.MethodGet, "/v1/store/stats"},
		{"put cell", http.MethodPut, "/v1/dir/cells/01J0000000000000000000000A"},
		{"create cell", http.MethodPost, "/v1/dir/cells/01J0000000000000000000000A"},
		{"publish cell", http.MethodPost, "/v1/dir/cells/01J0000000000000000000000A/publish"},
		{"lease acquire", http.MethodPost, "/v1/dir/cells/01J0000000000000000000000A/acquire"},
		{"dir vault", http.MethodPost, "/v1/dir/vault"},
		{"put other device", http.MethodPut, "/v1/dir/devices/" + testOtherDeviceID},
		{"revoke a device that is not a recorded phone", http.MethodPost, "/v1/dir/devices/" + testOtherDeviceID + "/revoke"},
		{"revoke with a nested path", http.MethodPost, "/v1/dir/devices/" + testPhoneID + "/extra/revoke"},
		{"client enroll", http.MethodPut, "/v1/clients/" + testPhoneID + "/enroll"},
		{"client register", http.MethodPut, "/v1/clients/" + testPhoneID},
		{"client computers", http.MethodGet, "/v1/clients/" + testPhoneID + "/computers"},
		{"watch without serving", http.MethodGet, "/v1/dir/watch"},
		{"watch with events only", http.MethodGet, "/v1/dir/watch?events=1&beat=10"},
		{"watch with serving=0", http.MethodGet, "/v1/dir/watch?" + clientproto.ServingParam + "=0"},
		{"post queue", http.MethodPost, "/v1/clients/" + testPhoneID + "/queue"},
		{"delete slot", http.MethodDelete, "/v1/clients/" + testPhoneID + "/slots/card." + self},
	}
	for _, tc := range disallowed {
		req, err := http.NewRequestWithContext(ctx, tc.method, fake.srv.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := phttp.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if !errors.Is(err, ErrQuiet) {
			t.Fatalf("%s (%s %s) err = %v, want ErrQuiet", tc.name, tc.method, tc.path, err)
		}
	}

	// Also verify disallowed PhoneDirectory methods return ErrQuiet without reaching the transport.
	if err := pdir.PutDevice(ctx, testOtherDeviceID, directory.Device{V: 1}); !errors.Is(err, ErrQuiet) {
		t.Fatalf("PhoneDirectory.PutDevice(other) = %v, want ErrQuiet", err)
	}
	if err := pdir.Revoke(ctx, testOtherDeviceID); !errors.Is(err, ErrQuiet) {
		t.Fatalf("PhoneDirectory.Revoke(not a phone) = %v, want ErrQuiet", err)
	}
	if _, err := pdir.Acquire(ctx, "01J0000000000000000000000A", directory.AcquireOpts{}); !errors.Is(err, ErrQuiet) {
		t.Fatalf("PhoneDirectory.Acquire = %v, want ErrQuiet", err)
	}
	if err := pdir.SetVault(ctx, "", "next"); !errors.Is(err, ErrQuiet) {
		t.Fatalf("PhoneDirectory.SetVault = %v, want ErrQuiet", err)
	}
	if w, ok := pdir.(directory.Watcher); ok {
		if s, err := w.Watch(ctx); !errors.Is(err, ErrQuiet) {
			if s != nil {
				s.Close()
			}
			t.Fatalf("PhoneDirectory.Watch (without serving=1) = %v, want ErrQuiet", err)
		}
	}

	if rec.count() != allowedCount {
		t.Fatalf("recorder count grew from %d to %d after disallowed attempts: %+v", allowedCount, rec.count(), rec.snapshot()[allowedCount:])
	}
}

// TestP13PhoneSoloWithoutPhoneSyncOffAndNonSolo verifies that a solo computer
// with no approved phone sends nothing, sync off sends nothing even with an
// approved phone, and a non-solo computer's phone traffic passes.
func TestP13PhoneSoloWithoutPhoneSyncOffAndNonSolo(t *testing.T) {
	fake := newFakePhoneServer(t)
	home, s, rec := newSoloPhoneHome(t, fake.srv.URL)
	ctx := context.Background()

	// 1. Solo computer with NO approved phone sends nothing at all.
	if PhonesApproved(home) || PhoneMayTalk(home) {
		t.Fatalf("fresh solo home has PhonesApproved=%v PhoneMayTalk=%v", PhonesApproved(home), PhoneMayTalk(home))
	}
	if _, err := s.PhoneDirectory().List(ctx); !errors.Is(err, ErrQuiet) {
		t.Fatalf("solo without phone List = %v, want ErrQuiet", err)
	}
	if sw, ok := s.PhoneDirectory().(directory.ServingWatcher); ok {
		if stream, err := sw.WatchServing(ctx); !errors.Is(err, ErrQuiet) {
			if stream != nil {
				stream.Close()
			}
			t.Fatalf("solo without phone WatchServing = %v, want ErrQuiet", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fake.srv.URL+"/v1/clients", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PhoneHTTP().Do(req); !errors.Is(err, ErrQuiet) {
		t.Fatalf("solo without phone GET /v1/clients = %v, want ErrQuiet", err)
	}
	if rec.count() != 0 {
		t.Fatalf("solo computer with no phone sent %d requests: %+v", rec.count(), rec.snapshot())
	}

	// 2. Sync off sends nothing even when a phone is approved.
	if err := NotePhoneApproved(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	t.Setenv(URLVar, "off")
	if PhoneMayTalk(home) {
		t.Fatal("PhoneMayTalk is true with sync off")
	}
	if _, err := s.PhoneDirectory().List(ctx); !errors.Is(err, ErrQuiet) {
		t.Fatalf("sync-off List = %v, want ErrQuiet", err)
	}
	if _, err := s.PhoneHTTP().Do(req); !errors.Is(err, ErrQuiet) {
		t.Fatalf("sync-off GET /v1/clients = %v, want ErrQuiet", err)
	}
	if rec.count() != 0 {
		t.Fatalf("sync-off computer sent %d requests", rec.count())
	}

	// 3. A non-solo computer's phone traffic passes even with no phones in phones.json.
	t.Setenv(URLVar, "")
	if err := ForgetPhone(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	if err := identity.EndSolo(home); err != nil {
		t.Fatal(err)
	}
	if !MayTalk(home) || !PhoneMayTalk(home) {
		t.Fatalf("non-solo home has MayTalk=%v PhoneMayTalk=%v", MayTalk(home), PhoneMayTalk(home))
	}
	if _, err := s.PhoneDirectory().List(ctx); err != nil {
		t.Fatalf("non-solo PhoneDirectory.List: %v", err)
	}
	resp, err := s.PhoneHTTP().Do(req)
	if err != nil {
		t.Fatalf("non-solo GET /v1/clients: %v", err)
	}
	_ = resp.Body.Close()
	if rec.count() != 2 {
		t.Fatalf("non-solo computer sent %d requests, want 2", rec.count())
	}
}

// TestP13PhoneForgetLastClosesServingSocketAndSilencesSolo verifies that
// forgetting the last phone returns a solo computer to quiet and closes an open
// serving watch socket on its next read or ping.
func TestP13PhoneForgetLastClosesServingSocketAndSilencesSolo(t *testing.T) {
	fake := newFakePhoneServer(t)
	home, s, rec := newSoloPhoneHome(t, fake.srv.URL)
	ctx := context.Background()

	if err := NotePhoneApproved(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	stream, err := s.PhoneDirectory().(directory.ServingWatcher).WatchServing(ctx)
	if err != nil {
		t.Fatalf("WatchServing: %v", err)
	}
	defer stream.Close()

	if err := stream.Ping(ctx); err != nil {
		t.Fatalf("Ping while approved: %v", err)
	}
	if frame, err := stream.Next(ctx); err != nil || !frame.Pong {
		t.Fatalf("Next while approved = %+v, %v; want pong", frame, err)
	}

	// Revoke the last phone on the solo computer.
	if err := ForgetPhone(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	if PhonesApproved(home) || PhoneMayTalk(home) {
		t.Fatalf("after ForgetPhone: PhonesApproved=%v PhoneMayTalk=%v", PhonesApproved(home), PhoneMayTalk(home))
	}

	// The open serving socket is closed at its next read and answers ErrQuiet.
	if _, err := stream.Next(ctx); !errors.Is(err, ErrQuiet) {
		t.Fatalf("Next after ForgetPhone = %v, want ErrQuiet", err)
	}
	if err := stream.Ping(ctx); !errors.Is(err, ErrQuiet) {
		t.Fatalf("Ping after ForgetPhone = %v, want ErrQuiet", err)
	}

	// Further requests send nothing.
	rec.reset()
	if _, err := s.PhoneDirectory().List(ctx); !errors.Is(err, ErrQuiet) {
		t.Fatalf("List after ForgetPhone = %v, want ErrQuiet", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fake.srv.URL+"/v1/clients", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PhoneHTTP().Do(req); !errors.Is(err, ErrQuiet) {
		t.Fatalf("PhoneHTTP after ForgetPhone = %v, want ErrQuiet", err)
	}
	if rec.count() != 0 {
		t.Fatalf("solo computer sent %d requests after ForgetPhone", rec.count())
	}
}

// TestP13PhoneMarkerPersistsDedupesAndNotifiesOnChange verifies that
// phones.json survives a restart (new Sync over the same home),
// NotePhoneApproved twice is one entry, and OnPhonesChange hears each
// transition once and ignores no-ops.
func TestP13PhoneMarkerPersistsDedupesAndNotifiesOnChange(t *testing.T) {
	fake := newFakePhoneServer(t)
	home, _, _ := newSoloPhoneHome(t, fake.srv.URL)

	var changes []string
	stop := OnPhonesChange(func(h string) {
		changes = append(changes, h)
	})
	defer stop()

	// Forgetting an absent phone on a fresh home is no error and notifies nobody.
	if err := ForgetPhone(home, testPhoneID); err != nil {
		t.Fatalf("ForgetPhone absent: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes after absent ForgetPhone = %v, want none", changes)
	}

	// First phone approval transitions PhonesApproved false -> true and notifies once.
	if err := NotePhoneApproved(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0] != home {
		t.Fatalf("changes after first NotePhoneApproved = %v, want [%s]", changes, home)
	}

	// NotePhoneApproved twice with the same phone keeps one entry and does not notify.
	if err := NotePhoneApproved(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes after duplicate NotePhoneApproved = %v, want 1", changes)
	}
	raw, err := os.ReadFile(phonesPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != testPhoneID {
		t.Fatalf("phones.json = %v, want [%s]", ids, testPhoneID)
	}
	info, err := os.Stat(phonesPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("phones.json mode = %#o, want 0600", perm)
	}

	// Approving a second phone keeps PhonesApproved true (no transition notification).
	if err := NotePhoneApproved(home, testSecondPhoneID); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes after second phone = %v, want 1", changes)
	}

	// Survives a restart: opening a new Sync over the same home still allows phone traffic.
	restarted, ok, err := Open(home)
	if err != nil || !ok {
		t.Fatalf("Open after restart = %v, %v", ok, err)
	}
	rec2 := &recordingTransport{next: http.DefaultTransport}
	restarted.phoneBase = rec2

	if !PhonesApproved(home) || !PhoneMayTalk(home) || MayTalk(home) || !Quiet(home) {
		t.Fatalf("after restart: PhonesApproved=%v PhoneMayTalk=%v MayTalk=%v Quiet=%v",
			PhonesApproved(home), PhoneMayTalk(home), MayTalk(home), Quiet(home))
	}
	if _, err := restarted.PhoneDirectory().List(context.Background()); err != nil {
		t.Fatalf("restarted PhoneDirectory.List: %v", err)
	}
	if rec2.count() != 1 {
		t.Fatalf("restarted recorder saw %d requests, want 1", rec2.count())
	}

	// Forgetting an unknown id is a no-op.
	if err := ForgetPhone(home, testOtherDeviceID); err != nil {
		t.Fatalf("ForgetPhone unknown: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes after unknown ForgetPhone = %v, want 1", changes)
	}

	// Forgetting the first phone leaves the second phone approved (no transition).
	if err := ForgetPhone(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	if !PhonesApproved(home) || len(changes) != 1 {
		t.Fatalf("after forgetting 1 of 2 phones: PhonesApproved=%v changes=%v", PhonesApproved(home), changes)
	}

	// Forgetting the last phone transitions PhonesApproved true -> false and notifies once.
	if err := ForgetPhone(home, testSecondPhoneID); err != nil {
		t.Fatal(err)
	}
	if PhonesApproved(home) || PhoneMayTalk(home) {
		t.Fatalf("after forgetting last phone: PhonesApproved=%v PhoneMayTalk=%v", PhonesApproved(home), PhoneMayTalk(home))
	}
	if len(changes) != 2 || changes[1] != home {
		t.Fatalf("changes after forgetting last phone = %v, want 2 notifications", changes)
	}

	// Forgetting the same phone again is a no-op and notifies nobody.
	if err := ForgetPhone(home, testSecondPhoneID); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes after repeat ForgetPhone = %v, want 2", changes)
	}
}

// TestP13PhoneGateLeavesMayTalkAndQuietUnchanged pins that the narrower phone
// gate never widens MayTalk or Quiet on a solo home with an approved phone.
func TestP13PhoneGateLeavesMayTalkAndQuietUnchanged(t *testing.T) {
	fake := newFakePhoneServer(t)
	home, s, _ := newSoloPhoneHome(t, fake.srv.URL)
	if err := NotePhoneApproved(home, testPhoneID); err != nil {
		t.Fatal(err)
	}
	if !PhoneMayTalk(home) {
		t.Fatal("PhoneMayTalk must be true for solo home with approved phone")
	}
	if MayTalk(home) {
		t.Fatal("MayTalk became true on a solo home when only a phone was approved")
	}
	if !Quiet(home) {
		t.Fatal("Quiet became false on a solo home when only a phone was approved")
	}
	ctx := context.Background()
	if _, err := s.Dir.Cell(ctx, "01J0000000000000000000000A"); !errors.Is(err, ErrQuiet) {
		t.Fatalf("Sync.Dir.Cell on solo+phone home = %v, want ErrQuiet", err)
	}
	if rows, err := s.Rows(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("Sync.Rows on solo+phone home = %v, %v; want empty, nil", rows, err)
	}
}

// TestP13ARosterWithOnlyAPhoneIsNotAnotherComputer verifies that a device list that
// holds this computer and a phone does not tell the home that another computer is
// paired, while a second computer does (contract P13).
func TestP13ARosterWithOnlyAPhoneIsNotAnotherComputer(t *testing.T) {
	fake := newFakePhoneServer(t)
	home, s, _ := newSoloPhoneHome(t, fake.srv.URL)
	binding := deviceBindingFor(s.Relay, s.Identity, s.Device)
	self := s.Device.ID()

	rememberOthers(home, binding, map[string]directory.Device{
		self:        {},
		testPhoneID: {Kind: clientproto.ClientLinkClass},
	})
	if s.OthersPaired() {
		t.Fatal("a roster holding this computer and a phone counted the phone as another computer")
	}
	rememberOthers(home, binding, map[string]directory.Device{
		self:              {},
		testPhoneID:       {Kind: clientproto.ClientLinkClass},
		testOtherDeviceID: {},
	})
	if !s.OthersPaired() {
		t.Fatal("a roster holding a second computer did not count as another computer")
	}
}
