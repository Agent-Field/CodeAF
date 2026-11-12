package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appPluginPause struct {
	reset string
	until time.Time
}
type appPluginOllama struct {
	address string
	checked time.Time
	probing bool
	running *bool
}

func (b *appBackend) pluginProviderFacts(view *appwire.PluginProvider) {
	profile := b.crewProfile()
	rows := config.PersistedSources(profile)
	sources := config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
	handed := false
	if credentials := config.AppCredentialSource(); credentials != nil {
		_, handed = credentials.Key(view.ID)
	}
	if view.ID == modelsource.DefaultID {
		switch config.APIKeySourceAt(profile) {
		case config.APIKeySourceOpenRouter:
			view.KeyEnv = "OPENROUTER_API_KEY"
		case config.APIKeySourceOpenAI:
			view.KeyEnv = "OPENAI_API_KEY"
		}
	} else if service, ok := sources.ByID(view.ID); ok && !handed {
		// Match the credential precedence used by config.sourceKeyFromRow.
		if name := strings.TrimSpace(service.Source.KeyEnv); name != "" && strings.TrimSpace(env.Value(name)) != "" {
			view.KeyEnv = name
		} else {
			for _, row := range rows {
				if row.ID == view.ID && strings.TrimSpace(row.Key) == "" && strings.TrimSpace(env.Value(row.KeyEnv)) != "" {
					view.KeyEnv = row.KeyEnv
				}
			}
		}
	}
	if view.ID == "codex" {
		if tokens, err := codexauth.Load(profile); err == nil {
			view.Account = tokens.Email
		}
	}
	b.locked(func() {
		if pause, ok := b.pluginPauses[view.ID]; ok {
			if !pause.until.IsZero() && !time.Now().Before(pause.until) {
				delete(b.pluginPauses, view.ID)
			} else {
				view.PlanPaused = &appwire.PluginPlanPaused{ResetsAt: pause.reset}
			}
		}
	})
	if view.ID == "ollama" {
		b.pluginOllamaFacts(view)
	}
}

// pluginPlanPhase retains the same refusal the terminal reports, without parsing its display sentence.
func (b *appBackend) pluginPlanPhase(news session.PhaseNews) {
	if news.SourceID == "" {
		return
	}
	b.locked(func() {
		if news.Phase == provider.PhasePlanPaused {
			if b.pluginPauses == nil {
				b.pluginPauses = map[string]appPluginPause{}
			}
			_, until := provider.RouteFailureOf(&provider.PlanPauseError{Reset: news.PlanReset})
			b.pluginPauses[news.SourceID] = appPluginPause{reset: news.PlanReset, until: until}
		} else if (news.Phase == provider.PhaseWriting || news.Phase == provider.PhaseThinking) && news.Door != "" && news.Door != "pay-as-you-go" {
			delete(b.pluginPauses, news.SourceID)
		}
	})
}

// pluginOllamaFacts returns the last local probe; network I/O never holds up plugins.list.
func (b *appBackend) pluginOllamaFacts(view *appwire.PluginProvider) {
	address := strings.TrimSuffix(strings.TrimRight(view.Address, "/"), "/v1")
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return
	}
	host := parsed.Hostname()
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return
	}
	start := false
	b.locked(func() {
		if b.closing {
			return
		}
		probe := &b.pluginOllama
		if probe.address != address {
			*probe = appPluginOllama{address: address}
		}
		view.Running = probe.running
		if !probe.probing && (probe.checked.IsZero() || time.Since(probe.checked) >= 5*time.Second) {
			probe.probing, start = true, true
		}
	})
	if !start {
		return
	}
	guard.Go("app/plugin-ollama-probe", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/api/tags", nil)
		running := false
		if err == nil {
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			if response, err := client.Do(request); err == nil {
				var listing struct {
					Models *[]json.RawMessage `json:"models"`
				}
				running = response.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&listing) == nil && listing.Models != nil
				_ = response.Body.Close()
			}
		}
		b.locked(func() {
			if b.pluginOllama.address == address {
				b.pluginOllama.running, b.pluginOllama.checked, b.pluginOllama.probing = &running, time.Now(), false
			}
		})
	})
}
