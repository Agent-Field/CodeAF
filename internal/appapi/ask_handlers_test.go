package appapi

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The fake is the session question interface, never another resource's store.
type askEngine struct {
	*turnEngine
	questionMu   sync.Mutex
	open         []session.Question
	answers      []session.Answer
	holds        []string
	events       chan session.Event
	refused      bool
	stops        int
	replyRunning bool
}

func (e *askEngine) OpenQuestions() []session.Question {
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	return append([]session.Question{}, e.open...)
}
func (e *askEngine) ResolveQuestion(answer session.Answer) error {
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	if e.refused {
		return errors.New("engine refused this answer")
	}
	for i, q := range e.open {
		if q.Kind == answer.Kind && q.Token() == answerTokenForTest(answer) {
			e.answers = append(e.answers, answer)
			e.open = append(e.open[:i], e.open[i+1:]...)
			e.events <- session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer}
			return nil
		}
	}
	return errors.New("already answered")
}
func answerTokenForTest(a session.Answer) string {
	if a.Ref != "" {
		return a.Ref
	}
	return (&session.Question{ID: a.ID}).Token()
}
func (e *askEngine) HoldQuestion(_ session.QuestionKind, token string) {
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	e.holds = append(e.holds, token)
}
func (e *askEngine) StopWork() error {
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	e.stops++
	return nil
}
func (e *askEngine) WatchQuestions() (<-chan session.Event, func()) { return e.events, func() {} }
func (e *askEngine) put(q session.Question) {
	e.questionMu.Lock()
	e.open = append(e.open, q)
	e.questionMu.Unlock()
}

type askFixture struct {
	*turnFixture
	door *askEngine
}

