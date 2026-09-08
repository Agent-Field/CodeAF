package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/trace"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestMain switches the model-call log OFF for this package by default and
// moves the whole state root somewhere disposable.
//
// The log is always on in the product, which means a test binary that says
// nothing about it appends a row per simulated call into the developer's own
// ~/.aforge. The tests in this file switch it back on, pointed at a temporary
// file of their own.
//
// AND THE SAME IS NOW TRUE OF THE BELIEF FILE. `internal/lane`'s ledger writes
// every sighting through a store under [lane.StorePath], which resolves under
// AFORGE_HOME on every call — so a package whose tests stream simulated answers
// through a client was folding lanes called "quicksilver" into the belief file
// of whoever ran the tests, and reading them back on the next run. The symptom
// was a first request arriving with an `order` it could not have learned yet,
// which is the pollution and the test failure in one. A home per test binary
// ends the first half of that; [forgetLanes] ends the second.
func TestMain(m *testing.M) {
	if _, pinned := os.LookupEnv(calllog.EnvVar); !pinned {
		os.Setenv(calllog.EnvVar, calllog.OffValue)
	}
	root, err := os.MkdirTemp("", "provider-home")
	if err != nil {
		panic(err)
	}
	os.Setenv(home.EnvVar, root)
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

// loggingTo points the process's one log at a fresh file for the duration of a
// test and hands back the reader for it.
//
// It is also the rig for the tests in this file, and so it takes
// [resetSharedLearners] on the way out. A PACKAGE-LEVEL LEARNER IS RESET BY THE
// RIG BETWEEN TESTS: without this line
// TestARepairedRefusalLeavesTheRefusedShapeAndThenTheAnswer passes only as the
// first run of its model in a process, because the quirks memo remembers the
// repair and the second run sends the repaired shape first, leaving no refusal
// for the assertion to see (#455). The lane rig's cleanup makes the same call,
// but a run filtered down to one test in this file never builds a lane rig.
func loggingTo(t *testing.T) func() []calllog.Record {
	t.Helper()
	t.Cleanup(resetSharedLearners)
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(calllog.EnvVar, path)
	calllog.Open("")
	t.Cleanup(func() {
		calllog.Close()
		os.Setenv(calllog.EnvVar, calllog.OffValue)
		calllog.Open("")
	})
	return func() []calllog.Record {
		t.Helper()
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("the log should exist by now: %v", err)
		}
		defer file.Close()
		var records []calllog.Record
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var record calllog.Record
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("a line of the log is not a record: %v", err)
			}
			records = append(records, record)
		}
		return records
	}
}

// ended is the rows about a finished call: the start rows are their partners
// and say nothing an assertion here wants.
func ended(records []calllog.Record) []calllog.Record {
	var done []calllog.Record
	for _, record := range records {
		if record.Phase != calllog.PhaseStart {
			done = append(done, record)
		}
	}
	return done
}

func TestACompletedCallLeavesOneRowWithTheShapeItActuallyHad(t *testing.T) {
	read := loggingTo(t)
	client, _ := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	ctx := WithCallNode(WithCallTag(WithReasoningEffort(context.Background(), EffortLow), "compile"), "n7")
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello"), ai.WithMaxTokens(8_192)); err != nil {
		t.Fatal(err)
	}

	records := read()
	if len(records) != 2 {
		t.Fatalf("one call should leave a start and an end; got %d rows", len(records))
	}
	start, done := records[0], records[1]
	if start.Phase != calllog.PhaseStart || done.Phase != "" {
		t.Fatalf("the pair is not one start and one end: %+v", records)
	}
	if start.ID == "" || start.ID != done.ID {
		t.Fatalf("the pair should share an id: %q and %q", start.ID, done.ID)
	}
	if done.Tag != "compile" || done.Node != "n7" {
		t.Errorf("the row should say what the call was for: tag %q node %q", done.Tag, done.Node)
	}
	if done.Model != "sim/model" || done.Effort != "low" {
		t.Errorf("the row should name the model and the effort that travelled: %+v", done)
	}
	// THE CEILING THAT TRAVELLED, not the caller's figure: a low pass is
	// allocated a fifth of the ceiling, so 8192 of answer needs 10240 of room.
	if done.MaxTokens != 10_240 {
		t.Errorf("max_tokens = %d, want the wire ceiling 10240", done.MaxTokens)
	}
	if done.Status != 200 || done.Finish != "stop" {
		t.Errorf("status %d finish %q, want 200/stop", done.Status, done.Finish)
	}
	if done.PromptTokens != 10 || done.CompletionTokens != 2 {
		t.Errorf("the row should carry the provider's own counts: %+v", done)
	}
	if done.Messages != 1 || done.Tools != 0 {
		t.Errorf("the row should carry the request's shape: %d messages, %d tools", done.Messages, done.Tools)
	}
}

