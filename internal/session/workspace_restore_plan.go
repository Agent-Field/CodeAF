package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// TaskCopyRestoreParents resolves typed incoming ground identities to tree IDs.
// It grants no path authority: every target must be another carried copy in the
// same conversation and project. Legacy records without GroundURI stay legacy.
func TaskCopyRestoreParents(place Place) (map[string]string, error) {
	return restoreParentsFromCheckpoint(place, place.Tasks())
}

// ForkTaskRestoreParents uses the original frontier while a fork is detaching
// it. A retry may already have replaced the operational checkpoint with history.
func ForkTaskRestoreParents(place Place) (map[string]string, error) {
	original := filepath.Join(ForkHistoryDir(place), "tasks.json")
	if _, err := os.Stat(original); errors.Is(err, os.ErrNotExist) {
		return TaskCopyRestoreParents(place)
	} else if err != nil {
		return nil, err
	}
	return restoreParentsFromCheckpoint(place, original)
}

func restoreParentsFromCheckpoint(place Place, path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	document, err := decodeTasks(raw)
	if err != nil {
		return nil, err
	}
	if codec, sealed := checkpointCodec(place.Tasks()); sealed {
		document = document.mapPaths(codec.resolve)
	}
	return incomingRestoreParents(place, document)
}

func incomingRestoreParents(place Place, document taskDocument) (map[string]string, error) {
	copies, err := restoreCopiesByIdentity(place, document)
	if err != nil {
		return nil, err
	}
	available := map[string]bool{}
	at := cell.Cell{ID: place.ID(), Root: place.Dir}
	for _, name := range taskcopy.Carried(at) {
		available[name] = true
	}
	parents := map[string]string{}
	for _, copy := range copies {
		name := filepath.Base(copy.Dir)
		if !available[name] || copy.GroundURI == "" || copy.GroundURI == copy.ProjectURI {
			continue
		}
		parent := copies[copy.GroundURI]
		if parent == nil {
			return nil, errors.New("task ground identity names a missing parent")
		}
		if parent.ChatURI != copy.ChatURI || parent.ProjectURI != copy.ProjectURI {
			return nil, errors.New("task parent belongs to another conversation or project")
		}
		if canonicalPath(copy.Ground) != canonicalPath(parent.Dir) {
			return nil, errors.New("task parent identity does not match its owned ground")
		}
		parents[name] = filepath.Base(parent.Dir)
	}
	if err := taskcopy.ValidateParentBindings(at, parents); err != nil {
		return nil, err
	}
	return parents, nil
}

func restoreCopiesByIdentity(place Place, document taskDocument) (map[string]*TaskCopyRecord, error) {
	records := incomingTaskCopyRecords(document)
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		return nil, err
	}
	byURI := map[string]*TaskCopyRecord{}
	names := map[string]string{}
	for _, record := range records {
		if record == nil || record.URI == "" || record.GroundURI == "" {
			continue
		}
		if err := validateIncomingRestoreCopy(place, record, meta.ChatURI); err != nil {
			return nil, err
		}
		if previous := byURI[record.URI]; previous != nil && (canonicalPath(previous.Dir) != canonicalPath(record.Dir) || previous.ChatURI != record.ChatURI || previous.ProjectURI != record.ProjectURI || previous.GroundURI != record.GroundURI) {
			return nil, errors.New("one incoming identity names conflicting task copies")
		}
		name := filepath.Base(record.Dir)
		if uri := names[name]; uri != "" && uri != record.URI {
			return nil, errors.New("several incoming identities name one task folder")
		}
		names[name], byURI[record.URI] = record.URI, record
	}
	return byURI, nil
}

func validateIncomingRestoreCopy(place Place, copy *TaskCopyRecord, expectedChatURI string) error {
	name := filepath.Base(copy.Dir)
	if copy.ChatURI == "" || copy.ProjectURI == "" || name == "." || canonicalPath(copy.Dir) != canonicalPath(filepath.Join(place.Trees(), name)) {
		return errors.New("incoming task identity has no owned conversation folder")
	}
	if copy.Mode == TaskModeInPlace || copy.Mode == TaskModeFolder {
		return errors.New("in-place project cannot be restored as an owned copy")
	}
	if expectedChatURI != "" && expectedChatURI != copy.ChatURI {
		return fmt.Errorf("incoming task belongs to another conversation")
	}
	return nil
}

func incomingTaskCopyRecords(document taskDocument) []*TaskCopyRecord {
	var records []*TaskCopyRecord
	for _, run := range document.Runs {
		records = append(records, run.Copy)
	}
	for _, node := range document.Nodes {
		records = append(records, &TaskCopyRecord{URI: node.WorkspaceURI, ChatURI: node.ChatURI, ProjectURI: node.ProjectURI, GroundURI: node.GroundURI, Dir: node.Worktree, Ground: node.Ground, Mode: node.Mode})
	}
	return records
}
