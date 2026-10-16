package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
)

// railConv is a fake conversation whose engine can say its limit, and can be
// told to refuse one figure.
type railConv struct {
	*fakeConversation
	refuse   float64
	readErr  error
	refusals int
}

func (r *railConv) SetSpendRail(usd float64) error {
	if r.refuse != 0 && usd == r.refuse {
		r.mu.Lock()
		r.refusals++
		r.mu.Unlock()
		return errors.New("the engine went away")
	}
	return r.fakeConversation.SetSpendRail(usd)
}

func (r *railConv) SpendRail() (float64, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rail, nil
}

func (r *railConv) railNow() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rail
}

// budgetBackend is a backend over conv with its own app file and profile.
func budgetBackend(t *testing.T, conv appConversation) *appBackend {
	t.Helper()
	backend, _ := budgetRecordedBackend(t, conv)
	return backend
}
func budgetRecordedBackend(t *testing.T, conv appConversation) (*appBackend, *appRecorder) {
	t.Helper()
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	var mu sync.Mutex
	var opens, releases int
	open := func(ask appOpenAsk) (*appConn, error) {
		mu.Lock()
		opens++
		mu.Unlock()
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() { mu.Lock(); releases++; mu.Unlock() }}, nil
	}
	rec := newAppRecorder()
	backend := newFakeBackend(rec, open)
	t.Cleanup(func() { _ = backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	return backend, rec
}

func budgetFigure(b appwire.AgentBudget) float64 {
	if b.USD == nil {
		return -1
	}
	return *b.USD
}

// A CUSTOM BUDGET IS THE AGENT'S, NOT THE DEFAULT'S. An agent made with $5
// reads $5 of its own on every Agent the engine sends, its engine is bound
// $5, and changing settings.budget_usd moves neither.
func TestAgentBudgetCreatedCustomIsReadAsItsOwn(t *testing.T) {
	conv := &railConv{fakeConversation: &fakeConversation{cost: 1.25}}
	backend, rec := budgetRecordedBackend(t, conv)
	five := 5.0
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), BudgetUSD: &five})
	if err != nil {
		t.Fatal(err)
	}
	// A blank conversation is never listed (appblank.go), so the agent is
	// spoken to before the list is read.
	if _, err := backend.Send(agent.ID, "draft the release notes", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	two := 2.0
	if _, err := backend.SetSettings(appwire.SettingsPatch{BudgetUSD: &appwire.Figure{USD: &two}}); err != nil {
		t.Fatal(err)
	}
	listed, err := backend.ListAgents("")
	if err != nil || len(listed) != 1 || listed[0].Budget == nil {
		t.Fatalf("listed %+v, %v", listed, err)
	}
	if got := *listed[0].Budget; got.Source != appwire.BudgetSet || got.Limit != appwire.LimitLimited || budgetFigure(got) != 5 || *got.BaseUSD != 5 || got.Engine != nil || got.Scope != appwire.BudgetScopeAgent {
		t.Fatalf("the list says %+v", got)
	}
	got, err := backend.AgentBudget(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.From != appwire.BudgetFromRecord || budgetFigure(got) != 5 || got.Engine == nil || got.Engine.Limit != appwire.LimitLimited || *got.Engine.USD != 5 || got.Engine.SpentUSD != 1.25 {
		t.Fatalf("get says %+v (engine %+v)", got, got.Engine)
	}
	if _, err := backend.AgentBudget("nobody"); err == nil {
		t.Fatal("a budget for an agent that is not there")
	}
}

// SET, THEN NONE, THEN A FIGURE AGAIN: each is written, bound to the open
// conversation, read back the same, and a raise for today ends with a set.
func TestAgentBudgetSetBindsAndPersists(t *testing.T) {
	conv := &railConv{fakeConversation: &fakeConversation{}}
	backend := budgetBackend(t, conv)
	five := 5.0
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), BudgetUSD: &five})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.RaiseCap(appwire.CapRaise{Scope: appwire.CapAgent, ID: agent.ID, ToUSD: 7, For: appwire.RaiseToday}); err != nil {
		t.Fatal(err)
	}
	raised, _ := backend.AgentBudget(agent.ID)
	if budgetFigure(raised) != 7 || *raised.BaseUSD != 5 || raised.RaisedUntil == nil || !raised.RaisedUntil.Equal(appMidnight(backend.now())) || conv.railNow() != 7 {
		t.Fatalf("raised for today: %+v, rail %v", raised, conv.railNow())
	}

	nine := 9.0
	got, err := backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID, USD: &nine})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != appwire.BudgetSet || budgetFigure(got) != 9 || got.RaisedUntil != nil || conv.railNow() != 9 || *got.Engine.USD != 9 {
		t.Fatalf("set 9: %+v, rail %v", got, conv.railNow())
	}

	got, err = backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != appwire.BudgetUnlimited || got.Limit != appwire.LimitUnlimited || got.USD != nil || got.BaseUSD != nil || conv.railNow() != 0 || got.Engine.Limit != appwire.LimitUnlimited {
		t.Fatalf("set none: %+v, rail %v", got, conv.railNow())
	}
	if usd, set := backend.agentBudget(agent.ID, backend.now()); !set || usd != 0 {
		t.Fatalf("an unlimited agent binds %v (%v)", usd, set)
	}
	// NOTHING TO RAISE FOR TODAY on an agent with no limit, and the refusal
	// leaves it unlimited.
	err = backend.RaiseCap(appwire.CapRaise{Scope: appwire.CapAgent, ID: agent.ID, ToUSD: 7, For: appwire.RaiseToday})
	if code := appwireCode(err); code != appwire.CodeBadParams {
		t.Fatalf("raise today on none: %v", err)
	}
	if again, _ := backend.AgentBudget(agent.ID); again.Source != appwire.BudgetUnlimited || conv.railNow() != 0 {
		t.Fatalf("the refused raise moved it: %+v", again)
	}
	// A raise for good is a figure the person chose.
	if err := backend.RaiseCap(appwire.CapRaise{Scope: appwire.CapAgent, ID: agent.ID, ToUSD: 6, For: appwire.RaiseGood}); err != nil {
		t.Fatal(err)
	}
	if again, _ := backend.AgentBudget(agent.ID); again.Source != appwire.BudgetSet || budgetFigure(again) != 6 || conv.railNow() != 6 {
		t.Fatalf("raised for good from none: %+v", again)
	}

	// Defence in depth behind the wire's own check.
	for _, bad := range []float64{0, -1} {
		usd := bad
		if _, err := backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID, USD: &usd}); appwireCode(err) != appwire.CodeBadParams {
			t.Fatalf("set %v: %v", bad, err)
		}
	}
	if again, _ := backend.AgentBudget(agent.ID); budgetFigure(again) != 6 {
		t.Fatalf("a refused set moved it: %+v", again)
	}
}

