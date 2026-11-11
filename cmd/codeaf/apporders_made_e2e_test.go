package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
)

// crewOrderMarker is the words a crew message carries that the scripted model
// answers with an automation proposal; every other turn is answered "done".
const crewOrderMarker = "crew-origin-fixture: every morning, check the build"

// crewAutomationModel is an OpenAI-shaped endpoint, local and keyless, that
// answers a request carrying crewOrderMarker in any message by calling
// the automation tool once, the turn after the tool's answer with words, and
// every other request with "done". It is the model half of the fixture and
// nothing in the product is told it exists beyond the base URL every
// real-engine test in this package already points at its own endpoint.
type crewAutomationModel struct {
	server *httptest.Server
	mu     sync.Mutex
	// seen counts every completion request; offered those that carried the
	// automation tool; marked those whose messages carried the marker; asked the
	// proposals made. A red run prints all four, so it names the link that broke.
	seen, offered, marked, asked int
}

func newCrewAutomationModel(t *testing.T) *crewAutomationModel {
	t.Helper()
	model := &crewAutomationModel{}
	model.server = httptest.NewServer(http.HandlerFunc(model.serve))
	t.Cleanup(model.server.Close)
	return model
}

// counts is what the model has seen, for a failure message.
func (m *crewAutomationModel) counts() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprintf("requests %d, offering automation %d, carrying the marker %d, proposals %d", m.seen, m.offered, m.marked, m.asked)
}

// proposals is how many automation proposals the model has made.
func (m *crewAutomationModel) proposals() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.asked
}

// crewModelRequest is the part of a completion request the model decides on.
type crewModelRequest struct {
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
	Stream bool `json:"stream"`
}

// decide is what the model answers a request with: a proposal when the
// automation tool is offered, the marker is in ANY message of the request (the
// engine may add user-role context after the person's words, so the newest
// user message is not where to look) and no tool answer has come back yet;
// "saved" after the tool's answer; "done" otherwise.
func (m *crewAutomationModel) decide(req crewModelRequest) (delta, finish string) {
	offered, marked, answered := false, false, false
	for _, tool := range req.Tools {
		offered = offered || tool.Function.Name == "automation"
	}
	for _, message := range req.Messages {
		switch strings.ToLower(message.Role) {
		case "user":
			marked = marked || strings.Contains(string(message.Content), "crew-origin-fixture")
		case "tool":
			answered = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen++
	if offered {
		m.offered++
	}
	if marked {
		m.marked++
	}
	switch {
	case offered && marked && !answered:
		m.asked++
		args, _ := json.Marshal(map[string]any{
			"op": "propose", "title": "morning build check", "words": crewOrderMarker,
			"when": map[string]string{"in": "10m"}, "say": "check the build",
		})
		quoted, _ := json.Marshal(string(args))
		return `{"tool_calls":[{"index":0,"id":"call_crew_order","type":"function","function":{"name":"automation","arguments":` + string(quoted) + `}}]}`, "tool_calls"
	case marked && answered:
		return `{"content":"saved"}`, "stop"
	}
	return `{"content":"done"}`, "stop"
}

func (m *crewAutomationModel) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var req crewModelRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	delta, finish := m.decide(req)
	if req.Stream {
		// The shape internal/e2e's scripted automation model streams, which the
		// real engine is proven against: the delta without a finish reason, then
		// an empty delta that carries it.
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := func(delta, finish string) {
			reason := "null"
			if finish != "" {
				reason = `"` + finish + `"`
			}
			_, _ = io.WriteString(w, `data: {"id":"crew-origin","choices":[{"index":0,"delta":`+delta+`,"finish_reason":`+reason+`}]}`+"\n\n")
		}
		chunk(delta, "")
		chunk(`{}`, finish)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		return
	}
	var message string
	if finish == "tool_calls" {
		message = strings.Replace(delta, `{"tool_calls":[{"index":0,`, `{"role":"assistant","tool_calls":[{`, 1)
	} else {
		message = strings.Replace(delta, `{"content"`, `{"role":"assistant","content"`, 1)
	}
	_, _ = io.WriteString(w, `{"choices":[{"index":0,"message":`+message+`,"finish_reason":"`+finish+`"}]}`)
}

// freshAutomationsStore gives this test the process's automations door opened
// under its own CODEAF_HOME, the store the in-process engine proposes through
// and the one orders.made_in must read. The door is opened once per process,
// so whichever test opened it first would otherwise hold it under a home that
// test has since removed. The earlier handle is put back afterwards. This is
// test-side state only: the product's own once-per-process door is unchanged.
func freshAutomationsStore(t *testing.T) *automation.Store {
	t.Helper()
	saved := automationsStore
	automationsOnce = sync.Once{}
	automationsStore = nil
	autos := v3Automations()
	if autos == nil {
		t.Fatal("automations are off in this test's home")
	}
	t.Cleanup(func() {
		// The engine is closed before the store it writes through.
		closeEngineProcess()
		_ = autos.Store.Close()
		automationsOnce = sync.Once{}
		if saved != nil {
			automationsOnce.Do(func() {})
		}
		automationsStore = saved
	})
	return autos.Store
}

// madeInWalk reads every orders.made_in page for crew over the registered wire
// and answers the orders and the summed unknown.
func madeInWalk(t *testing.T, w *actionWire, crew string) ([]appwire.Order, int) {
	t.Helper()
	var orders []appwire.Order
	unknown, cursor := 0, ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("the walk never ended")
		}
		raw, e := w.call("orders.made_in", appwire.OrdersMadeAsk{Crew: crew, Cursor: cursor})
		if e != nil {
			t.Fatalf("orders.made_in: %+v", e)
		}
		var page appwire.OrdersMadeResult
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		orders = append(orders, page.Orders...)
		unknown += page.Unknown
		if page.Next == "" {
			return orders, unknown
		}
		cursor = page.Next
	}
}

