package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
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
// (internal/factory/triage).
const factoryTriageEvery = 2 * time.Second

// factoryTriage is the one triage worker this process runs, started at most
// once.
var factoryTriage sync.Once

// startFactoryTriage starts this process's cheap read of new items over st,
// beside the GitHub poll and under the same rule: ONLY THE PROCESS THAT OPENED
// THE FLOOR FOR A PERSON'S WINDOW ([startFactoryPoll] calls it), ONE PER
// PROCESS, STOPPED WITH THE PROCESS, and a launch never waits on it.
//
// A CAPABILITY WITH NOTHING BEHIND IT IS ABSENT. Until a key resolves there is
// no worker at all: a small loop ([waitForFactoryTriage]) looks again every
// [factoryWatchEvery], so a key pasted into the first-run setup starts the
// reads without a relaunch, and a machine with no key never lists the floor
// for triage. The same loop takes the floor's triage lock and the worker holds
// it until this process ends, so two processes on one machine (two windows, or
// a window and its engine) never pay twice for one item: the one that lost
// looks again every [factoryWatchEvery] and takes over when the holder exits.
func startFactoryTriage(st *store.Store) {
	if st == nil {
		return
	}
	factoryTriage.Do(func() {
		guard.Go("factory/triage", func() {
			ctx := context.Background()
			call, release := waitForFactoryTriage(ctx, st, config.Load, factoryWatchEvery)
			if call == nil {
				return
			}
			defer release()
			// NOTHING IS LOGGED TO THE SCREEN, for the poll's reason: the
			// surface owns it in a window and stdout is the protocol in an
			// engine. An item that could not be read simply has no read.
			triage.Worker(ctx, st, call, factoryTriageEvery, nil)
		})
	})
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
// model, exactly as every other low-tier errand does.
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
		model := factoryTriageModel(settings)
		if model == "" {
			return "", errors.New("no model for the small-work seat")
		}
		client, err := provider.NewClient(settings.ClientConfig(model))
		if err != nil {
			return "", err
		}
		ctx = provider.WithoutStream(provider.WithCallTag(ctx, factoryTriageName))
		response, err := client.CompleteWithMessages(ctx, []ai.Message{judgeMessage("user", prompt)})
		if err != nil {
			return "", err
		}
		if usd := recordFactoryTriageUsage(settings, model, response); usd > 0 && noted != nil {
			noted(usd)
		}
		if response == nil || strings.TrimSpace(response.Text()) == "" {
			return "", errors.New("the model answered nothing")
		}
		return response.Text(), nil
	}, true
}

// factoryTriageModel is the small-work seat's model for this profile, or the
// conversation's when that row was cleared.
func factoryTriageModel(settings config.Config) string {
	if m := strings.TrimSpace(config.TierSeatAt(settings.ProfileDir, config.ModelTierLow).Model); m != "" {
		return m
	}
	return strings.TrimSpace(settings.Model)
}

// recordFactoryTriageUsage puts one read's cost on this machine's spend
// ledger: the role column says `factory triage`, the seat is the low one it
// ran on. The dollars are the provider's own receipt when one arrived,
// otherwise the model's published price over the tokens, and a call nobody
// priced writes no row ([session.RecordUsage] refuses an all-zero line).
//
// It answers the dollars it wrote, 0 when none were known.
func recordFactoryTriageUsage(settings config.Config, model string, response *ai.Response) float64 {
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
	session.RecordUsage(session.UsageLedgerPath(), session.TagUsage(line, roles.Role(factoryTriageName), session.SeatLow))
	return line.USD
}
