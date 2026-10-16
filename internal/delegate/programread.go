package delegate

import (
	"encoding/json"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/skills"
)

// ProgramRecordCap is the most of a program record any reader reads. A record
// codeaf writes is a few hundred bytes; one larger than this was not written
// by it, and is no record.
const ProgramRecordCap = 1 << 20

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
