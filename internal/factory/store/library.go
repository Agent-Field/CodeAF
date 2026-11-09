package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// libraryFile keeps the factory's pipeline library (internal/factory/pipeline,
// library.go): named pipelines and the steps they use, kept by the factory
// for every repository it works on, beside its rules and its copies of
// repositories' files. It is not a number, so [Store.List] never reads it as
// a job.
const libraryFile = "factory-library.json"

// ErrLibraryMoved refuses a write whose reader's version of the library is no
// longer the factory's. Nothing was written.
var ErrLibraryMoved = errors.New("the factory's pipeline library changed since it was read")

// KeptLibrary is the library as the factory keeps it: Text is the canonical
// YAML ("" for an empty library), Version counts every write from 1 (a
// library never written reads version 1), and the change says who wrote it
// last and when, absent when nobody ever did.
type KeptLibrary struct {
	Text    string `json:"text"`
	Version int    `json:"version"`
	factory.RulesChange
}

// librarySlot is the file as it is written: Writes counts the writes, so the
// version a reader names is Writes+1.
type librarySlot struct {
	Text   string `json:"text"`
	Writes int    `json:"writes"`
	factory.RulesChange
}

// PipelineLibrary is the factory's library. A FILE THAT CANNOT BE READ IS AN
// ERROR, NOT AN EMPTY LIBRARY: a rule naming a library pipeline must never
// quietly run something else.
func (st *Store) PipelineLibrary() (KeptLibrary, error) {
	slot, err := st.librarySlot()
	if err != nil {
		return KeptLibrary{}, err
	}
	return KeptLibrary{Text: slot.Text, Version: slot.Writes + 1, RulesChange: slot.RulesChange}, nil
}

// SetPipelineLibrary replaces the library's text under a cross-process lock
// when expect is the version the writer read, records by as its last change,
// and answers what was kept. text is kept as given: the caller has already
// checked and encoded it (pipeline.CheckLibrary). A version that moved since
// is [ErrLibraryMoved] and writes nothing; so does a text that is already the
// library's, which answers the library as it stands.
func (st *Store) SetPipelineLibrary(text string, by factory.RulesChange, expect int) (KeptLibrary, error) {
	var out KeptLibrary
	err := st.underLock(filepath.Join(st.root, "factory-library.lock"), func() error {
		slot, err := st.librarySlot()
		if err != nil {
			return err
		}
		if slot.Writes+1 != expect {
			return ErrLibraryMoved
		}
		if slot.Text == text {
			out = KeptLibrary{Text: slot.Text, Version: slot.Writes + 1, RulesChange: slot.RulesChange}
			return nil
		}
		next := librarySlot{Text: text, Writes: slot.Writes + 1, RulesChange: by}
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(st.root, libraryFile), append(raw, '\n')); err != nil {
			return err
		}
		out = KeptLibrary{Text: next.Text, Version: next.Writes + 1, RulesChange: next.RulesChange}
		return nil
	})
	return out, err
}

func (st *Store) librarySlot() (librarySlot, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, libraryFile))
	if errors.Is(err, os.ErrNotExist) {
		return librarySlot{}, nil
	}
	if err != nil {
		return librarySlot{}, err
	}
	var slot librarySlot
	if err := json.Unmarshal(raw, &slot); err != nil || slot.Writes < 0 {
		return librarySlot{}, errors.New("the factory's pipeline library file cannot be read")
	}
	return slot, nil
}
