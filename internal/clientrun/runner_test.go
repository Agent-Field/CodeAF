package clientrun

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/skillpick"
)

type signer struct {
	id  identity.Identity
	dev identity.Dev
}

func (s signer) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s signer) Cert() identity.Cert            { return s.dev.Cert }
func (s signer) Sign(p []byte) []byte           { return s.dev.Sign(p) }

type engine struct {
	appwire.Crewer
	appwire.Inboxer
	appwire.Worker
	appwire.Plugins
	appwire.SkillShelf
	appwire.HealthChecker
	inboxAnswers     int
	taskStarts       int
	taskIrreversible bool
	entryBytes       int
	entryPrefix      string
	entrySuffix      string
	returnedEntries  []appwire.Entry
	agents           []appwire.Agent
	onRead           func()
	created          int
	answers          int
	appwire.Engine
	reads int
	asks  []appwire.Ask
}

func (e *engine) ListAsks(agent string) ([]appwire.Ask, error) {
	e.reads++
	if e.onRead != nil {
		e.onRead()
	}
	return e.asks, nil
}

func (e *engine) ListAgents(string) ([]appwire.Agent, error) {
	if e.agents != nil {
		return e.agents, nil
	}
	return []appwire.Agent{{ID: chat}, {ID: otherChat}}, nil
}

func (e *engine) ListInbox(string, string) ([]appwire.InboxItem, appwire.InboxCounts, error) {
	return nil, appwire.InboxCounts{}, nil
}

func (e *engine) SpendToday() (appwire.Spend, error) {
	return appwire.Spend{}, nil
}

func (e *engine) ListModels() ([]appwire.Model, error) {
	return nil, nil
}

func (e *engine) Settings() (appwire.Settings, error) {
	return appwire.Settings{}, nil
}

func (e *engine) CrewList(bool) ([]appwire.Crew, error) {
	return nil, nil
}

func (e *engine) ListPlugins() (appwire.PluginsResult, error) {
	return appwire.PluginsResult{}, nil
}

func (e *engine) HealthCheck() (appwire.Health, error) {
	return appwire.Health{}, nil
}

func (e *engine) ListSkills(string) ([]skillpick.Row, error) {
	return nil, nil
}

const chat = "01J0000000000000000000000A"
const otherChat = "01J0000000000000000000000B"

