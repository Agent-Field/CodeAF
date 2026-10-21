package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// lendWaitFor polls a condition for a bounded time. The stage is parked on a
// card, so what is waited for is the app's own reading catching up with it,
// never a model or a clock.
func lendWaitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// lendModel is the stage model's recording plus whether it has proposed yet.
type lendModel struct {
	stageModel
	proposed bool
	// again, when set, is a second proposal raised by a RESUMED stage: the one
	// whose carry-on brief says "Plan again."
	again     string
	proposedB bool
}

// newLendModel is a scripted OpenAI-shaped endpoint: the stage's first request
// (the one with `propose_task` on its belt and the stage's brief in it) is
// answered with one irreversible proposal, and every other request, the
// proposal's own worker included, with "done". It reuses the stage model's
// recording so the test can read which model every request named.
func newLendModel(t *testing.T, proposal string) *lendModel {
	t.Helper()
	m := &lendModel{}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			recordedRequest
			Model string `json:"model"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		proposes, answered := false, 0
		for _, tool := range req.Tools {
			proposes = proposes || tool.Function.Name == "propose_task"
		}
		for _, msg := range req.Messages {
			if msg.Role == "tool" {
				answered++
			}
		}
		m.mu.Lock()
		// ONE PROPOSAL IN THE WHOLE RUN: the stage's later turns, woken by the
		// task's landing, must not raise a second card.
		propose := proposes && answered == 0 && !m.proposed && strings.Contains(string(raw), "Plan it.")
		m.proposed = m.proposed || propose
		card := proposal
		// A RESUMED STAGE raises ONE more card, titled differently, so the test can
		// tell the new agent's card from the old one's.
		if !propose && m.again != "" && proposes && !m.proposedB && strings.Contains(string(raw), "Plan again.") {
			propose, m.proposedB, card = true, true, m.again
		}
		m.models = append(m.models, req.Model)
		m.mu.Unlock()
		args, _ := json.Marshal(card)
		switch {
		case propose && req.Stream:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"propose_task","arguments":` + string(args) + `}}]},"finish_reason":null}]}` + "\n\n" +
				`data: {"id":"s","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
		case propose:
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"propose_task","arguments":` + string(args) + `}}]},"finish_reason":"tool_calls"}]}`))
		case req.Stream:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"s","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

