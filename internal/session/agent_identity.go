package session

// agent_identity.go gives every conversation (an agent, to the app) a DID of
// its own, and hands it to every command the agent runs as $CODEAF_AGENT_DID
// (internal/agentdid). A worker the agent starts for a task carries the same
// DID with the task's fragment, `<did>#w<task id>`, so a tool that records who
// wrote something can tell the agent and each of its workers apart.
//
// MINTED ONCE, KEPT WITH THE CONVERSATION. The DID is part of the sealed truth
// (metatruth.go), so it survives a restart and travels with the conversation.
// It is minted when the conversation opens and written with its next identity
// write: a conversation that has not taken a turn since this build has none on
// disk, and a reader says nothing for it.
//
// BOUND TO THE CONVERSATION'S ID. A copy or a branch takes a new id and starts
// from a copy of the old files; the binding is what makes it mint its own
// rather than share the original's identity.

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/agentdid"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// AgentIdentity is a conversation's versioned public document and encrypted
// key custody, bound to the conversation id it was minted for.
type AgentIdentity = agentdid.Document

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
	if current.AgentIdentity != nil && current.ID == next.ID && (current.AgentIdentity.Unknown() || current.AgentDID() != "") {
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
		config.agentDocument = conversationAgentIdentity(config.Place)
		if config.agentDocument != nil {
			config.agentDID = (Meta{ID: config.Place.ID(), AgentIdentity: config.agentDocument}).AgentDID()
		}
		config.actorDID = config.agentDID
	}
	if config.InTask && config.taskID != 0 && config.agentDID != "" {
		config.actorDID = agentdid.Worker(config.agentDID, config.taskID)
	}
	if config.actorDID == "" {
		return
	}
	config.Seat = executor.WithEnvironment(config.seat(), []string{agentdid.AgentDIDEnv + "=" + config.actorDID})
}

// conversationAgentIdentity reads the kept document, or mints a fresh one.
//
// OPENING WRITES NOTHING. A conversation opens without touching its files (an
// older folder must resume exactly as it was), so a minted DID is carried on
// the agent and the conversation's next identity write records it
// ([Agent.fillMetaLocked]). That write comes at the latest when the first turn
// ends, and a turn is the only thing that runs commands, so every DID a
// command has seen is on disk once its turn is over.
func conversationAgentIdentity(place Place) *AgentIdentity {
	if meta, err := LoadMeta(place.Dir); err == nil {
		if meta.ID == place.ID() && meta.AgentIdentity != nil && (meta.AgentIdentity.Unknown() || meta.AgentDID() != "") {
			return meta.AgentIdentity
		}
	}
	doc, _, err := agentdid.Generate(place.ID())
	if err != nil {
		return nil
	}
	return &doc
}

// agentIdentity is the identity a conversation writes into its own folder, and
// nil for every agent that is not a conversation.
func (c Config) agentIdentity() *AgentIdentity {
	if c.InTask || c.agentDID == "" || c.Place.Dir == "" {
		return nil
	}
	return c.agentDocument
}

// AgentDID is who this agent's commands act as: the conversation's DID, or a
// worker's `<did>#w<task id>`. It is empty when the agent has no identity.
func (a *Agent) AgentDID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.config.actorDID
}

// protectAgentIdentity runs only on a meta write. Reading an older folder
// neither creates a home identity nor upgrades its public-only record.
func protectAgentIdentity(doc *AgentIdentity) (*AgentIdentity, error) {
	if doc == nil || !doc.NeedsProtection() {
		return doc, nil
	}
	// Like the vault, custody creates a quiet local profile on first use.
	// Existing paired or solo profiles retain their sync posture.
	root, err := identity.EnsureSolo(home.Dir())
	if err != nil {
		return nil, err
	}
	next, err := doc.Persist(root.CellKey())
	return &next, err
}

// SignStatement proves this conversation's DID for one versioned purpose.
// Private material stays inside custody and is never handed to command tools.
func (a *Agent) SignStatement(purpose string, payload []byte) ([]byte, error) {
	doc, keys, err := a.identityCredentials()
	if err != nil {
		return nil, err
	}
	return keys.Sign(doc.DID, purpose, payload)
}

// OpenSealed opens material addressed to this conversation's companion key.
func (a *Agent) OpenSealed(purpose string, box []byte) ([]byte, error) {
	doc, keys, err := a.identityCredentials()
	if err != nil {
		return nil, err
	}
	return keys.Open(*doc, purpose, box)
}

func (a *Agent) identityCredentials() (*AgentIdentity, *agentdid.Credentials, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	doc := a.config.agentDocument
	if doc == nil || doc.Session != a.config.Place.ID() {
		return nil, nil, agentdid.ErrUnavailable
	}
	// Load the durable upgrade if an earlier meta write supplied legacy custody.
	if meta, err := LoadMeta(a.config.Place.Dir); err == nil && meta.AgentDID() == doc.DID && meta.AgentIdentity.V == agentdid.Version {
		doc = meta.AgentIdentity
	}
	root, err := identity.Load(home.Dir())
	if err != nil {
		keys, pendingErr := doc.Unlock(nil)
		if pendingErr != nil {
			return nil, nil, err
		}
		return doc, keys, nil
	}
	keys, err := doc.Unlock(root.CellKey())
	return doc, keys, err
}