// REOPENING RESTORES THE SAME CAP, NEVER TODAY'S DEFAULT. A conversation let
// go and opened again is bound the record's figure; a new process reading the
// same file reads and binds the same figures for a set and an unlimited agent.
func TestAgentBudgetSurvivesReopenAndRestart(t *testing.T) {
	conv := &railConv{fakeConversation: &fakeConversation{}}
	backend := budgetBackend(t, conv)
	five := 5.0
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), BudgetUSD: &five})
	if err != nil {
		t.Fatal(err)
	}
	eight := 8.0
	if _, err := backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID, USD: &eight}); err != nil {
		t.Fatal(err)
	}
	three := 3.0
	if _, err := backend.SetSettings(appwire.SettingsPatch{BudgetUSD: &appwire.Figure{USD: &three}}); err != nil {
		t.Fatal(err)
	}
	a, _ := backend.find(agent.ID)
	backend.letGo(a)
	if backend.liveOf(a) != nil {
		t.Fatal("the conversation is still held")
	}
	if listed, _ := backend.AgentBudget(agent.ID); listed.Engine != nil || budgetFigure(listed) != 8 {
		t.Fatalf("let go, it reads %+v", listed)
	}
	_ = conv.fakeConversation.SetSpendRail(123)
	if _, err := backend.OpenAgent(agent.ID); err != nil {
		t.Fatal(err)
	}
	if conv.railNow() != 8 {
		t.Fatalf("reopened, its engine is bound %v", conv.railNow())
	}

	// A new process: the same file, a fresh engine with the default on it.
	restarted := &appState{path: backend.state.where()}
	b2 := budgetBackendAt(t, restarted)
	fresh := &railConv{fakeConversation: &fakeConversation{rail: 3}}
	if err := b2.bindBudget(agent.ID, fresh); err != nil || fresh.railNow() != 8 {
		t.Fatalf("restarted, it binds %v (%v)", fresh.railNow(), err)
	}
	if _, err := backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID}); err != nil {
		t.Fatal(err)
	}
	b3 := budgetBackendAt(t, &appState{path: backend.state.where()})
	if err := b3.bindBudget(agent.ID, fresh); err != nil || fresh.railNow() != 0 {
		t.Fatalf("restarted unlimited, it binds %v (%v)", fresh.railNow(), err)
	}
	record, _ := b3.budgetRecord(agent.ID)
	if record.Source != appwire.BudgetUnlimited {
		t.Fatalf("restarted, the record says %+v", record)
	}
}

