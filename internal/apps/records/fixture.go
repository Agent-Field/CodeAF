package records

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// The walking skeleton serves the embedded CRM without a definition folder.
// Preserve its demo content until the store lane replaces it. Persisted apps,
// including a kept copy of this exact definition, always start with empty data.
func fixtureRows(def *spec.Bundle, scope svc.Scope, dir string) (map[string][]bridge.Record, error) {
	if scope.App != "crm" {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "definition")); err == nil {
		return nil, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	files, sample := spec.GoldenCRM()
	if len(def.Files) != len(files) {
		return nil, nil
	}
	for name, data := range files {
		if !bytes.Equal(def.Files[name], data) {
			return nil, nil
		}
	}
	var rows map[string][]bridge.Record
	err := json.Unmarshal(sample, &rows)
	return rows, err
}
