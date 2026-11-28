package clientrun

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// TestEnrollmentVersionPinRefusesLowerAndLatchesConflict verifies that the
// runner ignores enrollments with bad signatures or device bindings, pins the
// highest checked enrollment ver per client in pins.json, keeps serving the
// pinned statement when the relay returns a lower ver or badly signed row, and
// latches a conflict on the same ver with different bytes so nothing is served
// for that client until removed and re-paired (contract P11).
func TestEnrollmentVersionPinRefusesLowerAndLatchesConflict(t *testing.T) {
	g := newRig(t)
	pubBytes, err := hex.DecodeString(g.client.dev.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	// Register the client device row in the directory.
	g.request(t, g.s, "PUT", "/v1/clients/"+g.client.dev.ID(), map[string]any{
		"cert":    g.client.Cert(),
		"methods": []string{"asks.answer"},
		"name":    "",
	}, nil, 204)

	otherHome := t.TempDir()
	otherID, err := identity.Ensure(otherHome)
	if err != nil {
		t.Fatal(err)
	}
	// An enrollment signed by the wrong identity key is ignored and logged.
	badSigEnroll := signEnrollment(otherID, clientproto.Enrollment{
		V:       1,
		Device:  g.client.dev.ID(),
		SignPub: pubBytes,
		BoxPub:  g.pub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"asks.list"},
		Ver:     1,
	})

	var servedEnroll clientproto.Enrollment
	intercept := func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && req.URL.Path == "/v1/clients" {
			raw, _ := json.Marshal(map[string]any{
				"clients": []map[string]any{
					{"device": g.client.dev.ID(), "enroll": servedEnroll},
				},
			})
			return &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(raw)),
			}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	}
	g.reopen(t, transport(intercept), noopLog)

	servedEnroll = badSigEnroll
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 0 {
		t.Fatalf("accepted enrollment signed by wrong identity: %+v", g.r.Clients())
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("card", g.s.dev.ID()), nil, nil, 404)

	// Serve a valid ver=2 enrollment with reads=["agents.list"].
	validV2 := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  g.client.dev.ID(),
		SignPub: pubBytes,
		BoxPub:  g.pub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"agents.list"},
		Ver:     2,
	})
	servedEnroll = validV2
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 1 || g.r.Clients()[0].Ver != 2 {
		t.Fatalf("clients after ver 2: %+v", g.r.Clients())
	}
	initialAgents := g.readBoxSlot(t, "agents.list", g.s.dev.ID(), 0)
	if initialAgents.Ver != 2 {
		t.Fatalf("initial agents.list slot ver = %d, want 2", initialAgents.Ver)
	}

	// Relay rolls back to ver=1 with a longer reads list ["agents.list", "asks.list"] and a stale box_pub.
	// The ver=2 phone must keep being served from its pinned ver=2 statement:
	// still listed in Clients() at Ver 2, and card and agents.list updates still written!
	stalePub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	olderV1 := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  g.client.dev.ID(),
		SignPub: pubBytes,
		BoxPub:  stalePub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"agents.list", "asks.list"},
		Ver:     1,
	})
	servedEnroll = olderV1
	g.e.agents = []appwire.Agent{{ID: chat, Title: "still-served-at-v2"}}
	g.clock.Advance(clientproto.WatchingFloor)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 1 || g.r.Clients()[0].Ver != 2 {
		t.Fatalf("phone disappeared or downgraded on ver=1 rollback row: %+v", g.r.Clients())
	}
	// asks.list must never be published from the rolled-back ver=1 enrollment.
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("asks.list", g.s.dev.ID()), nil, nil, 404)
	// agents.list must still be written using the pinned ver=2 statement and box key.
	afterRollbackAgents := g.readBoxSlot(t, "agents.list", g.s.dev.ID(), initialAgents.At)
	if afterRollbackAgents.Ver != 2 || !strings.Contains(string(afterRollbackAgents.Payload), "still-served-at-v2") {
		t.Fatalf("ver=2 phone was not served after ver=1 rollback row: ver=%d payload=%s", afterRollbackAgents.Ver, afterRollbackAgents.Payload)
	}

	// A badly signed row after ver=2 is pinned also changes nothing: the ver=2 phone keeps being served.
	servedEnroll = badSigEnroll
	g.e.agents = []appwire.Agent{{ID: chat, Title: "still-served-after-badsig"}}
	g.clock.Advance(clientproto.WatchingFloor)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 1 || g.r.Clients()[0].Ver != 2 {
		t.Fatalf("phone disappeared on bad-signature row: %+v", g.r.Clients())
	}
	afterBadSigAgents := g.readBoxSlot(t, "agents.list", g.s.dev.ID(), afterRollbackAgents.At)
	if afterBadSigAgents.Ver != 2 || !strings.Contains(string(afterBadSigAgents.Payload), "still-served-after-badsig") {
		t.Fatalf("ver=2 phone was not served after bad-signature row: ver=%d payload=%s", afterBadSigAgents.Ver, afterBadSigAgents.Payload)
	}

	// Relay serves ver=2 with different bytes (reads=["asks.list"]): same ver conflict latches and stops serving that client.
	conflictingV2 := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  g.client.dev.ID(),
		SignPub: pubBytes,
		BoxPub:  g.pub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"asks.list"},
		Ver:     2,
	})
	servedEnroll = conflictingV2
	g.e.agents = []appwire.Agent{{ID: chat, Title: "must-not-publish-after-conflict"}}
	g.clock.Advance(clientproto.WatchingFloor)
	g.clearRequests()
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 0 {
		t.Fatalf("conflicting same-ver enrollment was served: %+v", g.r.Clients())
	}
	for _, req := range g.recordedRequests() {
		if strings.HasPrefix(req, "PUT ") {
			t.Fatalf("conflicting client still received PUT request: %q", req)
		}
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("asks.list", g.s.dev.ID()), nil, nil, 404)

	// Even if the relay later serves ver=3 for that same client id, the conflict latch serves nothing until removed and re-paired.
	laterV3 := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  g.client.dev.ID(),
		SignPub: pubBytes,
		BoxPub:  g.pub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"asks.list"},
		Ver:     3,
	})
	servedEnroll = laterV3
	g.reopen(t, transport(intercept), noopLog)
	g.clearRequests()
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 0 {
		t.Fatalf("conflict latch did not persist across restart: %+v", g.r.Clients())
	}
	for _, req := range g.recordedRequests() {
		if strings.HasPrefix(req, "PUT ") {
			t.Fatalf("latched client received PUT after restart: %q", req)
		}
	}
}

