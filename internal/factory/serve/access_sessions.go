package serve

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const agentEntriesFile = "entries.jsonl"

func (h *Handler) keepAfiSession(ws factory.OrgWorkstream, person string) {
	if !personHandle.MatchString(person) {
		return
	}
	if err := h.config.Store.BackfillAfiSession(workstreamResource(ws).ref, person, h.clock()); err != nil {
		slog.Error("could not register Afi's conversation", "error", err)
	}
}

func (h *Handler) keepTaskSession(key taskRunKey) {
	if err := h.config.Store.BackfillTaskSession(key.ws, key.task, h.clock()); err != nil {
		slog.Error("could not register the task's conversation", "error", err)
	}
}

func (h *Handler) backfillSessions() {
	if h.config.Store != nil {
		if err := h.backfillSessionResources(); err != nil {
			slog.Error("could not register older sessions", "error", err)
		}
	}
}

func (h *Handler) backfillSessionResources() error {
	all, err := h.config.Store.Workstreams()
	if err != nil {
		return err
	}
	for _, ws := range all {
		if !safeWorkstreamFolder(ws.ID) {
			continue
		}
		root := h.sharedAgentDir(ws.ID)
		people, err := os.ReadDir(filepath.Join(root, "people"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, person := range people {
			if !person.IsDir() || !personHandle.MatchString(person.Name()) {
				continue
			}
			// A directory holding only named chats has no Afi pane to restore.
			dir := h.agentDir(agentSeat{ws: ws.ID, person: person.Name()})
			if existsOnDisk(filepath.Join(dir, agentEntriesFile)) || existsOnDisk(filepath.Join(dir, "agent")) {
				h.keepAfiSession(ws, person.Name())
			}
		}
		tasks, err := os.ReadDir(filepath.Join(root, "tasks"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, task := range tasks {
			if task.IsDir() {
				h.keepTaskSession(taskRunKey{ws.ID, task.Name()})
			}
		}
	}
	return nil
}

func existsOnDisk(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// taskSession is the task conversation whose registry id is scoped to a parent.
func taskSession(it Item) bool {
	return it.Kind == store.SessionKind && strings.HasPrefix(it.ID, store.TaskSessionPrefix)
}
