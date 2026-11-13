package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// teamsFile keeps the organization's teams beside its go-ahead policies. It is
// not a number, so [Store.List] never reads it as a job.
const teamsFile = "factory-teams.json"

// Team is one group of people inside an organization (GET /v1/teams). Members
// are GitHub handles, normalized and sorted; a team with nobody in it keeps an
// empty list, never null.
type Team struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Members   []string  `json:"members"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Teams are every team this organization keeps, in the order they were saved.
// An organization that never made one answers none, never an error.
//
// A FILE THAT CANNOT BE READ IS AN ERROR, NOT AN EMPTY SET: a team list that
// silently came back empty would show a person outside every team they are in.
func (st *Store) Teams() ([]Team, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, teamsFile))
	if errors.Is(err, os.ErrNotExist) {
		return []Team{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Team
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("the organization's teams cannot be read")
	}
	if out == nil {
		out = []Team{}
	}
	for i := range out {
		if out[i].Members == nil {
			out[i].Members = []string{}
		}
	}
	return out, nil
}

// ChangeTeams rewrites the whole team list under a cross-process lock. change
// sees the teams kept now and edits them in place; an error from it writes
// nothing, so a refused change is never half-saved.
func (st *Store) ChangeTeams(change func(*[]Team) error) error {
	return st.underLock(filepath.Join(st.root, "factory-teams.lock"), func() error {
		all, err := st.Teams()
		if err != nil {
			return err
		}
		if err := change(&all); err != nil {
			return err
		}
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(st.root, teamsFile), append(raw, '\n'))
	})
}