// TestRevokeWritesTombstoneDeletesStateAndStopsPublishing verifies that when a
// phone is revoked (both via a revoked directory listing and via a watch-socket
// revoked event), the runner writes tombstones.json, deletes the phone's keys,
// receipts file entries, and outcomes, and makes zero relay PUTs afterward
// (contract V1).
func TestRevokeWritesTombstoneDeletesStateAndStopsPublishing(t *testing.T) {
	seedReceiptOnDisk := func(t *testing.T, g *rig, clientID string) {
		t.Helper()
		if err := g.r.Close(); err != nil {
			t.Fatal(err)
		}
		path := StatePath(g.state)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		doc["receipts"], _ = json.Marshal(map[string]Receipt{
			clientID + "/1": {
				Client: clientID,
				Number: 1,
				Agent:  chat,
				Method: "asks.answer",
				Status: "done",
			},
		})
		updated, _ := json.Marshal(doc)
		if err := os.WriteFile(path, updated, 0600); err != nil {
			t.Fatal(err)
		}
		g.reopen(t, nil, noopLog)
		if len(g.r.Receipts()) != 1 {
			t.Fatalf("seeded receipt not loaded: %+v", g.r.Receipts())
		}
	}

	assertReceiptsDeletedOnDisk := func(t *testing.T, g *rig, clientID string) {
		t.Helper()
		if len(g.r.Receipts()) != 0 {
			t.Fatalf("Receipts() not cleared after revoke: %+v", g.r.Receipts())
		}
		raw, err := os.ReadFile(StatePath(g.state))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), clientID) {
			t.Fatalf("runner.json still contains revoked client %s: %s", clientID, raw)
		}
	}

	t.Run("revoked_listing", func(t *testing.T) {
		g := newRig(t)
		g.enroll(t, []string{"asks.list"}, "asks.answer")
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		seedReceiptOnDisk(t, g, g.client.dev.ID())
		if err := g.r.RecordOutcome(g.client.dev.ID(), Outcome{Number: 1, Status: "done", Agent: chat}); err != nil {
			t.Fatal(err)
		}
		if len(g.r.Outcomes(g.client.dev.ID())) != 1 {
			t.Fatal("outcome not recorded")
		}

		// Revoke at the relay.
		if err := g.dir.Revoke(context.Background(), g.client.dev.ID()); err != nil {
			t.Fatal(err)
		}
		g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("asks.list", g.s.dev.ID()), nil, nil, 401)

		g.clearRequests()
		readsBefore := g.e.reads
		g.e.asks = []appwire.Ask{{ID: "after-revoke", Agent: chat}}
		g.clock.Advance(clientproto.WatchingFloor)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, req := range g.recordedRequests() {
			if strings.HasPrefix(req, "PUT ") {
				t.Fatalf("runner made PUT request after listing revoke: %q", req)
			}
		}
		if g.e.reads != readsBefore {
			t.Fatal("runner read engine snapshots for a revoked client")
		}
		if len(g.r.Clients()) != 0 {
			t.Fatalf("revoked client still in Clients(): %+v", g.r.Clients())
		}
		if len(g.r.Outcomes(g.client.dev.ID())) != 0 {
			t.Fatalf("revoked client outcomes not deleted: %+v", g.r.Outcomes(g.client.dev.ID()))
		}
		assertReceiptsDeletedOnDisk(t, g, g.client.dev.ID())
		tombs := g.r.Tombstones()
		if len(tombs) != 1 || tombs[0].Client != g.client.dev.ID() || tombs[0].DirectoryVersion == 0 {
			t.Fatalf("tombstones = %+v", tombs)
		}
		if _, err := os.Stat(TombstonesPath(g.state)); err != nil {
			t.Fatalf("tombstones.json missing: %v", err)
		}
	})

	t.Run("revoked_watch_event", func(t *testing.T) {
		g := newRig(t)
		g.enroll(t, []string{"asks.list"}, "asks.answer")
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		seedReceiptOnDisk(t, g, g.client.dev.ID())
		if err := g.r.RecordOutcome(g.client.dev.ID(), Outcome{Number: 1, Status: "done", Agent: chat}); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			g.r.Serve(ctx)
		}()
		defer func() {
			cancel()
			<-done
		}()
		g.waitForRequest(t, func(s string) bool {
			return strings.HasPrefix(s, "GET /v1/dir/watch?") && strings.Contains(s, "serving=1")
		})
		g.waitForRequest(t, func(s string) bool {
			return strings.HasPrefix(s, "PUT /v1/clients/"+g.client.dev.ID()+"/slots/")
		})

		g.clearRequests()
		// Revoking at the relay broadcasts a revoked watch event over Serve's open watch socket.
		if err := g.dir.Revoke(context.Background(), g.client.dev.ID()); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for len(g.r.Tombstones()) == 0 {
			if time.Now().After(deadline) {
				t.Fatal("Serve did not tombstone client on watch-socket revoked event")
			}
			time.Sleep(5 * time.Millisecond)
		}

		assertReceiptsDeletedOnDisk(t, g, g.client.dev.ID())
		if len(g.r.Outcomes(g.client.dev.ID())) != 0 {
			t.Fatalf("outcomes not deleted on watch-socket revoked event: %+v", g.r.Outcomes(g.client.dev.ID()))
		}

		// Subsequent changes and rounds must make zero PUT requests.
		g.clearRequests()
		g.e.asks = []appwire.Ask{{ID: "after-watch-revoke", Agent: chat}}
		g.clock.Advance(clientproto.WatchingFloor)
		g.r.Changed()
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, req := range g.recordedRequests() {
			if strings.HasPrefix(req, "PUT ") {
				t.Fatalf("runner made PUT request after watch-socket revoke: %q", req)
			}
		}
	})
}

