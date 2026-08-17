package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── a hub with no Google account behind it ──────────────────────────────────

// fakeHub is one service and a script for what connecting it does. It stands in
// for internal/connect so that every path through the ask, the arming and the
// failures is exercised without a network and without an account.
type fakeHub struct {
	mu        sync.Mutex
	connected bool
	account   string
	// beginErr fails the attempt at its start; waitErr fails it after the
	// person has been sent to the page.
	beginErr error
	waitErr  error
	// waitFor blocks the wait until it is closed, so a test can hold an attempt
	// open and watch the ceiling end it.
	waitFor chan struct{}
	begins  int
	clients int
	// transport answers the requests an armed tool makes, so that a test can
	// watch a whole call — the question, the account, the service — without a
	// network. Nil means nothing is expected to reach a service.
	transport *stubTransport

	// The other half: one account connected with a key rather than in a
	// browser. It is off unless a test turns it on, so that every test about
	// the browser half goes on reasoning about a list of one.
	keyService   bool
	keyConnected bool
	// key is what the person was taken to have given, and keyErr is the
	// service refusing it.
	key    string
	keyErr error
	// calls is every raw call an armed tool made, as "METHOD path".
	calls []string
}

// keyStatus is the account the fake connects with a key.
func keyStatus(connected bool) connectStatus {
	return connectStatus{
		ID: "stripe", Name: "Stripe", Auth: "key",
		Blurb:     "Reach your Stripe account at api.stripe.com, with a key you already hold.",
		Address:   "https://api.stripe.com",
		Connected: connected,
	}
}

// stubTransport is a service that answers one canned reply and remembers every
// request it was sent.
type stubTransport struct {
	mu       sync.Mutex
	requests []*http.Request
	answer   string
}

func (s *stubTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	s.mu.Unlock()
	answer := s.answer
	if answer == "" {
		answer = "{}"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(answer)),
		Request:    request,
	}, nil
}

func (s *stubTransport) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (h *fakeHub) Services() []connectStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	services := []connectStatus{{
		ID:        "google",
		Name:      "Google",
		Blurb:     "search and read your mail, and look at your calendar",
		Auth:      "browser",
		Connected: h.connected,
		Account:   h.account,
	}}
	if h.keyService {
		services = append(services, keyStatus(h.keyConnected))
	}
	return services
}

func (h *fakeHub) Connected(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch id {
	case "google":
		return h.connected
	case "stripe":
		return h.keyService && h.keyConnected
	}
	return false
}

func (h *fakeHub) ConnectKey(ctx context.Context, id string, key string) (connectStatus, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.keyErr != nil {
		return connectStatus{}, h.keyErr
	}
	h.key = key
	h.keyConnected = true
	return keyStatus(true), nil
}

func (h *fakeHub) Request(ctx context.Context, id, method, path, query, body string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, method+" "+path)
	return "Stripe · " + method + " " + path + " · 200 OK\n\n{}", nil
}

// gaveKey is what the person was taken to have handed over.
func (h *fakeHub) gaveKey() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.key
}

func (h *fakeHub) rawCalls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

func (h *fakeHub) BeginAuth(ctx context.Context, id string) (string, func(context.Context) (connectStatus, error), error) {
	h.mu.Lock()
	h.begins++
	begin, wait, hold := h.beginErr, h.waitErr, h.waitFor
	h.mu.Unlock()
	if begin != nil {
		return "", nil, begin
	}
	return "https://example.test/say-yes", func(ctx context.Context) (connectStatus, error) {
		if hold != nil {
			select {
			case <-hold:
			case <-ctx.Done():
				return connectStatus{}, ctx.Err()
			}
		}
		if wait != nil {
			return connectStatus{}, wait
		}
		h.mu.Lock()
		h.connected = true
		h.account = "you@example.test"
		h.mu.Unlock()
		return connectStatus{ID: "google", Name: "Google", Connected: true, Account: "you@example.test"}, nil
	}, nil
}

func (h *fakeHub) Client(ctx context.Context, id string) (*http.Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients++
	if !h.connected {
		return nil, errors.New("not connected")
	}
	if h.transport != nil {
		return &http.Client{Transport: h.transport}, nil
	}
	return http.DefaultClient, nil
}

func (h *fakeHub) attempts() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.begins
}

// connectAgent builds an agent whose accounts seam is the fake, with the surface
// watching unless the test says otherwise.
func connectAgent(t *testing.T, completer Completer, hub *fakeHub, watched bool) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.AskConsent = watched
	})
	return agent
}