// TestTheAppLendsItsOwnLiveFactoryStageInsteadOfRefusingIt goes through the
// production path with NO injected parent: the app factory host's own
// parentSource (the saved one-model policy), the real stage maker, and a REAL
// app backend whose opener is [appOpenInProcess]. The model is a scripted
// httptest endpoint; nothing here reaches a real provider.
//
// The stage proposes an irreversible task, which has no clock, so its turn is
// parked on a card that only a person can answer. Before the lend the crew hold
// refused it with "open in another codeaf window" (the journal's lock is this
// very process's), so the card reached nobody.
func TestTheAppLendsItsOwnLiveFactoryStageInsteadOfRefusingIt(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	profile := factoryPolicyProfile(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  savedOneModel,
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "")
	propose, _ := json.Marshal(map[string]any{
		"title": "read the ledger", "summary": "read it", "brief": "read the ledger and say what is in it",
		"deliverable": "a summary", "acceptance": "the summary names every line", "irreversible": true,
	})
	model := newLendModel(t, string(propose))
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })

	// THE PRODUCTION CONSTRUCTION (app_factory.go): no h.parent.
	host := &appFactoryHost{workspace: workspace, profile: profile}
	source, _, ok := host.parentSource()
	if !ok {
		t.Fatal("the worker could not build its own parent")
	}
	maker := factoryStageMakerFrom(nil, workspace, profile, source.Config).withRefusal(source.Refusal)

	st, err := store.Open(filepath.Join(root, "factory"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{
		Title: "tidy the ledger", Repo: "work", Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, Ask: "plan it", Until: "done", On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	item, _ := st.Get(id)
	team, err := factoryItemTeam(profile, item)
	if err != nil || team == "" {
		t.Fatalf("the item's team: %q, %v", team, err)
	}
	conv, err := maker.Open(context.Background(), factoryrun.ConversationSpec{Team: team, Name: "#1 · plan", Brief: "Plan it. End by calling stage_result once.", Dir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	closeStage := func() {
		if !closed {
			closed = true
			conv.Close()
			// The follower reads stageSettle, which the cleanup below restores.
			select {
			case <-conv.(*stageConversation).done:
			case <-time.After(10 * time.Second):
			}
		}
	}
	t.Cleanup(closeStage)
	transcript := conv.ID()
	live, ok := session.LiveAgentFor(transcript)
	if !ok {
		t.Fatal("the stage is not a live conversation in this process")
	}
	if !session.InUse(transcript) {
		t.Fatal("the stage does not hold its journal; the premise of the refusal is gone")
	}

	// A REAL BACKEND on the REAL opener, crews read from the same profile.
	rec := newAppRecorder()
	backend := newFakeBackend(rec, appOpenInProcess)
	backend.state = &appState{path: filepath.Join(root, "app", appStateName)}
	backend.profileDir = profile
	t.Cleanup(func() { backend.Close() })
	agent := appAgentID(transcript)
	run := backend.crews()
	lendWaitFor(t, "the crew to hold the live stage", func() bool {
		backend.crewTick(run, false)
		return backend.isHeld(agent)
	})
	rec.mu.Lock()
	for i, data := range rec.data {
		if changed, ok := data.(appwire.MemberChanged); ok && strings.HasPrefix(changed.Member.Line, "can't open here") {
			t.Errorf("event %d: the hold was refused: %q", i, changed.Member.Line)
		}
	}
	rec.mu.Unlock()

	// THE CARD IS REACHABLE: the parked proposal is a task the app lists.
	lendWaitFor(t, "the stage's proposal to reach the app", func() bool {
		tasks, err := backend.ListTasks(agent)
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range tasks {
			if strings.Contains(candidate.Title, "read the ledger") && candidate.Proposed {
				return true
			}
		}
		return false
	})

	// A VIEWER GOING AWAY ENDS NOTHING: closing the app's agent releases the
	// lent view while the stage's turn stays parked on its card.
	if !live.TurnRunning() {
		t.Fatal("the stage's turn is not parked on its proposal")
	}
	if err := backend.CloseAgent(agent); err != nil {
		t.Fatal(err)
	}
	if still, ok := session.LiveAgentFor(transcript); !ok || still != live {
		t.Fatal("closing the app's view ended the stage's conversation")
	}
	if !live.TurnRunning() {
		t.Fatal("closing the app's view stopped the stage's turn")
	}
	if !session.InUse(transcript) {
		t.Fatal("closing the app's view let go of the stage's journal")
	}

	// The crew holds it again, and the person's answer is the only grant.
	lendWaitFor(t, "the stage to be held again", func() bool {
		if _, err := backend.OpenAgent(agent); err != nil {
			return false
		}
		return backend.isHeld(agent)
	})
	// THE QUESTION LANE REPLAYS THE CARD to the new view: the proposal is an open
	// ask the person answers with "start it".
	var card appwire.Ask
	lendWaitFor(t, "the proposal's card to be asked again", func() bool {
		asks, err := backend.ListAsks(agent)
		if err != nil {
			t.Fatal(err)
		}
		for _, ask := range asks {
			if strings.Contains(ask.Title, "read the ledger") {
				card = ask
				return true
			}
		}
		return false
	})
	start := ""
	for _, option := range card.Options {
		if option.Label == "start it" {
			start = option.ID
		}
	}
	if start == "" {
		t.Fatalf("the card offers no way to start the task: %+v", card)
	}
	if err := backend.AnswerAsk(agent, card.ID, start, ""); err != nil {
		t.Fatalf("the person's go-ahead was refused: %v", err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := conv.Wait(waitCtx); err != nil {
		t.Fatalf("the stage did not finish after the person started the task: %v", err)
	}

	// The runner closes it; the next hold finds the journal free and the lend
	// no longer applies.
	closeStage()
	if _, ok := session.LiveAgentFor(transcript); ok {
		t.Fatal("the runner's close left the stage live")
	}
	if session.InUse(transcript) {
		t.Fatal("the runner's close left the journal locked")
	}

	model.mu.Lock()
	defer model.mu.Unlock()
	for _, m := range model.models {
		if m != savedOneModel {
			t.Errorf("a request went to %q; the no-parent policy pins %q", m, savedOneModel)
		}
	}
	if f, err := teams.Load(profile); err != nil || len(f.Teams) == 0 {
		t.Errorf("the item's team is gone: %v", err)
	}
}

// TestAConversationLockedByAnotherOwnerIsStillRefused is the lend's negative
// twin: a journal held by something that is not in this process's live
// registry is another window's, and the app says so.
func TestAConversationLockedByAnotherOwnerIsStillRefused(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	bucket, err := v3ProjectDir(filepath.Join(root, "work"))
	if err != nil {
		t.Fatal(err)
	}
	dir := writeV3Session(t, bucket, "00000000000000d1", "held elsewhere", time.Now())
	transcript := session.Place{Dir: dir}.Transcript()
	file, err := os.Open(transcript)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	defer filelock.Unlock(file)
	if _, ok := session.LiveAgentFor(transcript); ok {
		t.Fatal("nothing in this process holds that journal")
	}
	_, err = appOpenInProcess(appOpenAsk{Workspace: filepath.Join(root, "work"), Transcript: transcript})
	if err == nil || !strings.Contains(err.Error(), appMemberRefusal) {
		t.Fatalf("a journal another owner holds was opened or refused wrongly: %v", err)
	}
}

// lendWatchers counts the goroutines a lent view's closed-watch is running on.
// A released view must leave none behind.
func lendWatchers() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "watchAgentClosed.func") {
			n++
		}
	}
	return n
}

// lendRig is a live factory stage parked on an irreversible proposal, held by a
// real app backend over the real opener. Nothing reaches a real provider.
type lendRig struct {
	model      *lendModel
	maker      stageMaker
	spec       factoryrun.ConversationSpec
	conv       factoryrun.Conversation
	transcript string
	live       *session.Agent
	backend    *appBackend
	rec        *appRecorder
	agent      string
	closeStage func()
}

// newLendRig builds the production construction (no injected parent), opens the
// stage, and has the app's crew hold it. again is the second card a resumed stage
// raises, "" for none.
func newLendRig(t *testing.T, again string) *lendRig {
	t.Helper()
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	profile := factoryPolicyProfile(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  savedOneModel,
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "")
	propose, _ := json.Marshal(map[string]any{
		"title": "read the ledger", "summary": "read it", "brief": "read the ledger and say what is in it",
		"deliverable": "a summary", "acceptance": "the summary names every line", "irreversible": true,
	})
	r := &lendRig{}
	r.model = newLendModel(t, string(propose))
	if again != "" {
		second, _ := json.Marshal(map[string]any{
			"title": again, "summary": "read it again", "brief": "read the ledger once more",
			"deliverable": "a summary", "acceptance": "the summary names every line", "irreversible": true,
		})
		r.model.again = string(second)
	}
	t.Setenv("CODEAF_BASE_URL", r.model.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })

	host := &appFactoryHost{workspace: workspace, profile: profile}
	source, _, ok := host.parentSource()
	if !ok {
		t.Fatal("the worker could not build its own parent")
	}
	r.maker = factoryStageMakerFrom(nil, workspace, profile, source.Config).withRefusal(source.Refusal)
	st, err := store.Open(filepath.Join(root, "factory"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{
		Title: "tidy the ledger", Repo: "work", Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, Ask: "plan it", Until: "done", On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	item, _ := st.Get(id)
	team, err := factoryItemTeam(profile, item)
	if err != nil || team == "" {
		t.Fatalf("the item's team: %q, %v", team, err)
	}
	r.spec = factoryrun.ConversationSpec{Team: team, Name: "#1 · plan", Brief: "Plan it. End by calling stage_result once.", Dir: workspace}
	r.conv, err = r.maker.Open(context.Background(), r.spec)
	if err != nil {
		t.Fatal(err)
	}
	r.closeStage = r.closer(t, &r.conv)
	t.Cleanup(func() { r.closeStage() })
	r.transcript = r.conv.ID()
	if r.live, ok = session.LiveAgentFor(r.transcript); !ok {
		t.Fatal("the stage is not a live conversation in this process")
	}
	r.rec = newAppRecorder()
	r.backend = newFakeBackend(r.rec, appOpenInProcess)
	r.backend.state = &appState{path: filepath.Join(root, "app", appStateName)}
	r.backend.profileDir = profile
	t.Cleanup(func() { r.backend.Close() })
	r.agent = appAgentID(r.transcript)
	r.hold(t)
	return r
}

// closer is the runner's close of whichever stage conversation *conv is now.
func (r *lendRig) closer(t *testing.T, conv *factoryrun.Conversation) func() {
	closed := false
	return func() {
		if closed {
			return
		}
		closed = true
		(*conv).Close()
		select {
		case <-(*conv).(*stageConversation).done:
		case <-time.After(10 * time.Second):
		}
	}
}

// hold has the crew hold the live stage and waits for its first card.
func (r *lendRig) hold(t *testing.T) {
	t.Helper()
	run := r.backend.crews()
	lendWaitFor(t, "the crew to hold the live stage", func() bool {
		r.backend.crewTick(run, false)
		return r.backend.isHeld(r.agent)
	})
}

// askTitled waits for an open ask whose title contains want.
func (r *lendRig) askTitled(t *testing.T, want string) appwire.Ask {
	t.Helper()
	var card appwire.Ask
	lendWaitFor(t, "the card "+want+" to be asked", func() bool {
		asks, err := r.backend.ListAsks(r.agent)
		if err != nil {
			t.Fatal(err)
		}
		for _, ask := range asks {
			if strings.Contains(ask.Title, want) {
				card = ask
				return true
			}
		}
		return false
	})
	return card
}

// TestTheOwnersClosingALentStageEndsTheAppViewAndAResumedStageIsLentAgain is
// the lifetime gap: the runner closes a stage with agent.Close, which tells the
// lent session nothing. The app must let go of the dead view by itself, free its
// hold, and lend the NEW agent a resume of the same transcript opens, so that
// agent's card reaches the person.
func TestTheOwnersClosingALentStageEndsTheAppViewAndAResumedStageIsLentAgain(t *testing.T) {
	r := newLendRig(t, "read the ledger again")
	r.askTitled(t, "read the ledger")
	if lendWatchers() == 0 {
		t.Fatal("the lent view is not watching its owner")
	}

	// THE OWNER CLOSES the stage while the app still holds the view.
	r.closeStage()
	lendWaitFor(t, "the app to let go of the closed stage", func() bool { return !r.backend.isHeld(r.agent) })
	lendWaitFor(t, "the closed-watch and the view's lanes to end", func() bool { return lendWatchers() == 0 })

	// THE REALISTIC GAP: the crew polls every couple of seconds and a person
	// resumes minutes later, so a tick lands between the close and the resume. The
	// stage is no longer registered, so the tick boots the paused transcript as
	// the APP'S OWN conversation, and the resume then closes that agent
	// underneath the app (stageTakeJournal).
	r.hold(t)
	if conn := r.backend.liveOf(mustFind(t, r.backend, r.agent)).conn; conn.Lent || !conn.Assembled {
		t.Fatal("the paused transcript was not held as the app's own conversation; the test proves nothing")
	}
	owned, ok := session.LiveAgentFor(r.transcript)
	if !ok || owned == r.live {
		t.Fatal("the app did not boot its own agent on the paused transcript")
	}

	// THE SAME TRANSCRIPT RESUMED is a new agent, and the crew lends it again.
	resume := r.spec
	resume.Resume = r.transcript
	resume.Brief = "Plan again. End by calling stage_result once."
	conv, err := r.maker.Open(context.Background(), resume)
	if err != nil {
		t.Fatal(err)
	}
	closeResumed := r.closer(t, &conv)
	t.Cleanup(closeResumed)
	again, ok := session.LiveAgentFor(r.transcript)
	if !ok || again == r.live || again == owned {
		t.Fatal("a resume did not make a new live agent on the same transcript")
	}
	lendWaitFor(t, "the app to let go of the agent the resume closed underneath it", func() bool { return !r.backend.isHeld(r.agent) })
	r.hold(t)
	if !r.backend.liveOf(mustFind(t, r.backend, r.agent)).conn.Lent {
		t.Fatal("the resumed stage was not lent")
	}
	r.askTitled(t, "read the ledger again")

	// And letting go of the resumed view ends it too, with the new stage alive.
	if err := r.backend.CloseAgent(r.agent); err != nil {
		t.Fatal(err)
	}
	lendWaitFor(t, "the resumed view's watch to end", func() bool { return lendWatchers() == 0 })
	if still, ok := session.LiveAgentFor(r.transcript); !ok || still != again {
		t.Fatal("letting go of the resumed view ended the resumed stage")
	}
	closeResumed()
}

// TestAPersonsStopThroughTheLentViewStillReachesTheStage pins the other half of
// the lend: dropping a view's ENDINGS must not drop the person's own Stop.
func TestAPersonsStopThroughTheLentViewStillReachesTheStage(t *testing.T) {
	r := newLendRig(t, "")
	r.askTitled(t, "read the ledger")
	if !r.live.TurnRunning() {
		t.Fatal("the stage's turn is not parked on its proposal")
	}
	if err := r.backend.Stop(r.agent); err != nil {
		t.Fatal(err)
	}
	lendWaitFor(t, "the person's Stop to reach the stage's turn", func() bool { return !r.live.TurnRunning() })
	if still, ok := session.LiveAgentFor(r.transcript); !ok || still != r.live {
		t.Fatal("a Stop ended the conversation and not just the turn")
	}
}

// TestALentStageKeepsTheFactorysModelPostureAndBudget pins the policy half of the
// lend. The stage's model, approval posture and spend rail are the factory's:
// holding it binds none of the app's saved agent budget, and the app's per-agent
// model, role, posture and budget changes are refused with a conflict that names
// the factory, BEFORE anything is saved.
//
// LIMIT: the stage is opened by the real stage maker with no stage contract
// (ConversationSpec.Stage is nil), so it never ends through stage_result; the
// runner's executor is not exercised. What is real is the maker, the registry,
// the app's opener and the lent view.
func TestALentStageKeepsTheFactorysModelPostureAndBudget(t *testing.T) {
	r := newLendRig(t, "")
	r.askTitled(t, "read the ledger")
	model, posture, rail := r.live.Model(), r.live.ResolvedApprovalPosture(), r.live.SpendRail()

	// A SAVED AGENT BUDGET IS NOT BOUND onto the stage by a fresh hold.
	if err := r.backend.CloseAgent(r.agent); err != nil {
		t.Fatal(err)
	}
	if err := r.backend.state.write(func(f *appStateFile) error {
		f.Budgets = map[string]float64{r.agent: 0.0123}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lendWaitFor(t, "the stage to be held again", func() bool {
		_, err := r.backend.OpenAgent(r.agent)
		return err == nil && r.backend.isHeld(r.agent)
	})
	if got := r.live.SpendRail(); got != rail {
		t.Fatalf("holding the stage moved its spend rail from %v to %v", rail, got)
	}

	// EVERY PER-AGENT CHANGE IS REFUSED, and none is saved.
	conflict := func(what string, err error) {
		t.Helper()
		var wire *appwire.Error
		if err == nil || !asWire(err, &wire) || wire.Code != appwire.CodeConflict || !strings.Contains(err.Error(), "factory stage") {
			t.Errorf("%s: want a conflict naming the factory stage, got %v", what, err)
		}
	}
	_, err := r.backend.SetModel(r.agent, "other/model")
	conflict("SetModel", err)
	usd := 5.0
	_, err = r.backend.SetAgentBudget(appwire.AgentBudgetSet{ID: r.agent, USD: &usd})
	conflict("SetAgentBudget", err)
	conflict("RaiseCap", r.backend.RaiseCap(appwire.CapRaise{Scope: appwire.CapAgent, ID: r.agent, ToUSD: 9, For: appwire.RaiseGood}))
	conflict("memberRole", r.backend.memberRole(r.spec.Team, r.agent, "reviewer", "Review it.", "other/model"))
	if role := r.backend.roleNamed("reviewer"); role != nil {
		t.Errorf("a refused role was saved: %+v", role)
	}
	// A ROLE THE STAGE CARRIES CANNOT BE EDITED either: RoleUpdate would push the
	// new model onto it. The member is written straight into the layer, since
	// memberRole above is (rightly) refused.
	if err := r.backend.crewLayer().write(func(f *appCrewLayerFile) error {
		f.Roles = map[string]*appwire.Role{"role-x": {ID: "role-x", Name: "carried", Instructions: "Be careful.", Model: model}}
		f.member(r.spec.Team, r.agent).Role = "role-x"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	other, instr := "other/model", "Be reckless."
	_, err = r.backend.RoleUpdate("role-x", nil, &instr, &other)
	conflict("RoleUpdate", err)
	if got := r.backend.roleNamed("role-x"); got == nil || got.Model != model || got.Instructions != "Be careful." {
		t.Errorf("a refused role update was saved: %+v", got)
	}
	if saved, _ := r.backend.agentBudget(r.agent, r.backend.now()); saved != 0.0123 {
		t.Errorf("a refused budget was saved: %v", saved)
	}
	door, ok := r.backend.liveOf(mustFind(t, r.backend, r.agent)).conn.Conv.(appPostureDoor)
	if !ok {
		t.Fatal("the lent view has no posture door to refuse")
	}
	// The posture refusal crosses the lend's pipe as the engine's own text.
	if err := door.SetApprovalPosture("full"); err == nil || !strings.Contains(err.Error(), "factory stage") {
		t.Errorf("SetApprovalPosture: want a refusal naming the factory stage, got %v", err)
	}
	if r.live.Model() != model || r.live.ResolvedApprovalPosture() != posture || r.live.SpendRail() != rail {
		t.Fatal("a refused change reached the stage")
	}
	// THE PERSON'S STOP AND THE VIEW'S DETACH STILL WORK (see the twins above).
	if err := r.backend.Stop(r.agent); err != nil {
		t.Fatal(err)
	}
	lendWaitFor(t, "the person's Stop to reach the stage's turn", func() bool { return !r.live.TurnRunning() })
}

// TestAnAgentThatIsNotAFactoryStageIsNotLent is the registry's negative: a live
// journal in this process that the stage maker has not registered (a shaping
// turn, an in-process crew member) takes the path it took before the lend, and
// is refused as another window's.
func TestAnAgentThatIsNotAFactoryStageIsNotLent(t *testing.T) {
	r := newLendRig(t, "")
	r.askTitled(t, "read the ledger")
	if err := r.backend.CloseAgent(r.agent); err != nil {
		t.Fatal(err)
	}
	forgetStageAgent(r.live)
	t.Cleanup(func() { rememberStageAgent(r.live) })
	if _, ok := session.LiveAgentFor(r.transcript); !ok {
		t.Fatal("the agent is not live; the test proves nothing")
	}
	_, err := appOpenInProcess(appOpenAsk{Workspace: r.spec.Dir, Transcript: r.transcript})
	if err == nil || !strings.Contains(err.Error(), appMemberRefusal) {
		t.Fatalf("a live agent that is not a factory stage was lent or refused wrongly: %v", err)
	}
}