// A CREW MESSAGE'S ORDER IS READ BACK AS MADE IN THAT CREW, END TO END, WITH NO
// FAKE IN BETWEEN: a real backend over the in-process engine (the production
// loopback), a crew in the production teams profile holding the real
// conversation's transcript as its key, the message sent through the
// registered crews.message, the engine proving the crew at acceptance, a turn
// against a local scripted model that proposes an automation, the proposal
// stored by the engine's own automation tool, and the order read back through
// the registered orders.made_in — its recorded creation team the crew, its
// agent the member, a legacy order counted unknown, and the same after the
// person ratifies it through orders.ratify.
func TestCrewMessageOrderIsReadBackAsMadeInTheCrew(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	model := newCrewAutomationModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	store := freshAutomationsStore(t)
	legacy, err := store.Create(automation.Automation{
		Title: "from before", Schedule: automation.Schedule{Every: "1h", Zone: "UTC"},
		Action: automation.Action{Say: "older"}, Workspace: workspace,
		Origin: automation.Origin{SessionID: "older", Transcript: "/older/transcript.jsonl"},
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &realActionHarness{t: t, cwd: workspace}
	h.start()
	// The backend reads the store the engine writes; its profile is left to
	// the production default, the one the engine's teams read resolves to.
	h.b.orderStore = func() *automation.Store { return store }
	member, err := h.b.CreateAgent(appwire.CreateAsk{Workspace: workspace, Text: "say done"})
	if err != nil {
		t.Fatal(err)
	}
	h.rec.waitFor(t, 30*time.Second, appTurnDone)
	crew := managedTeamWith(t, root, appTranscriptOf(h.b, member.ID), workspace)

	raw, e := h.w.call("crews.message", appwire.CrewMessage{Crew: crew, To: member.ID, Text: crewOrderMarker, ClientID: "crew-origin-1"})
	if e != nil {
		t.Fatalf("crews.message: %+v", e)
	}
	var sent appwire.EntryResult
	if err := json.Unmarshal(raw, &sent); err != nil || sent.Entry.Turn == 0 {
		t.Fatalf("crews.message answered %s (%v); want a turn opened", raw, err)
	}

	// The proposal is stored while its card waits for the person.
	var made appwire.Order
	deadline := time.Now().Add(30 * time.Second)
	for {
		orders, unknown := madeInWalk(t, h.w, crew)
		if len(orders) == 1 {
			made = orders[0]
			if unknown != 1 {
				t.Fatalf("the walk counted %d unknown, want 1 (%s, saved with no creation team)", unknown, legacy.ID)
			}
			break
		}
		if len(orders) > 1 || time.Now().After(deadline) {
			t.Fatalf("orders made in the crew = %+v (%s)", orders, model.counts())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if made.Agent != member.ID || made.ID == legacy.ID {
		t.Fatalf("the order made in the crew is %+v; want the member %s's", made, member.ID)
	}
	stored, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Origin.Team == nil || !stored.Origin.Team.Known || stored.Origin.Team.ID != crew {
		t.Fatalf("the stored creation team = %+v, want %s", stored.Origin.Team, crew)
	}

	// The person ratifies it; the turn ends; where it was made does not move.
	if _, e := h.w.call("orders.ratify", map[string]string{"id": made.ID}); e != nil {
		t.Fatalf("orders.ratify: %+v", e)
	}
	appTurnDoneFor(t, h.rec, sent.Entry.Turn)
	orders, unknown := madeInWalk(t, h.w, crew)
	if len(orders) != 1 || orders[0].ID != made.ID || unknown != 1 {
		t.Fatalf("after the ratify the crew's orders = %+v, unknown %d", orders, unknown)
	}
	if after, err := store.Get(made.ID); err != nil || after.Origin.Team == nil || after.Origin.Team.ID != crew {
		t.Fatalf("after the ratify the stored creation team = %+v, %v", after.Origin.Team, err)
	}
}

// THE MODEL FINDS THE MARKER WHEREVER THE ENGINE PUT IT. A request whose NEWEST
// user message carries no marker (the engine appended context after the
// person's words) but whose earlier one does, with the automation tool offered
// and no tool answer yet, is answered with the proposal; after the tool's
// answer, with "saved"; with the tool absent, with "done". This is the cause of
// the first red run of the end-to-end fixture, tested without an engine.
func TestCrewAutomationModelFindsTheMarkerBeyondTheNewestUserMessage(t *testing.T) {
	model := &crewAutomationModel{}
	call := func(body string) (string, string) {
		var req crewModelRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		return model.decide(req)
	}
	tools := `"tools":[{"function":{"name":"automation"}}]`
	asking := `{"messages":[{"role":"user","content":"` + crewOrderMarker + `"},{"role":"user","content":"context the engine added"}],` + tools + `}`
	if delta, finish := call(asking); finish != "tool_calls" || !strings.Contains(delta, `"name":"automation"`) {
		t.Fatalf("a marker in an earlier user message: %s %s", delta, finish)
	}
	answered := `{"messages":[{"role":"user","content":"` + crewOrderMarker + `"},{"role":"assistant","content":""},{"role":"tool","content":"ok"}],` + tools + `}`
	if delta, finish := call(answered); finish != "stop" || !strings.Contains(delta, "saved") {
		t.Fatalf("after the tool's answer: %s %s", delta, finish)
	}
	bare := `{"messages":[{"role":"user","content":"` + crewOrderMarker + `"}]}`
	if delta, finish := call(bare); finish != "stop" || !strings.Contains(delta, "done") {
		t.Fatalf("with the tool absent: %s %s", delta, finish)
	}
	other := `{"messages":[{"role":"user","content":"say done"}],` + tools + `}`
	if delta, finish := call(other); finish != "stop" || !strings.Contains(delta, "done") || model.proposals() != 1 {
		t.Fatalf("a request without the marker: %s %s, %d proposals", delta, finish, model.proposals())
	}
}
