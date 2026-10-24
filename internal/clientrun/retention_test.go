package clientrun_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
)

func TestAcknowledgedReceiptsStayBoundedAndUnpairRemovesThem(t *testing.T) {
	g := newRig(t)
	grant := g.pair(t, "asks.answer")
	var original struct {
		Entries []json.RawMessage `json:"entries"`
	}
	for n := int64(1); n <= 16; n++ {
		g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
		g.enqueue(t, grant, n, false)
		if n == 1 {
			g.request(t, g.s, "GET", "/v1/clients/"+g.client.dev.ID()+"/queue", nil, &original, 200)
		}
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	receipts := g.r.Receipts()
	if len(receipts) != 1 || receipts[0].Number != 16 {
		t.Errorf("acked history retained %d receipts; want only watermark 16", len(receipts))
	}
	g.reopen(t, nil, noopLog)
	if len(g.r.Receipts()) != 1 {
		t.Error("restart loaded unbounded acknowledged history")
	}
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
	g.enqueue(t, grant, 17, false)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.answers != 17 {
		t.Fatalf("watermark lost sequence: %d answers", g.e.answers)
	}
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil || req.Method != "GET" || !strings.HasSuffix(req.URL.Path, "/queue") {
			return resp, err
		}
		resp.Body.Close()
		raw, _ := json.Marshal(original)
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return resp, nil
	}), noopLog)
	if err := g.r.Step(context.Background()); err == nil {
		t.Error("trimmed old signed proof was replayed")
	}
	if g.e.answers != 17 {
		t.Fatal("old proof repeated an effect")
	}
	g.reopen(t, nil, noopLog)
	if err := g.r.Unpair(context.Background(), g.client.dev.ID()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Receipts()) != 0 {
		t.Error("unpair retained client receipts")
	}
	g.reopen(t, nil, noopLog)
	if len(g.r.Receipts()) != 0 {
		t.Error("unpair restored receipts on restart")
	}
}

func TestOpeningLegacyHistoryRetainsOnlyReplayWatermarks(t *testing.T) {
	g := newRig(t)
	g.pair(t, "asks.answer")
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	// Seed the public state-file seam with a pre-trimming installation's history.
	path := clientrun.StatePath(g.state)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	receipts := map[string]clientrun.Receipt{}
	for n := int64(1); n <= 512; n++ {
		receipts[g.client.dev.ID()+"/"+strconv.FormatInt(n, 10)] = clientrun.Receipt{Client: g.client.dev.ID(), Number: n, Method: "asks.answer", Agent: chat, Status: "done"}
	}
	fixture["receipts"], _ = json.Marshal(receipts)
	raw, _ = json.Marshal(fixture)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var archived int
	g.reopen(t, nil, func(clientrun.Receipt) error { archived++; return nil })
	got := g.r.Receipts()
	if len(got) != 1 || got[0].Number != 512 {
		t.Errorf("loaded %d legacy receipts; want watermark 512", len(got))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 4096 {
		t.Errorf("legacy state stayed unbounded: %d bytes", info.Size())
	}
	if archived != 512 {
		t.Errorf("archived %d legacy chat lines; want 512", archived)
	}
}