func newAskFixture(t *testing.T) *askFixture {
	base := newTurnFixture(t)
	door := &askEngine{turnEngine: base.engine, events: make(chan session.Event, 32)}
	base.registry.live[base.id].conn.Engine = door
	base.server.options.ProfileDir = base.home
	base.server.SetInboxSource("engine", nil)
	return &askFixture{turnFixture: base, door: door}
}
func (f *askFixture) ask(t *testing.T, q session.Question) string {
	t.Helper()
	f.door.put(q)
	id := askID(f.id, q)
	turnStatus(t, f.request(t, "GET", "/app/v1/asks/"+id, ""), 200)
	return id
}
func permissionQuestion(id uint64) session.Question {
	return session.Question{ID: id, Kind: session.QuestionConsent, Ask: session.AskPermission, Head: "Allow git status?", Reason: "asks for this command", Asked: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC), Blocking: session.Blocking{Turn: true}, Subject: session.SubjectRef{Kind: session.SubjectCall, Name: "bash", CallID: "call"}, PermissionCommand: "git status", Options: session.AnswerOptions(session.QuestionConsent), Stakes: session.StakesCostly}
}
func readAsk(t *testing.T, w *httptest.ResponseRecorder) Ask {
	t.Helper()
	turnStatus(t, w, 200)
	var row Ask
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestAskReadContextIdentityAndClarificationReceipt(t *testing.T) {
	f := newAskFixture(t)
	f.engine.entries = []session.DisplayEntry{{Role: "user", Text: "first"}, {Role: "assistant", Text: "second"}, {Role: "user", Text: "third"}}
	q := session.Question{ID: 1, Kind: session.QuestionAsk, Ask: session.AskClarification, Head: "Which file?", Blocking: session.Blocking{Turn: true}, Asked: f.registry.now()}
	id := f.ask(t, q)
	row := readAsk(t, f.request(t, "GET", "/app/v1/asks/"+id, ""))
	if len(row.Before) != 2 || row.Before[0].Text != "second" || row.Before[1].Text != "third" || row.Clock == nil || row.Clock.PausesAt.Sub(*row.Clock.StartsAt) != QuestionClock {
		t.Fatalf("ask: %+v", row)
	}
	if askID("another-agent", q) == id {
		t.Fatal("question identity collides across agents")
	}
	f.engine.entries = append(f.engine.entries, session.DisplayEntry{Role: "assistant", Text: "later context"})
	again := readAsk(t, f.request(t, "GET", "/app/v1/asks/"+id, ""))
	if again.Before[0].Text != "second" {
		t.Fatal("context moved after the question")
	}
	turnStatus(t, f.request(t, "POST", "/app/v1/asks/"+id+"/answer", `{"words":"src/api.go"}`), 200)
	row = readAsk(t, f.request(t, "GET", "/app/v1/asks/"+id, ""))
	if row.State != "answered" || row.Receipt == nil || row.Receipt.By != "you" || !strings.Contains(row.Receipt.Text, "src/api.go") {
		t.Fatalf("receipt %+v", row)
	}
	w := f.request(t, "POST", "/app/v1/asks/"+id+"/answer", `{"words":"late answer"}`)
	turnStatus(t, w, 409)
	var conflict ConflictResponse
	json.Unmarshal(w.Body.Bytes(), &conflict)
	if conflict.By != "you" || conflict.At == nil || len(f.door.answers) != 1 {
		t.Fatalf("duplicate %+v", conflict)
	}
	snapshot := f.snapshot(t)
	if len(snapshot.Snapshot.OpenAsks) != 0 {
		t.Fatal("answered ask remained open")
	}
	found := false
	for _, entry := range snapshot.Snapshot.Entries {
		found = found || entry.Kind == "receipt" && entry.Link != nil && entry.Link.ID == id
	}
	if !found {
		t.Fatal("answer receipt absent from transcript")
	}
	turnStatus(t, f.request(t, "GET", "/app/v1/asks/unknown", ""), 404)
}
func TestAskLaterYouDecideAndValidation(t *testing.T) {
	f := newAskFixture(t)
	q := session.Question{ID: 2, Kind: session.QuestionAsk, Ask: session.AskChoice, Head: "Pick one", Options: []session.AnswerOption{{Key: "1", Label: "A"}, {Key: "2", Label: "B"}}, Pick: &session.Pick{Key: "2"}, Blocking: session.Blocking{Turn: true}}
	id := f.ask(t, q)
	path := "/app/v1/asks/" + id + "/answer"
	for _, body := range []string{`{}`, `null`, `{"unknown":1}`, `{"choice":"9"}`, `{"verb":"later","words":"x"}`, `{"verb":"other"}`} {
		turnStatus(t, f.request(t, "POST", path, body), 422)
	}
	turnStatus(t, f.request(t, "POST", path, `{"expect":{"state":"answered"},"choice":"1"}`), 409)
	turnStatus(t, f.request(t, "POST", path, `{"verb":"later"}`), 200)
	if len(f.door.holds) != 1 || len(f.door.answers) != 0 {
		t.Fatal("Later answered the question")
	}
	turnStatus(t, f.request(t, "POST", path, `{"verb":"you_decide"}`), 200)
	if f.door.answers[0].Key != "2" || f.door.answers[0].DecidedBy != session.DecidedByAsker {
		t.Fatal("suggested answer not returned")
	}
	q.ID = 3
	q.Stakes = session.StakesIrreversible
	id = f.ask(t, q)
	turnStatus(t, f.request(t, "POST", "/app/v1/asks/"+id+"/answer", `{"verb":"you_decide"}`), 422)
}
func TestPermissionSaveRemoveRestoreWhyAndIrreversible(t *testing.T) {
	f := newAskFixture(t)
	id := f.ask(t, permissionQuestion(4))
	body := `{"verb":"always","rule":{"match":"exact","who":"agent"}}`
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, body), 200)
	var rules RulesResponse
	w := f.request(t, "GET", "/app/v1/rules", "")
	turnStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules.Rules) != 1 || rules.Rules[0].Pattern != "git status" || rules.Rules[0].AgentID != f.id {
		t.Fatalf("rules %+v", rules)
	}
	rid := rules.Rules[0].ID
	w = f.request(t, "GET", "/app/v1/rules/"+rid+"/why", "")
	turnStatus(t, w, 200)
	var why RuleWhyResponse
	json.Unmarshal(w.Body.Bytes(), &why)
	if why.Matched.ID != id || why.Matched.Command != "git status" {
		t.Fatalf("why %+v", why)
	}
	row := readAsk(t, f.request(t, "GET", "/app/v1/asks/"+id, ""))
	if row.Receipt.Link.Kind != "rule" || row.Receipt.Link.ID != rid {
		t.Fatal("saved rule receipt has no Why link")
	}
	turnStatus(t, f.request(t, "DELETE", "/app/v1/rules/"+rid, `{"expect":{"state":"removed"}}`), 409)
	turnStatus(t, f.request(t, "DELETE", "/app/v1/rules/"+rid, `{}`), 200)
	w = f.request(t, "GET", "/app/v1/rules", "")
	json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules.Rules) != 0 {
		t.Fatal("removed rule listed")
	}
	turnStatus(t, f.request(t, "POST", "/app/v1/rules/"+rid+"/restore", `{}`), 200)
	turnStatus(t, f.request(t, "GET", "/app/v1/rules/missing/why", ""), 404)
	q := permissionQuestion(5)
	q.Stakes = session.StakesIrreversible
	q.PermissionCommand = "rm -rf /"
	id = f.ask(t, q)
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, body), 422)
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, `{"verb":"deny"}`), 200)
	if f.door.answers[len(f.door.answers)-1].Key != "3" {
		t.Fatal("deny did not reach engine")
	}
}
func TestPermissionTaskBatchChecksAllRowsBeforeAnswering(t *testing.T) {
	f := newAskFixture(t)
	q := permissionQuestion(6)
	q.Batch = "task/19"
	q.Blocking = session.Blocking{Tasks: []string{"19"}}
	first := f.ask(t, q)
	q.ID++
	q.PermissionCommand = "git diff"
	second := f.ask(t, q)
	row := readAsk(t, f.request(t, "GET", "/app/v1/asks/"+first, ""))
	if row.Permission.TaskID != "19" || len(row.Permission.Batch) != 2 {
		t.Fatalf("batch %+v", row.Permission)
	}
	body, _ := json.Marshal(PermissionRequest{Items: []PermissionDecision{{ID: first, Verb: "allow_once"}, {ID: second, Verb: "invalid"}}})
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+first, string(body)), 422)
	if len(f.door.answers) != 0 {
		t.Fatal("invalid batch partly answered")
	}
	body, _ = json.Marshal(PermissionRequest{Items: []PermissionDecision{{ID: second, Verb: "deny"}}})
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+first, string(body)), 200)
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+first, `{"verb":"allow_once"}`), 200)
	if len(f.door.answers) != 2 || f.door.answers[0].Key != "3" || f.door.answers[1].Key != "1" {
		t.Fatal("one-by-one did not preserve answers")
	}
}
func TestAskRefusedEngineKeepsWaitingWithoutReceipt(t *testing.T) {
	f := newAskFixture(t)
	id := f.ask(t, permissionQuestion(8))
	f.door.refused = true
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, `{"verb":"allow_once"}`), 409)
	row := readAsk(t, f.request(t, "GET", "/app/v1/asks/"+id, ""))
	if row.State != "waiting" || row.Receipt != nil {
		t.Fatal("refusal produced a receipt")
	}
}

