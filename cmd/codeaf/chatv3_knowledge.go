package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

func (p *v3Process) startKnowledge() {
	p.knowledgeDone = make(chan struct{})
	vault := knowledge.Open(config.ProfilePath(p.ProfileDir, ""))
	guard.Go("knowledge/background", func() {
		defer close(p.knowledgeDone)
		key := func() string {
			current := p.liveSettings(p.Settings)()
			service, ok := current.Sources.OrDefault(current.APIKey, current.BaseURL).ByID(modelsource.DefaultID)
			if !ok || !v3UsesDefaultOpenRouter(current) {
				return ""
			}
			return service.Key
		}
		if err := vault.Run(knowledgeCalls(p.lifetime(), p.ProfileDir, spendPreauthorized(false, os.Getenv)), session.KnowledgeHistory(session.PlacesRoot(), config.ProfilePath(p.ProfileDir, "")), key); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("knowledge: background worker stopped: %v", err)
		}
	})
}

// knowledgeCalls gives process-owned learning the same daily permission as turns.
func knowledgeCalls(ctx context.Context, profileDir string, preauthorized bool) context.Context {
	return knowledge.WithCompilerCalls(ctx, func() error { return session.DailyLimitReachedAt(profileDir, preauthorized) }, func(b provider.Billed) {
		session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{
			Role: "knowledge", Model: b.Model, Calls: 1,
			Input: b.PromptTokens, Output: b.CompletionTokens, USD: b.Cost,
		})
	})
}
