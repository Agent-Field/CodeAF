package session

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/exec"
)

// SetProgramAnswerAttribution credits the models in the loopback call record
// that answered, rather than the crew selected before the run began.
func SetProgramAnswerAttribution(folder *ProgramFolder, named bool) error {
	if folder == nil {
		return nil
	}
	// A log that cannot be read cannot prove that the configured seat answered.
	folder.NoAttribution = true
	folder.SignModel = ""
	models, err := delegate.AnsweredModels(folder.Keep)
	if err != nil {
		return err
	}
	folder.NoAttribution = len(models) == 0
	if named {
		bare := make([]string, 0, len(models))
		for _, model := range models {
			bare = append(bare, exec.BareModelName(model))
		}
		folder.SignModel = strings.Join(bare, ", ")
	}
	return nil
}