func budgetBackendAt(t *testing.T, state *appState) *appBackend {
	t.Helper()
	b := newFakeBackend(newAppRecorder(), func(appOpenAsk) (*appConn, error) { return nil, errors.New("not opened") })
	t.Cleanup(func() { _ = b.Close() })
	b.state = state
	return b
}

// NOTHING OF ITS OWN IS NOT A GUESS. A legacy agent with no figure — or the
// zero an older file may hold — reads inherited and unknown, never the
// default; held, it reads its engine's own figure, said as the engine's; and
// an engine that cannot say leaves it unknown.
func TestAgentBudgetLegacyIsUnknownUntilTheEngineSays(t *testing.T) {
	conv := &railConv{fakeConversation: &fakeConversation{rail: 4}}
	backend := budgetBackend(t, conv)
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	two := 2.0
	if _, err := backend.SetSettings(appwire.SettingsPatch{BudgetUSD: &appwire.Figure{USD: &two}}); err != nil {
		t.Fatal(err)
	}
	if err := backend.state.write(func(f *appStateFile) error {
		f.Budgets = map[string]float64{agent.ID: 0}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	record, _ := backend.budgetRecord(agent.ID)
	if record.Source != appwire.BudgetInherited || record.Limit != appwire.LimitUnknown || record.USD != nil || record.From != appwire.BudgetFromRecord {
		t.Fatalf("the record says %+v", record)
	}
	got, _ := backend.AgentBudget(agent.ID)
	if got.Source != appwire.BudgetInherited || got.From != appwire.BudgetFromEngine || got.Limit != appwire.LimitLimited || budgetFigure(got) != 4 {
		t.Fatalf("held, it says %+v", got)
	}
	conv.readErr = errors.New("no such method")
	got, _ = backend.AgentBudget(agent.ID)
	if got.Limit != appwire.LimitUnknown || got.USD != nil || got.Engine != nil {
		t.Fatalf("an engine that cannot say: %+v", got)
	}
	// A conversation with no reading at all is the same.
	plain := budgetBackend(t, &fakeConversation{rail: 4})
	other, err := mustCreate(t, plain, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := plain.AgentBudget(other.ID); got.Limit != appwire.LimitUnknown || got.Engine != nil {
		t.Fatalf("no reader: %+v", got)
	}
}

// A REFUSED SET CHANGES NOTHING. One the disk will not take leaves the file
// and the engine as they were; one the conversation will not take is rolled
// back, the old figure still bound.
func TestAgentBudgetRefusedSetConserves(t *testing.T) {
	conv := &railConv{fakeConversation: &fakeConversation{}, refuse: 9}
	backend := budgetBackend(t, conv)
	five := 5.0
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), BudgetUSD: &five})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.RaiseCap(appwire.CapRaise{Scope: appwire.CapAgent, ID: agent.ID, ToUSD: 6, For: appwire.RaiseToday}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(backend.state.where())

	nine := 9.0
	_, err = backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID, USD: &nine})
	if appwireCode(err) != appwire.CodeEngine {
		t.Fatalf("a refused bind answered %v", err)
	}
	after, _ := os.ReadFile(backend.state.where())
	got, _ := backend.AgentBudget(agent.ID)
	if conv.railNow() != 6 || budgetFigure(got) != 6 || *got.BaseUSD != 5 || got.RaisedUntil == nil || string(after) != string(before) {
		t.Fatalf("rolled back to %+v, rail %v\nbefore %s\nafter %s", got, conv.railNow(), before, after)
	}

	dir := filepath.Dir(backend.state.where())
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if probe, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		probe.Close()
		t.Skip("this user can write a read-only folder")
	}
	seven := 7.0
	if _, err := backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID, USD: &seven}); err == nil {
		t.Fatal("a set the disk refused was answered")
	}
	if _, err := backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID}); err == nil {
		t.Fatal("a none the disk refused was answered")
	}
	still, _ := os.ReadFile(backend.state.where())
	got, _ = backend.AgentBudget(agent.ID)
	if conv.railNow() != 6 || budgetFigure(got) != 6 || string(still) != string(before) {
		t.Fatalf("a refused write moved it: %+v, rail %v", got, conv.railNow())
	}
}

