package placegraph

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RememberUndoToken identifies the exact saved line across engine processes.
// A hash guards against removing a fact someone has edited since it was saved.
func RememberUndoToken(line Line) string {
	data, _ := json.Marshal(line)
	return fmt.Sprintf("remember_%s_%x", line.ID, sha256.Sum256(data))
}

// UndoRemember removes only the unchanged chat-authored line, under the same
// file lock as every graph mutation. Other windows' intervening work survives.
func (s *Store) UndoRemember(token string) (uint64, error) {
	rc, err := s.mutate(func(st *State, _ time.Time) (*change, error) {
		for i, line := range st.Lines {
			if !strings.HasPrefix(token, "remember_"+line.ID+"_") {
				continue
			}
			if line.Source.Kind != LineSaidInChat || line.Source.ChatID == "" || token != RememberUndoToken(line) {
				return nil, ErrRevisionConflict
			}
			for _, other := range st.Lines {
				if other.ReplacedBy == line.ID {
					return nil, ErrRevisionConflict
				}
			}
			st.Lines = append(st.Lines[:i:i], st.Lines[i+1:]...)
			return &change{ActionLineDelete, line.ID}, nil
		}
		return nil, ErrNotFound
	})
	return rc.AfterRevision, err
}