// useServiceTurn is the script for one turn that calls use_service and then
// says something.
func useServiceTurn(service string) []step {
	return []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "use_service", `{"service":"`+service+`"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}
}

// lastToolOutput is what the model was handed for the turn's one tool call.
func lastToolOutput(t *testing.T, events []Event) string {
	t.Helper()
	for index := len(events) - 1; index >= 0; index-- {
		switch events[index].Kind {
		case EventToolEnd, EventToolFailed:
			return events[index].Output
		}
	}
	t.Fatalf("no tool result in %v", kinds(events))
	return ""
}

// ── the nil law ─────────────────────────────────────────────────────────────

// A model must never be told about a hand it does not have. With no manager the
// two connect tools are absent from the belt entirely, rather than present and
// refusing.
func TestTheConnectToolsAreOnTheBeltOnlyWhenAHubIs(t *testing.T) {
	unwired, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if hasTool(unwired, "services") || hasTool(unwired, "use_service") {
		t.Fatalf("a session with no accounts carries the connect tools: %v", beltNames(unwired))
	}

	wired := connectAgent(t, &scriptedCompleter{}, &fakeHub{}, true)
	if !hasTool(wired, "services") || !hasTool(wired, "use_service") {
		t.Fatalf("a wired session is missing the connect tools: %v", beltNames(wired))
	}
	// And what an account BRINGS is not there until it is picked up: five
	// schemas at the front of every request are not carried by a conversation
	// that never touches mail.
	for _, name := range []string{"gmail_search", "gmail_read", "gmail_send", "calendar_list", "calendar_create"} {
		if hasTool(wired, name) {
			t.Fatalf("%s is on the belt before the account was picked up", name)
		}
	}
}

// ── the append law ──────────────────────────────────────────────────────────

// ARMING APPENDS AND NOTHING ELSE MOVES. The definition block rides at the front
// of every request, so a definition that shifted position would re-bill the
// whole transcript behind it; this test reads that law directly — the belt
// before arming is a PREFIX of the belt after, in order, and the definitions
// agree with the tools name for name.
func TestArmedToolsLandAtTheTailAndNothingAlreadyThereMoves(t *testing.T) {
	agent := connectAgent(t, &scriptedCompleter{}, &fakeHub{connected: true, account: "you@example.test"}, true)

	before := beltNames(agent)
	beforeDefinitions := definitionNames(agent)

	text, isError, err := agent.useService(context.Background(), "google")
	if err != nil || isError {
		t.Fatalf("use_service on a connected account: %q err=%v", text, err)
	}

	after := beltNames(agent)
	afterDefinitions := definitionNames(agent)
	if len(after) != len(before)+5 {
		t.Fatalf("belt grew by %d, want 5: %v", len(after)-len(before), after)
	}
	for index, name := range before {
		if after[index] != name {
			t.Fatalf("position %d moved: was %q, now %q", index, name, after[index])
		}
		if beforeDefinitions[index] != afterDefinitions[index] {
			t.Fatalf("definition %d moved: was %q, now %q", index, beforeDefinitions[index], afterDefinitions[index])
		}
	}
	tail := strings.Join(after[len(before):], ",")
	if tail != "gmail_search,gmail_read,gmail_send,calendar_list,calendar_create" {
		t.Fatalf("the family did not arrive at the tail: %q", tail)
	}
	if strings.Join(afterDefinitions, ",") != strings.Join(after, ",") {
		t.Fatalf("the definitions and the belt disagree:\n%v\n%v", afterDefinitions, after)
	}
	if !strings.Contains(text, "next turn") {
		t.Fatalf("the result does not say when the tools arrive: %q", text)
	}

	// Asking again is the cheap no-op that lets the two tools stay on the belt
	// forever: one short line, and not one entry added or moved.
	repeat, isError, _ := agent.useService(context.Background(), "google")
	if isError {
		t.Fatalf("a second ask errored: %q", repeat)
	}
	if !strings.Contains(repeat, "Already loaded") {
		t.Fatalf("a second ask did not read as a no-op: %q", repeat)
	}
	if names := strings.Join(beltNames(agent), ","); names != strings.Join(after, ",") {
		t.Fatalf("a second ask moved the belt: %q", names)
	}
}

func definitionNames(a *Agent) []string {
	definitions := a.beltDefinitions()
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Function.Name
	}
	return names
}

// An id nobody has is answered honestly, in one result that also says what
// there IS — a model that guessed a name gets the list rather than a second
// guess — and nothing is asked of the person.
func TestAnUnknownServiceIsAnHonestResultAndAsksNobody(t *testing.T) {
	hub := &fakeHub{}
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	text, isError, err := agent.useService(context.Background(), "dropbox")
	if err != nil || !isError {
		t.Fatalf("an unknown id: %q isError=%v err=%v", text, isError, err)
	}
	if !strings.Contains(text, "google") {
		t.Fatalf("the refusal does not name what there is: %q", text)
	}
	if hub.attempts() != 0 {
		t.Fatal("an unknown id started a connection")
	}
	if pending := agent.PendingConnect(); len(pending) != 0 {
		t.Fatalf("an unknown id asked the person: %v", pending)
	}
}

// ── the ask ─────────────────────────────────────────────────────────────────

// The whole approved path, end to end inside a real turn: the question, the
// page to open, the outcome, the family on the belt, and a result that tells the
// model when it may use it.
func TestConnectingAnAccountAsksThenArmsTheFamily(t *testing.T) {
	hub := &fakeHub{}
	completer := &scriptedCompleter{steps: useServiceTurn("google")}
	agent := connectAgent(t, completer, hub, true)

	events, err := agent.Submit(context.Background(), "what did priya say?")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainConnect(t, events, func(event Event) {
		agent.ResolveConnect(event.ConnectID, true)
		// A second click is late, not an error: the answer was already
		// delivered and the map no longer holds the question.
		agent.ResolveConnect(event.ConnectID, false)
	})

	ask, asked := firstOfKind(collected, EventConnectAsk)
	if !asked {
		t.Fatalf("nobody was asked: %v", kinds(collected))
	}
	if ask.Service != "google" || ask.ServiceName != "Google" || ask.ConnectID == "" {
		t.Fatalf("the question is missing its subject: %+v", ask)
	}
	auth, sent := firstOfKind(collected, EventConnectAuth)
	if !sent || auth.AuthURL == "" || auth.Service != "google" {
		t.Fatalf("no page to open: %+v", auth)
	}
	done, ended := firstOfKind(collected, EventConnectDone)
	if !ended || done.Failed || done.Account != "you@example.test" {
		t.Fatalf("the outcome is wrong: %+v", done)
	}
	if countKind(collected, EventConnectDone) != 1 {
		t.Fatalf("an attempt reported its outcome %d times", countKind(collected, EventConnectDone))
	}
	if !hasTool(agent, "gmail_search") {
		t.Fatalf("the family did not arrive: %v", beltNames(agent))
	}
	if output := lastToolOutput(t, collected); !strings.Contains(output, "you@example.test") {
		t.Fatalf("the model was not told who it is connected as: %q", output)
	}
}

// SILENCE IS A NO. The clock runs out, nothing is connected, nothing is armed,
// and the model is told the person did not agree rather than told the machine
// broke.
func TestAConnectQuestionNobodyAnswersIsANo(t *testing.T) {
	restore := connectAskTimeout
	connectAskTimeout = 20 * time.Millisecond
	t.Cleanup(func() { connectAskTimeout = restore })

	hub := &fakeHub{}
	completer := &scriptedCompleter{steps: useServiceTurn("google")}
	agent := connectAgent(t, completer, hub, true)

	events, err := agent.Submit(context.Background(), "check my mail")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainConnect(t, events, nil)

	if _, asked := firstOfKind(collected, EventConnectAsk); !asked {
		t.Fatalf("nobody was asked: %v", kinds(collected))
	}
	if _, sent := firstOfKind(collected, EventConnectAuth); sent {
		t.Fatal("an unanswered question still sent the person to a page")
	}
	if hub.attempts() != 0 {
		t.Fatal("an unanswered question still started a connection")
	}
	if hasTool(agent, "gmail_search") {
		t.Fatalf("an unanswered question still armed the family: %v", beltNames(agent))
	}
	if output := lastToolOutput(t, collected); !strings.Contains(output, "did not agree") {
		t.Fatalf("the model was not told plainly: %q", output)
	}
	if pending := agent.PendingConnect(); len(pending) != 0 {
		t.Fatalf("the abandoned question is still in the map: %v", pending)
	}
}

// A person who says no is a person who said no: the account stays unconnected
// and the model is told to do the work without it.
func TestADeclinedConnectionIsSaidPlainly(t *testing.T) {
	hub := &fakeHub{}
	completer := &scriptedCompleter{steps: useServiceTurn("google")}
	agent := connectAgent(t, completer, hub, true)

	events, err := agent.Submit(context.Background(), "check my mail")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainConnect(t, events, func(event Event) {
		agent.ResolveConnect(event.ConnectID, false)
	})

	if hub.attempts() != 0 {
		t.Fatal("a refusal still started a connection")
	}
	if hasTool(agent, "gmail_search") {
		t.Fatalf("a refusal still armed the family: %v", beltNames(agent))
	}
	if output := lastToolOutput(t, collected); !strings.Contains(output, "did not agree") {
		t.Fatalf("the model was not told plainly: %q", output)
	}
}

// An attempt that breaks after the person said yes reports its failure once,
// arms nothing, and hands the model an error it can act on rather than ending
// the turn.
func TestAConnectionThatFailsSaysSoAndArmsNothing(t *testing.T) {
	hub := &fakeHub{waitErr: errors.New("the page was closed")}
	completer := &scriptedCompleter{steps: useServiceTurn("google")}
	agent := connectAgent(t, completer, hub, true)

	events, err := agent.Submit(context.Background(), "check my mail")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainConnect(t, events, func(event Event) {
		agent.ResolveConnect(event.ConnectID, true)
	})

	done, ended := firstOfKind(collected, EventConnectDone)
	if !ended || !done.Failed || done.Account != "" {
		t.Fatalf("a failure did not report as one: %+v", done)
	}
	if hasTool(agent, "gmail_search") {
		t.Fatalf("a failed attempt armed the family: %v", beltNames(agent))
	}
	if output := lastToolOutput(t, collected); !strings.Contains(output, "did not finish connecting") {
		t.Fatalf("the model was not told what happened: %q", output)
	}
}

// Nobody is watching — a headless run, a task node — so the question would be
// asked into an empty room. The call refuses with a result the model can act on
// instead of blocking on an answer that can never arrive.
func TestConnectingRefusesWhenNobodyIsWatching(t *testing.T) {
	hub := &fakeHub{}
	agent := connectAgent(t, &scriptedCompleter{}, hub, false)

	text, isError, err := agent.useService(context.Background(), "google")
	if err != nil || !isError {
		t.Fatalf("an unwatched ask: %q isError=%v err=%v", text, isError, err)
	}
	if !strings.Contains(text, "nobody is watching") {
		t.Fatalf("the refusal does not say why: %q", text)
	}
	if hub.attempts() != 0 {
		t.Fatal("an unwatched session still started a connection")
	}
}

// ── the other door ──────────────────────────────────────────────────────────

// An account connected from the SURFACE arms the family and leaves ONE line on
// the ambient queue. It must not start a turn: nobody is standing there waiting
// for an answer about a thing they just did themselves.
func TestNoteConnectedArmsAndQueuesWithoutWakingTheSession(t *testing.T) {
	completer := &scriptedCompleter{}
	agent := connectAgent(t, completer, &fakeHub{}, true)

	agent.NoteConnected("google", "you@example.test")

	if !hasTool(agent, "calendar_list") {
		t.Fatalf("the family did not arrive: %v", beltNames(agent))
	}
	agent.mu.Lock()
	queued := len(agent.steering)
	var text string
	if queued == 1 {
		text = agent.steering[0].text()
	}
	running := agent.running
	agent.mu.Unlock()
	if queued != 1 {
		t.Fatalf("the note was queued %d times", queued)
	}
	if !strings.Contains(text, "you@example.test") || !strings.Contains(text, "Google") {
		t.Fatalf("the note does not say what happened: %q", text)
	}
	if running || completer.requests() != 0 {
		t.Fatalf("a connection started a turn nobody asked for: %d requests", completer.requests())
	}

	// A service this build does not have does nothing at all rather than
	// queueing a line about a thing that cannot be used.
	agent.NoteConnected("dropbox", "you@example.test")
	agent.mu.Lock()
	queued = len(agent.steering)
	agent.mu.Unlock()
	if queued != 1 {
		t.Fatalf("an unknown service queued a note: %d", queued)
	}
}

// ── the list ────────────────────────────────────────────────────────────────

// The services list says the one thing each row is for: a connected account
// reads as the address it is connected as, an unconnected one as an id to ask
// for.
func TestTheServicesListSaysWhatIsConnectedAndWhatIsAvailable(t *testing.T) {
	available := renderServices([]connectStatus{{ID: "google", Name: "Google", Blurb: "read your mail"}}, "")
	if !strings.Contains(available, "Not connected yet (1)") || !strings.Contains(available, "google") {
		t.Fatalf("an unconnected row does not offer itself: %q", available)
	}
	connected := renderServices([]connectStatus{{ID: "google", Name: "Google", Connected: true, Account: "you@example.test"}}, "")
	if !strings.Contains(connected, "connected as you@example.test") {
		t.Fatalf("a connected row does not say who: %q", connected)
	}
	// THE EMPTINESS LAW: one sentence, never a heading over nothing.
	empty := renderServices(nil, "")
	if strings.Contains(empty, "use_service") || !strings.Contains(empty, "No accounts") {
		t.Fatalf("an empty list is not one sentence: %q", empty)
	}
}

// ── the list stays small when the catalog is large ─────────────────────────

// A couple of hundred accounts must not cost a page and a half of context every
// time the model wonders what is connected.
func TestTheServicesListStaysSmallOverALargeCatalog(t *testing.T) {
	services := []connectStatus{{
		ID: "google", Name: "Google", Connected: true, Account: "you@example.test",
		Blurb: "Read and send Gmail; read and manage Calendar.",
	}}
	for index := 0; index < 250; index++ {
		name := "service" + strconv.Itoa(index)
		services = append(services, connectStatus{
			ID: name, Name: "Service " + strconv.Itoa(index), Auth: "key",
			Blurb: "Reach your Service " + strconv.Itoa(index) + " account at api.example.com, with a key you already hold.",
		})
	}
	whole := renderServices(services, "")
	if len(whole) > 6*1024 {
		t.Errorf("the list is %d characters, which is a page and a half of context", len(whole))
	}
	// The connected one is written out in full; the rest are ids only.
	if !strings.Contains(whole, "google — Google, connected as you@example.test") {
		t.Errorf("the connected account is not written out: %q", whole)
	}
	if strings.Contains(whole, "Reach your Service 7 account") {
		t.Errorf("an unconnected account brought its whole line with it")
	}
	if !strings.Contains(whole, "Not connected yet (250)") {
		t.Errorf("the count is not there: %q", whole)
	}
	for _, line := range strings.Split(whole, "\n") {
		if len(line) > 120 {
			t.Errorf("a line of %d characters: %q", len(line), line)
		}
	}

	// A filter searches the same list by name or by id.
	narrowed := renderServices(services, "service17")
	if !strings.Contains(narrowed, "service17,") && !strings.HasSuffix(strings.TrimSpace(narrowed), "service17") {
		if !strings.Contains(narrowed, "service17") {
			t.Errorf("the filter found nothing: %q", narrowed)
		}
	}
	if strings.Contains(narrowed, "service2,") {
		t.Errorf("the filter let something else through: %q", narrowed)
	}
	// THE EMPTINESS LAW again: a filter that matches nothing says THAT.
	none := renderServices(services, "nothing-like-this")
	if !strings.Contains(none, "No account here matches") {
		t.Errorf("a filter that matches nothing: %q", none)
	}
}

// drainConnect drains one turn's stream, answering every connect question as it
// arrives. It fails rather than hanging: a question that blocks forever is the
// fault worth catching here.
func drainConnect(t *testing.T, events <-chan Event, answer func(Event)) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
			if event.Kind == EventConnectAsk && answer != nil {
				answer(event)
			}
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
			return nil
		}
	}
}

// ── the two hands that act ──────────────────────────────────────────────────

// sendTurn is the script for one turn that sends a message and then says
// something.
func sendTurn() []step {
	return []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "gmail_send",
				`{"to":"alice@example.com","subject":"Lunch tomorrow?","body":"Noon works."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}
}

