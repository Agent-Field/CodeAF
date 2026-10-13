package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

type inboxFake struct {
	mu      sync.Mutex
	items   []InboxItem
	answers []InboxAnswerRequest
	err     error
}

func (f *inboxFake) InboxItems(context.Context) ([]InboxItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]InboxItem{}, f.items...), f.err
}
func (f *inboxFake) AnswerInbox(_ context.Context, item InboxItem, answer InboxAnswerRequest) (InboxAnswerResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, row := range f.items {
		if row.ID == item.ID {
			f.items = append(f.items[:i], f.items[i+1:]...)
			f.answers = append(f.answers, answer)
			return InboxAnswerResponse{OK: true}, nil
		}
	}
	return InboxAnswerResponse{}, errors.New("already answered")
}

type inboxTestClock struct {
	at        time.Time
	callbacks []func()
}

func inboxTestServer(t *testing.T) (*Server, *inboxTestClock) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := New(testToken, Options{Context: ctx})
	cancel()
	<-s.inbox.done
	clock := &inboxTestClock{at: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	s.inbox.now = func() time.Time { return clock.at }
	s.inbox.after = func(d time.Duration, fn func()) *time.Timer {
		if d <= 0 {
			t.Fatal("timer delay must be positive")
		}
		clock.callbacks = append(clock.callbacks, fn)
		return nil
	}
	t.Cleanup(s.Close)
	return s, clock
}
func inboxReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	return request(t, s, method, path, body, bearer(), "127.0.0.1:4000")
}
func inboxItems(t *testing.T, s *Server) []InboxItem {
	t.Helper()
	w := inboxReq(t, s, "GET", "/app/v1/inbox", "")
	if w.Code != 200 {
		t.Fatalf("Inbox: %d %s", w.Code, w.Body)
	}
	var reply InboxResponse
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return reply.Items
}
func TestInboxAllSixteenKindsAreStableDerivedAndOldestFirst(t *testing.T) {
	s, c := inboxTestServer(t)
	kinds := []string{"question", "clarification", "permission", "your_call", "approval_plan", "approval_proof", "approval_start", "budget", "out_of_rounds", "failed", "changes_requested", "conflict", "publish_failed", "runner_offline", "order_ask", "reminder"}
	source := &inboxFake{}
	for i, kind := range kinds {
		source.items = append(source.items, InboxItem{ID: "source:" + kind, Kind: kind, Source: SourceRef{Kind: "agent", ID: "a"}, CreatedAt: c.at.Add(-time.Duration(i) * time.Minute), Context: InboxContext{Before: []DisplayEntry{{Kind: "message", Text: "one"}, {Kind: "message", Text: "two"}, {Kind: "message", Text: "three"}}}})
	}
	s.SetInboxSource("native", source)
	first := inboxItems(t, s)
	again := inboxItems(t, s)
	if len(first) != 16 || !reflect.DeepEqual(first, again) {
		t.Fatalf("unstable set: %#v", first)
	}
	if first[0].Kind != "reminder" || first[15].Kind != "question" {
		t.Fatal("not oldest first")
	}
	for _, item := range first {
		if len(item.Context.Before) != 2 || item.Context.Before[0].Text != "two" {
			t.Fatal("wrong before context")
		}
		if item.Path == nil || item.Verbs == nil || item.WhoCanAnswer == nil {
			t.Fatal("null array")
		}
	}
	events, _, _, _, _ := s.events.read(0)
	if len(events) != 16 {
		t.Fatalf("duplicate additions: %d", len(events))
	}
	source.items = source.items[1:]
	if len(inboxItems(t, s)) != 15 {
		t.Fatal("resolved source remained")
	}
	source.err = errors.New("offline")
	if len(inboxItems(t, s)) != 15 {
		t.Fatal("offline falsely resolved items")
	}
	s.SetInboxSource("native", nil)
	if len(inboxItems(t, s)) != 0 {
		t.Fatal("detached source remained")
	}
}
func TestInboxAnswerWaitsFiveSecondsUndoSendsNothingAndAnswersOnce(t *testing.T) {
	s, c := inboxTestServer(t)
	source := &inboxFake{items: []InboxItem{{ID: "ask:one", Kind: "question", Title: "Which version?"}}}
	s.SetInboxSource("engine", source)
	w := inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/answer", `{"choice":"v2","words":"keep the API"}`)
	if w.Code != 200 {
		t.Fatalf("answer %d %s", w.Code, w.Body)
	}
	var reply InboxAnswerResponse
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	if reply.UndoUntil == nil || reply.UndoUntil.Sub(c.at) != 5*time.Second || len(source.answers) != 0 {
		t.Fatal("answer was sent before hold")
	}
	w = inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/answer", `{"choice":"v1"}`)
	if w.Code != 409 {
		t.Fatal("duplicate was accepted")
	}
	w = inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/undo", `{}`)
	if w.Code != 200 {
		t.Fatalf("Undo: %d %s", w.Code, w.Body)
	}
	c.at = c.at.Add(5 * time.Second)
	c.callbacks[0]()
	if len(source.answers) != 0 {
		t.Fatal("canceled decision was forwarded")
	}
	w = inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/answer", `{"choice":"v2","words":"keep the API"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	c.at = c.at.Add(5 * time.Second)
	w = inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/undo", `{}`)
	if w.Code != 409 {
		t.Fatal("Undo at deadline accepted")
	}
	c.callbacks[1]()
	c.callbacks[1]()
	if len(source.answers) != 1 || source.answers[0].Words != "keep the API" || len(inboxItems(t, s)) != 0 {
		t.Fatal("answer not forwarded exactly once")
	}
}
func TestInboxInPlaceAnswerCancelsHeldCallbackAndReportsResolution(t *testing.T) {
	s, c := inboxTestServer(t)
	source := &inboxFake{items: []InboxItem{{ID: "ask:one", Kind: "clarification"}}}
	s.SetInboxSource("engine", source)
	_ = inboxItems(t, s)
	w := inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/answer", `{"words":"v2"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	source.items = nil
	s.ResolveInbox("ask:one", "someone_else", "sam")
	c.at = c.at.Add(5 * time.Second)
	c.callbacks[0]()
	if len(source.answers) != 0 {
		t.Fatal("answered a retired question")
	}
	events, _, _, _, _ := s.events.read(0)
	last := events[len(events)-1]
	var resolved InboxResolvedEvent
	_ = json.Unmarshal(last.data, &resolved)
	if resolved.How != "someone_else" || resolved.By != "sam" {
		t.Fatalf("wrong resolution %s", last.data)
	}
}
func TestInboxRoutesRejectInvalidRequestsReplayKeyedAnswersAndRemainGuarded(t *testing.T) {
	s, _ := inboxTestServer(t)
	source := &inboxFake{items: []InboxItem{{ID: "ask:one", Kind: "permission"}}}
	s.SetInboxSource("engine", source)
	for _, body := range []string{"", "null", "[]", `{} {}`, `{"words":null}`, `{"unknown":true}`, `{}`, `{"expect":{}}`} {
		w := inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/answer", body)
		if w.Code != 422 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	h := bearer()
	h.Set("Idempotency-Key", "one")
	body := `{"verb":"allow_once","rule":{"match":"class","who":"agent"},"items":[{"id":"1","verb":"deny"}]}`
	a := request(t, s, "POST", "/app/v1/inbox/ask:one/answer", body, h, "127.0.0.1:4000")
	b := request(t, s, "POST", "/app/v1/inbox/ask:one/answer", body, h, "127.0.0.1:4000")
	if a.Code != 200 || a.Body.String() != b.Body.String() {
		t.Fatal("keyed retry changed response")
	}
	if w := request(t, s, "POST", "/app/v1/inbox/ask:one/answer", `{"verb":"deny"}`, h, "127.0.0.1:4000"); w.Code != 409 {
		t.Fatal("key reused with changed body")
	}
	if w := inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/undo", `{"words":"bad"}`); w.Code != 422 {
		t.Fatal("Undo accepted answer fields")
	}
	for _, path := range []string{"/app/v1/inbox", "/app/v1/inbox/ask:one/answer", "/app/v1/inbox/ask:one/undo"} {
		if w := request(t, s, "GET", path, "", nil, "127.0.0.1:4000"); w.Code != 401 {
			t.Fatal("unguarded Inbox route")
		}
	}
	if w := inboxReq(t, s, "DELETE", "/app/v1/inbox", `{}`); w.Code != 405 {
		t.Fatal("wrong method accepted")
	}
}

