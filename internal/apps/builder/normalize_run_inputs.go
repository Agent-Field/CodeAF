package builder

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func normalizeRunInputs(files map[string]string, app schema.App) ([]schema.Problem, error) {
	var notes []schema.Problem
	for _, path := range definition(files).RunFiles() {
		text, changed, err := schema.BindRunTrigger(app, []byte(files[path]))
		if err != nil || !changed {
			continue // Check reports malformed run files at their original path.
		}
		files[path] = string(text)
		notes = append(notes, schema.Problem{File: path, Code: normalizationNote,
			Message: fmt.Sprintf("Connected the record that starts %s to its input.", displayName(schema.FileID(path), "the automation"))})
	}
	return notes, nil
}