// armGoogle puts the account's family on the belt the way a conversation that
// picked it up earlier is already holding it.
func armGoogle(t *testing.T, agent *Agent) {
	t.Helper()
	if _, err := agent.armFamily(agent.familyTools(connectStatus{ID: "google", Name: "Google", Auth: "browser"})); err != nil {
		t.Fatalf("arm the family: %v", err)
	}
}

// THE RE-CONSENT PATH, end to end. The account was connected when this
// conversation picked it up, and what the person agreed to then does not cover
// sending — so the send is not an error, it is the ordinary question, and the
// message goes the moment they have signed in again.
func TestASendOnAnAccountThatNoLongerStandsAsksAndThenSends(t *testing.T) {
	service := &stubTransport{answer: `{"id":"sent-1"}`}
	hub := &fakeHub{transport: service}
	completer := &scriptedCompleter{steps: sendTurn()}
	agent := connectAgent(t, completer, hub, true)
	armGoogle(t, agent)

	events, err := agent.Submit(context.Background(), "tell alice noon works")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainConnect(t, events, func(event Event) {
		agent.ResolveConnect(event.ConnectID, true)
	})

	ask, asked := firstOfKind(collected, EventConnectAsk)
	if !asked {
		t.Fatalf("nobody was asked to sign in again: %v", kinds(collected))
	}
	if ask.ServiceName != "Google" || ask.Service != "google" {
		t.Fatalf("the question is missing its subject: %+v", ask)
	}
	if hub.attempts() != 1 {
		t.Fatalf("the sign-in ran %d times, want once", hub.attempts())
	}
	if service.calls() != 1 {
		t.Fatalf("the message was sent %d times, want once", service.calls())
	}
	if output := lastToolOutput(t, collected); !strings.Contains(output, "Sent to alice@example.com") {
		t.Fatalf("the model was not told what went: %q", output)
	}
}