// A SET ANSWERS THE OPEN BUDGET ASK, and the agent.changed it says carries
// the new figure.
func TestAgentBudgetSetClosesTheAskAndSaysIt(t *testing.T) {
	conv := &railConv{fakeConversation: &fakeConversation{cost: 4.5}}
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	defer backend.Close()
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.today = func(time.Time) float64 { return 0 }
	five := 5.0
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), BudgetUSD: &five})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "go on", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return ok && opened.Ask.Subkind == appwire.SubkindBudget
	})
	twenty := 20.0
	if _, err := backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID, USD: &twenty}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		closed, ok := data.(appwire.AskClosed)
		return ok && closed.Option == appwire.OptionRaise
	})
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.Budget != nil && budgetFigure(*changed.Agent.Budget) == 20
	})
	if asks, _ := backend.ListAsks(agent.ID); len(asks) != 0 {
		t.Fatalf("asks still open: %+v", asks)
	}
}

func appwireCode(err error) string {
	var w *appwire.Error
	if errors.As(err, &w) {
		return w.Code
	}
	return ""
}

// A successful cap change is a real shared fact, not just the requesting
// window's RPC result. Ordinary later agent events retain that same cap.
func TestAgentBudgetChangedFactsReachOtherConsumers(t *testing.T) {
	conv := &railConv{fakeConversation: &fakeConversation{}}
	backend, rec := budgetRecordedBackend(t, conv)
	five := 5.0
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), BudgetUSD: &five})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Budget == nil || budgetFigure(*agent.Budget) != 5 {
		t.Fatalf("created cap: %+v", agent.Budget)
	}
	eight := 8.0
	if _, err := backend.SetAgentBudget(appwire.AgentBudgetSet{ID: agent.ID, USD: &eight}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.ID == agent.ID && changed.Agent.Budget != nil && budgetFigure(*changed.Agent.Budget) == 8
	})
	if err := backend.RaiseCap(appwire.CapRaise{Scope: appwire.CapAgent, ID: agent.ID, ToUSD: 10, For: appwire.RaiseToday}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.ID == agent.ID && changed.Agent.Budget != nil && budgetFigure(*changed.Agent.Budget) == 10 && changed.Agent.Budget.RaisedUntil != nil
	})
	a, _ := backend.find(agent.ID)
	view := backend.sayAgent(a)
	if view.Budget == nil || budgetFigure(*view.Budget) != 10 {
		t.Fatalf("later agent event lost changed cap: %+v", view.Budget)
	}
}
