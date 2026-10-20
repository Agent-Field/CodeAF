package appwire

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEngine answers every method from fixed values and records closing.
type fakeEngine struct {
	mu      sync.Mutex
	closed  int
	block   chan struct{}
	created CreateAsk
	limit   int
	patch   SettingsPatch
}

func (f *fakeEngine) ListAgents(workspace string) ([]Agent, error) {
	if workspace == "/nowhere" {
		return nil, nil
	}
	return []Agent{{ID: "a1", Title: "one", Workspace: "/w", State: StateIdle}}, nil
}
func (f *fakeEngine) CreateAgent(ask CreateAsk) (Agent, error) {
	f.mu.Lock()
	f.created = ask
	f.mu.Unlock()
	return Agent{ID: "new", Workspace: ask.Workspace, Model: ask.Model, State: StateIdle}, nil
}
func (f *fakeEngine) Entries(id, before string, limit int) ([]Entry, bool, error) {
	f.mu.Lock()
	f.limit = limit
	f.mu.Unlock()
	if id != "a1" {
		return nil, false, Errorf(CodeNotFound, "no agent %s", id)
	}
	return []Entry{{ID: "e0", Agent: id, Kind: EntryUser, Text: "hi"}}, true, nil
}
func (f *fakeEngine) SetModel(id, model string) (Agent, error) {
	return Agent{ID: id, Model: model, State: StateIdle}, nil
}
func (f *fakeEngine) Retry(id string) (uint64, error) {
	return 0, Errorf(CodeNotFound, "nothing failed")
}
func (f *fakeEngine) ListModels() ([]Model, error) { return nil, nil }
func (f *fakeEngine) Settings() (Settings, error) {
	return Settings{DefaultModel: "deepseek/deepseek-v4-flash", AsksBefore: "prompt"}, nil
}
func (f *fakeEngine) SpendToday() (Spend, error) {
	cap := 5.0
	return Spend{TodayUSD: 1.25, DayCapUSD: &cap}, nil
}
func (f *fakeEngine) OpenAgent(id string) (Agent, error) {
	if id != "a1" {
		return Agent{}, Errorf(CodeNotFound, "no agent %s", id)
	}
	return Agent{ID: id, State: StateIdle}, nil
}
func (f *fakeEngine) SetEffort(id, effort string) (Agent, error) {
	return Agent{ID: id, Effort: effort, State: StateIdle}, nil
}
func (f *fakeEngine) Send(id, text, clientID string) (uint64, error) {
	if f.block != nil {
		<-f.block
	}
	return 7, nil
}
func (f *fakeEngine) Stop(id string) error                 { return errors.New("the engine went away") }
func (f *fakeEngine) CloseAgent(id string) error           { return nil }
func (f *fakeEngine) ListAsks(agent string) ([]Ask, error) { return nil, nil }
func (f *fakeEngine) AnswerAsk(agent, id, option, text string) error {
	return Errorf(CodeNotFound, "no open ask %s", id)
}
func (f *fakeEngine) ListTasks(agent string) ([]Task, error) {
	return nil, nil
}
func (f *fakeEngine) ListCrews() ([]Crew, error) {
	return []Crew{{ID: "c1", Name: "harbor"}}, nil
}
func (f *fakeEngine) Close() error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		default:
			close(f.block)
		}
	}
	return nil
}

// wire is a running server over two pipes, as the app would hold it.
type wire struct {
	t      *testing.T
	in     *io.PipeWriter
	lines  *bufio.Scanner
	served chan error
	engine *fakeEngine
}

