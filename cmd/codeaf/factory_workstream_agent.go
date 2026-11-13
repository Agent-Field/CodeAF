package main

// ── AN ORGANIZATION WORKSTREAM'S AGENT, RUN BY `codeaf factory serve` ──────
//
// internal/factory/serve keeps the server agent's conversation, its state,
// who may talk to it and what it spent (workstream_agent.go) and never calls a
// model. This is the turn itself: the engine's own conversation machinery
// (internal/session), run with the organization's key and one door onto the
// workstream, so the server's agent is the same agent a laptop runs, with a
// belt of exactly one tool (session's tools_workstream.go).
//
// ITS PROFILE IS ITS OWN FOLDER, NEVER THE SERVER USER'S. A conversation reads
// keys, memory and settings from its profile; the server's agent is given an
// empty one under the workstream's folder, the organization's key by hand, and
// ONE MODEL for every call, so nothing it does can reach a key or a memory that
// belongs to whoever runs the server.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryWorkstreamAgentBase is where the runner's model API is: tests point
// it at a fake; otherwise it is CODEAF_BASE_URL when the server's process was
// started with one (an operator's own proxy in front of OpenRouter), else ""
// for the default model source, OpenRouter, the one provider an organization
// key is kept for. The server profile's own base_url is NOT read: it belongs
// to whoever signs in to the machine, and the organization's key must never
// be sent somewhere that profile chose.
var factoryWorkstreamAgentBase = ""

func factoryWorkstreamAgentBaseURL() string {
	if factoryWorkstreamAgentBase != "" {
		return factoryWorkstreamAgentBase
	}
	return strings.TrimSpace(env.Get("CODEAF_BASE_URL"))
}

// factoryWorkstreamAgent answers one message to a workstream's server agent.
func factoryWorkstreamAgent(st *store.Store, load func() (config.Config, error)) serve.AgentRunner {
	return func(ctx context.Context, turn serve.AgentTurn) (serve.AgentTurnEnd, error) {
		agent, err := session.New(session.Config{
			Workspace:   filepath.Join(turn.Dir, "work"),
			SessionFile: filepath.Join(turn.Dir, "talk.jsonl"),
			ProfileDir:  filepath.Join(turn.Dir, "profile"),
			Model:       factoryWorkstreamAgentModel(st, load),
			APIKey:      turn.Key, BaseURL: factoryWorkstreamAgentBaseURL(),
			OneModel: true, DeterministicNames: true, CompactEnabled: true,
			System:     factoryWorkstreamAgentSystem(turn.Name),
			Workstream: turn.Door,
		})
		if err != nil {
			return serve.AgentTurnEnd{}, err
		}
		defer agent.Close()
		before := agent.Usage().CostUSD
		events, err := agent.Submit(ctx, turn.Text)
		if err != nil {
			return serve.AgentTurnEnd{}, err
		}
		// A stop is the context ending; the conversation is interrupted the
		// way a person's stop interrupts it, and its stream then ends.
		stopped := make(chan struct{})
		defer close(stopped)
		go func() {
			select {
			case <-ctx.Done():
				agent.Interrupt()
			case <-stopped:
			}
		}()
		var said strings.Builder
		say := func() {
			if text := strings.TrimSpace(said.String()); text != "" {
				turn.Step(api.AgentEntryAgent, text, "", false)
			}
			said.Reset()
		}
		var failed error
		for e := range events {
			switch e.Kind {
			case session.EventTextDelta:
				said.WriteString(e.Text)
			case session.EventToolBegin:
				say()
			case session.EventToolEnd:
				turn.Step(api.AgentEntryStep, factoryAgentStepWords(e), e.Tool, false)
			case session.EventToolFailed:
				turn.Step(api.AgentEntryStep, factoryAgentStepWords(e), e.Tool, true)
			case session.EventError:
				if e.Err != nil {
					failed = e.Err
				}
			}
		}
		say()
		usd := max(agent.Usage().CostUSD-before, 0)
		if ctx.Err() != nil {
			return serve.AgentTurnEnd{USD: usd}, ctx.Err()
		}
		return serve.AgentTurnEnd{USD: usd}, failed
	}
}

// factoryAgentStepWords is one step as a person reads it ("read the
// workstream", "made the page Retry runbook"), never the call's arguments
// whole: a page's words are the page's, not the conversation's.
func factoryAgentStepWords(e session.Event) string {
	var args struct {
		Ask   string `json:"ask"`
		Title string `json:"title"`
	}
	if e.Tool == session.WorkstreamPagesTool && json.Unmarshal([]byte(e.Args), &args) == nil {
		switch args.Ask {
		case "read":
			return "read the workstream"
		case "page":
			return "read a page"
		case "write":
			return "wrote a page"
		case "new":
			if title := strings.TrimSpace(args.Title); title != "" {
				return "made the page " + title
			}
			return "made a page"
		case "decide":
			return "proposed a decision"
		}
	}
	if hint := strings.TrimSpace(e.Hint); hint != "" {
		return hint
	}
	return e.Tool
}

// factoryWorkstreamAgentModel is the model the server's agent answers on: the
// factory's default model, else this server's own model, else codeaf's.
func factoryWorkstreamAgentModel(st *store.Store, load func() (config.Config, error)) string {
	if st != nil {
		if spending, err := st.Spending(); err == nil && strings.TrimSpace(spending.DefaultModel) != "" {
			return spending.DefaultModel
		}
	}
	if load != nil {
		if settings, err := load(); err == nil && strings.TrimSpace(settings.Model) != "" {
			return settings.Model
		}
	}
	return config.DefaultModel
}

// factoryWorkstreamAgentSystem is what the server's agent is told it is.
func factoryWorkstreamAgentSystem(name string) string {
	return "You are the agent of the organization workstream \"" + name + "\", running on the organization's server. " +
		"Everyone who can see the workstream reads this conversation, and each message comes from one of them.\n\n" +
		"You have one tool, " + session.WorkstreamPagesTool + ": read the workstream (ask read) before you answer about it, " +
		"read one page whole (ask page), write a page (ask write), make a new page (ask new), or propose a decision (ask decide). " +
		"You write as the person whose message you are answering. When a write answers that your text was kept as a version, " +
		"say so plainly: the page is open for editing, and anyone who can edit it restores that version from its history.\n\n" +
		"You cannot run commands, read or change a repository, open files, start factory jobs or use the web. " +
		"When a person asks for that, say that the server's agent cannot, and that the workstream's pages are what you can change.\n\n" +
		"Answer briefly, in plain words."
}
