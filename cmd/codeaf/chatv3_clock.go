package main

// chatv3_clock.go is the process side of automations (internal/automation;
// docs/design/automations/DESIGN.md): `codeaf clock`, the one process that runs
// them while a codeaf window is open, and the window's half — holding presence
// for as long as it is open and starting a clock when none is running.
//
// AND THE TWO SEAMS A WINDOW READS AUTOMATIONS THROUGH, side by side: this
// machine's store read directly ([v3AutomationsSeam]), and another machine's
// read over a connection ([hostAutomationsSeam]) — whose clock is kept running
// by the engine there, holding presence for the attached window
// ([engineAutomations] in engine.go).
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
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/subharness"
	"github.com/Agent-Field/codeaf/internal/tui3"
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

// v3AutomationsSeam is [tui3.Options.Automations] for a window on the machine
// its automations run on: the store itself, read and written directly, and the
// presence folder the clock counts windows from. A store that cannot be opened
// is the zero seam, which the surface reads as automations absent.
func v3AutomationsSeam() tui3.AutomationsSeam {
	autos := v3Automations()
	if autos == nil || autos.Store == nil {
		return tui3.AutomationsSeam{}
	}
	store := autos.Store
	presence := automation.NewPresence(store.Root())
	return tui3.AutomationsSeam{
		List:      store.List,
		Runs:      store.Runs,
		Changes:   store.Changes,
		Cursor:    store.Cursor,
		Active:    store.Active,
		Windows:   presence.Count,
		Create:    store.Create,
		Update:    store.Update,
		SetStatus: store.SetStatus,
		Delete:    store.Delete,
		RunNow: func(id string) error {
			_, err := store.QueueNow(id)
			return err
		},
		StopRun: store.RequestStop,
		Claim: func(id int64) bool {
			first, err := store.Claim(id)
			return err == nil && first
		},
		Zone: autos.Zone,
	}
}

// hostAutomationsSeam is [tui3.Options.Automations] for a window on ANOTHER
// machine — over --host or --at — and it is [v3AutomationsSeam] function for
// function, every one of them a call to the engine instead of a read of this
// disk (internal/remote's wire_automations.go). The conversation over there
// proposes into that machine's store and that machine's clock runs what it
// keeps, so the far store is the one this window reads, and the zone a typed
// rhythm is read in is the far machine's, as its welcome said.
//
// AN ENGINE THAT DOES NOT SAY IT HAS THE DOORS GETS THE ZERO SEAM, which the
// surface reads as automations absent over this connection — no list, no run
// lines, no notifications. It never gets this laptop's store instead: those
// automations run on a different machine, beside conversations this window
// cannot see.
func hostAutomationsSeam(client *remote.Client, welcome remote.Welcome) tui3.AutomationsSeam {
	if client == nil || !welcome.Automations {
		return tui3.AutomationsSeam{}
	}
	return tui3.AutomationsSeam{
		List:      client.AutomationsList,
		Runs:      client.AutomationsRuns,
		Changes:   client.AutomationsChanges,
		Cursor:    client.AutomationsCursor,
		Active:    client.AutomationsActive,
		Windows:   client.AutomationsWindows,
		Create:    client.AutomationsCreate,
		Update:    client.AutomationsUpdate,
		SetStatus: client.AutomationsSetStatus,
		Delete:    client.AutomationsDelete,
		RunNow:    client.AutomationsRunNow,
		StopRun:   client.AutomationsStopRun,
		// A CLAIM THAT DID NOT ANSWER IS NOT THIS WINDOW'S, the same reading the
		// local seam takes of a store that failed: one missed banner is better
		// than the same banner raised by every window that could not be sure.
		Claim: func(id int64) bool {
			first, err := client.AutomationsClaim(id)
			return err == nil && first
		},
		Zone: welcome.AutomationsZone,
	}
}

// keepAutomationsWindow registers this process as an open window and keeps a
// clock running while it is. The release is the window closing.
//
// IT IS ALSO WHAT AN ENGINE HOLDS FOR A WINDOW ATTACHED FROM ANOTHER MACHINE
// ([engineAutomations]), once per connection whose hello says it is a window,
// released when that connection ends. One door for both kinds of window is what
// keeps them one kind of thing to the clock that counts them.
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
	clock.Tidy = clockMemoryTidy(proc, store.Root())

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

// clockMemoryTidy is the pass over what is remembered that the clock asks for
// while a window is open (internal/session's memory_consolidate.go), or nil
// when memory is off or the person's assembly cannot be read — a capability
// that cannot work is absent.
//
// IT RIDES THE CLOCK because it wants exactly what the clock already has: one
// process elected among every window, and a machine where nobody is typing,
// which the pass asks for itself ([session.MachineIdle]). It is handed the
// store's PATH rather than an open store, so the clock does not hold a database
// connection between passes.
//
// THE PASS HAS NO PROJECT. It is resolved against the person's home and runs
// over what they and this machine remember, never over one project's lines,
// because nobody is in a project while it runs.
func clockMemoryTidy(proc *v3Process, root string) automation.Tidy {
	brain := v3MemoryPath(proc.ProfileDir)
	if brain == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	parent, _, _, err := v3ConfigFor(proc, v3Options{Interactive: true}, home, session.Place{}, "", nil)
	if err != nil {
		return nil
	}
	parent.MemoryProjectKey = ""
	return session.NewMemoryTidy(parent, brain, root, session.MachineIdle())
}

// v3MemoryPath is the brain's file when the memory row is on, and the empty
// string when it is off.
//
// IT READS THE SAME ROW [v3Memory] READS and answers a path rather than a
// handle, which is the difference between the conversation's need and a
// pass's: a window opens one store and keeps it for the session, while a pass
// wants one a few times a day and wants it closed again afterwards.
func v3MemoryPath(profileDir string) string {
	if !config.MemoryEnabledAt(profileDir) {
		return ""
	}
	return defaultChatDB()
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
