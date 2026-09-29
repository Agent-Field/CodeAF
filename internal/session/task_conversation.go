package session

import (
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// savedTaskSummary recovers a command-only conversation from the run it saved.
// /senior-dev starts work without inserting a model-facing chat message, so an
// interrupted run may be the only durable record of the person's opening words.
func savedTaskSummary(transcript string) (Summary, bool) {
	if filepath.Base(transcript) != placeTranscript {
		return Summary{}, false
	}
	path := filepath.Join(filepath.Dir(transcript), planStoreFilename)
	// The current run is newest. Only consult older stores if it cannot
	// supply a brief, keeping the normal picker read to one root row.
	paths := append(planArchivePaths(path), path)
	for i := len(paths) - 1; i >= 0; i-- {
		if _, err := os.Stat(paths[i]); err != nil {
			continue
		}
		root, err := plandb.ReadRootPreview(paths[i])
		if err != nil {
			continue
		}
		opening := summaryLine(root.Description)
		if opening == "" {
			opening = summaryLine(root.Title)
		}
		if opening == "" {
			continue
		}
		return Summary{File: transcript, Opening: opening, Last: opening, LastUser: opening, At: root.CreatedAt}, true
	}
	return Summary{}, false
}

// HasSavedTasks protects task-only conversations from empty-launch cleanup.
// Even unreadable or partly written work must survive: a failed read is not
// evidence that a folder is disposable.
func HasSavedTasks(dir string) bool {
	place := Place{Dir: dir}
	path := filepath.Join(dir, planStoreFilename)
	for _, saved := range []string{place.Tasks(), place.NodeJournals(), path, path + ".1"} {
		if _, err := os.Lstat(saved); !os.IsNotExist(err) {
			return true
		}
	}
	return false
}
