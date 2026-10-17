package main

import (
	"encoding/json"
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
// answers a turn whose newest person's words carry crewOrderMarker by calling
// the automation tool once, the turn after the tool's answer with words, and
// every other request with "done". It is the model half of the fixture and
// nothing in the product is told it exists beyond the base URL every
// real-engine test in this package already points at its own endpoint.
type crewAutomationModel struct {
	server *httptest.Server
	mu     sync.Mutex
	asked  int
}

func newCrewAutomationModel(t *testing.T) *crewAutomationModel {
	t.Helper()
	model := &crewAutomationModel{}
	model.server = httptest.NewServer(http.HandlerFunc(model.serve))
	t.Cleanup(model.server.Close)
	return model
}

// proposals is how many automation proposals the model has made.
func (m *crewAutomationModel) proposals() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.asked
}

func (m *crewAutomationModel) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body struct {
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
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	offered := false
	for _, tool := range body.Tools {
		offered = offered || tool.Function.Name == "automation"
	}
	// The newest person's line decides; a tool's answer after it means the
	// proposal was already made in this turn.
	marked, answered := false, false
	for _, message := range body.Messages {
		switch strings.ToLower(message.Role) {
		case "user":
			marked, answered = strings.Contains(string(message.Content), "crew-origin-fixture"), false
		case "tool":
			answered = true
		}
	}
	delta, finish := `{"role":"assistant","content":"done"}`, "stop"
	switch {
	case offered && marked && !answered:
		m.mu.Lock()
		m.asked++
		m.mu.Unlock()
		args, _ := json.Marshal(map[string]any{
			"op": "propose", "title": "morning build check", "words": crewOrderMarker,
			"when": map[string]string{"in": "10m"}, "say": "check the build",
		})
		quoted, _ := json.Marshal(string(args))
		delta = `{"role":"assistant","tool_calls":[{"index":0,"id":"call_crew_order","type":"function","function":{"name":"automation","arguments":` + string(quoted) + `}}]}`
		finish = "tool_calls"
	case marked && answered:
		delta = `{"role":"assistant","content":"saved"}`
	}
	if body.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"crew-origin","choices":[{"index":0,"delta":`+delta+`,"finish_reason":"`+finish+`"}]}`+"\n\ndata: [DONE]\n\n")
		return
	}
	message := strings.Replace(delta, `"tool_calls":[{"index":0,`, `"tool_calls":[{`, 1)
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
			t.Fatalf("orders made in the crew = %+v (model asked %d times)", orders, model.proposals())
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