// A person who will not sign in again is told nothing left, in the words the
// ordinary refusal uses — and nothing left.
func TestASendOnAnAccountThatNoLongerStandsSendsNothingWhenTheAnswerIsNo(t *testing.T) {
	service := &stubTransport{}
	hub := &fakeHub{transport: service}
	completer := &scriptedCompleter{steps: sendTurn()}
	agent := connectAgent(t, completer, hub, true)
	armGoogle(t, agent)

	events, err := agent.Submit(context.Background(), "tell alice noon works")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainConnect(t, events, func(event Event) {
		agent.ResolveConnect(event.ConnectID, false)
	})

	if service.calls() != 0 {
		t.Fatal("a refused sign-in still sent the message")
	}
	if output := lastToolOutput(t, collected); !strings.Contains(output, "did not agree") {
		t.Fatalf("the model was not told plainly: %q", output)
	}
}

// A BLANKET ALLOW CANNOT SEND SOMEBODY'S MAIL. The policy allows everything and
// nobody is watching, so the call is refused rather than run: the question the
// gate wanted to ask has no reader.
func TestSendingMailWithoutApprovalDoesNotHappen(t *testing.T) {
	service := &stubTransport{answer: `{"id":"sent-1"}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	completer := &scriptedCompleter{steps: sendTurn()}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = false
	})
	armGoogle(t, agent)

	collected := collect(t, mustSubmit(t, agent, "tell alice noon works"))

	if service.calls() != 0 {
		t.Fatal("a message left without anybody approving it")
	}
	failed, ok := firstOfKind(collected, EventToolFailed)
	if !ok {
		t.Fatalf("the call did not refuse: %v", kinds(collected))
	}
	if !strings.Contains(failed.Output, "needs approval") || !strings.Contains(failed.Output, "gmail_send") {
		t.Fatalf("the refusal does not say why: %q", failed.Output)
	}
}

// And what the person is asked says what is about to leave: who it is going to
// and what it says it is about.
func TestTheSendQuestionSaysWhatIsAboutToLeave(t *testing.T) {
	service := &stubTransport{answer: `{"id":"sent-1"}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	completer := &scriptedCompleter{steps: sendTurn()}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armGoogle(t, agent)

	collected := drainAnswering(t, mustSubmit(t, agent, "tell alice noon works"), func(event Event) {
		agent.ResolveConsent(event.ID, true)
	})

	request, asked := firstOfKind(collected, EventConsentRequest)
	if !asked {
		t.Fatalf("nobody was asked: %v", kinds(collected))
	}
	if !strings.Contains(request.Hint, "alice@example.com") || !strings.Contains(request.Hint, "Lunch tomorrow?") {
		t.Fatalf("the question does not say what is leaving: %q", request.Hint)
	}
	if !strings.Contains(request.Rule, "in your name") {
		t.Fatalf("the question does not say why it is being asked: %q", request.Rule)
	}
	if service.calls() != 1 {
		t.Fatalf("an approved message was sent %d times, want once", service.calls())
	}
}

// The event half of the same law: the question says what is going on the
// calendar and when.
func TestTheCalendarQuestionSaysWhatIsAboutToHappen(t *testing.T) {
	service := &stubTransport{answer: `{"id":"ev-1"}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "calendar_create",
				`{"title":"Standup","start":"2026-08-18T09:00:00Z","attendees":"alice@example.com"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.AskConsent = true
	})
	armGoogle(t, agent)

	collected := drainAnswering(t, mustSubmit(t, agent, "put standup on tuesday"), func(event Event) {
		agent.ResolveConsent(event.ID, false)
	})

	request, asked := firstOfKind(collected, EventConsentRequest)
	if !asked {
		t.Fatalf("nobody was asked: %v", kinds(collected))
	}
	if !strings.Contains(request.Hint, "Standup") || !strings.Contains(request.Hint, "2026-08-18T09:00:00Z") {
		t.Fatalf("the question does not say what is happening: %q", request.Hint)
	}
	if service.calls() != 0 {
		t.Fatal("a refused event went on the calendar anyway")
	}
}

// ── the accounts a key opens ────────────────────────────────────────────────

// The whole of the key flow, end to end: the question says a key is wanted, the
// person gives one, the account connects with no page opened, and the one tool
// it brings is on the belt for the next turn.
func TestAnAccountOpenedWithAKeyAsksForOneAndArmsItsTool(t *testing.T) {
	hub := &fakeHub{keyService: true}
	completer := &scriptedCompleter{steps: useServiceTurn("stripe")}
	agent := connectAgent(t, completer, hub, true)

	events, err := agent.Submit(context.Background(), "look at the billing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var asked []Event
	collected := drainConnect(t, events, func(event Event) {
		asked = append(asked, event)
		agent.ResolveConnectKey(event.ConnectID, " sk-live-1 ")
	})

	if len(asked) != 1 {
		t.Fatalf("questions asked: %d", len(asked))
	}
	if !asked[0].NeedsKey {
		t.Errorf("the question must say a key is wanted: %+v", asked[0])
	}
	if asked[0].ServiceName != "Stripe" {
		t.Errorf("the question names %q", asked[0].ServiceName)
	}
	// NOTHING OPENS. There is no page for an account connected with a key.
	for _, event := range collected {
		if event.Kind == EventConnectAuth {
			t.Errorf("a page was opened for an account that has none: %+v", event)
		}
	}
	var done bool
	for _, event := range collected {
		if event.Kind == EventConnectDone {
			done = true
			if event.Failed {
				t.Errorf("the attempt failed: %+v", event)
			}
			// THE EMPTINESS LAW: a key says nothing about whose key it is.
			if event.Account != "" {
				t.Errorf("an account was invented: %q", event.Account)
			}
		}
	}
	if !done {
		t.Errorf("no outcome was reported: %v", kinds(collected))
	}
	if hub.gaveKey() != "sk-live-1" {
		t.Errorf("the key that arrived was %q", hub.gaveKey())
	}
	if !hasTool(agent, "stripe_request") {
		t.Fatalf("the account's tool is not on the belt: %v", beltNames(agent))
	}
	// And nothing else came with it.
	if hasTool(agent, "stripe_objects") {
		t.Errorf("a tool nobody built is on the belt")
	}
	if !strings.Contains(lastToolOutput(t, collected), "stripe_request") {
		t.Errorf("the result does not name what arrived: %q", lastToolOutput(t, collected))
	}
}

// A YES TO A QUESTION THAT WANTED A KEY IS NOT AN ANSWER.
func TestABareYesToAKeyQuestionDeclines(t *testing.T) {
	hub := &fakeHub{keyService: true}
	completer := &scriptedCompleter{steps: useServiceTurn("stripe")}
	agent := connectAgent(t, completer, hub, true)

	events, err := agent.Submit(context.Background(), "look at the billing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainConnect(t, events, func(event Event) {
		agent.ResolveConnect(event.ConnectID, true)
	})
	if hub.gaveKey() != "" {
		t.Errorf("something was connected on a bare yes: %q", hub.gaveKey())
	}
	if hasTool(agent, "stripe_request") {
		t.Errorf("a tool arrived for an account nobody connected")
	}
	if text := lastToolOutput(t, collected); !strings.Contains(text, "did not agree") {
		t.Errorf("the model was told the wrong thing: %q", text)
	}
}

// An empty key is a decline, and so is a plain no.
func TestAnEmptyKeyIsADecline(t *testing.T) {
	for _, answer := range []string{"", "   "} {
		hub := &fakeHub{keyService: true}
		completer := &scriptedCompleter{steps: useServiceTurn("stripe")}
		agent := connectAgent(t, completer, hub, true)

		events, err := agent.Submit(context.Background(), "look at the billing")
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		collected := drainConnect(t, events, func(event Event) {
			agent.ResolveConnectKey(event.ConnectID, answer)
		})
		if hub.gaveKey() != "" {
			t.Errorf("%q connected something", answer)
		}
		if text := lastToolOutput(t, collected); !strings.Contains(text, "did not agree") {
			t.Errorf("%q: the model was told %q", answer, text)
		}
	}
}

// A service that refuses the key is one honest result, not a Go error, and
// nothing is armed.
func TestAKeyTheServiceRefusesIsOneHonestResult(t *testing.T) {
	hub := &fakeHub{keyService: true, keyErr: errors.New("Stripe did not accept that key: 401 Unauthorized")}
	completer := &scriptedCompleter{steps: useServiceTurn("stripe")}
	agent := connectAgent(t, completer, hub, true)

	events, err := agent.Submit(context.Background(), "look at the billing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainConnect(t, events, func(event Event) {
		agent.ResolveConnectKey(event.ConnectID, "sk-wrong")
	})
	if hasTool(agent, "stripe_request") {
		t.Errorf("a tool arrived for an account that did not connect")
	}
	var failed bool
	for _, event := range collected {
		if event.Kind == EventConnectDone && event.Failed {
			failed = true
		}
	}
	if !failed {
		t.Errorf("the failure was not reported: %v", kinds(collected))
	}
	if text := lastToolOutput(t, collected); !strings.Contains(text, "did not work") {
		t.Errorf("the model was told %q", text)
	}
}

