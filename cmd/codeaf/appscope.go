package main

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// SetAgentScope writes the conversation's stored scope without opening it.
// A held conversation reads the word beside its next task-index reading.
func (b *appBackend) SetAgentScope(id, scope string) (appwire.AgentScope, error) {
	if scope != session.ScopeProject && scope != session.ScopeConversation {
		return appwire.AgentScope{}, appwire.Errorf(appwire.CodeBadParams, "scope is project or conversation")
	}
	var a *appAgent
	b.locked(func() { a = b.agents[id] })
	if a == nil {
		var err error
		a, err = b.seenTarget(id)
		if err != nil {
			return appwire.AgentScope{}, err
		}
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if err := b.lentFence(a, "scope"); err != nil {
		return appwire.AgentScope{}, err
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	if strings.TrimSpace(transcript) == "" {
		return appwire.AgentScope{}, appwire.Errorf(appwire.CodeConflict, "this conversation has no local folder on this computer")
	}
	dir := session.DirOf(transcript)
	meta, err := session.LoadMeta(dir)
	if err != nil || meta.ID == "" {
		return appwire.AgentScope{}, appwire.Errorf(appwire.CodeConflict, "this conversation has no local folder on this computer")
	}
	if _, err := session.SetConversationScope(dir, scope); err != nil {
		return appwire.AgentScope{}, appEngineError(err)
	}
	return appwire.AgentScope{ID: id, Scope: session.ConversationScopeAt(dir)}, nil
}
