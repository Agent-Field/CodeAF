package serve

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Both normal delivery and retained-result recovery read the same compatibility
// metadata. Recovery never silently drops the step's model/chat/runtime evidence.
func workerReportHeaders(body *api.WorkerReport, headers http.Header) error {
	if raw := headers.Get(api.StepModelsHeader); raw != "" {
		var models map[string]string
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &models) != nil {
			return fmt.Errorf("invalid step models")
		}
		for i := range body.Steps {
			if model := models[body.Steps[i].Name]; model != "" {
				body.Steps[i].Model = model
			}
		}
	}
	if raw := headers.Get(api.StepChatsHeader); raw != "" {
		var chats map[string]string
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &chats) != nil {
			return fmt.Errorf("invalid step chats")
		}
		for i := range body.Steps {
			body.Steps[i].Chat = chats[body.Steps[i].Name]
		}
	}
	if raw := headers.Get(api.StepRuntimeHeader); raw != "" {
		var runtime map[string]api.StepRuntime
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &runtime) != nil {
			return fmt.Errorf("invalid step runtime")
		}
		known := map[string]bool{}
		for _, step := range body.Steps {
			known[step.Name] = true
		}
		for name, state := range runtime {
			if !known[name] || state.RoundLimit < 0 {
				return fmt.Errorf("step runtime names an unknown step or invalid limit")
			}
		}
		for i := range body.Steps {
			if state, ok := runtime[body.Steps[i].Name]; ok {
				body.Steps[i].RoundLimit, body.Steps[i].Verification = state.RoundLimit, state.Verification
			}
		}
	}
	return nil
}