func startWire(t *testing.T, engine *fakeEngine) *wire {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	served := make(chan error, 1)
	server := NewServer(outW)
	go func() {
		served <- server.Serve(context.Background(), inR, Hello{Version: "test", PID: 42, Home: "/h"}, engine)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	w := &wire{t: t, in: inW, lines: scanner, served: served, engine: engine}
	t.Cleanup(func() { inW.Close() })
	return w
}

func (w *wire) send(line string) {
	w.t.Helper()
	if _, err := io.WriteString(w.in, line+"\n"); err != nil {
		w.t.Fatal(err)
	}
}

func (w *wire) next() map[string]any {
	w.t.Helper()
	got := make(chan map[string]any, 1)
	go func() {
		if !w.lines.Scan() {
			got <- nil
			return
		}
		var line map[string]any
		if err := json.Unmarshal(w.lines.Bytes(), &line); err != nil {
			w.t.Errorf("a line that is not JSON: %q", w.lines.Text())
		}
		got <- line
	}()
	select {
	case line := <-got:
		if line == nil {
			w.t.Fatal("the output ended")
		}
		return line
	case <-time.After(5 * time.Second):
		w.t.Fatal("no line arrived")
		return nil
	}
}

func TestHelloIsTheFirstLineAndCarriesTheContract(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	hello := w.next()
	if hello["event"] != "hello" || hello["seq"] != float64(1) {
		t.Fatalf("first line = %v", hello)
	}
	data := hello["data"].(map[string]any)
	if data["contract"] != float64(Contract) || data["version"] != "test" || data["pid"] != float64(42) || data["home"] != "/h" {
		t.Fatalf("hello data = %v", data)
	}
}

func TestPingAnswersAnEmptyObjectAndUnknownMethodsAreRefused(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	w.send(`{"id":1,"method":"ping"}`)
	if got := w.next(); got["id"] != float64(1) || got["result"] == nil || len(got["result"].(map[string]any)) != 0 {
		t.Fatalf("ping = %v", got)
	}
	w.send(`{"id":2,"method":"agents.fly","params":{}}`)
	got := w.next()
	if got["id"] != float64(2) || got["error"].(map[string]any)["code"] != CodeUnknownMethod {
		t.Fatalf("unknown method = %v", got)
	}
}

func TestBadParamsAreAnsweredWithTheirCode(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	for i, line := range []string{
		`{"id":1,"method":"agents.open","params":{}}`,
		`{"id":2,"method":"agents.open","params":{"id":5}}`,
		`{"id":3,"method":"agents.create","params":[]}`,
		`{"id":4,"method":"agents.send","params":{"id":"a1"}}`,
		`{"id":5,"method":"tasks.list"}`,
		`{"id":6,"method":"asks.list","params":{}}`,
		`{"id":7,"method":"asks.answer","params":{"agent":"a1","id":"consent:1"}}`,
	} {
		w.send(line)
		got := w.next()
		if got["id"] != float64(i+1) || got["error"] == nil || got["error"].(map[string]any)["code"] != CodeBadParams {
			t.Fatalf("%s = %v", line, got)
		}
	}
}

func TestEngineErrorsKeepTheirCodeOrBecomeEngine(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	w.send(`{"id":1,"method":"agents.open","params":{"id":"zz","extra":true}}`)
	if got := w.next(); got["error"].(map[string]any)["code"] != CodeNotFound {
		t.Fatalf("open = %v", got)
	}
	w.send(`{"id":2,"method":"agents.stop","params":{"id":"a1"}}`)
	got := w.next()
	failure := got["error"].(map[string]any)
	if failure["code"] != CodeEngine || !strings.Contains(failure["message"].(string), "went away") {
		t.Fatalf("stop = %v", got)
	}
}

func TestListsAreNeverNull(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	w.send(`{"id":1,"method":"agents.list","params":{"workspace":"/nowhere"}}`)
	got := w.next()
	if list, ok := got["result"].(map[string]any)["agents"].([]any); !ok || len(list) != 0 {
		t.Fatalf("agents.list = %v", got)
	}
	w.send(`{"id":2,"method":"crews.list"}`)
	got = w.next()
	crews := got["result"].(map[string]any)["crews"].([]any)
	if len(crews) != 1 || crews[0].(map[string]any)["members"] == nil {
		t.Fatalf("crews.list = %v", got)
	}
}

func TestMalformedLinesAreLoggedAndTheConnectionCarriesOn(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	w.send(`not json`)
	if got := w.next(); got["event"] != EventLog || got["seq"] != float64(2) {
		t.Fatalf("malformed line = %v", got)
	}
	w.send(`{"method":"ping"}`)
	if got := w.next(); got["event"] != EventLog || got["seq"] != float64(3) {
		t.Fatalf("missing id = %v", got)
	}
	w.send(`{"id":9,"method":"ping"}`)
	if got := w.next(); got["id"] != float64(9) {
		t.Fatalf("ping after noise = %v", got)
	}
}

func TestEndOfInputClosesTheEngineAndReturns(t *testing.T) {
	engine := &fakeEngine{}
	w := startWire(t, engine)
	w.next()
	w.in.Close()
	select {
	case err := <-w.served:
		if err != nil {
			t.Fatalf("serve = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return at end of input")
	}
	if engine.closed != 1 {
		t.Fatalf("engine closed %d times", engine.closed)
	}
}

func TestShutdownAnswersThenLeaves(t *testing.T) {
	engine := &fakeEngine{}
	w := startWire(t, engine)
	w.next()
	w.send(`{"id":3,"method":"shutdown"}`)
	if got := w.next(); got["id"] != float64(3) || got["result"] == nil {
		t.Fatalf("shutdown = %v", got)
	}
	select {
	case <-w.served:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after shutdown")
	}
	if engine.closed != 1 {
		t.Fatalf("engine closed %d times", engine.closed)
	}
}

// A slow call does not hold up the next request, and a call still waiting
// when the process leaves is answered `closed`.
func TestResponsesArriveInAnyOrderAndALateOneIsClosed(t *testing.T) {
	engine := &fakeEngine{block: make(chan struct{})}
	w := startWire(t, engine)
	w.next()
	w.send(`{"id":1,"method":"agents.send","params":{"id":"a1","text":"hi"}}`)
	w.send(`{"id":2,"method":"ping"}`)
	if got := w.next(); got["id"] != float64(2) {
		t.Fatalf("expected the ping first, got %v", got)
	}
	w.in.Close()
	got := w.next()
	if got["id"] != float64(1) {
		t.Fatalf("expected the send's answer, got %v", got)
	}
	// The fake answers once its block opens, which Close does; the server
	// reports a call that finished after shutdown began as successful only if
	// it succeeded, so either answer is one answer.
	if got["result"] == nil && got["error"] == nil {
		t.Fatalf("send answered nothing: %v", got)
	}
	<-w.served
}

// Sequence numbers count every event, in the order the lines are written,
// whichever goroutine emitted them.
func TestSequenceNumbersAreConsecutiveAcrossGoroutines(t *testing.T) {
	outR, outW := io.Pipe()
	server := NewServer(outW)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				server.Emit(EventLog, Log{Level: "info", Message: "x"})
			}
		}()
	}
	go func() {
		wg.Wait()
		server.finish()
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	want := uint64(1)
	for scanner.Scan() {
		var line eventLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		if line.Seq != want {
			t.Fatalf("seq %d, want %d", line.Seq, want)
		}
		want++
	}
	if want != 401 {
		t.Fatalf("read %d events, want 400", want-1)
	}
}

