package pairboxtest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// wire is raw HTTP to the rig's relay, for the cases that must see bytes the
// Box client would hide: a body a client decodes away, a route it never calls.
type wire struct {
	base string
	peer string
	hc   *http.Client
}

// wireOf is the raw wire of a rig, skipping the case when the rig has none.
func (e env) wireOf(t *testing.T) wire {
	t.Helper()
	if e.URL == "" {
		t.Skip("this rig cannot be spoken to as raw HTTP")
	}
	return wire{base: e.URL, peer: e.peer, hc: &http.Client{Timeout: time.Minute}}
}

// do sends one request as the case's network and answers status and body.
func (w wire) do(t *testing.T, method, path string, key *pairbox.Key, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, w.base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(pairbox.PeerHeader, w.peer)
	if key != nil {
		req.Header.Set(pairbox.KeyHeader, key.String())
	}
	resp, err := w.hc.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: reading the answer: %v", method, path, err)
	}
	return resp.StatusCode, out
}

// create makes a mailbox over the wire and answers it with the body the relay
// sent, which the caller may inspect.
func (w wire) create(t *testing.T, e env) (mailbox, []byte) {
	t.Helper()
	key := newKey(t)
	status, body := w.do(t, http.MethodPost, pairbox.Path, &key, nil)
	if status != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", pairbox.Path, status, body)
	}
	var made struct {
		Nameplate string `json:"nameplate"`
	}
	if err := json.Unmarshal(body, &made); err != nil {
		t.Fatalf("create answered %q: %v", body, err)
	}
	e.forget(t, e.Box, made.Nameplate, key)
	return mailbox{made.Nameplate, key}, body
}
