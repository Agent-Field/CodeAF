package appapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type savedCrewRole struct {
	CrewRole
	Version uint64 `json:"version"`
}

// Roles have no conversation state. Atomic replacement lets member creation
// read a role while holding the team lock without reversing the writer locks.
func readCrewRoles(home string) (map[string]savedCrewRole, error) {
	roles := map[string]savedCrewRole{}
	raw, err := os.ReadFile(filepath.Join(home, "v3", "app", "roles.json"))
	if os.IsNotExist(err) {
		return roles, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &roles)
	return roles, err
}
func updateCrewRoles(home string, fn func(map[string]savedCrewRole) error) error {
	dir := filepath.Join(home, "v3", "app")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "roles.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	roles, err := readCrewRoles(home)
	if err != nil {
		return err
	}
	if err = fn(roles); err != nil {
		return err
	}
	raw, err := json.Marshal(roles)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".roles-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(raw)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "roles.json"))
}

type storedCrewRoles struct{ home string }

func (store storedCrewRoles) ReadCrewRole(_ context.Context, id string) (CrewRole, error) {
	roles, err := readCrewRoles(store.home)
	if err != nil {
		return CrewRole{}, err
	}
	role, ok := roles[id]
	if !ok {
		return CrewRole{}, agentError(404, "not_found", "no such role")
	}
	return role.CrewRole, nil
}
func roleCopies(f *teams.File, id string, d *agentDisk) ([]RoleCopy, error) {
	copies := []RoleCopy{}
	for _, t := range f.Teams {
		if t.Closed() {
			continue
		}
		meta, err := readCrewMeta(t)
		if err != nil {
			return nil, err
		}
		for _, member := range t.Members {
			mid := crewAgentID(d, member.Key)
			if meta.Members[mid].RoleID == id {
				copies = append(copies, RoleCopy{CrewID: t.ID, MemberID: mid})
			}
		}
	}
	sort.Slice(copies, func(i, j int) bool {
		if copies[i].CrewID != copies[j].CrewID {
			return copies[i].CrewID < copies[j].CrewID
		}
		return copies[i].MemberID < copies[j].MemberID
	})
	return copies, nil
}
