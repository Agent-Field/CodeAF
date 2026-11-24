package appwire

// FeatureAgentWorkstream names an engine that keeps which workstream a
// conversation belongs to (agents.set_workstream).
const FeatureAgentWorkstream = "agents.workstream"

// AgentWorkstream is a conversation's workstream mark: the workstream's id, or
// "" for an ordinary conversation.
type AgentWorkstream struct {
	ID         string `json:"id"`
	Workstream string `json:"workstream"`
}

// AgentWorkstreamer is an engine that marks conversations as a workstream's.
type AgentWorkstreamer interface {
	SetAgentWorkstream(id, workstream string) (AgentWorkstream, error)
}

func withAgentWorkstreamFeature(h Hello, e Engine) Hello {
	if _, ok := e.(AgentWorkstreamer); ok {
		return withFeature(h, FeatureAgentWorkstream)
	}
	return h
}

func init() {
	methods["agents.set_workstream"] = typed(func(e Engine, p struct {
		ID         string `json:"id"`
		Workstream string `json:"workstream"`
	}) (*AgentWorkstream, error) {
		if p.ID == "" {
			return nil, Errorf(CodeBadParams, "agents.set_workstream needs an id")
		}
		if len(p.Workstream) > 128 {
			return nil, Errorf(CodeBadParams, "workstream is an id of at most 128 characters")
		}
		marker, ok := e.(AgentWorkstreamer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve agents.set_workstream")
		}
		out, err := marker.SetAgentWorkstream(p.ID, p.Workstream)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
