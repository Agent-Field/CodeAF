package runs

import (
	"fmt"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/extensions"
)

// AttachCustomTools pins the declared code from the same app version as the run
// so recovery does not substitute a newer program into an existing checkpoint.
func AttachCustomTools(def *Definition, app App, read func(string) ([]byte, error)) error {
	var attach func([]Step) error
	attach = func(steps []Step) error {
		for _, step := range steps {
			if id, ok := extensions.ToolID(step.Tool); ok {
				if app.Kit != 2 || !slices.Contains(app.Capabilities.Tools, step.Tool) {
					return fmt.Errorf("This app cannot call %q.", step.Tool)
				}
				if def.CustomTools == nil {
					def.CustomTools = map[string]extensions.Step{}
				}
				if _, exists := def.CustomTools[step.Tool]; !exists {
					raw, err := read("custom/" + id + ".step")
					if err != nil {
						return fmt.Errorf("The custom step %q is missing from this version.", id)
					}
					custom, err := extensions.Parse(raw)
					if err != nil {
						return fmt.Errorf("The custom step %q is invalid: %w", id, err)
					}
					def.CustomTools[step.Tool] = custom
				}
			}
			if err := attach(step.Steps); err != nil {
				return err
			}
		}
		return nil
	}
	return attach(def.Steps)
}
