package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appPluginAttempt struct {
	cancel context.CancelFunc
	flow   *connect.Flow
}

func (b *appBackend) pluginManager() (*connect.Manager, error) {
	m := v3Connect(b.crewProfile())
	if m == nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "connections could not be read")
	}
	return m, nil
}
func (b *appBackend) ListPlugins() (appwire.PluginsResult, error) {
	m, err := b.pluginManager()
	if err != nil {
		return appwire.PluginsResult{}, err
	}
	providers, err := b.ListProviders()
	if err != nil {
		return appwire.PluginsResult{}, err
	}
	if providers == nil {
		providers = []appwire.Provider{}
	}
	views := make([]appwire.PluginProvider, 0, len(providers))
	for _, provider := range providers {
		view := appwire.PluginProvider{Provider: provider}
		b.pluginProviderFacts(&view)
		views = append(views, view)
	}
	featured, words := connect.Featured()
	return appwire.PluginsResult{Plugins: m.Plugins(), Providers: views, Featured: featured, FeaturedWords: words}, nil
}
func (b *appBackend) DescribePlugin(id string) (connect.PluginDescription, error) {
	m, err := b.pluginManager()
	if err != nil {
		return connect.PluginDescription{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := m.DescribePlugin(ctx, id)
	if err != nil {
		return d, appEngineError(err)
	}
	return d, nil
}
func (b *appBackend) ConnectPluginBrowser(p appwire.PluginBrowserParams) (appwire.PluginBrowserResult, error) {
	m, err := b.pluginManager()
	if err != nil {
		return appwire.PluginBrowserResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), appSignInWait)
	attempt := &appPluginAttempt{cancel: cancel}
	var refused error
	b.locked(func() {
		if b.closing {
			refused = appwire.Errorf(appwire.CodeEngine, "the engine is closing")
			return
		}
		if b.pluginAuth == nil {
			b.pluginAuth = map[string]*appPluginAttempt{}
		}
		if b.pluginAuth[p.ID] != nil {
			refused = appwire.Errorf(appwire.CodeBusy, "a sign-in is already waiting")
			return
		}
		b.pluginAuth[p.ID] = attempt
	})
	if refused != nil {
		cancel()
		return appwire.PluginBrowserResult{}, refused
	}
	flow, err := m.BeginAuth(ctx, p.ID, p.Answer)
	if err != nil {
		cancel()
		b.locked(func() {
			if b.pluginAuth[p.ID] == attempt {
				delete(b.pluginAuth, p.ID)
			}
		})
		return appwire.PluginBrowserResult{}, appEngineError(err)
	}
	active := false
	b.locked(func() {
		if b.pluginAuth[p.ID] == attempt {
			attempt.flow = flow
			active = true
		}
	})
	if !active {
		flow.Cancel()
		cancel()
		return appwire.PluginBrowserResult{}, appwire.Errorf(appwire.CodeEngine, "sign-in was cancelled")
	}
	guard.Go("app/plugin-sign-in", func() {
		defer cancel()
		defer flow.Cancel()
		status, waitErr := flow.Wait(ctx)
		current := false
		b.locked(func() {
			if b.pluginAuth[p.ID] == attempt {
				current = true
				delete(b.pluginAuth, p.ID)
			}
		})
		if !current {
			return
		}
		changed := appwire.PluginsChanged{ID: p.ID}
		if waitErr != nil {
			changed.Error = connect.SignInFailureReason(waitErr)
		} else {
			b.notePluginConnected(p.ID, status.Account)
		}
		b.emit.Emit(appwire.EventPluginsChanged, changed)
	})
	// The surface opens this link, as it does for providers.sign_in_start.
	return appwire.PluginBrowserResult{State: "waiting", Link: flow.URL()}, nil
}
func (b *appBackend) CancelPlugin(id string) error {
	var attempt *appPluginAttempt
	b.locked(func() {
		attempt = b.pluginAuth[id]
		delete(b.pluginAuth, id)
	})
	if attempt != nil {
		attempt.cancel()
		if attempt.flow != nil {
			attempt.flow.Cancel()
		}
	}
	return nil
}
func (b *appBackend) ConnectPluginKey(p appwire.PluginKeyParams) (appwire.PluginKeyResult, error) {
	m, err := b.pluginManager()
	if err != nil {
		return appwire.PluginKeyResult{}, err
	}
	key := strings.TrimSpace(p.Key)
	if p.KeyEnv != "" {
		name := strings.TrimPrefix(strings.TrimSpace(p.KeyEnv), "$")
		if !connect.ValidKeyEnv(name) {
			return appwire.PluginKeyResult{Error: "shape", Words: "Name an environment variable such as STRIPE_KEY"}, nil
		}
		key = "$" + name
	}
	if p.Answer != "" {
		key = strings.TrimSpace(p.Answer) + " " + key
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	status, err := m.ConnectKey(ctx, p.ID, key)
	if err != nil {
		var failure *connect.KeyFailure
		if errors.As(err, &failure) {
			return appwire.PluginKeyResult{Error: failure.Kind, Words: failure.Words}, nil
		}
		return appwire.PluginKeyResult{}, appEngineError(err)
	}
	b.notePluginConnected(p.ID, status.Account)
	b.emit.Emit(appwire.EventPluginsChanged, appwire.PluginsChanged{ID: p.ID})
	return appwire.PluginKeyResult{Account: status.Account, Checked: status.Checked}, nil
}
func (b *appBackend) SetPluginCapability(p appwire.PluginCapabilityParams) error {
	m, err := b.pluginManager()
	if err != nil {
		return err
	}
	if err = m.SetCapabilityState(p.ID, p.Capability, p.State); err != nil {
		return appEngineError(err)
	}
	b.emit.Emit(appwire.EventPluginsChanged, appwire.PluginsChanged{ID: p.ID})
	return nil
}
func (b *appBackend) DisconnectPlugin(p appwire.PluginDisconnectParams) error {
	m, err := b.pluginManager()
	if err != nil {
		return err
	}
	if p.Account != "" {
		found := false
		for _, s := range m.Services() {
			if s.ID == p.ID && s.Account == p.Account {
				found = true
			}
		}
		if !found {
			return appwire.Errorf(appwire.CodeNotFound, "that account is not connected")
		}
	}
	_ = b.CancelPlugin(p.ID)
	if err = m.Disconnect(p.ID); err != nil {
		return appEngineError(err)
	}
	b.emit.Emit(appwire.EventPluginsChanged, appwire.PluginsChanged{ID: p.ID})
	return nil
}
func (b *appBackend) notePluginConnected(id, account string) {
	var doors []interface{ NoteConnected(string, string) }
	b.locked(func() {
		for _, a := range b.agents {
			if a.live != nil {
				if door, ok := a.live.conn.Conv.(interface{ NoteConnected(string, string) }); ok {
					doors = append(doors, door)
				}
			}
		}
	})
	for _, door := range doors {
		door.NoteConnected(id, account)
	}
}

// setPlanPaused changes the same source rows used by the terminal's provider settings.
func (b *appBackend) setPlanPaused(policies map[string]string) error {
	rows := config.PersistedSources(b.crewProfile())
	for id, policy := range policies {
		if policy != config.PlanPausedWait && policy != config.PlanPausedUseMeter {
			return appwire.Errorf(appwire.CodeBadParams, "when_plan_paused is wait or use pay-as-you-go")
		}
		found := false
		for _, row := range rows {
			if row.ID == id {
				found = true
			}
		}
		if !found {
			return appwire.Errorf(appwire.CodeNotFound, "that provider is not connected")
		}
	}
	err := config.UpdateSources(b.crewProfile(), func(rows []config.PersistedSource) []config.PersistedSource {
		for i := range rows {
			if policy, ok := policies[rows[i].ID]; ok {
				rows[i].PlanPaused = policy
			}
		}
		return rows
	})
	if err != nil {
		return appEngineError(err)
	}
	return nil
}

// connectedPluginQuestion accepts a completed connection from either chat or Inbox.
func (b *appBackend) connectedPluginQuestion(q session.Question) session.Question {
	if q.Kind != session.QuestionConnect || q.Subject.Name == "" {
		return q
	}
	if m := v3Connect(b.crewProfile()); m != nil && m.Connected(q.Subject.Name) {
		q.Options = session.AnswerOptions(session.QuestionConnect)
		q.Input = session.InputShape{}
	}
	return q
}
