package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryTriageName is what the read's cost is called on the spend ledger, in
// the role column every errand names itself in. The manual quotes it
// (internal/manual/chat/factory.md), because "what is this charge" is asked.
const factoryTriageName = "factory triage"

// factoryTriageEvery is the worker's tick: one item read every two seconds
// while items are waiting, and IdleTicks times that while none are
// (internal/factory/triage). It is a variable so a test can shorten it.
var factoryTriageEvery = 2 * time.Second

// factoryTriage is the one triage worker this process runs, started at most
// once.
var factoryTriage sync.Once

// factoryTriageStop cancels and joins the worker before a test replaces its home.
// The caller's context still owns the worker in production.
var factoryTriageStop func()

// startFactoryTriage starts this process's cheap read of new items over st,
// beside the GitHub poll and under the same rule: ONLY THE PROCESS THAT OPENED
// THE FLOOR FOR A PERSON'S WINDOW ([startFactoryPoll] calls it) OR A FACTORY
// SERVER, ONE PER PROCESS, STOPPED WITH ITS CONTEXT, and a launch never waits on it.
//
// A CAPABILITY WITH NOTHING BEHIND IT IS ABSENT. Until a key resolves there is
// no worker at all: a small loop ([waitForFactoryTriage]) looks again every
// [factoryWatchEvery], so a key pasted into the first-run setup starts the
// reads without a relaunch, and a machine with no key never lists the floor
// for triage. The same loop takes the floor's triage lock and the worker holds
// it until this process ends, so two processes on one machine (two windows, or
// a window and its engine) never pay twice for one item: the one that lost
// looks again every [factoryWatchEvery] and takes over when the holder exits.
func startFactoryTriage(ctx context.Context, st *store.Store, newIntake ...func(*store.Store) *intake.Intake) {
	if st == nil {
		return
	}
	build := factoryIntake
	if len(newIntake) > 0 && newIntake[0] != nil {
		build = newIntake[0]
	}
	factoryTriage.Do(func() {
		ctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		factoryTriageStop = func() { cancel(); <-done }
		guard.Go("factory/triage", func() {
			defer close(done)
			runFactoryTriage(ctx, st, build)
		})
	})
}

// runFactoryTriage is the body of one triage worker, returning when ctx ends
// or no key ever resolves. It is the one loop the terminal's process-wide
// start ([startFactoryTriage]) and the app's per-host start
// ([appFactoryHost.readLoops]) both run, so the two cannot drift apart; it
// carries NO once of its own, and who may call it, and how often, is the
// caller's law. newIntake builds the floor's intake ([factoryIntake] unless a
// host brings its own), which asks each read what the repository's start
// rules make of it.
func runFactoryTriage(ctx context.Context, st *store.Store, newIntake func(*store.Store) *intake.Intake) {
	call, release := waitForFactoryTriage(ctx, st, config.Load, factoryWatchEvery)
	if call == nil {
		return
	}
	defer release()
	// NOTHING IS LOGGED TO THE SCREEN, for the poll's reason: the
	// surface owns it in a window and stdout is the protocol in an
	// engine. An item that could not be read simply has no read.
	in := newIntake(st)
	triage.Worker(ctx, st, call, factoryTriageEvery, nil, triage.Options{Standing: factorygithub.Rechecker(func(ctx context.Context) string {
		token, _ := factorygithub.TokenAt(ctx, factoryProfile(""))
		return token
	}),
		AskFirst: in.AskFirst, AfterTick: in.Once,
		Off: func() bool { return !factoryReadsOnArrival(st) },
	})
}

// factoryReadsOnArrival is the floor's "Read new issues as they arrive",
// read each time so a change on the settings page takes effect on the next
// tick. An unreadable settings file reads as the default, off.
func factoryReadsOnArrival(st *store.Store) bool {
	settings, _ := st.RunnerSettings()
	return settings.ReadOnArrival
}

// waitForFactoryTriage answers the worker's call and the triage lock's
// release once a key resolves and the lock is free, looking again every
// `every`, and nil only when ctx ended first.
func waitForFactoryTriage(ctx context.Context, st *store.Store, load func() (config.Config, error), every time.Duration) (triage.Call, func()) {
	for {
		// EACH READ'S PRICE IS NOTED ON THE FLOOR TOO (store.NoteReadCost), the
		// same dollars it puts on the ledger, so the floor can quote the last
		// read and estimate a whole-floor refresh without scanning the ledger.
		if call, ok := factoryTriageCallNoting(load, func(usd float64) { _ = st.NoteReadCost(usd) }); ok {
			if release, held := st.TryTriager(); held {
				return call, release
			}
		}
		t := time.NewTimer(every)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, nil
		case <-t.C:
		}
	}
}

// factoryTriageCall builds the worker's one-prompt call, and false when no key
// resolves for this profile.
//
// THE CHEAPEST SEAT THE CREW OFFERS: the `small work` row ([config.TierSeatAt]
// on the low tier), the seat a title, a caption and a task's name ride. A row
// the person cleared on purpose reads empty and follows the conversation's
// model, exactly as every other low-tier errand does. Under --one-model the
// launch's pin replaces that seat, including on a server with no conversation.
//
// THE SETTINGS ARE READ WHEN THE WORKER ASKS, NOT WHEN IT IS BUILT, as the
// pool's judge reads them (poolrecord.go): a key changed in /settings is the
// key the next read carries. Every call puts one row on the spend ledger under
// [factoryTriageName], on the low seat.
func factoryTriageCall(load func() (config.Config, error)) (triage.Call, bool) {
	return factoryTriageCallNoting(load, nil)
}

