package teams

import (
	"context"
	"encoding/json"
	"fmt"
)

// disk is the old teams.json's shape, kept for tests that state a store's
// rows as one JSON document: [storeRaw] writes it as rows, untidied, and
// [storedRaw] reads the rows back in it.
type disk struct {
	Version int    `json:"version"`
	Teams   []Team `json:"teams"`
}

// storeRaw makes the store hold exactly the teams raw lists, in order and
// with no repair, the way a store written by another build might be.
func storeRaw[T ~string | ~[]byte](dir string, raw T) error {
	var d disk
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return err
	}
	_, err := writeTeams(dir, nil, d.Teams)
	return err
}

// storedRaw is the store's rows as one JSON document, briefs left out. A
// store that is not there is an error, as a missing file was.
func storedRaw(dir string) ([]byte, error) {
	s, err := readPath(context.Background(), Path(dir))
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("no store in %s", dir)
	}
	return json.MarshalIndent(disk{Version: Version, Teams: s.teams}, "", "  ")
}
