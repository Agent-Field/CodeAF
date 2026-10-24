package appwire

import (
	"encoding/json"
	"testing"
)

type pairedEngine struct {
	fakeEngine
	paused  bool
	removed bool
}

func (e *pairedEngine) ListPairedClients() ([]PairedClient, error) {
	return []PairedClient{{ID: "client", Paused: e.paused}}, nil
}
func (e *pairedEngine) PausePairedClient(id string, paused bool) error     { e.paused = paused; return nil }
func (e *pairedEngine) UnpairClient(id string) error                       { e.removed = true; return nil }
func (e *pairedEngine) PairClient(p PairClientParams) (ClientGrant, error) { return ClientGrant{}, nil }
func TestPairedClientsAreManagedThroughTheDesktopTable(t *testing.T) {
	e := &pairedEngine{}
	if _, err := Call(e, "clients.pause", json.RawMessage(`{"id":"client","paused":true}`)); err != nil {
		t.Fatal(err)
	}
	result, err := Call(e, "clients.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	var got struct{ Clients []PairedClient }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Clients) != 1 || !got.Clients[0].Paused {
		t.Fatalf("%s", raw)
	}
	if _, err := Call(e, "clients.unpair", json.RawMessage(`{"id":"client"}`)); err != nil {
		t.Fatal(err)
	}
	if !e.removed {
		t.Fatal("unpair did not reach engine")
	}
	if _, err := Call(&fakeEngine{}, "clients.list", nil); err == nil {
		t.Fatal("old engine advertised clients")
	}
}
