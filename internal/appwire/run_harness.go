package appwire

import (
	"encoding/json"
	"strings"
)

// HarnessRunner is the optional engine door for running a saved shape as a turn.
type HarnessRunner interface {
	RunHarness(id, name, text, clientID string) (uint64, error)
}

type RunHarnessParams struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Text     string `json:"text"`
	ClientID string `json:"client_id,omitempty"`
}

func init() {
	methods["agents.run_harness"] = shaped[RunHarnessParams, TurnResult](func(e Engine, raw json.RawMessage) (*TurnResult, error) {
		var p RunHarnessParams
		if err := decodeStrict("agents.run_harness", raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "agents.run_harness needs an id, name and text")
		}
		runner, ok := e.(HarnessRunner)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve agents.run_harness")
		}
		turn, err := runner.RunHarness(p.ID, p.Name, p.Text, p.ClientID)
		if err != nil {
			return nil, err
		}
		return &TurnResult{Turn: turn}, nil
	})
}

func withRunHarnessFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(HarnessRunner); ok {
		return withFeature(hello, "agents.run_harness")
	}
	return hello
}