// TestAPinnedCallLogsTheEffortThatTravelled is C3: the row and the body name
// the same pinned word, with no override reading when the pin itself travelled.
func TestAPinnedCallLogsTheEffortThatTravelled(t *testing.T) {
	read := loggingTo(t)
	client, recorded := newTestClient(t, Config{
		Effort: EffortHigh,
		SupportsParameter: func(string, string) (bool, bool) {
			return true, true
		},
	})
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	reasoning, _ := recorded.body(0)["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("wire reasoning = %#v, want the pinned high effort", reasoning)
	}
	done := ended(read())
	if len(done) != 1 || done[0].Effort != "high" || done[0].EffortPin != "" {
		t.Fatalf("the pinned call's row should read high with no displaced pin: %+v", done)
	}
}

// TestARequiredCallOutranksAndNamesThePin is C5 and C4 together: the answer's
// own correctness bound wins, and the row says which seat pin did not travel.
func TestARequiredCallOutranksAndNamesThePin(t *testing.T) {
	read := loggingTo(t)
	client, recorded := newTestClient(t, Config{
		Effort: EffortHigh,
		SupportsParameter: func(string, string) (bool, bool) {
			return true, true
		},
	})
	ctx := WithRequiredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	reasoning, _ := recorded.body(0)["reasoning"].(map[string]any)
	if disabled, present := reasoning["enabled"].(bool); !present || disabled {
		t.Fatalf("wire reasoning = %#v, want the required disable", reasoning)
	}
	done := ended(read())
	if len(done) != 1 || done[0].Effort != "off" || done[0].EffortPin != "high" {
		t.Fatalf("the required call's row should read off and name pinned high: %+v", done)
	}
}

func TestARepairedRefusalLeavesTheRefusedShapeAndThenTheAnswer(t *testing.T) {
	read := loggingTo(t)
	// A model that will not have its thinking turned off: the first request is
	// refused, the adapter learns, and the second lands.
	calls := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"message":"Reasoning is mandatory for this endpoint; do not send effort: none","code":400}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":4,"completion_tokens":1}}`))
	})
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "refuser/model",
		HTTPClient:        handlerClient(handler),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithRequiredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello"), ai.WithMaxTokens(1_000)); err != nil {
		t.Fatalf("the repaired call should have landed: %v", err)
	}

	done := ended(read())
	if len(done) != 2 {
		t.Fatalf("a refusal and its repair are two finished rows; got %d: %+v", len(done), done)
	}
	refused, answered := done[0], done[1]
	if refused.Status != http.StatusBadRequest {
		t.Errorf("the first row should be the 400: %+v", refused)
	}
	if !strings.Contains(refused.Error, "Reasoning is mandatory") {
		t.Errorf("the row should carry the provider's own sentence: %q", refused.Error)
	}
	if len(refused.Learned) != 1 || refused.Learned[0] != learnedReasoningMandatory {
		t.Errorf("the row should say what the refusal taught: %v", refused.Learned)
	}
	if answered.Status != 200 || answered.Finish != "stop" {
		t.Errorf("the second row should be the answer: %+v", answered)
	}
}

func TestAStreamedCallLeavesARowOfItsOwn(t *testing.T) {
	read := loggingTo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		fmt.Fprint(w, `data: {"id":"one","provider":"gusher","choices":[{"index":0,`+
			`"delta":{"content":"the answer"}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, `data: {"id":"one","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":7,"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":2}}}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	ctx := WithCallTag(WithStreamObserver(context.Background(), func(StreamEvent) {}), "turn")
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	done := ended(read())
	if len(done) != 1 {
		t.Fatalf("one stream is one finished row; got %d: %+v", len(done), done)
	}
	row := done[0]
	if !row.Stream {
		t.Error("the row should say it was a stream")
	}
	if row.Tag != "turn" || row.Served != "gusher" || row.Finish != "stop" {
		t.Errorf("the row lost what the stream said: %+v", row)
	}
	// The one figure the SDK's usage type has no field for, off the same
	// terminal frame the token counts came from.
	if row.ReasoningTokens != 2 || row.CompletionTokens != 3 {
		t.Errorf("the row should carry the thinking pass's cost: %+v", row)
	}
}

func TestTheTagFallsBackToTheRoutingClassRatherThanBeingSpeltTwice(t *testing.T) {
	read := loggingTo(t)
	client, _ := newTestClient(t, Config{})
	ctx := WithCall(context.Background(), ClassPlanContract)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	done := ended(read())
	if len(done) != 1 || done[0].Tag != "contract" {
		t.Fatalf("plan.contract should read as \"contract\": %+v", done)
	}
}

