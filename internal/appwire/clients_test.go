package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

type pairedEngine struct {
	fakeEngine
	paused  bool
	removed bool
	lastAsk CreateAsk
}

func (e *pairedEngine) ListPairedClients() ([]PairedClient, error) {
	return []PairedClient{{
		ID:      "dev_phone",
		Name:    "Phone",
		Methods: []string{"agents.create", "agents.send"},
		Reads:   []string{"agents.list"},
		Paused:  e.paused,
	}}, nil
}

func (e *pairedEngine) PausePairedClient(id string, paused bool) error {
	e.paused = paused
	return nil
}

func (e *pairedEngine) UnpairClient(id string) error {
	e.removed = true
	return nil
}

func (e *pairedEngine) CreateAgent(ask CreateAsk) (Agent, error) {
	e.lastAsk = ask
	return Agent{ID: "a1", State: StateIdle}, nil
}

// TestPairedClientsAreManagedThroughTheDesktopTable verifies that engines
// implementing PairedClientManager advertise clients.manage and serve
// clients.list, clients.pause, and clients.unpair, that clients.pair is not on
// the wire, and that clients.list carries no key or secret.
func TestPairedClientsAreManagedThroughTheDesktopTable(t *testing.T) {
	e := &pairedEngine{}
	w := startWireOver(t, e, &e.fakeEngine)
	foundFeature := false
	for _, f := range features(w.next()) {
		if f == "clients.manage" {
			foundFeature = true
		}
	}
	if !foundFeature {
		t.Fatal("hello did not advertise clients.manage for PairedClientManager")
	}

	if _, err := Call(e, "clients.pause", json.RawMessage(`{"id":"dev_phone","paused":true}`)); err != nil {
		t.Fatal(err)
	}
	result, err := Call(e, "clients.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"key", "secret", "seed", "box_pub", "sign_pub", "cert"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Fatalf("clients.list leaked forbidden field %q in %s", forbidden, raw)
		}
	}
	var got struct {
		Clients []map[string]any `json:"clients"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Clients) != 1 || got.Clients[0]["id"] != "dev_phone" || got.Clients[0]["paused"] != true {
		t.Fatalf("clients.list = %s", raw)
	}
	for k := range got.Clients[0] {
		switch k {
		case "id", "name", "methods", "reads", "paused":
		default:
			t.Fatalf("unexpected field %q in clients.list row: %s", k, raw)
		}
	}

	if _, err := Call(e, "clients.unpair", json.RawMessage(`{"id":"dev_phone"}`)); err != nil {
		t.Fatal(err)
	}
	if !e.removed {
		t.Fatal("unpair did not reach engine")
	}
	if _, err := Call(e, "clients.pair", json.RawMessage(`{}`)); err == nil {
		t.Fatal("clients.pair should not exist on the wire")
	}
	if _, err := Call(&fakeEngine{}, "clients.list", nil); err == nil {
		t.Fatal("plain engine without PairedClientManager served clients.list")
	}
}

// TestCreateHooksCannotBeSetFromJSON verifies that CreateAsk.BeforeMint and
// CreateAsk.BeforeStart carry json:"-" so a wire caller cannot supply or fail
// an executable hook through JSON, while in-process callers can set them.
func TestCreateHooksCannotBeSetFromJSON(t *testing.T) {
	var ask CreateAsk
	if err := json.Unmarshal([]byte(`{"workspace":"/tmp","before_mint":"boom","BeforeMint":"boom","before_start":"boom","BeforeStart":"boom"}`), &ask); err != nil {
		t.Fatalf("json.Unmarshal failed on ignored hook keys: %v", err)
	}
	if ask.BeforeMint != nil || ask.BeforeStart != nil {
		t.Fatal("executable hooks were populated from JSON")
	}

	e := &pairedEngine{}
	if _, err := Call(e, "agents.create", json.RawMessage(`{"workspace":"/tmp","before_mint":"boom","before_start":"boom"}`)); err != nil {
		t.Fatalf("agents.create failed: %v", err)
	}
	if e.lastAsk.BeforeMint != nil || e.lastAsk.BeforeStart != nil {
		t.Fatal("wire Call populated BeforeMint or BeforeStart")
	}

	// Verify in-process hooks on CreateAsk and AskAnswer are assignable and callable.
	var mintCalled, startCalled, resolveCalled bool
	inProc := CreateAsk{
		BeforeMint:  func(string) error { mintCalled = true; return nil },
		BeforeStart: func(Agent) error { startCalled = true; return nil },
	}
	_ = inProc.BeforeMint("/tmp/transcript.jsonl")
	_ = inProc.BeforeStart(Agent{ID: "a1"})
	ans := AskAnswer{
		BeforeResolve: func([]Ask) error { resolveCalled = true; return nil },
	}
	_ = ans.BeforeResolve(nil)
	if !mintCalled || !startCalled || !resolveCalled {
		t.Fatalf("in-process hooks did not run: mint=%v start=%v resolve=%v", mintCalled, startCalled, resolveCalled)
	}
}
