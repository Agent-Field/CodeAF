package main

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// SetAgentWorkstream writes which workstream the conversation belongs to
// without opening it ("" clears it). A held conversation follows the mark at
// its next turn's start (session/workstream_mark.go).
func (b *appBackend) SetAgentWorkstream(id, workstream string) (appwire.AgentWorkstream, error) {
	var a *appAgent
	b.locked(func() { a = b.agents[id] })
	if a == nil {
		var err error
		a, err = b.seenTarget(id)
		if err != nil {
			return appwire.AgentWorkstream{}, err
		}
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if err := b.lentFence(a, "workstream"); err != nil {
		return appwire.AgentWorkstream{}, err
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	if strings.TrimSpace(transcript) == "" {
		return appwire.AgentWorkstream{}, appwire.Errorf(appwire.CodeConflict, "this conversation has no local folder on this computer")
	}
	dir := session.DirOf(transcript)
	meta, err := session.LoadMeta(dir)
	if err != nil || meta.ID == "" {
		return appwire.AgentWorkstream{}, appwire.Errorf(appwire.CodeConflict, "this conversation has no local folder on this computer")
	}
	if _, err := session.SetConversationWorkstream(dir, workstream); err != nil {
		return appwire.AgentWorkstream{}, appEngineError(err)
	}
	return appwire.AgentWorkstream{ID: id, Workstream: session.WorkstreamAt(dir)}, nil
}
