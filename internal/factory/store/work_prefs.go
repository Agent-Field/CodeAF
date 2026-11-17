package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A PERSON'S "START TASKS WHEN WHAT THEY WAIT ON IS DONE" (owner decisions
// 2026-11-17, 1). It is on unless the person turned it off, so the file keeps
// only the people who did: a person with no line here has the default. The
// server reads it for a task's owner when the last thing the task waited on is
// done (factory.StartsBySelf); the desk writes it from Settings › Work.

const workPrefsFile = "work-prefs.json"

// workPrefs is the file: each person's handle and whether their tasks start by
// themselves.
type workPrefs struct {
	StartWhenReady map[string]bool `json:"start_when_ready"`
}

func (st *Store) readWorkPrefs() (workPrefs, error) {
	var out workPrefs
	raw, err := os.ReadFile(filepath.Join(st.root, workPrefsFile))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return workPrefs{}, err
	}
	return out, nil
}

// StartWhenReady is whether person's tasks start by themselves when what they
// wait on is done: true unless they turned it off. A file that cannot be read
// answers the default with the error.
func (st *Store) StartWhenReady(person string) (bool, error) {
	prefs, err := st.readWorkPrefs()
	if err != nil {
		return true, err
	}
	on, set := prefs.StartWhenReady[factory.NormalizeHandle(person)]
	return on || !set, nil
}

// SetStartWhenReady keeps person's choice. On is the default, so it removes
// their line.
func (st *Store) SetStartWhenReady(person string, on bool) error {
	person = factory.NormalizeHandle(person)
	if person == "" {
		return errors.New("a setting belongs to a person")
	}
	return st.underLock(filepath.Join(st.root, "work-prefs.lock"), func() error {
		prefs, err := st.readWorkPrefs()
		if err != nil {
			return err
		}
		if prefs.StartWhenReady == nil {
			prefs.StartWhenReady = map[string]bool{}
		}
		if on {
			delete(prefs.StartWhenReady, person)
		} else {
			prefs.StartWhenReady[person] = false
		}
		raw, err := json.Marshal(prefs)
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(st.root, workPrefsFile), append(raw, '\n'))
	})
}
