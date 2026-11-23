package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// poolOwnerDoors opens the process and window doors on one profile, without
// unrelated startup requests hiding which owner's errand the test is holding.
func poolOwnerDoors(t *testing.T, baseURLs ...string) (*v3Process, *engineFleet) {
	t.Helper()
	poolNetworkFixture(t)
	t.Setenv("CODEAF_MODEL_POOL", "off")
	if len(baseURLs) == 0 {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[]}`)
		}))
		t.Cleanup(server.Close)
		t.Setenv("CODEAF_BASE_URL", server.URL+"/api/v1")
	} else {
		t.Setenv("CODEAF_BASE_URL", baseURLs[0])
	}
	previous := http.DefaultTransport
	http.DefaultTransport = moneyTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	proc := v3TestProcess(t)
	fleet := poolOwnerWindow(t, proc.ProfileDir)
	t.Setenv("CODEAF_MODEL_POOL", "read")
	return proc, fleet
}

func poolOwnerWindow(t *testing.T, profile string) *engineFleet {
	t.Helper()
	fleet := onePipeFleet("", hostedClient(t))
	t.Cleanup(fleet.closeAll)
	hostOptions(fleet, remote.Welcome{Version: remote.Version, ProfileDir: profile}, false)
	return fleet
}

// poolOwnerSweep substitutes only the work, keeping the launch's real errand
// registration, cancellation and close path under test.
func poolOwnerSweep(t *testing.T, work func(context.Context, string)) {
	t.Helper()
	previous := poolJudgeSweepRun
	poolJudgeSweepRun = func(ctx context.Context, _ config.Config, profile, _ string, _ func() []catalog.Model, _ func(string) judge.Ask, _ func() time.Time) {
		work(ctx, profile)
	}
	t.Cleanup(func() { poolJudgeSweepRun = previous })
}

func poolOwnerLaunch(t *testing.T, proc *v3Process) *v3Launch {
	t.Helper()
	launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return launch
}

func poolOwnerWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("owner's gated work did not finish")
	}
}

func poolOwnerRelease(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	return release, free
}

// TestPoolOwnerCloseLetsSurvivorCompleteWrite proves C1: closing a window
// leaves an in-flight process errand able to finish its profile write.
func TestPoolOwnerCloseLetsSurvivorCompleteWrite(t *testing.T) {
	proc, window := poolOwnerDoors(t)
	release, free := poolOwnerRelease(t)
	started, finished := make(chan struct{}), make(chan struct{})
	wrote := filepath.Join(proc.ProfileDir, "survivor")
	poolOwnerSweep(t, func(ctx context.Context, _ string) {
		defer close(finished)
		close(started)
		<-release
		if ctx.Err() == nil {
			if err := os.WriteFile(wrote, []byte("complete"), 0o600); err != nil {
				t.Error(err)
			}
		}
	})
	poolOwnerLaunch(t, proc)
	poolOwnerWait(t, started)
	window.closeAll()
	free()
	poolOwnerWait(t, finished)
	if data, err := os.ReadFile(wrote); err != nil || string(data) != "complete" {
		t.Fatalf("closing the window cancelled the surviving process's write: %q, %v", data, err)
	}
}

// poolOwnerWarm holds the catalog independently of the process so only the
// errand's cancellation can end its waiter. The guard seam reports completion
// after the real warm has returned, including its possible picker-cache write.
func poolOwnerWarm(t *testing.T, proc *v3Process, count int) (func(), <-chan struct{}) {
	t.Helper()
	release, free := poolOwnerRelease(t)
	models := catalog.LoadLazy(context.Background(), catalog.Options{
		Dir: t.TempDir(), BaseURL: "https://models.invalid", APIKey: "fixture",
		HTTPClient: &http.Client{Transport: moneyTransport(func(request *http.Request) (*http.Response, error) {
			select {
			case <-release:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"test/model","context_length":200000}]}`))}, nil
		})},
	})
	t.Cleanup(func() { free(); models.Close() })
	proc.Models.Close()
	proc.Models = models
	agent := v3TrackedAgent(t, t.TempDir())
	t.Cleanup(func() { _ = agent.Close() })
	started, finished := make(chan struct{}, count), make(chan struct{}, count)
	previous := poolRefreshGo
	poolRefreshGo = func(scope string, fn func()) {
		if scope != "test/pool-owner-warm" {
			previous(scope, fn)
			return
		}
		guard.Go(scope, func() {
			defer func() { finished <- struct{}{} }()
			started <- struct{}{}
			fn()
		})
	}
	t.Cleanup(func() { poolRefreshGo = previous })
	for range count {
		proc.warmModels("test/pool-owner-warm", agent, "test/model")
		poolOwnerWait(t, started)
	}
	proc.Models = nil
	return free, finished
}