func TestPromptsAreNotInTheLogUnlessSomebodyAsksForThem(t *testing.T) {
	read := loggingTo(t)
	client, _ := newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("my private prompt")); err != nil {
		t.Fatal(err)
	}
	for _, record := range read() {
		if record.RequestBody != "" || record.ResponseBody != "" {
			t.Fatalf("a call's bodies must not be in the log by default: %+v", record)
		}
	}

	t.Setenv(calllog.BodiesEnvVar, "1")
	readWithBodies := loggingTo(t)
	client, _ = newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("my private prompt")); err != nil {
		t.Fatal(err)
	}
	done := ended(readWithBodies())
	if len(done) != 1 {
		t.Fatalf("one call, one finished row; got %d", len(done))
	}
	if !strings.Contains(done[0].RequestBody, "my private prompt") {
		t.Errorf("with bodies asked for, the request should be whole: %q", done[0].RequestBody)
	}
	if !strings.Contains(done[0].ResponseBody, "finish_reason") {
		t.Errorf("with bodies asked for, the response should be whole: %q", done[0].ResponseBody)
	}
}

func TestAFailedAttemptLeavesItsOwnRowBeforeTheOneThatLanded(t *testing.T) {
	read := loggingTo(t)
	calls := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"error":{"message":"the upstream fell over","code":500}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":4,"completion_tokens":1}}`))
	})
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model",
		HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The backoff is seamed so a retry test does not have to wait one out.
	client.wait = func(context.Context, time.Duration) error { return nil }
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatalf("the retried call should have landed: %v", err)
	}

	done := ended(read())
	if len(done) != 2 {
		t.Fatalf("a failed attempt and the one that landed are two rows; got %d: %+v", len(done), done)
	}
	if done[0].Status != 500 || done[0].Attempt != 1 {
		t.Errorf("the first row should be attempt 1's 500: %+v", done[0])
	}
	if !strings.Contains(done[0].Error, "the upstream fell over") {
		t.Errorf("the row should carry the upstream's own words: %q", done[0].Error)
	}
	if done[1].Status != 200 || done[1].Attempt != 2 {
		t.Errorf("the answer's row should say it took two attempts: %+v", done[1])
	}
}

// A body that breaks off after the headers is an attempt that failed, and it
// leaves its end row like any other; three peer resets used to read as three
// calls still in flight.
func TestABodyThatBreaksOffStillEndsItsRow(t *testing.T) {
	read := loggingTo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "500")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server cannot hijack")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err == nil {
		t.Fatal("a body cut mid-read should fail the call")
	}
	rows := ended(read())
	if len(rows) == 0 {
		t.Fatal("the broken call left no end row")
	}
	if rows[len(rows)-1].Error == "" || !strings.Contains(rows[len(rows)-1].Error, "read response") {
		t.Fatalf("the end row should carry the read failure: %+v", rows[len(rows)-1])
	}
}

// A stream the caller abandons ends its row too.
func TestAnAbandonedStreamStillEndsItsRow(t *testing.T) {
	read := loggingTo(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if _, err := client.CompleteWithMessages(WithStreamObserver(ctx, func(StreamEvent) {}), userMessages("hello")); err == nil {
		t.Fatal("an abandoned stream should fail the call")
	}
	rows := ended(read())
	if len(rows) == 0 || !rows[len(rows)-1].Stream {
		t.Fatalf("the abandoned stream left no end row: %+v", rows)
	}
}

// TestBothRowsOfACallNameTheRunThatMadeIt is what makes this file joinable to
// the run that wrote it.
//
// A developer wanting the call count and the round count of one `aforge do`
// came here to reconstruct them, and found rows carrying a tag, a node and a
// timestamp and NOTHING naming the run — in a file every run on the machine
// appends to. Attribution was by clock alone. `aforge logs --run <id>` had been
// reading a `run` key off these rows for as long as it has existed, and nothing
// wrote one.
//
// Both rows carry it, not only the start: the end row is the one with the cost
// and the finish reason on it, and a reader filtering the file down to one run
// must not have to pair every row first to keep the halves that matter.
func TestBothRowsOfACallNameTheRunThatMadeIt(t *testing.T) {
	read := loggingTo(t)
	client, _ := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	ctx := trace.Begin(context.Background())
	run := trace.RunFrom(ctx)
	if run == "" {
		t.Fatal("the door minted no run id, so there is nothing for a row to carry")
	}
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello"), ai.WithMaxTokens(8_192)); err != nil {
		t.Fatal(err)
	}

	records := read()
	if len(records) != 2 {
		t.Fatalf("one call should leave a start and an end; got %d rows", len(records))
	}
	for _, record := range records {
		if record.Run != run {
			t.Errorf("a %s row carries run %q, and the call was made under %q — "+
				"a log a person is driven to read is attributable by timestamp alone without it",
				map[string]string{calllog.PhaseStart: "start", "": "end"}[record.Phase], record.Run, run)
		}
	}
	// And the count the run publishes in its `--json` envelope is the same
	// fact, kept in memory so it survives the log being switched off.
	if got := calllog.CallsFor(run); got != 1 {
		t.Errorf("the run made one call and reports %d", got)
	}
}
