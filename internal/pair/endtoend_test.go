package pair

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/relay"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// liveRelay starts a real relay and hands back its address.
func liveRelay(t *testing.T) (*relay.Server, string) {
	t.Helper()
	service := &relay.Server{}
	server := httptest.NewServer(service)
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)
	return service, server.URL
}

// tap sits between the clients and the relay and keeps a copy of every byte
// that crossed it, in both directions.
//
// IT IS THE STRONGEST FORM OF THE QUESTION "CAN THE RELAY READ THIS". Rather
// than asking the relay what it knows, this records everything that ever
// reached its socket — which is a superset of anything it could ever have known
// — and the test then asserts that the conversation is not in there.
type tap struct {
	address string

	mu   sync.Mutex
	seen bytes.Buffer
}

func newTap(t *testing.T, upstream string) *tap {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	host := strings.TrimPrefix(upstream, "http://")
	recorder := &tap{address: "http://" + listener.Addr().String()}
	go func() {
		for {
			near, err := listener.Accept()
			if err != nil {
				return
			}
			far, err := net.Dial("tcp", host)
			if err != nil {
				_ = near.Close()
				return
			}
			go func() {
				_, _ = io.Copy(io.MultiWriter(far, recorder), near)
				_ = far.Close()
			}()
			go func() {
				_, _ = io.Copy(io.MultiWriter(near, recorder), far)
				_ = near.Close()
			}()
		}
	}()
	return recorder
}

func (r *tap) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen.Write(b)
}

func (r *tap) bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte{}, r.seen.Bytes()...)
}

// ── the whole story, end to end ─────────────────────────────────────────────