// TestPoolOwnerSurvivorStillCancelsAndJoins proves C2: a model warm started
// after a window leaves is cancelled and joined by its own process close.
func TestPoolOwnerSurvivorStillCancelsAndJoins(t *testing.T) {
	proc, window := poolOwnerDoors(t)
	window.closeAll()
	free, finished := poolOwnerWarm(t, proc, 1)
	proc.closeAll()
	select {
	case <-finished:
	case <-time.After(250 * time.Millisecond):
		t.Error("survivor close did not cancel and join its new model warm")
		free()
		poolOwnerWait(t, finished)
	}
	free()
}

// TestPoolOwnerCloseJoinsEveryWriterBeforeReturning proves C3: after another
// owner leaves, every model warm stops before close and none writes afterward.
func TestPoolOwnerCloseJoinsEveryWriterBeforeReturning(t *testing.T) {
	t.Run("last-owner-model-warms", func(t *testing.T) {
		proc, window := poolOwnerDoors(t)
		window.closeAll()
		free, finished := poolOwnerWarm(t, proc, 2)
		proc.closeAll()
		before := poolFiles(t, filepath.Join(os.Getenv("CODEAF_HOME"), "v3"))
		free()
		poolOwnerWait(t, finished)
		poolOwnerWait(t, finished)
		if !reflect.DeepEqual(before, poolFiles(t, filepath.Join(os.Getenv("CODEAF_HOME"), "v3"))) {
			t.Error("owner's model warms wrote under the profile after close returned")
		}
	})
	t.Run("another-owner-remains", func(t *testing.T) {
		proc, _ := poolOwnerDoors(t)
		release, free := poolOwnerRelease(t)
		started, cancelled, finished := make(chan struct{}, 2), make(chan struct{}, 2), make(chan struct{}, 2)
		poolOwnerSweep(t, func(ctx context.Context, _ string) {
			// This signal is inside the tracked work, before its WaitGroup
			// release, so close must observe both completions before returning.
			defer func() { finished <- struct{}{} }()
			started <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			<-release
		})
		poolOwnerLaunch(t, proc)
		poolOwnerLaunch(t, proc)
		poolOwnerWait(t, started)
		poolOwnerWait(t, started)
		closed := make(chan struct{})
		guard.Go("test/pool-owner-join", func() { proc.closeAll(); close(closed) })
		poolOwnerWait(t, cancelled)
		poolOwnerWait(t, cancelled)
		select {
		case <-closed:
			t.Error("owner close returned before its cancelled writers finished")
		case <-time.After(5 * time.Millisecond):
		}
		free()
		poolOwnerWait(t, closed)
		for range 2 {
			select {
			case <-finished:
			default:
				t.Error("owner close did not join every writer while another owner remained")
			}
		}
	})
}

// TestPoolOwnerEmptyAndRepeatedClosePreservesOtherHold proves C4: closing an
// empty window twice cannot cancel or release the process's hold.
func TestPoolOwnerEmptyAndRepeatedClosePreservesOtherHold(t *testing.T) {
	proc, window := poolOwnerDoors(t)
	release, free := poolOwnerRelease(t)
	contexts := make(chan context.Context, 1)
	finished := make(chan struct{})
	poolOwnerSweep(t, func(ctx context.Context, _ string) {
		defer close(finished)
		contexts <- ctx
		<-release
	})
	poolOwnerLaunch(t, proc)
	ctx := <-contexts
	window.closeAll()
	window.closeAll()
	if ctx.Err() != nil {
		t.Error("empty or repeated window close released another owner's hold")
	}
	free()
	poolOwnerWait(t, finished)
	proc.closeAll()
	if ctx.Err() == nil {
		t.Error("process lost tracking after the empty window closed")
	}
}

