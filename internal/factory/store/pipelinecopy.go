package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// pipelineCopiesFile keeps the factory's own copies of repositories' pipeline
// files ([factory.PipelineCopy]) beside its rules. It is not a number, so
// [Store.List] never reads it as a job.
const pipelineCopiesFile = "factory-pipelines.json"

// pipelineCopySlot is one repository's place in that file. Version counts
// every change to the slot, a save and a clear alike, and a cleared slot keeps
// its count, so a writer that read version 3 can never land on a slot that was
// saved and cleared since. A slot never written reads version 1.
type pipelineCopySlot struct {
	Version int                   `json:"version"`
	Copy    *factory.PipelineCopy `json:"copy,omitempty"`
}

// PipelineCopy is the factory's own copy of repo's pipeline file, nil when it
// keeps none, and the slot's version, which a write names as its expect.
//
// A FILE THAT CANNOT BE READ IS AN ERROR, NOT "NO COPY": steps that silently
// went back to main's file would run what the person replaced.
func (st *Store) PipelineCopy(repo string) (*factory.PipelineCopy, int, error) {
	slots, err := st.pipelineCopies()
	if err != nil {
		return nil, 0, err
	}
	slot := slots[factory.PipelineCopyKey(repo)]
	return slot.Copy.Clone(), slot.Version + 1, nil
}

// ChangePipelineCopy changes repo's copy under a cross-process lock. change
// sees the copy and version as they are and answers the copy to keep (nil to
// clear it) and whether to write at all; an error writes nothing. It answers
// the copy and version as they stand afterwards.
func (st *Store) ChangePipelineCopy(repo string, change func(cur *factory.PipelineCopy, version int) (*factory.PipelineCopy, bool, error)) (*factory.PipelineCopy, int, error) {
	key := factory.PipelineCopyKey(repo)
	var out *factory.PipelineCopy
	version := 0
	err := st.underLock(filepath.Join(st.root, "factory-pipelines.lock"), func() error {
		slots, err := st.pipelineCopies()
		if err != nil {
			return err
		}
		slot := slots[key]
		next, write, err := change(slot.Copy.Clone(), slot.Version+1)
		if err != nil {
			return err
		}
		if !write {
			out, version = slot.Copy.Clone(), slot.Version+1
			return nil
		}
		slot.Version++
		slot.Copy = next.Clone()
		slots[key] = slot
		raw, err := json.Marshal(slots)
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(st.root, pipelineCopiesFile), append(raw, '\n')); err != nil {
			return err
		}
		out, version = slot.Copy.Clone(), slot.Version+1
		return nil
	})
	return out, version, err
}

func (st *Store) pipelineCopies() (map[string]pipelineCopySlot, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, pipelineCopiesFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]pipelineCopySlot{}, nil
	}
	if err != nil {
		return nil, err
	}
	slots := map[string]pipelineCopySlot{}
	if err := json.Unmarshal(raw, &slots); err != nil {
		return nil, errors.New("the factory's pipeline copies file cannot be read")
	}
	return slots, nil
}
