package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── A PIN THE WIRE REFUSES IS RETIRED WHERE THE PERSON CAN SEE IT ───────────
//
// THE MEASURED FAILURE (2026-09-13, a real drive against the live router). A
// conversation on `routing: simple`, pinned to a machine the account's own
// settings exclude, sent the demand, collected the router's 404, retired the
// pin and answered from another machine — and said nothing. The status line
// dropped `@deepseek` between one turn and the next and no sentence anywhere
// explained why, which is the exact complaint the mode exists to close.
//
// THE PROOF IS AT THIS LAYER AND NOT AT THE TRANSPORT'S, because the transport
// already had a test of its own that passed. What it could not see is the rest
// of the road the sentence has to travel: the turn's own observer, the loop
// that reads the stream, the events a surface is handed. A fact that is emitted
// into a channel nobody in the product is listening on is a fact nobody is
// told.
func TestAPinTheWireRefusesTellsTheConversationSo(t *testing.T) {
	// NO TEST WRITES THE REAL HOME: the lane registry's store lives under it.
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)
	ledger := filepath.Join(state, "v3", "usage.jsonl")
	writerEntered := make(chan struct{})
	releaseWriter := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWriter) }) }
	var enteredOnce sync.Once
	writer := &usageWriter{queue: make(chan usageWrite, usageQueueDepth), beforeWrite: func() {
		// beforeWrite runs on EVERY write, so the entered signal is closed once;
		// a second row through this writer must not close a closed channel.
		enteredOnce.Do(func() { close(writerEntered) })
		<-releaseWriter
	}}
	usageWritersMu.Lock()
	usageWriters[ledger] = writer
	usageWritersMu.Unlock()
	go writer.run(ledger)
	t.Cleanup(func() {
		release()
		FlushUsage()
		usageWritersMu.Lock()
		delete(usageWriters, ledger)
		usageWritersMu.Unlock()
	})
	const model = "stub/talk"
	// THE PINNED MACHINE IS ONE THE ROUTER DOES NOT SERVE FOR THIS MODEL, which
	// is the measured shape: the router publishes its serving set, the machine a
	// person pinned is not in it, and the demand earns `…but your request's
	// provider.only preference permits only: deepseek`.
	server := lanestub.New(model, lanestub.Lane{Name: "StreamLake", Profile: lanestub.Profile{Tokens: 3}})
	t.Cleanup(server.Close)
	// The stub publishes an endpoints page and names its lane on every answer,
	// so it really is a router and saying so is stating a fact about the
	// fixture (internal/provider's hedge_test.go makes the same statement).
	lanes.HeardPrefsCarried(server.URL())

	provider.RepinLane(provider.LanePin{Lane: "DeepSeek"})
	t.Cleanup(func() { provider.RepinLane(provider.LanePin{}) })

	agent, err := New(Config{
		Workspace: t.TempDir(),
		Model:     model,
		Routing:   provider.RoutingSimple,
		Sources: modelsource.NewSet(modelsource.Connected{
			Source:  modelsource.DefaultSource(server.URL()),
			Key:     "test-key",
			Address: server.URL(),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	said, other := turnLines(t, agent, "say only the word ok")
	want := provider.RetiredPinLine("DeepSeek")
	// AND IT ARRIVES AS NEWS ABOUT THE PERSON'S OWN ROW rather than as one more
	// line about the request's shape, which is the difference a surface acts on:
	// a notice is folded into the work chip once the answer lands and this is
	// the only account they get of why the machine they named disappeared
	// (session.go's [EventRowNews]).
	if !saidLine(said, want) {
		t.Fatalf("the pin was retired without telling anybody.\nwant a line reading %q\ngot row news %q and notices %q",
			want, said, other)
	}
	// AND THE DEMAND REALLY WENT OUT FIRST. Without this the test would pass on
	// a build that never asked for the machine at all, which is the other way
	// of breaking the same promise.
	if asked := server.Requests("DeepSeek"); asked != 1 {
		t.Fatalf("DeepSeek was demanded %d times, want exactly the one refusal that taught us", asked)
	}
	if served := server.Served(); len(served) == 0 {
		t.Fatal("nothing answered the turn, so the widened retry never landed")
	}

	<-writerEntered
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	release()
	FlushUsage()
	if _, err := os.Stat(ledger); err != nil {
		t.Fatalf("usage flush returned before its product-owned writer created %s: %v", ledger, err)
	}
}

// AND THE ERRANDS BESIDE THE TURN NEVER PAY FOR IT. The row is `lane.talk` and
// the slot is its whole scope (internal/provider's lanes.go): one refused round
// trip a pairing belongs to the person's own turn, and a title, a memory reflex
// or a route question demanding somebody's pinned machine on a model of its own
// is a second and a third 404 nobody asked for. Measured the same afternoon:
// one turn, three demands, three refusals, two of them errands.
func TestAnErrandBesideTheTurnDoesNotCarryTheTalkPin(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	const model = "stub/talk"
	server := lanestub.New(model, lanestub.Lane{Name: "StreamLake", Profile: lanestub.Profile{Tokens: 3}})
	t.Cleanup(server.Close)
	lanes.HeardPrefsCarried(server.URL())
	provider.RepinLane(provider.LanePin{Lane: "DeepSeek"})
	t.Cleanup(func() { provider.RepinLane(provider.LanePin{}) })

	agent, err := New(Config{
		Workspace: t.TempDir(),
		Model:     model,
		Routing:   provider.RoutingSimple,
		Sources: modelsource.NewSet(modelsource.Connected{
			Source:  modelsource.DefaultSource(server.URL()),
			Key:     "test-key",
			Address: server.URL(),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	for _, ask := range server.Asks() {
		if len(ask.Only) > 0 {
			t.Fatalf("a request demanded %v before the person had asked anything", ask.Only)
		}
	}
	turnLines(t, agent, "say only the word ok")
	if asked := server.Requests("DeepSeek"); asked != 1 {
		t.Fatalf("DeepSeek was demanded %d times over one turn, want the turn's own single ask", asked)
	}
}

// turnLines runs one turn and hands back the two kinds of line a person reads
// about it, apart: the news about their own rows, and the notices the adapter
// writes about the request's shape. Two slices rather than one because which
// channel a sentence arrived on is the thing under test.
func turnLines(t *testing.T, agent *Agent, text string) (rowNews, notices []string) {
	t.Helper()
	ctx, cancel := deadline(20 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, text)
	if err != nil {
		t.Fatalf("Submit(%q): %v", text, err)
	}
	for event := range events {
		switch event.Kind {
		case EventError:
			t.Fatalf("turn errored: %v", event.Err)
		case EventRowNews:
			rowNews = append(rowNews, event.Text)
		case EventNotice:
			notices = append(notices, event.Text)
		}
	}
	return rowNews, notices
}

func saidLine(said []string, want string) bool {
	for _, line := range said {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}