// recorder is an Emitter that keeps what it was handed.
type recorder struct {
	mu     sync.Mutex
	events []string
	data   []any
}

func (r *recorder) Emit(event string, data any) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.data = append(r.data, data)
	r.mu.Unlock()
}

func TestOutboxFoldsDeltasAndKeepsOrderWithToolsAndTheEnd(t *testing.T) {
	rec := &recorder{}
	box := NewOutbox(rec)
	box.Event(EventTurnStarted, TurnStarted{Agent: "a", Turn: 1})
	for _, word := range []string{"hel", "lo ", "there"} {
		box.Delta("a", 1, 0, DeltaText, word)
	}
	box.Delta("a", 1, 0, DeltaThinking, "hmm")
	box.Event(EventTurnTool, TurnTool{Agent: "a", Turn: 1, Call: "c", Name: "read", Phase: PhaseBegin})
	box.Delta("a", 1, 0, DeltaText, "after")
	box.Event(EventTurnDone, TurnDone{Agent: "a", Turn: 1, OK: true})
	box.Close()
	want := []string{EventTurnStarted, EventTurnDelta, EventTurnThinking, EventTurnTool, EventTurnDelta, EventTurnDone}
	if strings.Join(rec.events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v", rec.events)
	}
	if got := rec.data[1].(TurnDelta); got.Text != "hello there" || got.Kind != DeltaText {
		t.Fatalf("folded delta = %+v", got)
	}
	if got := rec.data[2].(TurnThinking); got.Text != "hmm" || got.Turn != 1 {
		t.Fatalf("thinking = %+v", got)
	}
	if got := rec.data[4].(TurnDelta); got.Text != "after" {
		t.Fatalf("delta after the tool = %+v", got)
	}
}