func poolOwnerTableSize() int {
	poolErrandsMu.Lock()
	defer poolErrandsMu.Unlock()
	return len(poolErrandSet)
}

// TestPoolOwnerClosedDoorsLeaveNoTrackers proves C5: repeated owners and a
// launch arriving after its process closed cannot leave a profile in the table.
func TestPoolOwnerClosedDoorsLeaveNoTrackers(t *testing.T) {
	before := poolOwnerTableSize()
	proc, window := poolOwnerDoors(t)
	poolOwnerSweep(t, func(context.Context, string) {})
	for range 4 {
		poolOwnerWindow(t, proc.ProfileDir).closeAll()
	}
	window.closeAll()
	proc.closeAll()
	// A queued launch must keep the closed owner's lifetime. Run its work
	// synchronously so the table assertion cannot race a wrongly admitted sweep.
	previous := poolRefreshGo
	poolRefreshGo = func(_ string, fn func()) { fn() }
	t.Cleanup(func() { poolRefreshGo = previous })
	_, _ = openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if got := poolOwnerTableSize(); got != before {
		t.Fatalf("closed owners left pool tracker table at %d, started at %d", got, before)
	}
}

// TestPoolOwnerIdleCloseDoesNotWaitForAnotherOwner proves C6: closing idle
// windows costs no errand budget even while the process has stuck work.
func TestPoolOwnerIdleCloseDoesNotWaitForAnotherOwner(t *testing.T) {
	proc, window := poolOwnerDoors(t)
	release, free := poolOwnerRelease(t)
	started, finished := make(chan struct{}), make(chan struct{})
	poolOwnerSweep(t, func(context.Context, string) {
		defer close(finished)
		close(started)
		<-release
	})
	poolOwnerLaunch(t, proc)
	poolOwnerWait(t, started)
	windows := []*engineFleet{window}
	for range 4 {
		windows = append(windows, poolOwnerWindow(t, proc.ProfileDir))
	}
	start := time.Now()
	for _, fleet := range windows {
		fleet.closeAll()
	}
	if elapsed := time.Since(start); elapsed >= poolCloseBudget/2 {
		t.Errorf("idle owners waited for another owner's stuck errand: %v", elapsed)
	}
	free()
	poolOwnerWait(t, finished)
	proc.closeAll()
}

// TestPoolOwnerJudgeHookContextBelongsToProcess proves C7: a live hook keeps
// its context when a window closes, and loses it when its process closes.
func TestPoolOwnerJudgeHookContextBelongsToProcess(t *testing.T) {
	release, free := poolOwnerRelease(t)
	requests := make(chan context.Context, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		_, _ = io.Copy(io.Discard, request.Body)
		requests <- request.Context()
		select {
		case <-release:
		case <-request.Context().Done():
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"score\":88,\"reason\":\"complete\"}"}}]}`)
	}))
	t.Cleanup(func() { free(); server.Close() })
	proc, window := poolOwnerDoors(t, server.URL+"/api/v1")
	poolOwnerSweep(t, func(context.Context, string) {})
	previousCatalog := config.CrewCatalog
	config.CrewCatalog = poolTestCatalog
	t.Cleanup(func() { config.CrewCatalog = previousCatalog })
	launch := poolOwnerLaunch(t, proc)
	window.closeAll()
	launch.Config.TaskLanded(poolTestLanding())
	var ctx context.Context
	select {
	case ctx = <-requests:
	case <-time.After(5 * time.Second):
		t.Fatalf("unrelated window close disabled the process's judge hook; files: %v", poolFiles(t, filepath.Join(proc.ProfileDir, "pool")))
	}
	if ctx.Err() != nil {
		t.Error("judge hook started with a cancelled context after window close")
	}
	proc.closeAll()
	poolOwnerWait(t, ctx.Done())
	before := poolFiles(t, filepath.Join(proc.ProfileDir, "pool"))
	free()
	launch.Config.TaskLanded(poolTestLanding())
	if !reflect.DeepEqual(before, poolFiles(t, filepath.Join(proc.ProfileDir, "pool"))) {
		t.Error("closed judge hook wrote under its old profile")
	}
}
