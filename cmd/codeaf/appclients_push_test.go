package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"golang.org/x/crypto/nacl/box"
)

// Measure delivery through the desktop emitter, its normal stream throttle,
// the running publisher and the relay HTTP interface. Timings are reported,
// not used as a machine-dependent performance gate.
func TestPhoneTokenToLongPollLatency(t *testing.T) {
	h := newStartHarness(t)
	agent, err := h.b.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	relay := relayserve.New(relayserve.Config{Store: t.TempDir()})
	var putAt atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/slots/") {
			putAt.Store(time.Now().UnixNano())
		}
		relay.Handler.ServeHTTP(w, r)
	}))
	defer relay.Close()
	defer srv.Close()
	root := t.TempDir()
	id, err := identity.Ensure(root)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(root)
	if err != nil {
		t.Fatal(err)
	}
	full := appClientSigner{id, dev}
	dir := directory.NewHTTP(srv.URL, reqsign.SignFor(full, time.Now), srv.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = dir.PutDevice(ctx, dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = dir.Create(ctx, agent.ID, directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	runner, err := clientrun.Open(clientrun.Config{Home: root, Relay: srv.URL, Signer: full, Directory: dir, Engine: appClientEngine{h.b}, HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	h.b.clientRunner.Store(runner)
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := identity.IssueCert(id, seed.Public(), time.Now())
	if _, err = runner.Pair(ctx, clientrun.Pairing{Cert: cert, BoxKey: pub[:], Reads: []string{"agents.entries"}}); err != nil {
		t.Fatal(err)
	}
	clientDev, err := seed.Join(cert)
	if err != nil {
		t.Fatal(err)
	}
	client := scopedSigner{id.PublicKey(), clientDev}
	if err = runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); runner.Run(ctx) }()
	defer func() { cancel(); <-done }()
	var version uint64
	var changed struct {
		Version uint64
		Slots   []string
	}
	path := "/v1/clients/" + clientDev.ID() + "/slots"
	clientWire(t, srv.URL, client, "GET", path+"?since=0&wait=0", nil, &changed)
	version = changed.Version
	var firstToken, steadyStream []time.Duration
	var tokenToPut, putToReturn []time.Duration
	for i := range 40 {
		if i%2 == 0 {
			h.b.clientStream.clear(agent.ID)
		}
		text := strings.Repeat("x", i%2+1)
		started := time.Now()
		h.b.emit.Emit(appwire.EventTurnDelta, appwire.TurnDelta{Agent: agent.ID, Turn: uint64(i/2 + 1), Kind: appwire.DeltaText, Text: "x"})
		clientWire(t, srv.URL, client, "GET", fmt.Sprintf("%s?since=%d&wait=25", path, version), nil, &changed)
		returned := time.Now()
		if i%2 == 0 {
			firstToken = append(firstToken, returned.Sub(started))
		} else {
			steadyStream = append(steadyStream, returned.Sub(started))
		}
		put := time.Unix(0, putAt.Load())
		tokenToPut = append(tokenToPut, put.Sub(started))
		putToReturn = append(putToReturn, returned.Sub(put))
		version = changed.Version
		var slot struct{ Sealed []byte }
		clientWire(t, srv.URL, client, "GET", path+"/"+clientrun.SlotName("agents.entries", dev.ID()), nil, &slot)
		plain, ok := box.OpenAnonymous(nil, slot.Sealed, pub, priv)
		if !ok {
			t.Fatal("cannot decrypt")
		}
		var rows []struct{ Result appwire.EntriesResult }
		if err = json.Unmarshal(plain, &rows); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			for _, entry := range row.Result.Entries {
				if entry.Kind == appwire.EntryAnswer && entry.Text == text {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("token %d missing from notified snapshot: %s", i, plain)
		}
	}
	for _, group := range []struct {
		name    string
		samples []time.Duration
	}{{"FIRST_TOKEN_TO_LONG_POLL", firstToken}, {"STEADY_STREAM_TO_LONG_POLL", steadyStream}, {"TOKEN_TO_PUT", tokenToPut}, {"PUT_TO_LONG_POLL", putToReturn}} {
		sort.Slice(group.samples, func(i, j int) bool { return group.samples[i] < group.samples[j] })
		n := len(group.samples)
		t.Logf("%s n=%d p50=%s p95=%s min=%s max=%s", group.name, n, group.samples[(n-1)/2], group.samples[(n*95-1)/100], group.samples[0], group.samples[n-1])
	}
}
