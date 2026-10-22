package store

import (
	"database/sql"
	"fmt"
)

const memoryMutationKinds = `kind IN ('memory_add','memory_update','memory_supersede','memory_forget','memory_restore','memory_rehomed','memory_moved')`

// A global memory-mutation watermark also sees a row moved out of the current
// owners. Neutral telemetry, proof events and graph activity never change it.
func (s *Store) MemoryMutationSeq(owners []string) (int64, error) {
	if len(owners) == 0 {
		return 0, fmt.Errorf("memory mutation seq: %w: no owners", ErrInvalid)
	}
	var seq int64
	err := s.db.QueryRow(`SELECT seq FROM events INDEXED BY memory_mutation_seq WHERE ` + memoryMutationKinds + ` ORDER BY seq DESC LIMIT 1`).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return seq, err
}