// A MACHINE SERVES, A DEVICE PAIRS WITH A CODE, AND THEN A REAL SESSION
// CONVERSATION CROSSES THE INTERNET THROUGH A RELAY THAT CANNOT READ IT.
//
// This is the design doc's six lines, run: `aforge serve` on one side,
// `aforge chat --at <name>` on the other, with internal/remote's own Dial and
// Serve at the two ends and nothing between them but the tunnel.
func TestPairingThenAWholeConversationThroughARelayThatCannotReadIt(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	_, upstream := liveRelay(t)
	watched := newTap(t, upstream)
	t.Setenv(RelayEnv, watched.address)

	machine := aDevice(t)
	surface := aDevice(t)
	desk := &Desk{}
	devices := BookAt(filepath.Join(t.TempDir(), "devices.json"))
	machines := BookAt(filepath.Join(t.TempDir(), "machines.json"))

	// ── the machine that owns the work ──────────────────────────────────────
	served := make(chan error, 4)
	host := &Host{
		Service: watched.address,
		Device:  machine,
		Devices: devices,
		Desk:    desk,
		Open: func(tunnel io.ReadWriteCloser) {
			// THE TUNNEL GOES STRAIGHT INTO internal/remote WITH NO WIRE
			// CHANGE. That is the whole reason Serve was written against a
			// reader and a writer.
			served <- remote.Serve(tunnel, tunnel, remote.Options{
				Boot: func(hello remote.Hello) (*remote.Engine, error) {
					return &remote.Engine{
						Agent:       &stubAgent{},
						Workspace:   "/work/over/there",
						SessionFile: "/work/over/there/session.jsonl",
					}, nil
				},
			})
		},
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = host.Run(ctx) }()

	// The name is a fact about the key, so the surface can be told it without
	// waiting for anything.
	name := machine.Name()

	// ── the device that wants in ────────────────────────────────────────────
	var said []string
	reach := Reach{
		Name:     name,
		Device:   surface,
		Machines: machines,
		Label:    "laptop",
		Say:      func(line string) { said = append(said, line) },
		AskCode: func(string) (string, error) {
			// What a person reads off the other machine's screen.
			code, err := desk.Offer()
			if err != nil {
				return "", err
			}
			return code.Shown(), nil
		},
	}

	tunnel, err := openWhenReady(ctx, reach)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tunnel.Close() }()

	if len(said) < 3 || said[0] != PairingPreamble(name) || said[1] != PairingWeight || said[2] != PairedLine(name) {
		t.Fatalf("the pairing said %q", said)
	}

	client, err := remote.Dial(tunnel, name, remote.Hello{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if got := client.Welcome().Workspace; got != "/work/over/there" {
		t.Fatalf("the welcome came back as %q", got)
	}

	// A real turn: the words go over, the reply comes back.
	const secret = "the-plaintext-that-must-not-appear-on-the-relay"
	events, err := client.Agent().Submit(context.Background(), secret)
	if err != nil {
		t.Fatal(err)
	}
	var reply strings.Builder
	for event := range events {
		if event.Text != "" {
			reply.WriteString(event.Text)
		}
	}
	if !strings.Contains(reply.String(), secret) {
		t.Fatalf("the far end answered %q", reply.String())
	}

	// ── and the relay saw none of it ────────────────────────────────────────
	crossed := watched.bytes()
	if len(crossed) == 0 {
		t.Fatal("nothing crossed the relay, so this test proved nothing")
	}
	if bytes.Contains(crossed, []byte(secret)) {
		t.Fatal("the words of the conversation appeared in what crossed the relay")
	}
	// The frame envelope itself must not be there either: if `"kind":"call"`
	// were readable, so would be everything it carries.
	for _, tell := range []string{`"kind"`, `"method"`, `"workspace"`, "Submit", "/work/over/there"} {
		if bytes.Contains(crossed, []byte(tell)) {
			t.Fatalf("%q appeared in what crossed the relay", tell)
		}
	}
	// What the relay DOES know, which is the whole of what it knows.
	if !bytes.Contains(crossed, []byte(name)) {
		t.Fatal("the machine name should be visible to the relay — it is how a dial is matched")
	}
}

// openWhenReady retries the first connection while the machine is still walking
// out to the relay. The registration is a round trip and this test does not own
// its clock, so the alternative would be a sleep that is either flaky or slow.
func openWhenReady(ctx context.Context, reach Reach) (*Tunnel, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		tunnel, err := reach.Open(ctx)
		if err == nil {
			return tunnel, nil
		}
		if !strings.Contains(err.Error(), "not connected to the relay") || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A DEVICE THAT HAS BEEN STOPPED IS STOPPED AT THE MACHINE, NOT AT THE RELAY.
// Revocation is the engine machine's decision, and this is that decision being
// made after the pairing already happened.
func TestARevokedDeviceIsTurnedAwayByTheMachineItself(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	_, address := liveRelay(t)
	t.Setenv(RelayEnv, address)

	machine := aDevice(t)
	surface := aDevice(t)
	desk := &Desk{}
	devices := BookAt(filepath.Join(t.TempDir(), "devices.json"))
	machines := BookAt(filepath.Join(t.TempDir(), "machines.json"))

	host := &Host{
		Service: address,
		Device:  machine,
		Devices: devices,
		Desk:    desk,
		Open:    func(tunnel io.ReadWriteCloser) { <-time.After(time.Second); _ = tunnel.Close() },
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = host.Run(ctx) }()

	reach := Reach{
		Name:     machine.Name(),
		Device:   surface,
		Machines: machines,
		Label:    "laptop",
		AskCode: func(string) (string, error) {
			code, err := desk.Offer()
			if err != nil {
				return "", err
			}
			return code.Shown(), nil
		},
	}
	tunnel, err := openWhenReady(ctx, reach)
	if err != nil {
		t.Fatal(err)
	}
	_ = tunnel.Close()

	// The machine's own list, and the machine's own decision.
	list, err := devices.Devices()
	if err != nil || len(list) != 1 {
		t.Fatalf("the machine's book holds %v (%v)", list, err)
	}
	gone, err := devices.Revoke(list[0].Label)
	if err != nil {
		t.Fatal(err)
	}
	if gone.Key != list[0].Key {
		t.Fatal("the wrong device was stopped")
	}

	_, err = reach.Open(ctx)
	if err == nil {
		t.Fatal("a stopped device opened a connection")
	}
	if !strings.Contains(err.Error(), "has been stopped on that machine") {
		t.Fatalf("a stopped device was told %q", err)
	}
}

// ── a session agent that says what it was told ──────────────────────────────

// stubAgent is the smallest thing internal/remote will serve. It exists so that
// this test drives the REAL protocol — the real Dial, the real Serve, the real
// frames — rather than a paraphrase of it, without dragging a model in.
type stubAgent struct{}

func (s *stubAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "heard: " + text}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	return events, nil
}

func (s *stubAgent) SubmitStanding(ctx context.Context, text string) (<-chan session.Event, error) {
	return s.Submit(ctx, text)
}

func (s *stubAgent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	return s.Submit(ctx, text)
}

func (s *stubAgent) FollowUp(text string) (<-chan session.Event, error) {
	return s.Submit(context.Background(), text)
}

func (s *stubAgent) Interrupt()                          {}
func (s *stubAgent) Compact(ctx context.Context) error   { return errors.New("not here") }
func (s *stubAgent) Close() error                        { return nil }
func (s *stubAgent) Model() string                       { return "a-model" }
func (s *stubAgent) SetModel(string)                     {}
func (s *stubAgent) SetContextWindow(int)                {}
func (s *stubAgent) ReasoningFor(string) string          { return "" }
func (s *stubAgent) SetReasoningFor(string, string)      {}
func (s *stubAgent) ResolveConsent(uint64, bool)         {}
func (s *stubAgent) Title() string                       { return "" }
func (s *stubAgent) Usage() session.Usage                { return session.Usage{} }
func (s *stubAgent) ContextTokens() int                  { return 0 }
func (s *stubAgent) Transcript() []session.DisplayEntry  { return nil }
func (s *stubAgent) RewindPoints() []session.RewindPoint { return nil }
func (s *stubAgent) NoteConnected(string, string)        {}
func (s *stubAgent) ResolveConnect(string, bool)         {}
func (s *stubAgent) ResolveConnectKey(string, string)    {}
func (s *stubAgent) ResolveHarness(uint64, bool, string) {}
func (s *stubAgent) EarlierHistory() session.EarlierHistory {
	return session.EarlierHistory{}
}

func (s *stubAgent) ResolveConsentRemember(uint64, bool, session.ConsentScope) {}
func (s *stubAgent) ResolveStanding(uint64, session.StandingAnswer)            {}

func (s *stubAgent) RewindAt(int) ([]session.DisplayEntry, error) {
	return nil, errors.New("not here")
}

var _ remote.WrappedAgent = (*stubAgent)(nil)
var _ = standing.Item{}
