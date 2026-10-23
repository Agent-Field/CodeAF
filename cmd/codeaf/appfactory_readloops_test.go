package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── what this Mac's factory reads by itself ────────────────────────────────
//
// Real appBackend, real wire, real local host, real poll and runner. Only the
// two outside worlds are fakes, both on loopback: a GitHub that lists issues
// and a model endpoint that would answer a triage read, which counts whether
// anything asked it.

const readLoopsPin = "deepseek/deepseek-v4.1-flash"

// readLoopsWorld is a fake GitHub and a fake model, counting what reached them.
type readLoopsWorld struct {
	t        *testing.T
	github   *httptest.Server
	model    *httptest.Server
	launches atomic.Int64
	ghCalls  atomic.Int64
	asked    atomic.Int64
	sources  atomic.Int64
	mu       sync.Mutex
	rules    pipeline.File
	issues   []int
	models   []string
}

func newReadLoopsWorld(t *testing.T) *readLoopsWorld {
	w := &readLoopsWorld{t: t}
	w.github = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.ghCalls.Add(1)
		rw.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api/issues":
			w.mu.Lock()
			rows := []map[string]any{}
			for _, n := range w.issues {
				rows = append(rows, map[string]any{"number": n, "title": fmt.Sprintf("Issue %d", n), "body": "b", "html_url": fmt.Sprintf("https://github.com/acme/api/issues/%d", n), "created_at": time.Now().Add(-time.Hour).Format(time.RFC3339), "updated_at": time.Now().Format(time.RFC3339), "user": map[string]any{"login": "owner"}})
			}
			w.mu.Unlock()
			_ = json.NewEncoder(rw).Encode(rows)
		case "/user":
			_, _ = io.WriteString(rw, `{"login":"owner"}`)
		default:
			_, _ = io.WriteString(rw, `[]`)
		}
	}))
	w.model = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if r.Method != http.MethodPost {
			// A catalog or probe read is not a model call.
			rw.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(rw, `{"data":[]}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.asked.Add(1)
		w.mu.Lock()
		w.models = append(w.models, req.Model)
		w.mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(rw, `{"choices":[{"message":{"role":"assistant","content":"{\"read\":\"A read\",\"size\":\"S\",\"est_usd\":1}"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(w.github.Close)
	t.Cleanup(w.model.Close)
	return w
}

func (w *readLoopsWorld) file(n int) {
	w.mu.Lock()
	w.issues = append(w.issues, n)
	w.mu.Unlock()
}

func (w *readLoopsWorld) requested() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.models...)
}

// source is the host's poll source over the fake GitHub, counting how many
// times the host asked for one.
func (w *readLoopsWorld) source() func(context.Context) *factorygithub.Source {
	return func(context.Context) *factorygithub.Source {
		w.sources.Add(1)
		return factorygithub.New(forge.NewClient("test-token").WithBaseURL(w.github.URL), []string{"acme/api"}, nil)
	}
}

// startRule makes the repository's start rules allow a New item labelled
// agent-ready and sized S, the way a factory.yaml would.
func (w *readLoopsWorld) startRule(t *testing.T) {
	t.Helper()
	f, err := pipeline.Parse("default_budget: 1\nstart:\n  labels: [agent-ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	w.rules = f
	w.mu.Unlock()
}

// intake is the floor's intake with the forge left out: no rules or owners are
// fetched, and the rules are whatever the world holds (none, until startRule).
// It has a real record, so the intake really runs. A launch counts and moves
// the item to Queued, as the real door does, so one item launches once.
func (w *readLoopsWorld) intake(st *store.Store) *intake.Intake {
	history, _ := record.Open(st, record.Options{})
	return intake.New(st, intake.Options{
		Record:  history,
		Refresh: func(context.Context) error { return nil },
		Load: func(string) (pipeline.File, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			return w.rules, nil
		},
		Launch: func(id int) error {
			w.launches.Add(1)
			return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
		},
	})
}

// readLoopsProfile saves the Task models policy the triage read must obey, in
// the profile the engine reads, and points the model at the fake. A pin it can
// meet is `readLoopsPin` on the default service; a pin it cannot is another
// provider with no source.
func readLoopsProfile(t *testing.T, w *readLoopsWorld, pin string) {
	t.Helper()
	// THE PROCESS-WIDE PARENT IS SNAPSHOT AND CLEARED: triage consults it before
	// the saved policy, and another test's engine boot may have left a one-model
	// parent there. It is restored with the test.
	previous := factoryWorkerParent()
	setFactoryProcess(func() { factoryProcess.parent = session.Config{} })
	t.Cleanup(func() { setFactoryProcess(func() { factoryProcess.parent = previous }) })
	t.Setenv("OPENROUTER_API_KEY", "sk-test-not-real")
	t.Setenv("CODEAF_BASE_URL", w.model.URL)
	rows := map[string]any{config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: pin, "model_pool": "off"}
	for _, role := range []string{"reflex", "low", "worker", "high", "mastermind"} {
		rows["models.tiers."+role] = "tier/" + role
	}
	profile := v3Profile(t, rows)
	t.Setenv(config.ProfileDirEnv, profile)
}

// quickReads makes the loops' waits milliseconds, restored with the test.
func quickReads(t *testing.T) {
	t.Helper()
	watch, tick := factoryWatchEvery, factoryTriageEvery
	factoryWatchEvery, factoryTriageEvery = 5*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { factoryWatchEvery, factoryTriageEvery = watch, tick })
}

// readLoopsSetup makes the local setup over a home, with the world
// wired in and the saved one-model policy in force.
func readLoopsSetup(t *testing.T, w *readLoopsWorld, pin string) *localSetup {
	t.Helper()
	// THE HOME COMES FIRST: newLocalSetup points the profile at a decoy, and
	// the saved policy must be written into the profile that is then in force.
	s := newLocalSetup(t)
	readLoopsProfile(t, w, pin)
	s.backend.local.pollSource = w.source()
	s.backend.local.newIntake = w.intake
	return s
}

// watchAndStart starts the factory and then watches acme/api, nudging the
// poll the way a saved repository choice does, so its first read is now.
func watchAndStart(t *testing.T, s *localSetup) appwire.FactoryLocalStatus {
	t.Helper()
	st := s.start()
	floor := s.backend.local.host.floor
	if err := floor.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	factorygithub.Nudge(floor)
	return st
}

func readLoopsItems(t *testing.T, floor *store.Store) []factory.Item {
	t.Helper()
	items, err := floor.List()
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// settle gives a loop that was never started every chance to appear.
func settle() { time.Sleep(250 * time.Millisecond) }

// THIS MAC'S FACTORY POLLS AND NEVER ASKS A MODEL ON ITS OWN. Once it knows
// whose GitHub it is, the start brings its runner and the GitHub poll on, so a
// filed issue reaches the floor; no triage reads an item, so the model is never
// asked, however long it runs and across a restart; quitting stops the poll.
func TestThisMacsFactoryPollsAndNeverAsksAModel(t *testing.T) {
	quickReads(t)
	world := newReadLoopsWorld(t)
	world.file(7)
	s := readLoopsSetup(t, world, readLoopsPin)
	first := watchAndStart(t, s)
	if !first.Runner {
		t.Fatalf("the start did not bring the runner on: %+v", first)
	}
	host := s.backend.local.host
	if _, err := host.floor.Create(factory.Item{Repo: "acme/api", Title: "Waiting", Kind: factory.KindIssue, State: factory.StateNew}); err != nil {
		t.Fatal(err)
	}
	filed := func(floor *store.Store, title string) func() bool {
		return func() bool {
			for _, it := range readLoopsItems(t, floor) {
				if it.Title == title {
					return true
				}
			}
			return false
		}
	}
	eventually(t, "the filed issue is on the floor", filed(host.floor, "Issue 7"))
	settle()
	noModel := func(floor *store.Store) {
		t.Helper()
		if n := world.asked.Load(); n != 0 {
			t.Fatalf("a model was asked %d times with nobody pressing Run: %v", n, world.requested())
		}
		for _, it := range readLoopsItems(t, floor) {
			if it.Triage.Read != "" || it.State != factory.StateNew {
				t.Fatalf("an item was read or started on its own: %+v", it)
			}
		}
		if release, ok := floor.TryTriager(); !ok {
			t.Fatal("a triager holds its lock")
		} else {
			release()
		}
	}
	noModel(host.floor)

	// A RESTART BRINGS THE POLL BACK, and still no model.
	s.backend.Close()
	w, _ := reopened(t, s.backend.factoryHub())
	w.backend.local.pollSource = world.source()
	w.backend.local.newIntake = world.intake
	var resumed appwire.FactoryLocalStatus
	w.must("factory.local_status", map[string]any{"caller": "w1"}, &resumed)
	if resumed.State != appwire.LocalRunning || !resumed.Runner || resumed.Factory != first.Factory {
		t.Fatalf("resume: %+v", resumed)
	}
	next := w.backend.local.host
	world.file(8)
	eventually(t, "the restarted factory polls", filed(next.floor, "Issue 8"))
	settle()
	noModel(next.floor)

	// QUIT STOPS THE POLL, BOUNDED, and nothing is read afterwards.
	started := time.Now()
	w.backend.closeLocalFactory()
	if took := time.Since(started); took > appLocalStopWait {
		t.Fatalf("quit took %s", took)
	}
	if release, ok := next.floor.TryPoller(); !ok {
		t.Fatal("quit left the poll lock held")
	} else {
		release()
	}
	gh := world.ghCalls.Load()
	world.file(9)
	settle()
	if world.ghCalls.Load() != gh {
		t.Fatal("the poll kept reading after quit")
	}
}

// AN ARRIVING ISSUE STARTS NOTHING. Even with a repository start rule that
// allows the waiting item, this Mac's runner, on and polling, launches no job:
// a job starts only when a person presses Run.
func TestStartRulesNeverLaunchAJobOnThisMac(t *testing.T) {
	quickReads(t)
	world := newReadLoopsWorld(t)
	world.startRule(t)
	world.file(4)
	s := readLoopsSetup(t, world, readLoopsPin)
	if st := watchAndStart(t, s); !st.Runner {
		t.Fatalf("start: %+v", st)
	}
	floor := s.backend.local.host.floor
	if _, err := floor.Create(factory.Item{Repo: "acme/api", Num: 3, Title: "Ready", Kind: factory.KindIssue, State: factory.StateNew, Labels: []string{"agent-ready", "size:S"}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the poll has run", func() bool { return len(readLoopsItems(t, floor)) >= 2 })
	settle()
	if n := world.launches.Load(); n != 0 {
		t.Fatalf("%d jobs launched with nobody pressing Run", n)
	}
	for _, it := range readLoopsItems(t, floor) {
		if it.State != factory.StateNew {
			t.Fatalf("an item left Backlog on its own: %+v", it)
		}
	}
}

// A HOST MADE LATER IN THE SAME PROCESS STARTS ITS OWN POLL: the start is per
// host, so quitting one local factory and starting another is not answered by
// a process-wide once that already fired.
func TestANewHostInTheSameProcessStartsItsOwnPoll(t *testing.T) {
	quickReads(t)
	world := newReadLoopsWorld(t)
	world.file(1)
	s := readLoopsSetup(t, world, readLoopsPin)
	watchAndStart(t, s)
	host := s.backend.local.host
	eventually(t, "the first host polls", func() bool { return len(readLoopsItems(t, host.floor)) == 1 })
	s.backend.Close()

	w, _ := reopened(t, s.backend.factoryHub())
	w.backend.local.pollSource = world.source()
	w.backend.local.newIntake = world.intake
	var st appwire.FactoryLocalStatus
	w.must("factory.local_status", map[string]any{"caller": "w1"}, &st)
	world.file(2)
	next := w.backend.local.host
	if next == host {
		t.Fatal("the same host came back")
	}
	eventually(t, "the second host polls the new issue", func() bool {
		for _, it := range readLoopsItems(t, next.floor) {
			if it.Title == "Issue 2" {
				return true
			}
		}
		return false
	})
	if world.sources.Load() != 2 {
		t.Fatalf("sources asked for = %d; each host asks once", world.sources.Load())
	}
}

// ANOTHER CODEAF'S FACTORY IS NEVER READ FOR: an engine that joined it is
// refused a run and starts no loop, no poll and no model call.
func TestAJoinedFactoryGetsNoReadLoops(t *testing.T) {
	quickReads(t)
	world := newReadLoopsWorld(t)
	world.file(1)
	a := newLocalSetup(t)
	readLoopsProfile(t, world, readLoopsPin)
	a.start()
	b := newFactoryWire(t, testFactoryHub(t))
	offlineLocal(b.backend, &setupGitHub{}, t)
	b.backend.local.pollSource = world.source()
	b.backend.local.newIntake = world.intake
	var second appwire.FactoryLocalStatus
	b.must("factory.local_start", map[string]any{"caller": "b"}, &second)
	if second.Authority != appwire.LocalAuthorityOther {
		t.Fatalf("second: %+v", second)
	}
	b.mustFail("factory.local_run", map[string]any{"caller": "b"}, appwire.CodeConflict)
	settle()
	if world.sources.Load() != 0 || world.ghCalls.Load() != 0 || world.asked.Load() != 0 {
		t.Fatalf("a joined factory was read: sources=%d github=%d model=%d", world.sources.Load(), world.ghCalls.Load(), world.asked.Load())
	}
}

// THE SAVED ONE MODEL IS STRICT where triage still runs (a terminal's factory):
// a pin the profile cannot meet is a refusal, so triage asks no model at all,
// never the tier or the fallback.
func TestTriageNeverFallsBackFromAnUnmetOneModel(t *testing.T) {
	quickReads(t)
	world := newReadLoopsWorld(t)
	readLoopsProfile(t, world, "openai/gpt-x@moonshot")
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := floor.Create(factory.Item{Repo: "acme/api", Title: "Waiting", Kind: factory.KindIssue, State: factory.StateNew}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); runFactoryTriage(ctx, floor, world.intake) }()
	t.Cleanup(func() { cancel(); <-done })
	// THE TRIAGER IS ALIVE: "no call" must mean refused, not absent.
	eventually(t, "triage holds its lock", func() bool {
		release, ok := floor.TryTriager()
		if ok {
			release()
		}
		return !ok
	})
	settle()
	if n := world.asked.Load(); n != 0 {
		t.Fatalf("triage asked a model %d times, %v, for a pin it could not meet", n, world.requested())
	}
	if items := readLoopsItems(t, floor); items[0].Triage.Read != "" {
		t.Fatalf("an item was read without its model: %+v", items[0])
	}
}

// THE TERMINAL'S PROCESS-WIDE START IS UNCHANGED: it still runs the shared
// loop and holds the lock until its context ends. This proves one loop is
// ACTIVE (a second would only block on the lock and look identical), not that
// the start ran its body once; nothing counts entries.
func TestTheTerminalTriageStartStillHoldsOneActiveLoop(t *testing.T) {
	quickReads(t)
	world := newReadLoopsWorld(t)
	readLoopsProfile(t, world, readLoopsPin)
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := floor.Create(factory.Item{Repo: "acme/api", Title: "Waiting", Kind: factory.KindIssue, State: factory.StateNew}); err != nil {
		t.Fatal(err)
	}
	factoryTriage = sync.Once{}
	t.Cleanup(func() { factoryTriage = sync.Once{} })
	ctx, cancel := context.WithCancel(context.Background())
	startFactoryTriage(ctx, floor)
	startFactoryTriage(ctx, floor)
	eventually(t, "the item is read", func() bool {
		items := readLoopsItems(t, floor)
		return len(items) == 1 && items[0].Triage.Read != ""
	})
	if release, ok := floor.TryTriager(); ok {
		release()
		t.Fatal("the terminal's triage does not hold its lock")
	}
	cancel()
	eventually(t, "the lock goes with the context", func() bool {
		release, ok := floor.TryTriager()
		if ok {
			release()
		}
		return ok
	})
}