// The tool an account brings names the account and the address its paths hang
// off, and its calls go through the seam.
func TestTheRawCallToolNamesItsAddressAndMakesTheCall(t *testing.T) {
	hub := &fakeHub{keyService: true, keyConnected: true}
	agent := connectAgent(t, &scriptedCompleter{}, hub, true)

	text, isError, err := agent.useService(context.Background(), "stripe")
	if err != nil || isError {
		t.Fatalf("use_service: %q err=%v", text, err)
	}
	tool := beltTool(t, agent, "stripe_request")
	if !strings.Contains(tool.Description, "https://api.stripe.com") {
		t.Errorf("the description does not name the address: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, "shortened") {
		t.Errorf("the description does not say answers are bounded: %q", tool.Description)
	}

	answer, isError, err := tool.Execute(context.Background(), json.RawMessage(`{"method":"get","path":"/v1/customers","query":"limit=2"}`))
	if err != nil || isError {
		t.Fatalf("the call: %q isError=%v err=%v", answer, isError, err)
	}
	if calls := hub.rawCalls(); len(calls) != 1 || calls[0] != "get /v1/customers" {
		t.Errorf("calls = %v", calls)
	}

	// A call with no path is refused before it reaches anybody.
	if _, isError, _ := tool.Execute(context.Background(), json.RawMessage(`{"method":"get"}`)); !isError {
		t.Errorf("a call with no path must be refused")
	}
}

// THE GATE AND THE BELT AGREE ON WHICH TOOLS THESE ARE. The name the belt builds
// is the name internal/approval's floor matches, and the floor holds for every
// verb that changes something.
func TestARawCallThatChangesSomethingGoesThroughTheFloor(t *testing.T) {
	name := serviceRequestName("stripe")
	if name != "stripe"+approval.ServiceRequestSuffix {
		t.Fatalf("the tool is named %q", name)
	}
	allowAll := approval.Policy{Default: approval.ActionAllow}
	if decision := allowAll.Check(name, json.RawMessage(`{"method":"get","path":"/v1/customers"}`)); decision.Action != approval.ActionAllow {
		t.Errorf("a read = %+v, want allow", decision)
	}
	for _, method := range []string{"post", "PUT", "patch", "DELETE"} {
		args := json.RawMessage(`{"method":"` + method + `","path":"/v1/customers"}`)
		decision := allowAll.Check(name, args)
		if decision.Action != approval.ActionPrompt {
			t.Errorf("%s = %+v, want prompt", method, decision)
		}
		if !strings.Contains(decision.Rule, name) {
			t.Errorf("%s: the memo does not name the tool: %q", method, decision.Rule)
		}
	}
}
