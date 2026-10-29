package session

// agent_identity.go gives every conversation (an agent, to the app) a DID of
// its own, and hands it to every command the agent runs as $CODEAF_AGENT_DID
// (internal/agentdid). A worker the agent starts for a task carries the same
// DID with the task's fragment, `<did>#w<task id>`, so a tool that records who
// wrote something can tell the agent and each of its workers apart.
//
// MINTED ONCE, KEPT WITH THE CONVERSATION. The DID is part of the sealed truth
// (metatruth.go), so it survives a restart and travels with the conversation.
// It is minted the first time the conversation opens: a conversation that
// never opens after this build has none, and a reader says nothing for it.
//
// BOUND TO THE CONVERSATION'S ID. A copy or a branch takes a new id and starts
// from a copy of the old files; the binding is what makes it mint its own
// rather than share the original's identity.

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/agentdid"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// AgentIdentity is one conversation's agent DID and the conversation id it
// was minted for.
type AgentIdentity struct {
	DID     string `json:"did"`
	Session string `json:"session"`
}

// AgentDID is the conversation's agent DID, or "" when it has none yet or the
// one it holds was minted for another id.
func (m Meta) AgentDID() string {
	if m.AgentIdentity == nil || strings.TrimSpace(m.ID) == "" || m.AgentIdentity.Session != m.ID || !agentdid.Valid(m.AgentIdentity.DID) {
		return ""
	}
	return m.AgentIdentity.DID
}

// keptAgentIdentity is the identity a save writes: the one already on disk for
// this id, else the one the save brings when it is bound to the save's id.
// Nothing replaces an identity once written.
func keptAgentIdentity(current, next Meta) *AgentIdentity {
	if current.AgentDID() != "" && current.ID == next.ID {
		return current.AgentIdentity
	}
	if next.AgentDID() != "" {
		return next.AgentIdentity
	}
	return nil
}

// configureAgentIdentity settles who this agent's commands act as. A
// conversation reads its DID from its folder, minting one when it has none; a
// task worker hangs its task's fragment off the DID it was handed; any other
// agent built inside one acts as whoever built it (newChildAgent).
//
// EVERY FAILURE IS SILENCE, for placemeta.go's reason: an identity that cannot
// be minted or written leaves the variable unset, and the conversation opens.
func configureAgentIdentity(config *Config) {
	if !config.InTask && config.Place.Dir != "" && config.agentDID == "" {
		config.agentDID = conversationAgentDID(config.Place)
		config.actorDID = config.agentDID
	}
	if config.InTask && config.taskID != 0 && config.agentDID != "" {
		config.actorDID = agentdid.Worker(config.agentDID, config.taskID)
	}
	if config.actorDID == "" {
		return
	}
	config.Seat = executor.WithEnvironment(config.seat(), []string{agentdid.Env + "=" + config.actorDID})
}

// conversationAgentDID answers the DID kept in place, minting one when it has
// none. A folder already holding a conversation gets the DID written at once,
// under the identity lock, so two processes opening it agree. A folder with no
// conversation in it yet is not written to: the DID is carried on the agent and
// the conversation's first identity write records it ([Agent.fillMetaLocked]).
func conversationAgentDID(place Place) string {
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		return ""
	}
	if did := meta.AgentDID(); did != "" {
		return did
	}
	if strings.TrimSpace(meta.ID) == "" {
		did, _ := agentdid.New()
		return did
	}
	var did string
	_ = withMetaLock(place.Dir, func() error {
		meta, err := LoadMeta(place.Dir)
		if err != nil || strings.TrimSpace(meta.ID) == "" {
			return err
		}
		if did = meta.AgentDID(); did != "" {
			return nil
		}
		minted, err := agentdid.New()
		if err != nil {
			return err
		}
		meta.AgentIdentity = &AgentIdentity{DID: minted, Session: meta.ID}
		if err := saveMetaLocked(place.Dir, meta); err != nil {
			return err
		}
		did = minted
		return nil
	})
	return did
}

// agentIdentity is the identity a conversation writes into its own folder, and
// nil for every agent that is not a conversation.
func (c Config) agentIdentity() *AgentIdentity {
	if c.InTask || c.agentDID == "" || c.Place.Dir == "" {
		return nil
	}
	return &AgentIdentity{DID: c.agentDID, Session: c.Place.ID()}
}

// AgentDID is who this agent's commands act as: the conversation's DID, or a
// worker's `<did>#w<task id>`. It is empty when the agent has no identity.
func (a *Agent) AgentDID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.config.actorDID
}
