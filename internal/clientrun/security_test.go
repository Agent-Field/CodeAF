package clientrun_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/nacl/box"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func (g *rig) reopen(t *testing.T, tr http.RoundTripper, log func(clientrun.Receipt) error) {
	t.Helper()
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := clientrun.Open(clientrun.Config{Home: g.state, Relay: g.url, Signer: g.s, Directory: g.dir, Engine: g.e, HTTP: &http.Client{Transport: tr}, Now: func() time.Time { return g.now }, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g.r = r
}
func noopLog(clientrun.Receipt) error { return nil }
func TestQueueProofCannotBeAlteredByRelay(t *testing.T) {
	for _, field := range []string{"signature", "number", "at", "method", "identity", "request_uri", "sealed"} {
		t.Run(field, func(t *testing.T) {
			g := newRig(t)
			g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
			grant := g.pair(t, "asks.answer")
			g.enqueue(t, grant, 1, false)
			g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
				resp, err := http.DefaultTransport.RoundTrip(req)
				if err != nil || req.Method != "GET" || !strings.HasSuffix(req.URL.Path, "/queue") {
					return resp, err
				}
				defer resp.Body.Close()
				var result map[string][]map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
					return nil, err
				}
				entry := result["entries"][0]
				switch field {
				case "number":
					entry[field] = 2
				case "at":
					entry[field] = g.now.Add(-25 * time.Hour).UnixMilli()
				default:
					entry[field] = "tampered"
				}
				raw, _ := json.Marshal(result)
				resp.Body = io.NopCloser(bytes.NewReader(raw))
				return resp, nil
			}), noopLog)
			if err := g.r.Step(context.Background()); err == nil {
				t.Fatal("accepted altered proof")
			}
			if g.e.answers != 0 {
				t.Fatal("altered proof executed")
			}
		})
	}
}

func TestAckFailureAndRestartNeverExecuteTwice(t *testing.T) {
	g := newRig(t)
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, false)
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "DELETE" {
			return nil, errors.New("link cut after execution")
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("ack failure not reported")
	}
	if g.e.answers != 1 {
		t.Fatal("answer did not run")
	}
	g.reopen(t, nil, noopLog)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.answers != 1 {
		t.Fatal("restart repeated answer")
	}
}

func TestUncertainDispatchIsNeverRepeatedAfterRestart(t *testing.T) {
	g := newRig(t)
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, false)
	g.reopen(t, nil, func(clientrun.Receipt) error { return errors.New("chat log unavailable") })
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("missing log did not stop dispatch")
	}
	if g.e.answers != 0 {
		t.Fatal("ran without log")
	}
	g.reopen(t, nil, noopLog)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.answers != 0 || g.r.Receipts()[0].Status != "started" {
		t.Fatal("uncertain call was retried")
	}
}

func TestExpiredQueuedCallIsRejectedLocally(t *testing.T) {
	g := newRig(t)
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, false)
	signedAt := g.now
	g.now = g.now.Add(24 * time.Hour)
	// Keep transport authentication on the relay clock so this proves the
	// executor's offline-entry age check, rather than HTTP skew rejection.
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.GetBody != nil {
			reader, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			body, err = io.ReadAll(reader)
			reader.Close()
			if err != nil {
				return nil, err
			}
		}
		reqsign.Sign(req, body, g.s, signedAt)
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	if err := g.r.Step(context.Background()); err == nil || !strings.Contains(err.Error(), "queued call expired") {
		t.Fatalf("local age refusal = %v", err)
	}
	if g.e.answers != 0 {
		t.Fatal("stale call ran")
	}
}

func TestFullQueueCanMakeProgressPastOneMiBOfSignedProofs(t *testing.T) {
	g := newRig(t)
	grant := g.pair(t, "agents.create")
	payload, _ := json.Marshal(clientrun.Call{Method: "agents.create", Params: json.RawMessage(`{"text":"` + strings.Repeat("x", 3800) + `"}`)})
	sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for number := 1; number <= 128; number++ {
		g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{"number": number, "at": g.now.UnixMilli(), "method": "agents.create", "target": "any", "sealed": sealed}, nil, 204)
	}
	stopped := errors.New("stop after first acknowledgement")
	g.reopen(t, nil, func(receipt clientrun.Receipt) error {
		if receipt.Number == 2 {
			return stopped
		}
		return nil
	})
	if err := g.r.Step(context.Background()); !errors.Is(err, stopped) {
		t.Fatalf("full queue did not reach second entry: %v", err)
	}
	var queue struct{ Entries []json.RawMessage }
	g.request(t, g.s, "GET", "/v1/clients/"+g.client.dev.ID()+"/queue", nil, &queue, 200)
	if g.e.created != 1 || len(queue.Entries) != 127 {
		t.Fatalf("executed %d; remaining %d", g.e.created, len(queue.Entries))
	}
}
