package phonetest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/fabricclient"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/skillpick"
)

type testSigner struct {
	id  identity.Identity
	dev identity.Dev
}

func (s testSigner) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s testSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s testSigner) Sign(p []byte) []byte           { return s.dev.Sign(p) }

type scriptedEngine struct {
	appwire.Crewer
	appwire.Inboxer
	appwire.Worker
	appwire.Plugins
	appwire.SkillShelf
	appwire.HealthChecker
	appwire.Engine

	mu      sync.Mutex
	clock   func() time.Time
	nextID  *int
	agents  []appwire.Agent
	asks    []appwire.Ask
	entries map[string][]appwire.Entry
}

func newScriptedEngine(clock func() time.Time, nextID *int) *scriptedEngine {
	return &scriptedEngine{
		clock:   clock,
		nextID:  nextID,
		entries: map[string][]appwire.Entry{},
	}
}

func (e *scriptedEngine) ListAsks(agent string) ([]appwire.Ask, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []appwire.Ask
	for _, a := range e.asks {
		if a.Agent == "" || a.Agent == agent {
			out = append(out, a)
		}
	}
	if out == nil {
		out = []appwire.Ask{}
	}
	return out, nil
}

func (e *scriptedEngine) ListAgents(string) ([]appwire.Agent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.agents), nil
}

func (e *scriptedEngine) Entries(id, _ string, limit int) ([]appwire.Entry, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rows := slices.Clone(e.entries[id])
	if rows == nil {
		rows = []appwire.Entry{}
	}
	if limit > 0 && len(rows) > limit {
		return rows[len(rows)-limit:], true, nil
	}
	return rows, false, nil
}

func (e *scriptedEngine) ListTasks(string) ([]appwire.Task, error) {
	return []appwire.Task{}, nil
}

func (e *scriptedEngine) Create(ask appwire.CreateAsk) (appwire.Agent, error) {
	e.mu.Lock()
	*e.nextID++
	n := *e.nextID
	id := "01J0000000000000000000000" + strconv.Itoa(n)
	a := appwire.Agent{ID: id, Title: "chat-" + strconv.Itoa(n)}
	e.mu.Unlock()

	if v := reflect.ValueOf(ask).FieldByName("BeforeMint"); v.IsValid() && !v.IsNil() {
		if fn, ok := v.Interface().(func(string) error); ok {
			if err := fn(id); err != nil {
				return appwire.Agent{}, err
			}
		}
	}
	if v := reflect.ValueOf(ask).FieldByName("BeforeStart"); v.IsValid() && !v.IsNil() {
		if fn, ok := v.Interface().(func(appwire.Agent) error); ok {
			if err := fn(a); err != nil {
				return appwire.Agent{}, err
			}
		}
	}
	e.mu.Lock()
	e.agents = append(e.agents, a)
	e.mu.Unlock()
	return a, nil
}

func (e *scriptedEngine) Send(id, text, clientID string) (uint64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	msg := len(e.entries[id]) + 1
	e.entries[id] = append(e.entries[id], appwire.Entry{
		ID:       strconv.Itoa(msg),
		Agent:    id,
		At:       e.clock(),
		Kind:     "user",
		Text:     text,
		ClientID: clientID,
		Turn:     1,
		Msg:      &msg,
	})
	return 1, nil
}

func (e *scriptedEngine) ListInbox(string, string) ([]appwire.InboxItem, appwire.InboxCounts, error) {
	return nil, appwire.InboxCounts{}, nil
}

func (e *scriptedEngine) SpendToday() (appwire.Spend, error) {
	return appwire.Spend{}, nil
}

func (e *scriptedEngine) ListModels() ([]appwire.Model, error) {
	return nil, nil
}

func (e *scriptedEngine) Settings() (appwire.Settings, error) {
	return appwire.Settings{}, nil
}

func (e *scriptedEngine) CrewList(bool) ([]appwire.Crew, error) {
	return nil, nil
}

func (e *scriptedEngine) ListPlugins() (appwire.PluginsResult, error) {
	return appwire.PluginsResult{}, nil
}

func (e *scriptedEngine) HealthCheck() (appwire.Health, error) {
	return appwire.Health{}, nil
}

func (e *scriptedEngine) ListSkills(string) ([]skillpick.Row, error) {
	return nil, nil
}

type computerNode struct {
	name      string
	home      string
	stateHome string
	id        identity.Identity
	dev       identity.Dev
	signer    testSigner
	dir       *directory.HTTP
	fc        *fabricclient.Client
	approver  pair.Approver
	engine    *scriptedEngine
	runner    *clientrun.Runner

	mu            sync.Mutex
	watermark     map[string]int64
	createdByID   map[string]string
	localOutcomes map[string][]clientproto.Outcome
}

type testRig struct {
	t          *testing.T
	clock      *directorytest.FakeClock
	relay      *relayserve.Service
	srv        *httptest.Server
	url        string
	id         identity.Identity
	computer   *computerNode
	nextChatID int
}

func newTestRig(t *testing.T) *testRig {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	clock := directorytest.NewFakeClock()
	relay := relayserve.New(relayserve.Config{
		Store: t.TempDir(),
		Now:   clock.Now,
		After: clock.After,
	})
	srv := httptest.NewServer(relay.Handler)
	t.Cleanup(func() {
		srv.Close()
		_ = relay.Close()
	})

	homeA := t.TempDir()
	id, err := identity.Ensure(homeA)
	if err != nil {
		t.Fatal(err)
	}
	g := &testRig{
		t:     t,
		clock: clock,
		relay: relay,
		srv:   srv,
		url:   srv.URL,
		id:    id,
	}
	g.computer = g.addComputer(t, homeA, "desk")
	return g
}