// TestFailClosedWhenDirectoryReadFailsAndOfflineReturnReadsDirectoryFirst
// verifies that a round does nothing for any phone when Directory.List fails,
// and an offline computer returning after a revoke reads the directory before
// publishing any slot and never re-admits the tombstoned phone (contract V2).
func TestFailClosedWhenDirectoryReadFailsAndOfflineReturnReadsDirectoryFirst(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Revoke the phone while this computer is offline.
	if err := g.dir.Revoke(context.Background(), g.client.dev.ID()); err != nil {
		t.Fatal(err)
	}

	// Simulate failed directory read: no fabric requests or engine reads may happen.
	failDir := &failingDirClient{Client: g.dir, fail: true}
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	var fabricCalls int
	r, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: failDir,
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			fabricCalls++
			return http.DefaultTransport.RoundTrip(req)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g.r = r

	readsBefore := g.e.reads
	g.clock.Advance(clientproto.WatchingFloor)
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("Step succeeded when Directory.List failed")
	}
	if fabricCalls != 0 || g.e.reads != readsBefore {
		t.Fatalf("failed directory read did not fail closed: fabricCalls=%d reads=%d", fabricCalls, g.e.reads-readsBefore)
	}

	// Computer comes back online: it reads Directory.List first, sees Revoked,
	// writes the tombstone, and makes zero slot PUTs.
	failDir.fail = false
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fabricCalls != 0 || g.e.reads != readsBefore {
		t.Fatalf("returning computer touched fabric for revoked client: fabricCalls=%d", fabricCalls)
	}
	if len(g.r.Tombstones()) != 1 || g.r.Tombstones()[0].Client != g.client.dev.ID() {
		t.Fatalf("tombstone not written on return: %+v", g.r.Tombstones())
	}
}

