package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// CanvasesFile keeps who owns and who may see each organization canvas
// ([factory.OrgCanvas]). The canvases themselves are canvas-core directories
// under [Store.CanvasRoot]. It is not a number, so [Store.List] never reads it
// as a job.
const CanvasesFile = "factory-canvases.json"

// canvasesLock serializes every change to [CanvasesFile] across processes.
const canvasesLock = "factory-canvases.lock"

// CanvasRoot is the folder holding one canvas-core directory per
// organization canvas.
func (st *Store) CanvasRoot() string {
	return filepath.Join(st.root, "canvases")
}

// Canvases are every organization canvas this server keeps. A FILE THAT
// CANNOT BE READ IS AN ERROR, NOT AN EMPTY SET, so the next save never writes
// an empty index over every canvas.
func (st *Store) Canvases() (factory.OrgCanvases, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, CanvasesFile))
	if errors.Is(err, os.ErrNotExist) {
		return factory.OrgCanvases{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out factory.OrgCanvases
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("the organization's canvases cannot be read")
	}
	if out == nil {
		out = factory.OrgCanvases{}
	}
	return out, nil
}

// Canvas is one canvas's record, and false when the server keeps none by id.
func (st *Store) Canvas(id string) (factory.OrgCanvas, bool, error) {
	all, err := st.Canvases()
	if err != nil {
		return factory.OrgCanvas{}, false, err
	}
	c, ok := all[id]
	return c, ok, nil
}

// EditCanvases changes the canvas records in one save under a cross-process
// lock and answers them as saved. An error from edit writes nothing; edit
// answering false changed nothing and writes nothing.
func (st *Store) EditCanvases(edit func(all factory.OrgCanvases) (bool, error)) (factory.OrgCanvases, error) {
	var out factory.OrgCanvases
	err := st.underLock(filepath.Join(st.root, canvasesLock), func() error {
		all, err := st.Canvases()
		if err != nil {
			return err
		}
		changed, err := edit(all)
		if err != nil {
			return err
		}
		out = all
		if !changed {
			return nil
		}
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(st.root, CanvasesFile), append(raw, '\n'))
	})
	return out, err
}