// factoryTriageCallNoting is [factoryTriageCall] that also hands each priced
// read's dollars to noted, when noted is not nil.
func factoryTriageCallNoting(load func() (config.Config, error), noted func(usd float64)) (triage.Call, bool) {
	if load == nil {
		return nil, false
	}
	if _, err := load(); err != nil {
		return nil, false
	}
	return func(ctx context.Context, prompt string) (string, error) {
		settings, err := load()
		if err != nil {
			return "", err
		}
		model, err := factoryTriageModel(settings)
		if err != nil {
			return "", err
		}
		if model == "" {
			return "", errors.New("no model for the small-work seat")
		}
		clientConfig := settings.ClientConfig(model)
		clientConfig.DropRefusedEffort = true
		client, err := provider.NewClient(clientConfig)
		if err != nil {
			return "", err
		}
		ctx = provider.WithoutStream(provider.WithCallTag(ctx, factoryTriageName))
		// The read asks at temperature 0, so the same issue read twice lands
		// in the same band.
		ctx, recorded := factoryAIAccounting(ctx, settings, model, factoryTriageName, noted)
		response, err := client.CompleteWithMessages(ctx, []ai.Message{judgeMessage("user", prompt)}, ai.WithTemperature(0))
		recorded(response)
		if err != nil {
			return "", err
		}
		if response == nil || strings.TrimSpace(response.Text()) == "" {
			return "", errors.New("the model answered nothing")
		}
		return response.Text(), nil
	}, true
}

// factoryTriageModel is the small-work seat's model for this profile, or the
// conversation's when that row was cleared. The launch's one-model pin wins
// before any profile row, including when the server has no conversation.
//
// A profile whose Task models are ONE MODEL reads the small-work seat from the
// same policy the manager and stages do (factory_taskmodels.go): the same
// route, and a refusal when it cannot be met — never the low tier's model.
func factoryTriageModel(settings config.Config) (string, error) {
	return factoryRoleModel(settings, roles.RoleInsightFactoryRead)
}

// factoryRoleModel is the model one factory AI call runs on: the launch's or
// the profile's one model when there is one, else the call's page-AI role
// (insight_factory_read or insight_factory_scout, then the insight family's
// pin and the engine's default for it), else the small-work seat.
func factoryRoleModel(settings config.Config, role roles.Role) (string, error) {
	if parent := factoryWorkerParent(); parent.OneModel {
		return strings.TrimSpace(parent.Model), nil
	}
	if send, one, err := factoryTaskOneModel(settings.ProfileDir, settings.Sources); err != nil {
		return "", err
	} else if one {
		return send, nil
	}
	if source, err := v3RolesSource("", settings.ProfileDir); err == nil {
		if m, err := roles.Resolve(source, role, ""); err == nil && strings.TrimSpace(m) != "" {
			return strings.TrimSpace(m), nil
		}
	}
	if m := strings.TrimSpace(config.TierSeatAt(settings.ProfileDir, config.ModelTierLow).Model); m != "" {
		return m, nil
	}
	return strings.TrimSpace(settings.Model), nil
}

// recordFactoryAIUsage puts one factory AI call's cost on the spend ledger
// under name in the role column, on the low seat, and answers its dollars.
func recordFactoryAIUsage(settings config.Config, model, name string, response *ai.Response) float64 {
	line := session.UsageLine{Model: model, Calls: 1}
	if response != nil && response.Usage != nil {
		used := response.Usage
		line.Input, line.Output = used.PromptTokens, used.CompletionTokens
		if used.Cost != nil {
			line.USD = *used.Cost
		} else if prompt, completion, known := settings.ClientConfig(model).ModelPrice(model); known {
			line.USD = float64(used.PromptTokens)*prompt + float64(used.CompletionTokens)*completion
		}
	}
	session.RecordUsage(session.UsageLedgerPath(), session.TagUsage(line, roles.Role(name), session.SeatLow))
	return line.USD
}

// factoryAIAccounting collects provider charges even when a failed generation
// returns no response. Its finisher banks them before the caller judges the
// error. The returned usage keeps its price-presence fact on successful calls.
func factoryAIAccounting(ctx context.Context, settings config.Config, model, name string, noted func(float64)) (context.Context, func(*ai.Response) float64) {
	var mu sync.Mutex
	var charges []provider.Billed
	ctx = provider.WithBilling(ctx, func(charge provider.Billed) {
		mu.Lock()
		defer mu.Unlock()
		charges = append(charges, charge)
	})
	return ctx, func(response *ai.Response) float64 {
		mu.Lock()
		defer mu.Unlock()
		total := 0.0
		bank := func(served string, response *ai.Response) {
			usd := recordFactoryAIUsage(settings, served, name, response)
			total += usd
			if usd > 0 && noted != nil {
				noted(usd)
			}
		}
		for i, charge := range charges {
			served := charge.Model
			if served == "" {
				served = model
			}
			if i == len(charges)-1 && response != nil {
				bank(served, response)
				continue
			}
			used := &ai.Usage{PromptTokens: charge.PromptTokens, CompletionTokens: charge.CompletionTokens}
			if charge.Cost != 0 {
				used.Cost = &charge.Cost
			}
			bank(served, &ai.Response{Usage: used})
		}
		if len(charges) == 0 && response != nil {
			bank(model, response)
		}
		return total
	}
}
