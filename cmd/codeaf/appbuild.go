package main

import (
	"context"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

const appBuildInboxAsk = "app-build"

type appBuildState struct {
	look   appLookState
	mu     sync.Mutex
	jobs   *builder.Jobs
	closed bool
}

// AppBuilds is shared by the wire and the app assistant's change handoff.
func (b *appBackend) AppBuilds() (*builder.Jobs, error) {
	door := &b.builds
	door.mu.Lock()
	defer door.mu.Unlock()
	if door.closed {
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	if door.jobs != nil {
		return door.jobs, nil
	}
	pipeline := check.Pipeline{Seed: b.seedBuildSample, DryRun: b.CheckDraftRun, Look: b.checkAppLook}
	jobs, err := builder.OpenJobs(builder.Options{
		Home: home.Dir(), Store: b.AppDefinitions(), Model: appBuilderModel,
		Check: pipeline.Run, Changed: b.appBuildChanged, Notify: b.appBuildNotice,
		Proposed: b.appAssistantProposed,
		Catalog:  b.appBuildCatalog, SeedPreview: b.seedBuildPreview,
		ClosePreview: b.closeBuildPreview,
		Compiler:     b, ValidateTypes: validateAppBuilderTypes,
		CanLook: b.builds.look.ready.Load,
		Look: func(ctx context.Context, draft, screen string) ([]string, error) {
			page, err := b.lookApp(ctx, appwire.AppLookParams{Draft: draft, Screen: screen})
			return page.Problems, err
		},
	})
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "App editing history could not be opened.")
	}
	door.jobs = jobs
	return jobs, nil
}

func (b *appBackend) appAssistantProposed(proposal builder.Proposal) {
	select {
	case <-b.left:
		return
	default:
	}
	kind, summary := "big", "Open the builder to review the change."
	if proposal.Screen != "" {
		kind, summary = "small", proposal.Summary
	}
	b.emit.Emit(appwire.EventAppChangeProposed, appwire.AppChangeProposed{
		App: proposal.App, Draft: proposal.Draft, Kind: kind, Screen: proposal.Screen,
		SummaryLines: []string{summary}, ProposalID: proposal.ID,
	})
}

func (b *appBackend) closeAppBuilds() {
	if jobs := b.builds.takeJobs(); jobs != nil {
		jobs.Close()
	}
}

func (door *appBuildState) takeJobs() *builder.Jobs {
	door.mu.Lock()
	defer door.mu.Unlock()
	door.closed = true
	return door.jobs
}

// The worker seat and credentials come from the same profile as other
// headless model calls. An explicit builder model is handy for acceptance.
func appBuilderModel() (builder.Completer, error) {
	settings, err := config.Load()
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(env.Get("CODEAF_APPS_BUILDER_MODEL"))
	if model == "" {
		model = config.TierModelAt(settings.ProfileDir, config.ModelTierWorker)
	}
	client, err := provider.NewClient(settings.ClientConfig(model))
	if err != nil {
		return nil, err
	}
	return builderModelClient{client: client, model: model}, nil
}

type builderModelClient struct {
	client *provider.Client
	model  string
}

func (m builderModelClient) ModelID() string { return m.model }

func (m builderModelClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	ctx = provider.WithRole(ctx, lane.RoleLeafUnattended)
	ctx = provider.WithCallTag(ctx, "app builder")
	return m.client.CompleteWithMessages(ctx, messages, opts...)
}

func (b *appBackend) appBuildChanged(event builder.Event) {
	b.emit.Emit(appwire.EventAppsBuild, event)
	if event.Stage != builder.NeedsYou && (event.Stage != builder.Ready || event.OK == nil) {
		id := appInboxID(appwire.AppDraftInboxPrefix+event.Draft, appBuildInboxAsk)
		b.inboxResolved(id, appwire.Entry{})
	}
}

func (b *appBackend) appBuildNotice(item builder.Summary) {
	title, detail := item.Name+" is ready to try", "Open the app to try it and keep it."
	if item.State == builder.NeedsYou {
		title, detail = item.Name+" needs you", "Open the app to choose what to change."
	}
	b.inboxOpened(appwire.Ask{
		ID: appBuildInboxAsk, Agent: appwire.AppDraftInboxPrefix + item.Draft, Kind: appwire.AskQuestion,
		Title: title, Detail: detail, Path: []string{"Apps", item.Name},
	})
}
