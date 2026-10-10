package main

// chatv3_clock.go is the process side of automations (internal/automation;
// docs/design/automations/DESIGN.md): `codeaf clock`, the one process that runs
// them while a codeaf window is open, and the window's half — holding presence
// for as long as it is open and starting a clock when none is running.
//
// `codeaf clock` IS MACHINERY, NOT A COMMAND, and like `codeaf engine` it is
// absent from the usage text: a window starts it, it draws nothing, and it
// leaves on its own when the last window has been closed for half a minute.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/subharness"
)

// automationsRoot is the folder the store, the presence files and the clock's
// lock live in. It follows CODEAF_HOME, so a demo home or a test home has a
// clock of its own.
func automationsRoot() string { return home.Join("v3", "automations") }

var (
	automationsOnce  sync.Once
	automationsStore *automation.Store
)

// v3Automations is the door a conversation proposes through, or nil.
//
// THE STORE IS OPENED ONCE PER PROCESS: every conversation a session host holds
// shares one handle on the database rather than opening its own. A store that
// cannot be opened is automations off for this process — a capability that
// cannot work is absent, not broken.
func v3Automations() *session.Automations {
	automationsOnce.Do(func() {
		opened, err := automation.Open(automationsRoot())
		if err != nil {
			log.Printf("automations are off in this process: %v", err)
			return
		}
		automationsStore = opened
	})
	if automationsStore == nil {
		return nil
	}
	return &session.Automations{Store: automationsStore, Zone: session.LocalZone()}
}

// keepAutomationsWindow registers this process as an open window and keeps a
// clock running while it is. The release is the window closing.
func keepAutomationsWindow(label string) func() {
	root := automationsRoot()
	binary, err := os.Executable()
	if err != nil || !automation.Startable(binary) {
		// A process that cannot start a clock still counts as an open window:
		// another window's clock runs while it is here.
		release, err := automation.Keep(root, label, nil)
		if err != nil {
			return func() {}
		}
		return release
	}
	release, err := automation.Keep(root, label, func() error {
		return automation.StartClock(root, binary, "clock")
	})
	if err != nil {
		return func() {}
	}
	return release
}

// runClock is `codeaf clock`.
func runClock(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("codeaf clock takes no arguments")
	}
	root := automationsRoot()
	if automation.Held(root) {
		return nil
	}
	store, err := automation.Open(root)
	if err != nil {
		return err
	}
	defer store.Close()
	proc, stop, err := openV3ClockProcess()
	if err != nil {
		return err
	}
	defer stop()
	logf := clockLogger(root)
	runner := session.NewAutomationRunner(func(workspace string) (session.Config, error) {
		// THE PERSON'S OWN ASSEMBLY, read the way their conversations read it:
		// Interactive is what picks their own approval rules rather than the
		// headless default. The runner then takes the person away — nobody is
		// there to ask — and nothing it does may arm another automation.
		cfg, _, _, err := v3ConfigFor(proc, v3Options{Interactive: true}, workspace, session.Place{}, "", nil)
		return cfg, err
	}, store.Root())
	clock := &automation.Clock{Store: store, Presence: automation.NewPresence(root), Runner: runner, Log: logf}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, retire := retireWhenReplaced(ctx, store, logf)
	defer retire()
	err = clock.Run(ctx)
	if errors.Is(err, automation.ErrHeld) {
		return nil
	}
	return err
}

// openV3ClockProcess is the clock's process: the person's settings, models,
// connected accounts, memory and harnesses — what a run's configuration is
// assembled from — and none of the launch's side effects. It returns its own
// release.
//
// A MACHINE WITH NO KEY STILL HAS A CLOCK. A reminder needs no model, so the
// clock runs keyless; a watch or a piece of work then says plainly that it has
// no key, on its own row.
func openV3ClockProcess() (*v3Process, func(), error) {
	settings, err := config.Load()
	if err != nil {
		keyless, keylessErr := config.LoadKeyless()
		if keylessErr != nil {
			return nil, nil, err
		}
		settings = keyless
	}
	workspace, err := os.UserHomeDir()
	if err != nil || workspace == "" {
		workspace = os.TempDir()
	}
	discovery := catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
		HTTPClient: config.CatalogHTTPClient(settings.Sources.Default()),
	}
	processCtx, processStop := context.WithCancel(context.Background())
	models := catalog.LoadLazy(processCtx, discovery)
	seatCrewRows(models.ModelsNow)
	shelf := newV3ModelShelf(models, discovery)
	shelf.setSources(settings.Sources)
	memory := v3Memory(settings.ProfileDir)
	var search *store.Store
	if memory != nil {
		search = memory
	} else {
		search = v3SearchStore(settings.ProfileDir)
	}
	proc := &v3Process{
		Settings:          settings,
		ProfileDir:        settings.ProfileDir,
		UnreadProfileKeys: append([]string(nil), settings.UnreadProfileKeys...),
		Models:            models,
		processCtx:        processCtx,
		processStop:       processStop,
		Shelf:             shelf,
		Harnesses:         subharness.Default(),
		Memory:            memory,
		Search:            search,
		Artifacts:         artifactsIndexPath(),
		Conns:             v3Connect(settings.ProfileDir),
		LaunchDir:         workspace,
	}
	proc.Skills, proc.skillsDir = v3SkillShelf(proc.Memory)
	stop := func() {
		processStop()
		if memory != nil {
			_ = memory.Close()
		}
		if search != nil && search != memory {
			_ = search.Close()
		}
	}
	return proc, stop, nil
}

// retireWhenReplaced ends the clock when its own binary has been replaced and
// nothing is running, so the next window's check starts the new build. A clock
// in the middle of a run finishes it first.
func retireWhenReplaced(parent context.Context, store *automation.Store, logf func(string)) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	binary, err := os.Executable()
	if err != nil {
		return ctx, cancel
	}
	started, err := os.Stat(binary)
	if err != nil {
		return ctx, cancel
	}
	guard.Go("automations/retire", func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			now, err := os.Stat(binary)
			if err == nil && os.SameFile(started, now) && now.ModTime().Equal(started.ModTime()) {
				continue
			}
			if active, err := store.Active(); err == nil && len(active) > 0 {
				continue
			}
			logf("this build was replaced; leaving so the new one takes over")
			cancel()
			return
		}
	})
	return ctx, cancel
}

// clockLogger appends the clock's own lines to clock.log under root.
func clockLogger(root string) func(string) {
	var mu sync.Mutex
	path := filepath.Join(root, automation.LogName)
	return func(line string) {
		mu.Lock()
		defer mu.Unlock()
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer file.Close()
		_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), line)
	}
}
