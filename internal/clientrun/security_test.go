package clientrun_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func (g *rig) reopen(t *testing.T, tr http.RoundTripper, log func(clientrun.Receipt) error) {
	t.Helper()
	r, err := clientrun.Open(clientrun.Config{Home: g.state, Relay: g.url, Signer: g.s, Directory: g.dir, Engine: g.e, HTTP: &http.Client{Transport: tr}, Now: func() time.Time { return g.now }, Log: log})
	if err != nil {
		t.Fatal(err)
	}
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
	g.now = g.now.Add(24 * time.Hour)
	g.reopen(t, nil, noopLog)
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("stale call accepted locally")
	}
	if g.e.answers != 0 {
		t.Fatal("stale call ran")
	}
}