func TestPermissionRefusedAnswerPreservesAnExistingSavedRule(t *testing.T) {
	f := newAskFixture(t)
	body := `{"verb":"always","rule":{"match":"exact","who":"agent"}}`
	id := f.ask(t, permissionQuestion(81))
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, body), 200)
	var before RulesResponse
	if err := json.Unmarshal(f.request(t, "GET", "/app/v1/rules", "").Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if len(before.Rules) != 1 {
		t.Fatalf("saved rules %+v", before.Rules)
	}
	id = f.ask(t, permissionQuestion(82))
	f.door.refused = true
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, body), 409)
	var after RulesResponse
	if err := json.Unmarshal(f.request(t, "GET", "/app/v1/rules", "").Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if len(after.Rules) != 1 || after.Rules[0].ID != before.Rules[0].ID {
		t.Fatalf("a refused answer removed the earlier rule: before %+v, after %+v", before.Rules, after.Rules)
	}
	row := readAsk(t, f.request(t, "GET", "/app/v1/asks/"+id, ""))
	if row.State != "waiting" || row.Receipt != nil {
		t.Fatal("a refused answer changed the pending ask")
	}
	q := permissionQuestion(83)
	q.PermissionCommand = "git diff"
	id = f.ask(t, q)
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, body), 409)
	if err := json.Unmarshal(f.request(t, "GET", "/app/v1/rules", "").Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if len(after.Rules) != 1 || after.Rules[0].ID != before.Rules[0].ID {
		t.Fatalf("a refused answer leaked its new rule: %+v", after.Rules)
	}
}
func TestAskQuestionClockPausesAtFifteenMinutesWithoutAnswer(t *testing.T) {
	f := newAskFixture(t)
	now := f.registry.now()
	f.registry.now = func() time.Time { return now }
	id := f.ask(t, permissionQuestion(9))
	f.registry.pauseAsk(f.id, id, f.door)
	if f.door.stops != 0 {
		t.Fatal("paused before the deadline")
	}
	now = now.Add(QuestionClock)
	f.registry.pauseAsk(f.id, id, f.door)
	f.registry.pauseAsk(f.id, id, f.door)
	if f.door.stops != 1 || len(f.door.answers) != 0 {
		t.Fatal("clock answered or stopped twice")
	}
	entries := f.snapshot(t).Snapshot.Entries
	found := false
	for _, entry := range entries {
		found = found || strings.Contains(entry.Text, "Paused · no answer")
	}
	if !found {
		t.Fatal("clock receipt absent")
	}
	q := permissionQuestion(10)
	q.Asked = now
	held := f.ask(t, q)
	turnStatus(t, f.request(t, "POST", "/app/v1/asks/"+held+"/answer", `{"verb":"later"}`), 200)
	// Holding either shape preserves the unanswered question.
	q.ID = 11
	q.Kind = session.QuestionAsk
	q.Ask = session.AskClarification
	held = f.ask(t, q)
	turnStatus(t, f.request(t, "POST", "/app/v1/asks/"+held+"/answer", `{"verb":"later"}`), 200)
	now = now.Add(QuestionClock)
	f.registry.pauseAsk(f.id, held, f.door)
	if f.door.stops != 1 {
		t.Fatal("held clock still paused work")
	}
}
func TestAskSubscriptionReceiptsOutsideAnswersWithdrawalsAndAutomaticRules(t *testing.T) {
	f := newAskFixture(t)
	a := f.registry
	a.mu.Lock()
	conn := a.live[f.id].conn
	a.watchAsks(f.id, &conn)
	a.live[f.id].conn = conn
	a.mu.Unlock()
	q := permissionQuestion(12)
	f.door.put(q)
	f.door.events <- session.Event{Kind: session.EventQuestion, Question: &q}
	turnWait(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		known := false
		_ = a.asks(false, func(d *askDisk) error { _, known = d.Asks[askID(f.id, q)]; return nil })
		return known
	})
	answer := session.Answer{Kind: q.Kind, ID: q.ID, Key: "1", From: "inbox", At: a.now(), DecidedBy: session.DecidedByPerson}
	if err := f.door.ResolveQuestion(answer); err != nil {
		t.Fatal(err)
	}
	turnWait(t, func() bool {
		return readAsk(t, f.request(t, "GET", "/app/v1/asks/"+askID(f.id, q), "")).State == "answered"
	})
	row := readAsk(t, f.request(t, "GET", "/app/v1/asks/"+askID(f.id, q), ""))
	if row.Receipt.By != "inbox" {
		t.Fatal("answer source lost")
	}
	q.ID = 13
	f.door.events <- session.Event{Kind: session.EventQuestionWithdrawn, Question: &q}
	turnWait(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		known := false
		_ = a.asks(false, func(d *askDisk) error {
			row, ok := d.Asks[askID(f.id, q)]
			known = ok && row.Ask.State == "withdrawn"
			return nil
		})
		return known
	})
	q.ID = 0
	q.Ref = "auto/command"
	answer.From = "rule:rule_saved"
	answer.DecidedBy = session.DecidedByDial
	f.door.events <- session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer}
	turnWait(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		known := false
		_ = a.asks(false, func(d *askDisk) error { _, known = d.Asks[askID(f.id, q)]; return nil })
		return known
	})
	row = readAsk(t, f.request(t, "GET", "/app/v1/asks/"+askID(f.id, q), ""))
	if row.Receipt.Link.Kind != "rule" || !strings.Contains(row.Receipt.Text, "Allowed automatically") {
		t.Fatal("automatic receipt missing Why")
	}
}

