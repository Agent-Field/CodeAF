//go:build !windows

package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// keptNoteFiles are the files in the folder's .senior-dev that the run and its
// model work from: the brief, the checklist, the pinned command and the
// messages taken. senior-dev writes the brief and the messages; the model
// writes the checklist and the pinned command with its own tools.
var keptNoteFiles = []string{seniorDevSpec, seniorDevChecklist, seniorDevPinned, steeringFile}

// keptNotes is what each of [keptNoteFiles] held the last time the run read
// it, keyed by its path relative to the workspace.
type keptNotes struct {
	mu    sync.Mutex
	files map[string][]byte
}

// readNote is a note file's content: what the folder holds now, kept for
// later; or, when the file is gone, what it last held, written back first.
//
// A .SENIOR-DEV REMOVED MID-RUN IS WRITTEN BACK FROM WHAT THE RUN LAST READ.
// The folder is the work's, and what the work runs can empty it: a
// benchmark's validate.py began `sudo rm -rf /src`, and OpenSSL 1.1.0's `make
// clean` deletes every link in the tree. The session store is kept elsewhere
// when there is anywhere else to keep it ([recordStore]); these files are
// the model's, it reads them by name, and a missing brief or checklist would
// end the run for want of a file the run itself knows the words of. Nothing
// else is mirrored, and nothing is retried: a file never read cannot be
// written back, and a write that fails is the read's error.
func (runner *pipeline) readNote(name string) ([]byte, error) {
	path := filepath.Join(runner.workspace, filepath.FromSlash(name))
	data, err := os.ReadFile(path)
	runner.kept.mu.Lock()
	if err == nil {
		if runner.kept.files == nil {
			runner.kept.files = map[string][]byte{}
		}
		runner.kept.files[name] = data
		runner.kept.mu.Unlock()
		return data, nil
	}
	last, known := runner.kept.files[name]
	runner.kept.mu.Unlock()
	if !known || !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, last, 0o644); err != nil {
		return nil, err
	}
	runner.events.stage("implement", notesRewrittenStatus, map[string]any{"file": name, "bytes": len(last)})
	return last, nil
}

// notesRewrittenStatus is the implement stage's record of a note file written
// back ([pipeline.readNote]).
const notesRewrittenStatus = "notes-rewritten"

// tendNotes reads every note file, keeping what each says and writing back
// each one that is gone. It runs after every tool call, which is where a
// command the model ran can have removed them, so the model's next read of
// its own brief finds it.
func (runner *pipeline) tendNotes() {
	for _, name := range keptNoteFiles {
		_, _ = runner.readNote(name)
	}
}
