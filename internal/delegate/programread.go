package delegate

import (
	"encoding/json"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/skills"
)

// ProgramRecordCap is the hard metadata read budget, including records this
// process wrote. JSON escaping can make a valid hello's stages exceed this
// bound; such a record is unavailable, not an empty or completed program.
// Covering every escaped hello needs a larger budget or bounded producer labels.
const ProgramRecordCap = maxLineBytes + 1<<20

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