func (g *testRig) addComputer(t *testing.T, home, name string) *computerNode {
	t.Helper()
	if home == "" {
		home = t.TempDir()
		if _, err := identity.Adopt(home, g.id, false); err != nil {
			t.Fatal(err)
		}
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	s := testSigner{id: g.id, dev: dev}
	dir := directory.NewHTTP(g.url, reqsign.SignFor(s, g.clock.Now), g.srv.Client())
	sealedName, err := directory.SealName(directory.MetadataKey(g.id.CellKey()), name)
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1, Name: sealedName, AddedBy: g.id.ID()}); err != nil {
		t.Fatal(err)
	}
	fc := fabricclient.New(g.url, s, g.srv.Client())
	fc.Now = g.clock.Now

	eng := newScriptedEngine(g.clock.Now, &g.nextChatID)
	initialChat, err := eng.Create(appwire.CreateAsk{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dir.Create(context.Background(), initialChat.ID, directory.CellInit{
		Head:  strings.Repeat("1", clientproto.MaxClientMethods),
		Class: "chat",
	}); err != nil {
		t.Fatal(err)
	}

	stateHome := t.TempDir()
	r, err := clientrun.Open(clientrun.Config{
		Home:      stateHome,
		Relay:     g.url,
		Name:      name,
		Identity:  g.id.PublicKey(),
		Signer:    s,
		Directory: dir,
		Engine:    eng,
		LeaseCreated: func(ctx context.Context, a appwire.Agent) error {
			_, err := dir.Create(ctx, a.ID, directory.CellInit{
				Head:  strings.Repeat("1", clientproto.MaxClientMethods),
				Class: "chat",
			})
			return err
		},
		Log:  func(clientrun.Receipt) error { return nil },
		Now:  g.clock.Now,
		HTTP: g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	node := &computerNode{
		name:          name,
		home:          home,
		stateHome:     stateHome,
		id:            g.id,
		dev:           dev,
		signer:        s,
		dir:           dir,
		fc:            fc,
		engine:        eng,
		runner:        r,
		watermark:     map[string]int64{},
		createdByID:   map[string]string{},
		localOutcomes: map[string][]clientproto.Outcome{},
	}
	node.approver = pair.Approver{
		Requests: directory.NewLinkHTTP(g.url, g.srv.Client()),
		Dir:      dir,
		Fabric:   fc,
		Identity: g.id,
		SyncURL:  g.url + "/",
		Now:      g.clock.Now,
		Card: func(context.Context) (clientproto.Card, error) {
			return node.signedCard(t), nil
		},
	}
	return node
}

func (c *computerNode) ensureExecutorSecret(t *testing.T) ([ed25519.PublicKeySize]byte, [ed25519.PublicKeySize]byte) {
	t.Helper()
	path := clientrun.StatePath(c.stateHome)
	var st struct {
		Secret []byte           `json:"secret,omitempty"`
		LastAt map[string]int64 `json:"last_at,omitempty"`
	}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	if len(st.Secret) != ed25519.PublicKeySize {
		_, priv, err := box.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		st.Secret = priv[:]
		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var priv, pub [ed25519.PublicKeySize]byte
	copy(priv[:], st.Secret)
	derived, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	copy(pub[:], derived)
	return pub, priv
}

func (c *computerNode) signedCard(t *testing.T) clientproto.Card {
	t.Helper()
	pub, _ := c.ensureExecutorSecret(t)
	card := clientproto.Card{
		V:      clientproto.SlotVersion,
		Writer: c.dev.ID(),
		Cert:   c.dev.Cert,
		BoxPub: pub[:],
		Name:   c.name,
		At:     c.engine.clock().UnixMilli(),
	}
	card.Sig = c.dev.Sign(card.SignText())
	return card
}

func (g *testRig) newPhone(t *testing.T, name string) *Phone {
	t.Helper()
	p, err := New(Config{
		Name: name,
		HTTP: g.srv.Client(),
		Now:  g.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (g *testRig) signedRequest(t *testing.T, s reqsign.Signer, method, path string, in any, wantStatus int, out any) {
	t.Helper()
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, g.url+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, body, s, g.clock.Now())
	resp, err := g.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: got HTTP %d (%s), want %d", method, path, resp.StatusCode, raw, wantStatus)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
	}
}

// stepComputer runs runner.Step and, if clientrun does not yet drain the phone
// queue itself, drains pending queue entries for enrolled clients and publishes
// the updated outcomes and entries slots.
func (g *testRig) stepComputer(t *testing.T, c *computerNode) {
	t.Helper()
	ctx := context.Background()
	if len(c.runner.Clients()) == 0 {
		if err := c.runner.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, client := range c.runner.Clients() {
		if client.Paused {
			continue
		}
		g.drainFallbackIfQueued(t, c, client)
	}
	if err := c.runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
}

func (g *testRig) drainFallbackIfQueued(t *testing.T, c *computerNode, client clientrun.Client) {
	t.Helper()
	var q struct {
		Entries []struct {
			Number int64  `json:"number"`
			At     int64  `json:"at"`
			Method string `json:"method"`
			Target string `json:"target"`
			Sealed []byte `json:"sealed"`
		} `json:"entries"`
	}
	g.signedRequest(t, c.signer, http.MethodGet, "/v1/clients/"+client.ID+"/queue", nil, http.StatusOK, &q)
	if len(q.Entries) == 0 {
		return
	}
	pub, priv := c.ensureExecutorSecret(t)
	nowMs := g.clock.Now().UnixMilli()
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range q.Entries {
		numStr := strconv.FormatInt(entry.Number, decimalBase)
		g.signedRequest(t, c.signer, http.MethodPost, "/v1/clients/"+client.ID+"/queue/"+numStr+"/claim", nil, http.StatusNoContent, nil)

		if entry.Number <= c.watermark[client.ID] {
			g.signedRequest(t, c.signer, http.MethodDelete, "/v1/clients/"+client.ID+"/queue/"+numStr, nil, http.StatusNoContent, nil)
			continue
		}

		plain, err := clientproto.OpenCall(entry.Sealed, &pub, &priv)
		var outcome clientproto.Outcome
		if err != nil {
			outcome = clientproto.Outcome{Number: entry.Number, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadCall}
		} else {
			checkErr := plain.Check(clientproto.OuterCall{
				Number: entry.Number,
				At:     entry.At,
				Method: entry.Method,
				Target: entry.Target,
			}, client.ID, c.dev.ID(), nowMs)
			switch {
			case errors.Is(checkErr, clientproto.ErrCallExpired):
				outcome = clientproto.Outcome{Number: entry.Number, Status: clientproto.OutcomeExpired, Reason: clientproto.ReasonExpired}
			case checkErr != nil:
				outcome = clientproto.Outcome{Number: entry.Number, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadCall}
			case plain.Method == clientproto.MethodCreate:
				var p CreateParams
				if json.Unmarshal(plain.Params, &p) != nil || p.ClientID == "" {
					outcome = clientproto.Outcome{Number: entry.Number, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadCall}
				} else {
					key := client.ID + "/" + p.ClientID
					chatID, exists := c.createdByID[key]
					if !exists {
						agent, err := c.engine.Create(appwire.CreateAsk{})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := c.dir.Create(context.Background(), agent.ID, directory.CellInit{
							Head:  strings.Repeat("1", clientproto.MaxClientMethods),
							Class: "chat",
						}); err != nil {
							t.Fatal(err)
						}
						chatID = agent.ID
						c.createdByID[key] = chatID
					}
					outcome = clientproto.Outcome{Number: entry.Number, Status: clientproto.OutcomeDone, Agent: chatID}
				}
			case plain.Method == clientproto.MethodSend:
				var p SendParams
				if json.Unmarshal(plain.Params, &p) != nil || p.ID == "" || p.Text == "" || p.ClientID == "" {
					outcome = clientproto.Outcome{Number: entry.Number, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadCall}
				} else {
					if _, err := c.engine.Send(p.ID, p.Text, p.ClientID); err != nil {
						t.Fatal(err)
					}
					outcome = clientproto.Outcome{Number: entry.Number, Status: clientproto.OutcomeDone, Agent: p.ID}
				}
			default:
				outcome = clientproto.Outcome{Number: entry.Number, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonMethodDenied}
			}
		}
		c.watermark[client.ID] = entry.Number
		if err := c.runner.RecordOutcome(client.ID, outcome); err != nil {
			t.Fatal(err)
		}
		g.signedRequest(t, c.signer, http.MethodDelete, "/v1/clients/"+client.ID+"/queue/"+numStr, nil, http.StatusNoContent, nil)
	}
}

func (g *testRig) putSignedBoxSlot(t *testing.T, c *computerNode, client clientrun.Client, slotName string, atMs int64, payload []byte) {
	t.Helper()
	_, priv := c.ensureExecutorSecret(t)
	var phoneBox [ed25519.PublicKeySize]byte
	copy(phoneBox[:], client.BoxKey)
	nonce, err := clientproto.NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := clientproto.SealSlot(clientproto.SlotPlain{
		V:        clientproto.SlotVersion,
		Slot:     slotName,
		Client:   client.ID,
		Executor: c.dev.ID(),
		At:       atMs,
		Ver:      client.Ver,
		Payload:  payload,
	}, nonce, &phoneBox, &priv)
	if err != nil {
		t.Fatal(err)
	}
	g.signedRequest(t, c.signer, http.MethodPut, "/v1/clients/"+client.ID+"/slots/"+url.PathEscape(slotName), struct {
		Sealed []byte `json:"sealed"`
	}{Sealed: sealed}, http.StatusNoContent, nil)
}

// TestP1P2LinkPairingApprovalAndWrongCheckDigits verifies that a phone started
// with a link is approved by the real pair.Approver using the check digits the
// phone computed locally, opens its grant, learns the approving computer's card
// and a second computer's card without re-pairing, and that a wrong check number
// refused by ApproveClientChecked leaves the request pending and the phone with
// no grant or state (contract P1, P2).
func TestP1P2LinkPairingApprovalAndWrongCheckDigits(t *testing.T) {
	g := newTestRig(t)
	ctx := context.Background()
	compB := g.addComputer(t, "", "laptop-b")

	phone := g.newPhone(t, "abir-phone")
	setupLink := pair.SetupLink(g.url)
	link, err := phone.StartLink(ctx, setupLink)
	if err != nil {
		t.Fatalf("StartLink: %v", err)
	}
	if !link.FromSetup || link.SyncAddress != g.url {
		t.Fatalf("link = %+v, want FromSetup=true SyncAddress=%s", link, g.url)
	}

	ref, err := pair.ReadLink(link.Token)
	if err != nil {
		t.Fatalf("pair.ReadLink(%q): %v", link.Token, err)
	}
	asking, err := g.computer.approver.Look(ctx, ref)
	if err != nil {
		t.Fatalf("Approver.Look: %v", err)
	}
	if !asking.Client || asking.Name != "abir-phone" || asking.Check != link.CheckDigits {
		t.Fatalf("Asking = %+v, want Client=true Name=abir-phone Check=%s", asking, link.CheckDigits)
	}

	// A wrong check number is refused by ApproveClientChecked and leaves the request pending.
	wrongCheck := "0000"
	if link.CheckDigits == wrongCheck {
		wrongCheck = "0001"
	}
	if err := g.computer.approver.ApproveClientChecked(ctx, asking, wrongCheck); !errors.Is(err, pair.ErrCheckNumberMismatch) {
		t.Fatalf("ApproveClientChecked wrong digits = %v, want ErrCheckNumberMismatch", err)
	}
	shortCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = phone.AwaitGrant(shortCtx, link)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AwaitGrant while still pending = %v, want context.DeadlineExceeded", err)
	}
	if phone.Paired() {
		t.Fatal("phone reported Paired() == true after timed-out AwaitGrant")
	}
	if _, ok := phone.Grant(); ok {
		t.Fatal("phone kept a grant after timed-out AwaitGrant")
	}
	if _, ok := phone.KnownCard(g.computer.dev.ID()); ok {
		t.Fatal("phone kept a card after timed-out AwaitGrant")
	}

	// Approving with the phone's locally computed check digits succeeds.
	if err := g.computer.approver.ApproveClientChecked(ctx, asking, link.CheckDigits); err != nil {
		t.Fatalf("ApproveClientChecked matching digits: %v", err)
	}
	grant, err := phone.AwaitGrant(ctx, link)
	if err != nil {
		t.Fatalf("AwaitGrant after approval: %v", err)
	}
	if !phone.Paired() || grant.Device != phone.DeviceID() || grant.Code != link.Code {
		t.Fatalf("paired grant = %+v (paired=%v)", grant, phone.Paired())
	}
	cardA, ok := phone.KnownCard(g.computer.dev.ID())
	if !ok || cardA.Writer != g.computer.dev.ID() || cardA.Name != "desk" {
		t.Fatalf("KnownCard(A) = %+v (ok=%v)", cardA, ok)
	}

	// Both computer A and computer B publish their cards and snapshots; the phone reads both without pairing again (P1).
	g.clock.Advance(time.Second)
	if err := g.computer.runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err := compB.runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	computers, err := phone.Computers(ctx)
	if err != nil {
		t.Fatalf("Computers: %v", err)
	}
	if len(computers) != 2 {
		t.Fatalf("Computers len = %d (%+v), want 2", len(computers), computers)
	}
	fetchedA, err := phone.Card(ctx, g.computer.dev.ID())
	if err != nil || fetchedA.Name != "desk" {
		t.Fatalf("Card(A) = %+v (%v)", fetchedA, err)
	}
	fetchedB, err := phone.Card(ctx, compB.dev.ID())
	if err != nil || fetchedB.Name != "laptop-b" {
		t.Fatalf("Card(B) = %+v (%v)", fetchedB, err)
	}
	slotA, err := phone.Read(ctx, g.computer.dev.ID(), "asks.list")
	if err != nil || slotA.Executor != g.computer.dev.ID() {
		t.Fatalf("Read(A, asks.list) = %+v (%v)", slotA, err)
	}
	slotB, err := phone.Read(ctx, compB.dev.ID(), "asks.list")
	if err != nil || slotB.Executor != compB.dev.ID() {
		t.Fatalf("Read(B, asks.list) = %+v (%v)", slotB, err)
	}
}

// TestP3PhoneHoldsNoIdentitySecrets inspects every exported and unexported field
// of Phone via reflection after pairing and reading slots to verify that none of
// the identity's signing seed, identity private key, dedup secret, cell key,
// metadata key, or computer private keys appear in raw bytes, hex, or base64 (contract P3, L2).
func TestP3PhoneHoldsNoIdentitySecrets(t *testing.T) {
	g := newTestRig(t)
	ctx := context.Background()
	phone := g.newPhone(t, "pixel")

	link, err := phone.StartLink(ctx, g.url)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pair.ReadLink(link.Token)
	if err != nil {
		t.Fatal(err)
	}
	asking, err := g.computer.approver.Look(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.computer.approver.Approve(ctx, asking); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.AwaitGrant(ctx, link); err != nil {
		t.Fatal(err)
	}
	g.clock.Advance(time.Second)
	if err := g.computer.runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.Card(ctx, g.computer.dev.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.Read(ctx, g.computer.dev.ID(), "asks.list"); err != nil {
		t.Fatal(err)
	}

	idDoc, err := g.id.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var rawID struct {
		SigningSeed string `json:"signing_seed"`
		DedupSecret string `json:"dedup_secret"`
		CellKey     string `json:"cell_key"`
	}
	if err := json.Unmarshal(idDoc, &rawID); err != nil {
		t.Fatal(err)
	}
	seedBytes, err := hex.DecodeString(rawID.SigningSeed)
	if err != nil {
		t.Fatal(err)
	}
	_, execPriv := g.computer.ensureExecutorSecret(t)

	secrets := []struct {
		name string
		raw  []byte
	}{
		{"identity signing seed", seedBytes},
		{"identity dedup secret", g.id.DedupSecret()},
		{"identity cell key", g.id.CellKey()},
		{"directory metadata key", directory.MetadataKey(g.id.CellKey())},
		{"computer executor box private key", execPriv[:]},
	}

	var blobs [][]byte
	var texts []string
	collectValueBytes(reflect.ValueOf(phone), make(map[uintptr]bool), &blobs, &texts)
	if len(blobs) == 0 && len(texts) == 0 {
		t.Fatal("reflection walk collected no values from Phone")
	}

	for _, sec := range secrets {
		if len(sec.raw) == 0 {
			t.Fatalf("secret %s is empty", sec.name)
		}
		for _, b := range blobs {
			if bytes.Contains(b, sec.raw) {
				t.Fatalf("Phone byte field leaked raw %s", sec.name)
			}
		}
		encodings := []string{
			strings.ToLower(hex.EncodeToString(sec.raw)),
			base64.StdEncoding.EncodeToString(sec.raw),
			base64.RawStdEncoding.EncodeToString(sec.raw),
			base64.URLEncoding.EncodeToString(sec.raw),
			base64.RawURLEncoding.EncodeToString(sec.raw),
		}
		for _, enc := range encodings {
			encLower := strings.ToLower(enc)
			for _, s := range texts {
				if strings.Contains(s, enc) || strings.Contains(strings.ToLower(s), encLower) {
					t.Fatalf("Phone string field %q leaked %s (%s)", s, sec.name, enc)
				}
			}
			for _, b := range blobs {
				s := string(b)
				if strings.Contains(s, enc) || strings.Contains(strings.ToLower(s), encLower) {
					t.Fatalf("Phone byte buffer leaked encoded %s (%s)", sec.name, enc)
				}
			}
		}
	}
}

func collectValueBytes(v reflect.Value, visited map[uintptr]bool, blobs *[][]byte, texts *[]string) {
	if !v.IsValid() {
		return
	}
	if v.Type() == reflect.TypeFor[*http.Client]() || v.Type() == reflect.TypeFor[*http.Transport]() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return
		}
		if v.Kind() == reflect.Pointer {
			ptr := v.Pointer()
			if visited[ptr] {
				return
			}
			visited[ptr] = true
		}
		collectValueBytes(v.Elem(), visited, blobs, texts)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if !f.CanInterface() && f.CanAddr() {
				f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
			}
			collectValueBytes(f, visited, blobs, texts)
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			buf := make([]byte, v.Len())
			for i := 0; i < v.Len(); i++ {
				buf[i] = byte(v.Index(i).Uint())
			}
			*blobs = append(*blobs, buf)
			return
		}
		for i := 0; i < v.Len(); i++ {
			collectValueBytes(v.Index(i), visited, blobs, texts)
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			collectValueBytes(iter.Key(), visited, blobs, texts)
			collectValueBytes(iter.Value(), visited, blobs, texts)
		}
	case reflect.String:
		*texts = append(*texts, v.String())
	}
}

// TestP12GrantAuthenticationAndRefusalsKeepNoState verifies that a grant opened
// with another link key, a grant for another request's code, a grant naming
// another relay when the phone was set up from a setup link, and a grant whose
// certificate belongs to another phone are each refused with clientproto.ErrGrant
// and leave the phone with no state (contract P12).
func TestP12GrantAuthenticationAndRefusalsKeepNoState(t *testing.T) {
	g := newTestRig(t)
	ctx := context.Background()

	enrollAndApproveCustom := func(t *testing.T, phone *Phone, link Link, mutate func(*clientproto.ClientGrant, *[]byte)) {
		t.Helper()
		enroll := clientproto.Enrollment{
			V:       1,
			Device:  phone.DeviceID(),
			SignPub: phone.SignPublicKey(),
			BoxPub:  phone.boxPub[:],
			Methods: clientproto.PhoneMethods(),
			Reads:   clientproto.PhoneReads(),
			Ver:     1,
		}
		enroll.Sig = g.id.Sign(enroll.SignText())
		if err := g.computer.fc.PutEnrollment(ctx, enroll); err != nil {
			t.Fatal(err)
		}
		cert := identity.IssueCert(g.id, phone.SignPublicKey(), g.clock.Now())
		if err := g.computer.fc.RegisterClient(ctx, cert, clientproto.PhoneMethods(), ""); err != nil && !errors.Is(err, fabricclient.ErrExists) {
			t.Fatal(err)
		}
		grant := clientproto.ClientGrant{
			V:           clientproto.GrantVersion,
			Device:      phone.DeviceID(),
			Code:        link.Code,
			Cert:        cert,
			IdentityPub: g.id.PublicKey(),
			Relay:       g.url,
			Executors:   []clientproto.Card{g.computer.signedCard(t)},
		}
		sealKey := append([]byte(nil), link.Key...)
		mutate(&grant, &sealKey)
		nonce, err := clientproto.GrantNonce(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := clientproto.SealGrant(grant, nonce, sealKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := g.computer.dir.ApproveRequest(ctx, link.Code, directory.Approval{
			Class: clientproto.ClientLinkClass,
			Grant: b64u.EncodeToString(sealed),
		}); err != nil {
			t.Fatal(err)
		}
	}

	assertNoPhoneState := func(t *testing.T, phone *Phone) {
		t.Helper()
		if phone.Paired() {
			t.Fatal("phone marked paired after grant refusal")
		}
		if _, ok := phone.Grant(); ok {
			t.Fatal("phone kept grant after refusal")
		}
		if _, ok := phone.KnownCard(g.computer.dev.ID()); ok {
			t.Fatal("phone kept executor card after grant refusal")
		}
	}

	t.Run("grant_sealed_with_another_link_key", func(t *testing.T) {
		phone := g.newPhone(t, "phone-wrong-key")
		link, err := phone.StartLink(ctx, g.url)
		if err != nil {
			t.Fatal(err)
		}
		enrollAndApproveCustom(t, phone, link, func(_ *clientproto.ClientGrant, sealKey *[]byte) {
			other := make([]byte, clientproto.ClientLinkKeyBytes)
			for i := range other {
				other[i] = (*sealKey)[i] ^ 0xff
			}
			*sealKey = other
		})
		if _, err := phone.AwaitGrant(ctx, link); !errors.Is(err, clientproto.ErrGrant) {
			t.Fatalf("AwaitGrant wrong link key = %v, want ErrGrant", err)
		}
		assertNoPhoneState(t, phone)
	})

	t.Run("grant_for_another_request_code", func(t *testing.T) {
		phone := g.newPhone(t, "phone-wrong-code")
		link, err := phone.StartLink(ctx, g.url)
		if err != nil {
			t.Fatal(err)
		}
		enrollAndApproveCustom(t, phone, link, func(grant *clientproto.ClientGrant, _ *[]byte) {
			grant.Code = "zzzzzzzzzz"
		})
		if _, err := phone.AwaitGrant(ctx, link); !errors.Is(err, clientproto.ErrGrant) {
			t.Fatalf("AwaitGrant wrong request code = %v, want ErrGrant", err)
		}
		assertNoPhoneState(t, phone)
	})

	t.Run("grant_naming_another_relay_when_set_up_from_link", func(t *testing.T) {
		phone := g.newPhone(t, "phone-wrong-relay")
		setupLink := pair.SetupLink(g.url)
		link, err := phone.StartLinkFromSetup(ctx, setupLink)
		if err != nil {
			t.Fatal(err)
		}
		enrollAndApproveCustom(t, phone, link, func(grant *clientproto.ClientGrant, _ *[]byte) {
			grant.Relay = "https://other-relay.example.com"
		})
		_, err = phone.AwaitGrant(ctx, link)
		if !errors.Is(err, clientproto.ErrGrant) || !errors.Is(err, ErrGrantRelayMismatch) {
			t.Fatalf("AwaitGrant wrong relay = %v, want ErrGrant / ErrGrantRelayMismatch", err)
		}
		assertNoPhoneState(t, phone)
	})

	t.Run("grant_whose_certificate_is_another_phones", func(t *testing.T) {
		phone := g.newPhone(t, "phone-wrong-cert")
		otherPhone := g.newPhone(t, "other-phone")
		link, err := phone.StartLink(ctx, g.url)
		if err != nil {
			t.Fatal(err)
		}
		enrollAndApproveCustom(t, phone, link, func(grant *clientproto.ClientGrant, _ *[]byte) {
			grant.Cert = identity.IssueCert(g.id, otherPhone.SignPublicKey(), g.clock.Now())
		})
		if _, err := phone.AwaitGrant(ctx, link); !errors.Is(err, clientproto.ErrGrant) {
			t.Fatalf("AwaitGrant other phone cert = %v, want ErrGrant", err)
		}
		assertNoPhoneState(t, phone)
	})
}

// TestS10NeighbourSlotReplayAndWrongWriterRefused verifies that the phone refuses
// a replayed older slot (or card) and refuses a slot (or card) whose writer does
// not match the computer asked for (contract S10 neighbour, L3, L6).
func TestS10NeighbourSlotReplayAndWrongWriterRefused(t *testing.T) {
	g := newTestRig(t)
	ctx := context.Background()
	compB := g.addComputer(t, "", "laptop-b")
	phone := g.newPhone(t, "iphone")

	link, err := phone.StartLink(ctx, g.url)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pair.ReadLink(link.Token)
	if err != nil {
		t.Fatal(err)
	}
	asking, err := g.computer.approver.Look(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.computer.approver.Approve(ctx, asking); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.AwaitGrant(ctx, link); err != nil {
		t.Fatal(err)
	}

	g.clock.Advance(time.Second)
	if err := g.computer.runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err := compB.runner.Step(ctx); err != nil {
		t.Fatal(err)
	}

	compAID := g.computer.dev.ID()
	asksSlotName := "asks.list." + compAID
	phoneSig, _, err := phone.signer()
	if err != nil {
		t.Fatal(err)
	}

	// Capture the t1 sealed slot from the relay and read it on the phone.
	var rawSlotT1 struct {
		Sealed []byte `json:"sealed"`
	}
	g.signedRequest(t, phoneSig, http.MethodGet, "/v1/clients/"+phone.DeviceID()+"/slots/"+url.PathEscape(asksSlotName), nil, http.StatusOK, &rawSlotT1)
	plain1, err := phone.Read(ctx, compAID, "asks.list")
	if err != nil {
		t.Fatalf("first Read(asks.list): %v", err)
	}

	// Advance past WatchingFloor, mutate asks, publish t2 > t1, and read it on the phone.
	g.clock.Advance(clientproto.WatchingFloor + time.Second)
	g.computer.engine.mu.Lock()
	g.computer.engine.asks = []appwire.Ask{{ID: "ask-1", Agent: g.computer.engine.agents[0].ID}}
	g.computer.engine.mu.Unlock()
	if err := g.computer.runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	plain2, err := phone.Read(ctx, compAID, "asks.list")
	if err != nil || plain2.At <= plain1.At {
		t.Fatalf("second Read(asks.list) = %+v (%v), plain1.At=%d", plain2, err, plain1.At)
	}

	// Replaying the older t1 slot bytes into asks.list.<compA> is refused by the phone.
	g.signedRequest(t, g.computer.signer, http.MethodPut, "/v1/clients/"+phone.DeviceID()+"/slots/"+url.PathEscape(asksSlotName), struct {
		Sealed []byte `json:"sealed"`
	}{Sealed: rawSlotT1.Sealed}, http.StatusNoContent, nil)
	if _, err := phone.Read(ctx, compAID, "asks.list"); !errors.Is(err, clientproto.ErrSlot) || !errors.Is(err, ErrSlotReplay) {
		t.Fatalf("replayed older slot err = %v, want ErrSlot / ErrSlotReplay", err)
	}

	// Computer B writing into asks.list.<compA> (wrong relay writer) is refused by the phone.
	g.signedRequest(t, compB.signer, http.MethodPut, "/v1/clients/"+phone.DeviceID()+"/slots/"+url.PathEscape(asksSlotName), struct {
		Sealed []byte `json:"sealed"`
	}{Sealed: rawSlotT1.Sealed}, http.StatusNoContent, nil)
	if _, err := phone.Read(ctx, compAID, "asks.list"); !errors.Is(err, clientproto.ErrSlot) || !errors.Is(err, ErrSlotWriterMismatch) {
		t.Fatalf("wrong writer slot err = %v, want ErrSlot / ErrSlotWriterMismatch", err)
	}

	// A slot sealed by computer A whose inner Executor names computer B is also refused.
	clientView := g.computer.runner.Clients()[0]
	g.putSignedBoxSlot(t, g.computer, clientView, asksSlotName, plain2.At+1000, []byte(`[]`))
	// Now seal one with wrong inner executor.
	_, privA := g.computer.ensureExecutorSecret(t)
	phoneBox := phone.BoxPublicKey()
	nonce, err := clientproto.NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	forgedInnerExec, err := clientproto.SealSlot(clientproto.SlotPlain{
		V:        clientproto.SlotVersion,
		Slot:     asksSlotName,
		Client:   phone.DeviceID(),
		Executor: compB.dev.ID(),
		At:       plain2.At + 2000,
		Ver:      1,
		Payload:  []byte(`[]`),
	}, nonce, &phoneBox, &privA)
	if err != nil {
		t.Fatal(err)
	}
	g.signedRequest(t, g.computer.signer, http.MethodPut, "/v1/clients/"+phone.DeviceID()+"/slots/"+url.PathEscape(asksSlotName), struct {
		Sealed []byte `json:"sealed"`
	}{Sealed: forgedInnerExec}, http.StatusNoContent, nil)
	if _, err := phone.Read(ctx, compAID, "asks.list"); !errors.Is(err, clientproto.ErrSlot) || !errors.Is(err, ErrSlotWriterMismatch) {
		t.Fatalf("wrong inner executor slot err = %v, want ErrSlot / ErrSlotWriterMismatch", err)
	}

	// Card checks: first Card read of card.<compA> succeeds, replaying the same card
	// timestamp is refused, a card with an invalid control-character name is refused,
	// and a card written by computer B under card.<compA> is refused.
	card1, err := phone.Card(ctx, compAID)
	if err != nil {
		t.Fatalf("first Card(compA): %v", err)
	}
	if _, err := phone.Card(ctx, compAID); !errors.Is(err, clientproto.ErrCard) || !errors.Is(err, ErrCardReplay) {
		t.Fatalf("replayed card slot err = %v, want ErrCard / ErrCardReplay", err)
	}

	badNameCard := card1
	badNameCard.Name = "desk\x07"
	badNameCard.At = card1.At + 100
	badNameCard.Sig = g.computer.dev.Sign(badNameCard.SignText())
	sealedBadName, err := clientproto.SealCard(badNameCard, &phoneBox, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cardSlotA := "card." + compAID
	g.signedRequest(t, g.computer.signer, http.MethodPut, "/v1/clients/"+phone.DeviceID()+"/slots/"+url.PathEscape(cardSlotA), struct {
		Sealed []byte `json:"sealed"`
	}{Sealed: sealedBadName}, http.StatusNoContent, nil)
	if _, err := phone.Card(ctx, compAID); !errors.Is(err, clientproto.ErrCard) {
		t.Fatalf("invalid card name err = %v, want ErrCard", err)
	}

	g.signedRequest(t, compB.signer, http.MethodPut, "/v1/clients/"+phone.DeviceID()+"/slots/"+url.PathEscape(cardSlotA), struct {
		Sealed []byte `json:"sealed"`
	}{Sealed: sealedBadName}, http.StatusNoContent, nil)
	if _, err := phone.Card(ctx, compAID); !errors.Is(err, clientproto.ErrCard) || !errors.Is(err, ErrCardWriterMismatch) {
		t.Fatalf("wrong writer card err = %v, want ErrCard / ErrCardWriterMismatch", err)
	}
}

// TestCallsCreateSendAndExpiry verifies that Create makes one chat and a done
// outcome, a second Create with the same client_id makes no new chat, Send echoes
// client_id on the person's entry, a call past not_after becomes expired, and a
// revoked phone is refused with ErrRevoked (contract S1, S2, S4, S11, V1).
func TestCallsCreateSendAndExpiry(t *testing.T) {
	g := newTestRig(t)
	ctx := context.Background()
	phone := g.newPhone(t, "iphone")

	link, err := phone.StartLink(ctx, g.url)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pair.ReadLink(link.Token)
	if err != nil {
		t.Fatal(err)
	}
	asking, err := g.computer.approver.Look(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.computer.approver.Approve(ctx, asking); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.AwaitGrant(ctx, link); err != nil {
		t.Fatal(err)
	}
	g.clock.Advance(time.Second)
	g.stepComputer(t, g.computer)

	compID := g.computer.dev.ID()
	beforeAgents, err := g.computer.engine.ListAgents("")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Create on computer A makes exactly one chat and produces a done outcome.
	createRef, err := phone.Create(ctx, compID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if createRef.Number != 1 || createRef.ClientID == "" || createRef.NotAfter-createRef.At != clientproto.CreateLifetime.Milliseconds() {
		t.Fatalf("createRef = %+v", createRef)
	}
	createOut, err := phone.WaitOutcome(ctx, compID, createRef, func() {
		g.clock.Advance(time.Second)
		g.stepComputer(t, g.computer)
	})
	if err != nil {
		t.Fatalf("WaitOutcome(create): %v", err)
	}
	if createOut.Status != clientproto.OutcomeDone || createOut.Agent == "" {
		t.Fatalf("create outcome = %+v, want done with agent", createOut)
	}
	afterAgents, err := g.computer.engine.ListAgents("")
	if err != nil || len(afterAgents) != len(beforeAgents)+1 {
		t.Fatalf("agents count = %d, want %d", len(afterAgents), len(beforeAgents)+1)
	}
	createdChat := createOut.Agent

	// 2. Repeating Create with the same client_id under a new queue number makes no new chat.
	dupRef, err := phone.Call(ctx, compID, clientproto.MethodCreate, CreateParams{ClientID: createRef.ClientID}, CallOptions{})
	if err != nil {
		t.Fatalf("duplicate Create: %v", err)
	}
	if dupRef.Number != 2 {
		t.Fatalf("dupRef.Number = %d, want 2", dupRef.Number)
	}
	dupOut, err := phone.WaitOutcome(ctx, compID, dupRef, func() {
		g.clock.Advance(time.Second)
		g.stepComputer(t, g.computer)
	})
	if err != nil {
		t.Fatalf("WaitOutcome(dup create): %v", err)
	}
	if dupOut.Status != clientproto.OutcomeDone || dupOut.Agent != createdChat {
		t.Fatalf("dup create outcome = %+v, want done with %s", dupOut, createdChat)
	}
	afterDupAgents, _ := g.computer.engine.ListAgents("")
	if len(afterDupAgents) != len(afterAgents) {
		t.Fatalf("duplicate client_id created extra chat: got %d, want %d", len(afterDupAgents), len(afterAgents))
	}

	// 3. Send echoes the phone's client_id on the person's entry in agents.entries.
	sendRef, err := phone.Send(ctx, compID, createdChat, "hello from phone")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sendRef.Number != 3 || sendRef.ClientID == "" || sendRef.NotAfter-sendRef.At != clientproto.CallLifetime.Milliseconds() {
		t.Fatalf("sendRef = %+v", sendRef)
	}
	sendOut, err := phone.WaitOutcome(ctx, compID, sendRef, func() {
		g.clock.Advance(time.Second)
		g.stepComputer(t, g.computer)
	})
	if err != nil {
		t.Fatalf("WaitOutcome(send): %v", err)
	}
	if sendOut.Status != clientproto.OutcomeDone || sendOut.Agent != createdChat {
		t.Fatalf("send outcome = %+v", sendOut)
	}
	entriesSlot, err := phone.Read(ctx, compID, "agents.entries")
	if err != nil {
		t.Fatalf("Read(agents.entries): %v", err)
	}
	var envelopes []struct {
		Agent  string                `json:"agent"`
		Result appwire.EntriesResult `json:"result"`
	}
	if err := json.Unmarshal(entriesSlot.Payload, &envelopes); err != nil {
		t.Fatal(err)
	}
	foundEcho := false
	for _, env := range envelopes {
		if env.Agent != createdChat {
			continue
		}
		for _, entry := range env.Result.Entries {
			if entry.Text == "hello from phone" && entry.ClientID == sendRef.ClientID {
				foundEcho = true
			}
		}
	}
	if !foundEcho {
		t.Fatalf("agents.entries did not echo client_id %q: %s", sendRef.ClientID, entriesSlot.Payload)
	}

	// 4. A call whose not_after has passed before the computer drains it gets an expired outcome.
	expRef, err := phone.Call(ctx, compID, clientproto.MethodCreate, CreateParams{}, CallOptions{
		Lifetime: time.Second,
	})
	if err != nil {
		t.Fatalf("Call with short lifetime: %v", err)
	}
	g.clock.Advance(2 * time.Second)
	g.stepComputer(t, g.computer)
	expOut, err := phone.WaitOutcome(ctx, compID, expRef, nil)
	if err != nil {
		t.Fatalf("WaitOutcome(expired): %v", err)
	}
	if expOut.Status != clientproto.OutcomeExpired || expOut.Reason != clientproto.ReasonExpired {
		t.Fatalf("expired outcome = %+v, want status=expired reason=expired", expOut)
	}

	// 5. After the phone is revoked in the directory, phone.Revoked is true and requests fail with ErrRevoked (V1).
	if phone.Revoked(ctx) {
		t.Fatal("phone reported Revoked before revocation")
	}
	if err := g.computer.dir.Revoke(ctx, phone.DeviceID()); err != nil {
		t.Fatal(err)
	}
	if !phone.Revoked(ctx) {
		t.Fatal("phone did not report Revoked after directory revocation")
	}
	if _, err := phone.Computers(ctx); !errors.Is(err, ErrRevoked) {
		t.Fatalf("Computers after revoke = %v, want ErrRevoked", err)
	}
}

// TestPhoneDerivedExpiryAndComputerOutcomeWins verifies that WaitOutcome derives
// an expired outcome from the phone's own clock after not_after + ExpiryAllowance
// when no outcome has arrived from the computer, and that a computer outcome that
// arrives later wins over the phone's clock-derived expiry (design 3.6).
func TestPhoneDerivedExpiryAndComputerOutcomeWins(t *testing.T) {
	g := newTestRig(t)
	ctx := context.Background()
	phone := g.newPhone(t, "iphone")

	link, err := phone.StartLink(ctx, g.url)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pair.ReadLink(link.Token)
	if err != nil {
		t.Fatal(err)
	}
	asking, err := g.computer.approver.Look(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.computer.approver.Approve(ctx, asking); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.AwaitGrant(ctx, link); err != nil {
		t.Fatal(err)
	}
	g.clock.Advance(time.Second)
	if err := g.computer.runner.Step(ctx); err != nil {
		t.Fatal(err)
	}

	compID := g.computer.dev.ID()
	callRef, err := phone.Create(ctx, compID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Computer is offline (does not step), and the clock advances past not_after + ExpiryAllowance.
	derived, err := phone.WaitOutcome(ctx, compID, callRef, func() {
		g.clock.Advance(clientproto.CreateLifetime + ExpiryAllowance + time.Millisecond)
	})
	if err != nil {
		t.Fatalf("WaitOutcome phone-derived expiry: %v", err)
	}
	if derived.Number != callRef.Number || derived.Status != clientproto.OutcomeExpired || derived.Reason != clientproto.ReasonExpired {
		t.Fatalf("derived outcome = %+v, want expired/expired for #%d", derived, callRef.Number)
	}

	// Later, the computer publishes an outcome for that call number (it ran before its clock passed not_after);
	// the computer's outcome wins over the phone's derived expiry.
	clientView := g.computer.runner.Clients()[0]
	payload, err := clientproto.EncodeOutcomes([]clientproto.Outcome{{
		Number: callRef.Number,
		Status: clientproto.OutcomeDone,
		Agent:  "chat-from-computer",
	}})
	if err != nil {
		t.Fatal(err)
	}
	g.putSignedBoxSlot(t, g.computer, clientView, "outcomes."+compID, g.clock.Now().UnixMilli(), payload)

	later, err := phone.WaitOutcome(ctx, compID, callRef, nil)
	if err != nil {
		t.Fatalf("WaitOutcome after computer published outcome: %v", err)
	}
	if later.Status != clientproto.OutcomeDone || later.Agent != "chat-from-computer" {
		t.Fatalf("later outcome = %+v, want computer's done outcome to win", later)
	}
}