type failingDirClient struct {
	directory.Client
	fail bool
}

func (f *failingDirClient) List(ctx context.Context) (directory.Listing, error) {
	if f.fail {
		return directory.Listing{}, directory.ErrUnreachable
	}
	return f.Client.List(ctx)
}

// TestDirectoryVersionPinPausesPhoneServingPerRelayAndAcceptOlderListRecovers
// verifies that Open rejects an empty Relay URL with a typed error, that a
// directory listing with a version lower than the pin for the same (identity,
// relay URL) pauses phone serving with the literal design 9.2 sentence and
// makes no PUT or GET /v1/clients, that switching to a different relay URL does
// not trip the pin, and that AcceptOlderList lowers the pin, keeps every
// tombstone and conflict latch, and re-fetches and checks enrollments
// (contract V3, V5).
func TestDirectoryVersionPinPausesPhoneServingPerRelayAndAcceptOlderListRecovers(t *testing.T) {
	if _, err := Open(Config{Home: t.TempDir()}); err == nil {
		t.Fatal("Open accepted empty Relay URL")
	} else {
		var emptyRelayErr *EmptyRelayError
		if !errors.Is(err, ErrEmptyRelay) || !errors.As(err, &emptyRelayErr) {
			t.Fatalf("Open empty Relay error = %v, want EmptyRelayError", err)
		}
	}

	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")

	// Enroll and revoke a second phone dev2 so it is tombstoned before the relay rollback.
	seed2, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	dev2, err := seed2.Join(identity.IssueCert(g.id, seed2.Public(), g.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	pub2, err := hex.DecodeString(dev2.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	e2 := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  dev2.ID(),
		SignPub: pub2,
		BoxPub:  g.pub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"asks.list"},
		Ver:     1,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+dev2.ID()+"/enroll", e2, nil, 204)
	g.request(t, g.s, "PUT", "/v1/clients/"+dev2.ID(), map[string]any{
		"cert":    dev2.Cert,
		"methods": []string{"asks.answer"},
		"name":    "",
	}, nil, 204)
	if err := g.dir.Revoke(context.Background(), dev2.ID()); err != nil {
		t.Fatal(err)
	}

	// Enroll a third phone dev3 and latch a same-ver conflict on it before the relay rollback.
	seed3, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	dev3, err := seed3.Join(identity.IssueCert(g.id, seed3.Public(), g.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	pub3, err := hex.DecodeString(dev3.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	e3a := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  dev3.ID(),
		SignPub: pub3,
		BoxPub:  g.pub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"asks.list"},
		Ver:     1,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+dev3.ID()+"/enroll", e3a, nil, 204)
	g.request(t, g.s, "PUT", "/v1/clients/"+dev3.ID(), map[string]any{
		"cert":    dev3.Cert,
		"methods": []string{"asks.answer"},
		"name":    "",
	}, nil, 204)

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Tombstones()) != 1 || g.r.Tombstones()[0].Client != dev2.ID() {
		t.Fatalf("expected tombstone for dev2: %+v", g.r.Tombstones())
	}

	// Trigger a same-ver conflict latch for dev3.
	e3Conflict := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  dev3.ID(),
		SignPub: pub3,
		BoxPub:  g.pub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"agents.list"},
		Ver:     1,
	})
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && req.URL.Path == "/v1/clients" {
			raw, _ := json.Marshal(map[string]any{
				"clients": []map[string]any{
					{"device": dev3.ID(), "enroll": e3Conflict},
				},
			})
			return &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(raw)),
			}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range g.r.Clients() {
		if c.ID == dev3.ID() {
			t.Fatalf("dev3 should be conflict-latched, got Clients()=%+v", g.r.Clients())
		}
	}

	realListing, err := g.dir.List(context.Background())
	if err != nil || realListing.Version < 2 {
		t.Fatalf("listing version = %d, err = %v", realListing.Version, err)
	}

	// A different relay URL with version 1 does NOT trip the pin for g.url (contract V3).
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	overrideDir := &versionedDirClient{Client: g.dir, version: 1}
	rOtherRelay, err := Open(Config{
		Home:      g.state,
		Relay:     g.url + "/other-relay",
		Name:      "desk",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: overrideDir,
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			var body []byte
			if req.Body != nil {
				body, _ = io.ReadAll(req.Body)
				req.Body = io.NopCloser(bytes.NewReader(body))
			}
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/other-relay")
			req.URL.RawPath = ""
			reqsign.Sign(req, body, g.s, g.clock.Now())
			return http.DefaultTransport.RoundTrip(req)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rOtherRelay.Step(context.Background()); err != nil {
		t.Fatalf("different relay URL tripped directory pin: %v", err)
	}
	if rOtherRelay.Paused() != "" {
		t.Fatalf("different relay URL paused runner: %q", rOtherRelay.Paused())
	}
	if err := rOtherRelay.Close(); err != nil {
		t.Fatal(err)
	}

	// Same relay URL serving an older directory version (1 < realListing.Version) pauses phone serving (contract V3).
	// Also simulate the rolled-back relay listing dev2 as NOT revoked to verify V5 tombstone persistence.
	overrideDir.unrevoke = dev2.ID()
	rSameRelay, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: overrideDir,
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP:      g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rSameRelay.Close() })
	g.r = rSameRelay

	g.clearRequests()
	readsBefore := g.e.reads
	err = g.r.Step(context.Background())
	var rollbackErr *DirectoryRollbackError
	if err == nil || !errors.Is(err, ErrDirectoryVersionRollback) || !errors.As(err, &rollbackErr) {
		t.Fatalf("Step error on lower directory version = %v, want DirectoryRollbackError", err)
	}
	const wantPauseSentence = "phone serving is paused: the device list on this sync address is older than one this computer saw. If that relay was reset, run codeaf phone serve --accept-older-list."
	if g.r.Paused() != wantPauseSentence {
		t.Fatalf("Paused() = %q, want %q", g.r.Paused(), wantPauseSentence)
	}
	if g.e.reads != readsBefore {
		t.Fatal("paused runner read engine snapshots")
	}
	for _, req := range g.recordedRequests() {
		if strings.HasPrefix(req, "PUT ") || req == "GET /v1/clients" {
			t.Fatalf("paused round made forbidden request %q in %v", req, g.recordedRequests())
		}
	}

	// AcceptOlderList lowers the pin, keeps the tombstone for dev2 and the
	// conflict latch for dev3 so neither is re-admitted, and re-fetches and
	// checks enrollments (contract V5).
	if err := g.r.AcceptOlderList(context.Background()); err != nil {
		t.Fatalf("AcceptOlderList: %v", err)
	}
	if g.r.Paused() != "" {
		t.Fatalf("Paused() after AcceptOlderList = %q, want empty", g.r.Paused())
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("Step after AcceptOlderList: %v", err)
	}
	clients := g.r.Clients()
	if len(clients) != 1 || clients[0].ID != g.client.dev.ID() {
		t.Fatalf("Clients() after AcceptOlderList = %+v; tombstoned dev2 and conflict-latched dev3 must stay excluded", clients)
	}
	if len(g.r.Tombstones()) != 1 || g.r.Tombstones()[0].Client != dev2.ID() {
		t.Fatalf("tombstones after AcceptOlderList = %+v", g.r.Tombstones())
	}
}

