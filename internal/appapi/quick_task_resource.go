package appapi

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) quicktaskRequest(w http.ResponseWriter, r *http.Request) {
	agent, local := r.PathValue("id"), ""
	if agent == "" {
		var err error
		agent, local, err = taskResourceParts(r.PathValue("qid"))
		if err != nil {
			fail(w, 404, "not_found", err.Error())
			return
		}
	}
	engine, ok := s.taskEngine(w, r, agent)
	if !ok {
		return
	}
	if r.Method == "GET" {
		rows, err := engine.AppQuickTasks()
		if err != nil {
			taskFailure(w, err)
			return
		}
		response := QuickTasksResponse{}
		for _, row := range rows {
			task := QuickTask{Version: row.Version, ID: taskResourceID(agent, row.ID), AgentID: agent, Title: row.Title, State: row.State, Added: row.Added, Removed: row.Removed, Files: []QuickTaskFile{}}
			for _, file := range row.Files {
				task.Files = append(task.Files, QuickTaskFile{file.Path, file.BeforeHash, file.AfterHash})
			}
			response = append(response, task)
		}
		write(w, 200, response)
		return
	}
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	intent, ok := decodeTaskAction(w, r, action)
	if !ok {
		return
	}
	intent.ID = local
	undone, changed, err := engine.AppQuickTaskAction(intent)
	if err != nil {
		taskFailure(w, err)
		return
	}
	if len(changed) > 0 {
		write(w, 409, ConflictResponse{ErrorResponse: ErrorResponse{Error: fmt.Sprintf("Can't undo cleanly: %d files changed since", len(changed)), Reason: "conflict", RequestID: fmt.Sprintf("%08x", requestSequence.Add(1))}, ChangedSince: changed})
		return
	}
	rows, err := engine.AppQuickTasks()
	if err == nil {
		for _, row := range rows {
			if row.ID == local {
				_, _ = s.events.Publish(EventQuickTaskChanged, QuickTaskChangedEvent{agent, taskResourceID(agent, local), row.State})
			}
		}
	}
	if action == "undo" {
		if undone == nil {
			undone = []string{}
		}
		write(w, 200, UndoQuickTaskResponse{Undone: undone})
		return
	}
	write(w, 200, ActionResponse{OK: true, Receipt: quickTaskActionReceipt(action, taskResourceID(agent, local))})
}

func quickTaskActionReceipt(action, id string) *DisplayEntry {
	text := map[string]string{"cancel": "Stopped by you", "promote": "Promoted to a task"}[action]
	if text == "" {
		return nil
	}
	return &DisplayEntry{Kind: "note", Text: text, Link: &SourceRef{Kind: "quick_task", ID: id}}
}
