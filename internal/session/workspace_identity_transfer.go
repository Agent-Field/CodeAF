package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// AdoptTaskWorkspaceIdentities is called only after an accepted takeover has
// restored the named task copies, before any receiver agent opens the chat.
// It imports structured identity fields, never arbitrary paths from transcripts.
// A copy omitted by the restorer remains unbound and cannot silently reopen.
func AdoptTaskWorkspaceIdentities(place Place, project string, restoredIDs []string) error {
	if !WorkspaceOwnershipEnabled(place) || len(restoredIDs) == 0 {
		return nil
	}
	restored := make(map[string]bool, len(restoredIDs))
	for _, id := range restoredIDs {
		if !filepath.IsLocal(id) || filepath.Base(id) != id || id == "." {
			return errors.New("restored task identity leaves its conversation")
		}
		restored[id] = true
	}
	raw, err := os.ReadFile(place.Tasks())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	document, err := decodeTasks(raw)
	if err != nil {
		return err
	}
	if codec, sealed := checkpointCodec(place.Tasks()); sealed {
		document = document.mapPaths(codec.resolve)
	}
	project = canonicalPath(project)
	var incomingCopies []*TaskCopyRecord
	for index := range document.Runs {
		incomingCopies = append(incomingCopies, document.Runs[index].Copy)
	}
	// Legacy nodes keep the same identity fields directly rather than in a run
	// row. Adapt only those typed fields; their briefs and reports stay verbatim.
	type nodeCopy struct {
		index int
		copy  *TaskCopyRecord
	}
	var nodeCopies []nodeCopy
	for index := range document.Nodes {
		node := &document.Nodes[index]
		if node.WorkspaceURI == "" {
			continue
		}
		copy := &TaskCopyRecord{URI: node.WorkspaceURI, ChatURI: node.ChatURI, ProjectURI: node.ProjectURI, GroundURI: node.GroundURI, RootURI: node.RootURI, Dir: node.Worktree, Ground: node.Ground, Mode: node.Mode}
		nodeCopies = append(nodeCopies, nodeCopy{index, copy})
		incomingCopies = append(incomingCopies, copy)
	}
	var selected []*TaskCopyRecord
	for _, copy := range incomingCopies {
		if copy == nil || copy.URI == "" {
			continue
		}
		if copy.Mode == TaskModeInPlace || copy.Mode == TaskModeFolder {
			return errors.New("an in-place project cannot be imported as an owned task copy")
		}
		name := filepath.Base(copy.Dir)
		if !restored[name] {
			continue
		}
		actual := filepath.Join(place.Trees(), name)
		if canonicalPath(copy.Dir) != actual || copy.ChatURI == "" || copy.ProjectURI == "" {
			return errors.New("restored task has no matching structured workspace identity")
		}

		if info, err := os.Stat(actual); err != nil || !info.IsDir() {
			return fmt.Errorf("restored task %s directory is unavailable", name)
		}
		selected = append(selected, copy)
	}
	if len(selected) == 0 {
		return nil
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return err
	}

	// Every target is derived from a successfully restored copy or this take's
	// project. Historical projects and missing parents are never guessed.
	targets := make(map[string]string, len(selected)*2)
	for _, copy := range selected {
		actual := filepath.Join(place.Trees(), filepath.Base(copy.Dir))
		if previous, exists := targets[copy.URI]; exists && previous != actual {
			return errors.New("one workspace identity names several restored copies")
		}
		targets[copy.URI] = actual
		targets[copy.ProjectURI] = project
	}
	for _, copy := range selected {
		if copy.GroundURI == "" {
			copy.GroundURI = copy.ProjectURI
		}
		expected, known := targets[copy.GroundURI]
		if !known || canonicalPath(copy.Ground) != expected {
			return errors.New("restored task belongs to an unavailable project or parent; its identity was not rebound")
		}
		if copy.Root != "" {
			if copy.RootURI == "" {
				copy.RootURI = copy.ProjectURI
			}
			expected, known := targets[copy.RootURI]
			if !known || canonicalPath(copy.Root) != expected {
				return errors.New("restored task landing target was not restored")
			}
		}
	}
	for _, copy := range selected {
		if _, err := registry.Adopt(workspaceidentity.Record{URI: copy.ProjectURI, Role: workspaceidentity.Project}, project, workspaceidentity.Links{}); err != nil {
			return fmt.Errorf("adopt task project: %w", err)
		}
		if _, err := registry.Adopt(workspaceidentity.Record{URI: copy.ChatURI, Role: workspaceidentity.Chat}, canonicalPath(place.Dir), workspaceidentity.Links{}); err != nil {
			return fmt.Errorf("adopt task conversation: %w", err)
		}
	}
	adopted := make(map[string]bool, len(selected)*2)
	for _, copy := range selected {
		adopted[copy.ProjectURI] = true
	}
	pending := append([]*TaskCopyRecord(nil), selected...)
	for len(pending) > 0 {
		remaining := pending[:0]
		progressed := false
		for _, copy := range pending {
			if !adopted[copy.GroundURI] || copy.RootURI != "" && !adopted[copy.RootURI] {
				remaining = append(remaining, copy)
				continue
			}
			actual := targets[copy.URI]
			if _, err := registry.Adopt(workspaceidentity.Record{URI: copy.URI, Role: workspaceidentity.Run, OwnerURI: copy.ChatURI, ProjectURI: copy.ProjectURI}, actual, workspaceidentity.Links{OwnerURI: copy.ChatURI, ProjectURI: copy.ProjectURI}); err != nil {
				return fmt.Errorf("adopt restored task copy: %w", err)
			}
			copy.Dir, copy.Ground = actual, targets[copy.GroundURI]
			if strings.TrimSpace(copy.Root) != "" {
				copy.Root = targets[copy.RootURI]
			}
			adopted[copy.URI] = true
			progressed = true
		}
		if !progressed {
			return errors.New("restored task parent identities contain a cycle or missing parent")
		}
		pending = remaining
	}

	for _, entry := range nodeCopies {
		node := &document.Nodes[entry.index]
		node.Worktree, node.Ground = entry.copy.Dir, entry.copy.Ground
		node.GroundURI, node.RootURI = entry.copy.GroundURI, entry.copy.RootURI
	}
	return newTaskStore(place.Tasks()).writeLocked(document)
}

func resolveOwnedProject(uri string) (string, error) {
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return "", err
	}
	project, err := registry.Resolve(uri)
	if err != nil {
		return "", err
	}
	if project.Role != workspaceidentity.Project {
		return "", errors.New("working copy project identity does not name a project")
	}
	return project.Path, nil
}

// resolveOwnedGround resolves an exact project or parent-run landing target.
func resolveOwnedGround(uri string, record *TaskCopyRecord) (string, error) {
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return "", err
	}
	ground, err := registry.Resolve(uri)
	if err != nil {
		return "", err
	}
	if record == nil {
		return "", errors.New("task landing has no recorded owner")
	}
	switch ground.Role {
	case workspaceidentity.Project:
		if ground.URI != record.ProjectURI {
			return "", errors.New("task landing belongs to another project")
		}
	case workspaceidentity.Run:
		if ground.OwnerURI != record.ChatURI || ground.ProjectURI != record.ProjectURI {
			return "", errors.New("task landing belongs to another conversation or project")
		}
	default:
		return "", errors.New("task ground identity does not name a project or parent run")
	}
	return ground.Path, nil
}
