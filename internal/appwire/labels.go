package appwire

import "strings"

const AgentLabelsMax = 32

type AgentLabel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type AgentLabels struct {
	Labels     []AgentLabel `json:"labels"`
	Incomplete bool         `json:"incomplete,omitempty"`
}
type AgentLabelReader interface {
	AgentLabels(ids []string) (AgentLabels, error)
}

func CheckAgentLabelIDs(ids []string) error {
	if len(ids) > AgentLabelsMax {
		return Errorf(CodeBadParams, "at most %d agent labels may be requested", AgentLabelsMax)
	}
	for _, id := range ids {
		if id == "" || len(id) > 256 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "/\\\x00") || strings.HasPrefix(id, ".") {
			return Errorf(CodeBadParams, "an agent label needs a valid agent id")
		}
	}
	return nil
}
func init() {
	methods["agents.labels"] = typed(func(e Engine, p struct {
		IDs []string `json:"ids"`
	}) (*AgentLabels, error) {
		if err := CheckAgentLabelIDs(p.IDs); err != nil {
			return nil, err
		}
		reader, ok := e.(AgentLabelReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve agents.labels")
		}
		out, err := reader.AgentLabels(p.IDs)
		if err != nil {
			return nil, err
		}
		if out.Labels == nil {
			out.Labels = []AgentLabel{}
		}
		return &out, nil
	})
}
