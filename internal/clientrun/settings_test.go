package clientrun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// TestRunnerBacksOffFailuresUsingItsClock verifies that Runner.Run backs off
// failed rounds exponentially using its injected refresh clock (contract S10).
func TestRunnerBacksOffFailuresUsingItsClock(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")
	durations := make(chan time.Duration, 4)
	ticks := make(chan time.Time)
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
		Now:       g.clock.Now,
		HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("offline")
		})},
		After: func(d time.Duration) <-chan time.Time {
			durations <- d
			return ticks
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); r.Run(ctx) }()
	for _, want := range []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second} {
		select {
		case got := <-durations:
			if got != want {
				t.Fatalf("backoff = %v; want %v", got, want)
			}
		case <-ctx.Done():
			t.Fatal("runner never scheduled its next round")
		}
		if want != 40*time.Second {
			select {
			case ticks <- g.clock.Now():
			case <-ctx.Done():
				t.Fatal("runner did not wait on injected clock")
			}
		}
	}
	cancel()
	<-done
}

// TestRunnerStateExcludesAnotherLiveProcess verifies that a second runner on
// the same home refuses with a typed LockedError (contract U4).
func TestRunnerStateExcludesAnotherLiveProcess(t *testing.T) {
	if path := os.Getenv("CODEAF_TEST_RUNNER_LOCK_HOME"); path != "" {
		_, err := Open(Config{Home: path, Relay: "https://relay.example"})
		var lockedErr *LockedError
		if err == nil || !errors.Is(err, ErrLocked) || !errors.As(err, &lockedErr) || !strings.Contains(err.Error(), "already open") {
			t.Fatalf("second process opened runner state: %v", err)
		}
		return
	}
	g := newRig(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerStateExcludesAnotherLiveProcess$")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_RUNNER_LOCK_HOME="+g.state)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("exclusive runner state: %v\n%s", err, out)
	}
}

// TestPausedClientDoesNoReadsAndUnpairRevokesItsKey verifies that local Pause
// suspends snapshot reads for that client in runner.json, and Unpair revokes at
// the relay and writes a local tombstone (contract V1).
func TestPausedClientDoesNoReadsAndUnpairRevokesItsKey(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Pause(g.client.dev.ID(), true); err != nil {
		t.Fatal(err)
	}
	readsBefore := g.e.reads
	g.clock.Advance(time.Minute)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.reads != readsBefore {
		t.Fatal("paused client read snapshots")
	}
	list := g.r.Clients()
	if len(list) != 1 || !list[0].Paused {
		t.Fatalf("clients: %+v", list)
	}
	if err := g.r.Pause(g.client.dev.ID(), false); err != nil {
		t.Fatal(err)
	}
	g.e.asks = []appwire.Ask{{ID: "resumed", Agent: chat}}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.reads == readsBefore {
		t.Fatal("resumed client did not read")
	}
	if err := g.r.Unpair(context.Background(), g.client.dev.ID()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 0 {
		t.Fatal("unpaired client still listed")
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("asks.list", g.s.dev.ID()), nil, nil, 401)
	g.reopen(t, nil, noopLog)
	if len(g.r.Clients()) != 0 {
		t.Fatal("restart restored revoked pairing")
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 0 {
		t.Fatal("Step after restart re-admitted tombstoned client")
	}
}

// TestOpeningLegacyHistoryRetainsOnlyReplayWatermarks verifies that opening a
// runner.json with legacy receipt history logs completed receipts and compacts
// them to one watermark per client (contract S3).
func TestOpeningLegacyHistoryRetainsOnlyReplayWatermarks(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	path := StatePath(g.state)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	receipts := map[string]Receipt{}
	for n := int64(1); n <= 512; n++ {
		receipts[g.client.dev.ID()+"/"+strconv.FormatInt(n, 10)] = Receipt{
			Client: g.client.dev.ID(),
			Number: n,
			Method: "asks.answer",
			Agent:  chat,
			Status: "done",
		}
	}
	fixture["receipts"], _ = json.Marshal(receipts)
	raw, _ = json.Marshal(fixture)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var archived int
	g.reopen(t, nil, func(Receipt) error { archived++; return nil })
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
