package appwire

import (
	"context"
	"encoding/json"
	"time"
)

// ── factory.agentfield_run: an AgentField job's run, as its tree ───────────
//
// A job filed with run_on is run by an AgentField reasoner, and the job's own
// read already carries every execution of that run as an ordinary step
// (steps[], with parent and group), each harness step's chat in
// factory.job_chats, and the person's Stop. This is the same run as a TREE:
// each top-level step with the calls it made folded under it, so a page can
// draw the top level and open a step to see inside it. It reads only what the
// job records; it asks the control plane nothing.
//
// A job that is not run on AgentField is not_found.

// FeatureFactoryAgentFieldRun is announced in hello when the engine serves
// factory.agentfield_run.
const FeatureFactoryAgentFieldRun = "factory.agentfield_run"

// FactoryAgentFieldRunParams names one job of one factory.
type FactoryAgentFieldRunParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
}

// FactoryAgentFieldRun is the run behind a job. Unknown fields are left out.
type FactoryAgentFieldRun struct {
	Job    int    `json:"job"`
	Target string `json:"target,omitempty"`
	// State is queued, running, done, failed or cancelled.
	State     string                  `json:"state,omitempty"`
	StartedAt *time.Time              `json:"started_at,omitempty"`
	EndedAt   *time.Time              `json:"ended_at,omitempty"`
	Error     string                  `json:"error,omitempty"`
	Steps     []FactoryAgentFieldStep `json:"steps"`
}

// FactoryAgentFieldStep is one execution of the run. Name is the step's name
// in the job's steps[]; Label the reasoner when the two differ. Agent is the
// step's chat, present only for a step that ran the codeaf harness, and
// Steerable is true only while that harness is working. Siblings sharing a
// Group ran at the same time.
type FactoryAgentFieldStep struct {
	ExecutionID string                  `json:"execution_id,omitempty"`
	Name        string                  `json:"name"`
	Label       string                  `json:"label,omitempty"`
	State       string                  `json:"state"`
	StartedAt   *time.Time              `json:"started_at,omitempty"`
	EndedAt     *time.Time              `json:"ended_at,omitempty"`
	Agent       string                  `json:"agent,omitempty"`
	Steerable   bool                    `json:"steerable,omitempty"`
	Transcript  bool                    `json:"transcript,omitempty"`
	Group       string                  `json:"group,omitempty"`
	Note        string                  `json:"note,omitempty"`
	Children    []FactoryAgentFieldStep `json:"children"`
}

// FactoryAgentFieldRunner is the engine's half of factory.agentfield_run.
type FactoryAgentFieldRunner interface {
	FactoryAgentFieldRun(ctx context.Context, p FactoryAgentFieldRunParams) (FactoryAgentFieldRun, error)
}

func withFactoryAgentFieldRunFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryAgentFieldRunner); ok {
		hello = withFeature(hello, FeatureFactoryAgentFieldRun)
	}
	return hello
}

func init() {
	methods["factory.agentfield_run"] = shaped[FactoryAgentFieldRunParams, FactoryAgentFieldRun](agentFieldRun)
}

func agentFieldRun(e Engine, raw json.RawMessage) (*FactoryAgentFieldRun, error) {
	const name = "factory.agentfield_run"
	r, ok := e.(FactoryAgentFieldRunner)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryAgentFieldRunParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	id, err := factoryID(p.Factory)
	if err != nil {
		return nil, err
	}
	p.Factory = id
	if p.Job <= 0 {
		return nil, Errorf(CodeBadParams, "job must be a job's number")
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryAgentFieldRun(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
