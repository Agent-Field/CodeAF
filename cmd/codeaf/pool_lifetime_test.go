package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestPoolSweepCloseDiscardsLateScoresAndRetriesOnRestart(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "read")
	proc := v3TestProcess(t)
	landing := poolTestLanding()
	if err := writePendingLanding(proc.ProfileDir, "do", landing); err != nil {
		t.Fatal(err)
	}
	started, finished := make(chan struct{}), make(chan struct{})
	old := poolJudgeSweepRun
	poolJudgeSweepRun = func(ctx context.Context, settings config.Config, profile, tasks string, models func() []catalog.Model, _ func(string) judge.Ask, now func() time.Time) {
		defer close(finished)
		ask := func(string) judge.Ask {
			return func(ctx context.Context, _, _ string) (string, error) {
				select {
				case <-started:
				default:
					close(started)
				}
				<-ctx.Done()
				// A provider can deliver a successful response alongside cancellation.
				return `{"score":88,"reason":"completed"}`, nil
			}
		}
		poolJudgeSweepContext(ctx, settings, profile, "", poolTestCatalog, ask, now)
	}
	t.Cleanup(func() { poolJudgeSweepRun = old })
	if _, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	<-started
	proc.closeAll()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("sweep did not stop at close")
	}
	pool := config.ProfilePath(proc.ProfileDir, "pool")
	for _, name := range []string{record.OwnSheetName, "outbox.jsonl", "install", "judge-last.json", "sweep-last.json"} {
		if _, err := os.Stat(filepath.Join(pool, name)); !os.IsNotExist(err) {
			t.Errorf("cancelled sweep wrote %s: %v", name, err)
		}
	}
	if alreadyJudged(pool, landing.ID, landing.Attempt) {
		t.Fatal("cancelled landing cannot be retried")
	}
	var asked []string
	poolJudgeSweep(proc.Settings, proc.ProfileDir, "", poolTestCatalog, poolTestAsk(proc.Settings, &asked), time.Now)
	if len(asked) == 0 || !alreadyJudged(pool, landing.ID, landing.Attempt) {
		t.Fatal("restart failed to score the cancelled landing")
	}
	sheet, err := record.LoadSheet(record.OwnSheetPath(pool))
	if err != nil || len(record.Cells(sheet)) != 2 {
		t.Fatalf("completed restart did not save both seats: %v", err)
	}
	if err := os.RemoveAll(pool); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pool); !os.IsNotExist(err) {
		t.Fatalf("pool exists after close: %v", err)
	}
}

func TestHostedPoolWritersBelongToFleet(t *testing.T) {
	profile := testHome(t)
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("CODEAF_TELEMETRY", "on")
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	old := poolRefreshGo
	poolRefreshGo = func(scope string, fn func()) {
		go func() {
			if scope == "pool/push" {
				close(started)
				<-release
				defer close(finished)
			}
			fn()
		}()
	}
	t.Cleanup(func() { poolRefreshGo = old })
	fleet := onePipeFleet("", hostedClient(t))
	t.Cleanup(fleet.closeAll)
	hostOptions(fleet, remote.Welcome{Version: remote.Version, ProfileDir: profile}, false)
	<-started
	closed := make(chan struct{})
	go func() { fleet.closeAll(); close(closed) }()
	select {
	case <-closed:
		close(release)
		<-finished
		t.Fatal("fleet closed while its pool writer was still running")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	<-closed
	<-finished
	pool := filepath.Join(profile, "pool")
	if _, err := os.Stat(pool); !os.IsNotExist(err) {
		t.Fatalf("cancelled startup writer created pool files: %v", err)
	}
}

func TestPoolCloseBoundsAnUncooperativeSweep(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_MODEL_POOL", "read")
	landing := poolTestLanding()
	if err := writePendingLanding(profile, "do", landing); err != nil {
		t.Fatal(err)
	}
	held := poolErrandsStart(profile)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ask := func(string) judge.Ask {
		return func(context.Context, string, string) (string, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			return `{"score":88,"reason":"completed"}`, nil
		}
	}
	poolErrandGoCtx(held, "pool/judge-sweep", func(ctx context.Context) {
		defer close(finished)
		poolJudgeSweepContext(ctx, config.Config{APIKey: "fixture"}, profile, "", poolTestCatalog, ask, time.Now)
	})
	<-started
	closed := make(chan struct{})
	go func() { stopPoolErrands(held); close(closed) }()
	select {
	case <-closed:
	case <-time.After(1500 * time.Millisecond):
		close(release)
		<-finished
		<-closed
		t.Fatal("pool close exceeded its one-second shutdown budget")
	}
	pool := filepath.Join(profile, "pool")
	// Removing the owner's files after close makes any surviving write visible.
	if err := os.RemoveAll(pool); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-finished
	if _, err := os.Stat(pool); !os.IsNotExist(err) {
		t.Fatalf("late provider response recreated pool after close: %v", err)
	}
}

func TestPoolLandingHookCannotWriteAfterProcessClose(t *testing.T) {
	t.Setenv("CODEAF_MODEL_POOL", "read")
	proc := v3TestProcess(t)
	launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	proc.closeAll()
	pool := config.ProfilePath(proc.ProfileDir, "pool")
	if err := os.RemoveAll(pool); err != nil {
		t.Fatal(err)
	}
	// Run any wrongly scheduled work before inspecting the files, rather than
	// letting an asynchronous writer race the assertion itself.
	old := poolRefreshGo
	poolRefreshGo = func(_ string, fn func()) { fn() }
	t.Cleanup(func() { poolRefreshGo = old })
	launch.Config.TaskLanded(poolTestLanding())
	if _, err := os.Stat(pool); !os.IsNotExist(err) {
		t.Fatalf("late landing recreated pool after close: %v", err)
	}
}