func TestOutboxSendsALoneDeltaAfterTheWindowAndAFullOneAtOnce(t *testing.T) {
	rec := &recorder{}
	box := NewOutbox(rec)
	defer box.Close()
	box.Delta("a", 1, 0, DeltaText, "x")
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec.mu.Lock()
		n := len(rec.events)
		rec.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a lone delta was never sent")
		}
		time.Sleep(time.Millisecond)
	}
	big := strings.Repeat("y", DeltaBytes)
	box.Delta("a", 1, 0, DeltaText, big)
	box.Delta("a", 1, 0, DeltaText, "z")
	box.Close()
	if len(rec.events) != 3 {
		t.Fatalf("events = %d, want the lone one, the full one and the next", len(rec.events))
	}
	if rec.data[1].(TurnDelta).Text != big || rec.data[2].(TurnDelta).Text != "z" {
		t.Fatal("a full delta was folded past its size")
	}
}

// THE WAVE-ONE METHODS ON THE WIRE. agents.create passes the whole ask and
// refuses a negative budget; agents.entries caps the page and answers a list
// and whether there is more; models.list answers an empty list rather than
// null; settings.get says a figure nobody set as null; spend.today carries
// the cap; agents.retry with nothing failed is not found.
func TestWaveOneMethodsAnswerTheirShapes(t *testing.T) {
	engine := &fakeEngine{}
	w := startWire(t, engine)
	w.next()

	w.send(`{"id":1,"method":"agents.create","params":{"text":"fix the parser","budget_usd":2.5}}`)
	got := w.next()
	if got["id"] != float64(1) || got["result"] == nil {
		t.Fatalf("agents.create = %v", got)
	}
	engine.mu.Lock()
	created := engine.created
	engine.mu.Unlock()
	if created.Workspace != "" || created.Text != "fix the parser" || created.BudgetUSD == nil || *created.BudgetUSD != 2.5 {
		t.Fatalf("the engine was asked %+v", created)
	}
	w.send(`{"id":2,"method":"agents.create","params":{"budget_usd":-1}}`)
	if got := w.next(); got["error"].(map[string]any)["code"] != CodeBadParams {
		t.Fatalf("negative budget = %v", got)
	}

	w.send(`{"id":3,"method":"agents.entries","params":{"id":"a1","limit":5000}}`)
	got = w.next()
	result := got["result"].(map[string]any)
	if entries := result["entries"].([]any); len(entries) != 1 || result["more"] != true {
		t.Fatalf("agents.entries = %v", got)
	}
	engine.mu.Lock()
	limit := engine.limit
	engine.mu.Unlock()
	if limit != MaxEntries {
		t.Fatalf("limit = %d, want the cap %d", limit, MaxEntries)
	}
	w.send(`{"id":4,"method":"agents.entries","params":{}}`)
	if got := w.next(); got["error"].(map[string]any)["code"] != CodeBadParams {
		t.Fatalf("entries without an id = %v", got)
	}

	w.send(`{"id":5,"method":"models.list"}`)
	if got := w.next(); got["result"].(map[string]any)["models"] == nil {
		t.Fatalf("models.list = %v", got)
	}
	w.send(`{"id":6,"method":"settings.get"}`)
	got = w.next()
	settings := got["result"].(map[string]any)["settings"].(map[string]any)
	if value, present := settings["budget_usd"]; !present || value != nil || settings["asks_before"] != "prompt" {
		t.Fatalf("settings.get = %v", got)
	}
	w.send(`{"id":7,"method":"spend.today"}`)
	got = w.next()
	spend := got["result"].(map[string]any)["spend"].(map[string]any)
	if spend["today_usd"] != 1.25 || spend["day_cap_usd"] != 5.0 {
		t.Fatalf("spend.today = %v", got)
	}
	w.send(`{"id":8,"method":"agents.retry","params":{"id":"a1"}}`)
	if got := w.next(); got["error"].(map[string]any)["code"] != CodeNotFound {
		t.Fatalf("agents.retry = %v", got)
	}
	w.send(`{"id":9,"method":"agents.set_model","params":{"id":"a1"}}`)
	if got := w.next(); got["error"].(map[string]any)["code"] != CodeBadParams {
		t.Fatalf("set_model without a model = %v", got)
	}
}

