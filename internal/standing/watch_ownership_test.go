package standing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func timerDefinitions(t *testing.T, w *Timer) map[string]string {
	t.Helper()
	paths := []string{w.primaryPath()}
	if w.platform == "linux" {
		paths = append(paths, w.linuxServicePath())
	}
	out := map[string]string{}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		out[path] = string(b)
	}
	return out
}

func TestImplicitTimerSetupRespectsOwnership(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		for _, scenario := range []string{"other-home", "other-home-gone", "same-home-live", "same-home-live-drift", "same-home-gone"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				runner := &recordingRunner{}
				owner := newTimer(t, platform, t.TempDir(), "", runner, time.Now())
				if err := owner.Install(context.Background()); err != nil {
					t.Fatal(err)
				}
				contender := *owner
				contender.executable = realProgram(t)
				if scenario == "other-home" || scenario == "other-home-gone" {
					contender.stateRoot = filepath.Join(t.TempDir(), "state")
				}
				if scenario == "other-home-gone" || scenario == "same-home-gone" {
					if err := os.Remove(owner.executable); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "same-home-live-drift" {
					f, err := os.OpenFile(owner.primaryPath(), os.O_APPEND|os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, err = f.WriteString("\n")
					_ = f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				before := timerDefinitions(t, owner)
				runner.calls = nil
				err := contender.Ensure(context.Background())
				denied := scenario == "other-home" || scenario == "other-home-gone" || scenario == "same-home-live-drift"
				if denied && !errors.Is(err, ErrWatchOwned) {
					t.Fatalf("want ownership refusal, got %v", err)
				}
				if !denied && err != nil {
					t.Fatal(err)
				}
				if scenario == "same-home-gone" {
					status, err := contender.Status()
					if err != nil || !status.Installed || len(runner.calls) == 0 {
						t.Fatalf("own stale timer not repaired: %+v %v", status, err)
					}
				} else {
					if len(runner.calls) != 0 || !reflect.DeepEqual(before, timerDefinitions(t, owner)) {
						t.Fatal("implicit setup changed another owner or touched its scheduler")
					}
				}
				// Explicit settings still deliberately take ownership.
				if err := contender.Install(context.Background()); err != nil {
					t.Fatal(err)
				}
				status, err := contender.Status()
				if err != nil || !status.Installed {
					t.Fatalf("explicit takeover failed: %+v %v", status, err)
				}
			})
		}
	}
}

func TestImplicitTimerSetupInstallsOnlyWhenAskedAndRepairKeepsAbsence(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			runner := &recordingRunner{}
			timer := newTimer(t, platform, t.TempDir(), "", runner, time.Now())
			drift, err := timer.Repair(context.Background())
			if err != nil || drift.Present || len(runner.calls) != 0 {
				t.Fatalf("repair installed an absent timer: %+v %v", drift, err)
			}
			if err := timer.Ensure(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(runner.calls) == 0 {
				t.Fatal("missing timer was not installed")
			}
			runner.calls = nil
			if err := timer.Ensure(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(runner.calls) != 0 {
				t.Fatal("healthy timer was reinstalled")
			}
		})
	}
}

func TestEveryTimerMutationSharesTheCancellableOwnerLock(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			runner := &recordingRunner{}
			timer := newTimer(t, platform, t.TempDir(), "", runner, time.Now())
			if err := timer.Install(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := timerDefinitions(t, timer)
			runner.calls = nil
			lock, err := os.OpenFile(timer.primaryPath()+".lock", os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := filelock.Lock(lock, true, false); err != nil {
				t.Fatal(err)
			}
			defer filelock.Unlock(lock)
			operations := map[string]func(context.Context) error{"ensure": timer.Ensure, "install": timer.Install, "uninstall": timer.Uninstall, "repair": func(ctx context.Context) error { _, err := timer.Repair(ctx); return err }}
			for name, operation := range operations {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				err := operation(ctx)
				cancel()
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("%s bypassed lock: %v", name, err)
				}
			}
			if len(runner.calls) != 0 || !reflect.DeepEqual(before, timerDefinitions(t, timer)) {
				t.Fatal("blocked writer touched scheduler or definitions")
			}
		})
	}
}

type heldInstallRunner struct {
	entered, release chan struct{}
	once             sync.Once
}

func (r *heldInstallRunner) Run(ctx context.Context, _ string, _ ...string) error {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestConcurrentProfilesCannotBothImplicitlyClaimTimer(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			runner := &heldInstallRunner{entered: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			defer release.Do(func() { close(runner.release) })
			first := newTimer(t, platform, t.TempDir(), "", runner, time.Now())
			second := *first
			second.stateRoot = filepath.Join(t.TempDir(), "second")
			second.executable = realProgram(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			a, b := make(chan error, 1), make(chan error, 1)
			go func() { a <- first.Ensure(ctx) }()
			select {
			case <-runner.entered:
			case <-ctx.Done():
				t.Fatal("first installer did not start")
			}
			go func() { b <- second.Ensure(ctx) }()
			select {
			case err := <-b:
				t.Fatalf("second installer bypassed ownership lock: %v", err)
			case <-time.After(40 * time.Millisecond):
			}
			release.Do(func() { close(runner.release) })
			if err := <-a; err != nil {
				t.Fatal(err)
			}
			if err := <-b; !errors.Is(err, ErrWatchOwned) {
				t.Fatalf("second profile stole timer: %v", err)
			}
			status, err := first.Status()
			if err != nil || !status.Installed {
				t.Fatalf("first owner lost timer: %+v %v", status, err)
			}
		})
	}
}

func TestUnsupportedTimerOperationsDoNotCreateHostFiles(t *testing.T) {
	dir := t.TempDir()
	timer := &Timer{platform: "unsupported", homeDir: dir}
	for _, operation := range []func(context.Context) error{timer.Ensure, timer.Install, timer.Uninstall, func(ctx context.Context) error { _, err := timer.Repair(ctx); return err }} {
		if err := operation(context.Background()); err == nil {
			t.Fatal("unsupported timer accepted")
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsupported operations touched host: %v %v", entries, err)
	}
}
