package delegate

import (
	"encoding/json"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/skills"
)

// ReadProgramWithin reads a metadata record without trusting its size or file type.
// Missing, corrupt, unsafe or over-budget records carry no authoritative facts.
func ReadProgramWithin(dir string, limit int64) (ProgramRecord, bool) {
	data, err := skills.ReadWholeRegular(filepath.Join(dir, ProgramFile), limit)
	if err != nil {
		return ProgramRecord{}, false
	}
	var record ProgramRecord
	if json.Unmarshal(data, &record) != nil || record.Name == "" {
		return ProgramRecord{}, false
	}
	return record, true
}