func TestAskIdentitySurvivesCounterReuseAndPermissionReplay(t *testing.T) {
	f := newAskFixture(t)
	q := permissionQuestion(1)
	id := f.ask(t, q)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/app/v1/permissions/"+id, strings.NewReader(`{"verb":"allow_once"}`))
	r.Host = "127.0.0.1:4000"
	r.Header = bearer()
	r.Header.Set("Idempotency-Key", "answer-once")
	f.server.ServeHTTP(w, r)
	turnStatus(t, w, 200)
	replay := httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/app/v1/permissions/"+id, strings.NewReader(`{"verb":"allow_once"}`))
	r.Host = "127.0.0.1:4000"
	r.Header = bearer()
	r.Header.Set("Idempotency-Key", "answer-once")
	f.server.ServeHTTP(replay, r)
	turnStatus(t, replay, 200)
	if w.Body.String() != replay.Body.String() || len(f.door.answers) != 1 {
		t.Fatal("permission replay resolved twice")
	}
	q.Asked = q.Asked.Add(time.Minute)
	second := f.ask(t, q)
	if second == id {
		t.Fatal("restart reused an old question identity")
	}
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+second, `{"verb":"deny"}`), 200)
}

func TestAskAndInboxUseOneQuestionSubscriptionAndUndoHold(t *testing.T) {
	f := newAskFixture(t)
	f.server.inbox.cancel()
	<-f.server.inbox.done
	t.Cleanup(f.server.Close)
	a := f.registry
	a.mu.Lock()
	conn := a.live[f.id].conn
	a.watchAsks(f.id, &conn)
	a.live[f.id].conn = conn
	a.mu.Unlock()
	f.server.SetInboxSource("engine", &engineInboxSource{s: f.server, a: a})
	clock := &inboxTestClock{at: a.now()}
	f.server.inbox.now = func() time.Time { return clock.at }
	f.server.inbox.after = func(_ time.Duration, fn func()) *time.Timer {
		clock.callbacks = append(clock.callbacks, fn)
		return nil
	}
	q := permissionQuestion(24)
	id := f.ask(t, q)
	items := inboxItems(t, f.server)
	if len(items) != 1 {
		t.Fatalf("Inbox items %+v", items)
	}
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, `{"verb":"always","rule":{"match":"invalid","who":"agent"}}`), 422)
	if len(clock.callbacks) != 0 {
		t.Fatal("invalid rule armed Undo")
	}
	w := f.request(t, "POST", "/app/v1/permissions/"+id, `{"verb":"allow_once","expect":{"state":"waiting"}}`)
	turnStatus(t, w, 200)
	if len(f.door.answers) != 0 {
		t.Fatal("source permission skipped the five-second hold")
	}
	turnStatus(t, inboxReq(t, f.server, "POST", "/app/v1/inbox/"+items[0].ID+"/answer", `{"verb":"deny"}`), 409)
	if len(clock.callbacks) != 1 {
		t.Fatal("chat and Inbox armed separate holds")
	}
	clock.at = clock.at.Add(5 * time.Second)
	clock.callbacks[0]()
	if len(f.door.answers) != 1 || f.door.answers[0].Key != "1" {
		t.Fatal("held permission did not resolve through its Ask")
	}
	row := readAsk(t, f.request(t, "GET", "/app/v1/asks/"+id, ""))
	if row.Receipt == nil || row.State != "answered" {
		t.Fatal("held answer has no durable receipt")
	}
	if len(inboxItems(t, f.server)) != 0 {
		t.Fatal("answered item stayed in Inbox")
	}
}