type rig struct {
	r          *Runner
	e          *engine
	id         identity.Identity
	s          signer
	client     signer
	pub, priv  *[32]byte
	url        string
	relayStore string
	relay      *relayserve.Service
	srv        *httptest.Server
	clock      *directorytest.FakeClock
	dir        directory.Client
	state      string

	reqMu    sync.Mutex
	requests []string
	reqCh    chan string
	logsMu   sync.Mutex
	logs     []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	clock := directorytest.NewFakeClock()
	relayStore := t.TempDir()
	g := &rig{
		clock:      clock,
		relayStore: relayStore,
		e:          &engine{},
		state:      t.TempDir(),
		reqCh:      make(chan string, 256),
	}
	g.startRelay(t)

	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	s := signer{id, dev}
	dir := directory.NewHTTP(g.url, reqsign.SignFor(s, clock.Now), g.srv.Client())
	if err := dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.Create(context.Background(), chat, directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	c, err := seed.Join(identity.IssueCert(id, seed.Public(), clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.id = id
	g.s = s
	g.client = signer{id, c}
	g.pub = pub
	g.priv = priv
	g.dir = dir

	r, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk",
		Identity:  id.PublicKey(),
		Signer:    s,
		Directory: dir,
		Engine:    g.e,
		LeaseCreated: func(ctx context.Context, a appwire.Agent) error {
			_, err := dir.Create(ctx, a.ID, directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat"})
			return err
		},
		Log:  noopLog,
		Logf: g.logf,
		Now:  clock.Now,
		HTTP: g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g.r = r
	return g
}

func (g *rig) startRelay(t *testing.T) {
	t.Helper()
	g.relay = relayserve.New(relayserve.Config{
		Store: g.relayStore,
		Now:   g.clock.Now,
		After: g.clock.After,
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		line := r.Method + " " + r.URL.RequestURI()
		g.reqMu.Lock()
		g.requests = append(g.requests, line)
		g.reqMu.Unlock()
		g.relay.Handler.ServeHTTP(w, r)
		select {
		case g.reqCh <- line:
		default:
		}
	})
	g.srv = httptest.NewServer(handler)
	g.url = g.srv.URL
	t.Cleanup(func() {
		if g.srv != nil {
			g.srv.Close()
			_ = g.relay.Close()
		}
	})
}

func (g *rig) logf(format string, args ...any) {
	g.logsMu.Lock()
	defer g.logsMu.Unlock()
	g.logs = append(g.logs, format)
}

func (g *rig) clearRequests() {
	g.reqMu.Lock()
	g.requests = nil
	g.reqMu.Unlock()
	for {
		select {
		case <-g.reqCh:
		default:
			return
		}
	}
}

func (g *rig) waitForRequest(t *testing.T, match func(string) bool) {
	t.Helper()
	for {
		for _, r := range g.recordedRequests() {
			if match(r) {
				return
			}
		}
		select {
		case r := <-g.reqCh:
			if match(r) {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for request; seen=%v", g.recordedRequests())
		}
	}
}

func (g *rig) recordedRequests() []string {
	g.reqMu.Lock()
	defer g.reqMu.Unlock()
	return slices.Clone(g.requests)
}

// enroll registers the phone in the relay's fabric enrollment store and
// directory, matching the approval order in design 3.7 (enroll, then register).
func (g *rig) enroll(t *testing.T, reads []string, methods ...string) clientproto.Enrollment {
	t.Helper()
	return g.enrollVer(t, 1, reads, methods...)
}

func signEnrollment(id identity.Identity, e clientproto.Enrollment) clientproto.Enrollment {
	if e.Methods == nil {
		e.Methods = []string{}
	}
	if e.Reads == nil {
		e.Reads = []string{}
	}
	e.Sig = id.Sign(e.SignText())
	return e
}

func (g *rig) enrollVer(t *testing.T, ver uint64, reads []string, methods ...string) clientproto.Enrollment {
	t.Helper()
	if reads == nil {
		reads = []string{"asks.list"}
	}
	if methods == nil {
		methods = []string{}
	}
	pubBytes, err := hex.DecodeString(g.client.dev.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	e := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  g.client.dev.ID(),
		SignPub: pubBytes,
		BoxPub:  g.pub[:],
		Methods: methods,
		Reads:   reads,
		Ver:     ver,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+g.client.dev.ID()+"/enroll", e, nil, 204)
	if ver == 1 {
		g.request(t, g.s, "PUT", "/v1/clients/"+g.client.dev.ID(), map[string]any{
			"cert":    g.client.Cert(),
			"methods": methods,
			"name":    "",
		}, nil, 204)
	}
	return e
}

func (g *rig) readCard(t *testing.T, writer string) clientproto.Card {
	t.Helper()
	var slot struct {
		Sealed []byte `json:"sealed"`
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("card", writer), nil, &slot, 200)
	card, err := clientproto.OpenCard(slot.Sealed, g.pub, g.priv)
	if err != nil {
		t.Fatalf("OpenCard: %v", err)
	}
	if err := card.Verify(g.id.PublicKey()); err != nil {
		t.Fatalf("Card.Verify: %v", err)
	}
	return card
}

func (g *rig) readBoxSlot(t *testing.T, method, writer string, lastAt int64) clientproto.SlotPlain {
	t.Helper()
	card := g.readCard(t, writer)
	slotName := SlotName(method, writer)
	var slot struct {
		Sealed []byte `json:"sealed"`
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+slotName, nil, &slot, 200)
	if len(slot.Sealed) > clientproto.MaxSlotBytes {
		t.Fatalf("sealed slot %s has %d bytes", slotName, len(slot.Sealed))
	}
	p, err := clientproto.OpenSlot(slot.Sealed, (*[32]byte)(card.BoxPub), g.priv)
	if err != nil {
		t.Fatalf("OpenSlot(%s): %v", slotName, err)
	}
	if err := p.Check(slotName, g.client.dev.ID(), writer, lastAt); err != nil {
		t.Fatalf("SlotPlain.Check(%s): %v", slotName, err)
	}
	return p
}

// TestNoEnrolledPhoneMakesOnlyDirectoryRequests verifies that a running runner
// with no phone enrolled, with only a revoked client in the directory, or with
// an enrollment stored for a device absent from the directory makes only
// directory requests and its serving=1 watch socket, and never calls
// GET /v1/clients or any card, slot, outcome, or queue route (contract P8).
func TestNoEnrolledPhoneMakesOnlyDirectoryRequests(t *testing.T) {
	assertOnlyDirRequests := func(t *testing.T, g *rig) {
		t.Helper()
		g.clearRequests()
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, req := range g.recordedRequests() {
			if req != "GET /v1/dir/list" {
				t.Fatalf("Step made unexpected request %q in %v", req, g.recordedRequests())
			}
		}

		// Run Serve across the initial round and one more full timer cycle on the fake clock.
		ticks := make(chan time.Time, 2)
		if err := g.r.Close(); err != nil {
			t.Fatal(err)
		}
		r, err := Open(Config{
			Home:      g.state,
			Relay:     g.url,
			Name:      "desk",
			Identity:  g.id.PublicKey(),
			Signer:    g.s,
			Directory: g.dir,
			Engine:    g.e,
			Log:       noopLog,
			Logf:      g.logf,
			Now:       g.clock.Now,
			// Only the refresh timer receives the tick; see the note in watching_test.go.
			After: func(d time.Duration) <-chan time.Time {
				if d == refreshEvery {
					return ticks
				}
				return nil
			},
			HTTP: g.srv.Client(),
		})
		if err != nil {
			t.Fatal(err)
		}
		g.r = r

		g.clearRequests()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			g.r.Serve(ctx)
		}()
		g.waitForRequest(t, func(s string) bool {
			return strings.HasPrefix(s, "GET /v1/dir/watch?") && strings.Contains(s, "serving=1") && strings.Contains(s, "events=1")
		})
		g.waitForRequest(t, func(s string) bool {
			return s == "GET /v1/dir/list"
		})
		// Advance the fake clock and fire a timer tick while no active phone is known;
		// even across another full timer window, no /v1/clients request may occur.
		g.clock.Advance(refreshEvery)
		ticks <- g.clock.Now()
		g.r.Changed()
		if err := g.r.Step(context.Background()); err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		cancel()
		<-done

		reqs := g.recordedRequests()
		listCount := 0
		for _, req := range reqs {
			if req == "GET /v1/dir/list" {
				listCount++
			}
			if strings.Contains(req, "/v1/clients") {
				t.Fatalf("touched client route when no active client is in the listing: %v", reqs)
			}
			if !strings.HasPrefix(req, "GET /v1/dir/list") && !strings.HasPrefix(req, "GET /v1/dir/watch?") {
				t.Fatalf("unexpected request: %q in %v", req, reqs)
			}
		}
		if listCount < 2 {
			t.Fatalf("expected at least 2 directory list rounds, got %d in %v", listCount, reqs)
		}
	}

	t.Run("no_client_enrolled", func(t *testing.T) {
		g := newRig(t)
		assertOnlyDirRequests(t, g)
	})

	t.Run("enrollment_stored_for_unregistered_device", func(t *testing.T) {
		g := newRig(t)
		pubBytes, err := hex.DecodeString(g.client.dev.Cert.Device)
		if err != nil {
			t.Fatal(err)
		}
		// Store a signed enrollment at /v1/clients/{id}/enroll without registering
		// the device in the directory listing via PUT /v1/clients/{id}.
		e := signEnrollment(g.id, clientproto.Enrollment{
			V:       1,
			Device:  g.client.dev.ID(),
			SignPub: pubBytes,
			BoxPub:  g.pub[:],
			Methods: []string{"asks.answer"},
			Reads:   []string{"asks.list"},
			Ver:     1,
		})
		g.request(t, g.s, "PUT", "/v1/clients/"+g.client.dev.ID()+"/enroll", e, nil, 204)
		assertOnlyDirRequests(t, g)
	})

	t.Run("only_client_in_listing_is_revoked", func(t *testing.T) {
		g := newRig(t)
		g.enroll(t, []string{"asks.list"}, "asks.answer")
		if err := g.dir.Revoke(context.Background(), g.client.dev.ID()); err != nil {
			t.Fatal(err)
		}
		assertOnlyDirRequests(t, g)
	})
}

// TestNonClientDeviceInListingIsNeverServedEvenWithEnrollment verifies that a
// device present in the directory listing with a kind other than "client" is
// never served even when a signed enrollment at the relay names it.
func TestNonClientDeviceInListingIsNeverServedEvenWithEnrollment(t *testing.T) {
	g := newRig(t)
	// Enroll a real phone so Step fetches GET /v1/clients.
	g.enroll(t, []string{"asks.list"}, "asks.answer")

	// Join a second non-client device to the directory listing.
	seedOther, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	devOther, err := seedOther.Join(identity.IssueCert(g.id, seedOther.Public(), g.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	pubOther, err := hex.DecodeString(devOther.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	otherBoxPub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// Store a valid identity-signed enrollment naming the non-client device, then
	// register that device in the directory as a non-client computer device.
	eOther := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  devOther.ID(),
		SignPub: pubOther,
		BoxPub:  otherBoxPub[:],
		Methods: []string{"asks.answer"},
		Reads:   []string{"asks.list"},
		Ver:     1,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+devOther.ID()+"/enroll", eOther, nil, 204)
	dirOther := directory.NewHTTP(g.url, reqsign.SignFor(signer{g.id, devOther}, g.clock.Now), g.srv.Client())
	if err := dirOther.PutDevice(context.Background(), devOther.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}

	g.clearRequests()
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	clients := g.r.Clients()
	if len(clients) != 1 || clients[0].ID != g.client.dev.ID() {
		t.Fatalf("non-client device was admitted into Clients(): %+v", clients)
	}
	for _, req := range g.recordedRequests() {
		if strings.Contains(req, "/v1/clients/"+devOther.ID()) {
			t.Fatalf("runner touched fabric route for non-client device %s: %q", devOther.ID(), req)
		}
	}
}

// TestSlotsAndCardUseAuthenticatedBoxAndKeepEmptyChatScope verifies that the
// runner publishes a device-signed card sealed with SealCard, snapshot slots
// sealed with SealSlot under the card's executor key, and an outcomes slot, and
// does not rewrite unchanged slots on later rounds (contract L3, L6).
func TestSlotsAndCardUseAuthenticatedBoxAndKeepEmptyChatScope(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	card := g.readCard(t, g.s.dev.ID())
	if card.Name != "desk" || card.Writer != g.s.dev.ID() {
		t.Fatalf("card = %+v", card)
	}
	p := g.readBoxSlot(t, "asks.list", g.s.dev.ID(), 0)
	if p.Ver != 1 {
		t.Fatalf("slot ver = %d, want 1", p.Ver)
	}
	var envelopes []struct {
		Writer string
		At     time.Time
		Agent  string
		Result appwire.AsksResult
	}
	if err := json.Unmarshal(p.Payload, &envelopes); err != nil {
		t.Fatal(err)
	}
	if len(envelopes) != 1 || envelopes[0].Agent != chat || envelopes[0].Writer != g.s.dev.ID() || !envelopes[0].At.Equal(g.clock.Now()) || envelopes[0].Result.Asks == nil {
		t.Fatalf("slot payload: %s", p.Payload)
	}

	outcomesSlot := g.readBoxSlot(t, "outcomes", g.s.dev.ID(), 0)
	if string(outcomesSlot.Payload) != "[]" {
		t.Fatalf("initial outcomes payload = %s, want []", outcomesSlot.Payload)
	}

	// An unchanged card, snapshot, or outcome slot is not rewritten on the next round.
	var beforeCard, beforeSlot, beforeOutcomes struct {
		Sealed  []byte `json:"sealed"`
		Version uint64 `json:"version"`
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("card", g.s.dev.ID()), nil, &beforeCard, 200)
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("asks.list", g.s.dev.ID()), nil, &beforeSlot, 200)
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("outcomes", g.s.dev.ID()), nil, &beforeOutcomes, 200)

	g.clock.Advance(time.Minute)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var afterCard, afterSlot, afterOutcomes struct {
		Sealed  []byte `json:"sealed"`
		Version uint64 `json:"version"`
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("card", g.s.dev.ID()), nil, &afterCard, 200)
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("asks.list", g.s.dev.ID()), nil, &afterSlot, 200)
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("outcomes", g.s.dev.ID()), nil, &afterOutcomes, 200)
	if !bytes.Equal(beforeCard.Sealed, afterCard.Sealed) || beforeCard.Version != afterCard.Version {
		t.Fatal("unchanged card slot was rewritten")
	}
	if !bytes.Equal(beforeSlot.Sealed, afterSlot.Sealed) || beforeSlot.Version != afterSlot.Version {
		t.Fatal("unchanged snapshot slot was rewritten")
	}
	if !bytes.Equal(beforeOutcomes.Sealed, afterOutcomes.Sealed) || beforeOutcomes.Version != afterOutcomes.Version {
		t.Fatal("unchanged outcomes slot was rewritten")
	}
}

// TestSnapshotsOnlyContainLeasedChats verifies that snapshots partition global
// results to only the chats leased by this computer.
func TestSnapshotsOnlyContainLeasedChats(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"agents.list", "asks.list"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := g.readBoxSlot(t, "agents.list", g.s.dev.ID(), 0)
	var got []struct {
		Agent  string
		Result appwire.AgentsResult
	}
	if err := json.Unmarshal(p.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Agent != chat || len(got[0].Result.Agents) != 1 || got[0].Result.Agents[0].ID != chat {
		t.Fatalf("unleased chat leaked: %s", p.Payload)
	}
	if err := g.dir.Release(context.Background(), chat, 1); err != nil {
		t.Fatal(err)
	}
	reads := g.e.reads
	g.clock.Advance(clientproto.WatchingFloor)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.reads != reads {
		t.Fatal("read a chat after releasing its lease")
	}
}

// TestStepReadsDirectoryOncePerRound verifies that a round reads Directory.List
// once at the start of the round and reuses that listing across every read of
// every phone (contract V2, L4).
func TestStepReadsDirectoryOncePerRound(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"agents.list", "asks.list", "inbox.list", "tasks.list", "spend.today"})
	g.clearRequests()
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	dirLists := 0
	for _, req := range g.recordedRequests() {
		if req == "GET /v1/dir/list" {
			dirLists++
		}
	}
	if dirLists != 1 {
		t.Fatalf("Step called GET /v1/dir/list %d times, want 1", dirLists)
	}
}

// TestOversizedSnapshotDoesNotStarveOtherSubscribedReadsOrPhones verifies that
// one oversized non-entry snapshot backs off only that (phone, slot) and does
// not prevent other subscribed slots or other phones from publishing or
// reacting to watch events (contract L5).
func TestOversizedSnapshotDoesNotStarveOtherSubscribedReadsOrPhones(t *testing.T) {
	g := newRig(t)
	g.e.agents = []appwire.Agent{{ID: chat, Title: strings.Repeat("x", 65536)}}
	g.enroll(t, []string{"agents.list", "asks.list"})

	// Enroll a second phone subscribed only to asks.list.
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
	boxPub2, boxPriv2, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client2 := signer{g.id, dev2}
	e2 := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  dev2.ID(),
		SignPub: pub2,
		BoxPub:  boxPub2[:],
		Methods: []string{},
		Reads:   []string{"asks.list"},
		Ver:     1,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+dev2.ID()+"/enroll", e2, nil, 204)
	g.request(t, g.s, "PUT", "/v1/clients/"+dev2.ID(), map[string]any{
		"cert":    dev2.Cert,
		"methods": []string{},
		"name":    "",
	}, nil, 204)

	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("oversized snapshot not reported by Step")
	}
	// Phone 1's asks.list and Phone 2's asks.list both published despite Phone 1's oversized agents.list.
	_ = g.readBoxSlot(t, "asks.list", g.s.dev.ID(), 0)
	var card2Slot, asks2Slot struct {
		Sealed []byte `json:"sealed"`
	}
	g.request(t, client2, "GET", "/v1/clients/"+dev2.ID()+"/slots/"+SlotName("card", g.s.dev.ID()), nil, &card2Slot, 200)
	card2, err := clientproto.OpenCard(card2Slot.Sealed, boxPub2, boxPriv2)
	if err != nil {
		t.Fatalf("phone 2 OpenCard: %v", err)
	}
	g.request(t, client2, "GET", "/v1/clients/"+dev2.ID()+"/slots/"+SlotName("asks.list", g.s.dev.ID()), nil, &asks2Slot, 200)
	if _, err := clientproto.OpenSlot(asks2Slot.Sealed, (*[32]byte)(card2.BoxPub), boxPriv2); err != nil {
		t.Fatalf("phone 2 OpenSlot(asks.list): %v", err)
	}

	// Now run Serve(ctx) while Phone 1's agents.list is still oversized; a change
	// to asks.list while watching must publish immediately without waiting for the
	// failed slot's 10s backoff.
	g.clock.Advance(time.Second)
	g.r.setWatching(g.client.dev.ID(), true)
	g.r.setWatching(dev2.ID(), true)
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

	g.clearRequests()
	g.e.asks = []appwire.Ask{{ID: "not-stalled", Agent: chat}}
	g.r.Changed()
	asksSlot1 := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("asks.list", g.s.dev.ID())
	asksSlot2 := "/v1/clients/" + dev2.ID() + "/slots/" + SlotName("asks.list", g.s.dev.ID())
	g.waitForRequest(t, func(s string) bool { return s == "PUT "+asksSlot1 })
	g.waitForRequest(t, func(s string) bool { return s == "PUT "+asksSlot2 })
	p1 := g.readBoxSlot(t, "asks.list", g.s.dev.ID(), 0)
	if !strings.Contains(string(p1.Payload), "not-stalled") {
		t.Fatalf("other read slot was stalled by oversized agents.list: %s", p1.Payload)
	}
}