type inboxEngine struct {
	*turnEngine
	mu        sync.Mutex
	questions []session.Question
	answered  []session.Answer
}

func (e *inboxEngine) OpenQuestions() []session.Question {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]session.Question{}, e.questions...)
}
func (e *inboxEngine) ResolveQuestion(answer session.Answer) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, q := range e.questions {
		if q.Kind == answer.Kind && q.ID == answer.ID {
			e.questions = append(e.questions[:i], e.questions[i+1:]...)
			e.answered = append(e.answered, answer)
			return nil
		}
	}
	return errors.New("already answered")
}
func TestInboxCanonicalEngineQuestionAndSourceRouteShareOneHold(t *testing.T) {
	f := newTurnFixture(t)
	f.server.inbox.cancel()
	<-f.server.inbox.done
	t.Cleanup(f.server.Close)
	engine := &inboxEngine{turnEngine: f.engine, questions: []session.Question{{Kind: session.QuestionAsk, Ask: session.AskClarification, ID: 41, Head: "Which API?", Asked: f.registry.now(), Options: []session.AnswerOption{{Key: "v2", Label: "v2"}}}}}
	f.registry.live[f.id].conn.Engine = engine
	c := &inboxTestClock{at: f.registry.now()}
	f.server.inbox.now = func() time.Time { return c.at }
	f.server.inbox.after = func(_ time.Duration, fn func()) *time.Timer { c.callbacks = append(c.callbacks, fn); return nil }
	items := inboxItems(t, f.server)
	if len(items) != 1 || items[0].Kind != "clarification" {
		t.Fatal(items)
	}
	id := items[0].ID
	w := inboxReq(t, f.server, "POST", "/app/v1/asks/ask:41/answer", `{"choice":"v2","words":"Use Contract 2"}`)
	if w.Code != 200 {
		t.Fatalf("source hold %d %s", w.Code, w.Body)
	}
	w = inboxReq(t, f.server, "POST", "/app/v1/inbox/"+url.PathEscape(id)+"/answer", `{"choice":"v2"}`)
	if w.Code != 409 {
		t.Fatal("Inbox acquired a second hold")
	}
	c.at = c.at.Add(5 * time.Second)
	c.callbacks[0]()
	if len(engine.answered) != 1 || engine.answered[0].ID != 41 || engine.answered[0].Change != "Use Contract 2" {
		t.Fatal("did not reach canonical resolver")
	}
	if len(inboxItems(t, f.server)) != 0 {
		t.Fatal("resolved engine item remains")
	}
}
func TestInboxFactoryContractTwoMergeFiltersPersonAndForwardsNativeUndo(t *testing.T) {
	s, c := inboxTestServer(t)
	var paths []string
	var decisions []map[string]any
	var mu sync.Mutex
	queue := factoryapi.Queue{Me: "sam", Jobs: []factoryapi.Job{{ID: 12, JobID: "j_same", Title: "Ship", State: "needs_you", Assignee: "sam", Version: 8, HeadCommit: "abc", NeedsYou: &factoryapi.NeedsYou{Kind: "approve_proof", Since: c.at, WhoCanAnswer: "job_person"}, Can: factoryapi.Can{Approve: true}}, {ID: 13, JobID: "j_other", Assignee: "other", NeedsYou: &factoryapi.NeedsYou{Kind: "approve_start", Since: c.at, WhoCanAnswer: "job_person"}, Can: factoryapi.Can{Approve: true}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing person credential")
		}
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v1/queue":
			_ = json.NewEncoder(w).Encode(queue)
		case "/v1/jobs/12/approve":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			decisions = append(decisions, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"undo_until": c.at.Add(5 * time.Second)})
		case "/v1/jobs/12/undo":
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, id := range []string{"solo", "team"} {
		s.SetInboxSource(id, &FactoryInboxSource{ID: id, URL: server.URL, Token: "secret"})
	}
	items := inboxItems(t, s)
	if len(items) != 2 || items[0].ID == items[1].ID || items[0].Kind != "approval_proof" {
		t.Fatalf("factory merge: %+v", items)
	}
	id := items[0].ID
	w := inboxReq(t, s, "POST", "/app/v1/inbox/"+url.PathEscape(id)+"/answer", `{"verb":"approve","expect":{"version":8}}`)
	if w.Code != 200 {
		t.Fatalf("approve %d %s", w.Code, w.Body)
	}
	if len(c.callbacks) != 0 {
		t.Fatal("factory approval acquired a second hold")
	}
	if len(decisions) != 1 || decisions[0]["commit"] != "abc" {
		t.Fatal("approval lost proof commit")
	}
	w = inboxReq(t, s, "POST", "/app/v1/inbox/"+url.PathEscape(id)+"/undo", `{}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if paths[len(paths)-1] != "/v1/jobs/12/undo" {
		t.Fatal("Undo not forwarded")
	}
}
func TestInboxFactoryAllNeedsKindsUseStableWaitIdentity(t *testing.T) {
	kinds := []string{"question", "approve_plan", "approve_proof", "approve_start", "budget", "out_of_rounds", "failed", "changes_requested", "conflict", "publish_failed", "runner_offline"}
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	queue := factoryapi.Queue{Me: "sam"}
	for i, kind := range kinds {
		queue.Jobs = append(queue.Jobs, factoryapi.Job{ID: i + 1, JobID: fmt.Sprintf("j_%d", i), State: "needs_you", Assignee: "sam", NeedsYou: &factoryapi.NeedsYou{Kind: kind, Since: at, WhoCanAnswer: "job_person"}, Can: factoryapi.Can{Answer: true}})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(queue) }))
	defer server.Close()
	source := &FactoryInboxSource{ID: "team", URL: server.URL}
	first, err := source.InboxItems(context.Background())
	if err != nil || len(first) != len(kinds) {
		t.Fatalf("kinds %v: %v", first, err)
	}
	queue.Jobs[0].Title = "Changed words"
	second, _ := source.InboxItems(context.Background())
	if first[0].ID != second[0].ID {
		t.Fatal("title changed item identity")
	}
	queue.Jobs[0].NeedsYou.Since = at.Add(time.Second)
	third, _ := source.InboxItems(context.Background())
	if first[0].ID == third[0].ID {
		t.Fatal("new wait reused old identity")
	}
}
func TestInboxAskProducerRetainsClimbedPathAndWaitingOnly(t *testing.T) {
	asks := []Ask{{ID: "order:7:run:2", Kind: "reminder", Source: SourceRef{Kind: "order", ID: "7"}, Path: []SourceRef{{Kind: "order", ID: "7"}, {Kind: "member", ID: "m"}, {Kind: "crew", ID: "c"}}, State: "waiting"}, {ID: "old", Kind: "order_ask", State: "answered"}}
	called := 0
	source := &AskInboxSource{List: func(context.Context) ([]Ask, error) { return asks, nil }, Answer: func(_ context.Context, ask Ask, answer InboxAnswerRequest) (InboxAnswerResponse, error) {
		called++
		asks[0].State = "answered"
		return InboxAnswerResponse{OK: true}, nil
	}}
	items, err := source.InboxItems(context.Background())
	if err != nil || len(items) != 1 || len(items[0].Path) != 3 || items[0].ID != "ask:order:7:run:2" {
		t.Fatal(items, err)
	}
	_, err = source.AnswerInbox(context.Background(), items[0], InboxAnswerRequest{Verb: "go"})
	if err != nil || called != 1 {
		t.Fatal("source answer failed")
	}
	_, err = source.AnswerInbox(context.Background(), items[0], InboxAnswerRequest{Verb: "go"})
	if err == nil || called != 1 {
		t.Fatal("source accepted second answer")
	}
}
func TestInboxCloseCancelsPendingAnswers(t *testing.T) {
	s, c := inboxTestServer(t)
	source := &inboxFake{items: []InboxItem{{ID: "one", Kind: "question"}}}
	s.SetInboxSource("engine", source)
	w := inboxReq(t, s, "POST", "/app/v1/inbox/one/answer", `{"words":"yes"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	s.Close()
	c.at = c.at.Add(5 * time.Second)
	c.callbacks[0]()
	if len(source.answers) != 0 {
		t.Fatal("shutdown sent a pending answer")
	}
}

func TestInboxFactoryEventsAreScopedFilteredAndReconnectCursorIsPreserved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := []FactoryEvent{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/events" || r.Header.Get("Authorization") != "Bearer memory-only" || r.Header.Get("Last-Event-ID") != "live_6" {
			t.Error("wrong event request")
		}
		_, _ = fmt.Fprint(w, "id: live_7\nevent: machine\ndata: {\"name\":\"private\"}\n\nid: live_8\nevent: needs_you\ndata: {\"job\":12,\"kind\":\"approve_plan\"}\n\nid: live_9\nevent: job.state\ndata: {\"job\":12,\"state\":\"needs_you\"}\n\n")
	}))
	defer server.Close()
	source := &FactoryInboxSource{ID: "team", URL: server.URL, Token: "memory-only"}
	cursor := "live_6"
	source.followInbox(ctx, &cursor, func(event FactoryEvent) { seen = append(seen, event) })
	if cursor != "live_9" || len(seen) != 2 || seen[0].Factory != "team" || seen[0].Type != "needs_you" {
		t.Fatalf("event relay: %s %+v", cursor, seen)
	}
}
func TestInboxStaleFactoryExpectAndWrongActionNeverReachFactory(t *testing.T) {
	source := &FactoryInboxSource{jobs: map[string]factoryapi.Job{"one": {State: "needs_you", Version: 4}}}
	item := InboxItem{ID: "one", Verbs: []string{"approve"}}
	version := uint64(3)
	if err := source.ValidateInbox(context.Background(), item, InboxAnswerRequest{Verb: "approve", Expect: &AgentExpect{Version: &version}}); err == nil {
		t.Fatal("stale approval accepted")
	}
	if err := source.ValidateInbox(context.Background(), item, InboxAnswerRequest{Verb: "answer", Words: "yes"}); err == nil {
		t.Fatal("unoffered action accepted")
	}
}
func TestInboxTimerBeforeDeadlineReschedulesWithoutSending(t *testing.T) {
	s, c := inboxTestServer(t)
	source := &inboxFake{items: []InboxItem{{ID: "one", Kind: "question"}}}
	s.SetInboxSource("source", source)
	_, err := s.AnswerInbox(context.Background(), "one", InboxAnswerRequest{Words: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	c.at = c.at.Add(4 * time.Second)
	c.callbacks[0]()
	if len(source.answers) != 0 || len(c.callbacks) != 2 {
		t.Fatal("early timer bypassed deadline")
	}
	c.at = c.at.Add(time.Second)
	c.callbacks[1]()
	if len(source.answers) != 1 {
		t.Fatal("deadline did not forward")
	}
}

func TestInboxDetachCancelsUnsentDecision(t *testing.T) {
	s, c := inboxTestServer(t)
	source := &inboxFake{items: []InboxItem{{ID: "one", Kind: "question"}}}
	s.SetInboxSource("source", source)
	_, err := s.AnswerInbox(context.Background(), "one", InboxAnswerRequest{Words: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	s.SetInboxSource("source", nil)
	c.at = c.at.Add(5 * time.Second)
	c.callbacks[0]()
	if len(source.answers) != 0 {
		t.Fatal("detached source received decision")
	}
}
func TestInboxFactorySomeoneElseAnswerHasOneResolution(t *testing.T) {
	s, c := inboxTestServer(t)
	queue := factoryapi.Queue{Me: "sam", Jobs: []factoryapi.Job{{ID: 1, JobID: "j_1", State: "needs_you", NeedsYou: &factoryapi.NeedsYou{Kind: "question", Since: c.at}, Can: factoryapi.Can{Answer: true}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(queue) }))
	defer server.Close()
	s.SetInboxSource("team", &FactoryInboxSource{ID: "team", URL: server.URL})
	if len(inboxItems(t, s)) != 1 {
		t.Fatal("question missing")
	}
	queue.Jobs[0].NeedsYou = nil
	queue.Jobs[0].Can.Answer = false
	queue.Jobs[0].Questions = []factory.Question{{State: "answered", Answer: &factory.Answer{By: "raj", Text: "use v2", At: c.at.Add(time.Second)}}}
	if len(inboxItems(t, s)) != 0 {
		t.Fatal("answer remained")
	}
	events, _, _, _, _ := s.events.read(0)
	var resolution InboxResolvedEvent
	_ = json.Unmarshal(events[len(events)-1].data, &resolution)
	if resolution.How != "someone_else" || resolution.By != "raj" {
		t.Fatalf("resolution %s", events[len(events)-1].data)
	}
	item := readInboxItem(t, s, resolution.Item)
	if item.Answer == nil || item.Answer.Words != "use v2" || item.Receipt == nil || item.Receipt.By != "raj" || item.Receipt.Text != "use v2" {
		t.Fatalf("factory answer detail: %+v", item)
	}
}

func TestInboxFactoryExistingApprovalHoldSurvivesRefreshAndReattachment(t *testing.T) {
	s, c := inboxTestServer(t)
	until := c.at.Add(5 * time.Second)
	queue := factoryapi.Queue{Me: "sam", Jobs: []factoryapi.Job{{ID: 1, JobID: "j_1", State: "needs_you", Assignee: "sam", NeedsYou: &factoryapi.NeedsYou{Kind: "approve_start", Since: c.at, WhoCanAnswer: "job_person"}, Can: factoryapi.Can{Approve: true}}}}
	undone := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			undone++
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		_ = json.NewEncoder(w).Encode(queue)
	}))
	defer server.Close()
	s.SetInboxSource("solo", &FactoryInboxSource{ID: "solo", URL: server.URL})
	first := inboxItems(t, s)
	queue.Jobs[0].Approval = &factory.Approval{State: "approving", Kind: "start", DecidedBy: "sam", UndoUntil: until}
	queue.Jobs[0].Can = factoryapi.Can{Undo: true}
	queue.Jobs[0].NeedsYou.Since = c.at.Add(time.Second)
	next := inboxItems(t, s)
	if len(next) != 1 || next[0].ID != first[0].ID {
		t.Fatal("pending approval changed identity")
	}
	if s.inbox.pending[first[0].ID] == nil {
		t.Fatal("existing source hold not recovered")
	}
	w := inboxReq(t, s, "POST", "/app/v1/inbox/"+url.PathEscape(first[0].ID)+"/undo", `{}`)
	if w.Code != 200 || undone != 1 {
		t.Fatalf("reattached Undo %d %s", w.Code, w.Body)
	}
}

func TestInboxFactoryDelayedAnswerKeepsOriginalVersion(t *testing.T) {
	s, c := inboxTestServer(t)
	posts := 0
	queue := factoryapi.Queue{Me: "sam", Jobs: []factoryapi.Job{{ID: 1, JobID: "j_1", State: "needs_you", Version: 4, NeedsYou: &factoryapi.NeedsYou{Kind: "question", Since: c.at}, Can: factoryapi.Can{Answer: true}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		_ = json.NewEncoder(w).Encode(queue)
	}))
	defer server.Close()
	s.SetInboxSource("team", &FactoryInboxSource{ID: "team", URL: server.URL})
	items := inboxItems(t, s)
	_, err := s.AnswerInbox(context.Background(), items[0].ID, InboxAnswerRequest{Words: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	queue.Jobs[0].Version = 5
	c.at = c.at.Add(5 * time.Second)
	c.callbacks[0]()
	if posts != 0 {
		t.Fatal("a refresh silently accepted changed source evidence")
	}
	if len(inboxItems(t, s)) != 1 {
		t.Fatal("stale answer resolved its source")
	}
}

func TestInboxFactoryUndoForwardsSourceExpect(t *testing.T) {
	received := factoryapi.Expect{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Expect factoryapi.Expect `json:"expect"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		received = body.Expect
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	source := &FactoryInboxSource{URL: server.URL, jobs: map[string]factoryapi.Job{"one": {ID: 12}}}
	state := "needs_you"
	version := uint64(9)
	err := source.UndoInbox(context.Background(), InboxItem{ID: "one"}, &AgentExpect{State: &state, Version: &version})
	if err != nil || received.State != state || received.Version == nil || *received.Version != 9 {
		t.Fatal("Undo dropped source expectation", err)
	}
}

func readInboxItem(t *testing.T, s *Server, id string) InboxItem {
	t.Helper()
	w := inboxReq(t, s, "GET", "/app/v1/inbox/"+url.PathEscape(id), "")
	if w.Code != 200 {
		t.Fatalf("item: %d %s", w.Code, w.Body)
	}
	var item InboxItem
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestInboxItemReadRetainsAnswerReceiptAfterHoldAndExpires(t *testing.T) {
	s, c := inboxTestServer(t)
	source := &inboxFake{items: []InboxItem{{ID: "ask:one", Kind: "clarification", Title: "Which API?"}}}
	s.SetInboxSource("engine", source)
	item := readInboxItem(t, s, "ask:one")
	if item.Title != "Which API?" || item.Answer != nil || item.Receipt != nil || item.Resolved != nil {
		t.Fatalf("waiting item: %+v", item)
	}
	turnStatus(t, inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/answer", `{"words":"keep v2"}`), 200)
	if readInboxItem(t, s, "ask:one").Resolved != nil {
		t.Fatal("held answer marked done before delivery")
	}
	turnStatus(t, inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/undo", `{}`), 200)
	if readInboxItem(t, s, "ask:one").Answer != nil {
		t.Fatal("Undo retained an unsent answer")
	}
	turnStatus(t, inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/answer", `{"words":"keep v2"}`), 200)
	c.at = c.at.Add(inboxAnswerHold)
	c.callbacks[1]()
	if len(inboxItems(t, s)) != 0 {
		t.Fatal("answered item remains in waiting list")
	}
	item = readInboxItem(t, s, "ask:one")
	if item.Answer == nil || item.Answer.Words != "keep v2" || item.Receipt == nil || item.Receipt.Text != "Answered · keep v2" || item.Resolved == nil || item.Resolved.How != "here" {
		t.Fatalf("resolved detail: %+v", item)
	}
	turnStatus(t, inboxReq(t, s, "POST", "/app/v1/inbox/ask:one/answer", `{"words":"again"}`), 409)
	c.at = c.at.Add(inboxResolvedHold)
	turnStatus(t, inboxReq(t, s, "GET", "/app/v1/inbox/ask:one", ""), 404)
	turnStatus(t, inboxReq(t, s, "GET", "/app/v1/inbox/unknown", ""), 404)
	turnStatus(t, inboxReq(t, s, "PUT", "/app/v1/inbox/unknown", `{}`), 405)
	w := request(t, s, "GET", "/app/v1/inbox/unknown", "", nil, "127.0.0.1:4000")
	if w.Code != 401 {
		t.Fatalf("unguarded item route: %d", w.Code)
	}
}

func TestInboxItemReadKeepsOutsideEngineAnswerAndCanonicalReceipt(t *testing.T) {
	s, c := inboxTestServer(t)
	q := session.Question{ID: 1, Kind: session.QuestionAsk, Head: "Which file?", Asked: c.at}
	id := inboxQuestionID("agent", q)
	source := &inboxFake{items: []InboxItem{{ID: id, Kind: "question"}}}
	s.SetInboxSource("engine", source)
	_ = readInboxItem(t, s, id)
	source.items = nil
	s.resolveInboxQuestion("agent", session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &session.Answer{Change: "src/api.go", From: "sam", DecidedBy: session.DecidedByPerson, At: c.at}})
	item := readInboxItem(t, s, id)
	if item.Answer == nil || item.Answer.Words != "src/api.go" || item.Receipt == nil || item.Receipt.By != "sam" || item.Receipt.Link == nil || item.Receipt.Link.Kind != "ask" || item.Resolved.By != "sam" {
		t.Fatalf("outside answer: %+v", item)
	}
}