func TestAskCodecUsesAppKindsForEveryEngineDecision(t *testing.T) {
	for _, probe := range []struct {
		lane  session.QuestionKind
		shape session.AskKind
		want  string
	}{{session.QuestionTask, "", "proposal"}, {session.QuestionLanding, "", "your_call"}, {session.QuestionFuel, "", "budget"}, {session.QuestionStanding, "", "order_ask"}, {session.QuestionAsk, session.AskConfirmation, "permission"}} {
		if row := turnAsk("agent", session.Question{ID: 1, Kind: probe.lane, Ask: probe.shape}); row.Kind != probe.want {
			t.Fatalf("%s/%s became %s", probe.lane, probe.shape, row.Kind)
		}
	}
}

func (e *askEngine) ReplyRunning() bool {
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	return e.replyRunning
}
func TestAskTaskPermissionLeavesTheHTTPConversationFree(t *testing.T) {
	f := newAskFixture(t)
	q := permissionQuestion(31)
	q.Blocking = session.Blocking{Tasks: []string{"task-31"}}
	f.ask(t, q)
	f.door.fakeAgentEngine.mu.Lock()
	f.door.fakeAgentEngine.needs = true
	f.door.fakeAgentEngine.mu.Unlock()
	f.door.replyRunning = true
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"Continue","mode":"send"}`), 409)
	f.door.replyRunning = false
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"Continue","mode":"send"}`), 200)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"Another inline reply","mode":"send"}`), 409)
	f.engine.stream(0) <- session.Event{Kind: session.EventTurnDone}
	close(f.engine.stream(0))
}

// The answer is taken at the click: agent.changed reaches the desk at once with
// a state that is not needs_you, Undo returns it, and the held window's end
// keeps it running.
func TestPermissionAnswerLeavesNeedsYouAtTheClickAndUndoRestoresIt(t *testing.T) {
	f := newAskFixture(t)
	f.server.inbox.cancel()
	<-f.server.inbox.done
	t.Cleanup(f.server.Close)
	a := f.registry
	f.server.SetInboxSource("engine", &engineInboxSource{s: f.server, a: a})
	clock := &inboxTestClock{at: a.now()}
	f.server.inbox.now = func() time.Time { return clock.at }
	f.server.inbox.after = func(_ time.Duration, fn func()) *time.Timer {
		clock.callbacks = append(clock.callbacks, fn)
		return nil
	}
	setNeeds := func(v bool) { f.engine.mu.Lock(); f.engine.needs = v; f.engine.mu.Unlock() }
	changes := func() int {
		batch, _, _, _, _ := f.server.Events().read(0)
		n := 0
		for _, event := range batch {
			if event.kind == EventAgentChanged && event.agent == f.id {
				n++
			}
		}
		return n
	}
	state := func() string {
		return readAgent(t, f.request(t, "GET", "/app/v1/agents/"+f.id, ""), 200).State
	}
	setNeeds(true)
	id := f.ask(t, permissionQuestion(31))
	if err := a.UpdateState(f.id, "needs_you"); err != nil || state() != "needs_you" {
		t.Fatalf("setup: %v %s", err, state())
	}
	before := changes()
	// Published inside the answer request itself, so well within 200 ms of the
	// click; a wall clock here would only measure the test host's load.
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, `{"verb":"allow_once"}`), 200)
	if changes() <= before {
		t.Fatal("answer returned before agent.changed was published")
	}
	if got := state(); got == "needs_you" {
		t.Fatalf("answered agent still reads %s", got)
	}
	items := inboxItems(t, f.server)
	before = changes()
	turnStatus(t, inboxReq(t, f.server, "POST", "/app/v1/inbox/"+items[0].ID+"/undo", `{}`), 200)
	if changes() <= before || state() != "needs_you" {
		t.Fatalf("undo did not restore needs_you with agent.changed: %s", state())
	}
	turnStatus(t, f.request(t, "POST", "/app/v1/permissions/"+id, `{"verb":"allow_once"}`), 200)
	setNeeds(false)
	clock.at = clock.at.Add(5 * time.Second)
	clock.callbacks[len(clock.callbacks)-1]()
	if len(f.door.answers) != 1 || state() != "running" {
		t.Fatalf("window end: answers %d state %s", len(f.door.answers), state())
	}
}
