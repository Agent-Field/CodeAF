package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

const clientToolRemoteWait = 10 * time.Second

// ClientToolsRemote marks the app backend's forwarding implementation for hello.
func (b *appBackend) ClientToolsRemote() bool { return true }

func (b *appBackend) clientToolRemoteAgent(a *appAgent) *remote.Agent {
	var agent *remote.Agent
	b.locked(func() {
		if a.view.Location != nil && !a.view.Location.Here {
			return
		}
		if a.live != nil && !a.live.conn.Assembled && !a.live.conn.Lent {
			agent, _ = a.live.conn.Conv.(*remote.Agent)
		}
	})
	return agent
}

// The agent gate serializes snapshots with holds; the registry lock never spans a call.
func (b *appBackend) setRemoteClientTools(a *appAgent, agent *remote.Agent, p appwire.ClientToolsSetParams) (appwire.ClientToolsSetResult, error) {
	result := appwire.ClientToolsSetResult{Agent: p.Agent, Set: p.Set, Tools: []string{}}
	held := b.clientToolBuiltinNames(a)
	r := &b.clientTools
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return result, appwire.Errorf(appwire.CodeClosed, "the app connection is closed")
	}
	state := r.agents[p.Agent]
	for name := range state.builtins {
		held[name] = true
	}
	if err := clientToolReplacementValid(p, state.sets, held); err != nil {
		r.mu.Unlock()
		return result, err
	}
	sets := make([]session.ClientToolSet, 0, len(state.sets)+1)
	for _, set := range state.sets {
		if set.ID != p.Set {
			sets = append(sets, set)
		}
	}
	if len(p.Tools) > 0 {
		set := clientToolSnapshot(p)
		sets = append(sets, set)
		for _, tool := range set.Tools {
			result.Tools = append(result.Tools, tool.Name)
		}
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].ID < sets[j].ID })
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), clientToolRemoteWait)
	defer cancel()
	if _, err := agent.SetClientTools(ctx, sets); err != nil {
		var refusal *remote.ClientToolsRefusal
		if errors.As(err, &refusal) {
			return result, appwire.Errorf(appwire.CodeBadParams, "%s", refusal.Reason)
		}
		if errors.Is(err, remote.ErrClientToolsUnsupported) {
			return result, appwire.Errorf(appwire.CodeBadScope, "client tools need a conversation this engine runs")
		}
		return result, appwire.Errorf(appwire.CodeUnreachable, "%s", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return result, appwire.Errorf(appwire.CodeClosed, "the app connection is closed")
	}
	if r.agents == nil {
		r.agents = make(map[string]clientToolAgentSets)
	}
	r.agents[p.Agent] = clientToolAgentSets{version: state.version + 1, sets: sets, builtins: r.agents[p.Agent].builtins}
	return result, nil
}

func (b *appBackend) holdClientTools(a *appAgent, live *appLive) {
	if live.conn.Assembled || live.conn.Lent {
		return
	}
	agent, ok := live.conn.Conv.(*remote.Agent)
	if !ok || !agent.SupportsClientTools() {
		return
	}
	door := &appClientToolDoor{backend: b, agent: a.view.ID}
	agent.SetClientToolHandler(func(ctx context.Context, call session.ClientToolCall) (session.ClientToolAnswer, error) {
		// A holder may call immediately after accepting a snapshot. Wait for
		// that setter to publish the matching app registry before checking it.
		a.gate.Lock()
		a.gate.Unlock()
		return door.CallClientTool(ctx, call)
	})
	release := live.conn.Release
	live.conn.Release = func() {
		agent.SetClientToolHandler(nil)
		if release != nil {
			release()
		}
	}
	_, sets := door.ClientToolSets()
	ctx, cancel := context.WithTimeout(context.Background(), clientToolRemoteWait)
	defer cancel()
	if _, err := agent.SetClientTools(ctx, sets); err != nil {
		log.Printf("app/client-tools: could not attach tools for %s: %v", door.agent, err)
	}
}

func clientToolContextEnded(ctx context.Context, name string) session.ClientToolAnswer {
	if errors.Is(context.Cause(ctx), remote.ErrClientToolDisconnected) {
		return session.ClientToolAnswer{Text: fmt.Sprintf("client_unavailable: the app that provides %s disconnected before answering; the outcome is unknown, so check before trying again.", name), IsError: true}
	}
	return clientToolTurnEnded(name)
}

// A host process shares one hub across initial, fresh and resumed openings.
func appClientToolsEngineOpener(open func(session.Config) (*session.Agent, error)) (func(session.Config) (*session.Agent, error), *remote.ClientToolHub) {
	if appClientToolsBackend.Load() != nil {
		return appClientToolsOpener(open), nil
	}
	hub := &remote.ClientToolHub{}
	return func(cfg session.Config) (*session.Agent, error) {
		cfg.ClientTools = hub
		return open(cfg)
	}, hub
}