type versionedDirClient struct {
	directory.Client
	version  uint64
	unrevoke string
}

func (v *versionedDirClient) List(ctx context.Context) (directory.Listing, error) {
	l, err := v.Client.List(ctx)
	if err != nil {
		return l, err
	}
	l.Version = v.version
	if v.unrevoke != "" {
		d := l.Devices[v.unrevoke]
		d.Revoked = false
		l.Devices[v.unrevoke] = d
	}
	return l, nil
}

// TestMonotonicAtPersistedBeforeUploadSurvivesClockRollbackAndRestart verifies
// that every card, snapshot, and outcome slot stamps at = max(wall_clock_ms, last_at+1)
// and persists the new watermark to disk before the PUT request leaves for the
// relay, asserted exclusively through the at fields of the card and slot
// plaintexts opened with the phone's box key (contract L6).
func TestMonotonicAtPersistedBeforeUploadSurvivesClockRollbackAndRestart(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstCard := g.readCard(t, g.s.dev.ID())
	firstSlot := g.readBoxSlot(t, "asks.list", g.s.dev.ID(), 0)

	// Step the computer's wall clock backwards by 2 minutes and reopen the runner.
	g.clock.Advance(-2 * time.Minute)
	g.e.asks = []appwire.Ask{{ID: "ask-1", Agent: chat}}

	// Capture an opaque byte copy of the state directory at the moment the slot
	// PUT is in flight (before RoundTrip sends it to the relay) without inspecting
	// runner.json's key layout.
	prePutHome := t.TempDir()
	var capturedPrePut bool
	slotName := SlotName("asks.list", g.s.dev.ID())

	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk-renamed",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: g.dir,
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "PUT" && strings.HasSuffix(req.URL.Path, "/slots/"+slotName) && !capturedPrePut {
				for _, fn := range []func(string) string{StatePath, PinsPath, TombstonesPath} {
					if raw, err := os.ReadFile(fn(g.state)); err == nil {
						dst := fn(prePutHome)
						_ = os.MkdirAll(filepath.Dir(dst), 0700)
						_ = os.WriteFile(dst, raw, 0600)
					}
				}
				capturedPrePut = true
			}
			return http.DefaultTransport.RoundTrip(req)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g.r = r

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !capturedPrePut {
		t.Fatal("slot PUT was not intercepted")
	}
	secondCard := g.readCard(t, g.s.dev.ID())
	if secondCard.At != firstCard.At+1 {
		t.Fatalf("card At after backward clock jump = %d, want %d", secondCard.At, firstCard.At+1)
	}
	secondSlot := g.readBoxSlot(t, "asks.list", g.s.dev.ID(), firstSlot.At)
	if secondSlot.At != firstSlot.At+1 {
		t.Fatalf("slot At after backward clock jump = %d, want %d", secondSlot.At, firstSlot.At+1)
	}

	// Open a runner from the pre-PUT disk snapshot while the clock is still 2
	// minutes behind. Because secondSlot.At was persisted to disk before the PUT
	// left the client, the next write must stamp thirdSlot.At = secondSlot.At + 1.
	g.e.asks = []appwire.Ask{{ID: "ask-2", Agent: chat}}
	rFromPrePut, err := Open(Config{
		Home:      prePutHome,
		Relay:     g.url,
		Name:      "desk-renamed",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: g.dir,
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP:      g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rFromPrePut.Close() })
	if err := rFromPrePut.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	thirdSlot := g.readBoxSlot(t, "asks.list", g.s.dev.ID(), secondSlot.At)
	if thirdSlot.At != secondSlot.At+1 {
		t.Fatalf("slot At from pre-PUT disk state = %d, want %d (watermark was not persisted before upload)", thirdSlot.At, secondSlot.At+1)
	}
}

