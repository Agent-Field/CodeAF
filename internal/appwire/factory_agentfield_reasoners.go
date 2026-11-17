package appwire

import (
	"context"
	"encoding/json"
)

// ── factory.agentfield_reasoners: what an agentfield step may name ─────────
//
// A pipeline step of kind agentfield names a reasoner, node.reasoner, and maps
// the job into its input. This is the list a page picks one from: every
// reasoner of every node the factory's AgentField control plane knows, whether
// its node is online now, the input fields its schema names, and the job's
// facts a field may be mapped from (placeholders, spelled {title}). It reads
// the control plane now; a factory that is not connected to one answers
// conflict with why.

// FeatureFactoryAgentFieldReasoners is announced in hello when the engine
// serves factory.agentfield_reasoners.
const FeatureFactoryAgentFieldReasoners = "factory.agentfield_reasoners"

// FactoryAgentFieldReasonersParams names one factory.
type FactoryAgentFieldReasonersParams struct {
	Factory string `json:"factory"`
}

// FactoryAgentFieldReasoners is the reasoners a step may run, and the job's
// facts its input may be mapped from.
type FactoryAgentFieldReasoners struct {
	Reasoners    []FactoryAgentFieldReasoner `json:"reasoners"`
	Placeholders []string                    `json:"placeholders"`
}

// FactoryAgentFieldReasoner is one reasoner. Target is what a step names
// (node.reasoner); Input is the fields its input schema names, when it names
// any.
type FactoryAgentFieldReasoner struct {
	Node        string                   `json:"node"`
	Reasoner    string                   `json:"reasoner"`
	Target      string                   `json:"target"`
	Description string                   `json:"description,omitempty"`
	Online      bool                     `json:"online"`
	Input       []FactoryAgentFieldInput `json:"input,omitempty"`
}

// FactoryAgentFieldInput is one field of a reasoner's input.
type FactoryAgentFieldInput struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
	Type     string `json:"type,omitempty"`
}

// FactoryAgentFieldReasonerLister is the engine's half.
type FactoryAgentFieldReasonerLister interface {
	FactoryAgentFieldReasoners(ctx context.Context, p FactoryAgentFieldReasonersParams) (FactoryAgentFieldReasoners, error)
}

func withFactoryAgentFieldReasonersFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryAgentFieldReasonerLister); ok {
		hello = withFeature(hello, FeatureFactoryAgentFieldReasoners)
	}
	return hello
}

func init() {
	methods["factory.agentfield_reasoners"] = shaped[FactoryAgentFieldReasonersParams, FactoryAgentFieldReasoners](agentFieldReasoners)
}

func agentFieldReasoners(e Engine, raw json.RawMessage) (*FactoryAgentFieldReasoners, error) {
	const name = "factory.agentfield_reasoners"
	r, ok := e.(FactoryAgentFieldReasonerLister)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryAgentFieldReasonersParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	id, err := factoryID(p.Factory)
	if err != nil {
		return nil, err
	}
	p.Factory = id
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryAgentFieldReasoners(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
