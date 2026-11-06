package appwire

// FeatureAgentScope names an engine that keeps a conversation's work scope.
const FeatureAgentScope = "agents.scope"

type AgentScope struct {
	ID    string `json:"id"`
	Scope string `json:"scope" enum:"project,conversation"`
}

type AgentScoper interface {
	SetAgentScope(id, scope string) (AgentScope, error)
}

func withAgentScopeFeature(h Hello, e Engine) Hello {
	if _, ok := e.(AgentScoper); ok {
		return withFeature(h, FeatureAgentScope)
	}
	return h
}

func init() {
	methods["agents.set_scope"] = typed(func(e Engine, p struct {
		ID    string `json:"id"`
		Scope string `json:"scope" enum:"project,conversation"`
	}) (AgentScope, error) {
		if p.ID == "" {
			return AgentScope{}, Errorf(CodeBadParams, "agents.set_scope needs an id")
		}
		if p.Scope != "project" && p.Scope != "conversation" {
			return AgentScope{}, Errorf(CodeBadParams, "scope is project or conversation")
		}
		scoper, ok := e.(AgentScoper)
		if !ok {
			return AgentScope{}, Errorf(CodeUnknownMethod, "this engine does not serve agents.set_scope")
		}
		return scoper.SetAgentScope(p.ID, p.Scope)
	})
}
