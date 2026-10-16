package delegate

import (
	"encoding/json"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/skills"
)

// ProgramRecordCap is the most of a program record any reader reads. A record
// is a few hundred bytes in practice, but its stages are a hello's, and a hello
// may be a whole record line ([maxLineBytes]); the cap covers that and the
// record's other fields, so no record codeaf wrote is read as none, and a file
// larger than this was not written by codeaf.
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
