package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// WorkstreamsFile keeps every organization workstream
// ([factory.OrgWorkstream]) beside the go-ahead policies. It is not a number,
// so [Store.List] never reads it as a job.
const WorkstreamsFile = "factory-workstreams.json"

// workstreamsLock serializes every change to [WorkstreamsFile] across
// processes, as the policies' lock does for theirs.
const workstreamsLock = "factory-workstreams.lock"

// Workstreams are every organization workstream this server keeps, archived
// ones included. A server that was never given one answers none.
//
// A FILE THAT CANNOT BE READ IS AN ERROR, NOT AN EMPTY SET: answering none
// would let the next save write an empty document over every workstream.
func (st *Store) Workstreams() (factory.OrgWorkstreams, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, WorkstreamsFile))
	if errors.Is(err, os.ErrNotExist) {
		return factory.OrgWorkstreams{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out factory.OrgWorkstreams
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("the organization's workstreams cannot be read")
	}
	if out == nil {
		out = factory.OrgWorkstreams{}
	}
	return out, nil
}

// Workstream is one workstream, and false when the server keeps none by id.
func (st *Store) Workstream(id string) (factory.OrgWorkstream, bool, error) {
	all, err := st.Workstreams()
	if err != nil {
		return factory.OrgWorkstream{}, false, err
	}
	w, ok := all[id]
	return w, ok, nil
}

// EditWorkstreams changes any number of workstreams in one save, under a
// cross-process lock, and answers every workstream as saved. edit sees them
// all, may add, change or delete entries, and names the ids it changed: each
// of those still present has its rev counted up by one, so a single save never
// moves a workstream it did not touch. An error from edit writes nothing.
//
// ONE SAVE FOR SEVERAL WORKSTREAMS is what lets a share that includes the
// linked ones land whole or not at all.
func (st *Store) EditWorkstreams(edit func(all factory.OrgWorkstreams) (changed []string, err error)) (factory.OrgWorkstreams, error) {
	var out factory.OrgWorkstreams
	err := st.underLock(filepath.Join(st.root, workstreamsLock), func() error {
		all, err := st.Workstreams()
		if err != nil {
			return err
		}
		before := make(map[string]int, len(all))
		for id, w := range all {
			before[id] = w.Rev
		}
		changed, err := edit(all)
		if err != nil {
			return err
		}
		if len(changed) == 0 {
			out = all
			return nil
		}
		for _, id := range changed {
			w, ok := all[id]
			if !ok {
				continue
			}
			w.ID, w.Rev = id, before[id]+1
			all[id] = w
		}
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		out = all
		return writeAtomic(filepath.Join(st.root, WorkstreamsFile), append(raw, '\n'))
	})
	return out, err
}

// TeamOwnsWorkstream is whether team owns any workstream, archived ones
// included: a team that owns one cannot be deleted, because a workstream with
// no owner has nobody to carry its budget or approve its go-aheads.
func (st *Store) TeamOwnsWorkstream(team string) (bool, error) {
	all, err := st.Workstreams()
	if err != nil {
		return false, err
	}
	for _, w := range all {
		if w.Owner.Kind == "team" && w.Owner.ID == team {
			return true, nil
		}
	}
	return false, nil
}

// SortWorkstreams orders ws newest change first, the id breaking a tie so the
// order never wobbles between two reads.
func SortWorkstreams(ws []factory.OrgWorkstream) {
	sort.Slice(ws, func(i, j int) bool {
		if !ws[i].UpdatedAt.Equal(ws[j].UpdatedAt) {
			return ws[i].UpdatedAt.After(ws[j].UpdatedAt)
		}
		return ws[i].ID > ws[j].ID
	})
}
