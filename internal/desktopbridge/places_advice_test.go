package desktopbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// askingAgent is a conversation whose engine answers the places question. It
// counts every question, so "no model call" is asserted as a fact.
type askingAgent struct {
	fakeAgent
	title   string
	entries []session.DisplayEntry
	mu      sync.Mutex
	asked   []placegraph.ModelRequest
	answer  func(context.Context, placegraph.ModelRequest) (session.PlacesAnswer, error)
}

func (a *askingAgent) Title() string { return a.title }
func (a *askingAgent) Transcript() []session.DisplayEntry {
	return append([]session.DisplayEntry(nil), a.entries...)
}
func (a *askingAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	return a.Transcript(), nil, func() {}
}
func (a *askingAgent) AskPlaces(ctx context.Context, req placegraph.ModelRequest) (session.PlacesAnswer, error) {
	a.mu.Lock()
	a.asked = append(a.asked, req)
	answer := a.answer
	a.mu.Unlock()
	if answer == nil {
		return session.PlacesAnswer{}, errors.New("no answer scripted")
	}
	return answer(ctx, req)
}
func (a *askingAgent) calls() int { a.mu.Lock(); defer a.mu.Unlock(); return len(a.asked) }

const adviceModel = "deepseek/deepseek-v4.1-flash"

type adviceRig struct {
	t      *testing.T
	b      *Bridge
	agent  *askingAgent
	store  *placegraph.Store
	ledger string
	advice *PlaceAdvice
	policy placegraph.RecommendPolicy
	rows   []session.SessionRow
	mu     sync.Mutex
	asks   bool
	ended  atomic.Int32
}