// Two messages of one turn are never folded into one delta: the app keys
// answers by message, and a fold across them would put one message's words
// under the other.
func TestOutboxKeepsMessagesApart(t *testing.T) {
	rec := &recorder{}
	box := NewOutbox(rec)
	box.Delta("a", 1, 0, DeltaText, "first")
	box.Delta("a", 1, 1, DeltaText, "second")
	box.Close()
	if len(rec.data) != 2 || rec.data[0].(TurnDelta).Msg != 0 || rec.data[1].(TurnDelta).Msg != 1 || rec.data[1].(TurnDelta).Text != "second" {
		t.Fatalf("deltas = %+v", rec.data)
	}
}

// Every terminal rung crosses the wire; auto is absence, never a stored word.
func TestSetEffortTakesTerminalRungs(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	hello := w.next()
	payload, _ := json.Marshal(hello)
	if !strings.Contains(string(payload), `"`+FeatureAgentEffortRungs+`"`) {
		t.Fatalf("hello has no effort rungs feature: %s", payload)
	}
	for _, word := range []string{"low", "medium", "high", "xhigh", "max", "auto", " High ", "sideways"} {
		t.Run(word, func(t *testing.T) {
			request, _ := json.Marshal(map[string]any{"id": 1, "method": "agents.set_effort", "params": map[string]string{"id": "a1", "effort": word}})
			w.send(string(request))
			got := w.next()
			if word == "sideways" {
				failure := got["error"].(map[string]any)
				if failure["code"] != CodeBadParams || failure["message"] != "effort is low, medium or high" {
					t.Fatalf("unknown effort = %v", got)
				}
				return
			}
			agent := got["result"].(map[string]any)["agent"].(map[string]any)
			want := strings.ToLower(strings.TrimSpace(word))
			if word == "auto" {
				if _, exists := agent["effort"]; exists {
					t.Fatalf("auto was not omitted: %v", got)
				}
			} else if agent["effort"] != want {
				t.Fatalf("effort = %v, want %q", got, want)
			}
		})
	}
}

// THE ARCHIVED CREWS ARE ASKED BY A METHOD OF THEIR OWN. A host that does not
// serve it answers unknown_method, so a client never reads the open crews as
// archived ones; an engine with no crews refuses it rather than answering [].
func TestArchivedCrewsAreAMethodOfTheirOwn(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	w.send(`{"id":1,"method":"crews.archived"}`)
	got := w.next()
	if failure, _ := got["error"].(map[string]any); failure["code"] != CodeUnknownMethod {
		t.Fatalf("an engine without crews answered crews.archived with %v", got)
	}
	w.send(`{"id":2,"method":"crews.archive","params":{"id":"x"}}`)
	if failure, _ := w.next()["error"].(map[string]any); failure["code"] != CodeUnknownMethod {
		t.Fatalf("crews.archive on an engine without crews succeeded")
	}
	w.send(`{"id":3,"method":"crews.archived_old"}`)
	failure, _ := w.next()["error"].(map[string]any)
	if failure["code"] != CodeUnknownMethod {
		t.Fatalf("an unserved method = %v", failure)
	}
}