// TestPairOnceAtACoveredByRunningBAndNewComputerC verifies that a phone paired
// once at computer A is automatically discovered and served by an already
// running runner on computer B via its watch socket with no action on B, and
// by a third computer C started after the phone was paired on C's first round
// (contract P1, P4, P5).
func TestPairOnceAtACoveredByRunningBAndNewComputerC(t *testing.T) {
	g := newRig(t)

	// Join computer B to the same identity and start B's runner BEFORE the phone is paired.
	homeB := t.TempDir()
	if _, err := identity.Adopt(homeB, g.id, false); err != nil {
		t.Fatal(err)
	}
	devB, err := identity.Device(homeB)
	if err != nil {
		t.Fatal(err)
	}
	signerB := signer{g.id, devB}
	dirB := directory.NewHTTP(g.url, reqsign.SignFor(signerB, g.clock.Now), g.srv.Client())
	if err := dirB.PutDevice(context.Background(), devB.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	runnerB, err := Open(Config{
		Home:      t.TempDir(),
		Relay:     g.url,
		Name:      "laptop-b",
		Identity:  g.id.PublicKey(),
		Signer:    signerB,
		Directory: dirB,
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP:      g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runnerB.Close() })

	g.clearRequests()
	ctxB, cancelB := context.WithCancel(context.Background())
	doneB := make(chan struct{})
	go func() {
		defer close(doneB)
		runnerB.Serve(ctxB)
	}()
	defer func() {
		cancelB()
		<-doneB
	}()

	// Wait until B's watch socket and initial directory read have completed with no phone enrolled.
	g.waitForRequest(t, func(s string) bool {
		return strings.HasPrefix(s, "GET /v1/dir/watch?") && strings.Contains(s, "serving=1")
	})
	g.waitForRequest(t, func(s string) bool {
		return s == "GET /v1/dir/list"
	})

	// Computer A approves/enrolls the phone. Nothing is touched on B.
	g.enroll(t, []string{"asks.list"}, "agents.create")

	// B learns of the phone from its directory watch socket and publishes its card and slots.
	cardSlotB := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("card", devB.ID())
	asksSlotB := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("asks.list", devB.ID())
	g.waitForRequest(t, func(s string) bool { return s == "PUT "+cardSlotB })
	g.waitForRequest(t, func(s string) bool { return s == "PUT "+asksSlotB })
	if len(runnerB.Clients()) != 1 || runnerB.Clients()[0].ID != g.client.dev.ID() {
		t.Fatalf("runner B did not pick up phone paired at A: %+v", runnerB.Clients())
	}
	cardB := g.readCard(t, devB.ID())
	if cardB.Name != "laptop-b" {
		t.Fatalf("card B name = %q, want laptop-b", cardB.Name)
	}
	_ = g.readBoxSlot(t, "asks.list", devB.ID(), 0)

	// Computer C joins the identity and starts its runner AFTER the phone was paired (contract P5).
	homeC := t.TempDir()
	if _, err := identity.Adopt(homeC, g.id, false); err != nil {
		t.Fatal(err)
	}
	devC, err := identity.Device(homeC)
	if err != nil {
		t.Fatal(err)
	}
	signerC := signer{g.id, devC}
	dirC := directory.NewHTTP(g.url, reqsign.SignFor(signerC, g.clock.Now), g.srv.Client())
	if err := dirC.PutDevice(context.Background(), devC.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	runnerC, err := Open(Config{
		Home:      t.TempDir(),
		Relay:     g.url,
		Name:      "studio-c",
		Identity:  g.id.PublicKey(),
		Signer:    signerC,
		Directory: dirC,
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP:      g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runnerC.Close() })

	if err := runnerC.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	cardC := g.readCard(t, devC.ID())
	if cardC.Name != "studio-c" {
		t.Fatalf("card C name = %q, want studio-c", cardC.Name)
	}
	_ = g.readBoxSlot(t, "asks.list", devC.ID(), 0)
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (g *rig) reopen(t *testing.T, tr http.RoundTripper, log func(Receipt) error) {
	t.Helper()
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	hc := g.srv.Client()
	if tr != nil {
		hc = &http.Client{Transport: tr}
	}
	r, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: g.dir,
		Engine:    g.e,
		HTTP:      hc,
		Now:       g.clock.Now,
		Log:       log,
		Logf:      g.logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g.r = r
}

func noopLog(Receipt) error { return nil }

func (g *rig) request(t *testing.T, s signer, method, path string, body, out any, status int) []byte {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, g.url+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, raw, s, g.clock.Now())
	resp, err := g.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != status {
		t.Fatalf("%s %s: %d %s; want %d", method, path, resp.StatusCode, data, status)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
	return data
}