func newAdviceRig(t *testing.T, asks bool) *adviceRig {
	t.Helper()
	dir := t.TempDir()
	rig := &adviceRig{t: t, policy: placegraph.DefaultRecommendPolicy(), asks: asks, ledger: filepath.Join(dir, "places-ai.json")}
	rig.agent = &askingAgent{
		fakeAgent: fakeAgent{events: make(chan session.Event, 8), model: Model},
		title:     "Fix the release pipeline flake",
		entries: []session.DisplayEntry{
			{Role: "user", Text: "the release pipeline fails on the signing step again"},
			{Role: "assistant", Text: "The signing step times out; here is the fix.", Answer: true},
		},
	}
	store, err := placegraph.Open(placegraph.Options{Path: filepath.Join(dir, "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	rig.store = store
	sessionFile := filepath.Join(dir, "chat-1", "session.jsonl")
	rig.b = New(testToken, func(string) (Connection, error) {
		return Connection{Agent: rig.agent, Welcome: remote.Welcome{SessionFile: sessionFile, Workspace: "/desk", Model: Model, Persistent: true, PlaceAsk: rig.asks, Launch: &remote.LaunchShape{OneModel: true}}, Close: func() {}}, nil
	})
	places := NewPlaces(store)
	places.World = func() session.World {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return session.World{Projects: []session.Project{{Name: "app", Sessions: append([]session.SessionRow(nil), rig.rows...)}}}
	}
	rig.b.UsePlaces(places)
	ledger, err := placegraph.OpenLedger(rig.ledger)
	if err != nil {
		t.Fatal(err)
	}
	rig.advice = &PlaceAdvice{Ledger: ledger, SettleWait: -1, IdleCheck: time.Hour, SharedWorkspace: "/desk",
		Policy: func() placegraph.RecommendPolicy { rig.mu.Lock(); defer rig.mu.Unlock(); return rig.policy }}
	if err := rig.b.UsePlaceAdvice(rig.advice); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rig.b.Close)
	return rig
}

func (r *adviceRig) place(name string) string {
	r.t.Helper()
	p, _, err := r.store.CreatePlace(placegraph.NewPlace{Name: name})
	if err != nil {
		r.t.Fatal(err)
	}
	return p.ID
}

func (r *adviceRig) open() string {
	r.t.Helper()
	w := request(r.b, "POST", "/api/engine/sessions", "{}")
	if w.Code != 200 {
		r.t.Fatal(w.Body.String())
	}
	var snap Snapshot
	_ = json.Unmarshal(w.Body.Bytes(), &snap)
	// Every stream end is counted, settled or not, so a test can wait for the
	// hook to have run rather than assert "nothing happened" too early.
	r.b.mu.Lock()
	s := r.b.sessions[snap.ID]
	r.b.mu.Unlock()
	hook := s.afterTurn
	s.afterTurn = func(s *conversation, settled bool) {
		hook(s, settled)
		r.ended.Add(1)
	}
	return snap.ID
}

// settle runs one turn to the engine's "done" and lets its stream end.
func (r *adviceRig) settle(id string, done bool) {
	r.t.Helper()
	events := make(chan session.Event, 4)
	r.agent.fakeAgent.events = events
	if w := request(r.b, "POST", "/api/engine/sessions/"+id+"/turn", `{"text":"go"}`); w.Code != 200 {
		r.t.Fatal(w.Body.String())
	}
	ended := r.ended.Load()
	events <- session.Event{Kind: session.EventTextDelta, Text: "ok"}
	if done {
		events <- session.Event{Kind: session.EventTurnDone}
	}
	close(events)
	eventually(r.t, "the stream's end", func() bool { return r.ended.Load() > ended })
}

func (r *adviceRig) view() (ProposalsView, int) {
	r.t.Helper()
	w := request(r.b, "GET", "/api/engine/places/proposals", "")
	var v ProposalsView
	if w.Code == 200 || w.Code == 202 {
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			r.t.Fatal(err)
		}
	}
	return v, w.Code
}

// idle waits for the one worker to have nothing queued or running.
func (r *adviceRig) idle() {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r.advice.mu.Lock()
		quiet := len(r.advice.order) == 0 && !r.advice.organize && r.advice.busy == "" && len(r.advice.timers) == 0
		r.advice.mu.Unlock()
		if quiet {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatal("the place advice never went quiet")
}

// eventually waits for the condition the worker is expected to bring about.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

func TestASettledTurnAsksTheChatsEngineOnceAndOffersThePlace(t *testing.T) {
	rig := newAdviceRig(t, true)
	release := rig.place("Release pipeline")
	rig.agent.answer = func(_ context.Context, req placegraph.ModelRequest) (session.PlacesAnswer, error) {
		return session.PlacesAnswer{Text: `{"place":"p1","confidence":90}`, Model: adviceModel}, nil
	}
	before, _ := rig.store.Revision()
	id := rig.open()
	rig.settle(id, true)
	eventually(t, "an offer", func() bool { v, _ := rig.view(); return len(v.Proposals) == 1 })
	// The offer lands in the ledger before the job writes down what its ask
	// came to, so the record is only final once the worker has gone quiet.
	rig.idle()
	v, _ := rig.view()
	p := v.Proposals[0]
	if p.Kind != placegraph.ProposalFile || p.PlaceID != release || p.ChatIDs[0] != "chat-1" || p.Basis != placegraph.BasisModel || p.OfferVersion == "" {
		t.Fatalf("offered %+v", p)
	}
	if rig.agent.calls() != 1 || rig.agent.asked[0].Role != roles.RolePlaceFile {
		t.Fatalf("asked %d times: %+v", rig.agent.calls(), rig.agent.asked)
	}
	if !strings.Contains(rig.agent.asked[0].User, "release pipeline fails on the signing step") {
		t.Fatalf("the question did not carry the chat's own opening message:\n%s", rig.agent.asked[0].User)
	}
	if strings.Contains(rig.agent.asked[0].User, "/desk") {
		t.Fatal("the folder every desktop chat runs in was sent as evidence")
	}
	if v.LastAsk == nil || v.LastAsk.Model != adviceModel || v.LastAsk.Role != "placefile" || v.LastAsk.Error != "" || !v.LastAsk.Offered {
		t.Fatalf("the answering model was not recorded: %+v", v.LastAsk)
	}
	if after, _ := rig.store.Revision(); after != before {
		t.Fatal("an offer changed the graph before anyone accepted it")
	}
	// A second settled turn of the same chat is not a second question.
	rig.settle(id, true)
	rig.idle()
	if rig.agent.calls() != 1 {
		t.Fatalf("the same chat was asked about %d times", rig.agent.calls())
	}
}

func TestReadingOffersAndReconnectingTheStreamNeverAskAModel(t *testing.T) {
	rig := newAdviceRig(t, true)
	rig.place("Release pipeline")
	rig.agent.answer = func(context.Context, placegraph.ModelRequest) (session.PlacesAnswer, error) {
		return session.PlacesAnswer{Text: `{"place":"p1","confidence":90}`, Model: adviceModel}, nil
	}
	id := rig.open()
	for i := 0; i < 5; i++ {
		if _, code := rig.view(); code != 200 {
			t.Fatalf("read answered %d", code)
		}
		ctx, cancel := context.WithCancel(context.Background())
		r := httptest.NewRequest("GET", "/api/engine/sessions/"+id+"/events", nil).WithContext(ctx)
		r.Host = "127.0.0.1:1420"
		r.Header.Set("Authorization", "Bearer "+testToken)
		done := make(chan struct{})
		go func() { rig.b.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
		cancel()
		<-done
	}
	rig.idle()
	if rig.agent.calls() != 0 {
		t.Fatalf("reading asked the model %d times", rig.agent.calls())
	}
}

func TestAStreamThatEndsWithoutDoneIsNotWeighed(t *testing.T) {
	rig := newAdviceRig(t, true)
	rig.place("Release pipeline")
	id := rig.open()
	rig.settle(id, false)
	rig.idle()
	if rig.agent.calls() != 0 {
		t.Fatal("a stopped or lost turn was weighed")
	}
}

func TestAnEngineThatCannotAskOffersRulesOnlyAndSaysSo(t *testing.T) {
	rig := newAdviceRig(t, false)
	rig.place("Release pipeline")
	id := rig.open()
	rig.settle(id, true)
	rig.idle()
	v, _ := rig.view()
	if rig.agent.calls() != 0 || len(v.Proposals) != 0 {
		t.Fatalf("calls %d, offers %+v", rig.agent.calls(), v.Proposals)
	}
	if v.Engine.Asks || v.Engine.Reason == "" {
		t.Fatalf("engine %+v", v.Engine)
	}
}

func TestARefusalOrMalformedAnswerOffersNothingAndChangesNothing(t *testing.T) {
	for name, answer := range map[string]string{
		"refusal":   "I can't help with sorting your chats.",
		"unknown":   `{"place":"p9","confidence":99}`,
		"malformed": `{"place":`,
	} {
		t.Run(name, func(t *testing.T) {
			rig := newAdviceRig(t, true)
			rig.place("Release pipeline")
			rig.agent.answer = func(context.Context, placegraph.ModelRequest) (session.PlacesAnswer, error) {
				return session.PlacesAnswer{Text: answer, Model: adviceModel}, nil
			}
			before, _ := rig.store.Revision()
			rig.settle(rig.open(), true)
			eventually(t, "the question", func() bool { return rig.agent.calls() == 1 })
			rig.idle()
			v, _ := rig.view()
			if after, _ := rig.store.Revision(); len(v.Proposals) != 0 || after != before {
				t.Fatalf("offers %+v, revision %d→%d", v.Proposals, before, after)
			}
			if v.LastAsk == nil || v.LastAsk.Offered || v.LastAsk.Error == "" {
				t.Fatalf("the unused answer was not said: %+v", v.LastAsk)
			}
		})
	}
}

func TestAcceptIsCheckedAgainstTheOfferSeenAndHappensOnce(t *testing.T) {
	rig := newAdviceRig(t, true)
	release := rig.place("Release pipeline")
	rig.agent.answer = func(context.Context, placegraph.ModelRequest) (session.PlacesAnswer, error) {
		return session.PlacesAnswer{Text: `{"place":"p1","confidence":90}`, Model: adviceModel}, nil
	}
	rig.settle(rig.open(), true)
	eventually(t, "an offer", func() bool { v, _ := rig.view(); return len(v.Proposals) == 1 })
	v, _ := rig.view()
	p := v.Proposals[0]
	path := "/api/engine/places/proposals/" + p.ID + "/accept"

	w := request(rig.b, "POST", path, `{"offerVersion":"0000000000000000"}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"stale_offer"`) {
		t.Fatalf("a changed offer was obeyed: %d %s", w.Code, w.Body.String())
	}
	w = request(rig.b, "POST", path, `{"offerVersion":"`+p.OfferVersion+`"}`)
	if w.Code != 200 {
		t.Fatalf("accept: %d %s", w.Code, w.Body.String())
	}
	snap, _ := rig.store.Snapshot()
	if got := snap.PlacesOf("chat-1"); len(got) != 1 || got[0].PlaceID != release {
		t.Fatalf("filed into %+v", got)
	}
	w = request(rig.b, "POST", path, `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"gone"`) {
		t.Fatalf("a second accept: %d %s", w.Code, w.Body.String())
	}
	if w := request(rig.b, "POST", "/api/engine/places/proposals/"+p.ID+"/decline", `{}`); w.Code != 409 {
		t.Fatalf("declining a decided offer: %d", w.Code)
	}
	if w := request(rig.b, "POST", "/api/engine/places/proposals/../../x/accept", `{}`); w.Code == 200 {
		t.Fatal("a malformed id was accepted")
	}
}

func TestDeclineClosesTheOfferAndTheChatIsNotAskedAgain(t *testing.T) {
	rig := newAdviceRig(t, true)
	rig.place("Release pipeline")
	rig.agent.answer = func(context.Context, placegraph.ModelRequest) (session.PlacesAnswer, error) {
		return session.PlacesAnswer{Text: `{"place":"p1","confidence":90}`, Model: adviceModel}, nil
	}
	id := rig.open()
	rig.settle(id, true)
	eventually(t, "an offer", func() bool { v, _ := rig.view(); return len(v.Proposals) == 1 })
	v, _ := rig.view()
	if w := request(rig.b, "POST", "/api/engine/places/proposals/"+v.Proposals[0].ID+"/decline", `{}`); w.Code != 200 {
		t.Fatalf("decline: %d %s", w.Code, w.Body.String())
	}
	rig.settle(id, true)
	rig.idle()
	if v, _ := rig.view(); len(v.Proposals) != 0 || rig.agent.calls() != 1 {
		t.Fatalf("offers %+v after decline, %d calls", v.Proposals, rig.agent.calls())
	}
}

func TestADamagedLedgerIsSetAsideAndSaid(t *testing.T) {
	rig := newAdviceRig(t, true)
	if err := os.WriteFile(rig.ledger, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, code := rig.view()
	if code != 200 || v.Recovered == "" || len(v.Proposals) != 0 {
		t.Fatalf("%d %+v", code, v)
	}
	matches, _ := filepath.Glob(rig.ledger + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("the damaged file was not kept: %v", matches)
	}
}

func TestClosingTheBridgeCancelsAQuestionInFlight(t *testing.T) {
	rig := newAdviceRig(t, true)
	rig.place("Release pipeline")
	started := make(chan struct{})
	var cancelled atomic.Bool
	rig.agent.answer = func(ctx context.Context, _ placegraph.ModelRequest) (session.PlacesAnswer, error) {
		close(started)
		<-ctx.Done()
		cancelled.Store(true)
		return session.PlacesAnswer{}, ctx.Err()
	}
	rig.settle(rig.open(), true)
	<-started
	finished := make(chan struct{})
	go func() { rig.b.Close(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(6 * time.Second):
		t.Fatal("closing the bridge waited on a model")
	}
	if !cancelled.Load() {
		t.Fatal("the question in flight was not cancelled")
	}
}

func TestOrganizeIsAskedOnceAndRunsInTheBackground(t *testing.T) {
	rig := newAdviceRig(t, true)
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		r := row(id, "garden irrigation plan "+id, at)
		r.Workspace, r.Tokens = "/home/u/garden", 10
		rig.rows = append(rig.rows, r)
	}
	w := request(rig.b, "POST", "/api/engine/places/proposals/organize", `{}`)
	if w.Code != 202 {
		t.Fatalf("organize: %d %s", w.Code, w.Body.String())
	}
	rig.idle()
	w = request(rig.b, "POST", "/api/engine/places/proposals/organize", `{}`)
	if w.Code != 200 {
		t.Fatalf("a second organize inside the gap was queued: %d", w.Code)
	}
	v, _ := rig.view()
	if len(v.Proposals) != 1 || v.Proposals[0].Kind != placegraph.ProposalCreate || len(v.Proposals[0].ChatIDs) != 5 {
		t.Fatalf("offers %+v", v.Proposals)
	}
	if rig.agent.calls() != 0 {
		t.Fatal("a group sharing a folder asked the model")
	}
}

func TestThePolicyRoutesReadAndWriteTheProfile(t *testing.T) {
	rig := newAdviceRig(t, true)
	rig.b.UseModels(&Models{ProfileDir: t.TempDir()})
	w := request(rig.b, "GET", "/api/engine/places/policy", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"settings"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("PUT", "/api/engine/places/policy/minClusterChats", strings.NewReader(`{"value":7}`))
	r.Host = "127.0.0.1:1420"
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rig.b.ServeHTTP(rec, r)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"value":7`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	r = httptest.NewRequest("PUT", "/api/engine/places/policy/minClusterChats", strings.NewReader(`{"value":"lots"}`))
	r.Host = "127.0.0.1:1420"
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	rig.b.ServeHTTP(rec, r)
	if rec.Code != 400 {
		t.Fatalf("a bad value: %d %s", rec.Code, rec.Body.String())
	}
}