// TestComputerCapsReadsAtTenForTwelveTotalSlots verifies that a computer never
// writes more than 12 slots per phone (1 card + 1 outcomes + 10 read snapshots,
// contract L6) even when the phone is enrolled with 12 valid read methods that
// the engine serves without error.
func TestComputerCapsReadsAtTenForTwelveTotalSlots(t *testing.T) {
	g := newRig(t)
	manyReads := []string{
		"asks.list", "agents.list", "inbox.list", "tasks.list", "spend.today",
		"agents.entries", "models.list", "settings.get", "crews.list", "ping",
		"plugins.list", "health.check",
	}
	// Confirm every read name in manyReads is served by the engine without error.
	for _, name := range manyReads {
		params, _ := json.Marshal(appwire.EntriesParams{ID: chat, Limit: 10})
		if name != "agents.entries" {
			params, _ = json.Marshal(appwire.AgentParams{Agent: chat})
		}
		if _, err := appwire.Call(g.e, name, params); err != nil {
			t.Fatalf("engine failed to serve read %q: %v", name, err)
		}
	}

	var slotPuts []string
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "PUT" && strings.Contains(req.URL.Path, "/slots/") {
			slotPuts = append(slotPuts, req.URL.Path)
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	g.enroll(t, manyReads)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("Step failed: %v", err)
	}
	if len(slotPuts) != clientproto.MaxComputerSlots {
		t.Fatalf("computer wrote %d slots (%v), want %d", len(slotPuts), slotPuts, clientproto.MaxComputerSlots)
	}
	for _, p := range slotPuts {
		if strings.Contains(p, "plugins.list") || strings.Contains(p, "health.check") {
			t.Fatalf("computer published past 10th read slot: %s", p)
		}
	}
}
