package main

import (
	"context"
	"errors"
	"log"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/modelsource"
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
		if err := vault.Run(p.lifetime(), session.KnowledgeHistory(session.PlacesRoot()), key); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("knowledge: background worker stopped: %v", err)
		}
	})
}
